package schema

type View struct {
	Name               string
	Definition         string
	ExtendedProperties map[string]string
}
type Procedure struct {
	Name               string
	Definition         string
	ExtendedProperties map[string]string
}
type Function struct {
	Name               string
	Definition         string
	ExtendedProperties map[string]string
}
type Trigger struct {
	Name               string
	Definition         string
	ExtendedProperties map[string]string
}
type Synonym struct {
	Name               string
	TargetObjectName   string
	ExtendedProperties map[string]string
}
type Sequence struct {
	Name               string
	StartValue         int64
	Increment          int64
	MinValue           int64
	MaxValue           int64
	IsCycling          bool
	CacheSize          int64
	ExtendedProperties map[string]string
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
