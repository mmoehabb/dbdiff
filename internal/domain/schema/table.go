package schema

// Table represents a database table.
type Table struct {
	Name        string
	Columns     map[string]*Column
	PrimaryKey  *PrimaryKey
	ForeignKeys map[string]*ForeignKey
	Indexes     map[string]*Index
}

// PrimaryKey represents a primary key constraint.
type PrimaryKey struct {
	Name    string
	Columns []string
}

// ForeignKey represents a foreign key constraint.
type ForeignKey struct {
	Name       string
	Columns    []string
	RefTable   string
	RefColumns []string
	OnUpdate   string
	OnDelete   string
}

// Index represents an index on a table.
type Index struct {
	Name     string
	Columns  []string
	IsUnique bool
}
