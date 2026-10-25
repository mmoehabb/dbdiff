package diff

import (
	"testing"

	"github.com/mmoehabb/dbdiff/internal/domain/schema"
)

func indexSnapshot(kind string, clustered bool, columns []string, descending []bool) *schema.Database {
	table := &schema.Table{Name: "Items", Columns: map[string]*schema.Column{
		"A": {Name: "A", Type: schema.DataType{Kind: schema.TypeInteger}},
		"B": {Name: "B", Type: schema.DataType{Kind: schema.TypeInteger}},
	}}
	if kind == "primary" {
		table.PrimaryKey = &schema.PrimaryKey{Name: "Key", Clustered: clustered, Columns: columns, Descending: descending}
	} else {
		table.Indexes = map[string]*schema.Index{"Key": {
			Name: "Key", Clustered: clustered, Columns: columns, Descending: descending,
			IsUnique: kind == "unique", IsConstraint: kind == "unique",
		}}
	}
	return &schema.Database{Schemas: map[string]*schema.Schema{"dbo": {Name: "dbo", Tables: map[string]*schema.Table{"Items": table}}}}
}

func TestIndexClusteringAndKeyOrder(t *testing.T) {
	for _, kind := range []string{"index", "unique", "primary"} {
		for _, tc := range []struct {
			name                         string
			oldClustered, newClustered   bool
			oldColumns, newColumns       []string
			oldDescending, newDescending []bool
			changed                      bool
		}{
			{"clustered to nonclustered", true, false, []string{"A", "B"}, []string{"A", "B"}, nil, nil, true},
			{"nonclustered to clustered", false, true, []string{"A", "B"}, []string{"A", "B"}, nil, nil, true},
			{"reordered keys", false, false, []string{"A", "B"}, []string{"B", "A"}, nil, nil, true},
			{"first key descending", false, false, []string{"A", "B"}, []string{"A", "B"}, nil, []bool{true, false}, true},
			{"second key descending", false, false, []string{"A", "B"}, []string{"A", "B"}, nil, []bool{false, true}, true},
			{"descending to ascending", false, false, []string{"A", "B"}, []string{"A", "B"}, []bool{false, true}, nil, true},
			{"identical composite", true, true, []string{"B", "A"}, []string{"B", "A"}, []bool{true, false}, []bool{true, false}, false},
			{"implicit ascending", false, false, []string{"A", "B"}, []string{"A", "B"}, nil, []bool{false, false}, false},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				target := indexSnapshot(kind, tc.oldClustered, tc.oldColumns, tc.oldDescending)
				source := indexSnapshot(kind, tc.newClustered, tc.newColumns, tc.newDescending)
				p := compareTest(t, source, target)
				if !tc.changed {
					if len(p.SchemaOperations) != 0 {
						t.Fatalf("equivalent keys produced changes: %v", p.SchemaOperations)
					}
					return
				}
				drop, create := DropIndex, CreateIndex
				if kind == "primary" {
					drop, create = DropPrimaryKey, AddPrimaryKey
				}
				if len(p.SchemaOperations) != 2 || p.SchemaOperations[0].OperationType() != drop || p.SchemaOperations[1].OperationType() != create {
					t.Fatalf("expected ordered replacement, got %v", p.SchemaOperations)
				}
				p.ApplySafety(true)
				if len(p.SchemaOperations) != 2 {
					t.Fatal("authorized replacement omitted")
				}
				p = compareTest(t, source, target)
				p.ApplySafety(false)
				if len(p.SchemaOperations) != 0 || len(p.Omitted) != 2 {
					t.Fatalf("partial replacement: %+v", p)
				}
			})
		}
	}
}

func TestClusteringMovesBetweenPrimaryKeyAndIndex(t *testing.T) {
	for _, primaryClustered := range []bool{false, true} {
		target := indexSnapshot("primary", primaryClustered, []string{"A", "B"}, nil)
		target.Schemas["dbo"].Tables["Items"].Indexes = map[string]*schema.Index{"IX": {Name: "IX", Clustered: !primaryClustered, Columns: []string{"B", "A"}}}
		source := cloneDB(target)
		source.Schemas["dbo"].Tables["Items"].PrimaryKey.Clustered = !primaryClustered
		source.Schemas["dbo"].Tables["Items"].Indexes["IX"].Clustered = primaryClustered
		p := compareTest(t, source, target)
		if len(p.SchemaOperations) != 4 {
			t.Fatalf("expected two replacements: %v", p.SchemaOperations)
		}
		oldClusterDrop, newClusterCreate := DropIndex, AddPrimaryKey
		if primaryClustered {
			oldClusterDrop, newClusterCreate = DropPrimaryKey, CreateIndex
		}
		dropped := false
		for _, op := range p.SchemaOperations {
			if op.OperationType() == oldClusterDrop {
				dropped = true
			}
			if op.OperationType() == newClusterCreate && !dropped {
				t.Fatal("new clustered key precedes removal of old clustered key")
			}
		}
		p.ApplySafety(false)
		if len(p.SchemaOperations) != 0 || len(p.Omitted) != 4 {
			t.Fatalf("partial clustering transfer: %+v", p)
		}
	}
}
