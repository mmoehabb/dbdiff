package diff

import (
	"github.com/mmoehabb/dbdiff/internal/domain/schema"
)

// SchemaDiffer implements ports.Differ
type SchemaDiffer struct{}

// NewSchemaDiffer creates a new instance of SchemaDiffer.
func NewSchemaDiffer() *SchemaDiffer {
	return &SchemaDiffer{}
}

// Compare compares the source and target databases and returns a MigrationPlan
// representing the operations required to transform target into source.
// Semantic rule: target + migration = source
func (d *SchemaDiffer) Compare(source, target *schema.Database) *MigrationPlan {
	plan := &MigrationPlan{
		SchemaOperations: []Operation{},
	}

	if source == nil || target == nil {
		return plan
	}

	// 1. Compare Schemas
	for schemaName, sourceSchema := range source.Schemas {
		targetSchema, exists := target.Schemas[schemaName]
		if !exists {
			// Create missing schema
			plan.SchemaOperations = append(plan.SchemaOperations, CreateSchemaOperation{
				SchemaName: schemaName,
			})
			// Since schema is missing, all tables in it must be created
			d.compareTables(sourceSchema, nil, plan)
		} else {
			// Compare tables within existing schema
			d.compareTables(sourceSchema, targetSchema, plan)
		}
	}

	for schemaName := range target.Schemas {
		if _, exists := source.Schemas[schemaName]; !exists {
			// Drop extra schema
			plan.SchemaOperations = append(plan.SchemaOperations, DropSchemaOperation{
				SchemaName: schemaName,
			})
		}
	}

	plan.SortOperations()
	return plan
}

func (d *SchemaDiffer) compareTables(sourceSchema, targetSchema *schema.Schema, plan *MigrationPlan) {
	if sourceSchema == nil {
		return
	}

	// Find tables to create or modify
	for tableName, sourceTable := range sourceSchema.Tables {
		var targetTable *schema.Table
		if targetSchema != nil {
			targetTable = targetSchema.Tables[tableName]
		}

		if targetTable == nil {
			// Create missing table

			// Strip PK, FKs, Indexes from the table used for CreateTableOperation
			// so they can be emitted as separate operations for dependency ordering
			tableForCreate := *sourceTable
			tableForCreate.PrimaryKey = nil
			tableForCreate.ForeignKeys = make(map[string]*schema.ForeignKey)
			tableForCreate.Indexes = make(map[string]*schema.Index)

			plan.SchemaOperations = append(plan.SchemaOperations, CreateTableOperation{
				SchemaName: sourceSchema.Name,
				Table:      tableForCreate,
			})

			// Compare against a dummy empty target table to emit AddPK, CreateIndex, AddFK operations
			dummyTarget := &schema.Table{
				Name:        sourceTable.Name,
				Columns:     make(map[string]*schema.Column),
				ForeignKeys: make(map[string]*schema.ForeignKey),
				Indexes:     make(map[string]*schema.Index),
			}
			d.comparePrimaryKeys(sourceSchema.Name, sourceTable, dummyTarget, plan)
			d.compareIndexes(sourceSchema.Name, sourceTable, dummyTarget, plan)
			d.compareForeignKeys(sourceSchema.Name, sourceTable, dummyTarget, plan)
		} else {
			// Compare existing table
			d.compareColumns(sourceSchema.Name, sourceTable, targetTable, plan)
			d.comparePrimaryKeys(sourceSchema.Name, sourceTable, targetTable, plan)
			d.compareIndexes(sourceSchema.Name, sourceTable, targetTable, plan)
			d.compareForeignKeys(sourceSchema.Name, sourceTable, targetTable, plan)
		}
	}

	// Find tables to drop
	if targetSchema != nil {
		for tableName := range targetSchema.Tables {
			if _, exists := sourceSchema.Tables[tableName]; !exists {
				plan.SchemaOperations = append(plan.SchemaOperations, DropTableOperation{
					SchemaName: sourceSchema.Name,
					TableName:  tableName,
				})
			}
		}
	}
}

func (d *SchemaDiffer) compareColumns(schemaName string, sourceTable, targetTable *schema.Table, plan *MigrationPlan) {
	for columnName, sourceCol := range sourceTable.Columns {
		targetCol, exists := targetTable.Columns[columnName]
		if !exists {
			// Add missing column
			plan.SchemaOperations = append(plan.SchemaOperations, AddColumnOperation{
				SchemaName: schemaName,
				TableName:  sourceTable.Name,
				Column:     *sourceCol,
			})
		} else {
			// Compare existing column
			if !d.isColumnEqual(sourceCol, targetCol) {
				plan.SchemaOperations = append(plan.SchemaOperations, AlterColumnOperation{
					SchemaName: schemaName,
					TableName:  sourceTable.Name,
					Column:     *sourceCol,
				})
			}
		}
	}

	for columnName := range targetTable.Columns {
		if _, exists := sourceTable.Columns[columnName]; !exists {
			// Drop extra column
			plan.SchemaOperations = append(plan.SchemaOperations, DropColumnOperation{
				SchemaName: schemaName,
				TableName:  sourceTable.Name,
				ColumnName: columnName,
			})
		}
	}
}

func (d *SchemaDiffer) isColumnEqual(sourceCol, targetCol *schema.Column) bool {
	if sourceCol.Type.Kind != targetCol.Type.Kind {
		return false
	}
	if !intPtrEqual(sourceCol.Type.Length, targetCol.Type.Length) {
		return false
	}
	if !intPtrEqual(sourceCol.Type.Precision, targetCol.Type.Precision) {
		return false
	}
	if !intPtrEqual(sourceCol.Type.Scale, targetCol.Type.Scale) {
		return false
	}
	if sourceCol.Nullable != targetCol.Nullable {
		return false
	}
	if sourceCol.Identity != targetCol.Identity {
		return false
	}
	if sourceCol.Computed != targetCol.Computed {
		return false
	}
	if sourceCol.ComputedExpr != targetCol.ComputedExpr {
		return false
	}
	if sourceCol.Default != nil && targetCol.Default != nil {
		if sourceCol.Default.Value != targetCol.Default.Value {
			return false
		}
	} else if sourceCol.Default != targetCol.Default {
		return false
	}
	return true
}

func (d *SchemaDiffer) comparePrimaryKeys(schemaName string, sourceTable, targetTable *schema.Table, plan *MigrationPlan) {
	if sourceTable.PrimaryKey != nil && targetTable.PrimaryKey == nil {
		plan.SchemaOperations = append(plan.SchemaOperations, AddPrimaryKeyOperation{
			SchemaName: schemaName,
			TableName:  sourceTable.Name,
			PrimaryKey: *sourceTable.PrimaryKey,
		})
	} else if sourceTable.PrimaryKey == nil && targetTable.PrimaryKey != nil {
		plan.SchemaOperations = append(plan.SchemaOperations, DropPrimaryKeyOperation{
			SchemaName: schemaName,
			TableName:  sourceTable.Name,
		})
	} else if sourceTable.PrimaryKey != nil && targetTable.PrimaryKey != nil {
		// Compare PKs
		if !d.isPrimaryKeyEqual(sourceTable.PrimaryKey, targetTable.PrimaryKey) {
			plan.SchemaOperations = append(plan.SchemaOperations, DropPrimaryKeyOperation{
				SchemaName: schemaName,
				TableName:  sourceTable.Name,
			})
			plan.SchemaOperations = append(plan.SchemaOperations, AddPrimaryKeyOperation{
				SchemaName: schemaName,
				TableName:  sourceTable.Name,
				PrimaryKey: *sourceTable.PrimaryKey,
			})
		}
	}
}

func (d *SchemaDiffer) isPrimaryKeyEqual(sourcePK, targetPK *schema.PrimaryKey) bool {
	if sourcePK.Name != targetPK.Name && (sourcePK.Name != "" && targetPK.Name != "") {
		// Strict name checking if both are provided, though in many engines PK names might be auto-generated.
		// For true semantic diff, compare columns. If columns match, it's the same PK.
		// We'll compare names if they matter, but definitely columns.
	}
	if len(sourcePK.Columns) != len(targetPK.Columns) {
		return false
	}
	for i, col := range sourcePK.Columns {
		if col != targetPK.Columns[i] {
			return false
		}
	}
	return true
}

func (d *SchemaDiffer) compareIndexes(schemaName string, sourceTable, targetTable *schema.Table, plan *MigrationPlan) {
	for indexName, sourceIdx := range sourceTable.Indexes {
		targetIdx, exists := targetTable.Indexes[indexName]
		if !exists {
			plan.SchemaOperations = append(plan.SchemaOperations, CreateIndexOperation{
				SchemaName: schemaName,
				TableName:  sourceTable.Name,
				Index:      *sourceIdx,
			})
		} else {
			if !d.isIndexEqual(sourceIdx, targetIdx) {
				plan.SchemaOperations = append(plan.SchemaOperations, DropIndexOperation{
					SchemaName: schemaName,
					TableName:  sourceTable.Name,
					IndexName:  indexName,
				})
				plan.SchemaOperations = append(plan.SchemaOperations, CreateIndexOperation{
					SchemaName: schemaName,
					TableName:  sourceTable.Name,
					Index:      *sourceIdx,
				})
			}
		}
	}

	for indexName := range targetTable.Indexes {
		if _, exists := sourceTable.Indexes[indexName]; !exists {
			plan.SchemaOperations = append(plan.SchemaOperations, DropIndexOperation{
				SchemaName: schemaName,
				TableName:  sourceTable.Name,
				IndexName:  indexName,
			})
		}
	}
}

func (d *SchemaDiffer) isIndexEqual(sourceIdx, targetIdx *schema.Index) bool {
	if sourceIdx.IsUnique != targetIdx.IsUnique {
		return false
	}
	if len(sourceIdx.Columns) != len(targetIdx.Columns) {
		return false
	}
	for i, col := range sourceIdx.Columns {
		if col != targetIdx.Columns[i] {
			return false
		}
	}
	return true
}

func (d *SchemaDiffer) compareForeignKeys(schemaName string, sourceTable, targetTable *schema.Table, plan *MigrationPlan) {
	for fkName, sourceFK := range sourceTable.ForeignKeys {
		targetFK, exists := targetTable.ForeignKeys[fkName]
		if !exists {
			plan.SchemaOperations = append(plan.SchemaOperations, AddForeignKeyOperation{
				SchemaName: schemaName,
				TableName:  sourceTable.Name,
				ForeignKey: *sourceFK,
			})
		} else {
			if !d.isForeignKeyEqual(sourceFK, targetFK) {
				plan.SchemaOperations = append(plan.SchemaOperations, DropForeignKeyOperation{
					SchemaName:     schemaName,
					TableName:      sourceTable.Name,
					ForeignKeyName: fkName,
				})
				plan.SchemaOperations = append(plan.SchemaOperations, AddForeignKeyOperation{
					SchemaName: schemaName,
					TableName:  sourceTable.Name,
					ForeignKey: *sourceFK,
				})
			}
		}
	}

	for fkName := range targetTable.ForeignKeys {
		if _, exists := sourceTable.ForeignKeys[fkName]; !exists {
			plan.SchemaOperations = append(plan.SchemaOperations, DropForeignKeyOperation{
				SchemaName:     schemaName,
				TableName:      sourceTable.Name,
				ForeignKeyName: fkName,
			})
		}
	}
}

func (d *SchemaDiffer) isForeignKeyEqual(sourceFK, targetFK *schema.ForeignKey) bool {
	if sourceFK.RefTable != targetFK.RefTable {
		return false
	}
	if sourceFK.OnUpdate != targetFK.OnUpdate {
		return false
	}
	if sourceFK.OnDelete != targetFK.OnDelete {
		return false
	}
	if len(sourceFK.Columns) != len(targetFK.Columns) {
		return false
	}
	for i, col := range sourceFK.Columns {
		if col != targetFK.Columns[i] {
			return false
		}
	}
	if len(sourceFK.RefColumns) != len(targetFK.RefColumns) {
		return false
	}
	for i, col := range sourceFK.RefColumns {
		if col != targetFK.RefColumns[i] {
			return false
		}
	}
	return true
}

func intPtrEqual(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
