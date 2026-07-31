package cmd

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/engine"
	"maleolabs.com/anvil/internal/output"
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
		MaximumNArgsWithUsage(1, "anvil init my-project"),
		func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 || args[0] == "" {
				return fmt.Errorf("the %q command requires 1 argument: <name>\nExample: anvil init my-project",
					cmd.CommandPath())
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
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("the %q command requires 1 argument: <name>", cmd.CommandPath()),
			Resolution: "Provide a project name, e.g.: anvil init my-project",
		})
	}

	if !validProjectName.MatchString(name) {
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("invalid project name '%s'", name),
			Reason:     "Project names may only contain letters, numbers, hyphens (-), and underscores (_)",
			Resolution: "Choose a name using only allowed characters, e.g.: anvil init my-project",
		})
	}

	result, err := engine.Initialize(name, path)
	if err != nil {
		if errors.Is(err, engine.ErrProjectAlreadyExists) {
			return ReportError(cmd, &output.AppError{
				Message:    fmt.Sprintf("project already exists in '%s'", path),
				Reason:     "The target directory already contains an Anvil project configuration",
				Resolution: "Use a different directory or remove the existing project first",
				Err:        err,
			})
		}
		return ReportPlainErrorf(cmd, err, "could not create project: %v", err)
	}

	switch result {
	case engine.ResultCreated:
		fmt.Fprintf(cmd.OutOrStdout(), "Project '%s' created. Ready for use.\n", name)
		fmt.Fprintln(cmd.OutOrStdout(), "Next steps:")
		fmt.Fprintf(cmd.OutOrStdout(), "  cd %s && anvil config validate\n", path)
	}

	return nil
}
