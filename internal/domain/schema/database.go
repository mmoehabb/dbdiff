package schema

// Database represents the highest level in the domain model.
type Database struct {
	Name    string
	Schemas map[string]*Schema
}

// Schema represents a namespace within a database.
type Schema struct {
	Name   string
	Tables map[string]*Table
}
