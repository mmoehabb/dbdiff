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

const (
	CreateView              OperationType = "create_view"
	DropView                OperationType = "drop_view"
	AlterView               OperationType = "alter_view"
	CreateProcedure         OperationType = "create_procedure"
	DropProcedure           OperationType = "drop_procedure"
	AlterProcedure          OperationType = "alter_procedure"
	CreateFunction          OperationType = "create_function"
	DropFunction            OperationType = "drop_function"
	AlterFunction           OperationType = "alter_function"
	CreateTrigger           OperationType = "create_trigger"
	DropTrigger             OperationType = "drop_trigger"
	AlterTrigger            OperationType = "alter_trigger"
	CreateSynonym           OperationType = "create_synonym"
	DropSynonym             OperationType = "drop_synonym"
	AlterSynonym            OperationType = "alter_synonym"
	CreateSequence          OperationType = "create_sequence"
	DropSequence            OperationType = "drop_sequence"
	AlterSequence           OperationType = "alter_sequence"
	CreatePartitionFunction OperationType = "create_partition_function"
	DropPartitionFunction   OperationType = "drop_partition_function"
	AlterPartitionFunction  OperationType = "alter_partition_function"
	CreatePartitionScheme   OperationType = "create_partition_scheme"
	DropPartitionScheme     OperationType = "drop_partition_scheme"
	AlterPartitionScheme    OperationType = "alter_partition_scheme"
	AddExtendedProperty     OperationType = "add_extended_property"
	DropExtendedProperty    OperationType = "drop_extended_property"
	AlterExtendedProperty   OperationType = "alter_extended_property"
	AlterTableProperties    OperationType = "alter_table_properties"
)

type CreateViewOperation struct {
	SchemaName string
	View       schema.View
}

func (o CreateViewOperation) OperationType() OperationType { return CreateView }
func (o CreateViewOperation) IsDestructive() bool          { return false }

type DropViewOperation struct {
	SchemaName string
	ViewName   string
}

func (o DropViewOperation) OperationType() OperationType { return DropView }
func (o DropViewOperation) IsDestructive() bool          { return true }

type AlterViewOperation struct {
	SchemaName string
	View       schema.View
}

func (o AlterViewOperation) OperationType() OperationType { return AlterView }
func (o AlterViewOperation) IsDestructive() bool          { return false }

type CreateProcedureOperation struct {
	SchemaName string
	Procedure  schema.Procedure
}

func (o CreateProcedureOperation) OperationType() OperationType { return CreateProcedure }
func (o CreateProcedureOperation) IsDestructive() bool          { return false }

type DropProcedureOperation struct {
	SchemaName    string
	ProcedureName string
}

func (o DropProcedureOperation) OperationType() OperationType { return DropProcedure }
func (o DropProcedureOperation) IsDestructive() bool          { return true }

type AlterProcedureOperation struct {
	SchemaName string
	Procedure  schema.Procedure
}

func (o AlterProcedureOperation) OperationType() OperationType { return AlterProcedure }
func (o AlterProcedureOperation) IsDestructive() bool          { return false }

type CreateFunctionOperation struct {
	SchemaName string
	Function   schema.Function
}

func (o CreateFunctionOperation) OperationType() OperationType { return CreateFunction }
func (o CreateFunctionOperation) IsDestructive() bool          { return false }

type DropFunctionOperation struct {
	SchemaName   string
	FunctionName string
}

func (o DropFunctionOperation) OperationType() OperationType { return DropFunction }
func (o DropFunctionOperation) IsDestructive() bool          { return true }

type AlterFunctionOperation struct {
	SchemaName string
	Function   schema.Function
}

func (o AlterFunctionOperation) OperationType() OperationType { return AlterFunction }
func (o AlterFunctionOperation) IsDestructive() bool          { return false }

type CreateTriggerOperation struct {
	SchemaName string
	TableName  string
	Trigger    schema.Trigger
}

func (o CreateTriggerOperation) OperationType() OperationType { return CreateTrigger }
func (o CreateTriggerOperation) IsDestructive() bool          { return false }

type DropTriggerOperation struct {
	SchemaName  string
	TableName   string
	TriggerName string
}

func (o DropTriggerOperation) OperationType() OperationType { return DropTrigger }
func (o DropTriggerOperation) IsDestructive() bool          { return true }

type AlterTriggerOperation struct {
	SchemaName string
	TableName  string
	Trigger    schema.Trigger
}

func (o AlterTriggerOperation) OperationType() OperationType { return AlterTrigger }
func (o AlterTriggerOperation) IsDestructive() bool          { return false }

type CreateSynonymOperation struct {
	SchemaName string
	Synonym    schema.Synonym
}

func (o CreateSynonymOperation) OperationType() OperationType { return CreateSynonym }
func (o CreateSynonymOperation) IsDestructive() bool          { return false }

type DropSynonymOperation struct {
	SchemaName  string
	SynonymName string
}

func (o DropSynonymOperation) OperationType() OperationType { return DropSynonym }
func (o DropSynonymOperation) IsDestructive() bool          { return true }

type AlterSynonymOperation struct {
	SchemaName string
	Synonym    schema.Synonym
}

func (o AlterSynonymOperation) OperationType() OperationType { return AlterSynonym }
func (o AlterSynonymOperation) IsDestructive() bool          { return false }

type CreateSequenceOperation struct {
	SchemaName string
	Sequence   schema.Sequence
}

func (o CreateSequenceOperation) OperationType() OperationType { return CreateSequence }
func (o CreateSequenceOperation) IsDestructive() bool          { return false }

type DropSequenceOperation struct {
	SchemaName   string
	SequenceName string
}

func (o DropSequenceOperation) OperationType() OperationType { return DropSequence }
func (o DropSequenceOperation) IsDestructive() bool          { return true }

type AlterSequenceOperation struct {
	SchemaName string
	Sequence   schema.Sequence
}

func (o AlterSequenceOperation) OperationType() OperationType { return AlterSequence }
func (o AlterSequenceOperation) IsDestructive() bool          { return false }

type CreatePartitionFunctionOperation struct{ PartitionFunction schema.PartitionFunction }

func (o CreatePartitionFunctionOperation) OperationType() OperationType {
	return CreatePartitionFunction
}
func (o CreatePartitionFunctionOperation) IsDestructive() bool { return false }

type DropPartitionFunctionOperation struct{ PartitionFunctionName string }

func (o DropPartitionFunctionOperation) OperationType() OperationType { return DropPartitionFunction }
func (o DropPartitionFunctionOperation) IsDestructive() bool          { return true }

type AlterPartitionFunctionOperation struct{ PartitionFunction schema.PartitionFunction }

func (o AlterPartitionFunctionOperation) OperationType() OperationType { return AlterPartitionFunction }
func (o AlterPartitionFunctionOperation) IsDestructive() bool          { return false }

type CreatePartitionSchemeOperation struct {
	SchemaName      string
	PartitionScheme schema.PartitionScheme
}

func (o CreatePartitionSchemeOperation) OperationType() OperationType { return CreatePartitionScheme }
func (o CreatePartitionSchemeOperation) IsDestructive() bool          { return false }

type DropPartitionSchemeOperation struct {
	SchemaName          string
	PartitionSchemeName string
}

func (o DropPartitionSchemeOperation) OperationType() OperationType { return DropPartitionScheme }
func (o DropPartitionSchemeOperation) IsDestructive() bool          { return true }

type AlterPartitionSchemeOperation struct {
	SchemaName      string
	PartitionScheme schema.PartitionScheme
}

func (o AlterPartitionSchemeOperation) OperationType() OperationType { return AlterPartitionScheme }
func (o AlterPartitionSchemeOperation) IsDestructive() bool          { return false }

type ExtendedPropertyLevel struct {
	Level0Type string
	Level0Name string
	Level1Type string
	Level1Name string
	Level2Type string
	Level2Name string
}
type AddExtendedPropertyOperation struct {
	Name  string
	Value string
	Level ExtendedPropertyLevel
}

func (o AddExtendedPropertyOperation) OperationType() OperationType { return AddExtendedProperty }
func (o AddExtendedPropertyOperation) IsDestructive() bool          { return false }

type DropExtendedPropertyOperation struct {
	Name  string
	Level ExtendedPropertyLevel
}

func (o DropExtendedPropertyOperation) OperationType() OperationType { return DropExtendedProperty }
func (o DropExtendedPropertyOperation) IsDestructive() bool          { return true }

type AlterExtendedPropertyOperation struct {
	Name  string
	Value string
	Level ExtendedPropertyLevel
}

func (o AlterExtendedPropertyOperation) OperationType() OperationType { return AlterExtendedProperty }
func (o AlterExtendedPropertyOperation) IsDestructive() bool          { return false }

type AlterTablePropertiesOperation struct {
	SchemaName string
	TableName  string
	Table      schema.Table
}

func (o AlterTablePropertiesOperation) OperationType() OperationType { return AlterTableProperties }
func (o AlterTablePropertiesOperation) IsDestructive() bool          { return false }
