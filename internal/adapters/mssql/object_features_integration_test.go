package mssql

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"

	"github.com/mmoehabb/dbdiff/internal/domain/diff"
	"github.com/mmoehabb/dbdiff/internal/domain/schema"
)

func TestMSSQLIntegration_ObjectFeatureNamespaces(t *testing.T) {
	server := integrationServer(t)
	t.Run("type-only and empty schemas", func(t *testing.T) {
		sourceDSN, source := testDatabase(t, server, "source")
		targetDSN, target := testDatabase(t, server, "target")
		ddl := `EXEC(N'CREATE SCHEMA catalog'); EXEC(N'CREATE SCHEMA empty_namespace'); CREATE TYPE catalog.Code FROM int NOT NULL;`
		execFixture(t, source, ddl)
		execFixture(t, target, ddl)
		execFixture(t, source, `CREATE TABLE catalog.Items(Id int NULL);`)
		a, b := inspectTest(t, sourceDSN), inspectTest(t, targetDSN)
		for _, name := range []string{"catalog", "empty_namespace"} {
			if b.Schemas[name] == nil {
				t.Fatalf("existing namespace %s was lost", name)
			}
		}
		if len(b.Schemas["catalog"].Tables) != 0 {
			t.Fatal("target namespace is not type-only")
		}
		p := planTest(t, a, b)
		p.ApplySafety(true)
		if p.Incomplete || len(p.SchemaOperations) != 1 || p.SchemaOperations[0].OperationType() != diff.CreateTable {
			t.Fatalf("type-only namespace was recreated: %+v", p)
		}
		applyTest(t, target, p)
		if p := planTest(t, a, inspectTest(t, targetDSN)); p.Incomplete || len(p.SchemaOperations) != 0 {
			t.Fatalf("namespace migration did not converge: %+v", p)
		}
	})
	t.Run("policy owns its namespace", func(t *testing.T) {
		sourceDSN, source := testDatabase(t, server, "source")
		targetDSN, target := testDatabase(t, server, "target")
		ddl := `EXEC(N'CREATE SCHEMA security'); CREATE TABLE security.Aux(Id int NULL); CREATE TABLE dbo.Items(Id int NOT NULL PRIMARY KEY); CREATE TABLE dbo.Independent(Id int NULL);
EXEC(N'CREATE FUNCTION dbo.Visible(@Id int) RETURNS TABLE WITH SCHEMABINDING AS RETURN SELECT 1 AS allowed WHERE @Id>0');
EXEC(N'CREATE SECURITY POLICY security.Policy ADD FILTER PREDICATE dbo.Visible(Id) ON dbo.Items WITH(STATE=ON)');`
		execFixture(t, source, ddl)
		execFixture(t, target, ddl)
		execFixture(t, source, `DROP SECURITY POLICY security.Policy; DROP TABLE security.Aux; DROP SCHEMA security; ALTER TABLE dbo.Independent ADD Extra int NULL;`)
		a, b := inspectTest(t, sourceDSN), inspectTest(t, targetDSN)
		found := false
		for _, f := range b.Features {
			found = found || (f.Schema == "security" && f.Object == "Policy" && f.Feature.Kind == "security policy")
		}
		if !found {
			t.Fatalf("policy's owning namespace not captured: %+v", b.Features)
		}
		p := planTest(t, a, b)
		p.ApplySafety(true)
		if !p.Incomplete || len(p.SchemaOperations) != 2 {
			t.Fatalf("namespace removal not isolated: %+v", p)
		}
		for _, op := range p.SchemaOperations {
			if op.OperationType() == diff.DropSchema {
				t.Fatal("DROP SCHEMA escaped isolation")
			}
		}
		blockedNamespace := false
		for _, d := range p.Omitted {
			blockedNamespace = blockedNamespace || (strings.Contains(d.ID, "drop_schema") && strings.Contains(d.Reason, "security policy"))
		}
		if !blockedNamespace {
			t.Fatalf("missing namespace diagnostic: %+v", p.Omitted)
		}
		applyTest(t, target, p)
		var count int
		if err := target.QueryRow(`SELECT COUNT(*) FROM sys.security_policies WHERE schema_id=SCHEMA_ID('security') AND name='Policy'`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("retained policy or namespace changed: %d %v", count, err)
		}
		if err := target.QueryRow(`SELECT COUNT(*) FROM sys.tables WHERE schema_id=SCHEMA_ID('security') AND name='Aux'`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("independent table drop not applied: %d %v", count, err)
		}
	})
}

// Exercise the production CLR projection with catalog-shaped fixture tables.
// SQL CLR need not be enabled, and no server-wide CLR/trust settings are changed.
func TestMSSQLIntegration_CLRDefinitionFingerprints(t *testing.T) {
	server := integrationServer(t)
	_, db := testDatabase(t, server, "clr_catalog")
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ddl := `EXEC(N'CREATE SCHEMA managed');
CREATE TABLE #clr_objects(object_id int,schema_id int,name sysname,type char(2),type_desc nvarchar(60),is_ms_shipped bit);
CREATE TABLE #clr_assembly_modules(object_id int,assembly_id int,assembly_class sysname,assembly_method sysname NULL,null_on_null_input bit,execute_as_principal_id int NULL);
CREATE TABLE #clr_assemblies(assembly_id int,name sysname,clr_name nvarchar(4000),permission_set_desc nvarchar(60),is_visible bit);
CREATE TABLE #clr_assembly_files(assembly_id int,file_id int,content varbinary(max));
CREATE TABLE #clr_parameters(object_id int,parameter_id int,name sysname,user_type_id int,max_length smallint,precision tinyint,scale tinyint,is_output bit,is_readonly bit,is_nullable bit,has_default_value bit,default_value sql_variant NULL);
CREATE TABLE #clr_columns(object_id int,column_id int,name sysname,user_type_id int,max_length smallint,precision tinyint,scale tinyint,is_nullable bit,collation_name sysname NULL);
INSERT INTO #clr_objects VALUES(1,SCHEMA_ID('managed'),'Transform','FS','CLR_SCALAR_FUNCTION',0),(2,SCHEMA_ID('managed'),'Run','PC','CLR_STORED_PROCEDURE',0),(3,SCHEMA_ID('managed'),'Rows','FT','CLR_TABLE_VALUED_FUNCTION',0),(4,SCHEMA_ID('managed'),'Aggregate','AF','AGGREGATE_FUNCTION',0);
INSERT INTO #clr_assembly_modules VALUES(1,1,'Helpers','Transform',1,NULL),(2,1,'Helpers','Run',0,NULL),(3,1,'Helpers','Rows',0,NULL),(4,1,'Accumulator',NULL,0,NULL);
INSERT INTO #clr_assemblies VALUES(1,'Fixture','Fixture, Version=1.0.0.0, Culture=neutral, PublicKeyToken=null','SAFE_ACCESS',1);
INSERT INTO #clr_assembly_files VALUES(1,1,0x01020304),(1,2,0x05060708);
INSERT INTO #clr_parameters VALUES(1,0,'',TYPE_ID('int'),4,10,0,0,0,1,0,NULL),(1,1,'@value',TYPE_ID('int'),4,10,0,0,0,1,1,0),(2,1,'@input',TYPE_ID('nvarchar'),40,0,0,0,0,1,0,NULL),(3,1,'@count',TYPE_ID('int'),4,10,0,0,0,1,0,NULL),(4,1,'@value',TYPE_ID('int'),4,10,0,0,0,1,0,NULL);
INSERT INTO #clr_columns VALUES(3,1,'Value',TYPE_ID('int'),4,10,0,1,NULL);`
	if _, err := conn.ExecContext(ctx, ddl); err != nil {
		t.Fatal(err)
	}
	query := strings.NewReplacer("sys.objects", "#clr_objects", "sys.assembly_modules", "#clr_assembly_modules", "sys.assemblies", "#clr_assemblies", "sys.assembly_files", "#clr_assembly_files", "sys.parameters", "#clr_parameters", "sys.columns", "#clr_columns").Replace(queryCLRObjectFeatures)
	read := func(t *testing.T, q interface {
		QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	}) map[string]schema.ObjectFeature {
		rows, err := q.QueryContext(ctx, query)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := map[string]schema.ObjectFeature{}
		for rows.Next() {
			var f schema.ObjectFeature
			if err := rows.Scan(&f.Schema, &f.Object, &f.Feature.Kind, &f.Feature.Scope, &f.Feature.Detail); err != nil {
				t.Fatal(err)
			}
			out[f.Object] = f
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if len(out) != 4 {
			t.Fatalf("missing CLR kinds: %+v", out)
		}
		return out
	}
	baseline := read(t, conn)
	// Object and assembly IDs are local catalog details. Assembly file positions
	// are part of the definition: file 1 is the manifest.
	if _, err := conn.ExecContext(ctx, `UPDATE #clr_objects SET object_id=object_id+100; UPDATE #clr_assembly_modules SET object_id=object_id+100,assembly_id=200; UPDATE #clr_parameters SET object_id=object_id+100; UPDATE #clr_columns SET object_id=object_id+100; UPDATE #clr_assemblies SET assembly_id=200; UPDATE #clr_assembly_files SET assembly_id=200;`); err != nil {
		t.Fatal(err)
	}
	if got := read(t, conn); !reflect.DeepEqual(got, baseline) {
		t.Fatal("database-local IDs changed the fingerprint")
	}
	for _, variant := range []struct{ name, object, sql string }{
		{"parameter type", "Transform", `UPDATE #clr_parameters SET user_type_id=TYPE_ID('bigint'),max_length=8,precision=19 WHERE object_id=101 AND parameter_id=1;`},
		{"return type", "Transform", `UPDATE #clr_parameters SET user_type_id=TYPE_ID('bigint'),max_length=8,precision=19 WHERE object_id=101 AND parameter_id=0;`},
		{"parameter default", "Transform", `UPDATE #clr_parameters SET default_value=1 WHERE object_id=101 AND parameter_id=1;`},
		{"procedure signature", "Run", `UPDATE #clr_parameters SET max_length=80,is_output=1 WHERE object_id=102;`},
		{"assembly class", "Transform", `UPDATE #clr_assembly_modules SET assembly_class='OtherHelpers' WHERE object_id=101;`},
		{"assembly method", "Transform", `UPDATE #clr_assembly_modules SET assembly_method='OtherTransform' WHERE object_id=101;`},
		{"assembly binding", "Transform", `INSERT INTO #clr_assemblies VALUES(201,'OtherFixture','OtherFixture, Version=1.0.0.0','SAFE_ACCESS',1); UPDATE #clr_assembly_modules SET assembly_id=201 WHERE object_id=101;`},
		{"assembly contents", "Transform", `UPDATE #clr_assembly_files SET content=0x05060708;`},
		{"manifest contents", "Transform", `UPDATE #clr_assembly_files SET content=CASE WHEN file_id=1 THEN 0x05060708 ELSE 0x01020304 END;`},
		{"null handling", "Transform", `UPDATE #clr_assembly_modules SET null_on_null_input=0 WHERE object_id=101;`},
		{"execution context", "Run", `UPDATE #clr_assembly_modules SET execute_as_principal_id=-2 WHERE object_id=102;`},
		{"result columns", "Rows", `UPDATE #clr_columns SET user_type_id=TYPE_ID('bigint'),max_length=8,precision=19 WHERE object_id=103;`},
		{"aggregate binding", "Aggregate", `UPDATE #clr_assembly_modules SET assembly_class='OtherAccumulator' WHERE object_id=104;`},
	} {
		t.Run(variant.name, func(t *testing.T) {
			tx, err := conn.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err := tx.ExecContext(ctx, variant.sql); err != nil {
				t.Fatal(err)
			}
			desired := read(t, tx)
			if desired[variant.object].Feature.Detail == baseline[variant.object].Feature.Detail {
				t.Fatal("CLR definition change was lost")
			}
			snapshot := func(features map[string]schema.ObjectFeature) *schema.Database {
				d := &schema.Database{Schemas: map[string]*schema.Schema{}}
				i := NewMSSQLIntrospector("")
				i.ensureSchema(d, "dbo")
				for _, name := range []string{"Transform", "Run", "Rows", "Aggregate"} {
					f := features[name]
					d.Features = append(d.Features, f)
					i.ensureSchema(d, f.Schema)
				}
				return d
			}
			a, b := snapshot(desired), snapshot(baseline)
			p := planTest(t, a, b)
			if !p.Incomplete || len(p.Omitted) < 2 || len(p.SchemaOperations) != 0 {
				t.Fatalf("CLR-only difference hidden: %+v", p)
			}
			a.Schemas["dbo"].Views["Consumer"] = &schema.View{Name: "Consumer", Definition: "SELECT 1 AS Value"}
			a.Dependencies = []schema.Dependency{{Schema: "dbo", Object: "Consumer", RefSchema: "managed", RefObject: variant.object}}
			p = planTest(t, a, b)
			if !p.Incomplete || len(p.SchemaOperations) != 0 {
				t.Fatalf("consumer of incompatible CLR signature allowed: %+v", p)
			}
			blocked := false
			for _, d := range p.Omitted {
				blocked = blocked || (strings.Contains(d.ID, "create_view") && strings.Contains(d.Reason, "CLR object"))
			}
			if !blocked {
				t.Fatalf("missing CLR consumer diagnostic: %+v", p.Omitted)
			}
		})
	}
}
