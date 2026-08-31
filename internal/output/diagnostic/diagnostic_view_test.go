// Package diagnostic provides shared formatters for consistent diagnostic
// output across all Anvil commands.
//
// Reference: TS-P9-08, TS-P9-09
package diagnostic

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

// TestFormatDiagnosticView_NoContextsSection verifies that the demoted
// view model (Platform-036 §3, TS-015-05-02) never renders the architectural
// context classification section — context-aware diagnostics are not a v2
// output surface.
//
// Reference: TS-P9-08, Platform-036
func TestFormatDiagnosticView_NoContextsSection(t *testing.T) {
	view := NewVerificationView(inspection.SystemVerificationResult{
		Status: inspection.HealthStatusDegraded,
		ComponentResults: []inspection.InspectionResult{
			{
				Component: "runtime",
				Checks: []inspection.InspectionCheck{
					{Name: "active_symlink", Passed: false, Details: "active symlink does not exist at .anvil/active"},
				},
				Passed: false,
			},
			{
				Component: "release",
				Checks: []inspection.InspectionCheck{
					{Name: "release_directory", Passed: false, Details: "release directory missing at /srv/releases"},
				},
				Passed: false,
			},
		},
		Summary: "2 component(s) failed",
	})
	view.Issues = inspection.IssuesFromComponents(view.Components)

	var buf bytes.Buffer
	FormatDiagnosticView(&buf, view)
	got := buf.String()

	if strings.Contains(got, "Contexts (") {
		t.Errorf("demoted output must not contain contexts section, got:\n%s", got)
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

// TestWriteDiagnosticJSON_Blockers verifies that the JSON payload carries
// the blockers field alongside the standard envelope fields, and that the
// demoted view model never emits a contexts field (Platform-036 §3,
// TS-015-05-02).
//
// Reference: TS-P9-09, Platform-036
func TestWriteDiagnosticJSON_Blockers(t *testing.T) {
	readinessView := NewReadinessView(inspection.ReadinessCoordinatorResult{
		Ready: false,
		Components: []inspection.InspectionResult{
			{Component: "runtime", Passed: false},
		},
		Blockers: []string{"[runtime] active_symlink: active symlink does not exist"},
		Summary:  "System is not ready: 1 blocker(s) found",
	})

	var buf bytes.Buffer
	if err := WriteDiagnosticJSON(&buf, readinessView); err != nil {
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

	blockers, ok := data["blockers"].([]interface{})
	if !ok || len(blockers) == 0 {
		t.Errorf("data.blockers should be a non-empty array, got: %v", data["blockers"])
	}

	if _, ok := data["contexts"]; ok {
		t.Errorf("data must not contain a contexts field (Platform-036 demotion)")
	}
	if _, ok := data["recommendations"]; ok {
		t.Errorf("data must not contain a recommendations field (Platform-036 demotion)")
	}
}
