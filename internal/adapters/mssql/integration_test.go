package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/mmoehabb/dbdiff/internal/domain/diff"
	"github.com/mmoehabb/dbdiff/internal/domain/schema"
	mssqlc "github.com/testcontainers/testcontainers-go/modules/mssql"
)

func integrationServer(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("SQL Server integration tests disabled by -short")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	checkCtx, checkCancel := context.WithTimeout(ctx, 15*time.Second)
	defer checkCancel()
	if err := exec.CommandContext(checkCtx, "docker", "info").Run(); err != nil {
		if os.Getenv("DBDIFF_REQUIRE_MSSQL") == "1" {
			t.Fatalf("required SQL Server integration environment unavailable: %v", err)
		}
		t.Skipf("SQL Server integration requires Docker: %v", err)
	}
	container, err := mssqlc.Run(ctx, "mcr.microsoft.com/mssql/server:2019-latest", mssqlc.WithAcceptEULA(), mssqlc.WithPassword("StrongPassword123!"))
	if err != nil {
		t.Fatalf("start SQL Server: %v", err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := container.Terminate(cleanup); err != nil {
			t.Errorf("stop SQL Server: %v", err)
		}
	})
	dsn, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return dsn
}
func testDatabase(t *testing.T, server, label string) (string, *sql.DB) {
	t.Helper()
	master, err := sql.Open("sqlserver", server)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("dbdiff_%s_%d", label, time.Now().UnixNano())
	if _, err := master.Exec("CREATE DATABASE " + quote(name)); err != nil {
		master.Close()
		t.Fatal(err)
	}
	u, err := url.Parse(server)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("database", name)
	u.RawQuery = q.Encode()
	dsn := u.String()
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		_, err := master.Exec("ALTER DATABASE " + quote(name) + " SET SINGLE_USER WITH ROLLBACK IMMEDIATE; DROP DATABASE " + quote(name))
		if err != nil {
			t.Errorf("drop test database: %v", err)
		}
		master.Close()
	})
	return dsn, db
}
func execFixture(t *testing.T, db *sql.DB, sql string) {
	t.Helper()
	if _, err := db.Exec(sql); err != nil {
		t.Fatalf("fixture: %v", err)
	}
}
func inspectTest(t *testing.T, dsn string) *schema.Database {
	t.Helper()
	d, err := NewMSSQLIntrospector(dsn).Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func planTest(t *testing.T, a, b *schema.Database) *diff.MigrationPlan {
	t.Helper()
	p, err := diff.NewSchemaDiffer().Compare(a, b)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func applyTest(t *testing.T, db *sql.DB, p *diff.MigrationPlan) {
	t.Helper()
	script, err := NewMSSQLRenderer().Render(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if script != "" {
		if _, err := db.Exec(script); err != nil {
			t.Fatalf("execute exact generated script: %v\n%s", err, script)
		}
	}
}

func TestMSSQLIntegration_RoundTrip(t *testing.T) {
	server := integrationServer(t)
	t.Run("new schema and data", func(t *testing.T) {
		sourceDSN, source := testDatabase(t, server, "source")
		targetDSN, target := testDatabase(t, server, "target")
		execFixture(t, source, `CREATE TABLE dbo.Articles(Id bigint NOT NULL CONSTRAINT PK_articles_actual PRIMARY KEY CLUSTERED);
CREATE TABLE dbo.ArticleViews(Id bigint IDENTITY(10,2) NOT NULL CONSTRAINT PK_views_actual PRIMARY KEY CLUSTERED, ArticleId bigint NOT NULL, Payload nvarchar(max) NULL, BinaryData varbinary(max) NULL, RequestId uniqueidentifier NULL, ViewedAt datetimeoffset(7) NULL, ViewedTime time(7) NULL, Enabled bit NOT NULL CONSTRAINT DF_enabled DEFAULT ((1)), Token varchar(50) NULL, CONSTRAINT CK_article CHECK (ArticleId>0), CONSTRAINT UQ_token UNIQUE NONCLUSTERED(Token), CONSTRAINT FK_article FOREIGN KEY(ArticleId) REFERENCES dbo.Articles(Id) ON DELETE CASCADE);
CREATE UNIQUE NONCLUSTERED INDEX IX_ArticleViews_ArticleId ON dbo.ArticleViews(ArticleId DESC) INCLUDE(Payload) WHERE ArticleId>0;
CREATE SEQUENCE dbo.seq AS bigint START WITH 3 INCREMENT BY 2 MINVALUE 1 MAXVALUE 999 CYCLE CACHE 5;
EXEC(N'CREATE VIEW dbo.v AS SELECT ArticleId FROM dbo.ArticleViews');
EXEC sys.sp_addextendedproperty @name=N'Description',@value=N'Article''s views',@level0type=N'SCHEMA',@level0name=N'dbo',@level1type=N'TABLE',@level1name=N'ArticleViews';
DECLARE @weight decimal(12,4)=123.4567; EXEC sys.sp_addextendedproperty @name=N'Weight',@value=@weight,@level0type=N'SCHEMA',@level0name=N'dbo',@level1type=N'TABLE',@level1name=N'ArticleViews',@level2type=N'COLUMN',@level2name=N'ArticleId';
INSERT INTO dbo.Articles VALUES(1);
INSERT INTO dbo.ArticleViews(ArticleId,Payload,BinaryData,RequestId,ViewedAt,ViewedTime,Token) VALUES(1,N'text',0x00FF27,NEWID(),'2026-10-24T13:14:15.1234567+03:00','13:14:15.1234567','token');`)
		a, b := inspectTest(t, sourceDSN), inspectTest(t, targetDSN)
		p := planTest(t, a, b)
		ops, err := diff.NewDataDiffer(NewMSSQLDataReader(sourceDSN), NewMSSQLDataReader(targetDSN)).CompareData(context.Background(), a, b)
		if err != nil {
			t.Fatal(err)
		}
		p.DataOperations = ops
		p.ApplySafety(true)
		applyTest(t, target, p)
		after := inspectTest(t, targetDSN)
		remaining := planTest(t, a, after)
		if len(remaining.SchemaOperations) != 0 {
			t.Fatalf("round trip left schema operations: %+v", remaining.SchemaOperations)
		}
		data, err := diff.NewDataDiffer(NewMSSQLDataReader(sourceDSN), NewMSSQLDataReader(targetDSN)).CompareData(context.Background(), a, after)
		if err != nil || len(data) != 0 {
			t.Fatalf("round trip left data changes: %+v %v", data, err)
		}
		var untrusted int
		if err := target.QueryRow("SELECT COUNT(*) FROM sys.foreign_keys WHERE is_disabled=1 OR is_not_trusted=1").Scan(&untrusted); err != nil || untrusted != 0 {
			t.Fatalf("constraint state changed: %d %v", untrusted, err)
		}
	})
	t.Run("same name index replacement", func(t *testing.T) {
		sourceDSN, source := testDatabase(t, server, "source")
		targetDSN, target := testDatabase(t, server, "target")
		ddl := `CREATE TABLE dbo.ArticleViews(ArticleId bigint NOT NULL,Payload nvarchar(max) NULL);`
		execFixture(t, source, ddl+`CREATE UNIQUE INDEX IX_ArticleViews_ArticleId ON dbo.ArticleViews(ArticleId DESC) INCLUDE(Payload) WHERE ArticleId>0;`)
		execFixture(t, target, ddl+`CREATE INDEX IX_ArticleViews_ArticleId ON dbo.ArticleViews(ArticleId);`)
		a, b := inspectTest(t, sourceDSN), inspectTest(t, targetDSN)
		safe := planTest(t, a, b)
		safe.ApplySafety(false)
		if len(safe.SchemaOperations) != 0 || len(safe.Omitted) != 2 {
			t.Fatalf("partial replacement: %+v", safe)
		}
		applyTest(t, target, safe)
		p := planTest(t, a, b)
		p.ApplySafety(true)
		applyTest(t, target, p)
		if next := planTest(t, a, inspectTest(t, targetDSN)); len(next.SchemaOperations) != 0 {
			t.Fatalf("index replacement did not converge: %+v", next.SchemaOperations)
		}
	})
	t.Run("rollback on failure", func(t *testing.T) {
		_, db := testDatabase(t, server, "rollback")
		p := &diff.MigrationPlan{SchemaOperations: []diff.Operation{diff.CreateTableOperation{SchemaName: "dbo", Table: schema.Table{Name: "WillRollback", Columns: map[string]*schema.Column{"Id": {Name: "Id", Type: schema.DataType{Kind: schema.TypeInteger}}}}}, diff.CreateIndexOperation{SchemaName: "dbo", TableName: "Missing", Index: schema.Index{Name: "fail", Columns: []string{"Id"}}}}}
		sql, err := NewMSSQLRenderer().Render(context.Background(), p)
		if err != nil {
			t.Fatal(err)
		}
		conn, err := db.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if _, err := conn.ExecContext(context.Background(), sql); err == nil {
			t.Fatal("injected failure succeeded")
		}
		var count, transactions int
		if err := conn.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM sys.tables WHERE name='WillRollback'").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if err := conn.QueryRowContext(context.Background(), "SELECT @@TRANCOUNT").Scan(&transactions); err != nil {
			t.Fatal(err)
		}
		if count != 0 || transactions != 0 {
			t.Fatalf("partial migration remained: tables=%d transactions=%d", count, transactions)
		}
	})
	t.Run("standalone statistics collision", func(t *testing.T) {
		sourceDSN, source := testDatabase(t, server, "source")
		targetDSN, target := testDatabase(t, server, "target")
		ddl := `CREATE TABLE dbo.ArticleViews(ArticleId int NOT NULL);`
		execFixture(t, source, ddl+`CREATE INDEX IX_ArticleViews_ArticleId ON dbo.ArticleViews(ArticleId);`)
		execFixture(t, target, ddl+`CREATE STATISTICS IX_ArticleViews_ArticleId ON dbo.ArticleViews(ArticleId);`)
		_, err := diff.NewSchemaDiffer().Compare(inspectTest(t, sourceDSN), inspectTest(t, targetDSN))
		if err == nil || !strings.Contains(err.Error(), "statistics") {
			t.Fatalf("collision not diagnosed: %v", err)
		}
	})
}
