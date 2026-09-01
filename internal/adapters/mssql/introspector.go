package mssql

import (
	"context"
	"dbdiff/internal/domain/schema"
)

// MSSQLIntrospector implements the ports.Introspector interface for SQL Server.
type MSSQLIntrospector struct {
	// db *sql.DB // To be added later
	ConnectionString string
}

func NewMSSQLIntrospector(conn string) *MSSQLIntrospector {
	return &MSSQLIntrospector{
		ConnectionString: conn,
	}
}

func (i *MSSQLIntrospector) Inspect(ctx context.Context) (*schema.Database, error) {
	// TODO: implement actual SQL Server catalog querying.
	// Returning a dummy schema for now.
	return &schema.Database{
		Name:    "dummy",
		Schemas: make(map[string]*schema.Schema),
	}, nil
}
