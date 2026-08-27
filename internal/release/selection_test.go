package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"maleolabs.com/anvil/internal/project"
)

// ---------------------------------------------------------------------------
// Release Selection for Activation Tests — ST-P4-12
// ---------------------------------------------------------------------------

// setupSelectionTest creates a temporary runtime directory with the project
// state structure and returns the runtime path.
func setupSelectionTest(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	s := project.NewStructure(dir)

	releasesDir := filepath.Join(s.StateDir, "releases")
	if err := os.MkdirAll(releasesDir, 0755); err != nil {
		t.Fatalf("mkdir releases dir: %v", err)
	}

	return dir
}

// TestSelectReleaseForActivation_NoReleaseID_LatestReady verifies that
// when no identity is provided, the most recently created Ready Release
// is selected.
//
// AC: Running activation without specifying a Release identity selects the
// most recent Release in Ready stage.
//
// Reference: ST-P4-12 AC-1
func TestSelectReleaseForActivation_NoReleaseID_LatestReady(t *testing.T) {
	runtimePath := setupSelectionTest(t)

	// Create two Ready releases with different timestamps.
	olderRel := &Release{
		ID:          ReleaseID("older-ready-001"),
		Version:     "1.0.0",
		Stage:       StageReady,
		CreatedAt:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
		Transitions: []TransitionRecord{},
	}
	saveTestRelease(t, runtimePath, olderRel)

	newerRel := &Release{
		ID:          ReleaseID("newer-ready-001"),
		Version:     "2.0.0",
		Stage:       StageReady,
		CreatedAt:   time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
		Transitions: []TransitionRecord{},
	}
	saveTestRelease(t, runtimePath, newerRel)

	selected, err := SelectReleaseForActivation(runtimePath, "")
	if err != nil {
		t.Fatalf("SelectReleaseForActivation returned unexpected error: %v", err)
	}

	if selected == nil {
		t.Fatal("SelectReleaseForActivation returned nil, expected a Release")
	}

	if selected.ID != newerRel.ID {
		t.Errorf("selected Release ID = %s, want %s (latest)", selected.ID, newerRel.ID)
	}

	if selected.Stage != StageReady {
		t.Errorf("selected Release Stage = %s, want %s", selected.Stage, StageReady)
	}
}

// TestSelectReleaseForActivation_WithReleaseID verifies that when a specific
// identity is provided, that Release is selected (subject to eligibility).
//
// AC: Running activation with a specific Release identity uses that Release.
//
// Reference: ST-P4-12 AC-2
func TestSelectReleaseForActivation_WithReleaseID(t *testing.T) {
	runtimePath := setupSelectionTest(t)

	rel := &Release{
		ID:          ReleaseID("specific-ready-001"),
		Version:     "1.0.0",
		Stage:       StageReady,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		Transitions: []TransitionRecord{},
	}
	saveTestRelease(t, runtimePath, rel)

	selected, err := SelectReleaseForActivation(runtimePath, rel.ID)
	if err != nil {
		t.Fatalf("SelectReleaseForActivation returned unexpected error: %v", err)
	}

	if selected == nil {
		t.Fatal("SelectReleaseForActivation returned nil, expected a Release")
	}

	if selected.ID != rel.ID {
		t.Errorf("selected Release ID = %s, want %s", selected.ID, rel.ID)
	}

	if selected.Stage != StageReady {
		t.Errorf("selected Release Stage = %s, want %s", selected.Stage, StageReady)
	}
}

// TestSelectReleaseForActivation_ReleaseNotInReady verifies that an error
// is returned when the specific Release is not in Ready stage.
//
// AC: If an explicit Release identity is provided but the Release is not
// in Ready stage, the command reports the error.
//
// Reference: ST-P4-12 AC-5
func TestSelectReleaseForActivation_ReleaseNotInReady(t *testing.T) {
	runtimePath := setupSelectionTest(t)

	rel := &Release{
		ID:          ReleaseID("not-ready-001"),
		Version:     "1.0.0",
		Stage:       StageActive,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		Transitions: []TransitionRecord{},
	}
	saveTestRelease(t, runtimePath, rel)

	_, err := SelectReleaseForActivation(runtimePath, rel.ID)
	if err == nil {
		t.Fatal("SelectReleaseForActivation should return error for non-Ready release")
	}

	if !strings.Contains(err.Error(), "expected Ready") {
		t.Errorf("error should mention 'expected Ready', got: %v", err)
	}
}

// TestSelectReleaseForActivation_NoReadyReleases verifies that an error
// is returned when no Releases are in Ready stage.
//
// AC: If no Release is in Ready stage, the command reports that no eligible
// Release exists.
//
// Reference: ST-P4-12 AC-4
func TestSelectReleaseForActivation_NoReadyReleases(t *testing.T) {
	runtimePath := setupSelectionTest(t)

	// Only an Active release (not Ready).
	rel := &Release{
		ID:          ReleaseID("active-001"),
		Version:     "1.0.0",
		Stage:       StageActive,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		Transitions: []TransitionRecord{},
	}
	saveTestRelease(t, runtimePath, rel)

	_, err := SelectReleaseForActivation(runtimePath, "")
	if err == nil {
		t.Fatal("SelectReleaseForActivation should return error when no Ready releases exist")
	}

	if !strings.Contains(err.Error(), "no releases in Ready stage") {
		t.Errorf("error should mention 'no releases in Ready stage', got: %v", err)
	}
}

// TestSelectReleaseForActivation_NonExistentRelease verifies that an error
// is returned when the specified Release does not exist.
//
// Reference: ST-P4-12 AC-5
func TestSelectReleaseForActivation_NonExistentRelease(t *testing.T) {
	runtimePath := setupSelectionTest(t)

	_, err := SelectReleaseForActivation(runtimePath, ReleaseID("nonexistent"))
	if err == nil {
		t.Fatal("SelectReleaseForActivation should return error for non-existent release")
	}

	if !strings.Contains(err.Error(), "release file not found") &&
		!strings.Contains(err.Error(), "select release for activation") {
		t.Errorf("error should indicate release not found, got: %v", err)
	}
}

// TestSelectReleaseForActivation_SelectionDeterministic verifies that the
// selection is deterministic — same inputs produce the same selection.
//
// AC: The selection is deterministic — same inputs produce the same selection.
//
// Reference: ST-P4-12 AC-6
func TestSelectReleaseForActivation_SelectionDeterministic(t *testing.T) {
	runtimePath := setupSelectionTest(t)

	// Create releases with same-stage but different CreatedAt values.
	releases := []*Release{
		{
			ID:          ReleaseID("rel-a-001"),
			Version:     "1.0.0",
			Stage:       StageReady,
			CreatedAt:   "2024-01-01T00:00:00Z",
			Transitions: []TransitionRecord{},
		},
		{
			ID:          ReleaseID("rel-b-001"),
			Version:     "2.0.0",
			Stage:       StageReady,
			CreatedAt:   "2024-06-01T00:00:00Z",
			Transitions: []TransitionRecord{},
		},
		{
			ID:          ReleaseID("rel-c-001"),
			Version:     "3.0.0",
			Stage:       StageReady,
			CreatedAt:   "2024-03-01T00:00:00Z",
			Transitions: []TransitionRecord{},
		},
	}

	for _, rel := range releases {
		saveTestRelease(t, runtimePath, rel)
	}

	// Run selection twice and verify the same result.
	first, err := SelectReleaseForActivation(runtimePath, "")
	if err != nil {
		t.Fatalf("first selection returned unexpected error: %v", err)
	}

	second, err := SelectReleaseForActivation(runtimePath, "")
	if err != nil {
		t.Fatalf("second selection returned unexpected error: %v", err)
	}

	if first.ID != second.ID {
		t.Errorf("selection not deterministic: first=%s, second=%s", first.ID, second.ID)
	}

	// The latest release should be rel-b (June 2024).
	if first.ID != ReleaseID("rel-b-001") {
		t.Errorf("expected latest release rel-b-001, got %s", first.ID)
	}
}

// TestSelectReleaseForActivation_EmptyReleasesDir verifies that an error is
// returned when the releases state directory does not exist (first-time setup).
//
// Reference: ST-P4-12 AC-4
func TestSelectReleaseForActivation_EmptyReleasesDir(t *testing.T) {
	runtimePath := t.TempDir()

	_, err := SelectReleaseForActivation(runtimePath, "")
	if err == nil {
		t.Fatal("SelectReleaseForActivation should return error when no releases exist")
	}

	if !strings.Contains(err.Error(), "no releases in Ready stage") {
		t.Errorf("error should mention 'no releases in Ready stage', got: %v", err)
	}
}
