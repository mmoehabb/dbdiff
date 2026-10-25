package diff

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"

	"github.com/mmoehabb/dbdiff/internal/domain/schema"
)

type tableRef struct{ Schema, Name string }

func parseRefTable(current, ref string) tableRef {
	parts, err := schema.ParseQualifiedName(ref)
	if err != nil {
		return tableRef{}
	}
	if len(parts) == 1 {
		return tableRef{current, parts[0]}
	}
	if len(parts) == 2 {
		return tableRef{parts[0], parts[1]}
	}
	return tableRef{}
}
func sortTableRefs(refs []tableRef) {
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Schema == refs[j].Schema {
			return refs[i].Name < refs[j].Name
		}
		return refs[i].Schema < refs[j].Schema
	})
}
func topologicalSortTables(tables map[tableRef]*schema.Table) ([]tableRef, error) {
	degrees := map[tableRef]int{}
	adj := map[tableRef][]tableRef{}
	for ref := range tables {
		degrees[ref] = 0
	}
	for ref, t := range tables {
		for _, fk := range t.ForeignKeys {
			parent := parseRefTable(ref.Schema, fk.RefTable)
			if _, ok := tables[parent]; ok {
				degrees[ref]++
				adj[parent] = append(adj[parent], ref)
			}
		}
	}
	var queue, ordered []tableRef
	for ref, n := range degrees {
		if n == 0 {
			queue = append(queue, ref)
		}
	}
	for len(queue) > 0 {
		sortTableRefs(queue)
		ref := queue[0]
		queue = queue[1:]
		ordered = append(ordered, ref)
		for _, child := range adj[ref] {
			degrees[child]--
			if degrees[child] == 0 {
				queue = append(queue, child)
			}
		}
	}
	if len(ordered) != len(tables) {
		return nil, fmt.Errorf("data migration with cyclic or self-referencing foreign keys is unsupported")
	}
	return ordered, nil
}

type DataReader interface {
	ReadTableData(context.Context, string, *schema.Table) ([]map[string]interface{}, error)
}
type DataDiffer struct {
	sourceReader, targetReader DataReader
	OmittedTables              []string
	OmittedReasons             map[string]string
	Incomplete                 bool
}

func NewDataDiffer(source, target DataReader) *DataDiffer {
	return &DataDiffer{sourceReader: source, targetReader: target}
}
func (d *DataDiffer) CompareData(ctx context.Context, source, target *schema.Database, skipped ...map[string]bool) ([]Operation, error) {
	d.OmittedTables = nil
	d.Incomplete = false
	d.OmittedReasons = map[string]string{}
	if source == nil || target == nil {
		return nil, fmt.Errorf("source and target schemas are required")
	}
	tables := map[tableRef]*schema.Table{}
	for sn, s := range source.Schemas {
		for tn, t := range s.Tables {
			tables[tableRef{sn, tn}] = t
		}
	}
	blocked := map[string]bool{}
	capabilityBlocked := map[string]bool{}
	for _, m := range skipped {
		for k, v := range m {
			blocked[k] = v
		}
	}
	for ref, t := range tables {
		for side, table := range []*schema.Table{t, tableAt(target, ref.Schema, ref.Name)} {
			if reason := dataFeatureReason(table); reason != "" {
				k := key(ref.Schema, ref.Name)
				blocked[k] = true
				capabilityBlocked[k] = true
				d.OmittedReasons[k] = fmt.Sprintf("%s database %q, %s.%s: %s; data synchronization requires an explicit migration", []string{"source", "target"}[side], []*schema.Database{source, target}[side].Name, ref.Schema, ref.Name, reason)
			}
		}
	}

	// A skipped target-only child can still be changed by a parent's cascade.
	for sn, s := range target.Schemas {
		for tn, t := range s.Tables {
			if tableAt(source, sn, tn) == nil && dataFeatureReason(t) != "" {
				k := key(sn, tn)
				blocked[k] = true
				capabilityBlocked[k] = true
			}
		}
	}
	// Keep each connected FK component together. Skipping only children would
	// still allow a parent DELETE/UPDATE to cascade into a skipped special table,
	// or fail because a retained child still references it. Include both snapshots
	// and target-only children whose schema removal may have been omitted.
	for changed := true; changed; {
		changed = false
		for _, db := range []*schema.Database{source, target} {
			for sn, s := range db.Schemas {
				for tn, t := range s.Tables {
					child := key(sn, tn)
					for _, fk := range t.ForeignKeys {
						ref := parseRefTable(sn, fk.RefTable)
						parent := key(ref.Schema, ref.Name)
						if blocked[child] && !blocked[parent] {
							blocked[parent] = true
							changed = true
						}
						if blocked[parent] && !blocked[child] {
							blocked[child] = true
							changed = true
						}
						if capabilityBlocked[child] && !capabilityBlocked[parent] {
							capabilityBlocked[parent] = true
							changed = true
						}
						if capabilityBlocked[parent] && !capabilityBlocked[child] {
							capabilityBlocked[child] = true
							changed = true
						}
					}
				}
			}
		}
	}

	for ref := range tables {
		if blocked[key(ref.Schema, ref.Name)] {
			if capabilityBlocked[key(ref.Schema, ref.Name)] {
				d.Incomplete = true
			}
			k := key(ref.Schema, ref.Name)
			d.OmittedTables = append(d.OmittedTables, k)
			if d.OmittedReasons[k] == "" {
				d.OmittedReasons[k] = "data synchronization belongs to a foreign-key component with an omitted schema change or blocked data prerequisite"
			}
			delete(tables, ref)
		}
	}
	sort.Strings(d.OmittedTables)
	refs, err := topologicalSortTables(tables)
	if err != nil {
		return nil, err
	}
	var writes, deletes []Operation
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		writesBefore := len(writes)
		st := tables[ref]
		var tt *schema.Table
		if ts := target.Schemas[ref.Schema]; ts != nil {
			tt = ts.Tables[ref.Name]
		}
		if st.PrimaryKey == nil || len(st.PrimaryKey.Columns) == 0 {
			return nil, fmt.Errorf("data comparison requires a primary key on %s.%s", ref.Schema, ref.Name)
		}
		if tt != nil && (tt.PrimaryKey == nil || !reflect.DeepEqual(st.PrimaryKey.Columns, tt.PrimaryKey.Columns)) {
			return nil, fmt.Errorf("incompatible primary keys on %s.%s", ref.Schema, ref.Name)
		}
		if tt != nil {
			for _, name := range st.PrimaryKey.Columns {
				sc, tc := st.Columns[name], tt.Columns[name]
				if sc == nil || tc == nil || !reflect.DeepEqual(sc.Type, tc.Type) {
					return nil, fmt.Errorf("incompatible primary key types on %s.%s", ref.Schema, ref.Name)
				}
			}
		}
		sr, err := d.sourceReader.ReadTableData(ctx, ref.Schema, st)
		if err != nil {
			return nil, fmt.Errorf("read source %s.%s: %w", ref.Schema, ref.Name, err)
		}
		var tr []map[string]interface{}
		if tt != nil {
			tr, err = d.targetReader.ReadTableData(ctx, ref.Schema, tt)
			if err != nil {
				return nil, fmt.Errorf("read target %s.%s: %w", ref.Schema, ref.Name, err)
			}
		}
		pk := st.PrimaryKey.Columns
		for _, name := range pk {
			c := st.Columns[name]
			if c != nil && c.Type.Kind == schema.TypeString {
				return nil, fmt.Errorf("text primary key %s.%s.%s requires collation-aware matching, which is unsupported", ref.Schema, ref.Name, name)
			}
			if c == nil || c.Generated || c.Computed {
				return nil, fmt.Errorf("data synchronization requires writable primary key columns on %s.%s", ref.Schema, ref.Name)
			}
		}
		old := map[string]map[string]interface{}{}
		seen := map[string]bool{}
		for _, row := range tr {
			k, err := checkedKey(pk, row)
			if err != nil {
				return nil, err
			}
			if old[k] != nil {
				return nil, fmt.Errorf("duplicate target primary key on %s.%s", ref.Schema, ref.Name)
			}
			old[k] = row
		}
		sort.Slice(sr, func(i, j int) bool { return generatePKKey(pk, sr[i]) < generatePKKey(pk, sr[j]) })
		identity := false
		for _, c := range st.Columns {
			identity = identity || c.Identity
		}
		for _, row := range sr {
			k, err := checkedKey(pk, row)
			if err != nil {
				return nil, err
			}
			if seen[k] {
				return nil, fmt.Errorf("duplicate source primary key on %s.%s", ref.Schema, ref.Name)
			}
			seen[k] = true
			prev, exists := old[k]
			values := map[string]interface{}{}
			for name, col := range st.Columns {
				if col.Computed || col.Generated {
					continue
				}
				value, present := row[name]
				if !present {
					return nil, fmt.Errorf("missing source value %s.%s.%s", ref.Schema, ref.Name, name)
				}
				values[name] = value
			}
			if !exists {
				writes = append(writes, InsertDataOperation{ref.Schema, ref.Name, values, identity})
				continue
			}
			updates := map[string]interface{}{}
			for name, value := range values {
				if !isEqual(value, prev[name]) {
					if st.Columns[name].Identity {
						return nil, fmt.Errorf("identity update on %s.%s is unsupported", ref.Schema, ref.Name)
					}
					updates[name] = value
				}
			}
			if len(updates) > 0 {
				writes = append(writes, UpdateDataOperation{ref.Schema, ref.Name, primaryValues(pk, row), updates})
			}
		}
		var keys []string
		for k := range old {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var tableDeletes []Operation
		for _, k := range keys {
			if !seen[k] {
				tableDeletes = append(tableDeletes, DeleteDataOperation{ref.Schema, ref.Name, primaryValues(pk, old[k])})
			}
		}
		// Prepend each table's deletes: children are deleted before parents.
		deletes = append(tableDeletes, deletes...)
		if len(writes) != writesBefore || len(tableDeletes) > 0 {
			if err := validateDataTransitions(st, sr, tr); err != nil {
				return nil, fmt.Errorf("%s.%s: %w", ref.Schema, ref.Name, err)
			}
		}
	}
	for _, op := range writes {
		u, ok := op.(UpdateDataOperation)
		if !ok {
			continue
		}
		for child, t := range tables {
			for _, fk := range t.ForeignKeys {
				parent := parseRefTable(child.Schema, fk.RefTable)
				if parent.Schema != u.SchemaName || parent.Name != u.TableName {
					continue
				}
				for _, col := range fk.RefColumns {
					if _, changed := u.Updates[col]; changed {
						return nil, fmt.Errorf("updating referenced key %s.%s.%s requires an explicit data migration", u.SchemaName, u.TableName, col)
					}
				}
			}
		}
	}
	// Writes precede deletes so FK updates can move children away from removed parents.
	return append(writes, deletes...), nil
}
func primaryValues(cols []string, row map[string]interface{}) map[string]interface{} {
	m := map[string]interface{}{}
	for _, c := range cols {
		m[c] = row[c]
	}
	return m
}
func checkedKey(cols []string, row map[string]interface{}) (string, error) {
	for _, c := range cols {
		v, ok := row[c]
		if !ok || v == nil {
			return "", fmt.Errorf("missing or NULL primary key component %s", c)
		}
	}
	return generatePKKey(cols, row), nil
}
func generatePKKey(cols []string, row map[string]interface{}) string {
	values := make([]interface{}, 0, len(cols))
	for _, c := range cols {
		v := row[c]
		values = append(values, struct {
			Type  string
			Value interface{}
		}{fmt.Sprintf("%T", v), v})
	}
	b, _ := json.Marshal(values)
	return string(b)
}
func isEqual(a, b interface{}) bool { return reflect.DeepEqual(a, b) }
func validateDataTransitions(t *schema.Table, source, target []map[string]interface{}) error {
	if len(target) == 0 {
		return nil
	}
	// Reusing another row's unique key can require deletes or multi-step updates. Do not guess an ordering.
	for _, idx := range t.Indexes {
		if !idx.IsUnique {
			continue
		}
		if idx.Filter != "" {
			return fmt.Errorf("data synchronization with filtered unique indexes is unsupported")
		}
		owners := map[string]string{}
		for _, r := range target {
			owners[generatePKKey(idx.Columns, r)] = generatePKKey(t.PrimaryKey.Columns, r)
		}
		for _, r := range source {
			owner, exists := owners[generatePKKey(idx.Columns, r)]
			if exists && owner != generatePKKey(t.PrimaryKey.Columns, r) {
				return fmt.Errorf("unique key transition for %s requires an explicit migration", idx.Name)
			}
		}
	}
	return nil
}
