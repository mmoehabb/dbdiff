package json

import (
	"context"
	"encoding/json"

	"github.com/mmoehabb/dbdiff/internal/domain/diff"
)

// JSONRenderer implements the ports.Renderer interface for JSON format.
type JSONRenderer struct{}

func NewJSONRenderer() *JSONRenderer {
	return &JSONRenderer{}
}

// Wrapper for JSON encoding since diff.Operation is an interface
type operationWrapper struct {
	Type          string      `json:"type"`
	IsDestructive bool        `json:"is_destructive"`
	Data          interface{} `json:"data"`
}

func (r *JSONRenderer) Render(ctx context.Context, plan *diff.MigrationPlan) (string, error) {
	wrappedSchemaOps := make([]operationWrapper, 0, len(plan.SchemaOperations))
	for _, op := range plan.SchemaOperations {
		wrappedSchemaOps = append(wrappedSchemaOps, operationWrapper{
			Type:          string(op.OperationType()),
			IsDestructive: op.IsDestructive(),
			Data:          op,
		})
	}

	wrappedDataOps := make([]operationWrapper, 0, len(plan.DataOperations))
	for _, op := range plan.DataOperations {
		wrappedDataOps = append(wrappedDataOps, operationWrapper{
			Type:          string(op.OperationType()),
			IsDestructive: op.IsDestructive(),
			Data:          op,
		})
	}

	output := map[string]interface{}{
		"schema_operations": wrappedSchemaOps,
		"data_operations":   wrappedDataOps,
	}

	bytes, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return "", err
	}

	return string(bytes), nil
}
