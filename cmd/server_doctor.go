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
// Scope note (ADR-036, TS-015-05-02): the system-health breadth is
// demoted. This command is an OPTIONAL, non-governing diagnostic: it
// observes and reports component state, but its health assessment never
// gates lifecycle operations — the command exits 0 whenever it runs
// successfully, no matter the health status. Lifecycle observability
// (what is active, release status, state queries) lives on "anvil server
// status" and is unaffected by this demotion.
//
// Command surface decision (ST-P9-01, ADR-010 §6.8):
//   - "anvil server doctor" is a Server Runtime diagnostic command, as
//     approved by PLAN-EPIC-009-AM-002 (server doctor) and referenced by
//     ST-009-001 ("a Server Runtime diagnostic command such as anvil
//     server doctor").
//   - It is distinct from "anvil system health" (ST-P9-05), which was a
//     readiness-oriented check (ready/not ready) and skipped configuration
//     inspection; that command was removed with the platform-ops demotion
//     (ADR-036 §3, TS-015-05-02). Doctor performs the full verification
//     including the ConfigInspector and reports the three-state health
//     assessment.
//   - Component mapping (ST-009-001 §4): project registration →
//     server_readiness (project_registries, install_roots) + registry;
//     configuration validity → config; artifact verification status →
//     release artifact_presence (consumed from the Artifact contract,
//     never re-verified); release lifecycle → release; runtime condition
//     → runtime; adapters are post-MVP and are not inspected.
//
// Exit codes:
//
//	0 - Command completed (informational output; health does not gate)
//
// Reference: ST-P9-01, ST-009-001, ADR-010 §6.8, PLAN-EPIC-009-AM-002,
// ADR-036
package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/inspection"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/output/diagnostic"
	"maleolabs.com/anvil/internal/runtime"
)

// serverDoctorCmd represents the "anvil server doctor" command that
// reports the platform health assessment.
//
// Reference: ST-P9-01
var serverDoctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Report platform health state (healthy, degraded, unhealthy)",
	Long: `Report the overall health state of the Anvil Server Runtime platform.

This command runs the full system verification engine across all platform
components — runtime, configuration, release, server readiness, and
registry — and reports a consolidated health assessment:

  healthy    - all checks pass, the platform is fully operational
  degraded   - some non-critical checks fail, the platform can still
               operate with limitations
  unhealthy  - critical checks fail, operations cannot proceed

Each component is reported individually with pass or fail status, and
failed checks include details for resolution.

This is optional, non-governing diagnostics (ADR-036): the assessment
observes component state and never gates lifecycle operations. The
command is read-only and does not modify any platform state or
configuration.

Artifact verification status is consumed from the Artifact contract
(EPIC-003) — this command never re-verifies artifacts.

Exit codes:
  0 - Command completed (informational output; health does not gate)

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
// standard DiagnosticView formatters.
//
// The command is demoted diagnostics (ADR-036 §3): it reports state and
// never gates — the exit code is 0 whenever the command runs
// successfully, regardless of the health status.
//
// When project configuration exists but cannot be loaded (invalid values,
// unreadable file), the config component is reported as failed and the
// health status is recomputed — the engine would otherwise skip config
// inspection entirely and could misreport the platform as healthy.
//
// Reference: ST-P9-01
func runServerDoctor(cmd *cobra.Command, args []string) error {
	// Check for --json flag first.
	jsonOutput, _ := cmd.Flags().GetBool("json")

	// Create step reporter for human-readable mode only.
	var reporter output.StepReporter
	if !jsonOutput {
		reporter = output.NewStepReporter(cmd.OutOrStdout())
		reporter.Start("Platform Health Assessment")
	}
	overallStart := time.Now()

	// Resolve server root.
	serverRoot := resolveServerRoot(cmd)

	// Create runtime config from server root.
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = serverRoot

	// Create all inspectors — the full component set, including the
	// ConfigInspector (unlike the removed "anvil system health" which
	// passed nil).
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

	// Attach reporter to engine for progress feedback.
	if reporter != nil {
		engine.WithReporter(&inspectionReporterAdapter{reporter: reporter})
	}

	result := engine.Verify(resolverArg)

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

	// Complete the reporter. The assessment is informational — findings
	// are reported, never gated.
	if reporter != nil {
		reporter.Complete("Health Assessment Complete", time.Since(overallStart))
	}

	// Build the presentation view: three-state health + per-component
	// results + guidance issues derived from the failed checks.
	view := diagnostic.NewVerificationView(result)
	view.Issues = inspection.IssuesFromComponents(result.ComponentResults)

	if jsonOutput {
		if err := diagnostic.WriteDiagnosticJSON(cmd.OutOrStdout(), view); err != nil {
			return fmt.Errorf("failed to write JSON output: %w", err)
		}
	} else {
		diagnostic.FormatDiagnosticView(cmd.OutOrStdout(), view)
	}

	// Demoted diagnostics never gate: exit 0 whenever the command ran
	// successfully (ADR-036 §3, TS-015-05-02).
	return nil
}

// inspectionReporterAdapter bridges output.StepReporter to inspection.InspectionReporter.
type inspectionReporterAdapter struct {
	reporter output.StepReporter
}

func (a *inspectionReporterAdapter) StepStart(name string) {
	a.reporter.StepStart(name)
}

func (a *inspectionReporterAdapter) StepComplete(name string, duration time.Duration) {
	a.reporter.StepComplete(name, duration)
}

func (a *inspectionReporterAdapter) StepFailed(name string, duration time.Duration, err error) {
	a.reporter.StepFailed(name, duration, err)
}
