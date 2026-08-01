// Package cmd implements the Anvil CLI commands.
//
// Reference: ADR-010 §6.8, TS-P8-02
package cmd

import (
	"github.com/spf13/cobra"
)

// systemCmd represents the "anvil system" parent command group for
// system-level operations such as diagnostics, version information,
// and health checks.
//
// This is a parent-only command group. It does not perform any action
// by itself — it serves as a namespace for system subcommands.
//
// Reference: ADR-010 §6.8
var systemCmd = &cobra.Command{
	Use:   "system",
	Short: "System-level operations",
	Long: `Inspect and manage Anvil system information and health.

System commands provide operators with tools for diagnostics,
version inspection, configuration validation, and health checks
of the Anvil CLI and its environment.

Examples:
  anvil system version
  anvil system health
  anvil system info`,
}

func init() {
	rootCmd.AddCommand(systemCmd)
}
