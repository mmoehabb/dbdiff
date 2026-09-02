package application_test

import (
	"context"
	"testing"

	"github.com/mmoehabb/dbdiff/internal/application"
	"github.com/mmoehabb/dbdiff/internal/domain/diff"
	"github.com/mmoehabb/dbdiff/internal/domain/schema"
)

// MockIntrospector is a simple mock for testing
type MockIntrospector struct {
	db *schema.Database
}

func (m *MockIntrospector) Inspect(ctx context.Context) (*schema.Database, error) {
	return m.db, nil
}

// MockDiffer is a simple mock for testing
type MockDiffer struct {
	plan *diff.MigrationPlan
}

func (m *MockDiffer) Compare(source, target *schema.Database) *diff.MigrationPlan {
	return m.plan
}

func TestCompare(t *testing.T) {
	sourceDB := &schema.Database{Name: "source"}
	targetDB := &schema.Database{Name: "target"}
	expectedPlan := &diff.MigrationPlan{}

	sourceIntrospector := &MockIntrospector{db: sourceDB}
	targetIntrospector := &MockIntrospector{db: targetDB}
	differ := &MockDiffer{plan: expectedPlan}

	result, err := application.Compare(context.Background(), sourceIntrospector, targetIntrospector, differ)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result.Plan != expectedPlan {
		t.Errorf("expected plan %v, got %v", expectedPlan, result.Plan)
	}
}
