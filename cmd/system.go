// Package cmd implements the Anvil CLI commands.
//
// Reference: ADR-010 §6.8, TS-P8-02
package cmd

import (
	"github.com/spf13/cobra"
)

// systemCmd represents the "anvil system" parent command group for
// system-level operations such as environment inspection.
//
// This is a parent command group that serves as a namespace for system
// subcommands. Like serverReleaseCmd, it combines a help-displaying Run
// with NoArgsWithSuggestions so that unknown subcommands produce a clear
// "unknown command" error instead of silently falling back to group help
// with exit 0 (BUG-012 Validation step 4).
//
// Scope note (ADR-036): the platform-ops breadth — system health and
// readiness verdicts ("anvil system health") and recommendations-style
// diagnostics ("anvil system diagnose") — was demoted/removed per
// ADR-036 §3 (TS-015-05-02). "anvil system inspect" remains as a
// targeted component inspection surface, and lifecycle observability
// lives on "anvil server status".
//
// Reference: ADR-010 §6.8
var systemCmd = &cobra.Command{
	Use:   "system",
	Short: "System-level operations",
	Long: `Inspect the Anvil CLI and its environment.

System commands provide operators with tools for targeted component
inspection of the Anvil CLI and its runtime.

Examples:
  anvil system inspect`,
	// Reject unknown subcommands ("anvil system version") with a clear
	// error instead of falling back to group help with exit code 0
	// (BUG-012 Validation step 4).
	Args: NoArgsWithSuggestions(),
	Run: func(cmd *cobra.Command, args []string) {
		cmd.HelpFunc()(cmd, args)
	},
}

func init() {
	rootCmd.AddCommand(systemCmd)
}
