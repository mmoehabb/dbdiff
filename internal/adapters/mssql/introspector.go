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
		Name:    dbName,
		Schemas: make(map[string]*schema.Schema),
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
