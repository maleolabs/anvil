// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P10-02, TS-P11-05, ST-P11-01, ADR-015, EPIC-010, EPIC-011,
// Decision 006, TD-006
package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/deployment"
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
//	anvil deployment upload <target-id> <artifact-path>
//
// validates the artifact path exists on disk — on a fresh runner with
// no local server state (TD-006).
func TestDeploymentUploadCommand_ValidatesArtifactPath(t *testing.T) {
	// Run upload with a non-existent artifact path. No server init is
	// performed: upload must not depend on local server state (TD-006).
	_, _, stderr, err := executeCommand("deployment", "upload", "my-target", "/nonexistent/artifact.tar.gz")
	if err == nil {
		t.Fatal("expected error for non-existent artifact, got nil")
	}

	if !contains(stderr, "not found") {
		t.Errorf("stderr should report artifact not found, got: %s", stderr)
	}
}

// TestDeploymentUploadCommand_ValidatesEmptyArtifact verifies that:
//
//	anvil deployment upload <target-id> <artifact-path>
//
// validates the artifact has content (non-empty).
func TestDeploymentUploadCommand_ValidatesEmptyArtifact(t *testing.T) {
	dir := t.TempDir()

	// Create an empty artifact file.
	emptyArtifact := filepath.Join(dir, "empty-artifact.tar.gz")
	if err := os.WriteFile(emptyArtifact, []byte{}, 0644); err != nil {
		t.Fatalf("failed to create empty artifact: %v", err)
	}

	_, _, stderr, err := executeCommand("deployment", "upload", "my-target", emptyArtifact)
	if err == nil {
		t.Fatal("expected error for empty artifact, got nil")
	}

	if !contains(stderr, "empty") {
		t.Errorf("stderr should report artifact is empty, got: %s", stderr)
	}
}

// TestDeploymentUploadCommand_FreshRunner_EnvOnly verifies that:
//
//	anvil deployment upload <target-id> <artifact-path>
//
// delivers the artifact on a fresh runner with env-only configuration:
// no local server initialization, no --server-root, and no local server
// config anywhere (TD-006 validation checklist: CI-environment scenario).
//
// This is the regression test for the TD-006 fix: previously upload
// required RequireServerInitialized + resolveServerRoot + server config
// for target identity, which made the CI POV (MVP-002 §3.7) impossible
// on a fresh runner. Now the target identity comes from the SSH
// credential environment (DEPLOY_SERVER_*) plus the <target-id>
// correlation label.
func TestDeploymentUploadCommand_FreshRunner_EnvOnly(t *testing.T) {
	// Deliberately no server init: this test proves upload works without
	// any local server state (TD-006).

	// Start an in-process SSH server and configure credentials.
	keyPath, publicKey := writeUploadTestKey(t)
	server := newUploadTestServer(t, []ssh.PublicKey{publicKey})

	// A valid packaged artifact (upload requires a readable manifest,
	// TD-011).
	artifactPath, _ := packageUploadTestArtifact(t)

	setUploadEnv(t, "127.0.0.1", server.Port(), "testuser", keyPath)
	t.Cleanup(func() {
		_ = os.Remove(path.Join("/tmp/anvil-uploads", path.Base(artifactPath)))
	})

	_, stdout, stderr, err := executeCommand("deployment", "upload", "prod-node", artifactPath)
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
	if !contains(stdout, path.Join("/tmp/anvil-uploads", path.Base(artifactPath))) {
		t.Errorf("stdout should contain the remote path, got: %s", stdout)
	}
	// The success hint must point at server-side verification commands
	// (not 'deployment info', which only reports target identity; TD-006
	// review).
	if !contains(stdout, "anvil server release status") || !contains(stdout, "anvil server status") {
		t.Errorf("stdout should point at server-side status commands, got: %s", stdout)
	}

	// Verify the artifact was actually transferred to the target.
	received, err := os.ReadFile(path.Join("/tmp/anvil-uploads", path.Base(artifactPath)))
	if err != nil {
		t.Fatalf("artifact not received by target: %v", err)
	}
	sent, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("cannot read local artifact: %v", err)
	}
	if string(received) != string(sent) {
		t.Errorf("received artifact mismatch: got %q, want %q", received, sent)
	}
}

// TestDeploymentUploadCommand_Success verifies that:
//
//	anvil deployment upload <target-id> <artifact-path>
//
// delivers the artifact to the SSH target and reports the delivery
// result with the remote path (ST-P11-01 AC-1, AC-6). The <target-id>
// is echoed as a correlation label in the output.
func TestDeploymentUploadCommand_Success(t *testing.T) {
	// Start an in-process SSH server and configure credentials.
	keyPath, publicKey := writeUploadTestKey(t)
	server := newUploadTestServer(t, []ssh.PublicKey{publicKey})

	// A valid packaged artifact (upload requires a readable manifest,
	// TD-011). The artifact basename must be unique so parallel-free
	// test runs never collide in the shared default remote dir.
	artifactPath, _ := packageUploadTestArtifact(t)

	setUploadEnv(t, "127.0.0.1", server.Port(), "testuser", keyPath)
	t.Cleanup(func() {
		_ = os.Remove(path.Join("/tmp/anvil-uploads", path.Base(artifactPath)))
	})

	_, stdout, stderr, err := executeCommand("deployment", "upload", "prod-node", artifactPath)
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
	if !contains(stdout, path.Join("/tmp/anvil-uploads", path.Base(artifactPath))) {
		t.Errorf("stdout should contain the remote path, got: %s", stdout)
	}

	// Verify the artifact was actually transferred to the target.
	received, err := os.ReadFile(path.Join("/tmp/anvil-uploads", path.Base(artifactPath)))
	if err != nil {
		t.Fatalf("artifact not received by target: %v", err)
	}
	sent, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("cannot read local artifact: %v", err)
	}
	if string(received) != string(sent) {
		t.Errorf("received artifact mismatch: got %q, want %q", received, sent)
	}
}

// TestDeploymentUploadCommand_JSONOutput verifies that:
//
//	anvil deployment upload <target-id> <artifact-path> --json
//
// produces valid JSON output including the remote path (ST-P11-01 AC-7)
// and the delivered artifact path (BUG-011).
func TestDeploymentUploadCommand_JSONOutput(t *testing.T) {
	// Start an in-process SSH server and configure credentials.
	keyPath, publicKey := writeUploadTestKey(t)
	server := newUploadTestServer(t, []ssh.PublicKey{publicKey})

	// A valid packaged artifact (upload requires a readable manifest,
	// TD-011).
	artifactPath, _ := packageUploadTestArtifact(t)

	setUploadEnv(t, "127.0.0.1", server.Port(), "testuser", keyPath)
	t.Cleanup(func() {
		_ = os.Remove(path.Join("/tmp/anvil-uploads", path.Base(artifactPath)))
	})

	_, stdout, stderr, err := executeCommand("deployment", "upload", "prod-node", artifactPath, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, `"target_id"`) {
		t.Errorf("stdout should contain JSON field 'target_id', got: %s", stdout)
	}
	if !contains(stdout, `"artifact"`) {
		t.Errorf("stdout should contain JSON field 'artifact', got: %s", stdout)
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
	if !contains(stdout, path.Join("/tmp/anvil-uploads", path.Base(artifactPath))) {
		t.Errorf("stdout should contain the remote path in JSON, got: %s", stdout)
	}
	// The artifact field must carry the delivered artifact path (BUG-011),
	// matching the human-readable output.
	if !contains(stdout, `"artifact": "`+artifactPath+`"`) {
		t.Errorf("stdout should contain the artifact path in the 'artifact' field, got: %s", stdout)
	}
}

// TestOutputUploadJSON_IncludesArtifactPath verifies that outputUploadJSON
// populates the artifact field with the delivered artifact path (BUG-011).
//
// Regression test for the placeholder handoff: the field was hardcoded to
// "" with a comment "set by caller after delivery", and no caller set it,
// so machine-readable output always omitted the artifact path.
func TestOutputUploadJSON_IncludesArtifactPath(t *testing.T) {
	cmd := rootCmd
	buf := new(strings.Builder)
	cmd.SetOut(buf)

	result := &deployment.TransportResult{
		TargetID:   "prod-node",
		Success:    true,
		RemotePath: "/srv/anvil/artifacts/my-artifact.tar.gz",
	}
	artifactPath := "path/to/my-artifact.tar.gz"

	if err := outputUploadJSON(cmd, result, artifactPath); err != nil {
		t.Fatalf("outputUploadJSON returned error: %v", err)
	}

	var out uploadJSONOutput
	if err := json.Unmarshal([]byte(buf.String()), &out); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, buf.String())
	}
	if out.Artifact != artifactPath {
		t.Errorf("Artifact = %q, want %q", out.Artifact, artifactPath)
	}
	if out.TargetID != string(result.TargetID) {
		t.Errorf("TargetID = %q, want %q", out.TargetID, string(result.TargetID))
	}
	if out.Status != "delivered" {
		t.Errorf("Status = %q, want %q", out.Status, "delivered")
	}
	if out.RemotePath != result.RemotePath {
		t.Errorf("RemotePath = %q, want %q", out.RemotePath, result.RemotePath)
	}
}

// packageUploadTestArtifact packages a valid Anvil artifact for upload
// tests, returning the artifact path and its embedded manifest.
func packageUploadTestArtifact(t *testing.T) (string, *artifact.Manifest) {
	t.Helper()
	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "index.php"), []byte("<?php\n"), 0644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	pkgResult, err := artifact.Package(artifact.PackageOptions{
		SourceDir: sourceDir,
		OutputDir: t.TempDir(),
		Version:   "1.0.0",
		Source:    "test-project",
		ProjectID: "test-project",
	})
	if err != nil {
		t.Fatalf("package artifact: %v", err)
	}
	return pkgResult.ArtifactPath, pkgResult.Manifest
}

// TestBuildUploadPayload_PopulatesManifest verifies that the upload
// payload carries the artifact manifest (TD-011): buildUploadPayload
// reads the manifest from the artifact and embeds it as
// ArtifactPayload.ManifestContent, completing the ADR-017 transport
// contract at the upload boundary.
func TestBuildUploadPayload_PopulatesManifest(t *testing.T) {
	artifactPath, pkgManifest := packageUploadTestArtifact(t)

	payload, err := buildUploadPayload(artifactPath)
	if err != nil {
		t.Fatalf("buildUploadPayload returned error: %v", err)
	}

	if payload.Path != artifactPath {
		t.Errorf("payload.Path = %q, want %q", payload.Path, artifactPath)
	}
	if len(payload.ManifestContent) == 0 {
		t.Fatal("payload.ManifestContent must not be empty (TD-011)")
	}

	// Byte-level assertion (TD-011 review): the payload must carry
	// exactly the manifest bytes produced from the packaged artifact.
	wantBytes, err := artifact.MarshalManifest(*pkgManifest)
	if err != nil {
		t.Fatalf("MarshalManifest failed: %v", err)
	}
	if !bytes.Equal(payload.ManifestContent, wantBytes) {
		t.Errorf("payload.ManifestContent = %s, want %s", payload.ManifestContent, wantBytes)
	}

	var manifest artifact.Manifest
	if err := json.Unmarshal(payload.ManifestContent, &manifest); err != nil {
		t.Fatalf("payload.ManifestContent is not valid manifest JSON: %v", err)
	}
	if manifest.ArtifactID != pkgManifest.ArtifactID {
		t.Errorf("manifest ArtifactID = %q, want %q", manifest.ArtifactID, pkgManifest.ArtifactID)
	}
	if manifest.ProjectID != pkgManifest.ProjectID {
		t.Errorf("manifest ProjectID = %q, want %q", manifest.ProjectID, pkgManifest.ProjectID)
	}
	if manifest.Version != pkgManifest.Version {
		t.Errorf("manifest Version = %q, want %q", manifest.Version, pkgManifest.Version)
	}
}

// TestBuildUploadPayload_InvalidArtifact verifies that buildUploadPayload
// returns an error when the file is not a valid Anvil package, so the
// upload never ships a payload with an unreadable manifest (TD-011).
func TestBuildUploadPayload_InvalidArtifact(t *testing.T) {
	rawArtifact := filepath.Join(t.TempDir(), "raw-artifact.tar.gz")
	if err := os.WriteFile(rawArtifact, []byte("not an anvil package"), 0644); err != nil {
		t.Fatalf("failed to create raw artifact: %v", err)
	}

	if _, err := buildUploadPayload(rawArtifact); err == nil {
		t.Fatal("expected error for non-package artifact, got nil")
	}
}

// TestDeploymentUploadCommand_InvalidArtifact verifies that:
//
//	anvil deployment upload <target-id> <artifact-path>
//
// reports an actionable error when the artifact is not a valid Anvil
// package whose manifest cannot be read (TD-011).
func TestDeploymentUploadCommand_InvalidArtifact(t *testing.T) {
	// A non-empty file that is not a valid Anvil package: it passes the
	// existence and content checks but fails manifest reading.
	rawArtifact := filepath.Join(t.TempDir(), "raw-artifact.tar.gz")
	if err := os.WriteFile(rawArtifact, []byte("not an anvil package"), 0644); err != nil {
		t.Fatalf("failed to create raw artifact: %v", err)
	}

	keyPath, _ := writeUploadTestKey(t)
	setUploadEnv(t, "127.0.0.1", 22, "testuser", keyPath)

	_, _, stderr, err := executeCommand("deployment", "upload", "my-target", rawArtifact)
	if err == nil {
		t.Fatal("expected error for invalid artifact, got nil")
	}
	if !contains(stderr, "could not read artifact manifest") {
		t.Errorf("stderr should report manifest read failure, got: %s", stderr)
	}
	if !contains(stderr, "anvil artifact package") {
		t.Errorf("stderr should include the packaging resolution, got: %s", stderr)
	}
}

// TestDeploymentUploadCommand_ArtifactRoundTripPreservesManifest verifies
// that the artifact file — with its embedded manifest — survives the SSH
// transport intact (TD-011).
//
// This is a file-level round-trip: SSHTransport.Deliver transmits
// payload.Path via SCP (it does not send ManifestContent separately), so
// the manifest read from the received file proves the transported file
// still carries its manifest. The payload-level ManifestContent is
// asserted byte-for-byte in TestBuildUploadPayload_PopulatesManifest.
func TestDeploymentUploadCommand_ArtifactRoundTripPreservesManifest(t *testing.T) {
	keyPath, publicKey := writeUploadTestKey(t)
	server := newUploadTestServer(t, []ssh.PublicKey{publicKey})

	artifactPath, pkgManifest := packageUploadTestArtifact(t)

	setUploadEnv(t, "127.0.0.1", server.Port(), "testuser", keyPath)
	t.Cleanup(func() {
		_ = os.Remove(path.Join("/tmp/anvil-uploads", path.Base(artifactPath)))
	})

	_, _, stderr, err := executeCommand("deployment", "upload", "prod-node", artifactPath)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	received := path.Join("/tmp/anvil-uploads", path.Base(artifactPath))
	manifest, err := artifact.ReadManifest(received)
	if err != nil {
		t.Fatalf("read manifest from received artifact: %v", err)
	}
	if manifest.ArtifactID != pkgManifest.ArtifactID {
		t.Errorf("received ArtifactID = %q, want %q", manifest.ArtifactID, pkgManifest.ArtifactID)
	}
	if manifest.ProjectID != pkgManifest.ProjectID {
		t.Errorf("received ProjectID = %q, want %q", manifest.ProjectID, pkgManifest.ProjectID)
	}
	if manifest.Version != pkgManifest.Version {
		t.Errorf("received Version = %q, want %q", manifest.Version, pkgManifest.Version)
	}
}

// TestBuildUploadPayload_ZipArtifact verifies that a zip-format artifact
// — a valid Anvil package produced by 'anvil artifact package' — fails
// with errArtifactZipFormat so the command can emit a format-specific
// resolution instead of claiming the artifact was never packaged
// (TD-011 review).
func TestBuildUploadPayload_ZipArtifact(t *testing.T) {
	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "index.php"), []byte("<?php\n"), 0644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	pkgResult, err := artifact.Package(artifact.PackageOptions{
		SourceDir: sourceDir,
		OutputDir: t.TempDir(),
		Version:   "1.0.0",
		Source:    "test-project",
		ProjectID: "test-project",
		Formats:   []string{"zip"},
	})
	if err != nil {
		t.Fatalf("package artifact: %v", err)
	}

	if _, err := buildUploadPayload(pkgResult.ArtifactPath); !errors.Is(err, errArtifactZipFormat) {
		t.Fatalf("expected errArtifactZipFormat, got: %v", err)
	}
}

// TestDeploymentUploadCommand_ZipArtifact verifies that uploading a
// zip-format artifact produces a format-specific resolution naming the
// .tar.gz requirement (TD-011 review).
func TestDeploymentUploadCommand_ZipArtifact(t *testing.T) {
	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "index.php"), []byte("<?php\n"), 0644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	pkgResult, err := artifact.Package(artifact.PackageOptions{
		SourceDir: sourceDir,
		OutputDir: t.TempDir(),
		Version:   "1.0.0",
		Source:    "test-project",
		ProjectID: "test-project",
		Formats:   []string{"zip"},
	})
	if err != nil {
		t.Fatalf("package artifact: %v", err)
	}

	keyPath, _ := writeUploadTestKey(t)
	setUploadEnv(t, "127.0.0.1", 22, "testuser", keyPath)

	_, _, stderr, err := executeCommand("deployment", "upload", "my-target", pkgResult.ArtifactPath)
	if err == nil {
		t.Fatal("expected error for zip artifact, got nil")
	}
	if !contains(stderr, ".tar.gz") {
		t.Errorf("stderr should name the .tar.gz requirement, got: %s", stderr)
	}
	if !contains(stderr, "zip") {
		t.Errorf("stderr should name the zip format, got: %s", stderr)
	}
}

// TestDeploymentUploadCommand_MissingCredentials verifies that missing
// SSH credential environment variables produce a clear error naming
// the required variables (ST-P11-01 AC-3).
func TestDeploymentUploadCommand_MissingCredentials(t *testing.T) {
	artifactPath := filepath.Join(t.TempDir(), "artifact.tar.gz")
	if err := os.WriteFile(artifactPath, []byte("artifact content"), 0644); err != nil {
		t.Fatalf("failed to create artifact: %v", err)
	}

	unsetUploadEnv(t)

	_, _, stderr, err := executeCommand("deployment", "upload", "my-target", artifactPath)
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
	artifactPath := filepath.Join(t.TempDir(), "artifact.tar.gz")
	if err := os.WriteFile(artifactPath, []byte("artifact content"), 0644); err != nil {
		t.Fatalf("failed to create artifact: %v", err)
	}

	keyPath, _ := writeUploadTestKey(t)
	setUploadEnv(t, "127.0.0.1", 0, "testuser", keyPath)
	// Override the port with an invalid value (restore handled by
	// setUploadEnv cleanup for previous values).
	_ = os.Setenv(envDeployServerPort, "not-a-port")
	t.Cleanup(func() { _ = os.Unsetenv(envDeployServerPort) })

	_, _, stderr, err := executeCommand("deployment", "upload", "my-target", artifactPath)
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
	// A valid artifact that passes the manifest check (TD-011) so the
	// test exercises the transport failure path.
	artifactPath, _ := packageUploadTestArtifact(t)

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

	_, _, stderr, err := executeCommand("deployment", "upload", "my-target", artifactPath)
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
	// A valid artifact that passes the manifest check (TD-011) so the
	// test exercises the transport failure path.
	artifactPath, _ := packageUploadTestArtifact(t)

	// The server authorizes no keys, so authentication must fail.
	keyPath, _ := writeUploadTestKey(t)
	server := newUploadTestServer(t, nil)
	setUploadEnv(t, "127.0.0.1", server.Port(), "testuser", keyPath)

	_, _, stderr, err := executeCommand("deployment", "upload", "my-target", artifactPath)
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
	// A valid artifact that passes the manifest check (TD-011) so the
	// test exercises the transport failure path.
	artifactPath, _ := packageUploadTestArtifact(t)

	keyPath, publicKey := writeUploadTestKey(t)
	server := newUploadTestServer(t, []ssh.PublicKey{publicKey})
	server.denySCP()
	setUploadEnv(t, "127.0.0.1", server.Port(), "testuser", keyPath)

	_, _, stderr, err := executeCommand("deployment", "upload", "my-target", artifactPath)
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

// TestDeploymentUploadCommand_HasNoServerRootFlag verifies that the
// --server-root flag is NOT available on the upload command: upload is
// decoupled from local server state and must not offer server-root
// overrides (TD-006, PM decision option a).
func TestDeploymentUploadCommand_HasNoServerRootFlag(t *testing.T) {
	uploadCmd, _, err := rootCmd.Find([]string{"deployment", "upload"})
	if err != nil {
		t.Fatalf("failed to find deployment upload command: %v", err)
	}
	flag := uploadCmd.Flags().Lookup("server-root")
	if flag != nil {
		t.Errorf("flag --server-root should NOT be on the deployment upload subcommand (TD-006)")
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

// TestDeploymentUploadCommand_ArtifactCheckBeforeCredentialCheck verifies
// that:
//
//	anvil deployment upload <target-id> <artifact-path>
//
// validates the artifact path BEFORE reading SSH credentials. This is the
// expected order: artifact validation first, then environment-based
// configuration. On a fresh runner (no server init, no credentials), a
// missing artifact must report the artifact error, not a credential error
// (TD-006).
func TestDeploymentUploadCommand_ArtifactCheckBeforeCredentialCheck(t *testing.T) {
	// Do NOT initialize the server and do NOT set credentials: upload
	// must not depend on local server state (TD-006).
	_, _, stderr, err := executeCommand("deployment", "upload", "my-target", "/nonexistent/artifact.tar.gz")
	if err == nil {
		t.Fatal("expected error for non-existent artifact, got nil")
	}

	if !contains(stderr, "not found") {
		t.Errorf("stderr should report artifact not found (not init/credential error), got: %s", stderr)
	}
}

// TestDeploymentUploadCommand_HelpListsEnvVars verifies that
// "anvil deployment upload --help" documents the SSH transport
// environment variables (ST-011-004 AC-5, TD-004) and the TD-006
// contract: env-only configuration (no local server state) and the
// <target-id> correlation label.
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
		"DEPLOY_SSH_KNOWN_HOSTS",
		"DEPLOY_SSH_KNOWN_HOSTS_MODE",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help output should document %q, got:\n%s", want, stdout)
		}
	}

	// TD-006 contract: help must state that upload works without local
	// server state (fresh runner, env-only) and that <target-id> is a
	// correlation label, not a selector.
	for _, want := range []string{
		"correlation label",
		"fresh runner",
		"env-only",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help output should document the TD-006 contract (%q), got:\n%s", want, stdout)
		}
	}
}
