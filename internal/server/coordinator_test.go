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

	// Create a temporary source directory with a single file.
	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "index.php"), []byte("<?php\n"), 0644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	// Create the output directory for the artifact.
	outputDir := t.TempDir()

	// Package the artifact with the test project ID.
	result, err := artifact.Package(artifact.PackageOptions{
		SourceDir: sourceDir,
		OutputDir: outputDir,
		Version:   "1.0.0",
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
	releasesStateDir := filepath.Join(installRoot, "state", "releases")
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

	return projectID, releaseFilePath
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

	// Verify the Release JSON was persisted to disk (runtime layout: state/releases/).
	releasesDir := filepath.Join(rel.RuntimePath, "state", "releases")
	releasePath := filepath.Join(releasesDir, rel.ID.String()+".json")
	if _, err := os.Stat(releasePath); err != nil {
		t.Errorf("Release JSON not found at %s: %v", releasePath, err)
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
