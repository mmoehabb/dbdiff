package diff

import (
	"encoding/json"
	"github.com/mmoehabb/dbdiff/internal/domain/schema"
	"reflect"
	"testing"
)

func fixture() *schema.Database {
	table := &schema.Table{Name: "ArticleViews", Columns: map[string]*schema.Column{"ArticleId": {Name: "ArticleId", Type: schema.DataType{Kind: schema.TypeInteger}}}, Indexes: map[string]*schema.Index{"IX_ArticleViews_ArticleId": {Name: "IX_ArticleViews_ArticleId", Columns: []string{"ArticleId"}}}}
	return &schema.Database{Schemas: map[string]*schema.Schema{"dbo": {Name: "dbo", Tables: map[string]*schema.Table{"ArticleViews": table}}}}
}
func cloneDB(d *schema.Database) *schema.Database {
	b, _ := json.Marshal(d)
	var out schema.Database
	_ = json.Unmarshal(b, &out)
	return &out
}
func compareTest(t *testing.T, a, b *schema.Database) *MigrationPlan {
	t.Helper()
	p, e := NewSchemaDiffer().Compare(a, b)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestIndexReplacementSafety(t *testing.T) {
	target := fixture()
	source := cloneDB(target)
	source.Schemas["dbo"].Tables["ArticleViews"].Indexes["IX_ArticleViews_ArticleId"].IsUnique = true
	p := compareTest(t, source, target)
	if len(p.SchemaOperations) != 2 {
		t.Fatalf("want drop/create, got %v", p.SchemaOperations)
	}
	if p.SchemaOperations[0].OperationType() != DropIndex || p.SchemaOperations[1].OperationType() != CreateIndex {
		t.Fatal("replacement out of order")
	}
	p.ApplySafety(false)
	if len(p.SchemaOperations) != 0 || len(p.Omitted) != 2 {
		t.Fatalf("partial replacement: %+v", p)
	}
	p = compareTest(t, source, target)
	p.ApplySafety(true)
	if len(p.SchemaOperations) != 2 {
		t.Fatal("authorized replacement omitted")
	}
	if p = compareTest(t, target, target); len(p.SchemaOperations) != 0 {
		t.Fatal("identical index produces changes")
	}
}
func TestIndependentAdditionSurvivesBlockedReplacement(t *testing.T) {
	target := fixture()
	source := cloneDB(target)
	table := source.Schemas["dbo"].Tables["ArticleViews"]
	table.Indexes["IX_ArticleViews_ArticleId"].IsUnique = true
	source.Schemas["dbo"].Tables["New"] = &schema.Table{Name: "New", Columns: map[string]*schema.Column{"Id": {Name: "Id", Type: schema.DataType{Kind: schema.TypeInteger}}}}
	p := compareTest(t, source, target)
	p.ApplySafety(false)
	if len(p.SchemaOperations) != 1 || p.SchemaOperations[0].OperationType() != CreateTable {
		t.Fatalf("independent create lost: %v", p.SchemaOperations)
	}
}
func TestIndexSemantics(t *testing.T) {
	for name, change := range map[string]func(*schema.Index){"include": func(i *schema.Index) { i.IncludedColumns = []string{"Other"} }, "filter": func(i *schema.Index) { i.Filter = "[ArticleId] > 0" }, "descending": func(i *schema.Index) { i.Descending = []bool{true} }, "clustered": func(i *schema.Index) { i.Clustered = true }} {
		t.Run(name, func(t *testing.T) {
			target := fixture()
			source := cloneDB(target)
			change(source.Schemas["dbo"].Tables["ArticleViews"].Indexes["IX_ArticleViews_ArticleId"])
			if p := compareTest(t, source, target); len(p.SchemaOperations) != 2 {
				t.Fatal("difference missed")
			}
		})
	}
	a := &schema.Index{Columns: []string{"Id"}, IncludedColumns: []string{"A", "B"}}
	b := &schema.Index{Columns: []string{"Id"}, IncludedColumns: []string{"B", "A"}, Descending: []bool{false}}
	if !NewSchemaDiffer().isIndexEqual(a, b) {
		t.Fatal("include order and explicit ASC should be equivalent")
	}
}
func TestColumnDependenciesAndSnapshotImmutability(t *testing.T) {
	target := fixture()
	parent := target.Schemas["dbo"].Tables["ArticleViews"]
	parent.PrimaryKey = &schema.PrimaryKey{Name: "PK_actual_name", Columns: []string{"ArticleId"}}
	target.Schemas["dbo"].Tables["Child"] = &schema.Table{Name: "Child", Columns: map[string]*schema.Column{"Parent": {Name: "Parent", Type: schema.DataType{Kind: schema.TypeInteger}}}, ForeignKeys: map[string]*schema.ForeignKey{"FK_child": {Name: "FK_child", Columns: []string{"Parent"}, RefTable: "dbo.ArticleViews", RefColumns: []string{"ArticleId"}}}}
	source := cloneDB(target)
	source.Schemas["dbo"].Tables["ArticleViews"].Columns["ArticleId"].Type.NativeType = "bigint"
	before := cloneDB(target)
	p := compareTest(t, source, target)
	if !reflect.DeepEqual(before, target) {
		t.Fatal("comparison mutated target")
	}
	positions := map[OperationType]int{}
	for i, op := range p.SchemaOperations {
		positions[op.OperationType()] = i
		if pk, ok := op.(DropPrimaryKeyOperation); ok && pk.ConstraintName != "PK_actual_name" {
			t.Fatal("lost target PK name")
		}
	}
	for _, pair := range [][2]OperationType{{DropForeignKey, DropPrimaryKey}, {DropPrimaryKey, AlterColumn}, {DropIndex, AlterColumn}, {AlterColumn, AddPrimaryKey}, {AddPrimaryKey, AddForeignKey}} {
		a, aok := positions[pair[0]]
		b, bok := positions[pair[1]]
		if !aok || !bok || a >= b {
			t.Fatalf("bad dependency %v: %v", pair, positions)
		}
	}
	p.ApplySafety(false)
	if len(p.SchemaOperations) > 0 {
		t.Fatalf("partial structural change survived: %v", p.SchemaOperations)
	}
}
func TestSchemaRemovalAndTriggerOrdering(t *testing.T) {
	source := fixture()
	source.Schemas["dbo"].Tables["ArticleViews"].Triggers = map[string]*schema.Trigger{"tr": {Name: "tr", Definition: "CREATE TRIGGER tr ON dbo.ArticleViews AFTER INSERT AS SELECT 1"}}
	target := &schema.Database{Schemas: map[string]*schema.Schema{}}
	p := compareTest(t, source, target)
	pos := map[OperationType]int{}
	for i, op := range p.SchemaOperations {
		pos[op.OperationType()] = i
	}
	if pos[CreateSchema] >= pos[CreateTable] || pos[CreateTable] >= pos[CreateTrigger] {
		t.Fatal(pos)
	}
	p = compareTest(t, target, source)
	if p.SchemaOperations[len(p.SchemaOperations)-1].OperationType() != DropSchema {
		t.Fatal("schema must be last")
	}
	found := false
	for _, op := range p.SchemaOperations {
		found = found || op.OperationType() == DropTable
	}
	if !found {
		t.Fatal("schema contents not dropped")
	}
}
func TestStatisticsCollisionAndUnsupportedChanges(t *testing.T) {
	target := fixture()
	source := cloneDB(target)
	target.Schemas["dbo"].Tables["ArticleViews"].Indexes = nil
	target.Schemas["dbo"].Tables["ArticleViews"].Statistics = map[string]bool{"IX_ArticleViews_ArticleId": true}
	if p := compareTest(t, source, target); !p.Incomplete || len(p.SchemaOperations) != 0 || len(p.Omitted) != 1 {
		t.Fatal("statistics collision was not isolated")
	}
	source.Schemas["dbo"].Tables["ArticleViews"].IsSystemVersioned = true
	if p := compareTest(t, source, source); p.Incomplete || len(p.SchemaOperations) != 0 {
		t.Fatal("unchanged temporal metadata should permit comparison")
	}
}
func TestDeterministicPlan(t *testing.T) {
	source := fixture()
	target := &schema.Database{Schemas: map[string]*schema.Schema{}}
	var want []byte
	for n := 0; n < 30; n++ {
		p := compareTest(t, source, target)
		got, _ := json.Marshal(p)
		if n > 0 && string(want) != string(got) {
			t.Fatal("nondeterministic plan")
		}
		want = got
	}
}

func TestIndependentIndexOnSameTableSurvives(t *testing.T) {
	target := fixture()
	source := cloneDB(target)
	table := source.Schemas["dbo"].Tables["ArticleViews"]
	table.Indexes["IX_ArticleViews_ArticleId"].IsUnique = true
	table.Indexes["IX_new"] = &schema.Index{Name: "IX_new", Columns: []string{"ArticleId"}}
	p := compareTest(t, source, target)
	p.ApplySafety(false)
	if len(p.SchemaOperations) != 1 || p.SchemaOperations[0].(CreateIndexOperation).Index.Name != "IX_new" {
		t.Fatalf("independent index lost: %v", p.SchemaOperations)
	}
}

func TestModulePropertiesFollowBlockedOwner(t *testing.T) {
	for _, kind := range []string{"VIEW", "PROCEDURE", "FUNCTION"} {
		t.Run(kind, func(t *testing.T) {
			target := fixture()
			source := cloneDB(target)
			source.Schemas["dbo"].Tables["ArticleViews"].Columns["ArticleId"].Type.NativeType = "bigint"
			props := map[string]schema.PropertyValue{"Description": {Type: "nvarchar", Value: "description"}}
			s := source.Schemas["dbo"]
			switch kind {
			case "VIEW":
				s.Views = map[string]*schema.View{"Module": {Name: "Module", Definition: "view", ExtendedProperties: props}}
			case "PROCEDURE":
				s.Procedures = map[string]*schema.Procedure{"Module": {Name: "Module", Definition: "procedure", ExtendedProperties: props}}
			case "FUNCTION":
				s.Functions = map[string]*schema.Function{"Module": {Name: "Module", Definition: "function", ExtendedProperties: props}}
			}
			source.Dependencies = []schema.Dependency{{Schema: "dbo", Object: "Module", RefSchema: "dbo", RefObject: "ArticleViews"}}
			p := compareTest(t, source, target)
			if len(p.SchemaOperations) != 5 { // index teardown/rebuild, column, module, property
				t.Fatalf("unexpected operations: %v", p.SchemaOperations)
			}
			p.ApplySafety(false)
			if len(p.SchemaOperations) != 0 {
				t.Fatalf("retained operations with omitted owner: %v", p.SchemaOperations)
			}
		})
	}
}

func TestModuleDependencyReversal(t *testing.T) {
	target := fixture()
	target.Schemas["dbo"].Views = map[string]*schema.View{
		"A": {Name: "A", Definition: "SELECT * FROM dbo.B"},
		"B": {Name: "B", Definition: "SELECT 1 AS Id"},
	}
	target.Dependencies = []schema.Dependency{{Schema: "dbo", Object: "A", RefSchema: "dbo", RefObject: "B"}}
	source := cloneDB(target)
	source.Schemas["dbo"].Views["A"].Definition = "SELECT 1 AS Id"
	source.Schemas["dbo"].Views["B"].Definition = "SELECT * FROM dbo.A"
	source.Dependencies = []schema.Dependency{{Schema: "dbo", Object: "B", RefSchema: "dbo", RefObject: "A"}}
	p := compareTest(t, source, target)
	if len(p.SchemaOperations) != 2 {
		t.Fatalf("unexpected operations: %v", p.SchemaOperations)
	}
	for i, name := range []string{"A", "B"} {
		if op, ok := p.SchemaOperations[i].(AlterViewOperation); !ok || op.View.Name != name {
			t.Fatalf("expected alteration of %s at %d, got %v", name, i, p.SchemaOperations[i])
		}
	}
}

func TestFunctionKindChangeRejected(t *testing.T) {
	for _, pair := range [][2]string{{"IF", "TF"}, {"TF", "IF"}, {"FN", "IF"}, {"IF", "FN"}} {
		t.Run(pair[0]+"_to_"+pair[1], func(t *testing.T) {
			target := fixture()
			target.Schemas["dbo"].Functions = map[string]*schema.Function{"F": {Name: "F", Kind: pair[0], Definition: "old"}}
			source := cloneDB(target)
			source.Schemas["dbo"].Functions["F"].Kind = pair[1]
			source.Schemas["dbo"].Functions["F"].Definition = "new"
			if p := compareTest(t, source, target); !p.Incomplete || len(p.SchemaOperations) != 0 || len(p.Omitted) != 1 {
				t.Fatalf("expected kind-change omission without executable operations, got %+v", p)
			}
		})
	}
	target := fixture()
	target.Schemas["dbo"].Functions = map[string]*schema.Function{"F": {Name: "F", Kind: "IF", Definition: "old"}}
	source := cloneDB(target)
	source.Schemas["dbo"].Functions["F"].Definition = "new"
	p := compareTest(t, source, target)
	if len(p.SchemaOperations) != 1 || p.SchemaOperations[0].OperationType() != AlterFunction {
		t.Fatalf("same-kind alteration rejected: %v", p.SchemaOperations)
	}
}

func TestRemovedModuleReferenceBeforeReferentDrop(t *testing.T) {
	target := fixture()
	target.Schemas["dbo"].Views = map[string]*schema.View{
		"A": {Name: "A", Definition: "SELECT * FROM dbo.B"},
		"B": {Name: "B", Definition: "SELECT 1 AS Id"},
	}
	target.Dependencies = []schema.Dependency{{Schema: "dbo", Object: "A", RefSchema: "dbo", RefObject: "B"}}
	source := cloneDB(target)
	source.Schemas["dbo"].Views["A"].Definition = "SELECT 1 AS Id"
	delete(source.Schemas["dbo"].Views, "B")
	source.Dependencies = nil
	p := compareTest(t, source, target)
	if len(p.SchemaOperations) != 2 || p.SchemaOperations[0].OperationType() != AlterView || p.SchemaOperations[1].OperationType() != DropView {
		t.Fatalf("reference must be removed before drop: %v", p.SchemaOperations)
	}
}

func TestModuleDropOrderUsesTargetDependencies(t *testing.T) {
	target := fixture()
	target.Schemas["dbo"].Views = map[string]*schema.View{
		"A": {Name: "A", Definition: "SELECT 1 AS Id"},
		"B": {Name: "B", Definition: "SELECT * FROM dbo.A"},
	}
	target.Dependencies = []schema.Dependency{{Schema: "dbo", Object: "B", RefSchema: "dbo", RefObject: "A"}}
	source := cloneDB(target)
	source.Schemas["dbo"].Views = nil
	source.Dependencies = nil
	p := compareTest(t, source, target)
	if len(p.SchemaOperations) != 2 {
		t.Fatalf("unexpected operations: %v", p.SchemaOperations)
	}
	for i, name := range []string{"B", "A"} {
		if op, ok := p.SchemaOperations[i].(DropViewOperation); !ok || op.ViewName != name {
			t.Fatalf("expected drop of %s at %d, got %v", name, i, p.SchemaOperations[i])
		}
	}
}
