package diff

import (
	"dbdiff/internal/domain/schema"
	"testing"
)

func TestSchemaDiffer_Compare_DecomposedCreateTable(t *testing.T) {
	differ := NewSchemaDiffer()

	sourceDB := &schema.Database{
		Name: "db1",
		Schemas: map[string]*schema.Schema{
			"dbo": {
				Name: "dbo",
				Tables: map[string]*schema.Table{
					"users": {
						Name: "users",
						Columns: map[string]*schema.Column{
							"id": {Name: "id", Type: schema.DataType{Kind: "int"}},
						},
						PrimaryKey: &schema.PrimaryKey{
							Name:    "pk_users",
							Columns: []string{"id"},
						},
						Indexes: map[string]*schema.Index{
							"idx_users_id": {Name: "idx_users_id", Columns: []string{"id"}},
						},
						ForeignKeys: map[string]*schema.ForeignKey{
							"fk_users_other": {Name: "fk_users_other", Columns: []string{"id"}, RefTable: "other", RefColumns: []string{"id"}},
						},
					},
				},
			},
		},
	}

	targetDB := &schema.Database{
		Name: "db2",
		Schemas: map[string]*schema.Schema{
			"dbo": {
				Name:   "dbo",
				Tables: map[string]*schema.Table{},
			},
		},
	}

	plan := differ.Compare(sourceDB, targetDB)

	var hasCreateTable, hasAddPK, hasCreateIndex, hasAddFK bool

	for _, op := range plan.SchemaOperations {
		switch op.OperationType() {
		case CreateTable:
			hasCreateTable = true
			ctOp := op.(CreateTableOperation)
			if ctOp.Table.PrimaryKey != nil {
				t.Error("CreateTableOperation should not contain a PrimaryKey")
			}
			if len(ctOp.Table.Indexes) > 0 {
				t.Error("CreateTableOperation should not contain Indexes")
			}
			if len(ctOp.Table.ForeignKeys) > 0 {
				t.Error("CreateTableOperation should not contain ForeignKeys")
			}
		case AddPrimaryKey:
			hasAddPK = true
		case CreateIndex:
			hasCreateIndex = true
		case AddForeignKey:
			hasAddFK = true
		}
	}

	if !hasCreateTable {
		t.Error("Expected CreateTableOperation")
	}
	if !hasAddPK {
		t.Error("Expected AddPrimaryKeyOperation")
	}
	if !hasCreateIndex {
		t.Error("Expected CreateIndexOperation")
	}
	if !hasAddFK {
		t.Error("Expected AddForeignKeyOperation")
	}
}
