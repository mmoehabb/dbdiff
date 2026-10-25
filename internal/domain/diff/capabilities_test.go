package diff

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mmoehabb/dbdiff/internal/domain/schema"
)

func TestSpecialFeaturesPermitUnchangedAndIndependentChanges(t *testing.T) {
	features := []schema.Feature{
		{Kind: "primary-key options", Scope: "index", Name: "PK", Detail: `{"fill_factor":80}`},
		{Kind: "compression", Scope: "index", Name: "IX_ArticleViews_ArticleId", Detail: "1:PAGE"},
		{Kind: "filegroup", Scope: "index", Name: "IX_ArticleViews_ArticleId", Detail: "Archive"},
		{Kind: "constraint state", Scope: "check", Name: "CK", Detail: "untrusted"},
		{Kind: "column storage or type", Scope: "column", Name: "ArticleId", Detail: "sparse"},
		{Kind: "dynamic data masking", Scope: "column", Name: "ArticleId", Detail: "default()"},
		{Kind: "full-text index", Scope: "table", Detail: "catalog"},
		{Kind: "temporal", Scope: "table", Detail: "history"},
	}
	for _, feature := range features {
		t.Run(feature.Kind, func(t *testing.T) {
			target := fixture()
			target.Name = "dev"
			target.Schemas["dbo"].Tables["ArticleViews"].Features = []schema.Feature{feature}
			source := cloneDB(target)
			if p := compareTest(t, source, target); p.Incomplete || len(p.Omitted) != 0 || len(p.SchemaOperations) != 0 {
				t.Fatalf("unchanged feature rejected: %+v", p)
			}
			source.Schemas["dbo"].Tables["Other"] = &schema.Table{Name: "Other", Columns: map[string]*schema.Column{"Id": {Name: "Id", Type: schema.DataType{Kind: schema.TypeInteger}}}}
			p := compareTest(t, source, target)
			if p.Incomplete || len(p.SchemaOperations) != 1 || p.SchemaOperations[0].OperationType() != CreateTable {
				t.Fatalf("independent change rejected: %+v", p)
			}
			delete(source.Schemas["dbo"].Tables, "Other")
			source.Schemas["dbo"].Tables["ArticleViews"].Columns["Extra"] = &schema.Column{Name: "Extra", Nullable: true, Type: schema.DataType{Kind: schema.TypeInteger}}
			p = compareTest(t, source, target)
			if feature.Kind == "temporal" {
				if !p.Incomplete || len(p.SchemaOperations) != 0 {
					t.Fatalf("special DDL allowed: %+v", p)
				}
			} else if p.Incomplete || len(p.SchemaOperations) != 1 {
				t.Fatalf("unrelated column addition rejected: %+v", p)
			}
		})
	}
}

func TestBlockedIndexRebuildRetainsAtomicGroupAndIndependentTable(t *testing.T) {
	target := fixture()
	target.Name = "dev"
	target.Schemas["dbo"].Tables["ArticleViews"].Features = []schema.Feature{{Kind: "compression", Scope: "index", Name: "IX_ArticleViews_ArticleId", Detail: "1:PAGE"}}
	source := cloneDB(target)
	source.Schemas["dbo"].Tables["ArticleViews"].Columns["ArticleId"].Type.NativeType = "bigint"
	source.Schemas["dbo"].Tables["Other"] = &schema.Table{Name: "Other", Columns: map[string]*schema.Column{"Id": {Name: "Id", Type: schema.DataType{Kind: schema.TypeInteger}}}}
	p := compareTest(t, source, target)
	p.ApplySafety(true)
	if !p.Incomplete || len(p.SchemaOperations) != 1 || len(p.Omitted) != 3 || !p.BlockedTables[key("dbo", "ArticleViews")] {
		t.Fatalf("partial rebuild leaked: %+v", p)
	}
	for _, d := range p.Omitted {
		if !strings.Contains(d.Reason, "compression") || !strings.Contains(d.Reason, `database "dev"`) {
			t.Fatalf("imprecise diagnostic: %+v", d)
		}
	}
}

func TestFeatureOnlyDifferenceIsVisibleAndDeterministic(t *testing.T) {
	target := fixture()
	source := cloneDB(target)
	target.Schemas["dbo"].Tables["ArticleViews"].Features = []schema.Feature{{Kind: "compression", Scope: "index", Name: "IX", Detail: "1:ROW"}}
	source.Schemas["dbo"].Tables["ArticleViews"].Features = []schema.Feature{{Kind: "compression", Scope: "index", Name: "IX", Detail: "1:PAGE"}}
	var previous string
	for i := 0; i < 20; i++ {
		p := compareTest(t, source, target)
		if !p.Incomplete || len(p.Omitted) != 2 || len(p.SchemaOperations) != 0 {
			t.Fatalf("feature difference lost: %+v", p)
		}
		b, _ := json.Marshal(p)
		if i > 0 && string(b) != previous {
			t.Fatal("nondeterministic diagnostics")
		}
		previous = string(b)
	}
}

func TestBlockedForeignKeyRebuildKeepsParentKeyAndDependants(t *testing.T) {
	target := fixture()
	parent := target.Schemas["dbo"].Tables["ArticleViews"]
	parent.PrimaryKey = &schema.PrimaryKey{Name: "PK", Columns: []string{"ArticleId"}}
	target.Schemas["dbo"].Tables["Child"] = &schema.Table{Name: "Child", Columns: map[string]*schema.Column{"Id": {Name: "Id", Type: schema.DataType{Kind: schema.TypeInteger}}}, ForeignKeys: map[string]*schema.ForeignKey{"FK": {Name: "FK", Columns: []string{"Id"}, RefTable: "dbo.ArticleViews", RefColumns: []string{"ArticleId"}}}, Features: []schema.Feature{{Kind: "constraint state", Scope: "foreign_key", Name: "FK", Detail: "untrusted"}}}
	source := cloneDB(target)
	source.Schemas["dbo"].Tables["ArticleViews"].PrimaryKey.Descending = []bool{true}
	p := compareTest(t, source, target)
	p.ApplySafety(true)
	if !p.Incomplete || len(p.SchemaOperations) != 0 || len(p.Omitted) != 4 {
		t.Fatalf("constraint temporarily dropped despite blocked rebuild: %+v", p)
	}
}

func TestSpecialDataIsSkippedBeforeReading(t *testing.T) {
	for _, kind := range []string{"temporal", "replication", "row-level security", "dynamic data masking", "disabled index", "constraint state", "trigger state", "column storage or type"} {
		t.Run(kind, func(t *testing.T) {
			source := dataFixture()
			target := cloneDB(source)
			target.Schemas["dbo"].Tables["ArticleViews"].Features = []schema.Feature{{Kind: kind, Scope: "table", Detail: "enabled"}}
			sr, tr := &memoryReader{}, &memoryReader{}
			d := NewDataDiffer(sr, tr)
			ops, err := d.CompareData(context.Background(), source, target)
			if err != nil || len(ops) != 0 || len(sr.calls) != 0 || len(tr.calls) != 0 || len(d.OmittedTables) != 1 {
				t.Fatalf("special data was read: %v %+v", err, d)
			}
			if !strings.Contains(d.OmittedReasons[key("dbo", "ArticleViews")], kind) {
				t.Fatal("missing data omission reason")
			}
		})
	}
}

func TestUnchangedPartitionAndIndexedViewPermitIndependentChanges(t *testing.T) {
	target := fixture()
	target.PartitionFunctions = map[string]*schema.PartitionFunction{"PF": {Name: "PF", InputParameterType: "int", BoundaryValues: []string{"10"}}}
	target.Features = []schema.ObjectFeature{{Schema: "dbo", Object: "Summary", Feature: schema.Feature{Kind: "indexed view", Scope: "view", Detail: "clustered"}}}
	source := cloneDB(target)
	source.Schemas["dbo"].Tables["ArticleViews"].Columns["Extra"] = &schema.Column{Name: "Extra", Type: schema.DataType{Kind: schema.TypeInteger}, Nullable: true}
	p := compareTest(t, source, target)
	if p.Incomplete || len(p.SchemaOperations) != 1 {
		t.Fatalf("unchanged standalone feature blocked unrelated table: %+v", p)
	}
	source.PartitionFunctions["PF"].BoundaryValues = []string{"20"}
	p = compareTest(t, source, target)
	if !p.Incomplete || len(p.SchemaOperations) != 1 || len(p.Omitted) != 1 {
		t.Fatalf("partition change was not isolated: %+v", p)
	}
}

func TestSpecializedIndexesBlockKeyAndColumnChanges(t *testing.T) {
	for _, change := range []string{"primary key", "column"} {
		t.Run(change, func(t *testing.T) {
			target := fixture()
			table := target.Schemas["dbo"].Tables["ArticleViews"]
			table.PrimaryKey = &schema.PrimaryKey{Name: "PK", Columns: []string{"ArticleId"}, Clustered: true}
			table.Features = []schema.Feature{{Kind: "specialized index", Scope: "index", Name: "XML_Index", Detail: "XML"}}
			source := cloneDB(target)
			if change == "primary key" {
				source.Schemas["dbo"].Tables["ArticleViews"].PrimaryKey.Descending = []bool{true}
			} else {
				source.Schemas["dbo"].Tables["ArticleViews"].Columns["ArticleId"].Type.NativeType = "bigint"
			}
			p := compareTest(t, source, target)
			p.ApplySafety(true)
			if !p.Incomplete || len(p.SchemaOperations) != 0 {
				t.Fatalf("specialized-index dependency lost: %+v", p)
			}
		})
	}
}

func TestStorageFeaturesPermitOrdinaryData(t *testing.T) {
	for _, kind := range []string{"compression", "filegroup", "primary-key options", "full-text index"} {
		t.Run(kind, func(t *testing.T) {
			db := dataFixture()
			db.Schemas["dbo"].Tables["ArticleViews"].Features = []schema.Feature{{Kind: kind, Scope: "index", Name: "PK", Detail: `[{"fill_factor":80,"ignore_dup_key":false}]`}}
			sr := &memoryReader{rows: map[string][]map[string]interface{}{"dbo.ArticleViews": {{"ArticleId": int64(1)}}}}
			d := NewDataDiffer(sr, &memoryReader{})
			ops, err := d.CompareData(context.Background(), db, db)
			if err != nil || len(ops) != 1 || len(d.OmittedTables) != 0 {
				t.Fatalf("safe data blocked: %v %+v", err, d)
			}
		})
	}
}

func TestBlockedDataPropagatesThroughTargetForeignKeys(t *testing.T) {
	source := dataFixture()
	source.Schemas["dbo"].Tables["Child"] = &schema.Table{Name: "Child", Columns: map[string]*schema.Column{"Id": {Name: "Id", Type: schema.DataType{Kind: schema.TypeInteger}}}, PrimaryKey: &schema.PrimaryKey{Columns: []string{"Id"}}}
	target := cloneDB(source)
	target.Schemas["dbo"].Tables["ArticleViews"].IsReplicated = true
	target.Schemas["dbo"].Tables["Child"].ForeignKeys = map[string]*schema.ForeignKey{"FK": {RefTable: "dbo.ArticleViews", Columns: []string{"Id"}, RefColumns: []string{"ArticleId"}}}
	sr, tr := &memoryReader{}, &memoryReader{}
	d := NewDataDiffer(sr, tr)
	ops, err := d.CompareData(context.Background(), source, target)
	if err != nil || len(ops) != 0 || len(sr.calls) != 0 || len(tr.calls) != 0 || len(d.OmittedTables) != 2 {
		t.Fatalf("target dependency ignored: %v %+v", err, d)
	}
}

func TestMetadataOnlyUnsupportedTransitionsRemainVisible(t *testing.T) {
	for _, kind := range []string{"generated column", "function kind", "sequence type"} {
		t.Run(kind, func(t *testing.T) {
			target := fixture()
			source := cloneDB(target)
			switch kind {
			case "generated column":
				source.Schemas["dbo"].Tables["ArticleViews"].Columns["ArticleId"].Generated = true
			case "function kind":
				target.Schemas["dbo"].Functions = map[string]*schema.Function{"F": {Name: "F", Kind: "IF", Definition: "same text"}}
				source.Schemas["dbo"].Functions = map[string]*schema.Function{"F": {Name: "F", Kind: "TF", Definition: "same text"}}
			case "sequence type":
				target.Schemas["dbo"].Sequences = map[string]*schema.Sequence{"S": {Name: "S", Type: schema.DataType{Kind: schema.TypeInteger, NativeType: "int"}}}
				source.Schemas["dbo"].Sequences = map[string]*schema.Sequence{"S": {Name: "S", Type: schema.DataType{Kind: schema.TypeInteger, NativeType: "bigint"}}}
			}
			p := compareTest(t, source, target)
			if !p.Incomplete || len(p.SchemaOperations) != 0 || len(p.Omitted) != 1 {
				t.Fatalf("unsupported transition hidden: %+v", p)
			}
		})
	}
}

func TestMissingUnsupportedObjectBlocksConsumers(t *testing.T) {
	target := fixture()
	source := cloneDB(target)
	source.Features = []schema.ObjectFeature{{Schema: "dbo", Object: "ClrFunction", Feature: schema.Feature{Kind: "CLR object", Scope: "object", Detail: "CLR_SCALAR_FUNCTION"}}}
	source.Schemas["dbo"].Views = map[string]*schema.View{"Consumer": {Name: "Consumer", Definition: "SELECT dbo.ClrFunction() AS Result"}}
	source.Dependencies = []schema.Dependency{{Schema: "dbo", Object: "Consumer", RefSchema: "dbo", RefObject: "ClrFunction"}}
	source.Schemas["dbo"].Tables["ArticleViews"].Columns["Extra"] = &schema.Column{Name: "Extra", Type: schema.DataType{Kind: schema.TypeInteger}, Nullable: true}
	p := compareTest(t, source, target)
	if !p.Incomplete || len(p.SchemaOperations) != 1 || p.SchemaOperations[0].OperationType() != AddColumn || len(p.Omitted) != 2 {
		t.Fatalf("consumer of missing object allowed: %+v", p)
	}
	target.Features = source.Features
	p = compareTest(t, source, target)
	if p.Incomplete || len(p.SchemaOperations) != 2 {
		t.Fatalf("consumer of existing unchanged object blocked: %+v", p)
	}
}

func TestMultipleTriggersHaveIndependentObjectDependencies(t *testing.T) {
	source := fixture()
	target := cloneDB(source)
	source.Schemas["dbo"].Tables["ArticleViews"].Triggers = map[string]*schema.Trigger{
		"First": {Name: "First", Definition: "first definition"}, "Second": {Name: "Second", Definition: "second definition"},
	}
	source.Dependencies = []schema.Dependency{
		{Schema: "dbo", Object: "First", RefSchema: "dbo", RefObject: "ArticleViews"},
		{Schema: "dbo", Object: "Second", RefSchema: "dbo", RefObject: "ArticleViews"},
	}
	p := compareTest(t, source, target)
	if p.Incomplete || len(p.SchemaOperations) != 2 {
		t.Fatalf("independent triggers created a dependency cycle: %+v", p)
	}
}

func TestBlockedChildPreventsParentCascades(t *testing.T) {
	source := dataFixture()
	source.Schemas["dbo"].Tables["Child"] = &schema.Table{Name: "Child", Columns: map[string]*schema.Column{"Id": {Name: "Id", Type: schema.DataType{Kind: schema.TypeInteger}}}, PrimaryKey: &schema.PrimaryKey{Columns: []string{"Id"}}, Features: []schema.Feature{{Kind: "row-level security", Scope: "table", Detail: "enabled"}}, ForeignKeys: map[string]*schema.ForeignKey{"FK": {RefTable: "dbo.ArticleViews", Columns: []string{"Id"}, RefColumns: []string{"ArticleId"}, OnDelete: "CASCADE"}}}
	source.Schemas["dbo"].Tables["Independent"] = &schema.Table{Name: "Independent", Columns: map[string]*schema.Column{"Id": {Name: "Id", Type: schema.DataType{Kind: schema.TypeInteger}}}, PrimaryKey: &schema.PrimaryKey{Columns: []string{"Id"}}}
	target := cloneDB(source)
	// The source no longer contains the child, but the target still has it.
	delete(source.Schemas["dbo"].Tables, "Child")
	sr := &memoryReader{rows: map[string][]map[string]interface{}{"dbo.Independent": {{"Id": int64(1)}}}}
	tr := &memoryReader{rows: map[string][]map[string]interface{}{"dbo.ArticleViews": {{"ArticleId": int64(2)}}}}
	d := NewDataDiffer(sr, tr)
	ops, err := d.CompareData(context.Background(), source, target)
	if err != nil || !d.Incomplete || len(ops) != 1 || len(d.OmittedTables) != 1 || len(sr.calls) != 1 || sr.calls[0] != "dbo.Independent" || len(tr.calls) != 1 || tr.calls[0] != "dbo.Independent" {
		t.Fatalf("blocked child could be changed by a parent cascade: %v %v %+v", err, ops, d)
	}
}

func TestSecurityPolicyBlocksPredicateFunctionChanges(t *testing.T) {
	for _, change := range []string{"alter", "drop", "drop without source policy"} {
		t.Run(change, func(t *testing.T) {
			target := fixture()
			target.Name = "dev"
			target.Schemas["dbo"].Functions = map[string]*schema.Function{"Visible": {Name: "Visible", Kind: "IF", Definition: "original definition"}}
			target.Schemas["dbo"].Tables["ArticleViews"].Features = []schema.Feature{{Kind: "row-level security", Scope: "table", Name: "Policy", Detail: "bound"}}
			target.Dependencies = []schema.Dependency{{Schema: "security", Object: "Policy", RefSchema: "dbo", RefObject: "Visible", SchemaBound: true, ReferencingType: "SP"}}
			source := cloneDB(target)
			if change == "alter" {
				source.Schemas["dbo"].Functions["Visible"].Definition = "changed definition"
			} else {
				delete(source.Schemas["dbo"].Functions, "Visible")
			}
			if change == "drop without source policy" {
				source.Schemas["dbo"].Tables["ArticleViews"].Features = nil
				source.Dependencies = nil
			}
			source.Schemas["dbo"].Tables["Independent"] = &schema.Table{Name: "Independent", Columns: map[string]*schema.Column{"Id": {Name: "Id", Type: schema.DataType{Kind: schema.TypeInteger}}}}
			p := compareTest(t, source, target)
			p.ApplySafety(true)
			if !p.Incomplete || len(p.SchemaOperations) != 1 || p.SchemaOperations[0].OperationType() != CreateTable {
				t.Fatalf("policy predicate change escaped isolation: %+v", p)
			}
			found := false
			for _, d := range p.Omitted {
				found = found || (strings.Contains(d.Reason, "security.Policy") && strings.Contains(d.Reason, "dbo.Visible") && strings.Contains(d.Reason, `database "dev"`))
			}
			if !found {
				t.Fatalf("missing policy dependency diagnostic: %+v", p.Omitted)
			}
		})
	}
}

func TestSchemaBoundFunctionDependents(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		for _, removed := range []bool{false, true} {
			for _, drop := range []bool{false, true} {
				target := fixture()
				s := target.Schemas["dbo"]
				s.Functions = map[string]*schema.Function{"Value": {Name: "Value", Kind: "FN", Definition: "old"}}
				s.Views = map[string]*schema.View{"Summary": {Name: "Summary", Definition: "bound view"}}
				target.Dependencies = []schema.Dependency{{Schema: "dbo", Object: "Summary", RefSchema: "dbo", RefObject: "Value", SchemaBound: true, ReferencingType: "V"}}
				if indexed {
					target.Features = []schema.ObjectFeature{{Schema: "dbo", Object: "Summary", Feature: schema.Feature{Kind: "indexed view", Scope: "view", Detail: "index"}}}
				}
				source := cloneDB(target)
				if removed {
					delete(source.Schemas["dbo"].Views, "Summary")
					source.Dependencies = nil
					source.Features = nil
				}
				if drop {
					delete(source.Schemas["dbo"].Functions, "Value")
				} else {
					source.Schemas["dbo"].Functions["Value"].Definition = "new"
				}
				p := compareTest(t, source, target)
				p.ApplySafety(true)
				blocked := indexed || !removed
				if p.Incomplete != blocked {
					t.Fatalf("indexed=%v removed=%v drop=%v: %+v", indexed, removed, drop, p)
				}
				for _, op := range p.SchemaOperations {
					if blocked && (op.OperationType() == AlterFunction || op.OperationType() == DropFunction) {
						t.Fatalf("blocked function change escaped: %+v", op)
					}
				}
				if !blocked && len(p.SchemaOperations) != 2 {
					t.Fatalf("supported dependent removal lost: %+v", p)
				}
			}
		}
	}
}

func TestFunctionChangeAfterDetachingSchemaBoundView(t *testing.T) {
	target := fixture()
	target.Schemas["dbo"].Functions = map[string]*schema.Function{"Value": {Name: "Value", Kind: "FN", Definition: "old"}}
	target.Schemas["dbo"].Views = map[string]*schema.View{"Summary": {Name: "Summary", Definition: "old view"}}
	target.Dependencies = []schema.Dependency{{Schema: "dbo", Object: "Summary", RefSchema: "dbo", RefObject: "Value", SchemaBound: true, ReferencingType: "V"}}
	source := cloneDB(target)
	source.Schemas["dbo"].Functions["Value"].Definition = "new"
	source.Schemas["dbo"].Views["Summary"].Definition = "detached view"
	source.Dependencies = nil
	p := compareTest(t, source, target)
	if p.Incomplete || len(p.SchemaOperations) != 2 || p.SchemaOperations[0].OperationType() != AlterView || p.SchemaOperations[1].OperationType() != AlterFunction {
		t.Fatalf("safe dependency detachment lost: %+v", p)
	}
}
