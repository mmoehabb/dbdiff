package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mmoehabb/dbdiff/internal/adapters/json"
	"github.com/mmoehabb/dbdiff/internal/adapters/mssql"
	"github.com/mmoehabb/dbdiff/internal/adapters/text"
	"github.com/mmoehabb/dbdiff/internal/application"
	"github.com/mmoehabb/dbdiff/internal/domain/diff"
	"github.com/mmoehabb/dbdiff/internal/ports"
)

var (
	source           string
	target           string
	driver           string
	format           string
	output           string
	allowDestructive bool
	quiet            bool
	tables           []string
	exclude          bool
	dataFlag         bool
)

var compareCmd = &cobra.Command{
	Use:   "compare",
	Short: "Compares two databases and outputs the differences or a migration script",
	Run: func(cmd *cobra.Command, args []string) {
		if driver != "mssql" {
			fmt.Fprintln(os.Stderr, "Sorry, this DBMS name either wrong or not supported yet. You can list the supported DBMS with the command: dbdiff list-drivers")
			os.Exit(2)
		}

		if exclude && len(tables) == 0 {
			fmt.Fprintln(os.Stderr, "Error: --exclude can only be used when --tables is provided")
			os.Exit(2)
		}

		ctx := context.Background()

		// Initialize introspectors
		sourceIntrospector := mssql.NewMSSQLIntrospector(source)
		targetIntrospector := mssql.NewMSSQLIntrospector(target)
		differ := diff.NewSchemaDiffer()

		if !quiet {
			fmt.Fprintln(os.Stderr, "Introspecting source database...")
		}
		sourceSchema, err := sourceIntrospector.Inspect(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error introspecting source database: %v\n", err)
			os.Exit(2)
		}

		if !quiet {
			fmt.Fprintln(os.Stderr, "Introspecting target database...")
		}
		targetSchema, err := targetIntrospector.Inspect(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error introspecting target database: %v\n", err)
			os.Exit(2)
		}

		if len(tables) > 0 {
			for _, tableReq := range tables {
				found := false
				for schemaName, s := range sourceSchema.Schemas {
					for tableName := range s.Tables {
						if fmt.Sprintf("%s.%s", schemaName, tableName) == tableReq {
							found = true
							break
						}
					}
					if found {
						break
					}
				}
				if !found && !exclude {
					fmt.Fprintf(os.Stderr, "\033[33mWarning: Table %s was requested but not found in the source database.\033[0m\n", tableReq)
				}
			}
		}

		sourceSchema, targetSchema, err = application.SelectTables(sourceSchema, targetSchema, application.CompareOptions{Tables: tables, Exclude: exclude, IncludeData: dataFlag})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error selecting tables: %v\n", err)
			os.Exit(2)
		}

		if !quiet {
			fmt.Fprintln(os.Stderr, "Comparing schemas...")
		}
		result, err := application.Compare(ctx, sourceSchema, targetSchema, differ)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error comparing databases: %v\n", err)
			os.Exit(2)
		}

		plan := result.Plan
		hasDifferences := len(plan.SchemaOperations) > 0
		plan.ApplySafety(allowDestructive)

		if dataFlag {
			if !quiet {
				fmt.Fprintln(os.Stderr, "Comparing data...")
			}
			sourceDataReader := mssql.NewMSSQLDataReader(source)
			targetDataReader := mssql.NewMSSQLDataReader(target)
			dataDiffer := diff.NewDataDiffer(sourceDataReader, targetDataReader)

			dataOps, err := dataDiffer.CompareData(ctx, sourceSchema, targetSchema, plan.BlockedTables)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error comparing data: %v\n", err)
				os.Exit(2)
			}
			plan.DataOperations = dataOps
			plan.Incomplete = plan.Incomplete || dataDiffer.Incomplete
			for _, table := range dataDiffer.OmittedTables {
				plan.Omitted = append(plan.Omitted, diff.Diagnostic{ID: "data:" + table, Reason: dataDiffer.OmittedReasons[table]})
			}
		}

		hasDifferences = hasDifferences || len(plan.DataOperations) > 0

		plan.FilterData(allowDestructive)
		for _, diagnostic := range plan.Omitted {
			fmt.Fprintf(os.Stderr, "Warning: omitted %s: %s\n", diagnostic.ID, diagnostic.Reason)
		}

		// Select renderer
		var renderer ports.Renderer
		switch format {
		case "json":
			renderer = json.NewJSONRenderer()
		case "text":
			renderer = text.NewTextRenderer()
		case "sql":
			renderer = mssql.NewMSSQLRenderer()
		default:
			fmt.Fprintf(os.Stderr, "Error: Unknown format '%s'\n", format)
			os.Exit(2)
		}

		if !quiet {
			fmt.Fprintln(os.Stderr, "Generating output...")
		}
		renderedOutput, err := renderer.Render(ctx, plan)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error rendering output: %v\n", err)
			os.Exit(2)
		}

		// Output handling
		if output != "" {
			err := os.WriteFile(output, []byte(renderedOutput), 0644)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error writing to output file: %v\n", err)
				os.Exit(2)
			}
		} else {
			fmt.Println(renderedOutput)
		}

		// Exit codes: 0 = no differences, 1 = differences, 2 = error, 3 = unsupported omissions.
		if plan.Incomplete {
			os.Exit(3)
		}
		if hasDifferences {
			os.Exit(1)
		} else {
			os.Exit(0)
		}
	},
}

func init() {
	compareCmd.Flags().StringVarP(&source, "source", "s", "", "Source database connection string (required)")
	compareCmd.Flags().StringVarP(&target, "target", "t", "", "Target database connection string (required)")
	compareCmd.Flags().StringVarP(&driver, "driver", "d", "", "DBMS driver (e.g. mssql) (required)")
	compareCmd.Flags().StringVarP(&format, "format", "f", "sql", "Output format (sql, json, text)")
	compareCmd.Flags().StringVarP(&output, "output", "o", "", "Output file (default is stdout)")
	compareCmd.Flags().BoolVar(&allowDestructive, "allow-destructive", false, "Allow destructive operations like DROP TABLE")
	compareCmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "Suppress progress messages")
	compareCmd.Flags().StringSliceVar(&tables, "tables", []string{}, "Comma-separated list of tables to include (e.g., dbo.users,dbo.orders)")
	compareCmd.Flags().BoolVar(&exclude, "exclude", false, "Exclude the tables specified in --tables instead of including them")
	compareCmd.Flags().BoolVar(&dataFlag, "data", false, "Include data migration in comparison")

	compareCmd.MarkFlagRequired("source")
	compareCmd.MarkFlagRequired("target")
	compareCmd.MarkFlagRequired("driver")

	rootCmd.AddCommand(compareCmd)
}
