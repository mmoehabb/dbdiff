package diff

import "github.com/mmoehabb/dbdiff/internal/domain/schema"

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
	IsDestructive() bool
}

type CreateTableOperation struct {
	SchemaName string
	Table      schema.Table
}

func (o CreateTableOperation) OperationType() OperationType { return CreateTable }
func (o CreateTableOperation) IsDestructive() bool          { return false }

type DropTableOperation struct {
	SchemaName string
	TableName  string
}

func (o DropTableOperation) OperationType() OperationType { return DropTable }
func (o DropTableOperation) IsDestructive() bool          { return true }

type AddColumnOperation struct {
	SchemaName string
	TableName  string
	Column     schema.Column
}

func (o AddColumnOperation) OperationType() OperationType { return AddColumn }
func (o AddColumnOperation) IsDestructive() bool          { return false }

type DropColumnOperation struct {
	SchemaName string
	TableName  string
	ColumnName string
}

func (o DropColumnOperation) OperationType() OperationType { return DropColumn }
func (o DropColumnOperation) IsDestructive() bool          { return true }

type CreateSchemaOperation struct {
	SchemaName string
}

func (o CreateSchemaOperation) OperationType() OperationType { return CreateSchema }
func (o CreateSchemaOperation) IsDestructive() bool          { return false }

type DropSchemaOperation struct {
	SchemaName string
}

func (o DropSchemaOperation) OperationType() OperationType { return DropSchema }
func (o DropSchemaOperation) IsDestructive() bool          { return true }

type AlterColumnOperation struct {
	SchemaName string
	TableName  string
	Column     schema.Column
}

func (o AlterColumnOperation) OperationType() OperationType { return AlterColumn }
func (o AlterColumnOperation) IsDestructive() bool          { return false }

type AddPrimaryKeyOperation struct {
	SchemaName string
	TableName  string
	PrimaryKey schema.PrimaryKey
}

func (o AddPrimaryKeyOperation) OperationType() OperationType { return AddPrimaryKey }
func (o AddPrimaryKeyOperation) IsDestructive() bool          { return false }

type DropPrimaryKeyOperation struct {
	SchemaName string
	TableName  string
}

func (o DropPrimaryKeyOperation) OperationType() OperationType { return DropPrimaryKey }
func (o DropPrimaryKeyOperation) IsDestructive() bool          { return true }

type AddForeignKeyOperation struct {
	SchemaName string
	TableName  string
	ForeignKey schema.ForeignKey
}

func (o AddForeignKeyOperation) OperationType() OperationType { return AddForeignKey }
func (o AddForeignKeyOperation) IsDestructive() bool          { return false }

type DropForeignKeyOperation struct {
	SchemaName     string
	TableName      string
	ForeignKeyName string
}

func (o DropForeignKeyOperation) OperationType() OperationType { return DropForeignKey }
func (o DropForeignKeyOperation) IsDestructive() bool          { return true }

type CreateIndexOperation struct {
	SchemaName string
	TableName  string
	Index      schema.Index
}

func (o CreateIndexOperation) OperationType() OperationType { return CreateIndex }
func (o CreateIndexOperation) IsDestructive() bool          { return false }

type DropIndexOperation struct {
	SchemaName string
	TableName  string
	IndexName  string
}

func (o DropIndexOperation) OperationType() OperationType { return DropIndex }
func (o DropIndexOperation) IsDestructive() bool          { return true }
