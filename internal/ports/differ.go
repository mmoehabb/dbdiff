package ports

import (
	"github.com/mmoehabb/dbdiff/internal/domain/diff"
	"github.com/mmoehabb/dbdiff/internal/domain/schema"
)

// Differ defines the port for comparing two databases and producing a migration plan.
type Differ interface {
	Compare(source, target *schema.Database) *diff.MigrationPlan
}
