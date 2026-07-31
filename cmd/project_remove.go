package cmd

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/project"
)

// projectRemoveCmd represents the 'anvil project remove' command.
var projectRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove an Anvil project",
	Long: `Remove the Anvil project in the current directory and all its contents.

The command prompts for confirmation before removing the project directory,
configuration, and state. Use the --force flag to skip the confirmation
prompt for non-interactive or automated environments.

The removal process:
  1. Loads the project configuration from anvil.yaml
  2. Transitions the project lifecycle state to Removed
  3. Removes the project directory and all contents

This operation is irreversible. Make sure to back up any data before
proceeding.

Examples:
  anvil project remove
  anvil project remove --force`,
	Args: cobra.NoArgs,
	RunE: runProjectRemove,
}

func init() {
	projectRemoveCmd.Flags().Bool("force", false, "Skip confirmation prompt (non-interactive mode)")
	projectCmd.AddCommand(projectRemoveCmd)
}

func runProjectRemove(cmd *cobra.Command, args []string) error {
	// Load the project configuration to display the project name.
	cfg, err := RequireProject(cmd)
	if err != nil {
		return err
	}

	projectName := ""
	if cfg.Project != nil {
		projectName = cfg.Project.Name
	}

	force, _ := cmd.Flags().GetBool("force")

	if !force {
		// Interactive confirmation prompt.
		fmt.Fprintf(cmd.OutOrStdout(), "Warning: You are about to remove project '%s'.\n", projectName)
		fmt.Fprintf(cmd.OutOrStdout(), "This will delete the project directory and all its contents.\n")
		fmt.Fprintf(cmd.OutOrStdout(), "Are you sure you want to remove project '%s'? This will delete the project directory and all its contents. [y/N] ", projectName)

		reader := bufio.NewReader(cmd.InOrStdin())
		response, err := reader.ReadString('\n')
		if err != nil {
			// Non-interactive mode without --force: refuse.
			fmt.Fprintf(cmd.ErrOrStderr(), "Error: non-interactive mode requires --force to remove a project.\n")
			return fmt.Errorf("non-interactive removal requires --force")
		}

		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Fprintln(cmd.OutOrStdout(), "Removal cancelled.")
			return nil
		}
	}

	// Perform the removal.
	root, err := project.Discover()
	if err != nil {
		// This should not happen since RequireProject already succeeded,
		// but handle it defensively.
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: could not locate project root: %v.\n", err)
		return err
	}

	if err := project.RemoveProject(root); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: could not remove project: %v.\n", err)
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Project '%s' has been removed.\n", projectName)
	return nil
}
