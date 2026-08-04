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

// parseSharedLink parses a --shared-link flag value in the format
// "from=shared/config/.env,to=.env" into a server.SharedLink.
func parseSharedLink(raw string) (server.SharedLink, error) {
	var link server.SharedLink
	parts := strings.Split(raw, ",")
	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "from":
			link.From = kv[1]
		case "to":
			link.To = kv[1]
		}
	}
	if link.From == "" || link.To == "" {
		return link, fmt.Errorf("invalid shared-link format %q: expected from=<path>,to=<path>", raw)
	}
	return link, nil
}

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

Does not inspect a repository or require anvil.yaml.`,
	Example: `  anvil server project register
  anvil server project register --project-id my-app --install-root /srv/apps/my-app
  anvil server project register --project-id my-app --install-root /srv/apps/my-app --display-name "My App" --non-interactive`,
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
	serverProjectRegisterCmd.Flags().String("adapter", "", "Deployment adapter (e.g., laravel, node)")
	serverProjectRegisterCmd.Flags().String("owner", "", "Responsible user or team")
	serverProjectRegisterCmd.Flags().String("group", "", "System group for file ownership")
	serverProjectRegisterCmd.Flags().StringArray("shared-link", []string{},
		"Shared resource symlink in format from=<path>,to=<path> (repeatable)")
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
		flagAdapter, _ := cmd.Flags().GetString("adapter")
		flagOwner, _ := cmd.Flags().GetString("owner")
		flagGroup, _ := cmd.Flags().GetString("group")
		var sharedLinks []server.SharedLink
		sharedLinkFlag := cmd.Flags().Lookup("shared-link")
		if sharedLinkFlag != nil && sharedLinkFlag.Changed {
			flagSharedLinks, _ := cmd.Flags().GetStringArray("shared-link")
			for _, raw := range flagSharedLinks {
				if raw == "" {
					continue
				}
				link, err := parseSharedLink(raw)
				if err != nil {
					return ReportPlainErrorf(cmd, err, "%v", err)
				}
				sharedLinks = append(sharedLinks, link)
			}
		}

		project = server.ProjectRegistry{
			Project: server.ProjectSection{
				ID:          flagProjectID,
				DisplayName: flagDisplayName,
				InstallRoot: flagInstallRoot,
				Adapter:     flagAdapter,
				Owner:       flagOwner,
				Group:       flagGroup,
				SharedLinks: sharedLinks,
			},
		}

		// Validate required fields.
		if err := project.Validate(); err != nil {
			return ReportPlainErrorf(cmd, err, "%v", err)
		}
	} else {
		// Interactive mode: prompt for all fields.
		reader := bufio.NewReader(cmd.InOrStdin())

		projectID := promptInput(reader, cmd, "Project ID")
		installRoot := promptInputWithDefault(reader, cmd, "Install root", "/srv/apps")
		displayName := promptInput(reader, cmd, "Display name (optional)")
		adapter := promptInput(reader, cmd, "Adapter (optional)")
		owner := promptInput(reader, cmd, "Owner (optional)")
		group := promptInput(reader, cmd, "Group (optional)")

		project = server.ProjectRegistry{
			Project: server.ProjectSection{
				ID:          projectID,
				DisplayName: displayName,
				InstallRoot: installRoot,
				Adapter:     adapter,
				Owner:       owner,
				Group:       group,
			},
		}

		// Display configuration summary.
		fmt.Fprintln(cmd.OutOrStdout(), "")
		fmt.Fprintln(cmd.OutOrStdout(), "Configuration summary:")
		fmt.Fprintf(cmd.OutOrStdout(), "  Project ID:      %s\n", project.Project.ID)
		fmt.Fprintf(cmd.OutOrStdout(), "  Install root:    %s\n", project.Project.InstallRoot)
		fmt.Fprintf(cmd.OutOrStdout(), "  Display name:    %s\n", project.Project.DisplayName)
		fmt.Fprintf(cmd.OutOrStdout(), "  Adapter:         %s\n", project.Project.Adapter)
		fmt.Fprintf(cmd.OutOrStdout(), "  Owner:           %s\n", project.Project.Owner)
		fmt.Fprintf(cmd.OutOrStdout(), "  Group:           %s\n", project.Project.Group)
		fmt.Fprintln(cmd.OutOrStdout(), "")

		// Ask for confirmation.
		confirm := promptConfirmation(reader, cmd, "Proceed with registration?")
		if !confirm {
			fmt.Fprintln(cmd.OutOrStdout(), "Registration cancelled.")
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

	// 6. Provision the project directory structure and apply ownership.
	if err := server.ProvisionProjectDir(project.Project.InstallRoot, project.Project.Owner, project.Project.Group); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), FmtWarning("project registered but directory setup failed: %v"), err)
		fmt.Fprintf(cmd.ErrOrStderr(), "Resolution: Directory structure may be incomplete. Check permissions and retry.\n")
		// Not a hard error — registry entry already created.
	}

	// 7. Success.
	projectPath := registryStore.ProjectPath(project.Project.ID)
	PrintSuccessf(cmd, "Project %s registered at %s", project.Project.ID, projectPath)
	if len(project.Project.SharedLinks) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "Shared links:")
		for _, link := range project.Project.SharedLinks {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s -> %s\n", link.To, link.From)
		}
	}

	return nil
}

// promptInput prompts the user for a value and returns the trimmed input.
func promptInput(reader *bufio.Reader, cmd *cobra.Command, label string) string {
	fmt.Fprintf(cmd.OutOrStdout(), "%s: ", label)
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

// promptInputWithDefault prompts the user for a value with a default and
// returns the trimmed input, or the default if the input is empty.
func promptInputWithDefault(reader *bufio.Reader, cmd *cobra.Command, label, defaultVal string) string {
	fmt.Fprintf(cmd.OutOrStdout(), "%s [%s]: ", label, defaultVal)
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
	fmt.Fprintf(cmd.OutOrStdout(), "%s [y/N]: ", prompt)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	return strings.EqualFold(input, "y")
}
