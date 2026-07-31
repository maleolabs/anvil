package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/project"
)

// RequireProject loads the project configuration for the current working
// directory. If no Anvil project is found, it prints a user-friendly error
// message (including the searched directories and guidance) to stderr and
// returns the error.
//
// Project-dependent commands should call this function early in their RunE
// to ensure they are executing within an Anvil project context.
//
// Returns:
//   - *project.ProjectConfig when a valid project is found
//   - error (ErrNoProjectFound) when no project exists in the search path
//
// Reference: ST-P1-06
func RequireProject(cmd *cobra.Command) (*project.ProjectConfig, error) {
	cfg, searched, err := project.LoadSearched()
	if err != nil {
		if errors.Is(err, project.ErrNoProjectFound) {
			fmt.Fprint(cmd.ErrOrStderr(), project.FormatMissingProjectError(searched))
			return nil, err
		}
		return nil, err
	}
	return cfg, nil
}
