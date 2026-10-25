package schema

type View struct {
	Name                 string
	Definition           string
	UsesANSI_NULLS       bool
	UsesQuotedIdentifier bool
	ExtendedProperties   map[string]PropertyValue
}
type Procedure struct {
	Name                 string
	Definition           string
	UsesANSI_NULLS       bool
	UsesQuotedIdentifier bool
	ExtendedProperties   map[string]PropertyValue
}
type Function struct {
	Kind                 string // SQL Server catalog kind: FN (scalar), IF (inline TVF), or TF (multi-statement TVF).
	Name                 string
	Definition           string
	UsesANSI_NULLS       bool
	UsesQuotedIdentifier bool
	ExtendedProperties   map[string]PropertyValue
}
type Trigger struct {
	Name                 string
	Definition           string
	UsesANSI_NULLS       bool
	UsesQuotedIdentifier bool
	ExtendedProperties   map[string]PropertyValue
}
type Synonym struct {
	Name               string
	TargetObjectName   string
	ExtendedProperties map[string]PropertyValue
}
type Sequence struct {
	Type               DataType
	Name               string
	StartValue         int64
	Increment          int64
	MinValue           int64
	MaxValue           int64
	IsCycling          bool
	CacheSize          int64
	ExtendedProperties map[string]PropertyValue
}
type PartitionFunction struct {
	Name               string
	InputParameterType string
	BoundaryValues     []string
}
type PartitionScheme struct {
	Name              string
	PartitionFunction string
	FileGroups        []string
}

// PropertyValue preserves SQL_VARIANT metadata for supported extended property types.
type PropertyValue struct {
	Type      string
	Value     string
	Length    int
	Precision int
	Scale     int
	IsNull    bool
}
