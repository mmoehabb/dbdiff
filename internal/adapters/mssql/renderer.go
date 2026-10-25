package mssql

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/mmoehabb/dbdiff/internal/domain/diff"
	"github.com/mmoehabb/dbdiff/internal/domain/schema"
)

// MSSQLRenderer implements the ports.Renderer interface for SQL Server.
type MSSQLRenderer struct{}

func NewMSSQLRenderer() *MSSQLRenderer {
	return &MSSQLRenderer{}
}

func (r *MSSQLRenderer) Render(ctx context.Context, plan *diff.MigrationPlan) (string, error) {
	if plan == nil {
		return "", fmt.Errorf("migration plan is required")
	}
	warnings := ""
	for _, d := range plan.Omitted {
		warnings += "-- Omitted " + strings.ReplaceAll(strings.ReplaceAll(d.ID+": "+d.Reason, "\r", " "), "\n", " ") + "\n"
	}
	var statements []string
	for _, op := range append(append([]diff.Operation{}, plan.SchemaOperations...), plan.DataOperations...) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		sql, err := r.renderOperation(op)
		if err != nil {
			return "", err
		}
		// Separate compilation scopes permit later statements to reference newly added columns.
		statements = append(statements, "EXEC sys.sp_executesql "+literal(sql)+";")
	}
	if len(statements) == 0 {
		return warnings, nil
	}
	return warnings + "IF @@TRANCOUNT <> 0 THROW 50000, 'Run migration outside an existing transaction', 1;\nSET XACT_ABORT ON;\nSET ANSI_NULLS ON;\nSET QUOTED_IDENTIFIER ON;\nSET ANSI_PADDING ON;\nSET ANSI_WARNINGS ON;\nSET ARITHABORT ON;\nSET CONCAT_NULL_YIELDS_NULL ON;\nSET NUMERIC_ROUNDABORT OFF;\nBEGIN TRY\nBEGIN TRANSACTION;\n\n" + strings.Join(statements, "\n\n") + "\n\nCOMMIT TRANSACTION;\nEND TRY\nBEGIN CATCH\nIF XACT_STATE() <> 0 ROLLBACK TRANSACTION;\nTHROW;\nEND CATCH;\n", nil
}

func quote(name string) string       { return "[" + strings.ReplaceAll(name, "]", "]]") + "]" }
func qualified(sn, tn string) string { return quote(sn) + "." + quote(tn) }
func literal(s string) string        { return "N'" + strings.ReplaceAll(s, "'", "''") + "'" }
func columns(names []string, descending []bool) string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = quote(n)
		if i < len(descending) && descending[i] {
			out[i] += " DESC"
		}
	}
	return strings.Join(out, ", ")
}
func (r *MSSQLRenderer) renderOperation(op diff.Operation) (string, error) {
	switch o := op.(type) {
	case diff.CreateSchemaOperation:
		return "CREATE SCHEMA " + quote(o.SchemaName) + ";", nil
	case diff.DropSchemaOperation:
		return "DROP SCHEMA " + quote(o.SchemaName) + ";", nil
	case diff.CreateTableOperation:
		return r.renderCreateTable(o)
	case diff.DropTableOperation:
		return "DROP TABLE " + qualified(o.SchemaName, o.TableName) + ";", nil
	case diff.AddColumnOperation:
		column, err := r.renderColumnDefinition(o.Column)
		if err != nil {
			return "", err
		}
		return "ALTER TABLE " + qualified(o.SchemaName, o.TableName) + " ADD " + column + ";", nil
	case diff.DropColumnOperation:
		sql := ""
		if o.DefaultName != "" {
			sql = "ALTER TABLE " + qualified(o.SchemaName, o.TableName) + " DROP CONSTRAINT " + quote(o.DefaultName) + ";\n"
		}
		return sql + "ALTER TABLE " + qualified(o.SchemaName, o.TableName) + " DROP COLUMN " + quote(o.ColumnName) + ";", nil
	case diff.AlterColumnOperation:
		table := qualified(o.SchemaName, o.TableName)
		sql := ""
		if o.OldDefaultName != "" {
			sql += "ALTER TABLE " + table + " DROP CONSTRAINT " + quote(o.OldDefaultName) + ";\n"
		}
		if o.OldColumn == nil || !reflect.DeepEqual(o.Column.Type, o.OldColumn.Type) || o.Column.Nullable != o.OldColumn.Nullable || o.Column.Collation != o.OldColumn.Collation {
			null := "NOT NULL"
			if o.Column.Nullable {
				null = "NULL"
			}
			collation, err := collationSQL(o.Column.Collation)
			if err != nil {
				return "", err
			}
			sql += "ALTER TABLE " + table + " ALTER COLUMN " + quote(o.Column.Name) + " " + r.renderDataType(o.Column.Type, o.Column.Name) + collation + " " + null + ";"
		}
		if o.Column.Default != nil {
			name := ""
			if o.Column.Default.Name != "" {
				name = "CONSTRAINT " + quote(o.Column.Default.Name) + " "
			}
			sql += "\nALTER TABLE " + table + " ADD " + name + "DEFAULT " + o.Column.Default.Value + " FOR " + quote(o.Column.Name) + ";"
		}
		return sql, nil
	case diff.AddPrimaryKeyOperation:
		name := o.PrimaryKey.Name
		if name == "" {
			name = "PK_" + o.TableName
		}
		kind := "NONCLUSTERED"
		if o.PrimaryKey.Clustered {
			kind = "CLUSTERED"
		}
		return "ALTER TABLE " + qualified(o.SchemaName, o.TableName) + " ADD CONSTRAINT " + quote(name) + " PRIMARY KEY " + kind + " (" + columns(o.PrimaryKey.Columns, o.PrimaryKey.Descending) + ");", nil
	case diff.DropPrimaryKeyOperation:
		if o.ConstraintName == "" {
			return "", fmt.Errorf("primary key name missing for %s.%s", o.SchemaName, o.TableName)
		}
		return "ALTER TABLE " + qualified(o.SchemaName, o.TableName) + " DROP CONSTRAINT " + quote(o.ConstraintName) + ";", nil
	case diff.AddForeignKeyOperation:
		ref, err := referenceName(o.SchemaName, o.ForeignKey.RefTable)
		if err != nil {
			return "", err
		}
		name := o.ForeignKey.Name
		if name == "" {
			name = "FK_" + o.TableName + "_" + strings.Join(o.ForeignKey.Columns, "_")
		}
		sql := "ALTER TABLE " + qualified(o.SchemaName, o.TableName) + " ADD CONSTRAINT " + quote(name) + " FOREIGN KEY (" + columns(o.ForeignKey.Columns, nil) + ") REFERENCES " + ref + " (" + columns(o.ForeignKey.RefColumns, nil) + ")"
		for _, action := range []struct{ keyword, value string }{{"DELETE", o.ForeignKey.OnDelete}, {"UPDATE", o.ForeignKey.OnUpdate}} {
			v := strings.ReplaceAll(strings.ToUpper(action.value), "_", " ")
			switch v {
			case "", "NO ACTION":
			case "CASCADE", "SET NULL", "SET DEFAULT":
				sql += " ON " + action.keyword + " " + v
			default:
				return "", fmt.Errorf("unsupported foreign key action %q", v)
			}
		}
		return sql + ";", nil
	case diff.DropForeignKeyOperation:
		return "ALTER TABLE " + qualified(o.SchemaName, o.TableName) + " DROP CONSTRAINT " + quote(o.ForeignKeyName) + ";", nil
	case diff.AddCheckOperation:
		return "ALTER TABLE " + qualified(o.SchemaName, o.TableName) + " ADD CONSTRAINT " + quote(o.Check.Name) + " CHECK " + o.Check.Definition + ";", nil
	case diff.DropCheckOperation:
		return "ALTER TABLE " + qualified(o.SchemaName, o.TableName) + " DROP CONSTRAINT " + quote(o.ConstraintName) + ";", nil
	case diff.CreateIndexOperation:
		return renderIndex(o)
	case diff.DropIndexOperation:
		if o.IsConstraint {
			return "ALTER TABLE " + qualified(o.SchemaName, o.TableName) + " DROP CONSTRAINT " + quote(o.IndexName) + ";", nil
		}
		return "DROP INDEX " + quote(o.IndexName) + " ON " + qualified(o.SchemaName, o.TableName) + ";", nil
	case diff.CreateViewOperation:
		return moduleSQL(o.View.Definition, o.View.UsesANSI_NULLS, o.View.UsesQuotedIdentifier, o.SchemaName, o.View.Name)
	case diff.AlterViewOperation:
		return moduleSQL(o.View.Definition, o.View.UsesANSI_NULLS, o.View.UsesQuotedIdentifier, o.SchemaName, o.View.Name)
	case diff.DropViewOperation:
		return "DROP VIEW " + qualified(o.SchemaName, o.ViewName) + ";", nil
	case diff.CreateProcedureOperation:
		return moduleSQL(o.Procedure.Definition, o.Procedure.UsesANSI_NULLS, o.Procedure.UsesQuotedIdentifier, o.SchemaName, o.Procedure.Name)
	case diff.AlterProcedureOperation:
		return moduleSQL(o.Procedure.Definition, o.Procedure.UsesANSI_NULLS, o.Procedure.UsesQuotedIdentifier, o.SchemaName, o.Procedure.Name)
	case diff.DropProcedureOperation:
		return "DROP PROCEDURE " + qualified(o.SchemaName, o.ProcedureName) + ";", nil
	case diff.CreateFunctionOperation:
		return moduleSQL(o.Function.Definition, o.Function.UsesANSI_NULLS, o.Function.UsesQuotedIdentifier, o.SchemaName, o.Function.Name)
	case diff.AlterFunctionOperation:
		return moduleSQL(o.Function.Definition, o.Function.UsesANSI_NULLS, o.Function.UsesQuotedIdentifier, o.SchemaName, o.Function.Name)
	case diff.DropFunctionOperation:
		return "DROP FUNCTION " + qualified(o.SchemaName, o.FunctionName) + ";", nil
	case diff.CreateTriggerOperation:
		return moduleSQL(o.Trigger.Definition, o.Trigger.UsesANSI_NULLS, o.Trigger.UsesQuotedIdentifier, o.SchemaName, o.Trigger.Name)
	case diff.AlterTriggerOperation:
		return moduleSQL(o.Trigger.Definition, o.Trigger.UsesANSI_NULLS, o.Trigger.UsesQuotedIdentifier, o.SchemaName, o.Trigger.Name)
	case diff.DropTriggerOperation:
		return "DROP TRIGGER " + qualified(o.SchemaName, o.TriggerName) + ";", nil
	case diff.CreateSynonymOperation:
		return "CREATE SYNONYM " + qualified(o.SchemaName, o.Synonym.Name) + " FOR " + o.Synonym.TargetObjectName + ";", nil
	case diff.DropSynonymOperation:
		return "DROP SYNONYM " + qualified(o.SchemaName, o.SynonymName) + ";", nil
	case diff.CreateSequenceOperation:
		return r.sequenceSQL(o.SchemaName, o.Sequence, false), nil
	case diff.AlterSequenceOperation:
		return r.sequenceSQL(o.SchemaName, o.Sequence, true), nil
	case diff.DropSequenceOperation:
		return "DROP SEQUENCE " + qualified(o.SchemaName, o.SequenceName) + ";", nil
	case diff.AddExtendedPropertyOperation:
		return propertySQL("add", o.Name, o.Value, o.Level)
	case diff.AlterExtendedPropertyOperation:
		return propertySQL("update", o.Name, o.Value, o.Level)
	case diff.DropExtendedPropertyOperation:
		return propertySQL("drop", o.Name, schema.PropertyValue{}, o.Level)
	case diff.InsertDataOperation:
		return r.renderInsertData(o)
	case diff.UpdateDataOperation:
		return r.renderUpdateData(o)
	case diff.DeleteDataOperation:
		return r.renderDeleteData(o)
	default:
		return "", fmt.Errorf("unsupported operation %T", op)
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
		cols = append(cols, quote(k))
		vals = append(vals, formatValue(o.Row[k]))
	}

	insertStmt := fmt.Sprintf("INSERT INTO [%s].[%s] (%s) VALUES (%s);",
		escapeName(o.SchemaName), escapeName(o.TableName), strings.Join(cols, ", "), strings.Join(vals, ", "))

	if o.HasIdentity {
		return fmt.Sprintf("SET IDENTITY_INSERT [%s].[%s] ON;\nBEGIN TRY\n%s\nSET IDENTITY_INSERT [%s].[%s] OFF;\nEND TRY\nBEGIN CATCH\nSET IDENTITY_INSERT [%s].[%s] OFF;\nTHROW;\nEND CATCH;",
			escapeName(o.SchemaName), escapeName(o.TableName), insertStmt, escapeName(o.SchemaName), escapeName(o.TableName), escapeName(o.SchemaName), escapeName(o.TableName)), nil
	}

	return insertStmt, nil
}

func (r *MSSQLRenderer) renderUpdateData(o diff.UpdateDataOperation) (string, error) {
	if len(o.PrimaryKey) == 0 || len(o.Updates) == 0 {
		return "", fmt.Errorf("update requires primary key and values")
	}
	var sets []string

	var keys []string
	for k := range o.Updates {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		sets = append(sets, fmt.Sprintf("%s = %s", quote(k), formatValue(o.Updates[k])))
	}

	var wheres []string
	var pkKeys []string
	for k := range o.PrimaryKey {
		pkKeys = append(pkKeys, k)
	}
	sort.Strings(pkKeys)

	for _, k := range pkKeys {
		wheres = append(wheres, fmt.Sprintf("%s = %s", quote(k), formatValue(o.PrimaryKey[k])))
	}

	return fmt.Sprintf("UPDATE [%s].[%s] SET %s WHERE %s;",
		escapeName(o.SchemaName), escapeName(o.TableName), strings.Join(sets, ", "), strings.Join(wheres, " AND ")), nil
}

func (r *MSSQLRenderer) renderDeleteData(o diff.DeleteDataOperation) (string, error) {
	if len(o.PrimaryKey) == 0 {
		return "", fmt.Errorf("delete requires primary key")
	}
	var wheres []string

	var pkKeys []string
	for k := range o.PrimaryKey {
		pkKeys = append(pkKeys, k)
	}
	sort.Strings(pkKeys)

	for _, k := range pkKeys {
		wheres = append(wheres, fmt.Sprintf("%s = %s", quote(k), formatValue(o.PrimaryKey[k])))
	}

	return fmt.Sprintf("DELETE FROM [%s].[%s] WHERE %s;",
		escapeName(o.SchemaName), escapeName(o.TableName), strings.Join(wheres, " AND ")), nil
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
	case schema.TemporalValue:
		return literal(v.Text)
	case time.Time:
		if v.Hour() == 0 && v.Minute() == 0 && v.Second() == 0 && v.Nanosecond() == 0 {
			return fmt.Sprintf("'%s'", v.Format("2006-01-02"))
		}
		return fmt.Sprintf("'%s'", v.Format("2006-01-02 15:04:05.9999999"))
	case bool:
		if v {
			return "1"
		}
		return "0"
	default:
		return fmt.Sprintf("%v", v)
	}
}

func (r *MSSQLRenderer) renderCreateTable(o diff.CreateTableOperation) (string, error) {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("CREATE TABLE [%s].[%s] (\n", escapeName(o.SchemaName), escapeName(o.Table.Name)))

	// Ensure columns are printed in a consistent order (e.g. sorted by name) for deterministic output,
	// though in a real database order matters. For now, we'll just iterate, maybe sort them if needed.
	colNames := make([]string, 0, len(o.Table.Columns))
	for name := range o.Table.Columns {
		colNames = append(colNames, name)
	}
	sort.Slice(colNames, func(i, j int) bool {
		a, b := o.Table.Columns[colNames[i]], o.Table.Columns[colNames[j]]
		if a.Ordinal != b.Ordinal {
			return a.Ordinal < b.Ordinal
		}
		return colNames[i] < colNames[j]
	})

	for i, name := range colNames {
		col := o.Table.Columns[name]
		column, err := r.renderColumnDefinition(*col)
		if err != nil {
			return "", err
		}
		builder.WriteString(fmt.Sprintf("    %s", column))
		if i < len(colNames)-1 {
			builder.WriteString(",")
		}
		builder.WriteString("\n")
	}

	builder.WriteString(");")
	return builder.String(), nil
}

func (r *MSSQLRenderer) renderColumnDefinition(c schema.Column) (string, error) {
	typeStr := r.renderDataType(c.Type, c.Name)

	nullStr := "NOT NULL"
	if c.Nullable {
		nullStr = "NULL"
	}

	identityStr := ""
	if c.Identity {
		seed, inc := c.IdentitySeed, c.IdentityIncrement
		if seed == "" {
			seed = "1"
		}
		if inc == "" {
			inc = "1"
		}
		identityStr = " IDENTITY(" + seed + "," + inc + ")"
	}

	defaultStr := ""
	if c.Default != nil {
		name := ""
		if c.Default.Name != "" {
			name = "CONSTRAINT " + quote(c.Default.Name) + " "
		}
		defaultStr = " " + name + "DEFAULT " + c.Default.Value
	}

	if c.Computed {
		persist := ""
		if c.Persisted {
			persist = " PERSISTED"
		}
		return quote(c.Name) + " AS " + c.ComputedExpr + persist, nil
	}

	collation, err := collationSQL(c.Collation)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s %s%s %s%s%s", quote(c.Name), typeStr, collation, nullStr, identityStr, defaultStr), nil
}

func (r *MSSQLRenderer) renderDataType(dt schema.DataType, colName string) string {
	if dt.NativeType != "" {
		n := strings.ToLower(dt.NativeType)
		switch n {
		case "varchar", "nvarchar", "char", "nchar", "binary", "varbinary":
			length := "MAX"
			if dt.Length != nil && *dt.Length > 0 {
				length = fmt.Sprint(*dt.Length)
			}
			return strings.ToUpper(n) + "(" + length + ")"
		case "decimal", "numeric":
			if dt.Precision != nil && dt.Scale != nil {
				return fmt.Sprintf("%s(%d, %d)", strings.ToUpper(n), *dt.Precision, *dt.Scale)
			}
		case "datetime2", "datetimeoffset", "time":
			if dt.Scale != nil {
				return fmt.Sprintf("%s(%d)", strings.ToUpper(n), *dt.Scale)
			}
		case "float":
			if dt.Precision != nil {
				return fmt.Sprintf("FLOAT(%d)", *dt.Precision)
			}
		}
		return strings.ToUpper(n)
	}
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
	case schema.TypeDate:
		return "DATE"
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

func escapeName(s string) string { return strings.ReplaceAll(s, "]", "]]") }
