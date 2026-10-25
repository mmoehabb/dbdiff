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
	rows, err := db.QueryContext(ctx, `SELECT name FROM sys.schemas WHERE schema_id < 16384 AND name NOT IN ('sys','guest','INFORMATION_SCHEMA')`)
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
	rows, err = db.QueryContext(ctx, `SELECT s.name,t.name,'specialized table or index' FROM sys.tables t JOIN sys.schemas s ON s.schema_id=t.schema_id WHERE t.is_memory_optimized=1 OR t.is_filetable=1 OR t.is_node=1 OR t.is_edge=1 OR t.is_tracked_by_cdc=1 OR EXISTS(SELECT 1 FROM sys.fulltext_indexes ft WHERE ft.object_id=t.object_id) OR EXISTS(SELECT 1 FROM sys.masked_columns mc WHERE mc.object_id=t.object_id AND mc.is_masked=1) OR EXISTS(SELECT 1 FROM sys.security_predicates sp WHERE sp.target_object_id=t.object_id) OR EXISTS(SELECT 1 FROM sys.indexes ix WHERE ix.object_id=t.object_id AND (ix.type NOT IN(0,1,2) OR ix.is_hypothetical=1)) OR EXISTS(SELECT 1 FROM sys.partitions p WHERE p.object_id=t.object_id AND p.data_compression<>0) OR EXISTS(SELECT 1 FROM sys.indexes ix JOIN sys.data_spaces ds ON ix.data_space_id=ds.data_space_id WHERE ix.object_id=t.object_id AND ds.name<>'PRIMARY') OR EXISTS(SELECT 1 FROM sys.indexes ix WHERE ix.object_id=t.object_id AND ix.is_primary_key=1 AND (ix.fill_factor<>0 OR ix.is_padded=1 OR ix.ignore_dup_key=1 OR ix.allow_row_locks=0 OR ix.allow_page_locks=0))
 UNION ALL SELECT s.name,t.name,'disabled, untrusted, or replication-specific constraint' FROM sys.tables t JOIN sys.schemas s ON s.schema_id=t.schema_id WHERE EXISTS(SELECT 1 FROM sys.foreign_keys f WHERE f.parent_object_id=t.object_id AND (f.is_disabled=1 OR f.is_not_trusted=1 OR f.is_not_for_replication=1)) OR EXISTS(SELECT 1 FROM sys.check_constraints c WHERE c.parent_object_id=t.object_id AND (c.is_disabled=1 OR c.is_not_trusted=1 OR c.is_not_for_replication=1))
 UNION ALL SELECT s.name,t.name,'unsupported column storage or type' FROM sys.tables t JOIN sys.schemas s ON s.schema_id=t.schema_id WHERE EXISTS(SELECT 1 FROM sys.columns c JOIN sys.types ty ON c.user_type_id=ty.user_type_id WHERE c.object_id=t.object_id AND (c.is_sparse=1 OR c.is_column_set=1 OR c.is_filestream=1 OR ty.is_user_defined=1 OR c.encryption_type IS NOT NULL OR c.generated_always_type<>0))
 UNION ALL SELECT s.name,t.name,'disabled or replication-specific trigger' FROM sys.triggers tr JOIN sys.tables t ON tr.parent_id=t.object_id JOIN sys.schemas s ON s.schema_id=t.schema_id WHERE tr.is_disabled=1 OR tr.is_not_for_replication=1`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var sn, tn, reason string
		if err := rows.Scan(&sn, &tn, &reason); err != nil {
			rows.Close()
			return err
		}
		t := i.ensureSchemaAndTable(d, sn, tn)
		t.Unsupported = append(t.Unsupported, reason)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = db.QueryContext(ctx, `SELECT 'indexed view '+QUOTENAME(s.name)+'.'+QUOTENAME(v.name) FROM sys.views v JOIN sys.schemas s ON s.schema_id=v.schema_id WHERE EXISTS(SELECT 1 FROM sys.indexes ix WHERE ix.object_id=v.object_id AND ix.index_id>0)
 UNION ALL SELECT 'unsupported CLR object '+QUOTENAME(SCHEMA_NAME(o.schema_id))+'.'+QUOTENAME(o.name) FROM sys.objects o WHERE o.type IN ('PC','FS','FT','AF') AND o.is_ms_shipped=0
 UNION ALL SELECT 'user-defined type '+QUOTENAME(SCHEMA_NAME(schema_id))+'.'+QUOTENAME(name) FROM sys.types WHERE is_user_defined=1
 UNION ALL SELECT 'view or database trigger '+QUOTENAME(tr.name) FROM sys.triggers tr WHERE tr.is_ms_shipped=0 AND NOT EXISTS(SELECT 1 FROM sys.tables t WHERE t.object_id=tr.parent_id)`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var reason string
		if err := rows.Scan(&reason); err != nil {
			rows.Close()
			return err
		}
		d.Unsupported = append(d.Unsupported, reason)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = db.QueryContext(ctx, `SELECT OBJECT_SCHEMA_NAME(CASE WHEN o.type IN ('D','C') THEN o.parent_object_id ELSE d.referencing_id END),OBJECT_NAME(CASE WHEN o.type IN ('D','C') THEN o.parent_object_id ELSE d.referencing_id END),COALESCE(d.referenced_schema_name,OBJECT_SCHEMA_NAME(d.referenced_id)),COALESCE(d.referenced_entity_name,OBJECT_NAME(d.referenced_id)),CASE WHEN o.type IN ('D','C') THEN CAST(0 AS bit) ELSE d.is_schema_bound_reference END, d.referenced_database_name,d.referenced_server_name FROM sys.sql_expression_dependencies d JOIN sys.objects o ON d.referencing_id=o.object_id WHERE o.is_ms_shipped=0`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var sn, n, rs, rn, rd, server sql.NullString
		var bound bool
		if err := rows.Scan(&sn, &n, &rs, &rn, &bound, &rd, &server); err != nil {
			return err
		}
		if !rs.Valid || !rn.Valid || rd.Valid || server.Valid {
			d.Unsupported = append(d.Unsupported, fmt.Sprintf("unresolved/external dependency from %s.%s", sn.String, n.String))
			continue
		}
		d.Dependencies = append(d.Dependencies, schema.Dependency{Schema: sn.String, Object: n.String, RefSchema: rs.String, RefObject: rn.String, SchemaBound: bound})
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
