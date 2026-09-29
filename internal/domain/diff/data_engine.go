package diff

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/mmoehabb/dbdiff/internal/domain/schema"
)

type tableRef struct {
	Schema string
	Name   string
}

func parseRefTable(currentSchema, refTable string) tableRef {
	cleanRefTable := strings.ReplaceAll(refTable, "[", "")
	cleanRefTable = strings.ReplaceAll(cleanRefTable, "]", "")

	parts := strings.SplitN(cleanRefTable, ".", 2)
	if len(parts) == 1 {
		return tableRef{Schema: currentSchema, Name: parts[0]}
	}
	return tableRef{Schema: parts[0], Name: parts[1]}
}

func sortTableRefs(refs []tableRef) {
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Schema == refs[j].Schema {
			return refs[i].Name < refs[j].Name
		}
		return refs[i].Schema < refs[j].Schema
	})
}

func topologicalSortTables(tables map[tableRef]*schema.Table) []tableRef {
	inDegree := make(map[tableRef]int)
	adjList := make(map[tableRef][]tableRef)

	for ref := range tables {
		inDegree[ref] = 0
	}

	for ref, table := range tables {
		for _, fk := range table.ForeignKeys {
			refRef := parseRefTable(ref.Schema, fk.RefTable)

			if _, exists := tables[refRef]; exists {
				// Don't count self-referencing foreign keys for topological sort purposes
				if refRef != ref {
					adjList[refRef] = append(adjList[refRef], ref)
					inDegree[ref]++
				}
			}
		}
	}

	var queue []tableRef
	for ref, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, ref)
		}
	}

	sortTableRefs(queue)

	var sorted []tableRef
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]

		sorted = append(sorted, u)

		neighbors := adjList[u]
		sortTableRefs(neighbors)
		for _, v := range neighbors {
			inDegree[v]--
			if inDegree[v] == 0 {
				queue = append(queue, v)
			}
		}
	}

	if len(sorted) != len(tables) {
		var remaining []tableRef
		for ref, degree := range inDegree {
			if degree > 0 {
				remaining = append(remaining, ref)
			}
		}
		sortTableRefs(remaining)
		sorted = append(sorted, remaining...)
	}

	return sorted
}

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

	// 1. Collect all shared tables
	sharedTables := make(map[tableRef]*schema.Table)
	for schemaName, sSchema := range sourceSchema.Schemas {
		tSchema, targetHasSchema := targetSchema.Schemas[schemaName]
		if !targetHasSchema {
			continue // If target doesn't have the schema, we can't migrate data yet
		}

		for tableName, sTable := range sSchema.Tables {
			if _, targetHasTable := tSchema.Tables[tableName]; targetHasTable {
				ref := tableRef{Schema: schemaName, Name: tableName}
				sharedTables[ref] = sTable
			}
		}
	}

	// 2. Perform topological sort based on dependencies
	sortedRefs := topologicalSortTables(sharedTables)

	// 3. Process tables in sorted order
	for _, ref := range sortedRefs {
		schemaName := ref.Schema
		tableName := ref.Name
		sTable := sharedTables[ref]
		tTable := targetSchema.Schemas[schemaName].Tables[tableName]

		sourceData, err := d.sourceReader.ReadTableData(ctx, schemaName, sTable)
		if err != nil {
			return nil, fmt.Errorf("error reading source data for %s.%s: %w", schemaName, tableName, err)
		}

		targetData, err := d.targetReader.ReadTableData(ctx, schemaName, tTable)
		if err != nil {
			return nil, fmt.Errorf("error reading target data for %s.%s: %w", schemaName, tableName, err)
		}

		hasIdentity := false
		for _, col := range tTable.Columns {
			if col.Identity {
				hasIdentity = true
				break
			}
		}

		if sTable.PrimaryKey == nil || len(sTable.PrimaryKey.Columns) == 0 {
			// No primary key, generate INSERTS for all source data
			for _, row := range sourceData {
				dataOperations = append(dataOperations, InsertDataOperation{
					SchemaName:  schemaName,
					TableName:   tableName,
					Row:         row,
					HasIdentity: hasIdentity,
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
						SchemaName:  schemaName,
						TableName:   tableName,
						Row:         sRow,
						HasIdentity: hasIdentity,
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
