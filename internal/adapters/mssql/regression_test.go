package mssql

import (
	"context"
	"github.com/mmoehabb/dbdiff/internal/domain/diff"
	"github.com/mmoehabb/dbdiff/internal/domain/schema"
	"strings"
	"testing"
)

func TestNativeTypesRoundTrip(t *testing.T) {
	r := NewMSSQLRenderer()
	for _, tc := range []struct {
		name                     string
		length, precision, scale int
		want                     string
	}{{"bigint", 8, 19, 0, "BIGINT"}, {"varchar", 8000, 0, 0, "VARCHAR(8000)"}, {"nvarchar", 100, 0, 0, "NVARCHAR(50)"}, {"datetimeoffset", 10, 34, 7, "DATETIMEOFFSET(7)"}, {"time", 5, 16, 7, "TIME(7)"}, {"numeric", 9, 19, 4, "NUMERIC(19, 4)"}, {"varbinary", -1, 0, 0, "VARBINARY(MAX)"}} {
		t.Run(tc.name, func(t *testing.T) {
			dt := mapMSSQLTypeToDomain(tc.name, tc.length, tc.precision, tc.scale)
			if got := r.renderDataType(dt, ""); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	if dt := mapMSSQLTypeToDomain("geography", 0, 0, 0); dt.Kind != "" {
		t.Fatal("unknown native type silently coerced")
	}
}
func TestDefaultExpressionPreserved(t *testing.T) {
	for _, s := range []string{"((0))", "(1)+(2)", "('a)(')", "(NEXT VALUE FOR dbo.seq)"} {
		if got := parseDefaultValue(s).Value; got != s {
			t.Fatalf("default changed: %s", got)
		}
	}
}
func TestIndexSQLAndEscaping(t *testing.T) {
	sql, err := renderIndex(diff.CreateIndexOperation{SchemaName: "a]b", TableName: "ArticleViews", Index: schema.Index{Name: "IX_ArticleViews_ArticleId", Columns: []string{"ArticleId"}, Descending: []bool{true}, IncludedColumns: []string{"Body]text"}, Filter: "[ArticleId] > 0", IsUnique: true}})
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"CREATE UNIQUE NONCLUSTERED INDEX", "[a]]b].[ArticleViews]", "[ArticleId] DESC", "INCLUDE ([Body]]text])", "WHERE [ArticleId] > 0"} {
		if !strings.Contains(sql, part) {
			t.Fatalf("missing %s: %s", part, sql)
		}
	}
}
func TestForeignKeyActionsAndNames(t *testing.T) {
	r := NewMSSQLRenderer()
	sql, err := r.renderOperation(diff.AddForeignKeyOperation{SchemaName: "dbo", TableName: "child", ForeignKey: schema.ForeignKey{Name: "FK_actual", Columns: []string{"Id"}, RefTable: "[other.schema].[Parent]]x]", RefColumns: []string{"Id"}, OnDelete: "CASCADE", OnUpdate: "SET_NULL"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "REFERENCES [other.schema].[Parent]]x]") || !strings.Contains(sql, "ON DELETE CASCADE ON UPDATE SET NULL") {
		t.Fatal(sql)
	}
	if _, err := r.renderOperation(diff.DropPrimaryKeyOperation{SchemaName: "dbo", TableName: "t"}); err == nil {
		t.Fatal("guessed primary key name")
	}
}
func TestAtomicScriptAndUnknownOperations(t *testing.T) {
	r := NewMSSQLRenderer()
	sql, err := r.Render(context.Background(), &diff.MigrationPlan{SchemaOperations: []diff.Operation{diff.CreateSchemaOperation{SchemaName: "x'y]"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"SET XACT_ABORT ON", "BEGIN TRY", "IF XACT_STATE() <> 0 ROLLBACK TRANSACTION", "THROW;", "CREATE SCHEMA [x''y]]]"} {
		if !strings.Contains(sql, part) {
			t.Fatalf("missing %s: %s", part, sql)
		}
	}
	if _, err := r.Render(context.Background(), &diff.MigrationPlan{SchemaOperations: []diff.Operation{diff.AlterTablePropertiesOperation{}}}); err == nil {
		t.Fatal("unsupported operation emitted executable output")
	}
}
func TestSequenceAlterDoesNotRestart(t *testing.T) {
	sql := NewMSSQLRenderer().sequenceSQL("dbo", schema.Sequence{Name: "seq", StartValue: 1, Increment: 2, MinValue: 1, MaxValue: 100, IsCycling: true, CacheSize: 10}, true)
	if strings.Contains(sql, "RESTART") || !strings.Contains(sql, "CYCLE CACHE 10") {
		t.Fatal(sql)
	}
}
func TestModuleHeaderOnlyRewritten(t *testing.T) {
	sql, err := moduleSQL("/* comment CREATE VIEW */ ALTER VIEW dbo.v AS SELECT 'CREATE VIEW' AS x", true, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "CREATE OR ALTER VIEW dbo.v") || !strings.Contains(sql, "''CREATE VIEW''") || strings.Contains(sql, "DROP VIEW") {
		t.Fatal(sql)
	}
}

func TestModuleQualifiedToActualSchema(t *testing.T) {
	for _, def := range []string{`CREATE VIEW v AS SELECT 1 AS x`, `CREATE VIEW [old.schema].[v]]x] AS SELECT 1 AS x`, `CREATE VIEW "old"."v" AS SELECT 1 AS x`} {
		got := canonicalModule(def, "actual.schema", "v]x")
		if got != "CREATE OR ALTER VIEW [actual.schema].[v]]x] AS SELECT 1 AS x" {
			t.Fatal(got)
		}
	}
}
func TestExtendedPropertyTypesAndLevels(t *testing.T) {
	sql, err := propertySQL("add", "It's", schema.PropertyValue{Type: "decimal", Value: "123.4567", Precision: 12, Scale: 4}, diff.ExtendedPropertyLevel{Level0Type: "SCHEMA", Level0Name: "dbo", Level1Type: "TABLE", Level1Name: "t", Level2Type: "COLUMN", Level2Name: "c"})
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"CONVERT(decimal(12,4), N'123.4567')", "@value=@dbdiff_property", "@name=N'It''s'", "@level2name=N'c'"} {
		if !strings.Contains(sql, part) {
			t.Fatal(sql)
		}
	}
	if _, err := propertySQL("add", "bad", schema.PropertyValue{Type: "unknown"}, diff.ExtendedPropertyLevel{}); err == nil {
		t.Fatal("unknown property type coerced")
	}
}

func TestIndexDefaultFillFactorUsesServerDefault(t *testing.T) {
	sql, err := renderIndex(diff.CreateIndexOperation{SchemaName: "dbo", TableName: "Items", Index: schema.Index{
		Name: "IX_Items_A", Columns: []string{"A"}, OptionsKnown: true,
		AllowRowLocks: true, AllowPageLocks: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sql, "FILLFACTOR = 0") || !strings.Contains(sql, "ALLOW_ROW_LOCKS = ON") {
		t.Fatalf("invalid default index options: %s", sql)
	}
}

func TestCollationRendering(t *testing.T) {
	r := NewMSSQLRenderer()
	op := diff.CreateTableOperation{SchemaName: "dbo", Table: schema.Table{Name: "Items", Columns: map[string]*schema.Column{
		"Name": {Name: "Name", Type: schema.DataType{Kind: schema.TypeString, NativeType: "nvarchar", Length: intPointer(100)}, Collation: "SQL_Latin1_General_CP1_CI_AS"},
	}}}
	sql, err := r.renderOperation(op)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "COLLATE SQL_Latin1_General_CP1_CI_AS") || strings.Contains(sql, "COLLATE [SQL_Latin1_General_CP1_CI_AS]") {
		t.Fatalf("invalid collation syntax: %s", sql)
	}
	op.Table.Columns["Name"].Collation = "Latin1; DROP TABLE Items;--"
	if _, err := r.renderOperation(op); err == nil {
		t.Fatal("unsafe collation name accepted")
	}
}

func intPointer(v int) *int { return &v }
