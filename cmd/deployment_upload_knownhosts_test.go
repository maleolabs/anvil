// Package cmd implements the Anvil CLI commands.
//
// Reference: TS-P11-05, ST-P11-01, EPIC-011, TD-004, TD-006
//
// Upload tests run env-only on a fresh runner (no server init, no
// --server-root): upload is decoupled from local server state (TD-006).
package cmd

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// TestDeploymentUploadCommand_KnownHostsVerification verifies that the
// upload command delivers end-to-end when the server's host key is
// pinned in the known_hosts file configured via DEPLOY_SSH_KNOWN_HOSTS
// (TD-004): verification works when configured.
func TestDeploymentUploadCommand_KnownHostsVerification(t *testing.T) {
	keyPath, publicKey := writeUploadTestKey(t)
	server := newUploadTestServer(t, []ssh.PublicKey{publicKey})
	artifactPath, _ := packageUploadTestArtifact(t)

	setUploadEnv(t, "127.0.0.1", server.Port(), "testuser", keyPath)
	t.Cleanup(func() {
		_ = os.Remove(path.Join("/tmp/anvil-uploads", path.Base(artifactPath)))
	})

	// Pin the server's real host key in the known_hosts file.
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(server.Port()))
	knownHosts := writeUploadKnownHosts(t, addr, server.HostKey())
	t.Setenv(envDeploySSHKnownHosts, knownHosts)
	t.Setenv(envDeploySSHKnownHostsMode, "strict")

	_, stdout, stderr, err := executeCommand("deployment", "upload", "prod-node", artifactPath)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}
	if !contains(stdout, "Delivery initiated") {
		t.Errorf("stdout should contain 'Delivery initiated', got: %s", stdout)
	}
	if !contains(stdout, "delivered") {
		t.Errorf("stdout should contain 'delivered', got: %s", stdout)
	}

	// The artifact was actually transferred to the target.
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

// TestDeploymentUploadCommand_KnownHostsRejectsUnknownHost verifies
// that an unverifiable server identity fails the upload with a clear
// host key verification error when DEPLOY_SSH_KNOWN_HOSTS is configured
// (TD-004): verification fails closed.
func TestDeploymentUploadCommand_KnownHostsRejectsUnknownHost(t *testing.T) {
	keyPath, publicKey := writeUploadTestKey(t)
	server := newUploadTestServer(t, []ssh.PublicKey{publicKey})
	artifactPath, _ := packageUploadTestArtifact(t)

	setUploadEnv(t, "127.0.0.1", server.Port(), "testuser", keyPath)
	t.Cleanup(func() {
		_ = os.Remove(path.Join("/tmp/anvil-uploads", path.Base(artifactPath)))
	})

	// The known_hosts file pins a DIFFERENT key to the server's
	// address: the offered server key does not verify.
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(server.Port()))
	knownHosts := writeUploadKnownHosts(t, addr, newUploadHostKey(t))
	t.Setenv(envDeploySSHKnownHosts, knownHosts)
	t.Setenv(envDeploySSHKnownHostsMode, "strict")

	_, _, stderr, err := executeCommand("deployment", "upload", "prod-node", artifactPath)
	if err == nil {
		t.Fatal("expected error for unverifiable server identity, got nil")
	}
	if !contains(stderr, "host key verification failed") {
		t.Errorf("stderr should report host key verification failure, got: %s", stderr)
	}
	if !contains(stderr, "known_hosts") {
		t.Errorf("stderr should point at the known_hosts configuration, got: %s", stderr)
	}
}

// TestDeploymentUploadCommand_InvalidKnownHostsMode verifies that an
// unsupported DEPLOY_SSH_KNOWN_HOSTS_MODE produces a clear error naming
// the variable (TD-004).
func TestDeploymentUploadCommand_InvalidKnownHostsMode(t *testing.T) {
	keyPath, publicKey := writeUploadTestKey(t)
	server := newUploadTestServer(t, []ssh.PublicKey{publicKey})
	artifactPath, _ := packageUploadTestArtifact(t)

	setUploadEnv(t, "127.0.0.1", server.Port(), "testuser", keyPath)
	t.Cleanup(func() {
		_ = os.Remove(path.Join("/tmp/anvil-uploads", path.Base(artifactPath)))
	})

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(server.Port()))
	knownHosts := writeUploadKnownHosts(t, addr, server.HostKey())
	t.Setenv(envDeploySSHKnownHosts, knownHosts)
	t.Setenv(envDeploySSHKnownHostsMode, "bogus")

	_, _, stderr, err := executeCommand("deployment", "upload", "prod-node", artifactPath)
	if err == nil {
		t.Fatal("expected error for invalid DEPLOY_SSH_KNOWN_HOSTS_MODE, got nil")
	}
	if !contains(stderr, envDeploySSHKnownHostsMode) {
		t.Errorf("stderr should name %s, got: %s", envDeploySSHKnownHostsMode, stderr)
	}
}

// writeUploadKnownHosts writes an OpenSSH known_hosts file containing a
// single entry for the given dial address and host key, and returns the
// file path (TD-004).
func writeUploadKnownHosts(t *testing.T, addr string, key ssh.PublicKey) string {
	t.Helper()
	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(knownHosts, []byte(knownhosts.Line([]string{addr}, key)+"\n"), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}
	return knownHosts
}

// newUploadHostKey generates an ephemeral host key and returns its
// public key, for building known_hosts files that must NOT match a test
// server (TD-004).
func newUploadHostKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("create ssh signer: %v", err)
	}
	return signer.PublicKey()
}
