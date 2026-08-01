// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P5-08, TS-P5-13, ADR-013, EPIC-005
package cmd

import (
	"github.com/spf13/cobra"
)

// serverProjectCmd represents the "anvil server project" parent command for
// managing registered projects in the Server Runtime Registry.
//
// Projects are registered under /etc/anvil/projects/{project-id}.yaml and
// are used to associate Runtime Releases with a project identity and
// install root.
//
// Reference: ST-P5-08, ADR-013
var serverProjectCmd = &cobra.Command{
	Use:   "project",
	Short: "Manage registered projects",
	Long: `Manage registered projects in the Server Runtime Registry.

Projects are registered under /etc/anvil/projects/{project-id}.yaml.

Commands under 'project' allow operators to register new projects and
look up existing project configurations.`,
}

func init() {
	serverCmd.AddCommand(serverProjectCmd)
}
