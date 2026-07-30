// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P1-01, ADR-010, ADR-012
package cmd

import (
	"github.com/spf13/cobra"
)

// CliVersion is the Anvil CLI version, set at build time via ldflags.
// When not overridden, it defaults to "0.0.0-dev".
var CliVersion = "0.0.0-dev"

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   "anvil",
	Short: "Release lifecycle engine for single-server deployments",
	Long: `Anvil is a release lifecycle engine for single-server deployments.

It provides a standardized, framework-agnostic toolkit for initializing
projects, packaging releases, activating deployments, and rolling back
when needed.

Complete documentation is available at https://maleolabs.com/anvil`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once.
func Execute() error {
	// Set version from the CliVersion variable, which may be set via ldflags
	// at build time (e.g., go build -ldflags="-X maleolabs.com/anvil/cmd.CliVersion=1.0.0").
	// This ensures --version flag reflects the actual release version.
	rootCmd.Version = CliVersion
	return rootCmd.Execute()
}

// ── Extension Points ──────────────────────────────────────────────
//
// EPIC-007 Adapter Commands (not yet implemented):
//
//	When EPIC-007 is implemented, adapter commands should be registered
//	by creating cmd/adapter.go with an init() function that calls:
//
//	    rootCmd.AddCommand(adapterCmd)
//
//	The adapterCmd variable should be defined as a parent-only cobra.Command
//	group (Use: "adapter") in that file. Domain-specific subcommands
//	(e.g., "anvil adapter list", "anvil adapter inspect") should be added
//	as children of adapterCmd.
//
//	See ADR-010 §6.7 for the command hierarchy specification.
// ──────────────────────────────────────────────────────────────────

func init() {
	rootCmd.AddCommand(initCmd)
}
