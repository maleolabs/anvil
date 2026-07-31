package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/runtime"
)

// ---------------------------------------------------------------------------
// PromoteRunner Tests — TS-P4-06
// ---------------------------------------------------------------------------

// setupPromotionTest creates a temp directory with the runtime directory
// structure and returns the configured RuntimeConfig and releases dir path.
func setupPromotionTest(t *testing.T) (runtime.RuntimeConfig, string) {
	t.Helper()

	dir := t.TempDir()

	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	// Create the releases directory so SwitchForActivation can succeed.
	releasesDir := cfg.ReleasesDirPath()
	if err := os.MkdirAll(releasesDir, 0755); err != nil {
		t.Fatalf("mkdir releases dir: %v", err)
	}

	return cfg, releasesDir
}

// createReleaseDirInRuntime creates a release directory at the expected
// runtime location: <releasesDir>/rel-<id> and returns the full path.
func createReleaseDirInRuntime(t *testing.T, releasesDir string, id ReleaseID) string {
	t.Helper()

	releaseDir := runtime.ReleaseDirPath(releasesDir, id.String())
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir release dir %s: %v", releaseDir, err)
	}
	return releaseDir
}

// TestPromoteRunner_Promote_Success verifies that a Release in Activating
// stage is promoted to Active, the symlink is switched, and the state
// transition is reflected on the Release.
//
// AC-1: Release transitions from Activating to Active
// AC-2: Active symlink is switched to the new Release
// AC-5: Exactly one Release is Active (via symlink and stage)
//
// Reference: TS-P4-06 AC-1, AC-2, AC-5
func TestPromoteRunner_Promote_Success(t *testing.T) {
	cfg, releasesDir := setupPromotionTest(t)
	switcher := runtime.NewSymlinkSwitcher(cfg)
	runner := NewPromoteRunner(switcher, releasesDir)

	// Create a Release in Activating stage.
	rel := &Release{
		ID:          ReleaseID("test-promote-001"),
		Stage:       StageActivating,
		Transitions: []TransitionRecord{},
	}

	// Create the release directory in the runtime.
	createReleaseDirInRuntime(t, releasesDir, rel.ID)

	// Act: perform promotion.
	if err := runner.Promote(rel); err != nil {
		t.Fatalf("Promote() returned unexpected error: %v", err)
	}

	// Assert AC-1: Release stage is now Active.
	if rel.Stage != StageActive {
		t.Errorf("Release Stage = %s, want %s", rel.Stage, StageActive)
	}

	// Assert AC-2: Symlink points to the new release directory.
	target, err := switcher.ActiveSymlinkTarget()
	if err != nil {
		t.Fatalf("ActiveSymlinkTarget() returned error: %v", err)
	}
	expectedDir := runtime.ReleaseDirPath(releasesDir, rel.ID.String())
	if target != expectedDir {
		t.Errorf("ActiveSymlinkTarget() = %q, want %q", target, expectedDir)
	}

	// Assert AC-5: Exactly one Active Release — symlink target exists and
	// is a directory (confirming the target is valid).
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat symlink target %q: %v", target, err)
	}
	if !info.IsDir() {
		t.Errorf("symlink target %q is not a directory", target)
	}

	// Verify the transition was recorded in history.
	if len(rel.Transitions) != 1 {
		t.Fatalf("expected 1 transition record, got %d", len(rel.Transitions))
	}
	rec := rel.Transitions[0]
	if rec.From != StageActivating {
		t.Errorf("transition From = %s, want %s", rec.From, StageActivating)
	}
	if rec.To != StageActive {
		t.Errorf("transition To = %s, want %s", rec.To, StageActive)
	}
	if rec.Outcome != "success" {
		t.Errorf("transition Outcome = %q, want %q", rec.Outcome, "success")
	}
}

// TestPromoteRunner_Promote_WrongStage verifies that promoting a Release
// that is not in Activating stage returns an error and leaves the symlink
// and Release state unchanged.
//
// This covers all non-Activating stages (Ready, Active, Failed, etc.).
//
// AC-4: On failure, previous Release remains Active (symlink not switched)
//
// Reference: TS-P4-06 AC-4
func TestPromoteRunner_Promote_WrongStage(t *testing.T) {
	cfg, releasesDir := setupPromotionTest(t)
	switcher := runtime.NewSymlinkSwitcher(cfg)
	runner := NewPromoteRunner(switcher, releasesDir)

	tests := []struct {
		name  string
		stage Stage
	}{
		{"Ready", StageReady},
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
				ID:          ReleaseID("test-wrong-stage-" + strings.ToLower(tt.name)),
				Stage:       tt.stage,
				Transitions: []TransitionRecord{},
			}

			// Create the release directory so symlink failure is not
			// the cause of error.
			createReleaseDirInRuntime(t, releasesDir, rel.ID)

			err := runner.Promote(rel)
			if err == nil {
				t.Fatal("Promote() should have returned an error for non-Activating stage")
			}

			// Verify error message mentions the current and expected stage.
			if !strings.Contains(err.Error(), "cannot promote") {
				t.Errorf("error should contain 'cannot promote', got: %v", err)
			}

			// Verify stage was not changed.
			if rel.Stage != tt.stage {
				t.Errorf("Release Stage changed from %s to %s after failed Promote", tt.stage, rel.Stage)
			}

			// Verify symlink was NOT created (remains absent).
			if switcher.SymlinkExists() {
				t.Error("SymlinkExists() should be false after failed Promote with wrong stage")
			}

			// Verify no transition was recorded.
			if len(rel.Transitions) != 0 {
				t.Errorf("expected 0 transitions after failed Promote, got %d", len(rel.Transitions))
			}
		})
	}
}

// TestPromoteRunner_Promote_NonExistentReleaseDir verifies that when the
// release directory does not exist in the runtime, the promotion fails
// with an appropriate error and the symlink is not modified.
//
// AC-4: On failure, previous Release remains Active (symlink not switched)
//
// Reference: TS-P4-06 AC-4
func TestPromoteRunner_Promote_NonExistentReleaseDir(t *testing.T) {
	cfg, releasesDir := setupPromotionTest(t)

	// Create an initial "previous" active release to establish baseline.
	prevReleaseDir := createReleaseDirInRuntime(t, releasesDir, ReleaseID("prev-release"))
	_ = prevReleaseDir // used implicitly via switcher below

	// Establish a previous active release so we can verify it remains.
	switcher := runtime.NewSymlinkSwitcher(cfg)
	if err := switcher.SwitchTo(prevReleaseDir); err != nil {
		t.Fatalf("initial SwitchTo() failed: %v", err)
	}

	runner := NewPromoteRunner(switcher, releasesDir)

	// Create a Release in Activating stage but DO NOT create the
	// release directory in the runtime.
	rel := &Release{
		ID:          ReleaseID("test-no-dir"),
		Stage:       StageActivating,
		Transitions: []TransitionRecord{},
	}

	err := runner.Promote(rel)
	if err == nil {
		t.Fatal("Promote() should have returned error for non-existent release dir")
	}

	// Verify error mentions the missing directory.
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error should mention 'does not exist', got: %v", err)
	}

	// Verify AC-4: Previous Active Release remains Active.
	target, err := switcher.ActiveSymlinkTarget()
	if err != nil {
		t.Fatalf("ActiveSymlinkTarget() returned error: %v", err)
	}
	if target != prevReleaseDir {
		t.Errorf("after failed Promote, symlink target = %q, want %q (previous preserved)", target, prevReleaseDir)
	}

	// Verify Release stage was not changed.
	if rel.Stage != StageActivating {
		t.Errorf("Release Stage = %s after failed Promote, want %s", rel.Stage, StageActivating)
	}
}

// TestPromoteRunner_Promote_Observable verifies that after a successful
// promotion, operators can confirm which Release is Active by reading the
// symlink target and the Release stage.
//
// AC-6: The promotion is observable
//
// Reference: TS-P4-06 AC-6
func TestPromoteRunner_Promote_Observable(t *testing.T) {
	cfg, releasesDir := setupPromotionTest(t)
	switcher := runtime.NewSymlinkSwitcher(cfg)
	runner := NewPromoteRunner(switcher, releasesDir)

	rel := &Release{
		ID:          ReleaseID("test-observable"),
		Stage:       StageActivating,
		Transitions: []TransitionRecord{},
	}
	createReleaseDirInRuntime(t, releasesDir, rel.ID)

	// Perform promotion.
	if err := runner.Promote(rel); err != nil {
		t.Fatalf("Promote() returned unexpected error: %v", err)
	}

	// Observation 1: Symlink target identifies the active release.
	target, err := switcher.ActiveSymlinkTarget()
	if err != nil {
		t.Fatalf("ActiveSymlinkTarget() returned error: %v", err)
	}
	expectedDir := runtime.ReleaseDirPath(releasesDir, rel.ID.String())
	if target != expectedDir {
		t.Errorf("ActiveSymlinkTarget() = %q, want %q", target, expectedDir)
	}

	// Observation 2: Symlink path is the configured active symlink.
	symlinkPath := switcher.ActiveSymlinkPath()
	if symlinkPath != cfg.ActiveSymlinkPath() {
		t.Errorf("ActiveSymlinkPath() = %q, want %q", symlinkPath, cfg.ActiveSymlinkPath())
	}

	// Observation 3: SymlinkExists returns true.
	if !switcher.SymlinkExists() {
		t.Error("SymlinkExists() should be true after successful promotion")
	}

	// Observation 4: Release stage is Active.
	if rel.Stage != StageActive {
		t.Errorf("Release Stage = %s, want %s", rel.Stage, StageActive)
	}

	// Observation 5: Transition history records the promotion.
	if len(rel.Transitions) != 1 {
		t.Fatalf("expected 1 transition, got %d", len(rel.Transitions))
	}
	if rel.Transitions[0].To != StageActive {
		t.Errorf("transition target = %s, want %s", rel.Transitions[0].To, StageActive)
	}
}

// TestPromoteRunner_Promote_ExactReleaseDirPath verifies that the release
// directory path follows the rel-<identity> convention expected by the
// runtime package (runtime.ReleaseDirPath).
//
// Reference: TS-P4-06
func TestPromoteRunner_Promote_ExactReleaseDirPath(t *testing.T) {
	cfg, releasesDir := setupPromotionTest(t)
	switcher := runtime.NewSymlinkSwitcher(cfg)
	runner := NewPromoteRunner(switcher, releasesDir)

	rel := &Release{
		ID:          ReleaseID("abc123def456"),
		Stage:       StageActivating,
		Transitions: []TransitionRecord{},
	}

	// Create the directory following the exact runtime convention.
	expectedDir := filepath.Join(releasesDir, "rel-"+rel.ID.String())
	if err := os.MkdirAll(expectedDir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", expectedDir, err)
	}

	if err := runner.Promote(rel); err != nil {
		t.Fatalf("Promote() returned unexpected error: %v", err)
	}

	// Verify symlink points to the exact path.
	target, err := switcher.ActiveSymlinkTarget()
	if err != nil {
		t.Fatalf("ActiveSymlinkTarget() returned error: %v", err)
	}
	if target != expectedDir {
		t.Errorf("symlink target = %q, want %q", target, expectedDir)
	}
}
