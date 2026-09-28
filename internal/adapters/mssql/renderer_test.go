package mssql

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mmoehabb/dbdiff/internal/domain/diff"
	"github.com/mmoehabb/dbdiff/internal/domain/schema"
)

func TestFormatValue(t *testing.T) {
	tm, _ := time.Parse(time.RFC3339Nano, "2026-05-16T19:14:03.58Z")
	tests := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{"nil", nil, "NULL"},
		{"string", "hello'world", "N'hello''world'"},
		{"int", 123, "123"},
		{"float", 123.45, "123.45"},
		{"bool true", true, "1"},
		{"bool false", false, "0"},
		{"time", tm, "'2026-05-16 19:14:03.58'"},
		{"bytes", []byte{0xDE, 0xAD}, "0xDEAD"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatValue(tt.input)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

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
							Name:     "id",
							Type:     schema.DataType{Kind: schema.TypeInteger},
							Nullable: false,
							Identity: true,
						},
						"email": {
							Name:     "email",
							Type:     schema.DataType{Kind: schema.TypeString, Length: ptr(255)},
							Nullable: true,
						},
						"meta": {
							Name:     "meta",
							Type:     schema.DataType{Kind: schema.TypeJSON},
							Nullable: true,
						},
					},
				},
			},
			diff.AddColumnOperation{
				SchemaName: "dbo",
				TableName:  "users",
				Column: schema.Column{
					Name:     "active",
					Type:     schema.DataType{Kind: schema.TypeBoolean},
					Nullable: false,
					Default:  &schema.DefaultExpression{Value: "1"},
				},
			},
			diff.AlterColumnOperation{
				SchemaName: "dbo",
				TableName:  "users",
				Column: schema.Column{
					Name:     "email",
					Type:     schema.DataType{Kind: schema.TypeString, Length: ptr(500)},
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
					Name:    "PK_users",
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
					Name:       "FK_orders_users",
					Columns:    []string{"user_id"},
					RefTable:   "users",
					RefColumns: []string{"id"},
				},
			},
			diff.DropForeignKeyOperation{
				SchemaName:     "dbo",
				TableName:      "orders",
				ForeignKeyName: "FK_orders_old",
			},
			diff.CreateIndexOperation{
				SchemaName: "dbo",
				TableName:  "users",
				Index: schema.Index{
					Name:     "IX_users_email",
					Columns:  []string{"email"},
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
		"EXEC('CREATE SCHEMA [dbo]');",
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

func TestMSSQLRenderer_RenderInsertData(t *testing.T) {
	renderer := NewMSSQLRenderer()

	op := diff.InsertDataOperation{
		SchemaName: "dbo",
		TableName:  "users",
		Row: map[string]interface{}{
			"id":   1,
			"name": "Alice",
		},
		HasIdentity: true,
	}

	plan := &diff.MigrationPlan{
		DataOperations: []diff.Operation{op},
	}

	sql, err := renderer.Render(context.Background(), plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedPrefix := "SET IDENTITY_INSERT [dbo].[users] ON;"
	expectedInsert := "INSERT INTO [dbo].[users] ([id], [name]) VALUES (1, N'Alice');"
	expectedSuffix := "SET IDENTITY_INSERT [dbo].[users] OFF;"

	if !strings.Contains(sql, expectedPrefix) {
		t.Errorf("expected SQL to contain:\n%s\n\nActual SQL:\n%s", expectedPrefix, sql)
	}
	if !strings.Contains(sql, expectedInsert) {
		t.Errorf("expected SQL to contain:\n%s\n\nActual SQL:\n%s", expectedInsert, sql)
	}
	if !strings.Contains(sql, expectedSuffix) {
		t.Errorf("expected SQL to contain:\n%s\n\nActual SQL:\n%s", expectedSuffix, sql)
	}
}

func TestMSSQLRenderer_Render_DataTypeMapping(t *testing.T) {
	renderer := NewMSSQLRenderer()

	plan := &diff.MigrationPlan{
		SchemaOperations: []diff.Operation{
			diff.CreateTableOperation{
				SchemaName: "dbo",
				Table: schema.Table{
					Name: "data_types",
					Columns: map[string]*schema.Column{
						"col_str_len": {
							Name:     "col_str_len",
							Type:     schema.DataType{Kind: schema.TypeString, Length: ptr(100)},
							Nullable: true,
						},
						"col_str_max": {
							Name:     "col_str_max",
							Type:     schema.DataType{Kind: schema.TypeString},
							Nullable: true,
						},
						"col_int": {
							Name:     "col_int",
							Type:     schema.DataType{Kind: schema.TypeInteger},
							Nullable: true,
						},
						"col_dec_prec": {
							Name:     "col_dec_prec",
							Type:     schema.DataType{Kind: schema.TypeDecimal, Precision: ptr(10), Scale: ptr(2)},
							Nullable: true,
						},
						"col_dec_def": {
							Name:     "col_dec_def",
							Type:     schema.DataType{Kind: schema.TypeDecimal},
							Nullable: true,
						},
						"col_bool": {
							Name:     "col_bool",
							Type:     schema.DataType{Kind: schema.TypeBoolean},
							Nullable: true,
						},
						"col_datetime": {
							Name:     "col_datetime",
							Type:     schema.DataType{Kind: schema.TypeDateTime},
							Nullable: true,
						},
						"col_uuid": {
							Name:     "col_uuid",
							Type:     schema.DataType{Kind: schema.TypeUUID},
							Nullable: true,
						},
						"col_bin_len": {
							Name:     "col_bin_len",
							Type:     schema.DataType{Kind: schema.TypeBinary, Length: ptr(64)},
							Nullable: true,
						},
						"col_bin_max": {
							Name:     "col_bin_max",
							Type:     schema.DataType{Kind: schema.TypeBinary},
							Nullable: true,
						},
						"col_json": {
							Name:     "col_json",
							Type:     schema.DataType{Kind: schema.TypeJSON},
							Nullable: true,
						},
					},
				},
			},
		},
	}

	sql, err := renderer.Render(context.Background(), plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedParts := []string{
		"[col_str_len] NVARCHAR(100) NULL",
		"[col_str_max] NVARCHAR(MAX) NULL",
		"[col_int] INT NULL",
		"[col_dec_prec] DECIMAL(10, 2) NULL",
		"[col_dec_def] DECIMAL(18, 0) NULL",
		"[col_bool] BIT NULL",
		"[col_datetime] DATETIME2 NULL",
		"[col_uuid] UNIQUEIDENTIFIER NULL",
		"[col_bin_len] VARBINARY(64) NULL",
		"[col_bin_max] VARBINARY(MAX) NULL",
		"[col_json] NVARCHAR(MAX) CHECK (ISJSON([col_json]) > 0) NULL",
	}

	for _, part := range expectedParts {
		if !strings.Contains(sql, part) {
			t.Errorf("expected SQL to contain:\n%s\n\nActual SQL:\n%s", part, sql)
		}
	}
}

func TestMSSQLRenderer_DataOperations_Constraints(t *testing.T) {
	renderer := NewMSSQLRenderer()

	op := diff.InsertDataOperation{
		SchemaName: "dbo",
		TableName:  "users",
		Row: map[string]interface{}{
			"id":   1,
			"name": "Alice",
		},
		HasIdentity: false,
	}

	plan := &diff.MigrationPlan{
		DataOperations: []diff.Operation{op},
	}

	sql, err := renderer.Render(context.Background(), plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(sql, "EXEC sp_msforeachtable 'ALTER TABLE ? NOCHECK CONSTRAINT all';") {
		t.Errorf("expected SQL to contain NOCHECK CONSTRAINT all")
	}
}
