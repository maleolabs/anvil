// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P4-07, EPIC-004, EPIC-005
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

// setupRollbackEnvironment creates a minimal server environment with:
//   - A registered project
//   - An Archived Release (rollback target — was previously Active)
//   - An Active Release (the one that will be rolled back)
//   - Runtime directories, symlink, and runtime-state pointing to the
//     Active Release
//
// Returns the projectID for use in the rollback command.
func setupRollbackEnvironment(t *testing.T, serverRoot string, targetReleaseID, activeReleaseID string) string {
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

// TestRollbackCommand_RollbackSuccess verifies that:
//
//	anvil server release rollback <project-id>
//
// restores the previously Active Release and transitions the currently
// Active Release to RolledBack stage.
//
// AC 1: The currently Active Release transitions to RolledBack.
// AC 2: The previously Active Release is restored to Active.
// AC 3: The command reports rollback details with both release IDs.
// AC 4: The runtime state is updated with the restored release ID.
func TestRollbackCommand_RollbackSuccess(t *testing.T) {
	serverRoot := t.TempDir()
	targetReleaseID := "rel-target-001"
	activeReleaseID := "rel-active-002"

	projectID := setupRollbackEnvironment(t, serverRoot, targetReleaseID, activeReleaseID)

	_, stdout, stderr, err := executeCommand("server", "release", "rollback",
		projectID, "--server-root", serverRoot)
	if err != nil {
		t.Fatalf("rollback command returned unexpected error: %v\nstderr: %s", err, stderr)
	}

	// Verify success output.
	if !contains(stdout, "Rollback completed") {
		t.Errorf("expected 'Rollback completed' in stdout, got: %s", stdout)
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

	// AC 1: Verify the previously Active Release is now RolledBack.
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

	// AC 2: Verify the target Release is now Active.
	restoredPath := filepath.Join(s.StateDir, "releases", targetReleaseID+".json")
	restoredRel, err := release.Load(restoredPath)
	if err != nil {
		t.Fatalf("load restored release: %v", err)
	}
	if restoredRel.Stage != release.StageActive {
		t.Errorf("restored Release Stage = %s, want %s", restoredRel.Stage, release.StageActive)
	}

	// AC 4: Verify runtime state was updated.
	statePath := filepath.Join(installRoot, "runtime-state.json")
	stateStore := runtime.NewStateStore(statePath)
	if err := stateStore.Load(); err != nil {
		t.Fatalf("load runtime state: %v", err)
	}
	currentState := stateStore.State()
	if currentState.ActiveReleaseID != targetReleaseID {
		t.Errorf("runtime ActiveReleaseID = %q, want %q", currentState.ActiveReleaseID, targetReleaseID)
	}

	// Verify the runtime condition is preserved.
	if currentState.RuntimeCondition != runtime.ConditionNormal {
		t.Errorf("runtime condition = %q, want %q", currentState.RuntimeCondition, runtime.ConditionNormal)
	}
}

// TestRollbackCommand_ProjectNotRegistered verifies that rolling back
// an unregistered project returns an appropriate error.
//
// AC 5: An error is returned when the project is not registered.
func TestRollbackCommand_ProjectNotRegistered(t *testing.T) {
	serverRoot := t.TempDir()

	// Initialize server config so we pass the RequireServerInitialized check
	// and exercise the project registry lookup.
	configStore := server.NewConfigStore(serverRoot)
	cfg := server.DefaultServerConfig()
	cfg.Runtime.ID = "test-runtime"
	if err := configStore.Save(cfg); err != nil {
		t.Fatalf("save server config: %v", err)
	}

	_, _, stderr, err := executeCommand("server", "release", "rollback",
		"nonexistent-project", "--server-root", serverRoot)
	if err == nil {
		t.Fatal("rollback command should return error for unregistered project")
	}

	if !contains(stderr, "project registry not found") {
		t.Errorf("expected 'project registry not found' in stderr, got: %s", stderr)
	}
}

// TestRollbackCommand_NoActiveRelease verifies that rolling back when there
// is no Active Release returns an appropriate error.
func TestRollbackCommand_NoActiveRelease(t *testing.T) {
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

	_, _, stderr, err := executeCommand("server", "release", "rollback",
		projectID, "--server-root", serverRoot)
	if err == nil {
		t.Fatal("rollback command should return error when no Active Release exists")
	}

	if !contains(stderr, "no Active Release") {
		t.Errorf("expected 'no Active Release' in stderr, got: %s", stderr)
	}
}

// TestRollbackCommand_NonDefaultServerRoot verifies that using a non-default
// server root emits a warning.
func TestRollbackCommand_NonDefaultServerRoot(t *testing.T) {
	// Create a non-default path (anything different from /etc/anvil).
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
	_, _, stderr, _ := executeCommand("server", "release", "rollback",
		projectID, "--server-root", customRoot)

	if !contains(stderr, "Warning: using non-default server root") {
		t.Errorf("expected non-default server root warning in stderr, got: %s", stderr)
	}
}
