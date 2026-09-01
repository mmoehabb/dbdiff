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
	// DataOperations could be added here in the future
}

// HasDestructiveOperations returns true if the plan contains any destructive operations.
func (p *MigrationPlan) HasDestructiveOperations() bool {
	for _, op := range p.SchemaOperations {
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
	return destructive
}

// SortOperations sorts the SchemaOperations to ensure dependency ordering.
func (p *MigrationPlan) SortOperations() {
	priority := map[OperationType]int{
		DropForeignKey: 1,
		DropIndex:      2,
		DropPrimaryKey: 3,
		DropColumn:     4,
		DropTable:      5,
		DropSchema:     6,
		CreateSchema:   7,
		CreateTable:    8,
		AddColumn:      9,
		AlterColumn:    10,
		AddPrimaryKey:  11,
		CreateIndex:    12,
		AddForeignKey:  13,
	}

	sort.SliceStable(p.SchemaOperations, func(i, j int) bool {
		opI := p.SchemaOperations[i].OperationType()
		opJ := p.SchemaOperations[j].OperationType()
		return priority[opI] < priority[opJ]
	})
}
