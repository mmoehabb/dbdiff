package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/microsoft/go-mssqldb"

	"github.com/mmoehabb/dbdiff/internal/domain/schema"
)

// MSSQLIntrospector implements the ports.Introspector interface for SQL Server.
type MSSQLIntrospector struct {
	ConnectionString string
}

func NewMSSQLIntrospector(conn string) *MSSQLIntrospector {
	return &MSSQLIntrospector{
		ConnectionString: conn,
	}
}

func (i *MSSQLIntrospector) Inspect(ctx context.Context) (*schema.Database, error) {
	db, err := sql.Open("sqlserver", i.ConnectionString)
	if err != nil {
		return nil, fmt.Errorf("failed to open connection: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	dbNameRow := db.QueryRowContext(ctx, "SELECT DB_NAME()")
	var dbName string
	if err := dbNameRow.Scan(&dbName); err != nil {
		return nil, fmt.Errorf("failed to query database name: %w", err)
	}

	database := &schema.Database{
		Name:               dbName,
		Schemas:            make(map[string]*schema.Schema),
		PartitionFunctions: make(map[string]*schema.PartitionFunction),
		ExtendedProperties: make(map[string]schema.PropertyValue),
	}

	if err := i.loadSafetyMetadata(ctx, db, database); err != nil {
		return nil, err
	}
	if err := i.loadTables(ctx, db, database); err != nil {
		return nil, err
	}
	if err := i.loadColumns(ctx, db, database); err != nil {
		return nil, err
	}
	if err := i.loadPrimaryKeys(ctx, db, database); err != nil {
		return nil, err
	}
	if err := i.loadForeignKeys(ctx, db, database); err != nil {
		return nil, err
	}

	if err := i.loadIndexes(ctx, db, database); err != nil {
		return nil, err
	}
	if err := i.loadViews(ctx, db, database); err != nil {
		return nil, err
	}
	if err := i.loadProcedures(ctx, db, database); err != nil {
		return nil, err
	}
	if err := i.loadFunctions(ctx, db, database); err != nil {
		return nil, err
	}
	if err := i.loadTriggers(ctx, db, database); err != nil {
		return nil, err
	}
	if err := i.loadSynonyms(ctx, db, database); err != nil {
		return nil, err
	}
	if err := i.loadSequences(ctx, db, database); err != nil {
		return nil, err
	}
	if err := i.loadTemporalTables(ctx, db, database); err != nil {
		return nil, err
	}
	if err := i.loadPartitionFunctions(ctx, db, database); err != nil {
		return nil, err
	}
	if err := i.loadPartitionSchemes(ctx, db, database); err != nil {
		return nil, err
	}
	if err := i.loadReplication(ctx, db, database); err != nil {
		return nil, err
	}
	if err := i.loadTablePartitioning(ctx, db, database); err != nil {
		return nil, err
	}
	if err := i.loadExtendedProperties(ctx, db, database); err != nil {
		return nil, err
	}

	if err := i.loadChecksAndStatistics(ctx, db, database); err != nil {
		return nil, err
	}
	return database, nil
}

func (i *MSSQLIntrospector) ensureSchemaAndTable(database *schema.Database, schemaName, tableName string) *schema.Table {
	if _, ok := database.Schemas[schemaName]; !ok {
		database.Schemas[schemaName] = &schema.Schema{
			Name:   schemaName,
			Tables: make(map[string]*schema.Table),
		}
	}
	s := database.Schemas[schemaName]

	if _, ok := s.Tables[tableName]; !ok {
		s.Tables[tableName] = &schema.Table{
			Name:        tableName,
			Columns:     make(map[string]*schema.Column),
			ForeignKeys: make(map[string]*schema.ForeignKey),
			Indexes:     make(map[string]*schema.Index),
			Triggers:    make(map[string]*schema.Trigger),
		}
	}
	return s.Tables[tableName]
}

func (i *MSSQLIntrospector) loadTables(ctx context.Context, db *sql.DB, database *schema.Database) error {
	rows, err := db.QueryContext(ctx, queryTables)
	if err != nil {
		return fmt.Errorf("failed to query tables: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var schemaName, tableName string
		if err := rows.Scan(&schemaName, &tableName); err != nil {
			return err
		}
		i.ensureSchemaAndTable(database, schemaName, tableName)
	}
	return rows.Err()
}

func (i *MSSQLIntrospector) loadColumns(ctx context.Context, db *sql.DB, database *schema.Database) error {
	rows, err := db.QueryContext(ctx, queryColumns)
	if err != nil {
		return fmt.Errorf("failed to query columns: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var schemaName, tableName, columnName, typeName string
		var maxLength int
		var precision, scale int
		var isNullable, isIdentity, isComputed bool
		var computedExpr sql.NullString
		var defaultValue sql.NullString
		var defaultName sql.NullString
		var collation, seed, increment sql.NullString
		var ordinal, generated int
		var persisted sql.NullBool

		if err := rows.Scan(&schemaName, &tableName, &columnName, &typeName, &maxLength, &precision, &scale, &isNullable, &isIdentity, &isComputed, &computedExpr, &defaultValue, &defaultName, &collation, &ordinal, &generated, &persisted, &seed, &increment); err != nil {
			return err
		}

		table := i.ensureSchemaAndTable(database, schemaName, tableName)

		col := &schema.Column{
			Name:      columnName,
			Collation: collation.String, Ordinal: ordinal, Generated: generated != 0 || typeName == "timestamp" || typeName == "rowversion", Persisted: persisted.Bool, IdentitySeed: seed.String, IdentityIncrement: increment.String,
			Type:     mapMSSQLTypeToDomain(typeName, maxLength, precision, scale),
			Nullable: isNullable,
			Identity: isIdentity,
			Computed: isComputed,
		}

		if isComputed && computedExpr.Valid {
			col.ComputedExpr = computedExpr.String
		}

		if defaultValue.Valid {
			col.Default = parseDefaultValue(defaultValue.String)
			if defaultName.Valid {
				col.Default.Name = defaultName.String
			}
		}

		table.Columns[columnName] = col
	}
	return rows.Err()
}

func (i *MSSQLIntrospector) loadPrimaryKeys(ctx context.Context, db *sql.DB, database *schema.Database) error {
	rows, err := db.QueryContext(ctx, queryPrimaryKeys)
	if err != nil {
		return fmt.Errorf("failed to query primary keys: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var schemaName, tableName, keyName, columnName string
		var indexType int
		var descending bool
		if err := rows.Scan(&schemaName, &tableName, &keyName, &columnName, &indexType, &descending); err != nil {
			return err
		}

		table := i.ensureSchemaAndTable(database, schemaName, tableName)

		if table.PrimaryKey == nil {
			table.PrimaryKey = &schema.PrimaryKey{
				Name: keyName, Clustered: indexType == 1,
				Columns: []string{},
			}
		}
		table.PrimaryKey.Columns = append(table.PrimaryKey.Columns, columnName)
		table.PrimaryKey.Descending = append(table.PrimaryKey.Descending, descending)
	}
	return rows.Err()
}

func (i *MSSQLIntrospector) loadForeignKeys(ctx context.Context, db *sql.DB, database *schema.Database) error {
	rows, err := db.QueryContext(ctx, queryForeignKeys)
	if err != nil {
		return fmt.Errorf("failed to query foreign keys: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var schemaName, tableName, fkName, columnName, refSchema, refTable, refColumn string
		var onUpdate, onDelete sql.NullString

		if err := rows.Scan(&schemaName, &tableName, &fkName, &columnName, &refSchema, &refTable, &refColumn, &onUpdate, &onDelete); err != nil {
			return err
		}

		table := i.ensureSchemaAndTable(database, schemaName, tableName)

		fk, ok := table.ForeignKeys[fkName]
		if !ok {
			fk = &schema.ForeignKey{
				Name:       fkName,
				Columns:    []string{},
				RefTable:   qualified(refSchema, refTable),
				RefColumns: []string{},
				OnUpdate:   onUpdate.String,
				OnDelete:   onDelete.String,
			}
			table.ForeignKeys[fkName] = fk
		}

		fk.Columns = append(fk.Columns, columnName)
		fk.RefColumns = append(fk.RefColumns, refColumn)
	}
	return rows.Err()
}

func (i *MSSQLIntrospector) loadIndexes(ctx context.Context, db *sql.DB, database *schema.Database) error {
	rows, err := db.QueryContext(ctx, queryIndexes)
	if err != nil {
		return fmt.Errorf("failed to query indexes: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var schemaName, tableName, indexName, columnName string
		var isUnique, descending, included, isConstraint, disabled, padded, ignoreDup, rowLocks, pageLocks bool
		var indexType, ordinal, fill int
		var filter sql.NullString

		if err := rows.Scan(&schemaName, &tableName, &indexName, &columnName, &isUnique, &indexType, &ordinal, &descending, &included, &filter, &isConstraint, &disabled, &fill, &padded, &ignoreDup, &rowLocks, &pageLocks); err != nil {
			return err
		}

		table := i.ensureSchemaAndTable(database, schemaName, tableName)

		idx, ok := table.Indexes[indexName]
		if !ok {
			idx = &schema.Index{
				Name:      indexName,
				Clustered: indexType == 1, Filter: filter.String, IsConstraint: isConstraint, Disabled: disabled, FillFactor: fill, PadIndex: padded, IgnoreDupKey: ignoreDup, AllowRowLocks: rowLocks, AllowPageLocks: pageLocks, OptionsKnown: true,
				Columns:  []string{},
				IsUnique: isUnique,
			}
			table.Indexes[indexName] = idx
		}

		if included {
			idx.IncludedColumns = append(idx.IncludedColumns, columnName)
		} else if ordinal > 0 {
			idx.Columns = append(idx.Columns, columnName)
			idx.Descending = append(idx.Descending, descending)
		}
	}
	return rows.Err()
}

func (i *MSSQLIntrospector) ensureSchema(database *schema.Database, schemaName string) *schema.Schema {
	if _, ok := database.Schemas[schemaName]; !ok {
		database.Schemas[schemaName] = &schema.Schema{
			Name:               schemaName,
			Tables:             make(map[string]*schema.Table),
			Views:              make(map[string]*schema.View),
			Procedures:         make(map[string]*schema.Procedure),
			Functions:          make(map[string]*schema.Function),
			Synonyms:           make(map[string]*schema.Synonym),
			Sequences:          make(map[string]*schema.Sequence),
			PartitionSchemes:   make(map[string]*schema.PartitionScheme),
			ExtendedProperties: make(map[string]schema.PropertyValue),
		}
	} else {
		s := database.Schemas[schemaName]
		if s.Tables == nil {
			s.Tables = make(map[string]*schema.Table)
		}
		if s.Views == nil {
			s.Views = make(map[string]*schema.View)
		}
		if s.Procedures == nil {
			s.Procedures = make(map[string]*schema.Procedure)
		}
		if s.Functions == nil {
			s.Functions = make(map[string]*schema.Function)
		}
		if s.Synonyms == nil {
			s.Synonyms = make(map[string]*schema.Synonym)
		}
		if s.Sequences == nil {
			s.Sequences = make(map[string]*schema.Sequence)
		}
		if s.PartitionSchemes == nil {
			s.PartitionSchemes = make(map[string]*schema.PartitionScheme)
		}
		if s.ExtendedProperties == nil {
			s.ExtendedProperties = make(map[string]schema.PropertyValue)
		}
	}
	return database.Schemas[schemaName]
}

func (i *MSSQLIntrospector) loadViews(ctx context.Context, db *sql.DB, database *schema.Database) error {
	rows, err := db.QueryContext(ctx, queryViews)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var s, v, d string
		var ansi, quoted bool
		if err := rows.Scan(&s, &v, &d, &ansi, &quoted); err != nil {
			return err
		}
		i.ensureSchema(database, s).Views[v] = &schema.View{Name: v, Definition: canonicalModule(d, s, v), UsesANSI_NULLS: ansi, UsesQuotedIdentifier: quoted}
	}
	return rows.Err()
}

func (i *MSSQLIntrospector) loadProcedures(ctx context.Context, db *sql.DB, database *schema.Database) error {
	rows, err := db.QueryContext(ctx, queryProcedures)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var s, p, d string
		var ansi, quoted bool
		if err := rows.Scan(&s, &p, &d, &ansi, &quoted); err != nil {
			return err
		}
		i.ensureSchema(database, s).Procedures[p] = &schema.Procedure{Name: p, Definition: canonicalModule(d, s, p), UsesANSI_NULLS: ansi, UsesQuotedIdentifier: quoted}
	}
	return rows.Err()
}

func (i *MSSQLIntrospector) loadFunctions(ctx context.Context, db *sql.DB, database *schema.Database) error {
	rows, err := db.QueryContext(ctx, queryFunctions)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var s, f, kind, d string
		var ansi, quoted bool
		if err := rows.Scan(&s, &f, &kind, &d, &ansi, &quoted); err != nil {
			return err
		}
		i.ensureSchema(database, s).Functions[f] = &schema.Function{Name: f, Kind: strings.TrimSpace(kind), Definition: canonicalModule(d, s, f), UsesANSI_NULLS: ansi, UsesQuotedIdentifier: quoted}
	}
	return rows.Err()
}

func (i *MSSQLIntrospector) loadTriggers(ctx context.Context, db *sql.DB, database *schema.Database) error {
	rows, err := db.QueryContext(ctx, queryTriggers)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var s, t, tb, d string
		var ansi, quoted bool
		if err := rows.Scan(&s, &t, &tb, &d, &ansi, &quoted); err != nil {
			return err
		}
		table := i.ensureSchemaAndTable(database, s, tb)
		if table.Triggers == nil {
			table.Triggers = make(map[string]*schema.Trigger)
		}
		table.Triggers[t] = &schema.Trigger{Name: t, Definition: canonicalModule(d, s, t), UsesANSI_NULLS: ansi, UsesQuotedIdentifier: quoted}
	}
	return rows.Err()
}

func (i *MSSQLIntrospector) loadSynonyms(ctx context.Context, db *sql.DB, database *schema.Database) error {
	rows, err := db.QueryContext(ctx, querySynonyms)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var s, syn, o string
		if err := rows.Scan(&s, &syn, &o); err != nil {
			return err
		}
		i.ensureSchema(database, s).Synonyms[syn] = &schema.Synonym{Name: syn, TargetObjectName: o}
	}
	return rows.Err()
}

func (i *MSSQLIntrospector) loadSequences(ctx context.Context, db *sql.DB, database *schema.Database) error {
	rows, err := db.QueryContext(ctx, querySequences)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var s, seq, typeName string
		var precision, scale int
		var st, inc, min, max, ca int64
		var cyc bool
		if err := rows.Scan(&s, &seq, &st, &inc, &min, &max, &cyc, &ca, &typeName, &precision, &scale); err != nil {
			return err
		}
		i.ensureSchema(database, s).Sequences[seq] = &schema.Sequence{Name: seq, Type: mapMSSQLTypeToDomain(typeName, 0, precision, scale), StartValue: st, Increment: inc, MinValue: min, MaxValue: max, IsCycling: cyc, CacheSize: ca}
	}
	return rows.Err()
}

func (i *MSSQLIntrospector) loadTemporalTables(ctx context.Context, db *sql.DB, database *schema.Database) error {
	rows, err := db.QueryContext(ctx, queryTemporalTables)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var s, t, hs, ht, vf, vt string
		var tt int
		if err := rows.Scan(&s, &t, &tt, &hs, &ht, &vf, &vt); err != nil {
			return err
		}
		table := i.ensureSchemaAndTable(database, s, t)
		table.IsSystemVersioned = true
		table.HistoryTable = qualified(hs, ht)
		table.ValidFromColumn = vf
		table.ValidToColumn = vt
	}
	return rows.Err()
}

func (i *MSSQLIntrospector) loadPartitionFunctions(ctx context.Context, db *sql.DB, database *schema.Database) error {
	rows, err := db.QueryContext(ctx, queryPartitionFunctions)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var f, p string
		var b sql.NullString
		if err := rows.Scan(&f, &p, &b); err != nil {
			return err
		}
		if _, ok := database.PartitionFunctions[f]; !ok {
			database.PartitionFunctions[f] = &schema.PartitionFunction{Name: f, InputParameterType: p}
		}
		if b.Valid {
			database.PartitionFunctions[f].BoundaryValues = append(database.PartitionFunctions[f].BoundaryValues, b.String)
		}
	}
	return rows.Err()
}

func (i *MSSQLIntrospector) loadPartitionSchemes(ctx context.Context, db *sql.DB, database *schema.Database) error {
	rows, err := db.QueryContext(ctx, queryPartitionSchemes)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var s, f, g string
		if err := rows.Scan(&s, &f, &g); err != nil {
			return err
		}
		sc := i.ensureSchema(database, "dbo")
		if _, ok := sc.PartitionSchemes[s]; !ok {
			sc.PartitionSchemes[s] = &schema.PartitionScheme{Name: s, PartitionFunction: f}
		}
		sc.PartitionSchemes[s].FileGroups = append(sc.PartitionSchemes[s].FileGroups, g)
	}
	return rows.Err()
}

func (i *MSSQLIntrospector) loadReplication(ctx context.Context, db *sql.DB, database *schema.Database) error {
	rows, err := db.QueryContext(ctx, queryReplication)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var s, t string
		var r bool
		if err := rows.Scan(&s, &t, &r); err != nil {
			return err
		}
		table := i.ensureSchemaAndTable(database, s, t)
		table.IsReplicated = r
	}
	return rows.Err()
}

func (i *MSSQLIntrospector) loadTablePartitioning(ctx context.Context, db *sql.DB, database *schema.Database) error {
	q := `SELECT s.name AS schema_name, t.name AS table_name, ps.name AS partition_scheme, c.name AS partition_column, fg.name AS file_group FROM sys.tables t JOIN sys.schemas s ON t.schema_id = s.schema_id JOIN sys.indexes i ON t.object_id = i.object_id AND i.index_id <= 1 JOIN sys.partition_schemes ps ON i.data_space_id = ps.data_space_id JOIN sys.index_columns ic ON i.object_id = ic.object_id AND i.index_id = ic.index_id AND ic.partition_ordinal > 0 JOIN sys.columns c ON ic.object_id = c.object_id AND ic.column_id = c.column_id LEFT JOIN sys.destination_data_spaces dds ON ps.data_space_id = dds.partition_scheme_id LEFT JOIN sys.filegroups fg ON dds.data_space_id = fg.data_space_id;`
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return fmt.Errorf("query table partitioning: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var s, t, ps, pc, fg string
		if err := rows.Scan(&s, &t, &ps, &pc, &fg); err != nil {
			return err
		}
		table := i.ensureSchemaAndTable(database, s, t)
		table.PartitionScheme = ps
		table.PartitionColumn = pc
		table.FileGroup = fg
	}
	return rows.Err()
}

func (i *MSSQLIntrospector) loadExtendedProperties(ctx context.Context, db *sql.DB, database *schema.Database) error {
	rows, err := db.QueryContext(ctx, queryExtendedProperties)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var class int
		var sn, obj, sub, name, value string
		var typ sql.NullString
		var length, precision, scale sql.NullInt64
		if err := rows.Scan(&class, &sn, &obj, &sub, &name, &value, &typ, &length, &precision, &scale); err != nil {
			return err
		}
		v := schema.PropertyValue{Type: typ.String, Value: value, Length: int(length.Int64), Precision: int(precision.Int64), Scale: int(scale.Int64), IsNull: !typ.Valid}
		if _, err := propertyType(v); err != nil {
			database.Unsupported = append(database.Unsupported, fmt.Sprintf("property %s: %v", name, err))
			continue
		}
		var dest *map[string]schema.PropertyValue
		switch class {
		case 0:
			dest = &database.ExtendedProperties
		case 3:
			s := i.ensureSchema(database, sn)
			dest = &s.ExtendedProperties
		case 1:
			s := i.ensureSchema(database, sn)
			if sub != "" {
				if t := s.Tables[obj]; t != nil {
					if c := t.Columns[sub]; c != nil {
						dest = &c.ExtendedProperties
					}
				}
			} else {
				if t := s.Tables[obj]; t != nil {
					dest = &t.ExtendedProperties
				} else if v := s.Views[obj]; v != nil {
					dest = &v.ExtendedProperties
				} else if p := s.Procedures[obj]; p != nil {
					dest = &p.ExtendedProperties
				} else if f := s.Functions[obj]; f != nil {
					dest = &f.ExtendedProperties
				}
			}
		}
		if dest == nil {
			database.Unsupported = append(database.Unsupported, fmt.Sprintf("property %s on unsupported class/object %d %s.%s.%s", name, class, sn, obj, sub))
			continue
		}
		if *dest == nil {
			*dest = make(map[string]schema.PropertyValue)
		}
		(*dest)[name] = v
	}
	return rows.Err()
}
