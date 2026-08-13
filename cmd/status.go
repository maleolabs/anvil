package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// statusCmd represents the "anvil status" command that displays information
// about the current Anvil project. It is a project-dependent command — it
// requires an existing Anvil project in the current or a parent directory.
//
// When invoked outside an Anvil project, it prints the missing-project
// guidance message and returns a non-zero exit code.
//
// Reference: ST-P1-06
var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Display the current Anvil project status",
	Long: `Show information about the Anvil project in the current directory.

If no Anvil project is found, this command prints a descriptive error
message listing the directories that were searched along with guidance
on how to create or find a project.`,
	Example:       `  anvil status`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	cfg, err := RequireProject(cmd)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Project: %s\n", cfg.Project.Name)
	fmt.Fprintf(cmd.OutOrStdout(), "Version: %s\n", cfg.Project.Version)
	if cfg.Project.Description != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Description: %s\n", cfg.Project.Description)
	}
	return nil
}
