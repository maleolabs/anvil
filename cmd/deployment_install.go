// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P10-03, ADR-015, EPIC-010
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/server"
)

// deploymentInstallCmd represents the "anvil deployment install" subcommand
// that installs an artifact and creates a Runtime Release on a deployment
// target.
//
// Per ADR-015, this command delegates installation to the Server Runtime
// command surface. It reads the artifact manifest to extract the project
// identity needed for Runtime delegation — this is allowed because the
// artifact is an input parameter, not Runtime internals.
//
// Reference: ST-P10-03, ADR-015
var deploymentInstallCmd = &cobra.Command{
	Use:   "install <target-id> <artifact-path>",
	Short: "Install an artifact and create a Runtime Release",
	Long: `Install an artifact and create a Runtime Release on a deployment target.

The installation process:
  1. Validates the artifact file exists on disk
  2. Reads the artifact manifest to extract project identity
  3. Delegates installation to the Server Runtime
  4. Reports the created Release details

The command reads the artifact manifest to determine the project ID.
The artifact must be verified before installation.

Use --json to get machine-readable output for CI/CD and automation tools.

Examples:
  anvil deployment install my-target path/to/artifact.tar.gz
  anvil deployment install my-target --server-root /tmp/anvil
  anvil deployment install my-target --json`,
	Args: cobra.ExactArgs(2),
	RunE: runDeploymentInstall,
}

func init() {
	deploymentCmd.AddCommand(deploymentInstallCmd)

	deploymentInstallCmd.Flags().String("server-root", "",
		"Override config root path (non-production only; overrides ANVIL_SERVER_ROOT env var)")
	AddJSONFlag(deploymentInstallCmd)
}

// runDeploymentInstall executes the "anvil deployment install" command.
//
// It validates the artifact, reads the manifest to extract the project ID,
// delegates to the ServerReleaseCoordinator, and displays the result.
func runDeploymentInstall(cmd *cobra.Command, args []string) error {
	targetID := args[0]
	artifactPath := args[1]

	// Step 1: Validate the artifact path is accessible.
	fileInfo, err := os.Stat(artifactPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(cmd.ErrOrStderr(), "Error: artifact not found: %s.\n", artifactPath)
			fmt.Fprintln(cmd.ErrOrStderr(), "Check that the artifact path is correct and try again.")
			return err
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: could not access artifact: %v.\n", err)
		return err
	}

	// Step 2: Validate artifact has content.
	if fileInfo.Size() == 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: artifact is empty: %s.\n", artifactPath)
		fmt.Fprintln(cmd.ErrOrStderr(), "Artifact must contain data.")
		return fmt.Errorf("artifact is empty: %s", artifactPath)
	}

	// Step 3: Resolve the server root.
	rootPath := resolveServerRoot(cmd)

	if rootPath != server.DefaultConfigRoot {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: using non-default server root %q (non-production override)\n", rootPath)
	}

	// Step 4: Validate the Runtime is initialized.
	if err := RequireServerInitialized(cmd, rootPath); err != nil {
		return err
	}

	// Step 5: Read the artifact manifest to extract the project ID.
	manifest, err := artifact.ReadManifest(artifactPath)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: could not read artifact manifest: %v.\n", err)
		fmt.Fprintln(cmd.ErrOrStderr(), "Ensure the artifact is a valid Anvil package with a manifest.json.")
		return err
	}

	projectID := manifest.ProjectID
	if projectID == "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: artifact manifest is missing project_id.\n")
		return fmt.Errorf("artifact manifest missing project_id")
	}

	// Step 6: Delegate installation to the ServerReleaseCoordinator.
	coordinator := server.NewServerReleaseCoordinator(rootPath)

	rel, err := coordinator.Install(projectID, artifactPath)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: installation failed: %v.\n", err)
		return err
	}

	// Step 7: Display the result.
	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		return outputInstallJSON(cmd, rel)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Installation completed.\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  Target ID:    %s\n", targetID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Project ID:   %s\n", projectID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Release ID:   %s\n", rel.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Artifact ID:  %s\n", rel.ArtifactID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Version:      %s\n", rel.Version)
	fmt.Fprintf(cmd.OutOrStdout(), "  Stage:        %s\n", rel.Stage)
	fmt.Fprintf(cmd.OutOrStdout(), "  Created:      %s\n", rel.CreatedAt)
	fmt.Fprintln(cmd.OutOrStdout(), "")
	fmt.Fprintln(cmd.OutOrStdout(), "The Release is now installed and ready for activation.")
	fmt.Fprintln(cmd.OutOrStdout(), "Next step: anvil deployment activate <target-id> <project-id> <release-id>")

	return nil
}
