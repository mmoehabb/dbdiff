package json

import (
	"context"
	"testing"

	"dbdiff/internal/domain/diff"
)

func TestJSONRenderer(t *testing.T) {
	plan := &diff.MigrationPlan{
		SchemaOperations: []diff.Operation{
			diff.CreateSchemaOperation{SchemaName: "dbo"},
			diff.DropTableOperation{SchemaName: "dbo", TableName: "users"},
		},
	}

	r := NewJSONRenderer()
	output, err := r.Render(context.Background(), plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output == "" {
		t.Errorf("expected output, got empty string")
	}
}
