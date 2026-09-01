package diff

// SchemaDiff represents the diff between two schemas conceptually.
type SchemaDiff struct {
	Operations []Operation
}

// MigrationPlan represents the ordered operations to migrate the target schema to the source schema.
type MigrationPlan struct {
	SchemaOperations []Operation
	// DataOperations could be added here in the future
}
