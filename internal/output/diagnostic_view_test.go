// Package output provides shared formatters for consistent CLI output
// across all Anvil commands.
//
// Reference: TS-P9-08, TS-P9-09
package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/inspection"
)

// TestNewVerificationView verifies that NewVerificationView maps the
// three-state verification result into the presentation view: status is
// preserved and passed mirrors "healthy".
//
// Reference: ST-P9-01, TS-P9-08
func TestNewVerificationView(t *testing.T) {
	passed := inspection.InspectionResult{Component: "runtime", Passed: true}
	failed := inspection.InspectionResult{Component: "release", Passed: false}

	tests := []struct {
		name       string
		status     inspection.HealthStatus
		components []inspection.InspectionResult
		wantPassed bool
	}{
		{name: "healthy → passed", status: inspection.HealthStatusHealthy, components: []inspection.InspectionResult{passed}, wantPassed: true},
		{name: "degraded → not passed", status: inspection.HealthStatusDegraded, components: []inspection.InspectionResult{passed, failed}, wantPassed: false},
		{name: "unhealthy → not passed", status: inspection.HealthStatusUnhealthy, components: []inspection.InspectionResult{failed}, wantPassed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := NewVerificationView(inspection.SystemVerificationResult{
				ComponentResults: tt.components,
				Status:           tt.status,
				Summary:          "summary",
			})

			if view.Status != tt.status {
				t.Errorf("view.Status = %q, want %q", view.Status, tt.status)
			}
			if view.Passed != tt.wantPassed {
				t.Errorf("view.Passed = %v, want %v", view.Passed, tt.wantPassed)
			}
			if len(view.Components) != len(tt.components) {
				t.Errorf("view.Components = %d entries, want %d", len(view.Components), len(tt.components))
			}
		})
	}
}

// TestNewDiagnosticView_Contexts verifies that NewDiagnosticView
// automatically classifies the issues into architectural contexts
// (ST-P9-06).
//
// Reference: ST-P9-06, TS-P9-08
func TestNewDiagnosticView_Contexts(t *testing.T) {
	result := inspection.DiagnosticResult{
		Issues: []inspection.DiagnosticIssue{
			{Component: "config", Severity: inspection.SeverityMajor, Description: "missing key", Location: "completeness"},
			{Component: "release", Severity: inspection.SeverityCritical, Description: "no artifacts", Location: "artifact_presence (/srv/releases/r1)"},
		},
		Passed:  false,
		Summary: "2 issue(s) detected",
	}

	view := NewDiagnosticView(result)

	if len(view.Contexts) != 2 {
		t.Fatalf("view.Contexts = %d entries, want 2", len(view.Contexts))
	}
	if view.Contexts[0].Context != inspection.ContextDevelopment {
		t.Errorf("context[0] = %q, want %q", view.Contexts[0].Context, inspection.ContextDevelopment)
	}
	if view.Contexts[1].Context != inspection.ContextArtifact {
		t.Errorf("context[1] = %q, want %q (artifact vs release identity separation)", view.Contexts[1].Context, inspection.ContextArtifact)
	}
}

// TestNewReadinessView_Blockers verifies that NewReadinessView carries the
// actionable blockers into the view.
//
// Reference: ST-P9-02, TS-P9-08
func TestNewReadinessView_Blockers(t *testing.T) {
	result := inspection.ReadinessCoordinatorResult{
		Ready: false,
		Components: []inspection.InspectionResult{
			{Component: "runtime", Passed: false},
		},
		Blockers: []string{"[runtime] active_symlink: active symlink does not exist"},
		Summary:  "System is not ready: 1 blocker(s) found",
	}

	view := NewReadinessView(result)

	if len(view.Blockers) != 1 || view.Blockers[0] != result.Blockers[0] {
		t.Errorf("view.Blockers = %v, want %v", view.Blockers, result.Blockers)
	}
}

// TestFormatDiagnosticView_ContextsSection verifies that the Contexts
// section renders each issue with its architectural context and that it is
// omitted when empty.
//
// Reference: ST-P9-06, TS-P9-08
func TestFormatDiagnosticView_ContextsSection(t *testing.T) {
	view := NewDiagnosticView(inspection.DiagnosticResult{
		Issues: []inspection.DiagnosticIssue{
			{Component: "runtime", Severity: inspection.SeverityCritical, Description: "broken", Location: "active_symlink (.anvil/active)", LikelyCause: "not provisioned"},
			{Component: "release", Severity: inspection.SeverityMajor, Description: "missing", Location: "release_directory (/srv/releases)", LikelyCause: "not created"},
		},
		Passed:  false,
		Summary: "2 issue(s) detected",
	})

	var buf bytes.Buffer
	FormatDiagnosticView(&buf, view)
	got := buf.String()

	if !strings.Contains(got, "Contexts (2):") {
		t.Errorf("output should contain contexts section header, got:\n%s", got)
	}
	if !strings.Contains(got, "  [server_runtime] runtime — active_symlink (.anvil/active)") {
		t.Errorf("output should contain server_runtime context line, got:\n%s", got)
	}
	if !strings.Contains(got, "  [release] release — release_directory (/srv/releases)") {
		t.Errorf("output should contain release context line, got:\n%s", got)
	}

	// Without issues, the contexts section must be omitted entirely.
	healthy := NewDiagnosticView(inspection.DiagnosticResult{
		Passed:  true,
		Summary: "No issues detected",
	})
	var healthyBuf bytes.Buffer
	FormatDiagnosticView(&healthyBuf, healthy)
	if strings.Contains(healthyBuf.String(), "Contexts (") {
		t.Errorf("healthy output should not contain contexts section, got:\n%s", healthyBuf.String())
	}
}

// TestFormatDiagnosticView_BlockersSection verifies that the Blockers
// section renders the actionable blocker list and is omitted when empty.
//
// Reference: ST-P9-02, TS-P9-08
func TestFormatDiagnosticView_BlockersSection(t *testing.T) {
	view := NewReadinessView(inspection.ReadinessCoordinatorResult{
		Ready: false,
		Components: []inspection.InspectionResult{
			{Component: "runtime", Passed: false},
		},
		Blockers: []string{
			"[runtime] active_symlink: active symlink does not exist",
			"[config] completeness: missing required values",
		},
		Summary: "System is not ready: 2 blocker(s) found",
	})

	var buf bytes.Buffer
	FormatDiagnosticView(&buf, view)
	got := buf.String()

	if !strings.Contains(got, "Blockers (2):") {
		t.Errorf("output should contain blockers section header, got:\n%s", got)
	}
	if !strings.Contains(got, "  1. [runtime] active_symlink: active symlink does not exist") {
		t.Errorf("output should contain first blocker line, got:\n%s", got)
	}
	if !strings.Contains(got, "  2. [config] completeness: missing required values") {
		t.Errorf("output should contain second blocker line, got:\n%s", got)
	}

	// Ready views carry no blockers — the section must be omitted.
	ready := NewReadinessView(inspection.ReadinessCoordinatorResult{
		Ready:   true,
		Summary: "System is ready for deployment operations",
	})
	var readyBuf bytes.Buffer
	FormatDiagnosticView(&readyBuf, ready)
	if strings.Contains(readyBuf.String(), "Blockers (") {
		t.Errorf("ready output should not contain blockers section, got:\n%s", readyBuf.String())
	}
}

// TestWriteDiagnosticJSON_ContextsAndBlockers verifies that the JSON
// payload carries the contexts and blockers fields alongside the existing
// envelope fields, without breaking the standard envelope.
//
// Reference: ST-P9-06, TS-P9-09
func TestWriteDiagnosticJSON_ContextsAndBlockers(t *testing.T) {
	diagnosticView := NewDiagnosticView(inspection.DiagnosticResult{
		Issues: []inspection.DiagnosticIssue{
			{Component: "config", Severity: inspection.SeverityMajor, Description: "missing", Location: "completeness"},
		},
		Passed:  false,
		Summary: "1 issue(s) detected",
	})

	readinessView := NewReadinessView(inspection.ReadinessCoordinatorResult{
		Ready: false,
		Components: []inspection.InspectionResult{
			{Component: "runtime", Passed: false},
		},
		Blockers: []string{"[runtime] active_symlink: active symlink does not exist"},
		Summary:  "System is not ready: 1 blocker(s) found",
	})

	tests := []struct {
		name     string
		view     DiagnosticView
		wantJSON string
	}{
		{name: "contexts", view: diagnosticView, wantJSON: "contexts"},
		{name: "blockers", view: readinessView, wantJSON: "blockers"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteDiagnosticJSON(&buf, tt.view); err != nil {
				t.Fatalf("WriteDiagnosticJSON returned error: %v", err)
			}

			var envelope map[string]interface{}
			if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
				t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
			}

			if envelope["version"] != "1" || envelope["status"] != "success" {
				t.Errorf("envelope = version %v status %v, want 1/success", envelope["version"], envelope["status"])
			}

			data, ok := envelope["data"].(map[string]interface{})
			if !ok {
				t.Fatalf("data should be an object, got: %v", envelope["data"])
			}

			payload, ok := data[tt.wantJSON].([]interface{})
			if !ok || len(payload) == 0 {
				t.Errorf("data.%s should be a non-empty array, got: %v", tt.wantJSON, data[tt.wantJSON])
			}
		})
	}
}
