package output

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

func TestNewDiagnosticView_StatusMapping(t *testing.T) {
	minorIssue := inspection.DiagnosticIssue{
		Component:   "runtime",
		Severity:    inspection.SeverityMinor,
		Description: "warning-level finding",
		Location:    "active_symlink",
		LikelyCause: "Runtime state was modified",
	}
	majorIssue := inspection.DiagnosticIssue{
		Component:   "release",
		Severity:    inspection.SeverityMajor,
		Description: "release state is broken",
		Location:    "release_state",
		LikelyCause: "Releases were not created correctly",
	}
	criticalIssue := inspection.DiagnosticIssue{
		Component:   "runtime",
		Severity:    inspection.SeverityCritical,
		Description: "active symlink does not exist",
		Location:    "active_symlink (.anvil/active)",
		LikelyCause: "Runtime was not provisioned",
	}

	tests := []struct {
		name   string
		result inspection.DiagnosticResult
		want   inspection.HealthStatus
	}{
		{
			name:   "no issues → healthy",
			result: inspection.DiagnosticResult{Issues: nil, Passed: true},
			want:   inspection.HealthStatusHealthy,
		},
		{
			name:   "minor-only issues → degraded",
			result: inspection.DiagnosticResult{Issues: []inspection.DiagnosticIssue{minorIssue, majorIssue}, Passed: false},
			want:   inspection.HealthStatusDegraded,
		},
		{
			name:   "any critical → unhealthy",
			result: inspection.DiagnosticResult{Issues: []inspection.DiagnosticIssue{minorIssue, criticalIssue}, Passed: false},
			want:   inspection.HealthStatusUnhealthy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := NewDiagnosticView(tt.result)
			if view.Status != tt.want {
				t.Errorf("NewDiagnosticView() status = %q, want %q", view.Status, tt.want)
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
	view := NewDiagnosticView(inspection.DiagnosticResult{
		Issues:  nil,
		Passed:  true,
		Summary: "No issues detected across all inspected components",
	})

	var buf bytes.Buffer
	FormatDiagnosticView(&buf, view)
	got := buf.String()

	if !strings.Contains(got, "Health: HEALTHY") {
		t.Errorf("output should contain health header, got:\n%s", got)
	}
	if !strings.Contains(got, "Summary: No issues detected across all inspected components") {
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
	view := NewDiagnosticView(inspection.DiagnosticResult{
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
		Passed:  false,
		Summary: "2 issue(s) detected: 1 major, 1 minor",
		Recommendations: []inspection.Recommendation{
			{
				Action:    "Ensure the Runtime is provisioned and ready",
				OwnerEpic: "EPIC-005",
				IssueType: inspection.IssueTypeRuntime,
			},
			{
				Action:    "Set the required configuration key in your project configuration",
				OwnerEpic: "EPIC-002",
				IssueType: inspection.IssueTypeConfiguration,
			},
		},
	})

	var buf bytes.Buffer
	FormatDiagnosticView(&buf, view)
	got := buf.String()

	if !strings.Contains(got, "Health: DEGRADED") {
		t.Errorf("output should contain degraded health header, got:\n%s", got)
	}
	if !strings.Contains(got, "Summary: 2 issue(s) detected: 1 major, 1 minor") {
		t.Errorf("output should contain summary line, got:\n%s", got)
	}
	if !strings.Contains(got, "Issues (2):") {
		t.Errorf("output should contain issues section header with count, got:\n%s", got)
	}
	if !strings.Contains(got, "  [MAJOR] runtime — active_symlink (.anvil/active)") {
		t.Errorf("output should contain issue line with severity and location, got:\n%s", got)
	}
	if !strings.Contains(got, "    The active symlink is broken") {
		t.Errorf("output should contain issue description, got:\n%s", got)
	}
	if !strings.Contains(got, "    Cause: Runtime was not provisioned") {
		t.Errorf("output should contain cause line, got:\n%s", got)
	}
	// ST-P9-03: the first issue carries its own actionable "How:" step
	// (recommendation paired by index) directly under the cause.
	if !strings.Contains(got, "    How: Ensure the Runtime is provisioned and ready [EPIC-005]") {
		t.Errorf("output should contain the first issue's How line with action and epic, got:\n%s", got)
	}
	if !strings.Contains(got, "  [MINOR] release — release_state") {
		t.Errorf("output should contain minor issue line, got:\n%s", got)
	}
	if !strings.Contains(got, "    Cause: Releases were not created correctly") {
		t.Errorf("output should contain the second issue's cause line, got:\n%s", got)
	}
	// ST-P9-03: the second issue carries its own "How:" step.
	if !strings.Contains(got, "    How: Set the required configuration key in your project configuration [EPIC-002]") {
		t.Errorf("output should contain the second issue's How line with action and epic, got:\n%s", got)
	}
	if !strings.Contains(got, "Recommendations (2):") {
		t.Errorf("output should contain recommendations section header with count, got:\n%s", got)
	}
	if !strings.Contains(got, "  - Ensure the Runtime is provisioned and ready [EPIC-005]") {
		t.Errorf("output should contain recommendation line with action and epic, got:\n%s", got)
	}
	if !strings.Contains(got, "Set the required configuration key in your project configuration [EPIC-002]") {
		t.Errorf("output should contain EPIC-002 recommendation, got:\n%s", got)
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
	view := NewDiagnosticView(inspection.DiagnosticResult{
		Issues: []inspection.DiagnosticIssue{
			{
				Component:   "runtime",
				Severity:    inspection.SeverityCritical,
				Description: "The active symlink does not exist",
				Location:    "active_symlink (.anvil/active)",
				LikelyCause: "Runtime was not provisioned",
			},
		},
		Passed:  false,
		Summary: "1 issue(s) detected: 1 critical",
	})

	var buf bytes.Buffer
	FormatDiagnosticView(&buf, view)
	got := buf.String()

	if !strings.Contains(got, "Health: UNHEALTHY") {
		t.Errorf("output should contain unhealthy health header, got:\n%s", got)
	}
	if !strings.Contains(got, "  [CRITICAL] runtime — active_symlink (.anvil/active)") {
		t.Errorf("output should contain critical issue line, got:\n%s", got)
	}
	// ST-P9-03: without a paired recommendation the issue must NOT render
	// a "How:" line — the recommendation section is absent entirely.
	if strings.Contains(got, "    How:") {
		t.Errorf("output should not contain a How line when no recommendations are paired, got:\n%s", got)
	}
	if strings.Contains(got, "Recommendations (") {
		t.Errorf("output should not contain recommendations section without recommendations, got:\n%s", got)
	}
}

// TestFormatDiagnosticView_IssueWithoutRecommendationOmitsHow verifies
// that an issue without a paired recommendation (e.g. a view built
// manually with issues only, like server doctor) renders without a
// "How:" line — the pairing is index-based and best-effort.
//
// Reference: ST-P9-03
func TestFormatDiagnosticView_IssueWithoutRecommendationOmitsHow(t *testing.T) {
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
		// Recommendations intentionally absent (server_doctor scenario).
	}

	var buf bytes.Buffer
	FormatDiagnosticView(&buf, view)
	got := buf.String()

	if !strings.Contains(got, "Issues (2):") {
		t.Errorf("output should contain issues section header with count, got:\n%s", got)
	}
	if strings.Contains(got, "    How:") {
		t.Errorf("output should not contain a How line without paired recommendations, got:\n%s", got)
	}
	if strings.Contains(got, "Recommendations (") {
		t.Errorf("output should not contain recommendations section without recommendations, got:\n%s", got)
	}
}

func TestWriteDiagnosticJSON_EnvelopeAndData(t *testing.T) {
	diagnosticView := NewDiagnosticView(inspection.DiagnosticResult{
		Issues: []inspection.DiagnosticIssue{
			{
				Component:   "runtime",
				Severity:    inspection.SeverityCritical,
				Description: "The active symlink does not exist",
				Location:    "active_symlink (.anvil/active)",
				LikelyCause: "Runtime was not provisioned",
			},
		},
		Passed:  false,
		Summary: "1 issue(s) detected: 1 critical",
		Recommendations: []inspection.Recommendation{
			{
				Action:    "Ensure the Runtime is provisioned and ready",
				OwnerEpic: "EPIC-005",
				IssueType: inspection.IssueTypeRuntime,
			},
		},
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
		{name: "diagnostic view", view: diagnosticView, want: "unhealthy"},
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

			if tt.view.Issues != nil {
				issues, ok := data["issues"].([]interface{})
				if !ok || len(issues) == 0 {
					t.Errorf("data.issues should be a non-empty array, got: %v", data["issues"])
				}
			}
			if tt.view.Recommendations != nil {
				recommendations, ok := data["recommendations"].([]interface{})
				if !ok || len(recommendations) == 0 {
					t.Errorf("data.recommendations should be a non-empty array, got: %v", data["recommendations"])
				}
			}
			if tt.view.Components != nil {
				components, ok := data["components"].([]interface{})
				if !ok || len(components) == 0 {
					t.Errorf("data.components should be a non-empty array, got: %v", data["components"])
				}
			}
		})
	}
}

func TestWriteDiagnosticJSON_Deterministic(t *testing.T) {
	view := NewDiagnosticView(inspection.DiagnosticResult{
		Issues: []inspection.DiagnosticIssue{
			{
				Component:   "release",
				Severity:    inspection.SeverityMajor,
				Description: "Release state is invalid",
				Location:    "release_state",
				LikelyCause: "Releases were not created correctly",
			},
		},
		Passed:  false,
		Summary: "1 issue(s) detected: 1 major",
		Recommendations: []inspection.Recommendation{
			{
				Action:    "Check release state using the release status command",
				OwnerEpic: "EPIC-004",
				IssueType: inspection.IssueTypeRelease,
			},
		},
	})

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
