package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/project"
)

// ---------------------------------------------------------------------------
// Release History Recorder Tests — TS-P4-08
// ---------------------------------------------------------------------------

// TestGetReleaseHistory_WithTransitions verifies that GetReleaseHistory
// returns all recorded transitions for a Release.
//
// AC-5: History is retrievable by Release identity.
//
// Reference: TS-P4-08 AC-5
func TestGetReleaseHistory_WithTransitions(t *testing.T) {
	runtimePath := setupQueryTest(t)

	rel := &Release{
		ID:    ReleaseID("test-history-001"),
		Stage: StageActive,
		Transitions: []TransitionRecord{
			{Timestamp: "2024-01-15T10:00:00Z", From: StageReady, To: StageActivating, Outcome: "success"},
			{Timestamp: "2024-01-15T10:00:05Z", From: StageActivating, To: StageActive, Outcome: "success"},
		},
	}
	saveTestRelease(t, runtimePath, rel)

	history, err := GetReleaseHistory(runtimePath, rel.ID)
	if err != nil {
		t.Fatalf("GetReleaseHistory returned unexpected error: %v", err)
	}

	if len(history) != 2 {
		t.Fatalf("expected 2 transition records, got %d", len(history))
	}

	if history[0].To != StageActivating {
		t.Errorf("history[0].To = %s, want %s", history[0].To, StageActivating)
	}
	if history[1].To != StageActive {
		t.Errorf("history[1].To = %s, want %s", history[1].To, StageActive)
	}
	if history[1].From != StageActivating {
		t.Errorf("history[1].From = %s, want %s", history[1].From, StageActivating)
	}
	if history[1].Outcome != "success" {
		t.Errorf("history[1].Outcome = %q, want %q", history[1].Outcome, "success")
	}
}

// TestGetReleaseHistory_EmptyHistory verifies that GetReleaseHistory
// returns nil (not an empty slice) when the Release has no transitions.
//
// Reference: TS-P4-08 AC-5
func TestGetReleaseHistory_EmptyHistory(t *testing.T) {
	runtimePath := setupQueryTest(t)

	rel := &Release{
		ID:          ReleaseID("test-empty-history-001"),
		Stage:       StageReady,
		Transitions: []TransitionRecord{},
	}
	saveTestRelease(t, runtimePath, rel)

	history, err := GetReleaseHistory(runtimePath, rel.ID)
	if err != nil {
		t.Fatalf("GetReleaseHistory returned unexpected error: %v", err)
	}

	if history != nil {
		t.Errorf("expected nil history for empty transitions, got %v", history)
	}
}

// TestGetReleaseHistory_NonExistentRelease verifies that GetReleaseHistory
// returns an error when the Release does not exist.
//
// Reference: TS-P4-08 AC-5
func TestGetReleaseHistory_NonExistentRelease(t *testing.T) {
	runtimePath := setupQueryTest(t)

	_, err := GetReleaseHistory(runtimePath, ReleaseID("nonexistent"))
	if err == nil {
		t.Fatal("GetReleaseHistory should return error for non-existent release")
	}
	if !strings.Contains(err.Error(), "release file not found") &&
		!strings.Contains(err.Error(), "get release history") {
		t.Errorf("error should indicate release not found, got: %v", err)
	}
}

// TestGetReleaseHistory_ReturnsCopy verifies that GetReleaseHistory returns
// a copy of the transitions, not a reference to the Release's internal slice.
//
// Reference: TS-P4-08
func TestGetReleaseHistory_ReturnsCopy(t *testing.T) {
	runtimePath := setupQueryTest(t)

	rel := &Release{
		ID:    ReleaseID("test-copy-001"),
		Stage: StageActive,
		Transitions: []TransitionRecord{
			{Timestamp: "2024-01-15T10:00:00Z", From: StageReady, To: StageActivating, Outcome: "success"},
			{Timestamp: "2024-01-15T10:00:05Z", From: StageActivating, To: StageActive, Outcome: "success"},
		},
	}
	saveTestRelease(t, runtimePath, rel)

	history, err := GetReleaseHistory(runtimePath, rel.ID)
	if err != nil {
		t.Fatalf("GetReleaseHistory returned unexpected error: %v", err)
	}

	// Modify the returned slice.
	if len(history) > 0 {
		history[0] = TransitionRecord{Timestamp: "modified"}
	}

	// Reload the Release and verify the original transitions are unchanged.
	reloaded, err := LookupByID(runtimePath, rel.ID)
	if err != nil {
		t.Fatalf("LookupByID returned unexpected error: %v", err)
	}

	if len(reloaded.Transitions) < 1 {
		t.Fatal("expected at least 1 transition in reloaded release")
	}
	if reloaded.Transitions[0].Timestamp == "modified" {
		t.Error("GetReleaseHistory returned a shared reference; expected a copy")
	}
	if reloaded.Transitions[0].Timestamp != "2024-01-15T10:00:00Z" {
		t.Errorf("original transition was modified; got timestamp %q", reloaded.Transitions[0].Timestamp)
	}
}

// TestReleaseHistory_Method verifies that the History() method on Release
// returns the transition history.
//
// Reference: TS-P4-08
func TestReleaseHistory_Method(t *testing.T) {
	rel := &Release{
		ID:    ReleaseID("test-method-001"),
		Stage: StageActive,
		Transitions: []TransitionRecord{
			{Timestamp: "2024-01-15T10:00:00Z", From: StageReady, To: StageActivating, Outcome: "success"},
		},
	}

	history := rel.History()
	if len(history) != 1 {
		t.Fatalf("expected 1 transition record, got %d", len(history))
	}
	if history[0].To != StageActivating {
		t.Errorf("history[0].To = %s, want %s", history[0].To, StageActivating)
	}
}

// TestReleaseHistory_Method_Empty verifies that History() returns nil
// when the Release has no transitions.
//
// Reference: TS-P4-08
func TestReleaseHistory_Method_Empty(t *testing.T) {
	rel := &Release{
		ID:          ReleaseID("test-empty-method-001"),
		Stage:       StageReady,
		Transitions: []TransitionRecord{},
	}

	history := rel.History()
	if history != nil {
		t.Errorf("expected nil for empty transitions, got %v", history)
	}
}

// TestGetReleaseHistory_NoStateModification verifies that GetReleaseHistory
// does not modify any state.
//
// Reference: TS-P4-08, TS-P4-09 AC-5
func TestGetReleaseHistory_NoStateModification(t *testing.T) {
	runtimePath := setupQueryTest(t)

	rel := &Release{
		ID:    ReleaseID("test-nomod-history-001"),
		Stage: StageActive,
		Transitions: []TransitionRecord{
			{Timestamp: "2024-01-15T10:00:00Z", From: StageReady, To: StageActivating, Outcome: "success"},
			{Timestamp: "2024-01-15T10:00:05Z", From: StageActivating, To: StageActive, Outcome: "success"},
		},
	}
	saveTestRelease(t, runtimePath, rel)

	// Read the state file content before query.
	s := project.NewStructure(runtimePath)
	relPath := filepath.Join(s.StateDir, "releases", rel.ID.String()+".json")
	before, err := os.ReadFile(relPath)
	if err != nil {
		t.Fatalf("read release file before query: %v", err)
	}

	// Perform the history query.
	_, err = GetReleaseHistory(runtimePath, rel.ID)
	if err != nil {
		t.Fatalf("GetReleaseHistory returned unexpected error: %v", err)
	}

	// Read the state file content after query.
	after, err := os.ReadFile(relPath)
	if err != nil {
		t.Fatalf("read release file after query: %v", err)
	}

	if string(before) != string(after) {
		t.Error("state file content changed after read-only query")
	}
}

// ---------------------------------------------------------------------------
// State Query Tests — TS-P4-09
// ---------------------------------------------------------------------------

// setupQueryTest creates a temporary runtime directory with the project
// state structure and returns the runtime path.
func setupQueryTest(t *testing.T) string {
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

// saveTestRelease persists a Release to the runtime's state directory
// for use in query tests.
func saveTestRelease(t *testing.T, runtimePath string, rel *Release) {
	t.Helper()

	s := project.NewStructure(runtimePath)
	releasesDir := filepath.Join(s.StateDir, "releases")
	relPath := filepath.Join(releasesDir, rel.ID.String()+".json")

	if err := rel.Save(relPath); err != nil {
		t.Fatalf("save test release %s: %v", rel.ID, err)
	}
}

// TestGetReleaseState_ExistingRelease verifies that GetReleaseState returns
// the correct stage for an existing Release identified by ID.
//
// AC-1: The current lifecycle stage of any Release is queryable by identity.
//
// Reference: TS-P4-09 AC-1
func TestGetReleaseState_ExistingRelease(t *testing.T) {
	runtimePath := setupQueryTest(t)

	rel := &Release{
		ID:          ReleaseID("test-state-001"),
		Stage:       StageReady,
		Transitions: []TransitionRecord{},
	}
	saveTestRelease(t, runtimePath, rel)

	stage, err := GetReleaseState(runtimePath, rel.ID)
	if err != nil {
		t.Fatalf("GetReleaseState returned unexpected error: %v", err)
	}

	if stage != StageReady {
		t.Errorf("GetReleaseState = %s, want %s", stage, StageReady)
	}
}

// TestGetReleaseState_NonExistentRelease verifies that GetReleaseState
// returns an error when the Release does not exist.
//
// Reference: TS-P4-09 AC-1
func TestGetReleaseState_NonExistentRelease(t *testing.T) {
	runtimePath := setupQueryTest(t)

	_, err := GetReleaseState(runtimePath, ReleaseID("nonexistent"))
	if err == nil {
		t.Fatal("GetReleaseState should return error for non-existent release")
	}
}

// TestListReleasesByStage_NoMatching verifies that ListReleasesByStage
// returns an empty slice when no Releases match the given stage.
//
// AC-2: Releases can be listed by stage (e.g., all Active, all Ready).
//
// Reference: TS-P4-09 AC-2
func TestListReleasesByStage_NoMatching(t *testing.T) {
	runtimePath := setupQueryTest(t)

	// Save a Release in Ready stage.
	rel := &Release{
		ID:          ReleaseID("test-list-001"),
		Stage:       StageReady,
		Transitions: []TransitionRecord{},
	}
	saveTestRelease(t, runtimePath, rel)

	// Query for Active — should return empty.
	active, err := ListReleasesByStage(runtimePath, StageActive)
	if err != nil {
		t.Fatalf("ListReleasesByStage returned unexpected error: %v", err)
	}

	if len(active) != 0 {
		t.Errorf("expected 0 Active releases, got %d", len(active))
	}
}

// TestListReleasesByStage_Matching verifies that ListReleasesByStage
// returns Releases matching the specified stage.
//
// Reference: TS-P4-09 AC-2
func TestListReleasesByStage_Matching(t *testing.T) {
	runtimePath := setupQueryTest(t)

	// Save releases in different stages.
	readyRel := &Release{
		ID:          ReleaseID("test-ready-001"),
		Stage:       StageReady,
		Transitions: []TransitionRecord{},
	}
	saveTestRelease(t, runtimePath, readyRel)

	activeRel := &Release{
		ID:          ReleaseID("test-active-001"),
		Stage:       StageActive,
		Transitions: []TransitionRecord{},
	}
	saveTestRelease(t, runtimePath, activeRel)

	anotherActive := &Release{
		ID:          ReleaseID("test-active-002"),
		Stage:       StageActive,
		Transitions: []TransitionRecord{},
	}
	saveTestRelease(t, runtimePath, anotherActive)

	// Query for Active.
	active, err := ListReleasesByStage(runtimePath, StageActive)
	if err != nil {
		t.Fatalf("ListReleasesByStage returned unexpected error: %v", err)
	}

	if len(active) != 2 {
		t.Errorf("expected 2 Active releases, got %d", len(active))
	}

	// Query for Ready.
	ready, err := ListReleasesByStage(runtimePath, StageReady)
	if err != nil {
		t.Fatalf("ListReleasesByStage returned unexpected error: %v", err)
	}

	if len(ready) != 1 {
		t.Errorf("expected 1 Ready release, got %d", len(ready))
	}
	if ready[0].ID != readyRel.ID {
		t.Errorf("Ready release ID = %s, want %s", ready[0].ID, readyRel.ID)
	}
}

// TestGetActiveRelease_Exists verifies that GetActiveRelease returns the
// Release currently in Active stage.
//
// AC-3: The Active Release for a given Runtime is determinable.
//
// Reference: TS-P4-09 AC-3
func TestGetActiveRelease_Exists(t *testing.T) {
	runtimePath := setupQueryTest(t)

	// Create releases: one in Ready, one in Active.
	readyRel := &Release{
		ID:          ReleaseID("test-ready-002"),
		Stage:       StageReady,
		Transitions: []TransitionRecord{},
	}
	saveTestRelease(t, runtimePath, readyRel)

	activeRel := &Release{
		ID:          ReleaseID("test-active-003"),
		Stage:       StageActive,
		Transitions: []TransitionRecord{},
	}
	saveTestRelease(t, runtimePath, activeRel)

	// Query for Active.
	active, err := GetActiveRelease(runtimePath)
	if err != nil {
		t.Fatalf("GetActiveRelease returned unexpected error: %v", err)
	}

	if active == nil {
		t.Fatal("GetActiveRelease returned nil, expected an Active release")
	}

	if active.ID != activeRel.ID {
		t.Errorf("Active release ID = %s, want %s", active.ID, activeRel.ID)
	}

	if active.Stage != StageActive {
		t.Errorf("Active release Stage = %s, want %s", active.Stage, StageActive)
	}
}

// TestGetActiveRelease_NoActiveRelease verifies that GetActiveRelease
// returns nil when no Release is in Active stage.
//
// Reference: TS-P4-09 AC-3
func TestGetActiveRelease_NoActiveRelease(t *testing.T) {
	runtimePath := setupQueryTest(t)

	// Only a Ready release — no Active.
	rel := &Release{
		ID:          ReleaseID("test-ready-003"),
		Stage:       StageReady,
		Transitions: []TransitionRecord{},
	}
	saveTestRelease(t, runtimePath, rel)

	active, err := GetActiveRelease(runtimePath)
	if err != nil {
		t.Fatalf("GetActiveRelease returned unexpected error: %v", err)
	}

	if active != nil {
		t.Errorf("expected nil Active release, got %v", active)
	}
}

// TestGetActiveRelease_EmptyStateDir verifies that GetActiveRelease returns
// nil when the releases state directory does not exist (first-time setup).
//
// Reference: TS-P4-09 AC-3
func TestGetActiveRelease_EmptyStateDir(t *testing.T) {
	// Use a temp dir without any state directory.
	runtimePath := t.TempDir()

	active, err := GetActiveRelease(runtimePath)
	if err != nil {
		t.Fatalf("GetActiveRelease returned unexpected error: %v", err)
	}

	if active != nil {
		t.Errorf("expected nil Active release, got %v", active)
	}
}

// TestListReleasesByStage_EmptyStateDir verifies that ListReleasesByStage
// returns an empty slice when the state directory does not exist.
//
// Reference: TS-P4-09 AC-2
func TestListReleasesByStage_EmptyStateDir(t *testing.T) {
	runtimePath := t.TempDir()

	result, err := ListReleasesByStage(runtimePath, StageReady)
	if err != nil {
		t.Fatalf("ListReleasesByStage returned unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("ListReleasesByStage returned nil, expected empty slice")
	}
	if len(result) != 0 {
		t.Errorf("expected 0 releases, got %d", len(result))
	}
}

// TestGetReleaseState_NoStateModification verifies that query operations
// do not modify state.
//
// AC-5: State queries do not modify state.
//
// Reference: TS-P4-09 AC-5
func TestGetReleaseState_NoStateModification(t *testing.T) {
	runtimePath := setupQueryTest(t)

	rel := &Release{
		ID:          ReleaseID("test-nomod-001"),
		Stage:       StageReady,
		Transitions: []TransitionRecord{},
	}
	saveTestRelease(t, runtimePath, rel)

	// Read the state file content before query.
	s := project.NewStructure(runtimePath)
	relPath := filepath.Join(s.StateDir, "releases", rel.ID.String()+".json")
	before, err := os.ReadFile(relPath)
	if err != nil {
		t.Fatalf("read release file before query: %v", err)
	}

	// Perform queries.
	_, _ = GetReleaseState(runtimePath, rel.ID)
	_, _ = ListReleasesByStage(runtimePath, StageReady)
	_, _ = GetActiveRelease(runtimePath)

	// Read the state file content after query.
	after, err := os.ReadFile(relPath)
	if err != nil {
		t.Fatalf("read release file after query: %v", err)
	}

	if string(before) != string(after) {
		t.Error("state file content changed after read-only query")
	}
}

// TestGetReleaseState_ErrorOnCorruptFile verifies that GetReleaseState
// returns an error when the release file is corrupt.
func TestGetReleaseState_ErrorOnCorruptFile(t *testing.T) {
	runtimePath := setupQueryTest(t)

	s := project.NewStructure(runtimePath)
	releasesDir := filepath.Join(s.StateDir, "releases")

	// Write an invalid JSON file.
	badPath := filepath.Join(releasesDir, "corrupt.json")
	if err := os.WriteFile(badPath, []byte("{invalid json}"), 0644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	_, err := GetReleaseState(runtimePath, ReleaseID("corrupt"))
	if err == nil {
		t.Fatal("GetReleaseState should return error for corrupt release file")
	}

	if !strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("error should mention unmarshal failure, got: %v", err)
	}
}
