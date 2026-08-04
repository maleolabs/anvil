package cmd

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/runtime"
)

// runtimeStatusCmd represents the "anvil runtime status" command that
// displays the current operational state of a Runtime instance.
//
// Unlike "anvil status" (which shows project-level information), this
// command reads RuntimeState from a StateStore and displays the runtime's
// operational condition, active release, and resource status.
//
// DEPRECATED: use "anvil server status" instead (ADR-021 migration table).
//
// Reference: ST-P5-03
var runtimeStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Display Runtime operational state",
	Long: `Show the current operational state of the Anvil Runtime.

DEPRECATED: "anvil runtime status" is deprecated. Use "anvil server status"
instead (ADR-021 migration table, docs/migration-guide-v1.5.md). This command
is retained for backward compatibility and will be removed in a future
release — behavior and exit codes are unchanged.

This command reads the Runtime state from the state file and displays:
  - Active Release ID (if any)
  - Runtime condition (normal, degraded, offline)
  - Shared resource status (accessible, inaccessible)
  - Last updated timestamp

This is a read-only command — it does not modify any state.`,
	Example: `  anvil runtime status
  anvil runtime status --state-file /opt/anvil/runtime-state.json`,
	Deprecated:   `use "anvil server status" instead (see docs/migration-guide-v1.5.md)`,
	SilenceUsage: true,
	RunE:         runRuntimeStatus,
}

func init() {
	runtimeCmd.AddCommand(runtimeStatusCmd)

	runtimeStatusCmd.Flags().String(
		"state-file",
		defaultStateFilePath(),
		"path to the Runtime state file",
	)
}

// defaultStateFilePath returns the default path for the Runtime state file.
func defaultStateFilePath() string {
	return filepath.Join(runtime.DefaultInstallRoot, "runtime-state.json")
}

// runRuntimeStatus executes the runtime status command.
//
// It loads the RuntimeState from the state file and displays the current
// operational state. Returns an error if the state file cannot be loaded.
func runRuntimeStatus(cmd *cobra.Command, args []string) error {
	stateFile, _ := cmd.Flags().GetString("state-file")

	store := runtime.NewStateStore(stateFile)
	if err := store.Load(); err != nil {
		return fmt.Errorf("load runtime state: %w", err)
	}

	state := store.State()

	fmt.Fprintf(cmd.OutOrStdout(), "Runtime Status\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  Active Release:  %s\n", displayActiveRelease(state.ActiveReleaseID))
	fmt.Fprintf(cmd.OutOrStdout(), "  Condition:       %s\n", state.RuntimeCondition)
	fmt.Fprintf(cmd.OutOrStdout(), "  Shared Resource: %s\n", state.SharedResourceStatus)
	fmt.Fprintf(cmd.OutOrStdout(), "  Last Updated:    %s\n", state.LastUpdated.Format(time.RFC3339))

	return nil
}

// displayActiveRelease returns the active release ID or "none" if empty.
func displayActiveRelease(releaseID string) string {
	if releaseID == "" {
		return "none"
	}
	return releaseID
}
