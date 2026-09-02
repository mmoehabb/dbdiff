package mssql

import (
	"strings"

	"github.com/mmoehabb/dbdiff/internal/domain/schema"
)

func mapMSSQLTypeToDomain(sqlType string, maxLength int, precision int, scale int) schema.DataType {
	sqlType = strings.ToLower(sqlType)
	dt := schema.DataType{}

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
	case "datetime", "datetime2", "date", "time", "smalldatetime", "datetimeoffset":
		dt.Kind = schema.TypeDateTime
	case "uniqueidentifier":
		dt.Kind = schema.TypeUUID
	case "binary", "varbinary", "image":
		dt.Kind = schema.TypeBinary
		if maxLength > 0 {
			dt.Length = &maxLength
		} else if maxLength == -1 {
			l := -1
			dt.Length = &l
		}
	default:
		// Fallback for unsupported types
		dt.Kind = schema.TypeString
	}

	return dt
}

func parseDefaultValue(val string) *schema.DefaultExpression {
	if val == "" {
		return nil
	}
	// Strip outer parentheses commonly added by SQL Server
	// e.g. ((0)) -> 0, ('abc') -> 'abc'
	for strings.HasPrefix(val, "(") && strings.HasSuffix(val, ")") {
		val = val[1 : len(val)-1]
	}
	return &schema.DefaultExpression{Value: val}
}
