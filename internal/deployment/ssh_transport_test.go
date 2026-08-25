// Package deployment defines the Deployment bounded context for Anvil.
//
// Reference: TS-P11-01, TS-011-001, EPIC-011, ADR-015
package deployment

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// TestSSHTransport_InterfaceSatisfaction verifies that SSHTransport
// satisfies the Transport interface (compile-time check).
//
// Reference: TS-011-001 AC-1
func TestSSHTransport_InterfaceSatisfaction(t *testing.T) {
	var _ Transport = (*SSHTransport)(nil)
}

// TestSSHTransport_ConstructorDefaults verifies the constructor applies
// the documented defaults for zero-valued optional settings.
//
// Reference: EPIC-011 §7.2, §7.3
func TestSSHTransport_ConstructorDefaults(t *testing.T) {
	transport := NewSSHTransport("10.0.0.5", "deploy", "/keys/id_ed25519", 0)

	if transport.Host != "10.0.0.5" {
		t.Errorf("Host = %q, want %q", transport.Host, "10.0.0.5")
	}
	if transport.User != "deploy" {
		t.Errorf("User = %q, want %q", transport.User, "deploy")
	}
	if transport.KeyPath != "/keys/id_ed25519" {
		t.Errorf("KeyPath = %q, want %q", transport.KeyPath, "/keys/id_ed25519")
	}
	if transport.Port != 22 {
		t.Errorf("Port = %d, want default 22", transport.Port)
	}
	if transport.Timeout != 10*time.Second {
		t.Errorf("Timeout = %v, want default 10s", transport.Timeout)
	}
	if transport.RemoteDir != "/tmp/anvil-uploads" {
		t.Errorf("RemoteDir = %q, want default /tmp/anvil-uploads", transport.RemoteDir)
	}
}

// TestSSHTransport_ConstructorOptions verifies that functional options
// override the defaults and preserve explicitly provided credentials.
//
// Reference: EPIC-011 §7.2
func TestSSHTransport_ConstructorOptions(t *testing.T) {
	transport := NewSSHTransport("h", "u", "k", 2222,
		WithTimeout(3*time.Second), WithRemoteDir("/srv/uploads"))

	if transport.Port != 2222 {
		t.Errorf("Port = %d, want 2222", transport.Port)
	}
	if transport.Timeout != 3*time.Second {
		t.Errorf("Timeout = %v, want 3s", transport.Timeout)
	}
	if transport.RemoteDir != "/srv/uploads" {
		t.Errorf("RemoteDir = %q, want /srv/uploads", transport.RemoteDir)
	}
}

// TestSSHTransport_Deliver_Success verifies end-to-end delivery: the
// artifact is transferred through the in-process SSH server, the
// received file content matches the payload, and the result reports
// Success, the target ID, and the expected remote path.
//
// Reference: TS-011-001 AC-2, AC-3, AC-4, EPIC-011 §7.5
func TestSSHTransport_Deliver_Success(t *testing.T) {
	keyPath, publicKey := writeTestKey(t)
	server := newSSHTestServer(t, []ssh.PublicKey{publicKey})

	remoteDir := filepath.Join(t.TempDir(), "remote-uploads")
	payloadPath, content := writeTestPayload(t)

	transport := NewSSHTransport(
		"127.0.0.1",
		"testuser",
		keyPath,
		server.Port(),
		WithTimeout(5*time.Second),
		WithRemoteDir(remoteDir),
	)
	target := &testTarget{id: TargetID("node-1")}

	result, err := transport.Deliver(ArtifactPayload{Path: payloadPath}, target)
	if err != nil {
		t.Fatalf("Deliver() returned error: %v", err)
	}
	if !result.Success {
		t.Error("TransportResult.Success = false, want true")
	}
	if result.TargetID != target.ID() {
		t.Errorf("TransportResult.TargetID = %q, want %q", result.TargetID, target.ID())
	}

	wantRemotePath := path.Join(remoteDir, "artifact.tar.gz")
	if result.RemotePath != wantRemotePath {
		t.Errorf("TransportResult.RemotePath = %q, want %q", result.RemotePath, wantRemotePath)
	}

	received, err := os.ReadFile(wantRemotePath)
	if err != nil {
		t.Fatalf("read received artifact: %v", err)
	}
	if !bytes.Equal(received, content) {
		t.Errorf("received artifact mismatch: got %d bytes, want %d bytes", len(received), len(content))
	}
}

// TestSSHTransport_Deliver_AuthenticationFailure verifies that an
// unauthorized key yields a non-recoverable TransportError classified
// as KindAuthenticationFailed whose reason names the authentication
// failure.
//
// Reference: TS-011-001 AC-5, TS-011-004 AC-2, EPIC-011 §7.6
func TestSSHTransport_Deliver_AuthenticationFailure(t *testing.T) {
	keyPath, _ := writeTestKey(t)
	// The server authorizes no keys, so the handshake must fail.
	server := newSSHTestServer(t, nil)
	payloadPath, _ := writeTestPayload(t)

	transport := NewSSHTransport(
		"127.0.0.1", "testuser", keyPath, server.Port(),
		WithTimeout(5*time.Second),
	)

	_, err := transport.Deliver(ArtifactPayload{Path: payloadPath}, &testTarget{id: TargetID("node-1")})
	transportErr := assertTransportError(t, err, false, "authent")
	if transportErr.Kind != KindAuthenticationFailed {
		t.Errorf("TransportError.Kind = %q, want %q", transportErr.Kind, KindAuthenticationFailed)
	}
	if got := transportErr.Guidance(); got == "" {
		t.Error("Guidance() = empty, want actionable guidance for authentication failure")
	}
}

// TestSSHTransport_Deliver_ConnectionRefused verifies that an
// unreachable host yields a recoverable TransportError classified as
// KindConnectionRefused with an actionable reason.
//
// Reference: TS-011-001 AC-4, TS-011-004 AC-1, EPIC-011 §7.6
func TestSSHTransport_Deliver_ConnectionRefused(t *testing.T) {
	keyPath, _ := writeTestKey(t)
	payloadPath, _ := writeTestPayload(t)

	// Port 1 is not bound by the test process; dialing it fails fast
	// with connection refused.
	transport := NewSSHTransport(
		"127.0.0.1", "testuser", keyPath, 1,
		WithTimeout(5*time.Second),
	)

	_, err := transport.Deliver(ArtifactPayload{Path: payloadPath}, &testTarget{id: TargetID("node-1")})
	transportErr := assertTransportError(t, err, true, "connection refused")
	if transportErr.Kind != KindConnectionRefused {
		t.Errorf("TransportError.Kind = %q, want %q", transportErr.Kind, KindConnectionRefused)
	}
	if got := transportErr.Guidance(); got == "" {
		t.Error("Guidance() = empty, want actionable guidance for connection refused")
	}
}

// TestSSHTransport_Deliver_MissingKeyFile verifies that a missing key
// file yields a non-recoverable TransportError classified as a local
// configuration problem instead of an authentication failure.
//
// Reference: TS-011-001 AC-5, TS-011-004
func TestSSHTransport_Deliver_MissingKeyFile(t *testing.T) {
	payloadPath, _ := writeTestPayload(t)
	missingKey := filepath.Join(t.TempDir(), "does-not-exist")

	transport := NewSSHTransport(
		"127.0.0.1", "testuser", missingKey, 22,
		WithTimeout(2*time.Second),
	)

	_, err := transport.Deliver(ArtifactPayload{Path: payloadPath}, &testTarget{id: TargetID("node-1")})
	transportErr := assertTransportError(t, err, false, "private key")
	if transportErr.Kind != KindConfiguration {
		t.Errorf("TransportError.Kind = %q, want %q", transportErr.Kind, KindConfiguration)
	}
}

// TestSSHTransport_Deliver_InvalidKeyFile verifies that an unparsable
// key file yields a non-recoverable TransportError classified as a
// local configuration problem.
//
// Reference: TS-011-001 AC-5, TS-011-004
func TestSSHTransport_Deliver_InvalidKeyFile(t *testing.T) {
	payloadPath, _ := writeTestPayload(t)
	badKey := filepath.Join(t.TempDir(), "bad_key")
	if err := os.WriteFile(badKey, []byte("not a private key"), 0o600); err != nil {
		t.Fatalf("write bad key file: %v", err)
	}

	transport := NewSSHTransport(
		"127.0.0.1", "testuser", badKey, 22,
		WithTimeout(2*time.Second),
	)

	_, err := transport.Deliver(ArtifactPayload{Path: payloadPath}, &testTarget{id: TargetID("node-1")})
	transportErr := assertTransportError(t, err, false, "parse")
	if transportErr.Kind != KindConfiguration {
		t.Errorf("TransportError.Kind = %q, want %q", transportErr.Kind, KindConfiguration)
	}
}

// TestSSHTransport_Deliver_TransferFailure verifies that a remote scp
// failure yields a recoverable TransportError classified as
// KindTransferFailed whose reason includes the remote scp stderr
// output.
//
// Reference: TS-011-001 AC-5, TS-011-004 AC-4, EPIC-011 §7.6
func TestSSHTransport_Deliver_TransferFailure(t *testing.T) {
	keyPath, publicKey := writeTestKey(t)
	server := newSSHTestServer(t, []ssh.PublicKey{publicKey})
	payloadPath, _ := writeTestPayload(t)

	// A directory occupying the artifact's target name: mkdir -p of the
	// remote dir succeeds, but the remote scp cannot write through it.
	remoteDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(remoteDir, "artifact.tar.gz"), 0o755); err != nil {
		t.Fatalf("create blocking directory: %v", err)
	}

	transport := NewSSHTransport(
		"127.0.0.1", "testuser", keyPath, server.Port(),
		WithTimeout(5*time.Second),
		WithRemoteDir(remoteDir),
	)

	_, err := transport.Deliver(ArtifactPayload{Path: payloadPath}, &testTarget{id: TargetID("node-1")})
	transportErr := assertTransportError(t, err, true, "scp")
	if transportErr.Kind != KindTransferFailed {
		t.Errorf("TransportError.Kind = %q, want %q", transportErr.Kind, KindTransferFailed)
	}
	if got := transportErr.Guidance(); got == "" {
		t.Error("Guidance() = empty, want actionable guidance for transfer failure")
	}
}

// TestSSHTransport_Deliver_PermissionDenied verifies that a remote
// permission denial yields a recoverable TransportError classified as
// KindPermissionDenied whose reason includes the remote "Permission
// denied" message.
//
// Reference: TS-011-004 AC-3, EPIC-011 §7.6
func TestSSHTransport_Deliver_PermissionDenied(t *testing.T) {
	keyPath, publicKey := writeTestKey(t)
	server := newSSHTestServer(t, []ssh.PublicKey{publicKey})
	server.denySCP()
	payloadPath, _ := writeTestPayload(t)

	transport := NewSSHTransport(
		"127.0.0.1", "testuser", keyPath, server.Port(),
		WithTimeout(5*time.Second),
	)

	_, err := transport.Deliver(ArtifactPayload{Path: payloadPath}, &testTarget{id: TargetID("node-1")})
	transportErr := assertTransportError(t, err, true, "permission denied")
	if transportErr.Kind != KindPermissionDenied {
		t.Errorf("TransportError.Kind = %q, want %q", transportErr.Kind, KindPermissionDenied)
	}
	if got := transportErr.Guidance(); got == "" {
		t.Error("Guidance() = empty, want actionable guidance for permission denied")
	}
}

// TestSSHTransport_Deliver_MissingPayloadFile verifies that a missing
// artifact file yields a non-recoverable TransportError naming the
// artifact path.
//
// Reference: TS-011-001 AC-5
func TestSSHTransport_Deliver_MissingPayloadFile(t *testing.T) {
	keyPath, _ := writeTestKey(t)
	missingPayload := filepath.Join(t.TempDir(), "does-not-exist.tar.gz")

	transport := NewSSHTransport(
		"127.0.0.1", "testuser", keyPath, 22,
		WithTimeout(2*time.Second),
	)

	_, err := transport.Deliver(ArtifactPayload{Path: missingPayload}, &testTarget{id: TargetID("node-1")})
	assertTransportError(t, err, false, missingPayload)
}

// TestSSHTransport_Deliver_UnsafeBasename verifies that an artifact
// basename containing shell metacharacters is rejected before any
// network I/O: the error is a non-recoverable validation error and the
// SSH server never receives a connection.
//
// Reference: TS-011-001 AC-5, defense-in-depth on remote shell commands
func TestSSHTransport_Deliver_UnsafeBasename(t *testing.T) {
	keyPath, publicKey := writeTestKey(t)
	server := newSSHTestServer(t, []ssh.PublicKey{publicKey})

	// The payload file exists, so the only possible failure is the
	// basename validation. The basename is path.Base'd from the local
	// path, so any slash in the name would be stripped — a semicolon
	// and spaces survive path.Base and must be rejected.
	payloadPath := filepath.Join(t.TempDir(), "foo;rm -rf x")
	if err := os.WriteFile(payloadPath, []byte("payload"), 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	transport := NewSSHTransport(
		"127.0.0.1", "testuser", keyPath, server.Port(),
		WithTimeout(5*time.Second),
	)

	_, err := transport.Deliver(ArtifactPayload{Path: payloadPath}, &testTarget{id: TargetID("node-1")})
	transportErr := assertTransportError(t, err, false, "basename")
	if !strings.Contains(strings.ToLower(transportErr.Reason), "foo;rm") {
		t.Errorf("TransportError.Reason = %q, want it to name the rejected basename", transportErr.Reason)
	}
	if server.Connections() != 0 {
		t.Errorf("server received %d connection(s), want 0: validation must abort before dialing", server.Connections())
	}
}

// TestSSHTransport_Deliver_UnsafeRemoteDir verifies that a RemoteDir
// containing shell metacharacters is rejected before any network I/O,
// with a non-recoverable TransportError.
//
// Reference: TS-011-001 AC-5, defense-in-depth on remote shell commands
func TestSSHTransport_Deliver_UnsafeRemoteDir(t *testing.T) {
	keyPath, publicKey := writeTestKey(t)
	server := newSSHTestServer(t, []ssh.PublicKey{publicKey})
	payloadPath, _ := writeTestPayload(t)

	transport := NewSSHTransport(
		"127.0.0.1", "testuser", keyPath, server.Port(),
		WithTimeout(5*time.Second),
		WithRemoteDir("/tmp/x;rm -rf /"),
	)

	_, err := transport.Deliver(ArtifactPayload{Path: payloadPath}, &testTarget{id: TargetID("node-1")})
	assertTransportError(t, err, false, "remote path")
	if server.Connections() != 0 {
		t.Errorf("server received %d connection(s), want 0: validation must abort before dialing", server.Connections())
	}
}

// TestSSHTransport_Deliver_EmptyConfigInputs verifies that empty
// configuration inputs produce clear non-recoverable TransportErrors
// before any file or network I/O.
//
// Reference: TS-011-001 AC-5
func TestSSHTransport_Deliver_EmptyConfigInputs(t *testing.T) {
	tests := []struct {
		name       string
		host       string
		user       string
		keyPath    string
		payload    string
		wantReason string
	}{
		{name: "empty_host", wantReason: "host is empty"},
		{name: "empty_user", host: "127.0.0.1", wantReason: "user is empty"},
		{name: "empty_key_path", host: "127.0.0.1", user: "testuser", wantReason: "key path is empty"},
		{name: "empty_payload_path", host: "127.0.0.1", user: "testuser", keyPath: "/keys/id", wantReason: "artifact path is empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := NewSSHTransport(tt.host, tt.user, tt.keyPath, 22, WithTimeout(2*time.Second))
			_, err := transport.Deliver(ArtifactPayload{Path: tt.payload}, &testTarget{id: TargetID("node-1")})
			assertTransportError(t, err, false, tt.wantReason)
		})
	}
}

// TestSSHTransport_Deliver_KnownHostsStrict_Verified verifies that a
// delivery succeeds end-to-end when the server's host key is pinned in
// the configured known_hosts file (TD-004).
func TestSSHTransport_Deliver_KnownHostsStrict_Verified(t *testing.T) {
	keyPath, publicKey := writeTestKey(t)
	server := newSSHTestServer(t, []ssh.PublicKey{publicKey})
	remoteDir := filepath.Join(t.TempDir(), "remote-uploads")
	payloadPath, content := writeTestPayload(t)

	knownHosts := writeKnownHosts(t, server.Addr(), server.HostKey())

	transport := NewSSHTransport(
		"127.0.0.1", "testuser", keyPath, server.Port(),
		WithTimeout(5*time.Second),
		WithRemoteDir(remoteDir),
		WithKnownHosts(knownHosts, KnownHostsModeStrict),
	)

	result, err := transport.Deliver(ArtifactPayload{Path: payloadPath}, &testTarget{id: TargetID("node-1")})
	if err != nil {
		t.Fatalf("Deliver() returned error: %v", err)
	}
	if !result.Success {
		t.Error("TransportResult.Success = false, want true")
	}

	received, err := os.ReadFile(path.Join(remoteDir, "artifact.tar.gz"))
	if err != nil {
		t.Fatalf("read received artifact: %v", err)
	}
	if !bytes.Equal(received, content) {
		t.Errorf("received artifact mismatch: got %d bytes, want %d bytes", len(received), len(content))
	}
}

// TestSSHTransport_Deliver_KnownHostsStrict_UnknownHost verifies the
// MITM scenario of an unknown host: with an empty known_hosts file the
// delivery fails closed with a non-recoverable
// KindHostKeyVerificationFailed error (TD-004).
func TestSSHTransport_Deliver_KnownHostsStrict_UnknownHost(t *testing.T) {
	keyPath, publicKey := writeTestKey(t)
	server := newSSHTestServer(t, []ssh.PublicKey{publicKey})
	payloadPath, _ := writeTestPayload(t)

	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(knownHosts, nil, 0o600); err != nil {
		t.Fatalf("write empty known_hosts: %v", err)
	}

	transport := NewSSHTransport(
		"127.0.0.1", "testuser", keyPath, server.Port(),
		WithTimeout(5*time.Second),
		WithKnownHosts(knownHosts, KnownHostsModeStrict),
	)

	_, err := transport.Deliver(ArtifactPayload{Path: payloadPath}, &testTarget{id: TargetID("node-1")})
	assertHostKeyVerificationError(t, err, "key is unknown")
}

// TestSSHTransport_Deliver_KnownHostsStrict_ChangedKey verifies the
// MITM scenario of a changed host key: the known_hosts file pins a
// different key to the server's address, and the delivery fails closed
// with a non-recoverable KindHostKeyVerificationFailed error (TD-004).
func TestSSHTransport_Deliver_KnownHostsStrict_ChangedKey(t *testing.T) {
	keyPath, publicKey := writeTestKey(t)
	server := newSSHTestServer(t, []ssh.PublicKey{publicKey})
	payloadPath, _ := writeTestPayload(t)

	// The known-hosts file pins a different key to the same address:
	// the offered server key mismatches, which signals a possible MITM.
	knownHosts := writeKnownHosts(t, server.Addr(), newTestHostKey(t))

	transport := NewSSHTransport(
		"127.0.0.1", "testuser", keyPath, server.Port(),
		WithTimeout(5*time.Second),
		WithKnownHosts(knownHosts, KnownHostsModeStrict),
	)

	_, err := transport.Deliver(ArtifactPayload{Path: payloadPath}, &testTarget{id: TargetID("node-1")})
	assertHostKeyVerificationError(t, err, "key mismatch")
}

// TestSSHTransport_Deliver_KnownHosts_MissingFile verifies that a
// missing known-hosts file in strict mode is a non-recoverable
// configuration error reported before any network I/O (TD-004).
func TestSSHTransport_Deliver_KnownHosts_MissingFile(t *testing.T) {
	keyPath, publicKey := writeTestKey(t)
	server := newSSHTestServer(t, []ssh.PublicKey{publicKey})
	payloadPath, _ := writeTestPayload(t)

	missing := filepath.Join(t.TempDir(), "does-not-exist-known_hosts")

	transport := NewSSHTransport(
		"127.0.0.1", "testuser", keyPath, server.Port(),
		WithTimeout(5*time.Second),
		WithKnownHosts(missing, KnownHostsModeStrict),
	)

	_, err := transport.Deliver(ArtifactPayload{Path: payloadPath}, &testTarget{id: TargetID("node-1")})
	transportErr := assertTransportError(t, err, false, "known-hosts file")
	if transportErr.Kind != KindConfiguration {
		t.Errorf("TransportError.Kind = %q, want %q", transportErr.Kind, KindConfiguration)
	}
	if server.Connections() != 0 {
		t.Errorf("server received %d connection(s), want 0: known-hosts validation must abort before dialing", server.Connections())
	}
}

// TestSSHTransport_Deliver_KnownHosts_InvalidFile verifies that an
// unparsable known-hosts file is a non-recoverable configuration error
// reported before any network I/O (TD-004).
func TestSSHTransport_Deliver_KnownHosts_InvalidFile(t *testing.T) {
	keyPath, publicKey := writeTestKey(t)
	server := newSSHTestServer(t, []ssh.PublicKey{publicKey})
	payloadPath, _ := writeTestPayload(t)

	invalid := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(invalid, []byte("garbage\n"), 0o600); err != nil {
		t.Fatalf("write invalid known_hosts: %v", err)
	}

	transport := NewSSHTransport(
		"127.0.0.1", "testuser", keyPath, server.Port(),
		WithTimeout(5*time.Second),
		WithKnownHosts(invalid, KnownHostsModeStrict),
	)

	_, err := transport.Deliver(ArtifactPayload{Path: payloadPath}, &testTarget{id: TargetID("node-1")})
	transportErr := assertTransportError(t, err, false, "known-hosts file")
	if transportErr.Kind != KindConfiguration {
		t.Errorf("TransportError.Kind = %q, want %q", transportErr.Kind, KindConfiguration)
	}
	if server.Connections() != 0 {
		t.Errorf("server received %d connection(s), want 0: known-hosts validation must abort before dialing", server.Connections())
	}
}

// TestSSHTransport_Deliver_KnownHosts_AcceptNew_RecordsUnknownHost
// verifies accept-new mode: an unknown host key is recorded in the
// known-hosts file on first contact, the delivery succeeds, and a later
// strict verification against the recorded file also succeeds (TD-004).
func TestSSHTransport_Deliver_KnownHosts_AcceptNew_RecordsUnknownHost(t *testing.T) {
	keyPath, publicKey := writeTestKey(t)
	server := newSSHTestServer(t, []ssh.PublicKey{publicKey})
	remoteDir := filepath.Join(t.TempDir(), "remote-uploads")
	payloadPath, content := writeTestPayload(t)

	// The known-hosts file does not exist yet: accept-new creates it.
	knownHosts := filepath.Join(t.TempDir(), "known_hosts")

	transport := NewSSHTransport(
		"127.0.0.1", "testuser", keyPath, server.Port(),
		WithTimeout(5*time.Second),
		WithRemoteDir(remoteDir),
		WithKnownHosts(knownHosts, KnownHostsModeAcceptNew),
	)

	result, err := transport.Deliver(ArtifactPayload{Path: payloadPath}, &testTarget{id: TargetID("node-1")})
	if err != nil {
		t.Fatalf("Deliver() with accept-new returned error: %v", err)
	}
	if !result.Success {
		t.Error("TransportResult.Success = false, want true")
	}

	// The file must now contain the server's key for the dial address.
	wantLine := knownhosts.Line([]string{server.Addr()}, server.HostKey()) + "\n"
	got, err := os.ReadFile(knownHosts)
	if err != nil {
		t.Fatalf("read recorded known_hosts: %v", err)
	}
	if string(got) != wantLine {
		t.Errorf("recorded known_hosts = %q, want %q", got, wantLine)
	}

	// A second delivery with strict verification against the recorded
	// file must succeed: the recorded key proves the server identity.
	strict := NewSSHTransport(
		"127.0.0.1", "testuser", keyPath, server.Port(),
		WithTimeout(5*time.Second),
		WithRemoteDir(remoteDir),
		WithKnownHosts(knownHosts, KnownHostsModeStrict),
	)
	if _, err := strict.Deliver(ArtifactPayload{Path: payloadPath}, &testTarget{id: TargetID("node-1")}); err != nil {
		t.Fatalf("strict Deliver() against recorded key returned error: %v", err)
	}

	received, err := os.ReadFile(path.Join(remoteDir, "artifact.tar.gz"))
	if err != nil {
		t.Fatalf("read received artifact: %v", err)
	}
	if !bytes.Equal(received, content) {
		t.Errorf("received artifact mismatch: got %d bytes, want %d bytes", len(received), len(content))
	}
}

// TestSSHTransport_Deliver_KnownHosts_AcceptNew_ChangedKeyRejected
// verifies that accept-new mode still fails closed on a changed host
// key: the recorded key for the address differs from the server's, and
// the delivery fails with a non-recoverable
// KindHostKeyVerificationFailed error instead of overwriting the
// recorded key (TD-004).
func TestSSHTransport_Deliver_KnownHosts_AcceptNew_ChangedKeyRejected(t *testing.T) {
	keyPath, publicKey := writeTestKey(t)
	server := newSSHTestServer(t, []ssh.PublicKey{publicKey})
	payloadPath, _ := writeTestPayload(t)

	knownHosts := writeKnownHosts(t, server.Addr(), newTestHostKey(t))

	transport := NewSSHTransport(
		"127.0.0.1", "testuser", keyPath, server.Port(),
		WithTimeout(5*time.Second),
		WithKnownHosts(knownHosts, KnownHostsModeAcceptNew),
	)

	_, err := transport.Deliver(ArtifactPayload{Path: payloadPath}, &testTarget{id: TargetID("node-1")})
	assertHostKeyVerificationError(t, err, "key mismatch")
}

// TestSSHTransport_Deliver_NoKnownHosts_AcceptsAnyKey verifies the
// default posture (TD-004): without a configured known-hosts file the
// transport still delivers to a server whose key is recorded nowhere.
// This pins the legacy behavior so existing CI flows keep working.
func TestSSHTransport_Deliver_NoKnownHosts_AcceptsAnyKey(t *testing.T) {
	keyPath, publicKey := writeTestKey(t)
	server := newSSHTestServer(t, []ssh.PublicKey{publicKey})
	remoteDir := filepath.Join(t.TempDir(), "remote-uploads")
	payloadPath, _ := writeTestPayload(t)

	transport := NewSSHTransport(
		"127.0.0.1", "testuser", keyPath, server.Port(),
		WithTimeout(5*time.Second),
		WithRemoteDir(remoteDir),
	)

	result, err := transport.Deliver(ArtifactPayload{Path: payloadPath}, &testTarget{id: TargetID("node-1")})
	if err != nil {
		t.Fatalf("Deliver() returned error: %v", err)
	}
	if !result.Success {
		t.Error("TransportResult.Success = false, want true")
	}
}

// assertTransportError asserts that err is a *TransportError for
// target "node-1" with the expected recoverability, and that its
// reason contains the given substring (case-insensitive).
func assertTransportError(t *testing.T, err error, wantRecoverable bool, reasonSubstr string) *TransportError {
	t.Helper()
	var transportErr *TransportError
	if !errors.As(err, &transportErr) {
		t.Fatalf("expected *TransportError, got %T: %v", err, err)
	}
	if transportErr.TargetID != TargetID("node-1") {
		t.Errorf("TransportError.TargetID = %q, want %q", transportErr.TargetID, TargetID("node-1"))
	}
	if transportErr.Recoverable != wantRecoverable {
		t.Errorf("TransportError.Recoverable = %v, want %v (reason: %q)",
			transportErr.Recoverable, wantRecoverable, transportErr.Reason)
	}
	if reasonSubstr != "" && !strings.Contains(strings.ToLower(transportErr.Reason), strings.ToLower(reasonSubstr)) {
		t.Errorf("TransportError.Reason = %q, want it to mention %q", transportErr.Reason, reasonSubstr)
	}
	return transportErr
}

// assertHostKeyVerificationError asserts that err is a non-recoverable
// *TransportError classified as KindHostKeyVerificationFailed whose
// reason names the verification failure detail (TD-004).
func assertHostKeyVerificationError(t *testing.T, err error, detail string) *TransportError {
	t.Helper()
	transportErr := assertTransportError(t, err, false, "host key verification")
	if transportErr.Kind != KindHostKeyVerificationFailed {
		t.Errorf("TransportError.Kind = %q, want %q", transportErr.Kind, KindHostKeyVerificationFailed)
	}
	if !strings.Contains(strings.ToLower(transportErr.Reason), strings.ToLower(detail)) {
		t.Errorf("TransportError.Reason = %q, want it to mention %q", transportErr.Reason, detail)
	}
	if got := transportErr.Guidance(); got == "" {
		t.Error("Guidance() = empty, want actionable guidance for host key verification failure")
	}
	return transportErr
}

// writeKnownHosts writes an OpenSSH known_hosts file containing a
// single entry for the given dial address and host key, and returns the
// file path (TD-004).
func writeKnownHosts(t *testing.T, addr string, key ssh.PublicKey) string {
	t.Helper()
	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(knownHosts, []byte(knownhosts.Line([]string{addr}, key)+"\n"), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}
	return knownHosts
}

// newTestHostKey generates an ephemeral host key and returns its public
// key, for building known_hosts files that must NOT match a test
// server (TD-004).
func newTestHostKey(t *testing.T) ssh.PublicKey {
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

// writeTestKey generates an ephemeral ed25519 key pair, writes the
// private key in OpenSSH PEM format to a temp file, and returns the
// file path and the corresponding public key.
func writeTestKey(t *testing.T) (string, ssh.PublicKey) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("create ssh signer: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(privateKey, "anvil-test-key")
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	keyPath := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}
	return keyPath, signer.PublicKey()
}

// writeTestPayload creates an artifact file in a temp dir and returns
// its path and content.
func writeTestPayload(t *testing.T) (string, []byte) {
	t.Helper()
	content := bytes.Repeat([]byte("anvil-ssh-payload-"), 16384) // ~180 KiB
	payloadPath := filepath.Join(t.TempDir(), "artifact.tar.gz")
	if err := os.WriteFile(payloadPath, content, 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	return payloadPath, content
}

// sshTestServer is a minimal in-process SSH server used to exercise
// SSHTransport without an external SSH daemon. It accepts public-key
// authentication only and executes two commands against the local
// filesystem: "mkdir -p <dir>" (creates the directory) and
// "scp -t <path>" (receives an SCP stream and writes it to <path>).
// Because the server runs in-process, the client's "remote" paths map
// directly to local paths, which makes received-file verification
// trivial.
//
// Reference: TS-P11-01 test strategy
type sshTestServer struct {
	t        *testing.T
	listener net.Listener
	config   *ssh.ServerConfig
	hostKey  ssh.Signer
	// conns counts TCP connections that reached the server; used to
	// assert that validation failures happen before any dial attempt.
	conns atomic.Int32
	// scpDenied makes receiveSCP fail with "Permission denied",
	// simulating a remote authorization/filesystem denial.
	scpDenied atomic.Bool
	// failNextSCP makes the next SCP transfer fail mid-stream (partial write)
	// to test atomic retry (AC2): the next receiveSCP consumes header then returns
	// error without creating final artifact, then resets.
	failNextSCP atomic.Bool
}

// newSSHTestServer starts an SSH server on 127.0.0.1 with a random
// port. Only the given public keys are authorized; an empty slice
// authorizes no keys at all. The listener is closed on test cleanup.
func newSSHTestServer(t *testing.T, authorizedKeys []ssh.PublicKey) *sshTestServer {
	t.Helper()
	server := &sshTestServer{t: t}

	config := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			for _, authorized := range authorizedKeys {
				if bytes.Equal(authorized.Marshal(), key.Marshal()) {
					return nil, nil
				}
			}
			return nil, fmt.Errorf("public key for %q is not authorized", conn.User())
		},
	}

	_, hostKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostKey)
	if err != nil {
		t.Fatalf("create host signer: %v", err)
	}
	config.AddHostKey(hostSigner)
	server.config = config
	server.hostKey = hostSigner

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server.listener = listener
	t.Cleanup(func() { _ = listener.Close() })

	go server.serve()
	return server
}

// Port returns the TCP port the server listens on.
func (s *sshTestServer) Port() int {
	_, port, err := net.SplitHostPort(s.listener.Addr().String())
	if err != nil {
		s.t.Fatalf("split host port: %v", err)
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		s.t.Fatalf("parse port: %v", err)
	}
	return n
}

// Connections returns how many TCP connections have reached the
// server. Tests use this to prove that validation failures abort
// before any dial attempt.
func (s *sshTestServer) Connections() int32 {
	return s.conns.Load()
}

// HostKey returns the server's host public key, for building
// known_hosts files in tests (TD-004).
func (s *sshTestServer) HostKey() ssh.PublicKey {
	return s.hostKey.PublicKey()
}

// Addr returns the dial address of the server (host:port).
func (s *sshTestServer) Addr() string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(s.Port()))
}

// denySCP makes subsequent SCP transfers fail with "Permission
// denied", simulating a remote authorization or filesystem denial.
func (s *sshTestServer) denySCP() {
	s.scpDenied.Store(true)
}

// serve accepts connections until the listener is closed.
func (s *sshTestServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

// handleConn performs the SSH handshake and dispatches channels.
func (s *sshTestServer) handleConn(conn net.Conn) {
	defer conn.Close()
	s.conns.Add(1)
	sshConn, channels, requests, err := ssh.NewServerConn(conn, s.config)
	if err != nil {
		s.t.Logf("handshake rejected: %v", err)
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(requests)

	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "only session channels are supported")
			continue
		}
		channel, channelRequests, err := newChannel.Accept()
		if err != nil {
			s.t.Logf("accept channel: %v", err)
			return
		}
		go s.handleSession(channel, channelRequests)
	}
}

// handleSession handles a single session channel: it executes the
// requested command and reports the exit status.
func (s *sshTestServer) handleSession(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer channel.Close()
	for request := range requests {
		switch request.Type {
		case "exec":
			var payload struct{ Command string }
			if err := ssh.Unmarshal(request.Payload, &payload); err != nil {
				s.t.Logf("unmarshal exec request: %v", err)
				request.Reply(false, nil)
				continue
			}
			request.Reply(true, nil)
			s.runCommand(channel, payload.Command)
			return
		case "shell":
			request.Reply(false, nil)
			return
		default:
			if request.WantReply {
				request.Reply(false, nil)
			}
		}
	}
}

// runCommand dispatches the supported server commands and reports the
// exit status. Atomic transport uses additional commands:
//   mv -f <src> <dst>  — atomic rename for tmp.<rand> -> final (AC2)
//   rm -f <path>        — best-effort tmp cleanup
func (s *sshTestServer) runCommand(channel ssh.Channel, command string) {
	switch {
	case strings.HasPrefix(command, "mkdir -p "):
		dir := strings.TrimPrefix(command, "mkdir -p ")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(channel.Stderr(), "mkdir: %v\n", err)
			sendExitStatus(channel, 1)
			return
		}
		sendExitStatus(channel, 0)
	case strings.HasPrefix(command, "scp -t "):
		target := strings.TrimPrefix(command, "scp -t ")
		if err := s.receiveSCP(channel, target); err != nil {
			s.t.Logf("scp receive failed: %v", err)
			fmt.Fprintf(channel.Stderr(), "scp: %v\n", err)
			sendExitStatus(channel, 1)
			return
		}
		// fsync the written file to simulate atomic durability (AC2)
		if f, err := os.Open(target); err == nil {
			_ = f.Sync()
			f.Close()
		}
		sendExitStatus(channel, 0)
	case strings.HasPrefix(command, "mv -f "):
		rest := strings.TrimPrefix(command, "mv -f ")
		parts := strings.Fields(rest)
		if len(parts) != 2 {
			fmt.Fprintf(channel.Stderr(), "mv: invalid args %q\n", command)
			sendExitStatus(channel, 1)
			return
		}
		src, dst := parts[0], parts[1]
		if err := os.Rename(src, dst); err != nil {
			fmt.Fprintf(channel.Stderr(), "mv: %v\n", err)
			sendExitStatus(channel, 1)
			return
		}
		sendExitStatus(channel, 0)
	case strings.HasPrefix(command, "rm -f "):
		target := strings.TrimPrefix(command, "rm -f ")
		_ = os.Remove(strings.TrimSpace(target))
		sendExitStatus(channel, 0)
	default:
		s.t.Logf("unsupported command %q", command)
		sendExitStatus(channel, 1)
	}
}

// receiveSCP implements the SCP sink side of the protocol: it reads
// the "C0644 <size> <basename>" header, acknowledges it, receives the
// content and the terminating 0x00, acknowledges, and writes the
// content to target.
func (s *sshTestServer) receiveSCP(channel ssh.Channel, target string) error {
	reader := bufio.NewReader(channel)

	header, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("cannot read scp header: %v", err)
	}
	fields := strings.Fields(header)
	if len(fields) != 3 || !strings.HasPrefix(fields[0], "C") {
		return fmt.Errorf("unexpected scp header %q", header)
	}
	size, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid scp size %q: %v", fields[1], err)
	}
	if _, err := channel.Write([]byte{0}); err != nil {
		return fmt.Errorf("cannot ack scp header: %v", err)
	}

	if s.failNextSCP.Load() {
		s.failNextSCP.Store(false)
		// Simulate mid-transfer disconnect: consume header ack then fail before Copy
		// Do not create final file; leave partial tmp if any (caller will rm -f)
		return fmt.Errorf("simulated mid-transfer disconnect (partial write)")
	}
	if s.scpDenied.Load() {
		return fmt.Errorf("Permission denied")
	}

	file, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("cannot create %q: %v", target, err)
	}
	defer file.Close()

	if _, err := io.CopyN(file, reader, size); err != nil {
		return fmt.Errorf("cannot receive scp content: %v", err)
	}

	terminator := make([]byte, 1)
	if _, err := io.ReadFull(reader, terminator); err != nil {
		return fmt.Errorf("cannot read scp terminator: %v", err)
	}
	if terminator[0] != 0 {
		return fmt.Errorf("invalid scp terminator 0x%02x", terminator[0])
	}
	if _, err := channel.Write([]byte{0}); err != nil {
		return fmt.Errorf("cannot ack scp content: %v", err)
	}
	return nil
}

// sendExitStatus reports the command exit status to the client.
func sendExitStatus(channel ssh.Channel, code uint32) {
	_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{code}))
}
