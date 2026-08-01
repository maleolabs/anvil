// Package inspection provides read-only diagnostic inspection capabilities
// for Anvil Runtime environments and configuration.
//
// Reference: TS-009-004
package inspection

import (
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/runtime"
)

// TestNewRecommendationEngine verifies that NewRecommendationEngine
// creates a non-nil engine.
//
// Reference: TS-009-004
func TestNewRecommendationEngine(t *testing.T) {
	engine := NewRecommendationEngine()
	if engine == nil {
		t.Fatal("NewRecommendationEngine() returned nil")
	}
}

// TestRecommendationEngine_RecommendationsFor_EmptyIssues verifies that no
// issues produce no recommendations.
//
// Reference: TS-009-004
func TestRecommendationEngine_RecommendationsFor_EmptyIssues(t *testing.T) {
	engine := NewRecommendationEngine()

	recommendations := engine.RecommendationsFor(nil)
	if len(recommendations) != 0 {
		t.Errorf("len(RecommendationsFor(nil)) = %d, want 0", len(recommendations))
	}

	recommendations = engine.RecommendationsFor([]DiagnosticIssue{})
	if len(recommendations) != 0 {
		t.Errorf("len(RecommendationsFor(empty)) = %d, want 0", len(recommendations))
	}
}

// TestRecommendationEngine_RecommendationsFor_Mapping verifies that each
// issue category maps to the correct action, owning Epic, and issue type.
//
// Reference: TS-009-004 §7
func TestRecommendationEngine_RecommendationsFor_Mapping(t *testing.T) {
	tests := []struct {
		name         string
		issue        DiagnosticIssue
		wantAction   string
		wantEpic     string
		wantIssueTyp IssueType
	}{
		{
			name:         "configuration issue maps to EPIC-002",
			issue:        DiagnosticIssue{Component: "config", Severity: SeverityCritical, Location: "completeness (project.name)"},
			wantAction:   "Set the required configuration key in your project configuration",
			wantEpic:     "EPIC-002",
			wantIssueTyp: IssueTypeConfiguration,
		},
		{
			name:         "runtime issue maps to EPIC-005",
			issue:        DiagnosticIssue{Component: "runtime", Severity: SeverityCritical, Location: "active_symlink (/opt/anvil/active)"},
			wantAction:   "Ensure the Runtime is provisioned and ready",
			wantEpic:     "EPIC-005",
			wantIssueTyp: IssueTypeRuntime,
		},
		{
			name:         "artifact issue maps to EPIC-003",
			issue:        DiagnosticIssue{Component: "release", Severity: SeverityMajor, Location: "artifact_presence"},
			wantAction:   "Run artifact verification to confirm integrity",
			wantEpic:     "EPIC-003",
			wantIssueTyp: IssueTypeArtifact,
		},
		{
			name:         "release issue maps to EPIC-004",
			issue:        DiagnosticIssue{Component: "release", Severity: SeverityCritical, Location: "release_directory (/opt/anvil/releases)"},
			wantAction:   "Check release state using the release status command",
			wantEpic:     "EPIC-004",
			wantIssueTyp: IssueTypeRelease,
		},
		{
			name:         "server registry issue maps to EPIC-005",
			issue:        DiagnosticIssue{Component: "server", Severity: SeverityCritical, Location: "registry_files (/opt/anvil/projects/proj1.yaml)"},
			wantAction:   "Verify project registries are present and valid in the server registry store",
			wantEpic:     "EPIC-005",
			wantIssueTyp: IssueTypeServer,
		},
		{
			name:         "server config issue maps to EPIC-005",
			issue:        DiagnosticIssue{Component: "server", Severity: SeverityCritical, Location: "server_config (/opt/anvil/.anvil/config.yaml)"},
			wantAction:   "Ensure the server configuration is valid and the server runtime is initialized",
			wantEpic:     "EPIC-005",
			wantIssueTyp: IssueTypeServer,
		},
		{
			name:         "server readiness issue maps to EPIC-005",
			issue:        DiagnosticIssue{Component: "server", Severity: SeverityMajor, Location: "install_roots"},
			wantAction:   "Ensure the server runtime is provisioned and ready",
			wantEpic:     "EPIC-005",
			wantIssueTyp: IssueTypeServer,
		},
		{
			name:         "unknown component falls back to EPIC-009",
			issue:        DiagnosticIssue{Component: "unknown_component", Severity: SeverityMinor, Location: "mystery_check"},
			wantAction:   "Inspect the unknown_component component state",
			wantEpic:     "EPIC-009",
			wantIssueTyp: IssueTypeServer,
		},
	}

	engine := NewRecommendationEngine()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recommendations := engine.RecommendationsFor([]DiagnosticIssue{tt.issue})

			if len(recommendations) != 1 {
				t.Fatalf("len(RecommendationsFor) = %d, want 1", len(recommendations))
			}

			rec := recommendations[0]
			if rec.Action != tt.wantAction {
				t.Errorf("Action = %q, want %q", rec.Action, tt.wantAction)
			}
			if rec.OwnerEpic != tt.wantEpic {
				t.Errorf("OwnerEpic = %q, want %q", rec.OwnerEpic, tt.wantEpic)
			}
			if rec.IssueType != tt.wantIssueTyp {
				t.Errorf("IssueType = %q, want %q", rec.IssueType, tt.wantIssueTyp)
			}
		})
	}
}

// TestRecommendationEngine_RecommendationsFor_OnePerIssue verifies that
// every issue produces exactly one recommendation.
//
// Reference: TS-009-004 §3
func TestRecommendationEngine_RecommendationsFor_OnePerIssue(t *testing.T) {
	engine := NewRecommendationEngine()

	issues := []DiagnosticIssue{
		{Component: "config", Location: "validity (project.name)"},
		{Component: "runtime", Location: "shared_resources (/opt/anvil/shared)"},
		{Component: "release", Location: "artifact_presence"},
		{Component: "server", Location: "registry_consistency"},
	}

	recommendations := engine.RecommendationsFor(issues)

	if len(recommendations) != len(issues) {
		t.Fatalf("len(RecommendationsFor) = %d, want %d", len(recommendations), len(issues))
	}

	epics := map[int]string{
		0: "EPIC-002",
		1: "EPIC-005",
		2: "EPIC-003",
		3: "EPIC-005",
	}
	for i, rec := range recommendations {
		if rec.OwnerEpic != epics[i] {
			t.Errorf("recommendation[%d].OwnerEpic = %q, want %q", i, rec.OwnerEpic, epics[i])
		}
	}
}

// TestDiagnosticEngine_Diagnose_AutomaticRecommendations verifies the
// TS-009-004 integration end to end: diagnosing a broken environment
// automatically produces one recommendation per issue with the owning
// Epic reference.
//
// Reference: TS-009-004 §3, TS-009-003 §6
func TestDiagnosticEngine_Diagnose_AutomaticRecommendations(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = filepath.Join(dir, "nonexistent")

	engine := NewDiagnosticEngine(
		NewRuntimeInspector(cfg),
		nil,
		nil,
		NewRegistryInspector(filepath.Join(dir, "nonexistent")),
		nil,
	)

	result := engine.Diagnose("", nil)

	if len(result.Issues) == 0 {
		t.Fatal("expected issues from broken environment")
	}
	if len(result.Recommendations) != len(result.Issues) {
		t.Fatalf("len(Recommendations) = %d, want %d", len(result.Recommendations), len(result.Issues))
	}

	// Every recommendation must be actionable and reference an owning Epic.
	for i, rec := range result.Recommendations {
		if rec.Action == "" {
			t.Errorf("recommendation[%d].Action is empty", i)
		}
		if rec.OwnerEpic == "" {
			t.Errorf("recommendation[%d].OwnerEpic is empty", i)
		}
	}

	// Runtime issues must reference EPIC-005, server issues EPIC-005.
	for i, issue := range result.Issues {
		rec := result.Recommendations[i]
		switch issue.Component {
		case "runtime", "server":
			if rec.OwnerEpic != "EPIC-005" {
				t.Errorf("issue %q recommendation OwnerEpic = %q, want EPIC-005", issue.Location, rec.OwnerEpic)
			}
		}
	}
}
