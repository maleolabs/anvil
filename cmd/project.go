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
// Reference: ADR-010 §6.1, ADR-015
var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Manage project configuration and metadata",
	Long: `Manage project-level settings, versioning, and metadata.

Project commands operate within an Anvil project context and require
anvil.yaml to be present in the current or parent directory.`,
}

func init() {
	rootCmd.AddCommand(projectCmd)
}
