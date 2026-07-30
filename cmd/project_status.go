package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/project"
)

// projectStatusCmd represents the 'anvil project status' command that displays
// the current lifecycle stage and configuration status of an Anvil project.
//
// It requires an existing Anvil project in the current or a parent directory.
// When invoked outside an Anvil project, it prints the missing-project
// guidance message and returns a non-zero exit code.
//
// Reference: ST-P1-08
var projectStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Display project status and lifecycle stage",
	Long: `Show the current lifecycle stage and configuration status of the Anvil project.

Displays the project name, lifecycle stage (created, active, modified, or removed),
and whether the project configuration is valid.

The lifecycle stage reflects the project's current phase:
  - created:  Project has been initialized but not yet activated
  - active:   Project is active and in normal operation
  - modified: Project configuration has been changed
  - removed:  Project has been removed (terminal stage)

When the project has been removed, this command reports that the project
no longer exists because the configuration file is absent.`,
	Args: cobra.NoArgs,
	RunE: runProjectStatus,
}

func init() {
	projectCmd.AddCommand(projectStatusCmd)
}

func runProjectStatus(cmd *cobra.Command, args []string) error {
	cfg, err := RequireProject(cmd)
	if err != nil {
		return err
	}

	// Get project root for lifecycle state.
	root, err := project.Discover()
	if err != nil {
		return err
	}

	// Load lifecycle stage.
	stage, err := project.LoadLifecycleState(root)
	if err != nil {
		// Log but don't fail — lifecycle state may not exist yet.
		stage = project.StageActive // safe default
	}

	// Display project info.
	fmt.Fprintf(cmd.OutOrStdout(), "Project: %s\n", cfg.Project.Name)
	fmt.Fprintf(cmd.OutOrStdout(), "Lifecycle: %s\n", stage.String())

	// Display config validity.
	result := project.ValidateProject(cfg)
	if result.Valid {
		fmt.Fprintf(cmd.OutOrStdout(), "Configuration: valid\n")
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Configuration: invalid\n")
		for _, errMsg := range result.Errors {
			fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", errMsg)
		}
	}

	return nil
}
