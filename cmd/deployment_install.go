// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P10-03, ADR-015, EPIC-010
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/output"
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
	Args: ExactArgsWithUsage(2, "anvil deployment install my-target path/to/artifact.tar.gz"),
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
			return ReportError(cmd, &output.AppError{
				Message:    fmt.Sprintf("artifact not found: %s", artifactPath),
				Reason:     "The specified artifact path does not exist",
				Resolution: "Check that the artifact path is correct and try again",
				Err:        err,
			})
		}
		return ReportPlainErrorf(cmd, err, "could not access artifact: %v", err)
	}

	// Step 2: Validate artifact has content.
	if fileInfo.Size() == 0 {
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("artifact is empty: %s", artifactPath),
			Reason:     "The artifact file exists but contains no data",
			Resolution: "Re-package the artifact with 'anvil artifact package'",
			Err:        fmt.Errorf("artifact is empty: %s", artifactPath),
		})
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
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("could not read artifact manifest: %v", err),
			Reason:     "The artifact may not be a valid Anvil package",
			Resolution: "Ensure the artifact was created with 'anvil artifact package' and contains a manifest.json",
			Err:        err,
		})
	}

	projectID := manifest.ProjectID
	if projectID == "" {
		return ReportError(cmd, &output.AppError{
			Message:    "artifact manifest is missing project_id",
			Reason:     "The manifest.json exists but does not contain a project_id field",
			Resolution: "Re-package the artifact with a valid anvil.yaml that has a project name configured",
			Err:        fmt.Errorf("artifact manifest missing project_id"),
		})
	}

	// Step 6: Delegate installation to the ServerReleaseCoordinator.
	coordinator := server.NewServerReleaseCoordinator(rootPath)

	rel, err := coordinator.Install(projectID, artifactPath)
	if err != nil {
		return ReportPlainErrorf(cmd, err, "installation failed: %v", err)
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
	PrintSuccess(cmd, "The Release is now installed and ready for activation.")
	fmt.Fprintln(cmd.OutOrStdout(), "Next step: anvil deployment activate <target-id> <project-id> <release-id>")

	return nil
}
