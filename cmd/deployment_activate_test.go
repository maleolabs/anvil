// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P10-04, ADR-015, EPIC-010
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

// setupDeploymentActivateEnvironment creates a minimal server environment with a
// registered project, runtime directories, a release directory, an artifact
// in the artifact store, and a persisted Release JSON file in the Ready
// stage. It returns:
//   - projectID: the registered project ID
//   - releaseID: the release identity to activate
//   - releasePath: the full path to the Release JSON file
//
// The server root is a temp directory managed by the test.
func setupDeploymentActivateEnvironment(t *testing.T, serverRoot, releaseID string) (projectID, releasePath string) {
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

// TestDeploymentActivateCommand_RegistersUnderDeployment verifies that:
//
//	anvil deployment activate
//
// is registered as a subcommand of the deployment command.
func TestDeploymentActivateCommand_RegistersUnderDeployment(t *testing.T) {
	sub, _, err := rootCmd.Find([]string{"deployment", "activate"})
	if err != nil {
		t.Fatalf("rootCmd.Find([\"deployment\", \"activate\"]) returned error: %v", err)
	}
	if sub == nil {
		t.Fatal("rootCmd.Find([\"deployment\", \"activate\"]) returned nil command")
	}
	if sub.Use != "activate <project-id> <release-id>" {
		t.Errorf("command Use = %q, want %q", sub.Use, "activate <project-id> <release-id>")
	}

	// Verify it's nested under deployment (parent is deploymentCmd).
	if sub.Parent() == nil || sub.Parent().Use != "deployment" {
		t.Errorf("activate command parent = %v, want deployment subcommand", sub.Parent())
	}
}

// TestDeploymentActivateCommand_UninitializedRuntime verifies that:
//
//	anvil deployment activate <project-id> <release-id> --server-root <dir>
//
// reports error when the Runtime has not been initialized.
func TestDeploymentActivateCommand_UninitializedRuntime(t *testing.T) {
	dir := t.TempDir()

	_, stdout, stderr, err := executeCommand("deployment", "activate", "my-project", "rel-001", "--server-root", dir)
	if err == nil {
		t.Fatal("expected error for uninitialized runtime, got nil")
	}

	if !contains(stderr, "not initialized") {
		t.Errorf("stderr should report 'not initialized', got: %s", stderr)
	}
	_ = stdout
}

// TestDeploymentActivateCommand_ActivatesRelease verifies that:
//
//	anvil deployment activate <project-id> <release-id> --server-root <dir>
//
// transitions a Release from Ready to Active.
//
// AC 1: Delegates activation to the Server Runtime command surface.
// AC 2: Reports successful activation with the Release ID and stage.
func TestDeploymentActivateCommand_ActivatesRelease(t *testing.T) {
	serverRoot := t.TempDir()
	releaseID := "rel-001-activate"
	projectID, releasePath := setupDeploymentActivateEnvironment(t, serverRoot, releaseID)

	_, stdout, stderr, err := executeCommand("deployment", "activate", projectID, releaseID, "--server-root", serverRoot)
	if err != nil {
		t.Fatalf("activate command returned unexpected error: %v\nstderr: %s", err, stderr)
	}

	// Verify success output.
	if !contains(stdout, "Activation completed") {
		t.Errorf("expected 'Activation completed' in stdout, got: %s", stdout)
	}
	if !contains(stdout, projectID) {
		t.Errorf("expected project ID %q in stdout, got: %s", projectID, stdout)
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
}

// TestDeploymentActivateCommand_JSONOutput verifies that:
//
//	anvil deployment activate <project-id> <release-id> --server-root <dir> --json
//
// produces valid JSON output.
func TestDeploymentActivateCommand_JSONOutput(t *testing.T) {
	serverRoot := t.TempDir()
	releaseID := "rel-json-activate"
	projectID, _ := setupDeploymentActivateEnvironment(t, serverRoot, releaseID)

	_, stdout, stderr, err := executeCommand("deployment", "activate", projectID, releaseID, "--server-root", serverRoot, "--json")
	if err != nil {
		t.Fatalf("activate command returned unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, `"project_id"`) {
		t.Errorf("stdout should contain JSON field 'project_id', got: %s", stdout)
	}
	if !contains(stdout, `"release_id"`) {
		t.Errorf("stdout should contain JSON field 'release_id', got: %s", stdout)
	}
	if !contains(stdout, `"stage"`) {
		t.Errorf("stdout should contain JSON field 'stage', got: %s", stdout)
	}
	if contains(stdout, `"target_id"`) {
		t.Errorf("stdout should NOT contain JSON field 'target_id' (phantom argument removed, TS-019-04-03), got: %s", stdout)
	}
}

// TestDeploymentActivateCommand_ProjectNotRegistered verifies that activating
// a release for an unregistered project returns an appropriate error.
//
// AC 3: An error is returned when the project is not registered.
func TestDeploymentActivateCommand_ProjectNotRegistered(t *testing.T) {
	serverRoot := t.TempDir()

	// Initialize the server config so we pass RequireServerInitialized.
	configStore := server.NewConfigStore(serverRoot)
	cfg := server.DefaultServerConfig()
	cfg.Runtime.ID = "test-runtime"
	if err := configStore.Save(cfg); err != nil {
		t.Fatalf("save server config: %v", err)
	}

	_, _, stderr, err := executeCommand("deployment", "activate", "nonexistent-project", "rel-001", "--server-root", serverRoot)
	if err == nil {
		t.Fatal("activate command should return error for unregistered project")
	}

	if !contains(stderr, "project registry not found") {
		t.Errorf("expected 'project registry not found' in stderr, got: %s", stderr)
	}
}

// TestDeploymentActivateCommand_ServerRootFlag verifies that the --server-root
// flag is available on the activate command.
func TestDeploymentActivateCommand_ServerRootFlag(t *testing.T) {
	activateCmd, _, err := rootCmd.Find([]string{"deployment", "activate"})
	if err != nil {
		t.Fatalf("failed to find deployment activate command: %v", err)
	}
	flag := activateCmd.Flags().Lookup("server-root")
	if flag == nil {
		t.Errorf("flag --server-root should be on the deployment activate subcommand")
	}

	// Verify --json flag exists.
	jsonFlag := activateCmd.Flags().Lookup("json")
	if jsonFlag == nil {
		t.Errorf("flag --json should be on the deployment activate subcommand")
	}
}

// TestDeploymentActivateCommand_ExactArgs verifies that:
//
//	anvil deployment activate
//
// requires exactly two positional arguments.
func TestDeploymentActivateCommand_ExactArgs(t *testing.T) {
	// Test with no args.
	_, _, stderr, err := executeCommand("deployment", "activate")
	if err == nil {
		t.Fatal("expected error for missing args, got nil")
	}
	if !contains(stderr, "requires 2 argument") {
		t.Errorf("stderr should require project-id and release-id, got: %s", stderr)
	}

	// Test with one arg.
	_, _, stderr2, err2 := executeCommand("deployment", "activate", "project-1")
	if err2 == nil {
		t.Fatal("expected error for missing release-id, got nil")
	}
	if !contains(stderr2, "requires 2 argument") {
		t.Errorf("stderr should require exactly 2 args, got: %s", stderr2)
	}

	// Test with three args (the pre-removal form
	// "<target-id> <project-id> <release-id>"): the phantom target-id
	// argument must be rejected per the announced deprecation schedule
	// (TS-019-04-03, ADR-032 D10).
	_, _, stderr3, err3 := executeCommand("deployment", "activate", "target-1", "project-1", "rel-1")
	if err3 == nil {
		t.Fatal("expected error for extra args, got nil")
	}
	if !contains(stderr3, "requires 2 argument") {
		t.Errorf("stderr should require exactly 2 args, got: %s", stderr3)
	}
}

// TestDeploymentActivateCommand_NonDefaultServerRoot verifies that using a
// non-default server root emits a warning.
func TestDeploymentActivateCommand_NonDefaultServerRoot(t *testing.T) {
	// Set up a minimal environment for the command to fail gracefully.
	customRoot := t.TempDir()

	// Create server config so we pass RequireServerInitialized.
	configStore := server.NewConfigStore(customRoot)
	srvCfg := server.DefaultServerConfig()
	srvCfg.Runtime.ID = "test-runtime"
	if err := configStore.Save(srvCfg); err != nil {
		t.Fatalf("save server config: %v", err)
	}

	// We expect the command to fail (no project), but the warning should
	// still appear before the error.
	_, _, stderr, _ := executeCommand("deployment", "activate", "test-project", "rel-001", "--server-root", customRoot)

	if !contains(stderr, "Warning: using non-default server root") {
		t.Errorf("expected non-default server root warning in stderr, got: %s", stderr)
	}
}
