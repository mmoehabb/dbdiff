package application

import (
	"context"
	"github.com/mmoehabb/dbdiff/internal/domain/diff"
	"github.com/mmoehabb/dbdiff/internal/domain/schema"
	"github.com/mmoehabb/dbdiff/internal/ports"
)

// ComparisonResult wraps the output of the compare operation.
type ComparisonResult struct {
	Plan     *diff.MigrationPlan
	Warnings []string
}

// Compare takes a source and a target schema and a differ, and returns a MigrationPlan.
func Compare(
	ctx context.Context,
	sourceSchema *schema.Database,
	targetSchema *schema.Database,
	differ ports.Differ,
) (*ComparisonResult, error) {

	migrationPlan := differ.Compare(sourceSchema, targetSchema)

	return &ComparisonResult{
		Plan: migrationPlan,
	}, nil
}
