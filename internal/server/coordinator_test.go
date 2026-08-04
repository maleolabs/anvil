// Package server provides models and utilities for managing Anvil Server
// Runtime configuration — global Runtime metadata persistence, YAML schema
// definition, defaults, and validation, as well as per-project Registry
// metadata.
//
// Reference: TS-P4-11, ST-P4-13, ST-P4-14
package server

import (
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/project"
	"maleolabs.com/anvil/internal/release"
	"maleolabs.com/anvil/internal/runtime"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// setupServerEnv creates a minimal server environment with an initialized
// config store and a registered project. It returns the server root path,
// the project ID, and the install root.
func setupServerEnv(t *testing.T, serverRoot string) (projectID, installRoot string) {
	t.Helper()

	projectID = "test-project"
	installRoot = filepath.Join(serverRoot, "projects", projectID)

	// Initialize server config.
	configStore := NewConfigStore(serverRoot)
	cfg := DefaultServerConfig()
	cfg.Runtime.ID = "test-runtime"
	if err := configStore.Save(cfg); err != nil {
		t.Fatalf("save server config: %v", err)
	}

	// Register the project.
	reg := DefaultProjectRegistry()
	reg.Project.ID = projectID
	reg.Project.InstallRoot = installRoot
	reg.Project.DisplayName = "Test Project"

	registryStore := NewRegistryStore(serverRoot)
	if err := registryStore.Register(reg); err != nil {
		t.Fatalf("register project: %v", err)
	}

	return projectID, installRoot
}

// createTestArtifact creates a minimal valid artifact archive using
// artifact.Package and returns the path to the artifact. The artifact
// is created with the given projectID so that manifest validation passes.
func createTestArtifact(t *testing.T, projectID string) string {
	t.Helper()

	return createTestArtifactVariant(t, projectID, "1.0.0", "<?php\n")
}

// createTestArtifactVariant creates a minimal valid artifact archive with
// the given version and source file content, so callers can produce
// multiple distinct artifacts for the same project. Artifact identity is
// content-derived (TS-P3-04), so different content yields different
// ArtifactIDs — required to bypass the install idempotency check when
// installing two releases for one project.
func createTestArtifactVariant(t *testing.T, projectID, version, content string) string {
	t.Helper()

	// Create a temporary source directory with a single file.
	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "index.php"), []byte(content), 0644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	// Create the output directory for the artifact.
	outputDir := t.TempDir()

	// Package the artifact with the test project ID.
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

// setupActivateEnvironment creates a complete environment for activation
// tests: server config, registered project, runtime directories, a release
// directory, an artifact in the artifact store, and a Release JSON in the
// Ready stage with ArtifactPath pointing to the stored artifact.
//
// It returns the project ID, release ID, and the path to the Release JSON.
func setupActivateEnvironment(t *testing.T, serverRoot, releaseID string) (projectID, releasePath string) {
	t.Helper()

	projectID, installRoot := setupServerEnv(t, serverRoot)
	releasePath = setupActivateRelease(t, projectID, installRoot, releaseID)
	return projectID, releasePath
}

// setupActivateRelease creates the per-release fixture for an
// already-registered project: runtime directories, the release directory,
// an artifact in the artifact store, and a Ready Release JSON. Unlike
// setupActivateEnvironment it does NOT register the project — Register
// rejects duplicates — so multiple releases can be built against one
// project (e.g., the two-release invariant tests).
//
// It returns the path to the Release JSON.
func setupActivateRelease(t *testing.T, projectID, installRoot, releaseID string) string {
	t.Helper()

	// Build RuntimeConfig for path resolution.
	runtimeCfg := runtime.DefaultRuntimeConfig()
	runtimeCfg.InstallRoot = installRoot

	// Create all runtime directories (shared, releases, artifacts, state).
	for _, d := range runtimeCfg.AllDirs() {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	artifactStoreDir := filepath.Join(installRoot, "artifacts")
	// The release state directory is the unified layout the coordinator
	// writes and internal/release reads: <installRoot>/.anvil/state/releases
	// (BUG-002).
	releasesStateDir := filepath.Join(project.NewStructure(installRoot).StateDir, "releases")
	for _, d := range []string{artifactStoreDir, releasesStateDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	// Create the release directory (empty, as Install would do).
	releasesDir := runtimeCfg.ReleasesDirPath()
	if _, err := runtime.CreateReleaseDir(releasesDir, releaseID); err != nil {
		t.Fatalf("create release dir: %v", err)
	}

	// Create a test artifact and copy it to the artifact store.
	sourceArtifact := createTestArtifact(t, projectID)
	storeArtifactPath := filepath.Join(artifactStoreDir, releaseID+".tar.gz")
	if err := copyFile(sourceArtifact, storeArtifactPath); err != nil {
		t.Fatalf("copy artifact to store: %v", err)
	}

	// Create the Release JSON with ArtifactPath set.
	releaseFilePath := filepath.Join(releasesStateDir, releaseID+".json")
	rel := &release.Release{
		ID:           release.ReleaseID(releaseID),
		ArtifactPath: storeArtifactPath,
		Stage:        release.StageReady,
		Transitions:  []release.TransitionRecord{},
	}
	if err := rel.Save(releaseFilePath); err != nil {
		t.Fatalf("save release JSON: %v", err)
	}

	return releaseFilePath
}

// ---------------------------------------------------------------------------
// Install tests
// ---------------------------------------------------------------------------

// TestInstall_CreatesRelease verifies that the coordinator:
//   - Creates a Release with a unique identity
//   - Persists the Release to the project state directory
//   - Creates the Release in Ready stage
//   - Records the artifact metadata (ArtifactID, Version)
//
// AC 1: Release created when artifact is verified.
// AC 2: Created Release has unique identity.
// AC 3: Release references the stored artifact.
// AC 4: Release lifecycle stage is Ready after installation.
func TestInstall_CreatesRelease(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, _ := setupServerEnv(t, serverRoot)
	artifactPath := createTestArtifact(t, projectID)

	coordinator := NewServerReleaseCoordinator(serverRoot)
	rel, err := coordinator.Install(projectID, artifactPath)
	if err != nil {
		t.Fatalf("Install returned unexpected error: %v", err)
	}

	// Verify the Release has a unique identity.
	if rel.ID == "" {
		t.Error("Release ID must not be empty")
	}
	if len(rel.ID.String()) != 32 {
		t.Errorf("Release ID length = %d, want 32", len(rel.ID.String()))
	}

	// Verify Release metadata.
	if rel.ArtifactID == "" {
		t.Error("ArtifactID must not be empty")
	}
	if rel.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", rel.Version, "1.0.0")
	}
	if rel.Source != projectID {
		t.Errorf("Source = %q, want %q", rel.Source, projectID)
	}
	if rel.Stage != release.StageReady {
		t.Errorf("Stage = %s, want %s", rel.Stage, release.StageReady)
	}
	if rel.RuntimePath == "" {
		t.Error("RuntimePath must not be empty")
	}

	// Verify the Release JSON was persisted to disk (unified layout:
	// .anvil/state/releases/ — the directory internal/release reads; BUG-002).
	releasesDir := filepath.Join(project.NewStructure(rel.RuntimePath).StateDir, "releases")
	releasePath := filepath.Join(releasesDir, rel.ID.String()+".json")
	if _, err := os.Stat(releasePath); err != nil {
		t.Errorf("Release JSON not found at %s: %v", releasePath, err)
	}

	// Verify only ONE release-state directory is used: the legacy
	// <installRoot>/state/releases layout must not exist on a fresh root
	// (BUG-002 validation step 5).
	legacyDir := filepath.Join(rel.RuntimePath, "state", "releases")
	if _, err := os.Stat(legacyDir); err == nil {
		t.Errorf("legacy state dir %s must not exist on a fresh root", legacyDir)
	} else if !os.IsNotExist(err) {
		t.Errorf("stat legacy state dir %s: %v", legacyDir, err)
	}

	// Verify the artifact was stored in the artifact store.
	artifactStorePath := rel.ArtifactPath
	if _, err := os.Stat(artifactStorePath); err != nil {
		t.Errorf("Stored artifact not found at %s: %v", artifactStorePath, err)
	}

	// Verify the artifact store path is within the install root.
	expectedStoreDir := filepath.Join(rel.RuntimePath, "artifacts")
	if filepath.Dir(artifactStorePath) != expectedStoreDir {
		t.Errorf("Artifact store path = %s, expected dir %s", artifactStorePath, expectedStoreDir)
	}
}

// TestInstall_ArtifactStoredInStore verifies that the coordinator copies the
// artifact into the Runtime Artifact Store and that the copy is accessible
// and has the expected content.
//
// AC: Artifact is stored at {installRoot}/.anvil/artifacts/{releaseID}.tar.gz.
func TestInstall_ArtifactStoredInStore(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, installRoot := setupServerEnv(t, serverRoot)
	artifactPath := createTestArtifact(t, projectID)

	coordinator := NewServerReleaseCoordinator(serverRoot)
	rel, err := coordinator.Install(projectID, artifactPath)
	if err != nil {
		t.Fatalf("Install returned unexpected error: %v", err)
	}

	// Verify the artifact was stored at the correct path (runtime layout: artifacts/).
	expectedPath := filepath.Join(installRoot, "artifacts", rel.ID.String()+".tar.gz")
	if rel.ArtifactPath != expectedPath {
		t.Errorf("ArtifactPath = %q, want %q", rel.ArtifactPath, expectedPath)
	}

	// Verify the stored artifact exists and has content.
	info, err := os.Stat(rel.ArtifactPath)
	if err != nil {
		t.Fatalf("stat stored artifact: %v", err)
	}
	if info.Size() == 0 {
		t.Error("stored artifact has zero size")
	}

	// Verify the original artifact still exists (not moved).
	if _, err := os.Stat(artifactPath); err != nil {
		t.Errorf("original artifact should still exist: %v", err)
	}
}

// TestInstall_ProjectIDMismatch verifies that installing an artifact whose
// manifest.project_id does not match the registered project ID returns an
// error.
//
// AC: An error is returned when the artifact project ID does not match the
// registered project.
func TestInstall_ProjectIDMismatch(t *testing.T) {
	serverRoot := t.TempDir()
	registeredProjectID, _ := setupServerEnv(t, serverRoot)

	// Create an artifact with a DIFFERENT project ID.
	differentProjectID := "other-project"
	artifactPath := createTestArtifact(t, differentProjectID)

	coordinator := NewServerReleaseCoordinator(serverRoot)
	_, err := coordinator.Install(registeredProjectID, artifactPath)
	if err == nil {
		t.Fatal("expected error for project ID mismatch, got nil")
	}

	// Verify the error message mentions the mismatch.
	if !contains(err.Error(), "does not match") {
		t.Errorf("expected error to mention 'does not match', got: %v", err)
	}
}

// TestInstall_NonexistentArtifact verifies that installing a non-existent
// artifact returns an appropriate error.
//
// AC: An error is returned when the artifact file does not exist.
func TestInstall_NonexistentArtifact(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, _ := setupServerEnv(t, serverRoot)

	coordinator := NewServerReleaseCoordinator(serverRoot)
	_, err := coordinator.Install(projectID, "/nonexistent/artifact.tar.gz")
	if err == nil {
		t.Fatal("expected error for non-existent artifact, got nil")
	}

	if !contains(err.Error(), "artifact not found") {
		t.Errorf("expected error to mention 'artifact not found', got: %v", err)
	}
}

// TestInstall_UnregisteredProject verifies that installing for an unregistered
// project returns an appropriate error.
//
// AC: An error is returned when the project is not registered.
func TestInstall_UnregisteredProject(t *testing.T) {
	serverRoot := t.TempDir()
	artifactPath := createTestArtifact(t, "some-project")

	coordinator := NewServerReleaseCoordinator(serverRoot)
	_, err := coordinator.Install("nonexistent-project", artifactPath)
	if err == nil {
		t.Fatal("expected error for unregistered project, got nil")
	}

	if !contains(err.Error(), "project registry not found") {
		t.Errorf("expected error to mention 'project registry not found', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Activate tests
// ---------------------------------------------------------------------------

// TestActivate_ActivatesRelease verifies that the coordinator:
//   - Transitions a Release from Ready to Active
//   - Persists the stage transitions
//   - Updates the Runtime State with the active release ID
//
// AC 1: Activating a Release in Ready stage transitions it to Active.
// AC 2: Runtime state is updated with the active release ID.
func TestActivate_ActivatesRelease(t *testing.T) {
	serverRoot := t.TempDir()
	releaseID := "rel-001-activate"

	// Setup: register project, create runtime directories, persist a Ready
	// Release JSON.
	projectID, releasePath := setupActivateEnvironment(t, serverRoot, releaseID)

	coordinator := NewServerReleaseCoordinator(serverRoot)
	err := coordinator.Activate(projectID, releaseID)
	if err != nil {
		t.Fatalf("Activate returned unexpected error: %v", err)
	}

	// Verify the Release JSON was updated to Active.
	rel, err := release.Load(releasePath)
	if err != nil {
		t.Fatalf("load release after activation: %v", err)
	}
	if rel.Stage != release.StageActive {
		t.Errorf("Release Stage = %s after activation, want %s", rel.Stage, release.StageActive)
	}

	// Verify transition history.
	if len(rel.Transitions) == 0 {
		t.Fatal("expected at least 1 transition record, got 0")
	}
	foundActive := false
	for _, tr := range rel.Transitions {
		if tr.To == release.StageActive {
			foundActive = true
			break
		}
	}
	if !foundActive {
		t.Error("expected a transition to Active stage")
	}

	// Verify runtime state was updated.
	reg, err := NewRegistryStore(serverRoot).Load(projectID)
	if err != nil {
		t.Fatalf("load project registry: %v", err)
	}
	installRoot := reg.Project.InstallRoot
	statePath := filepath.Join(installRoot, "runtime-state.json")
	stateStore := runtime.NewStateStore(statePath)
	if err := stateStore.Load(); err != nil {
		t.Fatalf("load runtime state: %v", err)
	}
	currentState := stateStore.State()
	if currentState.ActiveReleaseID != releaseID {
		t.Errorf("runtime ActiveReleaseID = %q, want %q", currentState.ActiveReleaseID, releaseID)
	}
}

// TestActivate_NonReadyRelease verifies that activating a Release that is
// not in Ready stage returns an error and does not transition the Release.
//
// AC: An error is returned when the Release is not in Ready stage.
func TestActivate_NonReadyRelease(t *testing.T) {
	serverRoot := t.TempDir()
	releaseID := "rel-not-ready"

	// Setup: register project, create runtime directories, persist a Release
	// JSON that is already in Active stage.
	projectID, releasePath := setupActivateEnvironment(t, serverRoot, releaseID)

	// Overwrite the Release JSON with a Release that is already Active.
	// Keep ArtifactPath from the setup so extraction doesn't fail first.
	existingRel, err := release.Load(releasePath)
	if err != nil {
		t.Fatalf("load release before overwrite: %v", err)
	}
	rel := &release.Release{
		ID:           release.ReleaseID(releaseID),
		ArtifactPath: existingRel.ArtifactPath,
		Stage:        release.StageActive,
		Transitions:  []release.TransitionRecord{},
	}
	if err := rel.Save(releasePath); err != nil {
		t.Fatalf("save active release: %v", err)
	}

	coordinator := NewServerReleaseCoordinator(serverRoot)
	err = coordinator.Activate(projectID, releaseID)
	if err == nil {
		t.Fatal("expected error for non-Ready release, got nil")
	}

	if !contains(err.Error(), "activation failed") {
		t.Errorf("expected error to mention 'activation failed', got: %v", err)
	}
}

// TestActivate_ReleaseNotFound verifies that activating a non-existent
// Release returns an error.
//
// AC: An error is returned when the Release JSON file does not exist.
func TestActivate_ReleaseNotFound(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, _ := setupServerEnv(t, serverRoot)

	coordinator := NewServerReleaseCoordinator(serverRoot)
	err := coordinator.Activate(projectID, "nonexistent-release")
	if err == nil {
		t.Fatal("expected error for non-existent release, got nil")
	}

	if !contains(err.Error(), "load release") {
		t.Errorf("expected error to mention 'load release', got: %v", err)
	}
}

// TestActivate_UnregisteredProject verifies that activating for an
// unregistered project returns an error.
//
// AC: An error is returned when the project is not registered.
func TestActivate_UnregisteredProject(t *testing.T) {
	serverRoot := t.TempDir()

	coordinator := NewServerReleaseCoordinator(serverRoot)
	err := coordinator.Activate("nonexistent-project", "some-release")
	if err == nil {
		t.Fatal("expected error for unregistered project, got nil")
	}

	if !contains(err.Error(), "project registry not found") {
		t.Errorf("expected error to mention 'project registry not found', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Unified release state layout (BUG-002)
// ---------------------------------------------------------------------------

// TestInstall_WritesToReleasePackageStateDir verifies that the coordinator
// persists Release JSON to the SAME directory the internal/release package
// reads (<installRoot>/.anvil/state/releases/), and that the legacy
// <installRoot>/state/releases directory is never used as a second release
// state directory.
//
// DoD (BUG-002): "A test asserting the coordinator writes to the same
// directory the internal/release package reads."
func TestInstall_WritesToReleasePackageStateDir(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, installRoot := setupServerEnv(t, serverRoot)
	artifactPath := createTestArtifact(t, projectID)

	coordinator := NewServerReleaseCoordinator(serverRoot)
	rel, err := coordinator.Install(projectID, artifactPath)
	if err != nil {
		t.Fatalf("Install returned unexpected error: %v", err)
	}

	// The reader path (release.SavePath / release.LookupByID) resolves the
	// state dir via project.NewStructure(runtimePath).StateDir — assert the
	// coordinator's write lands there.
	canonicalPath := filepath.Join(project.NewStructure(installRoot).StateDir, "releases", rel.ID.String()+".json")
	if _, err := os.Stat(canonicalPath); err != nil {
		t.Errorf("Release JSON not found at canonical path %s: %v", canonicalPath, err)
	}

	// The internal/release package must be able to read the Release back —
	// this is the exact read path used by `server release active`, history,
	// and rollback.
	loaded, err := release.LookupByID(installRoot, rel.ID)
	if err != nil {
		t.Fatalf("LookupByID could not read the Release the coordinator wrote: %v", err)
	}
	if loaded.Stage != release.StageReady {
		t.Errorf("loaded Release Stage = %s, want %s", loaded.Stage, release.StageReady)
	}

	// Only ONE release-state directory: the legacy layout must not exist on
	// a fresh root (BUG-002 validation step 5).
	legacyDir := filepath.Join(installRoot, "state", "releases")
	if _, err := os.Stat(legacyDir); err == nil {
		t.Errorf("legacy state dir %s must not exist on a fresh root", legacyDir)
	} else if !os.IsNotExist(err) {
		t.Errorf("stat legacy state dir %s: %v", legacyDir, err)
	}
}

// TestInstallActivate_ReadableThroughReleasePackage verifies the full
// coordinator-level release lifecycle: install + activate through the
// ServerReleaseCoordinator, then read back the Release through the
// internal/release package via GetActiveRelease and LookupByID — the exact
// observability/rollback read path that was broken by the directory
// mismatch (BUG-002).
//
// DoD (BUG-002): "A coordinator-level integration test that performs
// install + activate through ServerReleaseCoordinator and then reads back
// the release via GetActiveRelease / LookupByID."
func TestInstallActivate_ReadableThroughReleasePackage(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, installRoot := setupServerEnv(t, serverRoot)
	artifactPath := createTestArtifact(t, projectID)

	coordinator := NewServerReleaseCoordinator(serverRoot)

	// Install through the coordinator.
	rel, err := coordinator.Install(projectID, artifactPath)
	if err != nil {
		t.Fatalf("Install returned unexpected error: %v", err)
	}

	// Activate through the coordinator.
	if err := coordinator.Activate(projectID, rel.ID.String()); err != nil {
		t.Fatalf("Activate returned unexpected error: %v", err)
	}

	// Read back via GetActiveRelease — the `server release active` path.
	active, err := release.GetActiveRelease(installRoot)
	if err != nil {
		t.Fatalf("GetActiveRelease returned unexpected error: %v", err)
	}
	if active == nil {
		t.Fatal("GetActiveRelease returned nil — the activated Release is not observable (BUG-002 symptom)")
	}
	if active.ID != rel.ID {
		t.Errorf("active Release ID = %s, want %s", active.ID, rel.ID)
	}
	if active.Stage != release.StageActive {
		t.Errorf("active Release Stage = %s, want %s", active.Stage, release.StageActive)
	}

	// Read back via LookupByID — the `server release history` path.
	lookedUp, err := release.LookupByID(installRoot, rel.ID)
	if err != nil {
		t.Fatalf("LookupByID returned unexpected error: %v", err)
	}
	if lookedUp.Stage != release.StageActive {
		t.Errorf("looked-up Release Stage = %s, want %s", lookedUp.Stage, release.StageActive)
	}
	if len(lookedUp.Transitions) == 0 {
		t.Fatal("expected recorded lifecycle transitions after activation")
	}
	foundActive := false
	for _, tr := range lookedUp.Transitions {
		if tr.To == release.StageActive {
			foundActive = true
			break
		}
	}
	if !foundActive {
		t.Error("expected a transition to Active stage in the recorded history")
	}
}

// TestActivate_LegacyLayoutReadable verifies the back-compat behavior for
// server roots provisioned before the layout was unified (BUG-002): a
// Release persisted only in the legacy <installRoot>/state/releases
// directory must remain readable by the coordinator, and activation
// migrates the Release to the canonical .anvil/state/releases directory.
//
// The fixture is a genuine pre-fix legacy root: the entire .anvil tree is
// removed before Activate, so the canonical state directory does not exist
// and must be created by the migration path (Release.Save does not create
// parent directories).
//
// The legacy directory is read-only for the coordinator — the canonical
// directory is the only one ever written.
func TestActivate_LegacyLayoutReadable(t *testing.T) {
	serverRoot := t.TempDir()
	releaseID := "rel-legacy-layout"

	// Build the full activation environment (release directory, artifact
	// store, artifact), then relocate the Release JSON to the legacy
	// directory and remove the entire .anvil tree so the fixture is a
	// genuine pre-fix legacy root.
	projectID, canonicalReleasePath := setupActivateEnvironment(t, serverRoot, releaseID)
	installRoot := filepath.Join(serverRoot, "projects", projectID)

	legacyDir := filepath.Join(installRoot, "state", "releases")
	if err := os.MkdirAll(legacyDir, 0755); err != nil {
		t.Fatalf("mkdir legacy releases dir: %v", err)
	}
	data, err := os.ReadFile(canonicalReleasePath)
	if err != nil {
		t.Fatalf("read canonical release: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, releaseID+".json"), data, 0644); err != nil {
		t.Fatalf("write legacy release: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(installRoot, ".anvil")); err != nil {
		t.Fatalf("remove canonical state tree: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(canonicalReleasePath)); !os.IsNotExist(err) {
		t.Fatalf("canonical state dir must not exist on a pure legacy root, stat err = %v", err)
	}

	// Activate must succeed reading from the legacy location and creating
	// the canonical state directory before migrating the Release.
	coordinator := NewServerReleaseCoordinator(serverRoot)
	if err := coordinator.Activate(projectID, releaseID); err != nil {
		t.Fatalf("Activate of legacy-layout Release returned unexpected error: %v", err)
	}

	// The Release must have been migrated to the canonical directory with
	// the Active stage.
	if _, err := os.Stat(canonicalReleasePath); err != nil {
		t.Errorf("Release not migrated to canonical path %s: %v", canonicalReleasePath, err)
	}
	migrated, err := release.Load(canonicalReleasePath)
	if err != nil {
		t.Fatalf("load migrated release: %v", err)
	}
	if migrated.Stage != release.StageActive {
		t.Errorf("migrated Release Stage = %s, want %s", migrated.Stage, release.StageActive)
	}

	// The stale legacy copy must have been removed — no dual source of truth.
	if _, err := os.Stat(filepath.Join(legacyDir, releaseID+".json")); !os.IsNotExist(err) {
		t.Errorf("stale legacy Release file should have been removed after migration, stat err = %v", err)
	}

	// Canonical-wins precedence: even if a conflicting legacy copy exists
	// again (e.g., written by an older binary or the unsupported manual
	// copy), the read path used by observability and rollback must return
	// the canonical Release.
	if err := os.WriteFile(filepath.Join(legacyDir, releaseID+".json"), data, 0644); err != nil {
		t.Fatalf("recreate stale legacy release: %v", err)
	}
	lookedUp, err := release.LookupByID(installRoot, release.ReleaseID(releaseID))
	if err != nil {
		t.Fatalf("LookupByID after migration returned unexpected error: %v", err)
	}
	if lookedUp.Stage != release.StageActive {
		t.Errorf("LookupByID Stage = %s, want canonical %s (canonical-wins)", lookedUp.Stage, release.StageActive)
	}
	active, err := release.GetActiveRelease(installRoot)
	if err != nil {
		t.Fatalf("GetActiveRelease after migration returned unexpected error: %v", err)
	}
	if active == nil || active.ID.String() != releaseID || active.Stage != release.StageActive {
		t.Errorf("GetActiveRelease = %v, want the canonical Active Release %s", active, releaseID)
	}
}

// ---------------------------------------------------------------------------
// Active release invariant wiring (BUG-003)
// ---------------------------------------------------------------------------

// TestActivate_TwoReleases_ArchivesPreviousActive verifies the production
// release lifecycle end-to-end: when two releases are installed and
// activated through the ServerReleaseCoordinator, the previously Active
// Release transitions to Archived (TS-P4-10, ADR-003 §9.1) and is persisted
// as Archived on disk, while the newly activated Release is the only one in
// Active stage.
//
// Before the fix, ServerReleaseCoordinator.Activate constructed the
// ActivationEngine with a nil active-release invariant, so both releases
// remained Active and no Archived release ever existed for rollback target
// identification (BUG-003).
//
// DoD (BUG-003): "A coordinator-level test that activates two releases
// through ServerReleaseCoordinator and asserts the first transitions to
// Archived."
func TestActivate_TwoReleases_ArchivesPreviousActive(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, installRoot := setupServerEnv(t, serverRoot)

	coordinator := NewServerReleaseCoordinator(serverRoot)

	// Two distinct artifacts for the same project — ArtifactID is
	// content-derived (TS-P3-04), so identical content would collide with
	// the install idempotency check.
	artifactA := createTestArtifactVariant(t, projectID, "1.0.0", "<?php // release A\n")
	artifactB := createTestArtifactVariant(t, projectID, "1.1.0", "<?php // release B\n")

	// Install both releases (Ready stage).
	relA, err := coordinator.Install(projectID, artifactA)
	if err != nil {
		t.Fatalf("Install release A returned unexpected error: %v", err)
	}
	relB, err := coordinator.Install(projectID, artifactB)
	if err != nil {
		t.Fatalf("Install release B returned unexpected error: %v", err)
	}

	// Activate the first release, then the second (Section 4 repro steps).
	if err := coordinator.Activate(projectID, relA.ID.String()); err != nil {
		t.Fatalf("Activate release A returned unexpected error: %v", err)
	}
	if err := coordinator.Activate(projectID, relB.ID.String()); err != nil {
		t.Fatalf("Activate release B returned unexpected error: %v", err)
	}

	// The first release must be Archived — the invariant was enforced
	// during the second activation (BUG-003 symptom: both stayed Active).
	archivedA, err := release.LookupByID(installRoot, relA.ID)
	if err != nil {
		t.Fatalf("LookupByID release A returned unexpected error: %v", err)
	}
	if archivedA.Stage != release.StageArchived {
		t.Errorf("first release Stage = %s after second activation, want %s", archivedA.Stage, release.StageArchived)
	}

	// The archived release is persisted on disk (Validation step 4) and its
	// history records the Active → Archived transition.
	foundArchival := false
	for _, tr := range archivedA.Transitions {
		if tr.To == release.StageArchived && tr.Outcome == "success" {
			foundArchival = true
			break
		}
	}
	if !foundArchival {
		t.Error("first release history does not record a successful Active → Archived transition")
	}

	// The second release must be Active.
	activeB, err := release.LookupByID(installRoot, relB.ID)
	if err != nil {
		t.Fatalf("LookupByID release B returned unexpected error: %v", err)
	}
	if activeB.Stage != release.StageActive {
		t.Errorf("second release Stage = %s, want %s", activeB.Stage, release.StageActive)
	}

	// Exactly one Active release — the invariant (TS-P4-10 AC-3).
	active, err := release.GetActiveRelease(installRoot)
	if err != nil {
		t.Fatalf("GetActiveRelease returned unexpected error: %v", err)
	}
	if active == nil {
		t.Fatal("GetActiveRelease returned nil — expected exactly one Active release")
	}
	if active.ID != relB.ID {
		t.Errorf("Active release ID = %s, want %s (the most recently activated)", active.ID, relB.ID)
	}

	// Runtime state tracks the most recently activated release.
	statePath := filepath.Join(installRoot, "runtime-state.json")
	stateStore := runtime.NewStateStore(statePath)
	if err := stateStore.Load(); err != nil {
		t.Fatalf("load runtime state: %v", err)
	}
	if currentState := stateStore.State(); currentState.ActiveReleaseID != relB.ID.String() {
		t.Errorf("runtime ActiveReleaseID = %q, want %q", currentState.ActiveReleaseID, relB.ID.String())
	}
}

// TestActivate_WiringArchivesPreviouslyActiveRelease is the wiring-level
// check for BUG-003: with a previously Active Release persisted in the
// runtime state, a subsequent Activate through ServerReleaseCoordinator
// must archive it before promoting the new Release.
//
// This test isolates the production wiring — the construction of the
// ActivationEngine inside Activate. The ONLY way the previous release can
// reach Archived here is if the production path passes a non-nil
// ActiveReleaseInvariant: with the pre-fix nil wiring both releases would
// remain Active and this test fails.
//
// DoD (BUG-003): "A test asserting NewActivationEngine receives a non-nil
// invariant in the production wiring (or equivalent wiring-level check)."
func TestActivate_WiringArchivesPreviouslyActiveRelease(t *testing.T) {
	serverRoot := t.TempDir()
	releaseID1 := "rel-001-wiring"
	releaseID2 := "rel-002-wiring"

	// Register the project once and build both release fixtures against it
	// (setupActivateRelease does not re-register — Register rejects
	// duplicates).
	projectID, _ := setupServerEnv(t, serverRoot)
	installRoot := filepath.Join(serverRoot, "projects", projectID)
	releasePath1 := setupActivateRelease(t, projectID, installRoot, releaseID1)
	releasePath2 := setupActivateRelease(t, projectID, installRoot, releaseID2)

	coordinator := NewServerReleaseCoordinator(serverRoot)

	// Activate the first release — no previous Active, so the invariant
	// is a no-op.
	if err := coordinator.Activate(projectID, releaseID1); err != nil {
		t.Fatalf("Activate release 1 returned unexpected error: %v", err)
	}

	// Activate the second release — the invariant must archive release 1.
	if err := coordinator.Activate(projectID, releaseID2); err != nil {
		t.Fatalf("Activate release 2 returned unexpected error: %v", err)
	}

	// Release 1 must be Archived, persisted on disk (Validation step 4).
	rel1, err := release.Load(releasePath1)
	if err != nil {
		t.Fatalf("load release 1: %v", err)
	}
	if rel1.Stage != release.StageArchived {
		t.Errorf("release 1 Stage = %s after activating release 2, want %s", rel1.Stage, release.StageArchived)
	}

	// Release 2 must be Active.
	rel2, err := release.Load(releasePath2)
	if err != nil {
		t.Fatalf("load release 2: %v", err)
	}
	if rel2.Stage != release.StageActive {
		t.Errorf("release 2 Stage = %s, want %s", rel2.Stage, release.StageActive)
	}
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

// contains reports whether substr is within s.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

// containsStr is a simple substring check without external dependencies.
func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
