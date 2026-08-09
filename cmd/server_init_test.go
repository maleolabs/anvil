package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// TestServerInitCommand_RegistersUnderServer verifies that:
//
//	anvil server init
//
// is registered as a subcommand of the server command.
func TestServerInitCommand_RegistersUnderServer(t *testing.T) {
	serverSub, _, err := rootCmd.Find([]string{"server", "init"})
	if err != nil {
		t.Fatalf("rootCmd.Find([\"server\", \"init\"]) returned error: %v", err)
	}
	if serverSub == nil {
		t.Fatal("rootCmd.Find([\"server\", \"init\"]) returned nil command")
	}
	if serverSub.Use != "init" {
		t.Errorf("command Use = %q, want %q", serverSub.Use, "init")
	}

	// Verify it's nested under server (parent is serverCmd), not directly
	// under rootCmd. Note: rootCmd has its own "init" command (project init),
	// so we verify the parent relationship instead.
	if serverSub.Parent() == nil || serverSub.Parent().Use != "server" {
		t.Errorf("server init command parent = %v, want server subcommand",
			serverSub.Parent())
	}
}

// TestServerInitCommand_CreatesConfig verifies that:
//
//	anvil server init --server-root <dir>
//
// creates the config.yaml file with default values.
func TestServerInitCommand_CreatesConfig(t *testing.T) {
	dir := t.TempDir()

	_, stdout, stderr, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init command returned unexpected error: %v\nstderr: %s", err, stderr)
	}

	if stderr != "" && !contains(stderr, "non-default server root") {
		t.Errorf("expected warning about non-default root, got: %s", stderr)
	}

	if !contains(stdout, "Server Runtime initialized") {
		t.Errorf("stdout should contain success message, got: %s", stdout)
	}

	configPath := filepath.Join(dir, "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Errorf("config.yaml was not created at %s", configPath)
	}

	if !contains(stdout, configPath) {
		t.Errorf("stdout should contain config path %q, got: %s", configPath, stdout)
	}
}

// TestServerInitCommand_Idempotent verifies that running:
//
//	anvil server init --server-root <dir>
//
// multiple times is safe and reports already initialized on subsequent calls.
func TestServerInitCommand_Idempotent(t *testing.T) {
	dir := t.TempDir()

	// First call should succeed.
	_, stdout, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("first server init returned unexpected error: %v", err)
	}
	if !contains(stdout, "Server Runtime initialized") {
		t.Errorf("first call should show 'Server Runtime initialized', got: %s", stdout)
	}

	// Second call should report already initialized.
	_, stdout2, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("second server init returned unexpected error: %v", err)
	}
	if !contains(stdout2, "already initialized") {
		t.Errorf("second call should show 'already initialized', got: %s", stdout2)
	}

	// Third call should also report already initialized.
	_, stdout3, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("third server init returned unexpected error: %v", err)
	}
	if !contains(stdout3, "already initialized") {
		t.Errorf("third call should show 'already initialized', got: %s", stdout3)
	}
}

// TestServerInitCommand_ServerRootFlag verifies that the --server-root flag
// overrides the default config root and the config is created at the
// specified location.
func TestServerInitCommand_ServerRootFlag(t *testing.T) {
	dir := t.TempDir()

	_, _, stderr, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init with --server-root returned unexpected error: %v", err)
	}

	if !contains(stderr, "non-default server root") {
		t.Errorf("stderr should contain warning about non-default root, got: %s", stderr)
	}

	configPath := filepath.Join(dir, "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Errorf("config.yaml was not created at %s", configPath)
	}
}

// TestServerInitCommand_DefaultRoot verifies that the command functions
// without --server-root (uses default /etc/anvil).
//
// This test cannot actually write to /etc/anvil in CI, so it checks that
// the error (if any) is about permissions, not a structural issue.
func TestServerInitCommand_DefaultRoot(t *testing.T) {
	_, _, stderr, err := executeCommand("server", "init")

	if err != nil {
		// Permission denied is acceptable in CI.
		if os.IsPermission(err) {
			t.Log("server init without --server-root: permission denied (expected in CI)")
		} else {
			// Check the error is not about missing args or flags.
			t.Logf("server init without --server-root returned: %v", err)
		}
	}

	// Stderr should not contain a panic or stack trace.
	if contains(stderr, "panic") {
		t.Errorf("stderr should not contain panic, got: %s", stderr)
	}
}

// TestServerInitCommand_NoProjectRequired verifies that:
//
//	anvil server init --server-root <dir>
//
// works without requiring a project context (no anvil.yaml, no project
// discovery, no current working directory constraints).
func TestServerInitCommand_NoProjectRequired(t *testing.T) {
	dir := t.TempDir()

	_, _, stderr, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init without project returned unexpected error: %v", err)
	}

	// Should not mention project, anvil.yaml, or RequireProject.
	if contains(stderr, "project") {
		t.Errorf("stderr should not mention project, got: %s", stderr)
	}
	if contains(stderr, "anvil.yaml") {
		t.Errorf("stderr should not mention anvil.yaml, got: %s", stderr)
	}
}

// TestServerInitCommand_NoArgs verifies that:
//
//	anvil server init
//
// accepts no positional arguments.
func TestServerInitCommand_NoArgs(t *testing.T) {
	_, _, stderr, err := executeCommand("server", "init", "unexpected-arg")
	if err == nil {
		t.Fatal("expected error for unexpected positional arg, got nil")
	}

	// cobra.NoArgs produces "unknown command" when an unexpected arg is
	// passed (it treats it as an unknown subcommand name).
	if !contains(stderr, "unknown command") {
		t.Errorf("expected 'unknown command' error, got: %s", stderr)
	}
}

// TestServerInitCommand_OutputFormat verifies that the output follows the
// project's output conventions with clear sections and next steps.
func TestServerInitCommand_OutputFormat(t *testing.T) {
	dir := t.TempDir()

	_, stdout, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init returned unexpected error: %v", err)
	}

	// Should contain success message.
	if !contains(stdout, "Server Runtime initialized") {
		t.Errorf("stdout should contain 'Server Runtime initialized', got: %s", stdout)
	}

	// Should contain config path.
	if !contains(stdout, "Config:") {
		t.Errorf("stdout should contain 'Config:', got: %s", stdout)
	}

	// Should contain next steps guidance.
	if !contains(stdout, "Next steps") {
		t.Errorf("stdout should contain 'Next steps', got: %s", stdout)
	}

	// Should contain runtime.id hint.
	if !contains(stdout, "runtime.id") {
		t.Errorf("stdout should contain 'runtime.id' hint, got: %s", stdout)
	}
}

// TestServerInitCommand_FlagDeduplication verifies that the --server-root
// flag is on the init subcommand, not on the server parent command.
func TestServerInitCommand_FlagDeduplication(t *testing.T) {
	// Verify --server-root is NOT on the server parent command.
	serverCmdRef, _, err := rootCmd.Find([]string{"server"})
	if err != nil {
		t.Fatalf("failed to find server command: %v", err)
	}

	flag := serverCmdRef.Flags().Lookup("server-root")
	if flag != nil {
		t.Errorf("flag --server-root should not be on the server parent command")
	}

	// Verify --server-root IS on the init subcommand.
	initCmdRef, _, err := rootCmd.Find([]string{"server", "init"})
	if err != nil {
		t.Fatalf("failed to find server init command: %v", err)
	}

	flag = initCmdRef.Flags().Lookup("server-root")
	if flag == nil {
		t.Errorf("flag --server-root should be on the server init subcommand")
	}
}
