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

	// Compare Partition Functions
	for pfName, sourcePf := range source.PartitionFunctions {
		targetPf, exists := target.PartitionFunctions[pfName]
		if !exists {
			plan.SchemaOperations = append(plan.SchemaOperations, CreatePartitionFunctionOperation{PartitionFunction: *sourcePf})
		} else if !d.isPartitionFunctionEqual(sourcePf, targetPf) {
			plan.SchemaOperations = append(plan.SchemaOperations, AlterPartitionFunctionOperation{PartitionFunction: *sourcePf})
		}
	}
	if target != nil {
		for pfName := range target.PartitionFunctions {
			if _, exists := source.PartitionFunctions[pfName]; !exists {
				plan.SchemaOperations = append(plan.SchemaOperations, DropPartitionFunctionOperation{PartitionFunctionName: pfName})
			}
		}
	}

	// Compare Database Extended Properties
	for epName, sourceVal := range source.ExtendedProperties {
		targetVal, exists := target.ExtendedProperties[epName]
		if !exists {
			plan.SchemaOperations = append(plan.SchemaOperations, AddExtendedPropertyOperation{Name: epName, Value: sourceVal, Level: ExtendedPropertyLevel{}})
		} else if sourceVal != targetVal {
			plan.SchemaOperations = append(plan.SchemaOperations, AlterExtendedPropertyOperation{Name: epName, Value: sourceVal, Level: ExtendedPropertyLevel{}})
		}
	}
	if target != nil {
		for epName := range target.ExtendedProperties {
			if _, exists := source.ExtendedProperties[epName]; !exists {
				plan.SchemaOperations = append(plan.SchemaOperations, DropExtendedPropertyOperation{Name: epName, Level: ExtendedPropertyLevel{}})
			}
		}
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
			d.compareViews(sourceSchema, nil, plan)
			d.compareProcedures(sourceSchema, nil, plan)
			d.compareFunctions(sourceSchema, nil, plan)
			d.compareSynonyms(sourceSchema, nil, plan)
			d.compareSequences(sourceSchema, nil, plan)
			d.comparePartitionSchemes(sourceSchema, nil, plan)
		} else {
			// Compare tables within existing schema
			d.compareTables(sourceSchema, targetSchema, plan)
			d.compareViews(sourceSchema, targetSchema, plan)
			d.compareProcedures(sourceSchema, targetSchema, plan)
			d.compareFunctions(sourceSchema, targetSchema, plan)
			d.compareSynonyms(sourceSchema, targetSchema, plan)
			d.compareSequences(sourceSchema, targetSchema, plan)
			d.comparePartitionSchemes(sourceSchema, targetSchema, plan)
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
				Triggers:    make(map[string]*schema.Trigger),
			}
			d.comparePrimaryKeys(sourceSchema.Name, sourceTable, dummyTarget, plan)
			d.compareIndexes(sourceSchema.Name, sourceTable, dummyTarget, plan)
			d.compareForeignKeys(sourceSchema.Name, sourceTable, dummyTarget, plan)
			d.compareTriggers(sourceSchema.Name, sourceTable, dummyTarget, plan)
			d.compareTableExtendedProperties(sourceSchema.Name, sourceTable, dummyTarget, plan)
		} else {
			// Compare existing table
			d.compareColumns(sourceSchema.Name, sourceTable, targetTable, plan)
			d.comparePrimaryKeys(sourceSchema.Name, sourceTable, targetTable, plan)
			d.compareIndexes(sourceSchema.Name, sourceTable, targetTable, plan)
			d.compareForeignKeys(sourceSchema.Name, sourceTable, targetTable, plan)
			d.compareTriggers(sourceSchema.Name, sourceTable, targetTable, plan)
			d.compareTableExtendedProperties(sourceSchema.Name, sourceTable, targetTable, plan)

			if sourceTable.IsSystemVersioned != targetTable.IsSystemVersioned || sourceTable.IsReplicated != targetTable.IsReplicated || sourceTable.PartitionScheme != targetTable.PartitionScheme || sourceTable.PartitionColumn != targetTable.PartitionColumn || sourceTable.FileGroup != targetTable.FileGroup {
				plan.SchemaOperations = append(plan.SchemaOperations, AlterTablePropertiesOperation{
					SchemaName: sourceSchema.Name,
					TableName:  sourceTable.Name,
					Table:      *sourceTable,
				})
			}
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

func (d *SchemaDiffer) compareViews(sourceSchema, targetSchema *schema.Schema, plan *MigrationPlan) {
	if sourceSchema == nil {
		return
	}
	for viewName, sourceView := range sourceSchema.Views {
		var targetView *schema.View
		if targetSchema != nil {
			targetView = targetSchema.Views[viewName]
		}
		if targetView == nil {
			plan.SchemaOperations = append(plan.SchemaOperations, CreateViewOperation{SchemaName: sourceSchema.Name, View: *sourceView})
		} else if sourceView.Definition != targetView.Definition {
			plan.SchemaOperations = append(plan.SchemaOperations, AlterViewOperation{SchemaName: sourceSchema.Name, View: *sourceView})
		}
	}
	if targetSchema != nil {
		for viewName := range targetSchema.Views {
			if _, exists := sourceSchema.Views[viewName]; !exists {
				plan.SchemaOperations = append(plan.SchemaOperations, DropViewOperation{SchemaName: sourceSchema.Name, ViewName: viewName})
			}
		}
	}
}

func (d *SchemaDiffer) compareProcedures(sourceSchema, targetSchema *schema.Schema, plan *MigrationPlan) {
	if sourceSchema == nil {
		return
	}
	for procName, sourceProc := range sourceSchema.Procedures {
		var targetProc *schema.Procedure
		if targetSchema != nil {
			targetProc = targetSchema.Procedures[procName]
		}
		if targetProc == nil {
			plan.SchemaOperations = append(plan.SchemaOperations, CreateProcedureOperation{SchemaName: sourceSchema.Name, Procedure: *sourceProc})
		} else if sourceProc.Definition != targetProc.Definition {
			plan.SchemaOperations = append(plan.SchemaOperations, AlterProcedureOperation{SchemaName: sourceSchema.Name, Procedure: *sourceProc})
		}
	}
	if targetSchema != nil {
		for procName := range targetSchema.Procedures {
			if _, exists := sourceSchema.Procedures[procName]; !exists {
				plan.SchemaOperations = append(plan.SchemaOperations, DropProcedureOperation{SchemaName: sourceSchema.Name, ProcedureName: procName})
			}
		}
	}
}

func (d *SchemaDiffer) compareFunctions(sourceSchema, targetSchema *schema.Schema, plan *MigrationPlan) {
	if sourceSchema == nil {
		return
	}
	for funcName, sourceFunc := range sourceSchema.Functions {
		var targetFunc *schema.Function
		if targetSchema != nil {
			targetFunc = targetSchema.Functions[funcName]
		}
		if targetFunc == nil {
			plan.SchemaOperations = append(plan.SchemaOperations, CreateFunctionOperation{SchemaName: sourceSchema.Name, Function: *sourceFunc})
		} else if sourceFunc.Definition != targetFunc.Definition {
			plan.SchemaOperations = append(plan.SchemaOperations, AlterFunctionOperation{SchemaName: sourceSchema.Name, Function: *sourceFunc})
		}
	}
	if targetSchema != nil {
		for funcName := range targetSchema.Functions {
			if _, exists := sourceSchema.Functions[funcName]; !exists {
				plan.SchemaOperations = append(plan.SchemaOperations, DropFunctionOperation{SchemaName: sourceSchema.Name, FunctionName: funcName})
			}
		}
	}
}

func (d *SchemaDiffer) compareSynonyms(sourceSchema, targetSchema *schema.Schema, plan *MigrationPlan) {
	if sourceSchema == nil {
		return
	}
	for synName, sourceSyn := range sourceSchema.Synonyms {
		var targetSyn *schema.Synonym
		if targetSchema != nil {
			targetSyn = targetSchema.Synonyms[synName]
		}
		if targetSyn == nil {
			plan.SchemaOperations = append(plan.SchemaOperations, CreateSynonymOperation{SchemaName: sourceSchema.Name, Synonym: *sourceSyn})
		} else if sourceSyn.TargetObjectName != targetSyn.TargetObjectName {
			plan.SchemaOperations = append(plan.SchemaOperations, DropSynonymOperation{SchemaName: sourceSchema.Name, SynonymName: synName})
			plan.SchemaOperations = append(plan.SchemaOperations, CreateSynonymOperation{SchemaName: sourceSchema.Name, Synonym: *sourceSyn})
		}
	}
	if targetSchema != nil {
		for synName := range targetSchema.Synonyms {
			if _, exists := sourceSchema.Synonyms[synName]; !exists {
				plan.SchemaOperations = append(plan.SchemaOperations, DropSynonymOperation{SchemaName: sourceSchema.Name, SynonymName: synName})
			}
		}
	}
}

func (d *SchemaDiffer) compareSequences(sourceSchema, targetSchema *schema.Schema, plan *MigrationPlan) {
	if sourceSchema == nil {
		return
	}
	for seqName, sourceSeq := range sourceSchema.Sequences {
		var targetSeq *schema.Sequence
		if targetSchema != nil {
			targetSeq = targetSchema.Sequences[seqName]
		}
		if targetSeq == nil {
			plan.SchemaOperations = append(plan.SchemaOperations, CreateSequenceOperation{SchemaName: sourceSchema.Name, Sequence: *sourceSeq})
		} else if !d.isSequenceEqual(sourceSeq, targetSeq) {
			plan.SchemaOperations = append(plan.SchemaOperations, AlterSequenceOperation{SchemaName: sourceSchema.Name, Sequence: *sourceSeq})
		}
	}
	if targetSchema != nil {
		for seqName := range targetSchema.Sequences {
			if _, exists := sourceSchema.Sequences[seqName]; !exists {
				plan.SchemaOperations = append(plan.SchemaOperations, DropSequenceOperation{SchemaName: sourceSchema.Name, SequenceName: seqName})
			}
		}
	}
}

func (d *SchemaDiffer) isSequenceEqual(s1, s2 *schema.Sequence) bool {
	return s1.StartValue == s2.StartValue && s1.Increment == s2.Increment && s1.MinValue == s2.MinValue && s1.MaxValue == s2.MaxValue && s1.IsCycling == s2.IsCycling && s1.CacheSize == s2.CacheSize
}

func (d *SchemaDiffer) comparePartitionSchemes(sourceSchema, targetSchema *schema.Schema, plan *MigrationPlan) {
	if sourceSchema == nil {
		return
	}
	for psName, sourcePs := range sourceSchema.PartitionSchemes {
		var targetPs *schema.PartitionScheme
		if targetSchema != nil {
			targetPs = targetSchema.PartitionSchemes[psName]
		}
		if targetPs == nil {
			plan.SchemaOperations = append(plan.SchemaOperations, CreatePartitionSchemeOperation{SchemaName: sourceSchema.Name, PartitionScheme: *sourcePs})
		} else if !d.isPartitionSchemeEqual(sourcePs, targetPs) {
			plan.SchemaOperations = append(plan.SchemaOperations, DropPartitionSchemeOperation{SchemaName: sourceSchema.Name, PartitionSchemeName: psName})
			plan.SchemaOperations = append(plan.SchemaOperations, CreatePartitionSchemeOperation{SchemaName: sourceSchema.Name, PartitionScheme: *sourcePs})
		}
	}
	if targetSchema != nil {
		for psName := range targetSchema.PartitionSchemes {
			if _, exists := sourceSchema.PartitionSchemes[psName]; !exists {
				plan.SchemaOperations = append(plan.SchemaOperations, DropPartitionSchemeOperation{SchemaName: sourceSchema.Name, PartitionSchemeName: psName})
			}
		}
	}
}

func (d *SchemaDiffer) isPartitionSchemeEqual(ps1, ps2 *schema.PartitionScheme) bool {
	if ps1.PartitionFunction != ps2.PartitionFunction {
		return false
	}
	if len(ps1.FileGroups) != len(ps2.FileGroups) {
		return false
	}
	for i, fg := range ps1.FileGroups {
		if fg != ps2.FileGroups[i] {
			return false
		}
	}
	return true
}

func (d *SchemaDiffer) isPartitionFunctionEqual(pf1, pf2 *schema.PartitionFunction) bool {
	if pf1.InputParameterType != pf2.InputParameterType {
		return false
	}
	if len(pf1.BoundaryValues) != len(pf2.BoundaryValues) {
		return false
	}
	for i, bv := range pf1.BoundaryValues {
		if bv != pf2.BoundaryValues[i] {
			return false
		}
	}
	return true
}

func (d *SchemaDiffer) compareTriggers(schemaName string, sourceTable, targetTable *schema.Table, plan *MigrationPlan) {
	for trigName, sourceTrig := range sourceTable.Triggers {
		var targetTrig *schema.Trigger
		if targetTable != nil && targetTable.Triggers != nil {
			targetTrig = targetTable.Triggers[trigName]
		}
		if targetTrig == nil {
			plan.SchemaOperations = append(plan.SchemaOperations, CreateTriggerOperation{SchemaName: schemaName, TableName: sourceTable.Name, Trigger: *sourceTrig})
		} else if sourceTrig.Definition != targetTrig.Definition {
			plan.SchemaOperations = append(plan.SchemaOperations, AlterTriggerOperation{SchemaName: schemaName, TableName: sourceTable.Name, Trigger: *sourceTrig})
		}
	}
	if targetTable != nil && targetTable.Triggers != nil {
		for trigName := range targetTable.Triggers {
			if _, exists := sourceTable.Triggers[trigName]; !exists {
				plan.SchemaOperations = append(plan.SchemaOperations, DropTriggerOperation{SchemaName: schemaName, TableName: sourceTable.Name, TriggerName: trigName})
			}
		}
	}
}

func (d *SchemaDiffer) compareTableExtendedProperties(schemaName string, sourceTable, targetTable *schema.Table, plan *MigrationPlan) {
	for epName, sourceVal := range sourceTable.ExtendedProperties {
		targetVal, exists := targetTable.ExtendedProperties[epName]
		level := ExtendedPropertyLevel{Level0Type: "SCHEMA", Level0Name: schemaName, Level1Type: "TABLE", Level1Name: sourceTable.Name}
		if !exists {
			plan.SchemaOperations = append(plan.SchemaOperations, AddExtendedPropertyOperation{Name: epName, Value: sourceVal, Level: level})
		} else if sourceVal != targetVal {
			plan.SchemaOperations = append(plan.SchemaOperations, AlterExtendedPropertyOperation{Name: epName, Value: sourceVal, Level: level})
		}
	}
	if targetTable != nil {
		for epName := range targetTable.ExtendedProperties {
			if _, exists := sourceTable.ExtendedProperties[epName]; !exists {
				level := ExtendedPropertyLevel{Level0Type: "SCHEMA", Level0Name: schemaName, Level1Type: "TABLE", Level1Name: sourceTable.Name}
				plan.SchemaOperations = append(plan.SchemaOperations, DropExtendedPropertyOperation{Name: epName, Level: level})
			}
		}
	}
}
