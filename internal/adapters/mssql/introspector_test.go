package mssql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"github.com/mmoehabb/dbdiff/internal/domain/schema"
	"io"
	"sync/atomic"
	"testing"
)

// A small database/sql driver exercises real Scan/Rows.Err behavior without SQL Server.
type metadataDriver struct {
	values   [][]driver.Value
	terminal error
}
type metadataConn struct{ d *metadataDriver }
type metadataRows struct {
	values   [][]driver.Value
	terminal error
	pos      int
}

func (d *metadataDriver) Open(string) (driver.Conn, error) { return &metadataConn{d}, nil }
func (c *metadataConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not implemented")
}
func (c *metadataConn) Close() error              { return nil }
func (c *metadataConn) Begin() (driver.Tx, error) { return nil, errors.New("not implemented") }
func (c *metadataConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &metadataRows{values: c.d.values, terminal: c.d.terminal}, nil
}
func (r *metadataRows) Columns() []string {
	names := make([]string, len(r.values[0]))
	for i := range names {
		names[i] = fmt.Sprint(i)
	}
	return names
}
func (r *metadataRows) Close() error { return nil }
func (r *metadataRows) Next(dest []driver.Value) error {
	if r.pos == len(r.values) {
		if r.terminal != nil {
			return r.terminal
		}
		return io.EOF
	}
	copy(dest, r.values[r.pos])
	r.pos++
	return nil
}

var driverNumber atomic.Int64

func metadataDB(t *testing.T, values [][]driver.Value, terminal error) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("dbdiff_metadata_%d", driverNumber.Add(1))
	sql.Register(name, &metadataDriver{values, terminal})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
func TestIndexIntrospectionSeparatesKeysAndIncludes(t *testing.T) {
	row := func(column string, ordinal int64, included, descending bool) []driver.Value {
		return []driver.Value{"dbo", "ArticleViews", "IX_ArticleViews_ArticleId", column, true, int64(2), ordinal, descending, included, "([ArticleId]>(0))", false, false, int64(80), false, false, true, true}
	}
	db := metadataDB(t, [][]driver.Value{row("Payload", 0, true, false), row("ArticleId", 1, false, true)}, nil)
	snapshot := &schema.Database{Schemas: map[string]*schema.Schema{}}
	if err := NewMSSQLIntrospector("").loadIndexes(context.Background(), db, snapshot); err != nil {
		t.Fatal(err)
	}
	index := snapshot.Schemas["dbo"].Tables["ArticleViews"].Indexes["IX_ArticleViews_ArticleId"]
	if len(index.Columns) != 1 || index.Columns[0] != "ArticleId" || len(index.IncludedColumns) != 1 || index.IncludedColumns[0] != "Payload" || !index.Descending[0] || index.Filter == "" || index.FillFactor != 80 {
		t.Fatalf("lost index metadata: %+v", index)
	}
}
func TestMetadataIterationFailurePropagates(t *testing.T) {
	failure := errors.New("connection interrupted")
	db := metadataDB(t, [][]driver.Value{{"dbo", "v", "CREATE VIEW dbo.v AS SELECT 1 AS x", true, true}}, failure)
	snapshot := &schema.Database{Schemas: map[string]*schema.Schema{}}
	err := NewMSSQLIntrospector("").loadViews(context.Background(), db, snapshot)
	if !errors.Is(err, failure) {
		t.Fatalf("partial snapshot accepted: %v", err)
	}
}

func TestFunctionIntrospectionPreservesKind(t *testing.T) {
	for _, kind := range []string{"FN", "IF", "TF"} {
		t.Run(kind, func(t *testing.T) {
			db := metadataDB(t, [][]driver.Value{{"dbo", "f", kind + " ", "CREATE FUNCTION dbo.f() RETURNS int AS BEGIN RETURN 1 END", true, true}}, nil)
			snapshot := &schema.Database{Schemas: map[string]*schema.Schema{}}
			if err := NewMSSQLIntrospector("").loadFunctions(context.Background(), db, snapshot); err != nil {
				t.Fatal(err)
			}
			if got := snapshot.Schemas["dbo"].Functions["f"].Kind; got != kind {
				t.Fatalf("function kind = %q, want %q", got, kind)
			}
		})
	}
}
