package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var listDriversCmd = &cobra.Command{
	Use:   "list-drivers",
	Short: "Lists all supported DBMS drivers",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Supported drivers:")
		fmt.Println("- mssql")
	},
}

func init() {
	rootCmd.AddCommand(listDriversCmd)
}
