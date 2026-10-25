package mssql

// Each row identifies the exact feature and owning object.
const queryTableFeatures = `
SELECT s.name,t.name,'temporal history','table','','history table' COLLATE DATABASE_DEFAULT
FROM sys.tables t JOIN sys.schemas s ON s.schema_id=t.schema_id WHERE t.temporal_type=1 AND t.is_ms_shipped=0
UNION ALL
SELECT s.name,t.name,'memory-optimized','table','','enabled'
FROM sys.tables t JOIN sys.schemas s ON s.schema_id=t.schema_id
WHERE t.is_ms_shipped=0 AND (t.is_memory_optimized=1)
UNION ALL
SELECT s.name,t.name,'filetable','table','','enabled'
FROM sys.tables t JOIN sys.schemas s ON s.schema_id=t.schema_id
WHERE t.is_ms_shipped=0 AND (t.is_filetable=1)
UNION ALL
SELECT s.name,t.name,'graph node','table','','enabled'
FROM sys.tables t JOIN sys.schemas s ON s.schema_id=t.schema_id
WHERE t.is_ms_shipped=0 AND (t.is_node=1)
UNION ALL
SELECT s.name,t.name,'graph edge','table','','enabled'
FROM sys.tables t JOIN sys.schemas s ON s.schema_id=t.schema_id
WHERE t.is_ms_shipped=0 AND (t.is_edge=1)
UNION ALL
SELECT s.name,t.name,'CDC','table','','enabled'
FROM sys.tables t JOIN sys.schemas s ON s.schema_id=t.schema_id
WHERE t.is_ms_shipped=0 AND (t.is_tracked_by_cdc=1)
UNION ALL
SELECT s.name,t.name,'full-text index','table','',(SELECT fcatalog.name AS catalog_name,ki.name AS key_index,ft.change_tracking_state,ft.is_enabled,CASE WHEN ft.stoplist_id IS NULL THEN 'OFF' WHEN ft.stoplist_id=0 THEN 'SYSTEM' ELSE 'CUSTOM' END AS stoplist_mode,stop.name AS stoplist_name,
 (SELECT sw.language_id,sw.stopword FROM sys.fulltext_stopwords sw WHERE sw.stoplist_id=ft.stoplist_id ORDER BY sw.language_id,sw.stopword COLLATE Latin1_General_100_BIN2 FOR JSON PATH) AS stopwords_json, (SELECT COL_NAME(fc.object_id,fc.column_id) AS column_name,COL_NAME(fc.object_id,fc.type_column_id) AS type_column,fc.language_id,fc.statistical_semantics FROM sys.fulltext_index_columns fc WHERE fc.object_id=t.object_id ORDER BY fc.column_id FOR JSON PATH) AS columns_json FOR JSON PATH)
FROM sys.tables t JOIN sys.schemas s ON s.schema_id=t.schema_id JOIN sys.fulltext_indexes ft ON ft.object_id=t.object_id JOIN sys.fulltext_catalogs fcatalog ON fcatalog.fulltext_catalog_id=ft.fulltext_catalog_id JOIN sys.indexes ki ON ki.object_id=ft.object_id AND ki.index_id=ft.unique_index_id LEFT JOIN sys.fulltext_stoplists stop ON stop.stoplist_id=ft.stoplist_id
WHERE t.is_ms_shipped=0 AND (1=1)
UNION ALL
SELECT s.name,t.name,'row-level security','table',sp.name,(SELECT pr.predicate_definition,pr.predicate_type,pr.operation,sp.is_enabled,sp.is_schema_bound FOR JSON PATH)
FROM sys.tables t JOIN sys.schemas s ON s.schema_id=t.schema_id JOIN sys.security_predicates pr ON pr.target_object_id=t.object_id JOIN sys.security_policies sp ON sp.object_id=pr.object_id
WHERE t.is_ms_shipped=0 AND (1=1)
UNION ALL
SELECT s.name,t.name,'specialized index','index',ix.name,
 (SELECT ix.type_desc,ix.is_unique,ix.is_disabled,ix.filter_definition,ix.fill_factor,ix.is_padded,ix.ignore_dup_key,ix.allow_row_locks,ix.allow_page_locks,
  (SELECT COL_NAME(ic.object_id,ic.column_id) AS column_name,ic.key_ordinal,ic.partition_ordinal,ic.is_descending_key,ic.is_included_column FROM sys.index_columns ic WHERE ic.object_id=ix.object_id AND ic.index_id=ix.index_id ORDER BY ic.index_column_id FOR JSON PATH) AS columns_json,
  (SELECT xi.secondary_type_desc,pi.name AS primary_xml_index FROM sys.xml_indexes xi LEFT JOIN sys.indexes pi ON pi.object_id=xi.object_id AND pi.index_id=xi.using_xml_index_id WHERE xi.object_id=ix.object_id AND xi.index_id=ix.index_id FOR JSON PATH) AS xml_json,
  (SELECT st.tessellation_scheme,st.bounding_box_xmin,st.bounding_box_ymin,st.bounding_box_xmax,st.bounding_box_ymax,st.level_1_grid,st.level_2_grid,st.level_3_grid,st.level_4_grid,st.cells_per_object FROM sys.spatial_index_tessellations st WHERE st.object_id=ix.object_id AND st.index_id=ix.index_id FOR JSON PATH) AS spatial_json
 FOR JSON PATH)
FROM sys.tables t JOIN sys.schemas s ON s.schema_id=t.schema_id JOIN sys.indexes ix ON ix.object_id=t.object_id
WHERE t.is_ms_shipped=0 AND (ix.type NOT IN (0,1,2))
UNION ALL
SELECT s.name,t.name,'hypothetical index','index',ix.name,'enabled'
FROM sys.tables t JOIN sys.schemas s ON s.schema_id=t.schema_id JOIN sys.indexes ix ON ix.object_id=t.object_id
WHERE t.is_ms_shipped=0 AND (ix.is_hypothetical=1)
UNION ALL
SELECT s.name,t.name,'disabled index','index',ix.name,'disabled'
FROM sys.tables t JOIN sys.schemas s ON s.schema_id=t.schema_id JOIN sys.indexes ix ON ix.object_id=t.object_id
WHERE t.is_ms_shipped=0 AND (ix.is_disabled=1)
UNION ALL
SELECT s.name,t.name,'compression','index',COALESCE(ix.name,''),CONCAT(p.partition_number,':',p.data_compression_desc)
FROM sys.tables t JOIN sys.schemas s ON s.schema_id=t.schema_id JOIN sys.partitions p ON p.object_id=t.object_id JOIN sys.indexes ix ON ix.object_id=p.object_id AND ix.index_id=p.index_id
WHERE t.is_ms_shipped=0 AND (p.data_compression<>0)
UNION ALL
SELECT s.name,t.name,'filegroup','index',COALESCE(ix.name,''),ds.name
FROM sys.tables t JOIN sys.schemas s ON s.schema_id=t.schema_id JOIN sys.indexes ix ON ix.object_id=t.object_id JOIN sys.data_spaces ds ON ds.data_space_id=ix.data_space_id
WHERE t.is_ms_shipped=0 AND (ds.name<>'PRIMARY')
UNION ALL
SELECT s.name,t.name,'primary-key options','index',ix.name,(SELECT ix.fill_factor,ix.is_padded,ix.ignore_dup_key,ix.allow_row_locks,ix.allow_page_locks FOR JSON PATH)
FROM sys.tables t JOIN sys.schemas s ON s.schema_id=t.schema_id JOIN sys.indexes ix ON ix.object_id=t.object_id
WHERE t.is_ms_shipped=0 AND (ix.is_primary_key=1 AND (ix.fill_factor<>0 OR ix.is_padded=1 OR ix.ignore_dup_key=1 OR ix.allow_row_locks=0 OR ix.allow_page_locks=0))
UNION ALL
SELECT s.name,t.name,'constraint state','foreign_key',c.name,(SELECT c.is_disabled,c.is_not_trusted,c.is_not_for_replication FOR JSON PATH)
FROM sys.tables t JOIN sys.schemas s ON s.schema_id=t.schema_id JOIN sys.foreign_keys c ON c.parent_object_id=t.object_id
WHERE t.is_ms_shipped=0 AND (c.is_disabled=1 OR c.is_not_trusted=1 OR c.is_not_for_replication=1)
UNION ALL
SELECT s.name,t.name,'constraint state','check',c.name,(SELECT c.is_disabled,c.is_not_trusted,c.is_not_for_replication FOR JSON PATH)
FROM sys.tables t JOIN sys.schemas s ON s.schema_id=t.schema_id JOIN sys.check_constraints c ON c.parent_object_id=t.object_id
WHERE t.is_ms_shipped=0 AND (c.is_disabled=1 OR c.is_not_trusted=1 OR c.is_not_for_replication=1)
UNION ALL
SELECT s.name,t.name,'column storage or type','column',c.name,(SELECT ty.name,SCHEMA_NAME(ty.schema_id) AS type_schema,c.is_sparse,c.is_column_set,c.is_filestream,c.encryption_type,c.generated_always_type FOR JSON PATH)
FROM sys.tables t JOIN sys.schemas s ON s.schema_id=t.schema_id JOIN sys.columns c ON c.object_id=t.object_id JOIN sys.types ty ON c.user_type_id=ty.user_type_id
WHERE t.is_ms_shipped=0 AND (c.is_sparse=1 OR c.is_column_set=1 OR c.is_filestream=1 OR ty.is_user_defined=1 OR c.encryption_type IS NOT NULL OR c.generated_always_type<>0)
UNION ALL
SELECT s.name,t.name,'dynamic data masking','column',c.name,c.masking_function
FROM sys.tables t JOIN sys.schemas s ON s.schema_id=t.schema_id JOIN sys.masked_columns c ON c.object_id=t.object_id
WHERE t.is_ms_shipped=0 AND (c.is_masked=1)
UNION ALL
SELECT s.name,t.name,'trigger state','trigger',tr.name,(SELECT tr.is_disabled,tr.is_not_for_replication FOR JSON PATH)
FROM sys.tables t JOIN sys.schemas s ON s.schema_id=t.schema_id JOIN sys.triggers tr ON tr.parent_id=t.object_id
WHERE t.is_ms_shipped=0 AND (tr.is_disabled=1 OR tr.is_not_for_replication=1)
`

// IDs and activity timestamps are deliberately excluded from fingerprints.
const queryObjectFeatures = `
SELECT s.name,v.name,'indexed view','view',
 (SELECT ix.name,ix.type,ix.is_unique,ix.is_disabled,ix.fill_factor,ix.filter_definition,ix.is_padded,ix.ignore_dup_key,ix.allow_row_locks,ix.allow_page_locks,
  (SELECT ds.name FROM sys.data_spaces ds WHERE ds.data_space_id=ix.data_space_id FOR JSON PATH) AS storage_json,
  (SELECT p.partition_number,p.data_compression_desc FROM sys.partitions p WHERE p.object_id=ix.object_id AND p.index_id=ix.index_id ORDER BY p.partition_number FOR JSON PATH) AS compression_json,
  (SELECT COL_NAME(ic.object_id,ic.column_id) AS column_name,ic.key_ordinal,ic.is_descending_key,ic.is_included_column FROM sys.index_columns ic WHERE ic.object_id=ix.object_id AND ic.index_id=ix.index_id ORDER BY ic.index_column_id FOR JSON PATH) AS columns_json
 FROM sys.indexes ix WHERE ix.object_id=v.object_id AND ix.index_id>0 ORDER BY ix.name FOR JSON PATH) COLLATE DATABASE_DEFAULT
FROM sys.views v JOIN sys.schemas s ON s.schema_id=v.schema_id
WHERE EXISTS(SELECT 1 FROM sys.indexes ix WHERE ix.object_id=v.object_id AND ix.index_id>0)
UNION ALL
` + queryCLRObjectFeatures + `
UNION ALL
SELECT SCHEMA_NAME(ty.schema_id),ty.name,'user-defined type','type',
 (SELECT TYPE_NAME(ty.system_type_id) AS base_type,ty.max_length,ty.precision,ty.scale,ty.is_nullable,ty.is_assembly_type,ty.is_table_type,
  (SELECT tt.is_memory_optimized,
   (SELECT c.name,SCHEMA_NAME(ct.schema_id) AS type_schema,ct.name AS type_name,c.max_length,c.precision,c.scale,c.is_nullable,c.collation_name,
    c.is_identity,CAST(idc.seed_value AS nvarchar(100)) AS identity_seed,CAST(idc.increment_value AS nvarchar(100)) AS identity_increment,
    cc.definition AS computed_definition,cc.is_persisted,dc.definition AS default_definition
    FROM sys.columns c JOIN sys.types ct ON ct.user_type_id=c.user_type_id
    LEFT JOIN sys.identity_columns idc ON idc.object_id=c.object_id AND idc.column_id=c.column_id
    LEFT JOIN sys.computed_columns cc ON cc.object_id=c.object_id AND cc.column_id=c.column_id
    LEFT JOIN sys.default_constraints dc ON dc.object_id=c.default_object_id
    WHERE c.object_id=tt.type_table_object_id ORDER BY c.column_id FOR JSON PATH) AS columns_json,
   (SELECT definitions.definition FROM (
    SELECT (SELECT CASE WHEN ix.is_primary_key=0 AND ix.is_unique_constraint=0 THEN ix.name END AS name,
     ix.type,ix.is_primary_key,ix.is_unique_constraint,ix.is_unique,ix.ignore_dup_key,ix.filter_definition,hi.bucket_count,
     (SELECT COL_NAME(ic.object_id,ic.column_id) AS column_name,ic.key_ordinal,ic.is_descending_key,ic.is_included_column
      FROM sys.index_columns ic WHERE ic.object_id=ix.object_id AND ic.index_id=ix.index_id
      ORDER BY ic.index_column_id FOR JSON PATH) AS columns_json FOR JSON PATH,WITHOUT_ARRAY_WRAPPER) AS definition
    FROM sys.indexes ix LEFT JOIN sys.hash_indexes hi ON hi.object_id=ix.object_id AND hi.index_id=ix.index_id
    WHERE ix.object_id=tt.type_table_object_id AND ix.index_id>0
   ) definitions ORDER BY definitions.definition COLLATE DATABASE_DEFAULT FOR JSON PATH) AS indexes_json,
   (SELECT COL_NAME(ch.parent_object_id,ch.parent_column_id) AS column_name,ch.definition,ch.is_disabled,ch.is_not_trusted
    FROM sys.check_constraints ch WHERE ch.parent_object_id=tt.type_table_object_id
    ORDER BY ch.parent_column_id,ch.definition COLLATE DATABASE_DEFAULT FOR JSON PATH) AS checks_json
   WHERE tt.type_table_object_id IS NOT NULL FOR JSON PATH) AS table_definition_json
 FOR JSON PATH)
FROM sys.types ty LEFT JOIN sys.table_types tt ON tt.user_type_id=ty.user_type_id WHERE ty.is_user_defined=1
UNION ALL
SELECT SCHEMA_NAME(sp.schema_id),sp.name,'security policy','security_policy',
 (SELECT sp.is_enabled,sp.is_schema_bound,sp.is_not_for_replication,
  (SELECT OBJECT_SCHEMA_NAME(pr.target_object_id) AS target_schema,OBJECT_NAME(pr.target_object_id) AS target_table,
   pr.predicate_definition,pr.predicate_type,pr.operation
   FROM sys.security_predicates pr WHERE pr.object_id=sp.object_id
   ORDER BY target_schema,target_table,pr.predicate_type,pr.operation,pr.predicate_definition COLLATE DATABASE_DEFAULT FOR JSON PATH) AS predicates_json
 FOR JSON PATH)
FROM sys.security_policies sp WHERE sp.is_ms_shipped=0

UNION ALL
SELECT COALESCE(OBJECT_SCHEMA_NAME(tr.parent_id),''),COALESCE(OBJECT_NAME(tr.parent_id),tr.name),'view or database trigger',CASE WHEN tr.parent_id=0 THEN 'database' ELSE 'view' END,
 (SELECT tr.name,tr.is_disabled,tr.is_not_for_replication,OBJECT_DEFINITION(tr.object_id) AS definition FOR JSON PATH)
FROM sys.triggers tr WHERE tr.is_ms_shipped=0 AND NOT EXISTS(SELECT 1 FROM sys.tables t WHERE t.object_id=tr.parent_id)
`

// CLR modules have no SQL definition. Fingerprint their signature, binding and
// assembly contents without database-local IDs, file paths or activity dates.
const queryCLRObjectFeatures = `
SELECT SCHEMA_NAME(o.schema_id),o.name,'CLR object','object',
 (SELECT o.type_desc,a.name AS assembly_name,a.clr_name,a.permission_set_desc,a.is_visible,
  am.assembly_class,am.assembly_method,am.null_on_null_input,
  CASE WHEN am.execute_as_principal_id IS NULL THEN 'CALLER' WHEN am.execute_as_principal_id=-2 THEN 'OWNER' ELSE 'USER' END AS execute_as_mode,
  CASE WHEN am.execute_as_principal_id>0 THEN USER_NAME(am.execute_as_principal_id) END AS execute_as_user,
  (SELECT af.file_id AS assembly_file,CONVERT(varchar(64),HASHBYTES('SHA2_256',af.content),2) AS sha256
   FROM sys.assembly_files af WHERE af.assembly_id=a.assembly_id
   ORDER BY af.file_id FOR JSON PATH) AS assembly_files_json,
  (SELECT p.parameter_id,p.name,SCHEMA_NAME(pt.schema_id) AS type_schema,pt.name AS type_name,
   p.max_length,p.precision,p.scale,p.is_output,p.is_readonly,p.is_nullable,p.has_default_value,
   CONVERT(nvarchar(128),SQL_VARIANT_PROPERTY(p.default_value,'BaseType')) AS default_type,
   CONVERT(varchar(max),CONVERT(varbinary(max),p.default_value),2) AS default_value_hex
   FROM sys.parameters p JOIN sys.types pt ON pt.user_type_id=p.user_type_id
   WHERE p.object_id=o.object_id ORDER BY p.parameter_id FOR JSON PATH) AS parameters_json,
  (SELECT c.name,SCHEMA_NAME(ct.schema_id) AS type_schema,ct.name AS type_name,c.max_length,c.precision,c.scale,c.is_nullable,c.collation_name
   FROM sys.columns c JOIN sys.types ct ON ct.user_type_id=c.user_type_id
   WHERE c.object_id=o.object_id ORDER BY c.column_id FOR JSON PATH) AS result_columns_json
 FOR JSON PATH)
FROM sys.objects o
JOIN sys.assembly_modules am ON am.object_id=o.object_id
JOIN sys.assemblies a ON a.assembly_id=am.assembly_id
WHERE o.type IN ('PC','FS','FT','AF') AND o.is_ms_shipped=0
`
