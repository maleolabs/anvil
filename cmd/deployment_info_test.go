// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P10-01, ADR-015, EPIC-010
package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDeploymentInfoCommand_RegistersUnderDeployment verifies that:
//
//	anvil deployment info
//
// is registered as a subcommand of the deployment command.
func TestDeploymentInfoCommand_RegistersUnderDeployment(t *testing.T) {
	sub, _, err := rootCmd.Find([]string{"deployment", "info"})
	if err != nil {
		t.Fatalf("rootCmd.Find([\"deployment\", \"info\"]) returned error: %v", err)
	}
	if sub == nil {
		t.Fatal("rootCmd.Find([\"deployment\", \"info\"]) returned nil command")
	}
	if sub.Use != "info <target-id>" {
		t.Errorf("command Use = %q, want %q", sub.Use, "info <target-id>")
	}

	// Verify it's nested under deployment (parent is deploymentCmd).
	if sub.Parent() == nil || sub.Parent().Use != "deployment" {
		t.Errorf("info command parent = %v, want deployment subcommand", sub.Parent())
	}
}

// TestDeploymentInfoCommand_UninitializedRuntime verifies that:
//
//	anvil deployment info <target-id> --server-root <dir>
//
// reports "not initialized" when the Runtime has not been initialized.
func TestDeploymentInfoCommand_UninitializedRuntime(t *testing.T) {
	dir := t.TempDir()

	_, stdout, stderr, err := executeCommand("deployment", "info", "my-target", "--server-root", dir)
	if err == nil {
		t.Fatal("expected error for uninitialized runtime, got nil")
	}

	if !contains(stderr, "not initialized") {
		t.Errorf("stderr should report 'not initialized', got: %s", stderr)
	}
	if !contains(stderr, "anvil server init") {
		t.Errorf("stderr should mention 'anvil server init', got: %s", stderr)
	}
	_ = stdout
}

// TestDeploymentInfoCommand_InitializedRuntime verifies that:
//
//	anvil deployment info <target-id> --server-root <dir>
//
// displays target information after the Runtime has been initialized.
func TestDeploymentInfoCommand_InitializedRuntime(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server first.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	_, stdout, stderr, err := executeCommand("deployment", "info", "my-target", "--server-root", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, "Deployment Target") {
		t.Errorf("stdout should contain 'Deployment Target', got: %s", stdout)
	}
	if !contains(stdout, "my-target") {
		t.Errorf("stdout should contain target ID 'my-target', got: %s", stdout)
	}
	if !contains(stdout, "Deployment information is distinct from Runtime status") {
		t.Errorf("stdout should contain context disclaimer, got: %s", stdout)
	}
}

// TestDeploymentInfoCommand_JSONOutput verifies that:
//
//	anvil deployment info <target-id> --server-root <dir> --json
//
// produces valid JSON output.
func TestDeploymentInfoCommand_JSONOutput(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server first.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	_, stdout, stderr, err := executeCommand("deployment", "info", "my-target", "--server-root", dir, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, `"target_id"`) {
		t.Errorf("stdout should contain JSON field 'target_id', got: %s", stdout)
	}
	if !contains(stdout, `"my-target"`) {
		t.Errorf("stdout should contain target ID 'my-target' in JSON, got: %s", stdout)
	}
	if !contains(stdout, `"delivery_status"`) {
		t.Errorf("stdout should contain JSON field 'delivery_status', got: %s", stdout)
	}
}

// TestDeploymentInfoCommand_WithRuntimeID verifies that:
//
//	anvil deployment info <target-id> --server-root <dir>
//
// displays the runtime ID from config when it has been set.
func TestDeploymentInfoCommand_WithRuntimeID(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server first.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// Set a runtime ID so we can verify it appears in the output.
	_, _, _, err = executeCommand("server", "config", "set", "runtime.id", "test-server-01", "--server-root", dir)
	if err != nil {
		t.Fatalf("config set failed: %v", err)
	}

	_, stdout, stderr, err := executeCommand("deployment", "info", "prod-node", "--server-root", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, "prod-node") {
		t.Errorf("stdout should contain target ID 'prod-node', got: %s", stdout)
	}
	if !contains(stdout, "test-server-01") {
		t.Errorf("stdout should contain runtime ID 'test-server-01', got: %s", stdout)
	}
}

// TestDeploymentInfoCommand_ServerRootFlag verifies that the --server-root flag
// overrides the default config root and the info command uses the specified
// location.
func TestDeploymentInfoCommand_ServerRootFlag(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// Verify --server-root flag exists on this command.
	infoCmd, _, err := rootCmd.Find([]string{"deployment", "info"})
	if err != nil {
		t.Fatalf("failed to find deployment info command: %v", err)
	}
	flag := infoCmd.Flags().Lookup("server-root")
	if flag == nil {
		t.Errorf("flag --server-root should be on the deployment info subcommand")
	}

	// Verify --json flag exists.
	jsonFlag := infoCmd.Flags().Lookup("json")
	if jsonFlag == nil {
		t.Errorf("flag --json should be on the deployment info subcommand")
	}
}

// TestDeploymentInfoCommand_ReadOnly verifies that:
//
//	anvil deployment info --server-root <dir>
//
// does not create any files or modify any state.
func TestDeploymentInfoCommand_ReadOnly(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server first.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// Record config file modification time before info.
	configPath := filepath.Join(dir, "config.yaml")
	configBefore, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("failed to stat config before info: %v", err)
	}

	// Run info command.
	_, _, stderr, err := executeCommand("deployment", "info", "my-target", "--server-root", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	// Verify config file was not modified.
	configAfter, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("failed to stat config after info: %v", err)
	}
	if !configBefore.ModTime().Equal(configAfter.ModTime()) {
		t.Error("config.yaml was modified by read-only info command")
	}
}

// TestDeploymentInfoCommand_ExactArgs verifies that:
//
//	anvil deployment info
//
// requires exactly one positional argument.
func TestDeploymentInfoCommand_ExactArgs(t *testing.T) {
	// Test with no args.
	_, _, stderr, err := executeCommand("deployment", "info")
	if err == nil {
		t.Fatal("expected error for missing args, got nil")
	}
	if !contains(stderr, "requires 1 argument") {
		t.Errorf("stderr should require target-id, got: %s", stderr)
	}

	// Test with too many args.
	_, _, stderr2, err2 := executeCommand("deployment", "info", "target-1", "extra-arg")
	if err2 == nil {
		t.Fatal("expected error for extra args, got nil")
	}
	if !contains(stderr2, "requires 1 argument") {
		t.Errorf("stderr should require exactly 1 arg, got: %s", stderr2)
	}
}
