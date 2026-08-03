// Package cmd implements the Anvil CLI commands.
//
// ── Context-Aware Diagnostic Report (ST-P9-06) ─────────────────────────
//
// "anvil system diagnose" runs the cross-component diagnostic engine and
// produces a context-aware diagnostic report: every finding is classified
// into the architectural context that owns the failure — Development/CI
// configuration, Artifact, Release, Deployment, or Server Runtime —
// following the four-domain architecture (ADR-015).
//
// Command surface decision (ST-P9-06):
//   - "anvil system diagnose" lives under the system group because it is
//     a cross-component, cross-domain report: it inspects Development
//     configuration (config component), Artifact/Release state (release
//     component), and Server Runtime state (runtime, server components).
//     "anvil server doctor" (ST-P9-01) remains the Server Runtime-scoped
//     health assessment; "anvil system diagnose" provides the wider
//     architectural view with owner and next action per finding.
//   - Classification is evidence-based (ADR-015, EPIC-009 §8.3): a
//     failure is never attributed to repository source (Development) or
//     transport (Deployment) without evidence recorded in the issue.
//     Artifact identity findings (artifact_presence) are kept distinct
//     from Release identity findings. Adapter-related data remains
//     generic in MVP because no adapter component is inspected.
//   - Each finding carries an owner (the Epic that owns the resolution)
//     and a next action through the RecommendationEngine (TS-009-004),
//     rendered in the Recommendations section.
//
// Exit codes:
//
//	0 - No issues detected
//	1 - Issues detected
//
// Reference: ST-P9-06, ST-009-006, ADR-015, EPIC-009 §8.3
package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/config"
	"maleolabs.com/anvil/internal/inspection"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/output/diagnostic"
	"maleolabs.com/anvil/internal/runtime"
)

// systemDiagnoseCmd represents the "anvil system diagnose" command that
// produces a context-aware diagnostic report.
//
// Reference: ST-P9-06
var systemDiagnoseCmd = &cobra.Command{
	Use:   "diagnose",
	Short: "Produce a context-aware diagnostic report",
	Long: `Diagnose the platform and produce a context-aware report.

This command runs the diagnostic engine across all platform components
and reports every detected issue with its severity, location, likely
cause, and the architectural context that owns the failure:

  development     Development/CI configuration (project config)
  artifact        Artifact metadata and integrity
  release         Release lifecycle state
  deployment      Deployment orchestration (reserved for EPIC-010)
  server_runtime  Server Runtime state

Every finding includes an owner (the Epic that owns the resolution) and
a next action via the recommendations section. Classification is
evidence-based: Runtime failures are never attributed to repository
source without evidence, and Artifact identity is kept distinct from
Release identity.

This command is read-only and does not modify any platform state.

Exit codes:
  0 - No issues detected
  1 - Issues detected

Examples:
  anvil system diagnose
  anvil system diagnose --server-root /etc/anvil
  anvil system diagnose --json`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runSystemDiagnose,
}

func init() {
	systemCmd.AddCommand(systemDiagnoseCmd)

	systemDiagnoseCmd.Flags().String(
		"server-root",
		"",
		"override the server root directory (default: ANVIL_SERVER_ROOT or /etc/anvil)",
	)

	systemDiagnoseCmd.Flags().Bool(
		"json",
		false,
		"output result as JSON",
	)
}

// runSystemDiagnose executes the context-aware diagnostic report.
//
// It resolves the server root, creates all inspectors, runs the
// DiagnosticEngine, and formats the result through the standard
// DiagnosticView formatters. The view carries the architectural context
// classification of every issue (ST-P9-06) plus owner and next action via
// the recommendations. Exit code is 0 when no issues were detected, 1
// otherwise.
//
// When project configuration exists but cannot be loaded, the failure is
// appended as a config issue (classified as Development context) so the
// report does not silently skip a broken configuration.
//
// Reference: ST-P9-06
func runSystemDiagnose(cmd *cobra.Command, args []string) error {
	// Check for --json flag first.
	jsonOutput, _ := cmd.Flags().GetBool("json")

	// Create step reporter for human-readable mode only.
	var reporter output.StepReporter
	if !jsonOutput {
		reporter = output.NewStepReporter(cmd.OutOrStdout())
		reporter.Start("System Diagnosis")
	}
	overallStart := time.Now()

	// Resolve server root.
	serverRoot := resolveServerRoot(cmd)

	// Create runtime config from server root.
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = serverRoot

	// Create all inspectors.
	runtimeInspector := inspection.NewRuntimeInspector(cfg)
	configInspector := inspection.NewDefaultConfigInspector()
	releaseInspector := inspection.NewReleaseInspector(cfg)
	registryInspector := inspection.NewRegistryInspector(serverRoot)
	serverReadinessInspector := inspection.NewServerReadinessInspector(serverRoot)

	// Load the project configuration resolver for config inspection.
	resolver, configErr := loadConfigResolver()

	// The engine treats a nil resolver as "config inspection skipped". A
	// typed nil *config.Resolver must not reach the engine (it would be
	// treated as present), so only pass the resolver when the load
	// succeeded.
	var resolverArg *config.Resolver
	if configErr == nil {
		resolverArg = resolver
	}

	// Run the diagnostic engine (recommendations are produced
	// automatically for every issue).
	engine := inspection.NewDiagnosticEngine(
		runtimeInspector,
		configInspector,
		releaseInspector,
		registryInspector,
		serverReadinessInspector,
	)

	// Attach reporter to engine for progress feedback.
	if reporter != nil {
		engine.WithReporter(&inspectionReporterAdapter{reporter: reporter})
	}

	result := engine.Diagnose(serverRoot, resolverArg)

	// Surface a configuration load failure as a Development-context issue
	// (only when configuration sources actually exist).
	if configErr != nil && hasConfigSources() {
		applyDiagnosticConfigLoadFailure(&result, configErr)
	}

	// Complete the reporter.
	if reporter != nil {
		if result.Passed {
			reporter.Complete("No Issues Detected", time.Since(overallStart))
		} else {
			reporter.Failed("Issues Detected", time.Since(overallStart))
		}
	}

	// Build the presentation view (issues + contexts + recommendations).
	view := diagnostic.NewDiagnosticView(result)

	if jsonOutput {
		if err := diagnostic.WriteDiagnosticJSON(cmd.OutOrStdout(), view); err != nil {
			return fmt.Errorf("failed to write JSON output: %w", err)
		}
	} else {
		diagnostic.FormatDiagnosticView(cmd.OutOrStdout(), view)
	}

	// Exit code: 0 no issues, 1 issues detected (consistent with the
	// diagnostic command family).
	if !result.Passed {
		appErr := &output.AppError{
			Message:    "diagnostic issues found",
			Reason:     result.Summary,
			Resolution: "Resolve the issues listed above; each recommendation references the Epic that owns the resolution.",
		}
		output.WriteAppError(cmd.ErrOrStderr(), appErr)
		return appErr
	}
	return nil
}
