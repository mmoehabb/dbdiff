package text

import (
	"context"
	"fmt"
	"strings"

	"github.com/mmoehabb/dbdiff/internal/domain/diff"
)

// TextRenderer implements the ports.Renderer interface for simple text format.
type TextRenderer struct{}

func NewTextRenderer() *TextRenderer {
	return &TextRenderer{}
}

func (r *TextRenderer) Render(ctx context.Context, plan *diff.MigrationPlan) (string, error) {
	var builder strings.Builder

	for _, op := range plan.SchemaOperations {
		builder.WriteString(r.renderOperation(op))
		builder.WriteString("\n")
	}

	return builder.String(), nil
}

func (r *TextRenderer) renderOperation(op diff.Operation) string {
	switch o := op.(type) {
	case diff.CreateSchemaOperation:
		return fmt.Sprintf("+ CREATE SCHEMA %s", o.SchemaName)
	case diff.DropSchemaOperation:
		return fmt.Sprintf("- DROP SCHEMA %s", o.SchemaName)
	case diff.CreateTableOperation:
		return fmt.Sprintf("+ CREATE TABLE %s.%s", o.SchemaName, o.Table.Name)
	case diff.DropTableOperation:
		return fmt.Sprintf("- DROP TABLE %s.%s", o.SchemaName, o.TableName)
	case diff.AddColumnOperation:
		return fmt.Sprintf("+ ADD COLUMN %s.%s.%s", o.SchemaName, o.TableName, o.Column.Name)
	case diff.DropColumnOperation:
		return fmt.Sprintf("- DROP COLUMN %s.%s.%s", o.SchemaName, o.TableName, o.ColumnName)
	case diff.AlterColumnOperation:
		return fmt.Sprintf("~ ALTER COLUMN %s.%s.%s", o.SchemaName, o.TableName, o.Column.Name)
	case diff.AddPrimaryKeyOperation:
		pkName := o.PrimaryKey.Name
		if pkName == "" {
			pkName = fmt.Sprintf("PK_%s", o.TableName)
		}
		return fmt.Sprintf("+ ADD PRIMARY KEY %s TO %s.%s", pkName, o.SchemaName, o.TableName)
	case diff.DropPrimaryKeyOperation:
		return fmt.Sprintf("- DROP PRIMARY KEY FROM %s.%s", o.SchemaName, o.TableName)
	case diff.AddForeignKeyOperation:
		fkName := o.ForeignKey.Name
		if fkName == "" {
			fkName = fmt.Sprintf("FK_%s_%s", o.TableName, o.ForeignKey.RefTable)
		}
		return fmt.Sprintf("+ ADD FOREIGN KEY %s TO %s.%s", fkName, o.SchemaName, o.TableName)
	case diff.DropForeignKeyOperation:
		return fmt.Sprintf("- DROP FOREIGN KEY %s FROM %s.%s", o.ForeignKeyName, o.SchemaName, o.TableName)
	case diff.CreateIndexOperation:
		idxName := o.Index.Name
		if idxName == "" {
			idxName = fmt.Sprintf("IX_%s_%s", o.TableName, strings.Join(o.Index.Columns, "_"))
		}
		return fmt.Sprintf("+ CREATE INDEX %s ON %s.%s", idxName, o.SchemaName, o.TableName)
	case diff.DropIndexOperation:
		return fmt.Sprintf("- DROP INDEX %s ON %s.%s", o.IndexName, o.SchemaName, o.TableName)
	default:
		return fmt.Sprintf("? UNKNOWN OPERATION: %T", op)
	}
}
