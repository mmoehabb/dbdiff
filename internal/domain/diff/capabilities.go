package diff

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/mmoehabb/dbdiff/internal/domain/schema"
)

func tableAt(db *schema.Database, sn, tn string) *schema.Table {
	if s := db.Schemas[sn]; s != nil {
		return s.Tables[tn]
	}
	return nil
}

// Include modeled features too: their presence is safe, but their migration is
// not implemented by the SQL renderer.
func tableFeatures(t *schema.Table) []schema.Feature {
	if t == nil {
		return nil
	}
	fs := append([]schema.Feature{}, t.Features...)
	if t.IsSystemVersioned {
		fs = append(fs, schema.Feature{Kind: "temporal", Scope: "table", Detail: key(t.HistoryTable, t.ValidFromColumn, t.ValidToColumn)})
	}
	if t.IsReplicated {
		fs = append(fs, schema.Feature{Kind: "replication", Scope: "table", Detail: "enabled"})
	}
	if t.PartitionScheme != "" {
		fs = append(fs, schema.Feature{Kind: "partitioning", Scope: "table", Detail: key(t.PartitionScheme, t.PartitionColumn, t.FileGroup)})
	}
	for n, c := range t.Columns {
		if c.Type.Kind == "" {
			fs = append(fs, schema.Feature{Kind: "unknown column type", Scope: "column", Name: n, Detail: c.Type.NativeType})
		}
	}
	// Synthetic snapshots may not have catalog feature rows.
	for n, ix := range t.Indexes {
		if ix.Disabled {
			fs = append(fs, schema.Feature{Kind: "disabled index", Scope: "index", Name: n, Detail: "disabled"})
		}
	}
	sort.Slice(fs, func(i, j int) bool { return featureKey(fs[i]) < featureKey(fs[j]) })
	return fs
}
func featureKey(f schema.Feature) string { return key(f.Kind, f.Scope, f.Name, f.Detail) }
func featureSet(t *schema.Table) map[string]schema.Feature {
	out := map[string]schema.Feature{}
	for _, f := range tableFeatures(t) {
		normalized := f
		if t.PrimaryKey != nil && f.Scope == "index" && f.Name == t.PrimaryKey.Name {
			normalized.Name = "$primary_key"
		}
		out[featureKey(normalized)] = f
	}
	return out
}
func featureReason(side string, db *schema.Database, sn, tn string, f schema.Feature) string {
	return fmt.Sprintf("%s database %q, %s.%s: %s on %s %q (%s) requires an explicit migration", side, db.Name, sn, tn, f.Kind, f.Scope, f.Name, f.Detail)
}

// restrictCapabilities runs after Finalize so that a blocked rebuild removes its
// entire atomic group and all transitive dependants, including temporary drops.
func (p *MigrationPlan) restrictCapabilities(source, target *schema.Database) {
	blocked := map[string]string{}
	if p.BlockedTables == nil {
		p.BlockedTables = map[string]bool{}
	}
	// Unsupported standalone objects may have no candidate operation to attach
	// dependency edges to (for example, a new CLR function or user-defined type).
	// Block consumers of their missing/different definitions explicitly.
	unavailable := map[string]schema.ObjectFeature{}
	targetFeatures := map[string]bool{}
	for _, f := range target.Features {
		targetFeatures[key(f.Schema, f.Object, featureKey(f.Feature))] = true
	}
	for _, f := range source.Features {
		if !targetFeatures[key(f.Schema, f.Object, featureKey(f.Feature))] {
			k := key(f.Schema, f.Object)
			if previous, ok := unavailable[k]; !ok || featureKey(f.Feature) < featureKey(previous.Feature) {
				unavailable[k] = f
			}
		}
	}

	for _, op := range p.SchemaOperations {
		for _, dep := range source.Dependencies {
			if f, ok := unavailable[key(dep.RefSchema, dep.RefObject)]; ok && matchesObject(op, dep.Schema, dep.Object) {
				group := p.Step(op).GroupID
				reason := "blocked prerequisite: " + featureReason("source", source, f.Schema, f.Object, f.Feature)
				if blocked[group] == "" || reason < blocked[group] {
					blocked[group] = reason
				}
			}
		}

		if reason := operationRestriction(op, source, target); reason != "" {
			blocked[p.Step(op).GroupID] = reason
		}
	}

	for side, db := range []*schema.Database{source, target} {
		label := []string{"source", "target"}[side]
		features := append([]schema.ObjectFeature{}, db.Features...)
		sort.Slice(features, func(i, j int) bool {
			return key(features[i].Schema, features[i].Object, featureKey(features[i].Feature)) < key(features[j].Schema, features[j].Object, featureKey(features[j].Feature))
		})
		for _, op := range p.SchemaOperations {
			sn, tn := Scope(op)
			for _, f := range features {
				if f.Feature.Scope == "database" || matchesObject(op, f.Schema, f.Object) || (op.OperationType() == DropSchema && sn == f.Schema) {
					blocked[p.Step(op).GroupID] = featureReason(label, db, f.Schema, f.Object, f.Feature)
				}
			}
			switch op.OperationType() {
			case CreatePartitionFunction, AlterPartitionFunction, DropPartitionFunction, CreatePartitionScheme, AlterPartitionScheme, DropPartitionScheme, AlterTableProperties:
				blocked[p.Step(op).GroupID] = "partition, temporal, replication, or table storage changes require an explicit migration"
			}
			for _, f := range tableFeatures(tableAt(db, sn, tn)) {
				if featureBlocks(op, f, tableAt(db, sn, tn)) {
					blocked[p.Step(op).GroupID] = featureReason(label, db, sn, tn, f)
					break
				}
			}
		}
	}
	// Differences in unmodeled properties must remain visible even if they produce
	// no ordinary DDL operations. Compare sets, independent of catalog row order.
	refs := map[tableRef]bool{}
	for _, db := range []*schema.Database{source, target} {
		for sn, s := range db.Schemas {
			for tn := range s.Tables {
				refs[tableRef{sn, tn}] = true
			}
		}
	}
	ordered := make([]tableRef, 0, len(refs))
	for ref := range refs {
		ordered = append(ordered, ref)
	}
	sortTableRefs(ordered)
	for _, ref := range ordered {
		a, b := tableAt(source, ref.Schema, ref.Name), tableAt(target, ref.Schema, ref.Name)
		af, bf := featureSet(a), featureSet(b)
		if reflect.DeepEqual(af, bf) {
			continue
		}
		// The original names are diagnostic only (primary-key names may differ).
		same := len(af) == len(bf)
		for k := range af {
			if _, ok := bf[k]; !ok {
				same = false
			}
		}
		if same {
			continue
		}
		p.Incomplete = true
		p.BlockedTables[key(ref.Schema, ref.Name)] = true
		for side, fs := range []map[string]schema.Feature{af, bf} {
			other := bf
			if side == 1 {
				other = af
			}
			keys := make([]string, 0, len(fs))
			for k := range fs {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if _, ok := other[k]; ok {
					continue
				}
				p.Omitted = append(p.Omitted, Diagnostic{ID: "feature:" + key(ref.Schema, ref.Name) + ":" + k, Reason: featureReason([]string{"source", "target"}[side], []*schema.Database{source, target}[side], ref.Schema, ref.Name, fs[k])})
			}
		}
	}
	objectSet := func(db *schema.Database) map[string]schema.ObjectFeature {
		out := map[string]schema.ObjectFeature{}
		for _, f := range db.Features {
			out[key(f.Schema, f.Object, featureKey(f.Feature))] = f
		}
		return out
	}
	objectSets := []map[string]schema.ObjectFeature{objectSet(source), objectSet(target)}
	for side, fs := range objectSets {
		keys := make([]string, 0, len(fs))
		for k := range fs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if _, ok := objectSets[1-side][k]; ok {
				continue
			}
			f := fs[k]
			p.Incomplete = true
			p.Omitted = append(p.Omitted, Diagnostic{ID: "feature:" + k, Reason: featureReason([]string{"source", "target"}[side], []*schema.Database{source, target}[side], f.Schema, f.Object, f.Feature)})
		}
	}

	if len(blocked) > 0 {
		p.Incomplete = true
		p.omitGroups(blocked)
	}
}

func featureBlocks(op Operation, f schema.Feature, t *schema.Table) bool {
	typ := op.OperationType()
	switch typ {
	case AddExtendedProperty, AlterExtendedProperty, DropExtendedProperty:
		return false
	case CreateTable, DropTable:
		return true
	}
	switch f.Scope {
	case "column":
		switch o := op.(type) {
		case AddColumnOperation:
			return o.Column.Name == f.Name
		case AlterColumnOperation:
			return o.Column.Name == f.Name
		case DropColumnOperation:
			return o.ColumnName == f.Name
		}
		// An unknown type may also make an index or key rebuild unsafe.
		if f.Kind == "unknown column type" || f.Kind == "column storage or type" {
			return usesColumn(op, f.Name)
		}
		return false
	case "index":
		if f.Kind == "disabled index" {
			clustered := t.PrimaryKey != nil && t.PrimaryKey.Name == f.Name && t.PrimaryKey.Clustered
			if ix := t.Indexes[f.Name]; ix != nil {
				clustered = clustered || ix.Clustered
			}
			if clustered {
				return true
			}
		}
		if f.Kind == "specialized index" {
			switch typ {
			case AlterColumn, DropColumn, DropPrimaryKey, AddPrimaryKey:
				return true
			}
			if o, ok := op.(CreateIndexOperation); ok && o.Index.Clustered {
				return true
			}
			if o, ok := op.(DropIndexOperation); ok {
				if ix := t.Indexes[o.IndexName]; ix != nil && ix.Clustered {
					return true
				}
			}
		}
		switch o := op.(type) {
		case DropIndexOperation:
			return o.IndexName == f.Name
		case CreateIndexOperation:
			// A clustered index also changes heap/table storage.
			return o.Index.Name == f.Name || ((f.Kind == "filegroup" || f.Kind == "compression") && o.Index.Clustered)
		case DropPrimaryKeyOperation:
			return t.PrimaryKey != nil && t.PrimaryKey.Name == f.Name
		case AddPrimaryKeyOperation:
			return (t.PrimaryKey != nil && t.PrimaryKey.Name == f.Name) || (f.Name == "" && o.PrimaryKey.Clustered)
		case AlterColumnOperation, DropColumnOperation:
			return f.Name == "" // compressed/non-default heap
		}
		return false
	case "foreign_key":
		switch o := op.(type) {
		case DropForeignKeyOperation:
			return o.ForeignKeyName == f.Name
		case AddForeignKeyOperation:
			return o.ForeignKey.Name == f.Name
		}
		return false
	case "check":
		switch o := op.(type) {
		case DropCheckOperation:
			return o.ConstraintName == f.Name
		case AddCheckOperation:
			return o.Check.Name == f.Name
		}
		return false
	case "trigger":
		switch o := op.(type) {
		case DropTriggerOperation:
			return o.TriggerName == f.Name
		case CreateTriggerOperation:
			return o.Trigger.Name == f.Name
		case AlterTriggerOperation:
			return o.Trigger.Name == f.Name
		}
		return false
	case "table":
		if f.Kind == "full-text index" {
			switch typ {
			case AlterColumn, DropColumn, DropPrimaryKey, DropIndex:
				return true
			}
			return false
		}
		// Temporal, replication, security, graph, CDC and memory-optimized tables
		// need dedicated DDL rules. Unrelated tables still migrate normally.
		return true
	default:
		return true
	}
}

// Data synchronization has separate restrictions; successful schema inspection
// must never imply that special tables can be read and rewritten safely.
func dataFeatureReason(t *schema.Table) string {
	for _, f := range tableFeatures(t) {
		switch f.Kind {
		case "compression", "filegroup", "full-text index", "hypothetical index":
			continue
		case "primary-key options":
			if !strings.Contains(f.Detail, `"ignore_dup_key":true`) {
				continue
			}
		}
		return f.Kind + " on " + f.Scope + " " + f.Name
	}
	return ""
}

// Validate transitions against the original snapshots, after dependency rebuilds
// have been expanded into explicit operations.
func operationRestriction(op Operation, source, target *schema.Database) string {
	// Retained schema-bound references prevent ALTER as well as DROP FUNCTION.
	// Removed references are ordered by the planner; unsupported dependents such
	// as security policies remain until explicitly migrated.
	var functionName string
	switch o := op.(type) {
	case AlterFunctionOperation:
		functionName = o.Function.Name
	case DropFunctionOperation:
		functionName = o.FunctionName
	}
	if functionName != "" {
		sn, _ := Scope(op)
		reasons := []string{}
		for _, dep := range target.Dependencies {
			if !dep.SchemaBound || dep.RefSchema != sn || dep.RefObject != functionName {
				continue
			}
			retained := dep.ReferencingType == "SP"
			for _, f := range target.Features {
				retained = retained || (f.Schema == dep.Schema && f.Object == dep.Object)
			}
			for _, ref := range source.Dependencies {
				retained = retained || (ref.Schema == dep.Schema && ref.Object == dep.Object && ref.RefSchema == sn && ref.RefObject == functionName)
			}
			if retained {
				kind := "dependent"
				if dep.ReferencingType == "SP" {
					kind = "security policy"
				}
				reasons = append(reasons, fmt.Sprintf("target database %q: function %s.%s is referenced by schema-bound %s %s.%s; requires an explicit migration", target.Name, sn, functionName, kind, dep.Schema, dep.Object))
			}
		}
		if len(reasons) > 0 {
			sort.Strings(reasons)
			return reasons[0]
		}
	}

	sn, tn := Scope(op)
	before, after := tableAt(target, sn, tn), tableAt(source, sn, tn)
	var column string
	structural := false
	switch o := op.(type) {
	case AlterColumnOperation:
		column = o.Column.Name
		if before != nil && after != nil {
			a, b := before.Columns[column], after.Columns[column]
			if a != nil && b != nil {
				if a.Identity != b.Identity || a.IdentitySeed != b.IdentitySeed || a.IdentityIncrement != b.IdentityIncrement || a.Computed != b.Computed || a.ComputedExpr != b.ComputedExpr || a.Persisted != b.Persisted || a.Generated != b.Generated {
					return fmt.Sprintf("%s.%s.%s requires an identity/computed/generated column rebuild", sn, tn, column)
				}
				structural = !reflect.DeepEqual(a.Type, b.Type) || a.Collation != b.Collation || a.Nullable != b.Nullable
			}
		}
	case DropColumnOperation:
		column = o.ColumnName
		structural = true
	case CreateIndexOperation:
		if before != nil && before.Statistics[o.Index.Name] && before.Indexes[o.Index.Name] == nil {
			return fmt.Sprintf("index %s.%s.%s conflicts with existing standalone statistics", sn, tn, o.Index.Name)
		}
	case AlterFunctionOperation:
		if s := target.Schemas[sn]; s != nil {
			if prev := s.Functions[o.Function.Name]; prev != nil && prev.Kind != o.Function.Kind {
				return fmt.Sprintf("function %s.%s kind change requires an explicit drop/recreate migration", sn, o.Function.Name)
			}
		}
	case AlterSequenceOperation:
		if s := target.Schemas[sn]; s != nil {
			if prev := s.Sequences[o.Sequence.Name]; prev != nil && (prev.StartValue != o.Sequence.StartValue || !reflect.DeepEqual(prev.Type, o.Sequence.Type)) {
				return fmt.Sprintf("sequence %s.%s start/type change requires an explicit migration", sn, o.Sequence.Name)
			}
		}
	}
	if structural && before != nil {
		names := make([]string, 0, len(before.UserStatistics))
		for name := range before.UserStatistics {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			columns := before.UserStatistics[name]
			for _, c := range columns {
				if c == column {
					return fmt.Sprintf("column change on %s.%s.%s requires rebuilding standalone statistics %s", sn, tn, column, name)
				}
			}
		}
	}
	if structural || op.OperationType() == DropTable {
		reasons := []string{}
		for _, dep := range target.Dependencies {
			if !dep.SchemaBound || dep.RefSchema != sn || dep.RefObject != tn {
				continue
			}
			if op.OperationType() == DropTable {
				s := source.Schemas[dep.Schema]
				if s == nil || (s.Views[dep.Object] == nil && s.Functions[dep.Object] == nil && s.Tables[dep.Object] == nil) {
					continue
				}
			}
			reasons = append(reasons, fmt.Sprintf("change on %s.%s requires rebuilding schema-bound dependent %s.%s", sn, tn, dep.Schema, dep.Object))
		}
		if len(reasons) > 0 {
			sort.Strings(reasons)
			return reasons[0]
		}
	}
	return ""
}
