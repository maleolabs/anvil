// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P4-01, ADR-010, ADR-012, ADR-013, ADR-014, EPIC-004
package cmd

import (
	"github.com/spf13/cobra"
)

// serverCmd represents the "anvil server" parent command for managing
// Anvil Server Runtime instances.
//
// Server commands allow operators to manage Runtime instances, install
// artifacts, activate releases, and inspect Runtime state.
//
// Reference: ADR-013, ADR-014
var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Manage Anvil Server Runtime instances",
	Long: `Manage Server Runtime instances.

Server Runtime commands allow operators to install artifacts, create
Releases, activate Releases, and inspect Runtime state on registered
Anvil Server Runtime environments.`,
}

func init() {
	rootCmd.AddCommand(serverCmd)
}
