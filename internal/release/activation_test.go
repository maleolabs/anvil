package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/runtime"
)

// ---------------------------------------------------------------------------
// ActivationEngine Tests — TS-P4-05
// ---------------------------------------------------------------------------

// setupActivationTest creates a temp directory with the full runtime
// directory structure and returns the configured RuntimeConfig, releases
// dir path, and the necessary engine dependencies.
func setupActivationTest(t *testing.T) (
	runtime.RuntimeConfig,
	string,
	*runtime.SharedResourceManager,
	*PromoteRunner,
	*ActivationEngine,
) {
	t.Helper()

	dir := t.TempDir()

	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	// Create all runtime directories (shared, releases, etc.).
	releasesDir := cfg.ReleasesDirPath()
	for _, d := range cfg.AllDirs() {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	// Create dependencies.
	sharedMgr := runtime.NewSharedResourceManager(cfg)
	switcher := runtime.NewSymlinkSwitcher(cfg)
	promoteRunner := NewPromoteRunner(switcher, releasesDir)
	engine := NewActivationEngine(sharedMgr, promoteRunner, nil)

	return cfg, releasesDir, sharedMgr, promoteRunner, engine
}

// TestActivationEngine_Activate_Success verifies that a Release in Ready
// stage is successfully activated through the full phase sequence:
// Prepare → Configure → Promote.
//
// AC-1: Release in Ready stage can be activated
// AC-2: Phases execute in order: Prepare → Configure → Promote
// AC-3: If all phases succeed, Release transitions to Active
//
// Reference: TS-P4-05 AC-1, AC-2, AC-3
func TestActivationEngine_Activate_Success(t *testing.T) {
	_, releasesDir, _, _, engine := setupActivationTest(t)

	rel := &Release{
		ID:          ReleaseID("test-activate-success"),
		Stage:       StageReady,
		Transitions: []TransitionRecord{},
	}

	// Create the release directory in the runtime (needed for Promote phase).
	releaseDir := runtime.ReleaseDirPath(releasesDir, rel.ID.String())
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir release dir: %v", err)
	}

	// Act: run the full activation sequence.
	if err := engine.Activate(rel); err != nil {
		t.Fatalf("Activate() returned unexpected error: %v", err)
	}

	// Assert AC-3: Release transitions to Active.
	if rel.Stage != StageActive {
		t.Errorf("Release Stage = %s after activation, want %s", rel.Stage, StageActive)
	}

	// Verify the full transition history: Ready → Activating → Active.
	if len(rel.Transitions) != 2 {
		t.Fatalf("expected 2 transition records, got %d", len(rel.Transitions))
	}
	if rel.Transitions[0].From != StageReady || rel.Transitions[0].To != StageActivating {
		t.Errorf("transition[0] = %s -> %s, want %s -> %s",
			rel.Transitions[0].From, rel.Transitions[0].To, StageReady, StageActivating)
	}
	if rel.Transitions[1].From != StageActivating || rel.Transitions[1].To != StageActive {
		t.Errorf("transition[1] = %s -> %s, want %s -> %s",
			rel.Transitions[1].From, rel.Transitions[1].To, StageActivating, StageActive)
	}
}

// TestActivationEngine_Activate_NotReady verifies that attempting to
// activate a Release that is not in Ready stage returns an error.
//
// AC-5: Release not in Ready stage cannot be activated
//
// Reference: TS-P4-05 AC-5
func TestActivationEngine_Activate_NotReady(t *testing.T) {
	_, _, _, _, engine := setupActivationTest(t)

	tests := []struct {
		name  string
		stage Stage
	}{
		{"Activating", StageActivating},
		{"Active", StageActive},
		{"Failed", StageFailed},
		{"RollingBack", StageRollingBack},
		{"RolledBack", StageRolledBack},
		{"Archived", StageArchived},
		{"Removed", StageRemoved},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rel := &Release{
				ID:          ReleaseID("test-not-ready-" + strings.ToLower(tt.name)),
				Stage:       tt.stage,
				Transitions: []TransitionRecord{},
			}

			err := engine.Activate(rel)
			if err == nil {
				t.Fatal("Activate() should return error for non-Ready stage")
			}

			// Verify error indicates Prepare phase failure.
			if !strings.Contains(err.Error(), "Prepare") {
				t.Errorf("error should mention 'Prepare' phase, got: %v", err)
			}

			// Verify stage was not changed.
			if rel.Stage != tt.stage {
				t.Errorf("Release Stage changed from %s to %s after failed activation", tt.stage, rel.Stage)
			}
		})
	}
}

// TestActivationEngine_Activate_ConfigureFails verifies that when the
// Configure phase fails (e.g., shared directories don't exist), the
// activation fails and the Release transitions to Failed (best-effort).
//
// AC-4: If any phase fails, activation is considered failed
//
// Reference: TS-P4-05 AC-4
func TestActivationEngine_Activate_ConfigureFails(t *testing.T) {
	dir := t.TempDir()

	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	// Create ONLY the releases directory, NOT shared dirs — this will
	// cause the Configure phase to fail.
	releasesDir := cfg.ReleasesDirPath()
	if err := os.MkdirAll(releasesDir, 0755); err != nil {
		t.Fatalf("mkdir releases dir: %v", err)
	}

	sharedMgr := runtime.NewSharedResourceManager(cfg)
	switcher := runtime.NewSymlinkSwitcher(cfg)
	promoteRunner := NewPromoteRunner(switcher, releasesDir)
	engine := NewActivationEngine(sharedMgr, promoteRunner, nil)

	rel := &Release{
		ID:          ReleaseID("test-configure-fail"),
		Stage:       StageReady,
		Transitions: []TransitionRecord{},
	}

	// Create the release directory (shouldn't matter since Configure fails).
	releaseDir := runtime.ReleaseDirPath(releasesDir, rel.ID.String())
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir release dir: %v", err)
	}

	err := engine.Activate(rel)
	if err == nil {
		t.Fatal("Activate() should return error when Configure phase fails")
	}

	// Verify error indicates Configure phase failure.
	if !strings.Contains(err.Error(), "Configure") {
		t.Errorf("error should mention 'Configure' phase, got: %v", err)
	}

	// Verify the Release was transitioned to Failed (best-effort AC-4).
	// Note: this is best-effort; the transition may or may not succeed
	// depending on whether StageActivating→StageFailed is valid (it is).
	if rel.Stage == StageFailed {
		// This is the expected best-effort outcome.
		// Verify at least the Activating transition was recorded.
		if len(rel.Transitions) < 1 {
			t.Error("expected at least 1 transition record (Ready→Activating)")
		}
	} else if rel.Stage != StageActivating {
		// The Release must be at least in Activating (the first transition
		// succeeded before Configure failed).
		t.Errorf("Release Stage = %s after failed Configure, want Activating or Failed", rel.Stage)
	}
}

// TestActivationEngine_Activate_PromoteFails verifies that when the
// Promote phase fails (e.g., release directory doesn't exist in runtime),
// the activation fails and the Release transitions to Failed (best-effort).
//
// AC-4: If any phase fails, activation is considered failed
//
// Reference: TS-P4-05 AC-4
func TestActivationEngine_Activate_PromoteFails(t *testing.T) {
	_, releasesDir, _, _, engine := setupActivationTest(t)

	rel := &Release{
		ID:          ReleaseID("test-promote-fail"),
		Stage:       StageReady,
		Transitions: []TransitionRecord{},
	}

	// Deliberately do NOT create the release directory in the runtime.
	// The Prepare and Configure phases will succeed, but Promote will
	// fail when it tries to switch the symlink.
	_ = releasesDir

	err := engine.Activate(rel)
	if err == nil {
		t.Fatal("Activate() should return error when Promote phase fails")
	}

	// Verify error indicates Promote phase failure.
	if !strings.Contains(err.Error(), "Promote") {
		t.Errorf("error should mention 'Promote' phase, got: %v", err)
	}

	// Verify the Release was transitioned to Failed (best-effort AC-4).
	if rel.Stage != StageFailed {
		t.Errorf("Release Stage = %s after failed Promote, want %s", rel.Stage, StageFailed)
	}

	// Verify transition history includes the Failed transition.
	// Expected history: Ready→Activating (transition 0), Activating→Failed (transition 1)
	if len(rel.Transitions) < 2 {
		t.Fatalf("expected at least 2 transition records, got %d", len(rel.Transitions))
	}
	if rel.Transitions[0].To != StageActivating {
		t.Errorf("transition[0] should be to Activating, got %s", rel.Transitions[0].To)
	}
	if rel.Transitions[1].To != StageFailed {
		t.Errorf("transition[1] should be to Failed, got %s", rel.Transitions[1].To)
		if rel.Transitions[1].From != StageActivating {
			t.Errorf("transition[1].From should be Activating, got %s", rel.Transitions[1].From)
		}
	}
}

// TestActivationEngine_Activate_PhaseOrder verifies that the phases
// execute in the correct order: Prepare → Configure → Promote.
//
// This is verified by checking the Release stage after each phase.
//
// AC-2: Phases execute in order
//
// Reference: TS-P4-05 AC-2
func TestActivationEngine_Activate_PhaseOrder(t *testing.T) {
	_, releasesDir, _, _, engine := setupActivationTest(t)

	rel := &Release{
		ID:          ReleaseID("test-phase-order"),
		Stage:       StageReady,
		Transitions: []TransitionRecord{},
	}

	// Create release directory for the Promote phase.
	releaseDir := runtime.ReleaseDirPath(releasesDir, rel.ID.String())
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir release dir: %v", err)
	}

	// Activate the release.
	if err := engine.Activate(rel); err != nil {
		t.Fatalf("Activate() returned unexpected error: %v", err)
	}

	// Verify phase order through transition history:
	// Phase 1 (Prepare): no transition, but must start from Ready
	// Phase 2 (Configure): Ready→Activating (transition before Configure)
	// Phase 3 (Promote): Activating→Active (by PromoteRunner)
	if len(rel.Transitions) != 2 {
		t.Fatalf("expected 2 transitions, got %d", len(rel.Transitions))
	}

	// Transition 0: Ready → Activating (after Prepare, before Configure)
	if rel.Transitions[0].From != StageReady {
		t.Errorf("transition[0].From = %s, want %s", rel.Transitions[0].From, StageReady)
	}
	if rel.Transitions[0].To != StageActivating {
		t.Errorf("transition[0].To = %s, want %s", rel.Transitions[0].To, StageActivating)
	}

	// Transition 1: Activating → Active (Promote phase via PromoteRunner)
	if rel.Transitions[1].From != StageActivating {
		t.Errorf("transition[1].From = %s, want %s", rel.Transitions[1].From, StageActivating)
	}
	if rel.Transitions[1].To != StageActive {
		t.Errorf("transition[1].To = %s, want %s", rel.Transitions[1].To, StageActive)
	}
}

// TestActivationEngine_Activate_IsolationFailure verifies that the
// Configure phase catches shared resource isolation violations.
//
// AC-4: Configure phase failure leads to activation failure
//
// Reference: TS-P4-05 AC-4
func TestActivationEngine_Activate_IsolationFailure(t *testing.T) {
	dir := t.TempDir()

	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	// Create the releases directory.
	releasesDir := cfg.ReleasesDirPath()
	if err := os.MkdirAll(releasesDir, 0755); err != nil {
		t.Fatalf("mkdir releases dir: %v", err)
	}

	// Create a shared directory that is a subdirectory of releases
	// — this violates the isolation rule.
	badSharedDir := filepath.Join(releasesDir, "shared-config")
	if err := os.MkdirAll(badSharedDir, 0755); err != nil {
		t.Fatalf("mkdir bad shared dir: %v", err)
	}

	// Override SharedConfigDir to be inside releases dir.
	cfg.SharedConfigDir, _ = filepath.Rel(dir, badSharedDir)

	// Create other required dirs (except SharedConfigDir which now
	// points inside releases).
	sharedDirs := []string{
		cfg.SharedStorageDirPath(),
		cfg.LogsDirPath(),
		cfg.TempDirPath(),
	}
	for _, d := range sharedDirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	sharedMgr := runtime.NewSharedResourceManager(cfg)
	switcher := runtime.NewSymlinkSwitcher(cfg)
	promoteRunner := NewPromoteRunner(switcher, releasesDir)
	engine := NewActivationEngine(sharedMgr, promoteRunner, nil)

	rel := &Release{
		ID:          ReleaseID("test-isolation-fail"),
		Stage:       StageReady,
		Transitions: []TransitionRecord{},
	}

	err := engine.Activate(rel)
	if err == nil {
		t.Fatal("Activate() should return error when isolation validation fails")
	}

	if !strings.Contains(err.Error(), "isolation") {
		t.Errorf("error should mention 'isolation', got: %v", err)
	}

	// Verify Release moved to Failed (best-effort).
	if rel.Stage != StageFailed && rel.Stage != StageActivating {
		t.Errorf("Release Stage = %s, want Failed (or Activating in edge case)", rel.Stage)
	}
}
