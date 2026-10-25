package mssql

import (
	"strings"
	"testing"

	"github.com/mmoehabb/dbdiff/internal/domain/diff"
	"github.com/mmoehabb/dbdiff/internal/domain/schema"
)

func TestMSSQLIntegration_IndexedViewFunction(t *testing.T) {
	server := integrationServer(t)
	sourceDSN, source := testDatabase(t, server, "source")
	targetDSN, target := testDatabase(t, server, "target")
	const options = `SET ANSI_NULLS ON; SET QUOTED_IDENTIFIER ON; SET ANSI_PADDING ON; SET ANSI_WARNINGS ON; SET ARITHABORT ON; SET CONCAT_NULL_YIELDS_NULL ON; SET NUMERIC_ROUNDABORT OFF;`
	const view = `EXEC(N'CREATE VIEW dbo.Summary WITH SCHEMABINDING AS SELECT Id,dbo.Value(Id) AS Value FROM dbo.Items'); CREATE UNIQUE CLUSTERED INDEX IX_Summary ON dbo.Summary(Id);`
	ddl := options + `CREATE TABLE dbo.Items(Id int NOT NULL PRIMARY KEY); CREATE TABLE dbo.Independent(Id int NULL);
 EXEC(N'CREATE FUNCTION dbo.Value(@Id int) RETURNS int WITH SCHEMABINDING AS BEGIN RETURN @Id+1 END');` + view
	execFixture(t, source, ddl)
	execFixture(t, target, ddl)
	execFixture(t, source, options+`DROP VIEW dbo.Summary; EXEC(N'ALTER FUNCTION dbo.Value(@Id int) RETURNS int WITH SCHEMABINDING AS BEGIN RETURN @Id+2 END');`+view+`ALTER TABLE dbo.Independent ADD Extra int NULL;`)
	p := planTest(t, inspectTest(t, sourceDSN), inspectTest(t, targetDSN))
	p.ApplySafety(true)
	if !p.Incomplete || len(p.SchemaOperations) != 1 || p.SchemaOperations[0].OperationType() != diff.AddColumn {
		t.Fatalf("indexed view function not isolated: %+v", p)
	}
	applyTest(t, target, p)
	var definition string
	if err := target.QueryRow("SELECT OBJECT_DEFINITION(OBJECT_ID('dbo.Value'))").Scan(&definition); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(definition, "@Id+1") {
		t.Fatalf("target function changed: %s", definition)
	}
	execFixture(t, source, `DROP VIEW dbo.Summary; DROP FUNCTION dbo.Value;`)
	p = planTest(t, inspectTest(t, sourceDSN), inspectTest(t, targetDSN))
	p.ApplySafety(true)
	if !p.Incomplete || len(p.SchemaOperations) != 0 {
		t.Fatalf("unsupported view teardown allowed function drop: %+v", p)
	}
}

func TestMSSQLIntegration_FullTextStoplistModes(t *testing.T) {
	server := integrationServer(t)
	dsn, db := testDatabase(t, server, "stoplists")
	execFixture(t, db, "CREATE TABLE dbo.Items(Id int NOT NULL CONSTRAINT PK_Items PRIMARY KEY, Content nvarchar(100) NULL);")
	// Execute the production full-text projection with catalog-shaped rows because
	// the integration server need not have the optional full-text service installed.
	var query string
	for _, branch := range strings.Split(queryTableFeatures, "UNION ALL") {
		if strings.Contains(branch, "'full-text index'") {
			query = branch
			break
		}
	}
	if query == "" {
		t.Fatal("missing full-text projection")
	}
	query = strings.NewReplacer(
		"sys.fulltext_indexes", "fixture_indexes",
		"sys.fulltext_catalogs", "fixture_catalogs",
		"sys.fulltext_stoplists", "fixture_stoplists",
		"sys.fulltext_stopwords", "fixture_stopwords",
		"sys.fulltext_index_columns", "fixture_columns",
	).Replace(query)
	query = `WITH fixture_indexes AS (
 SELECT OBJECT_ID('dbo.Items') object_id, 1 unique_index_id, 1 fulltext_catalog_id, 'A' change_tracking_state, CAST(1 AS bit) is_enabled, CAST(@p1 AS int) stoplist_id
 ), fixture_catalogs AS (SELECT 1 fulltext_catalog_id, 'Catalog' name),
 fixture_stoplists AS (SELECT CAST(@p2 AS int) stoplist_id, 'Custom' name),
 fixture_stopwords AS (SELECT CAST(@p2 AS int) stoplist_id,1033 language_id,CAST(@p3 AS nvarchar(64)) stopword),
 fixture_columns AS (SELECT OBJECT_ID('dbo.Items') object_id, 2 column_id, CAST(NULL AS int) type_column_id, 1033 language_id, CAST(0 AS bit) statistical_semantics)
 ` + query
	read := func(id any, customID int, word string) schema.Feature {
		t.Helper()
		var sn, tn string
		var f schema.Feature
		if err := db.QueryRow(query, id, customID, word).Scan(&sn, &tn, &f.Kind, &f.Scope, &f.Name, &f.Detail); err != nil {
			t.Fatal(err)
		}
		return f
	}
	off, system, custom := read(nil, 7, "the"), read(0, 7, "the"), read(7, 7, "the")
	for _, tc := range []struct {
		f    schema.Feature
		mode string
	}{{off, "OFF"}, {system, "SYSTEM"}, {custom, "CUSTOM"}} {
		if !strings.Contains(tc.f.Detail, `"stoplist_mode":"`+tc.mode+`"`) {
			t.Fatalf("missing mode %s: %s", tc.mode, tc.f.Detail)
		}
	}
	if custom.Detail != read(19, 19, "the").Detail {
		t.Fatal("fingerprint includes database-local stoplist ID")
	}
	if custom.Detail == read(7, 7, "a").Detail {
		t.Fatal("custom stoplist word changes lost")
	}
	a, b := inspectTest(t, dsn), inspectTest(t, dsn)
	a.Schemas["dbo"].Tables["Items"].Features = []schema.Feature{system}
	b.Schemas["dbo"].Tables["Items"].Features = []schema.Feature{off}
	p := planTest(t, a, b)
	if !p.Incomplete || len(p.Omitted) == 0 {
		t.Fatalf("stoplist difference lost: %+v", p)
	}
}
