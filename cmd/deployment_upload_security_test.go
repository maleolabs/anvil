// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-011-003, TS-P11-05, ADR-019, EPIC-011
package cmd

import (
	"os"
	"path"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

// Distinctive sentinel values for SSH credential leakage assertions
// (ST-011-003 AC-4): these substrings must never appear in any command
// output, human or JSON. The host cannot carry a sentinel on the success
// path because it must be a real dialable address (127.0.0.1), so the
// actual host value is asserted instead.
const (
	// securitySentinelUser is the DEPLOY_SERVER_USER value.
	securitySentinelUser = "user-sentinel-9d4e"
	// securitySentinelKey is a distinctive substring of the DEPLOY_SSH_KEY
	// file path value.
	securitySentinelKey = "security-sentinel-7c2a"
	// securitySentinelHost is the DEPLOY_SERVER_HOST value used on the
	// success path (real dialable address).
	securitySentinelHost = "127.0.0.1"
)

// writeUploadTestKeySentinel generates an ephemeral ed25519 key pair and
// places the private key at a path whose name carries a distinctive
// sentinel, so tests can assert that the DEPLOY_SSH_KEY value never
// appears in command output (ST-011-003 AC-4).
func writeUploadTestKeySentinel(t *testing.T) (string, ssh.PublicKey) {
	t.Helper()
	keyPath, publicKey := writeUploadTestKey(t)
	sentinelPath := filepath.Join(filepath.Dir(keyPath), "id_ed25519-"+securitySentinelKey+".pem")
	if err := os.Rename(keyPath, sentinelPath); err != nil {
		t.Fatalf("rename test key to sentinel path: %v", err)
	}
	return sentinelPath, publicKey
}

// assertNoCredentialLeak fails the test if any sentinel credential value
// appears in the captured output (ST-011-003 AC-4).
func assertNoCredentialLeak(t *testing.T, stdout, stderr string) {
	t.Helper()
	for _, sentinel := range []string{
		securitySentinelUser,
		securitySentinelKey,
		securitySentinelHost,
	} {
		if contains(stdout, sentinel) {
			t.Errorf("credential value %q leaked into stdout, got:\n%s", sentinel, stdout)
		}
		if contains(stderr, sentinel) {
			t.Errorf("credential value %q leaked into stderr, got:\n%s", sentinel, stderr)
		}
	}
}

// TestDeploymentUploadCommand_Security_JSONOutput_DoesNotLeakCredentials verifies
// that:
//
//	anvil deployment upload <target-id> <artifact-path> --json
//
// never includes SSH credential values (DEPLOY_SERVER_USER,
// DEPLOY_SSH_KEY, DEPLOY_SERVER_HOST) in the machine-readable output on
// a successful delivery (ST-011-003 AC-4).
func TestDeploymentUploadCommand_Security_JSONOutput_DoesNotLeakCredentials(t *testing.T) {
	keyPath, publicKey := writeUploadTestKeySentinel(t)
	server := newUploadTestServer(t, []ssh.PublicKey{publicKey})

	// A valid packaged artifact (upload requires a readable manifest,
	// TD-011).
	artifactPath, _ := packageUploadTestArtifact(t)

	setUploadEnv(t, securitySentinelHost, server.Port(), securitySentinelUser, keyPath)
	t.Cleanup(func() {
		_ = os.Remove(path.Join("/tmp/anvil-uploads", path.Base(artifactPath)))
	})

	_, stdout, stderr, err := executeCommand("deployment", "upload", "prod-node", artifactPath, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	// Sanity: the transfer actually succeeded and produced JSON output.
	if !contains(stdout, `"status"`) || !contains(stdout, `"delivered"`) {
		t.Errorf("stdout should contain successful JSON delivery, got: %s", stdout)
	}

	assertNoCredentialLeak(t, stdout, stderr)
}

// TestDeploymentUploadCommand_Security_HumanOutput_DoesNotLeakCredentials verifies
// that:
//
//	anvil deployment upload <target-id> <artifact-path>
//
// never includes SSH credential values (DEPLOY_SERVER_USER,
// DEPLOY_SSH_KEY, DEPLOY_SERVER_HOST) in the human-readable output on a
// successful delivery (ST-011-003 AC-4).
func TestDeploymentUploadCommand_Security_HumanOutput_DoesNotLeakCredentials(t *testing.T) {
	keyPath, publicKey := writeUploadTestKeySentinel(t)
	server := newUploadTestServer(t, []ssh.PublicKey{publicKey})

	// A valid packaged artifact (upload requires a readable manifest,
	// TD-011).
	artifactPath, _ := packageUploadTestArtifact(t)

	setUploadEnv(t, securitySentinelHost, server.Port(), securitySentinelUser, keyPath)
	t.Cleanup(func() {
		_ = os.Remove(path.Join("/tmp/anvil-uploads", path.Base(artifactPath)))
	})

	_, stdout, stderr, err := executeCommand("deployment", "upload", "prod-node", artifactPath)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	// Sanity: the transfer actually succeeded.
	if !contains(stdout, "Delivery initiated") || !contains(stdout, "delivered") {
		t.Errorf("stdout should contain successful delivery, got: %s", stdout)
	}

	assertNoCredentialLeak(t, stdout, stderr)
}

// TestDeploymentUploadCommand_Security_MissingCredentialError_DoesNotEchoSetValues
// verifies that the missing-credential error names every missing
// environment variable but never echoes the value of a set variable
// (ST-011-003 AC-4, business rule: credentials must not appear in
// output).
func TestDeploymentUploadCommand_Security_MissingCredentialError_DoesNotEchoSetValues(t *testing.T) {
	artifactPath := filepath.Join(t.TempDir(), "artifact.tar.gz")
	if err := os.WriteFile(artifactPath, []byte("artifact content"), 0644); err != nil {
		t.Fatalf("failed to create artifact: %v", err)
	}

	// DEPLOY_SERVER_USER is set with a distinctive value; the required
	// DEPLOY_SERVER_HOST and DEPLOY_SSH_KEY remain unset.
	unsetUploadEnv(t)
	_ = os.Setenv(envDeployServerUser, securitySentinelUser)
	t.Cleanup(func() { _ = os.Unsetenv(envDeployServerUser) })

	_, stdout, stderr, err := executeCommand("deployment", "upload", "my-target", artifactPath)
	if err == nil {
		t.Fatal("expected error for missing credentials, got nil")
	}

	// The error names every missing variable (ST-P11-01 AC-3)...
	if !contains(stderr, "DEPLOY_SERVER_HOST") {
		t.Errorf("stderr should name DEPLOY_SERVER_HOST, got: %s", stderr)
	}
	if !contains(stderr, "DEPLOY_SSH_KEY") {
		t.Errorf("stderr should name DEPLOY_SSH_KEY, got: %s", stderr)
	}
	// ...but never echoes the value of the set DEPLOY_SERVER_USER.
	if contains(stderr, securitySentinelUser) {
		t.Errorf("set credential value %q leaked into stderr, got: %s", securitySentinelUser, stderr)
	}
	if contains(stdout, securitySentinelUser) {
		t.Errorf("set credential value %q leaked into stdout, got: %s", securitySentinelUser, stdout)
	}
}
