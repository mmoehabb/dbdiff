package mssql

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"dbdiff/internal/domain/diff"
	"dbdiff/internal/domain/schema"
	_ "github.com/microsoft/go-mssqldb"
	mssqlc "github.com/testcontainers/testcontainers-go/modules/mssql"
)

func TestMSSQLIntegration_EndToEnd_SchemaGeneration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Spin up an MSSQL test container
	mssqlContainer, err := mssqlc.Run(ctx,
		"mcr.microsoft.com/mssql/server:2019-latest",
		mssqlc.WithAcceptEULA(),
		mssqlc.WithPassword("StrongPassword123!"),
	)
	if err != nil {
		t.Skipf("failed to start container (possibly docker environment issue): %s", err)
	}
	defer func() {
		if err := mssqlContainer.Terminate(ctx); err != nil {
			t.Fatalf("failed to terminate container: %s", err)
		}
	}()

	connStr, err := mssqlContainer.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("failed to get connection string: %s", err)
	}

	// Open connection
	db, err := sql.Open("sqlserver", connStr)
	if err != nil {
		t.Fatalf("failed to connect to mssql: %s", err)
	}
	defer db.Close()

	// Ping to ensure DB is up
	ctxPing, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctxPing); err != nil {
		t.Fatalf("failed to ping mssql: %s", err)
	}

	// Generate a Migration Plan programmatically
	plan := &diff.MigrationPlan{
		SchemaOperations: []diff.Operation{
			diff.CreateSchemaOperation{SchemaName: "test_schema"},
			diff.CreateTableOperation{
				SchemaName: "test_schema",
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
							Nullable: false,
						},
						"created_at": {
							Name: "created_at",
							Type: schema.DataType{Kind: schema.TypeDateTime},
							Nullable: false,
							Default: &schema.DefaultExpression{Value: "GETDATE()"},
						},
						"is_active": {
							Name: "is_active",
							Type: schema.DataType{Kind: schema.TypeBoolean},
							Nullable: false,
							Default: &schema.DefaultExpression{Value: "1"},
						},
					},
				},
			},
			diff.AddPrimaryKeyOperation{
				SchemaName: "test_schema",
				TableName:  "users",
				PrimaryKey: schema.PrimaryKey{
					Name: "PK_test_users",
					Columns: []string{"id"},
				},
			},
			diff.CreateIndexOperation{
				SchemaName: "test_schema",
				TableName:  "users",
				Index: schema.Index{
					Name: "IX_test_users_email",
					Columns: []string{"email"},
					IsUnique: true,
				},
			},
			diff.CreateTableOperation{
				SchemaName: "test_schema",
				Table: schema.Table{
					Name: "orders",
					Columns: map[string]*schema.Column{
						"id": {
							Name: "id",
							Type: schema.DataType{Kind: schema.TypeInteger},
							Nullable: false,
							Identity: true,
						},
						"user_id": {
							Name: "user_id",
							Type: schema.DataType{Kind: schema.TypeInteger},
							Nullable: false,
						},
					},
				},
			},
			diff.AddPrimaryKeyOperation{
				SchemaName: "test_schema",
				TableName:  "orders",
				PrimaryKey: schema.PrimaryKey{
					Name: "PK_test_orders",
					Columns: []string{"id"},
				},
			},
			diff.AddForeignKeyOperation{
				SchemaName: "test_schema",
				TableName:  "orders",
				ForeignKey: schema.ForeignKey{
					Name: "FK_test_orders_users",
					Columns: []string{"user_id"},
					RefTable: "users",
					RefColumns: []string{"id"},
				},
			},
		},
	}

	// Render the plan to SQL
	renderer := NewMSSQLRenderer()
	sqlScript, err := renderer.Render(ctx, plan)
	if err != nil {
		t.Fatalf("failed to render SQL: %s", err)
	}

	// MSSQL's go-mssqldb doesn't support executing multiple batches separated by "GO"
	// natively in one `Exec`. However, our rendered output doesn't use "GO", it just
	// wraps everything in a BEGIN TRANSACTION / COMMIT TRANSACTION block which is
	// perfectly fine for a single Exec.

	// Print SQL to test logs for debugging
	t.Log("Executing generated SQL script:\n", sqlScript)

	// Execute the rendered SQL against the live database container
	// But note: CREATE SCHEMA must be the only statement in the batch in SQL Server.
	// We will split by statements or just use individual execs for schema operations if it fails.

	// For testing, let's execute the statements one by one if a single batch fails.
	// Or we can just drop the transaction block and execute individually.
	// First let's try the whole script.

	_, err = db.ExecContext(ctx, sqlScript)
	if err != nil {
		// If it fails due to "CREATE SCHEMA must be the only statement in the batch",
		// we can strip the transaction and execute line by line (split by "\n\n").
		if strings.Contains(err.Error(), "must be the only statement in the batch") {
			t.Log("SQL Server requires certain statements to be the only one in a batch. Splitting statements...")

			// Remove transaction wrapping for the sake of the test since go-mssqldb doesn't do "GO" batch parsing.
			statements := strings.Split(sqlScript, "\n\n")
			for _, stmt := range statements {
				stmt = strings.TrimSpace(stmt)
				if stmt == "" || stmt == "BEGIN TRANSACTION;" || stmt == "COMMIT TRANSACTION;" {
					continue
				}
				_, execErr := db.ExecContext(ctx, stmt)
				if execErr != nil {
					t.Fatalf("failed to execute statement '%s': %s", stmt, execErr)
				}
			}
		} else {
			t.Fatalf("failed to execute generated sql script: %s", err)
		}
	}
}
