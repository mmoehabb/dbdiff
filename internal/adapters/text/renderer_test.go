package text

import (
	"context"
	"strings"
	"testing"

	"dbdiff/internal/domain/diff"
)

func TestTextRenderer(t *testing.T) {
	plan := &diff.MigrationPlan{
		SchemaOperations: []diff.Operation{
			diff.CreateSchemaOperation{SchemaName: "dbo"},
			diff.DropTableOperation{SchemaName: "dbo", TableName: "users"},
		},
	}

	r := NewTextRenderer()
	output, err := r.Render(context.Background(), plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(output, "+ CREATE SCHEMA dbo") {
		t.Errorf("expected output to contain '+ CREATE SCHEMA dbo', got %s", output)
	}
	if !strings.Contains(output, "- DROP TABLE dbo.users") {
		t.Errorf("expected output to contain '- DROP TABLE dbo.users', got %s", output)
	}
}
