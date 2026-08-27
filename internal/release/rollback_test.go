package release

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/project"
	"maleolabs.com/anvil/internal/runtime"
)

// ---------------------------------------------------------------------------
// Rollback Engine Tests — TS-P4-07
// ---------------------------------------------------------------------------

// setupRollbackTest creates a temporary runtime with:
// - A previously Active (now Archived) Release — the rollback target
// - A currently Active Release — the one to roll back
// - All runtime directories for shared resources and symlinks
func setupRollbackTest(t *testing.T) (
	runtimePath string,
	runtimeCfg runtime.RuntimeConfig,
	rollbackTarget *Release,
	activeRelease *Release,
	engine *RollbackEngine,
) {
	t.Helper()

	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	// Create all runtime directories.
	for _, d := range cfg.AllDirs() {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	// Create project state directory for release persistence.
	runtimePath = dir
	s := project.NewStructure(runtimePath)
	releasesStateDir := filepath.Join(s.StateDir, "releases")
	if err := os.MkdirAll(releasesStateDir, 0755); err != nil {
		t.Fatalf("mkdir releases state dir: %v", err)
	}

	releasesDir := cfg.ReleasesDirPath()

	// Create the rollback target (previously Active, now Archived).
	target := &Release{
		ID:           ReleaseID("rollback-target"),
		ArtifactID:   "artifact-target-001",
		Version:      "2.0.0",
		ArtifactPath: "/tmp/artifacts/target.tar.gz",
		RuntimePath:  dir,
		Stage:        StageArchived,
		CreatedAt:    "2026-07-28T09:00:00Z",
		Transitions:  []TransitionRecord{},
	}
	// Add an Archived transition with a timestamp.
	target.Transitions = append(target.Transitions, TransitionRecord{
		Timestamp: "2026-07-28T10:00:00Z",
		From:      StageActive,
		To:        StageArchived,
		Outcome:   "success",
	})
	saveTestRelease(t, runtimePath, target)

	// Create the target release directory in the runtime.
	targetDir := runtime.ReleaseDirPath(releasesDir, target.ID.String())
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatalf("mkdir target release dir: %v", err)
	}

	// Create the currently Active Release.
	active := &Release{
		ID:           ReleaseID("current-active"),
		ArtifactID:   "artifact-active-002",
		Version:      "3.0.0",
		ArtifactPath: "/tmp/artifacts/active.tar.gz",
		RuntimePath:  dir,
		Stage:        StageActive,
		CreatedAt:    "2026-07-29T09:00:00Z",
		Transitions:  []TransitionRecord{},
	}
	saveTestRelease(t, runtimePath, active)

	// Create the active release directory.
	activeDir := runtime.ReleaseDirPath(releasesDir, active.ID.String())
	if err := os.MkdirAll(activeDir, 0755); err != nil {
		t.Fatalf("mkdir active release dir: %v", err)
	}

	// Set the symlink to point to the currently Active Release.
	switcher := runtime.NewSymlinkSwitcher(cfg)
	if err := switcher.SwitchTo(activeDir); err != nil {
		t.Fatalf("switch symlink to active release: %v", err)
	}

	// Create the rollback engine.
	sharedMgr := runtime.NewSharedResourceManager(cfg)
	engine = NewRollbackEngine(runtimePath, sharedMgr, switcher, releasesDir)

	return runtimePath, cfg, target, active, engine
}

// TestRollbackEngine_Rollback_Success verifies that a rollback completes
// successfully, restoring the previously Active Release.
//
// AC-1: An Active Release can be rolled back.
// AC-2: The previously Active Release is identified as the rollback target.
// AC-3: The rollback target transitions back to Active.
// AC-4: The rolled-back Release transitions to Rolled Back stage.
// AC-6: The rolled-back Release is preserved for inspection.
//
// Reference: TS-P4-07 AC-1, AC-2, AC-3, AC-4, AC-6
func TestRollbackEngine_Rollback_Success(t *testing.T) {
	runtimePath, cfg, target, active, engine := setupRollbackTest(t)

	// Act: perform rollback.
	result, err := engine.Rollback()
	if err != nil {
		t.Fatalf("Rollback() returned unexpected error: %v", err)
	}

	// Assert AC-1: Rollback completed — result is not nil.
	if result == nil {
		t.Fatal("Rollback() returned nil result")
	}

	// Assert AC-2: The rollback target is the previously Active Release.
	if result.RestoredRelease.ID != target.ID {
		t.Errorf("RestoredRelease ID = %s, want %s", result.RestoredRelease.ID, target.ID)
	}

	// Assert AC-3: The rollback target transitions back to Active.
	restoredStage, err := GetReleaseState(runtimePath, target.ID)
	if err != nil {
		t.Fatalf("GetReleaseState for target returned error: %v", err)
	}
	if restoredStage != StageActive {
		t.Errorf("restored target Release stage = %s, want %s", restoredStage, StageActive)
	}

	// Assert AC-4: The rolled-back Release transitions to RolledBack.
	rolledBackStage, err := GetReleaseState(runtimePath, active.ID)
	if err != nil {
		t.Fatalf("GetReleaseState for rolled-back release returned error: %v", err)
	}
	if rolledBackStage != StageRolledBack {
		t.Errorf("rolled-back Release stage = %s, want %s", rolledBackStage, StageRolledBack)
	}

	// Assert AC-6: The rolled-back Release is preserved for inspection.
	rolledBackRel, err := LookupByID(runtimePath, active.ID)
	if err != nil {
		t.Fatalf("rolled-back Release not found (not preserved): %v", err)
	}
	if rolledBackRel == nil {
		t.Fatal("rolled-back Release is nil")
	}
	if rolledBackRel.ID != active.ID {
		t.Errorf("rolled-back Release ID = %s, want %s", rolledBackRel.ID, active.ID)
	}

	// Verify the symlink now points to the restored target.
	releasesDir := cfg.ReleasesDirPath()
	expectedTargetDir := runtime.ReleaseDirPath(releasesDir, target.ID.String())
	symlinkPath := cfg.ActiveSymlinkPath()
	targetOfLink, err := os.Readlink(symlinkPath)
	if err != nil {
		t.Fatalf("read symlink: %v", err)
	}
	if targetOfLink != expectedTargetDir {
		t.Errorf("symlink target = %q, want %q", targetOfLink, expectedTargetDir)
	}
}

// TestRollbackEngine_Rollback_NoActiveRelease verifies that rollback fails
// with a clear error when there is no Active Release.
//
// Reference: TS-P4-07
func TestRollbackEngine_Rollback_NoActiveRelease(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	for _, d := range cfg.AllDirs() {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	releasesDir := cfg.ReleasesDirPath()
	sharedMgr := runtime.NewSharedResourceManager(cfg)
	switcher := runtime.NewSymlinkSwitcher(cfg)

	engine := NewRollbackEngine(dir, sharedMgr, switcher, releasesDir)

	_, err := engine.Rollback()
	if err == nil {
		t.Fatal("Rollback() should return error when no Active Release exists")
	}

	if !strings.Contains(err.Error(), "no Active Release") {
		t.Errorf("error should mention 'no Active Release', got: %v", err)
	}
}

// TestRollbackEngine_Rollback_NoArchivedTarget verifies that rollback fails
// when there is no Archived Release to restore (no rollback target).
//
// AC-5: A Release that is not Active cannot be rolled back (implied: a rollback
// requires a valid target).
//
// Reference: TS-P4-07
func TestRollbackEngine_Rollback_NoArchivedTarget(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	for _, d := range cfg.AllDirs() {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	// Create project state directory.
	runtimePath := dir
	s := project.NewStructure(runtimePath)
	releasesStateDir := filepath.Join(s.StateDir, "releases")
	if err := os.MkdirAll(releasesStateDir, 0755); err != nil {
		t.Fatalf("mkdir releases state dir: %v", err)
	}

	releasesDir := cfg.ReleasesDirPath()

	// Create only an Active Release (no Archived target).
	active := &Release{
		ID:          ReleaseID("lonely-active"),
		Stage:       StageActive,
		Transitions: []TransitionRecord{},
	}
	saveTestRelease(t, runtimePath, active)

	activeDir := runtime.ReleaseDirPath(releasesDir, active.ID.String())
	if err := os.MkdirAll(activeDir, 0755); err != nil {
		t.Fatalf("mkdir active release dir: %v", err)
	}

	switcher := runtime.NewSymlinkSwitcher(cfg)
	if err := switcher.SwitchTo(activeDir); err != nil {
		t.Fatalf("switch symlink: %v", err)
	}

	sharedMgr := runtime.NewSharedResourceManager(cfg)
	engine := NewRollbackEngine(runtimePath, sharedMgr, switcher, releasesDir)

	_, err := engine.Rollback()
	if err == nil {
		t.Fatal("Rollback() should return error when no Archived target exists")
	}

	if !strings.Contains(err.Error(), "no rollback target") {
		t.Errorf("error should mention 'no rollback target', got: %v", err)
	}
}

// TestRollbackEngine_Rollback_ActiveNotInActiveStage verifies that a Release
// that is not in Active stage cannot be rolled back via validation.
//
// AC-5: A Release that is not Active cannot be rolled back.
//
// Reference: TS-P4-07 AC-5
func TestRollbackEngine_Rollback_ActiveNotInActiveStage(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	// Create a Ready Release (not Active).
	runtimePath := dir
	s := project.NewStructure(runtimePath)
	releasesStateDir := filepath.Join(s.StateDir, "releases")
	if err := os.MkdirAll(releasesStateDir, 0755); err != nil {
		t.Fatalf("mkdir releases state dir: %v", err)
	}

	readyRel := &Release{
		ID:          ReleaseID("ready-not-active"),
		Stage:       StageReady,
		Transitions: []TransitionRecord{},
	}
	saveTestRelease(t, runtimePath, readyRel)

	releasesDir := cfg.ReleasesDirPath()
	sharedMgr := runtime.NewSharedResourceManager(cfg)
	switcher := runtime.NewSymlinkSwitcher(cfg)

	engine := NewRollbackEngine(runtimePath, sharedMgr, switcher, releasesDir)

	// Run Rollback — should fail because the only release is in Ready, not Active.
	_, err := engine.Rollback()
	if err == nil {
		t.Fatal("Rollback() should return error when no Active release exists")
	}
}

// TestRollbackEngine_Rollback_PreservesRolledBack verifies that the rolled-back
// Release is preserved and accessible after rollback.
//
// AC-6: The rolled-back Release is preserved for inspection.
//
// Reference: TS-P4-07 AC-6, ST-P4-09
func TestRollbackEngine_Rollback_PreservesRolledBack(t *testing.T) {
	runtimePath, _, _, active, engine := setupRollbackTest(t)

	// Snapshot metadata before rollback.
	origID := active.ID
	origArtifactID := active.ArtifactID
	origVersion := active.Version
	origArtifactPath := active.ArtifactPath
	origCreatedAt := active.CreatedAt

	// Act: perform rollback.
	result, err := engine.Rollback()
	if err != nil {
		t.Fatalf("Rollback() returned unexpected error: %v", err)
	}

	// Verify the rolled-back Release is preserved.
	rolledBack := result.RolledBackRelease
	if rolledBack == nil {
		t.Fatal("RolledBackRelease is nil")
	}

	if rolledBack.ID != active.ID {
		t.Errorf("RolledBackRelease ID = %s, want %s", rolledBack.ID, active.ID)
	}

	// Verify the rolled-back Release still exists in the state directory.
	loaded, err := LookupByID(runtimePath, active.ID)
	if err != nil {
		t.Fatalf("rolled-back Release cannot be loaded (not preserved): %v", err)
	}
	if loaded == nil {
		t.Fatal("loaded rolled-back Release is nil")
	}
	if loaded.Stage != StageRolledBack {
		t.Errorf("loaded rolled-back Release stage = %s, want %s", loaded.Stage, StageRolledBack)
	}

	// ST-P4-09: Verify the JSON file still exists on disk (rollback does not delete).
	jsonPath := loaded.SavePath(runtimePath)
	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		t.Errorf("rolled-back Release JSON file was deleted by rollback: %s", jsonPath)
	}

	// ST-P4-09: Verify all metadata fields are preserved unchanged.
	if loaded.ID != origID {
		t.Errorf("ID changed after rollback: %s, want %s", loaded.ID, origID)
	}
	if loaded.ArtifactID != origArtifactID {
		t.Errorf("ArtifactID changed after rollback: %q, want %q", loaded.ArtifactID, origArtifactID)
	}
	if loaded.Version != origVersion {
		t.Errorf("Version changed after rollback: %q, want %q", loaded.Version, origVersion)
	}
	if loaded.ArtifactPath != origArtifactPath {
		t.Errorf("ArtifactPath changed after rollback: %q, want %q", loaded.ArtifactPath, origArtifactPath)
	}
	if loaded.CreatedAt != origCreatedAt {
		t.Errorf("CreatedAt changed after rollback: %q, want %q", loaded.CreatedAt, origCreatedAt)
	}

	// Verify the rolled-back Release's transition history records the rollback.
	foundRollbackTransition := false
	for _, tr := range loaded.Transitions {
		if tr.To == StageRolledBack && tr.Outcome == "success" {
			foundRollbackTransition = true
			break
		}
	}
	if !foundRollbackTransition {
		t.Error("rolled-back Release transition history does not contain RollingBack → RolledBack")
	}
}

// TestRollbackEngine_Rollback_RolledBackListed verifies that the rolled-back
// Release is visible when listing by RolledBack stage.
//
// ST-P4-09: The rolled-back Release must be visible in status/history queries
// and marked as RolledBack.
//
// Reference: ST-P4-09
func TestRollbackEngine_Rollback_RolledBackListed(t *testing.T) {
	runtimePath, _, _, active, engine := setupRollbackTest(t)

	// Act: perform rollback.
	if _, err := engine.Rollback(); err != nil {
		t.Fatalf("Rollback() returned unexpected error: %v", err)
	}

	// ST-P4-09: Verify the rolled-back Release appears in ListReleasesByStage(RolledBack).
	rolledBackList, err := ListReleasesByStage(runtimePath, StageRolledBack)
	if err != nil {
		t.Fatalf("ListReleasesByStage(RolledBack) returned error: %v", err)
	}
	if len(rolledBackList) != 1 {
		t.Fatalf("expected exactly 1 RolledBack release, got %d", len(rolledBackList))
	}
	if rolledBackList[0].ID != active.ID {
		t.Errorf("RolledBack release ID = %s, want %s", rolledBackList[0].ID, active.ID)
	}
	if rolledBackList[0].Stage != StageRolledBack {
		t.Errorf("RolledBack release stage = %s, want %s", rolledBackList[0].Stage, StageRolledBack)
	}
}

// TestRollbackEngine_Rollback_SymlinkUpdated verifies that the active symlink
// is updated to point to the restored Release after rollback.
//
// Reference: TS-P4-07 AC-3
func TestRollbackEngine_Rollback_SymlinkUpdated(t *testing.T) {
	_, cfg, target, _, engine := setupRollbackTest(t)

	// Act: perform rollback.
	if _, err := engine.Rollback(); err != nil {
		t.Fatalf("Rollback() returned unexpected error: %v", err)
	}

	// Verify the symlink now points to the target Release.
	releasesDir := cfg.ReleasesDirPath()
	expectedTargetDir := runtime.ReleaseDirPath(releasesDir, target.ID.String())
	symlinkPath := cfg.ActiveSymlinkPath()
	targetOfLink, err := os.Readlink(symlinkPath)
	if err != nil {
		t.Fatalf("read symlink: %v", err)
	}
	if targetOfLink != expectedTargetDir {
		t.Errorf("symlink target = %q, want %q", targetOfLink, expectedTargetDir)
	}
}

// TestRollbackEngine_Rollback_ExactlyOneActive verifies that after rollback,
// exactly one Release is Active (the restored target).
//
// Reference: TS-P4-07 AC-3
func TestRollbackEngine_Rollback_ExactlyOneActive(t *testing.T) {
	runtimePath, _, _, _, engine := setupRollbackTest(t)

	// Act: perform rollback.
	if _, err := engine.Rollback(); err != nil {
		t.Fatalf("Rollback() returned unexpected error: %v", err)
	}

	// Verify exactly one Active Release.
	active, err := ListReleasesByStage(runtimePath, StageActive)
	if err != nil {
		t.Fatalf("ListReleasesByStage returned error: %v", err)
	}
	if len(active) != 1 {
		t.Errorf("expected exactly 1 Active release after rollback, got %d", len(active))
	}
}

// TestRollbackEngine_Rollback_ErrorMessages verifies that rollback error
// messages are clear and actionable.
//
// Reference: TS-P4-07
func TestRollbackEngine_Rollback_ErrorMessages(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	releasesDir := cfg.ReleasesDirPath()
	sharedMgr := runtime.NewSharedResourceManager(cfg)
	switcher := runtime.NewSymlinkSwitcher(cfg)

	// Test with non-existent runtime path.
	engine := NewRollbackEngine("/tmp/nonexistent-12345", sharedMgr, switcher, releasesDir)
	_, err := engine.Rollback()
	if err == nil {
		t.Fatal("Rollback() should return error for non-existent runtime")
	}
	if !strings.Contains(err.Error(), "Identify target") {
		t.Errorf("error should mention 'Identify target' phase, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ST-P4-15: Interrupted Rollback State Reconciliation
// ---------------------------------------------------------------------------

// TestReconcileInterruptedRollback_NoStuckReleases verifies that
// ReconcileInterruptedRollback returns nil/nil when no Releases are
// stuck in RollingBack stage.
//
// AC: Reconciliation is a no-op when no interrupted rollback exists.
func TestReconcileInterruptedRollback_NoStuckReleases(t *testing.T) {
	runtimePath, _, _, _, engine := setupRollbackTest(t)

	reconciled, err := engine.ReconcileInterruptedRollback()
	if err != nil {
		t.Fatalf("ReconcileInterruptedRollback returned error: %v", err)
	}
	if reconciled != nil {
		t.Errorf("expected nil reconciled list, got %v", reconciled)
	}

	// Verify no releases are in RollingBack stage.
	releases, err := ListReleasesByStage(runtimePath, StageRollingBack)
	if err != nil {
		t.Fatalf("list RollingBack releases: %v", err)
	}
	if len(releases) != 0 {
		t.Errorf("expected 0 RollingBack releases, got %d", len(releases))
	}
}

// TestReconcileInterruptedRollback_ReconcilesStuckRelease verifies that
// a Release stuck in RollingBack stage is transitioned to RolledBack.
//
// AC 5: Reconciles interrupted rollback state before another operation.
func TestReconcileInterruptedRollback_ReconcilesStuckRelease(t *testing.T) {
	runtimePath, cfg, _, _, engine := setupRollbackTest(t)

	// Create a Release stuck in RollingBack stage (simulating interrupted rollback).
	stuckRel := &Release{
		ID:          ReleaseID("rel-stuck-001"),
		Stage:       StageRollingBack,
		Transitions: []TransitionRecord{},
	}
	s := project.NewStructure(runtimePath)
	releasesStateDir := filepath.Join(s.StateDir, "releases")
	if err := os.MkdirAll(releasesStateDir, 0755); err != nil {
		t.Fatalf("mkdir releases state dir: %v", err)
	}
	stuckPath := stuckRel.SavePath(runtimePath)
	if err := stuckRel.Save(stuckPath); err != nil {
		t.Fatalf("save stuck release: %v", err)
	}

	// Create the release directory on disk so it mirrors a real release.
	releaseDir := runtime.ReleaseDirPath(cfg.ReleasesDirPath(), "rel-stuck-001")
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir release dir: %v", err)
	}

	// Run reconciliation.
	reconciled, err := engine.ReconcileInterruptedRollback()
	if err != nil {
		t.Fatalf("ReconcileInterruptedRollback returned error: %v", err)
	}

	// Verify the stuck release was reconciled.
	if len(reconciled) != 1 {
		t.Fatalf("expected 1 reconciled release, got %d", len(reconciled))
	}
	if reconciled[0] != stuckRel.ID {
		t.Errorf("reconciled release ID = %s, want %s", reconciled[0], stuckRel.ID)
	}

	// Verify the release is now RolledBack.
	loaded, err := Load(stuckPath)
	if err != nil {
		t.Fatalf("load reconciled release: %v", err)
	}
	if loaded.Stage != StageRolledBack {
		t.Errorf("reconciled Release Stage = %s, want %s", loaded.Stage, StageRolledBack)
	}

	// Verify transition was recorded.
	foundRollingBack := false
	foundRolledBack := false
	for _, tr := range loaded.Transitions {
		if tr.From == StageRollingBack && tr.To == StageRolledBack && tr.Outcome == "success" {
			foundRolledBack = true
		}
	}
	if !foundRolledBack {
		t.Errorf("transition history should contain RollingBack → RolledBack, got: %v", loaded.Transitions)
	}
	_ = foundRollingBack

	// Verify no releases remain in RollingBack stage.
	remaining, err := ListReleasesByStage(runtimePath, StageRollingBack)
	if err != nil {
		t.Fatalf("list RollingBack releases: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("expected 0 remaining RollingBack releases, got %d", len(remaining))
	}
}

// TestReconcileInterruptedRollback_MultipleStuck verifies that multiple
// Releases stuck in RollingBack are all reconciled.
func TestReconcileInterruptedRollback_MultipleStuck(t *testing.T) {
	runtimePath, cfg, _, _, engine := setupRollbackTest(t)
	s := project.NewStructure(runtimePath)
	releasesStateDir := filepath.Join(s.StateDir, "releases")
	if err := os.MkdirAll(releasesStateDir, 0755); err != nil {
		t.Fatalf("mkdir releases state dir: %v", err)
	}

	// Create three Releases stuck in RollingBack stage.
	stuckIDs := []ReleaseID{"rel-stuck-a", "rel-stuck-b", "rel-stuck-c"}
	for _, id := range stuckIDs {
		rel := &Release{
			ID:          id,
			Stage:       StageRollingBack,
			Transitions: []TransitionRecord{},
		}
		if err := rel.Save(rel.SavePath(runtimePath)); err != nil {
			t.Fatalf("save stuck release %s: %v", id, err)
		}

		// Create release directories.
		releaseDir := runtime.ReleaseDirPath(cfg.ReleasesDirPath(), id.String())
		if err := os.MkdirAll(releaseDir, 0755); err != nil {
			t.Fatalf("mkdir release dir %s: %v", id, err)
		}
	}

	reconciled, err := engine.ReconcileInterruptedRollback()
	if err != nil {
		t.Fatalf("ReconcileInterruptedRollback returned error: %v", err)
	}

	if len(reconciled) != len(stuckIDs) {
		t.Fatalf("expected %d reconciled releases, got %d", len(stuckIDs), len(reconciled))
	}

	// Verify all are now RolledBack.
	for _, id := range stuckIDs {
		rel := &Release{ID: id}
		relPath := rel.SavePath(runtimePath)
		rel, err := Load(relPath)
		if err != nil {
			t.Fatalf("load release %s: %v", id, err)
		}
		if rel.Stage != StageRolledBack {
			t.Errorf("Release %s Stage = %s, want %s", id, rel.Stage, StageRolledBack)
		}
	}
}

// ---------------------------------------------------------------------------
// BUG-004: Rollback failure recovery — valid transitions, explicit logging,
// and symlink/persisted-stage consistency
// ---------------------------------------------------------------------------

// captureLog redirects the standard logger to a buffer for the duration of
// the test so recovery logging can be asserted.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	log.SetOutput(buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return buf
}

// assertFailedRollbackRecovery asserts the invariants that must hold after a
// rollback fails before the persist phase:
//   - the error mentions the failing phase and reports the recovery outcome,
//   - the rolled-back Release is persisted as Failed — the valid
//     RollingBack → Failed failure exit (ADR-003 §4.7, §8.4),
//   - the rollback target is still persisted as Archived,
//   - the active symlink points at the rolled-back Release's directory,
//   - the recovery was logged explicitly.
func assertFailedRollbackRecovery(
	t *testing.T,
	runtimePath string,
	cfg runtime.RuntimeConfig,
	active, target *Release,
	err error,
	wantPhase string,
	logBuf *bytes.Buffer,
) {
	t.Helper()

	if err == nil {
		t.Fatal("Rollback() should return an error when a phase fails")
	}
	if !strings.Contains(err.Error(), wantPhase) {
		t.Errorf("error should mention %q, got: %v", wantPhase, err)
	}
	if !strings.Contains(err.Error(), "rollback recovery") {
		t.Errorf("error should report the recovery outcome, got: %v", err)
	}

	// The rolled-back Release must be persisted as Failed — the only valid
	// failure exit from RollingBack per ADR-003 §4.7 / §8.4.
	stage, err := GetReleaseState(runtimePath, active.ID)
	if err != nil {
		t.Fatalf("GetReleaseState for rolled-back Release: %v", err)
	}
	if stage != StageFailed {
		t.Errorf("rolled-back Release stage = %s, want %s", stage, StageFailed)
	}

	// The rollback target must remain Archived.
	stage, err = GetReleaseState(runtimePath, target.ID)
	if err != nil {
		t.Fatalf("GetReleaseState for target Release: %v", err)
	}
	if stage != StageArchived {
		t.Errorf("target Release stage = %s, want %s", stage, StageArchived)
	}

	// The active symlink must point at the rolled-back Release's directory.
	assertActiveSymlinkTarget(t, cfg, active.ID)

	// The recovery must be logged explicitly.
	if !strings.Contains(logBuf.String(), "rollback recovery") {
		t.Errorf("recovery should be logged explicitly, got log: %q", logBuf.String())
	}
}

// assertActiveSymlinkTarget verifies the active symlink points at the release
// directory for the Release with the given ID.
func assertActiveSymlinkTarget(t *testing.T, cfg runtime.RuntimeConfig, id ReleaseID) {
	t.Helper()
	wantLink := runtime.ReleaseDirPath(cfg.ReleasesDirPath(), id.String())
	gotLink, err := os.Readlink(cfg.ActiveSymlinkPath())
	if err != nil {
		t.Fatalf("read active symlink: %v", err)
	}
	if gotLink != wantLink {
		t.Errorf("active symlink target = %q, want %q", gotLink, wantLink)
	}
}

// TestRollbackEngine_Rollback_ReverseConfigFailureRecovery verifies that a
// failure in the Reverse configuration phase recovers through a valid
// transition: the rolled-back Release is transitioned RollingBack → Failed
// (ADR-003 §4.7, §8.4), persisted, and the recovery is logged explicitly.
//
// Previously the recovery attempted an invalid RollingBack → Active
// transition that always failed silently (BUG-004).
//
// Reference: BUG-004 Validation 1, 4
func TestRollbackEngine_Rollback_ReverseConfigFailureRecovery(t *testing.T) {
	runtimePath, cfg, target, active, engine := setupRollbackTest(t)
	logBuf := captureLog(t)

	// Force the Reverse configuration phase to fail: remove a shared
	// resource directory so EnsureDirectoriesExist returns an error.
	if err := os.RemoveAll(cfg.SharedConfigDirPath()); err != nil {
		t.Fatalf("remove shared config dir: %v", err)
	}

	_, err := engine.Rollback()
	assertFailedRollbackRecovery(t, runtimePath, cfg, active, target, err, "Reverse configuration", logBuf)
}

// TestRollbackEngine_Rollback_PromoteFailureRecovery verifies that a failure
// in the Promote phase — here the symlink switch fails before any filesystem
// change — recovers through the valid RollingBack → Failed transition with
// explicit logging, keeping the symlink and persisted stages consistent.
//
// Reference: BUG-004 Validation 2, 4
func TestRollbackEngine_Rollback_PromoteFailureRecovery(t *testing.T) {
	runtimePath, cfg, target, active, engine := setupRollbackTest(t)
	logBuf := captureLog(t)

	// Force the Promote phase to fail: remove the target's release directory
	// so the atomic symlink switch fails before switching.
	targetDir := runtime.ReleaseDirPath(cfg.ReleasesDirPath(), target.ID.String())
	if err := os.RemoveAll(targetDir); err != nil {
		t.Fatalf("remove target release dir: %v", err)
	}

	_, err := engine.Rollback()
	assertFailedRollbackRecovery(t, runtimePath, cfg, active, target, err, "Promote", logBuf)
}

// TestRollbackEngine_Rollback_PromoteFailureAfterSwitch_RestoresSymlink
// verifies the BUG-004 divergence scenario: when the Promote phase fails
// AFTER the symlink has been switched to the target Release, the recovery
// restores the symlink to the rolled-back Release and persists the valid
// RollingBack → Failed transition, so the filesystem and the persisted
// stages stay consistent.
//
// The failure is forced by temporarily removing Archived → Active from the
// transition map, making target.Transition(StageActive) fail after the
// symlink switch.
//
// Reference: BUG-004 Validation 2, 3; Definition of Done
func TestRollbackEngine_Rollback_PromoteFailureAfterSwitch_RestoresSymlink(t *testing.T) {
	runtimePath, cfg, target, active, engine := setupRollbackTest(t)
	logBuf := captureLog(t)

	// Force promoteTarget to fail AFTER the symlink switch: make
	// Archived → Active an invalid transition for the duration of the test.
	origArchivedTransitions := transitionMap[StageArchived]
	transitionMap[StageArchived] = []Stage{StageRemoved}
	t.Cleanup(func() { transitionMap[StageArchived] = origArchivedTransitions })

	_, err := engine.Rollback()
	assertFailedRollbackRecovery(t, runtimePath, cfg, active, target, err, "Promote", logBuf)

	// The recovery must explicitly report the symlink restoration in both
	// the error and the log — the operator can see what was attempted.
	if !strings.Contains(err.Error(), "symlink restored to Release") {
		t.Errorf("error should report symlink restoration, got: %v", err)
	}
	if !strings.Contains(logBuf.String(), "symlink restored to Release") {
		t.Errorf("log should report symlink restoration, got: %q", logBuf.String())
	}
}

// TestRollbackEngine_Rollback_PersistFailure_RestoresSymlink verifies that a
// failure while persisting the rolled-back Release after a successful promote
// restores the symlink to the rolled-back Release's directory, so the
// filesystem does not point at the target Release while its state transition
// was never persisted (BUG-004 Investigation Notes).
//
// The persistence failure is forced by making the releases state directory
// read-only. Since TD-002 persistence is atomic (temp file + rename, see
// fsutil.WriteFileAtomic), the write fails when the temp file cannot be
// created in the directory — the file itself being read-only no longer
// blocks replacement, because the rename only requires write permission on
// the containing directory. Reading existing release files still works on a
// read-only directory (r-x), so the Identify target phase can load both
// Releases.
//
// Running as root bypasses filesystem permissions, so the test is skipped in
// that case.
//
// Reference: BUG-004 Validation 3; Definition of Done, TD-002
func TestRollbackEngine_Rollback_PersistFailure_RestoresSymlink(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires non-root to enforce read-only release state directory")
	}

	runtimePath, cfg, target, active, engine := setupRollbackTest(t)
	logBuf := captureLog(t)

	// Make the releases state directory read-only so the rolled-back
	// Release's Save fails at the persist phase (after promoteTarget has
	// already switched the symlink to the target Release). The directory
	// must remain readable (r-x) so the Identify target phase can still
	// load both Releases.
	releasesStateDir := filepath.Join(project.NewStructure(runtimePath).StateDir, "releases")
	if err := os.Chmod(releasesStateDir, 0o555); err != nil {
		t.Fatalf("chmod releases state directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(releasesStateDir, 0o755) })

	_, err := engine.Rollback()
	if err == nil {
		t.Fatal("Rollback() should return an error when persistence fails")
	}
	if !strings.Contains(err.Error(), "persist") {
		t.Errorf("error should mention the persist failure, got: %v", err)
	}
	if !strings.Contains(err.Error(), "rollback recovery") {
		t.Errorf("error should report the recovery outcome, got: %v", err)
	}

	// The rolled-back Release could not be persisted: its on-disk state is
	// unchanged (still Active — the truthful pre-rollback state).
	stage, err := GetReleaseState(runtimePath, active.ID)
	if err != nil {
		t.Fatalf("GetReleaseState for rolled-back Release: %v", err)
	}
	if stage != StageActive {
		t.Errorf("rolled-back Release stage = %s, want %s (save failed, state unchanged)", stage, StageActive)
	}

	// The rollback target was never persisted as Active: it remains Archived.
	stage, err = GetReleaseState(runtimePath, target.ID)
	if err != nil {
		t.Fatalf("GetReleaseState for target Release: %v", err)
	}
	if stage != StageArchived {
		t.Errorf("target Release stage = %s, want %s", stage, StageArchived)
	}

	// The symlink must be restored to the rolled-back Release so the
	// filesystem and the persisted state agree.
	assertActiveSymlinkTarget(t, cfg, active.ID)

	// The recovery must be logged explicitly, including the symlink restore.
	if !strings.Contains(logBuf.String(), "rollback recovery") {
		t.Errorf("recovery should be logged explicitly, got log: %q", logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "symlink restored to Release") {
		t.Errorf("log should report symlink restoration, got: %q", logBuf.String())
	}
}
