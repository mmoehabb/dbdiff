package mssql

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/microsoft/go-mssqldb"

	"github.com/mmoehabb/dbdiff/internal/domain/schema"
)

// MSSQLIntrospector implements the ports.Introspector interface for SQL Server.
type MSSQLIntrospector struct {
	ConnectionString string
}

func NewMSSQLIntrospector(conn string) *MSSQLIntrospector {
	return &MSSQLIntrospector{
		ConnectionString: conn,
	}
}

func (i *MSSQLIntrospector) Inspect(ctx context.Context) (*schema.Database, error) {
	db, err := sql.Open("sqlserver", i.ConnectionString)
	if err != nil {
		return nil, fmt.Errorf("failed to open connection: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// TODO: implement actual SQL Server catalog querying.
	// Returning a dummy schema for now.
	return &schema.Database{
		Name:    "dummy",
		Schemas: make(map[string]*schema.Schema),
	}, nil
}
