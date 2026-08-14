package inspection

import (
	"testing"

	"maleolabs.com/anvil/internal/config"
)

// TestNewConfigInspector verifies that NewConfigInspector creates a
// non-nil inspector with the given schema.
//
// Reference: TS-009-006
func TestNewConfigInspector(t *testing.T) {
	schema := config.CoreSchema()
	inspector := NewConfigInspector(schema)
	if inspector == nil {
		t.Fatal("NewConfigInspector() returned nil")
	}
}

// TestNewDefaultConfigInspector verifies that NewDefaultConfigInspector
// creates a non-nil inspector using the canonical schema.
//
// Reference: TS-009-006
func TestNewDefaultConfigInspector(t *testing.T) {
	inspector := NewDefaultConfigInspector()
	if inspector == nil {
		t.Fatal("NewDefaultConfigInspector() returned nil")
	}
}

// TestConfigInspector_InspectCompleteness_AllPresent verifies that the
// completeness check passes when all required values are present.
//
// Reference: TS-009-006
func TestConfigInspector_InspectCompleteness_AllPresent(t *testing.T) {
	inspector := NewDefaultConfigInspector()

	resolved := map[string]interface{}{
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

	check := inspector.InspectCompleteness(resolved)

	if !check.Passed {
		t.Errorf("InspectCompleteness().Passed = false, want true; details: %s", check.Details)
	}
	if check.Name != "completeness" {
		t.Errorf("check.Name = %q, want %q", check.Name, "completeness")
	}
}

// TestConfigInspector_InspectCompleteness_MissingRequired verifies that
// the completeness check fails when required values are missing.
//
// Reference: TS-009-006
func TestConfigInspector_InspectCompleteness_MissingRequired(t *testing.T) {
	inspector := NewDefaultConfigInspector()

	// project.name is required but missing.
	resolved := map[string]interface{}{
		"project.version":  "1.0.0",
		"global.log_level": "info",
	}

	check := inspector.InspectCompleteness(resolved)

	if check.Passed {
		t.Errorf("InspectCompleteness().Passed = true, want false (missing project.name)")
	}
}

// TestConfigInspector_InspectCompleteness_EmptyConfig verifies behavior
// with an empty configuration.
//
// Reference: TS-009-006
func TestConfigInspector_InspectCompleteness_EmptyConfig(t *testing.T) {
	inspector := NewDefaultConfigInspector()
	resolved := map[string]interface{}{}

	check := inspector.InspectCompleteness(resolved)

	if check.Passed {
		t.Errorf("InspectCompleteness().Passed = true, want false (empty config)")
	}
}

// TestConfigInspector_InspectValidity_AllValid verifies that the validity
// check passes when all values conform to the schema.
//
// Reference: TS-009-006
func TestConfigInspector_InspectValidity_AllValid(t *testing.T) {
	inspector := NewDefaultConfigInspector()

	resolved := map[string]interface{}{
		"project.name":             "test-project",
		"project.version":          "1.0.0",
		"project.description":      "A test project",
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

	check := inspector.InspectValidity(resolved)

	if !check.Passed {
		t.Errorf("InspectValidity().Passed = false, want true; details: %s", check.Details)
	}
}

// TestConfigInspector_InspectValidity_InvalidType verifies that the
// validity check fails when a value has the wrong type.
//
// Reference: TS-009-006
func TestConfigInspector_InspectValidity_InvalidType(t *testing.T) {
	inspector := NewDefaultConfigInspector()

	resolved := map[string]interface{}{
		"project.name":         "test-project",
		"project.version":      "1.0.0",
		"artifact.manifest":    "yes",  // should be bool
		"release.max_retained": "five", // should be int
	}

	check := inspector.InspectValidity(resolved)

	if check.Passed {
		t.Errorf("InspectValidity().Passed = true, want false (invalid types)")
	}
}

// TestConfigInspector_InspectValidity_InvalidAllowedValue verifies that
// the check fails when a string value is not in the allowed set.
//
// Reference: TS-009-006
func TestConfigInspector_InspectValidity_InvalidAllowedValue(t *testing.T) {
	inspector := NewDefaultConfigInspector()

	resolved := map[string]interface{}{
		"project.name":     "test-project",
		"project.version":  "1.0.0",
		"global.log_level": "verbose", // not in allowed [debug, info, warn, error]
	}

	check := inspector.InspectValidity(resolved)

	if check.Passed {
		t.Errorf("InspectValidity().Passed = true, want false (invalid log_level)")
	}
}

// TestConfigInspector_InspectValidity_InvalidSemver verifies that the
// check fails when project.version is not valid SemVer.
//
// Reference: TS-009-006
func TestConfigInspector_InspectValidity_InvalidSemver(t *testing.T) {
	inspector := NewDefaultConfigInspector()

	resolved := map[string]interface{}{
		"project.name":    "test-project",
		"project.version": "not-a-version",
	}

	check := inspector.InspectValidity(resolved)

	if check.Passed {
		t.Errorf("InspectValidity().Passed = true, want false (invalid semver)")
	}
}

// TestConfigInspector_InspectResolution_NoConflicts verifies that the
// resolution check passes when keys are defined at only one level.
//
// Reference: TS-009-006
func TestConfigInspector_InspectResolution_NoConflicts(t *testing.T) {
	inspector := NewDefaultConfigInspector()

	global := map[string]interface{}{
		"global.log_level": "info",
	}
	project := map[string]interface{}{
		"project.name": "test-project",
	}
	resolver := config.NewResolver(global, project, nil, nil)

	check := inspector.InspectResolution(resolver)

	if !check.Passed {
		t.Errorf("InspectResolution().Passed = false, want true; details: %s", check.Details)
	}
}

// TestConfigInspector_InspectResolution_WithConflicts verifies that the
// resolution check detects cross-level conflicts.
//
// Reference: TS-009-006
func TestConfigInspector_InspectResolution_WithConflicts(t *testing.T) {
	inspector := NewDefaultConfigInspector()

	global := map[string]interface{}{
		"global.log_level": "info",
	}
	project := map[string]interface{}{
		"project.name":     "test-project",
		"global.log_level": "debug", // conflict with global
	}
	resolver := config.NewResolver(global, project, nil, nil)

	check := inspector.InspectResolution(resolver)

	// Conflicts are informational — check should still pass.
	if !check.Passed {
		t.Errorf("InspectResolution().Passed = false, want true (conflicts are informational)")
	}
	// But details should mention the conflict.
	if check.Details == "no cross-level conflicts detected" {
		t.Errorf("InspectResolution().Details = %q, want conflict info", check.Details)
	}
}

// TestConfigInspector_InspectResolution_SameValueNoConflict verifies that
// the same value at multiple levels does not count as a conflict.
//
// Reference: TS-009-006
func TestConfigInspector_InspectResolution_SameValueNoConflict(t *testing.T) {
	inspector := NewDefaultConfigInspector()

	global := map[string]interface{}{
		"global.log_level": "info",
	}
	project := map[string]interface{}{
		"global.log_level": "info", // same value, not a conflict
	}
	resolver := config.NewResolver(global, project, nil, nil)

	check := inspector.InspectResolution(resolver)

	if !check.Passed {
		t.Errorf("InspectResolution().Passed = false, want true")
	}
	if check.Details != "no cross-level conflicts detected" {
		t.Errorf("InspectResolution().Details = %q, want %q", check.Details, "no cross-level conflicts detected")
	}
}

// TestConfigInspector_InspectResolution_EmptyResolver verifies behavior
// with an empty resolver.
//
// Reference: TS-009-006
func TestConfigInspector_InspectResolution_EmptyResolver(t *testing.T) {
	inspector := NewDefaultConfigInspector()
	resolver := config.NewResolver(nil, nil, nil, nil)

	check := inspector.InspectResolution(resolver)

	if !check.Passed {
		t.Errorf("InspectResolution().Passed = false, want true (empty resolver)")
	}
}

// TestConfigInspector_Inspect_AllPassing verifies that Inspect returns a
// passing result when all checks pass.
//
// Reference: TS-009-006
func TestConfigInspector_Inspect_AllPassing(t *testing.T) {
	inspector := NewDefaultConfigInspector()

	resolved := map[string]interface{}{
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

	// Build resolver with the same values at global level only.
	resolver := config.NewResolver(resolved, nil, nil, nil)

	result := inspector.Inspect(resolver)

	if !result.Passed {
		t.Errorf("Inspect().Passed = false, want true")
		for _, c := range result.Checks {
			if !c.Passed {
				t.Logf("  failed check: %s — %s", c.Name, c.Details)
			}
		}
	}
	if result.Component != "config" {
		t.Errorf("Component = %q, want %q", result.Component, "config")
	}
	if len(result.Checks) != 3 {
		t.Errorf("len(Checks) = %d, want 3", len(result.Checks))
	}
}

// TestConfigInspector_Inspect_MissingRequired verifies that Inspect fails
// when required values are missing.
//
// Reference: TS-009-006
func TestConfigInspector_Inspect_MissingRequired(t *testing.T) {
	inspector := NewDefaultConfigInspector()

	// project.name is required but missing.
	resolved := map[string]interface{}{
		"project.version": "1.0.0",
	}
	resolver := config.NewResolver(resolved, nil, nil, nil)

	result := inspector.Inspect(resolver)

	if result.Passed {
		t.Errorf("Inspect().Passed = true, want false (missing required)")
	}
}

// TestConfigInspector_Inspect_MultipleFailures verifies that Inspect
// reports all failures when multiple checks fail.
//
// Reference: TS-009-006
func TestConfigInspector_Inspect_MultipleFailures(t *testing.T) {
	inspector := NewDefaultConfigInspector()

	// Missing required + invalid type.
	resolved := map[string]interface{}{
		"artifact.manifest": "not-a-bool",
	}
	resolver := config.NewResolver(resolved, nil, nil, nil)

	result := inspector.Inspect(resolver)

	if result.Passed {
		t.Errorf("Inspect().Passed = true, want false")
	}

	// Both completeness and validity should fail.
	checkMap := make(map[string]bool)
	for _, c := range result.Checks {
		checkMap[c.Name] = c.Passed
	}

	if checkMap["completeness"] {
		t.Error("completeness check should fail (missing required)")
	}
	if checkMap["validity"] {
		t.Error("validity check should fail (invalid type)")
	}
}
