package schema

// Table represents a database table.
type Table struct {
	Name               string
	Unsupported        []string
	Statistics         map[string]bool
	UserStatistics     map[string][]string
	Checks             map[string]*CheckConstraint
	Columns            map[string]*Column
	PrimaryKey         *PrimaryKey
	ForeignKeys        map[string]*ForeignKey
	Indexes            map[string]*Index
	Triggers           map[string]*Trigger
	ExtendedProperties map[string]PropertyValue

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
	Clustered          bool
	Descending         []bool
	Name               string
	Columns            []string
	ExtendedProperties map[string]PropertyValue
}

type ForeignKey struct {
	Name               string
	Columns            []string
	RefTable           string
	RefColumns         []string
	OnUpdate           string
	OnDelete           string
	ExtendedProperties map[string]PropertyValue
}

type Index struct {
	Clustered          bool
	Descending         []bool
	IncludedColumns    []string
	Filter             string
	IsConstraint       bool
	Disabled           bool
	FillFactor         int
	PadIndex           bool
	IgnoreDupKey       bool
	AllowRowLocks      bool
	AllowPageLocks     bool
	OptionsKnown       bool
	Name               string
	Columns            []string
	IsUnique           bool
	ExtendedProperties map[string]PropertyValue
}

type CheckConstraint struct {
	Name       string
	Definition string
}
