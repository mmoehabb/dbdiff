package diff

import (
	"context"
	"github.com/mmoehabb/dbdiff/internal/domain/schema"
	"reflect"
	"testing"
)

type memoryReader struct {
	rows  map[string][]map[string]interface{}
	calls []string
}

func (r *memoryReader) ReadTableData(_ context.Context, sn string, t *schema.Table) ([]map[string]interface{}, error) {
	r.calls = append(r.calls, sn+"."+t.Name)
	return r.rows[sn+"."+t.Name], nil
}
func dataFixture() *schema.Database {
	db := fixture()
	t := db.Schemas["dbo"].Tables["ArticleViews"]
	t.PrimaryKey = &schema.PrimaryKey{Columns: []string{"ArticleId"}}
	return db
}
func TestCompositeKeyEncoding(t *testing.T) {
	cols := []string{"a", "b"}
	a := map[string]interface{}{"a": "x|y", "b": "z"}
	b := map[string]interface{}{"a": "x", "b": "y|z"}
	if generatePKKey(cols, a) == generatePKKey(cols, b) {
		t.Fatal("delimiter collision")
	}
	if generatePKKey([]string{"a"}, map[string]interface{}{"a": int64(1)}) == generatePKKey([]string{"a"}, map[string]interface{}{"a": "1"}) {
		t.Fatal("type collision")
	}
}
func TestDataForNewTable(t *testing.T) {
	source := dataFixture()
	source.Schemas["dbo"].Tables["ArticleViews"].Columns["Computed"] = &schema.Column{Name: "Computed", Computed: true, Type: schema.DataType{Kind: schema.TypeInteger}}
	sr := &memoryReader{rows: map[string][]map[string]interface{}{"dbo.ArticleViews": {{"ArticleId": int64(1), "Computed": 5}}}}
	tr := &memoryReader{}
	ops, err := NewDataDiffer(sr, tr).CompareData(context.Background(), source, &schema.Database{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 || len(tr.calls) != 0 {
		t.Fatalf("new table not copied: %v", ops)
	}
	if _, ok := ops[0].(InsertDataOperation).Row["Computed"]; ok {
		t.Fatal("computed column written")
	}
}
func TestDataRejectsKeylessAndCycles(t *testing.T) {
	db := fixture()
	d := NewDataDiffer(&memoryReader{}, &memoryReader{})
	if _, err := d.CompareData(context.Background(), db, db); err == nil {
		t.Fatal("keyless data accepted")
	}
	db = dataFixture()
	db.Schemas["dbo"].Tables["ArticleViews"].ForeignKeys = map[string]*schema.ForeignKey{"self": {RefTable: "dbo.ArticleViews"}}
	if _, err := d.CompareData(context.Background(), db, db); err == nil {
		t.Fatal("self reference silently accepted")
	}
}
func TestDataBinaryAndNoOp(t *testing.T) {
	db := dataFixture()
	db.Schemas["dbo"].Tables["ArticleViews"].Columns["Bytes"] = &schema.Column{Name: "Bytes", Type: schema.DataType{Kind: schema.TypeBinary}}
	rows := map[string][]map[string]interface{}{"dbo.ArticleViews": {{"ArticleId": int64(1), "Bytes": []byte{0, 255, 0x27}}}}
	ops, err := NewDataDiffer(&memoryReader{rows: rows}, &memoryReader{rows: rows}).CompareData(context.Background(), db, db)
	if err != nil || len(ops) != 0 {
		t.Fatalf("identical data changes: %v %v", ops, err)
	}
	ops, err = NewDataDiffer(&memoryReader{rows: rows}, &memoryReader{}).CompareData(context.Background(), db, db)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ops[0].(InsertDataOperation).Row["Bytes"], []byte{0, 255, 0x27}) {
		t.Fatal("binary value corrupted")
	}
}
func TestDataSkipsBlockedTables(t *testing.T) {
	db := dataFixture()
	sr := &memoryReader{}
	ops, err := NewDataDiffer(sr, &memoryReader{}).CompareData(context.Background(), db, db, map[string]bool{key("dbo", "ArticleViews"): true})
	if err != nil || len(ops) != 0 || len(sr.calls) != 0 {
		t.Fatal("blocked table data was read")
	}
}
