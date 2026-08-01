// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P4-07, EPIC-004, EPIC-005
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/server"
)

// rollbackCmd represents the "anvil server release rollback" subcommand
// that rolls back the currently Active Release, restoring the previously
// Active Release.
//
// Reference: ST-P4-07
var rollbackCmd = &cobra.Command{
	Use:   "rollback <project-id>",
	Short: "Rollback the Active Release",
	Long: `Rollback the currently Active Release restoring the previously Active Release.

The rollback process:
  1. Validates the Active Release is eligible for rollback
  2. Identifies the previously Active Release as the rollback target
  3. Reverses configuration changes (shared resources)
  4. Switches the active symlink to the target Release
  5. Transitions the rolled-back Release to RolledBack stage

The rolled-back Release is preserved for inspection.
Active Release that was restored is now Active and serving traffic.

Examples:
  anvil server release rollback my-project
  anvil server release rollback my-project --server-root /tmp/anvil`,
	Args: ExactArgsWithUsage(1, "anvil server release rollback my-project"),
	RunE: runRollback,
}

func init() {
	serverReleaseCmd.AddCommand(rollbackCmd)

	rollbackCmd.Flags().String("server-root", "",
		"Override config root path (non-production only; overrides ANVIL_SERVER_ROOT env var)")
}

// runRollback executes the release rollback command.
//
// It resolves the server root, delegates rollback to the
// ServerReleaseCoordinator, and reports the result showing both the
// rolled-back and restored Release information.
func runRollback(cmd *cobra.Command, args []string) error {
	projectID := args[0]

	rootPath := resolveServerRoot(cmd)

	if rootPath != server.DefaultConfigRoot {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: using non-default server root %q (non-production override)\n", rootPath)
	}

	// Require the Server Runtime to be initialized before proceeding.
	if err := RequireServerInitialized(cmd, rootPath); err != nil {
		return err
	}

	// Step 1: Delegate to the ServerReleaseCoordinator.
	coordinator := server.NewServerReleaseCoordinator(rootPath)

	result, err := coordinator.Rollback(projectID)
	if err != nil {
		return ReportPlainErrorf(cmd, err, "%v", err)
	}

	// Step 2: Display the result.
	PrintSuccess(cmd, "Rollback completed.")
	fmt.Fprintf(cmd.OutOrStdout(), "  Rolled Back Release ID: %s\n", result.RolledBackRelease.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Rolled Back Stage: %s\n", result.RolledBackRelease.Stage)
	fmt.Fprintf(cmd.OutOrStdout(), "  Restored Release ID: %s\n", result.RestoredRelease.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Restored Stage: %s\n", result.RestoredRelease.Stage)
	fmt.Fprintln(cmd.OutOrStdout(), "")
	PrintSuccess(cmd, "The previously Active Release is now restored and serving traffic.")

	return nil
}
