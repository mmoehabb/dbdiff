package mssql

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mmoehabb/dbdiff/internal/domain/diff"
	"github.com/mmoehabb/dbdiff/internal/domain/schema"
)

func TestMSSQLIntegration_ReviewRegressions(t *testing.T) {
	server := integrationServer(t)
	t.Run("security-policy predicate functions", func(t *testing.T) {
		sourceDSN, source := testDatabase(t, server, "source")
		targetDSN, target := testDatabase(t, server, "target")
		ddl := `EXEC(N'CREATE SCHEMA security');
CREATE TABLE dbo.Items(Id int NOT NULL PRIMARY KEY);
CREATE TABLE dbo.Independent(Id int NULL);
EXEC(N'CREATE FUNCTION dbo.Visible(@Id int) RETURNS TABLE WITH SCHEMABINDING AS RETURN SELECT 1 AS allowed WHERE @Id>0');
EXEC(N'CREATE SECURITY POLICY security.Policy ADD FILTER PREDICATE dbo.Visible(Id) ON dbo.Items WITH(STATE=ON)');`
		execFixture(t, source, ddl)
		execFixture(t, target, ddl)
		a, b := inspectTest(t, sourceDSN), inspectTest(t, targetDSN)
		found := false
		for _, dep := range b.Dependencies {
			found = found || (dep.ReferencingType == "SP" && dep.SchemaBound && dep.Schema == "security" && dep.Object == "Policy" && dep.RefSchema == "dbo" && dep.RefObject == "Visible")
		}
		if !found {
			t.Fatalf("security policy function dependency not captured: %+v", b.Dependencies)
		}
		if p := planTest(t, a, b); p.Incomplete || len(p.SchemaOperations) != 0 {
			t.Fatalf("unchanged policy not comparable: %+v", p)
		}
		execFixture(t, source, `DROP SECURITY POLICY security.Policy;
EXEC(N'ALTER FUNCTION dbo.Visible(@Id int) RETURNS TABLE WITH SCHEMABINDING AS RETURN SELECT 1 AS allowed WHERE @Id>10');
EXEC(N'CREATE SECURITY POLICY security.Policy ADD FILTER PREDICATE dbo.Visible(Id) ON dbo.Items WITH(STATE=ON)');
ALTER TABLE dbo.Independent ADD Extra int NULL;`)
		p := planTest(t, inspectTest(t, sourceDSN), b)
		p.ApplySafety(true)
		assertPolicyOmission(t, p)
		if len(p.SchemaOperations) != 1 || p.SchemaOperations[0].OperationType() != diff.AddColumn {
			t.Fatalf("predicate alteration not isolated: %+v", p)
		}
		applyTest(t, target, p)
		var definition string
		if err := target.QueryRow(`SELECT OBJECT_DEFINITION(OBJECT_ID('dbo.Visible'))`).Scan(&definition); err != nil || !strings.Contains(definition, "@Id>0") {
			t.Fatalf("predicate function changed: %s %v", definition, err)
		}
		// A source-side policy removal cannot make the target function safe to drop:
		// security policy teardown is itself unsupported and is not in the plan.
		execFixture(t, source, `DROP SECURITY POLICY security.Policy; DROP FUNCTION dbo.Visible;`)
		p = planTest(t, inspectTest(t, sourceDSN), inspectTest(t, targetDSN))
		p.ApplySafety(true)
		assertPolicyOmission(t, p)
		if len(p.SchemaOperations) != 0 {
			t.Fatalf("predicate drop escaped isolation: %+v", p)
		}
		applyTest(t, target, p)
	})
	t.Run("table-type definitions", func(t *testing.T) {
		sourceDSN, source := testDatabase(t, server, "source")
		targetDSN, target := testDatabase(t, server, "target")
		const columns = `Id int NOT NULL,Other int NOT NULL DEFAULT 0,Payload nvarchar(20) NULL,PRIMARY KEY(Id,Other),UNIQUE(Payload),CHECK(Other>=0)`
		ddl := `CREATE TABLE dbo.Independent(Id int NULL); CREATE TYPE dbo.Code FROM nvarchar(10) NULL; CREATE TYPE dbo.Rows AS TABLE (` + columns + `);`
		execFixture(t, source, ddl)
		execFixture(t, target, ddl)
		a, b := inspectTest(t, sourceDSN), inspectTest(t, targetDSN)
		baseline := tableTypeFeature(t, b)
		// Generated constraint names and database-local IDs must not cause drift.
		if got := tableTypeFeature(t, a); got != baseline {
			t.Fatalf("identical table types have different fingerprints:\n%s\n%s", got, baseline)
		}
		if p := planTest(t, a, b); p.Incomplete || len(p.SchemaOperations) != 0 {
			t.Fatalf("identical table types not comparable: %+v", p)
		}
		execFixture(t, source, `ALTER TABLE dbo.Independent ADD Extra int NULL;`)
		var last *diff.MigrationPlan
		for _, variant := range []struct{ name, definition string }{
			{"column type", strings.Replace(columns, "nvarchar(20)", "nvarchar(40)", 1)},
			{"nullability", strings.Replace(columns, "Payload nvarchar(20) NULL", "Payload nvarchar(20) NOT NULL", 1)},
			{"column order", strings.Replace(columns, "Id int NOT NULL,Other int NOT NULL DEFAULT 0", "Other int NOT NULL DEFAULT 0,Id int NOT NULL", 1)},
			{"key order", strings.Replace(columns, "PRIMARY KEY(Id,Other)", "PRIMARY KEY(Other,Id)", 1)},
			{"key direction", strings.Replace(columns, "PRIMARY KEY(Id,Other)", "PRIMARY KEY(Id DESC,Other)", 1)},
			{"unique constraint", strings.Replace(columns, ",UNIQUE(Payload)", "", 1)},
			{"check constraint", strings.Replace(columns, "CHECK(Other>=0)", "CHECK(Other>0)", 1)},
			{"default constraint", strings.Replace(columns, "DEFAULT 0", "DEFAULT 1", 1)},
		} {
			t.Run(variant.name, func(t *testing.T) {
				execFixture(t, source, `DROP PROCEDURE IF EXISTS dbo.Consumer; DROP TYPE dbo.Rows; CREATE TYPE dbo.Rows AS TABLE (`+variant.definition+`);`)
				desired := inspectTest(t, sourceDSN)
				if got := tableTypeFeature(t, desired); got == baseline {
					t.Fatalf("table-type structure difference lost: %s", got)
				}
				// Detect feature-only changes before adding any consumer operation.
				p := planTest(t, desired, b)
				if !p.Incomplete || len(p.Omitted) != 2 || len(p.SchemaOperations) != 1 {
					t.Fatalf("type-only difference hidden: %+v", p)
				}
				execFixture(t, source, `EXEC(N'CREATE PROCEDURE dbo.Consumer @input dbo.Rows READONLY AS SELECT Id FROM @input;');`)
				p = planTest(t, inspectTest(t, sourceDSN), b)
				p.ApplySafety(true)
				if !p.Incomplete || len(p.SchemaOperations) != 1 || p.SchemaOperations[0].OperationType() != diff.AddColumn {
					t.Fatalf("incompatible type consumer escaped isolation: %+v", p)
				}
				blockedConsumer := false
				for _, d := range p.Omitted {
					blockedConsumer = blockedConsumer || (strings.Contains(d.ID, "create_procedure") && strings.Contains(d.Reason, "user-defined type"))
				}
				if !blockedConsumer {
					t.Fatalf("missing consumer dependency diagnostic: %+v", p.Omitted)
				}
				last = p
			})
		}
		if last == nil {
			t.Fatal("no valid partial plan")
		}
		applyTest(t, target, last)
		if got := tableTypeFeature(t, inspectTest(t, targetDSN)); got != baseline {
			t.Fatal("partial migration changed target type")
		}
		var count int
		if err := target.QueryRow(`SELECT COUNT(*) FROM sys.procedures WHERE name='Consumer'`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("incompatible consumer created: %d %v", count, err)
		}
	})
}

func assertPolicyOmission(t *testing.T, p *diff.MigrationPlan) {
	t.Helper()
	if !p.Incomplete {
		t.Fatalf("policy dependency not marked incomplete: %+v", p)
	}
	for _, d := range p.Omitted {
		if strings.Contains(d.Reason, "schema-bound security policy security.Policy") && strings.Contains(d.Reason, "dbo.Visible") {
			return
		}
	}
	t.Fatalf("missing policy diagnostic: %+v", p.Omitted)
}

func tableTypeFeature(t *testing.T, db *schema.Database) string {
	t.Helper()
	for _, f := range db.Features {
		if f.Schema == "dbo" && f.Object == "Rows" && f.Feature.Kind == "user-defined type" {
			if !json.Valid([]byte(f.Feature.Detail)) {
				t.Fatalf("invalid fingerprint JSON: %s", f.Feature.Detail)
			}
			return f.Feature.Detail
		}
	}
	t.Fatal("missing table-type feature")
	return ""
}
