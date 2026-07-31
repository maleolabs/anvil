// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P10-02, ADR-015, EPIC-010, Decision 006
package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDeploymentUploadCommand_RegistersUnderDeployment verifies that:
//
//	anvil deployment upload
//
// is registered as a subcommand of the deployment command.
func TestDeploymentUploadCommand_RegistersUnderDeployment(t *testing.T) {
	sub, _, err := rootCmd.Find([]string{"deployment", "upload"})
	if err != nil {
		t.Fatalf("rootCmd.Find([\"deployment\", \"upload\"]) returned error: %v", err)
	}
	if sub == nil {
		t.Fatal("rootCmd.Find([\"deployment\", \"upload\"]) returned nil command")
	}
	if sub.Use != "upload <target-id> <artifact-path>" {
		t.Errorf("command Use = %q, want %q", sub.Use, "upload <target-id> <artifact-path>")
	}

	// Verify it's nested under deployment (parent is deploymentCmd).
	if sub.Parent() == nil || sub.Parent().Use != "deployment" {
		t.Errorf("upload command parent = %v, want deployment subcommand", sub.Parent())
	}
}

// TestDeploymentUploadCommand_ValidatesArtifactPath verifies that:
//
//	anvil deployment upload <target-id> <artifact-path> --server-root <dir>
//
// validates the artifact path exists on disk.
func TestDeploymentUploadCommand_ValidatesArtifactPath(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server first.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// Run upload with a non-existent artifact path.
	_, _, stderr, err := executeCommand("deployment", "upload", "my-target", "/nonexistent/artifact.tar.gz", "--server-root", dir)
	if err == nil {
		t.Fatal("expected error for non-existent artifact, got nil")
	}

	if !contains(stderr, "not found") {
		t.Errorf("stderr should report artifact not found, got: %s", stderr)
	}
}

// TestDeploymentUploadCommand_ValidatesEmptyArtifact verifies that:
//
//	anvil deployment upload <target-id> <artifact-path> --server-root <dir>
//
// validates the artifact has content (non-empty).
func TestDeploymentUploadCommand_ValidatesEmptyArtifact(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server first.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// Create an empty artifact file.
	emptyArtifact := filepath.Join(dir, "empty-artifact.tar.gz")
	if err := os.WriteFile(emptyArtifact, []byte{}, 0644); err != nil {
		t.Fatalf("failed to create empty artifact: %v", err)
	}

	_, _, stderr, err := executeCommand("deployment", "upload", "my-target", emptyArtifact, "--server-root", dir)
	if err == nil {
		t.Fatal("expected error for empty artifact, got nil")
	}

	if !contains(stderr, "empty") {
		t.Errorf("stderr should report artifact is empty, got: %s", stderr)
	}
}

// TestDeploymentUploadCommand_UninitializedRuntime verifies that:
//
//	anvil deployment upload <target-id> <artifact-path> --server-root <dir>
//
// reports error when the Runtime has not been initialized.
func TestDeploymentUploadCommand_UninitializedRuntime(t *testing.T) {
	dir := t.TempDir()

	// Create a valid artifact file (even though runtime isn't initialized,
	// the artifact check happens first, so we need to pass that check).
	artifactPath := filepath.Join(dir, "artifact.tar.gz")
	if err := os.WriteFile(artifactPath, []byte("artifact content"), 0644); err != nil {
		t.Fatalf("failed to create artifact: %v", err)
	}

	_, stdout, stderr, err := executeCommand("deployment", "upload", "my-target", artifactPath, "--server-root", dir)
	if err == nil {
		t.Fatal("expected error for uninitialized runtime, got nil")
	}

	if !contains(stderr, "not initialized") {
		t.Errorf("stderr should report 'not initialized', got: %s", stderr)
	}
	_ = stdout
}

// TestDeploymentUploadCommand_Success verifies that:
//
//	anvil deployment upload <target-id> <artifact-path> --server-root <dir>
//
// succeeds when the artifact exists and the Runtime is initialized.
func TestDeploymentUploadCommand_Success(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server first.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// Create a valid artifact file.
	artifactPath := filepath.Join(dir, "my-artifact.tar.gz")
	if err := os.WriteFile(artifactPath, []byte("artifact content for testing"), 0644); err != nil {
		t.Fatalf("failed to create artifact: %v", err)
	}

	_, stdout, stderr, err := executeCommand("deployment", "upload", "prod-node", artifactPath, "--server-root", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, "Delivery initiated") {
		t.Errorf("stdout should contain 'Delivery initiated', got: %s", stdout)
	}
	if !contains(stdout, "prod-node") {
		t.Errorf("stdout should contain target ID 'prod-node', got: %s", stdout)
	}
	if !contains(stdout, "delivered") {
		t.Errorf("stdout should contain 'delivered', got: %s", stdout)
	}
}

// TestDeploymentUploadCommand_JSONOutput verifies that:
//
//	anvil deployment upload <target-id> <artifact-path> --server-root <dir> --json
//
// produces valid JSON output.
func TestDeploymentUploadCommand_JSONOutput(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server first.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// Create a valid artifact file.
	artifactPath := filepath.Join(dir, "my-artifact.tar.gz")
	if err := os.WriteFile(artifactPath, []byte("artifact content for JSON test"), 0644); err != nil {
		t.Fatalf("failed to create artifact: %v", err)
	}

	_, stdout, stderr, err := executeCommand("deployment", "upload", "prod-node", artifactPath, "--server-root", dir, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, `"target_id"`) {
		t.Errorf("stdout should contain JSON field 'target_id', got: %s", stdout)
	}
	if !contains(stdout, `"status"`) {
		t.Errorf("stdout should contain JSON field 'status', got: %s", stdout)
	}
	if !contains(stdout, `"prod-node"`) {
		t.Errorf("stdout should contain target ID 'prod-node' in JSON, got: %s", stdout)
	}
}

// TestDeploymentUploadCommand_ServerRootFlag verifies that the --server-root
// flag is available on the upload command.
func TestDeploymentUploadCommand_ServerRootFlag(t *testing.T) {
	uploadCmd, _, err := rootCmd.Find([]string{"deployment", "upload"})
	if err != nil {
		t.Fatalf("failed to find deployment upload command: %v", err)
	}
	flag := uploadCmd.Flags().Lookup("server-root")
	if flag == nil {
		t.Errorf("flag --server-root should be on the deployment upload subcommand")
	}

	// Verify --json flag exists.
	jsonFlag := uploadCmd.Flags().Lookup("json")
	if jsonFlag == nil {
		t.Errorf("flag --json should be on the deployment upload subcommand")
	}
}

// TestDeploymentUploadCommand_ExactArgs verifies that:
//
//	anvil deployment upload
//
// requires exactly two positional arguments.
func TestDeploymentUploadCommand_ExactArgs(t *testing.T) {
	// Test with no args.
	_, _, stderr, err := executeCommand("deployment", "upload")
	if err == nil {
		t.Fatal("expected error for missing args, got nil")
	}
	if !contains(stderr, "requires 2 argument") {
		t.Errorf("stderr should require target-id and artifact-path, got: %s", stderr)
	}

	// Test with one arg.
	_, _, stderr2, err2 := executeCommand("deployment", "upload", "target-1")
	if err2 == nil {
		t.Fatal("expected error for missing artifact-path, got nil")
	}
	if !contains(stderr2, "requires 2 argument") {
		t.Errorf("stderr should require exactly 2 args, got: %s", stderr2)
	}

	// Test with three args.
	_, _, stderr3, err3 := executeCommand("deployment", "upload", "target-1", "path", "extra-arg")
	if err3 == nil {
		t.Fatal("expected error for extra args, got nil")
	}
	if !contains(stderr3, "requires 2 argument") {
		t.Errorf("stderr should require exactly 2 args, got: %s", stderr3)
	}
}

// TestDeploymentUploadCommand_ArtifactCheckBeforeInit verifies that:
//
//	anvil deployment upload --server-root <dir>
//
// validates the artifact path BEFORE checking server initialization.
// This is the expected order: artifact validation first, then server context.
func TestDeploymentUploadCommand_ArtifactCheckBeforeInit(t *testing.T) {
	dir := t.TempDir()

	// Do NOT initialize the server.
	// Trying to upload a non-existent artifact should fail with artifact error,
	// not server initialization error — because artifact validation happens first.
	_, _, stderr, err := executeCommand("deployment", "upload", "my-target", "/nonexistent/artifact.tar.gz", "--server-root", dir)
	if err == nil {
		t.Fatal("expected error for non-existent artifact, got nil")
	}

	if !contains(stderr, "not found") {
		t.Errorf("stderr should report artifact not found (not init error), got: %s", stderr)
	}
}
