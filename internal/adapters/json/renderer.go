package json

import (
	"context"
	"encoding/json"

	"dbdiff/internal/domain/diff"
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
	wrappedOps := make([]operationWrapper, 0, len(plan.SchemaOperations))
	for _, op := range plan.SchemaOperations {
		wrappedOps = append(wrappedOps, operationWrapper{
			Type:          string(op.OperationType()),
			IsDestructive: op.IsDestructive(),
			Data:          op,
		})
	}

	output := map[string]interface{}{
		"operations": wrappedOps,
	}

	bytes, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return "", err
	}

	return string(bytes), nil
}
