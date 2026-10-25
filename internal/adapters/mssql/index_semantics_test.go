package mssql

import (
	"context"
	"database/sql/driver"
	"reflect"
	"strings"
	"testing"

	"github.com/mmoehabb/dbdiff/internal/domain/diff"
	"github.com/mmoehabb/dbdiff/internal/domain/schema"
)

func TestCompositeIndexIntrospection(t *testing.T) {
	for _, kind := range []string{"index", "unique", "primary"} {
		for _, clustered := range []bool{false, true} {
			label := "nonclustered"
			indexType := int64(2)
			if clustered {
				label, indexType = "clustered", 1
			}
			t.Run(kind+"/"+label, func(t *testing.T) {
				var rows [][]driver.Value
				if kind == "primary" {
					rows = [][]driver.Value{
						{"dbo", "Items", "Key", "B", indexType, true},
						{"dbo", "Items", "Key", "A", indexType, false},
					}
				} else {
					row := func(column string, ordinal int64, descending, included bool) []driver.Value {
						return []driver.Value{"dbo", "Items", "Key", column, kind == "unique", indexType, ordinal, descending, included, nil, kind == "unique", false, int64(0), false, false, true, true}
					}
					// Catalog order is key_ordinal, not table column order or alphabetical order.
					if kind == "index" && !clustered {
						rows = append(rows, row("Payload", 0, false, true))
					}
					rows = append(rows, row("B", 1, true, false), row("A", 2, false, false))
				}
				db := metadataDB(t, rows, nil)
				snapshot := &schema.Database{Schemas: map[string]*schema.Schema{}}
				inspector := NewMSSQLIntrospector("")
				var gotColumns []string
				var gotDescending []bool
				var gotClustered bool
				if kind == "primary" {
					if err := inspector.loadPrimaryKeys(context.Background(), db, snapshot); err != nil {
						t.Fatal(err)
					}
					key := snapshot.Schemas["dbo"].Tables["Items"].PrimaryKey
					gotColumns, gotDescending, gotClustered = key.Columns, key.Descending, key.Clustered
				} else {
					if err := inspector.loadIndexes(context.Background(), db, snapshot); err != nil {
						t.Fatal(err)
					}
					key := snapshot.Schemas["dbo"].Tables["Items"].Indexes["Key"]
					gotColumns, gotDescending, gotClustered = key.Columns, key.Descending, key.Clustered
					if key.IsConstraint != (kind == "unique") {
						t.Fatal("lost unique constraint identity")
					}
					if kind == "index" && !clustered && !reflect.DeepEqual(key.IncludedColumns, []string{"Payload"}) {
						t.Fatal("lost included column")
					}
				}
				if gotClustered != clustered || !reflect.DeepEqual(gotColumns, []string{"B", "A"}) || !reflect.DeepEqual(gotDescending, []bool{true, false}) {
					t.Fatalf("lost key metadata: clustered=%v columns=%v descending=%v", gotClustered, gotColumns, gotDescending)
				}
			})
		}
	}
}

func TestCompositeIndexSQL(t *testing.T) {
	for _, kind := range []string{"index", "unique", "primary"} {
		for _, clustered := range []bool{false, true} {
			storage := "NONCLUSTERED"
			if clustered {
				storage = "CLUSTERED"
			}
			for _, directions := range [][]bool{nil, {true, false}, {false, true}} {
				var op diff.Operation
				prefix := "CREATE " + storage + " INDEX [Key] ON [dbo].[Items]"
				if kind == "primary" {
					op = diff.AddPrimaryKeyOperation{SchemaName: "dbo", TableName: "Items", PrimaryKey: schema.PrimaryKey{Name: "Key", Clustered: clustered, Columns: []string{"B", "A"}, Descending: directions}}
					prefix = "ALTER TABLE [dbo].[Items] ADD CONSTRAINT [Key] PRIMARY KEY " + storage
				} else {
					op = diff.CreateIndexOperation{SchemaName: "dbo", TableName: "Items", Index: schema.Index{Name: "Key", Clustered: clustered, Columns: []string{"B", "A"}, Descending: directions, IsUnique: kind == "unique", IsConstraint: kind == "unique"}}
					if kind == "unique" {
						prefix = "ALTER TABLE [dbo].[Items] ADD CONSTRAINT [Key] UNIQUE " + storage
					}
				}
				keys := "[B], [A]"
				if len(directions) > 0 && directions[0] {
					keys = "[B] DESC, [A]"
				}
				if len(directions) > 1 && directions[1] {
					keys = "[B], [A] DESC"
				}
				got, err := NewMSSQLRenderer().renderOperation(op)
				if err != nil {
					t.Fatal(err)
				}
				want := prefix + " (" + keys + ");"
				if strings.TrimSpace(got) != want {
					t.Fatalf("got %s; want %s", got, want)
				}
			}
		}
	}
}
