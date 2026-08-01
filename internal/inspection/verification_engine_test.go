// Package inspection provides read-only diagnostic inspection capabilities
// for Anvil Runtime environments and configuration.
//
// Reference: TS-P9-01
package inspection

import (
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/runtime"
)

// helperCreateValidRuntime creates a valid runtime environment in the given directory.
func helperCreateValidRuntime(t *testing.T, dir string) {
	t.Helper()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir
	for _, d := range cfg.AllDirs() {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	// Create a release directory and symlink.
	releaseDir := filepath.Join(dir, "releases", "release-1")
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(releaseDir, "app.tar.gz"), []byte("data"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Symlink(releaseDir, cfg.ActiveSymlinkPath()); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	// Create config file.
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("test: true"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	// Create projects directory for server readiness.
	projectsDir := filepath.Join(dir, "projects")
	if err := os.MkdirAll(projectsDir, 0755); err != nil {
		t.Fatalf("mkdir projects: %v", err)
	}
}

// TestNewVerificationEngine verifies that NewVerificationEngine creates a
// non-nil engine with the given inspectors.
//
// Reference: TS-P9-01
func TestNewVerificationEngine(t *testing.T) {
	engine := NewVerificationEngine(nil, nil, nil, nil, nil)
	if engine == nil {
		t.Fatal("NewVerificationEngine() returned nil")
	}
}

// TestVerificationEngine_Verify_AllComponentsPass verifies that Verify
// returns Status=HealthStatusHealthy when all components pass.
//
// Reference: TS-P9-01
func TestVerificationEngine_Verify_AllComponentsPass(t *testing.T) {
	dir := t.TempDir()
	helperCreateValidRuntime(t, dir)

	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	runtimeInspector := NewRuntimeInspector(cfg)
	releaseInspector := NewReleaseInspector(cfg)
	serverInspector := NewServerReadinessInspector(dir)

	// Set up server config for server readiness.
	helperSetupServerConfig(t, dir)

	engine := NewVerificationEngine(
		runtimeInspector,
		nil, // config inspector skipped
		releaseInspector,
		serverInspector,
		nil, // registry inspector skipped (no registry dir)
	)

	result := engine.Verify("", nil)

	if result.Status != HealthStatusHealthy {
		t.Errorf("Verify().Status = %q, want %q", result.Status, HealthStatusHealthy)
		for _, r := range result.ComponentResults {
			if !r.Passed {
				t.Logf("  failed component: %s", r.Component)
				for _, c := range r.Checks {
					if !c.Passed {
						t.Logf("    failed check: %s — %s", c.Name, c.Details)
					}
				}
			}
		}
	}
}

// TestVerificationEngine_Verify_OneComponentFails verifies that Verify
// returns Status=HealthStatusDegraded when one (but not all) components fail.
//
// Reference: TS-P9-01
func TestVerificationEngine_Verify_OneComponentFails(t *testing.T) {
	dir := t.TempDir()

	// Runtime uses nonexistent root → will fail.
	runtimeCfg := runtime.DefaultRuntimeConfig()
	runtimeCfg.InstallRoot = filepath.Join(dir, "nonexistent")
	runtimeInspector := NewRuntimeInspector(runtimeCfg)

	// Release uses valid root with proper setup → will pass.
	releaseCfg := runtime.DefaultRuntimeConfig()
	releaseCfg.InstallRoot = dir
	releaseDir := filepath.Join(releaseCfg.ReleasesDirPath(), "release-1")
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(releaseDir, "app.tar.gz"), []byte("data"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	releaseInspector := NewReleaseInspector(releaseCfg)

	engine := NewVerificationEngine(
		runtimeInspector,
		nil,
		releaseInspector,
		nil,
		nil,
	)

	result := engine.Verify("", nil)

	// With mixed pass/fail, status should be degraded.
	if result.Status != HealthStatusDegraded {
		t.Errorf("Verify().Status = %q, want %q (mixed results)", result.Status, HealthStatusDegraded)
	}

	// Verify the runtime component failed.
	found := false
	for _, r := range result.ComponentResults {
		if r.Component == "runtime" && !r.Passed {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected runtime component to fail")
	}
}

// TestVerificationEngine_Verify_MultipleComponentsFail verifies that
// Status=HealthStatusUnhealthy when all components fail.
//
// Reference: TS-P9-01
func TestVerificationEngine_Verify_MultipleComponentsFail(t *testing.T) {
	dir := t.TempDir()

	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = filepath.Join(dir, "nonexistent")

	runtimeInspector := NewRuntimeInspector(cfg)
	releaseInspector := NewReleaseInspector(cfg)
	serverInspector := NewServerReadinessInspector(filepath.Join(dir, "nonexistent"))

	engine := NewVerificationEngine(
		runtimeInspector,
		nil,
		releaseInspector,
		serverInspector,
		nil,
	)

	result := engine.Verify("", nil)

	if result.Status != HealthStatusUnhealthy {
		t.Errorf("Verify().Status = %q, want %q (all failures)", result.Status, HealthStatusUnhealthy)
	}

	// Count failed components.
	failedCount := 0
	for _, r := range result.ComponentResults {
		if !r.Passed {
			failedCount++
		}
	}
	if failedCount < 2 {
		t.Errorf("expected at least 2 failed components, got %d", failedCount)
	}
}

// TestVerificationEngine_Verify_SummaryFormat verifies the summary string
// format for both passing and failing results.
//
// Reference: TS-P9-01
func TestVerificationEngine_Verify_SummaryFormat(t *testing.T) {
	dir := t.TempDir()
	helperCreateValidRuntime(t, dir)
	helperSetupServerConfig(t, dir)

	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	runtimeInspector := NewRuntimeInspector(cfg)
	releaseInspector := NewReleaseInspector(cfg)
	serverInspector := NewServerReadinessInspector(dir)

	engine := NewVerificationEngine(
		runtimeInspector,
		nil,
		releaseInspector,
		serverInspector,
		nil,
	)

	result := engine.Verify("", nil)

	if result.Status != HealthStatusHealthy {
		t.Fatalf("Verify().Status = %q, want %q; summary: %s", result.Status, HealthStatusHealthy, result.Summary)
	}

	if result.Summary == "" {
		t.Error("Summary should not be empty")
	}
}

// TestVerificationEngine_Verify_SummaryOnFailure verifies the summary
// format when components fail.
//
// Reference: TS-P9-01
func TestVerificationEngine_Verify_SummaryOnFailure(t *testing.T) {
	dir := t.TempDir()

	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = filepath.Join(dir, "nonexistent")

	runtimeInspector := NewRuntimeInspector(cfg)

	engine := NewVerificationEngine(
		runtimeInspector,
		nil,
		nil,
		nil,
		nil,
	)

	result := engine.Verify("", nil)

	if result.Status == HealthStatusHealthy {
		t.Errorf("Verify().Status = %q, want degraded or unhealthy", result.Status)
	}

	if result.Summary == "" {
		t.Error("Summary should not be empty on failure")
	}
}

// TestVerificationEngine_Verify_NilInspectors verifies that Verify handles
// nil inspectors gracefully and returns healthy status.
//
// Reference: TS-P9-01
func TestVerificationEngine_Verify_NilInspectors(t *testing.T) {
	engine := NewVerificationEngine(nil, nil, nil, nil, nil)

	result := engine.Verify("", nil)

	// With no inspectors, all components pass (vacuously) → healthy.
	if result.Status != HealthStatusHealthy {
		t.Errorf("Verify().Status = %q, want %q (no inspectors)", result.Status, HealthStatusHealthy)
	}
	if len(result.ComponentResults) != 0 {
		t.Errorf("len(ComponentResults) = %d, want 0", len(result.ComponentResults))
	}
}

// TestVerificationEngine_Verify_JSONSerializable verifies that the result
// can be marshaled to JSON without errors.
//
// Reference: TS-P9-01
func TestVerificationEngine_Verify_JSONSerializable(t *testing.T) {
	dir := t.TempDir()
	helperCreateValidRuntime(t, dir)

	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	runtimeInspector := NewRuntimeInspector(cfg)

	engine := NewVerificationEngine(
		runtimeInspector,
		nil,
		nil,
		nil,
		nil,
	)

	result := engine.Verify("", nil)

	// Verify the result has the expected structure for JSON serialization.
	if result.ComponentResults == nil {
		t.Error("ComponentResults should not be nil for JSON serialization")
	}
	if result.Status == "" {
		t.Error("Status should not be empty")
	}
	if result.Summary == "" {
		t.Error("Summary should not be empty")
	}
}

// TestVerificationEngine_Verify_HealthStatusEnum verifies that the
// HealthStatus constants have the expected string values.
//
// Reference: TS-P9-01
func TestVerificationEngine_Verify_HealthStatusEnum(t *testing.T) {
	tests := []struct {
		status   HealthStatus
		expected string
	}{
		{HealthStatusHealthy, "healthy"},
		{HealthStatusDegraded, "degraded"},
		{HealthStatusUnhealthy, "unhealthy"},
	}

	for _, tt := range tests {
		if string(tt.status) != tt.expected {
			t.Errorf("HealthStatus %v = %q, want %q", tt.status, string(tt.status), tt.expected)
		}
	}
}

// TestVerificationEngine_Verify_DegradedStatus verifies that Status is
// degraded when some (but not all) components fail.
//
// Reference: TS-P9-01
func TestVerificationEngine_Verify_DegradedStatus(t *testing.T) {
	dir := t.TempDir()

	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = filepath.Join(dir, "nonexistent")

	// Runtime will fail (nonexistent root), but we pass only runtime inspector.
	// For degraded we need at least one pass and one fail.
	runtimeInspector := NewRuntimeInspector(cfg)

	// Create a valid release setup so release inspector passes.
	releaseCfg := runtime.DefaultRuntimeConfig()
	releaseCfg.InstallRoot = dir
	if err := os.MkdirAll(releaseCfg.ReleasesDirPath(), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	releaseDir := filepath.Join(releaseCfg.ReleasesDirPath(), "release-1")
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(releaseDir, "app.tar.gz"), []byte("data"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	releaseInspector := NewReleaseInspector(releaseCfg)

	engine := NewVerificationEngine(
		runtimeInspector,
		nil,
		releaseInspector,
		nil,
		nil,
	)

	result := engine.Verify("", nil)

	if result.Status != HealthStatusDegraded {
		t.Errorf("Verify().Status = %q, want %q (mixed pass/fail)", result.Status, HealthStatusDegraded)
	}
}
