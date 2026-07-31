// Package cmd implements the Anvil CLI commands.
//
// Reference: TS-P5-13, ADR-013, EPIC-005
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
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

	registryStore := server.NewRegistryStore(rootPath)

	cfg, err := registryStore.Load(projectID)
	if err != nil {
		return ReportPlainErrorf(cmd, err, "%v", err)
	}

	// Display project configuration.
	fmt.Fprintln(cmd.OutOrStdout(), "Project:")
	fmt.Fprintf(cmd.OutOrStdout(), "  ID:             %s\n", cfg.Project.ID)
	if cfg.Project.DisplayName != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  Display Name:   %s\n", cfg.Project.DisplayName)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "  Install Root:   %s\n", cfg.Project.InstallRoot)
	if cfg.Project.Adapter != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  Adapter:        %s\n", cfg.Project.Adapter)
	}
	if cfg.Project.Owner != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  Owner:          %s\n", cfg.Project.Owner)
	}
	if cfg.Project.Group != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  Group:          %s\n", cfg.Project.Group)
	}

	return nil
}
