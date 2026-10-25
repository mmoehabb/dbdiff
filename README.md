# dbdiff

`dbdiff` compares SQL Server databases and generates a script that makes the **target match the source**. It generates output; it does not execute migrations.

## Build and compare

Requires Go 1.24.3 or later. The MSSQL adapter targets SQL Server 2019 or later and requires database-level `VIEW DEFINITION` permission on both connections. Data comparison also needs `SELECT` on the selected tables.

```bash
go build -o dbdiff .
./dbdiff compare --driver mssql \
  --source 'sqlserver://user:password@source-host?database=SourceDB' \
  --target 'sqlserver://user:password@target-host?database=TargetDB' \
  --output migration.sql
```

Options:

- `--format sql|json|text`: output format; defaults to SQL.
- `--allow-destructive`: include drops and potentially data-losing column changes.
- `--tables dbo.Articles,dbo.ArticleViews`: compare selected tables, including their columns, indexes, constraints, triggers, and supported properties.
- `--exclude`: exclude the tables listed by `--tables`.
- `--data`: include row synchronization for supported tables with compatible primary keys.
- `--quiet`: suppress progress messages, while retaining errors and omission warnings.

Exit codes are `0` for no differences, `1` for differences (including omitted changes), and `2` for errors.

## Migration safety

An existing index with a changed definition needs a replacement. Without `--allow-destructive`, dbdiff omits the entire replacement and its dependent changes, rather than emitting `CREATE INDEX` while leaving the existing index in place. The same rule covers keys, constraints, and structural dependency rebuilds. Independent changes remain in the output. Omission reasons appear on stderr and in SQL comments, JSON, and text output.

The planner uses stable operation identifiers, atomic change groups, and dependency ordering. JSON retains `type`, `is_destructive`, and `data`, and adds step identifiers, dependency information, and an `omitted` array.

Generated SQL uses a single transaction with `XACT_ABORT`, rollback, and error propagation. Schema statements run in separate compilation scopes. Data scripts keep constraints enabled. Run a script outside an existing transaction, against the target state that was compared. Regenerate it after applying changes or after the target changes; scripts are not intended to be replayed.

`--tables` does not migrate standalone database objects. A selection that needs changes to an unselected dependency is rejected. Data selections with dependent tables outside the selection are also rejected, preventing cascading changes outside the requested scope.

## Supported scope and explicit limits

The adapter preserves supported built-in SQL types and parameters, column collation and identity definitions, defaults, ordinary rowstore indexes (including descending keys, includes, filters, uniqueness, clustering, and modeled options), primary/unique/check/foreign-key constraints, and foreign-key actions. It also compares ordinary SQL modules, synonyms, sequences, and supported extended properties at database/schema/table/column/view/procedure/function levels. Primary-key and default-constraint names alone do not trigger replacement; actual target names are used when removing constraints.

Unsupported cases produce errors rather than approximate SQL. These include temporal/partitioned/replicated or specialized storage, non-default filegroup placement, specialized or disabled indexes, indexed views, CLR/user-defined types, disabled or untrusted constraints, unsupported property types or levels, unresolved external dependencies, case-only renames, and identity/computed transformations that need a table rebuild. Function-kind changes (scalar, inline table-valued, or multi-statement table-valued) require an explicit drop/recreate migration. Existing schema-bound dependencies can require an explicit migration. Capability checks can reject a comparison even when an unsupported feature is unchanged. This is a schema/data tool, not a full database backup or a security/principal migration tool.

Data synchronization supports new tables and ordinary existing tables with compatible, writable primary keys. It preserves binary values, UUIDs, and temporal offsets, excludes generated/computed columns from writes, and rejects keyless synchronization, text primary keys requiring collation-aware matching, cyclic/self-referencing foreign keys, and unsupported unique/referenced-key transitions. Data is read into memory; use table selection for large databases. Quiesce concurrent schema/data changes while comparing and applying a migration.

Extended properties preserve supported string, integer, decimal, money, binary, UUID, and NULL values and their SQL types. Other value types are rejected explicitly. Sequence option changes do not restart the current sequence; changing a sequence's start/type requires an explicit migration.

## Validation

```bash
go test -race -short ./...
go vet ./...

# Requires Docker; creates and removes an isolated SQL Server container/databases.
DBDIFF_REQUIRE_MSSQL=1 go test -v -count=1 -timeout=15m \
  ./internal/adapters/mssql -run TestMSSQLIntegration
```

Integration tests inspect independent source/target databases, generate and apply the exact SQL output, then require a second comparison to converge. They also cover the same-name index regression, statistics collisions, constraint enforcement, and rollback on failure. CI requires these tests to execute; an unavailable Docker/SQL Server environment fails the integration job. Local runs without `DBDIFF_REQUIRE_MSSQL=1` report an explicit skip if Docker is unavailable.
