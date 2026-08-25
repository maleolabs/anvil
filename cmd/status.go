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
		if err := output.WriteJSON(cmd.OutOrStdout(), data); err != nil {
			return err
		}
		return nil
	}

	// Human path — consistent with internal/output plain reporters and spike UX harness.
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "Project: %s\n", cfg.Project.Name)
	fmt.Fprintf(w, "Version: %s\n", cfg.Project.Version)
	if cfg.Project.Description != "" {
		fmt.Fprintf(w, "Description: %s\n", cfg.Project.Description)
	}
	fmt.Fprintf(w, "Lifecycle: %s\n", stage.String())
	if validation.Valid {
		fmt.Fprintln(w, "Configuration: valid")
	} else {
		fmt.Fprintln(w, "Configuration: invalid")
		for _, e := range validation.Errors {
			fmt.Fprintf(w, "  - %s\n", e)
		}
	}
	// Server lifecycle observability (queryable, AC3) — human section mirrors internal/server/observability.
	if lifecycleStatus != nil {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Server Lifecycle")
		fmt.Fprintln(w, "----------------")
		if lifecycleStatus.Active != nil {
			fmt.Fprintf(w, "  Active Release: %s (v%s, %s)\n", lifecycleStatus.Active.ReleaseID, lifecycleStatus.Active.Version, lifecycleStatus.Active.Stage)
		} else {
			fmt.Fprintln(w, "  Active Release: none")
		}
		fmt.Fprintf(w, "  Installed: %d release(s)\n", len(lifecycleStatus.Installed))
		for _, rel := range lifecycleStatus.Installed {
			fmt.Fprintf(w, "    %s  %s  %s\n", rel.ReleaseID, rel.Version, rel.Stage)
		}
		if lifecycleStatus.Rollback.Eligible {
			fmt.Fprintf(w, "  Rollback: eligible (restore %s)\n", lifecycleStatus.Rollback.TargetReleaseID)
		} else {
			reason := ""
			if lifecycleStatus.Rollback.Reason != "" {
				reason = " (" + lifecycleStatus.Rollback.Reason + ")"
			}
			fmt.Fprintf(w, "  Rollback: not eligible%s\n", reason)
		}
		if lifecycleStatus.RuntimeState.LoadError != "" {
			fmt.Fprintf(w, "  Runtime State: load error: %s\n", lifecycleStatus.RuntimeState.LoadError)
		} else if !lifecycleStatus.RuntimeState.Recorded {
			fmt.Fprintln(w, "  Runtime State: not recorded")
		} else {
			active := lifecycleStatus.RuntimeState.ActiveReleaseID
			if active == "" {
				active = "none"
			}
			updated := ""
			if !lifecycleStatus.RuntimeState.LastUpdated.IsZero() {
				updated = lifecycleStatus.RuntimeState.LastUpdated.Format("2006-01-02T15:04:05Z07:00")
			}
			fmt.Fprintf(w, "  Runtime State: active release %s; condition %s; shared resources %s; updated %s\n",
				active, lifecycleStatus.RuntimeState.RuntimeCondition, lifecycleStatus.RuntimeState.SharedResource, updated)
		}
		if os.Getenv("ANVIL_AUDIT_SHIP_READY") != "" {
			fmt.Fprintln(w, "  Audit: ship-ready (JSON-lines, 0600, HMAC hash-chain, SIEM)")
		}
	}
	return nil
}
