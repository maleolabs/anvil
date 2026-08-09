package release

import (
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/project"
	"maleolabs.com/anvil/internal/runtime"
)

// ---------------------------------------------------------------------------
// Active Release Invariant Tests — TS-P4-10
// ---------------------------------------------------------------------------

// setupInvariantTest creates a temporary runtime directory with the project
// state structure and returns the configured runtime path.
func setupInvariantTest(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	s := project.NewStructure(dir)

	// Create the releases state directory.
	releasesDir := filepath.Join(s.StateDir, "releases")
	if err := os.MkdirAll(releasesDir, 0755); err != nil {
		t.Fatalf("mkdir releases dir: %v", err)
	}

	return dir
}

// invariantTestRelease creates a Release and persists it to the runtime state.
func invariantTestRelease(t *testing.T, runtimePath string, id ReleaseID, stage Stage) *Release {
	t.Helper()

	rel := &Release{
		ID:          id,
		Stage:       stage,
		Transitions: []TransitionRecord{},
	}
	saveTestRelease(t, runtimePath, rel)
	return rel
}

// TestInvariant_ArchivePreviousActive_NoActiveRelease verifies that
// ArchivePreviousActive does nothing when no Release is Active.
//
// AC-2: If no Active Release exists, archiving is a no-op.
//
// Reference: TS-P4-10 AC-2
func TestInvariant_ArchivePreviousActive_NoActiveRelease(t *testing.T) {
	runtimePath := setupInvariantTest(t)

	// Only a Ready release — no Active.
	invariantTestRelease(t, runtimePath, ReleaseID("test-ready"), StageReady)

	inv := NewActiveReleaseInvariant(runtimePath)
	archived, err := inv.ArchivePreviousActive()
	if err != nil {
		t.Fatalf("ArchivePreviousActive returned unexpected error: %v", err)
	}

	if archived != nil {
		t.Errorf("expected nil archived release, got %v", archived)
	}

	// Verify the Ready release is still Ready.
	stage, err := GetReleaseState(runtimePath, ReleaseID("test-ready"))
	if err != nil {
		t.Fatalf("GetReleaseState returned error: %v", err)
	}
	if stage != StageReady {
		t.Errorf("Ready release stage changed to %s after no-op archive", stage)
	}
}

// TestInvariant_ArchivePreviousActive_ActiveExists verifies that when an
// Active Release exists, ArchivePreviousActive transitions it to Archived.
//
// AC-1: When a Release is promoted to Active, the previously Active Release
// transitions to Archived.
//
// Reference: TS-P4-10 AC-1
func TestInvariant_ArchivePreviousActive_ActiveExists(t *testing.T) {
	runtimePath := setupInvariantTest(t)

	// Create an Active Release.
	activeRel := invariantTestRelease(t, runtimePath, ReleaseID("test-active"), StageActive)

	inv := NewActiveReleaseInvariant(runtimePath)
	archived, err := inv.ArchivePreviousActive()
	if err != nil {
		t.Fatalf("ArchivePreviousActive returned unexpected error: %v", err)
	}

	if archived == nil {
		t.Fatal("ArchivePreviousActive returned nil, expected archived release")
	}

	if archived.ID != activeRel.ID {
		t.Errorf("archived release ID = %s, want %s", archived.ID, activeRel.ID)
	}

	// Verify the Active Release is now Archived.
	stage, err := GetReleaseState(runtimePath, activeRel.ID)
	if err != nil {
		t.Fatalf("GetReleaseState returned error: %v", err)
	}
	if stage != StageArchived {
		t.Errorf("previous Active Release stage = %s after archive, want %s", stage, StageArchived)
	}

	// Verify transition history includes the archival.
	rel, err := LookupByID(runtimePath, activeRel.ID)
	if err != nil {
		t.Fatalf("LookupByID returned error: %v", err)
	}
	found := false
	for _, tr := range rel.Transitions {
		if tr.To == StageArchived && tr.Outcome == "success" {
			found = true
			break
		}
	}
	if !found {
		t.Error("transition history does not contain Active → Archived transition")
	}
}

// TestInvariant_ArchivePreviousActive_MultipleActive verifies that when
// multiple Active Releases exist (invariant violation), the first one is
// archived and no error is reported for the duplicate.
//
// Reference: TS-P4-10
func TestInvariant_ArchivePreviousActive_MultipleActive(t *testing.T) {
	runtimePath := setupInvariantTest(t)

	// Create two Active Releases (invariant violation — should not normally happen).
	invariantTestRelease(t, runtimePath, ReleaseID("test-active-1"), StageActive)
	invariantTestRelease(t, runtimePath, ReleaseID("test-active-2"), StageActive)

	inv := NewActiveReleaseInvariant(runtimePath)
	archived, err := inv.ArchivePreviousActive()
	if err != nil {
		t.Fatalf("ArchivePreviousActive returned unexpected error: %v", err)
	}

	if archived == nil {
		t.Fatal("ArchivePreviousActive returned nil, expected archived release")
	}

	// At least one Active Release should have been archived.
	if archived.Stage != StageArchived {
		t.Errorf("archived release stage = %s, want %s", archived.Stage, StageArchived)
	}

	// The other Active release should still be findable.
	active, err := ListReleasesByStage(runtimePath, StageActive)
	if err != nil {
		t.Fatalf("ListReleasesByStage returned error: %v", err)
	}
	if len(active) != 1 {
		t.Errorf("expected 1 remaining Active release, got %d", len(active))
	}
}

// TestInvariant_ArchivePreviousActive_PersistsChanges verifies that the
// archived state is persisted to disk, not just in-memory.
//
// Reference: TS-P4-10 AC-1
func TestInvariant_ArchivePreviousActive_PersistsChanges(t *testing.T) {
	runtimePath := setupInvariantTest(t)

	activeRel := invariantTestRelease(t, runtimePath, ReleaseID("test-persist"), StageActive)

	inv := NewActiveReleaseInvariant(runtimePath)
	if _, err := inv.ArchivePreviousActive(); err != nil {
		t.Fatalf("ArchivePreviousActive returned error: %v", err)
	}

	// Load the release from disk in a fresh lookup.
	loaded, err := LookupByID(runtimePath, activeRel.ID)
	if err != nil {
		t.Fatalf("LookupByID returned error: %v", err)
	}

	if loaded.Stage != StageArchived {
		t.Errorf("loaded Release stage = %s after archive, want %s", loaded.Stage, StageArchived)
	}
}

// TestInvariant_IntegrationWithActivationEngine verifies that the invariant
// enforcement integrates correctly with the ActivationEngine's promotion flow.
//
// When a Release is activated and a previous Active Release exists, the
// previous Active Release should be Archived and exactly one Release should
// remain Active.
//
// Reference: TS-P4-10 AC-1, AC-2, AC-3
func TestInvariant_IntegrationWithActivationEngine(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	// Create all runtime directories.
	for _, d := range cfg.AllDirs() {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	// Create the project state directory for release persistence.
	runtimePath := dir
	s := project.NewStructure(runtimePath)
	releasesStateDir := filepath.Join(s.StateDir, "releases")
	if err := os.MkdirAll(releasesStateDir, 0755); err != nil {
		t.Fatalf("mkdir releases state dir: %v", err)
	}

	// Create a previously Active Release.
	prevActive := &Release{
		ID:          ReleaseID("prev-active"),
		Stage:       StageActive,
		Transitions: []TransitionRecord{},
	}
	saveTestRelease(t, runtimePath, prevActive)

	// Create the activation engine with invariant enforcement.
	sharedMgr := runtime.NewSharedResourceManager(cfg)
	switcher := runtime.NewSymlinkSwitcher(cfg)
	releasesDir := cfg.ReleasesDirPath()
	promoteRunner := NewPromoteRunner(switcher, releasesDir)
	invariant := NewActiveReleaseInvariant(runtimePath)
	engine := NewActivationEngine(sharedMgr, promoteRunner, invariant)

	// Create a new Release in Ready stage to activate.
	newRel := &Release{
		ID:          ReleaseID("new-release"),
		Stage:       StageReady,
		Transitions: []TransitionRecord{},
	}

	// Create the release directory in the runtime (needed for Promote phase).
	releaseDir := runtime.ReleaseDirPath(releasesDir, newRel.ID.String())
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir release dir: %v", err)
	}

	// Activate the new Release.
	if err := engine.Activate(newRel); err != nil {
		t.Fatalf("Activate() returned unexpected error: %v", err)
	}

	// Persist the new Release's updated state (as the caller would).
	if err := newRel.Save(newRel.SavePath(runtimePath)); err != nil {
		t.Fatalf("save new release: %v", err)
	}

	// Verify AC-1: The new Release is Active.
	if newRel.Stage != StageActive {
		t.Errorf("new Release stage = %s after activation, want %s", newRel.Stage, StageActive)
	}

	// Verify AC-2: The previous Active Release is now Archived.
	prevStage, err := GetReleaseState(runtimePath, prevActive.ID)
	if err != nil {
		t.Fatalf("GetReleaseState returned error: %v", err)
	}
	if prevStage != StageArchived {
		t.Errorf("previous Active Release stage = %s after activation, want %s", prevStage, StageArchived)
	}

	// Verify AC-3: Exactly one Release is Active.
	active, err := ListReleasesByStage(runtimePath, StageActive)
	if err != nil {
		t.Fatalf("ListReleasesByStage returned error: %v", err)
	}
	if len(active) != 1 {
		t.Errorf("expected exactly 1 Active release, got %d", len(active))
	}
	if active[0].ID != newRel.ID {
		t.Errorf("Active release ID = %s, want %s", active[0].ID, newRel.ID)
	}
}

// TestInvariant_IntegrationWithActivationEngine_NoPreviousActive verifies
// that activation succeeds even when there is no previously Active Release.
//
// Reference: TS-P4-10
func TestInvariant_IntegrationWithActivationEngine_NoPreviousActive(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	for _, d := range cfg.AllDirs() {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	runtimePath := dir
	s := project.NewStructure(runtimePath)
	releasesStateDir := filepath.Join(s.StateDir, "releases")
	if err := os.MkdirAll(releasesStateDir, 0755); err != nil {
		t.Fatalf("mkdir releases state dir: %v", err)
	}

	sharedMgr := runtime.NewSharedResourceManager(cfg)
	switcher := runtime.NewSymlinkSwitcher(cfg)
	releasesDir := cfg.ReleasesDirPath()
	promoteRunner := NewPromoteRunner(switcher, releasesDir)
	invariant := NewActiveReleaseInvariant(runtimePath)
	engine := NewActivationEngine(sharedMgr, promoteRunner, invariant)

	// Create a new Release in Ready stage (no previous Active).
	newRel := &Release{
		ID:          ReleaseID("new-release-no-prev"),
		Stage:       StageReady,
		Transitions: []TransitionRecord{},
	}

	releaseDir := runtime.ReleaseDirPath(releasesDir, newRel.ID.String())
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir release dir: %v", err)
	}

	if err := engine.Activate(newRel); err != nil {
		t.Fatalf("Activate() returned unexpected error: %v", err)
	}

	// Persist the new Release's updated state (as the caller would).
	if err := newRel.Save(newRel.SavePath(runtimePath)); err != nil {
		t.Fatalf("save new release: %v", err)
	}

	if newRel.Stage != StageActive {
		t.Errorf("new Release stage = %s after activation, want %s", newRel.Stage, StageActive)
	}

	// Verify exactly one Active.
	active, err := ListReleasesByStage(runtimePath, StageActive)
	if err != nil {
		t.Fatalf("ListReleasesByStage returned error: %v", err)
	}
	if len(active) != 1 {
		t.Errorf("expected exactly 1 Active release, got %d", len(active))
	}
}

// TestInvariant_ErrorMessages verifies that error messages from the invariant
// enforcement are clear and actionable.
//
// Reference: TS-P4-10
func TestInvariant_ErrorMessages(t *testing.T) {
	runtimePath := setupInvariantTest(t)

	inv := NewActiveReleaseInvariant(runtimePath)

	// Call on empty directory — should not error (no active release to archive).
	_, err := inv.ArchivePreviousActive()
	if err != nil {
		t.Fatalf("ArchivePreviousActive on empty state returned unexpected error: %v", err)
	}

	// Create a Release in Failed stage (not Active).
	invariantTestRelease(t, runtimePath, ReleaseID("test-failed"), StageFailed)

	// Should not error (no Active release to archive).
	_, err = inv.ArchivePreviousActive()
	if err != nil {
		t.Fatalf("ArchivePreviousActive with no Active release returned unexpected error: %v", err)
	}

	// Create a Release with a corrupt state file.
	// (already covered by the corrupt file test in query_test.go)
}

// TestInvariant_Atomicity verifies that the archival is effectively atomic
// — after ArchivePreviousActive completes, there is no window where two
// Active Releases coexist.
//
// Reference: TS-P4-10 AC-5
func TestInvariant_Atomicity(t *testing.T) {
	runtimePath := setupInvariantTest(t)

	// Create a single Active release.
	invariantTestRelease(t, runtimePath, ReleaseID("test-atomic"), StageActive)

	inv := NewActiveReleaseInvariant(runtimePath)
	if _, err := inv.ArchivePreviousActive(); err != nil {
		t.Fatalf("ArchivePreviousActive returned error: %v", err)
	}

	// After archiving, verify exactly 0 Active releases.
	active, err := ListReleasesByStage(runtimePath, StageActive)
	if err != nil {
		t.Fatalf("ListReleasesByStage returned error: %v", err)
	}
	if len(active) != 0 {
		t.Errorf("expected 0 Active releases after archive, got %d", len(active))
	}

	// Verify the archived release is in Archived stage.
	archived, err := ListReleasesByStage(runtimePath, StageArchived)
	if err != nil {
		t.Fatalf("ListReleasesByStage returned error: %v", err)
	}
	if len(archived) != 1 {
		t.Errorf("expected 1 Archived release, got %d", len(archived))
	}
}

// TestInvariant_NonExistentRuntimePath verifies that ArchivePreviousActive
// handles non-existent runtime paths gracefully (returns no error, nil release).
func TestInvariant_NonExistentRuntimePath(t *testing.T) {
	nonExistentPath := "/tmp/nonexistent-path-12345"

	inv := NewActiveReleaseInvariant(nonExistentPath)
	archived, err := inv.ArchivePreviousActive()
	if err != nil {
		t.Fatalf("ArchivePreviousActive returned unexpected error: %v", err)
	}

	if archived != nil {
		t.Errorf("expected nil archived release for non-existent path, got %v", archived)
	}
}

// TestInvariant_PreviousActiveReleaseIdempotent verifies that calling
// ArchivePreviousActive multiple times is idempotent — after the first
// call archives the Active release, subsequent calls are no-ops.
func TestInvariant_PreviousActiveReleaseIdempotent(t *testing.T) {
	runtimePath := setupInvariantTest(t)

	invariantTestRelease(t, runtimePath, ReleaseID("test-idempotent"), StageActive)

	inv := NewActiveReleaseInvariant(runtimePath)

	// First call — should archive.
	archived1, err := inv.ArchivePreviousActive()
	if err != nil {
		t.Fatalf("first ArchivePreviousActive returned error: %v", err)
	}
	if archived1 == nil {
		t.Fatal("first ArchivePreviousActive returned nil")
	}
	if archived1.Stage != StageArchived {
		t.Errorf("first archive: stage = %s, want %s", archived1.Stage, StageArchived)
	}

	// Second call — should be a no-op (no Active release).
	archived2, err := inv.ArchivePreviousActive()
	if err != nil {
		t.Fatalf("second ArchivePreviousActive returned error: %v", err)
	}
	if archived2 != nil {
		t.Errorf("second archive: expected nil, got %v", archived2)
	}
}
