// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P10-02, TS-P11-05, ST-P11-01, ADR-015, EPIC-010, EPIC-011, Decision 006
package cmd

import (
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
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
// delivers the artifact to the SSH target and reports the delivery
// result with the remote path (ST-P11-01 AC-1, AC-6).
func TestDeploymentUploadCommand_Success(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server first.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// Start an in-process SSH server and configure credentials.
	keyPath, publicKey := writeUploadTestKey(t)
	server := newUploadTestServer(t, []ssh.PublicKey{publicKey})

	// The artifact basename must be unique so parallel-free test runs
	// never collide in the shared default remote dir.
	content := []byte("artifact content for testing")
	artifactPath := filepath.Join(t.TempDir(), "my-artifact.tar.gz")
	if err := os.WriteFile(artifactPath, content, 0644); err != nil {
		t.Fatalf("failed to create artifact: %v", err)
	}

	setUploadEnv(t, "127.0.0.1", server.Port(), "testuser", keyPath)
	t.Cleanup(func() {
		_ = os.Remove(path.Join("/tmp/anvil-uploads", "my-artifact.tar.gz"))
	})

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
	if !contains(stdout, "/tmp/anvil-uploads/my-artifact.tar.gz") {
		t.Errorf("stdout should contain the remote path, got: %s", stdout)
	}

	// Verify the artifact was actually transferred to the target.
	received, err := os.ReadFile("/tmp/anvil-uploads/my-artifact.tar.gz")
	if err != nil {
		t.Fatalf("artifact not received by target: %v", err)
	}
	if string(received) != string(content) {
		t.Errorf("received artifact mismatch: got %q, want %q", received, content)
	}
}

// TestDeploymentUploadCommand_JSONOutput verifies that:
//
//	anvil deployment upload <target-id> <artifact-path> --server-root <dir> --json
//
// produces valid JSON output including the remote path (ST-P11-01 AC-7).
func TestDeploymentUploadCommand_JSONOutput(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server first.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// Start an in-process SSH server and configure credentials.
	keyPath, publicKey := writeUploadTestKey(t)
	server := newUploadTestServer(t, []ssh.PublicKey{publicKey})

	artifactPath := filepath.Join(t.TempDir(), "json-artifact.tar.gz")
	if err := os.WriteFile(artifactPath, []byte("artifact content for JSON test"), 0644); err != nil {
		t.Fatalf("failed to create artifact: %v", err)
	}

	setUploadEnv(t, "127.0.0.1", server.Port(), "testuser", keyPath)
	t.Cleanup(func() {
		_ = os.Remove(path.Join("/tmp/anvil-uploads", "json-artifact.tar.gz"))
	})

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
	if !contains(stdout, `"remote_path"`) {
		t.Errorf("stdout should contain JSON field 'remote_path', got: %s", stdout)
	}
	if !contains(stdout, `/tmp/anvil-uploads/json-artifact.tar.gz`) {
		t.Errorf("stdout should contain the remote path in JSON, got: %s", stdout)
	}
}

// TestDeploymentUploadCommand_MissingCredentials verifies that missing
// SSH credential environment variables produce a clear error naming
// the required variables (ST-P11-01 AC-3).
func TestDeploymentUploadCommand_MissingCredentials(t *testing.T) {
	dir := t.TempDir()

	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	artifactPath := filepath.Join(dir, "artifact.tar.gz")
	if err := os.WriteFile(artifactPath, []byte("artifact content"), 0644); err != nil {
		t.Fatalf("failed to create artifact: %v", err)
	}

	unsetUploadEnv(t)

	_, _, stderr, err := executeCommand("deployment", "upload", "my-target", artifactPath, "--server-root", dir)
	if err == nil {
		t.Fatal("expected error for missing credentials, got nil")
	}

	if !contains(stderr, "DEPLOY_SERVER_HOST") || !contains(stderr, "DEPLOY_SERVER_USER") || !contains(stderr, "DEPLOY_SSH_KEY") {
		t.Errorf("stderr should name all missing variables, got: %s", stderr)
	}
}

// TestDeploymentUploadCommand_InvalidPort verifies that an invalid
// DEPLOY_SERVER_PORT produces a clear error.
func TestDeploymentUploadCommand_InvalidPort(t *testing.T) {
	dir := t.TempDir()

	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	artifactPath := filepath.Join(dir, "artifact.tar.gz")
	if err := os.WriteFile(artifactPath, []byte("artifact content"), 0644); err != nil {
		t.Fatalf("failed to create artifact: %v", err)
	}

	keyPath, _ := writeUploadTestKey(t)
	setUploadEnv(t, "127.0.0.1", 0, "testuser", keyPath)
	// Override the port with an invalid value (restore handled by
	// setUploadEnv cleanup for previous values).
	_ = os.Setenv(envDeployServerPort, "not-a-port")
	t.Cleanup(func() { _ = os.Unsetenv(envDeployServerPort) })

	_, _, stderr, err := executeCommand("deployment", "upload", "my-target", artifactPath, "--server-root", dir)
	if err == nil {
		t.Fatal("expected error for invalid port, got nil")
	}
	if !contains(stderr, "DEPLOY_SERVER_PORT") {
		t.Errorf("stderr should name DEPLOY_SERVER_PORT, got: %s", stderr)
	}
}

// TestDeploymentUploadCommand_ConnectionRefused verifies that an
// unreachable server produces an actionable error message (ST-P11-01
// AC-4).
func TestDeploymentUploadCommand_ConnectionRefused(t *testing.T) {
	dir := t.TempDir()

	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	artifactPath := filepath.Join(dir, "artifact.tar.gz")
	if err := os.WriteFile(artifactPath, []byte("artifact content"), 0644); err != nil {
		t.Fatalf("failed to create artifact: %v", err)
	}

	// Reserve a port and close it: dialing it fails fast with
	// connection refused.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	keyPath, _ := writeUploadTestKey(t)
	setUploadEnv(t, "127.0.0.1", port, "testuser", keyPath)

	_, _, stderr, err := executeCommand("deployment", "upload", "my-target", artifactPath, "--server-root", dir)
	if err == nil {
		t.Fatal("expected error for connection refused, got nil")
	}
	if !contains(stderr, "connection refused") {
		t.Errorf("stderr should report connection refused, got: %s", stderr)
	}
	if !contains(stderr, "Resolution") {
		t.Errorf("stderr should include actionable resolution guidance, got: %s", stderr)
	}
}

// TestDeploymentUploadCommand_AuthenticationFailure verifies that an
// SSH authentication failure produces an actionable error message
// (ST-P11-01 AC-5).
func TestDeploymentUploadCommand_AuthenticationFailure(t *testing.T) {
	dir := t.TempDir()

	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	artifactPath := filepath.Join(dir, "artifact.tar.gz")
	if err := os.WriteFile(artifactPath, []byte("artifact content"), 0644); err != nil {
		t.Fatalf("failed to create artifact: %v", err)
	}

	// The server authorizes no keys, so authentication must fail.
	keyPath, _ := writeUploadTestKey(t)
	server := newUploadTestServer(t, nil)
	setUploadEnv(t, "127.0.0.1", server.Port(), "testuser", keyPath)

	_, _, stderr, err := executeCommand("deployment", "upload", "my-target", artifactPath, "--server-root", dir)
	if err == nil {
		t.Fatal("expected error for authentication failure, got nil")
	}
	if !contains(stderr, "authentication") {
		t.Errorf("stderr should report authentication failure, got: %s", stderr)
	}
	if !contains(stderr, "Resolution") {
		t.Errorf("stderr should include actionable resolution guidance, got: %s", stderr)
	}
}

// TestDeploymentUploadCommand_PermissionDenied verifies that a remote
// permission denial produces an actionable error message.
func TestDeploymentUploadCommand_PermissionDenied(t *testing.T) {
	dir := t.TempDir()

	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	artifactPath := filepath.Join(dir, "artifact.tar.gz")
	if err := os.WriteFile(artifactPath, []byte("artifact content"), 0644); err != nil {
		t.Fatalf("failed to create artifact: %v", err)
	}

	keyPath, publicKey := writeUploadTestKey(t)
	server := newUploadTestServer(t, []ssh.PublicKey{publicKey})
	server.denySCP()
	setUploadEnv(t, "127.0.0.1", server.Port(), "testuser", keyPath)

	_, _, stderr, err := executeCommand("deployment", "upload", "my-target", artifactPath, "--server-root", dir)
	if err == nil {
		t.Fatal("expected error for permission denied, got nil")
	}
	if !contains(stderr, "Permission denied") {
		t.Errorf("stderr should report permission denied, got: %s", stderr)
	}
	if !contains(stderr, "Resolution") {
		t.Errorf("stderr should include actionable resolution guidance, got: %s", stderr)
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

// TestDeploymentUploadCommand_HelpListsEnvVars verifies that
// "anvil deployment upload --help" documents the SSH transport
// environment variables (ST-011-004 AC-5).
//
// AC: `anvil deployment upload --help` shows environment variables.
func TestDeploymentUploadCommand_HelpListsEnvVars(t *testing.T) {
	_, stdout, stderr, err := executeCommand("deployment", "upload", "--help")
	if err != nil {
		t.Fatalf("executeCommand('deployment', 'upload', '--help') returned error: %v", err)
	}
	if stderr != "" {
		t.Errorf("help output produced unexpected stderr: %q", stderr)
	}

	for _, want := range []string{
		"DEPLOY_SERVER_HOST",
		"DEPLOY_SERVER_USER",
		"DEPLOY_SERVER_PORT",
		"DEPLOY_SSH_KEY",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help output should document %q, got:\n%s", want, stdout)
		}
	}
}
