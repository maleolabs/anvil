// Package cmd implements the Anvil CLI commands.
//
// Reference: ADR-010 §6.2, TS-P8-02
package cmd

import (
	"github.com/spf13/cobra"
)

// releaseCmd represents the "anvil release" parent command group for
// managing the release lifecycle.
//
// This is a top-level command group (parent only) that serves as a
// namespace for release-related operations. It does not perform any
// action by itself.
//
// NOTE: This is distinct from "anvil server release" (defined in
// cmd/server_release.go, variable name serverReleaseCmd). The top-level
// "anvil release" group is for release lifecycle management at the
// project level, while "anvil server release" manages releases on an
// Anvil Server Runtime instance.
//
// Reference: ADR-010 §6.2
var releaseCmd = &cobra.Command{
	Use:   "release",
	Short: "Manage release lifecycle",
	Long: `Manage the release lifecycle for Anvil projects.

Release commands allow operators to create, inspect, and manage
releases through their lifecycle stages: prepare, release, activate,
and archive.

Releases are versioned, immutable snapshots of a project at a point
in time, tied to a specific artifact identity.

Examples:
  anvil release list
  anvil release show <release-id>`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.HelpFunc()(cmd, args)
	},
}

func init() {
	rootCmd.AddCommand(releaseCmd)
}
