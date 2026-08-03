// Package cmd implements the Anvil CLI commands.
//
// Reference: ADR-010 §6.7, EPIC-007
package cmd

import (
	"github.com/spf13/cobra"
)

// adapterCmd represents the "anvil adapter" parent command group for
// managing framework adapters.
//
// The group is a parent-only namespace (ADR-010 §6.7): it has no RunE,
// Run, or Args — running "anvil adapter" displays the group help listing
// the subcommands below.
//
// Reference: ADR-010 §6.7, EPIC-007, TS-007-031, TS-007-032, TS-007-033,
// TS-007-037
var adapterCmd = &cobra.Command{
	Use:   "adapter",
	Short: "Manage framework adapters",
	Long: `Discover, inspect, install, and configure framework adapters.

Framework adapters provide platform-specific integrations for
Anvil's release lifecycle engine, allowing projects to define
custom behaviours for packaging, deployment, and activation.

Subcommands:
  list       List available adapters
  inspect    Inspect an adapter's capabilities
  use        Set the active framework for the project
  install    Install an adapter binary from the release
  uninstall  Uninstall an installed adapter binary

Examples:
  anvil adapter list
  anvil adapter inspect laravel
  anvil adapter use laravel
  anvil adapter install laravel
  anvil adapter uninstall laravel`,
}

func init() {
	rootCmd.AddCommand(adapterCmd)
	adapterCmd.AddCommand(adapterListCmd, adapterInspectCmd, adapterUseCmd, adapterInstallCmd, adapterUninstallCmd)
}
