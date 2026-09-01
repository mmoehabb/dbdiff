package mssql

import (
	"context"
	"dbdiff/internal/domain/diff"
)

// MSSQLRenderer implements the ports.Renderer interface for SQL Server.
type MSSQLRenderer struct {
}

func NewMSSQLRenderer() *MSSQLRenderer {
	return &MSSQLRenderer{}
}

func (r *MSSQLRenderer) Render(ctx context.Context, plan *diff.MigrationPlan) (string, error) {
	// TODO: implement actual SQL rendering for SQL Server.
	return "-- Migration script\n", nil
}
