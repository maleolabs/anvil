// Package inspection provides read-only diagnostic inspection capabilities
// for Anvil Runtime environments and configuration.
//
// Reference: TS-009-003, TS-009-004, ADR-003 §8.5, ADR-006 §5.1/§5.2/§9
package inspection

import (
	"fmt"
	"strings"
	"time"

	"maleolabs.com/anvil/internal/config"
)

// Severity represents the impact level of a diagnostic issue.
// Issues are classified as critical, major, or minor based on
// the impact of the underlying failure.
//
// Reference: TS-009-003 §7, ADR-006 §5.2
type Severity string

const (
	// SeverityCritical indicates required state is missing or absent —
	// the component cannot operate until the state is restored.
	SeverityCritical Severity = "critical"

	// SeverityMajor indicates existing state is broken, invalid, or
	// inconsistent — the component is degraded.
	SeverityMajor Severity = "major"

	// SeverityMinor indicates warning-level findings that do not block
	// operation.
	SeverityMinor Severity = "minor"
)

// DiagnosticIssue is a structured record describing a single detected
// problem. It identifies what is wrong, where the issue is located, why it
// may have occurred, and how severe the impact is.
//
// Reference: TS-009-003 §7, ADR-006 §8.3
type DiagnosticIssue struct {
	// Component identifies which component has the issue
	// (e.g. "runtime", "config", "release", "server").
	Component string `json:"component"`

	// Severity classifies the impact: critical, major, or minor.
	Severity Severity `json:"severity"`

	// Description explains what is wrong in human-readable form.
	Description string `json:"description"`

	// Location identifies where the issue is located: the failed check
	// name plus a specific path or configuration key when identifiable.
	Location string `json:"location"`

	// LikelyCause explains why the issue may have occurred, combining
	// component context with the failing check evidence.
	LikelyCause string `json:"likely_cause"`
}

// DiagnosticResult is the consolidated output of the DiagnosticEngine.
// It contains all identified issues, an overall pass assessment, a
// human-readable summary, and the recommendations produced from the
// issues (TS-009-004 integration).
//
// Reference: TS-009-003 §7, TS-009-004 §3
type DiagnosticResult struct {
	// Issues contains all detected diagnostic issues.
	// Empty when the system is healthy.
	Issues []DiagnosticIssue `json:"issues"`

	// Passed is true when no issues were detected.
	Passed bool `json:"passed"`

	// Summary provides a human-readable summary of the diagnostic result.
	Summary string `json:"summary"`

	// Recommendations contains actionable steps for resolving the
	// detected issues. Automatically produced from Issues by the
	// RecommendationEngine (TS-009-004). Empty when there are no issues.
	Recommendations []Recommendation `json:"recommendations,omitempty"`
}

// DiagnosticEngine orchestrates the existing component inspectors and
// produces structured diagnostic issues. It consumes inspection results
// without duplicating any inspection logic and never attempts remediation —
// it only identifies and describes problems.
//
// Reference: TS-009-003 §3/§7, ADR-006 §5.1/§9
type DiagnosticEngine struct {
	runtimeInspector         *RuntimeInspector
	configInspector          *ConfigInspector
	releaseInspector         *ReleaseInspector
	registryInspector        *RegistryInspector
	serverReadinessInspector *ServerReadinessInspector
	reporter                 InspectionReporter
}

// NewDiagnosticEngine creates a DiagnosticEngine with the given inspector
// dependencies. All inspectors are optional and may be nil; nil inspectors
// are skipped during diagnosis. Inspectors must be pre-configured and ready
// to run.
//
// Reference: TS-009-003
func NewDiagnosticEngine(
	runtimeInspector *RuntimeInspector,
	configInspector *ConfigInspector,
	releaseInspector *ReleaseInspector,
	registryInspector *RegistryInspector,
	serverReadinessInspector *ServerReadinessInspector,
) *DiagnosticEngine {
	return &DiagnosticEngine{
		runtimeInspector:         runtimeInspector,
		configInspector:          configInspector,
		releaseInspector:         releaseInspector,
		registryInspector:        registryInspector,
		serverReadinessInspector: serverReadinessInspector,
	}
}

// WithReporter sets an optional progress reporter on the engine.
func (de *DiagnosticEngine) WithReporter(reporter InspectionReporter) *DiagnosticEngine {
	de.reporter = reporter
	return de
}

// Diagnose runs all configured inspectors and converts every failed check
// into a structured DiagnosticIssue. The engine is read-only: no state is
// created, modified, or remediated.
//
// Component mapping:
//   - RuntimeInspector → "runtime"
//   - ConfigInspector → "config"
//   - ReleaseInspector → "release"
//   - RegistryInspector → "server"
//   - ServerReadinessInspector → "server"
//
// The serverRoot parameter is passed to ReleaseInspector for shared link
// validation. If empty, shared link validation is skipped.
//
// The configResolver parameter is passed to ConfigInspector for
// configuration inspection. If nil, config inspection is skipped.
//
// Issues automatically produce recommendations (TS-009-004) which are
// attached to the result.
//
// Reference: TS-009-003 §3, TS-009-004 §3, ADR-006 §9
func (de *DiagnosticEngine) Diagnose(serverRoot string, configResolver *config.Resolver) DiagnosticResult {
	var issues []DiagnosticIssue

	// Run RuntimeInspector (no arguments).
	if de.runtimeInspector != nil {
		de.reportStepStart("Runtime")
		start := time.Now()
		result := de.runtimeInspector.Inspect()
		issues = append(issues, de.issuesFrom("runtime", result)...)
		de.reportInspectionResult("Runtime", start, result)
	}

	// Run ConfigInspector (requires a resolver).
	if de.configInspector != nil && configResolver != nil {
		de.reportStepStart("Configuration")
		start := time.Now()
		result := de.configInspector.Inspect(configResolver)
		issues = append(issues, de.issuesFrom("config", result)...)
		de.reportInspectionResult("Configuration", start, result)
	}

	// Run ReleaseInspector (requires serverRoot).
	if de.releaseInspector != nil {
		de.reportStepStart("Release")
		start := time.Now()
		result := de.releaseInspector.Inspect(serverRoot)
		issues = append(issues, de.issuesFrom("release", result)...)
		de.reportInspectionResult("Release", start, result)
	}

	// Run RegistryInspector (no arguments).
	if de.registryInspector != nil {
		de.reportStepStart("Registry")
		start := time.Now()
		result := de.registryInspector.Inspect()
		issues = append(issues, de.issuesFrom("server", result)...)
		de.reportInspectionResult("Registry", start, result)
	}

	// Run ServerReadinessInspector (no arguments).
	if de.serverReadinessInspector != nil {
		de.reportStepStart("Server Readiness")
		start := time.Now()
		result := de.serverReadinessInspector.Inspect()
		issues = append(issues, de.issuesFrom("server", result)...)
		de.reportInspectionResult("Server Readiness", start, result)
	}

	passed := len(issues) == 0
	summary := BuildDiagnosticSummary(issues)

	// TS-009-004 integration: issues automatically produce recommendations.
	recommendations := NewRecommendationEngine().RecommendationsFor(issues)

	return DiagnosticResult{
		Issues:          issues,
		Passed:          passed,
		Summary:         summary,
		Recommendations: recommendations,
	}
}

// reportStepStart reports the start of an inspection step.
func (de *DiagnosticEngine) reportStepStart(name string) {
	if de.reporter != nil {
		de.reporter.StepStart(name)
	}
}

// reportInspectionResult reports the result of an inspection step.
func (de *DiagnosticEngine) reportInspectionResult(name string, start time.Time, result InspectionResult) {
	if de.reporter != nil {
		duration := time.Since(start)
		if result.Passed {
			de.reporter.StepComplete(name, duration)
		} else {
			de.reporter.StepFailed(name, duration, fmt.Errorf("issues detected"))
		}
	}
}

// issuesFrom converts every failed check in an inspection result into a
// structured DiagnosticIssue for the given component. Passing checks are
// ignored — only failures produce issues.
func (de *DiagnosticEngine) issuesFrom(component string, result InspectionResult) []DiagnosticIssue {
	var issues []DiagnosticIssue

	for _, check := range result.Checks {
		if !check.Passed {
			issues = append(issues, newDiagnosticIssue(component, check))
		}
	}

	return issues
}

// IssuesFromComponents converts every failed check across the given
// component results into structured DiagnosticIssues using the same
// component mapping and issue construction as the DiagnosticEngine.
// Passing components and passing checks are ignored.
//
// It is exported so that consumers running the VerificationEngine (which
// produces component results rather than issues) can derive the same
// issue records for guidance output without re-running the inspectors.
//
// Reference: TS-009-003 §7
func IssuesFromComponents(components []InspectionResult) []DiagnosticIssue {
	var issues []DiagnosticIssue

	for _, component := range components {
		for _, check := range component.Checks {
			if !check.Passed {
				issues = append(issues, newDiagnosticIssue(component.Component, check))
			}
		}
	}

	return issues
}

// newDiagnosticIssue builds a structured issue record for a failed check.
// All five required fields (component, severity, description, location,
// likely cause) are populated from the check evidence and component context.
//
// Reference: TS-009-003 §7
func newDiagnosticIssue(component string, check InspectionCheck) DiagnosticIssue {
	return DiagnosticIssue{
		Component:   component,
		Severity:    classifySeverity(check),
		Description: check.Details,
		Location:    extractLocation(check),
		LikelyCause: likelyCause(component, check),
	}
}

// classifySeverity maps a failed check to a Severity using a deterministic
// keyword heuristic over the check details. The rules are:
//
//  1. critical — required state is missing or absent. Matches details
//     describing absence: "does not exist", "not found", "missing",
//     "no such file", "no runtime config found". The component cannot
//     operate until the state is restored.
//  2. major — existing state is broken, invalid, or inconsistent. Matches
//     details describing degradation: "invalid", "broken", "not a
//     directory", "not accessible", "cannot read", "cannot load",
//     "cannot list", "unreadable", "not absolute", "permission denied",
//     "consistency", "without artifacts", "load error", "validation error".
//  3. minor — warning-level or unclassified findings. Details that match
//     neither rule are treated as minor.
//
// Critical markers are evaluated first because broken-state error strings
// often embed the underlying "no such file or directory" cause.
//
// Reference: TS-009-003 §7
func classifySeverity(check InspectionCheck) Severity {
	details := strings.ToLower(check.Details)

	criticalMarkers := []string{
		"does not exist",
		"not found",
		"missing",
		"no such file",
		"no runtime config found",
	}

	for _, marker := range criticalMarkers {
		if strings.Contains(details, marker) {
			return SeverityCritical
		}
	}

	majorMarkers := []string{
		"invalid",
		"broken",
		"not a directory",
		"not accessible",
		"cannot read",
		"cannot load",
		"cannot list",
		"unreadable",
		"not absolute",
		"permission denied",
		"consistency",
		"without artifacts",
		"load error",
		"validation error",
	}

	for _, marker := range majorMarkers {
		if strings.Contains(details, marker) {
			return SeverityMajor
		}
	}

	return SeverityMinor
}

// extractLocation determines where the issue is located. The failed check
// name is the primary locator (e.g. "active_symlink"); when the check
// details identify a specific path or configuration key, it is appended in
// parentheses for precision.
func extractLocation(check InspectionCheck) string {
	for _, token := range strings.Fields(check.Details) {
		trimmed := strings.Trim(token, `"',:;()`)
		if isPathLike(trimmed) {
			return fmt.Sprintf("%s (%s)", check.Name, trimmed)
		}
	}
	return check.Name
}

// isPathLike reports whether the token identifies a specific location:
// an absolute path, a home-relative path, a project-relative path, a
// dot-prefixed entry, or a dot-separated identifier such as a configuration
// key (e.g. "project.name") or file name (e.g. "config.yaml").
//
// Version tokens (e.g. "1.0.0", "v1.0.0") are deliberately excluded: their
// dot-separated segments start with digits, which identifier-style segments
// never do.
func isPathLike(token string) bool {
	if token == "" {
		return false
	}

	// Absolute paths, home-relative paths, and dot-prefixed entries
	// (e.g. ".anvil/active", ".gitignore") are always location identifiers.
	if strings.HasPrefix(token, "/") || strings.HasPrefix(token, "~") || strings.HasPrefix(token, ".") {
		return true
	}

	// A dot-separated identifier must contain at least one dot and no
	// whitespace; plain words such as "expected" are not locations.
	if !strings.Contains(token, ".") || strings.ContainsAny(token, " \t") {
		return false
	}

	return isIdentifierPath(token)
}

// isIdentifierPath reports whether a dot-separated token is made of
// identifier-style segments: every segment starts with a letter or
// underscore and continues with letters, digits, underscores, or hyphens.
// This accepts configuration keys ("project.name"), file names
// ("config.yaml", "release-1.yaml"), and relative paths, while rejecting
// version tokens whose segments start with digits ("1.0.0", "v1.0.0").
func isIdentifierPath(token string) bool {
	segments := strings.Split(token, ".")

	for _, segment := range segments {
		if segment == "" || !isIdentifierStart(segment[0]) {
			return false
		}
		for i := 1; i < len(segment); i++ {
			if !isIdentifierPart(segment[i]) {
				return false
			}
		}
	}

	return true
}

// isIdentifierStart reports whether c can start an identifier segment.
func isIdentifierStart(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
}

// isIdentifierPart reports whether c can appear inside an identifier segment.
func isIdentifierPart(c byte) bool {
	return isIdentifierStart(c) || (c >= '0' && c <= '9') || c == '-'
}

// likelyCause explains why the issue may have occurred. It combines
// component-specific context with the failing check name and evidence
// from the check details.
//
// Reference: TS-009-003 §7
func likelyCause(component string, check InspectionCheck) string {
	cause := componentCause(component)
	return fmt.Sprintf("%s Check %q failed: %s", cause, check.Name, check.Details)
}

// componentCause returns the component-specific context used to explain
// the likely cause of issues.
func componentCause(component string) string {
	switch component {
	case "config":
		return "Configuration is missing, invalid, or inconsistent. This usually means required keys were not set or values do not conform to the canonical schema (EPIC-002)."
	case "release":
		return "Release infrastructure is missing or inconsistent. This usually means releases were not created correctly, artifacts are absent, or shared links were broken (EPIC-004)."
	case "server":
		return "Server runtime state is missing or inconsistent. This usually means the server was not initialized or project registrations are corrupted (EPIC-005)."
	default: // "runtime"
		return "Runtime environment state is missing or broken. This usually means the Runtime was not provisioned or its filesystem layout was modified (EPIC-005)."
	}
}

// BuildDiagnosticSummary generates a human-readable summary of the
// diagnostic result, including a severity breakdown when issues exist.
//
// It is exported so that consumers extending a diagnostic result (e.g.
// appending an issue the engine skipped) can rebuild the summary with the
// same wording.
func BuildDiagnosticSummary(issues []DiagnosticIssue) string {
	if len(issues) == 0 {
		return "No issues detected across all inspected components"
	}

	counts := map[Severity]int{
		SeverityCritical: 0,
		SeverityMajor:    0,
		SeverityMinor:    0,
	}
	for _, issue := range issues {
		counts[issue.Severity]++
	}

	return fmt.Sprintf("%d issue(s) detected: %d critical, %d major, %d minor",
		len(issues), counts[SeverityCritical], counts[SeverityMajor], counts[SeverityMinor])
}
