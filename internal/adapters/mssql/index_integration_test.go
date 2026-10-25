package mssql

import (
	"database/sql"
	"fmt"
	"reflect"
	"testing"
)

// Check the actual catalog independently of our introspector and comparison code.
func assertCatalogKey(t *testing.T, db *sql.DB, name string, clustered bool, columns []string, descending []bool) {
	t.Helper()
	rows, err := db.Query(`SELECT i.type, c.name, ic.key_ordinal, ic.is_descending_key
FROM sys.indexes i
JOIN sys.index_columns ic ON ic.object_id=i.object_id AND ic.index_id=i.index_id
JOIN sys.columns c ON c.object_id=ic.object_id AND c.column_id=ic.column_id
WHERE i.object_id=OBJECT_ID(N'dbo.Items') AND i.name=@p1 AND ic.key_ordinal>0
ORDER BY ic.key_ordinal`, name)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	wantType := 2
	if clustered {
		wantType = 1
	}
	var gotColumns []string
	var gotDescending []bool
	for rows.Next() {
		var indexType, ordinal int
		var column string
		var desc bool
		if err := rows.Scan(&indexType, &column, &ordinal, &desc); err != nil {
			t.Fatal(err)
		}
		if indexType != wantType || ordinal != len(gotColumns)+1 {
			t.Fatalf("%s: type=%d ordinal=%d, want type=%d ordinal=%d", name, indexType, ordinal, wantType, len(gotColumns)+1)
		}
		gotColumns = append(gotColumns, column)
		gotDescending = append(gotDescending, desc)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotColumns, columns) || !reflect.DeepEqual(gotDescending, descending) {
		t.Fatalf("%s: columns=%v directions=%v, want columns=%v directions=%v", name, gotColumns, gotDescending, columns, descending)
	}
}

func TestMSSQLIntegration_IndexClusteringAndOrder(t *testing.T) {
	server := integrationServer(t)
	const tableDDL = `CREATE TABLE dbo.Items(A int NOT NULL, B int NOT NULL, Payload int NULL); INSERT INTO dbo.Items VALUES(1,2,3),(2,1,4);`
	for _, kind := range []string{"index", "unique", "primary"} {
		for _, clustered := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/clustered_%v", kind, clustered), func(t *testing.T) {
				sourceDSN, source := testDatabase(t, server, "source")
				targetDSN, target := testDatabase(t, server, "target")
				ddl := func(clustered bool, keys string) string {
					storage := "NONCLUSTERED"
					if clustered {
						storage = "CLUSTERED"
					}
					switch kind {
					case "primary":
						return "ALTER TABLE dbo.Items ADD CONSTRAINT [Key] PRIMARY KEY " + storage + " (" + keys + ");"
					case "unique":
						return "ALTER TABLE dbo.Items ADD CONSTRAINT [Key] UNIQUE " + storage + " (" + keys + ");"
					default:
						return "CREATE " + storage + " INDEX [Key] ON dbo.Items (" + keys + ");"
					}
				}
				// Source keys deliberately differ from physical table-column order.
				execFixture(t, source, tableDDL+ddl(clustered, "B DESC, A ASC"))
				execFixture(t, target, tableDDL+ddl(!clustered, "A DESC, B ASC"))
				a, b := inspectTest(t, sourceDSN), inspectTest(t, targetDSN)
				var gotColumns []string
				var gotDescending []bool
				var gotClustered bool
				if kind == "primary" {
					key := a.Schemas["dbo"].Tables["Items"].PrimaryKey
					gotColumns, gotDescending, gotClustered = key.Columns, key.Descending, key.Clustered
				} else {
					key := a.Schemas["dbo"].Tables["Items"].Indexes["Key"]
					gotColumns, gotDescending, gotClustered = key.Columns, key.Descending, key.Clustered
				}
				if gotClustered != clustered || !reflect.DeepEqual(gotColumns, []string{"B", "A"}) || !reflect.DeepEqual(gotDescending, []bool{true, false}) {
					t.Fatalf("introspection lost source key: clustered=%v columns=%v descending=%v", gotClustered, gotColumns, gotDescending)
				}
				safe := planTest(t, a, b)
				safe.ApplySafety(false)
				if len(safe.SchemaOperations) != 0 || len(safe.Omitted) != 2 {
					t.Fatalf("partial replacement: %+v", safe)
				}
				applyTest(t, target, safe)
				assertCatalogKey(t, target, "Key", !clustered, []string{"A", "B"}, []bool{true, false})
				p := planTest(t, a, b)
				p.ApplySafety(true)
				applyTest(t, target, p)
				assertCatalogKey(t, target, "Key", clustered, []string{"B", "A"}, []bool{true, false})
				if next := planTest(t, a, inspectTest(t, targetDSN)); len(next.SchemaOperations) != 0 {
					t.Fatalf("replacement did not converge: %v", next.SchemaOperations)
				}
			})
		}
	}
	for _, primaryClustered := range []bool{false, true} {
		t.Run(fmt.Sprintf("transfer/primary_clustered_%v", primaryClustered), func(t *testing.T) {
			sourceDSN, source := testDatabase(t, server, "source")
			targetDSN, target := testDatabase(t, server, "target")
			ddl := func(primaryClustered bool) string {
				primaryType, indexType := "NONCLUSTERED", "CLUSTERED"
				if primaryClustered {
					primaryType, indexType = indexType, primaryType
				}
				return "ALTER TABLE dbo.Items ADD CONSTRAINT PK_Items PRIMARY KEY " + primaryType + " (A, B DESC); CREATE " + indexType + " INDEX IX_Items ON dbo.Items(B DESC, A);"
			}
			execFixture(t, source, tableDDL+ddl(primaryClustered))
			execFixture(t, target, tableDDL+ddl(!primaryClustered))
			a, b := inspectTest(t, sourceDSN), inspectTest(t, targetDSN)
			p := planTest(t, a, b)
			p.ApplySafety(true)
			applyTest(t, target, p)
			assertCatalogKey(t, target, "PK_Items", primaryClustered, []string{"A", "B"}, []bool{false, true})
			assertCatalogKey(t, target, "IX_Items", !primaryClustered, []string{"B", "A"}, []bool{true, false})
			if next := planTest(t, a, inspectTest(t, targetDSN)); len(next.SchemaOperations) != 0 {
				t.Fatalf("clustering transfer did not converge: %v", next.SchemaOperations)
			}
		})
	}
}
