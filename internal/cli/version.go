package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version can be set at build time
var Version = "v0.1.2"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version of dbdiff",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("dbdiff %s\n", Version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
