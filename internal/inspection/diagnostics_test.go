// Package inspection provides read-only diagnostic inspection capabilities
// for Anvil Runtime environments and configuration.
//
// Reference: TS-009-003
package inspection

import (
	"path/filepath"
	"testing"

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

// TestIssuesFromComponents_IssueFieldsComplete verifies that
// IssuesFromComponents converts every failed check into a structured
// issue record with all five required fields populated. The
// DiagnosticEngine that previously produced issues was removed with the
// platform-ops breadth demotion (ADR-036 §3, TS-015-05-02);
// IssuesFromComponents is the remaining issue construction path (used by
// "anvil server doctor").
//
// Reference: TS-009-003 §7
func TestIssuesFromComponents_IssueFieldsComplete(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = filepath.Join(dir, "nonexistent")

	// Build the failing component results the same way the
	// VerificationEngine produces them for a broken environment.
	runtimeInspector := NewRuntimeInspector(cfg)
	releaseInspector := NewReleaseInspector(cfg)
	registryInspector := NewRegistryInspector(filepath.Join(dir, "nonexistent"))
	serverReadinessInspector := NewServerReadinessInspector(filepath.Join(dir, "nonexistent"))

	components := []InspectionResult{
		runtimeInspector.Inspect(),
		releaseInspector.Inspect(),
		registryInspector.Inspect(),
		serverReadinessInspector.Inspect(),
	}

	issues := IssuesFromComponents(components)
	if len(issues) == 0 {
		t.Fatal("expected issues from broken environment")
	}

	for _, issue := range issues {
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

// TestClassifySeverity verifies the deterministic severity mapping over
// failed check details.
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
