package diff

import "github.com/mmoehabb/dbdiff/internal/domain/schema"

func properties(a, b map[string]schema.PropertyValue, l ExtendedPropertyLevel, p *MigrationPlan) {
	for n, v := range a {
		old, ok := b[n]
		if !ok {
			p.SchemaOperations = append(p.SchemaOperations, AddExtendedPropertyOperation{n, v, l})
		} else if old != v {
			p.SchemaOperations = append(p.SchemaOperations, AlterExtendedPropertyOperation{n, v, l})
		}
	}
	for n := range b {
		if _, ok := a[n]; !ok {
			p.SchemaOperations = append(p.SchemaOperations, DropExtendedPropertyOperation{n, l})
		}
	}
}
func (d *SchemaDiffer) compareProperties(source, target *schema.Database, p *MigrationPlan) {
	for sn, s := range source.Schemas {
		old := target.Schemas[sn]
		if old == nil {
			old = &schema.Schema{}
		}
		base := ExtendedPropertyLevel{Level0Type: "SCHEMA", Level0Name: sn}
		properties(s.ExtendedProperties, old.ExtendedProperties, base, p)
		for tn, t := range s.Tables {
			prev := old.Tables[tn]
			if prev == nil {
				prev = &schema.Table{}
			}
			level := base
			level.Level1Type = "TABLE"
			level.Level1Name = tn
			properties(t.ExtendedProperties, prev.ExtendedProperties, level, p)
			for cn, c := range t.Columns {
				pc := prev.Columns[cn]
				if pc == nil {
					pc = &schema.Column{}
				}
				cl := level
				cl.Level2Type = "COLUMN"
				cl.Level2Name = cn
				properties(c.ExtendedProperties, pc.ExtendedProperties, cl, p)
			}
		}
		for n, v := range s.Views {
			ov := old.Views[n]
			if ov == nil {
				ov = &schema.View{}
			}
			l := base
			l.Level1Type = "VIEW"
			l.Level1Name = n
			properties(v.ExtendedProperties, ov.ExtendedProperties, l, p)
		}
		for n, v := range s.Procedures {
			ov := old.Procedures[n]
			if ov == nil {
				ov = &schema.Procedure{}
			}
			l := base
			l.Level1Type = "PROCEDURE"
			l.Level1Name = n
			properties(v.ExtendedProperties, ov.ExtendedProperties, l, p)
		}
		for n, v := range s.Functions {
			ov := old.Functions[n]
			if ov == nil {
				ov = &schema.Function{}
			}
			l := base
			l.Level1Type = "FUNCTION"
			l.Level1Name = n
			properties(v.ExtendedProperties, ov.ExtendedProperties, l, p)
		}
	}
}
