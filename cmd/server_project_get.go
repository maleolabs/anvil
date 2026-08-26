// Package cmd implements the Anvil CLI commands.
//
// Reference: TS-P5-13, ADR-013, EPIC-005
package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/server"
)

// serverProjectGetCmd represents the "anvil server project get" command
// that looks up a registered project by its project ID.
//
// Resolves Registry data by project ID and displays the project
// configuration. Never discovers from cwd or repository.
//
// Reference: TS-P5-13, ADR-013
var serverProjectGetCmd = &cobra.Command{
	Use:   "get <project-id>",
	Short: "Look up a registered project by project ID",
	Long: `Look up a registered project in the Server Runtime Registry.

Resolves the project configuration by its project ID from the Registry
at /etc/anvil/projects/{project-id}.yaml (or configured override path).

Displays the project configuration as formatted output.

Does not inspect a repository or require anvil.yaml.`,
	Example: `  anvil server project get my-app
  anvil server project get my-app --server-root /tmp/anvil`,
	Args: ExactArgsWithUsage(1, "anvil server project get my-app"),
	RunE: runServerProjectGet,
}

func init() {
	serverProjectCmd.AddCommand(serverProjectGetCmd)

	serverProjectGetCmd.Flags().String("server-root", "",
		"Override config root path (non-production only; overrides ANVIL_SERVER_ROOT env var)")
}

// runServerProjectGet executes the "anvil server project get" command.
//
// It resolves the config root path, loads the project registry by the
// provided project ID, and displays the project configuration.
func runServerProjectGet(cmd *cobra.Command, args []string) error {
	rootPath := resolveServerRoot(cmd)
	projectID := args[0]

	if rootPath != server.DefaultConfigRoot {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: using non-default server root %q (non-production override)\n", rootPath)
	}

	// Check Runtime is initialized (precondition error, exit 4). A lookup
	// against an uninitialized Runtime is a missing prerequisite, not a
	// missing project (Registration Automation Contract).
	if err := RequireServerInitialized(cmd, rootPath); err != nil {
		return err
	}

	registryStore := server.NewRegistryStore(rootPath)

	cfg, err := registryStore.Load(projectID)
	if err != nil {
		// A missing project is a runtime error (exit 3, TS-P8-07 /
		// ADR-010 §8.1) per the Registration Automation Contract; other
		// load failures (unreadable/corrupt registry file) are general
		// errors (exit 1).
		if errors.Is(err, server.ErrProjectNotFound) {
			return ReportErrorWithCode(cmd, &output.AppError{
				Message:    fmt.Sprintf("project %q not found in the Server Runtime Registry", projectID),
				Reason:     "No project with the given ID is registered",
				Resolution: "Check the project ID, or register it with 'anvil server project register'",
				Err:        err,
			}, output.ExitCodeRuntime)
		}
		return ReportPlainErrorf(cmd, err, "%v", err)
	}

	// Display project configuration.
	fmt.Fprintln(styleFor(cmd).W, "Project:")
	fmt.Fprintf(styleFor(cmd).W, "  ID:             %s\n", cfg.Project.ID)
	if cfg.Project.DisplayName != "" {
		fmt.Fprintf(styleFor(cmd).W, "  Display Name:   %s\n", cfg.Project.DisplayName)
	}
	fmt.Fprintf(styleFor(cmd).W, "  Install Root:   %s\n", cfg.Project.InstallRoot)
	// The resolved standard (canonical project.standard; the legacy
	// project.adapter key is read as an alias during the deprecation
	// window — every read emits a deprecation warning naming
	// project.standard on stderr, so stdout stays machine-readable,
	// TS-019-02-02 / ADR-032).
	cfg.Project.WarnIfLegacyAdapter(cmd.ErrOrStderr())
	if std := cfg.Project.StandardName(); std != "" {
		fmt.Fprintf(styleFor(cmd).W, "  Standard:       %s\n", std)
	}

	return nil
}
