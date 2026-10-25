package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/mmoehabb/dbdiff/internal/domain/schema"
)

func (i *MSSQLIntrospector) loadSafetyMetadata(ctx context.Context, db *sql.DB, d *schema.Database) error {
	var visible int
	if err := db.QueryRowContext(ctx, "SELECT HAS_PERMS_BY_NAME(DB_NAME(), 'DATABASE', 'VIEW DEFINITION')").Scan(&visible); err != nil {
		return err
	}
	if visible != 1 {
		return fmt.Errorf("VIEW DEFINITION permission on the database is required for a complete comparison")
	}
	rows, err := db.QueryContext(ctx, `SELECT name FROM sys.schemas WHERE name NOT IN ('sys','guest','INFORMATION_SCHEMA','db_accessadmin','db_backupoperator','db_datareader','db_datawriter','db_ddladmin','db_denydatareader','db_denydatawriter','db_owner','db_securityadmin')`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return err
		}
		i.ensureSchema(d, n)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = db.QueryContext(ctx, queryTableFeatures)
	if err != nil {
		return err
	}
	for rows.Next() {
		var sn, tn string
		var feature schema.Feature
		if err := rows.Scan(&sn, &tn, &feature.Kind, &feature.Scope, &feature.Name, &feature.Detail); err != nil {
			rows.Close()
			return err
		}
		t := i.ensureSchemaAndTable(d, sn, tn)
		t.Features = append(t.Features, feature)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if err := i.loadObjectFeatures(ctx, db, d); err != nil {
		return err
	}
	rows, err = db.QueryContext(ctx, `SELECT OBJECT_SCHEMA_NAME(CASE WHEN o.type IN ('D','C') THEN o.parent_object_id ELSE d.referencing_id END),OBJECT_NAME(CASE WHEN o.type IN ('D','C') THEN o.parent_object_id ELSE d.referencing_id END),COALESCE(d.referenced_schema_name,OBJECT_SCHEMA_NAME(d.referenced_id)),COALESCE(d.referenced_entity_name,OBJECT_NAME(d.referenced_id)),CASE WHEN o.type IN ('D','C') THEN CAST(0 AS bit) ELSE d.is_schema_bound_reference END, d.referenced_database_name,d.referenced_server_name,RTRIM(o.type) FROM sys.sql_expression_dependencies d JOIN sys.objects o ON d.referencing_id=o.object_id WHERE o.is_ms_shipped=0`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var sn, n, rs, rn, rd, server sql.NullString
		var bound bool
		var referencingType string
		if err := rows.Scan(&sn, &n, &rs, &rn, &bound, &rd, &server, &referencingType); err != nil {
			return err
		}
		if !rs.Valid || !rn.Valid || rd.Valid || server.Valid {
			d.Unsupported = append(d.Unsupported, fmt.Sprintf("unresolved/external dependency from %s.%s", sn.String, n.String))
			continue
		}
		d.Dependencies = append(d.Dependencies, schema.Dependency{Schema: sn.String, Object: n.String, RefSchema: rs.String, RefObject: rn.String, SchemaBound: bound, ReferencingType: referencingType})
	}
	return rows.Err()
}
func (i *MSSQLIntrospector) loadChecksAndStatistics(ctx context.Context, db *sql.DB, d *schema.Database) error {
	rows, err := db.QueryContext(ctx, `SELECT s.name,t.name,c.name,c.definition FROM sys.check_constraints c JOIN sys.tables t ON c.parent_object_id=t.object_id JOIN sys.schemas s ON t.schema_id=s.schema_id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var sn, tn, n, def string
		if err := rows.Scan(&sn, &tn, &n, &def); err != nil {
			rows.Close()
			return err
		}
		t := i.ensureSchemaAndTable(d, sn, tn)
		if t.Checks == nil {
			t.Checks = map[string]*schema.CheckConstraint{}
		}
		t.Checks[n] = &schema.CheckConstraint{Name: n, Definition: def}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = db.QueryContext(ctx, `SELECT s.name,t.name,st.name,c.name,st.auto_created FROM sys.stats st JOIN sys.tables t ON st.object_id=t.object_id JOIN sys.schemas s ON t.schema_id=s.schema_id JOIN sys.stats_columns sc ON sc.object_id=st.object_id AND sc.stats_id=st.stats_id JOIN sys.columns c ON c.object_id=sc.object_id AND c.column_id=sc.column_id WHERE NOT EXISTS(SELECT 1 FROM sys.indexes ix WHERE ix.object_id=st.object_id AND ix.index_id=st.stats_id) ORDER BY s.name,t.name,st.name,sc.stats_column_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var sn, tn, n, column string
		var auto bool
		if err := rows.Scan(&sn, &tn, &n, &column, &auto); err != nil {
			return err
		}
		t := i.ensureSchemaAndTable(d, sn, tn)
		if t.Statistics == nil {
			t.Statistics = map[string]bool{}
		}
		t.Statistics[n] = true
		if !auto {
			if t.UserStatistics == nil {
				t.UserStatistics = map[string][]string{}
			}
			t.UserStatistics[n] = append(t.UserStatistics[n], column)
		}
	}
	return rows.Err()
}

func (i *MSSQLIntrospector) loadObjectFeatures(ctx context.Context, db *sql.DB, d *schema.Database) error {
	rows, err := db.QueryContext(ctx, queryObjectFeatures)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var f schema.ObjectFeature
		if err := rows.Scan(&f.Schema, &f.Object, &f.Feature.Kind, &f.Feature.Scope, &f.Feature.Detail); err != nil {
			return err
		}
		if f.Schema != "" {
			i.ensureSchema(d, f.Schema)
		}
		d.Features = append(d.Features, f)
	}
	return rows.Err()
}
