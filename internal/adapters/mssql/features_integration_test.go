package mssql

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mmoehabb/dbdiff/internal/domain/diff"
)

func TestMSSQLIntegration_FeatureIsolation(t *testing.T) {
	server := integrationServer(t)
	for _, feature := range []struct{ name, ddl, kind string }{
		{"primary-key options", `ALTER INDEX PK_Items ON dbo.Items REBUILD WITH(FILLFACTOR=80);`, "primary-key options"},
		{"compression", `ALTER INDEX IX_Payload ON dbo.Items REBUILD WITH(DATA_COMPRESSION=PAGE);`, "compression"},
		{"untrusted check", `ALTER TABLE dbo.Items WITH NOCHECK ADD CONSTRAINT CK_Payload CHECK(Payload>=0);`, "constraint state"},
		{"masking", `ALTER TABLE dbo.Items ALTER COLUMN Payload ADD MASKED WITH(FUNCTION='default()');`, "dynamic data masking"},
		{"disabled index", `ALTER INDEX IX_Payload ON dbo.Items DISABLE;`, "disabled index"},
		{"XML index", `ALTER TABLE dbo.Items ADD XmlData xml NULL; CREATE PRIMARY XML INDEX IX_XmlData ON dbo.Items(XmlData);`, "specialized index"},
	} {
		t.Run(feature.name, func(t *testing.T) {
			sourceDSN, source := testDatabase(t, server, "source")
			targetDSN, target := testDatabase(t, server, "target")
			ddl := `CREATE TABLE dbo.Items(Id int NOT NULL CONSTRAINT PK_Items PRIMARY KEY,Payload int NULL); CREATE INDEX IX_Payload ON dbo.Items(Payload);`
			execFixture(t, source, ddl+feature.ddl)
			execFixture(t, target, ddl+feature.ddl)
			a, b := inspectTest(t, sourceDSN), inspectTest(t, targetDSN)
			found := false
			for _, f := range b.Schemas["dbo"].Tables["Items"].Features {
				found = found || f.Kind == feature.kind
			}
			if !found {
				t.Fatalf("feature not detected: %+v", b.Schemas["dbo"].Tables["Items"].Features)
			}
			if p := planTest(t, a, b); p.Incomplete || len(p.SchemaOperations) != 0 {
				t.Fatalf("unchanged feature rejected: %+v", p)
			}
			execFixture(t, source, `ALTER TABLE dbo.Items ADD Extra int NULL;`)
			p := planTest(t, inspectTest(t, sourceDSN), b)
			p.ApplySafety(true)
			if p.Incomplete || len(p.SchemaOperations) != 1 {
				t.Fatalf("independent column addition rejected: %+v", p)
			}
			applyTest(t, target, p)
			after := inspectTest(t, targetDSN)
			if p := planTest(t, inspectTest(t, sourceDSN), after); p.Incomplete || len(p.SchemaOperations) != 0 {
				t.Fatalf("feature was lost or migration did not converge: %+v", p)
			}
			if feature.name == "primary-key options" || feature.name == "compression" || feature.name == "untrusted check" || feature.name == "disabled index" {
				// Construct the desired source independently so it has the changed column
				// type with the same special property; SQL Server enforces dependencies.
				drop := `DROP TABLE dbo.Items;`
				desired := strings.ReplaceAll(ddl, "Id int", "Id bigint")
				if feature.name != "primary-key options" {
					desired = strings.ReplaceAll(ddl, "Payload int", "Payload bigint")
				}
				execFixture(t, source, drop+desired+feature.ddl+`ALTER TABLE dbo.Items ADD Extra int NULL; CREATE TABLE dbo.Independent(Id int NULL);`)
				p = planTest(t, inspectTest(t, sourceDSN), after)
				p.ApplySafety(true)
				if !p.Incomplete || len(p.SchemaOperations) != 1 || p.SchemaOperations[0].OperationType() != diff.CreateTable {
					t.Fatalf("unsafe rebuild or lost independent change: %+v", p)
				}
				applyTest(t, target, p)
				var native string
				column := "Payload"
				if feature.name == "primary-key options" {
					column = "Id"
				}
				if err := target.QueryRow(`SELECT TYPE_NAME(system_type_id) FROM sys.columns WHERE object_id=OBJECT_ID('dbo.Items') AND name=@p1`, column).Scan(&native); err != nil || native != "int" {
					t.Fatalf("blocked column changed: %s %v", native, err)
				}
			}
		})
	}
	t.Run("feature-only differences", func(t *testing.T) {
		sourceDSN, source := testDatabase(t, server, "source")
		targetDSN, target := testDatabase(t, server, "target")
		ddl := `CREATE TABLE dbo.Items(Id int NOT NULL PRIMARY KEY);`
		execFixture(t, source, ddl+`ALTER TABLE dbo.Items REBUILD WITH(DATA_COMPRESSION=PAGE);`)
		execFixture(t, target, ddl)
		p := planTest(t, inspectTest(t, sourceDSN), inspectTest(t, targetDSN))
		if !p.Incomplete || len(p.SchemaOperations) != 0 || len(p.Omitted) != 1 || !strings.Contains(p.Omitted[0].Reason, "compression") {
			t.Fatalf("feature-only difference hidden: %+v", p)
		}
		sql, err := NewMSSQLRenderer().Render(context.Background(), p)
		if err != nil || !strings.Contains(sql, "compression") || strings.Contains(sql, "BEGIN TRANSACTION") {
			t.Fatalf("invalid partial output: %s %v", sql, err)
		}
	})
	t.Run("temporal and data isolation", func(t *testing.T) {
		sourceDSN, source := testDatabase(t, server, "source")
		targetDSN, target := testDatabase(t, server, "target")
		ddl := `CREATE TABLE dbo.HistoryItems(Id int NOT NULL PRIMARY KEY,ValidFrom datetime2 GENERATED ALWAYS AS ROW START NOT NULL,ValidTo datetime2 GENERATED ALWAYS AS ROW END NOT NULL,PERIOD FOR SYSTEM_TIME(ValidFrom,ValidTo)) WITH(SYSTEM_VERSIONING=ON(HISTORY_TABLE=dbo.HistoryItemsHistory)); CREATE TABLE dbo.Ordinary(Id int NOT NULL PRIMARY KEY);`
		execFixture(t, source, ddl+`INSERT INTO dbo.HistoryItems(Id) VALUES(1); INSERT INTO dbo.Ordinary VALUES(2);`)
		execFixture(t, target, ddl)
		a, b := inspectTest(t, sourceDSN), inspectTest(t, targetDSN)
		p := planTest(t, a, b)
		if p.Incomplete || len(p.SchemaOperations) != 0 {
			t.Fatalf("unchanged temporal schema rejected: %+v", p)
		}
		// History tables are keyless, but must be skipped before key validation.
		d := diff.NewDataDiffer(NewMSSQLDataReader(sourceDSN), NewMSSQLDataReader(targetDSN))
		ops, err := d.CompareData(context.Background(), a, b)
		if err != nil || len(ops) != 1 || len(d.OmittedTables) != 2 {
			t.Fatalf("data isolation failed: %v %v %v", err, ops, d.OmittedTables)
		}
		p.DataOperations = ops
		applyTest(t, target, p)
		var count int
		if err := target.QueryRow(`SELECT COUNT(*) FROM dbo.HistoryItems`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("temporal data changed: %d %v", count, err)
		}
		execFixture(t, source, `ALTER TABLE dbo.HistoryItems ADD Extra int NULL; CREATE TABLE dbo.Independent(Id int NULL);`)
		p = planTest(t, inspectTest(t, sourceDSN), b)
		if !p.Incomplete || len(p.SchemaOperations) != 1 {
			t.Fatalf("temporal DDL not isolated: %+v", p)
		}
	})
	t.Run("filegroup and explicit placement", func(t *testing.T) {
		sourceDSN, source := testDatabase(t, server, "source")
		targetDSN, target := testDatabase(t, server, "target")
		storage := `DECLARE @sql nvarchar(max),@path nvarchar(128)=CONVERT(nvarchar(128),SERVERPROPERTY('InstanceDefaultDataPath'))+DB_NAME()+'_archive.ndf';
SET @sql=N'ALTER DATABASE '+QUOTENAME(DB_NAME())+N' ADD FILEGROUP Archive'; EXEC(@sql);
SET @sql=N'ALTER DATABASE '+QUOTENAME(DB_NAME())+N' ADD FILE(NAME=ArchiveData,FILENAME='+QUOTENAME(@path,CHAR(39))+N',SIZE=8MB) TO FILEGROUP Archive'; EXEC(@sql);
SET @sql=N'ALTER DATABASE '+QUOTENAME(DB_NAME())+N' MODIFY FILEGROUP Archive DEFAULT'; EXEC(@sql);
CREATE TABLE dbo.Items(Id int NOT NULL CONSTRAINT PK_Items PRIMARY KEY) ON Archive;`
		execFixture(t, source, storage)
		execFixture(t, target, storage)
		execFixture(t, source, `ALTER TABLE dbo.Items ADD Extra int NULL; CREATE INDEX IX_Extra ON dbo.Items(Extra) ON [PRIMARY]; CREATE TABLE dbo.Independent(Id int NULL) ON [PRIMARY];`)
		p := planTest(t, inspectTest(t, sourceDSN), inspectTest(t, targetDSN))
		p.ApplySafety(true)
		if p.Incomplete || len(p.SchemaOperations) != 3 {
			t.Fatalf("safe changes on non-default storage blocked: %+v", p)
		}
		applyTest(t, target, p)
		if p := planTest(t, inspectTest(t, sourceDSN), inspectTest(t, targetDSN)); p.Incomplete || len(p.SchemaOperations) != 0 {
			t.Fatalf("storage placement not preserved: %+v", p)
		}
	})
	t.Run("indexed view", func(t *testing.T) {
		sourceDSN, source := testDatabase(t, server, "source")
		targetDSN, target := testDatabase(t, server, "target")
		ddl := `SET ANSI_NULLS ON; SET QUOTED_IDENTIFIER ON; SET ANSI_PADDING ON; SET ANSI_WARNINGS ON; SET ARITHABORT ON; SET CONCAT_NULL_YIELDS_NULL ON; SET NUMERIC_ROUNDABORT OFF;
CREATE TABLE dbo.Items(Id int NOT NULL PRIMARY KEY,Payload int NULL);
EXEC(N'CREATE VIEW dbo.Summary WITH SCHEMABINDING AS SELECT Id,Payload FROM dbo.Items'); CREATE UNIQUE CLUSTERED INDEX IX_Summary ON dbo.Summary(Id);`
		execFixture(t, source, ddl)
		execFixture(t, target, ddl)
		a, b := inspectTest(t, sourceDSN), inspectTest(t, targetDSN)
		if len(a.Features) != 1 || a.Features[0].Feature.Kind != "indexed view" {
			t.Fatalf("indexed view not detected: %+v", a.Features)
		}
		if p := planTest(t, a, b); p.Incomplete || len(p.SchemaOperations) != 0 {
			t.Fatalf("unchanged indexed view rejected: %+v", p)
		}
		execFixture(t, source, `EXEC(N'ALTER VIEW dbo.Summary WITH SCHEMABINDING AS SELECT Id,Payload FROM dbo.Items WHERE Id>0'); CREATE UNIQUE CLUSTERED INDEX IX_Summary ON dbo.Summary(Id); CREATE TABLE dbo.Independent(Id int NULL);`)
		p := planTest(t, inspectTest(t, sourceDSN), b)
		if !p.Incomplete || len(p.SchemaOperations) != 1 {
			t.Fatalf("indexed view alteration not isolated: %+v", p)
		}
		applyTest(t, target, p)
		var count int
		if err := target.QueryRow(`SELECT COUNT(*) FROM sys.indexes WHERE object_id=OBJECT_ID('dbo.Summary') AND index_id>0`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("indexed view lost index: %d %v", count, err)
		}
	})
	t.Run("CLI incomplete output and selection", func(t *testing.T) {
		sourceDSN, source := testDatabase(t, server, "source")
		targetDSN, target := testDatabase(t, server, "target")
		ddl := `CREATE TABLE dbo.Items(Id int NOT NULL PRIMARY KEY); CREATE TABLE dbo.Ordinary(Id int NOT NULL PRIMARY KEY);`
		execFixture(t, source, ddl+`ALTER TABLE dbo.Items REBUILD WITH(DATA_COMPRESSION=PAGE); ALTER TABLE dbo.Ordinary ADD Extra int NULL;`)
		execFixture(t, target, ddl)
		binary := filepath.Join(t.TempDir(), "dbdiff")
		if output, err := exec.Command("go", "build", "-o", binary, "../../..").CombinedOutput(); err != nil {
			t.Fatalf("build CLI: %s %v", output, err)
		}
		run := func(extra ...string) (string, int) {
			args := append([]string{"compare", "-d", "mssql", "-s", sourceDSN, "-t", targetDSN, "--allow-destructive", "--quiet", "-f", "json"}, extra...)
			cmd := exec.Command(binary, args...)
			var stderr strings.Builder
			cmd.Stderr = &stderr
			output, err := cmd.Output()
			code := 0
			if err != nil {
				if exit, ok := err.(*exec.ExitError); ok {
					code = exit.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			return string(output), code
		}
		output, code := run()
		var result struct {
			Incomplete       bool              `json:"incomplete"`
			SchemaOperations []json.RawMessage `json:"schema_operations"`
			Omitted          []json.RawMessage `json:"omitted"`
		}
		if err := json.Unmarshal([]byte(output), &result); err != nil || code != 3 || !result.Incomplete || len(result.SchemaOperations) != 1 || len(result.Omitted) != 1 {
			t.Fatalf("incomplete comparison incorrectly reported: code=%d output=%s err=%v", code, output, err)
		}
		output, code = run("--tables", "dbo.Ordinary")
		if err := json.Unmarshal([]byte(output), &result); err != nil || code != 1 || result.Incomplete || len(result.SchemaOperations) != 1 || len(result.Omitted) != 0 {
			t.Fatalf("selection included unrelated feature: code=%d output=%s err=%v", code, output, err)
		}
	})

	t.Run("row-level security", func(t *testing.T) {
		sourceDSN, source := testDatabase(t, server, "source")
		targetDSN, target := testDatabase(t, server, "target")
		ddl := `CREATE TABLE dbo.Items(Id int NOT NULL PRIMARY KEY); CREATE TABLE dbo.Independent(Id int NOT NULL PRIMARY KEY);
EXEC(N'CREATE FUNCTION dbo.Visible(@Id int) RETURNS TABLE WITH SCHEMABINDING AS RETURN SELECT 1 AS allowed WHERE @Id>0');
EXEC(N'CREATE SECURITY POLICY dbo.Policy ADD FILTER PREDICATE dbo.Visible(Id) ON dbo.Items WITH(STATE=ON)');`
		execFixture(t, source, ddl)
		execFixture(t, target, ddl)
		a, b := inspectTest(t, sourceDSN), inspectTest(t, targetDSN)
		if p := planTest(t, a, b); p.Incomplete || len(p.SchemaOperations) != 0 {
			t.Fatalf("unchanged security policy rejected: %+v", p)
		}
		execFixture(t, source, `ALTER TABLE dbo.Items ADD Extra int NULL; ALTER TABLE dbo.Independent ADD Extra int NULL;`)
		p := planTest(t, inspectTest(t, sourceDSN), b)
		p.ApplySafety(true)
		if !p.Incomplete || len(p.SchemaOperations) != 1 {
			t.Fatalf("security DDL not isolated: %+v", p)
		}
		applyTest(t, target, p)
		d := diff.NewDataDiffer(NewMSSQLDataReader(sourceDSN), NewMSSQLDataReader(targetDSN))
		if ops, err := d.CompareData(context.Background(), a, b); err != nil || len(ops) != 0 || len(d.OmittedTables) != 1 {
			t.Fatalf("security data not skipped: %v %v %+v", err, ops, d)
		}
	})

	t.Run("blocked child prevents cascading data changes", func(t *testing.T) {
		sourceDSN, source := testDatabase(t, server, "source")
		targetDSN, target := testDatabase(t, server, "target")
		ddl := `CREATE TABLE dbo.Parent(Id int NOT NULL PRIMARY KEY);
CREATE TABLE dbo.Child(Id int NOT NULL PRIMARY KEY,ParentId int NOT NULL REFERENCES dbo.Parent(Id) ON DELETE CASCADE,Secret int MASKED WITH(FUNCTION='default()') NULL);
CREATE TABLE dbo.Independent(Id int NOT NULL PRIMARY KEY);`
		execFixture(t, source, ddl+`INSERT INTO dbo.Independent VALUES(3);`)
		execFixture(t, target, ddl+`INSERT INTO dbo.Parent VALUES(1); INSERT INTO dbo.Child VALUES(2,1,42);`)
		a, b := inspectTest(t, sourceDSN), inspectTest(t, targetDSN)
		p := planTest(t, a, b)
		d := diff.NewDataDiffer(NewMSSQLDataReader(sourceDSN), NewMSSQLDataReader(targetDSN))
		ops, err := d.CompareData(context.Background(), a, b)
		if err != nil || !d.Incomplete || len(ops) != 1 || len(d.OmittedTables) != 2 {
			t.Fatalf("cascade not isolated: %v %v %+v", err, ops, d)
		}
		p.DataOperations = ops
		applyTest(t, target, p)
		var count int
		if err := target.QueryRow(`SELECT COUNT(*) FROM dbo.Child WHERE Id=2 AND Secret=42`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("blocked child was changed through cascade: %d %v", count, err)
		}
		if err := target.QueryRow(`SELECT COUNT(*) FROM dbo.Independent WHERE Id=3`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("independent data was lost: %d %v", count, err)
		}
	})

}
