package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	mssqldb "github.com/microsoft/go-mssqldb"
	"github.com/mmoehabb/dbdiff/internal/domain/schema"
)

type MSSQLDataReader struct {
	dsn string
}

func NewMSSQLDataReader(dsn string) *MSSQLDataReader {
	return &MSSQLDataReader{dsn: dsn}
}

func (r *MSSQLDataReader) ReadTableData(ctx context.Context, schemaName string, table *schema.Table) ([]map[string]interface{}, error) {
	db, err := sql.Open("sqlserver", r.dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	query := fmt.Sprintf("SELECT * FROM [%s].[%s]", escapeName(schemaName), escapeName(table.Name))
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query table data: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	var data []map[string]interface{}

	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range columns {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		row := make(map[string]interface{})
		for i, col := range columns {
			val := values[i]

			b, ok := val.([]byte)
			if ok {
				if c := table.Columns[col]; c != nil && c.Type.Kind == schema.TypeUUID {
					var id mssqldb.UniqueIdentifier
					if err := id.Scan(b); err != nil {
						return nil, fmt.Errorf("decode UUID %s: %w", col, err)
					}
					row[col] = id.String()
				} else if c := table.Columns[col]; c != nil && c.Type.Kind == schema.TypeBinary {
					row[col] = append([]byte(nil), b...)
				} else {
					row[col] = string(b)
				}
			} else {
				row[col] = val
			}
		}
		for name, col := range table.Columns {
			value := row[name]
			if tm, ok := value.(time.Time); ok {
				switch col.Type.NativeType {
				case "datetimeoffset":
					row[name] = schema.TemporalValue{Type: "datetimeoffset", Text: tm.Format("2006-01-02T15:04:05.9999999Z07:00")}
				case "time":
					row[name] = schema.TemporalValue{Type: "time", Text: tm.Format("15:04:05.9999999")}
				}
			}
			if text, ok := value.(string); ok && (col.Type.NativeType == "decimal" || col.Type.NativeType == "numeric" || col.Type.NativeType == "money" || col.Type.NativeType == "smallmoney") {
				if strings.Contains(text, ".") {
					text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
				}
				if text == "-0" {
					text = "0"
				}
				row[name] = text
			}
		}
		data = append(data, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return data, nil
}
