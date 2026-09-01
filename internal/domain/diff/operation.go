package diff

import "dbdiff/internal/domain/schema"

// OperationType categorizes operations
type OperationType string

const (
	CreateSchema OperationType = "create_schema"
	DropSchema   OperationType = "drop_schema"

	CreateTable OperationType = "create_table"
	DropTable   OperationType = "drop_table"
	RenameTable OperationType = "rename_table"

	AddColumn   OperationType = "add_column"
	DropColumn  OperationType = "drop_column"
	AlterColumn OperationType = "alter_column"

	AddPrimaryKey  OperationType = "add_primary_key"
	DropPrimaryKey OperationType = "drop_primary_key"

	AddForeignKey  OperationType = "add_foreign_key"
	DropForeignKey OperationType = "drop_foreign_key"

	CreateIndex OperationType = "create_index"
	DropIndex   OperationType = "drop_index"
)

// Operation represents a single migration step conceptually.
type Operation interface {
	OperationType() OperationType
}

type CreateTableOperation struct {
	SchemaName string
	Table      schema.Table
}

func (o CreateTableOperation) OperationType() OperationType { return CreateTable }

type DropTableOperation struct {
	SchemaName string
	TableName  string
}

func (o DropTableOperation) OperationType() OperationType { return DropTable }

type AddColumnOperation struct {
	SchemaName string
	TableName  string
	Column     schema.Column
}

func (o AddColumnOperation) OperationType() OperationType { return AddColumn }

type DropColumnOperation struct {
	SchemaName string
	TableName  string
	ColumnName string
}

func (o DropColumnOperation) OperationType() OperationType { return DropColumn }
