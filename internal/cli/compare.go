package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	source string
	target string
	format string
	output string
)

var compareCmd = &cobra.Command{
	Use:   "compare",
	Short: "Compares two databases and outputs the differences or a migration script",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Comparing databases...")
		fmt.Printf("Source: %s\n", source)
		fmt.Printf("Target: %s\n", target)
		fmt.Printf("Format: %s\n", format)

		// In the future this will initialize adapters and call application.Compare
		fmt.Println("Migration direction: target -> source")
		fmt.Println("Done.")
	},
}

func init() {
	compareCmd.Flags().StringVarP(&source, "source", "s", "", "Source database connection string (required)")
	compareCmd.Flags().StringVarP(&target, "target", "t", "", "Target database connection string (required)")
	compareCmd.Flags().StringVarP(&format, "format", "f", "sql", "Output format (sql, json, text)")
	compareCmd.Flags().StringVarP(&output, "output", "o", "", "Output file (default is stdout)")

	compareCmd.MarkFlagRequired("source")
	compareCmd.MarkFlagRequired("target")

	rootCmd.AddCommand(compareCmd)
}
