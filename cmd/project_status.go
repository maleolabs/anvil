package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/output"
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
	Example: `  anvil project status`,
	Args:    cobra.NoArgs,
	RunE:    runProjectStatus,
}

func init() {
	projectCmd.AddCommand(projectStatusCmd)
}

func runProjectStatus(cmd *cobra.Command, args []string) error {
	s := styleFor(cmd)
	cfg, err := RequireProject(cmd)
	if err != nil {
		return err
	}
	root, err := project.Discover()
	if err != nil {
		return err
	}
	stage, err := project.LoadLifecycleState(root)
	if err != nil {
		stage = project.StageActive
	}
	// Modern Header
	h := output.NewHeader(s, "Project")
	h.Add("Name", cfg.Project.Name)
	h.Add("Version", cfg.Project.Version)
	if cfg.Project.Description != "" {
		h.Add("Description", cfg.Project.Description)
	}
	h.Add("Lifecycle", stage.String())
	h.Pipeline("Configuration")
	h.Render()
	result := project.ValidateProject(cfg)
	if result.Valid {
		fmt.Fprintln(s.W, s.Success("  "+output.IconDone+" Configuration valid"))
	} else {
		fmt.Fprintln(s.W, s.Error("  Configuration invalid"))
		tbl := output.NewStyledTable(s, "Issue", "Details")
		for _, e := range result.Errors {
			tbl.AddRow([]string{"•", e}, []func(string) string{nil, s.Dim})
		}
		tbl.Render()
	}
	return nil
}
