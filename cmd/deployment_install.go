// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P10-03, ADR-015, EPIC-010, TD-006
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/release"
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
// This command is a local target-centric alias of
// 'anvil server release install': it runs on the local server runtime
// through the ServerRelease coordinator and requires a locally
// initialized server. It is NOT an SSH transport command — only
// 'anvil deployment upload' transports artifacts (TD-006).
//
// Reference: ST-P10-03, ADR-015, TD-006
var deploymentInstallCmd = &cobra.Command{
	Use:   "install <artifact-path>",
	Short: "Install an artifact and create a Runtime Release",
	Long: `Install an artifact and create a Runtime Release on a deployment target.

This is a local target-centric alias of 'anvil server release install':
it runs on the local server runtime through the ServerRelease
coordinator and requires a locally initialized server. It is NOT an
SSH transport command — only 'anvil deployment upload' transports
artifacts to remote targets (TD-006).

The installation process:
  1. Validates the artifact file exists on disk
  2. Reads the artifact manifest to extract project identity
  3. Delegates installation to the Server Runtime
  4. Reports the created Release details

The command reads the artifact manifest to determine the project ID.
The artifact must be verified before installation.

Use --json to get machine-readable output for CI/CD and automation tools.

Examples:
  anvil deployment install path/to/artifact.tar.gz
  anvil deployment install path/to/artifact.tar.gz --server-root /tmp/anvil
  anvil deployment install path/to/artifact.tar.gz --json`,
	Args: ExactArgsWithUsage(1, "anvil deployment install path/to/artifact.tar.gz"),
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
	artifactPath := args[0]

	// Check for --json flag first.
	asJSON, _ := cmd.Flags().GetBool("json")

	// Create step reporter for human-readable mode only.
	var reporter output.StepReporter
	if !asJSON {
		reporter = output.NewStepReporter(cmd.OutOrStdout())
		reporter.Start("Deployment Install")
	}
	overallStart := time.Now()

	// Step 1: Resolve the server root.
	reporterStepStart(reporter, "Resolve server root")
	rootPath := resolveServerRoot(cmd)
	reporterStepComplete(reporter, "Resolve server root")

	if rootPath != server.DefaultConfigRoot {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: using non-default server root %q (non-production override)\n", rootPath)
	}

	// Step 2: Validate the Runtime is initialized — the first check, so
	// the precondition category (4) is never masked by a later input
	// validation failure (TS-019-03-02 §9.3).
	reporterStepStart(reporter, "Check Runtime")
	if err := RequireServerInitialized(cmd, rootPath); err != nil {
		reporterStepFailed(reporter, "Check Runtime", err)
		return err
	}
	reporterStepComplete(reporter, "Check Runtime")

	// Step 3: Validate the artifact path is accessible.
	reporterStepStart(reporter, "Validate artifact")
	fileInfo, err := os.Stat(artifactPath)
	if err != nil {
		reporterStepFailed(reporter, "Validate artifact", err)
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

	// Step 4: Validate artifact has content.
	if fileInfo.Size() == 0 {
		reporterStepFailed(reporter, "Validate artifact", fmt.Errorf("empty artifact"))
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("artifact is empty: %s", artifactPath),
			Reason:     "The artifact file exists but contains no data",
			Resolution: "Re-package the artifact with 'anvil artifact package'",
			Err:        fmt.Errorf("artifact is empty: %s", artifactPath),
		})
	}
	reporterStepComplete(reporter, "Validate artifact")

	// Step 5: Read the artifact manifest to extract the project ID.
	reporterStepStart(reporter, "Read manifest")
	manifest, err := artifact.ReadManifest(artifactPath)
	if err != nil {
		reporterStepFailed(reporter, "Read manifest", err)
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("could not read artifact manifest: %v", err),
			Reason:     "The artifact may not be a valid Anvil package",
			Resolution: "Ensure the artifact was created with 'anvil artifact package' and contains a manifest.json",
			Err:        err,
		})
	}

	projectID := manifest.ProjectID
	if projectID == "" {
		reporterStepFailed(reporter, "Read manifest", fmt.Errorf("missing project_id"))
		return ReportError(cmd, &output.AppError{
			Message:    "artifact manifest is missing project_id",
			Reason:     "The manifest.json exists but does not contain a project_id field",
			Resolution: "Re-package the artifact with a valid anvil.yaml that has a project name configured",
			Err:        fmt.Errorf("artifact manifest missing project_id"),
		})
	}
	reporterStepComplete(reporter, "Read manifest")

	// Step 6: Delegate installation to the ServerReleaseCoordinator.
	reporterStepStart(reporter, "Install artifact")
	coordinator := server.NewServerReleaseCoordinator(rootPath, server.WithWarningWriter(cmd.ErrOrStderr()))

	rel, err := coordinator.Install(projectID, artifactPath)
	if err != nil {
		reporterStepFailed(reporter, "Install artifact", err)
		return ReportPlainErrorf(cmd, err, "installation failed: %v", err)
	}
	reporterStepComplete(reporter, "Install artifact")

	// Complete the reporter.
	if reporter != nil {
		reporter.Complete("Installation Complete", time.Since(overallStart))
	}

	// Step 7: Display the result.
	if asJSON {
		return outputDeploymentInstallJSON(cmd, projectID, rel)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\nInstallation completed.\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  Project ID:   %s\n", projectID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Release ID:   %s\n", rel.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Artifact ID:  %s\n", rel.ArtifactID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Version:      %s\n", rel.Version)
	fmt.Fprintf(cmd.OutOrStdout(), "  Stage:        %s\n", rel.Stage)
	fmt.Fprintf(cmd.OutOrStdout(), "  Created:      %s\n", rel.CreatedAt)
	fmt.Fprintln(cmd.OutOrStdout(), "")
	fmt.Fprintln(cmd.OutOrStdout(), "Next step: anvil deployment activate <project-id> <release-id>")

	return nil
}

// reporterStepStart is a helper that calls reporter.StepStart if reporter is non-nil.
func reporterStepStart(reporter output.StepReporter, name string) {
	if reporter != nil {
		reporter.StepStart(name)
	}
}

// reporterStepComplete is a helper that calls reporter.StepComplete if reporter is non-nil.
func reporterStepComplete(reporter output.StepReporter, name string) {
	if reporter != nil {
		reporter.StepComplete(name, 0) // Duration tracked externally
	}
}

// reporterStepFailed is a helper that calls reporter.StepFailed if reporter is non-nil.
func reporterStepFailed(reporter output.StepReporter, name string, err error) {
	if reporter != nil {
		reporter.StepFailed(name, 0, err) // Duration tracked externally
	}
}

// deploymentInstallJSONOutput is the machine-readable output format for
// the --json flag on deployment install.
//
// Includes project_id for consistency with other deployment commands
// (e.g., deployment activate).
type deploymentInstallJSONOutput struct {
	ProjectID string `json:"project_id"`
	ReleaseID string `json:"release_id"`
	Stage     string `json:"stage"`
}

// outputDeploymentInstallJSON writes the deployment install result as JSON
// to stdout.
func outputDeploymentInstallJSON(cmd *cobra.Command, projectID string, rel *release.Release) error {
	out := deploymentInstallJSONOutput{
		ProjectID: projectID,
		ReleaseID: rel.ID.String(),
		Stage:     rel.Stage.String(),
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("encode JSON output: %w", err)
	}

	return nil
}
