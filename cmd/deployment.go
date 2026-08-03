// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P10-01, ST-P10-02, ADR-015, EPIC-010
package cmd

import (
	"github.com/spf13/cobra"
)

// deploymentCmd represents the "anvil deployment" parent command for
// managing deployment targets and artifact delivery.
//
// Deployment commands allow operators to inspect deployment targets,
// upload artifacts, and manage the delivery context without reading
// Runtime Registry, State, release directories, or symlinks (ADR-015).
//
// Reference: ST-P10-01, ST-P10-02, ADR-015, EPIC-010
var deploymentCmd = &cobra.Command{
	Use:   "deployment",
	Short: "Manage deployments",
	Long: `Manage deployment targets and artifact delivery.

Deployment commands allow operators to inspect deployment target
identity and delivery context, and upload artifacts to targets
through configured transport mechanisms.

Deployment context does NOT read Registry, State, release directories,
or symlinks — it is independent from Runtime filesystem layout
per ADR-015.`,
}

func init() {
	rootCmd.AddCommand(deploymentCmd)
}
