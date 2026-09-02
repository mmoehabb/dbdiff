package diff

import (
	"github.com/mmoehabb/dbdiff/internal/domain/schema"
	"testing"
)

func ptr[T any](v T) *T {
	return &v
}

func TestSchemaDiffer_Compare_Full(t *testing.T) {
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
							"id": {
								Name:     "id",
								Type:     schema.DataType{Kind: schema.TypeInteger},
								Nullable: false,
							},
							"email": {
								Name:     "email",
								Type:     schema.DataType{Kind: schema.TypeString, Length: ptr(255)},
								Nullable: false,
							},
						},
						PrimaryKey: &schema.PrimaryKey{
							Name:    "PK_users",
							Columns: []string{"id"},
						},
						Indexes: map[string]*schema.Index{
							"IX_users_email": {
								Name:     "IX_users_email",
								Columns:  []string{"email"},
								IsUnique: true,
							},
						},
					},
					"new_table": {
						Name: "new_table",
						Columns: map[string]*schema.Column{
							"id": {
								Name: "id",
								Type: schema.DataType{Kind: schema.TypeInteger},
							},
						},
					},
				},
			},
			"new_schema": {
				Name:   "new_schema",
				Tables: map[string]*schema.Table{},
			},
		},
	}

	targetDB := &schema.Database{
		Name: "db2",
		Schemas: map[string]*schema.Schema{
			"dbo": {
				Name: "dbo",
				Tables: map[string]*schema.Table{
					"users": {
						Name: "users",
						Columns: map[string]*schema.Column{
							"id": {
								Name:     "id",
								Type:     schema.DataType{Kind: schema.TypeInteger},
								Nullable: false,
							},
							"email": {
								Name:     "email",
								Type:     schema.DataType{Kind: schema.TypeString, Length: ptr(100)}, // Length changed
								Nullable: true,                                                       // Nullability changed
							},
							"old_column": {
								Name: "old_column",
								Type: schema.DataType{Kind: schema.TypeInteger},
							},
						},
						PrimaryKey: &schema.PrimaryKey{
							Name:    "PK_users",
							Columns: []string{"id"},
						},
						Indexes: map[string]*schema.Index{
							"IX_users_old": {
								Name:    "IX_users_old",
								Columns: []string{"id"},
							},
						},
					},
					"legacy_table": {
						Name: "legacy_table",
						Columns: map[string]*schema.Column{
							"id": {Name: "id", Type: schema.DataType{Kind: schema.TypeInteger}},
						},
					},
				},
			},
			"legacy_schema": {
				Name:   "legacy_schema",
				Tables: map[string]*schema.Table{},
			},
		},
	}

	plan := differ.Compare(sourceDB, targetDB)

	expectedOps := map[OperationType]int{
		CreateSchema: 1, // new_schema
		DropSchema:   1, // legacy_schema
		CreateTable:  1, // new_table
		DropTable:    1, // legacy_table
		AlterColumn:  1, // email in users
		DropColumn:   1, // old_column in users
		CreateIndex:  1, // IX_users_email
		DropIndex:    1, // IX_users_old
	}

	actualOps := make(map[OperationType]int)
	for _, op := range plan.SchemaOperations {
		actualOps[op.OperationType()]++
	}

	for opType, expectedCount := range expectedOps {
		if actualOps[opType] != expectedCount {
			t.Errorf("Expected %d operations of type %s, got %d", expectedCount, opType, actualOps[opType])
		}
	}

	// Also verify that there are no unexpected operations
	for opType, actualCount := range actualOps {
		if expectedCount, ok := expectedOps[opType]; !ok || actualCount > expectedCount {
			t.Errorf("Unexpected extra operations of type %s (found %d, expected %d)", opType, actualCount, expectedOps[opType])
		}
	}
}
