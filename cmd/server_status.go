// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P5-09, ADR-013, EPIC-005
package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/server"
)

// serverStatusCmd represents the "anvil server status" command that reports
// the Server Runtime status, including initialization state, registry status,
// and project registration overview.
//
// When called without a project ID, it displays the initialization state and
// an overview of all registered projects.
// When called with a project ID, it additionally shows detailed information
// for that specific project, including the lifecycle observability section:
// the Active Release, installed Releases with their lifecycle stages,
// rollback eligibility, and the persisted Runtime state.
//
// This is a read-only command — it does not modify any state.
//
// Reference: ST-P5-09, ADR-013, TS-015-05-01, ADR-036 §3
var serverStatusCmd = &cobra.Command{
	Use:   "status [<project-id>]",
	Short: "Display Server Runtime status and readiness",
	Long: `Display the Server Runtime status and project registry overview.

Without a project ID, shows:
  - Runtime initialization state
  - All registered projects overview

With a project ID, additionally shows:
  - Detailed project configuration
  - Lifecycle observability: the Active Release, installed
    Releases with their lifecycle stages, rollback eligibility, and the
    persisted Runtime state — read from the authoritative lifecycle state

This is a read-only command — it does not modify any state.`,
	Example: `  anvil server status
  anvil server status my-app
  anvil server status --server-root /tmp/anvil`,
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
//
// The detail view ends with the lifecycle observability section
// (TS-015-05-01, ADR-036 §3): what is active, what is installed, what can
// roll back, and the persisted Runtime state — all read from the
// authoritative lifecycle state via server.QueryLifecycleStatus.
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
	// The resolved standard (canonical project.standard; the legacy
	// project.adapter key is read as an alias during the deprecation
	// window — every read emits a deprecation warning naming
	// project.standard on stderr, so stdout stays machine-readable,
	// TS-019-02-02 / ADR-032).
	reg.Project.WarnIfLegacyAdapter(cmd.ErrOrStderr())
	if std := reg.Project.StandardName(); std != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  Standard: %s\n", std)
	}

	fmt.Fprintln(cmd.OutOrStdout(), "")
	displayProjectLifecycle(cmd, registryStore.RootPath(), projectID)
}

// displayProjectLifecycle renders the lifecycle observability section for a
// project: the Active Release, installed Releases with lifecycle stages,
// rollback eligibility, and the persisted Runtime state.
//
// The section is read-only — it observes the authoritative lifecycle state
// and never mutates it (TS-015-05-01, ADR-036 §3).
func displayProjectLifecycle(cmd *cobra.Command, serverRoot, projectID string) {
	fmt.Fprintln(cmd.OutOrStdout(), "  Lifecycle")
	fmt.Fprintln(cmd.OutOrStdout(), "  ---------")

	status, err := server.QueryLifecycleStatus(serverRoot, projectID)
	if err != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "    (error: %v)\n", err)
		return
	}

	// What is active.
	if status.Active != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "    Active Release:  %s (v%s, %s)\n",
			status.Active.ReleaseID, status.Active.Version, status.Active.Stage)
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "    Active Release:  none")
	}

	// What is installed + release status (stage per Release).
	fmt.Fprintf(cmd.OutOrStdout(), "    Installed:       %d release(s)\n", len(status.Installed))
	for _, rel := range status.Installed {
		fmt.Fprintf(cmd.OutOrStdout(), "      %s  %s  %s\n", rel.ReleaseID, rel.Version, rel.Stage)
	}

	// What can roll back.
	if status.Rollback.Eligible {
		fmt.Fprintf(cmd.OutOrStdout(), "    Rollback:        eligible (restore %s)\n",
			status.Rollback.TargetReleaseID)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "    Rollback:        not eligible%s\n",
			displayRollbackReason(status.Rollback.Reason))
	}

	// State queries: the persisted Runtime state.
	displayRuntimeState(cmd, status.RuntimeState)
}

// displayRollbackReason returns the rollback ineligibility reason formatted
// for the status line, or an empty string when no reason was reported.
func displayRollbackReason(reason string) string {
	if reason == "" {
		return ""
	}
	return " (" + reason + ")"
}

// displayRuntimeState renders the persisted Runtime state observation
// (runtime-state.json): recorded state fields, or not-recorded / load-error
// conditions.
func displayRuntimeState(cmd *cobra.Command, state server.RuntimeStateStatus) {
	if state.LoadError != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "    Runtime State:   load error: %s\n", state.LoadError)
		return
	}
	if !state.Recorded {
		fmt.Fprintln(cmd.OutOrStdout(), "    Runtime State:   not recorded")
		return
	}

	active := state.ActiveReleaseID
	if active == "" {
		active = "none"
	}
	updated := ""
	if !state.LastUpdated.IsZero() {
		updated = state.LastUpdated.Format(time.RFC3339)
	}
	fmt.Fprintf(cmd.OutOrStdout(),
		"    Runtime State:   active release %s; condition %s; shared resources %s; updated %s\n",
		active, state.RuntimeCondition, state.SharedResource, updated)
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
