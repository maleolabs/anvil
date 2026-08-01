// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P10-05, ADR-015, EPIC-010
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/server"
)

// deploymentRollbackCmd represents the "anvil deployment rollback" subcommand
// that rolls back the currently Active Release on a deployment target,
// restoring the previously Active Release.
//
// Per ADR-015, this command delegates rollback to the Server Runtime
// command surface. It reports the rollback outcome without reading
// Runtime internals.
//
// Reference: ST-P10-05, ADR-015
var deploymentRollbackCmd = &cobra.Command{
	Use:   "rollback <target-id> <project-id>",
	Short: "Rollback the Active Release on a deployment target",
	Long: `Rollback the currently Active Release on a deployment target,
restoring the previously Active Release.

The rollback process:
  1. Validates the Active Release is eligible for rollback
  2. Identifies the previously Active Release as the rollback target
  3. Reverses configuration changes (shared resources)
  4. Switches the active symlink to the target Release
  5. Transitions the rolled-back Release to RolledBack stage

The rolled-back Release is preserved for inspection.
The restored Release is now Active and serving traffic.

Use --json to get machine-readable output for CI/CD and automation tools.

Examples:
  anvil deployment rollback my-target my-project
  anvil deployment rollback my-target --server-root /tmp/anvil
  anvil deployment rollback my-target --json`,
	Args: ExactArgsWithUsage(2, "anvil deployment rollback my-target my-project"),
	RunE: runDeploymentRollback,
}

func init() {
	deploymentCmd.AddCommand(deploymentRollbackCmd)

	deploymentRollbackCmd.Flags().String("server-root", "",
		"Override config root path (non-production only; overrides ANVIL_SERVER_ROOT env var)")
	AddJSONFlag(deploymentRollbackCmd)
}

// runDeploymentRollback executes the "anvil deployment rollback" command.
//
// It resolves the server root, validates the Runtime is initialized,
// delegates rollback to the ServerReleaseCoordinator, and displays
// the result.
func runDeploymentRollback(cmd *cobra.Command, args []string) error {
	targetID := args[0]
	projectID := args[1]

	// Step 1: Resolve the server root.
	rootPath := resolveServerRoot(cmd)

	if rootPath != server.DefaultConfigRoot {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: using non-default server root %q (non-production override)\n", rootPath)
	}

	// Step 2: Validate the Runtime is initialized.
	if err := RequireServerInitialized(cmd, rootPath); err != nil {
		return err
	}

	// Step 3: Delegate rollback to the ServerReleaseCoordinator.
	coordinator := server.NewServerReleaseCoordinator(rootPath)

	result, err := coordinator.Rollback(projectID)
	if err != nil {
		return ReportPlainErrorf(cmd, err, "rollback failed: %v", err)
	}

	// Step 4: Display the result.
	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		// Convert release identity and stage to strings using fmt.Stringer.
		out := deploymentRollbackJSONOutput{
			TargetID:            targetID,
			ProjectID:           projectID,
			RolledBackReleaseID: fmt.Sprintf("%s", result.RolledBackRelease.ID),
			RolledBackStage:     fmt.Sprintf("%s", result.RolledBackRelease.Stage),
			RestoredReleaseID:   fmt.Sprintf("%s", result.RestoredRelease.ID),
			RestoredStage:       fmt.Sprintf("%s", result.RestoredRelease.Stage),
		}
		return WriteJSON(cmd, out)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Rollback completed.\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  Target ID:                %s\n", targetID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Project ID:               %s\n", projectID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Rolled Back Release ID:   %s\n", result.RolledBackRelease.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Rolled Back Stage:        %s\n", result.RolledBackRelease.Stage)
	fmt.Fprintf(cmd.OutOrStdout(), "  Restored Release ID:      %s\n", result.RestoredRelease.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Restored Stage:           %s\n", result.RestoredRelease.Stage)
	fmt.Fprintln(cmd.OutOrStdout(), "")
	PrintSuccess(cmd, "The previously Active Release is now restored and serving traffic.")

	return nil
}

// deploymentRollbackJSONOutput is the machine-readable output format for
// the --json flag.
type deploymentRollbackJSONOutput struct {
	TargetID            string `json:"target_id"`
	ProjectID           string `json:"project_id"`
	RolledBackReleaseID string `json:"rolled_back_release_id"`
	RolledBackStage     string `json:"rolled_back_stage"`
	RestoredReleaseID   string `json:"restored_release_id"`
	RestoredStage       string `json:"restored_stage"`
}
