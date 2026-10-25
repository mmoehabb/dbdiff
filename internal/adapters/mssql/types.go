package mssql

import (
	"strings"

	"github.com/mmoehabb/dbdiff/internal/domain/schema"
)

func mapMSSQLTypeToDomain(sqlType string, maxLength int, precision int, scale int) schema.DataType {
	sqlType = strings.ToLower(sqlType)
	dt := schema.DataType{NativeType: sqlType}

	switch sqlType {
	case "varchar", "nvarchar", "char", "nchar", "text", "ntext":
		dt.Kind = schema.TypeString
		if maxLength > 0 {
			// For nvarchar/nchar, max_length is in bytes, so we divide by 2 for character length.
			if strings.HasPrefix(sqlType, "n") {
				l := maxLength / 2
				dt.Length = &l
			} else {
				dt.Length = &maxLength
			}
		} else if maxLength == -1 {
			// MAX
			l := -1
			dt.Length = &l
		}
	case "int", "bigint", "smallint", "tinyint":
		dt.Kind = schema.TypeInteger
	case "decimal", "numeric", "money", "smallmoney":
		dt.Kind = schema.TypeDecimal
		dt.Precision = &precision
		dt.Scale = &scale
	case "bit":
		dt.Kind = schema.TypeBoolean
	case "date":
		dt.Kind = schema.TypeDate
	case "datetime", "datetime2", "time", "smalldatetime", "datetimeoffset":
		dt.Scale = &scale
		dt.Kind = schema.TypeDateTime
	case "uniqueidentifier":
		dt.Kind = schema.TypeUUID
	case "binary", "varbinary", "image", "timestamp", "rowversion":
		dt.Kind = schema.TypeBinary
		if maxLength > 0 {
			dt.Length = &maxLength
		} else if maxLength == -1 {
			l := -1
			dt.Length = &l
		}
	case "float", "real":
		dt.Kind = schema.TypeDecimal
		dt.Precision = &precision
	default:
		// An empty kind is rejected by planning; never coerce unknown types to text.
	}

	return dt
}

func parseDefaultValue(val string) *schema.DefaultExpression {
	if val == "" {
		return nil
	}
	return &schema.DefaultExpression{Value: val}
}
