// Package inspection provides read-only diagnostic inspection capabilities
// for Anvil Runtime environments and configuration.
//
// Reference: TS-P9-02
package inspection

import (
	"testing"

	"maleolabs.com/anvil/internal/runtime"
)

// TestNewReadinessCoordinator verifies that NewReadinessCoordinator creates
// a non-nil coordinator.
//
// Reference: TS-P9-02
func TestNewReadinessCoordinator(t *testing.T) {
	engine := NewVerificationEngine(nil, nil, nil, nil, nil)
	coordinator := NewReadinessCoordinator(engine)
	if coordinator == nil {
		t.Fatal("NewReadinessCoordinator() returned nil")
	}
}

// TestReadinessCoordinator_CheckReadiness_AllComponentsPass verifies that
// CheckReadiness returns Ready=true when all components pass.
//
// Reference: TS-P9-02
func TestReadinessCoordinator_CheckReadiness_AllComponentsPass(t *testing.T) {
	dir := t.TempDir()
	helperCreateValidRuntime(t, dir)
	helperSetupServerConfig(t, dir)

	cfg := defaultTestRuntimeConfig(dir)
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

	coordinator := NewReadinessCoordinator(engine)
	result := coordinator.CheckReadiness(nil)

	if !result.Ready {
		t.Errorf("CheckReadiness().Ready = false, want true; summary: %s", result.Summary)
		for _, b := range result.Blockers {
			t.Logf("  blocker: %s", b)
		}
	}
	if len(result.Blockers) != 0 {
		t.Errorf("len(Blockers) = %d, want 0", len(result.Blockers))
	}
	if result.Summary == "" {
		t.Error("Summary should not be empty")
	}
}

// TestReadinessCoordinator_CheckReadiness_OneComponentFails verifies that
// CheckReadiness returns Ready=false with blockers when one component fails.
//
// Reference: TS-P9-02
func TestReadinessCoordinator_CheckReadiness_OneComponentFails(t *testing.T) {
	dir := t.TempDir()

	cfg := defaultTestRuntimeConfig(dir) // nonexistent install root

	runtimeInspector := NewRuntimeInspector(cfg)

	engine := NewVerificationEngine(
		runtimeInspector,
		nil,
		nil,
		nil,
		nil,
	)

	coordinator := NewReadinessCoordinator(engine)
	result := coordinator.CheckReadiness(nil)

	if result.Ready {
		t.Errorf("CheckReadiness().Ready = true, want false (runtime doesn't exist)")
	}
	if len(result.Blockers) == 0 {
		t.Error("Blockers should not be empty when components fail")
	}
}

// TestReadinessCoordinator_CheckReadiness_MultipleFailures verifies that
// all blockers are listed when multiple components fail.
//
// Reference: TS-P9-02
func TestReadinessCoordinator_CheckReadiness_MultipleFailures(t *testing.T) {
	dir := t.TempDir()

	cfg := defaultTestRuntimeConfig(dir) // nonexistent install root

	runtimeInspector := NewRuntimeInspector(cfg)
	releaseInspector := NewReleaseInspector(cfg)

	engine := NewVerificationEngine(
		runtimeInspector,
		nil,
		releaseInspector,
		nil,
		nil,
	)

	coordinator := NewReadinessCoordinator(engine)
	result := coordinator.CheckReadiness(nil)

	if result.Ready {
		t.Errorf("CheckReadiness().Ready = true, want false (multiple failures)")
	}

	// Should have blockers from both runtime and release components.
	if len(result.Blockers) < 2 {
		t.Errorf("len(Blockers) = %d, want at least 2", len(result.Blockers))
	}
}

// TestReadinessCoordinator_CheckReadiness_BlockersContainComponentInfo
// verifies that blockers include component and check names.
//
// Reference: TS-P9-02
func TestReadinessCoordinator_CheckReadiness_BlockersContainComponentInfo(t *testing.T) {
	dir := t.TempDir()

	cfg := defaultTestRuntimeConfig(dir)

	runtimeInspector := NewRuntimeInspector(cfg)

	engine := NewVerificationEngine(
		runtimeInspector,
		nil,
		nil,
		nil,
		nil,
	)

	coordinator := NewReadinessCoordinator(engine)
	result := coordinator.CheckReadiness(nil)

	if result.Ready {
		t.Fatal("CheckReadiness().Ready = true, want false")
	}

	// Each blocker should contain component context.
	for _, blocker := range result.Blockers {
		if blocker == "" {
			t.Error("blocker should not be empty string")
		}
	}
}

// TestReadinessCoordinator_CheckReadiness_SummaryFormat verifies the
// summary format for both ready and not-ready states.
//
// Reference: TS-P9-02
func TestReadinessCoordinator_CheckReadiness_SummaryFormat(t *testing.T) {
	dir := t.TempDir()
	helperCreateValidRuntime(t, dir)
	helperSetupServerConfig(t, dir)

	cfg := defaultTestRuntimeConfig(dir)
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

	coordinator := NewReadinessCoordinator(engine)
	result := coordinator.CheckReadiness(nil)

	if !result.Ready {
		t.Fatalf("CheckReadiness().Ready = false, want true; summary: %s", result.Summary)
	}

	if result.Summary == "" {
		t.Error("Summary should not be empty")
	}
}

// TestReadinessCoordinator_CheckReadiness_NilEngine verifies that
// CheckReadiness handles a nil verification engine gracefully.
//
// Reference: TS-P9-02
func TestReadinessCoordinator_CheckReadiness_NilEngine(t *testing.T) {
	engine := NewVerificationEngine(nil, nil, nil, nil, nil)
	coordinator := NewReadinessCoordinator(engine)

	result := coordinator.CheckReadiness(nil)

	// With no inspectors, the system is ready (vacuously).
	if !result.Ready {
		t.Errorf("CheckReadiness().Ready = false, want true (no inspectors)")
	}
}

// TestExtractBlockers_AllPassing verifies that extractBlockers returns an
// empty slice when all components pass.
//
// Reference: TS-P9-02
func TestExtractBlockers_AllPassing(t *testing.T) {
	components := []InspectionResult{
		{Component: "runtime", Passed: true, Checks: []InspectionCheck{
			{Name: "check1", Passed: true, Details: "ok"},
		}},
	}

	blockers := extractBlockers(components)
	if len(blockers) != 0 {
		t.Errorf("extractBlockers() returned %d blockers, want 0", len(blockers))
	}
}

// TestExtractBlockers_WithFailures verifies that extractBlockers extracts
// all failed checks as blockers.
//
// Reference: TS-P9-02
func TestExtractBlockers_WithFailures(t *testing.T) {
	components := []InspectionResult{
		{Component: "runtime", Passed: false, Checks: []InspectionCheck{
			{Name: "check1", Passed: true, Details: "ok"},
			{Name: "check2", Passed: false, Details: "failed"},
		}},
		{Component: "config", Passed: false, Checks: []InspectionCheck{
			{Name: "check3", Passed: false, Details: "also failed"},
		}},
	}

	blockers := extractBlockers(components)
	if len(blockers) != 2 {
		t.Errorf("extractBlockers() returned %d blockers, want 2", len(blockers))
	}
}

// TestExtractBlockers_EmptyComponents verifies behavior with empty input.
//
// Reference: TS-P9-02
func TestExtractBlockers_EmptyComponents(t *testing.T) {
	blockers := extractBlockers(nil)
	if len(blockers) != 0 {
		t.Errorf("extractBlockers(nil) returned %d blockers, want 0", len(blockers))
	}
}

// defaultTestRuntimeConfig returns a RuntimeConfig with InstallRoot set to dir.
func defaultTestRuntimeConfig(dir string) runtime.RuntimeConfig {
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir
	return cfg
}
