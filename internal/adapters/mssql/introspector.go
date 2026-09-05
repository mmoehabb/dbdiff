package mssql

import (
	"context"
	"database/sql"
	"fmt"

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
		ExtendedProperties: make(map[string]string),
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

		if err := rows.Scan(&schemaName, &tableName, &columnName, &typeName, &maxLength, &precision, &scale, &isNullable, &isIdentity, &isComputed, &computedExpr, &defaultValue); err != nil {
			return err
		}

		table := i.ensureSchemaAndTable(database, schemaName, tableName)

		col := &schema.Column{
			Name:     columnName,
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
		if err := rows.Scan(&schemaName, &tableName, &keyName, &columnName); err != nil {
			return err
		}

		table := i.ensureSchemaAndTable(database, schemaName, tableName)

		if table.PrimaryKey == nil {
			table.PrimaryKey = &schema.PrimaryKey{
				Name:    keyName,
				Columns: []string{},
			}
		}
		table.PrimaryKey.Columns = append(table.PrimaryKey.Columns, columnName)
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
				RefTable:   fmt.Sprintf("[%s].[%s]", refSchema, refTable),
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
		var isUnique bool

		if err := rows.Scan(&schemaName, &tableName, &indexName, &columnName, &isUnique); err != nil {
			return err
		}

		table := i.ensureSchemaAndTable(database, schemaName, tableName)

		idx, ok := table.Indexes[indexName]
		if !ok {
			idx = &schema.Index{
				Name:     indexName,
				Columns:  []string{},
				IsUnique: isUnique,
			}
			table.Indexes[indexName] = idx
		}

		idx.Columns = append(idx.Columns, columnName)
	}
	return rows.Err()
}

func (i *MSSQLIntrospector) ensureSchema(database *schema.Database, schemaName string) *schema.Schema {
	if _, ok := database.Schemas[schemaName]; !ok {
		database.Schemas[schemaName] = &schema.Schema{
			Name:       schemaName,
			Tables:     make(map[string]*schema.Table),
			Views:      make(map[string]*schema.View),
			Procedures: make(map[string]*schema.Procedure),
			Functions:  make(map[string]*schema.Function),
			Synonyms:   make(map[string]*schema.Synonym),
			Sequences:  make(map[string]*schema.Sequence),
			PartitionSchemes: make(map[string]*schema.PartitionScheme),
		}
	}
	return database.Schemas[schemaName]
}

func (i *MSSQLIntrospector) loadViews(ctx context.Context, db *sql.DB, database *schema.Database) error {
	rows, err := db.QueryContext(ctx, queryViews)
	if err != nil { return err }
	defer rows.Close()
	for rows.Next() {
		var s, v, d string
		if err := rows.Scan(&s, &v, &d); err != nil { return err }
		i.ensureSchema(database, s).Views[v] = &schema.View{Name: v, Definition: d}
	}
	return nil
}

func (i *MSSQLIntrospector) loadProcedures(ctx context.Context, db *sql.DB, database *schema.Database) error {
	rows, err := db.QueryContext(ctx, queryProcedures)
	if err != nil { return err }
	defer rows.Close()
	for rows.Next() {
		var s, p, d string
		if err := rows.Scan(&s, &p, &d); err != nil { return err }
		i.ensureSchema(database, s).Procedures[p] = &schema.Procedure{Name: p, Definition: d}
	}
	return nil
}

func (i *MSSQLIntrospector) loadFunctions(ctx context.Context, db *sql.DB, database *schema.Database) error {
	rows, err := db.QueryContext(ctx, queryFunctions)
	if err != nil { return err }
	defer rows.Close()
	for rows.Next() {
		var s, f, d string
		if err := rows.Scan(&s, &f, &d); err != nil { return err }
		i.ensureSchema(database, s).Functions[f] = &schema.Function{Name: f, Definition: d}
	}
	return nil
}

func (i *MSSQLIntrospector) loadTriggers(ctx context.Context, db *sql.DB, database *schema.Database) error {
	rows, err := db.QueryContext(ctx, queryTriggers)
	if err != nil { return err }
	defer rows.Close()
	for rows.Next() {
		var s, t, tb, d string
		if err := rows.Scan(&s, &t, &tb, &d); err != nil { return err }
		table := i.ensureSchemaAndTable(database, s, tb)
		if table.Triggers == nil { table.Triggers = make(map[string]*schema.Trigger) }
		table.Triggers[t] = &schema.Trigger{Name: t, Definition: d}
	}
	return nil
}

func (i *MSSQLIntrospector) loadSynonyms(ctx context.Context, db *sql.DB, database *schema.Database) error {
	rows, err := db.QueryContext(ctx, querySynonyms)
	if err != nil { return err }
	defer rows.Close()
	for rows.Next() {
		var s, syn, o string
		if err := rows.Scan(&s, &syn, &o); err != nil { return err }
		i.ensureSchema(database, s).Synonyms[syn] = &schema.Synonym{Name: syn, TargetObjectName: o}
	}
	return nil
}

func (i *MSSQLIntrospector) loadSequences(ctx context.Context, db *sql.DB, database *schema.Database) error {
	rows, err := db.QueryContext(ctx, querySequences)
	if err != nil { return err }
	defer rows.Close()
	for rows.Next() {
		var s, seq string
		var st, inc, min, max, ca int64
		var cyc bool
		if err := rows.Scan(&s, &seq, &st, &inc, &min, &max, &cyc, &ca); err != nil { return err }
		i.ensureSchema(database, s).Sequences[seq] = &schema.Sequence{Name: seq, StartValue: st, Increment: inc, MinValue: min, MaxValue: max, IsCycling: cyc, CacheSize: ca}
	}
	return nil
}

func (i *MSSQLIntrospector) loadTemporalTables(ctx context.Context, db *sql.DB, database *schema.Database) error {
	rows, err := db.QueryContext(ctx, queryTemporalTables)
	if err != nil { return err }
	defer rows.Close()
	for rows.Next() {
		var s, t, hs, ht, vf, vt string
		var tt int
		if err := rows.Scan(&s, &t, &tt, &hs, &ht, &vf, &vt); err != nil { return err }
		table := i.ensureSchemaAndTable(database, s, t)
		table.IsSystemVersioned = true
		table.HistoryTable = fmt.Sprintf("[%s].[%s]", hs, ht)
		table.ValidFromColumn = vf
		table.ValidToColumn = vt
	}
	return nil
}

func (i *MSSQLIntrospector) loadPartitionFunctions(ctx context.Context, db *sql.DB, database *schema.Database) error {
	rows, err := db.QueryContext(ctx, queryPartitionFunctions)
	if err != nil { return err }
	defer rows.Close()
	for rows.Next() {
		var f, p string
		var b sql.NullString
		if err := rows.Scan(&f, &p, &b); err != nil { return err }
		if _, ok := database.PartitionFunctions[f]; !ok {
			database.PartitionFunctions[f] = &schema.PartitionFunction{Name: f, InputParameterType: p}
		}
		if b.Valid {
			database.PartitionFunctions[f].BoundaryValues = append(database.PartitionFunctions[f].BoundaryValues, b.String)
		}
	}
	return nil
}

func (i *MSSQLIntrospector) loadPartitionSchemes(ctx context.Context, db *sql.DB, database *schema.Database) error {
	rows, err := db.QueryContext(ctx, queryPartitionSchemes)
	if err != nil { return err }
	defer rows.Close()
	for rows.Next() {
		var s, f, g string
		if err := rows.Scan(&s, &f, &g); err != nil { return err }
		sc := i.ensureSchema(database, "dbo")
		if _, ok := sc.PartitionSchemes[s]; !ok {
			sc.PartitionSchemes[s] = &schema.PartitionScheme{Name: s, PartitionFunction: f}
		}
		sc.PartitionSchemes[s].FileGroups = append(sc.PartitionSchemes[s].FileGroups, g)
	}
	return nil
}

func (i *MSSQLIntrospector) loadReplication(ctx context.Context, db *sql.DB, database *schema.Database) error {
	rows, err := db.QueryContext(ctx, queryReplication)
	if err != nil { return err }
	defer rows.Close()
	for rows.Next() {
		var s, t string
		var r bool
		if err := rows.Scan(&s, &t, &r); err != nil { return err }
		table := i.ensureSchemaAndTable(database, s, t)
		table.IsReplicated = r
	}
	return nil
}

func (i *MSSQLIntrospector) loadTablePartitioning(ctx context.Context, db *sql.DB, database *schema.Database) error {
	q := `SELECT s.name AS schema_name, t.name AS table_name, ps.name AS partition_scheme, c.name AS partition_column, fg.name AS file_group FROM sys.tables t JOIN sys.schemas s ON t.schema_id = s.schema_id JOIN sys.indexes i ON t.object_id = i.object_id AND i.index_id <= 1 JOIN sys.partition_schemes ps ON i.data_space_id = ps.data_space_id JOIN sys.index_columns ic ON i.object_id = ic.object_id AND i.index_id = ic.index_id AND ic.partition_ordinal > 0 JOIN sys.columns c ON ic.object_id = c.object_id AND ic.column_id = c.column_id LEFT JOIN sys.destination_data_spaces dds ON ps.data_space_id = dds.partition_scheme_id LEFT JOIN sys.filegroups fg ON dds.data_space_id = fg.data_space_id;`
	rows, err := db.QueryContext(ctx, q)
	if err != nil { return nil } // if err, just skip for now so we don't break on old sql versions
	defer rows.Close()
	for rows.Next() {
		var s, t, ps, pc, fg string
		if err := rows.Scan(&s, &t, &ps, &pc, &fg); err != nil { return err }
		table := i.ensureSchemaAndTable(database, s, t)
		table.PartitionScheme = ps
		table.PartitionColumn = pc
		table.FileGroup = fg
	}
	return nil
}

func (i *MSSQLIntrospector) loadExtendedProperties(ctx context.Context, db *sql.DB, database *schema.Database) error {
	rows, err := db.QueryContext(ctx, queryExtendedProperties)
	if err != nil { return err }
	defer rows.Close()
	for rows.Next() {
		var class int
		var schemaName, objName, subName, n, v string
		if err := rows.Scan(&class, &schemaName, &objName, &subName, &n, &v); err != nil { return err }
		if class == 0 {
			database.ExtendedProperties[n] = v
		} else if class == 1 {
			if schemaName != "" && objName != "" {
				s := i.ensureSchema(database, schemaName)
				if subName == "" {
					if t, ok := s.Tables[objName]; ok {
						if t.ExtendedProperties == nil { t.ExtendedProperties = make(map[string]string) }
						t.ExtendedProperties[n] = v
					} else if v2, ok := s.Views[objName]; ok {
						if v2.ExtendedProperties == nil { v2.ExtendedProperties = make(map[string]string) }
						v2.ExtendedProperties[n] = v
					} else if p, ok := s.Procedures[objName]; ok {
						if p.ExtendedProperties == nil { p.ExtendedProperties = make(map[string]string) }
						p.ExtendedProperties[n] = v
					} else if f, ok := s.Functions[objName]; ok {
						if f.ExtendedProperties == nil { f.ExtendedProperties = make(map[string]string) }
						f.ExtendedProperties[n] = v
					}
				} else {
					if t, ok := s.Tables[objName]; ok {
						if col, ok := t.Columns[subName]; ok {
							if col.ExtendedProperties == nil { col.ExtendedProperties = make(map[string]string) }
							col.ExtendedProperties[n] = v
						}
					}
				}
			}
		} else if class == 3 {
			if schemaName != "" {
				s := i.ensureSchema(database, schemaName)
				if s.ExtendedProperties == nil { s.ExtendedProperties = make(map[string]string) }
				s.ExtendedProperties[n] = v
			}
		}
	}
	return nil
}
