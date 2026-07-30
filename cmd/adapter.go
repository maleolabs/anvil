// Package cmd implements the Anvil CLI commands.
//
// Reference: ADR-010 §6.7, EPIC-007, TS-P8-02
package cmd

import (
	"github.com/spf13/cobra"
)

// adapterCmd represents the "anvil adapter" parent command group for
// managing framework adapters.
//
// NOTE: This is a stub command group. EPIC-007 (Framework Adapter
// System) is not yet implemented. When EPIC-007 is implemented, this
// group will be populated with subcommands such as:
//   - anvil adapter list       — list available adapters
//   - anvil adapter inspect    — inspect an adapter's capabilities
//   - anvil adapter use        — set the active adapter
//
// Until EPIC-007 is implemented, running "anvil adapter" will display
// this help text indicating the feature is not yet available.
//
// Reference: ADR-010 §6.7
var adapterCmd = &cobra.Command{
	Use:   "adapter",
	Short: "Manage framework adapters",
	Long: `Discover, inspect, and configure framework adapters.

Framework adapters provide platform-specific integrations for
Anvil's release lifecycle engine, allowing projects to define
custom behaviours for packaging, deployment, and activation.

NOT YET IMPLEMENTED: This command group is a placeholder for
EPIC-007. Subcommands will be added when the Framework Adapter
System is implemented.

Examples (future):
  anvil adapter list
  anvil adapter inspect <adapter-name>
  anvil adapter use <adapter-name>`,
}

func init() {
	rootCmd.AddCommand(adapterCmd)
}
