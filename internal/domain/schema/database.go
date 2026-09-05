package schema

// Database represents the highest level in the domain model.
type Database struct {
	Name               string
	Schemas            map[string]*Schema
	PartitionFunctions map[string]*PartitionFunction
	ExtendedProperties map[string]string
}

// Schema represents a namespace within a database.
type Schema struct {
	Name               string
	Tables             map[string]*Table
	Views              map[string]*View
	Procedures         map[string]*Procedure
	Functions          map[string]*Function
	Synonyms           map[string]*Synonym
	Sequences          map[string]*Sequence
	PartitionSchemes   map[string]*PartitionScheme
	ExtendedProperties map[string]string
}
