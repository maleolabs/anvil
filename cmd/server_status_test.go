package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/release"
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
	if contains(stdout, "Owner") || contains(stdout, "Group") {
		t.Errorf("stdout must not render demoted owner/group output (ADR-031 §3), got: %s", stdout)
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
	if !contains(stdout, "Server Runtime") {
		t.Errorf("stdout should contain 'Server Runtime' (modern Header), got: %s", stdout)
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
	if contains(stdout, "team-a") || contains(stdout, "devs") || contains(stdout, "Owner") || contains(stdout, "Group") {
		t.Errorf("stdout must not render demoted owner/group output (ADR-031 §3), got: %s", stdout)
	}
}

// ensure server import is used for the test file.
var _ = server.NewConfigStore

// ---------------------------------------------------------------------------
// Lifecycle observability section — TS-015-05-01, ADR-036 §3
//
// "anvil server status <project-id>" must observe the lifecycle convention:
// what is active, what is installed, what can roll back, release status,
// and runtime state — read from the authoritative lifecycle state
// (ADR-031 §3), never inferred, and never mutated.
// ---------------------------------------------------------------------------

// packageObservabilityArtifact creates a minimal verified artifact via the
// production packaging path (artifact.Package).
func packageObservabilityArtifact(t *testing.T, projectID, version, content string) string {
	t.Helper()

	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "index.php"), []byte(content), 0644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	result, err := artifact.Package(artifact.PackageOptions{
		SourceDir: sourceDir,
		OutputDir: t.TempDir(),
		Version:   version,
		Source:    projectID,
		ProjectID: projectID,
	})
	if err != nil {
		t.Fatalf("package artifact: %v", err)
	}
	return result.ArtifactPath
}

// installAndActivateViaCoordinator runs the production install + activate
// lifecycle paths for a project and returns the installed Release.
func installAndActivateViaCoordinator(t *testing.T, serverRoot, projectID, version, content string) *release.Release {
	t.Helper()

	coordinator := server.NewServerReleaseCoordinator(serverRoot)
	rel, err := coordinator.Install(projectID, packageObservabilityArtifact(t, projectID, version, content))
	if err != nil {
		t.Fatalf("Install %s: %v", version, err)
	}
	// Shared env gate (console build-once): ensure valid .env before Activate.
	// The install root is resolved from the project registry.
	store := server.NewRegistryStore(serverRoot)
	if reg, err := store.Load(projectID); err == nil {
		sharedDir := filepath.Join(reg.Project.InstallRoot, "shared")
		_ = os.MkdirAll(sharedDir, 0755)
		_ = os.WriteFile(filepath.Join(sharedDir, ".env"), []byte("APP_ENV=production\nAPP_KEY=base64:testkey1234567890\n"), 0644)
	}
	if err := coordinator.Activate(projectID, rel.ID.String()); err != nil {
		t.Fatalf("Activate %s: %v", version, err)
	}
	return rel
}

// TestServerStatusCommand_LifecycleReportsAuthoritativeState verifies that
// "anvil server status <project-id>" renders the lifecycle observability
// section matching the authoritative lifecycle state after real lifecycle
// operations (install A + activate A, install B + activate B):
//
//   - Active Release = B (from release state)
//   - Installed = 2 releases with stages (A archived, B active)
//   - Rollback = eligible, restoring A (the previously Active Release)
//   - Runtime State = recorded, active release B
//
// Reference: TS-015-05-01 AC: observability reports what is active, what is
// installed, what can roll back; release status and state queries return
// authoritative state.
func TestServerStatusCommand_LifecycleReportsAuthoritativeState(t *testing.T) {
	dir := t.TempDir()
	installRoot := filepath.Join(dir, "srv", "status-test")

	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}
	_, _, _, err = executeCommand(
		"server", "project", "register",
		"--server-root", dir,
		"--project-id", "status-test",
		"--install-root", installRoot,
		"--non-interactive",
	)
	if err != nil {
		t.Fatalf("registration failed: %v", err)
	}

	relA := installAndActivateViaCoordinator(t, dir, "status-test", "1.0.0", "<?php // lifecycle A\n")
	relB := installAndActivateViaCoordinator(t, dir, "status-test", "1.1.0", "<?php // lifecycle B\n")

	_, stdout, stderr, err := executeCommand("server", "status", "status-test", "--server-root", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	// Section presence.
	if !contains(stdout, "Lifecycle") {
		t.Errorf("stdout should contain a Lifecycle section, got: %s", stdout)
	}

	// What is active — must be B, matching release.GetActiveRelease (modern Header).
	if !contains(stdout, "Active Release") || !contains(stdout, relB.ID.String()) {
		t.Errorf("stdout should report Active Release %s, got: %s", relB.ID, stdout)
	}
	// B is Active; A must not appear in the Active line (it may appear in the table).
	for _, line := range strings.Split(stdout, "\n") {
		if strings.Contains(line, "Active Release") && strings.Contains(line, relA.ID.String()) {
			t.Errorf("stdout must not report %s as Active (B is Active), got: %s", relA.ID, stdout)
		}
	}

	// What is installed + release status — both releases with stages (StyledTable).
	if !contains(stdout, "2 release(s)") {
		t.Errorf("stdout should report 2 installed releases, got: %s", stdout)
	}
	if !contains(stdout, relA.ID.String()) || !contains(stdout, "1.0.0") || !contains(stdout, "archived") {
		t.Errorf("stdout should list A as archived, got: %s", stdout)
	}
	if !contains(stdout, relB.ID.String()) || !contains(stdout, "1.1.0") || !contains(stdout, "active") {
		t.Errorf("stdout should list B as active, got: %s", stdout)
	}

	// What can roll back — eligible, restoring A.
	if !contains(stdout, "Rollback") || !contains(stdout, "eligible") || !contains(stdout, relA.ID.String()) {
		t.Errorf("stdout should report rollback eligible restoring %s, got: %s", relA.ID, stdout)
	}

	// State queries — runtime state recorded with B active.
	if !contains(stdout, "Runtime State:") || !contains(stdout, "active release "+relB.ID.String()) {
		t.Errorf("stdout should report runtime state with active release %s, got: %s", relB.ID, stdout)
	}
	if !contains(stdout, "condition normal") {
		t.Errorf("stdout should report runtime condition, got: %s", stdout)
	}
	if !contains(stdout, "shared accessible") {
		t.Errorf("stdout should report shared resource status, got: %s", stdout)
	}
}

// TestServerStatusCommand_LifecycleEmptyProject verifies that a registered
// project with no lifecycle activity renders an empty-but-valid lifecycle
// section: no Active Release, zero installed Releases, rollback not
// eligible, runtime state not recorded.
func TestServerStatusCommand_LifecycleEmptyProject(t *testing.T) {
	dir := t.TempDir()
	installRoot := filepath.Join(dir, "srv", "empty-test")

	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}
	_, _, _, err = executeCommand(
		"server", "project", "register",
		"--server-root", dir,
		"--project-id", "empty-test",
		"--install-root", installRoot,
		"--non-interactive",
	)
	if err != nil {
		t.Fatalf("registration failed: %v", err)
	}

	_, stdout, stderr, err := executeCommand("server", "status", "empty-test", "--server-root", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, "Active Release") || !contains(stdout, "none") {
		t.Errorf("stdout should report Active Release none (modern Header), got: %s", stdout)
	}
	if !contains(stdout, "0 release(s)") {
		t.Errorf("stdout should report 0 installed releases, got: %s", stdout)
	}
	if !contains(stdout, "not eligible") {
		t.Errorf("stdout should report rollback not eligible, got: %s", stdout)
	}
	if !contains(stdout, "Runtime State: not recorded") {
		t.Errorf("stdout should report runtime state not recorded, got: %s", stdout)
	}
}

// TestServerStatusCommand_LifecycleReadOnly verifies the read-only property
// of the observability surface: running "anvil server status <project-id>"
// leaves every state file byte-identical — the surface observes and never
// mutates state (TS-015-05-01).
func TestServerStatusCommand_LifecycleReadOnly(t *testing.T) {
	dir := t.TempDir()
	installRoot := filepath.Join(dir, "srv", "ro-test")

	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}
	_, _, _, err = executeCommand(
		"server", "project", "register",
		"--server-root", dir,
		"--project-id", "ro-test",
		"--install-root", installRoot,
		"--non-interactive",
	)
	if err != nil {
		t.Fatalf("registration failed: %v", err)
	}

	installAndActivateViaCoordinator(t, dir, "ro-test", "1.0.0", "<?php // lifecycle ro\n")

	before := snapshotStatusFiles(t, installRoot)

	_, _, stderr, err := executeCommand("server", "status", "ro-test", "--server-root", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	after := snapshotStatusFiles(t, installRoot)
	for path, want := range before {
		if got, ok := after[path]; !ok {
			t.Errorf("file %s removed by read-only status command", path)
		} else if got != want {
			t.Errorf("file %s modified by read-only status command", path)
		}
	}
	if len(after) != len(before) {
		t.Errorf("status command created files: before=%d after=%d", len(before), len(after))
	}
}

// snapshotStatusFiles returns the content of every regular file under the
// install root, keyed by relative path.
func snapshotStatusFiles(t *testing.T, installRoot string) map[string]string {
	t.Helper()

	files := map[string]string{}
	err := filepath.Walk(installRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(installRoot, path)
		if err != nil {
			return err
		}
		files[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot install root %s: %v", installRoot, err)
	}
	return files
}
