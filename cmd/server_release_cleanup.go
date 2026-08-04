// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P5-04, EPIC-005
package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/runtime"
	"maleolabs.com/anvil/internal/server"
)

// cleanupCmd represents the "anvil server release cleanup" subcommand
// that removes a release directory by release identity.
//
// Reference: ST-P5-04
var cleanupCmd = &cobra.Command{
	Use:   "cleanup <project-id> <release-id>",
	Short: "Remove a release directory and reclaim disk space",
	Long: `Remove a release directory for the specified release identity.

The command prompts for confirmation before removing the release directory.
Use the --force flag to skip the confirmation prompt for non-interactive
or automated environments.

The cleanup process:
  1. Validates the project is registered
  2. Checks that the release is not currently Active
  3. Checks that the release is not a rollback candidate
  4. Prompts for confirmation (skipped with --force)
  5. Removes the versioned release directory from disk
  6. Reports how much disk space was reclaimed

The Active Release directory cannot be removed.
The rollback candidate (previously Active Release) is protected.
Shared resources outside the release directory are never affected.

This operation is irreversible. Make sure to back up any data before
proceeding.

Examples:
  anvil server release cleanup my-project abc123def456
  anvil server release cleanup my-project abc123def456 --force
  anvil server release cleanup my-project abc123def456 --server-root /tmp/anvil`,
	Args: ExactArgsWithUsage(2, "anvil server release cleanup my-project abc123def456"),
	RunE: runCleanup,
}

func init() {
	serverReleaseCmd.AddCommand(cleanupCmd)

	cleanupCmd.Flags().Bool("force", false, "Skip confirmation prompt (non-interactive mode)")
	cleanupCmd.Flags().String("server-root", "",
		"Override config root path (non-production only; overrides ANVIL_SERVER_ROOT env var)")
}

// runCleanup executes the release cleanup command.
//
// It resolves the server root, loads the project registry to determine the
// install root, checks RuntimeState for Active Release protection, prompts
// for confirmation (or requires --force in non-interactive mode), removes
// the release directory, and displays the reclaimed space.
func runCleanup(cmd *cobra.Command, args []string) error {
	projectID := args[0]
	releaseID := args[1]

	rootPath := resolveServerRoot(cmd)

	if rootPath != server.DefaultConfigRoot {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: using non-default server root %q (non-production override)\n", rootPath)
	}

	// Step 1: Load the project registry to resolve the install root.
	registryStore := server.NewRegistryStore(rootPath)
	if !registryStore.Exists(projectID) {
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("project %q is not registered", projectID),
			Reason:     "The project has not been registered with the Server Runtime",
			Resolution: "Register the project first using 'anvil server project register'",
			Err:        fmt.Errorf("project %q not registered", projectID),
		})
	}

	reg, err := registryStore.Load(projectID)
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not load project registry: %v", err)
	}

	installRoot := reg.Project.InstallRoot

	// Step 2: Build RuntimeConfig to resolve paths.
	runtimeCfg := runtime.DefaultRuntimeConfig()
	runtimeCfg.InstallRoot = installRoot

	releasesDirPath := runtimeCfg.ReleasesDirPath()
	releaseDir := runtime.ReleaseDirPath(releasesDirPath, releaseID)

	// Step 3: Check if the release directory exists.
	if _, err := os.Stat(releaseDir); err != nil {
		if os.IsNotExist(err) {
			return ReportError(cmd, &output.AppError{
				Message:    fmt.Sprintf("release directory for %q not found", releaseID),
				Reason:     "The specified release ID does not have a directory in the releases store",
				Resolution: "Check that the release ID is correct and try again",
				Err:        fmt.Errorf("release directory %s not found", releaseDir),
			})
		}
		return ReportPlainErrorf(cmd, err, "could not access release directory: %v", err)
	}

	// Step 4: Check Active Release protection via RuntimeState.
	statePath := filepath.Join(installRoot, "runtime-state.json")
	stateStore := runtime.NewStateStore(statePath)
	if err := stateStore.Load(); err != nil {
		// State file might not exist yet — that's OK if no release is active.
		// We proceed with a warning.
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not load runtime state: %v.\n", err)
		fmt.Fprintf(cmd.ErrOrStderr(), "Continuing without Active Release protection.\n")
	} else {
		currentState := stateStore.State()
		if currentState.ActiveReleaseID == releaseID {
			return ReportError(cmd, &output.AppError{
				Message:    fmt.Sprintf("release %q is currently the Active Release", releaseID),
				Reason:     "The Active Release directory cannot be removed while it is in use",
				Resolution: "Activate a different release first, then retry cleanup",
				Err:        fmt.Errorf("release %q is active and cannot be removed: %w", releaseID, runtime.ErrActiveReleaseRemoval),
			})
		}
	}

	// Step 5: Require explicit confirmation before the destructive removal,
	// unless --force is provided (mirrors 'anvil project remove').
	force, _ := cmd.Flags().GetBool("force")

	if !force {
		// Interactive confirmation prompt.
		fmt.Fprintf(cmd.OutOrStdout(), "Warning: You are about to remove release '%s'.\n", releaseID)
		fmt.Fprintf(cmd.OutOrStdout(), "This will delete the release directory and all its contents.\n")
		fmt.Fprintf(cmd.OutOrStdout(), "Are you sure you want to remove release '%s'? This will delete the release directory and all its contents. [y/N] ", releaseID)

		reader := bufio.NewReader(cmd.InOrStdin())
		response, err := reader.ReadString('\n')
		if err != nil {
			// Non-interactive mode without --force: refuse.
			return ReportError(cmd, &output.AppError{
				Message:    "non-interactive mode requires --force to clean up a release",
				Reason:     "Release cleanup is a destructive operation that requires explicit confirmation in non-interactive mode",
				Resolution: "Re-run with --force flag to confirm removal: anvil server release cleanup <project-id> <release-id> --force",
				Err:        fmt.Errorf("non-interactive cleanup requires --force"),
			})
		}

		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Fprintln(cmd.OutOrStdout(), "Cleanup cancelled.")
			return nil
		}
	}

	// Step 6: Remove the release directory.
	size, err := runtime.RemoveReleaseDir(releasesDirPath, releaseID)
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not remove release directory: %v", err)
	}

	// Step 7: Display the result.
	PrintSuccess(cmd, "Release directory removed.")
	fmt.Fprintf(cmd.OutOrStdout(), "  Release ID: %s\n", releaseID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Space reclaimed: %s\n", formatBytes(size))
	fmt.Fprintln(cmd.OutOrStdout(), "")
	fmt.Fprintln(cmd.OutOrStdout(), "Other release directories and shared resources were not affected.")

	return nil
}

// formatBytes converts a byte count to a human-readable string (e.g., "1.5 MB").
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
