// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P5-04, EPIC-005
package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/runtime"
	"maleolabs.com/anvil/internal/server"
)

// setupCleanupEnvironment creates a minimal server environment with a
// registered project and a release directory. It returns:
//   - rootPath: the server config root
//   - projectID: the registered project ID
//   - releaseID: the release identity for cleanup
//   - cleanup: a function to clean up the temp directory
func setupCleanupEnvironment(t *testing.T, serverRoot string) (projectID, releaseID string) {
	t.Helper()

	projectID = "test-project"
	releaseID = "rel-001-test"

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

	// Create the release directory.
	runtimeCfg := runtime.DefaultRuntimeConfig()
	runtimeCfg.InstallRoot = installRoot

	releasesDir := runtimeCfg.ReleasesDirPath()
	if _, err := runtime.CreateReleaseDir(releasesDir, releaseID); err != nil {
		t.Fatalf("create release dir: %v", err)
	}

	// Add a file so the reclaimed space is > 0.
	releaseDir := runtime.ReleaseDirPath(releasesDir, releaseID)
	if err := os.WriteFile(filepath.Join(releaseDir, "index.php"), []byte("<?php\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	return projectID, releaseID
}

// TestCleanupCommand_RemovesReleaseDirectory verifies that:
//
//	anvil server release cleanup <project-id> <release-id>
//
// removes the release directory and reports reclaimed space.
//
// AC 1: Running the cleanup command for a safe-to-remove Release removes its directory.
// AC 5: The cleanup command reports how much space was reclaimed.
func TestCleanupCommand_RemovesReleaseDirectory(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, releaseID := setupCleanupEnvironment(t, serverRoot)

	_, stdout, stderr, err := executeCommand("server", "release", "cleanup",
		projectID, releaseID, "--server-root", serverRoot, "--force")
	if err != nil {
		t.Fatalf("cleanup command returned unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, "Release directory removed") {
		t.Errorf("expected 'Release directory removed' in stdout, got: %s", stdout)
	}
	if !contains(stdout, "Space reclaimed:") {
		t.Errorf("expected 'Space reclaimed:' in stdout, got: %s", stdout)
	}
	if !contains(stdout, releaseID) {
		t.Errorf("expected release ID in stdout, got: %s", stdout)
	}

	// Verify the release directory no longer exists.
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = filepath.Join(serverRoot, "projects", projectID)
	releaseDir := runtime.ReleaseDirPath(cfg.ReleasesDirPath(), releaseID)
	if _, err := os.Stat(releaseDir); !os.IsNotExist(err) {
		t.Errorf("expected release directory %q to be removed, stat: %v", releaseDir, err)
	}
}

// TestCleanupCommand_OtherReleaseDirsUnaffected verifies that cleaning up
// one release directory does not affect other release directories.
//
// AC 2: Other Release directories are unaffected by the cleanup.
func TestCleanupCommand_OtherReleaseDirsUnaffected(t *testing.T) {
	serverRoot := t.TempDir()
	projectID := "test-project"
	releaseID1 := "rel-001"
	releaseID2 := "rel-002"

	// Initialize server and register project.
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

	// Create two release directories.
	runtimeCfg := runtime.DefaultRuntimeConfig()
	runtimeCfg.InstallRoot = installRoot
	releasesDir := runtimeCfg.ReleasesDirPath()

	dir1, err := runtime.CreateReleaseDir(releasesDir, releaseID1)
	if err != nil {
		t.Fatalf("create first release dir: %v", err)
	}
	dir2, err := runtime.CreateReleaseDir(releasesDir, releaseID2)
	if err != nil {
		t.Fatalf("create second release dir: %v", err)
	}

	// Remove the first one.
	_, stdout, stderr, err := executeCommand("server", "release", "cleanup",
		projectID, releaseID1, "--server-root", serverRoot, "--force")
	if err != nil {
		t.Fatalf("cleanup command failed: %v\nstderr: %s", err, stderr)
	}

	// Verify first directory is removed.
	if _, err := os.Stat(dir1); !os.IsNotExist(err) {
		t.Errorf("expected first release dir %q to be removed", dir1)
	}

	// Verify second directory still exists.
	info, err := os.Stat(dir2)
	if err != nil {
		t.Errorf("expected second release dir %q to exist: %v", dir2, err)
	} else if !info.IsDir() {
		t.Errorf("%q exists but is not a directory", dir2)
	}

	if !contains(stdout, "Release directory removed") {
		t.Errorf("expected success message in stdout, got: %s", stdout)
	}
}

// TestCleanupCommand_UnregisteredProject verifies that running cleanup
// against an unregistered project returns a clear error.
func TestCleanupCommand_UnregisteredProject(t *testing.T) {
	serverRoot := t.TempDir()

	_, _, stderr, err := executeCommand("server", "release", "cleanup",
		"unknown-project", "rel-001", "--server-root", serverRoot)
	if err == nil {
		t.Fatal("expected error for unregistered project, got nil")
	}
	if !contains(stderr, "not registered") {
		t.Errorf("expected 'not registered' error, got: %s", stderr)
	}
}

// TestCleanupCommand_NonexistentRelease verifies that running cleanup
// with a nonexistent release ID returns a clear error.
//
// AC 6: Attempting to remove a nonexistent Release reports that no directory was found.
func TestCleanupCommand_NonexistentRelease(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, _ := setupCleanupEnvironment(t, serverRoot)

	_, _, stderr, err := executeCommand("server", "release", "cleanup",
		projectID, "nonexistent-rel", "--server-root", serverRoot)
	if err == nil {
		t.Fatal("expected error for nonexistent release, got nil")
	}
	if !contains(stderr, "not found") || !contains(stderr, "release") {
		t.Errorf("expected 'not found' error about release, got: %s", stderr)
	}
}

// TestCleanupCommand_MissingArgs verifies that running without arguments
// produces an error.
func TestCleanupCommand_MissingArgs(t *testing.T) {
	// No args.
	_, _, stderr, err := executeCommand("server", "release", "cleanup")
	if err == nil {
		t.Fatal("expected error for missing args, got nil")
	}
	if !contains(stderr, "requires") && !contains(stderr, "accepts") {
		t.Errorf("expected arg validation error, got: %s", stderr)
	}

	// Only one arg.
	_, _, stderr, err = executeCommand("server", "release", "cleanup", "only-project")
	if err == nil {
		t.Fatal("expected error for missing release arg, got nil")
	}
}

// TestCleanupCommand_OutputFormat verifies that the command output follows
// the project's output conventions.
func TestCleanupCommand_OutputFormat(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, releaseID := setupCleanupEnvironment(t, serverRoot)

	_, stdout, stderr, err := executeCommand("server", "release", "cleanup",
		projectID, releaseID, "--server-root", serverRoot, "--force")
	if err != nil {
		t.Fatalf("cleanup command failed: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, "Release directory removed") {
		t.Error("output should start with 'Release directory removed'")
	}
	if !contains(stdout, "Release ID:") {
		t.Error("output should contain Release ID")
	}
	if !contains(stdout, "Space reclaimed:") {
		t.Error("output should contain Space reclaimed")
	}
	if !contains(stdout, "were not affected") {
		t.Error("output should mention other directories were not affected")
	}

	// Verify space reclaimed is a positive value.
	for _, line := range splitLines(stdout) {
		if contains(line, "Space reclaimed:") {
			if contains(line, "0 B") {
				t.Errorf("expected non-zero space reclaimed, got: %s", line)
			}
		}
	}
}

// TestCleanupCommand_ActiveReleaseProtected verifies that attempting to
// remove the Active Release's directory is rejected.
//
// AC 4: Attempting to remove the Active Release's directory is rejected.
func TestCleanupCommand_ActiveReleaseProtected(t *testing.T) {
	serverRoot := t.TempDir()
	projectID := "test-project"
	releaseID := "rel-active"

	// Initialize server and register project.
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

	// Create the release directory.
	runtimeCfg := runtime.DefaultRuntimeConfig()
	runtimeCfg.InstallRoot = installRoot
	releasesDir := runtimeCfg.ReleasesDirPath()
	if _, err := runtime.CreateReleaseDir(releasesDir, releaseID); err != nil {
		t.Fatalf("create release dir: %v", err)
	}

	// Set up RuntimeState with this release as the Active Release.
	statePath := filepath.Join(installRoot, "runtime-state.json")
	stateStore := runtime.NewStateStore(statePath)
	stateStore.SetActiveRelease(releaseID)
	if err := stateStore.Save(); err != nil {
		t.Fatalf("save runtime state: %v", err)
	}

	// Attempt to remove the active release.
	_, _, stderr, err := executeCommand("server", "release", "cleanup",
		projectID, releaseID, "--server-root", serverRoot)
	if err == nil {
		t.Fatal("expected error for active release removal, got nil")
	}
	if !contains(stderr, "Active Release") {
		t.Errorf("expected 'Active Release' error message, got: %s", stderr)
	}

	// Verify the release directory still exists.
	releaseDir := runtime.ReleaseDirPath(releasesDir, releaseID)
	if _, err := os.Stat(releaseDir); os.IsNotExist(err) {
		t.Error("active release directory should still exist after rejected removal")
	}
}

// TestCleanupCommand_NonInteractiveWithoutForceRefused verifies that running
// cleanup in a non-interactive context (empty stdin / EOF) without the
// --force flag is refused with an error message telling the user to use
// --force, and the release directory is left intact.
//
// MVP-001 AC 9.9: Destructive operations require explicit confirmation or
// an override flag.
func TestCleanupCommand_NonInteractiveWithoutForceRefused(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, releaseID := setupCleanupEnvironment(t, serverRoot)

	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = filepath.Join(serverRoot, "projects", projectID)
	releaseDir := runtime.ReleaseDirPath(cfg.ReleasesDirPath(), releaseID)

	// Simulate empty stdin (EOF immediately — non-interactive context).
	rootCmd.SetIn(bytes.NewBufferString(""))
	_, _, stderr, err := executeCommand("server", "release", "cleanup",
		projectID, releaseID, "--server-root", serverRoot)
	rootCmd.SetIn(nil)

	// Non-interactive without --force should return an error.
	if err == nil {
		t.Fatal("expected error for non-interactive cleanup without --force, got nil")
	}
	if !contains(stderr, "requires --force") {
		t.Errorf("expected error about requiring --force, got: %s", stderr)
	}

	// Release directory should still exist (cleanup was refused).
	if _, err := os.Stat(releaseDir); err != nil {
		t.Errorf("expected release directory %q to still exist after refused cleanup, stat: %v", releaseDir, err)
	}
}

// TestCleanupCommand_WithoutForce_Cancelled verifies that running cleanup
// without --force shows the confirmation prompt and, when declined, does
// not remove the release directory.
//
// MVP-001 AC 9.9: Destructive operations require explicit confirmation or
// an override flag.
func TestCleanupCommand_WithoutForce_Cancelled(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, releaseID := setupCleanupEnvironment(t, serverRoot)

	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = filepath.Join(serverRoot, "projects", projectID)
	releaseDir := runtime.ReleaseDirPath(cfg.ReleasesDirPath(), releaseID)

	// Run cleanup without --force, providing "n" as stdin input.
	rootCmd.SetIn(bytes.NewBufferString("n\n"))
	_, stdout, stderr, err := executeCommand("server", "release", "cleanup",
		projectID, releaseID, "--server-root", serverRoot)
	rootCmd.SetIn(nil)

	// Cancellation is not an error.
	if err != nil {
		t.Errorf("expected no error for cancelled cleanup, got: %v\nstderr: %s", err, stderr)
	}

	// Verify cancellation message and that the prompt was shown.
	if !contains(stdout, "Cleanup cancelled") {
		t.Errorf("expected cancellation message, got: %s", stdout)
	}
	if !contains(stdout, "Are you sure") {
		t.Errorf("expected confirmation prompt in stdout, got: %s", stdout)
	}

	// Release directory should still exist.
	if _, err := os.Stat(releaseDir); err != nil {
		t.Errorf("expected release directory %q to still exist after cancelled cleanup, stat: %v", releaseDir, err)
	}
}

// TestCleanupCommand_WithoutForce_Confirmed verifies that running cleanup
// without --force shows the confirmation prompt and, when confirmed with
// "y", removes the release directory.
//
// MVP-001 AC 9.9: Destructive operations require explicit confirmation or
// an override flag.
func TestCleanupCommand_WithoutForce_Confirmed(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, releaseID := setupCleanupEnvironment(t, serverRoot)

	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = filepath.Join(serverRoot, "projects", projectID)
	releaseDir := runtime.ReleaseDirPath(cfg.ReleasesDirPath(), releaseID)

	// Run cleanup without --force, confirming with "y".
	rootCmd.SetIn(bytes.NewBufferString("y\n"))
	_, stdout, stderr, err := executeCommand("server", "release", "cleanup",
		projectID, releaseID, "--server-root", serverRoot)
	rootCmd.SetIn(nil)

	if err != nil {
		t.Fatalf("cleanup command returned unexpected error: %v\nstderr: %s", err, stderr)
	}
	if !contains(stdout, "Release directory removed") {
		t.Errorf("expected success message in stdout, got: %s", stdout)
	}

	// Release directory should be removed after confirmation.
	if _, err := os.Stat(releaseDir); !os.IsNotExist(err) {
		t.Errorf("expected release directory %q to be removed after confirmed cleanup, stat: %v", releaseDir, err)
	}
}

// TestCleanupCommand_ActiveReleaseProtectedWithForce verifies that the
// Active-release protection still holds even when --force is provided.
//
// AC 4 / BUG-010 Validation step 3: cleanup of the Active release is refused.
func TestCleanupCommand_ActiveReleaseProtectedWithForce(t *testing.T) {
	serverRoot := t.TempDir()
	projectID := "test-project"
	releaseID := "rel-active"

	// Initialize server and register project.
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

	// Create the release directory.
	runtimeCfg := runtime.DefaultRuntimeConfig()
	runtimeCfg.InstallRoot = installRoot
	releasesDir := runtimeCfg.ReleasesDirPath()
	if _, err := runtime.CreateReleaseDir(releasesDir, releaseID); err != nil {
		t.Fatalf("create release dir: %v", err)
	}

	// Set up RuntimeState with this release as the Active Release.
	statePath := filepath.Join(installRoot, "runtime-state.json")
	stateStore := runtime.NewStateStore(statePath)
	stateStore.SetActiveRelease(releaseID)
	if err := stateStore.Save(); err != nil {
		t.Fatalf("save runtime state: %v", err)
	}

	// Attempt to remove the active release with --force.
	_, _, stderr, err := executeCommand("server", "release", "cleanup",
		projectID, releaseID, "--server-root", serverRoot, "--force")
	if err == nil {
		t.Fatal("expected error for active release removal with --force, got nil")
	}
	if !contains(stderr, "Active Release") {
		t.Errorf("expected 'Active Release' error message, got: %s", stderr)
	}

	// Verify the release directory still exists.
	releaseDir := runtime.ReleaseDirPath(releasesDir, releaseID)
	if _, err := os.Stat(releaseDir); os.IsNotExist(err) {
		t.Error("active release directory should still exist after rejected removal")
	}
}

// TestCleanupCommand_SharedResourcesUnaffected verifies that shared resource
// directories are not affected by the cleanup.
//
// AC 3: Shared resources are unaffected by the cleanup.
func TestCleanupCommand_SharedResourcesUnaffected(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, releaseID := setupCleanupEnvironment(t, serverRoot)

	// Create a shared resource file before cleanup.
	installRoot := filepath.Join(serverRoot, "projects", projectID)
	sharedConfigDir := filepath.Join(installRoot, "shared", "config")
	if err := os.MkdirAll(sharedConfigDir, 0755); err != nil {
		t.Fatalf("mkdir shared config: %v", err)
	}
	sharedFile := filepath.Join(sharedConfigDir, "app.conf")
	if err := os.WriteFile(sharedFile, []byte("debug=true\n"), 0644); err != nil {
		t.Fatalf("write shared config: %v", err)
	}

	_, _, stderr, err := executeCommand("server", "release", "cleanup",
		projectID, releaseID, "--server-root", serverRoot, "--force")
	if err != nil {
		t.Fatalf("cleanup command failed: %v\nstderr: %s", err, stderr)
	}

	// Verify shared resource file still exists.
	if _, err := os.Stat(sharedFile); os.IsNotExist(err) {
		t.Error("shared resource file should still exist after cleanup")
	}
}

// splitLines splits a string into lines (simple helper for tests).
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
