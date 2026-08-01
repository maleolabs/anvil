// Package inspection provides read-only diagnostic inspection capabilities
// for Anvil Runtime environments and configuration.
//
// Reference: TS-009-003
package inspection

import (
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/config"
	"maleolabs.com/anvil/internal/runtime"
)

// TestSeverity_Values verifies that the Severity constants have the
// expected string values.
//
// Reference: TS-009-003 §7
func TestSeverity_Values(t *testing.T) {
	tests := []struct {
		severity Severity
		expected string
	}{
		{SeverityCritical, "critical"},
		{SeverityMajor, "major"},
		{SeverityMinor, "minor"},
	}

	for _, tt := range tests {
		if string(tt.severity) != tt.expected {
			t.Errorf("Severity %v = %q, want %q", tt.severity, string(tt.severity), tt.expected)
		}
	}
}

// TestNewDiagnosticEngine verifies that NewDiagnosticEngine creates a
// non-nil engine with the given inspectors.
//
// Reference: TS-009-003
func TestNewDiagnosticEngine(t *testing.T) {
	engine := NewDiagnosticEngine(nil, nil, nil, nil, nil)
	if engine == nil {
		t.Fatal("NewDiagnosticEngine() returned nil")
	}
}

// TestDiagnosticEngine_Diagnose_HealthyState verifies that Diagnose returns
// no issues when all configured inspectors pass.
//
// Reference: TS-009-003
func TestDiagnosticEngine_Diagnose_HealthyState(t *testing.T) {
	dir := t.TempDir()
	helperCreateValidRuntime(t, dir)
	helperSetupServerConfig(t, dir)

	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	engine := NewDiagnosticEngine(
		NewRuntimeInspector(cfg),
		NewDefaultConfigInspector(),
		NewReleaseInspector(cfg),
		NewRegistryInspector(dir),
		NewServerReadinessInspector(dir),
	)

	resolver := config.NewResolver(helperFullConfig(), nil, nil, nil)

	result := engine.Diagnose("", resolver)

	if !result.Passed {
		t.Errorf("Diagnose().Passed = false, want true")
		for _, issue := range result.Issues {
			t.Logf("  issue: %s [%s] %s", issue.Component, issue.Severity, issue.Description)
		}
	}
	if len(result.Issues) != 0 {
		t.Errorf("len(Issues) = %d, want 0", len(result.Issues))
	}
	if len(result.Recommendations) != 0 {
		t.Errorf("len(Recommendations) = %d, want 0", len(result.Recommendations))
	}
	if result.Summary != "No issues detected across all inspected components" {
		t.Errorf("Summary = %q, want %q", result.Summary, "No issues detected across all inspected components")
	}
}

// TestDiagnosticEngine_Diagnose_RuntimeIssues verifies that failed runtime
// checks are converted into runtime issues with critical severity for
// missing state.
//
// Reference: TS-009-003
func TestDiagnosticEngine_Diagnose_RuntimeIssues(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir // empty temp dir: no runtime state at all

	engine := NewDiagnosticEngine(
		NewRuntimeInspector(cfg),
		nil,
		nil,
		nil,
		nil,
	)

	result := engine.Diagnose("", nil)

	if result.Passed {
		t.Errorf("Diagnose().Passed = true, want false (empty runtime)")
	}
	if len(result.Issues) != 4 {
		t.Fatalf("len(Issues) = %d, want 4 (all runtime checks fail)", len(result.Issues))
	}

	for _, issue := range result.Issues {
		if issue.Component != "runtime" {
			t.Errorf("issue.Component = %q, want %q", issue.Component, "runtime")
		}
		// Missing runtime state is critical.
		if issue.Severity != SeverityCritical {
			t.Errorf("issue[%s].Severity = %q, want %q", issue.Location, issue.Severity, SeverityCritical)
		}
	}

	// The active symlink issue must identify its location.
	found := false
	for _, issue := range result.Issues {
		if strings.HasPrefix(issue.Location, "active_symlink") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected an issue located at active_symlink")
	}
}

// TestDiagnosticEngine_Diagnose_ConfigIssues verifies that failed
// configuration checks are converted into config issues: missing required
// values are critical, invalid values are major.
//
// Reference: TS-009-003
func TestDiagnosticEngine_Diagnose_ConfigIssues(t *testing.T) {
	engine := NewDiagnosticEngine(
		nil,
		NewDefaultConfigInspector(),
		nil,
		nil,
		nil,
	)

	// Missing required values + one invalid value.
	resolver := config.NewResolver(map[string]interface{}{
		"project.version":   "1.0.0",
		"artifact.manifest": "not-a-bool",
	}, nil, nil, nil)

	result := engine.Diagnose("", resolver)

	if result.Passed {
		t.Errorf("Diagnose().Passed = true, want false (invalid config)")
	}
	if len(result.Issues) != 2 {
		t.Fatalf("len(Issues) = %d, want 2 (completeness + validity)", len(result.Issues))
	}

	severityByLocation := make(map[string]Severity)
	for _, issue := range result.Issues {
		if issue.Component != "config" {
			t.Errorf("issue.Component = %q, want %q", issue.Component, "config")
		}
		for _, prefix := range []string{"completeness", "validity"} {
			if strings.HasPrefix(issue.Location, prefix) {
				severityByLocation[prefix] = issue.Severity
			}
		}
	}

	if severityByLocation["completeness"] != SeverityCritical {
		t.Errorf("completeness severity = %q, want %q (missing required values)", severityByLocation["completeness"], SeverityCritical)
	}
	if severityByLocation["validity"] != SeverityMajor {
		t.Errorf("validity severity = %q, want %q (invalid values)", severityByLocation["validity"], SeverityMajor)
	}
}

// TestDiagnosticEngine_Diagnose_ReleaseIssues verifies that failed release
// checks are converted into release issues.
//
// Reference: TS-009-003
func TestDiagnosticEngine_Diagnose_ReleaseIssues(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = filepath.Join(dir, "nonexistent")

	engine := NewDiagnosticEngine(
		nil,
		nil,
		NewReleaseInspector(cfg),
		nil,
		nil,
	)

	result := engine.Diagnose("", nil)

	if result.Passed {
		t.Errorf("Diagnose().Passed = true, want false (no release infrastructure)")
	}
	if len(result.Issues) != 2 {
		t.Fatalf("len(Issues) = %d, want 2 (release_directory + artifact_presence)", len(result.Issues))
	}

	for _, issue := range result.Issues {
		if issue.Component != "release" {
			t.Errorf("issue.Component = %q, want %q", issue.Component, "release")
		}
	}
}

// TestDiagnosticEngine_Diagnose_ServerIssues verifies that failed registry
// and server readiness checks are both mapped to the "server" component.
//
// Reference: TS-009-003
func TestDiagnosticEngine_Diagnose_ServerIssues(t *testing.T) {
	dir := t.TempDir()
	missingRoot := filepath.Join(dir, "nonexistent")

	engine := NewDiagnosticEngine(
		nil,
		nil,
		nil,
		NewRegistryInspector(missingRoot),
		NewServerReadinessInspector(missingRoot),
	)

	result := engine.Diagnose("", nil)

	if result.Passed {
		t.Errorf("Diagnose().Passed = true, want false (no server state)")
	}
	if len(result.Issues) < 4 {
		t.Fatalf("len(Issues) = %d, want >= 4 (registry + readiness failures)", len(result.Issues))
	}

	hasRegistryIssue := false
	hasReadinessIssue := false
	for _, issue := range result.Issues {
		if issue.Component != "server" {
			t.Errorf("issue.Component = %q, want %q", issue.Component, "server")
		}
		if strings.HasPrefix(issue.Location, "registry_") {
			hasRegistryIssue = true
		}
		if strings.HasPrefix(issue.Location, "server_config") {
			hasReadinessIssue = true
		}
	}
	if !hasRegistryIssue {
		t.Error("expected at least one registry issue mapped to server component")
	}
	if !hasReadinessIssue {
		t.Error("expected at least one server readiness issue mapped to server component")
	}
}

// TestDiagnosticEngine_Diagnose_NilInspectors verifies that Diagnose
// handles nil inspectors gracefully and reports a healthy result.
//
// Reference: TS-009-003
func TestDiagnosticEngine_Diagnose_NilInspectors(t *testing.T) {
	engine := NewDiagnosticEngine(nil, nil, nil, nil, nil)

	result := engine.Diagnose("", nil)

	if !result.Passed {
		t.Errorf("Diagnose().Passed = false, want true (no inspectors)")
	}
	if len(result.Issues) != 0 {
		t.Errorf("len(Issues) = %d, want 0", len(result.Issues))
	}
	if len(result.Recommendations) != 0 {
		t.Errorf("len(Recommendations) = %d, want 0", len(result.Recommendations))
	}
}

// TestDiagnosticEngine_Diagnose_NilConfigResolver verifies that config
// inspection is skipped (without panicking) when the config inspector is
// configured but the resolver is nil.
//
// Reference: TS-009-003
func TestDiagnosticEngine_Diagnose_NilConfigResolver(t *testing.T) {
	engine := NewDiagnosticEngine(
		nil,
		NewDefaultConfigInspector(),
		nil,
		nil,
		nil,
	)

	result := engine.Diagnose("", nil)

	if !result.Passed {
		t.Errorf("Diagnose().Passed = false, want true (config inspection skipped)")
	}
	if len(result.Issues) != 0 {
		t.Errorf("len(Issues) = %d, want 0", len(result.Issues))
	}
}

// TestDiagnosticEngine_Diagnose_IssueFieldsComplete verifies that every
// issue record contains all five required fields.
//
// Reference: TS-009-003 §7
func TestDiagnosticEngine_Diagnose_IssueFieldsComplete(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = filepath.Join(dir, "nonexistent")

	engine := NewDiagnosticEngine(
		NewRuntimeInspector(cfg),
		NewDefaultConfigInspector(),
		NewReleaseInspector(cfg),
		NewRegistryInspector(filepath.Join(dir, "nonexistent")),
		NewServerReadinessInspector(filepath.Join(dir, "nonexistent")),
	)

	// Config inspection is skipped (nil resolver); all other components fail.
	result := engine.Diagnose("", nil)

	if result.Passed {
		t.Fatal("Diagnose().Passed = true, want false (all components broken)")
	}
	if len(result.Issues) == 0 {
		t.Fatal("expected issues from broken environment")
	}

	for _, issue := range result.Issues {
		if issue.Component == "" {
			t.Error("issue.Component is empty")
		}
		switch issue.Severity {
		case SeverityCritical, SeverityMajor, SeverityMinor:
		default:
			t.Errorf("issue.Severity = %q, want critical/major/minor", issue.Severity)
		}
		if issue.Description == "" {
			t.Error("issue.Description is empty")
		}
		if issue.Location == "" {
			t.Error("issue.Location is empty")
		}
		if issue.LikelyCause == "" {
			t.Error("issue.LikelyCause is empty")
		}
	}
}

// TestDiagnosticEngine_Diagnose_SummaryFormat verifies the summary
// includes a severity breakdown when issues exist.
//
// Reference: TS-009-003
func TestDiagnosticEngine_Diagnose_SummaryFormat(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = filepath.Join(dir, "nonexistent")

	engine := NewDiagnosticEngine(
		NewRuntimeInspector(cfg),
		nil,
		nil,
		nil,
		nil,
	)

	result := engine.Diagnose("", nil)

	if result.Summary == "" {
		t.Fatal("Summary should not be empty")
	}
	if !strings.Contains(result.Summary, "issue(s) detected") {
		t.Errorf("Summary = %q, want severity breakdown", result.Summary)
	}
	if !strings.Contains(result.Summary, "critical") {
		t.Errorf("Summary = %q, want critical count", result.Summary)
	}
}

// TestDiagnosticEngine_Diagnose_RecommendationsAttached verifies the
// TS-009-004 integration: issues automatically produce recommendations
// attached to the diagnostic result.
//
// Reference: TS-009-003 §6, TS-009-004 §3
func TestDiagnosticEngine_Diagnose_RecommendationsAttached(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = filepath.Join(dir, "nonexistent")

	engine := NewDiagnosticEngine(
		NewRuntimeInspector(cfg),
		nil,
		NewReleaseInspector(cfg),
		nil,
		nil,
	)

	result := engine.Diagnose("", nil)

	if len(result.Issues) == 0 {
		t.Fatal("expected issues from broken environment")
	}
	if len(result.Recommendations) != len(result.Issues) {
		t.Errorf("len(Recommendations) = %d, want %d (one per issue)", len(result.Recommendations), len(result.Issues))
	}

	for _, rec := range result.Recommendations {
		if rec.Action == "" {
			t.Error("recommendation Action is empty")
		}
		if rec.OwnerEpic == "" {
			t.Error("recommendation OwnerEpic is empty")
		}
		if rec.IssueType == "" {
			t.Error("recommendation IssueType is empty")
		}
	}
}

// TestDiagnosticEngine_Diagnose_LikelyCauseContent verifies that each
// issue's LikelyCause includes the failed check name as evidence and the
// owning Epic reference for the component.
//
// Reference: TS-009-003 §7
func TestDiagnosticEngine_Diagnose_LikelyCauseContent(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = filepath.Join(dir, "nonexistent")

	engine := NewDiagnosticEngine(
		NewRuntimeInspector(cfg),
		NewDefaultConfigInspector(),
		nil,
		NewRegistryInspector(filepath.Join(dir, "nonexistent")),
		nil,
	)

	// Invalid config values to produce configuration issues.
	resolver := config.NewResolver(map[string]interface{}{
		"artifact.manifest": "not-a-bool",
	}, nil, nil, nil)

	result := engine.Diagnose("", resolver)

	if len(result.Issues) == 0 {
		t.Fatal("expected issues from broken environment")
	}

	// The Epic referenced in LikelyCause is the one that owns the
	// resolution for the component (see componentCause).
	expectedEpicByComponent := map[string]string{
		"runtime": "EPIC-005",
		"config":  "EPIC-002",
		"server":  "EPIC-005",
	}

	for _, issue := range result.Issues {
		// The failed check name is the leading part of Location and must
		// appear in the LikelyCause evidence.
		checkName := strings.Split(issue.Location, " ")[0]
		if !strings.Contains(issue.LikelyCause, checkName) {
			t.Errorf("LikelyCause = %q, want it to contain check name %q", issue.LikelyCause, checkName)
		}

		expectedEpic, ok := expectedEpicByComponent[issue.Component]
		if !ok {
			continue
		}
		if !strings.Contains(issue.LikelyCause, expectedEpic) {
			t.Errorf("LikelyCause = %q, want it to contain %q for component %q", issue.LikelyCause, expectedEpic, issue.Component)
		}
	}
}

// TestClassifySeverity verifies the severity heuristic for failed checks.
//
// Reference: TS-009-003 §7
func TestClassifySeverity(t *testing.T) {
	tests := []struct {
		name  string
		check InspectionCheck
		want  Severity
	}{
		{
			name:  "missing active symlink is critical",
			check: InspectionCheck{Name: "active_symlink", Passed: false, Details: "active symlink does not exist at /opt/anvil/active"},
			want:  SeverityCritical,
		},
		{
			name:  "runtime config not found is critical",
			check: InspectionCheck{Name: "runtime_config", Passed: false, Details: "no runtime config found at /opt/anvil/config.yaml or /opt/anvil/.anvil/config.yaml"},
			want:  SeverityCritical,
		},
		{
			name:  "missing required values is critical",
			check: InspectionCheck{Name: "completeness", Passed: false, Details: "missing required values:\nproject.name: expected string, got <nil>"},
			want:  SeverityCritical,
		},
		{
			name:  "missing directory from stat error is critical",
			check: InspectionCheck{Name: "release_directories", Passed: false, Details: "releases directory /opt/anvil/releases: stat /opt/anvil/releases: no such file or directory"},
			want:  SeverityCritical,
		},
		{
			name:  "missing install root in registry file is critical",
			check: InspectionCheck{Name: "registry_files", Passed: false, Details: "invalid registry files: proj1 (install_root /opt/proj1: stat /opt/proj1: no such file or directory)"},
			want:  SeverityCritical,
		},
		{
			name:  "invalid config values are major",
			check: InspectionCheck{Name: "validity", Passed: false, Details: "2 invalid value(s): project.name: expected string, got 123; artifact.manifest: expected boolean, got yes"},
			want:  SeverityMajor,
		},
		{
			name:  "release dir not a directory is major",
			check: InspectionCheck{Name: "release_directory", Passed: false, Details: "release path /opt/anvil/releases exists but is not a directory"},
			want:  SeverityMajor,
		},
		{
			name:  "release dirs without artifacts are major",
			check: InspectionCheck{Name: "artifact_presence", Passed: false, Details: "release directories without artifacts: release-1; release-2"},
			want:  SeverityMajor,
		},
		{
			name:  "broken shared links are major",
			check: InspectionCheck{Name: "shared_links", Passed: false, Details: "broken shared links: proj1: shared path .anvil/shared: permission denied"},
			want:  SeverityMajor,
		},
		{
			name:  "unreadable registry files are major",
			check: InspectionCheck{Name: "registry_files", Passed: false, Details: "invalid registry files: proj1 (unreadable: permission denied)"},
			want:  SeverityMajor,
		},
		{
			name:  "consistency issues are major",
			check: InspectionCheck{Name: "registry_consistency", Passed: false, Details: "consistency issues: duplicate project ID \"x\" found in a.yaml and b.yaml; orphaned directory: c (no matching c.yaml)"},
			want:  SeverityMajor,
		},
		{
			name:  "unclassified failure is minor",
			check: InspectionCheck{Name: "some_check", Passed: false, Details: "unexpected state encountered during inspection"},
			want:  SeverityMinor,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifySeverity(tt.check)
			if got != tt.want {
				t.Errorf("classifySeverity(%q) = %q, want %q", tt.check.Details, got, tt.want)
			}
		})
	}
}

// TestExtractLocation verifies the issue location extraction from check
// name and detail evidence.
//
// Reference: TS-009-003 §7
func TestExtractLocation(t *testing.T) {
	tests := []struct {
		name    string
		check   InspectionCheck
		wantLoc string
	}{
		{
			name:    "absolute path extracted",
			check:   InspectionCheck{Name: "active_symlink", Details: "active symlink does not exist at /opt/anvil/active"},
			wantLoc: "active_symlink (/opt/anvil/active)",
		},
		{
			name:    "config key extracted",
			check:   InspectionCheck{Name: "validity", Details: "2 invalid value(s): project.name: expected string, got 123"},
			wantLoc: "validity (project.name)",
		},
		{
			name:    "no identifiable location uses check name",
			check:   InspectionCheck{Name: "artifact_presence", Details: "release directories without artifacts: release-1"},
			wantLoc: "artifact_presence",
		},
		{
			name:    "shared path extracted",
			check:   InspectionCheck{Name: "shared_links", Details: "broken shared links: proj1: shared path .anvil/shared: permission denied"},
			wantLoc: "shared_links (.anvil/shared)",
		},
		{
			name:    "tilde path extracted",
			check:   InspectionCheck{Name: "runtime_config", Details: "runtime config not found at ~/.anvil/config.yaml"},
			wantLoc: "runtime_config (~/.anvil/config.yaml)",
		},
		{
			name:    "dot-prefixed path extracted",
			check:   InspectionCheck{Name: "active_symlink", Details: "symlink target missing at .anvil/active"},
			wantLoc: "active_symlink (.anvil/active)",
		},
		{
			name:    "version token is not a location",
			check:   InspectionCheck{Name: "validity", Details: "expected semver version, got v1.0.0"},
			wantLoc: "validity",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractLocation(tt.check)
			if got != tt.wantLoc {
				t.Errorf("extractLocation() = %q, want %q", got, tt.wantLoc)
			}
		})
	}
}

// TestIsPathLike verifies the location token heuristic, including the
// rejection of version tokens that resemble dot-separated identifiers.
//
// Reference: TS-009-003 §7
func TestIsPathLike(t *testing.T) {
	tests := []struct {
		token string
		want  bool
	}{
		// Paths and path-prefixed entries are always locations.
		{"/opt/anvil/active", true},
		{"~/.anvil/config.yaml", true},
		{".anvil/shared", true},
		{".gitignore", true},
		// Dot-separated identifiers: config keys and file names.
		{"project.name", true},
		{"config.yaml", true},
		{"release-1.yaml", true},
		// Version tokens are values, not locations.
		{"1.0.0", false},
		{"v1.0.0", false},
		{"0.1.4", false},
		// Plain words and empty tokens are not locations.
		{"release-1", false},
		{"expected", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.token, func(t *testing.T) {
			got := isPathLike(tt.token)
			if got != tt.want {
				t.Errorf("isPathLike(%q) = %v, want %v", tt.token, got, tt.want)
			}
		})
	}
}

// helperFullConfig returns a fully valid configuration map for the
// canonical CoreSchema, used to build healthy config resolvers.
func helperFullConfig() map[string]interface{} {
	return map[string]interface{}{
		"project.name":             "test-project",
		"project.version":          "1.0.0",
		"project.description":      "",
		"artifact.include":         []interface{}{"**/*"},
		"artifact.exclude":         []interface{}{".git/**"},
		"artifact.output":          ".anvil/artifacts",
		"artifact.manifest":        true,
		"release.max_retained":     5,
		"release.retention_policy": "keep-last",
		"release.auto_verify":      true,
		"release.version_schema":   "semver",
		"runtime.install_root":     ".anvil/releases",
		"runtime.shared_resources": ".anvil/shared",
		"runtime.active_symlink":   ".anvil/active",
		"runtime.temp_dir":         ".anvil/tmp",
		"global.log_level":         "info",
		"global.output_format":     "human",
		"global.no_color":          false,
		"global.auto_progress":     true,
	}
}
