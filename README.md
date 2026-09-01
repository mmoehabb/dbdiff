# dbdiff

> **Note:** This project is currently a work in progress. Please refer to the [TODO / V1 Roadmap](#todo--v1-roadmap) section below to see what remains to be implemented for the initial release.

`dbdiff` is a database-diff engine built to compare database schemas and generate migration scripts. It is designed to be database-agnostic at its core, enabling comparisons and potential extensions to various relational database systems like SQL Server, PostgreSQL, and MySQL.

Currently, `dbdiff` provides the foundational architecture mapped out in [V1_PLAN.md](V1_PLAN.md) including standard models for schemas, migrations, diffing operations, and a skeletal implementation of an MSSQL adapter.

## Installation

Ensure you have [Go](https://go.dev/) (version 1.20+ recommended) installed.

Clone the repository and build the project:

```bash
git clone <repository_url>
cd dbdiff
go build -o dbdiff ./cmd/dbdiff
```

You can then run `./dbdiff` from your terminal or add it to your `PATH`.

## How to Compile and Run

To compile the code manually during development:

```bash
go build ./...
```

To run the application directly without building a persistent binary:

```bash
go run ./cmd/dbdiff
```

To run tests:

```bash
go test ./...
```

## Overview of How to Use the Tool

The CLI provides a `compare` command which compares two databases. The target will be analyzed and compared against the source. The resulting migration will represent instructions on how to make the `target` match the `source`.

```bash
dbdiff compare --source "sqlserver://connection-string-a" --target "sqlserver://connection-string-b"
```

Options:
- `-s, --source`: Source database connection string (required).
- `-t, --target`: Target database connection string (required).
- `-f, --format`: Output format, e.g., `sql`, `json`, `text` (default is `sql`).
- `-o, --output`: Optional file to output the diff to (prints to `stdout` by default).

Example to generate a file:
```bash
dbdiff compare --source "prod-db" --target "staging-db" --output migration.sql
```

_Note: In the current v1 foundation release, the tool only outlines the core structure and parses arguments. Actual MSSQL logic implementation is stubbed pending future phases._

## Architecture

The underlying architecture prioritizes the complete separation of core logic from database-specific mechanisms. It operates through three main phases defined by three different models:

1. **Introspection & Canonical Schema Model**:
   Adapters (e.g., MSSQL Introspector) read specific database metadata and convert it into a **Canonical Schema Model**. This represents the data exactly as it is structured in an abstract, DB-agnostic form.

2. **Diff Engine & Migration Plan**:
   The engine reads two canonical schemas (source and target) and computes a **Migration Plan**. The migration plan is an Intermediate Representation (IR) consisting of explicit, directional operations (e.g., `AddColumnOperation`, `CreateTableOperation`) designed to turn the target into the source.

3. **Renderer & SQL Generation**:
   A target-specific adapter (e.g., MSSQL Renderer) accepts the Migration Plan and generates native database logic (like T-SQL DDL scripts) to execute the migration safely.

By using this approach, adding support for other engines like PostgreSQL or MySQL primarily involves creating new Introspectors and Renderers without modifying the complex schema diffing engine itself. Future phases also plan to seamlessly support Data Operations alongside Schema Operations in the Migration Plan.

For a comprehensive dive into the design decisions and roadmaps, please refer to the [V1_PLAN.md](V1_PLAN.md) file at the root of the project.

## TODO / V1 Roadmap

The foundational layer (Phase 1) is complete. The following phases remain to get V1 released:

- **Phase 2 — Schema diff**
  - Implement comparison logic for: schemas, tables, columns, types, nullability, identity, defaults, PKs, FKs, and indexes.
- **Phase 3 — Migration plan**
  - Introduce intermediate representations like `MigrationPlan` and `MigrationOperation`.
  - Implement dependency ordering to ensure safe execution (e.g., creating tables before adding foreign keys).
  - Add destructive operation detection.
- **Phase 4 — MSSQL SQL renderer**
  - Generate target-specific SQL DDL scripts for all supported schema changes (e.g., `CREATE SCHEMA`, `CREATE TABLE`, `ALTER TABLE`, etc.).
- **Phase 5 — CLI UX**
  - Add support for multiple output formats (text, JSON, SQL output).
  - Implement `--output`, `--allow-destructive` flags, and standardize exit codes for CI integration.
- **Phase 6 — Testing**
  - Expand test coverage including unit tests, diff/SQL golden tests, and MSSQL integration tests (potentially using Testcontainers).
