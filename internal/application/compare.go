package application

import (
	"context"
	"dbdiff/internal/domain/diff"
	"dbdiff/internal/ports"
)

// ComparisonResult wraps the output of the compare operation.
type ComparisonResult struct {
	Plan     *diff.MigrationPlan
	Warnings []string
}

// Compare takes a source and a target introspector and a differ, and returns a MigrationPlan.
// It will inspect both databases, generate the diff, and produce a migration plan.
func Compare(
	ctx context.Context,
	source ports.Introspector,
	target ports.Introspector,
	differ ports.Differ,
) (*ComparisonResult, error) {

	sourceSchema, err := source.Inspect(ctx)
	if err != nil {
		return nil, err
	}

	targetSchema, err := target.Inspect(ctx)
	if err != nil {
		return nil, err
	}

	migrationPlan := differ.Compare(sourceSchema, targetSchema)

	return &ComparisonResult{
		Plan: migrationPlan,
	}, nil
}
