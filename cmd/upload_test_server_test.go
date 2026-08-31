// Package cmd implements the Anvil CLI commands.
//
// Reference: TS-P11-05, ST-P11-01, EPIC-011
package cmd

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// uploadTestServer is a minimal in-process SSH server used to exercise
// the "anvil deployment upload" command end-to-end without an external
// SSH daemon. It accepts public-key authentication only and executes
// "mkdir -p <dir>" and "scp -t <path>" against the local filesystem,
// mirroring the harness in internal/deployment/ssh_transport_test.go.
//
// Reference: TS-P11-05, ST-P11-01 test strategy
type uploadTestServer struct {
	t         *testing.T
	listener  net.Listener
	config    *ssh.ServerConfig
	hostKey   ssh.Signer
	scpDenied bool
}

// newUploadTestServer starts an SSH server on 127.0.0.1 with a random
// port. Only the given public keys are authorized; an empty slice
// authorizes no keys at all. The listener is closed on test cleanup.
func newUploadTestServer(t *testing.T, authorizedKeys []ssh.PublicKey) *uploadTestServer {
	t.Helper()
	server := &uploadTestServer{t: t}

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
func (s *uploadTestServer) Port() int {
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

// denySCP makes subsequent SCP transfers fail with "Permission
// denied", simulating a remote authorization/filesystem denial.
func (s *uploadTestServer) denySCP() {
	s.scpDenied = true
}

// HostKey returns the server's host public key, for building
// known_hosts files in tests (TD-004).
func (s *uploadTestServer) HostKey() ssh.PublicKey {
	return s.hostKey.PublicKey()
}

// serve accepts connections until the listener is closed.
func (s *uploadTestServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

// handleConn performs the SSH handshake and dispatches channels.
func (s *uploadTestServer) handleConn(conn net.Conn) {
	defer conn.Close()
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
func (s *uploadTestServer) handleSession(channel ssh.Channel, requests <-chan *ssh.Request) {
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
//
//	mv -f <src> <dst>  — atomic rename for tmp.<rand> -> final (AC2)
//	rm -f <path>        — best-effort tmp cleanup
func (s *uploadTestServer) runCommand(channel ssh.Channel, command string) {
	switch {
	case strings.HasPrefix(command, "mkdir -p "):
		dir := strings.TrimPrefix(command, "mkdir -p ")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(channel.Stderr(), "mkdir: %v\n", err)
			sendUploadExitStatus(channel, 1)
			return
		}
		sendUploadExitStatus(channel, 0)
	case strings.HasPrefix(command, "scp -t "):
		target := strings.TrimPrefix(command, "scp -t ")
		if err := s.receiveSCP(channel, target); err != nil {
			s.t.Logf("scp receive failed: %v", err)
			fmt.Fprintf(channel.Stderr(), "scp: %v\n", err)
			sendUploadExitStatus(channel, 1)
			return
		}
		// fsync the written file to simulate atomic durability (AC2)
		if f, err := os.Open(target); err == nil {
			_ = f.Sync()
			f.Close()
		}
		sendUploadExitStatus(channel, 0)
	case strings.HasPrefix(command, "mv -f "):
		rest := strings.TrimPrefix(command, "mv -f ")
		parts := strings.Fields(rest)
		if len(parts) != 2 {
			fmt.Fprintf(channel.Stderr(), "mv: invalid args %q\n", command)
			sendUploadExitStatus(channel, 1)
			return
		}
		src, dst := parts[0], parts[1]
		if err := os.Rename(src, dst); err != nil {
			fmt.Fprintf(channel.Stderr(), "mv: %v\n", err)
			sendUploadExitStatus(channel, 1)
			return
		}
		sendUploadExitStatus(channel, 0)
	case strings.HasPrefix(command, "rm -f "):
		target := strings.TrimPrefix(command, "rm -f ")
		_ = os.Remove(strings.TrimSpace(target))
		sendUploadExitStatus(channel, 0)
	default:
		s.t.Logf("unsupported command %q", command)
		sendUploadExitStatus(channel, 1)
	}
}

// receiveSCP implements the SCP sink side of the protocol: it reads
// the "C0644 <size> <basename>" header, acknowledges it, receives the
// content and the terminating 0x00, acknowledges, and writes the
// content to target.
func (s *uploadTestServer) receiveSCP(channel ssh.Channel, target string) error {
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

	if s.scpDenied {
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

// sendUploadExitStatus reports the command exit status to the client.
func sendUploadExitStatus(channel ssh.Channel, code uint32) {
	_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{code}))
}

// writeUploadTestKey generates an ephemeral ed25519 key pair, writes
// the private key in OpenSSH PEM format to a temp file, and returns
// the file path and the corresponding public key.
func writeUploadTestKey(t *testing.T) (string, ssh.PublicKey) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("create ssh signer: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(privateKey, "anvil-upload-test-key")
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	keyPath := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}
	return keyPath, signer.PublicKey()
}

// setUploadEnv configures the SSH credential environment variables for
// an upload test. Previous values are restored via t.Cleanup. The
// optional host key verification variables (DEPLOY_SSH_KNOWN_HOSTS,
// DEPLOY_SSH_KNOWN_HOSTS_MODE, TD-004) are unset so upload tests run
// with verification disabled regardless of the developer's shell
// environment.
func setUploadEnv(t *testing.T, host string, port int, user, keyPath string) {
	t.Helper()
	restore := func(name string) {
		previous, existed := os.LookupEnv(name)
		t.Cleanup(func() {
			if existed {
				_ = os.Setenv(name, previous)
			} else {
				_ = os.Unsetenv(name)
			}
		})
	}
	restore(envDeployServerHost)
	restore(envDeployServerUser)
	restore(envDeployServerPort)
	restore(envDeploySSHKey)
	restore(envDeploySSHKnownHosts)
	restore(envDeploySSHKnownHostsMode)

	_ = os.Setenv(envDeployServerHost, host)
	_ = os.Setenv(envDeployServerUser, user)
	_ = os.Setenv(envDeployServerPort, strconv.Itoa(port))
	_ = os.Setenv(envDeploySSHKey, keyPath)
	_ = os.Unsetenv(envDeploySSHKnownHosts)
	_ = os.Unsetenv(envDeploySSHKnownHostsMode)
}

// unsetUploadEnv removes the SSH credential environment variables and
// restores the previous values after the test. Tests using this helper
// must not call t.Parallel.
func unsetUploadEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		envDeployServerHost, envDeployServerUser, envDeployServerPort,
		envDeploySSHKey, envDeploySSHKnownHosts, envDeploySSHKnownHostsMode,
	} {
		previous, existed := os.LookupEnv(name)
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("unsetenv %s: %v", name, err)
		}
		t.Cleanup(func() {
			if existed {
				_ = os.Setenv(name, previous)
			} else {
				_ = os.Unsetenv(name)
			}
		})
	}
}

// envDeploy* constants mirror the deployment package env var names;
// they are repeated here so cmd tests can reference them without
// coupling to internal constants.
const (
	envDeployServerHost        = "DEPLOY_SERVER_HOST"
	envDeployServerUser        = "DEPLOY_SERVER_USER"
	envDeployServerPort        = "DEPLOY_SERVER_PORT"
	envDeploySSHKey            = "DEPLOY_SSH_KEY"
	envDeploySSHKnownHosts     = "DEPLOY_SSH_KNOWN_HOSTS"
	envDeploySSHKnownHostsMode = "DEPLOY_SSH_KNOWN_HOSTS_MODE"
)
