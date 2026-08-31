// Package server provides models and utilities for managing Anvil Server
// Runtime configuration — global Runtime metadata persistence, YAML schema
// definition, defaults, and validation, as well as per-project Registry
// metadata.
//
// This file codifies the state-only runtime scope (TS-015-04-01, ADR-031):
//
//  1. Lifecycle state is persisted, queryable, and authoritative — state,
//     install, activate, and rollback operate within the state-only scope.
//  2. State survives crashes and restarts — a fresh coordinator and a fresh
//     StateStore observe exactly the state the previous session persisted.
//  3. Registry files never carry operational state — the project registry
//     YAML stays purely declarative across the full lifecycle, and the
//     runtime state file never duplicates registry configuration (ADR-013
//     Registry/State separation, ADR-031 §6).
//  4. Decisions derive from state, never from filesystem inference — the
//     active release is determined from persisted state even when the
//     active symlink is missing or points elsewhere (crash-window
//     consistency, TD-002).
//
// Reference: TS-015-04-01, ADR-031, ADR-013, TD-002
package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/project"
	"maleolabs.com/anvil/internal/release"
	"maleolabs.com/anvil/internal/runtime"
)

// runStateOnlyLifecycle executes the full release lifecycle against a fresh
// server root: init + register + install A + activate A + install B +
// activate B + rollback. It returns the project ID, install root, and both
// releases. All operations go through the production coordinator paths.
func runStateOnlyLifecycle(t *testing.T, serverRoot string) (projectID, installRoot string, relA, relB *release.Release) {
	t.Helper()

	projectID, installRoot = setupServerEnv(t, serverRoot)
	relA, relB = runStateOnlyLifecycleOnEnv(t, serverRoot, projectID)
	return projectID, installRoot, relA, relB
}

// runStateOnlyLifecycleOnEnv runs the full release lifecycle (install A,
// activate A, install B, activate B, rollback) against an already-registered
// project environment. It returns both releases.
func runStateOnlyLifecycleOnEnv(t *testing.T, serverRoot, projectID string) (relA, relB *release.Release) {
	t.Helper()

	coordinator := NewServerReleaseCoordinator(serverRoot)

	artifactA := createTestArtifactVariant(t, projectID, "1.0.0", "<?php // state-only A\n")
	relA, err := coordinator.Install(projectID, artifactA)
	if err != nil {
		t.Fatalf("Install A: %v", err)
	}
	if err := coordinator.Activate(projectID, relA.ID.String()); err != nil {
		t.Fatalf("Activate A: %v", err)
	}

	artifactB := createTestArtifactVariant(t, projectID, "1.1.0", "<?php // state-only B\n")
	relB, err = coordinator.Install(projectID, artifactB)
	if err != nil {
		t.Fatalf("Install B: %v", err)
	}
	if err := coordinator.Activate(projectID, relB.ID.String()); err != nil {
		t.Fatalf("Activate B: %v", err)
	}

	if _, err := coordinator.Rollback(projectID); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	return relA, relB
}

// TestInstall_PreservesExistingRuntimeState verifies that installing a second
// release never resets the persisted runtime state: after A is Active, an
// install of B must leave runtime-state.json recording A as the Active
// Release (ADR-031: state is authoritative and survives; install only adds
// state, it never clobbers it).
//
// This is a regression test for the pre-fix behavior where Install
// unconditionally saved a fresh default StateStore, wiping the recorded
// active release on every subsequent install.
func TestInstall_PreservesExistingRuntimeState(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, installRoot := setupServerEnv(t, serverRoot)

	coordinator := NewServerReleaseCoordinator(serverRoot)

	relA, err := coordinator.Install(projectID, createTestArtifactVariant(t, projectID, "1.0.0", "<?php // A\n"))
	if err != nil {
		t.Fatalf("Install A: %v", err)
	}
	if err := coordinator.Activate(projectID, relA.ID.String()); err != nil {
		t.Fatalf("Activate A: %v", err)
	}
	assertRuntimeActiveReleaseID(t, installRoot, relA.ID.String())

	// Install B while A is Active. The persisted state must still record A
	// as Active — Install must not reset runtime-state.json.
	relB, err := coordinator.Install(projectID, createTestArtifactVariant(t, projectID, "1.1.0", "<?php // B\n"))
	if err != nil {
		t.Fatalf("Install B: %v", err)
	}
	if relB.Stage != release.StageReady {
		t.Errorf("release B stage after install = %s, want %s", relB.Stage, release.StageReady)
	}
	assertRuntimeActiveReleaseID(t, installRoot, relA.ID.String())

	// The state file must still be readable and complete after the install.
	stateStore := runtime.NewStateStore(filepath.Join(installRoot, "runtime-state.json"))
	if err := stateStore.Load(); err != nil {
		t.Fatalf("load runtime state after second install: %v", err)
	}
	if got := stateStore.State().ActiveReleaseID; got != relA.ID.String() {
		t.Errorf("runtime ActiveReleaseID after install B = %q, want %q (install must not reset state)", got, relA.ID.String())
	}
}

// TestActivateAndRollback_PreserveUnrelatedRuntimeState verifies that
// activate and rollback update only the active release record: unrelated
// operational state recorded in runtime-state.json (runtime condition,
// shared resource status) survives both operations (ADR-031: state is
// authoritative; lifecycle operations derive from state and never reset it).
func TestActivateAndRollback_PreserveUnrelatedRuntimeState(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, installRoot := setupServerEnv(t, serverRoot)

	coordinator := NewServerReleaseCoordinator(serverRoot)

	relA, err := coordinator.Install(projectID, createTestArtifactVariant(t, projectID, "1.0.0", "<?php // preserve A\n"))
	if err != nil {
		t.Fatalf("Install A: %v", err)
	}
	if err := coordinator.Activate(projectID, relA.ID.String()); err != nil {
		t.Fatalf("Activate A: %v", err)
	}

	// Record unrelated operational state (as an observability consumer
	// would): the runtime is degraded and shared resources inaccessible.
	statePath := filepath.Join(installRoot, "runtime-state.json")
	stateStore := runtime.NewStateStore(statePath)
	if err := stateStore.Load(); err != nil {
		t.Fatalf("load runtime state: %v", err)
	}
	stateStore.SetRuntimeCondition(runtime.ConditionDegraded)
	stateStore.SetSharedResourceStatus(runtime.ResourceInaccessible)
	if err := stateStore.Save(); err != nil {
		t.Fatalf("save runtime state with condition: %v", err)
	}

	// Activate B: the active release changes, the condition must not.
	relB, err := coordinator.Install(projectID, createTestArtifactVariant(t, projectID, "1.1.0", "<?php // preserve B\n"))
	if err != nil {
		t.Fatalf("Install B: %v", err)
	}
	if err := coordinator.Activate(projectID, relB.ID.String()); err != nil {
		t.Fatalf("Activate B: %v", err)
	}

	assertRuntimeActiveReleaseID(t, installRoot, relB.ID.String())
	stateStore = runtime.NewStateStore(statePath)
	if err := stateStore.Load(); err != nil {
		t.Fatalf("load runtime state after activate: %v", err)
	}
	state := stateStore.State()
	if state.RuntimeCondition != runtime.ConditionDegraded {
		t.Errorf("runtime condition after activate = %s, want %s (activate must not reset state)", state.RuntimeCondition, runtime.ConditionDegraded)
	}
	if state.SharedResourceStatus != runtime.ResourceInaccessible {
		t.Errorf("shared resource status after activate = %s, want %s (activate must not reset state)", state.SharedResourceStatus, runtime.ResourceInaccessible)
	}

	// Rollback: the restored release becomes active, the condition must
	// survive here too.
	if _, err := coordinator.Rollback(projectID); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	assertRuntimeActiveReleaseID(t, installRoot, relA.ID.String())
	stateStore = runtime.NewStateStore(statePath)
	if err := stateStore.Load(); err != nil {
		t.Fatalf("load runtime state after rollback: %v", err)
	}
	state = stateStore.State()
	if state.RuntimeCondition != runtime.ConditionDegraded {
		t.Errorf("runtime condition after rollback = %s, want %s (rollback must not reset state)", state.RuntimeCondition, runtime.ConditionDegraded)
	}
	if state.SharedResourceStatus != runtime.ResourceInaccessible {
		t.Errorf("shared resource status after rollback = %s, want %s (rollback must not reset state)", state.SharedResourceStatus, runtime.ResourceInaccessible)
	}
}

// TestStateSurvivesRestart verifies that lifecycle state survives process
// restarts: every decision point reached through a fresh coordinator and a
// fresh StateStore (simulating a new process) reflects exactly the state the
// previous session persisted — no memory or filesystem inference is needed.
func TestStateSurvivesRestart(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, installRoot := setupServerEnv(t, serverRoot)

	// Session 1: install A, activate A, install B, activate B.
	session1 := NewServerReleaseCoordinator(serverRoot)
	artifactA := createTestArtifactVariant(t, projectID, "1.0.0", "<?php // restart A\n")
	relA, err := session1.Install(projectID, artifactA)
	if err != nil {
		t.Fatalf("Install A: %v", err)
	}
	if err := session1.Activate(projectID, relA.ID.String()); err != nil {
		t.Fatalf("Activate A: %v", err)
	}
	artifactB := createTestArtifactVariant(t, projectID, "1.1.0", "<?php // restart B\n")
	relB, err := session1.Install(projectID, artifactB)
	if err != nil {
		t.Fatalf("Install B: %v", err)
	}
	if err := session1.Activate(projectID, relB.ID.String()); err != nil {
		t.Fatalf("Activate B: %v", err)
	}

	// Restart: a fresh process observes the persisted state. The active
	// release, every release stage, and the runtime state file must all
	// report B as Active and A as Archived.
	assertActiveRelease(t, installRoot, relB.ID.String())
	assertStatusSnapshot(t, installRoot, map[release.ReleaseID]release.Stage{
		relA.ID: release.StageArchived,
		relB.ID: release.StageActive,
	})

	// Session 2 (fresh process): rollback operates on the persisted state
	// and persists the restored state.
	session2 := NewServerReleaseCoordinator(serverRoot)
	result, err := session2.Rollback(projectID)
	if err != nil {
		t.Fatalf("Rollback after restart: %v", err)
	}
	if result.RestoredRelease == nil || result.RestoredRelease.ID != relA.ID {
		t.Errorf("restored release = %v, want A (%s)", result.RestoredRelease, relA.ID)
	}

	// Second restart: every read path — active query, status query, and the
	// runtime state file — reports A as Active and B as RolledBack.
	assertActiveRelease(t, installRoot, relA.ID.String())
	assertStatusSnapshot(t, installRoot, map[release.ReleaseID]release.Stage{
		relA.ID: release.StageActive,
		relB.ID: release.StageRolledBack,
	})
}

// TestRegistryFilesNeverCarryOperationalState verifies the ADR-013
// Registry/State separation within the state-only scope (ADR-031 §6):
//
//   - the project registry YAML is purely declarative — the full lifecycle
//     leaves it byte-identical, and it never contains operational state
//     fields (active release, lifecycle stage, conditions, timestamps);
//   - the runtime state file never duplicates registry configuration
//     (install root, adapter, and — before their demotion — owner, group,
//     shared links).
//
// It also verifies the ADR-031 §3 demotion: the registry written by Anvil
// never carries the demoted ownership/shared-links keys (TS-015-04-02).
func TestRegistryFilesNeverCarryOperationalState(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, _ := setupServerEnv(t, serverRoot)

	// Snapshot the registry file as written by registration — before any
	// lifecycle operation touches the server.
	registryPath := filepath.Join(serverRoot, "projects", projectID+".yaml")
	registryBefore, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatalf("read registry before lifecycle: %v", err)
	}

	// Run the full lifecycle: install A, activate A, install B, activate B,
	// rollback.
	installRoot := filepath.Join(serverRoot, "projects", projectID)
	relA, relB := runStateOnlyLifecycleOnEnv(t, serverRoot, projectID)

	// The registry file must be byte-identical: lifecycle operations never
	// write registry files, so they cannot smuggle operational state in.
	registryAfter, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatalf("read registry after lifecycle: %v", err)
	}
	if string(registryAfter) != string(registryBefore) {
		t.Errorf("project registry changed across the lifecycle:\nbefore:\n%s\nafter:\n%s",
			registryBefore, registryAfter)
	}

	// The registry content must not contain operational state fields.
	registryText := string(registryAfter)
	for _, opField := range []string{
		"active_release_id", "runtime_condition", "shared_resource_status",
		"last_updated", "stage", "artifact_id",
	} {
		if strings.Contains(registryText, opField) {
			t.Errorf("project registry carries operational state field %q — registry files must never carry operational state (ADR-031 §6)", opField)
		}
	}

	// The registry must never carry the demoted ownership/shared-links keys
	// (ADR-031 §3, TS-015-04-02).
	for _, demotedKey := range []string{"owner", "group", "shared_links"} {
		if strings.Contains(registryText, demotedKey) {
			t.Errorf("project registry carries demoted key %q — ownership/shared-links keys are not part of the v2 runtime (ADR-031 §3)", demotedKey)
		}
	}

	// The registry must still declare only its declarative identity keys.
	for _, declField := range []string{"id", "install_root", "display_name"} {
		if !strings.Contains(registryText, declField) {
			t.Errorf("project registry lost declarative field %q — registry must keep its declarative metadata", declField)
		}
	}

	// The runtime state file must not duplicate registry configuration.
	stateData, err := os.ReadFile(filepath.Join(installRoot, "runtime-state.json"))
	if err != nil {
		t.Fatalf("read runtime state: %v", err)
	}
	stateText := string(stateData)
	for _, cfgField := range []string{
		"install_root", "display_name", "adapter", "owner", "group", "shared_links",
	} {
		if strings.Contains(stateText, cfgField) {
			t.Errorf("runtime state duplicates registry configuration field %q — state must not duplicate registry configuration (ADR-013)", cfgField)
		}
	}

	// The state file carries exactly the expected operational fields, and
	// the recorded active release matches the queryable state.
	for _, stateField := range []string{"active_release_id", "runtime_condition", "shared_resource_status", "last_updated"} {
		if !strings.Contains(stateText, stateField) {
			t.Errorf("runtime state missing operational field %q", stateField)
		}
	}
	assertRuntimeActiveReleaseID(t, installRoot, relA.ID.String())

	// Release state files live in the project state directory — the
	// authoritative per-release record — and are never registry files.
	releasesStateDir := filepath.Join(project.NewStructure(installRoot).StateDir, "releases")
	for _, rel := range []*release.Release{relA, relB} {
		relPath := filepath.Join(releasesStateDir, rel.ID.String()+".json")
		if _, err := os.Stat(relPath); err != nil {
			t.Errorf("release state file %s: %v", relPath, err)
		}
	}
}

// TestDecisionsDeriveFromStateNotFilesystemInference verifies that the
// active release decision is derived from persisted state, never from the
// filesystem (ADR-031: decisions derive from state, never from memory or
// filesystem inference). The active symlink is a serving convenience, not a
// decision input: removing it (crash-window consistency, TD-002) must not
// change what the query paths report.
func TestDecisionsDeriveFromStateNotFilesystemInference(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, installRoot := setupServerEnv(t, serverRoot)

	coordinator := NewServerReleaseCoordinator(serverRoot)

	relA, err := coordinator.Install(projectID, createTestArtifactVariant(t, projectID, "1.0.0", "<?php // infer A\n"))
	if err != nil {
		t.Fatalf("Install A: %v", err)
	}
	if err := coordinator.Activate(projectID, relA.ID.String()); err != nil {
		t.Fatalf("Activate A: %v", err)
	}
	relB, err := coordinator.Install(projectID, createTestArtifactVariant(t, projectID, "1.1.0", "<?php // infer B\n"))
	if err != nil {
		t.Fatalf("Install B: %v", err)
	}
	if err := coordinator.Activate(projectID, relB.ID.String()); err != nil {
		t.Fatalf("Activate B: %v", err)
	}

	// Pre-condition: the filesystem symlink and the persisted state agree
	// on B.
	assertActiveRelease(t, installRoot, relB.ID.String())

	// Simulate the crash-window case: the symlink is removed while the
	// persisted state still records B as Active (TD-002 — the symlink is
	// switched before the state persists, so a crash between the two leaves
	// the filesystem behind the state). The active release query must still
	// derive B from the persisted state.
	activeSymlink := filepath.Join(installRoot, "current")
	if err := os.Remove(activeSymlink); err != nil {
		t.Fatalf("remove active symlink: %v", err)
	}

	assertActiveRelease(t, installRoot, relB.ID.String())

	// The state file must be unchanged by the query paths (queries are
	// read-only — they never repair or rewrite state).
	assertStatusSnapshot(t, installRoot, map[release.ReleaseID]release.Stage{
		relA.ID: release.StageArchived,
		relB.ID: release.StageActive,
	})
}
