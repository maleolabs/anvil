package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/project"
	"maleolabs.com/anvil/internal/server"
)

// statusCmd represents the "anvil status" command that displays information
// about the current Anvil project. It is a project-dependent command — it
// requires an existing Anvil project in the current or a parent directory.
//
// When invoked outside an Anvil project, it prints the missing-project
// guidance message and returns a non-zero exit code.
//
// With --json, it emits a machine-readable envelope v1 (version:1, status:success)
// queryable by automation and lifecycle observability (AC2, AC3). The JSON data
// includes project identity, lifecycle stage, configuration validity, and when
// the project is registered on the server, the full server.QueryLifecycleStatus
// snapshot (active, installed, rollback, runtime state) — the authoritative
// status_after_activate.json shape (ADR-036, TS-015-05-01).
//
// Reference: ST-P1-06, anvil-cli/sto:local-deploy-observability AC2+AC3, ADR-036, spike local-deploy-ux
var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Display the current Anvil project status",
	Long: `Show information about the Anvil project in the current directory.

If no Anvil project is found, this command prints a descriptive error
message listing the directories that were searched along with guidance
on how to create or find a project.

With --json, outputs a machine-readable envelope {"version":"1","status":"success","data":{...}}
queryable for lifecycle observability (active release, installed releases, rollback eligibility,
runtime state) — the status_after_activate.json shape via server.QueryLifecycleStatus.`,
	Example: `  anvil status
  anvil status --json
  anvil status --server-root /tmp/anvil --json`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
	AddJSONFlag(statusCmd)
	statusCmd.Flags().String("server-root", "", "Override config root path (non-production only; overrides ANVIL_SERVER_ROOT env var)")
}

func runStatus(cmd *cobra.Command, args []string) error {
	cfg, err := RequireProject(cmd)
	if err != nil {
		return err
	}
	asJSON, _ := cmd.Flags().GetBool("json")

	// Resolve project root and lifecycle stage (local project lifecycle).
	root, err := project.Discover()
	if err != nil {
		// No project root found — RequireProject already handled, but keep safe.
		root = ""
	}
	stage := project.StageActive
	if root != "" {
		if s, serr := project.LoadLifecycleState(root); serr == nil {
			stage = s
		}
	}
	validation := project.ValidateProject(cfg)

	// Try to query server lifecycle when project is registered (observability, AC3).
	// This is best-effort: a project may not be registered on the server, or server not initialized.
	var lifecycleStatus *server.LifecycleStatus
	var serverRoot string
	{
		serverRoot = resolveServerRoot(cmd)
		if pid := cfg.Identity().Name(); pid != "" {
			if st, qerr := server.QueryLifecycleStatus(serverRoot, pid); qerr == nil {
				lifecycleStatus = st
			} else {
				// Also try alternative project id from cfg.Project.Name (some registries use Name).
				if st2, qerr2 := server.QueryLifecycleStatus(serverRoot, cfg.Project.Name); qerr2 == nil {
					lifecycleStatus = st2
				}
			}
		}
	}

	if asJSON {
		s := styleFor(cmd)
		data := map[string]interface{}{
			"project": map[string]string{
				"name":        cfg.Project.Name,
				"version":     cfg.Project.Version,
				"description": cfg.Project.Description,
			},
			"lifecycle": map[string]string{
				"stage": stage.String(),
			},
			"configuration": map[string]interface{}{
				"valid":  validation.Valid,
				"errors": validation.Errors,
			},
			"server_root": serverRoot,
		}
		if lifecycleStatus != nil {
			data["server_lifecycle"] = lifecycleStatus
			// Also expose flattened status_after_activate shape for direct comparison with spike evidence.
			data["active"] = lifecycleStatus.Active
			data["installed"] = lifecycleStatus.Installed
			data["rollback"] = lifecycleStatus.Rollback
			data["runtime_state"] = lifecycleStatus.RuntimeState
		}
		if err := output.WriteJSON(s.Raw(), data); err != nil {
			return err
		}
		return nil
	}

	// Human path — modern professional UI via Header + StyledTable + Container
	s := styleFor(cmd)
	// Project header (Accent, aligned, Container via Header's leading blank)
	h := output.NewHeader(s, "Project")
	h.Add("Name", cfg.Project.Name)
	h.Add("Version", cfg.Project.Version)
	if cfg.Project.Description != "" {
		h.Add("Description", cfg.Project.Description)
	}
	h.Add("Lifecycle", stage.String())
	if serverRoot != "" {
		h.Add("Server Root", serverRoot)
	}
	h.Pipeline("Configuration")
	h.Render()
	// Configuration — Success/Error with Table for errors
	if validation.Valid {
		fmt.Fprintln(s.W, s.Success("  "+output.IconDone+" Configuration valid"))
	} else {
		fmt.Fprintln(s.W, s.Error("  Configuration invalid"))
		tbl := output.NewStyledTable(s, "Issue", "Details")
		for _, e := range validation.Errors {
			tbl.AddRow([]string{"•", e}, []func(string) string{nil, s.Dim})
		}
		tbl.Render()
	}
	// Server lifecycle — modern table + timeline
	if lifecycleStatus != nil {
		fmt.Fprintln(s.W, "")
		h2 := output.NewHeader(s, "Server Lifecycle")
		if lifecycleStatus.Active != nil {
			h2.Add("Active Release", fmt.Sprintf("%s (v%s, %s)", lifecycleStatus.Active.ReleaseID, lifecycleStatus.Active.Version, lifecycleStatus.Active.Stage))
		} else {
			h2.Add("Active Release", "none")
		}
		h2.Add("Installed", fmt.Sprintf("%d release(s)", len(lifecycleStatus.Installed)))
		h2.Render()
		if len(lifecycleStatus.Installed) > 0 {
			tbl := output.NewStyledTable(s, "Release", "Version", "Stage")
			for _, rel := range lifecycleStatus.Installed {
				col := s.Dim
				if rel.Stage == "active" {
					col = s.Success
				}
				tbl.AddRow([]string{rel.ReleaseID, rel.Version, rel.Stage}, []func(string) string{nil, nil, col})
			}
			tbl.Render()
		}
		// Rollback + Runtime State as secondary dim lines
		if lifecycleStatus.Rollback.Eligible {
			fmt.Fprintln(s.W, s.Success("  "+output.IconDone+" Rollback eligible (restore "+lifecycleStatus.Rollback.TargetReleaseID+")"))
		} else {
			reason := lifecycleStatus.Rollback.Reason
			if reason == "" {
				reason = "not eligible"
			}
			fmt.Fprintln(s.W, s.Dim("  Rollback: "+reason))
		}
		if lifecycleStatus.RuntimeState.LoadError != "" {
			fmt.Fprintln(s.W, s.Error("  Runtime State load error: "+lifecycleStatus.RuntimeState.LoadError))
		} else if !lifecycleStatus.RuntimeState.Recorded {
			fmt.Fprintln(s.W, s.Dim("  Runtime State: not recorded"))
		} else {
			active := lifecycleStatus.RuntimeState.ActiveReleaseID
			if active == "" {
				active = "none"
			}
			updated := ""
			if !lifecycleStatus.RuntimeState.LastUpdated.IsZero() {
				updated = lifecycleStatus.RuntimeState.LastUpdated.Format("2006-01-02T15:04:05Z07:00")
			}
			fmt.Fprintf(s.W, "  %s\n", s.Dim(fmt.Sprintf("Runtime State: active %s · %s · shared %s · updated %s", active, lifecycleStatus.RuntimeState.RuntimeCondition, lifecycleStatus.RuntimeState.SharedResource, updated)))
		}
		if os.Getenv("ANVIL_AUDIT_SHIP_READY") != "" {
			fmt.Fprintln(s.W, s.Dim("  Audit: ship-ready (JSON-lines, 0600, HMAC hash-chain, SIEM)"))
		}
	}
	return nil
}
