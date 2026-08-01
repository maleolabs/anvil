// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P9-05
package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/runtime"
	"maleolabs.com/anvil/internal/server"
)

// TestSystemHealthCommand_RegistersUnderSystem verifies that the health
// command is registered as a subcommand of the system command.
//
// Reference: ST-P9-05
func TestSystemHealthCommand_RegistersUnderSystem(t *testing.T) {
	systemSub, _, err := rootCmd.Find([]string{"system", "health"})
	if err != nil {
		t.Fatalf("rootCmd.Find([\"system\", \"health\"]) returned error: %v", err)
	}
	if systemSub == nil {
		t.Fatal("rootCmd.Find([\"system\", \"health\"]) returned nil command")
	}
	if systemSub.Use != "health" {
		t.Errorf("command Use = %q, want %q", systemSub.Use, "health")
	}

	// Verify it's nested under system (not directly under root).
	_, _, err = rootCmd.Find([]string{"health"})
	if err == nil {
		t.Error("rootCmd.Find([\"health\"]) should have failed (health is not a direct subcommand)")
	}
}

// TestSystemHealthCommand_JsonFlag verifies that the --json flag is
// registered on the health command.
//
// Reference: ST-P9-05
func TestSystemHealthCommand_JsonFlag(t *testing.T) {
	healthCmd, _, err := rootCmd.Find([]string{"system", "health"})
	if err != nil {
		t.Fatalf("failed to find health command: %v", err)
	}

	flag := healthCmd.Flags().Lookup("json")
	if flag == nil {
		t.Error("--json flag should be on the health subcommand")
	}
}

// TestSystemHealthCommand_ServerRootFlag verifies that the --server-root
// flag is registered on the health command.
//
// Reference: ST-P9-05
func TestSystemHealthCommand_ServerRootFlag(t *testing.T) {
	healthCmd, _, err := rootCmd.Find([]string{"system", "health"})
	if err != nil {
		t.Fatalf("failed to find health command: %v", err)
	}

	flag := healthCmd.Flags().Lookup("server-root")
	if flag == nil {
		t.Error("--server-root flag should be on the health subcommand")
	}
}

// TestSystemHealthCommand_Ready verifies that when all components are
// healthy, the health command outputs READY and exits with code 0.
//
// Reference: ST-P9-05
func TestSystemHealthCommand_Ready(t *testing.T) {
	dir := t.TempDir()
	setupHealthyServerRoot(t, dir)

	_, stdout, stderr, err := executeCommand("system", "health", "--server-root", dir)
	if err != nil {
		t.Fatalf("health command returned unexpected error: %v", err)
	}

	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	if !contains(stdout, "READY") {
		t.Errorf("stdout should contain 'READY', got: %s", stdout)
	}
}

// TestSystemHealthCommand_NotReady verifies that when components are
// unhealthy, the health command outputs NOT READY and exits with code 1.
//
// Reference: ST-P9-05
func TestSystemHealthCommand_NotReady(t *testing.T) {
	dir := t.TempDir()
	// Don't set up anything — everything will fail.

	_, stdout, stderr, err := executeCommand("system", "health", "--server-root", dir)

	// Must return an error (exit code 1).
	if err == nil {
		t.Fatal("expected error (exit code 1) when system is not ready, got nil")
	}

	// stderr should contain the error message.
	if !contains(stderr, "system is not ready") {
		t.Errorf("stderr should contain 'system is not ready', got: %s", stderr)
	}

	// Must contain NOT READY.
	if !contains(stdout, "NOT READY") {
		t.Errorf("stdout should contain 'NOT READY', got: %s", stdout)
	}
}

// TestSystemHealthCommand_JsonOutput verifies that --json flag produces
// valid JSON output.
//
// Reference: ST-P9-05
func TestSystemHealthCommand_JsonOutput(t *testing.T) {
	dir := t.TempDir()
	setupHealthyServerRoot(t, dir)

	_, stdout, _, err := executeCommand("system", "health", "--server-root", dir, "--json")
	if err != nil {
		t.Fatalf("health command returned unexpected error: %v", err)
	}

	// Verify JSON structure.
	if !contains(stdout, "\"ready\"") {
		t.Errorf("JSON output should contain 'ready' field, got: %s", stdout)
	}
	if !contains(stdout, "\"components\"") {
		t.Errorf("JSON output should contain 'components' field, got: %s", stdout)
	}
	if !contains(stdout, "\"summary\"") {
		t.Errorf("JSON output should contain 'summary' field, got: %s", stdout)
	}
}

// TestSystemHealthCommand_JsonNotReady verifies that --json flag works
// when system is not ready.
//
// Reference: ST-P9-05
func TestSystemHealthCommand_JsonNotReady(t *testing.T) {
	dir := t.TempDir()
	// Don't set up anything.

	_, stdout, _, err := executeCommand("system", "health", "--server-root", dir, "--json")

	// Should return error (not ready).
	if err == nil {
		t.Fatal("expected error when system is not ready, got nil")
	}

	// JSON output should still be valid.
	if !contains(stdout, "\"ready\": false") {
		t.Errorf("JSON output should contain ready=false, got: %s", stdout)
	}
}

// TestSystemHealthCommand_ServerRootOverride verifies that --server-root
// flag correctly overrides the default server root.
//
// Reference: ST-P9-05
func TestSystemHealthCommand_ServerRootOverride(t *testing.T) {
	dir := t.TempDir()
	setupHealthyServerRoot(t, dir)

	_, stdout, _, err := executeCommand("system", "health", "--server-root", dir)
	if err != nil {
		t.Fatalf("health command returned unexpected error: %v", err)
	}

	if !contains(stdout, "READY") {
		t.Errorf("stdout should contain 'READY', got: %s", stdout)
	}
}

// TestSystemHealthCommand_ExitCodeReady verifies exit code 0 when ready.
//
// Reference: ST-P9-05
func TestSystemHealthCommand_ExitCodeReady(t *testing.T) {
	dir := t.TempDir()
	setupHealthyServerRoot(t, dir)

	_, _, _, err := executeCommand("system", "health", "--server-root", dir)
	if err != nil {
		t.Errorf("expected nil error (exit 0) when ready, got: %v", err)
	}
}

// TestSystemHealthCommand_ExitCodeNotReady verifies exit code 1 when not ready.
//
// Reference: ST-P9-05
func TestSystemHealthCommand_ExitCodeNotReady(t *testing.T) {
	dir := t.TempDir()
	// No setup.

	_, _, _, err := executeCommand("system", "health", "--server-root", dir)
	if err == nil {
		t.Error("expected non-nil error (exit 1) when not ready, got nil")
	}
}

// TestSystemHealthCommand_OutputContainsComponentNames verifies that all
// component names are present in the output.
//
// Reference: ST-P9-05
func TestSystemHealthCommand_OutputContainsComponentNames(t *testing.T) {
	dir := t.TempDir()
	setupHealthyServerRoot(t, dir)

	_, stdout, _, err := executeCommand("system", "health", "--server-root", dir)
	if err != nil {
		t.Fatalf("health command returned unexpected error: %v", err)
	}

	// All component names should appear.
	expectedComponents := []string{"runtime", "release", "server_readiness", "registry"}
	for _, comp := range expectedComponents {
		if !contains(stdout, comp) {
			t.Errorf("stdout should contain component %q, got: %s", comp, stdout)
		}
	}
}

// TestSystemHealthCommand_SecureExecution verifies that running the
// health command does not create unexpected files.
//
// Reference: ST-P9-05
func TestSystemHealthCommand_SecureExecution(t *testing.T) {
	dir := t.TempDir()

	entriesBefore, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read directory before: %v", err)
	}

	_, _, _, _ = executeCommand("system", "health", "--server-root", dir)

	entriesAfter, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read directory after: %v", err)
	}

	if len(entriesBefore) != len(entriesAfter) {
		t.Errorf("directory contents changed: before=%d entries, after=%d entries",
			len(entriesBefore), len(entriesAfter))
	}
}

// setupHealthyServerRoot creates a minimal healthy server root for testing.
func setupHealthyServerRoot(t *testing.T, dir string) {
	t.Helper()

	// Create runtime directories.
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir
	for _, d := range cfg.AllDirs() {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	// Create a release directory with an artifact.
	releaseDir := filepath.Join(dir, "releases", "release-1")
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(releaseDir, "app.tar.gz"), []byte("data"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Create active symlink.
	if err := os.Symlink(releaseDir, cfg.ActiveSymlinkPath()); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Create config file.
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("test: true"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Create server config.
	store := server.NewConfigStore(dir)
	serverCfg := server.DefaultServerConfig()
	serverCfg.Runtime.ID = "test-runtime"
	if err := store.Save(serverCfg); err != nil {
		t.Fatalf("save server config: %v", err)
	}

	// Create projects directory.
	projectsDir := filepath.Join(dir, "projects")
	if err := os.MkdirAll(projectsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
}
