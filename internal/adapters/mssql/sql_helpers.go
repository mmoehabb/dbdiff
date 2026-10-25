package mssql

import (
	"fmt"
	"github.com/mmoehabb/dbdiff/internal/domain/diff"
	"github.com/mmoehabb/dbdiff/internal/domain/schema"
	"regexp"
	"strings"
)

func renderIndex(o diff.CreateIndexOperation) (string, error) {
	x := o.Index
	if len(x.Columns) == 0 {
		return "", fmt.Errorf("index %s has no key columns", x.Name)
	}
	name := x.Name
	if name == "" {
		name = "IX_" + o.TableName + "_" + strings.Join(x.Columns, "_")
	}
	kind := "NONCLUSTERED"
	if x.Clustered {
		kind = "CLUSTERED"
	}
	sql := "CREATE "
	if x.IsUnique {
		sql += "UNIQUE "
	}
	sql += kind + " INDEX " + quote(name) + " ON " + qualified(o.SchemaName, o.TableName)
	if x.IsConstraint {
		sql = "ALTER TABLE " + qualified(o.SchemaName, o.TableName) + " ADD CONSTRAINT " + quote(name) + " UNIQUE " + kind
	}
	sql += " (" + columns(x.Columns, x.Descending) + ")"
	if len(x.IncludedColumns) > 0 {
		sql += " INCLUDE (" + columns(x.IncludedColumns, nil) + ")"
	}
	if x.Filter != "" {
		sql += " WHERE " + x.Filter
	}
	if x.OptionsKnown {
		options := []string{}
		// SQL Server represents its default fill factor as 0 in sys.indexes,
		// but CREATE INDEX only accepts values from 1 through 100.
		if x.FillFactor > 0 {
			options = append(options, fmt.Sprintf("FILLFACTOR = %d", x.FillFactor))
		}
		options = append(options,
			"PAD_INDEX = "+onOff(x.PadIndex),
			"IGNORE_DUP_KEY = "+onOff(x.IgnoreDupKey),
			"ALLOW_ROW_LOCKS = "+onOff(x.AllowRowLocks),
			"ALLOW_PAGE_LOCKS = "+onOff(x.AllowPageLocks),
		)
		sql += " WITH (" + strings.Join(options, ", ") + ")"
	}
	return sql + ";", nil
}
func onOff(v bool) string {
	if v {
		return "ON"
	}
	return "OFF"
}

var validCollationName = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func collationSQL(name string) (string, error) {
	if name == "" {
		return "", nil
	}
	if !validCollationName.MatchString(name) {
		return "", fmt.Errorf("unsupported SQL Server collation name %q", name)
	}
	return " COLLATE " + name, nil
}

var moduleHeader = regexp.MustCompile(`(?is)^(CREATE\s+OR\s+ALTER|CREATE|ALTER)\s+(VIEW|PROC(?:EDURE)?|FUNCTION|TRIGGER)\b`)

func normalizeModule(def string, name ...string) (string, error) {
	// Strip leading comments only; never rewrite keywords inside the module body.
	def = strings.TrimSpace(def)
	for {
		if strings.HasPrefix(def, "--") {
			n := strings.IndexByte(def, '\n')
			if n < 0 {
				return "", fmt.Errorf("module definition has no SQL")
			}
			def = strings.TrimSpace(def[n+1:])
			continue
		}
		if strings.HasPrefix(def, "/*") {
			depth, end := 1, 2
			for end < len(def) && depth > 0 {
				if strings.HasPrefix(def[end:], "/*") {
					depth++
					end += 2
				} else if strings.HasPrefix(def[end:], "*/") {
					depth--
					end += 2
				} else {
					end++
				}
			}
			if depth != 0 {
				return "", fmt.Errorf("unterminated module comment")
			}
			def = strings.TrimSpace(def[end:])
			continue
		}
		break
	}
	loc := moduleHeader.FindStringSubmatchIndex(def)
	if loc == nil {
		return "", fmt.Errorf("unsupported module definition header")
	}
	rest := def[loc[1]:]
	if len(name) == 2 {
		match := moduleObjectName.FindStringIndex(rest)
		if match == nil {
			return "", fmt.Errorf("unsupported module object name")
		}
		rest = " " + qualified(name[0], name[1]) + rest[match[1]:]
	}
	return "CREATE OR ALTER " + strings.ToUpper(def[loc[4]:loc[5]]) + rest, nil
}

var moduleObjectName = regexp.MustCompile(`(?s)^\s*(?:\[(?:[^\]]|\]\])*\]|"(?:[^"]|"")*"|[^\s.();]+)(?:\s*\.\s*(?:\[(?:[^\]]|\]\])*\]|"(?:[^"]|"")*"|[^\s.();]+))*`)

func moduleSQL(def string, ansi, quoted bool, name ...string) (string, error) {
	normalized, err := normalizeModule(def, name...)
	if err != nil {
		return "", err
	}
	return "SET ANSI_NULLS " + onOff(ansi) + "; SET QUOTED_IDENTIFIER " + onOff(quoted) + "; EXEC sys.sp_executesql " + literal(normalized) + ";", nil
}
func (r *MSSQLRenderer) sequenceSQL(sn string, s schema.Sequence, alter bool) string {
	verb := "CREATE"
	if alter {
		verb = "ALTER"
	}
	sql := verb + " SEQUENCE " + qualified(sn, s.Name)
	if !alter {
		if s.Type.NativeType != "" {
			sql += " AS " + r.renderDataType(s.Type, "")
		}
		sql += fmt.Sprintf(" START WITH %d", s.StartValue)
	}
	sql += fmt.Sprintf(" INCREMENT BY %d MINVALUE %d MAXVALUE %d", s.Increment, s.MinValue, s.MaxValue)
	if s.IsCycling {
		sql += " CYCLE"
	} else {
		sql += " NO CYCLE"
	}
	if s.CacheSize == 0 {
		sql += " NO CACHE"
	} else {
		sql += fmt.Sprintf(" CACHE %d", s.CacheSize)
	}
	return sql + ";"
}
func propertyType(v schema.PropertyValue) (string, error) {
	if v.IsNull {
		return "sql_variant", nil
	}
	switch v.Type {
	case "int", "bigint", "smallint", "tinyint", "bit", "money", "smallmoney", "uniqueidentifier":
		return v.Type, nil
	case "decimal", "numeric":
		if v.Precision >= 1 && v.Precision <= 38 && v.Scale >= 0 && v.Scale <= v.Precision {
			return fmt.Sprintf("%s(%d,%d)", v.Type, v.Precision, v.Scale), nil
		}
	case "nvarchar", "varchar", "nchar", "char", "binary", "varbinary":
		n := v.Length
		if strings.HasPrefix(v.Type, "n") {
			n /= 2
		}
		if n > 0 && n <= 8000 {
			return fmt.Sprintf("%s(%d)", v.Type, n), nil
		}
	}
	return "", fmt.Errorf("unsupported extended property type %q", v.Type)
}
func propertySQL(action, name string, value schema.PropertyValue, l diff.ExtendedPropertyLevel) (string, error) {
	prefix := ""
	sql := "EXEC sys.sp_" + action + "extendedproperty @name=" + literal(name)
	if action != "drop" {
		typ, err := propertyType(value)
		if err != nil {
			return "", err
		}
		expr := "NULL"
		if !value.IsNull {
			expr = "CONVERT(" + typ + ", " + literal(value.Value)
			if value.Type == "binary" || value.Type == "varbinary" {
				expr += ", 2"
			}
			expr += ")"
		}
		prefix = "DECLARE @dbdiff_property sql_variant = " + expr + ";\n"
		sql += ", @value=@dbdiff_property"
	}
	for i, pair := range [][2]string{{l.Level0Type, l.Level0Name}, {l.Level1Type, l.Level1Name}, {l.Level2Type, l.Level2Name}} {
		if pair[0] != "" {
			sql += fmt.Sprintf(", @level%dtype=%s, @level%dname=%s", i, literal(pair[0]), i, literal(pair[1]))
		}
	}
	return prefix + sql + ";", nil
}
func referenceName(sn, ref string) (string, error) {
	parts, err := schema.ParseQualifiedName(ref)
	if err != nil {
		return "", err
	}
	if len(parts) == 1 {
		return qualified(sn, parts[0]), nil
	}
	if len(parts) == 2 {
		return qualified(parts[0], parts[1]), nil
	}
	return "", fmt.Errorf("unsupported reference %q", ref)
}

// Canonicalize only the module header; retain executable body text.
func canonicalModule(def string, name ...string) string {
	normalized, err := normalizeModule(def, name...)
	if err != nil {
		return def
	}
	return normalized
}
