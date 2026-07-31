package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/runtime"
)

// listCmd represents the "anvil runtime list" command that displays all
// Runtimes with their identity, name, environment type, lifecycle stage,
// and active release.
//
// Reference: ST-P5-06
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all Runtimes and their status",
	Long: `Display all provisioned Runtimes and their current status.

This command reads the Runtime registry and for each Runtime loads its
lifecycle stage and active release state.

Output columns:
  ID          Runtime identifier (truncated)
  Name        Runtime name
  Environment Deployment environment
  Stage       Lifecycle stage (provisioned, ready, active, retired)
  Release     Active release ID (if any)

This is a read-only command — it does not modify any state.`,
	Example: `  anvil runtime list
  anvil runtime list --runtimes-path /opt/anvil/runtimes.json`,
	SilenceUsage: true,
	RunE:         runList,
}

func init() {
	runtimeCmd.AddCommand(listCmd)

	listCmd.Flags().String(
		"runtimes-path",
		runtime.DefaultRegistryPath(),
		"path to the Runtime registry file",
	)
}

// runList executes the list command.
//
// It loads the Runtime registry and displays each runtime's current
// identity, lifecycle stage, and active release in a table format.
func runList(cmd *cobra.Command, args []string) error {
	runtimesPath, _ := cmd.Flags().GetString("runtimes-path")

	registry := runtime.NewRuntimeRegistry(runtimesPath)
	if err := registry.Load(); err != nil {
		return fmt.Errorf("load runtime registry: %w", err)
	}

	entries := registry.ListAll()
	if len(entries) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No Runtimes provisioned.")
		return nil
	}

	// Build and render table using the output.Table formatter.
	t := output.NewTable("ID", "Name", "Environment", "Stage", "Release")
	for _, entry := range entries {
		idDisplay := truncateID(string(entry.ID))
		stage := readLifecycleStage(entry.InstallPath)
		release := readActiveRelease(entry.InstallPath)

		t.AddRow(idDisplay, entry.Name, string(entry.Environment), stage, release)
	}
	t.Format(cmd.OutOrStdout())

	return nil
}

// truncateID returns the first 8 characters of a UUID string.
func truncateID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}

// readLifecycleStage reads the lifecycle stage from the given install path.
// Returns "unknown" if the lifecycle file cannot be read.
func readLifecycleStage(installPath string) string {
	lifecyclePath := filepath.Join(installPath, "lifecycle.json")
	lifecycle := runtime.NewLifecycle()
	if err := lifecycle.Load(lifecyclePath); err != nil {
		return "unknown"
	}
	return lifecycle.Stage().String()
}

// readActiveRelease reads the active release ID from the state file at the
// given install path. Returns "none" if the state file cannot be read or
// no active release is set.
func readActiveRelease(installPath string) string {
	statePath := filepath.Join(installPath, "state.json")
	store := runtime.NewStateStore(statePath)
	if err := store.Load(); err != nil {
		return "none"
	}
	state := store.State()
	if state.ActiveReleaseID == "" {
		return "none"
	}
	return state.ActiveReleaseID
}
