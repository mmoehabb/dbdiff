package diff

import (
	"context"
	"fmt"
	"reflect"

	"github.com/mmoehabb/dbdiff/internal/domain/schema"
)

type DataReader interface {
	ReadTableData(ctx context.Context, schemaName string, table *schema.Table) ([]map[string]interface{}, error)
}

type DataDiffer struct {
	sourceReader DataReader
	targetReader DataReader
}

func NewDataDiffer(sourceReader, targetReader DataReader) *DataDiffer {
	return &DataDiffer{
		sourceReader: sourceReader,
		targetReader: targetReader,
	}
}

func (d *DataDiffer) CompareData(ctx context.Context, sourceSchema, targetSchema *schema.Database) ([]Operation, error) {
	var dataOperations []Operation

	for schemaName, sSchema := range sourceSchema.Schemas {
		tSchema, targetHasSchema := targetSchema.Schemas[schemaName]
		if !targetHasSchema {
			continue // If target doesn't have the schema, we can't migrate data yet (handled by SchemaOperations)
		}

		for tableName, sTable := range sSchema.Tables {
			tTable, targetHasTable := tSchema.Tables[tableName]
			if !targetHasTable {
				continue // If target doesn't have the table, wait for SchemaOperations to create it
			}

			sourceData, err := d.sourceReader.ReadTableData(ctx, schemaName, sTable)
			if err != nil {
				return nil, fmt.Errorf("error reading source data for %s.%s: %w", schemaName, tableName, err)
			}

			targetData, err := d.targetReader.ReadTableData(ctx, schemaName, tTable)
			if err != nil {
				return nil, fmt.Errorf("error reading target data for %s.%s: %w", schemaName, tableName, err)
			}

			if sTable.PrimaryKey == nil || len(sTable.PrimaryKey.Columns) == 0 {
				// No primary key, generate INSERTS for all source data
				for _, row := range sourceData {
					dataOperations = append(dataOperations, InsertDataOperation{
						SchemaName: schemaName,
						TableName:  tableName,
						Row:        row,
					})
				}
			} else {
				// We have a primary key, compare rows
				pkColumns := sTable.PrimaryKey.Columns

				// Index target data by PK
				targetDataIndex := make(map[string]map[string]interface{})
				for _, row := range targetData {
					pkKey := generatePKKey(pkColumns, row)
					targetDataIndex[pkKey] = row
				}

				// Compare source to target
				sourcePKs := make(map[string]bool)
				for _, sRow := range sourceData {
					pkKey := generatePKKey(pkColumns, sRow)
					sourcePKs[pkKey] = true

					tRow, exists := targetDataIndex[pkKey]
					if !exists {
						// Missing in target -> INSERT
						dataOperations = append(dataOperations, InsertDataOperation{
							SchemaName: schemaName,
							TableName:  tableName,
							Row:        sRow,
						})
					} else {
						// Exists in target -> Check for UPDATE
						updates := make(map[string]interface{})
						for colName, sVal := range sRow {
							tVal := tRow[colName]
							if !isEqual(sVal, tVal) {
								updates[colName] = sVal
							}
						}

						if len(updates) > 0 {
							pkMap := make(map[string]interface{})
							for _, col := range pkColumns {
								pkMap[col] = sRow[col]
							}
							dataOperations = append(dataOperations, UpdateDataOperation{
								SchemaName: schemaName,
								TableName:  tableName,
								PrimaryKey: pkMap,
								Updates:    updates,
							})
						}
					}
				}

				// Check target data for DELETE
				for pkKey, tRow := range targetDataIndex {
					if !sourcePKs[pkKey] {
						pkMap := make(map[string]interface{})
						for _, col := range pkColumns {
							pkMap[col] = tRow[col]
						}
						dataOperations = append(dataOperations, DeleteDataOperation{
							SchemaName: schemaName,
							TableName:  tableName,
							PrimaryKey: pkMap,
						})
					}
				}
			}
		}
	}

	return dataOperations, nil
}

func generatePKKey(pkColumns []string, row map[string]interface{}) string {
	var key string
	for _, col := range pkColumns {
		key += fmt.Sprintf("%v|", row[col])
	}
	return key
}

func isEqual(val1, val2 interface{}) bool {
	// Basic type-agnostic equality check. Might need refinement based on exact driver types.
	return reflect.DeepEqual(val1, val2)
}
