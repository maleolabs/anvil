// Package server provides models and utilities for managing Anvil Server
// Runtime configuration — global Runtime metadata persistence, YAML schema
// definition, defaults, and validation, as well as per-project Registry
// metadata.
//
// This file covers the coordinator critical state paths: install, activate,
// active, and rollback (TD-010), plus an end-to-end coordinator lifecycle
// test (install → activate → active → rollback) that runs through the real
// production state paths — not fixture-divergent ones.
//
// The provisioning/ownership paths (ProvisionProjectDir, ApplyFileOwnership,
// applySharedLinks) were removed with the platform-ambition excess demotion
// (ADR-031 §3, TS-015-04-02) and are no longer covered here.
//
// Reference: TD-010, ADR-014, ADR-031, MVP-001 AC 9.5
package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/contracts"
	"maleolabs.com/anvil/internal/project"
	"maleolabs.com/anvil/internal/release"
	"maleolabs.com/anvil/internal/runtime"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// isRoot reports whether the test process runs with root privileges, which
// changes the semantics of permission-denied failure paths (root bypasses
// read-only directory restrictions).
func isRoot() bool {
	return os.Geteuid() == 0
}

// createTestArtifactWithFiles packages an artifact whose deployable content
// contains the given files (relative path → content), so the packaged app
// can include nested paths.
func createTestArtifactWithFiles(t *testing.T, projectID, version string, files map[string]string) string {
	t.Helper()

	sourceDir := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(sourceDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatalf("mkdir artifact source %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatalf("write artifact source file %s: %v", rel, err)
		}
	}

	outputDir := t.TempDir()
	result, err := artifact.Package(artifact.PackageOptions{
		SourceDir: sourceDir,
		OutputDir: outputDir,
		Version:   version,
		Source:    projectID,
		ProjectID: projectID,
	})
	if err != nil {
		t.Fatalf("package artifact: %v", err)
	}
	return result.ArtifactPath
}

// registerE2EProject registers a project whose registry carries the
// declarative metadata the coordinator consumes (identity, install root,
// display name). It complements setupServerEnv, which registers a bare
// project.
func registerE2EProject(t *testing.T, serverRoot string) (projectID, installRoot string) {
	t.Helper()

	projectID = "e2e-project"
	installRoot = filepath.Join(serverRoot, "projects", projectID)

	configStore := NewConfigStore(serverRoot)
	cfg := DefaultServerConfig()
	cfg.Runtime.ID = "test-runtime"
	if err := configStore.Save(cfg); err != nil {
		t.Fatalf("save server config: %v", err)
	}

	reg := DefaultProjectRegistry()
	reg.Project.ID = projectID
	reg.Project.InstallRoot = installRoot
	reg.Project.DisplayName = "E2E Project"

	registryStore := NewRegistryStore(serverRoot)
	if err := registryStore.Register(reg); err != nil {
		t.Fatalf("register project: %v", err)
	}
	return projectID, installRoot
}

// ---------------------------------------------------------------------------
// Demoted provisioning surfaces (ADR-031 §3, TS-015-04-02)
// ---------------------------------------------------------------------------

// TestInstall_LegacyRegistryKeysIgnored verifies back-compat reading of
// registries provisioned before the demotion: legacy owner/group/shared_links
// keys in the registry YAML are ignored on load and must not cause any
// user/group lookup or chown attempt. An unresolvable owner name is used to
// prove the ownership path is truly gone — a stale lookup would fail the
// install.
func TestInstall_LegacyRegistryKeysIgnored(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, installRoot := setupServerEnv(t, serverRoot)

	// Rewrite the registry with the demoted legacy keys and an owner that
	// cannot resolve (the pre-demotion Install step 14 would have failed
	// here).
	registryPath := filepath.Join(serverRoot, "projects", projectID+".yaml")
	registryYAML := "project:\n" +
		"  id: " + projectID + "\n" +
		"  install_root: " + installRoot + "\n" +
		"  display_name: Legacy Project\n" +
		"  owner: anvil-nonexistent-user-xyz\n" +
		"  group: anvil-nonexistent-group-xyz\n" +
		"  shared_links:\n" +
		"    - from: shared/config/app.env\n" +
		"      to: config/app.env\n"
	if err := os.WriteFile(registryPath, []byte(registryYAML), 0644); err != nil {
		t.Fatalf("rewrite registry with legacy keys: %v", err)
	}

	artifactPath := createTestArtifactWithFiles(t, projectID, "1.0.0", map[string]string{
		"config/app.env": "APP_ENV=legacy\n",
	})
	coordinator := NewServerReleaseCoordinator(serverRoot)
	rel, err := coordinator.Install(projectID, artifactPath)
	if err != nil {
		t.Fatalf("Install with legacy registry keys returned unexpected error: %v", err)
	}

	// The release must be readable at the canonical state path — the
	// state-only lifecycle is unaffected by the legacy keys.
	canonical := filepath.Join(project.NewStructure(installRoot).StateDir, "releases", rel.ID.String()+".json")
	if _, err := os.Stat(canonical); err != nil {
		t.Errorf("release JSON not found at canonical path %s: %v", canonical, err)
	}

	// Activation must also succeed: the removed shared-link/ownership steps
	// must not resurface through legacy keys, and the artifact content must
	// be extracted untouched (no remove-and-link replacement).
	if err := coordinator.Activate(projectID, rel.ID.String()); err != nil {
		t.Fatalf("Activate with legacy registry keys returned unexpected error: %v", err)
	}
	runtimeCfg := runtime.DefaultRuntimeConfig()
	runtimeCfg.InstallRoot = installRoot
	releaseDir := runtime.ReleaseDirPath(runtimeCfg.ReleasesDirPath(), rel.ID.String())
	entry, err := os.ReadDir(filepath.Join(releaseDir, "config"))
	if err != nil {
		t.Fatalf("read release config dir: %v", err)
	}
	if len(entry) != 1 || entry[0].Name() != "app.env" {
		t.Errorf("release config dir = %v, want the artifact's app.env untouched (no shared-link replacement)", entry)
	}
}

// ---------------------------------------------------------------------------
// End-to-end coordinator lifecycle test (TD-010 §4, §9)
//
// Runs the real production state paths — the same class of defect that
// escaped the fixture-based unit tests (BUG-002, BUG-003) cannot escape
// here: nothing writes state except the coordinator itself.
// ---------------------------------------------------------------------------

// TestCoordinatorLifecycle_InstallActivateActiveRollback executes the full
// release lifecycle through ServerReleaseCoordinator on a fresh server root:
// install (A) → activate (A) → active reports A → install (B) → activate (B,
// archiving A) → active reports B → rollback → active reports A again.
//
// Every release state assertion goes through the internal/release read paths
// used by observability and rollback, and the extracted artifact content is
// verified untouched — activation no longer rewrites the release directory
// (ownership and shared-link application were demoted per ADR-031 §3).
func TestCoordinatorLifecycle_InstallActivateActiveRollback(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, installRoot := registerE2EProject(t, serverRoot)

	coordinator := NewServerReleaseCoordinator(serverRoot)

	// Two distinct artifacts for the same project — ArtifactID is
	// content-derived (TS-P3-04), so different content yields different IDs
	// (required to bypass the install idempotency check).
	artifactA := createTestArtifactWithFiles(t, projectID, "1.0.0", map[string]string{
		"index.php":            "<?php // release A\n",
		"config/app.env":       "APP_ENV=artifact-a\n",
		"storage/artifact.txt": "artifact-a-storage\n",
	})
	artifactB := createTestArtifactWithFiles(t, projectID, "1.1.0", map[string]string{
		"index.php":            "<?php // release B\n",
		"config/app.env":       "APP_ENV=artifact-b\n",
		"storage/artifact.txt": "artifact-b-storage\n",
	})

	// ------------------------------------------------------------------
	// Install A
	// ------------------------------------------------------------------
	relA, err := coordinator.Install(projectID, artifactA)
	if err != nil {
		t.Fatalf("Install A returned unexpected error: %v", err)
	}
	if relA.Stage != release.StageReady {
		t.Errorf("release A stage = %s after install, want %s", relA.Stage, release.StageReady)
	}
	// The Release must be readable at the canonical state path.
	canonicalA := filepath.Join(project.NewStructure(installRoot).StateDir, "releases", relA.ID.String()+".json")
	if _, err := os.Stat(canonicalA); err != nil {
		t.Errorf("release A JSON not found at canonical path %s: %v", canonicalA, err)
	}

	// ------------------------------------------------------------------
	// Activate A → active reports A
	// ------------------------------------------------------------------
	if err := coordinator.Activate(projectID, relA.ID.String()); err != nil {
		t.Fatalf("Activate A returned unexpected error: %v", err)
	}
	active, err := release.GetActiveRelease(installRoot)
	if err != nil {
		t.Fatalf("GetActiveRelease returned unexpected error: %v", err)
	}
	if active == nil || active.ID != relA.ID || active.Stage != release.StageActive {
		t.Errorf("active release after activating A = %v, want A (%s) in Active stage", active, relA.ID)
	}
	assertRuntimeActiveReleaseID(t, installRoot, relA.ID.String())

	// The extracted artifact must be untouched in the release directory:
	// activation performs no shared-link replacement or ownership change
	// (ADR-031 §3 — the shared-links model is demoted).
	runtimeCfg := runtime.DefaultRuntimeConfig()
	runtimeCfg.InstallRoot = installRoot
	releaseDirA := runtime.ReleaseDirPath(runtimeCfg.ReleasesDirPath(), relA.ID.String())
	appEnv, err := os.ReadFile(filepath.Join(releaseDirA, "config", "app.env"))
	if err != nil {
		t.Fatalf("read extracted app.env: %v", err)
	}
	if string(appEnv) != "APP_ENV=artifact-a\n" {
		t.Errorf("extracted app.env content = %q, want the artifact content %q (no shared-link replacement)", appEnv, "APP_ENV=artifact-a\n")
	}
	if fi, err := os.Lstat(filepath.Join(releaseDirA, "config", "app.env")); err != nil {
		t.Fatalf("lstat extracted app.env: %v", err)
	} else if fi.Mode()&os.ModeSymlink != 0 {
		t.Error("extracted config/app.env must stay a regular file — no shared-link symlink may replace it (ADR-031 §3)")
	}
	storageTxt, err := os.ReadFile(filepath.Join(releaseDirA, "storage", "artifact.txt"))
	if err != nil {
		t.Fatalf("read extracted storage/artifact.txt: %v", err)
	}
	if string(storageTxt) != "artifact-a-storage\n" {
		t.Errorf("extracted storage/artifact.txt content = %q, want the artifact content", storageTxt)
	}

	// ------------------------------------------------------------------
	// Install B + Activate B → A archived, active reports B
	// ------------------------------------------------------------------
	relB, err := coordinator.Install(projectID, artifactB)
	if err != nil {
		t.Fatalf("Install B returned unexpected error: %v", err)
	}
	if err := coordinator.Activate(projectID, relB.ID.String()); err != nil {
		t.Fatalf("Activate B returned unexpected error: %v", err)
	}

	// A must be Archived (ActiveReleaseInvariant wired — BUG-003) and
	// persisted on disk.
	archivedA, err := release.LookupByID(installRoot, relA.ID)
	if err != nil {
		t.Fatalf("LookupByID A returned unexpected error: %v", err)
	}
	if archivedA.Stage != release.StageArchived {
		t.Errorf("release A stage = %s after activating B, want %s", archivedA.Stage, release.StageArchived)
	}
	active, err = release.GetActiveRelease(installRoot)
	if err != nil {
		t.Fatalf("GetActiveRelease returned unexpected error: %v", err)
	}
	if active == nil || active.ID != relB.ID || active.Stage != release.StageActive {
		t.Errorf("active release after activating B = %v, want B (%s) in Active stage", active, relB.ID)
	}
	assertRuntimeActiveReleaseID(t, installRoot, relB.ID.String())

	// ------------------------------------------------------------------
	// Rollback → active reports A again
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

	// The restored release is Active again; the rolled-back one is RolledBack
	// (BUG-004 transitions); runtime state tracks A.
	active, err = release.GetActiveRelease(installRoot)
	if err != nil {
		t.Fatalf("GetActiveRelease after rollback returned unexpected error: %v", err)
	}
	if active == nil || active.ID != relA.ID || active.Stage != release.StageActive {
		t.Errorf("active release after rollback = %v, want A (%s) in Active stage", active, relA.ID)
	}
	rolledBackB, err := release.LookupByID(installRoot, relB.ID)
	if err != nil {
		t.Fatalf("LookupByID B after rollback returned unexpected error: %v", err)
	}
	if rolledBackB.Stage != release.StageRolledBack {
		t.Errorf("release B stage after rollback = %s, want %s", rolledBackB.Stage, release.StageRolledBack)
	}
	assertRuntimeActiveReleaseID(t, installRoot, relA.ID.String())

	// Rollback is a full lifecycle round trip: the symlink state must also
	// point back at A's directory. Verify through the switcher's read path.
	link, err := os.Readlink(runtimeCfg.ActiveSymlinkPath())
	if err != nil {
		t.Fatalf("readlink active symlink: %v", err)
	}
	resolvedLink := filepath.Clean(link)
	if !filepath.IsAbs(link) {
		resolvedLink = filepath.Clean(filepath.Join(filepath.Dir(runtimeCfg.ActiveSymlinkPath()), link))
	}
	if resolvedLink != releaseDirA {
		t.Errorf("active symlink resolves to %s, want %s (release A)", resolvedLink, releaseDirA)
	}
}

// TestCoordinatorLifecycle_RollbackWithoutArchivedTarget_Fails verifies the
// rollback failure path: a project whose only release is Active has no
// Archived target, so rollback must fail with a descriptive error.
func TestCoordinatorLifecycle_RollbackWithoutArchivedTarget_Fails(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, installRoot := setupServerEnv(t, serverRoot)
	artifactPath := createTestArtifact(t, projectID)

	coordinator := NewServerReleaseCoordinator(serverRoot)
	rel, err := coordinator.Install(projectID, artifactPath)
	if err != nil {
		t.Fatalf("Install returned unexpected error: %v", err)
	}
	if err := coordinator.Activate(projectID, rel.ID.String()); err != nil {
		t.Fatalf("Activate returned unexpected error: %v", err)
	}

	_, err = coordinator.Rollback(projectID)
	if err == nil {
		t.Fatal("expected rollback error for missing Archived target, got nil")
	}
	if !contains(err.Error(), "no Archived Release") {
		t.Errorf("expected error to mention 'no Archived Release', got: %v", err)
	}

	// The failed rollback must not corrupt state: A is still Active.
	active, err := release.GetActiveRelease(installRoot)
	if err != nil {
		t.Fatalf("GetActiveRelease returned unexpected error: %v", err)
	}
	if active == nil || active.ID != rel.ID || active.Stage != release.StageActive {
		t.Errorf("active release after failed rollback = %v, want A (%s) Active", active, rel.ID)
	}
}

// TestCoordinatorLifecycle_RollbackWithoutActiveRelease_Fails verifies the
// rollback failure path for a project with no Active Release: rollback must
// fail with a descriptive error instead of mutating anything.
func TestCoordinatorLifecycle_RollbackWithoutActiveRelease_Fails(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, _ := setupServerEnv(t, serverRoot)

	coordinator := NewServerReleaseCoordinator(serverRoot)
	_, err := coordinator.Rollback(projectID)
	if err == nil {
		t.Fatal("expected rollback error for missing Active release, got nil")
	}
	if !contains(err.Error(), "no Active Release") {
		t.Errorf("expected error to mention 'no Active Release', got: %v", err)
	}
}

// TestInstall_SameArtifactRejected verifies the install idempotency failure
// path: installing the same artifact twice must be rejected with a
// descriptive error — the check scans both the canonical and the legacy
// (read-only, BUG-002) release state directories.
func TestInstall_SameArtifactRejected(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, _ := setupServerEnv(t, serverRoot)
	artifactPath := createTestArtifact(t, projectID)

	coordinator := NewServerReleaseCoordinator(serverRoot)
	rel, err := coordinator.Install(projectID, artifactPath)
	if err != nil {
		t.Fatalf("first Install returned unexpected error: %v", err)
	}

	// A duplicate install is rejected by the canonical state scan.
	_, err = coordinator.Install(projectID, artifactPath)
	if err == nil {
		t.Fatal("expected duplicate-install error, got nil")
	}
	if !contains(err.Error(), "already installed") {
		t.Errorf("expected error to mention 'already installed', got: %v", err)
	}

	// Legacy-only duplicate (BUG-002 back-compat): remove the canonical
	// Release JSON and plant only a legacy <installRoot>/state/releases
	// copy. The idempotency scan must still find it and reject the install
	// — the legacy directory is read-only back-compat, never a second
	// source of truth.
	installRoot := filepath.Join(serverRoot, "projects", projectID)
	canonicalDir := filepath.Join(project.NewStructure(installRoot).StateDir, "releases")
	data, err := os.ReadFile(filepath.Join(canonicalDir, rel.ID.String()+".json"))
	if err != nil {
		t.Fatalf("read canonical release JSON: %v", err)
	}
	if err := os.Remove(filepath.Join(canonicalDir, rel.ID.String()+".json")); err != nil {
		t.Fatalf("remove canonical release JSON: %v", err)
	}
	legacyDir := filepath.Join(installRoot, "state", "releases")
	if err := os.MkdirAll(legacyDir, 0755); err != nil {
		t.Fatalf("mkdir legacy releases dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, rel.ID.String()+".json"), data, 0644); err != nil {
		t.Fatalf("write legacy release JSON: %v", err)
	}

	_, err = coordinator.Install(projectID, artifactPath)
	if err == nil {
		t.Fatal("expected duplicate-install error from the legacy scan, got nil")
	}
	if !contains(err.Error(), "already installed") {
		t.Errorf("expected error to mention 'already installed', got: %v", err)
	}
}

// assertRuntimeActiveReleaseID asserts the persisted runtime state records
// the given active release ID — the state the `server release active`
// observability path reads.
func assertRuntimeActiveReleaseID(t *testing.T, installRoot, wantID string) {
	t.Helper()

	statePath := filepath.Join(installRoot, "runtime-state.json")
	stateStore := runtime.NewStateStore(statePath)
	if err := stateStore.Load(); err != nil {
		t.Fatalf("load runtime state: %v", err)
	}
	if got := stateStore.State().ActiveReleaseID; got != wantID {
		t.Errorf("runtime ActiveReleaseID = %q, want %q", got, wantID)
	}
}

// ---------------------------------------------------------------------------
// Install failure paths (TD-010 §9 — coordinator critical paths)
// ---------------------------------------------------------------------------

// TestInstall_UnverifiedArtifactRejected verifies that an artifact that
// fails verification is rejected before any state is written.
func TestInstall_UnverifiedArtifactRejected(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, _ := setupServerEnv(t, serverRoot)

	// A non-archive file fails artifact verification (gzip tolerates
	// trailing garbage, so appending bytes would not be enough).
	corruptPath := filepath.Join(t.TempDir(), "corrupt.tar.gz")
	if err := os.WriteFile(corruptPath, []byte("not a gzip archive at all"), 0644); err != nil {
		t.Fatalf("write corrupt artifact: %v", err)
	}

	coordinator := NewServerReleaseCoordinator(serverRoot)
	_, err := coordinator.Install(projectID, corruptPath)
	if err == nil {
		t.Fatal("expected error for unverified artifact, got nil")
	}
	if !contains(err.Error(), "must be verified first") {
		t.Errorf("expected error to mention 'must be verified first', got: %v", err)
	}
}

// TestInstall_AccessArtifactError verifies the failure path for an artifact
// path that cannot even be statted (a non-ENOENT stat error, e.g. a path
// component that is a regular file).
func TestInstall_AccessArtifactError(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, _ := setupServerEnv(t, serverRoot)

	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}

	coordinator := NewServerReleaseCoordinator(serverRoot)
	_, err := coordinator.Install(projectID, filepath.Join(blocker, "artifact.tar.gz"))
	if err == nil {
		t.Fatal("expected access error for artifact under a file, got nil")
	}
	if !contains(err.Error(), "access artifact") {
		t.Errorf("expected error to mention 'access artifact', got: %v", err)
	}
}

// TestInstall_CreateDirectoryFailure verifies the failure path when the
// runtime directory structure cannot be created (a path component exists as
// a regular file).
func TestInstall_CreateDirectoryFailure(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, _ := setupServerEnv(t, serverRoot)
	installRoot := filepath.Join(serverRoot, "projects", projectID)

	// artifacts/ must be created by Install, but it exists as a file.
	if err := os.MkdirAll(installRoot, 0755); err != nil {
		t.Fatalf("mkdir install root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installRoot, "artifacts"), []byte("x"), 0644); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}

	artifactPath := createTestArtifact(t, projectID)
	coordinator := NewServerReleaseCoordinator(serverRoot)
	_, err := coordinator.Install(projectID, artifactPath)
	if err == nil {
		t.Fatal("expected directory creation error, got nil")
	}
	if !contains(err.Error(), "create directory") {
		t.Errorf("expected error to mention 'create directory', got: %v", err)
	}
}

// TestInstall_CopyArtifactFailure verifies the failure path when the
// artifact cannot be copied into the artifact store (non-root process
// against a read-only store directory).
func TestInstall_CopyArtifactFailure(t *testing.T) {
	if isRoot() {
		t.Skip("permission-denied copy requires a non-root process")
	}

	serverRoot := t.TempDir()
	projectID, _ := setupServerEnv(t, serverRoot)
	installRoot := filepath.Join(serverRoot, "projects", projectID)

	// Pre-create the artifact store as read-only so Install's MkdirAll
	// succeeds (dir exists) but the copy fails with EACCES. The install
	// root is created with normal permissions first — MkdirAll would
	// otherwise propagate 0555 to every parent it creates.
	if err := os.MkdirAll(installRoot, 0755); err != nil {
		t.Fatalf("mkdir install root: %v", err)
	}
	artifactStoreDir := filepath.Join(installRoot, "artifacts")
	if err := os.Mkdir(artifactStoreDir, 0555); err != nil {
		t.Fatalf("mkdir read-only artifact store: %v", err)
	}
	defer func() { _ = os.Chmod(artifactStoreDir, 0755) }()

	artifactPath := createTestArtifact(t, projectID)
	coordinator := NewServerReleaseCoordinator(serverRoot)
	_, err := coordinator.Install(projectID, artifactPath)
	if err == nil {
		t.Fatal("expected copy error, got nil")
	}
	if !contains(err.Error(), "copy artifact to store") {
		t.Errorf("expected error to mention 'copy artifact to store', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Stored-copy verification (TD-009 — artifact verification TOCTOU)
// ---------------------------------------------------------------------------

// TestVerifyStoredArtifact_ValidCopy verifies the TD-009 helper on the happy
// path: a byte-perfect copy of a verified artifact passes the stored-copy
// verification.
func TestVerifyStoredArtifact_ValidCopy(t *testing.T) {
	projectID := "test-project"
	artifactPath := createTestArtifact(t, projectID)
	storePath := filepath.Join(t.TempDir(), "store.tar.gz")
	if err := copyFile(artifactPath, storePath); err != nil {
		t.Fatalf("copy artifact to store: %v", err)
	}

	manifest, err := artifact.ReadManifest(artifactPath)
	if err != nil {
		t.Fatalf("read source manifest: %v", err)
	}

	if err := verifyStoredArtifact(storePath, manifest); err != nil {
		t.Fatalf("verifyStoredArtifact returned unexpected error: %v", err)
	}
}

// TestVerifyStoredArtifact_CorruptCopyFails verifies that a corrupt stored
// copy — e.g. an interrupted write that truncates the archive — fails the
// stored-copy verification, so it cannot be promoted to a release.
//
// DoD (TD-009): "A corrupt copy cannot produce a release."
func TestVerifyStoredArtifact_CorruptCopyFails(t *testing.T) {
	projectID := "test-project"
	artifactPath := createTestArtifact(t, projectID)
	storePath := filepath.Join(t.TempDir(), "store.tar.gz")
	if err := copyFile(artifactPath, storePath); err != nil {
		t.Fatalf("copy artifact to store: %v", err)
	}

	// Truncate the stored copy like an interrupted write. The manifest is
	// the last tar entry, so the truncated gzip stream cannot yield it and
	// verification must fail.
	info, err := os.Stat(storePath)
	if err != nil {
		t.Fatalf("stat stored copy: %v", err)
	}
	if err := os.Truncate(storePath, info.Size()/2); err != nil {
		t.Fatalf("truncate stored copy: %v", err)
	}

	manifest, err := artifact.ReadManifest(artifactPath)
	if err != nil {
		t.Fatalf("read source manifest: %v", err)
	}

	err = verifyStoredArtifact(storePath, manifest)
	if err == nil {
		t.Fatal("expected error for corrupt stored copy, got nil")
	}
	if !contains(err.Error(), "stored artifact failed verification") {
		t.Errorf("expected error to mention 'stored artifact failed verification', got: %v", err)
	}
}

// TestVerifyStoredArtifact_ManifestMismatchFails verifies that a stored copy
// containing a different — but equally valid — artifact fails the stored-copy
// verification: the source was swapped for another valid artifact between
// verification and copy, and the release record must never be built from a
// manifest that does not describe the stored bytes.
//
// DoD (TD-009): "A source change between verification and copy is detected."
func TestVerifyStoredArtifact_ManifestMismatchFails(t *testing.T) {
	projectID := "test-project"

	// The artifact that passed verification.
	verifiedArtifact := createTestArtifact(t, projectID)
	manifest, err := artifact.ReadManifest(verifiedArtifact)
	if err != nil {
		t.Fatalf("read verified source manifest: %v", err)
	}

	// The swapped source: a different, fully valid artifact (different
	// content yields a different content-derived identity, TS-P3-04).
	swappedArtifact := createTestArtifactVariant(t, projectID, "1.0.0", "<?php // swapped after verification\n")
	storePath := filepath.Join(t.TempDir(), "store.tar.gz")
	if err := copyFile(swappedArtifact, storePath); err != nil {
		t.Fatalf("copy swapped artifact to store: %v", err)
	}

	err = verifyStoredArtifact(storePath, manifest)
	if err == nil {
		t.Fatal("expected manifest mismatch error for swapped artifact, got nil")
	}
	if !contains(err.Error(), "does not match") {
		t.Errorf("expected error to mention 'does not match', got: %v", err)
	}
}

// TestInstall_StoredArtifactVerified verifies the TD-009 invariant from the
// positive side: after a successful install, the stored artifact passes full
// verification and its manifest matches the release record — the release
// references a proven-intact payload.
//
// DoD (TD-009): "The stored copy is verified (or checksum-compared) before
// release creation."
func TestInstall_StoredArtifactVerified(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, _ := setupServerEnv(t, serverRoot)
	artifactPath := createTestArtifact(t, projectID)

	coordinator := NewServerReleaseCoordinator(serverRoot)
	rel, err := coordinator.Install(projectID, artifactPath)
	if err != nil {
		t.Fatalf("Install returned unexpected error: %v", err)
	}

	if err := artifact.RequireVerified(rel.ArtifactPath); err != nil {
		t.Errorf("stored artifact failed verification after install: %v", err)
	}

	storedManifest, err := artifact.ReadManifest(rel.ArtifactPath)
	if err != nil {
		t.Fatalf("read stored manifest: %v", err)
	}
	if storedManifest.ArtifactID != rel.ArtifactID {
		t.Errorf("stored ArtifactID = %q, want %q", storedManifest.ArtifactID, rel.ArtifactID)
	}
}

// TestInstall_SourceChangeBetweenVerifyAndCopy_Fails is the TD-009 TOCTOU
// regression test: it simulates the source artifact changing between the
// verification step and the copy step, so the bytes stored in the artifact
// store differ from the bytes that were verified. Install must fail the
// stored-copy verification, must not create a release, and must not leave
// the unverified copy in the artifact store.
//
// The injection mirrors the existing adapterRunner/adapterExecutable test
// seam convention: the coordinator's storedArtifactVerifier is replaced with
// a hook that corrupts the stored copy before delegating to the default
// implementation — deterministically producing the exact effect a TOCTOU
// source change has on the stored bytes.
//
// DoD (TD-009): "A source change between verification and copy is detected."
// / "A corrupt copy cannot produce a release."
func TestInstall_SourceChangeBetweenVerifyAndCopy_Fails(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, installRoot := setupServerEnv(t, serverRoot)
	artifactPath := createTestArtifact(t, projectID)

	coordinator := NewServerReleaseCoordinator(serverRoot)
	defaultVerify := verifyStoredArtifact
	coordinator.storedArtifactVerifier = func(storePath string, expected *artifact.Manifest) error {
		// The verified source bytes were copied, but the stored bytes are
		// replaced — exactly what a source swap between verification and
		// copy produces.
		if err := os.WriteFile(storePath, []byte("replaced between verify and copy"), 0644); err != nil {
			t.Fatalf("replace stored copy: %v", err)
		}
		return defaultVerify(storePath, expected)
	}

	_, err := coordinator.Install(projectID, artifactPath)
	if err == nil {
		t.Fatal("expected install failure for TOCTOU source change, got nil")
	}
	if !contains(err.Error(), "verify stored artifact") {
		t.Errorf("expected error to mention 'verify stored artifact', got: %v", err)
	}

	// No release may be created from the unverified payload.
	releasesStateDir := filepath.Join(project.NewStructure(installRoot).StateDir, "releases")
	entries, err := os.ReadDir(releasesStateDir)
	if err != nil {
		t.Fatalf("read releases state dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("release JSON created despite stored-copy verification failure: %d entries", len(entries))
	}

	// The unverified copy must not remain in the artifact store (ADR-017
	// store contract: verified artifacts only).
	storeDir := filepath.Join(installRoot, "artifacts")
	entries, err = os.ReadDir(storeDir)
	if err != nil {
		t.Fatalf("read artifact store dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("unverified artifact left in store: %d entries", len(entries))
	}
}

// TestInstall_SaveReleaseFailure verifies the failure path when the Release
// JSON cannot be persisted (non-root process against a read-only release
// state directory).
func TestInstall_SaveReleaseFailure(t *testing.T) {
	if isRoot() {
		t.Skip("permission-denied save requires a non-root process")
	}

	serverRoot := t.TempDir()
	projectID, _ := setupServerEnv(t, serverRoot)
	installRoot := filepath.Join(serverRoot, "projects", projectID)

	// Pre-create the canonical state dir as read-only: Install's MkdirAll
	// succeeds (dir exists), but Release.Save cannot create its temp file.
	// The install root is created with normal permissions first — MkdirAll
	// would otherwise propagate 0555 to every parent it creates.
	if err := os.MkdirAll(installRoot, 0755); err != nil {
		t.Fatalf("mkdir install root: %v", err)
	}
	releasesStateDir := filepath.Join(project.NewStructure(installRoot).StateDir, "releases")
	if err := os.MkdirAll(filepath.Dir(releasesStateDir), 0755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	if err := os.Mkdir(releasesStateDir, 0555); err != nil {
		t.Fatalf("mkdir read-only release state dir: %v", err)
	}
	defer func() { _ = os.Chmod(releasesStateDir, 0755) }()

	artifactPath := createTestArtifact(t, projectID)
	coordinator := NewServerReleaseCoordinator(serverRoot)
	_, err := coordinator.Install(projectID, artifactPath)
	if err == nil {
		t.Fatal("expected save error, got nil")
	}
	if !contains(err.Error(), "save release") {
		t.Errorf("expected error to mention 'save release', got: %v", err)
	}
}

// TestInstall_SaveRuntimeStateFailure verifies the failure path when the
// runtime state cannot be persisted (runtime-state.json exists as a
// directory, so the atomic rename fails).
func TestInstall_SaveRuntimeStateFailure(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, _ := setupServerEnv(t, serverRoot)
	installRoot := filepath.Join(serverRoot, "projects", projectID)

	if err := os.MkdirAll(installRoot, 0755); err != nil {
		t.Fatalf("mkdir install root: %v", err)
	}
	if err := os.Mkdir(filepath.Join(installRoot, "runtime-state.json"), 0755); err != nil {
		t.Fatalf("mkdir runtime-state.json as directory: %v", err)
	}

	artifactPath := createTestArtifact(t, projectID)
	coordinator := NewServerReleaseCoordinator(serverRoot)
	_, err := coordinator.Install(projectID, artifactPath)
	if err == nil {
		t.Fatal("expected runtime state error, got nil")
	}
	// The state file exists but is unreadable (it is a directory). Install
	// must fail loudly instead of overwriting state it cannot read — a
	// corrupt/unreadable state file is preserved for recovery (ADR-031:
	// state is authoritative and survives).
	if !contains(err.Error(), "runtime state") {
		t.Errorf("expected error to mention 'runtime state', got: %v", err)
	}
}

// TestInstall_SkipsCorruptReleaseJSON verifies that the install idempotency
// scan tolerates unreadable entries in the releases state directory
// (corrupt JSON and non-file entries are skipped, not fatal).
func TestInstall_SkipsCorruptReleaseJSON(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, _ := setupServerEnv(t, serverRoot)
	installRoot := filepath.Join(serverRoot, "projects", projectID)

	// Plant corrupt entries in the canonical state dir before Install.
	releasesStateDir := filepath.Join(project.NewStructure(installRoot).StateDir, "releases")
	if err := os.MkdirAll(releasesStateDir, 0755); err != nil {
		t.Fatalf("mkdir release state dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(releasesStateDir, "corrupt.json"), []byte("{not json"), 0644); err != nil {
		t.Fatalf("write corrupt release JSON: %v", err)
	}
	if err := os.Mkdir(filepath.Join(releasesStateDir, "dir.json"), 0755); err != nil {
		t.Fatalf("mkdir directory entry: %v", err)
	}

	artifactPath := createTestArtifact(t, projectID)
	coordinator := NewServerReleaseCoordinator(serverRoot)
	if _, err := coordinator.Install(projectID, artifactPath); err != nil {
		t.Fatalf("Install returned unexpected error with corrupt state entries: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Activate failure paths (TD-010 §9 — coordinator critical paths)
// ---------------------------------------------------------------------------

// TestActivate_ExtractFailure verifies the failure path when the stored
// artifact cannot be extracted into the release directory (corrupt store
// content).
func TestActivate_ExtractFailure(t *testing.T) {
	serverRoot := t.TempDir()
	releaseID := "rel-extract-fail"

	projectID, _ := setupActivateEnvironment(t, serverRoot, releaseID)
	installRoot := filepath.Join(serverRoot, "projects", projectID)

	// Corrupt the stored artifact so extraction fails during Activate.
	storeArtifactPath := filepath.Join(installRoot, "artifacts", releaseID+".tar.gz")
	if err := os.WriteFile(storeArtifactPath, []byte("not a real archive"), 0644); err != nil {
		t.Fatalf("corrupt stored artifact: %v", err)
	}

	coordinator := NewServerReleaseCoordinator(serverRoot)
	err := coordinator.Activate(projectID, releaseID)
	if err == nil {
		t.Fatal("expected extraction error, got nil")
	}
	if !contains(err.Error(), "extract artifact for activation") {
		t.Errorf("expected error to mention 'extract artifact for activation', got: %v", err)
	}
}

// TestActivate_SaveReleaseFailure verifies the failure path when the
// post-activation Release save fails (non-root process against a read-only
// release state directory).
func TestActivate_SaveReleaseFailure(t *testing.T) {
	if isRoot() {
		t.Skip("permission-denied save requires a non-root process")
	}

	serverRoot := t.TempDir()
	releaseID := "rel-save-fail"

	projectID, _ := setupActivateEnvironment(t, serverRoot, releaseID)
	installRoot := filepath.Join(serverRoot, "projects", projectID)

	// Make the canonical state dir read-only after the fixture wrote the
	// Ready Release JSON: Load still works, the post-activation Save fails.
	releasesStateDir := filepath.Join(project.NewStructure(installRoot).StateDir, "releases")
	if err := os.Chmod(releasesStateDir, 0555); err != nil {
		t.Fatalf("chmod release state dir read-only: %v", err)
	}
	defer func() { _ = os.Chmod(releasesStateDir, 0755) }()

	coordinator := NewServerReleaseCoordinator(serverRoot)
	err := coordinator.Activate(projectID, releaseID)
	if err == nil {
		t.Fatal("expected release save error, got nil")
	}
	if !contains(err.Error(), "save release state") {
		t.Errorf("expected error to mention 'save release state', got: %v", err)
	}
}

// TestActivate_SaveRuntimeStateFailure verifies the failure path when the
// runtime state cannot be persisted after a successful activation
// (runtime-state.json exists as a directory, so the atomic rename fails).
func TestActivate_SaveRuntimeStateFailure(t *testing.T) {
	serverRoot := t.TempDir()
	releaseID := "rel-state-fail"

	projectID, _ := setupActivateEnvironment(t, serverRoot, releaseID)
	installRoot := filepath.Join(serverRoot, "projects", projectID)

	if err := os.Mkdir(filepath.Join(installRoot, "runtime-state.json"), 0755); err != nil {
		t.Fatalf("mkdir runtime-state.json as directory: %v", err)
	}

	coordinator := NewServerReleaseCoordinator(serverRoot)
	err := coordinator.Activate(projectID, releaseID)
	if err == nil {
		t.Fatal("expected runtime state error, got nil")
	}
	// The state file exists but is unreadable (it is a directory). Activate
	// must fail loudly instead of overwriting state it cannot read — a
	// corrupt/unreadable state file is preserved for recovery (ADR-031:
	// state is authoritative and survives).
	if !contains(err.Error(), "runtime state") {
		t.Errorf("expected error to mention 'runtime state', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Rollback failure paths (TD-010 §9 — coordinator critical paths)
// ---------------------------------------------------------------------------

// TestRollback_UnregisteredProject verifies the failure path for rollback
// against an unknown project.
func TestRollback_UnregisteredProject(t *testing.T) {
	serverRoot := t.TempDir()

	coordinator := NewServerReleaseCoordinator(serverRoot)
	_, err := coordinator.Rollback("nonexistent-project")
	if err == nil {
		t.Fatal("expected error for unregistered project, got nil")
	}
	if !contains(err.Error(), "project registry not found") {
		t.Errorf("expected error to mention 'project registry not found', got: %v", err)
	}
}

// TestRollback_SaveRuntimeStateFailure verifies the failure path when the
// runtime state cannot be persisted after a successful rollback engine run
// (runtime-state.json exists as a directory, so the atomic rename fails).
func TestRollback_SaveRuntimeStateFailure(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, installRoot := setupServerEnv(t, serverRoot)

	coordinator := NewServerReleaseCoordinator(serverRoot)

	// Full two-release setup so the rollback engine has a target.
	artifactA := createTestArtifactVariant(t, projectID, "1.0.0", "<?php // release A\n")
	artifactB := createTestArtifactVariant(t, projectID, "1.1.0", "<?php // release B\n")
	relA, err := coordinator.Install(projectID, artifactA)
	if err != nil {
		t.Fatalf("Install A returned unexpected error: %v", err)
	}
	relB, err := coordinator.Install(projectID, artifactB)
	if err != nil {
		t.Fatalf("Install B returned unexpected error: %v", err)
	}
	if err := coordinator.Activate(projectID, relA.ID.String()); err != nil {
		t.Fatalf("Activate A returned unexpected error: %v", err)
	}
	if err := coordinator.Activate(projectID, relB.ID.String()); err != nil {
		t.Fatalf("Activate B returned unexpected error: %v", err)
	}

	// Block the final runtime-state persistence: Install/Activate already
	// created runtime-state.json, so replace it with a directory before
	// rollback.
	runtimeStatePath := filepath.Join(installRoot, "runtime-state.json")
	if err := os.Remove(runtimeStatePath); err != nil {
		t.Fatalf("remove runtime-state.json: %v", err)
	}
	if err := os.Mkdir(runtimeStatePath, 0755); err != nil {
		t.Fatalf("mkdir runtime-state.json as directory: %v", err)
	}

	_, err = coordinator.Rollback(projectID)
	if err == nil {
		t.Fatal("expected runtime state error, got nil")
	}
	// The state file exists but is unreadable (it is a directory). Rollback
	// must fail loudly instead of overwriting state it cannot read — a
	// corrupt/unreadable state file is preserved for recovery (ADR-031:
	// state is authoritative and survives).
	if !contains(err.Error(), "runtime state") {
		t.Errorf("expected error to mention 'runtime state', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Adapter executable resolution (TS-P7-11 wiring)
// ---------------------------------------------------------------------------

// TestInstall_MissingAdapterExecutable_Fails verifies the failure path when
// a project selects a framework adapter whose executable is not on PATH:
// the coordinator must fail with a descriptive error instead of silently
// skipping verification.
func TestInstall_MissingAdapterExecutable_Fails(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, installRoot := setupServerEnv(t, serverRoot)

	registryPath := filepath.Join(serverRoot, "projects", projectID+".yaml")
	registryYAML := "project:\n" +
		"  id: " + projectID + "\n" +
		"  install_root: " + installRoot + "\n" +
		"  display_name: Test Project\n" +
		"  adapter: anvil-totally-missing-adapter\n"
	if err := os.WriteFile(registryPath, []byte(registryYAML), 0644); err != nil {
		t.Fatalf("rewrite registry with missing adapter: %v", err)
	}

	artifactPath := createTestArtifact(t, projectID)
	coordinator := NewServerReleaseCoordinator(serverRoot)
	_, err := coordinator.Install(projectID, artifactPath)
	if err == nil {
		t.Fatal("expected adapter executable error, got nil")
	}
	if !contains(err.Error(), "not found on PATH") {
		t.Errorf("expected error to mention 'not found on PATH', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Build wiring helpers (TS-007-040, TS-P7-14)
// ---------------------------------------------------------------------------

// TestFirstBuildFailure verifies the failure-detail selection of the build
// report: structured phase error first, output excerpt as fallback, and a
// descriptive message when no failing phase is reported.
func TestFirstBuildFailure(t *testing.T) {
	cases := []struct {
		name   string
		phases []contracts.BuildPhaseResult
		want   string
	}{
		{
			name:   "structured error wins",
			phases: []contracts.BuildPhaseResult{{Phase: "composer", Success: false, Error: "exit status 1"}},
			want:   `phase "composer": exit status 1`,
		},
		{
			name:   "output excerpt fallback",
			phases: []contracts.BuildPhaseResult{{Phase: "composer", Success: false, Output: "composer install failed"}},
			want:   `phase "composer": composer install failed`,
		},
		{
			name:   "first failing phase wins",
			phases: []contracts.BuildPhaseResult{{Phase: "a", Success: true}, {Phase: "b", Success: false, Error: "boom"}, {Phase: "c", Success: false, Error: "ignored"}},
			want:   `phase "b": boom`,
		},
		{
			name:   "all succeeded",
			phases: []contracts.BuildPhaseResult{{Phase: "a", Success: true}, {Phase: "b", Success: true}},
			want:   "no failing phase reported",
		},
		{
			name:   "no phases",
			phases: nil,
			want:   "no failing phase reported",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstBuildFailure(tc.phases); got != tc.want {
				t.Errorf("firstBuildFailure() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestBuildOutputExcerpt verifies the bounded, trimmed output excerpt used
// in build failure details.
func TestBuildOutputExcerpt(t *testing.T) {
	longOutput := strings.Repeat("x", 2000)

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"short output passthrough", "composer install failed", "composer install failed"},
		{"whitespace trimmed", "  composer failed  ", "composer failed"},
		{"long output truncated", longOutput, strings.Repeat("x", 1000) + "..."},
		{"empty output", "", "no output or error reported for the phase"},
		{"whitespace only", "   \n\t", "no output or error reported for the phase"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildOutputExcerpt(tc.in); got != tc.want {
				t.Errorf("buildOutputExcerpt() = %q, want %q", got, tc.want)
			}
		})
	}
}
