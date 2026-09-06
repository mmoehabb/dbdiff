package schema

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

type DataType struct {
	Kind      DataTypeKind
	Length    *int
	Precision *int
	Scale     *int
}
type DefaultExpression struct {
	Value string
}
type Column struct {
	Name               string
	Type               DataType
	Nullable           bool
	Default            *DefaultExpression
	Identity           bool
	Computed           bool
	ComputedExpr       string
	ExtendedProperties map[string]string
}
