package cmd

import (
	"github.com/spf13/cobra"
)

// runtimeCmd represents the "anvil runtime" parent command for managing
// Anvil Runtime instances. It does not perform any action by itself — it
// serves as a namespace for subcommands such as "anvil runtime readiness".
//
// Reference: ST-P5-02
var runtimeCmd = &cobra.Command{
	Use:   "runtime",
	Short: "Manage Anvil Runtime instances",
	Long: `Inspect and manage Runtime instances.

Runtime commands allow operators to observe the condition of Anvil Runtime
environments, check readiness, and manage Runtime metadata.`,
}

func init() {
	rootCmd.AddCommand(runtimeCmd)
}
