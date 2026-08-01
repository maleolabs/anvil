package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/server"
)

// TestServerStatusCommand_RegistersUnderServer verifies that:
//
//	anvil server status
//
// is registered as a subcommand under "anvil server".
func TestServerStatusCommand_RegistersUnderServer(t *testing.T) {
	sub, _, err := rootCmd.Find([]string{"server", "status"})
	if err != nil {
		t.Fatalf("rootCmd.Find([\"server\", \"status\"]) returned error: %v", err)
	}
	if sub == nil {
		t.Fatal("rootCmd.Find([\"server\", \"status\"]) returned nil command")
	}
	if sub.Use != "status [<project-id>]" {
		t.Errorf("command Use = %q, want %q", sub.Use, "status [<project-id>]")
	}

	// Verify it's nested under server (parent is serverCmd).
	if sub.Parent() == nil || sub.Parent().Use != "server" {
		t.Errorf("status command parent = %v, want server subcommand", sub.Parent())
	}
}

// TestServerStatusCommand_UninitializedRuntime verifies that:
//
//	anvil server status --server-root <dir>
//
// reports "not initialized" when the Runtime has not been initialized.
func TestServerStatusCommand_UninitializedRuntime(t *testing.T) {
	dir := t.TempDir()

	_, stdout, stderr, err := executeCommand("server", "status", "--server-root", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, "not initialized") {
		t.Errorf("stdout should report 'not initialized', got: %s", stdout)
	}
	if !contains(stdout, "anvil server init") {
		t.Errorf("stdout should mention 'anvil server init', got: %s", stdout)
	}
}

// TestServerStatusCommand_InitializedRuntime verifies that:
//
//	anvil server status --server-root <dir>
//
// reports "initialized" after the Runtime has been initialized.
func TestServerStatusCommand_InitializedRuntime(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server first.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	_, stdout, stderr, err := executeCommand("server", "status", "--server-root", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, "initialized") {
		t.Errorf("stdout should report 'initialized', got: %s", stdout)
	}
	if contains(stdout, "not initialized") {
		t.Errorf("stdout should not report 'not initialized', got: %s", stdout)
	}
}

// TestServerStatusCommand_RegisteredProject verifies that:
//
//	anvil server status <project-id> --server-root <dir>
//
// reports "registered" for a project that exists in the Registry.
func TestServerStatusCommand_RegisteredProject(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server first.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// Register a project.
	_, _, _, err = executeCommand(
		"server", "project", "register",
		"--server-root", dir,
		"--project-id", "status-test",
		"--install-root", "/srv/status-test",
		"--display-name", "Status Test",
		"--adapter", "node",
		"--owner", "admin",
		"--group", "admin",
		"--non-interactive",
	)
	if err != nil {
		t.Fatalf("registration failed: %v", err)
	}

	// Check status with project ID.
	_, stdout, stderr, err := executeCommand(
		"server", "status",
		"status-test",
		"--server-root", dir,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, "registered") {
		t.Errorf("stdout should report 'registered', got: %s", stdout)
	}
	if !contains(stdout, "status-test") {
		t.Errorf("stdout should contain project ID, got: %s", stdout)
	}
	if !contains(stdout, "/srv/status-test") {
		t.Errorf("stdout should contain install root, got: %s", stdout)
	}
	if !contains(stdout, "Status Test") {
		t.Errorf("stdout should contain display name, got: %s", stdout)
	}
	if !contains(stdout, "node") {
		t.Errorf("stdout should contain adapter, got: %s", stdout)
	}
	if !contains(stdout, "admin") {
		t.Errorf("stdout should contain owner/group, got: %s", stdout)
	}
}

// TestServerStatusCommand_UnknownProject verifies that:
//
//	anvil server status <nonexistent-id> --server-root <dir>
//
// reports "unknown (not registered)" for a project that does not exist.
func TestServerStatusCommand_UnknownProject(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server first.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	_, stdout, stderr, err := executeCommand(
		"server", "status",
		"unknown-project",
		"--server-root", dir,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, "unknown") {
		t.Errorf("stdout should report 'unknown', got: %s", stdout)
	}
	if !contains(stdout, "not registered") {
		t.Errorf("stdout should report 'not registered', got: %s", stdout)
	}
}

// TestServerStatusCommand_NoProjectArg verifies that:
//
//	anvil server status --server-root <dir>
//
// works without a project ID argument and shows registered projects overview.
func TestServerStatusCommand_NoProjectArg(t *testing.T) {
	dir := t.TempDir()

	_, stdout, stderr, err := executeCommand("server", "status", "--server-root", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	// Should still show runtime status.
	if !contains(stdout, "Server Runtime Status") {
		t.Errorf("stdout should contain 'Server Runtime Status', got: %s", stdout)
	}
	// Should show registered projects section (even if empty).
	if !contains(stdout, "Registered Projects") {
		t.Errorf("stdout should contain 'Registered Projects', got: %s", stdout)
	}
	// Should show guidance when no projects are registered.
	if !contains(stdout, "No projects registered") {
		t.Errorf("stdout should mention 'No projects registered', got: %s", stdout)
	}
}

// TestServerStatusCommand_ServerRootFlag verifies that:
//
//	anvil server status --server-root <dir>
//
// uses the specified server root and reports status correctly.
func TestServerStatusCommand_ServerRootFlag(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// Check status with --server-root.
	_, stdout, stderr, err := executeCommand("server", "status", "--server-root", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, "initialized") {
		t.Errorf("stdout should report 'initialized', got: %s", stdout)
	}

	// Verify --server-root flag exists on this command.
	statusCmd, _, err := rootCmd.Find([]string{"server", "status"})
	if err != nil {
		t.Fatalf("failed to find status command: %v", err)
	}
	flag := statusCmd.Flags().Lookup("server-root")
	if flag == nil {
		t.Errorf("flag --server-root should be on the server status subcommand")
	}
}

// TestServerStatusCommand_ReadOnly verifies that:
//
//	anvil server status --server-root <dir>
//
// does not create any files or modify any state.
func TestServerStatusCommand_ReadOnly(t *testing.T) {
	dir := t.TempDir()

	// Record initial state before running status.
	initialEntries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read directory before status: %v", err)
	}

	// Run status (uninitialized, so no existing files).
	_, _, stderr, err := executeCommand("server", "status", "--server-root", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	// Verify no files were created.
	afterEntries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read directory after status: %v", err)
	}

	if len(afterEntries) != len(initialEntries) {
		t.Errorf("status command created files: before=%d, after=%d",
			len(initialEntries), len(afterEntries))
		for _, e := range afterEntries {
			t.Logf("  file: %s", e.Name())
		}
	}

	// Also verify with an initialized runtime.
	dir2 := t.TempDir()
	_, _, _, err = executeCommand("server", "init", "--server-root", dir2)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// Register a project.
	_, _, _, err = executeCommand(
		"server", "project", "register",
		"--server-root", dir2,
		"--project-id", "readonly-test",
		"--install-root", "/srv/readonly-test",
		"--non-interactive",
	)
	if err != nil {
		t.Fatalf("registration failed: %v", err)
	}

	// Record config file modification time before status.
	configPath := filepath.Join(dir2, "config.yaml")
	configBefore, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("failed to stat config before status: %v", err)
	}

	projectPath := filepath.Join(dir2, "projects", "readonly-test.yaml")
	projectBefore, err := os.Stat(projectPath)
	if err != nil {
		t.Fatalf("failed to stat project file before status: %v", err)
	}

	// Run status with project ID.
	_, _, stderr, err = executeCommand(
		"server", "status",
		"readonly-test",
		"--server-root", dir2,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	// Verify config file was not modified.
	configAfter, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("failed to stat config after status: %v", err)
	}
	if !configBefore.ModTime().Equal(configAfter.ModTime()) {
		t.Error("config.yaml was modified by read-only status command")
	}

	// Verify project file was not modified.
	projectAfter, err := os.Stat(projectPath)
	if err != nil {
		t.Fatalf("failed to stat project file after status: %v", err)
	}
	if !projectBefore.ModTime().Equal(projectAfter.ModTime()) {
		t.Error("project file was modified by read-only status command")
	}
}

// TestServerStatusCommand_AllProjects verifies that:
//
//	anvil server status --server-root <dir>
//
// lists all registered projects when no project ID argument is provided.
func TestServerStatusCommand_AllProjects(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// Register two projects.
	for _, pid := range []string{"project-alpha", "project-beta"} {
		_, _, _, err = executeCommand(
			"server", "project", "register",
			"--server-root", dir,
			"--project-id", pid,
			"--install-root", "/srv/"+pid,
			"--display-name", pid,
			"--non-interactive",
		)
		if err != nil {
			t.Fatalf("registration failed for %s: %v", pid, err)
		}
	}

	// Check status without project ID.
	_, stdout, stderr, err := executeCommand("server", "status", "--server-root", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	// Should list both projects.
	if !contains(stdout, "project-alpha") {
		t.Errorf("stdout should contain 'project-alpha', got: %s", stdout)
	}
	if !contains(stdout, "project-beta") {
		t.Errorf("stdout should contain 'project-beta', got: %s", stdout)
	}
}

// TestServerStatusCommand_ProjectConfigValidation verifies that:
//
//	anvil server status <project-id> --server-root <dir>
//
// validates the project registry configuration without modifying it.
func TestServerStatusCommand_ProjectConfigValidation(t *testing.T) {
	dir := t.TempDir()

	// Initialize and register a project manually (not via command) to test
	// validation output with various field states.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// Register a full project.
	_, _, _, err = executeCommand(
		"server", "project", "register",
		"--server-root", dir,
		"--project-id", "validation-test",
		"--install-root", "/srv/validation-test",
		"--display-name", "Validation Test",
		"--adapter", "laravel",
		"--owner", "team-a",
		"--group", "devs",
		"--non-interactive",
	)
	if err != nil {
		t.Fatalf("registration failed: %v", err)
	}

	// Check status and verify all fields are displayed.
	_, stdout, stderr, err := executeCommand(
		"server", "status",
		"validation-test",
		"--server-root", dir,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, "validation-test") {
		t.Errorf("stdout should contain project ID, got: %s", stdout)
	}
	if !contains(stdout, "/srv/validation-test") {
		t.Errorf("stdout should contain install root, got: %s", stdout)
	}
	if !contains(stdout, "Validation Test") {
		t.Errorf("stdout should contain display name, got: %s", stdout)
	}
	if !contains(stdout, "laravel") {
		t.Errorf("stdout should contain adapter, got: %s", stdout)
	}
	if !contains(stdout, "team-a") {
		t.Errorf("stdout should contain owner, got: %s", stdout)
	}
	if !contains(stdout, "devs") {
		t.Errorf("stdout should contain group, got: %s", stdout)
	}
}

// ensure server import is used for the test file.
var _ = server.NewConfigStore
