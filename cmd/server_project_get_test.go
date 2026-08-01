package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/server"
)

// TestServerProjectGetCommand_RegistersUnderServerProject verifies that:
//
//	anvil server project get
//
// is registered as a subcommand under "anvil server project".
func TestServerProjectGetCommand_RegistersUnderServerProject(t *testing.T) {
	sub, _, err := rootCmd.Find([]string{"server", "project", "get"})
	if err != nil {
		t.Fatalf("rootCmd.Find([\"server\", \"project\", \"get\"]) returned error: %v", err)
	}
	if sub == nil {
		t.Fatal("rootCmd.Find([\"server\", \"project\", \"get\"]) returned nil command")
	}
	if sub.Use != "get <project-id>" {
		t.Errorf("command Use = %q, want %q", sub.Use, "get <project-id>")
	}

	// Verify it's nested under project (parent is serverProjectCmd).
	if sub.Parent() == nil || sub.Parent().Use != "project" {
		t.Errorf("get command parent = %v, want project subcommand", sub.Parent())
	}
}

// TestServerProjectGetCommand_LookupByID verifies that:
//
//	anvil server project get <project-id> --server-root <dir>
//
// resolves and displays the project configuration by its project ID.
func TestServerProjectGetCommand_LookupByID(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server first.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// Register a project via non-interactive mode.
	_, _, _, err = executeCommand(
		"server", "project", "register",
		"--server-root", dir,
		"--project-id", "lookup-test",
		"--install-root", "/srv/lookup-test",
		"--display-name", "Lookup Test",
		"--adapter", "node",
		"--owner", "admin",
		"--group", "admin",
		"--non-interactive",
	)
	if err != nil {
		t.Fatalf("registration failed: %v", err)
	}

	// Lookup by project ID.
	_, stdout, stderr, err := executeCommand(
		"server", "project", "get",
		"lookup-test",
		"--server-root", dir,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	// Verify output contains project details.
	if !contains(stdout, "lookup-test") {
		t.Errorf("stdout should contain project ID, got: %s", stdout)
	}
	if !contains(stdout, "/srv/lookup-test") {
		t.Errorf("stdout should contain install root, got: %s", stdout)
	}
	if !contains(stdout, "Lookup Test") {
		t.Errorf("stdout should contain display name, got: %s", stdout)
	}
	if !contains(stdout, "node") {
		t.Errorf("stdout should contain adapter, got: %s", stdout)
	}
	if !contains(stdout, "admin") {
		t.Errorf("stdout should contain owner, got: %s", stdout)
	}
}

// TestServerProjectGetCommand_NotFound verifies that:
//
//	anvil server project get <nonexistent-id> --server-root <dir>
//
// returns an error when the project is not found.
func TestServerProjectGetCommand_NotFound(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server first.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	_, _, stderr, err := executeCommand(
		"server", "project", "get",
		"nonexistent-project",
		"--server-root", dir,
	)
	if err == nil {
		t.Fatal("expected error for non-existent project, got nil")
	}

	if !contains(stderr, "not found") {
		t.Errorf("stderr should contain 'not found', got: %s", stderr)
	}
}

// TestServerProjectGetCommand_NoCwdDependency verifies that:
//
//	anvil server project get <project-id> --server-root <dir>
//
// does not depend on cwd or repository discovery. It resolves purely by
// project ID from the Registry.
func TestServerProjectGetCommand_NoCwdDependency(t *testing.T) {
	dir := t.TempDir()

	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// Register a project.
	_, _, _, err = executeCommand(
		"server", "project", "register",
		"--server-root", dir,
		"--project-id", "no-cwd-test",
		"--install-root", "/srv/no-cwd-test",
		"--non-interactive",
	)
	if err != nil {
		t.Fatalf("registration failed: %v", err)
	}

	// Lookup the project. This should work regardless of cwd.
	_, stdout, stderr, err := executeCommand(
		"server", "project", "get",
		"no-cwd-test",
		"--server-root", dir,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, "no-cwd-test") {
		t.Errorf("stdout should contain project ID, got: %s", stdout)
	}

	// Should not mention anvil.yaml or RequireProject.
	if contains(stderr, "anvil.yaml") {
		t.Errorf("stderr should not mention anvil.yaml, got: %s", stderr)
	}
}

// TestServerProjectGetCommand_ServerRootFlag verifies that:
//
//	anvil server project get <project-id> --server-root <dir>
//
// uses the specified server root and locates the project file correctly.
func TestServerProjectGetCommand_ServerRootFlag(t *testing.T) {
	dir := t.TempDir()

	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// Register a project.
	_, _, _, err = executeCommand(
		"server", "project", "register",
		"--server-root", dir,
		"--project-id", "root-flag-test",
		"--install-root", "/srv/root-flag-test",
		"--non-interactive",
	)
	if err != nil {
		t.Fatalf("registration failed: %v", err)
	}

	// Lookup with --server-root.
	_, stdout, stderr, err := executeCommand(
		"server", "project", "get",
		"root-flag-test",
		"--server-root", dir,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, "root-flag-test") {
		t.Errorf("stdout should contain project ID, got: %s", stdout)
	}

	// Verify the file exists at the expected path under our server root.
	expectedPath := filepath.Join(dir, "projects", "root-flag-test.yaml")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("project file should exist at %s", expectedPath)
	}

	// Verify --server-root flag exists on this command (not on parent).
	getCmd, _, err := rootCmd.Find([]string{"server", "project", "get"})
	if err != nil {
		t.Fatalf("failed to find get command: %v", err)
	}
	flag := getCmd.Flags().Lookup("server-root")
	if flag == nil {
		t.Errorf("flag --server-root should be on the server project get subcommand")
	}
}

// TestServerProjectGetCommand_MissingArg verifies that:
//
//	anvil server project get
//
// (without the required project ID argument) returns an error.
func TestServerProjectGetCommand_MissingArg(t *testing.T) {
	_, _, stderr, err := executeCommand("server", "project", "get")
	if err == nil {
		t.Fatal("expected error for missing project ID argument, got nil")
	}

	if !contains(stderr, "requires 1 argument") {
		t.Errorf("stderr should mention 'requires 1 argument', got: %s", stderr)
	}
}

// TestServerProjectGetCommand_MinimalProject verifies that:
//
//	anvil server project get <project-id> --server-root <dir>
//
// displays correctly for a minimally registered project (only ID and
// install root, no optional fields).
func TestServerProjectGetCommand_MinimalProject(t *testing.T) {
	dir := t.TempDir()

	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// Register a minimal project.
	_, _, _, err = executeCommand(
		"server", "project", "register",
		"--server-root", dir,
		"--project-id", "minimal",
		"--install-root", "/srv/minimal",
		"--non-interactive",
	)
	if err != nil {
		t.Fatalf("registration failed: %v", err)
	}

	// Lookup.
	_, stdout, stderr, err := executeCommand(
		"server", "project", "get",
		"minimal",
		"--server-root", dir,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, "minimal") {
		t.Errorf("stdout should contain project ID, got: %s", stdout)
	}
	if !contains(stdout, "/srv/minimal") {
		t.Errorf("stdout should contain install root, got: %s", stdout)
	}
}

// ensure server import is used for the test file.
var _ = server.NewRegistryStore
