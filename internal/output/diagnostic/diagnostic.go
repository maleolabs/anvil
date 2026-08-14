// Package diagnostic provides shared formatters for consistent diagnostic
// output across all Anvil commands.
//
// ── Diagnostic Formatting (TS-P9-08, TS-P9-09) ────────────────────────
//
// EPIC-009 diagnostic and verification results are presented in two
// consistent forms:
//
//   - Human-readable (TS-P9-08): a health status header, per-component
//     check status, and issues with severity and location.
//   - Machine-readable (TS-P9-09): the same normalized payload wrapped
//     in the standard OutputEnvelope (TS-P8-05) with a version
//     identifier, for automation consumers.
//
// Both formatters consume the shared DiagnosticView presentation model
// so every EPIC-009 command renders identical output.
//
// Scope note (ADR-036, TS-015-05-02): the recommendations-style and
// context-classification output (recommendations, contexts, "How:" lines)
// was removed with the platform-ops breadth demotion. The view now carries
// observations only — it never gates lifecycle operations.
//
// Reference: TS-P9-08, TS-P9-09, ADR-010 §7.1/§7.2, EPIC-009 §8.2/§8.4
package diagnostic

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"maleolabs.com/anvil/internal/inspection"
	"maleolabs.com/anvil/internal/output"
)

// ── Diagnostic View ───────────────────────────────────────────────────

// DiagnosticView is the normalized presentation model for all EPIC-009
// diagnostic and verification results (TS-P9-08/09).
//
// It combines the three-state health assessment (healthy, degraded,
// unhealthy), a binary pass/fail verdict, a human-readable summary, and
// the optional component, issue, and blocker payloads. Both the
// human-readable and machine-readable formatters consume this view, so
// the two output modes stay field-for-field consistent.
//
// Reference: TS-P9-08, TS-P9-09, ADR-010 §7.1/§7.2, EPIC-009 §8.2/§8.4
type DiagnosticView struct {
	// Status is the three-state health assessment: healthy, degraded,
	// or unhealthy (EPIC-009 §8.2).
	Status inspection.HealthStatus `json:"status"`

	// Passed is the binary verdict: true when everything passed.
	Passed bool `json:"passed"`

	// Summary is a human-readable summary of the result.
	Summary string `json:"summary"`

	// Components contains the per-component inspection results.
	// Present for readiness and verification views.
	Components []inspection.InspectionResult `json:"components,omitempty"`

	// Issues contains the detected diagnostic issues.
	// Present for verification views with guidance output.
	Issues []inspection.DiagnosticIssue `json:"issues,omitempty"`

	// Blockers contains the actionable blocker descriptions that prevent
	// readiness. Present for readiness views when not ready.
	Blockers []string `json:"blockers,omitempty"`
}

// ── View Constructors ─────────────────────────────────────────────────

// NewReadinessView normalizes a ReadinessCoordinatorResult into a
// DiagnosticView for presentation.
//
// The health status is derived from the component results: all passed
// (or zero components) → healthy, all failed → unhealthy, otherwise
// degraded. Passed mirrors result.Ready. The actionable blocker
// descriptions are carried into the view so the formatters can render
// guidance for every failed readiness check.
//
// Reference: TS-P9-08, EPIC-009 §8.2
func NewReadinessView(result inspection.ReadinessCoordinatorResult) DiagnosticView {
	return DiagnosticView{
		Status:     healthStatusFromComponents(result.Components),
		Passed:     result.Ready,
		Summary:    result.Summary,
		Components: result.Components,
		Blockers:   result.Blockers,
	}
}

// NewVerificationView normalizes a SystemVerificationResult into a
// DiagnosticView for presentation.
//
// The health status is taken directly from the verification result's
// three-state assessment (healthy, degraded, unhealthy). Passed is true
// only when the status is healthy. The per-component results are carried
// into the view for per-component rendering.
//
// Reference: TS-P9-08, EPIC-009 §8.2
func NewVerificationView(result inspection.SystemVerificationResult) DiagnosticView {
	return DiagnosticView{
		Status:     result.Status,
		Passed:     result.Status == inspection.HealthStatusHealthy,
		Summary:    result.Summary,
		Components: result.ComponentResults,
	}
}

// healthStatusFromComponents derives the three-state health assessment
// from per-component inspection results:
//   - Healthy: all components passed (or no components inspected)
//   - Unhealthy: all components failed
//   - Degraded: some components failed
//
// Reference: TS-P9-08, EPIC-009 §8.2
func healthStatusFromComponents(components []inspection.InspectionResult) inspection.HealthStatus {
	if len(components) == 0 {
		return inspection.HealthStatusHealthy
	}

	passedCount := 0
	for _, component := range components {
		if component.Passed {
			passedCount++
		}
	}

	switch passedCount {
	case len(components):
		return inspection.HealthStatusHealthy
	case 0:
		return inspection.HealthStatusUnhealthy
	default:
		return inspection.HealthStatusDegraded
	}
}

// ── Human-Readable Formatting (TS-P9-08) ──────────────────────────────

// FormatDiagnosticView renders the diagnostic view in the EPIC-009
// human-readable report format.
//
// Output shape:
//
//	Health: DEGRADED
//	Summary: 2 issue(s) detected: 1 critical, 1 major
//
//	Components:
//	  [PASS] runtime — all checks passed
//	  [FAIL] release — 1 check(s) failed
//
//	Issues (2):
//	  [CRITICAL] runtime — active_symlink (.anvil/active)
//	    The active symlink does not exist
//	    Cause: Runtime was not provisioned
//
//	  [MAJOR] release — release_state
//	    Release state is invalid
//	    Cause: Releases were not created correctly
//
//	Blockers (1):
//	  1. [runtime] active_symlink: The active symlink does not exist
//
// The Components, Issues, and Blockers sections are only rendered when
// non-empty. A single blank line separates the sections; no trailing
// blank line is produced.
//
// Scope note (ADR-036, TS-015-05-02): the Contexts and Recommendations
// sections were removed with the platform-ops breadth demotion — the
// output observes state and never carries governing guidance.
//
// Reference: TS-P9-08, ADR-010 §7.1, EPIC-009 §8.4
func FormatDiagnosticView(w io.Writer, view DiagnosticView) {
	fmt.Fprintf(w, "Health: %s\n", strings.ToUpper(string(view.Status)))
	fmt.Fprintf(w, "Summary: %s\n", view.Summary)

	// Components section.
	if len(view.Components) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Components:")
		for _, component := range view.Components {
			formatComponent(w, component)
		}
	}

	// Issues section.
	if len(view.Issues) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Issues (%d):\n", len(view.Issues))
		for i, issue := range view.Issues {
			if i > 0 {
				fmt.Fprintln(w)
			}
			formatIssue(w, issue)
		}
	}

	// Blockers section (readiness views): actionable failure descriptions
	// that prevent readiness.
	if len(view.Blockers) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Blockers (%d):\n", len(view.Blockers))
		for i, blocker := range view.Blockers {
			fmt.Fprintf(w, "  %d. %s\n", i+1, blocker)
		}
	}
}

// formatComponent renders a single component line in the Components
// section. Passing components report "all checks passed"; failing
// components report the number of failed checks.
func formatComponent(w io.Writer, component inspection.InspectionResult) {
	if component.Passed {
		fmt.Fprintf(w, "  [%s] %s — all checks passed\n", output.StatusPass, component.Component)
		return
	}

	failedCount := 0
	for _, check := range component.Checks {
		if !check.Passed {
			failedCount++
		}
	}
	fmt.Fprintf(w, "  [%s] %s — %d check(s) failed\n", output.StatusFail, component.Component, failedCount)
}

// formatIssue renders a single issue in the Issues section: a severity
// badge with component and location, the description, and the likely
// cause.
//
// Reference: TS-P9-08
func formatIssue(w io.Writer, issue inspection.DiagnosticIssue) {
	fmt.Fprintf(w, "  [%s] %s — %s\n", strings.ToUpper(string(issue.Severity)), issue.Component, issue.Location)
	fmt.Fprintf(w, "    %s\n", issue.Description)
	fmt.Fprintf(w, "    Cause: %s\n", issue.LikelyCause)
}

// ── Machine-Readable Formatting (TS-P9-09) ────────────────────────────

// WriteDiagnosticJSON encodes the diagnostic view as a machine-readable
// JSON document wrapped in the standard OutputEnvelope (TS-P8-05):
// version "1", status "success", and the view payload in the data
// field.
//
// The output is pretty-printed with two-space indentation. The schema
// mirrors the human-readable format field-for-field (TS-009-009 mirrors
// TS-009-008).
//
// Usage:
//
//	err := output.WriteDiagnosticJSON(cmd.OutOrStdout(), view)
//
// Reference: TS-P9-09, TS-P8-05, ADR-010 §7.2/§8.5, EPIC-009 §8.4
func WriteDiagnosticJSON(w io.Writer, view DiagnosticView) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	envelope := output.OutputEnvelope{
		Version: "1",
		Status:  "success",
		Data:    view,
	}
	return enc.Encode(envelope)
}
