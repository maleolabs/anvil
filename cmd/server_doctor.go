// Package cmd implements the Anvil CLI commands.
//
// ── Server Doctor (ST-P9-01) ───────────────────────────────────────────
//
// "anvil server doctor" is the platform health assessment command: it runs
// the full system verification engine (runtime, config, release, server
// readiness, registry) and reports a consolidated three-state health
// assessment — healthy, degraded, or unhealthy — with per-component
// details and guidance for failed checks.
//
// Command surface decision (ST-P9-01, ADR-010 §6.8):
//   - "anvil server doctor" is a Server Runtime diagnostic command, as
//     approved by PLAN-EPIC-009-AM-002 (server doctor) and referenced by
//     ST-009-001 ("a Server Runtime diagnostic command such as anvil
//     server doctor").
//   - It is distinct from "anvil system health" (ST-P9-05), which is a
//     readiness-oriented check (ready/not ready) and skips configuration
//     inspection. Doctor performs the full verification including the
//     ConfigInspector and reports the three-state health assessment.
//   - Component mapping (ST-009-001 §4): project registration →
//     server_readiness (project_registries, install_roots) + registry;
//     configuration validity → config; artifact verification status →
//     release artifact_presence (consumed from the Artifact contract,
//     never re-verified); release lifecycle → release; runtime condition
//     → runtime; adapters are post-MVP and are not inspected.
//
// Exit codes:
//
//	0 - Platform is healthy
//	1 - Platform is degraded or unhealthy
//
// Reference: ST-P9-01, ST-009-001, ADR-010 §6.8, PLAN-EPIC-009-AM-002
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/inspection"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/output/diagnostic"
	"maleolabs.com/anvil/internal/runtime"
)

// serverDoctorCmd represents the "anvil server doctor" command that
// performs a full platform health assessment.
//
// Reference: ST-P9-01
var serverDoctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Assess platform health (healthy, degraded, unhealthy)",
	Long: `Assess the overall health of the Anvil Server Runtime platform.

This command runs the full system verification engine across all platform
components — runtime, configuration, release, server readiness, and
registry — and reports a consolidated health assessment:

  healthy    - all checks pass, the platform is fully operational
  degraded   - some non-critical checks fail, the platform can still
               operate with limitations
  unhealthy  - critical checks fail, operations cannot proceed

Each component is reported individually with pass or fail status, and
failed checks include details and guidance for resolution.

Artifact verification status is consumed from the Artifact contract
(EPIC-003) — this command never re-verifies artifacts. The assessment is
read-only and does not modify any platform state or configuration.

Exit codes:
  0 - Platform is healthy
  1 - Platform is degraded or unhealthy

Examples:
  anvil server doctor
  anvil server doctor --server-root /etc/anvil
  anvil server doctor --json`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runServerDoctor,
}

func init() {
	serverCmd.AddCommand(serverDoctorCmd)

	serverDoctorCmd.Flags().String(
		"server-root",
		"",
		"override the server root directory (default: ANVIL_SERVER_ROOT or /etc/anvil)",
	)

	serverDoctorCmd.Flags().Bool(
		"json",
		false,
		"output result as JSON",
	)
}

// runServerDoctor executes the platform health assessment.
//
// It resolves the server root, creates all inspectors, runs the full
// verification engine (including the ConfigInspector), derives guidance
// issues from the failed checks, and formats the result through the
// standard DiagnosticView formatters. Exit code is 0 when healthy, 1
// when degraded or unhealthy.
//
// When project configuration exists but cannot be loaded (invalid values,
// unreadable file), the config component is reported as failed and the
// health status is recomputed — the engine would otherwise skip config
// inspection entirely and could misreport the platform as healthy.
//
// Reference: ST-P9-01
func runServerDoctor(cmd *cobra.Command, args []string) error {
	// Resolve server root.
	serverRoot := resolveServerRoot(cmd)

	// Create runtime config from server root.
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = serverRoot

	// Create all inspectors — the full component set, including the
	// ConfigInspector (unlike "anvil system health" which passes nil).
	runtimeInspector := inspection.NewRuntimeInspector(cfg)
	configInspector := inspection.NewDefaultConfigInspector()
	releaseInspector := inspection.NewReleaseInspector(cfg)
	serverReadinessInspector := inspection.NewServerReadinessInspector(serverRoot)
	registryInspector := inspection.NewRegistryInspector(serverRoot)

	// Load the project configuration resolver for config inspection.
	resolver, configErr := loadConfigResolver()

	// The engine treats a nil resolver as "config inspection skipped". A
	// typed nil *config.Resolver must not reach the engine (it would be
	// treated as present), so only pass the resolver when the load
	// succeeded.
	var resolverArg interface{}
	if configErr == nil {
		resolverArg = resolver
	}

	// Run the full verification engine.
	engine := inspection.NewVerificationEngine(
		runtimeInspector,
		configInspector,
		releaseInspector,
		serverReadinessInspector,
		registryInspector,
	)
	result := engine.Verify(serverRoot, resolverArg)

	// Surface a configuration load failure as a failing config component
	// (only when configuration sources actually exist; a machine without
	// any project configuration is not unhealthy).
	if configErr != nil && hasConfigSources() {
		applyVerificationConfigLoadFailure(&result, configErr)
	}

	// Report the config component as not available when no project
	// configuration exists, consistent with "anvil system inspect config".
	// The availability check passes vacuously (like the other inspectors'
	// "not applicable" checks) so the platform can still be healthy
	// without a project — the story precondition states a project may or
	// may not exist.
	if configErr != nil && !hasConfigSources() {
		comp := inspection.NewInspectionResult("config")
		comp.AddCheck("config_availability", true,
			"config component not available: no project configuration found (anvil.yaml or ANVIL_CFG_* environment variables)")
		result.ComponentResults = append(result.ComponentResults, *comp)
		result.Status = inspection.ComputeHealthStatus(result.ComponentResults)
		result.Summary = inspection.BuildSummary(result.ComponentResults, result.Status)
	}

	// Build the presentation view: three-state health + per-component
	// results + guidance issues derived from the failed checks.
	view := diagnostic.NewVerificationView(result)
	view.Issues = inspection.IssuesFromComponents(result.ComponentResults)

	// Check for --json flag.
	jsonOutput, _ := cmd.Flags().GetBool("json")

	if jsonOutput {
		if err := diagnostic.WriteDiagnosticJSON(cmd.OutOrStdout(), view); err != nil {
			return fmt.Errorf("failed to write JSON output: %w", err)
		}
	} else {
		diagnostic.FormatDiagnosticView(cmd.OutOrStdout(), view)
	}

	// Exit code: 0 healthy, 1 degraded/unhealthy (consistent with the
	// "anvil system health" convention: 0 ready / 1 not ready).
	if result.Status != inspection.HealthStatusHealthy {
		appErr := &output.AppError{
			Message: fmt.Sprintf("platform health is %s", result.Status),
			Reason:  result.Summary,
			Resolution: "Resolve the failed checks listed above. The issues section provides details; " +
				"run 'anvil system diagnose' for recommendations referencing the owning Epic.",
		}
		output.WriteAppError(cmd.ErrOrStderr(), appErr)
		return appErr
	}
	return nil
}
