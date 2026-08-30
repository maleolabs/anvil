// Package release defines the Release model and lifecycle stage management
// for Anvil Runtime Releases.
//
// This file validates the lifecycle observability queries (TS-015-05-01):
// FindRollbackTarget and GetRollbackEligibility — read-only queries over the
// authoritative lifecycle state.
//
// Reference: TS-015-05-01, ADR-036 §3, ADR-031
package release

import (
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/project"
)

// setupObservabilityRuntime creates a temporary runtime with the given
// Releases persisted through the production Save path
// (<runtimePath>/.anvil/state/releases/<id>.json). Each stage entry maps a
// Release ID to its lifecycle stage.
func setupObservabilityRuntime(t *testing.T, stages map[string]Stage) (runtimePath string) {
	t.Helper()

	dir := t.TempDir()
	s := project.NewStructure(dir)
	releasesStateDir := filepath.Join(s.StateDir, "releases")
	if err := os.MkdirAll(releasesStateDir, 0755); err != nil {
		t.Fatalf("mkdir releases state dir: %v", err)
	}

	for id, stage := range stages {
		rel := &Release{
			ID:          ReleaseID(id),
			Version:     "1.0.0",
			Stage:       stage,
			RuntimePath: dir,
		}
		// Record the transition that produced the stage so target
		// identification can order Archived Releases by timestamp.
		rel.Transitions = []TransitionRecord{{
			Timestamp: "2026-08-01T10:00:00Z",
			From:      StageReady,
			To:        stage,
			Outcome:   "success",
		}}
		if err := rel.Save(rel.SavePath(dir)); err != nil {
			t.Fatalf("save release %s: %v", id, err)
		}
	}

	return dir
}

// archivedRelease builds an Archived Release with a specific archival
// timestamp, so tests can control which Archived Release is "most recent".
func archivedRelease(t *testing.T, runtimePath, id, timestamp string) {
	t.Helper()

	rel := &Release{
		ID:          ReleaseID(id),
		Version:     "1.0.0",
		Stage:       StageArchived,
		RuntimePath: runtimePath,
		Transitions: []TransitionRecord{{
			Timestamp: timestamp,
			From:      StageActive,
			To:        StageArchived,
			Outcome:   "success",
		}},
	}
	if err := rel.Save(rel.SavePath(runtimePath)); err != nil {
		t.Fatalf("save release %s: %v", id, err)
	}
}

// TestFindRollbackTarget_ReturnsMostRecentlyArchived verifies that
// FindRollbackTarget returns the Archived Release with the most recent
// transition to Archived — the Release that was Active before the current
// one (TS-P4-07 target identification, observed read-only).
func TestFindRollbackTarget_ReturnsMostRecentlyArchived(t *testing.T) {
	runtimePath := setupObservabilityRuntime(t, map[string]Stage{
		"release-current": StageActive,
	})

	archivedRelease(t, runtimePath, "release-older", "2026-08-01T09:00:00Z")
	archivedRelease(t, runtimePath, "release-newer", "2026-08-02T09:00:00Z")

	target, err := FindRollbackTarget(runtimePath)
	if err != nil {
		t.Fatalf("FindRollbackTarget: %v", err)
	}
	if target == nil {
		t.Fatal("FindRollbackTarget = nil, want the most recently Archived Release")
	}
	if target.ID != ReleaseID("release-newer") {
		t.Errorf("FindRollbackTarget = %s, want release-newer (most recent archival)", target.ID)
	}
}

// TestFindRollbackTarget_NoArchived verifies that FindRollbackTarget
// returns nil without error when no Archived Release exists.
func TestFindRollbackTarget_NoArchived(t *testing.T) {
	runtimePath := setupObservabilityRuntime(t, map[string]Stage{
		"release-ready":  StageReady,
		"release-active": StageActive,
	})

	target, err := FindRollbackTarget(runtimePath)
	if err != nil {
		t.Fatalf("FindRollbackTarget: %v", err)
	}
	if target != nil {
		t.Errorf("FindRollbackTarget = %v, want nil when no Archived Release exists", target.ID)
	}
}

// TestFindRollbackTarget_EmptyRuntime verifies that FindRollbackTarget
// returns nil without error when no Releases exist at all.
func TestFindRollbackTarget_EmptyRuntime(t *testing.T) {
	runtimePath := t.TempDir()

	target, err := FindRollbackTarget(runtimePath)
	if err != nil {
		t.Fatalf("FindRollbackTarget: %v", err)
	}
	if target != nil {
		t.Errorf("FindRollbackTarget = %v, want nil on empty runtime", target.ID)
	}
}

// TestGetRollbackEligibility_Eligible verifies that rollback is reported
// eligible exactly when an Active Release and a previously Active
// (Archived) Release are both present, with both identities reported.
func TestGetRollbackEligibility_Eligible(t *testing.T) {
	runtimePath := setupObservabilityRuntime(t, map[string]Stage{
		"release-current": StageActive,
	})
	archivedRelease(t, runtimePath, "release-prev", "2026-08-01T09:00:00Z")

	eligibility, err := GetRollbackEligibility(runtimePath)
	if err != nil {
		t.Fatalf("GetRollbackEligibility: %v", err)
	}
	if !eligibility.Eligible {
		t.Errorf("rollback eligible = false, want true; reason: %q", eligibility.Reason)
	}
	if eligibility.ActiveReleaseID != ReleaseID("release-current") {
		t.Errorf("ActiveReleaseID = %s, want release-current", eligibility.ActiveReleaseID)
	}
	if eligibility.TargetReleaseID != ReleaseID("release-prev") {
		t.Errorf("TargetReleaseID = %s, want release-prev", eligibility.TargetReleaseID)
	}
	if eligibility.Reason != "" {
		t.Errorf("Reason = %q, want empty when eligible", eligibility.Reason)
	}
}

// TestGetRollbackEligibility_NoActiveRelease verifies that rollback is
// reported not eligible with an explanatory reason when no Release is
// Active — there is nothing to roll back.
func TestGetRollbackEligibility_NoActiveRelease(t *testing.T) {
	runtimePath := setupObservabilityRuntime(t, map[string]Stage{
		"release-ready":    StageReady,
		"release-archived": StageArchived,
	})

	eligibility, err := GetRollbackEligibility(runtimePath)
	if err != nil {
		t.Fatalf("GetRollbackEligibility: %v", err)
	}
	if eligibility.Eligible {
		t.Error("rollback eligible = true, want false when no Active Release")
	}
	if eligibility.ActiveReleaseID != "" {
		t.Errorf("ActiveReleaseID = %s, want empty", eligibility.ActiveReleaseID)
	}
	if eligibility.Reason == "" {
		t.Error("Reason = empty, want explanation when not eligible")
	}
}

// TestGetRollbackEligibility_NoTarget verifies that rollback is reported
// not eligible with an explanatory reason when an Active Release exists
// but no previously Active (Archived) Release is present.
func TestGetRollbackEligibility_NoTarget(t *testing.T) {
	runtimePath := setupObservabilityRuntime(t, map[string]Stage{
		"release-current": StageActive,
	})

	eligibility, err := GetRollbackEligibility(runtimePath)
	if err != nil {
		t.Fatalf("GetRollbackEligibility: %v", err)
	}
	if eligibility.Eligible {
		t.Error("rollback eligible = true, want false when no rollback target")
	}
	if eligibility.ActiveReleaseID != ReleaseID("release-current") {
		t.Errorf("ActiveReleaseID = %s, want release-current", eligibility.ActiveReleaseID)
	}
	if eligibility.TargetReleaseID != "" {
		t.Errorf("TargetReleaseID = %s, want empty", eligibility.TargetReleaseID)
	}
	if eligibility.Reason == "" {
		t.Error("Reason = empty, want explanation when not eligible")
	}
}

// TestGetRollbackEligibility_EmptyRuntime verifies that the eligibility
// query reports not eligible (no error) on a runtime with no state at all.
func TestGetRollbackEligibility_EmptyRuntime(t *testing.T) {
	runtimePath := t.TempDir()

	eligibility, err := GetRollbackEligibility(runtimePath)
	if err != nil {
		t.Fatalf("GetRollbackEligibility: %v", err)
	}
	if eligibility.Eligible {
		t.Error("rollback eligible = true, want false on empty runtime")
	}
}

// TestObservabilityQueries_ReadOnly verifies that the observability queries
// never mutate state: every persisted Release file is byte-identical after
// the queries run (TS-015-05-01 — the surface observes, it never repairs or
// rewrites).
func TestObservabilityQueries_ReadOnly(t *testing.T) {
	runtimePath := setupObservabilityRuntime(t, map[string]Stage{
		"release-current": StageActive,
	})
	archivedRelease(t, runtimePath, "release-prev", "2026-08-01T09:00:00Z")

	s := project.NewStructure(runtimePath)
	releasesStateDir := filepath.Join(s.StateDir, "releases")
	before := readReleaseFiles(t, releasesStateDir)

	if _, err := FindRollbackTarget(runtimePath); err != nil {
		t.Fatalf("FindRollbackTarget: %v", err)
	}
	if _, err := GetRollbackEligibility(runtimePath); err != nil {
		t.Fatalf("GetRollbackEligibility: %v", err)
	}

	after := readReleaseFiles(t, releasesStateDir)
	for path, want := range before {
		if got := after[path]; got != want {
			t.Errorf("release file %s changed by read-only observability query", path)
		}
	}
	if len(after) != len(before) {
		t.Errorf("observability query created/removed release files: before=%d after=%d", len(before), len(after))
	}
}

// readReleaseFiles reads every release JSON file's content into a map keyed
// by path, for read-only assertions.
func readReleaseFiles(t *testing.T, releasesStateDir string) map[string]string {
	t.Helper()

	files := map[string]string{}
	entries, err := os.ReadDir(releasesStateDir)
	if err != nil {
		t.Fatalf("read releases state dir: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(releasesStateDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		files[path] = string(data)
	}
	return files
}
