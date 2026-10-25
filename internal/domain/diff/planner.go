package diff

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/mmoehabb/dbdiff/internal/domain/schema"
)

// StepMetadata describes execution prerequisites and indivisible changes.
type StepMetadata struct {
	ID        string   `json:"id"`
	GroupID   string   `json:"group_id"`
	DependsOn []string `json:"depends_on"`
}
type Diagnostic struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

func key(parts ...string) string { b, _ := json.Marshal(parts); return string(b) }

// Scope returns the schema and owning table, if the operation acts on a table.
func Scope(op Operation) (sn, tn string) {
	v := reflect.ValueOf(op)
	if v.Kind() != reflect.Struct {
		return
	}
	if f := v.FieldByName("SchemaName"); f.IsValid() {
		sn = f.String()
	}
	if f := v.FieldByName("TableName"); f.IsValid() {
		tn = f.String()
	}
	if f := v.FieldByName("Table"); f.IsValid() {
		tn = f.FieldByName("Name").String()
	}
	if f := v.FieldByName("Level"); f.IsValid() {
		sn = f.FieldByName("Level0Name").String()
		if f.FieldByName("Level1Type").String() == "TABLE" {
			tn = f.FieldByName("Level1Name").String()
		}
	}
	return
}
func objectKey(op Operation) string {
	sn, tn := Scope(op)
	if reflect.ValueOf(op).Kind() != reflect.Struct {
		return key(fmt.Sprintf("%T", op))
	}
	typ := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(string(op.OperationType()), "create_"), "drop_"), "add_")
	typ = strings.TrimPrefix(typ, "alter_")
	name := ""
	v := reflect.ValueOf(op)
	for _, field := range []string{"Column", "Index", "ForeignKey", "View", "Procedure", "Function", "Trigger", "Synonym", "Sequence", "PartitionFunction", "PartitionScheme", "Check"} {
		f := v.FieldByName(field)
		if f.IsValid() {
			name = f.FieldByName("Name").String()
			break
		}
	}
	for _, field := range []string{"ColumnName", "IndexName", "ForeignKeyName", "ViewName", "ProcedureName", "FunctionName", "TriggerName", "SynonymName", "SequenceName", "PartitionFunctionName", "PartitionSchemeName", "Name"} {
		f := v.FieldByName(field)
		if f.IsValid() {
			name = f.String()
			break
		}
	}
	if typ == "check" {
		if f := v.FieldByName("ConstraintName"); f.IsValid() {
			name = f.String()
		}
	}
	if typ == "primary_key" {
		name = ""
	}
	if typ == "extended_property" {
		b, _ := json.Marshal(v.FieldByName("Level").Interface())
		name = string(b) + name
	}
	return key(sn, tn, typ, name)
}

var priorities = map[OperationType]int{
	DropTrigger: 1, DropView: 1, DropProcedure: 1, DropFunction: 1, DropSynonym: 1,
	DropForeignKey: 2, DropCheck: 3, DropIndex: 4, DropPrimaryKey: 5, DropExtendedProperty: 6,
	DropColumn: 7, DropTable: 8, DropSequence: 9, DropPartitionScheme: 10, DropPartitionFunction: 11, DropSchema: 12,
	CreateSchema: 13, CreatePartitionFunction: 14, CreatePartitionScheme: 15, CreateSequence: 16, AlterSequence: 16,
	CreateFunction: 17, AlterFunction: 17, CreateTable: 18, AddColumn: 19, AlterColumn: 20, AddPrimaryKey: 21, CreateIndex: 22, AddCheck: 23,
	AddForeignKey: 24, CreateSynonym: 25, AlterSynonym: 25, CreateView: 26, AlterView: 26, CreateProcedure: 27, AlterProcedure: 27,
	CreateTrigger: 28, AlterTrigger: 28, AddExtendedProperty: 29, AlterExtendedProperty: 29,
	AlterPartitionFunction: 14, AlterPartitionScheme: 15, AlterTableProperties: 20,
}

func lessOperation(a, b Operation) bool {
	if priorities[a.OperationType()] != priorities[b.OperationType()] {
		return priorities[a.OperationType()] < priorities[b.OperationType()]
	}
	return string(a.OperationType())+objectKey(a) < string(b.OperationType())+objectKey(b)
}

func (p *MigrationPlan) Finalize(source, target *schema.Database) error {
	p.Metadata = map[string]StepMetadata{}
	ops := p.SchemaOperations
	for _, op := range ops {
		if op == nil || reflect.ValueOf(op).Kind() != reflect.Struct {
			return fmt.Errorf("unsupported operation representation %T", op)
		}
		if _, ok := priorities[op.OperationType()]; !ok {
			return fmt.Errorf("unsupported operation %s", op.OperationType())
		}
	}
	sort.Slice(ops, func(i, j int) bool { return lessOperation(ops[i], ops[j]) })
	structural := map[string]bool{}
	for _, op := range ops {
		sn, tn := Scope(op)
		switch op.OperationType() {
		case AlterColumn, DropColumn:
			structural[key(sn, tn)] = true
		}
	}
	objects := make([]string, len(ops))
	scopes := make([][2]string, len(ops))
	ids := make([]string, len(ops))
	groups := make([]string, len(ops))
	edges := make([]map[int]bool, len(ops))
	for i, op := range ops {
		if _, ok := priorities[op.OperationType()]; !ok {
			return fmt.Errorf("unsupported operation %s", op.OperationType())
		}
		objects[i] = objectKey(op)
		ids[i] = string(op.OperationType()) + ":" + objects[i]
		if _, exists := p.Metadata[ids[i]]; exists {
			return fmt.Errorf("duplicate operation %s", ids[i])
		}
		sn, tn := Scope(op)
		scopes[i] = [2]string{sn, tn}
		groups[i] = objects[i]
		if structural[key(sn, tn)] {
			groups[i] = key(sn, tn, "structure")
		}
		p.Metadata[ids[i]] = StepMetadata{ID: ids[i]}
		edges[i] = map[int]bool{}
	}
	add := func(after, before int) {
		if after != before {
			edges[after][before] = true
		}
	}
	for i, a := range ops {
		as, at := scopes[i][0], scopes[i][1]
		for j, b := range ops {
			bs, bt := scopes[j][0], scopes[j][1]
			if objects[i] == objects[j] && priorities[b.OperationType()] < priorities[a.OperationType()] {
				add(i, j)
			}
			if propertyDependsOnOwner(a, b) {
				add(i, j)
			}
			if as == bs && b.OperationType() == CreateSchema && a.OperationType() != DropSchema {
				add(i, j)
			}
			if as == bs && a.OperationType() == DropSchema && strings.HasPrefix(string(b.OperationType()), "drop_") {
				add(i, j)
			}
			if at != "" && as == bs && at == bt {
				if b.OperationType() == CreateTable && !strings.HasPrefix(string(a.OperationType()), "drop_") {
					add(i, j)
				}
				if priorities[b.OperationType()] < priorities[a.OperationType()] && (structural[key(as, at)] || (a.OperationType() == DropTable && strings.HasPrefix(string(b.OperationType()), "drop_"))) {
					add(i, j)
				}
				if col, ok := b.(AddColumnOperation); ok && usesColumn(a, col.Column.Name) {
					add(i, j)
				}
				if name, clustered, ok := createdIndex(a); ok {
					oldName, oldClustered := "", false
					if ts := target.Schemas[as]; ts != nil {
						if tt := ts.Tables[at]; tt != nil {
							switch drop := b.(type) {
							case DropIndexOperation:
								oldName = drop.IndexName
								if idx := tt.Indexes[oldName]; idx != nil {
									oldClustered = idx.Clustered
								}
							case DropPrimaryKeyOperation:
								oldName = drop.ConstraintName
								if tt.PrimaryKey != nil {
									oldClustered = tt.PrimaryKey.Clustered
								}
							}
						}
					}
					if oldName != "" && (name == oldName || (clustered && oldClustered)) {
						add(i, j)
					}
				}
			}
			if fk, ok := a.(AddForeignKeyOperation); ok {
				ref := parseRefTable(as, fk.ForeignKey.RefTable)
				if ref.Schema == bs && ref.Name == bt && (b.OperationType() == CreateTable || b.OperationType() == AddPrimaryKey || b.OperationType() == CreateIndex || b.OperationType() == AlterColumn || b.OperationType() == AddColumn) {
					add(i, j)
				}
			}
			if fk, ok := b.(DropForeignKeyOperation); ok {
				if ts := target.Schemas[fk.SchemaName]; ts != nil {
					if tt := ts.Tables[fk.TableName]; tt != nil {
						if old := tt.ForeignKeys[fk.ForeignKeyName]; old != nil {
							ref := parseRefTable(fk.SchemaName, old.RefTable)
							if ref.Schema == as && ref.Name == at && (a.OperationType() == DropPrimaryKey || a.OperationType() == DropIndex || a.OperationType() == DropColumn || a.OperationType() == AlterColumn || a.OperationType() == DropTable) {
								add(i, j)
							}
						}
					}
				}
			}
		}
	}
	// Source references require the referenced object's final definition first.
	// Target references that disappear must be removed before changing their referent.
	sourceRefs := map[string]bool{}
	for _, dep := range source.Dependencies {
		sourceRefs[key(dep.Schema, dep.Object, dep.RefSchema, dep.RefObject)] = true
	}
	for snapshot, db := range []*schema.Database{source, target} {
		for _, dep := range db.Dependencies {
			if dep.Schema == dep.RefSchema && dep.Object == dep.RefObject {
				continue
			}
			retained := sourceRefs[key(dep.Schema, dep.Object, dep.RefSchema, dep.RefObject)]
			for i, a := range ops {
				if !matchesObject(a, dep.Schema, dep.Object) {
					continue
				}
				for j, b := range ops {
					if !matchesObject(b, dep.RefSchema, dep.RefObject) {
						continue
					}
					ad := strings.HasPrefix(string(a.OperationType()), "drop_")
					bd := strings.HasPrefix(string(b.OperationType()), "drop_")
					if snapshot == 0 && !ad && !bd {
						add(i, j)
					} else if snapshot == 1 && ((ad || !retained) && (bd || strings.HasPrefix(string(b.OperationType()), "alter_"))) {
						add(j, i)
					}
				}
			}
		}
	}
	// A dependent constraint temporarily removed for a structural change belongs to that same change.
	for i, op := range ops {
		if _, ok := op.(DropForeignKeyOperation); ok {
			for j, a := range ops {
				if edges[j][i] && a.OperationType() == AlterColumn {
					old := groups[i]
					for k := range groups {
						if groups[k] == old {
							groups[k] = groups[j]
						}
					}
				}
			}
		}
	}
	result := make([]Operation, 0, len(ops))
	done := map[int]bool{}
	for len(result) < len(ops) {
		found := -1
		for i := range ops {
			if done[i] {
				continue
			}
			ready := true
			for j := range edges[i] {
				if !done[j] {
					ready = false
					break
				}
			}
			if ready {
				found = i
				break
			}
		}
		if found < 0 {
			return fmt.Errorf("unsupported cyclic schema dependencies")
		}
		i := found
		done[i] = true
		result = append(result, ops[i])
		meta := StepMetadata{ID: ids[i], GroupID: groups[i], DependsOn: []string{}}
		for j := range edges[i] {
			meta.DependsOn = append(meta.DependsOn, ids[j])
		}
		sort.Strings(meta.DependsOn)
		p.Metadata[ids[i]] = meta
	}
	p.SchemaOperations = result
	return nil
}

// Property writes require their owner's creation or alteration to survive filtering.
func propertyDependsOnOwner(property, owner Operation) bool {
	var level ExtendedPropertyLevel
	switch op := property.(type) {
	case AddExtendedPropertyOperation:
		level = op.Level
	case AlterExtendedPropertyOperation:
		level = op.Level
	default:
		return false
	}
	switch owner.OperationType() {
	case CreateView, AlterView:
		return level.Level1Type == "VIEW" && matchesObject(owner, level.Level0Name, level.Level1Name)
	case CreateProcedure, AlterProcedure:
		return level.Level1Type == "PROCEDURE" && matchesObject(owner, level.Level0Name, level.Level1Name)
	case CreateFunction, AlterFunction:
		return level.Level1Type == "FUNCTION" && matchesObject(owner, level.Level0Name, level.Level1Name)
	}
	return false
}

func matchesObject(op Operation, sn, name string) bool {
	s, t := Scope(op)
	if s != sn {
		return false
	}
	if t != "" {
		return t == name
	}
	v := reflect.ValueOf(op)
	for _, f := range []string{"View", "Procedure", "Function", "Sequence", "Synonym"} {
		x := v.FieldByName(f)
		if x.IsValid() {
			return x.FieldByName("Name").String() == name
		}
	}
	for _, f := range []string{"ViewName", "ProcedureName", "FunctionName", "SequenceName", "SynonymName"} {
		x := v.FieldByName(f)
		if x.IsValid() {
			return x.String() == name
		}
	}
	return false
}
func (p *MigrationPlan) Step(op Operation) StepMetadata {
	id := string(op.OperationType()) + ":" + objectKey(op)
	switch op.OperationType() {
	case InsertData, UpdateData, DeleteData:
		b, _ := json.Marshal(op)
		hash := sha256.Sum256(b)
		id += fmt.Sprintf(":%x", hash[:])
	}
	if m, ok := p.Metadata[id]; ok {
		return m
	}
	return StepMetadata{ID: id, GroupID: objectKey(op), DependsOn: []string{}}
}

// ApplySafety omits entire blocked groups and their transitive dependants.
func (p *MigrationPlan) ApplySafety(allow bool) {
	if allow {
		return
	}
	blocked := map[string]bool{}
	for _, op := range p.SchemaOperations {
		if op.IsDestructive() {
			blocked[p.Step(op).GroupID] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for _, op := range p.SchemaOperations {
			m := p.Step(op)
			if blocked[m.GroupID] {
				continue
			}
			for _, id := range m.DependsOn {
				if blocked[p.Metadata[id].GroupID] {
					blocked[m.GroupID] = true
					changed = true
					break
				}
			}
		}
	}
	kept := []Operation{}
	p.BlockedTables = map[string]bool{}
	for _, op := range p.SchemaOperations {
		m := p.Step(op)
		if blocked[m.GroupID] {
			p.Omitted = append(p.Omitted, Diagnostic{m.ID, "requires --allow-destructive or a blocked prerequisite"})
			sn, tn := Scope(op)
			p.BlockedTables[key(sn, tn)] = true
		} else {
			kept = append(kept, op)
		}
	}
	p.SchemaOperations = kept
	p.FilterData(allow)
}
func (p *MigrationPlan) FilterData(allow bool) {
	kept := []Operation{}
	for _, op := range p.DataOperations {
		sn, tn := Scope(op)
		if (!allow && op.IsDestructive()) || p.BlockedTables[key(sn, tn)] {
			p.Omitted = append(p.Omitted, Diagnostic{string(op.OperationType()) + ":" + key(sn, tn), "data change is destructive or depends on omitted schema changes"})
		} else {
			kept = append(kept, op)
		}
	}
	p.DataOperations = kept
}

func usesColumn(op Operation, name string) bool {
	var cols []string
	switch o := op.(type) {
	case CreateIndexOperation:
		cols = append(append([]string{}, o.Index.Columns...), o.Index.IncludedColumns...)
		if o.Index.Filter != "" {
			return true
		}
	case AddPrimaryKeyOperation:
		cols = o.PrimaryKey.Columns
	case AddForeignKeyOperation:
		cols = o.ForeignKey.Columns
	case AddCheckOperation:
		return true
	default:
		return false
	}
	for _, c := range cols {
		if c == name {
			return true
		}
	}
	return false
}
func createdIndex(op Operation) (string, bool, bool) {
	switch o := op.(type) {
	case CreateIndexOperation:
		return o.Index.Name, o.Index.Clustered, true
	case AddPrimaryKeyOperation:
		return o.PrimaryKey.Name, o.PrimaryKey.Clustered, true
	}
	return "", false, false
}
