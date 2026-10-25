package diff

import (
	"sort"
)

// SchemaDiff represents the diff between two schemas conceptually.
type SchemaDiff struct {
	Operations []Operation
}

// MigrationPlan represents the ordered operations to migrate the target schema to the source schema.
type MigrationPlan struct {
	SchemaOperations []Operation
	DataOperations   []Operation
	Metadata         map[string]StepMetadata
	Omitted          []Diagnostic
	BlockedTables    map[string]bool
}

// HasDestructiveOperations returns true if the plan contains any destructive operations.
func (p *MigrationPlan) HasDestructiveOperations() bool {
	for _, op := range p.SchemaOperations {
		if op.IsDestructive() {
			return true
		}
	}
	for _, op := range p.DataOperations {
		if op.IsDestructive() {
			return true
		}
	}
	return false
}

// GetDestructiveOperations returns a slice of all destructive operations in the plan.
func (p *MigrationPlan) GetDestructiveOperations() []Operation {
	var destructive []Operation
	for _, op := range p.SchemaOperations {
		if op.IsDestructive() {
			destructive = append(destructive, op)
		}
	}
	for _, op := range p.DataOperations {
		if op.IsDestructive() {
			destructive = append(destructive, op)
		}
	}
	return destructive
}

// SortOperations sorts the SchemaOperations to ensure dependency ordering.
func (p *MigrationPlan) SortOperations() {
	sort.SliceStable(p.SchemaOperations, func(i, j int) bool { return lessOperation(p.SchemaOperations[i], p.SchemaOperations[j]) })
}
