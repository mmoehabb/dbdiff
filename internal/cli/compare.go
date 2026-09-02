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
	format           string
	output           string
	allowDestructive bool
	quiet            bool
)

var compareCmd = &cobra.Command{
	Use:   "compare",
	Short: "Compares two databases and outputs the differences or a migration script",
	Run: func(cmd *cobra.Command, args []string) {
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

		// Handle destructive operations
		if plan.HasDestructiveOperations() {
			if !allowDestructive {
				fmt.Fprintln(os.Stderr, "Warning: Destructive operations detected. They have been omitted.")
				fmt.Fprintln(os.Stderr, "Use --allow-destructive to include them.")

				// Filter out destructive operations
				filteredOps := []diff.Operation{}
				for _, op := range plan.SchemaOperations {
					if !op.IsDestructive() {
						filteredOps = append(filteredOps, op)
					}
				}
				plan.SchemaOperations = filteredOps
			}
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

		// Exit codes: 0 = no differences, 1 = differences found, 2 = error (handled above)
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
	compareCmd.Flags().StringVarP(&format, "format", "f", "sql", "Output format (sql, json, text)")
	compareCmd.Flags().StringVarP(&output, "output", "o", "", "Output file (default is stdout)")
	compareCmd.Flags().BoolVar(&allowDestructive, "allow-destructive", false, "Allow destructive operations like DROP TABLE")
	compareCmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "Suppress progress messages")

	compareCmd.MarkFlagRequired("source")
	compareCmd.MarkFlagRequired("target")

	rootCmd.AddCommand(compareCmd)
}
