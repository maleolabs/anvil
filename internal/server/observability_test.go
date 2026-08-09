// Package server provides models and utilities for managing Anvil Server
// Runtime configuration — global Runtime metadata persistence, YAML schema
// definition, defaults, and validation, as well as per-project Registry
// metadata.
//
// This file validates the lifecycle observability query (TS-015-05-01,
// ADR-036 §3): QueryLifecycleStatus reports what is active, what is
// installed, what can roll back, release status, and runtime state — read
// from the authoritative lifecycle state (ADR-031 §3) and never inferred
// from memory or filesystem symlinks. The query is read-only.
//
// Reference: TS-015-05-01, ADR-036 §3, ADR-031
package server

import (
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/release"
	"maleolabs.com/anvil/internal/runtime"
)

// TestQueryLifecycleStatus_ReportsAuthoritativeState verifies that the
// lifecycle observability query reports the full lifecycle snapshot after
// real lifecycle operations: install A + activate A, then install B +
// activate B (archiving A). Every observation must match the authoritative
// read paths:
//
//   - active → release.GetActiveRelease (B)
//   - installed → release.ListReleases (A archived, B active)
//   - rollback → eligible, restoring A (the previously Active Release)
//   - runtime state → runtime.StateStore (records B)
func TestQueryLifecycleStatus_ReportsAuthoritativeState(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, installRoot := setupServerEnv(t, serverRoot)

	coordinator := NewServerReleaseCoordinator(serverRoot)

	relA, err := coordinator.Install(projectID, createTestArtifactVariant(t, projectID, "1.0.0", "<?php // obs A\n"))
	if err != nil {
		t.Fatalf("Install A: %v", err)
	}
	if err := coordinator.Activate(projectID, relA.ID.String()); err != nil {
		t.Fatalf("Activate A: %v", err)
	}
	relB, err := coordinator.Install(projectID, createTestArtifactVariant(t, projectID, "1.1.0", "<?php // obs B\n"))
	if err != nil {
		t.Fatalf("Install B: %v", err)
	}
	if err := coordinator.Activate(projectID, relB.ID.String()); err != nil {
		t.Fatalf("Activate B: %v", err)
	}

	status, err := QueryLifecycleStatus(serverRoot, projectID)
	if err != nil {
		t.Fatalf("QueryLifecycleStatus: %v", err)
	}

	// What is active: must match release.GetActiveRelease.
	active, err := release.GetActiveRelease(installRoot)
	if err != nil {
		t.Fatalf("GetActiveRelease: %v", err)
	}
	if status.Active == nil {
		t.Fatal("Active = nil, want the Active Release B")
	}
	if status.Active.ReleaseID != relB.ID.String() || active.ID != relB.ID {
		t.Errorf("Active = %v, want %s (authoritative: %s)", status.Active, relB.ID, active.ID)
	}
	if status.Active.Stage != release.StageActive.String() {
		t.Errorf("Active stage = %q, want %q", status.Active.Stage, release.StageActive)
	}

	// What is installed + release status: every Release with its stage,
	// matching release.ListReleases.
	if len(status.Installed) != 2 {
		t.Fatalf("Installed = %d release(s), want 2", len(status.Installed))
	}
	stages := map[string]string{}
	for _, rel := range status.Installed {
		stages[rel.ReleaseID] = rel.Stage
	}
	if stages[relA.ID.String()] != release.StageArchived.String() {
		t.Errorf("Release A stage = %q, want %q", stages[relA.ID.String()], release.StageArchived)
	}
	if stages[relB.ID.String()] != release.StageActive.String() {
		t.Errorf("Release B stage = %q, want %q", stages[relB.ID.String()], release.StageActive)
	}

	// What can roll back: eligible, restoring A.
	if !status.Rollback.Eligible {
		t.Fatalf("Rollback.Eligible = false, want true; reason: %q", status.Rollback.Reason)
	}
	if status.Rollback.TargetReleaseID != relA.ID.String() {
		t.Errorf("Rollback.TargetReleaseID = %s, want %s (previously Active Release)", status.Rollback.TargetReleaseID, relA.ID)
	}
	if status.Rollback.ActiveReleaseID != relB.ID.String() {
		t.Errorf("Rollback.ActiveReleaseID = %s, want %s", status.Rollback.ActiveReleaseID, relB.ID)
	}

	// State queries: the persisted Runtime state must record B as active.
	if !status.RuntimeState.Recorded {
		t.Fatal("RuntimeState.Recorded = false, want true")
	}
	if status.RuntimeState.ActiveReleaseID != relB.ID.String() {
		t.Errorf("RuntimeState.ActiveReleaseID = %s, want %s", status.RuntimeState.ActiveReleaseID, relB.ID)
	}
	if status.RuntimeState.RuntimeCondition != string(runtime.ConditionNormal) {
		t.Errorf("RuntimeState.RuntimeCondition = %q, want %q", status.RuntimeState.RuntimeCondition, runtime.ConditionNormal)
	}
	if status.RuntimeState.LoadError != "" {
		t.Errorf("RuntimeState.LoadError = %q, want empty", status.RuntimeState.LoadError)
	}
}

// TestQueryLifecycleStatus_ActiveDerivedFromStateNotSymlink verifies the
// ADR-031 alignment: the observability surface reports the Active Release
// from the authoritative persisted state even when the filesystem symlink
// is missing (crash-window consistency, TD-002) — never by symlink
// inference.
func TestQueryLifecycleStatus_ActiveDerivedFromStateNotSymlink(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, installRoot := setupServerEnv(t, serverRoot)

	coordinator := NewServerReleaseCoordinator(serverRoot)

	rel, err := coordinator.Install(projectID, createTestArtifactVariant(t, projectID, "1.0.0", "<?php // obs symlink\n"))
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := coordinator.Activate(projectID, rel.ID.String()); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	// Simulate the crash window: the symlink is removed while the persisted
	// state still records the Release as Active.
	activeSymlink := filepath.Join(installRoot, "current")
	if err := os.Remove(activeSymlink); err != nil {
		t.Fatalf("remove active symlink: %v", err)
	}

	status, err := QueryLifecycleStatus(serverRoot, projectID)
	if err != nil {
		t.Fatalf("QueryLifecycleStatus: %v", err)
	}
	if status.Active == nil || status.Active.ReleaseID != rel.ID.String() {
		t.Errorf("Active = %v, want %s derived from persisted state (symlink removed)", status.Active, rel.ID)
	}
}

// TestQueryLifecycleStatus_RegisteredProjectNoLifecycle verifies that a
// registered project with no lifecycle activity reports an empty-but-valid
// snapshot: no Active Release, no installed Releases, rollback not
// eligible, runtime state not recorded — and no error.
func TestQueryLifecycleStatus_RegisteredProjectNoLifecycle(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, _ := setupServerEnv(t, serverRoot)

	status, err := QueryLifecycleStatus(serverRoot, projectID)
	if err != nil {
		t.Fatalf("QueryLifecycleStatus: %v", err)
	}

	if status.Active != nil {
		t.Errorf("Active = %v, want nil", status.Active)
	}
	if len(status.Installed) != 0 {
		t.Errorf("Installed = %v, want empty", status.Installed)
	}
	if status.Rollback.Eligible {
		t.Errorf("Rollback.Eligible = true, want false")
	}
	if status.Rollback.Reason == "" {
		t.Error("Rollback.Reason = empty, want explanation")
	}
	if status.RuntimeState.Recorded {
		t.Errorf("RuntimeState.Recorded = true, want false (no state file yet)")
	}
	if status.RuntimeState.LoadError != "" {
		t.Errorf("RuntimeState.LoadError = %q, want empty", status.RuntimeState.LoadError)
	}
}

// TestQueryLifecycleStatus_NotEligibleOnFirstActivation verifies the
// rollback-able observation: after the first activation there is no
// previously Active Release, so rollback is reported not eligible even
// though a Release is Active.
func TestQueryLifecycleStatus_NotEligibleOnFirstActivation(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, _ := setupServerEnv(t, serverRoot)

	coordinator := NewServerReleaseCoordinator(serverRoot)

	rel, err := coordinator.Install(projectID, createTestArtifactVariant(t, projectID, "1.0.0", "<?php // obs first\n"))
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := coordinator.Activate(projectID, rel.ID.String()); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	status, err := QueryLifecycleStatus(serverRoot, projectID)
	if err != nil {
		t.Fatalf("QueryLifecycleStatus: %v", err)
	}

	if status.Active == nil {
		t.Fatal("Active = nil, want the first Active Release")
	}
	if status.Rollback.Eligible {
		t.Error("Rollback.Eligible = true, want false on first activation (no previously Active Release)")
	}
	if status.Rollback.ActiveReleaseID != rel.ID.String() {
		t.Errorf("Rollback.ActiveReleaseID = %s, want %s", status.Rollback.ActiveReleaseID, rel.ID)
	}
	if status.Rollback.TargetReleaseID != "" {
		t.Errorf("Rollback.TargetReleaseID = %s, want empty", status.Rollback.TargetReleaseID)
	}
	if status.Rollback.Reason == "" {
		t.Error("Rollback.Reason = empty, want explanation")
	}
}

// TestQueryLifecycleStatus_CorruptStateFile verifies that a corrupt runtime
// state file is surfaced as a load error instead of being silently ignored
// or overwritten (ADR-031: corrupt state is preserved for recovery).
func TestQueryLifecycleStatus_CorruptStateFile(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, installRoot := setupServerEnv(t, serverRoot)

	if err := os.MkdirAll(installRoot, 0755); err != nil {
		t.Fatalf("create install root: %v", err)
	}
	statePath := filepath.Join(installRoot, "runtime-state.json")
	if err := os.WriteFile(statePath, []byte("{ not json"), 0644); err != nil {
		t.Fatalf("write corrupt state file: %v", err)
	}

	status, err := QueryLifecycleStatus(serverRoot, projectID)
	if err != nil {
		t.Fatalf("QueryLifecycleStatus: %v", err)
	}

	if status.RuntimeState.Recorded {
		t.Error("RuntimeState.Recorded = true, want false for corrupt state file")
	}
	if status.RuntimeState.LoadError == "" {
		t.Error("RuntimeState.LoadError = empty, want the load error surfaced")
	}
}

// TestQueryLifecycleStatus_UnknownProject verifies that the query errors
// for a project that is not registered.
func TestQueryLifecycleStatus_UnknownProject(t *testing.T) {
	serverRoot := t.TempDir()

	_, err := QueryLifecycleStatus(serverRoot, "no-such-project")
	if err == nil {
		t.Fatal("QueryLifecycleStatus = nil error, want error for unknown project")
	}
}

// TestQueryLifecycleStatus_ReadOnly verifies the read-only property: after
// the query runs against a populated lifecycle, every state file under the
// server root is byte-identical — the surface observes and never mutates,
// repairs, or rewrites state (TS-015-05-01).
func TestQueryLifecycleStatus_ReadOnly(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, _ := setupServerEnv(t, serverRoot)

	coordinator := NewServerReleaseCoordinator(serverRoot)

	relA, err := coordinator.Install(projectID, createTestArtifactVariant(t, projectID, "1.0.0", "<?php // obs ro A\n"))
	if err != nil {
		t.Fatalf("Install A: %v", err)
	}
	if err := coordinator.Activate(projectID, relA.ID.String()); err != nil {
		t.Fatalf("Activate A: %v", err)
	}
	relB, err := coordinator.Install(projectID, createTestArtifactVariant(t, projectID, "1.1.0", "<?php // obs ro B\n"))
	if err != nil {
		t.Fatalf("Install B: %v", err)
	}
	if err := coordinator.Activate(projectID, relB.ID.String()); err != nil {
		t.Fatalf("Activate B: %v", err)
	}

	before := snapshotTree(t, serverRoot)

	if _, err := QueryLifecycleStatus(serverRoot, projectID); err != nil {
		t.Fatalf("QueryLifecycleStatus: %v", err)
	}
	if _, err := QueryLifecycleStatus(serverRoot, projectID); err != nil {
		t.Fatalf("QueryLifecycleStatus (repeat): %v", err)
	}

	after := snapshotTree(t, serverRoot)
	for path, want := range before {
		if got, ok := after[path]; !ok {
			t.Errorf("file %s removed by observability query", path)
		} else if got != want {
			t.Errorf("file %s changed by observability query", path)
		}
	}
	if len(after) != len(before) {
		t.Errorf("observability query created files: before=%d after=%d", len(before), len(after))
	}

	// The persisted release stages must be untouched as well.
	status, err := QueryLifecycleStatus(serverRoot, projectID)
	if err != nil {
		t.Fatalf("QueryLifecycleStatus: %v", err)
	}
	stages := map[string]string{}
	for _, rel := range status.Installed {
		stages[rel.ReleaseID] = rel.Stage
	}
	if stages[relA.ID.String()] != release.StageArchived.String() || stages[relB.ID.String()] != release.StageActive.String() {
		t.Errorf("release stages changed by queries: %v", stages)
	}
}

// snapshotTree returns the content of every regular file under root, keyed
// by relative path.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()

	files := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot tree %s: %v", root, err)
	}
	return files
}
