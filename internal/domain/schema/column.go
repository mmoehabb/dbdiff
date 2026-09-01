package schema

// DataTypeKind represents an abstract data type.
type DataTypeKind string

const (
	TypeString   DataTypeKind = "string"
	TypeInteger  DataTypeKind = "integer"
	TypeDecimal  DataTypeKind = "decimal"
	TypeBoolean  DataTypeKind = "boolean"
	TypeDateTime DataTypeKind = "datetime"
	TypeUUID     DataTypeKind = "uuid"
	TypeBinary   DataTypeKind = "binary"
	TypeJSON     DataTypeKind = "json"
)

// DataType represents the type of a column.
type DataType struct {
	Kind      DataTypeKind
	Length    *int
	Precision *int
	Scale     *int
}

// DefaultExpression represents a default value for a column.
type DefaultExpression struct {
	Value string
}

// Column represents a column in a table.
type Column struct {
	Name         string
	Type         DataType
	Nullable     bool
	Default      *DefaultExpression
	Identity     bool
	Computed     bool
	ComputedExpr string
}
