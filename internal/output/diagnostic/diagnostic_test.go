package diagnostic

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/inspection"
)

// ── Health Status Mapping Tests ───────────────────────────────────────

func TestHealthStatusFromComponents(t *testing.T) {
	passed := inspection.InspectionResult{Component: "runtime", Passed: true}
	failed := inspection.InspectionResult{Component: "release", Passed: false}

	tests := []struct {
		name       string
		components []inspection.InspectionResult
		want       inspection.HealthStatus
	}{
		{
			name:       "all passed → healthy",
			components: []inspection.InspectionResult{passed, passed},
			want:       inspection.HealthStatusHealthy,
		},
		{
			name:       "mixed → degraded",
			components: []inspection.InspectionResult{passed, failed},
			want:       inspection.HealthStatusDegraded,
		},
		{
			name:       "all failed → unhealthy",
			components: []inspection.InspectionResult{failed, failed},
			want:       inspection.HealthStatusUnhealthy,
		},
		{
			name:       "empty → healthy",
			components: nil,
			want:       inspection.HealthStatusHealthy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := healthStatusFromComponents(tt.components); got != tt.want {
				t.Errorf("healthStatusFromComponents() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewReadinessView(t *testing.T) {
	passed := inspection.InspectionResult{Component: "runtime", Passed: true}
	failed := inspection.InspectionResult{Component: "release", Passed: false}

	tests := []struct {
		name   string
		result inspection.ReadinessCoordinatorResult
		want   inspection.HealthStatus
		wantOK bool
	}{
		{
			name:   "ready → healthy + passed",
			result: inspection.ReadinessCoordinatorResult{Ready: true, Components: []inspection.InspectionResult{passed}},
			want:   inspection.HealthStatusHealthy,
			wantOK: true,
		},
		{
			name:   "mixed components → degraded + not passed",
			result: inspection.ReadinessCoordinatorResult{Ready: false, Components: []inspection.InspectionResult{passed, failed}},
			want:   inspection.HealthStatusDegraded,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := NewReadinessView(tt.result)
			if view.Status != tt.want {
				t.Errorf("NewReadinessView() status = %q, want %q", view.Status, tt.want)
			}
			if view.Passed != tt.wantOK {
				t.Errorf("NewReadinessView() passed = %v, want %v", view.Passed, tt.wantOK)
			}
			if len(view.Components) != len(tt.result.Components) {
				t.Errorf("NewReadinessView() components = %d, want %d", len(view.Components), len(tt.result.Components))
			}
		})
	}
}

// ── Human-Readable Formatting Tests (TS-P9-08) ────────────────────────

func TestFormatDiagnosticView_Healthy(t *testing.T) {
	view := NewVerificationView(inspection.SystemVerificationResult{
		Status:  inspection.HealthStatusHealthy,
		Summary: "Platform is healthy",
	})

	var buf bytes.Buffer
	FormatDiagnosticView(&buf, view)
	got := buf.String()

	if !strings.Contains(got, "Health: HEALTHY") {
		t.Errorf("output should contain health header, got:\n%s", got)
	}
	if !strings.Contains(got, "Summary: Platform is healthy") {
		t.Errorf("output should contain summary line, got:\n%s", got)
	}
	if strings.Contains(got, "Issues (") {
		t.Errorf("healthy output should not contain issues section, got:\n%s", got)
	}
	if strings.Contains(got, "Recommendations (") {
		t.Errorf("healthy output should not contain recommendations section, got:\n%s", got)
	}
	if strings.Contains(got, "Components:") {
		t.Errorf("healthy output should not contain components section, got:\n%s", got)
	}
}

func TestFormatDiagnosticView_Degraded(t *testing.T) {
	view := NewVerificationView(inspection.SystemVerificationResult{
		Status: inspection.HealthStatusDegraded,
		ComponentResults: []inspection.InspectionResult{
			{
				Component: "runtime",
				Checks: []inspection.InspectionCheck{
					{Name: "active_symlink", Passed: false, Details: "The active symlink is broken"},
				},
				Passed: false,
			},
			{
				Component: "release",
				Checks: []inspection.InspectionCheck{
					{Name: "release_state", Passed: false, Details: "Release state is inconsistent"},
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

	if !strings.Contains(got, "Health: DEGRADED") {
		t.Errorf("output should contain degraded health header, got:\n%s", got)
	}
	if !strings.Contains(got, "Summary: 2 component(s) failed") {
		t.Errorf("output should contain summary line, got:\n%s", got)
	}
	if !strings.Contains(got, "Issues (2):") {
		t.Errorf("output should contain issues section header with count, got:\n%s", got)
	}
	if !strings.Contains(got, "  [MAJOR] runtime — active_symlink") {
		t.Errorf("output should contain issue line with severity and location, got:\n%s", got)
	}
	if !strings.Contains(got, "    The active symlink is broken") {
		t.Errorf("output should contain issue description, got:\n%s", got)
	}
	if !strings.Contains(got, "    Cause:") {
		t.Errorf("output should contain cause line, got:\n%s", got)
	}
	if strings.Contains(got, "    How:") {
		t.Errorf("demoted output must not render recommendation How lines, got:\n%s", got)
	}
	if strings.Contains(got, "Recommendations (") {
		t.Errorf("demoted output must not render recommendations section, got:\n%s", got)
	}
}

func TestFormatDiagnosticView_Components(t *testing.T) {
	view := NewReadinessView(inspection.ReadinessCoordinatorResult{
		Ready: false,
		Components: []inspection.InspectionResult{
			{
				Component: "runtime",
				Checks: []inspection.InspectionCheck{
					{Name: "active_symlink", Passed: true, Details: "exists"},
				},
				Passed: true,
			},
			{
				Component: "release",
				Checks: []inspection.InspectionCheck{
					{Name: "active_symlink", Passed: false, Details: "does not exist"},
				},
				Passed: false,
			},
		},
		Blockers: []string{"[release] active_symlink: does not exist"},
		Summary:  "System is not ready: 1 blocker(s) found",
	})

	var buf bytes.Buffer
	FormatDiagnosticView(&buf, view)
	got := buf.String()

	if !strings.Contains(got, "Components:") {
		t.Errorf("output should contain components section header, got:\n%s", got)
	}
	if !strings.Contains(got, "  [PASS] runtime — all checks passed") {
		t.Errorf("output should contain passing component line, got:\n%s", got)
	}
	if !strings.Contains(got, "  [FAIL] release — 1 check(s) failed") {
		t.Errorf("output should contain failing component line with failed check count, got:\n%s", got)
	}
}

// ── Machine-Readable Formatting Tests (TS-P9-09) ──────────────────────

func TestFormatDiagnosticView_Unhealthy(t *testing.T) {
	view := NewVerificationView(inspection.SystemVerificationResult{
		Status: inspection.HealthStatusUnhealthy,
		ComponentResults: []inspection.InspectionResult{
			{
				Component: "runtime",
				Checks: []inspection.InspectionCheck{
					{Name: "active_symlink", Passed: false, Details: "The active symlink does not exist"},
				},
				Passed: false,
			},
		},
		Summary: "1 component(s) failed",
	})
	view.Issues = inspection.IssuesFromComponents(view.Components)

	var buf bytes.Buffer
	FormatDiagnosticView(&buf, view)
	got := buf.String()

	if !strings.Contains(got, "Health: UNHEALTHY") {
		t.Errorf("output should contain unhealthy health header, got:\n%s", got)
	}
	if !strings.Contains(got, "  [CRITICAL] runtime — active_symlink") {
		t.Errorf("output should contain critical issue line, got:\n%s", got)
	}
	// Platform-036 §3 (TS-015-05-02): recommendations-style output is demoted —
	// issues never render a "How:" line and no recommendations section is
	// produced.
	if strings.Contains(got, "    How:") {
		t.Errorf("demoted output should not contain a How line, got:\n%s", got)
	}
	if strings.Contains(got, "Recommendations (") {
		t.Errorf("demoted output should not contain recommendations section, got:\n%s", got)
	}
}

// TestFormatDiagnosticView_IssuesNeverRenderRecommendations verifies that
// the demoted view model (Platform-036 §3, TS-015-05-02) renders issues
// without recommendation "How:" lines or a recommendations section —
// recommendations-style diagnostics are not a v2 output surface.
//
// Reference: TS-P9-08, Platform-036
func TestFormatDiagnosticView_IssuesNeverRenderRecommendations(t *testing.T) {
	view := DiagnosticView{
		Status:  inspection.HealthStatusDegraded,
		Passed:  false,
		Summary: "2 issue(s) detected: 1 major, 1 minor",
		Issues: []inspection.DiagnosticIssue{
			{
				Component:   "runtime",
				Severity:    inspection.SeverityMajor,
				Description: "The active symlink is broken",
				Location:    "active_symlink (.anvil/active)",
				LikelyCause: "Runtime was not provisioned",
			},
			{
				Component:   "release",
				Severity:    inspection.SeverityMinor,
				Description: "Release state is inconsistent",
				Location:    "release_state",
				LikelyCause: "Releases were not created correctly",
			},
		},
	}

	var buf bytes.Buffer
	FormatDiagnosticView(&buf, view)
	got := buf.String()

	if !strings.Contains(got, "Issues (2):") {
		t.Errorf("output should contain issues section header with count, got:\n%s", got)
	}
	if strings.Contains(got, "    How:") {
		t.Errorf("demoted output should not contain a How line, got:\n%s", got)
	}
	if strings.Contains(got, "Recommendations (") {
		t.Errorf("demoted output should not contain recommendations section, got:\n%s", got)
	}
}

func TestWriteDiagnosticJSON_EnvelopeAndData(t *testing.T) {
	verificationView := NewVerificationView(inspection.SystemVerificationResult{
		Status: inspection.HealthStatusUnhealthy,
		ComponentResults: []inspection.InspectionResult{
			{
				Component: "runtime",
				Checks: []inspection.InspectionCheck{
					{Name: "active_symlink", Passed: false, Details: "The active symlink does not exist"},
				},
				Passed: false,
			},
		},
		Summary: "1 component(s) failed",
	})

	readinessView := NewReadinessView(inspection.ReadinessCoordinatorResult{
		Ready: true,
		Components: []inspection.InspectionResult{
			{Component: "runtime", Passed: true},
		},
		Summary: "System is ready for deployment operations",
	})

	tests := []struct {
		name string
		view DiagnosticView
		want string
	}{
		{name: "verification view", view: verificationView, want: "unhealthy"},
		{name: "readiness view", view: readinessView, want: "healthy"},
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

			if envelope["version"] != "1" {
				t.Errorf("version = %v, want %q", envelope["version"], "1")
			}
			if envelope["status"] != "success" {
				t.Errorf("status = %v, want %q", envelope["status"], "success")
			}

			data, ok := envelope["data"].(map[string]interface{})
			if !ok {
				t.Fatalf("data should be an object, got: %v", envelope["data"])
			}
			if data["status"] != tt.want {
				t.Errorf("data.status = %v, want %q", data["status"], tt.want)
			}

			if tt.view.Components != nil {
				components, ok := data["components"].([]interface{})
				if !ok || len(components) == 0 {
					t.Errorf("data.components should be a non-empty array, got: %v", data["components"])
				}
			}

			// Platform-036 §3 (TS-015-05-02): the demoted view model never
			// emits recommendations or contexts fields.
			if _, ok := data["recommendations"]; ok {
				t.Errorf("data must not contain a recommendations field (Platform-036 demotion)")
			}
			if _, ok := data["contexts"]; ok {
				t.Errorf("data must not contain a contexts field (Platform-036 demotion)")
			}
		})
	}
}

func TestWriteDiagnosticJSON_Deterministic(t *testing.T) {
	view := NewVerificationView(inspection.SystemVerificationResult{
		Status: inspection.HealthStatusDegraded,
		ComponentResults: []inspection.InspectionResult{
			{
				Component: "release",
				Checks: []inspection.InspectionCheck{
					{Name: "release_state", Passed: false, Details: "Release state is invalid"},
				},
				Passed: false,
			},
		},
		Summary: "1 component(s) failed",
	})
	view.Issues = inspection.IssuesFromComponents(view.Components)

	var first, second bytes.Buffer
	if err := WriteDiagnosticJSON(&first, view); err != nil {
		t.Fatalf("first WriteDiagnosticJSON returned error: %v", err)
	}
	if err := WriteDiagnosticJSON(&second, view); err != nil {
		t.Fatalf("second WriteDiagnosticJSON returned error: %v", err)
	}

	if first.String() != second.String() {
		t.Errorf("WriteDiagnosticJSON output is not deterministic:\nfirst:\n%s\nsecond:\n%s",
			first.String(), second.String())
	}
}
