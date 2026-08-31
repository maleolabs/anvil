// Package cmd implements the Anvil CLI commands.
//
// ── Server Readiness (ST-P9-02) ────────────────────────────────────────
//
// "anvil server readiness" checks the prerequisites for Release activation
// — configuration validity, Artifact verification, Release stage
// eligibility, Runtime availability, and generic resource readiness —
// reporting each check individually with pass or fail status and
// actionable blockers.
//
// Scope note (ADR-036, TS-015-05-02): the readiness breadth is demoted.
// This command is an OPTIONAL, non-governing diagnostic: it observes and
// reports component state, but its output never gates lifecycle
// operations — the command exits 0 whenever it runs successfully, no
// matter how many blockers it finds. Lifecycle observability (what is
// active, release status, state queries) lives on "anvil server status"
// and is unaffected by this demotion.
//
// Command surface decision (ST-P9-02):
//   - "anvil server readiness" is the Server Runtime diagnostic equivalent
//     of the Deployment orchestration readiness check described by
//     ST-009-002 ("a Deployment orchestration readiness command or an
//     equivalent Server Runtime diagnostic"). The readiness coordinator
//     evaluates Server Runtime components, so the command lives under the
//     "anvil server" group.
//   - It is deliberately NOT named "anvil system readiness": "anvil system
//     health" (ST-P9-05) already exposed system-level readiness semantics
//     under the system group; that command was removed with the platform
//     ops demotion (ADR-036 §3, TS-015-05-02). "anvil server readiness"
//     expresses the pre-activation purpose (before "anvil server release
//     activate") without overlapping "anvil runtime readiness" (ST-P5-02),
//     which only checks the Runtime filesystem layout.
//
// Artifact verification status is consumed from the Artifact contract
// (EPIC-003) — this command never re-verifies artifacts. The check is
// read-only and does not modify any platform state or configuration.
//
// Exit codes:
//
//	0 - Command completed (informational output; findings do not gate)
//	1 - Input-resolution failure: the project registry, the Release, or
//	    the artifact registration index could not be resolved before the
//	    readiness engine ran (documented row, TS-019-03-02 D-08)
//
// Reference: ST-P9-02, ST-009-002, ADR-010 §6.8, ADR-036
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/inspection"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/output/diagnostic"
	"maleolabs.com/anvil/internal/project"
	"maleolabs.com/anvil/internal/release"
	"maleolabs.com/anvil/internal/runtime"
	"maleolabs.com/anvil/internal/server"
)

// serverReadinessCmd represents the "anvil server readiness" command that
// reports the pre-activation readiness state of the Server Runtime.
//
// Readiness is evaluated for an existing Runtime Release identified by
// project and release identity (ST-009-002 §2): the artifact referenced by
// the Release must be verified (EPIC-003 consumption) and the Release must
// be in the Ready stage (EPIC-004 consumption).
//
// Reference: ST-P9-02
var serverReadinessCmd = &cobra.Command{
	Use:   "readiness <project-id> <release-id>",
	Short: "Report pre-activation readiness state of the Server Runtime",
	Long: `Report the pre-activation readiness state of the Server Runtime.

Readiness is evaluated for an existing Runtime Release identified by
project and release identity. This command runs the readiness
coordinator across all prerequisite components — configuration validity,
Artifact verification, Release eligibility, Runtime availability, and
generic resources — and reports each check individually with pass or
fail status.

This is optional, non-governing diagnostics (ADR-036): the output
observes component state and never gates lifecycle operations. No
operation is triggered; this is a read-only assessment.

Artifact verification status is consumed from the Artifact contract
(EPIC-003) — this command never re-verifies artifacts.

Exit codes:
  0 - Command completed (informational output; findings do not gate)
  1 - Input-resolution failure: the project registry, the Release, or
      the artifact registration index could not be resolved before the
      readiness engine ran (documented row, TS-019-03-02 D-08)

Examples:
  anvil server readiness my-project abc123def456
  anvil server readiness my-project --server-root /etc/anvil
  anvil server readiness my-project abc123def456 --json`,
	Args:         ExactArgsWithUsage(2, "anvil server readiness <project-id> <release-id>"),
	SilenceUsage: true,
	RunE:         runServerReadiness,
}

func init() {
	serverCmd.AddCommand(serverReadinessCmd)

	serverReadinessCmd.Flags().String(
		"server-root",
		"",
		"override the server root directory (default: ANVIL_SERVER_ROOT or /etc/anvil)",
	)

	serverReadinessCmd.Flags().Bool(
		"json",
		false,
		"output result as JSON",
	)
}

// runServerReadiness executes the pre-activation readiness report.
//
// It resolves the server root, loads the project registry and the Runtime
// Release identified by project and release identity, runs the readiness
// coordinator across the platform components, and appends the identity-
// based release eligibility checks (artifact verification status from
// EPIC-003 and release stage eligibility from EPIC-004). The result is
// formatted through the standard DiagnosticView formatters.
//
// The command is demoted diagnostics (ADR-036 §3): it reports state and
// never gates — the exit code is 0 whenever the command runs
// successfully, regardless of the readiness findings.
//
// When project configuration exists but cannot be loaded, the failing
// config component is appended with its blocker as an informational
// finding.
//
// Reference: ST-P9-02
func runServerReadiness(cmd *cobra.Command, args []string) error {
	projectID := args[0]
	releaseID := args[1]

	// Check for --json flag first.
	jsonOutput, _ := cmd.Flags().GetBool("json")

	// Create step reporter for human-readable mode only.
	var reporter output.StepReporter
	if !jsonOutput {
		reporter = output.NewStepReporter(styleFor(cmd).W)
		reporter.Start("Server Readiness Assessment")
	}
	overallStart := time.Now()

	// Resolve server root.
	serverRoot := resolveServerRoot(cmd)

	// Load the project registry to resolve the install root (ADR-013).
	registryStore := server.NewRegistryStore(serverRoot)
	reg, err := registryStore.Load(projectID)
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not load project registry: %v", err)
	}
	installRoot := reg.Project.InstallRoot

	// Load the Runtime Release by identity (EPIC-004 state).
	rel, err := release.LookupByID(installRoot, release.ReleaseID(releaseID))
	if err != nil {
		return ReportPlainErrorf(cmd, err, "Release %q not found: %v", releaseID, err)
	}

	// Load the artifact registration store (EPIC-003 contract): only
	// artifacts that passed verification are registered, so registration
	// status IS the verification status. A missing index means no artifact
	// has been registered (treated as an empty store).
	regPath := filepath.Join(project.NewStructure(installRoot).StateDir, "registration-index.json")
	regStore := artifact.NewRegistrationStore(regPath)
	if _, statErr := os.Stat(regPath); statErr == nil {
		if err := regStore.Load(); err != nil {
			return ReportPlainErrorf(cmd, err, "could not load artifact registration index: %v", err)
		}
	}

	// Create runtime config from server root.
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = serverRoot

	// Create all inspectors — the full component set, including the
	// ConfigInspector (configuration validity is a readiness prerequisite).
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

	// Run the readiness coordinator.
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

	coordinator := inspection.NewReadinessCoordinator(engine)
	result := coordinator.CheckReadiness(resolverArg)

	// A configuration load failure is reported as an informational finding
	// (only when configuration sources actually exist; a machine without
	// any project configuration is not blocked on config).
	if configErr != nil && hasConfigSources() {
		applyReadinessConfigLoadFailure(&result, configErr)
	}

	// Report the config component as not available when no project
	// configuration exists, consistent with "anvil system inspect config".
	if configErr != nil && !hasConfigSources() {
		comp := inspection.NewInspectionResult("config")
		comp.AddCheck("config_availability", true,
			"config component not available: no project configuration found (anvil.yaml or ANVIL_CFG_* environment variables)")
		result.Components = append(result.Components, *comp)
	}

	// Append the identity-based release eligibility checks: artifact
	// verification status (EPIC-003 consumption) and release stage
	// eligibility (EPIC-004 consumption).
	applyReleaseEligibility(&result, inspection.BuildReleaseEligibilityComponent(rel, regStore.IsRegistered))

	// Complete the reporter. The assessment is informational — findings
	// are reported, never gated.
	if reporter != nil {
		reporter.Complete("Readiness Assessment Complete", time.Since(overallStart))
	}

	// Build the presentation view (components + blockers).
	view := diagnostic.NewReadinessView(result)

	if jsonOutput {
		if err := diagnostic.WriteDiagnosticJSON(styleFor(cmd).W, view); err != nil {
			return fmt.Errorf("failed to write JSON output: %w", err)
		}
	} else {
		diagnostic.FormatDiagnosticView(styleFor(cmd).W, view)
	}

	// Demoted diagnostics never gate: exit 0 whenever the command ran
	// successfully (ADR-036 §3, TS-015-05-02).
	return nil
}
