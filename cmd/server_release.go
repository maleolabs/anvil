// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P4-01, ST-P4-02, ST-P4-11, ST-P4-13, ADR-010, ADR-012, ADR-014, EPIC-004
// Reference: ST-P4-10, ST-P4-11
package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/contracts"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/release"
	"maleolabs.com/anvil/internal/server"
)

// serverReleaseCmd represents the "anvil server release" parent command
// for managing Runtime Releases.
//
// The variable is named serverReleaseCmd (not releaseCmd) to make clear
// this is the server-scoped release group. The ghost top-level "anvil
// release" group (previously cmd/release.go) was removed (BUG-012); this
// is the only release group in the CLI.
//
// Reference: ST-P4-01, EPIC-004
var serverReleaseCmd = &cobra.Command{
	Use:   "release",
	Short: "Manage Runtime Releases",
	Long: `Create, inspect, and manage Runtime Releases on an Anvil Server.

Release commands allow operators to install artifacts, create tracked
Releases, and manage the Release lifecycle on a Runtime.`,
	// Validate that unknown subcommands produce "Did you mean?" suggestions.
	// Run displays help for this parent group.
	Args: NoArgsWithSuggestions(),
	Run: func(cmd *cobra.Command, args []string) {
		cmd.HelpFunc()(cmd, args)
	},
}

// installCmd represents the "anvil server release install" subcommand
// that installs a verified artifact and creates a Runtime Release.
//
// Reference: ST-P4-01, ST-P4-02, ST-P4-11, ST-P4-13
var installCmd = &cobra.Command{
	Use:   "install <project-id> <artifact-path>",
	Short: "Install an artifact and create a Runtime Release",
	Long: `Install a verified artifact and create a Runtime Release
(server-scoped, repository-free).

The installation process:
  1. Validates the artifact file exists on disk
  2. Verifies the artifact's integrity (checksum, manifest, archive)
  3. Reads the artifact manifest to extract metadata
  4. Validates the artifact project ID matches the registered project
  5. Generates a unique Release identity
  6. Copies the artifact to the Runtime Artifact Store
  7. Creates a Runtime Release in the "ready" stage
  8. Persists the Release to the project state directory

The created Release is ready for activation. It is not automatically
activated.

Use --json to get machine-readable output for CI/CD and automation tools.

Examples:
  anvil server release install my-project path/to/artifact.tar.gz
  anvil server release install my-project path/to/artifact.tar.gz --server-root /tmp/anvil
  anvil server release install my-project path/to/artifact.tar.gz --json
  anvil server release install my-project --help`,
	Args: ExactArgsWithUsage(2, "anvil server release install my-project path/to/artifact.tar.gz"),
	RunE: runInstall,
}

func init() {
	serverReleaseCmd.AddCommand(installCmd)
	serverReleaseCmd.AddCommand(historyCmd)
	serverReleaseCmd.AddCommand(activeCmd)
	serverReleaseCmd.AddCommand(serverReleaseStatusCmd)
	serverReleaseCmd.AddCommand(serverReleaseBuildCmd)
	serverCmd.AddCommand(serverReleaseCmd)

	installCmd.Flags().String("server-root", "",
		"Override config root path (non-production only; overrides ANVIL_SERVER_ROOT env var)")
	installCmd.Flags().Bool("json", false,
		"Output release details in JSON format for machine consumption")

	historyCmd.Flags().String("server-root", "",
		"Override config root path (non-production only; overrides ANVIL_SERVER_ROOT env var)")
	historyCmd.Flags().Bool("json", false,
		"Output release history in JSON format for machine consumption")

	activeCmd.Flags().String("server-root", "",
		"Override config root path (non-production only; overrides ANVIL_SERVER_ROOT env var)")
	activeCmd.Flags().Bool("json", false,
		"Output active release in JSON format for machine consumption")

	serverReleaseStatusCmd.Flags().String("server-root", "",
		"Override config root path (non-production only; overrides ANVIL_SERVER_ROOT env var)")
	serverReleaseStatusCmd.Flags().Bool("json", false,
		"Output release status in JSON format for machine consumption")

	serverReleaseBuildCmd.Flags().String("server-root", "",
		"Override config root path (non-production only; overrides ANVIL_SERVER_ROOT env var)")
	serverReleaseBuildCmd.Flags().Bool("json", false,
		"Output build details in JSON format for machine consumption")
	serverReleaseBuildCmd.Flags().String("target", "",
		`Comma-separated build targets to execute (e.g. "web,apk"); empty runs all declared phases`)
	serverReleaseBuildCmd.Flags().Bool("strict", false,
		"Fail instead of skipping build targets unsupported on the current platform")
}

// runInstall executes the release install command.
//
// It resolves the server root, validates the artifact, delegates to the
// ServerReleaseCoordinator, and displays the result.
func runInstall(cmd *cobra.Command, args []string) error {
	projectID := args[0]
	artifactPath := args[1]

	// Check for --json flag first.
	asJSON, _ := cmd.Flags().GetBool("json")

	// Create step reporter for human-readable mode only.
	var reporter output.StepReporter
	if !asJSON {
		reporter = output.NewStepReporter(cmd.OutOrStdout())
		reporter.Start("Release Install")
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
	if _, err := os.Stat(artifactPath); err != nil {
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
	reporterStepComplete(reporter, "Validate artifact")

	// Step 4: Delegate to the ServerReleaseCoordinator.
	reporterStepStart(reporter, "Create Release")
	coordinator := server.NewServerReleaseCoordinator(rootPath, server.WithWarningWriter(cmd.ErrOrStderr()))

	rel, err := coordinator.Install(projectID, artifactPath)
	if err != nil {
		reporterStepFailed(reporter, "Create Release", err)
		return ReportPlainErrorf(cmd, err, "could not create Release: %v", err)
	}
	reporterStepComplete(reporter, "Create Release")

	// Complete the reporter.
	if reporter != nil {
		reporter.Complete("Release Installed", time.Since(overallStart))
	}

	// Step 5: Display the result.
	if asJSON {
		return outputInstallJSON(cmd, rel)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\nRelease created.\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  Release ID: %s\n", rel.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Artifact ID: %s\n", rel.ArtifactID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Version: %s\n", rel.Version)
	fmt.Fprintf(cmd.OutOrStdout(), "  Stage: %s\n", rel.Stage)
	fmt.Fprintf(cmd.OutOrStdout(), "  Created: %s\n", rel.CreatedAt)
	fmt.Fprintln(cmd.OutOrStdout(), "")
	fmt.Fprintln(cmd.OutOrStdout(), "Next step: anvil server release activate <project-id> <release-id>")

	return nil
}

// installJSONOutput is the machine-readable output format for --json flag.
type installJSONOutput struct {
	ReleaseID  string `json:"release_id"`
	ArtifactID string `json:"artifact_id"`
	Version    string `json:"version"`
	Stage      string `json:"stage"`
	CreatedAt  string `json:"created_at"`
}

// outputInstallJSON writes the install result as JSON to stdout.
func outputInstallJSON(cmd *cobra.Command, rel *release.Release) error {
	out := installJSONOutput{
		ReleaseID:  rel.ID.String(),
		ArtifactID: rel.ArtifactID,
		Version:    rel.Version,
		Stage:      rel.Stage.String(),
		CreatedAt:  rel.CreatedAt,
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("encode JSON output: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Release History Inspection — ST-P4-10
// ---------------------------------------------------------------------------

// historyCmd represents the "anvil server release history" subcommand
// that displays the lifecycle transition history for a specific Release.
//
// Reference: ST-P4-10
var historyCmd = &cobra.Command{
	Use:   "history <project-id> <release-id>",
	Short: "Display Release lifecycle history",
	Long: `Display the complete lifecycle transition history for a Runtime Release.

Shows every recorded transition with timestamp, from/to stages, and outcome.
The current stage is indicated in the display.

This is a read-only command — it does not modify any state.

Use --json to get machine-readable output for automation and integration.

Examples:
  anvil server release history my-project abc123def456
  anvil server release history my-project abc123def456 --server-root /tmp/anvil
  anvil server release history my-project abc123def456 --json`,
	Args: ExactArgsWithUsage(2, "anvil server release history my-project abc123def456"),
	// SilenceUsage keeps cobra's usage echo off the machine-readable
	// stdout on the --json failure path (the pattern of every other
	// --json command; TS-019-03-02 regression tests).
	SilenceUsage: true,
	RunE:         runHistory,
}

// runHistory executes the release history command.
//
// It resolves the server root, loads the project registry to determine
// the install root, loads the Release by identity, and displays the
// complete transition history.
func runHistory(cmd *cobra.Command, args []string) error {
	projectID := args[0]
	releaseID := args[1]

	rootPath := resolveServerRoot(cmd)

	if rootPath != server.DefaultConfigRoot {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: using non-default server root %q (non-production override)\n", rootPath)
	}

	// The initialized Server Runtime is a prerequisite (exit 4,
	// TS-019-03-02 F-01).
	if err := RequireServerInitialized(cmd, rootPath); err != nil {
		return err
	}

	// Load the project registry to resolve the install root. A project
	// not found in the registry is a runtime not-found (exit 3, F-02);
	// an unreadable registry is a general error (exit 1).
	registryStore := server.NewRegistryStore(rootPath)
	reg, err := registryStore.Load(projectID)
	if err != nil {
		if errors.Is(err, server.ErrProjectNotFound) {
			return reportProjectNotFoundError(cmd, projectID, err)
		}
		return ReportPlainErrorf(cmd, err, "could not load project registry: %v", err)
	}
	installRoot := reg.Project.InstallRoot

	// Look up the Release by identity and retrieve its transition history.
	// A missing Release is a runtime not-found (exit 3, F-02).
	rel, err := release.LookupByID(installRoot, release.ReleaseID(releaseID))
	if err != nil {
		if errors.Is(err, release.ErrReleaseNotFound) {
			return reportReleaseNotFoundError(cmd, projectID, releaseID, err)
		}
		return ReportPlainErrorf(cmd, err, "Release %q not found: %v", releaseID, err)
	}

	history := rel.History()

	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		return outputHistoryJSON(cmd, rel, history)
	}

	// Human-readable output.
	fmt.Fprintf(cmd.OutOrStdout(), "Release History for %s\n", rel.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Current Stage: %s\n", rel.Stage)
	fmt.Fprintln(cmd.OutOrStdout(), "")

	if len(history) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "  No transitions recorded.")
		return nil
	}

	fmt.Fprintln(cmd.OutOrStdout(), "  Transitions:")
	for i, tr := range history {
		fmt.Fprintf(cmd.OutOrStdout(), "  %d. %s  %s \u2192 %s  %s\n",
			i+1, tr.Timestamp, tr.From, tr.To, tr.Outcome)
	}

	return nil
}

// historyJSONOutput is the machine-readable output format for --json flag.
type historyJSONOutput struct {
	ReleaseID    string                     `json:"release_id"`
	CurrentStage string                     `json:"current_stage"`
	Transitions  []release.TransitionRecord `json:"transitions"`
}

// outputHistoryJSON writes the release history as JSON to stdout.
func outputHistoryJSON(cmd *cobra.Command, rel *release.Release, history []release.TransitionRecord) error {
	if history == nil {
		history = []release.TransitionRecord{}
	}

	out := historyJSONOutput{
		ReleaseID:    rel.ID.String(),
		CurrentStage: rel.Stage.String(),
		Transitions:  history,
	}

	return WriteJSON(cmd, out)
}

// ---------------------------------------------------------------------------
// Active Release Query — ST-P4-11
// ---------------------------------------------------------------------------

// activeCmd represents the "anvil server release active" subcommand
// that displays the currently Active Release for a project.
//
// Reference: ST-P4-11
var activeCmd = &cobra.Command{
	Use:   "active <project-id>",
	Short: "Display the Active Release",
	Long: `Display the currently Active Runtime Release for a registered project.

Shows the Release identity, version, artifact reference, activation
timestamp, and current stage.

This is a read-only command — it does not modify any state.

Use --json to get machine-readable output for automation and integration.

Examples:
  anvil server release active my-project
  anvil server release active my-project --server-root /tmp/anvil
  anvil server release active my-project --json`,
	Args: ExactArgsWithUsage(1, "anvil server release active my-project"),
	RunE: runActive,
}

// runActive executes the active release query command.
//
// It resolves the server root, loads the project registry to determine
// the install root, queries the Active Release, and displays details.
func runActive(cmd *cobra.Command, args []string) error {
	projectID := args[0]

	rootPath := resolveServerRoot(cmd)

	if rootPath != server.DefaultConfigRoot {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: using non-default server root %q (non-production override)\n", rootPath)
	}

	// The initialized Server Runtime is a prerequisite (exit 4,
	// TS-019-03-02 F-01).
	if err := RequireServerInitialized(cmd, rootPath); err != nil {
		return err
	}

	// Load the project registry to resolve the install root. A project
	// not found in the registry is a runtime not-found (exit 3, F-02).
	registryStore := server.NewRegistryStore(rootPath)
	reg, err := registryStore.Load(projectID)
	if err != nil {
		if errors.Is(err, server.ErrProjectNotFound) {
			return reportProjectNotFoundError(cmd, projectID, err)
		}
		return ReportPlainErrorf(cmd, err, "could not load project registry: %v", err)
	}
	installRoot := reg.Project.InstallRoot

	// Query for the Active Release.
	rel, err := release.GetActiveRelease(installRoot)
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not query Active Release: %v", err)
	}

	if rel == nil {
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			out := activeJSONOutput{
				Active: false,
			}
			return WriteJSON(cmd, out)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "No Active Release.\n")
		fmt.Fprintln(cmd.OutOrStdout(), "")
		fmt.Fprintf(cmd.OutOrStdout(), "No Release is currently Active for project %q.\n", projectID)
		fmt.Fprintln(cmd.OutOrStdout(), "Install and activate a Release using:")
		fmt.Fprintf(cmd.OutOrStdout(), "  anvil server release install %s <artifact-path>\n", projectID)
		fmt.Fprintf(cmd.OutOrStdout(), "  anvil server release activate %s <release-id>\n", projectID)
		return nil
	}

	// Find the activation timestamp from the transition history.
	activationTimestamp := ""
	for _, tr := range rel.Transitions {
		if tr.To == release.StageActive && tr.Outcome == "success" {
			activationTimestamp = tr.Timestamp
			break
		}
	}

	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		out := activeJSONOutput{
			Active:              true,
			ReleaseID:           rel.ID.String(),
			Version:             rel.Version,
			ArtifactReference:   rel.ArtifactID,
			ActivationTimestamp: activationTimestamp,
			Stage:               rel.Stage.String(),
		}
		return WriteJSON(cmd, out)
	}

	// Human-readable output.
	fmt.Fprintf(cmd.OutOrStdout(), "Active Release\n")
	PrintKeyValue(cmd, "Release ID", rel.ID)
	PrintKeyValue(cmd, "Version", rel.Version)
	if rel.ArtifactID != "" {
		PrintKeyValue(cmd, "Artifact Reference", rel.ArtifactID)
	}
	if activationTimestamp != "" {
		PrintKeyValue(cmd, "Activation", activationTimestamp)
	}
	PrintKeyValue(cmd, "Stage", rel.Stage)

	return nil
}

// activeJSONOutput is the machine-readable output format for --json flag.
type activeJSONOutput struct {
	Active              bool   `json:"active"`
	ReleaseID           string `json:"release_id,omitempty"`
	Version             string `json:"version,omitempty"`
	ArtifactReference   string `json:"artifact_reference,omitempty"`
	ActivationTimestamp string `json:"activation_timestamp,omitempty"`
	Stage               string `json:"stage,omitempty"`
}

// ---------------------------------------------------------------------------
// Release Status Query — TS-012-002
// ---------------------------------------------------------------------------

// serverReleaseStatusCmd represents the "anvil server release status"
// subcommand that displays the lifecycle stage of every Release for a
// project, or of a single Release when a release ID is given.
//
// Reference: TS-012-002, MVP-001 AC 9.7
var serverReleaseStatusCmd = &cobra.Command{
	Use:   "status <project-id> [release-id]",
	Short: "Display Release lifecycle stages",
	Long: `Display the lifecycle stage of Releases for a registered project.

Without a release ID, lists every Release with its lifecycle stage
(ready/activating/active/rolled back/archived/failed). With a release ID,
shows the stage of that specific Release.

This is a read-only command — it does not modify any state.

Use --json to get machine-readable output for automation and integration.

Examples:
  anvil server release status my-project
  anvil server release status my-project abc123def456
  anvil server release status my-project --server-root /tmp/anvil
  anvil server release status my-project --json`,
	Args: RangeArgsWithUsage(1, 2, "anvil server release status my-project [release-id]"),
	RunE: runServerReleaseStatus,
}

// runServerReleaseStatus executes the release status command.
//
// It resolves the server root, loads the project registry to determine
// the install root, and either lists every Release with its lifecycle
// stage or shows the stage of a single Release by identity.
func runServerReleaseStatus(cmd *cobra.Command, args []string) error {
	projectID := args[0]

	rootPath := resolveServerRoot(cmd)

	if rootPath != server.DefaultConfigRoot {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: using non-default server root %q (non-production override)\n", rootPath)
	}

	// The initialized Server Runtime is a prerequisite (exit 4,
	// TS-019-03-02 F-01).
	if err := RequireServerInitialized(cmd, rootPath); err != nil {
		return err
	}

	// Load the project registry to resolve the install root. A project
	// not found in the registry is a runtime not-found (exit 3, F-02).
	registryStore := server.NewRegistryStore(rootPath)
	reg, err := registryStore.Load(projectID)
	if err != nil {
		if errors.Is(err, server.ErrProjectNotFound) {
			return reportProjectNotFoundError(cmd, projectID, err)
		}
		return ReportPlainErrorf(cmd, err, "could not load project registry: %v", err)
	}
	installRoot := reg.Project.InstallRoot

	asJSON, _ := cmd.Flags().GetBool("json")

	// Detail view: the stage of a single Release. A missing Release is a
	// runtime not-found (exit 3, F-02).
	if len(args) == 2 {
		releaseID := args[1]
		rel, err := release.LookupByID(installRoot, release.ReleaseID(releaseID))
		if err != nil {
			if errors.Is(err, release.ErrReleaseNotFound) {
				return reportReleaseNotFoundError(cmd, projectID, releaseID, err)
			}
			return ReportPlainErrorf(cmd, err, "Release %q not found: %v", releaseID, err)
		}

		if asJSON {
			return outputStatusDetailJSON(cmd, rel)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Release Status for %s\n", rel.ID)
		PrintKeyValue(cmd, "Version", rel.Version)
		if rel.ArtifactID != "" {
			PrintKeyValue(cmd, "Artifact Reference", rel.ArtifactID)
		}
		PrintKeyValue(cmd, "Stage", rel.Stage)
		PrintKeyValue(cmd, "Created", rel.CreatedAt)

		return nil
	}

	// List view: every Release with its lifecycle stage.
	releases, err := release.ListReleases(installRoot)
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not list Releases: %v", err)
	}

	if asJSON {
		return outputStatusListJSON(cmd, projectID, releases)
	}

	if len(releases) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "No Releases found for project %q.\n", projectID)
		fmt.Fprintln(cmd.OutOrStdout(), "")
		fmt.Fprintf(cmd.OutOrStdout(), "Install a Release using:\n")
		fmt.Fprintf(cmd.OutOrStdout(), "  anvil server release install %s <artifact-path>\n", projectID)
		return nil
	}

	t := output.NewTable("Release ID", "Version", "Stage")
	for _, rel := range releases {
		t.AddRow(rel.ID.String(), rel.Version, rel.Stage.String())
	}
	t.Format(cmd.OutOrStdout())

	return nil
}

// statusReleaseJSONOutput is the machine-readable per-Release shape used
// by the status command. Its field names match the existing server
// release commands (TS-P8-05 envelope, snake_case keys).
type statusReleaseJSONOutput struct {
	ReleaseID  string `json:"release_id"`
	ArtifactID string `json:"artifact_id,omitempty"`
	Version    string `json:"version"`
	Stage      string `json:"stage"`
	CreatedAt  string `json:"created_at"`
}

// statusListJSONOutput is the machine-readable output format for
// "anvil server release status <project-id>" with --json.
type statusListJSONOutput struct {
	ProjectID string                    `json:"project_id"`
	Releases  []statusReleaseJSONOutput `json:"releases"`
}

// outputStatusListJSON writes the release status listing as JSON to stdout.
func outputStatusListJSON(cmd *cobra.Command, projectID string, releases []*release.Release) error {
	out := statusListJSONOutput{
		ProjectID: projectID,
		Releases:  []statusReleaseJSONOutput{},
	}
	for _, rel := range releases {
		out.Releases = append(out.Releases, toStatusReleaseJSON(rel))
	}

	return WriteJSON(cmd, out)
}

// outputStatusDetailJSON writes the single-Release status as JSON to
// stdout, mirroring the per-Release shape of the install command.
func outputStatusDetailJSON(cmd *cobra.Command, rel *release.Release) error {
	return WriteJSON(cmd, toStatusReleaseJSON(rel))
}

// toStatusReleaseJSON converts a Release into the machine-readable
// per-Release status shape.
func toStatusReleaseJSON(rel *release.Release) statusReleaseJSONOutput {
	return statusReleaseJSONOutput{
		ReleaseID:  rel.ID.String(),
		ArtifactID: rel.ArtifactID,
		Version:    rel.Version,
		Stage:      rel.Stage.String(),
		CreatedAt:  rel.CreatedAt,
	}
}

// ---------------------------------------------------------------------------
// Server Release Build — TS-007-040
// ---------------------------------------------------------------------------

// serverReleaseBuildCmd represents the "anvil server release build"
// subcommand that runs the project's framework adapter build pipeline on
// the server. The adapter-owned build phases are the single source of
// build knowledge at server release time (ADR-020 §4).
//
// Reference: TS-007-040, ADR-020 §4, ADR-017
var serverReleaseBuildCmd = &cobra.Command{
	Use:   "build <project-id>",
	Short: "Build a release via the project's delivery lifecycle standard",
	Long: `Run the project's delivery lifecycle standard build pipeline on the
server.

The build executes the standard's build phases (e.g. for Laravel:
composer -> npm -> artisan caches) in the project install root, bounded
by a 15-minute timeout. The standard's build phases are the single
source of build knowledge at server release time.

The build produces the project tree the release artifact is packaged
from; package and install the result with:
  anvil artifact package
  anvil server release install <project-id> <artifact-path>

The project must declare a delivery lifecycle standard (project.standard
in the project registry — the legacy project.adapter key is still
honored for backward compatibility, see docs/migration-guide-v2.md); a
missing standard fails with a descriptive error — there is no silent
fallback to a generic build.

Use --json to get machine-readable output for CI/CD and automation tools.

Examples:
  anvil server release build my-project
  anvil server release build my-project --target web,apk --strict
  anvil server release build my-project --server-root /tmp/anvil
  anvil server release build my-project --json`,
	Args: ExactArgsWithUsage(1, "anvil server release build my-project"),
	RunE: runServerReleaseBuild,
}

// runServerReleaseBuild executes the server release build command.
//
// It resolves the server root, validates the Runtime is initialized,
// delegates to the ServerReleaseCoordinator, and displays the build
// outcome phase by phase.
func runServerReleaseBuild(cmd *cobra.Command, args []string) error {
	projectID := args[0]

	// Check for --json flag first.
	asJSON, _ := cmd.Flags().GetBool("json")

	// Create step reporter for human-readable mode only.
	var reporter output.StepReporter
	if !asJSON {
		reporter = output.NewStepReporter(cmd.OutOrStdout())
		reporter.Start("Server Release Build")
	}
	overallStart := time.Now()

	// Step 1: Resolve the server root.
	reporterStepStart(reporter, "Resolve server root")
	rootPath := resolveServerRoot(cmd)
	reporterStepComplete(reporter, "Resolve server root")

	if rootPath != server.DefaultConfigRoot {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: using non-default server root %q (non-production override)\n", rootPath)
	}

	// Step 2: Validate the Runtime is initialized.
	reporterStepStart(reporter, "Check Runtime")
	if err := RequireServerInitialized(cmd, rootPath); err != nil {
		reporterStepFailed(reporter, "Check Runtime", err)
		return err
	}
	reporterStepComplete(reporter, "Check Runtime")

	// Step 3: Delegate to the ServerReleaseCoordinator.
	targetFlag, _ := cmd.Flags().GetString("target")
	strict, _ := cmd.Flags().GetBool("strict")

	reporterStepStart(reporter, "Build Release")
	coordinator := server.NewServerReleaseCoordinator(rootPath, server.WithWarningWriter(cmd.ErrOrStderr()))
	result, err := coordinator.BuildRelease(context.Background(), projectID, server.BuildReleaseOptions{
		Targets: parseTargetList(targetFlag),
		Strict:  strict,
	})
	if err != nil {
		reporterStepFailed(reporter, "Build Release", err)
		return ReportPlainErrorf(cmd, err, "build failed: %v", err)
	}
	reporterStepComplete(reporter, "Build Release")

	// Complete the reporter.
	if reporter != nil {
		reporter.Complete("Build Complete", time.Since(overallStart))
	}

	// Step 4: Display the result.
	if asJSON {
		return outputServerReleaseBuildJSON(cmd, projectID, result)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\nBuild complete.\n")
	for _, phase := range result.Phases {
		switch {
		case phase.Skipped:
			fmt.Fprintf(cmd.OutOrStdout(), "  %s — skipped (%s)\n", phase.Phase, phase.Warning)
		case phase.Success:
			fmt.Fprintf(cmd.OutOrStdout(), "  %s — ok\n", phase.Phase)
		default:
			fmt.Fprintf(cmd.OutOrStdout(), "  %s — failed: %s\n", phase.Phase, phase.Error)
		}
	}
	fmt.Fprintln(cmd.OutOrStdout(), "")
	fmt.Fprintln(cmd.OutOrStdout(), "Next step: anvil artifact package, then anvil server release install <project-id> <artifact-path>")

	return nil
}

// serverReleaseBuildJSONOutput is the machine-readable output format for
// the --json flag on server release build.
type serverReleaseBuildJSONOutput struct {
	ProjectID string                       `json:"project_id"`
	Success   bool                         `json:"success"`
	Phases    []contracts.BuildPhaseResult `json:"phases"`
}

// outputServerReleaseBuildJSON writes the server release build result as
// JSON to stdout.
func outputServerReleaseBuildJSON(cmd *cobra.Command, projectID string, result *server.BuildReleaseResult) error {
	out := serverReleaseBuildJSONOutput{
		ProjectID: projectID,
		Success:   result.Success,
		Phases:    result.Phases,
	}

	return WriteJSON(cmd, out)
}
