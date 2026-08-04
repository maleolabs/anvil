// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P4-05, EPIC-004, EPIC-005
package cmd

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/project"
	"maleolabs.com/anvil/internal/release"
	"maleolabs.com/anvil/internal/runtime"
	"maleolabs.com/anvil/internal/server"
)

// setupActivateEnvironment creates a minimal server environment with a
// registered project, runtime directories, a release directory, an artifact
// in the artifact store, and a persisted Release JSON file in the Ready
// stage. It returns:
//   - projectID: the registered project ID
//   - releaseID: the release identity to activate
//   - releasePath: the full path to the Release JSON file
//
// The server root is a temp directory managed by the test.
func setupActivateEnvironment(t *testing.T, serverRoot, releaseID string) (projectID, releasePath string) {
	t.Helper()

	projectID = "test-project"

	// Initialize the server config.
	configStore := server.NewConfigStore(serverRoot)
	cfg := server.DefaultServerConfig()
	cfg.Runtime.ID = "test-runtime"
	if err := configStore.Save(cfg); err != nil {
		t.Fatalf("save server config: %v", err)
	}

	// Register the project with an install root inside the server root.
	installRoot := filepath.Join(serverRoot, "projects", projectID)
	reg := server.DefaultProjectRegistry()
	reg.Project.ID = projectID
	reg.Project.InstallRoot = installRoot
	reg.Project.DisplayName = "Test Project"

	registryStore := server.NewRegistryStore(serverRoot)
	if err := registryStore.Register(reg); err != nil {
		t.Fatalf("register project: %v", err)
	}

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
	// Release state dir: unified layout the coordinator reads/writes
	// (<installRoot>/.anvil/state/releases — BUG-002).
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
	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "index.php"), []byte("<?php\n"), 0644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	sourceArtifact, err := artifact.Package(artifact.PackageOptions{
		SourceDir: sourceDir,
		OutputDir: t.TempDir(),
		Version:   "1.0.0",
		Source:    projectID,
		ProjectID: projectID,
	})
	if err != nil {
		t.Fatalf("package artifact: %v", err)
	}
	storeArtifactPath := filepath.Join(artifactStoreDir, releaseID+".tar.gz")
	src, err := os.Open(sourceArtifact.ArtifactPath)
	if err != nil {
		t.Fatalf("open source artifact: %v", err)
	}
	defer src.Close()
	dst, err := os.Create(storeArtifactPath)
	if err != nil {
		t.Fatalf("create store artifact: %v", err)
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
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

// TestActivateCommand_ActivatesRelease verifies that:
//
//	anvil server release activate <project-id> <release-id>
//
// transitions a Release from Ready to Active through the full activation
// phase sequence and updates the runtime state.
//
// AC 1: Activating a Release in Ready stage transitions it to Active.
// AC 2: The command reports successful activation with the Release ID and stage.
// AC 3: The runtime state is updated with the active release ID.
func TestActivateCommand_ActivatesRelease(t *testing.T) {
	serverRoot := t.TempDir()
	releaseID := "rel-001-activate"
	projectID, releasePath := setupActivateEnvironment(t, serverRoot, releaseID)

	_, stdout, stderr, err := executeCommand("server", "release", "activate",
		projectID, releaseID, "--server-root", serverRoot)
	if err != nil {
		t.Fatalf("activate command returned unexpected error: %v\nstderr: %s", err, stderr)
	}

	// Verify success output.
	if !contains(stdout, "Release activated") {
		t.Errorf("expected 'Release activated' in stdout, got: %s", stdout)
	}
	if !contains(stdout, "Release ID:") {
		t.Errorf("expected 'Release ID:' in stdout, got: %s", stdout)
	}
	if !contains(stdout, releaseID) {
		t.Errorf("expected release ID in stdout, got: %s", stdout)
	}
	if !contains(stdout, "active") {
		t.Errorf("expected stage 'active' in stdout, got: %s", stdout)
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
	if len(rel.Transitions) != 2 {
		t.Fatalf("expected 2 transition records, got %d", len(rel.Transitions))
	}
	if rel.Transitions[0].To != release.StageActivating {
		t.Errorf("transition[0].To = %s, want %s", rel.Transitions[0].To, release.StageActivating)
	}
	if rel.Transitions[1].To != release.StageActive {
		t.Errorf("transition[1].To = %s, want %s", rel.Transitions[1].To, release.StageActive)
	}

	// Verify runtime state was updated.
	installRoot := filepath.Join(serverRoot, "projects", projectID)
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

// TestActivateCommand_ProjectNotRegistered verifies that activating a release
// for an unregistered project returns an appropriate error.
//
// AC 4: An error is returned when the project is not registered.
func TestActivateCommand_ProjectNotRegistered(t *testing.T) {
	serverRoot := t.TempDir()

	_, _, stderr, err := executeCommand("server", "release", "activate",
		"nonexistent-project", "some-release-id", "--server-root", serverRoot)
	if err == nil {
		t.Fatal("activate command should return error for unregistered project")
	}

	if !contains(stderr, "project registry not found") {
		t.Errorf("expected 'project registry not found' in stderr, got: %s", stderr)
	}
}

// TestActivateCommand_ReleaseNotFound verifies that activating a release
// that does not exist returns an appropriate error.
//
// AC 5: An error is returned when the release JSON file does not exist.
func TestActivateCommand_ReleaseNotFound(t *testing.T) {
	serverRoot := t.TempDir()
	projectID := "test-project"

	// Initialize server config and register project (no release created).
	configStore := server.NewConfigStore(serverRoot)
	cfg := server.DefaultServerConfig()
	cfg.Runtime.ID = "test-runtime"
	if err := configStore.Save(cfg); err != nil {
		t.Fatalf("save server config: %v", err)
	}

	installRoot := filepath.Join(serverRoot, "projects", projectID)
	reg := server.DefaultProjectRegistry()
	reg.Project.ID = projectID
	reg.Project.InstallRoot = installRoot

	registryStore := server.NewRegistryStore(serverRoot)
	if err := registryStore.Register(reg); err != nil {
		t.Fatalf("register project: %v", err)
	}

	_, _, stderr, err := executeCommand("server", "release", "activate",
		projectID, "nonexistent-release", "--server-root", serverRoot)
	if err == nil {
		t.Fatal("activate command should return error for nonexistent release")
	}

	if !contains(stderr, "load release") {
		t.Errorf("expected 'load release' in stderr, got: %s", stderr)
	}
}

// TestActivateCommand_ReleaseNotReady verifies that activating a release
// that is not in Ready stage returns an appropriate error.
//
// AC 6: An error is returned when the Release is not in Ready stage.
func TestActivateCommand_ReleaseNotReady(t *testing.T) {
	serverRoot := t.TempDir()
	releaseID := "rel-not-ready"

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
		t.Fatalf("save release JSON: %v", err)
	}

	var stderr string
	_, _, stderr, err = executeCommand("server", "release", "activate",
		projectID, releaseID, "--server-root", serverRoot)
	if err == nil {
		t.Fatal("activate command should return error for non-Ready release")
	}

	if !contains(stderr, "activation failed") {
		t.Errorf("expected 'activation failed' in stderr, got: %s", stderr)
	}

	// Verify the Release JSON was saved with any recorded transitions (best-effort).
	rel, err = release.Load(releasePath)
	if err != nil {
		t.Fatalf("load release after failed activation: %v", err)
	}
	if rel.Stage == release.StageReady {
		t.Errorf("Release Stage should have changed from Active after failed activation, got %s", rel.Stage)
	}
}
