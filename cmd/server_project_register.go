// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P5-08, TS-P5-13, ADR-013, EPIC-005
package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/server"
)

// serverProjectRegisterCmd represents the "anvil server project register"
// command that registers a project in the Server Runtime Registry.
//
// Supports two modes:
//   - Interactive mode (default): prompts the user for project details
//     and asks for confirmation before registering.
//   - Non-interactive mode (--non-interactive or all required flags
//     provided): registers directly from flag values without prompts.
//
// Does not inspect a repository or require anvil.yaml.
//
// The registry is declarative metadata only: ownership (owner/group) and
// shared-link declarations were demoted per ADR-031 §3 and are not part
// of the v2 runtime — provisioning concerns are deferred to external
// configuration management and deployment tools.
//
// Reference: ST-P5-08, TS-P5-13, ADR-013
var serverProjectRegisterCmd = &cobra.Command{
	Use:   "register",
	Short: "Register a project in the Server Runtime Registry",
	Long: `Register a project in the Server Runtime Registry.

Creates a project registry entry at /etc/anvil/projects/{project-id}.yaml
(or configured override path).

Supports interactive mode (no flags) and non-interactive mode
(--non-interactive or all required flags provided).

Required fields:
  - project-id: unique project identifier
  - install-root: absolute filesystem path for the project

The delivery lifecycle standard the project uses is declared through
--standard (canonical key project.standard). The legacy --adapter flag
remains accepted for backward compatibility (see
docs/migration-guide-v2.md).

Does not inspect a repository or require anvil.yaml.`,
	Example: `  anvil server project register
  anvil server project register --project-id my-app --install-root /srv/apps/my-app
  anvil server project register --project-id my-app --install-root /srv/apps/my-app --display-name "My App" --standard laravel --non-interactive`,
	Args: cobra.NoArgs,
	RunE: runServerProjectRegister,
}

func init() {
	serverProjectCmd.AddCommand(serverProjectRegisterCmd)

	serverProjectRegisterCmd.Flags().String("server-root", "",
		"Override config root path (non-production only; overrides ANVIL_SERVER_ROOT env var)")

	// Non-interactive mode flags.
	serverProjectRegisterCmd.Flags().String("project-id", "", "Project ID (required for non-interactive mode)")
	serverProjectRegisterCmd.Flags().String("install-root", "", "Install root path (required for non-interactive mode)")
	serverProjectRegisterCmd.Flags().String("display-name", "", "Human-readable display name")
	serverProjectRegisterCmd.Flags().String("standard", "", "Delivery lifecycle standard the project uses (canonical key project.standard; e.g., laravel)")
	serverProjectRegisterCmd.Flags().String("adapter", "", "Deprecated alias for the standard declaration (project.adapter; use --standard; see docs/migration-guide-v2.md)")
	serverProjectRegisterCmd.Flags().Bool("non-interactive", false, "Skip prompts and use flag values only")
}

// runServerProjectRegister executes the "anvil server project register" command.
//
// It resolves the config root path and determines whether to run in
// interactive or non-interactive mode based on the provided flags.
func runServerProjectRegister(cmd *cobra.Command, args []string) error {
	rootPath := resolveServerRoot(cmd)

	if rootPath != server.DefaultConfigRoot {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: using non-default server root %q (non-production override)\n", rootPath)
	}

	// 1. Check Runtime is initialized (precondition error, exit 4).
	if err := RequireServerInitialized(cmd, rootPath); err != nil {
		return err
	}

	registryStore := server.NewRegistryStore(rootPath)

	// Determine mode.
	nonInteractive, _ := cmd.Flags().GetBool("non-interactive")

	// Check if required flags were explicitly provided by the user.
	projectIDFlag := cmd.Flags().Lookup("project-id")
	installRootFlag := cmd.Flags().Lookup("install-root")
	projectIDExplicit := projectIDFlag != nil && projectIDFlag.Changed
	installRootExplicit := installRootFlag != nil && installRootFlag.Changed

	useNonInteractive := nonInteractive || (projectIDExplicit && installRootExplicit)

	var project server.ProjectRegistry

	if useNonInteractive {
		// Non-interactive mode: read flag values.
		flagProjectID, _ := cmd.Flags().GetString("project-id")
		flagInstallRoot, _ := cmd.Flags().GetString("install-root")
		flagDisplayName, _ := cmd.Flags().GetString("display-name")
		flagStandard, _ := cmd.Flags().GetString("standard")
		flagAdapter, _ := cmd.Flags().GetString("adapter")

		project = server.ProjectRegistry{
			Project: server.ProjectSection{
				ID:          flagProjectID,
				DisplayName: flagDisplayName,
				InstallRoot: flagInstallRoot,
				Standard:    flagStandard,
				Adapter:     flagAdapter,
			},
		}

		// The legacy --adapter flag is an alias for --standard during the
		// deprecation window: using it emits a deprecation warning naming
		// project.standard (TS-019-02-02, ADR-032 — registration
		// automation contract: "--adapter ... with a warning"). The
		// warning goes to stderr; stdout stays machine-readable.
		project.Project.WarnIfLegacyAdapter(cmd.ErrOrStderr())

		// Validate required fields. Declaring both --standard and
		// --adapter is rejected (ADR-032 — the rename policy is
		// explicit, never a silent preference).
		if err := project.Validate(); err != nil {
			return ReportPlainErrorf(cmd, err, "%v", err)
		}
	} else {
		// Interactive mode: prompt for all fields.
		reader := bufio.NewReader(cmd.InOrStdin())

		projectID := promptInput(reader, cmd, "Project ID")
		installRoot := promptInputWithDefault(reader, cmd, "Install root", "/srv/apps")
		displayName := promptInput(reader, cmd, "Display name (optional)")
		standard := promptInput(reader, cmd, "Standard (optional)")

		project = server.ProjectRegistry{
			Project: server.ProjectSection{
				ID:          projectID,
				DisplayName: displayName,
				InstallRoot: installRoot,
				Standard:    standard,
			},
		}

		// Display configuration summary. The summary reads the resolved
		// standard; a legacy project.adapter declaration emits the
		// deprecation warning on stderr (TS-019-02-02, ADR-032).
		fmt.Fprintln(styleFor(cmd).W, "")
		fmt.Fprintln(styleFor(cmd).W, "Configuration summary:")
		fmt.Fprintf(styleFor(cmd).W, "  Project ID:      %s\n", project.Project.ID)
		fmt.Fprintf(styleFor(cmd).W, "  Install root:    %s\n", project.Project.InstallRoot)
		fmt.Fprintf(styleFor(cmd).W, "  Display name:    %s\n", project.Project.DisplayName)
		project.Project.WarnIfLegacyAdapter(cmd.ErrOrStderr())
		fmt.Fprintf(styleFor(cmd).W, "  Standard:        %s\n", project.Project.StandardName())
		fmt.Fprintln(styleFor(cmd).W, "")

		// Ask for confirmation.
		confirm := promptConfirmation(reader, cmd, "Proceed with registration?")
		if !confirm {
			fmt.Fprintln(styleFor(cmd).W, "Registration cancelled.")
			return nil
		}
	}

	// 5. Register the project. A duplicate project ID is a configuration
	// error (exit 2, TS-P8-07 / ADR-010 §8.1) per the Registration
	// Automation Contract; all other failures are general errors (exit 1).
	if err := registryStore.Register(project); err != nil {
		if errors.Is(err, server.ErrProjectAlreadyRegistered) {
			return ReportErrorWithCode(cmd, &output.AppError{
				Message:    fmt.Sprintf("project already registered: %q", project.Project.ID),
				Reason:     "A project with the same ID already exists in the Server Runtime Registry",
				Resolution: "Use a different project ID, or inspect the existing registration with 'anvil server project get'",
				Err:        err,
			}, output.ExitCodeConfig)
		}
		return ReportPlainErrorf(cmd, err, "%v", err)
	}

	// 6. Success. No provisioning is performed here: registry ownership
	// semantics and directory provisioning were demoted per ADR-031 §3;
	// install roots are provisioned by external configuration management.
	projectPath := registryStore.ProjectPath(project.Project.ID)
	PrintSuccessf(cmd, "Project %s registered at %s", project.Project.ID, projectPath)

	return nil
}

// promptInput prompts the user for a value and returns the trimmed input.
func promptInput(reader *bufio.Reader, cmd *cobra.Command, label string) string {
	fmt.Fprintf(styleFor(cmd).W, "%s: ", label)
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

// promptInputWithDefault prompts the user for a value with a default and
// returns the trimmed input, or the default if the input is empty.
func promptInputWithDefault(reader *bufio.Reader, cmd *cobra.Command, label, defaultVal string) string {
	fmt.Fprintf(styleFor(cmd).W, "%s [%s]: ", label, defaultVal)
	input, _ := reader.ReadString('\n')
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return defaultVal
	}
	return trimmed
}

// promptConfirmation asks the user for a y/N confirmation and returns
// true only if the response is "y" or "Y".
func promptConfirmation(reader *bufio.Reader, cmd *cobra.Command, prompt string) bool {
	fmt.Fprintf(styleFor(cmd).W, "%s [y/N]: ", prompt)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	return strings.EqualFold(input, "y")
}
