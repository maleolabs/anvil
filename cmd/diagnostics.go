// Package cmd implements the Anvil CLI commands.
//
// ── Shared Diagnostics Helpers (ST-P9-01, ST-P9-02, ST-P9-04, ST-P9-06) ─
//
// The EPIC-009 diagnostic commands (server doctor, server readiness,
// system inspect, system diagnose) share a small set of helpers:
//
//   - loadConfigResolver: builds a *config.Resolver from the loaded
//     configuration so the ConfigInspector can run. ProvisionConfig does
//     not expose its internal resolver, so the resolver is reconstructed
//     from the level maps (identical maps, identical precedence).
//   - hasConfigSources: distinguishes "no project configuration exists"
//     (component not available) from "configuration exists but is broken"
//     (component must fail).
//   - apply*ConfigLoadFailure: surfaces a configuration load failure as a
//     failing config component/issue in the engine result, because the
//     engines skip config inspection when no resolver can be built.
//
// Reference: ST-P9-01, ST-P9-02, ST-P9-04, ST-P9-06, ADR-005 §10.2
package cmd

import (
	"fmt"
	"os"
	"strings"

	"maleolabs.com/anvil/internal/config"
	"maleolabs.com/anvil/internal/inspection"
)

// loadConfigResolver loads the project configuration and returns a
// *config.Resolver suitable for the inspection engines.
//
// ProvisionConfig intentionally does not expose its internal resolver
// (immutability boundary), so the resolver is reconstructed from the
// per-level maps exposed by ProvisionConfig.LevelMap. The reconstruction
// uses the exact same level maps and the same deterministic precedence
// (Execution > Environment > Project > Global), so resolution results are
// identical to the original resolver.
//
// It returns an error when the configuration cannot be loaded or fails
// validation (e.g. no project configuration or invalid values).
//
// Reference: ADR-005 §7.5/§10.2
func loadConfigResolver() (*config.Resolver, error) {
	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, err
	}

	return config.NewResolver(
		cfg.LevelMap(config.ScopeGlobal),
		cfg.LevelMap(config.ScopeProject),
		cfg.LevelMap(config.ScopeEnvironment),
		cfg.LevelMap(config.ScopeExecution),
	), nil
}

// hasConfigSources reports whether any project or global configuration
// source exists: discovered configuration files (anvil.yaml/anvil.yml in
// the project root or global config directory) or ANVIL_CFG_ environment
// variables.
//
// This distinguishes "no project configured" (component not available,
// reported as a vacuous pass like the other inspectors) from "project
// configuration exists but is broken" (component must be reported as
// failed).
//
// Reference: ADR-005 §7.2/§10.2
func hasConfigSources() bool {
	if len(config.DiscoverConfigFiles()) > 0 {
		return true
	}

	for _, env := range os.Environ() {
		if strings.HasPrefix(env, config.EnvPrefix) {
			return true
		}
	}

	return false
}

// configLoadDetails renders a configuration load error as a single-line
// description. Configuration validation errors can span multiple lines;
// flattening keeps the message readable in single-line check details and
// readiness blockers.
func configLoadDetails(loadErr error) string {
	return fmt.Sprintf("cannot load configuration: %s", strings.ReplaceAll(loadErr.Error(), "\n", "; "))
}

// failingConfigComponent builds a failing "config" component result for a
// configuration load failure. The check name is "config_load" and the
// details carry the loader error, matching the config component's
// check-naming convention.
func failingConfigComponent(loadErr error) inspection.InspectionResult {
	result := inspection.NewInspectionResult("config")
	result.AddCheck("config_load", false, configLoadDetails(loadErr))
	return *result
}

// configLoadIssue builds the diagnostic issue record for a configuration
// load failure. The severity follows the deterministic keyword heuristic:
// "cannot load" is a major marker (existing state is broken or invalid).
func configLoadIssue(loadErr error) inspection.DiagnosticIssue {
	return inspection.DiagnosticIssue{
		Component:   "config",
		Severity:    inspection.SeverityMajor,
		Description: configLoadDetails(loadErr),
		Location:    "config_load",
		LikelyCause: "Configuration could not be loaded from the project or global configuration sources; the project configuration may be invalid, unreadable, or missing required values",
	}
}

// applyVerificationConfigLoadFailure appends the failing config component
// to a verification result and recomputes the three-state health status
// and summary. It is used when project configuration exists but cannot be
// loaded, so the VerificationEngine (which skips config inspection without
// a resolver) would otherwise report the platform healthy.
func applyVerificationConfigLoadFailure(result *inspection.SystemVerificationResult, loadErr error) {
	result.ComponentResults = append(result.ComponentResults, failingConfigComponent(loadErr))
	result.Status = inspection.ComputeHealthStatus(result.ComponentResults)
	result.Summary = inspection.BuildSummary(result.ComponentResults, result.Status)
}

// applyReadinessConfigLoadFailure appends the failing config component to
// a readiness result, marks the platform not ready, and adds the
// corresponding actionable blocker, consistent with the coordinator's
// blocker format ("[component] check: details").
func applyReadinessConfigLoadFailure(result *inspection.ReadinessCoordinatorResult, loadErr error) {
	component := failingConfigComponent(loadErr)
	result.Components = append(result.Components, component)
	result.Ready = false
	result.Blockers = append(result.Blockers, fmt.Sprintf("[config] config_load: %s", component.Checks[0].Details))
	result.Summary = fmt.Sprintf("System is not ready: %d blocker(s) found", len(result.Blockers))
}

// applyDiagnosticConfigLoadFailure appends the config load failure as a
// diagnostic issue and recomputes the summary and recommendations so the
// report stays internally consistent (one recommendation per issue).
func applyDiagnosticConfigLoadFailure(result *inspection.DiagnosticResult, loadErr error) {
	result.Issues = append(result.Issues, configLoadIssue(loadErr))
	result.Passed = false
	result.Summary = inspection.BuildDiagnosticSummary(result.Issues)
	result.Recommendations = inspection.NewRecommendationEngine().RecommendationsFor(result.Issues)
}

// applyReleaseEligibility merges the identity-based release eligibility
// component (artifact verification status + release stage) into a
// readiness result. When any eligibility check fails, the platform is not
// ready and an actionable blocker is added for each failed check, using
// the coordinator's blocker format ("[component] check: details").
func applyReleaseEligibility(result *inspection.ReadinessCoordinatorResult, component inspection.InspectionResult) {
	result.Components = append(result.Components, component)

	if component.Passed {
		return
	}

	result.Ready = false
	for _, check := range component.Checks {
		if !check.Passed {
			result.Blockers = append(result.Blockers,
				fmt.Sprintf("[%s] %s: %s", component.Component, check.Name, check.Details))
		}
	}
	result.Summary = fmt.Sprintf("System is not ready: %d blocker(s) found", len(result.Blockers))
}
