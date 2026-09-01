package ports

import (
	"context"
	"dbdiff/internal/domain/schema"
)

// Introspector defines the port for reading a schema from a database.
type Introspector interface {
	Inspect(ctx context.Context) (*schema.Database, error)
}
