// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P4-01, ST-P4-02, ST-P4-11, ST-P4-13, ADR-010, ADR-012, ADR-014, EPIC-004
// Reference: ST-P4-10, ST-P4-11
package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/release"
	"maleolabs.com/anvil/internal/server"
)

// serverReleaseCmd represents the "anvil server release" parent command
// for managing Runtime Releases.
//
// The variable is named serverReleaseCmd (not releaseCmd) to avoid
// conflicting with the top-level "anvil release" command group defined
// in cmd/release.go.
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
  anvil server release install my-project --server-root /tmp/anvil
  anvil server release install my-project --json
  anvil server release install my-project --help`,
	Args: ExactArgsWithUsage(2, "anvil server release install my-project path/to/artifact.tar.gz"),
	RunE: runInstall,
}

func init() {
	serverReleaseCmd.AddCommand(installCmd)
	serverReleaseCmd.AddCommand(historyCmd)
	serverReleaseCmd.AddCommand(activeCmd)
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
}

// runInstall executes the release install command.
//
// It resolves the server root, validates the artifact, delegates to the
// ServerReleaseCoordinator, and displays the result.
func runInstall(cmd *cobra.Command, args []string) error {
	projectID := args[0]
	artifactPath := args[1]

	// Step 1: Validate the artifact path is accessible.
	if _, err := os.Stat(artifactPath); err != nil {
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

	// Step 2: Resolve the server root.
	rootPath := resolveServerRoot(cmd)

	if rootPath != server.DefaultConfigRoot {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: using non-default server root %q (non-production override)\n", rootPath)
	}

	// Step 3: Delegate to the ServerReleaseCoordinator.
	coordinator := server.NewServerReleaseCoordinator(rootPath)

	rel, err := coordinator.Install(projectID, artifactPath)
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not create Release: %v", err)
	}

	// Step 4: Display the result.
	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		return outputInstallJSON(cmd, rel)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Release created.\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  Release ID: %s\n", rel.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Artifact ID: %s\n", rel.ArtifactID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Version: %s\n", rel.Version)
	fmt.Fprintf(cmd.OutOrStdout(), "  Stage: %s\n", rel.Stage)
	fmt.Fprintf(cmd.OutOrStdout(), "  Created: %s\n", rel.CreatedAt)
	fmt.Fprintln(cmd.OutOrStdout(), "")
	fmt.Fprintln(cmd.OutOrStdout(), "The Release is ready for activation.")
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
  anvil server release history my-project --server-root /tmp/anvil
  anvil server release history my-project --json`,
	Args: ExactArgsWithUsage(2, "anvil server release history my-project abc123def456"),
	RunE: runHistory,
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

	// Load the project registry to resolve the install root.
	registryStore := server.NewRegistryStore(rootPath)
	reg, err := registryStore.Load(projectID)
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not load project registry: %v", err)
	}
	installRoot := reg.Project.InstallRoot

	// Look up the Release by identity and retrieve its transition history.
	rel, err := release.LookupByID(installRoot, release.ReleaseID(releaseID))
	if err != nil {
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

	// Load the project registry to resolve the install root.
	registryStore := server.NewRegistryStore(rootPath)
	reg, err := registryStore.Load(projectID)
	if err != nil {
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
