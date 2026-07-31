// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P3-08, ADR-010, ADR-012, EPIC-003
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/project"
)

// artifactStatusCmd represents the 'anvil artifact status' subcommand.
var artifactStatusCmd = &cobra.Command{
	Use:   "status <identity>",
	Short: "Display artifact lifecycle status",
	Long: `Display the lifecycle status of a registered artifact.

This command shows:
  - Artifact identity, version, and creation timestamp
  - Current lifecycle stage (from the lifecycle state machine)
  - Registration status (from the artifact registry)
  - Transition history (when a lifecycle state machine file exists)

This is a read-only command — it does not modify any state.

Examples:
  anvil artifact status abc123def456
  anvil artifact status abc123def456 --state-dir /path/to/.anvil/state
  anvil artifact status --help`,
	Args: ExactArgsWithUsage(1, "anvil artifact status abc123def456"),
	RunE: runArtifactStatus,
}

func init() {
	artifactCmd.AddCommand(artifactStatusCmd)

	artifactStatusCmd.Flags().String(
		"state-dir",
		"",
		"path to the project .anvil/state directory (default: auto-discovered)",
	)
}

// resolveStateDir determines the project state directory. If a --state-dir
// flag is provided, it uses that path directly. Otherwise it auto-discovers
// the project root and constructs the .anvil/state path.
func resolveStateDir(cmd *cobra.Command) (string, error) {
	stateDir, _ := cmd.Flags().GetString("state-dir")
	if stateDir != "" {
		return stateDir, nil
	}

	root, err := project.Discover()
	if err != nil {
		return "", err
	}

	structInfo := project.NewStructure(root)
	return structInfo.StateDir, nil
}

// runArtifactStatus executes the artifact status command.
func runArtifactStatus(cmd *cobra.Command, args []string) error {
	identity := args[0]

	stateDir, err := resolveStateDir(cmd)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: no Anvil project found.\n")
		return err
	}

	// Ensure the state directory exists.
	if _, statErr := os.Stat(stateDir); statErr != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: state directory not found: %s.\n", stateDir)
		return statErr
	}

	// Load the registration store.
	regPath := filepath.Join(stateDir, "registration-index.json")
	store := artifact.NewRegistrationStore(regPath)

	// The registration index may not exist yet if no artifacts have been
	// registered. Treat a missing index as an empty store.
	if _, statErr := os.Stat(regPath); statErr == nil {
		if err := store.Load(); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Error: could not load registration index: %v.\n", err)
			return err
		}
	}

	// Look up the artifact in the registration store.
	record, found := store.Lookup(identity)

	// Display artifact information.
	fmt.Fprintf(cmd.OutOrStdout(), "Artifact: %s\n", identity)

	if found {
		fmt.Fprintf(cmd.OutOrStdout(), "  Status:       registered\n")
		fmt.Fprintf(cmd.OutOrStdout(), "  Version:      %s\n", record.Version)
		fmt.Fprintf(cmd.OutOrStdout(), "  Project:      %s\n", record.ProjectID)
		fmt.Fprintf(cmd.OutOrStdout(), "  Registered:   %s\n", record.RegisteredAt)
		if record.ManifestContent != nil && record.ManifestContent.CreatedAt != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "  Created:      %s\n", record.ManifestContent.CreatedAt)
		}
		if record.ArtifactStorePath != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "  Store Path:   %s\n", record.ArtifactStorePath)
		}
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "  Status:       not registered\n")
	}

	// Attempt to load the lifecycle state machine for this artifact.
	lifecyclePath := filepath.Join(stateDir, identity+".json")
	lsm := artifact.NewLifecycleStateMachine(artifact.StageCreated)

	if _, statErr := os.Stat(lifecyclePath); statErr == nil {
		if err := lsm.Load(lifecyclePath); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Error: could not load lifecycle state: %v.\n", err)
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), "  Lifecycle:    %s\n", lsm.Stage().String())

		// Display transition history.
		history := lsm.History()
		if len(history) > 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "  Transitions:")
			for i, t := range history {
				marker := "✓"
				if t.Outcome != "success" {
					marker = "✗"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "    %d. [%s] %s → %s: %s\n",
					i+1, t.Timestamp, t.From.String(), t.To.String(), marker)
				if t.Outcome != "success" {
					fmt.Fprintf(cmd.OutOrStdout(), "       Reason: %s\n", t.Outcome)
				}
			}
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), "  Transitions:  none")
		}
	} else if found {
		// Artifact is registered but no lifecycle state file exists yet.
		// Default stage for a registered artifact is StageRegistered.
		fmt.Fprintf(cmd.OutOrStdout(), "  Lifecycle:    registered (default)\n")
		fmt.Fprintln(cmd.OutOrStdout(), "  Transitions:  none")
	}

	return nil
}
