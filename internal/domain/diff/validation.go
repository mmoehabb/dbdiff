package diff

import (
	"fmt"
	"github.com/mmoehabb/dbdiff/internal/domain/schema"
	"reflect"
	"slices"
	"strings"
)

func directionsEqual(a, b []bool, n int) bool {
	for i := 0; i < n; i++ {
		av, bv := false, false
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av != bv {
			return false
		}
	}
	return true
}
func indexOptionsEqual(a, b *schema.Index) bool {
	ac, bc := *a, *b
	ac.Name = ""
	bc.Name = ""
	ac.Columns = nil
	bc.Columns = nil
	ac.Descending = nil
	bc.Descending = nil
	ac.ExtendedProperties = nil
	bc.ExtendedProperties = nil
	ac.IncludedColumns = slices.Clone(a.IncludedColumns)
	bc.IncludedColumns = slices.Clone(b.IncludedColumns)
	slices.Sort(ac.IncludedColumns)
	slices.Sort(bc.IncludedColumns)
	if len(ac.IncludedColumns) == 0 {
		ac.IncludedColumns = nil
	}
	if len(bc.IncludedColumns) == 0 {
		bc.IncludedColumns = nil
	}
	return reflect.DeepEqual(ac, bc) && directionsEqual(a.Descending, b.Descending, len(a.Columns))
}
func columnDestructive(next, old *schema.Column) bool {
	if old.Nullable && !next.Nullable {
		return true
	}
	if next.Type.Kind != old.Type.Kind || next.Type.NativeType != old.Type.NativeType || next.Collation != old.Collation {
		return true
	}
	for _, pair := range [][2]*int{{next.Type.Length, old.Type.Length}, {next.Type.Precision, old.Type.Precision}, {next.Type.Scale, old.Type.Scale}} {
		if pair[0] != nil && pair[1] != nil && *pair[0] != *pair[1] && (*pair[0] != -1 && (*pair[1] == -1 || *pair[0] < *pair[1])) {
			return true
		}
	}
	return false
}
func validateSchemas(source, target *schema.Database) error {
	for _, db := range []*schema.Database{source, target} {
		if len(db.Unsupported) > 0 {
			return fmt.Errorf("database %s: unsupported metadata: %s", db.Name, strings.Join(db.Unsupported, "; "))
		}
		if len(db.PartitionFunctions) > 0 {
			return fmt.Errorf("partition function migrations are not supported")
		}
		for sn, s := range db.Schemas {
			if len(s.PartitionSchemes) > 0 {
				return fmt.Errorf("partition schemes in %s are not supported", sn)
			}
			for tn, t := range s.Tables {
				if t.IsSystemVersioned || t.IsReplicated || t.PartitionScheme != "" || len(t.Unsupported) > 0 {
					return fmt.Errorf("unsupported table %s.%s: temporal, replicated, partitioned, or specialized storage (%s)", sn, tn, strings.Join(t.Unsupported, "; "))
				}
				for _, c := range t.Columns {
					if c.Type.Kind == "" {
						return fmt.Errorf("unsupported type %s on %s.%s.%s", c.Type.NativeType, sn, tn, c.Name)
					}
				}
				for _, idx := range t.Indexes {
					if idx.Disabled {
						return fmt.Errorf("disabled index %s.%s.%s is not supported", sn, tn, idx.Name)
					}
				}
			}
		}
	}
	for _, dep := range target.Dependencies {
		if !dep.SchemaBound {
			continue
		}
		ts := target.Schemas[dep.RefSchema]
		if ts == nil {
			continue
		}
		old := ts.Tables[dep.RefObject]
		if old == nil {
			continue
		}
		var next *schema.Table
		if ss := source.Schemas[dep.RefSchema]; ss != nil {
			next = ss.Tables[dep.RefObject]
		}
		if next == nil { // Dropping both dependency and dependent is ordered by the planner.
			ss := source.Schemas[dep.Schema]
			if ss != nil && (ss.Views[dep.Object] != nil || ss.Functions[dep.Object] != nil || ss.Tables[dep.Object] != nil) {
				return fmt.Errorf("cannot drop %s.%s with retained schema-bound dependent %s.%s", dep.RefSchema, dep.RefObject, dep.Schema, dep.Object)
			}
			continue
		}
		for n, oc := range old.Columns {
			nc := next.Columns[n]
			if nc == nil || !reflect.DeepEqual(nc.Type, oc.Type) || nc.Collation != oc.Collation || nc.Nullable != oc.Nullable {
				return fmt.Errorf("column change on %s.%s requires rebuilding schema-bound dependent %s.%s", dep.RefSchema, dep.RefObject, dep.Schema, dep.Object)
			}
		}
	}
	if err := validateNames(source, target); err != nil {
		return err
	}
	for sn, s := range source.Schemas {
		ts := target.Schemas[sn]
		if ts == nil {
			continue
		}
		for tn, t := range s.Tables {
			old := ts.Tables[tn]
			if old == nil {
				continue
			}
			for name, columns := range old.UserStatistics {
				for _, column := range columns {
					before, after := old.Columns[column], t.Columns[column]
					if after == nil || before == nil || !reflect.DeepEqual(before.Type, after.Type) || before.Collation != after.Collation || before.Nullable != after.Nullable {
						return fmt.Errorf("column change on %s.%s requires rebuilding unsupported standalone statistics %s", sn, tn, name)
					}
				}
			}
			for cn, c := range t.Columns {
				oc := old.Columns[cn]
				if oc == nil {
					continue
				}
				if c.Identity != oc.Identity || c.IdentitySeed != oc.IdentitySeed || c.IdentityIncrement != oc.IdentityIncrement || c.Computed != oc.Computed || c.ComputedExpr != oc.ComputedExpr || c.Persisted != oc.Persisted || c.Generated != oc.Generated {
					return fmt.Errorf("%s.%s.%s requires an unsupported identity/computed/generated column rebuild", sn, tn, cn)
				}
			}
			for n, idx := range t.Indexes {
				if old.Statistics[n] && old.Indexes[n] == nil {
					return fmt.Errorf("index %s.%s.%s conflicts with existing standalone statistics", sn, tn, idx.Name)
				}
			}
		}
		for n, function := range s.Functions {
			if prev := ts.Functions[n]; prev != nil && function.Kind != prev.Kind {
				return fmt.Errorf("function %s.%s kind change from %q to %q requires an explicit drop/recreate migration", sn, n, prev.Kind, function.Kind)
			}
		}
		for n, seq := range s.Sequences {
			if prev := ts.Sequences[n]; prev != nil && (seq.StartValue != prev.StartValue || !reflect.DeepEqual(seq.Type, prev.Type)) {
				return fmt.Errorf("sequence %s.%s start/type change requires an explicit migration", sn, n)
			}
		}
	}
	return nil
}

func (d *SchemaDiffer) prepareDependencies(source, target *schema.Database, p *MigrationPlan) *schema.Database {
	// Copy all mutable maps: comparison must never modify caller-owned snapshots.
	copyDB := *target
	copyDB.Schemas = map[string]*schema.Schema{}
	affected := map[tableRef]map[string]bool{}
	keyChanged := map[tableRef]bool{}
	for sn, s := range target.Schemas {
		cs := *s
		cs.Tables = map[string]*schema.Table{}
		copyDB.Schemas[sn] = &cs
		for tn, t := range s.Tables {
			ct := *t
			ct.Indexes = map[string]*schema.Index{}
			for n, v := range t.Indexes {
				ct.Indexes[n] = v
			}
			ct.ForeignKeys = map[string]*schema.ForeignKey{}
			for n, v := range t.ForeignKeys {
				ct.ForeignKeys[n] = v
			}
			ct.Checks = map[string]*schema.CheckConstraint{}
			for n, v := range t.Checks {
				ct.Checks[n] = v
			}
			cs.Tables[tn] = &ct
			var next *schema.Table
			if ss := source.Schemas[sn]; ss != nil {
				next = ss.Tables[tn]
			}
			ref := tableRef{sn, tn}
			affected[ref] = map[string]bool{}
			if next == nil {
				for cn := range t.Columns {
					affected[ref][cn] = true
				}
				keyChanged[ref] = true
				continue
			}
			for cn, oc := range t.Columns {
				nc := next.Columns[cn]
				if nc == nil || !reflect.DeepEqual(nc.Type, oc.Type) || nc.Nullable != oc.Nullable || nc.Collation != oc.Collation {
					affected[ref][cn] = true
				}
			}
			if t.PrimaryKey != nil && (next.PrimaryKey == nil || !d.isPrimaryKeyEqual(next.PrimaryKey, t.PrimaryKey)) {
				keyChanged[ref] = true
			}
			for n, idx := range t.Indexes {
				ni := next.Indexes[n]
				if idx.IsUnique && (ni == nil || !d.isIndexEqual(ni, idx)) {
					keyChanged[ref] = true
				}
			}
		}
	}
	touches := func(cols []string, set map[string]bool) bool {
		for _, c := range cols {
			if set[c] {
				return true
			}
		}
		return false
	}
	for sn, s := range copyDB.Schemas {
		for tn, t := range s.Tables {
			ref := tableRef{sn, tn}
			changed := affected[ref]
			var next *schema.Table
			if ss := source.Schemas[sn]; ss != nil {
				next = ss.Tables[tn]
			}
			if next != nil {
				if t.PrimaryKey != nil && touches(t.PrimaryKey.Columns, changed) {
					p.SchemaOperations = append(p.SchemaOperations, DropPrimaryKeyOperation{ConstraintName: t.PrimaryKey.Name, SchemaName: sn, TableName: tn})
					t.PrimaryKey = nil
					keyChanged[ref] = true
				}
				for n, idx := range t.Indexes {
					if touches(idx.Columns, changed) || touches(idx.IncludedColumns, changed) || (idx.Filter != "" && len(changed) > 0) {
						p.SchemaOperations = append(p.SchemaOperations, DropIndexOperation{SchemaName: sn, TableName: tn, IndexName: n, IsConstraint: idx.IsConstraint})
						delete(t.Indexes, n)
						if idx.IsUnique {
							keyChanged[ref] = true
						}
					}
				}
				if len(changed) > 0 {
					for n := range t.Checks {
						p.SchemaOperations = append(p.SchemaOperations, DropCheckOperation{sn, tn, n})
						delete(t.Checks, n)
					}
				}
			}
		}
	}
	for sn, s := range copyDB.Schemas {
		for tn, t := range s.Tables {
			ref := tableRef{sn, tn}
			for n, fk := range t.ForeignKeys {
				rr := parseRefTable(sn, fk.RefTable)
				if touches(fk.Columns, affected[ref]) || touches(fk.RefColumns, affected[rr]) || keyChanged[rr] {
					p.SchemaOperations = append(p.SchemaOperations, DropForeignKeyOperation{sn, tn, n})
					delete(t.ForeignKeys, n)
				}
			}
		}
	}
	return &copyDB
}

func validateNames(source, target *schema.Database) error {
	check := func(a, b []string, scope string) error {
		for _, x := range a {
			for _, y := range b {
				if x != y && strings.EqualFold(x, y) {
					return fmt.Errorf("case-only name difference in %s: %q versus %q requires an explicit rename", scope, x, y)
				}
			}
		}
		return nil
	}
	names := func(v interface{}) []string {
		rv := reflect.ValueOf(v)
		var out []string
		for _, k := range rv.MapKeys() {
			out = append(out, k.String())
		}
		return out
	}
	if err := check(names(source.Schemas), names(target.Schemas), "schemas"); err != nil {
		return err
	}
	for sn, s := range source.Schemas {
		old := target.Schemas[sn]
		if old == nil {
			continue
		}
		if err := check(names(s.Tables), names(old.Tables), sn); err != nil {
			return err
		}
		for tn, t := range s.Tables {
			ot := old.Tables[tn]
			if ot == nil {
				continue
			}
			for _, pair := range [][2]interface{}{{t.Columns, ot.Columns}, {t.Indexes, ot.Indexes}, {t.ForeignKeys, ot.ForeignKeys}, {t.Checks, ot.Checks}} {
				if err := check(names(pair[0]), names(pair[1]), sn+"."+tn); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
