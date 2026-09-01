package mssql

import (
	"context"
	"strings"
	"testing"

	"dbdiff/internal/domain/diff"
	"dbdiff/internal/domain/schema"
)

func ptr[T any](v T) *T {
	return &v
}

func TestMSSQLRenderer_Render(t *testing.T) {
	renderer := NewMSSQLRenderer()

	plan := &diff.MigrationPlan{
		SchemaOperations: []diff.Operation{
			diff.CreateSchemaOperation{SchemaName: "dbo"},
			diff.CreateTableOperation{
				SchemaName: "dbo",
				Table: schema.Table{
					Name: "users",
					Columns: map[string]*schema.Column{
						"id": {
							Name: "id",
							Type: schema.DataType{Kind: schema.TypeInteger},
							Nullable: false,
							Identity: true,
						},
						"email": {
							Name: "email",
							Type: schema.DataType{Kind: schema.TypeString, Length: ptr(255)},
							Nullable: true,
						},
						"meta": {
							Name: "meta",
							Type: schema.DataType{Kind: schema.TypeJSON},
							Nullable: true,
						},
					},
				},
			},
			diff.AddColumnOperation{
				SchemaName: "dbo",
				TableName:  "users",
				Column: schema.Column{
					Name: "active",
					Type: schema.DataType{Kind: schema.TypeBoolean},
					Nullable: false,
					Default: &schema.DefaultExpression{Value: "1"},
				},
			},
			diff.AlterColumnOperation{
				SchemaName: "dbo",
				TableName:  "users",
				Column: schema.Column{
					Name: "email",
					Type: schema.DataType{Kind: schema.TypeString, Length: ptr(500)},
					Nullable: false,
				},
			},
			diff.DropColumnOperation{
				SchemaName: "dbo",
				TableName:  "users",
				ColumnName: "old_column",
			},
			diff.AddPrimaryKeyOperation{
				SchemaName: "dbo",
				TableName:  "users",
				PrimaryKey: schema.PrimaryKey{
					Name: "PK_users",
					Columns: []string{"id"},
				},
			},
			diff.DropPrimaryKeyOperation{
				SchemaName: "dbo",
				TableName:  "users",
			},
			diff.AddForeignKeyOperation{
				SchemaName: "dbo",
				TableName:  "orders",
				ForeignKey: schema.ForeignKey{
					Name: "FK_orders_users",
					Columns: []string{"user_id"},
					RefTable: "users",
					RefColumns: []string{"id"},
				},
			},
			diff.DropForeignKeyOperation{
				SchemaName: "dbo",
				TableName:  "orders",
				ForeignKeyName: "FK_orders_old",
			},
			diff.CreateIndexOperation{
				SchemaName: "dbo",
				TableName:  "users",
				Index: schema.Index{
					Name: "IX_users_email",
					Columns: []string{"email"},
					IsUnique: true,
				},
			},
			diff.DropIndexOperation{
				SchemaName: "dbo",
				TableName:  "users",
				IndexName:  "IX_users_old",
			},
			diff.DropTableOperation{
				SchemaName: "dbo",
				TableName:  "legacy_users",
			},
			diff.DropSchemaOperation{
				SchemaName: "legacy_schema",
			},
		},
	}

	sql, err := renderer.Render(context.Background(), plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedParts := []string{
		"BEGIN TRANSACTION;",
		"CREATE SCHEMA [dbo];",
		"CREATE TABLE [dbo].[users] (\n    [email] NVARCHAR(255) NULL,\n    [id] INT NOT NULL IDENTITY(1,1),\n    [meta] NVARCHAR(MAX) CHECK (ISJSON([meta]) > 0) NULL\n);",
		"ALTER TABLE [dbo].[users] ADD [active] BIT NOT NULL DEFAULT 1;",
		"ALTER TABLE [dbo].[users] ALTER COLUMN [email] NVARCHAR(500) NOT NULL;",
		"ALTER TABLE [dbo].[users] DROP COLUMN [old_column];",
		"ALTER TABLE [dbo].[users] ADD CONSTRAINT [PK_users] PRIMARY KEY ([id]);",
		"-- TODO: Drop PK on [dbo].[users] (Name unknown)\nALTER TABLE [dbo].[users] DROP CONSTRAINT [PK_users];",
		"ALTER TABLE [dbo].[orders] ADD CONSTRAINT [FK_orders_users] FOREIGN KEY ([user_id]) REFERENCES [dbo].[users] ([id]);",
		"ALTER TABLE [dbo].[orders] DROP CONSTRAINT [FK_orders_old];",
		"CREATE UNIQUE INDEX [IX_users_email] ON [dbo].[users] ([email]);",
		"DROP INDEX [IX_users_old] ON [dbo].[users];",
		"DROP TABLE [dbo].[legacy_users];",
		"DROP SCHEMA [legacy_schema];",
		"COMMIT TRANSACTION;",
	}

	for _, part := range expectedParts {
		if !strings.Contains(sql, part) {
			t.Errorf("expected SQL to contain:\n%s\n\nActual SQL:\n%s", part, sql)
		}
	}
}
