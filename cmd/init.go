package cmd

import (
	"errors"
	"fmt"
	"regexp"

	"maleolabs.com/anvil/internal/engine"
	"github.com/spf13/cobra"
)

var (
	// validProjectName matches names containing only alphanumeric characters,
	// hyphens, and underscores.
	validProjectName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

	// ErrInvalidProjectName is returned when the project name contains
	// disallowed characters.
	ErrInvalidProjectName = errors.New("project name contains invalid characters")
)

// initFlags holds parsed flags for the init command.
type initFlags struct {
	path string
}

var initCmd = &cobra.Command{
	Use:   "init <name>",
	Short: "Initialize a new Anvil project",
	Long: `Create a new Anvil project with a valid configuration file
containing sensible defaults and the expected directory structure.

The project name must contain only letters, numbers, hyphens,
and underscores.

Examples:
  anvil init my-project
  anvil init my-project --path /var/www/my-app`,
	Args: cobra.MatchAll(
		cobra.MaximumNArgs(1),
		func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 || args[0] == "" {
				return fmt.Errorf("project name is required")
			}
			return nil
		},
	),
	RunE: runInit,
}

func init() {
	initCmd.Flags().String("path", ".", "Target directory for the project")
}

func runInit(cmd *cobra.Command, args []string) error {
	name := args[0]
	path, _ := cmd.Flags().GetString("path")

	if name == "" {
		fmt.Fprintln(cmd.ErrOrStderr(), "Error: project name is required.")
		fmt.Fprintln(cmd.ErrOrStderr(), "Usage: anvil init <name>")
		fmt.Fprintln(cmd.ErrOrStderr(), "Example: anvil init my-project")
		return fmt.Errorf("project name is required")
	}

	if !validProjectName.MatchString(name) {
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: invalid project name '%s'.\n", name)
		fmt.Fprintln(cmd.ErrOrStderr(), "Project names may only contain letters, numbers, hyphens (-), and underscores (_).")
		fmt.Fprintln(cmd.ErrOrStderr(), "Example: anvil init my-project")
		return ErrInvalidProjectName
	}

	result, err := engine.Initialize(name, path)
	if err != nil {
		if errors.Is(err, engine.ErrProjectAlreadyExists) {
			fmt.Fprintf(cmd.ErrOrStderr(), "Error: project already exists in '%s'.\n", path)
			fmt.Fprintln(cmd.ErrOrStderr(), "Use a different directory or remove the existing project first.")
			return err
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: could not create project: %v.\n", err)
		return err
	}

	switch result {
	case engine.ResultCreated:
		fmt.Fprintf(cmd.OutOrStdout(), "Project '%s' created. Ready for use.\n", name)
		fmt.Fprintln(cmd.OutOrStdout(), "Next steps:")
		fmt.Fprintf(cmd.OutOrStdout(), "  cd %s && anvil config validate\n", path)
	}

	return nil
}
