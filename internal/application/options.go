package application

import (
	"fmt"
	"github.com/mmoehabb/dbdiff/internal/domain/diff"
	"github.com/mmoehabb/dbdiff/internal/domain/schema"
	"reflect"
	"slices"
)

type CompareOptions struct {
	Tables      []string
	Exclude     bool
	IncludeData bool
}

// SelectTables retains the full snapshots for checking dependencies outside the selection.
// A table filter intentionally excludes standalone schema objects and database properties.
func SelectTables(source, target *schema.Database, options CompareOptions) (*schema.Database, *schema.Database, error) {
	if len(options.Tables) == 0 {
		return source, target, nil
	}
	selected := map[string]bool{}
	for _, n := range options.Tables {
		parts, err := schema.ParseQualifiedName(n)
		if err != nil || len(parts) != 2 {
			return nil, nil, fmt.Errorf("table selection must be schema-qualified: %q", n)
		}
		selected[parts[0]+"\x00"+parts[1]] = true
	}
	includes := func(s, t string) bool { return selected[s+"\x00"+t] != options.Exclude }
	filter := func(db *schema.Database) *schema.Database {
		out := *db
		out.Schemas = map[string]*schema.Schema{}
		out.PartitionFunctions = nil
		out.ExtendedProperties = nil
		for sn, s := range db.Schemas {
			ns := &schema.Schema{Name: sn, Tables: map[string]*schema.Table{}}
			for tn, t := range s.Tables {
				if includes(sn, tn) {
					ns.Tables[tn] = t
				}
			}
			// New namespaces belong to the selection only when they own selected tables.
			// Existing target namespaces must survive filtering to prevent schema drops.
			if len(ns.Tables) > 0 || target.Schemas[sn] != nil {
				out.Schemas[sn] = ns
			}
		}
		return &out
	}
	if options.IncludeData {
		for sn, s := range target.Schemas {
			for tn, t := range s.Tables {
				if includes(sn, tn) {
					continue
				}
				for _, fk := range t.ForeignKeys {
					parts, err := schema.ParseQualifiedName(fk.RefTable)
					if err != nil {
						return nil, nil, err
					}
					rs, rn := sn, parts[0]
					if len(parts) == 2 {
						rs, rn = parts[0], parts[1]
					}
					if includes(rs, rn) {
						return nil, nil, fmt.Errorf("data migration of %s.%s has dependent table %s.%s outside the selection", rs, rn, sn, tn)
					}
				}
			}
		}
	}
	a, b := filter(source), filter(target)
	// A selected table cannot introduce a reference to an absent or incompatible unselected parent.
	for sn, s := range a.Schemas {
		for _, t := range s.Tables {
			for _, fk := range t.ForeignKeys {
				parts, err := schema.ParseQualifiedName(fk.RefTable)
				if err != nil {
					return nil, nil, err
				}
				rs, rn := sn, parts[0]
				if len(parts) == 2 {
					rs, rn = parts[0], parts[1]
				}
				if includes(rs, rn) {
					continue
				}
				var parent *schema.Table
				if ts := target.Schemas[rs]; ts != nil {
					parent = ts.Tables[rn]
				}
				if parent == nil {
					return nil, nil, fmt.Errorf("selected table %s.%s references absent unselected parent %s.%s", sn, t.Name, rs, rn)
				}
				unique := parent.PrimaryKey != nil && slices.Equal(parent.PrimaryKey.Columns, fk.RefColumns)
				for _, idx := range parent.Indexes {
					unique = unique || (idx.IsUnique && idx.Filter == "" && slices.Equal(idx.Columns, fk.RefColumns))
				}
				if !unique {
					return nil, nil, fmt.Errorf("unselected parent %s.%s lacks the referenced unique key", rs, rn)
				}
				if len(fk.Columns) != len(fk.RefColumns) {
					return nil, nil, fmt.Errorf("invalid foreign key column mapping for %s", fk.Name)
				}
				for i, col := range fk.RefColumns {
					pc := parent.Columns[col]
					cc := t.Columns[fk.Columns[i]]
					if pc == nil || cc == nil || !reflect.DeepEqual(pc.Type, cc.Type) || pc.Collation != cc.Collation {
						return nil, nil, fmt.Errorf("unselected parent %s.%s has incompatible referenced columns", rs, rn)
					}
				}
			}
		}
	}
	for _, dep := range source.Dependencies {
		if !includes(dep.Schema, dep.Object) {
			continue
		}
		if ss := a.Schemas[dep.Schema]; ss == nil || ss.Tables[dep.Object] == nil {
			continue
		}
		if dep.Schema == dep.RefSchema && dep.Object == dep.RefObject {
			continue
		}
		ts := target.Schemas[dep.RefSchema]
		ss := a.Schemas[dep.RefSchema]
		creatingTable := ss != nil && ss.Tables[dep.RefObject] != nil
		if !creatingTable && (ts == nil || (ts.Tables[dep.RefObject] == nil && ts.Sequences[dep.RefObject] == nil && ts.Functions[dep.RefObject] == nil && ts.Views[dep.RefObject] == nil && ts.Synonyms[dep.RefObject] == nil)) {
			return nil, nil, fmt.Errorf("selected table %s.%s requires unselected dependency %s.%s", dep.Schema, dep.Object, dep.RefSchema, dep.RefObject)
		}
	}
	// Retain namespaces on both sides so table selection cannot drop a schema.
	for sn := range b.Schemas {
		if a.Schemas[sn] == nil {
			a.Schemas[sn] = &schema.Schema{Name: sn}
		}
	}
	// FK dependencies must be inspected even when the child is outside the selection.
	d := diff.NewSchemaDiffer()
	plan, err := d.Compare(a, b)
	if err != nil {
		return nil, nil, err
	}
	for _, op := range plan.SchemaOperations {
		sn, tn := diff.Scope(op)
		switch op.OperationType() {
		case diff.AlterColumn, diff.DropColumn, diff.DropPrimaryKey, diff.DropIndex, diff.DropTable:
			for cs, s := range target.Schemas {
				for ct, t := range s.Tables {
					if includes(cs, ct) {
						continue
					}
					for _, fk := range t.ForeignKeys {
						parts, err := schema.ParseQualifiedName(fk.RefTable)
						if err != nil {
							return nil, nil, err
						}
						rs, rn := cs, parts[0]
						if len(parts) == 2 {
							rs, rn = parts[0], parts[1]
						}
						if rs == sn && rn == tn {
							return nil, nil, fmt.Errorf("%s.%s requires changing foreign key %s on unselected table %s.%s", sn, tn, fk.Name, cs, ct)
						}
					}
				}
			}
			for _, dep := range target.Dependencies {
				if dep.RefSchema == sn && dep.RefObject == tn && dep.SchemaBound && (dep.Schema != sn || dep.Object != tn) {
					return nil, nil, fmt.Errorf("%s.%s has an external schema-bound dependent %s.%s", sn, tn, dep.Schema, dep.Object)
				}
			}
		}
	}
	return a, b, nil
}
