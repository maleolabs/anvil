// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P5-09, ADR-013, EPIC-005
package cmd

import (
	"fmt"

	"maleolabs.com/anvil/internal/server"
	"github.com/spf13/cobra"
)

// serverStatusCmd represents the "anvil server status" command that reports
// the Server Runtime status, including initialization state, registry status,
// and project registration overview.
//
// When called without a project ID, it displays the initialization state and
// an overview of all registered projects.
// When called with a project ID, it additionally shows detailed information
// for that specific project.
//
// This is a read-only command — it does not modify any state.
//
// Reference: ST-P5-09, ADR-013
var serverStatusCmd = &cobra.Command{
	Use:   "status [<project-id>]",
	Short: "Display Server Runtime status and readiness",
	Long: `Display the Server Runtime status and project registry overview.

Without a project ID, shows:
  - Runtime initialization state
  - All registered projects overview

With a project ID, additionally shows:
  - Detailed project configuration

This is a read-only command — it does not modify any state.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runServerStatus,
}

func init() {
	serverCmd.AddCommand(serverStatusCmd)

	serverStatusCmd.Flags().String("server-root", "",
		"Override config root path (non-production only; overrides ANVIL_SERVER_ROOT env var)")
}

// runServerStatus executes the "anvil server status" command.
//
// It resolves the config root path, checks Runtime initialization status,
// and displays project information.
func runServerStatus(cmd *cobra.Command, args []string) error {
	rootPath := resolveServerRoot(cmd)

	if rootPath != server.DefaultConfigRoot {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: using non-default server root %q (non-production override)\n", rootPath)
	}

	configStore := server.NewConfigStore(rootPath)
	registryStore := server.NewRegistryStore(rootPath)

	// --- Section 1: Runtime initialization status ---
	fmt.Fprintln(cmd.OutOrStdout(), "Server Runtime Status")
	fmt.Fprintln(cmd.OutOrStdout(), "=====================")
	fmt.Fprintln(cmd.OutOrStdout(), "")

	if configStore.Exists() {
		cfg, err := configStore.Load()
		if err != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "Runtime:  initialized (load error: %v)\n", err)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Runtime:  initialized\n")
			fmt.Fprintf(cmd.OutOrStdout(), "  Config: %s\n", configStore.ConfigPath())
			fmt.Fprintf(cmd.OutOrStdout(), "  ID:     %s\n", displayOrDefault(cfg.Runtime.ID, "(empty)"))
			if cfg.Runtime.DisplayName != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  Name:   %s\n", cfg.Runtime.DisplayName)
			}
		}
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Runtime:  not initialized\n")
		fmt.Fprintf(cmd.OutOrStdout(), "  Config: %s (not found)\n", configStore.ConfigPath())
		fmt.Fprintln(cmd.OutOrStdout(), "")
		fmt.Fprintln(cmd.OutOrStdout(), "Run 'anvil server init' to initialize the Server Runtime.")
	}

	fmt.Fprintln(cmd.OutOrStdout(), "")

	// --- Section 2: Project registry status ---
	if len(args) > 0 {
		// Specific project requested.
		projectID := args[0]

		fmt.Fprintln(cmd.OutOrStdout(), "Project Registry")
		fmt.Fprintln(cmd.OutOrStdout(), "----------------")

		if registryStore.Exists(projectID) {
			displayProjectDetail(cmd, registryStore, projectID)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "  Project: %s\n", projectID)
			fmt.Fprintf(cmd.OutOrStdout(), "  Status:  unknown (not registered)\n")
		}
	} else {
		// No project ID — show all registered projects.
		fmt.Fprintln(cmd.OutOrStdout(), "Registered Projects")
		fmt.Fprintln(cmd.OutOrStdout(), "-------------------")

		projectIDs, err := registryStore.List()
		if err != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "  Error listing projects: %v\n", err)
		} else if len(projectIDs) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "  No projects registered.")
			fmt.Fprintln(cmd.OutOrStdout(), "")
			fmt.Fprintln(cmd.OutOrStdout(), "Register a project using 'anvil server project register'.")
		} else {
			for _, pid := range projectIDs {
				displayProjectSummary(cmd, registryStore, pid)
			}
		}
	}

	return nil
}

// displayProjectDetail prints detailed information for a single project.
func displayProjectDetail(cmd *cobra.Command, registryStore *server.RegistryStore, projectID string) {
	reg, err := registryStore.Load(projectID)
	if err != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "  Project: %s (load error: %v)\n", projectID, err)
		return
	}

	fmt.Fprintf(cmd.OutOrStdout(), "  Project: %s\n", reg.Project.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Status:  registered\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  Root:    %s\n", reg.Project.InstallRoot)
	if reg.Project.DisplayName != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  Name:    %s\n", reg.Project.DisplayName)
	}
	if reg.Project.Adapter != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  Adapter: %s\n", reg.Project.Adapter)
	}
	if reg.Project.Owner != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  Owner:   %s\n", reg.Project.Owner)
	}
	if reg.Project.Group != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  Group:   %s\n", reg.Project.Group)
	}
}

// displayProjectSummary prints a one-line summary for a registered project.
func displayProjectSummary(cmd *cobra.Command, registryStore *server.RegistryStore, projectID string) {
	reg, err := registryStore.Load(projectID)
	if err != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "  %s (error: %v)\n", projectID, err)
		return
	}

	display := reg.Project.DisplayName
	if display == "" {
		display = reg.Project.ID
	}
	fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", display)
	fmt.Fprintf(cmd.OutOrStdout(), "    ID:    %s\n", reg.Project.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "    Root:  %s\n", reg.Project.InstallRoot)
}

// displayOrDefault returns the value if non-empty, otherwise returns the
// fallback string.
func displayOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
