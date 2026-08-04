// Package server provides models and utilities for managing Anvil Server
// Runtime configuration — global Runtime metadata persistence, YAML schema
// definition, defaults, and validation, as well as per-project Registry
// metadata.
//
// This file codifies the ST-012-001 release lifecycle E2E validation: the
// full install → activate (A) → activate (B, archiving A) → rollback
// sequence against a fresh server root, with stage observability asserted
// through the exact read path the `server release status` command uses
// (TS-012-002).
//
// The sequence reproduces the two REV-009 failure modes that escaped the
// fixture-based unit tests:
//
//	F1 — state directory mismatch: the coordinator persisted releases to
//	     <installRoot>/state/releases while internal/release read
//	     <installRoot>/.anvil/state/releases, so the coordinator's own
//	     lifecycle was invisible to observability and rollback. Every
//	     assertion here reads through the production paths (BUG-002).
//	F2 — ActiveReleaseInvariant never wired: no Release ever reached
//	     Archived, so the rollback engine could never find a target.
//	     Step 4 asserts A reaches Archived before rollback (BUG-003).
//
// Reference: ST-012-001, BUG-002, BUG-003, BUG-004, TS-012-002,
// MVP-001 AC 9.6, AC 9.7
package server

import (
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/project"
	"maleolabs.com/anvil/internal/release"
)

// ---------------------------------------------------------------------------
// Release lifecycle E2E (ST-012-001)
//
// The sequence runs against a fresh server root with no pre-seeded state
// and no fixture shortcuts: `server init` (InitializeServer), `server
// project register` (RegistryStore.Register), `artifact package`
// (artifact.Package), and the coordinator Install/Activate/Rollback
// methods — the same code paths the CLI commands invoke. Nothing writes
// release state except the coordinator itself.
// ---------------------------------------------------------------------------

// assertStatusSnapshot asserts the `server release status` list view
// (TS-012-002 AC-1) reports exactly the given release ID → stage mapping
// via release.ListReleases — the query the status command renders.
func assertStatusSnapshot(t *testing.T, installRoot string, want map[release.ReleaseID]release.Stage) {
	t.Helper()

	releases, err := release.ListReleases(installRoot)
	if err != nil {
		t.Fatalf("ListReleases (status read path): %v", err)
	}
	got := map[release.ReleaseID]release.Stage{}
	for _, rel := range releases {
		got[rel.ID] = rel.Stage
	}

	if len(got) != len(want) {
		t.Errorf("status list reports %d releases %v, want %d %v", len(got), got, len(want), want)
	}
	for id, stage := range want {
		if got[id] != stage {
			t.Errorf("status: release %s stage = %s, want %s", id, got[id], stage)
		}
	}

	// Detail view parity (TS-012-002 AC-2): GetReleaseState must agree with
	// the list view for every release in the snapshot.
	for id, stage := range want {
		gotStage, err := release.GetReleaseState(installRoot, id)
		if err != nil {
			t.Fatalf("GetReleaseState %s (status detail read path): %v", id, err)
		}
		if gotStage != stage {
			t.Errorf("status detail: release %s stage = %s, want %s", id, gotStage, stage)
		}
	}
}

// assertActiveRelease asserts the `server release active` read path
// (release.GetActiveRelease — ST-P4-11) reports the given Release in the
// Active stage, and that runtime state records its identity.
func assertActiveRelease(t *testing.T, installRoot, wantID string) {
	t.Helper()

	active, err := release.GetActiveRelease(installRoot)
	if err != nil {
		t.Fatalf("GetActiveRelease: %v", err)
	}
	if active == nil || active.ID.String() != wantID || active.Stage != release.StageActive {
		t.Errorf("active release = %v, want %s in Active stage", active, wantID)
	}
	assertRuntimeActiveReleaseID(t, installRoot, wantID)
}

// TestReleaseLifecycleE2E_InstallActivateArchiveRollbackStatus executes
// the full release lifecycle end-to-end (ST-012-001 §3) on a fresh server
// root:
//
//  1. server init + server project register + artifact package → A, B
//  2. install (A) + activate (A) → `active` reports A
//  3. activate (B) → `active` reports B; A reaches Archived (F2)
//  4. rollback → `active` reports A again (previous release restored)
//  5. `server release status` shows the expected stage at each step
//
// Every assertion reads through the production paths — the same class of
// defect that escaped the unit fixtures (F1, F2) cannot escape here.
func TestReleaseLifecycleE2E_InstallActivateArchiveRollbackStatus(t *testing.T) {
	serverRoot := t.TempDir()

	// ------------------------------------------------------------------
	// Step 1: server init — the production InitializeServer path.
	// ------------------------------------------------------------------
	initResult, err := InitializeServer(serverRoot)
	if err != nil {
		t.Fatalf("InitializeServer returned unexpected error: %v", err)
	}
	if initResult.AlreadyInitialized {
		t.Error("InitializeServer on a fresh root must not report already-initialized")
	}
	if _, err := os.Stat(initResult.ConfigPath); err != nil {
		t.Fatalf("server config not created at %s: %v", initResult.ConfigPath, err)
	}

	// Step 1b: server project register — the production registry path.
	projectID := "e2e-project"
	installRoot := filepath.Join(serverRoot, "projects", projectID)
	reg := DefaultProjectRegistry()
	reg.Project.ID = projectID
	reg.Project.InstallRoot = installRoot
	reg.Project.DisplayName = "E2E Project"
	registryStore := NewRegistryStore(serverRoot)
	if err := registryStore.Register(reg); err != nil {
		t.Fatalf("register project: %v", err)
	}

	// Step 1c: artifact package — the production packaging engine
	// (TS-P3-01). Two distinct artifacts for the same project: ArtifactID
	// is content-derived (TS-P3-04), so different content yields different
	// IDs (required to bypass the install idempotency check).
	artifactA := createTestArtifactWithFiles(t, projectID, "1.0.0", map[string]string{
		"index.php": "<?php // release A\n",
	})
	artifactB := createTestArtifactWithFiles(t, projectID, "1.1.0", map[string]string{
		"index.php": "<?php // release B\n",
	})

	coordinator := NewServerReleaseCoordinator(serverRoot)

	// ------------------------------------------------------------------
	// Step 2: install (A) + activate (A) → active reports A.
	// ------------------------------------------------------------------
	relA, err := coordinator.Install(projectID, artifactA)
	if err != nil {
		t.Fatalf("Install A returned unexpected error: %v", err)
	}
	if relA.Stage != release.StageReady {
		t.Errorf("release A stage = %s after install, want %s", relA.Stage, release.StageReady)
	}

	// Status observability right after install: A is Ready (BUG-002 layout
	// — the canonical .anvil/state/releases path is the only state dir).
	canonicalA := filepath.Join(project.NewStructure(installRoot).StateDir, "releases", relA.ID.String()+".json")
	if _, err := os.Stat(canonicalA); err != nil {
		t.Fatalf("release A JSON not found at canonical path %s: %v (F1 regression)", canonicalA, err)
	}
	assertStatusSnapshot(t, installRoot, map[release.ReleaseID]release.Stage{
		relA.ID: release.StageReady,
	})

	if err := coordinator.Activate(projectID, relA.ID.String()); err != nil {
		t.Fatalf("Activate A returned unexpected error: %v", err)
	}

	// `active` reports A; status shows A in Active stage.
	assertActiveRelease(t, installRoot, relA.ID.String())
	assertStatusSnapshot(t, installRoot, map[release.ReleaseID]release.Stage{
		relA.ID: release.StageActive,
	})

	// ------------------------------------------------------------------
	// Step 3: activate (B) → active reports B; A reaches Archived.
	// ------------------------------------------------------------------
	relB, err := coordinator.Install(projectID, artifactB)
	if err != nil {
		t.Fatalf("Install B returned unexpected error: %v", err)
	}
	if err := coordinator.Activate(projectID, relB.ID.String()); err != nil {
		t.Fatalf("Activate B returned unexpected error: %v", err)
	}

	// A must be Archived — the ActiveReleaseInvariant wired in the
	// production activation path (F2 regression) — and active reports B.
	archivedA, err := release.LookupByID(installRoot, relA.ID)
	if err != nil {
		t.Fatalf("LookupByID A returned unexpected error: %v", err)
	}
	if archivedA.Stage != release.StageArchived {
		t.Errorf("release A stage = %s after activating B, want %s (F2 regression)", archivedA.Stage, release.StageArchived)
	}
	assertActiveRelease(t, installRoot, relB.ID.String())
	assertStatusSnapshot(t, installRoot, map[release.ReleaseID]release.Stage{
		relA.ID: release.StageArchived,
		relB.ID: release.StageActive,
	})

	// ------------------------------------------------------------------
	// Step 4: rollback → active reports A again (previous release
	// restored); B transitions to RolledBack (BUG-004 recovery path).
	// ------------------------------------------------------------------
	result, err := coordinator.Rollback(projectID)
	if err != nil {
		t.Fatalf("Rollback returned unexpected error: %v", err)
	}
	if result.RolledBackRelease == nil || result.RolledBackRelease.ID != relB.ID {
		t.Errorf("rolled-back release = %v, want B (%s)", result.RolledBackRelease, relB.ID)
	}
	if result.RestoredRelease == nil || result.RestoredRelease.ID != relA.ID {
		t.Errorf("restored release = %v, want A (%s)", result.RestoredRelease, relA.ID)
	}

	assertActiveRelease(t, installRoot, relA.ID.String())
	rolledBackB, err := release.LookupByID(installRoot, relB.ID)
	if err != nil {
		t.Fatalf("LookupByID B after rollback returned unexpected error: %v", err)
	}
	if rolledBackB.Stage != release.StageRolledBack {
		t.Errorf("release B stage after rollback = %s, want %s", rolledBackB.Stage, release.StageRolledBack)
	}
	assertStatusSnapshot(t, installRoot, map[release.ReleaseID]release.Stage{
		relA.ID: release.StageActive,
		relB.ID: release.StageRolledBack,
	})

	// ------------------------------------------------------------------
	// Step 5: final stage observability — every release of the project
	// is visible with its expected stage through the status read path,
	// and the legacy <installRoot>/state/releases layout was never
	// written (F1: single source of truth is the canonical layout).
	// ------------------------------------------------------------------
	legacyDir := filepath.Join(installRoot, "state", "releases")
	if _, err := os.Stat(legacyDir); err == nil {
		t.Errorf("legacy state dir %s must never be written by the lifecycle (F1 regression)", legacyDir)
	} else if !os.IsNotExist(err) {
		t.Errorf("stat legacy state dir %s: %v", legacyDir, err)
	}
}
