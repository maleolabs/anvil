// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P10-04, ADR-015, EPIC-010
package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/project"
	"maleolabs.com/anvil/internal/release"
	"maleolabs.com/anvil/internal/server"
)

// deploymentActivateCmd represents the "anvil deployment activate" subcommand
// that activates a Runtime Release on a deployment target.
//
// Per ADR-015, this command delegates activation to the Server Runtime
// command surface. It reports the activation outcome without reading
// Runtime internals.
//
// Reference: ST-P10-04, ADR-015
var deploymentActivateCmd = &cobra.Command{
	Use:   "activate <target-id> <project-id> <release-id>",
	Short: "Activate a Runtime Release on a deployment target",
	Long: `Activate a Runtime Release on a deployment target.

The activation process transitions a Release from Ready to Active:
  1. Validates the Release is in Ready stage
  2. Transitions the Release to Activating stage
  3. Configures shared resources
  4. Promotes the Release to Active atomically
  5. Updates the runtime state with the active release

The Release must be in Ready stage to be activated.
Once activated, the Release is deployed and serving traffic.

Use --json to get machine-readable output for CI/CD and automation tools.

Examples:
  anvil deployment activate my-target my-project abc123def456
  anvil deployment activate my-target --server-root /tmp/anvil
  anvil deployment activate my-target --json`,
	Args: ExactArgsWithUsage(3, "anvil deployment activate my-target my-project abc123def456"),
	RunE: runDeploymentActivate,
}

func init() {
	deploymentCmd.AddCommand(deploymentActivateCmd)

	deploymentActivateCmd.Flags().String("server-root", "",
		"Override config root path (non-production only; overrides ANVIL_SERVER_ROOT env var)")
	AddJSONFlag(deploymentActivateCmd)
}

// runDeploymentActivate executes the "anvil deployment activate" command.
//
// It resolves the server root, validates the Runtime is initialized,
// delegates activation to the ServerReleaseCoordinator, and displays
// the result.
func runDeploymentActivate(cmd *cobra.Command, args []string) error {
	targetID := args[0]
	projectID := args[1]
	releaseID := args[2]

	// Step 1: Resolve the server root.
	rootPath := resolveServerRoot(cmd)

	if rootPath != server.DefaultConfigRoot {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: using non-default server root %q (non-production override)\n", rootPath)
	}

	// Step 2: Validate the Runtime is initialized.
	if err := RequireServerInitialized(cmd, rootPath); err != nil {
		return err
	}

	// Step 3: Delegate activation to the ServerReleaseCoordinator.
	coordinator := server.NewServerReleaseCoordinator(rootPath)

	if err := coordinator.Activate(projectID, releaseID); err != nil {
		return ReportPlainErrorf(cmd, err, "activation failed: %v", err)
	}

	// Step 4: Load the activated Release for display.
	registryStore := server.NewRegistryStore(rootPath)
	reg, err := registryStore.Load(projectID)
	if err != nil {
		// Non-fatal for display — activation already succeeded.
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			return outputDeploymentActivateJSON(cmd, targetID, projectID, releaseID, "active")
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Activation completed.\n")
		fmt.Fprintf(cmd.OutOrStdout(), "  Target ID:    %s\n", targetID)
		fmt.Fprintf(cmd.OutOrStdout(), "  Project ID:   %s\n", projectID)
		fmt.Fprintf(cmd.OutOrStdout(), "  Release ID:   %s\n", releaseID)
		fmt.Fprintln(cmd.OutOrStdout(), "")
		fmt.Fprintln(cmd.OutOrStdout(), "The Release is now active and serving traffic.")
		return nil
	}

	installRoot := reg.Project.InstallRoot
	s := project.NewStructure(installRoot)
	releasePath := filepath.Join(s.StateDir, "releases", releaseID+".json")

	rel, err := release.Load(releasePath)
	if err != nil {
		// Non-fatal for display — activation already succeeded.
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			return outputDeploymentActivateJSON(cmd, targetID, projectID, releaseID, "active")
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Activation completed.\n")
		fmt.Fprintf(cmd.OutOrStdout(), "  Target ID:    %s\n", targetID)
		fmt.Fprintf(cmd.OutOrStdout(), "  Project ID:   %s\n", projectID)
		fmt.Fprintf(cmd.OutOrStdout(), "  Release ID:   %s\n", releaseID)
		fmt.Fprintln(cmd.OutOrStdout(), "")
		fmt.Fprintln(cmd.OutOrStdout(), "The Release is now active and serving traffic.")
		return nil
	}

	// Step 5: Display the result.
	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		return outputDeploymentActivateJSON(cmd, targetID, projectID, rel.ID.String(), rel.Stage.String())
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Activation completed.\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  Target ID:    %s\n", targetID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Project ID:   %s\n", projectID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Release ID:   %s\n", rel.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Stage:        %s\n", rel.Stage)
	fmt.Fprintln(cmd.OutOrStdout(), "")
	fmt.Fprintln(cmd.OutOrStdout(), "The Release is now active and serving traffic.")

	return nil
}

// deploymentActivateJSONOutput is the machine-readable output format for
// the --json flag.
type deploymentActivateJSONOutput struct {
	TargetID  string `json:"target_id"`
	ProjectID string `json:"project_id"`
	ReleaseID string `json:"release_id"`
	Stage     string `json:"stage"`
}

// outputDeploymentActivateJSON writes the activation result as JSON to stdout.
func outputDeploymentActivateJSON(cmd *cobra.Command, targetID, projectID, releaseID, stage string) error {
	out := deploymentActivateJSONOutput{
		TargetID:  targetID,
		ProjectID: projectID,
		ReleaseID: releaseID,
		Stage:     stage,
	}
	return WriteJSON(cmd, out)
}
