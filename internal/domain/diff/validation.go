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
	for side, db := range []*schema.Database{source, target} {
		if len(db.Unsupported) > 0 {
			return fmt.Errorf("%s database %q: incomplete or unsupported metadata: %s", []string{"source", "target"}[side], db.Name, strings.Join(db.Unsupported, "; "))
		}
	}
	return validateNames(source, target)
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
