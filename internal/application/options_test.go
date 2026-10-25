package application

import (
	"github.com/mmoehabb/dbdiff/internal/domain/diff"
	"github.com/mmoehabb/dbdiff/internal/domain/schema"
	"testing"
)

func TestSelectionRetainsNamespaceAndChecksExternalForeignKeys(t *testing.T) {
	parent := &schema.Table{Name: "Parent", Columns: map[string]*schema.Column{"Id": {Name: "Id", Type: schema.DataType{Kind: schema.TypeInteger}}}, PrimaryKey: &schema.PrimaryKey{Name: "PK_parent", Columns: []string{"Id"}}}
	child := &schema.Table{Name: "Child", Columns: map[string]*schema.Column{"Id": {Name: "Id", Type: schema.DataType{Kind: schema.TypeInteger}}}, ForeignKeys: map[string]*schema.ForeignKey{"FK_child": {Name: "FK_child", Columns: []string{"Id"}, RefTable: "dbo.Parent", RefColumns: []string{"Id"}}}}
	target := &schema.Database{Schemas: map[string]*schema.Schema{"dbo": {Name: "dbo", Tables: map[string]*schema.Table{"Parent": parent, "Child": child}}}}
	source := &schema.Database{Schemas: map[string]*schema.Schema{"dbo": {Name: "dbo", Tables: map[string]*schema.Table{"Child": child}}}}
	if _, _, err := SelectTables(source, target, CompareOptions{Tables: []string{"dbo.Parent"}}); err == nil {
		t.Fatal("external FK dependency ignored")
	}
	a, b, err := SelectTables(source, target, CompareOptions{Tables: []string{"dbo.Child"}})
	if err != nil {
		t.Fatal(err)
	}
	p, err := diff.NewSchemaDiffer().Compare(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.SchemaOperations) != 0 {
		t.Fatalf("unselected parent changed: %+v", p.SchemaOperations)
	}
	if len(target.Schemas["dbo"].Tables) != 2 {
		t.Fatal("selection mutated full snapshot")
	}
}

func TestSelectionLimitsSourceOnlyNamespaces(t *testing.T) {
	article := &schema.Table{Name: "Articles"}
	unrelated := &schema.Table{Name: "Other"}
	source := &schema.Database{Schemas: map[string]*schema.Schema{
		"dbo":   {Name: "dbo", Tables: map[string]*schema.Table{"Articles": article}},
		"new":   {Name: "new", Tables: map[string]*schema.Table{"Other": unrelated}},
		"empty": {Name: "empty"},
	}}
	target := &schema.Database{Schemas: map[string]*schema.Schema{
		"dbo":      {Name: "dbo", Tables: map[string]*schema.Table{"Articles": article}},
		"existing": {Name: "existing"},
	}}
	for _, options := range []CompareOptions{
		{Tables: []string{"dbo.Articles"}},
		{Tables: []string{"new.Other"}, Exclude: true},
	} {
		a, b, err := SelectTables(source, target, options)
		if err != nil {
			t.Fatal(err)
		}
		if a.Schemas["new"] != nil || a.Schemas["empty"] != nil {
			t.Fatal("retained unrelated source-only namespace")
		}
		if a.Schemas["existing"] == nil || b.Schemas["existing"] == nil {
			t.Fatal("target-only namespace was not preserved")
		}
		p, err := diff.NewSchemaDiffer().Compare(a, b)
		if err != nil {
			t.Fatal(err)
		}
		if len(p.SchemaOperations) != 0 {
			t.Fatalf("identical selection produced changes: %v", p.SchemaOperations)
		}
	}

	a, b, err := SelectTables(source, target, CompareOptions{Tables: []string{"new.Other"}})
	if err != nil {
		t.Fatal(err)
	}
	p, err := diff.NewSchemaDiffer().Compare(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.SchemaOperations) != 2 || p.SchemaOperations[0].OperationType() != diff.CreateSchema || p.SchemaOperations[1].OperationType() != diff.CreateTable {
		t.Fatalf("selected new table needs its namespace: %v", p.SchemaOperations)
	}
	if len(source.Schemas) != 3 || len(target.Schemas) != 2 {
		t.Fatal("selection mutated full snapshots")
	}
}
