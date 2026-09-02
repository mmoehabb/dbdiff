package ports

import (
	"context"
	"github.com/mmoehabb/dbdiff/internal/domain/diff"
)

// Renderer defines the port for rendering a migration plan into target-specific SQL.
type Renderer interface {
	Render(ctx context.Context, plan *diff.MigrationPlan) (string, error)
}
