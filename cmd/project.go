// Package cmd implements the Anvil CLI commands.
//
// Reference: ADR-010, EPIC-001
package cmd

import (
	"github.com/spf13/cobra"
)

// projectCmd represents the 'anvil project' parent command for managing
// project-level configuration and metadata.
//
// This is a parent command group that serves as a namespace for project
// subcommands. Like systemCmd and serverReleaseCmd, it combines a
// help-displaying Run with NoArgsWithSuggestions so that unknown
// subcommands — including the undocumented surfaces removed in
// TS-019-04-02 ("project remove", "project version") — produce a clear
// "unknown command" error instead of silently falling back to group
// help with exit 0 (BUG-012 Validation step 4).
//
// Reference: ADR-010 §6.1, ADR-015
var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Manage project configuration and metadata",
	Long: `Manage project-level settings and metadata.

Project commands operate within an Anvil project context and require
anvil.yaml to be present in the current or parent directory.`,
	// Reject unknown subcommands with a clear error instead of falling
	// back to group help with exit code 0 (BUG-012 Validation step 4).
	Args: NoArgsWithSuggestions(),
	Run: func(cmd *cobra.Command, args []string) {
		cmd.HelpFunc()(cmd, args)
	},
}

func init() {
	rootCmd.AddCommand(projectCmd)
}
