package mssql

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mmoehabb/dbdiff/internal/domain/diff"
	"github.com/mmoehabb/dbdiff/internal/domain/schema"
)

// MSSQLRenderer implements the ports.Renderer interface for SQL Server.
type MSSQLRenderer struct{}

func NewMSSQLRenderer() *MSSQLRenderer {
	return &MSSQLRenderer{}
}

func (r *MSSQLRenderer) Render(ctx context.Context, plan *diff.MigrationPlan) (string, error) {
	var builder strings.Builder

	builder.WriteString("BEGIN TRANSACTION;\n\n")

	for _, op := range plan.SchemaOperations {
		sql, err := r.renderOperation(op)
		if err != nil {
			return "", err
		}
		if sql != "" {
			builder.WriteString(sql)
			builder.WriteString("\n\n")
		}
	}

	if len(plan.DataOperations) > 0 {
		builder.WriteString("-- Data Operations\n\n")
		for _, op := range plan.DataOperations {
			sql, err := r.renderOperation(op)
			if err != nil {
				return "", err
			}
			if sql != "" {
				builder.WriteString(sql)
				builder.WriteString("\n")
			}
		}
		builder.WriteString("\n")
	}

	builder.WriteString("COMMIT TRANSACTION;\n")

	return builder.String(), nil
}

func (r *MSSQLRenderer) renderOperation(op diff.Operation) (string, error) {
	switch o := op.(type) {
	case diff.CreateSchemaOperation:
		return fmt.Sprintf("CREATE SCHEMA [%s];", o.SchemaName), nil
	case diff.DropSchemaOperation:
		return fmt.Sprintf("DROP SCHEMA [%s];", o.SchemaName), nil
	case diff.CreateTableOperation:
		return r.renderCreateTable(o)
	case diff.DropTableOperation:
		return fmt.Sprintf("DROP TABLE [%s].[%s];", o.SchemaName, o.TableName), nil
	case diff.AddColumnOperation:
		colDef := r.renderColumnDefinition(o.Column)
		return fmt.Sprintf("ALTER TABLE [%s].[%s] ADD %s;", o.SchemaName, o.TableName, colDef), nil
	case diff.DropColumnOperation:
		return fmt.Sprintf("ALTER TABLE [%s].[%s] DROP COLUMN [%s];", o.SchemaName, o.TableName, o.ColumnName), nil
	case diff.AlterColumnOperation:
		colDef := r.renderColumnDefinition(o.Column)
		return fmt.Sprintf("ALTER TABLE [%s].[%s] ALTER COLUMN %s;", o.SchemaName, o.TableName, colDef), nil
	case diff.AddPrimaryKeyOperation:
		cols := make([]string, len(o.PrimaryKey.Columns))
		for i, c := range o.PrimaryKey.Columns {
			cols[i] = fmt.Sprintf("[%s]", c)
		}
		pkName := o.PrimaryKey.Name
		if pkName == "" {
			pkName = fmt.Sprintf("PK_%s", o.TableName)
		}
		return fmt.Sprintf("ALTER TABLE [%s].[%s] ADD CONSTRAINT [%s] PRIMARY KEY (%s);",
			o.SchemaName, o.TableName, pkName, strings.Join(cols, ", ")), nil
	case diff.DropPrimaryKeyOperation:
		// For DropPrimaryKeyOperation we need to know the PK name, but the engine currently doesn't provide it
		// We can generate a conventional drop or just put a TODO comment for the user to replace it.
		// Since we didn't add it in the domain yet, we'll output a placeholder.
		return fmt.Sprintf("-- TODO: Drop PK on [%s].[%s] (Name unknown)\nALTER TABLE [%s].[%s] DROP CONSTRAINT [PK_%s];", o.SchemaName, o.TableName, o.SchemaName, o.TableName, o.TableName), nil
	case diff.AddForeignKeyOperation:
		cols := make([]string, len(o.ForeignKey.Columns))
		for i, c := range o.ForeignKey.Columns {
			cols[i] = fmt.Sprintf("[%s]", c)
		}
		refCols := make([]string, len(o.ForeignKey.RefColumns))
		for i, c := range o.ForeignKey.RefColumns {
			refCols[i] = fmt.Sprintf("[%s]", c)
		}
		fkName := o.ForeignKey.Name
		if fkName == "" {
			fkName = fmt.Sprintf("FK_%s_%s", o.TableName, o.ForeignKey.RefTable)
		}
		return fmt.Sprintf("ALTER TABLE [%s].[%s] ADD CONSTRAINT [%s] FOREIGN KEY (%s) REFERENCES [%s].[%s] (%s);",
			o.SchemaName, o.TableName, fkName, strings.Join(cols, ", "), o.SchemaName, o.ForeignKey.RefTable, strings.Join(refCols, ", ")), nil
	case diff.DropForeignKeyOperation:
		return fmt.Sprintf("ALTER TABLE [%s].[%s] DROP CONSTRAINT [%s];", o.SchemaName, o.TableName, o.ForeignKeyName), nil
	case diff.CreateIndexOperation:
		cols := make([]string, len(o.Index.Columns))
		for i, c := range o.Index.Columns {
			cols[i] = fmt.Sprintf("[%s]", c)
		}
		unique := ""
		if o.Index.IsUnique {
			unique = "UNIQUE "
		}
		idxName := o.Index.Name
		if idxName == "" {
			idxName = fmt.Sprintf("IX_%s_%s", o.TableName, strings.Join(o.Index.Columns, "_"))
		}
		return fmt.Sprintf("CREATE %sINDEX [%s] ON [%s].[%s] (%s);", unique, idxName, o.SchemaName, o.TableName, strings.Join(cols, ", ")), nil
	case diff.DropIndexOperation:
		return fmt.Sprintf("DROP INDEX [%s] ON [%s].[%s];", o.IndexName, o.SchemaName, o.TableName), nil
	case diff.InsertDataOperation:
		return r.renderInsertData(o)
	case diff.UpdateDataOperation:
		return r.renderUpdateData(o)
	case diff.DeleteDataOperation:
		return r.renderDeleteData(o)
	default:
		return fmt.Sprintf("-- Unknown operation type: %T", op), nil
	}
}

func (r *MSSQLRenderer) renderInsertData(o diff.InsertDataOperation) (string, error) {
	var cols []string
	var vals []string

	// Sort columns to ensure deterministic output
	var keys []string
	for k := range o.Row {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		cols = append(cols, fmt.Sprintf("[%s]", k))
		vals = append(vals, formatValue(o.Row[k]))
	}

	return fmt.Sprintf("INSERT INTO [%s].[%s] (%s) VALUES (%s);",
		o.SchemaName, o.TableName, strings.Join(cols, ", "), strings.Join(vals, ", ")), nil
}

func (r *MSSQLRenderer) renderUpdateData(o diff.UpdateDataOperation) (string, error) {
	var sets []string

	var keys []string
	for k := range o.Updates {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		sets = append(sets, fmt.Sprintf("[%s] = %s", k, formatValue(o.Updates[k])))
	}

	var wheres []string
	var pkKeys []string
	for k := range o.PrimaryKey {
		pkKeys = append(pkKeys, k)
	}
	sort.Strings(pkKeys)

	for _, k := range pkKeys {
		wheres = append(wheres, fmt.Sprintf("[%s] = %s", k, formatValue(o.PrimaryKey[k])))
	}

	return fmt.Sprintf("UPDATE [%s].[%s] SET %s WHERE %s;",
		o.SchemaName, o.TableName, strings.Join(sets, ", "), strings.Join(wheres, " AND ")), nil
}

func (r *MSSQLRenderer) renderDeleteData(o diff.DeleteDataOperation) (string, error) {
	var wheres []string

	var pkKeys []string
	for k := range o.PrimaryKey {
		pkKeys = append(pkKeys, k)
	}
	sort.Strings(pkKeys)

	for _, k := range pkKeys {
		wheres = append(wheres, fmt.Sprintf("[%s] = %s", k, formatValue(o.PrimaryKey[k])))
	}

	return fmt.Sprintf("DELETE FROM [%s].[%s] WHERE %s;",
		o.SchemaName, o.TableName, strings.Join(wheres, " AND ")), nil
}

func formatValue(val interface{}) string {
	if val == nil {
		return "NULL"
	}
	switch v := val.(type) {
	case string:
		return fmt.Sprintf("N'%s'", strings.ReplaceAll(v, "'", "''"))
	case []byte:
		return fmt.Sprintf("0x%X", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func (r *MSSQLRenderer) renderCreateTable(o diff.CreateTableOperation) (string, error) {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("CREATE TABLE [%s].[%s] (\n", o.SchemaName, o.Table.Name))

	// Ensure columns are printed in a consistent order (e.g. sorted by name) for deterministic output,
	// though in a real database order matters. For now, we'll just iterate, maybe sort them if needed.
	colNames := make([]string, 0, len(o.Table.Columns))
	for name := range o.Table.Columns {
		colNames = append(colNames, name)
	}
	sort.Strings(colNames)

	for i, name := range colNames {
		col := o.Table.Columns[name]
		builder.WriteString(fmt.Sprintf("    %s", r.renderColumnDefinition(*col)))
		if i < len(colNames)-1 {
			builder.WriteString(",")
		}
		builder.WriteString("\n")
	}

	builder.WriteString(");")
	return builder.String(), nil
}

func (r *MSSQLRenderer) renderColumnDefinition(c schema.Column) string {
	typeStr := r.renderDataType(c.Type, c.Name)

	nullStr := "NOT NULL"
	if c.Nullable {
		nullStr = "NULL"
	}

	identityStr := ""
	if c.Identity {
		identityStr = " IDENTITY(1,1)"
	}

	defaultStr := ""
	if c.Default != nil {
		defaultStr = fmt.Sprintf(" DEFAULT %s", c.Default.Value)
	}

	if c.Computed {
		return fmt.Sprintf("[%s] AS %s", c.Name, c.ComputedExpr)
	}

	return fmt.Sprintf("[%s] %s %s%s%s", c.Name, typeStr, nullStr, identityStr, defaultStr)
}

func (r *MSSQLRenderer) renderDataType(dt schema.DataType, colName string) string {
	switch dt.Kind {
	case schema.TypeString:
		if dt.Length != nil && *dt.Length > 0 && *dt.Length <= 4000 {
			return fmt.Sprintf("NVARCHAR(%d)", *dt.Length)
		}
		return "NVARCHAR(MAX)"
	case schema.TypeInteger:
		return "INT"
	case schema.TypeDecimal:
		prec := 18
		if dt.Precision != nil {
			prec = *dt.Precision
		}
		scale := 0
		if dt.Scale != nil {
			scale = *dt.Scale
		}
		return fmt.Sprintf("DECIMAL(%d, %d)", prec, scale)
	case schema.TypeBoolean:
		return "BIT"
	case schema.TypeDateTime:
		return "DATETIME2"
	case schema.TypeUUID:
		return "UNIQUEIDENTIFIER"
	case schema.TypeBinary:
		if dt.Length != nil && *dt.Length > 0 && *dt.Length <= 8000 {
			return fmt.Sprintf("VARBINARY(%d)", *dt.Length)
		}
		return "VARBINARY(MAX)"
	case schema.TypeJSON:
		return fmt.Sprintf("NVARCHAR(MAX) CHECK (ISJSON([%s]) > 0)", colName)
	default:
		return "NVARCHAR(MAX)"
	}
}
