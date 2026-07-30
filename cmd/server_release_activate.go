// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P4-05, ST-P4-14, EPIC-004, EPIC-005
package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/project"
	"maleolabs.com/anvil/internal/release"
	"maleolabs.com/anvil/internal/server"
)

// activateCmd represents the "anvil server release activate" subcommand
// that activates a Runtime Release from the Ready stage through the
// activation phase sequence (Prepare → Configure → Promote).
//
// Reference: ST-P4-05
var activateCmd = &cobra.Command{
	Use:   "activate <project-id> <release-id>",
	Short: "Activate a Runtime Release",
	Long: `Activate a Runtime Release, transitioning it from Ready to Active.

The activation process:
  1. Validates the Release is in Ready stage
  2. Transitions the Release to Activating stage
  3. Configures shared resources
  4. Promotes the Release to Active atomically
  5. Updates the runtime state with the active release

The Release must be in Ready stage to be activated.
Once activated, the Release is deployed and serving traffic.

Examples:
  anvil server release activate my-project abc123def456
  anvil server release activate my-project --server-root /tmp/anvil`,
	Args: cobra.ExactArgs(2),
	RunE: runActivate,
}

func init() {
	serverReleaseCmd.AddCommand(activateCmd)

	activateCmd.Flags().String("server-root", "",
		"Override config root path (non-production only; overrides ANVIL_SERVER_ROOT env var)")
}

// runActivate executes the release activate command.
//
// It resolves the server root, delegates activation to the
// ServerReleaseCoordinator, loads the activated Release for display,
// and reports the result.
func runActivate(cmd *cobra.Command, args []string) error {
	projectID := args[0]
	releaseID := args[1]

	rootPath := resolveServerRoot(cmd)

	if rootPath != server.DefaultConfigRoot {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: using non-default server root %q (non-production override)\n", rootPath)
	}

	// Step 1: Delegate to the ServerReleaseCoordinator.
	coordinator := server.NewServerReleaseCoordinator(rootPath)

	if err := coordinator.Activate(projectID, releaseID); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: %v.\n", err)
		return err
	}

	// Step 2: Load the activated Release for display.
	registryStore := server.NewRegistryStore(rootPath)
	reg, err := registryStore.Load(projectID)
	if err != nil {
		// Non-fatal for display — activation already succeeded.
		fmt.Fprintf(cmd.OutOrStdout(), "Release activated.\n")
		fmt.Fprintf(cmd.OutOrStdout(), "  Release ID: %s\n", releaseID)
		fmt.Fprintln(cmd.OutOrStdout(), "")
		fmt.Fprintln(cmd.OutOrStdout(), "The Release is now active.")
		return nil
	}

	installRoot := reg.Project.InstallRoot
	s := project.NewStructure(installRoot)
	releasePath := filepath.Join(s.StateDir, "releases", releaseID+".json")

	rel, err := release.Load(releasePath)
	if err != nil {
		// Non-fatal for display — activation already succeeded.
		fmt.Fprintf(cmd.OutOrStdout(), "Release activated.\n")
		fmt.Fprintf(cmd.OutOrStdout(), "  Release ID: %s\n", releaseID)
		fmt.Fprintln(cmd.OutOrStdout(), "")
		fmt.Fprintln(cmd.OutOrStdout(), "The Release is now active.")
		return nil
	}

	// Step 3: Display the result.
	fmt.Fprintf(cmd.OutOrStdout(), "Release activated.\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  Release ID: %s\n", rel.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Stage: %s\n", rel.Stage)
	fmt.Fprintln(cmd.OutOrStdout(), "")
	fmt.Fprintln(cmd.OutOrStdout(), "The Release is now active.")

	return nil
}
