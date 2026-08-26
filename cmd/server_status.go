// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P5-09, ADR-013, EPIC-005
package cmd

import (
	"fmt"
	"maleolabs.com/anvil/internal/output"
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
	s := styleFor(cmd)
	rootPath := resolveServerRoot(cmd)

	if rootPath != server.DefaultConfigRoot {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: using non-default server root %q (non-production override)\n", rootPath)
	}

	configStore := server.NewConfigStore(rootPath)
	registryStore := server.NewRegistryStore(rootPath)

	// --- Section 1: Runtime initialization status — Header modern ---
	h := output.NewHeader(s, "Server Runtime")
	if configStore.Exists() {
		cfg, err := configStore.Load()
		if err != nil {
			h.Add("Runtime", s.Error("initialized (load error: "+err.Error()+")"))
			h.Add("Config", configStore.ConfigPath())
		} else {
			h.Add("Runtime", s.Success("initialized"))
			h.Add("Config", configStore.ConfigPath())
			h.Add("ID", displayOrDefault(cfg.Runtime.ID, "(empty)"))
			if cfg.Runtime.DisplayName != "" {
				h.Add("Name", cfg.Runtime.DisplayName)
			}
		}
	} else {
		h.Add("Runtime", s.Error("not initialized"))
		h.Add("Config", configStore.ConfigPath()+" (not found)")
	}
	h.Render()
	if !configStore.Exists() {
		fmt.Fprintln(s.W, s.Dim("  Run 'anvil server init' to initialize the Server Runtime."))
	}

	fmt.Fprintln(s.W, "")

	// --- Section 2: Project registry status ---
	if len(args) > 0 {
		projectID := args[0]
		h2 := output.NewHeader(s, "Project Registry")
		h2.Add("Project", projectID)
		if registryStore.Exists(projectID) {
			h2.Add("Status", s.Success("registered"))
		} else {
			h2.Add("Status", s.Error("unknown (not registered)"))
		}
		h2.Render()
		if registryStore.Exists(projectID) {
			displayProjectDetail(cmd, registryStore, projectID)
		}
	} else {
		h2 := output.NewHeader(s, "Registered Projects")
		projectIDs, err := registryStore.List()
		if err != nil {
			h2.Add("Error", err.Error())
			h2.Render()
		} else if len(projectIDs) == 0 {
			h2.Add("Count", "0")
			h2.Render()
			fmt.Fprintln(s.W, s.Dim("  No projects registered."))
			fmt.Fprintln(s.W, s.Dim("  Register a project using 'anvil server project register'."))
		} else {
			h2.Add("Count", fmt.Sprintf("%d", len(projectIDs)))
			h2.Render()
			tbl := output.NewStyledTable(s, "Project", "Status", "Root")
			for _, pid := range projectIDs {
				reg, _ := registryStore.Load(pid)
				root := ""
				std := ""
				if reg != nil {
					root = reg.Project.InstallRoot
					std = reg.Project.StandardName()
				}
				tbl.AddRow([]string{pid, "registered", root}, []func(string) string{s.Info, s.Success, s.Dim})
				if std != "" {
					tbl.AddRow([]string{"", "Standard: "+std, ""}, []func(string) string{nil, s.Dim, nil})
				}
			}
			tbl.Render()
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
	s := styleFor(cmd)
	reg, err := registryStore.Load(projectID)
	if err != nil {
		fmt.Fprintf(s.W, "  %s (load error: %v)\n", s.Error("Project: "+projectID), err)
		return
	}
	h := output.NewHeader(s, "Project Detail")
	h.Add("Project", reg.Project.ID)
	h.Add("Status", s.Success("registered"))
	h.Add("Root", reg.Project.InstallRoot)
	if reg.Project.DisplayName != "" {
		h.Add("Name", reg.Project.DisplayName)
	}
	reg.Project.WarnIfLegacyAdapter(cmd.ErrOrStderr())
	if std := reg.Project.StandardName(); std != "" {
		h.Add("Standard", std)
	}
	h.Render()
	displayProjectLifecycle(cmd, registryStore.RootPath(), projectID)
}

// displayProjectLifecycle renders the lifecycle observability section for a
// project: the Active Release, installed Releases with lifecycle stages,
// rollback eligibility, and the persisted Runtime state.
//
// The section is read-only — it observes the authoritative lifecycle state
// and never mutates it (TS-015-05-01, ADR-036 §3).
func displayProjectLifecycle(cmd *cobra.Command, serverRoot, projectID string) {
	s := styleFor(cmd)

	status, err := server.QueryLifecycleStatus(serverRoot, projectID)
	if err != nil {
		fmt.Fprintf(s.W, "  %s (error: %v)\n", s.Error("Lifecycle"), err)
		return
	}

	h := output.NewHeader(s, "Lifecycle")
	if status.Active != nil {
		h.Add("Active Release", fmt.Sprintf("%s (v%s, %s)", status.Active.ReleaseID, status.Active.Version, status.Active.Stage))
	} else {
		h.Add("Active Release", s.Dim("none"))
	}
	h.Add("Installed", fmt.Sprintf("%d release(s)", len(status.Installed)))
	if status.Rollback.Eligible {
		h.Add("Rollback", s.Success("eligible (restore "+status.Rollback.TargetReleaseID+")"))
	} else {
		h.Add("Rollback", s.Dim("not eligible"+displayRollbackReason(status.Rollback.Reason)))
	}
	h.Render()

	if len(status.Installed) > 0 {
		tbl := output.NewStyledTable(s, "Release", "Version", "Stage")
		for _, rel := range status.Installed {
			stageCol := s.Dim
			if rel.Stage == "active" {
				stageCol = s.Success
			}
			tbl.AddRow([]string{rel.ReleaseID, rel.Version, rel.Stage}, []func(string) string{nil, nil, stageCol})
		}
		tbl.Render()
	}
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
	s := styleFor(cmd)
	if state.LoadError != "" {
		fmt.Fprintf(s.W, "  %s load error: %s\n", s.Error("Runtime State:"), state.LoadError)
		return
	}
	if !state.Recorded {
		fmt.Fprintln(s.W, s.Dim("  Runtime State: not recorded"))
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
	fmt.Fprintf(s.W, "  %s active release %s · condition %s · shared %s · updated %s\n",
		s.Dim("Runtime State:"), active, state.RuntimeCondition, state.SharedResource, updated)
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
