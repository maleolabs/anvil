// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P5-07, ADR-013, EPIC-005
package cmd

import (
	"github.com/spf13/cobra"
)

// serverConfigCmd represents the "anvil server config" parent command
// for inspecting and modifying Server Runtime configuration at
// /etc/anvil/config.yaml (or configured override path).
//
// Reference: ST-P5-07, ADR-013
var serverConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage Server Runtime configuration",
	Long: `Inspect and modify the Server Runtime configuration stored at
/etc/anvil/config.yaml (or configured override path).

Subcommands:
  set   Set a configuration value (e.g., runtime.id)
  get   Get a configuration value

The configuration is persisted in YAML format and shared across all
Anvil server commands operating on this Runtime.`,
}

func init() {
	serverCmd.AddCommand(serverConfigCmd)
}
