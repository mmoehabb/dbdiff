package diff

import (
	"testing"
)

func TestMigrationPlan_DestructiveOperations(t *testing.T) {
	plan := &MigrationPlan{
		SchemaOperations: []Operation{
			CreateTableOperation{},
			DropTableOperation{},
			AddColumnOperation{},
			DropColumnOperation{},
		},
	}

	if !plan.HasDestructiveOperations() {
		t.Error("Expected plan to have destructive operations")
	}

	destructiveOps := plan.GetDestructiveOperations()
	if len(destructiveOps) != 2 {
		t.Errorf("Expected 2 destructive operations, got %d", len(destructiveOps))
	}

	if destructiveOps[0].OperationType() != DropTable {
		t.Errorf("Expected first destructive op to be DropTable, got %v", destructiveOps[0].OperationType())
	}
	if destructiveOps[1].OperationType() != DropColumn {
		t.Errorf("Expected second destructive op to be DropColumn, got %v", destructiveOps[1].OperationType())
	}
}

func TestMigrationPlan_NoDestructiveOperations(t *testing.T) {
	plan := &MigrationPlan{
		SchemaOperations: []Operation{
			CreateTableOperation{},
			AddColumnOperation{},
		},
	}

	if plan.HasDestructiveOperations() {
		t.Error("Expected plan to not have destructive operations")
	}

	if len(plan.GetDestructiveOperations()) != 0 {
		t.Error("Expected empty slice for destructive operations")
	}
}

func TestMigrationPlan_SortOperations(t *testing.T) {
	plan := &MigrationPlan{
		SchemaOperations: []Operation{
			AddForeignKeyOperation{},
			CreateTableOperation{},
			DropTableOperation{},
			DropForeignKeyOperation{},
			CreateSchemaOperation{},
			DropSchemaOperation{},
			CreateIndexOperation{},
			DropIndexOperation{},
			AddPrimaryKeyOperation{},
			DropPrimaryKeyOperation{},
			AddColumnOperation{},
			DropColumnOperation{},
			AlterColumnOperation{},
		},
	}

	plan.SortOperations()

	expectedOrder := []OperationType{
		DropForeignKey,
		DropIndex,
		DropPrimaryKey,
		DropColumn,
		DropTable,
		DropSchema,
		CreateSchema,
		CreateTable,
		AddColumn,
		AlterColumn,
		AddPrimaryKey,
		CreateIndex,
		AddForeignKey,
	}

	if len(plan.SchemaOperations) != len(expectedOrder) {
		t.Fatalf("Expected %d operations, got %d", len(expectedOrder), len(plan.SchemaOperations))
	}

	for i, op := range plan.SchemaOperations {
		if op.OperationType() != expectedOrder[i] {
			t.Errorf("At index %d: expected %v, got %v", i, expectedOrder[i], op.OperationType())
		}
	}
}
