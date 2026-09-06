package mssql

const queryViews = `SELECT s.name, v.name, m.definition FROM sys.views v JOIN sys.schemas s ON v.schema_id = s.schema_id JOIN sys.sql_modules m ON v.object_id = m.object_id WHERE v.is_ms_shipped = 0;`
const queryProcedures = `SELECT s.name, p.name, m.definition FROM sys.procedures p JOIN sys.schemas s ON p.schema_id = s.schema_id JOIN sys.sql_modules m ON p.object_id = m.object_id WHERE p.is_ms_shipped = 0;`
const queryFunctions = `SELECT s.name, f.name, m.definition FROM sys.objects f JOIN sys.schemas s ON f.schema_id = s.schema_id JOIN sys.sql_modules m ON f.object_id = m.object_id WHERE f.type IN ('FN', 'IF', 'TF') AND f.is_ms_shipped = 0;`
const queryTriggers = `SELECT s.name, t.name, tb.name, m.definition FROM sys.triggers t JOIN sys.tables tb ON t.parent_id = tb.object_id JOIN sys.schemas s ON tb.schema_id = s.schema_id JOIN sys.sql_modules m ON t.object_id = m.object_id WHERE t.is_ms_shipped = 0;`
const querySynonyms = `SELECT s.name, syn.name, syn.base_object_name FROM sys.synonyms syn JOIN sys.schemas s ON syn.schema_id = s.schema_id;`
const querySequences = `SELECT s.name, seq.name, CAST(seq.start_value AS BIGINT), CAST(seq.increment AS BIGINT), CAST(seq.minimum_value AS BIGINT), CAST(seq.maximum_value AS BIGINT), seq.is_cycling, CAST(ISNULL(seq.cache_size, 0) AS BIGINT) FROM sys.sequences seq JOIN sys.schemas s ON seq.schema_id = s.schema_id;`
const queryTemporalTables = `SELECT s.name, t.name, t.temporal_type, hs.name, ht.name, pc1.name, pc2.name FROM sys.tables t JOIN sys.schemas s ON t.schema_id = s.schema_id LEFT JOIN sys.tables ht ON t.history_table_id = ht.object_id LEFT JOIN sys.schemas hs ON ht.schema_id = hs.schema_id LEFT JOIN sys.periods p ON t.object_id = p.object_id LEFT JOIN sys.columns pc1 ON p.object_id = pc1.object_id AND p.start_column_id = pc1.column_id LEFT JOIN sys.columns pc2 ON p.object_id = pc2.object_id AND p.end_column_id = pc2.column_id WHERE t.temporal_type = 2;`
const queryPartitionFunctions = `SELECT pf.name, t.name, prv.value FROM sys.partition_functions pf JOIN sys.partition_parameters pp ON pf.function_id = pp.function_id JOIN sys.types t ON pp.system_type_id = t.system_type_id AND pp.user_type_id = t.user_type_id LEFT JOIN sys.partition_range_values prv ON pf.function_id = prv.function_id ORDER BY pf.name, prv.boundary_id;`
const queryPartitionSchemes = `SELECT ps.name, pf.name, fg.name FROM sys.partition_schemes ps JOIN sys.partition_functions pf ON ps.function_id = pf.function_id JOIN sys.destination_data_spaces dds ON ps.data_space_id = dds.partition_scheme_id JOIN sys.filegroups fg ON dds.data_space_id = fg.data_space_id ORDER BY ps.name, dds.destination_id;`
const queryReplication = `SELECT s.name, t.name, t.is_replicated FROM sys.tables t JOIN sys.schemas s ON t.schema_id = s.schema_id WHERE t.is_replicated = 1;`
const queryExtendedProperties = `
SELECT
    ep.class,
    COALESCE(s.name, ''),
    COALESCE(o.name, ''),
    COALESCE(c.name, ''),
    ep.name,
    CAST(ep.value AS NVARCHAR(MAX))
FROM sys.extended_properties ep
LEFT JOIN sys.objects o ON ep.major_id = o.object_id AND ep.class IN (1, 2, 7)
LEFT JOIN sys.schemas s ON o.schema_id = s.schema_id
LEFT JOIN sys.columns c ON ep.major_id = c.object_id AND ep.minor_id = c.column_id AND ep.class = 1;
`
