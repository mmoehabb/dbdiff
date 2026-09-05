package schema

// Table represents a database table.
type Table struct {
	Name               string
	Columns            map[string]*Column
	PrimaryKey         *PrimaryKey
	ForeignKeys        map[string]*ForeignKey
	Indexes            map[string]*Index
	Triggers           map[string]*Trigger
	ExtendedProperties map[string]string

	IsSystemVersioned bool
	HistoryTable      string
	ValidFromColumn   string
	ValidToColumn     string

	IsReplicated bool

	PartitionScheme string
	PartitionColumn string
	FileGroup       string
}

type PrimaryKey struct {
	Name               string
	Columns            []string
	ExtendedProperties map[string]string
}

type ForeignKey struct {
	Name               string
	Columns            []string
	RefTable           string
	RefColumns         []string
	OnUpdate           string
	OnDelete           string
	ExtendedProperties map[string]string
}

type Index struct {
	Name               string
	Columns            []string
	IsUnique           bool
	ExtendedProperties map[string]string
}
