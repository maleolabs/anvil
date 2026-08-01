// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P10-05, ADR-015, EPIC-010
package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/project"
	"maleolabs.com/anvil/internal/release"
	"maleolabs.com/anvil/internal/runtime"
	"maleolabs.com/anvil/internal/server"
)

// setupDeploymentRollbackEnvironment creates a minimal server environment with:
//   - A registered project
//   - An Archived Release (rollback target — was previously Active)
//   - An Active Release (the one that will be rolled back)
//   - Runtime directories, symlink, and runtime-state pointing to the
//     Active Release
//
// Returns the projectID for use in the rollback command.
func setupDeploymentRollbackEnvironment(t *testing.T, serverRoot string, targetReleaseID, activeReleaseID string) string {
	t.Helper()

	projectID := "test-project"

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

	// Create all runtime directories.
	for _, d := range runtimeCfg.AllDirs() {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	// Create project state directory for release persistence.
	s := project.NewStructure(installRoot)
	releasesStateDir := filepath.Join(s.StateDir, "releases")
	if err := os.MkdirAll(releasesStateDir, 0755); err != nil {
		t.Fatalf("mkdir releases state dir: %v", err)
	}
	artifactStoreDir := filepath.Join(installRoot, "artifacts")
	if err := os.MkdirAll(artifactStoreDir, 0755); err != nil {
		t.Fatalf("mkdir artifacts dir: %v", err)
	}

	releasesDir := runtimeCfg.ReleasesDirPath()

	// Create the target Release (previously Active, now Archived).
	targetRel := &release.Release{
		ID:          release.ReleaseID(targetReleaseID),
		Stage:       release.StageArchived,
		Transitions: []release.TransitionRecord{},
	}
	// Add an Archived transition with a timestamp so it's the most recent.
	targetRel.Transitions = append(targetRel.Transitions, release.TransitionRecord{
		Timestamp: "2026-07-28T10:00:00Z",
		From:      release.StageActive,
		To:        release.StageArchived,
		Outcome:   "success",
	})
	targetReleasePath := filepath.Join(releasesStateDir, targetReleaseID+".json")
	if err := targetRel.Save(targetReleasePath); err != nil {
		t.Fatalf("save target release: %v", err)
	}

	// Create the target release directory.
	targetDir := runtime.ReleaseDirPath(releasesDir, targetReleaseID)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatalf("mkdir target release dir: %v", err)
	}

	// Create the Active Release (the one that will be rolled back).
	activeRel := &release.Release{
		ID:          release.ReleaseID(activeReleaseID),
		Stage:       release.StageActive,
		Transitions: []release.TransitionRecord{},
	}
	activeReleasePath := filepath.Join(releasesStateDir, activeReleaseID+".json")
	if err := activeRel.Save(activeReleasePath); err != nil {
		t.Fatalf("save active release: %v", err)
	}

	// Create the active release directory.
	activeDir := runtime.ReleaseDirPath(releasesDir, activeReleaseID)
	if err := os.MkdirAll(activeDir, 0755); err != nil {
		t.Fatalf("mkdir active release dir: %v", err)
	}

	// Set the symlink to point to the currently Active Release.
	switcher := runtime.NewSymlinkSwitcher(runtimeCfg)
	if err := switcher.SwitchTo(activeDir); err != nil {
		t.Fatalf("switch symlink to active release: %v", err)
	}

	// Create runtime-state.json with the Active Release.
	statePath := filepath.Join(installRoot, "runtime-state.json")
	stateStore := runtime.NewStateStore(statePath)
	stateStore.SetActiveRelease(activeReleaseID)
	stateStore.SetRuntimeCondition(runtime.ConditionNormal)
	stateStore.SetSharedResourceStatus(runtime.ResourceAccessible)
	if err := stateStore.Save(); err != nil {
		t.Fatalf("save runtime state: %v", err)
	}

	return projectID
}

// TestDeploymentRollbackCommand_RegistersUnderDeployment verifies that:
//
//	anvil deployment rollback
//
// is registered as a subcommand of the deployment command.
func TestDeploymentRollbackCommand_RegistersUnderDeployment(t *testing.T) {
	sub, _, err := rootCmd.Find([]string{"deployment", "rollback"})
	if err != nil {
		t.Fatalf("rootCmd.Find([\"deployment\", \"rollback\"]) returned error: %v", err)
	}
	if sub == nil {
		t.Fatal("rootCmd.Find([\"deployment\", \"rollback\"]) returned nil command")
	}
	if sub.Use != "rollback <target-id> <project-id>" {
		t.Errorf("command Use = %q, want %q", sub.Use, "rollback <target-id> <project-id>")
	}

	// Verify it's nested under deployment (parent is deploymentCmd).
	if sub.Parent() == nil || sub.Parent().Use != "deployment" {
		t.Errorf("rollback command parent = %v, want deployment subcommand", sub.Parent())
	}
}

// TestDeploymentRollbackCommand_UninitializedRuntime verifies that:
//
//	anvil deployment rollback <target-id> <project-id> --server-root <dir>
//
// reports error when the Runtime has not been initialized.
func TestDeploymentRollbackCommand_UninitializedRuntime(t *testing.T) {
	dir := t.TempDir()

	_, stdout, stderr, err := executeCommand("deployment", "rollback", "my-target", "my-project", "--server-root", dir)
	if err == nil {
		t.Fatal("expected error for uninitialized runtime, got nil")
	}

	if !contains(stderr, "not initialized") {
		t.Errorf("stderr should report 'not initialized', got: %s", stderr)
	}
	_ = stdout
}

// TestDeploymentRollbackCommand_RollbackSuccess verifies that:
//
//	anvil deployment rollback <target-id> <project-id> --server-root <dir>
//
// restores the previously Active Release and transitions the currently
// Active Release to RolledBack stage.
//
// AC 1: Delegates rollback to the Server Runtime command surface.
// AC 2: Reports rollback details with both release IDs and stages.
func TestDeploymentRollbackCommand_RollbackSuccess(t *testing.T) {
	serverRoot := t.TempDir()
	targetReleaseID := "rel-target-001"
	activeReleaseID := "rel-active-002"

	projectID := setupDeploymentRollbackEnvironment(t, serverRoot, targetReleaseID, activeReleaseID)

	_, stdout, stderr, err := executeCommand("deployment", "rollback", "my-target", projectID, "--server-root", serverRoot)
	if err != nil {
		t.Fatalf("rollback command returned unexpected error: %v\nstderr: %s", err, stderr)
	}

	// Verify success output.
	if !contains(stdout, "Rollback completed") {
		t.Errorf("expected 'Rollback completed' in stdout, got: %s", stdout)
	}
	if !contains(stdout, "my-target") {
		t.Errorf("expected target ID 'my-target' in stdout, got: %s", stdout)
	}
	if !contains(stdout, "Rolled Back Release ID:") {
		t.Errorf("expected 'Rolled Back Release ID:' in stdout, got: %s", stdout)
	}
	if !contains(stdout, activeReleaseID) {
		t.Errorf("expected active release ID %q in stdout, got: %s", activeReleaseID, stdout)
	}
	if !contains(stdout, "Restored Release ID:") {
		t.Errorf("expected 'Restored Release ID:' in stdout, got: %s", stdout)
	}
	if !contains(stdout, targetReleaseID) {
		t.Errorf("expected target release ID %q in stdout, got: %s", targetReleaseID, stdout)
	}
	if !contains(stdout, "restored and serving traffic") {
		t.Errorf("expected 'restored and serving traffic' in stdout, got: %s", stdout)
	}

	// Verify the previously Active Release is now RolledBack.
	installRoot := filepath.Join(serverRoot, "projects", projectID)
	s := project.NewStructure(installRoot)
	rolledBackPath := filepath.Join(s.StateDir, "releases", activeReleaseID+".json")
	rolledBackRel, err := release.Load(rolledBackPath)
	if err != nil {
		t.Fatalf("load rolled-back release: %v", err)
	}
	if rolledBackRel.Stage != release.StageRolledBack {
		t.Errorf("rolled-back Release Stage = %s, want %s", rolledBackRel.Stage, release.StageRolledBack)
	}

	// Verify the target Release is now Active.
	restoredPath := filepath.Join(s.StateDir, "releases", targetReleaseID+".json")
	restoredRel, err := release.Load(restoredPath)
	if err != nil {
		t.Fatalf("load restored release: %v", err)
	}
	if restoredRel.Stage != release.StageActive {
		t.Errorf("restored Release Stage = %s, want %s", restoredRel.Stage, release.StageActive)
	}
}

// TestDeploymentRollbackCommand_JSONOutput verifies that:
//
//	anvil deployment rollback <target-id> <project-id> --server-root <dir> --json
//
// produces valid JSON output.
func TestDeploymentRollbackCommand_JSONOutput(t *testing.T) {
	serverRoot := t.TempDir()
	targetReleaseID := "rel-json-target"
	activeReleaseID := "rel-json-active"

	projectID := setupDeploymentRollbackEnvironment(t, serverRoot, targetReleaseID, activeReleaseID)

	_, stdout, stderr, err := executeCommand("deployment", "rollback", "my-target", projectID, "--server-root", serverRoot, "--json")
	if err != nil {
		t.Fatalf("rollback command returned unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, `"target_id"`) {
		t.Errorf("stdout should contain JSON field 'target_id', got: %s", stdout)
	}
	if !contains(stdout, `"project_id"`) {
		t.Errorf("stdout should contain JSON field 'project_id', got: %s", stdout)
	}
	if !contains(stdout, `"rolled_back_release_id"`) {
		t.Errorf("stdout should contain JSON field 'rolled_back_release_id', got: %s", stdout)
	}
	if !contains(stdout, `"restored_release_id"`) {
		t.Errorf("stdout should contain JSON field 'restored_release_id', got: %s", stdout)
	}
	if !contains(stdout, `"my-target"`) {
		t.Errorf("stdout should contain target ID 'my-target' in JSON, got: %s", stdout)
	}
}

// TestDeploymentRollbackCommand_ProjectNotRegistered verifies that rolling back
// an unregistered project returns an appropriate error.
//
// AC 3: An error is returned when the project is not registered.
func TestDeploymentRollbackCommand_ProjectNotRegistered(t *testing.T) {
	serverRoot := t.TempDir()

	// Initialize server config so we pass the RequireServerInitialized check.
	configStore := server.NewConfigStore(serverRoot)
	cfg := server.DefaultServerConfig()
	cfg.Runtime.ID = "test-runtime"
	if err := configStore.Save(cfg); err != nil {
		t.Fatalf("save server config: %v", err)
	}

	_, _, stderr, err := executeCommand("deployment", "rollback", "my-target", "nonexistent-project", "--server-root", serverRoot)
	if err == nil {
		t.Fatal("rollback command should return error for unregistered project")
	}

	if !contains(stderr, "project registry not found") {
		t.Errorf("expected 'project registry not found' in stderr, got: %s", stderr)
	}
}

// TestDeploymentRollbackCommand_NoActiveRelease verifies that rolling back
// when there is no Active Release returns an appropriate error.
func TestDeploymentRollbackCommand_NoActiveRelease(t *testing.T) {
	serverRoot := t.TempDir()
	projectID := "test-project"

	// Initialize server config and register project (no releases created).
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

	_, _, stderr, err := executeCommand("deployment", "rollback", "my-target", projectID, "--server-root", serverRoot)
	if err == nil {
		t.Fatal("rollback command should return error when no Active Release exists")
	}

	if !contains(stderr, "no Active Release") {
		t.Errorf("expected 'no Active Release' in stderr, got: %s", stderr)
	}
}

// TestDeploymentRollbackCommand_ServerRootFlag verifies that the --server-root
// flag is available on the rollback command.
func TestDeploymentRollbackCommand_ServerRootFlag(t *testing.T) {
	rollbackCmd, _, err := rootCmd.Find([]string{"deployment", "rollback"})
	if err != nil {
		t.Fatalf("failed to find deployment rollback command: %v", err)
	}
	flag := rollbackCmd.Flags().Lookup("server-root")
	if flag == nil {
		t.Errorf("flag --server-root should be on the deployment rollback subcommand")
	}

	// Verify --json flag exists.
	jsonFlag := rollbackCmd.Flags().Lookup("json")
	if jsonFlag == nil {
		t.Errorf("flag --json should be on the deployment rollback subcommand")
	}
}

// TestDeploymentRollbackCommand_ExactArgs verifies that:
//
//	anvil deployment rollback
//
// requires exactly two positional arguments.
func TestDeploymentRollbackCommand_ExactArgs(t *testing.T) {
	// Test with no args.
	_, _, stderr, err := executeCommand("deployment", "rollback")
	if err == nil {
		t.Fatal("expected error for missing args, got nil")
	}
	if !contains(stderr, "requires 2 argument") {
		t.Errorf("stderr should require target-id and project-id, got: %s", stderr)
	}

	// Test with one arg.
	_, _, stderr2, err2 := executeCommand("deployment", "rollback", "target-1")
	if err2 == nil {
		t.Fatal("expected error for missing project-id, got nil")
	}
	if !contains(stderr2, "requires 2 argument") {
		t.Errorf("stderr should require exactly 2 args, got: %s", stderr2)
	}

	// Test with three args.
	_, _, stderr3, err3 := executeCommand("deployment", "rollback", "target-1", "project-1", "extra-arg")
	if err3 == nil {
		t.Fatal("expected error for extra args, got nil")
	}
	if !contains(stderr3, "requires 2 argument") {
		t.Errorf("stderr should require exactly 2 args, got: %s", stderr3)
	}
}

// TestDeploymentRollbackCommand_NonDefaultServerRoot verifies that using a
// non-default server root emits a warning.
func TestDeploymentRollbackCommand_NonDefaultServerRoot(t *testing.T) {
	// Create a non-default path.
	customRoot := t.TempDir()

	// Set up a minimal environment to avoid "project not found" error first.
	projectID := "test-project"
	configStore := server.NewConfigStore(customRoot)
	srvCfg := server.DefaultServerConfig()
	srvCfg.Runtime.ID = "test-runtime"
	if err := configStore.Save(srvCfg); err != nil {
		t.Fatalf("save server config: %v", err)
	}

	installRoot := filepath.Join(customRoot, "projects", projectID)
	reg := server.DefaultProjectRegistry()
	reg.Project.ID = projectID
	reg.Project.InstallRoot = installRoot

	registryStore := server.NewRegistryStore(customRoot)
	if err := registryStore.Register(reg); err != nil {
		t.Fatalf("register project: %v", err)
	}

	// We expect the command to fail (no releases), but the warning should
	// still appear before the error.
	_, _, stderr, _ := executeCommand("deployment", "rollback", "my-target", projectID, "--server-root", customRoot)

	if !contains(stderr, "Warning: using non-default server root") {
		t.Errorf("expected non-default server root warning in stderr, got: %s", stderr)
	}
}
