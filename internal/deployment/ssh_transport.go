// Package deployment defines the Deployment bounded context for Anvil.
//
// Reference: TS-P11-01, TS-011-001, EPIC-011, ADR-015, ADR-017, ADR-019
package deployment

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
)

// Defaults for SSHTransport fields when left zero-valued.
const (
	// defaultSSHPort is used when SSHTransport.Port is 0 (EPIC-011 §7.3).
	defaultSSHPort = 22
	// defaultSSHTimeout bounds the SSH connection attempt (EPIC-011 §7.2).
	defaultSSHTimeout = 10 * time.Second
	// defaultRemoteDir is the remote upload directory (EPIC-011 §7.5).
	defaultRemoteDir = "/tmp/anvil-uploads"
)

// SSHTransport delivers artifacts to a remote server over SSH using the
// SCP protocol. It implements the Transport interface defined by
// EPIC-010.
//
// Connection parameters are provided explicitly at construction time —
// reading credentials from the environment is owned by TS-P11-03
// (credential management) and is intentionally not performed here. The
// host key callback accepts any host key: pinning hosts to known keys
// requires a known-hosts source that is out of scope for this work item
// (see TS-P11-03/TS-P11-05) and is a known residual risk.
//
// Reference: TS-P11-01, TS-011-001, EPIC-011 §7, ADR-015, ADR-017
type SSHTransport struct {
	// Host is the remote server hostname or IP address.
	Host string

	// Port is the SSH port; 0 selects the default (22).
	Port int

	// User is the SSH login username.
	User string

	// KeyPath is the path to the private key file used for
	// public-key authentication.
	KeyPath string

	// Timeout bounds the SSH connection attempt; 0 selects the
	// default (10s). It does not bound the transfer itself.
	Timeout time.Duration

	// RemoteDir is the remote directory the artifact is uploaded to;
	// empty selects the default (/tmp/anvil-uploads).
	RemoteDir string
}

// Compile-time assertion that SSHTransport satisfies the Transport
// contract (TS-011-001 AC-1).
var _ Transport = (*SSHTransport)(nil)

// Option configures optional SSHTransport settings.
//
// Reference: TS-P11-01, EPIC-011 §7.2
type Option func(*SSHTransport)

// WithTimeout sets the SSH connection timeout for a transport built by
// NewSSHTransport.
func WithTimeout(timeout time.Duration) Option {
	return func(t *SSHTransport) { t.Timeout = timeout }
}

// WithRemoteDir sets the remote upload directory for a transport built
// by NewSSHTransport.
func WithRemoteDir(dir string) Option {
	return func(t *SSHTransport) { t.RemoteDir = dir }
}

// NewSSHTransport returns an SSHTransport for the given connection
// credentials.
//
// The functional-options pattern is used because only Timeout and
// RemoteDir are optional: the required credentials (host, user, key
// path, port) stay positional so a transport can never be constructed
// with them missing. Defaults are applied after options so the
// constructed transport always carries concrete values: port 22,
// timeout 10s, remote dir /tmp/anvil-uploads.
//
// Reference: TS-P11-01, EPIC-011 §7.2
func NewSSHTransport(host, user, keyPath string, port int, opts ...Option) *SSHTransport {
	t := &SSHTransport{
		Host:    host,
		Port:    port,
		User:    user,
		KeyPath: keyPath,
	}
	for _, opt := range opts {
		opt(t)
	}
	if t.Port == 0 {
		t.Port = defaultSSHPort
	}
	if t.Timeout == 0 {
		t.Timeout = defaultSSHTimeout
	}
	if t.RemoteDir == "" {
		t.RemoteDir = defaultRemoteDir
	}
	return t
}

// Deliver uploads the artifact payload to the target over SSH/SCP.
//
// The delivery flow (EPIC-011 §7.5):
//
//  0. Validate configuration inputs and remote paths.
//  1. Open the local artifact file (read-only) and load the private key.
//  2. Dial host:port and authenticate with the key.
//  3. Create RemoteDir on the target if it does not exist.
//  4. SCP the artifact file to RemoteDir/<basename>.
//
// Failures are reported as *TransportError with Recoverable=true for
// transient network/transfer failures and Recoverable=false for
// authentication, key, configuration, or local input errors (EPIC-011
// §7.6). Configuration and validation errors are reported before any
// network I/O. Runtime State is never mutated (ADR-015, Decision 006).
//
// Reference: TS-P11-01, TS-011-001 AC-2..AC-5, EPIC-011 §7.5, §7.6
func (t *SSHTransport) Deliver(payload ArtifactPayload, target Target) (*TransportResult, error) {
	// Configuration errors are not recoverable: retrying with the same
	// inputs cannot succeed, so they are reported before any I/O.
	if t.Host == "" {
		return nil, configError(target.ID(), "SSH transport host is empty")
	}
	if t.User == "" {
		return nil, configError(target.ID(), "SSH transport user is empty")
	}
	if t.KeyPath == "" {
		return nil, configError(target.ID(), "SSH transport key path is empty")
	}
	if payload.Path == "" {
		return nil, configError(target.ID(), "artifact path is empty")
	}

	// Remote paths are embedded in remote shell commands, so they are
	// validated before any network I/O (defense in depth: the values
	// are operator-controlled configuration, but a safe default keeps
	// paths from being interpreted by the remote shell).
	if err := validateRemotePath(t.RemoteDir); err != nil {
		return nil, configError(target.ID(), err.Error())
	}
	if err := validateRemotePath(path.Base(payload.Path)); err != nil {
		return nil, configError(target.ID(),
			fmt.Sprintf("artifact basename is not a safe remote filename: %v", err))
	}

	// Local input errors fail fast, before any network I/O: retrying
	// with the same inputs cannot succeed, so they are not recoverable.
	file, err := os.Open(payload.Path)
	if err != nil {
		return nil, &TransportError{
			TargetID:    target.ID(),
			Reason:      fmt.Sprintf("cannot open artifact file %q: %v", payload.Path, err),
			Recoverable: false,
		}
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, &TransportError{
			TargetID:    target.ID(),
			Reason:      fmt.Sprintf("cannot stat artifact file %q: %v", payload.Path, err),
			Recoverable: false,
		}
	}

	signer, err := t.loadSigner()
	if err != nil {
		// A missing or unparsable key file is a local configuration
		// problem: retrying with the same inputs cannot succeed, and
		// the server is never reached. Guidance points at the key
		// configuration rather than server-side authorization.
		return nil, &TransportError{
			TargetID:    target.ID(),
			Kind:        KindConfiguration,
			Reason:      err.Error(),
			Recoverable: false,
		}
	}

	client, err := t.dial(signer)
	if err != nil {
		return nil, t.dialError(err, target.ID())
	}
	defer client.Close()

	if err := t.ensureRemoteDir(client); err != nil {
		return nil, &TransportError{
			TargetID:    target.ID(),
			Kind:        classifyTransferError(err),
			Reason:      err.Error(),
			Recoverable: true,
		}
	}

	remotePath := path.Join(t.RemoteDir, path.Base(payload.Path))

	session, err := client.NewSession()
	if err != nil {
		return nil, &TransportError{
			TargetID:    target.ID(),
			Kind:        KindTransferFailed,
			Reason:      fmt.Sprintf("cannot open SSH session: %v", err),
			Recoverable: true,
		}
	}
	defer session.Close()

	if err := t.scpUpload(session, file, fileInfo.Size(), remotePath); err != nil {
		return nil, &TransportError{
			TargetID:    target.ID(),
			Kind:        classifyTransferError(err),
			Reason:      err.Error(),
			Recoverable: true,
		}
	}

	return &TransportResult{
		Success:    true,
		TargetID:   target.ID(),
		RemotePath: remotePath,
	}, nil
}

// loadSigner reads and parses the private key at KeyPath.
func (t *SSHTransport) loadSigner() (ssh.Signer, error) {
	keyData, err := os.ReadFile(t.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read SSH private key file %q: %v", t.KeyPath, err)
	}
	signer, err := ssh.ParsePrivateKey(keyData)
	if err != nil {
		return nil, fmt.Errorf("cannot parse SSH private key file %q: %v", t.KeyPath, err)
	}
	return signer, nil
}

// dial connects to and authenticates against the transport host.
func (t *SSHTransport) dial(signer ssh.Signer) (*ssh.Client, error) {
	config := &ssh.ClientConfig{
		User:    t.User,
		Auth:    []ssh.AuthMethod{ssh.PublicKeys(signer)},
		Timeout: t.Timeout,
		// Host key verification against known_hosts requires a
		// known-hosts source, which is out of scope for this work item
		// (TS-P11-03/TS-P11-05). Documented as a known risk.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	return ssh.Dial("tcp", t.addr(), config)
}

// addr returns the host:port dial address.
func (t *SSHTransport) addr() string {
	return net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
}

// configError builds a non-recoverable TransportError: configuration
// or validation failures cannot succeed on retry with the same inputs.
func configError(targetID TargetID, reason string) *TransportError {
	return &TransportError{
		TargetID:    targetID,
		Kind:        KindConfiguration,
		Reason:      reason,
		Recoverable: false,
	}
}

// dialError classifies a dial failure per EPIC-011 §7.6: authentication
// failures are not recoverable; everything else (connection refused,
// timeout, network errors) is. Network-level failures share the
// connection-refused guidance: the server is not reachable.
func (t *SSHTransport) dialError(err error, targetID TargetID) *TransportError {
	if isAuthError(err) {
		return &TransportError{
			TargetID:    targetID,
			Kind:        KindAuthenticationFailed,
			Reason:      fmt.Sprintf("authentication failed for user %q on %s: %v", t.User, t.addr(), err),
			Recoverable: false,
		}
	}
	if isConnectionRefused(err) {
		return &TransportError{
			TargetID:    targetID,
			Kind:        KindConnectionRefused,
			Reason:      fmt.Sprintf("connection refused by %s: %v", t.addr(), err),
			Recoverable: true,
		}
	}
	return &TransportError{
		TargetID:    targetID,
		Kind:        KindConnectionRefused,
		Reason:      fmt.Sprintf("cannot connect to %s: %v", t.addr(), err),
		Recoverable: true,
	}
}

// isAuthError reports whether an SSH dial error is an authentication
// failure rather than a network-level failure. x/crypto/ssh reports
// authentication failures with a message containing "unable to
// authenticate".
func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "unable to authenticate") ||
		strings.Contains(msg, "no supported methods remain")
}

// isConnectionRefused reports whether an SSH dial error is a
// network-level unreachability failure (connection refused, timeout).
// The errors.Is check unwraps the net.OpError chain produced by
// x/crypto/ssh; the string fallback covers wrapped errors that lose
// the sentinel.
func isConnectionRefused(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no route to host") ||
		strings.Contains(msg, "timed out") ||
		strings.Contains(msg, "i/o timeout")
}

// classifyTransferError classifies a remote-side transfer failure
// (remote directory creation or SCP upload): a permission problem is
// reported as such, everything else is a transfer failure. Detection
// relies on the remote stderr text, which remote scp/mkdir report for
// authorization and filesystem denials ("Permission denied").
func classifyTransferError(err error) TransportErrorKind {
	if isPermissionDenied(err) {
		return KindPermissionDenied
	}
	return KindTransferFailed
}

// isPermissionDenied reports whether an error from a remote command
// indicates a permission problem (key not authorized or filesystem
// denied). Remote scp and mkdir report "Permission denied" on stderr,
// which is captured into the error message by the transport.
func isPermissionDenied(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Permission denied") ||
		strings.Contains(msg, "permission denied")
}

// validateRemotePath rejects paths that would be unsafe to embed in a
// remote shell command (defense in depth: Deliver calls this before any
// network I/O). Rejected characters are shell metacharacters, quotes,
// wildcards, tilde, and whitespace — anything that could change how the
// remote shell interprets the command or the path.
func validateRemotePath(p string) error {
	if p == "" {
		return fmt.Errorf("remote path is empty; configure a non-empty remote path")
	}
	if strings.ContainsAny(p, "\r\n;`$()<>&|*?[]~ '\"\\") {
		return fmt.Errorf(
			"remote path %q contains whitespace or shell metacharacters, which are not allowed; use a path without spaces or special characters",
			p,
		)
	}
	return nil
}

// ensureRemoteDir creates RemoteDir on the target with mkdir -p.
//
// mkdir -p succeeds silently when the directory already exists, so no
// pre-flight existence check is needed. A failing mkdir is tolerated
// only when the remote reports "File exists" (some servers reject
// mkdir -p with that message when a path component already exists);
// any other failure is returned.
func (t *SSHTransport) ensureRemoteDir(client *ssh.Client) error {
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("cannot open SSH session: %v", err)
	}
	defer session.Close()

	var stderr bytes.Buffer
	session.Stderr = &stderr

	// RemoteDir was validated by Deliver before any network I/O, so it
	// is safe to embed in the remote shell command.
	if err := session.Run("mkdir -p " + t.RemoteDir); err != nil {
		if strings.Contains(stderr.String(), "File exists") {
			return nil
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("cannot create remote directory %q: %s (%v)", t.RemoteDir, msg, err)
		}
		return fmt.Errorf("cannot create remote directory %q: %v", t.RemoteDir, err)
	}
	return nil
}

// scpUpload streams the artifact file to the target with the SCP
// protocol in sink mode (`scp -t <remote-path>`), using no extra
// dependency beyond the SSH channel.
//
// The wire sequence is the classic SCP source flow:
//
//	send "C0644 <size> <basename>\n"  →  await 0x00 ack
//	stream <size> bytes               →  send 0x00 terminator  →  await 0x00 ack
//
// Errors reported by the remote scp arrive on stderr and are included
// in the returned error. The payload file is only read, never
// modified. Remote paths are validated by Deliver before the session
// is opened, so they are safe to embed in the remote command.
func (t *SSHTransport) scpUpload(session *ssh.Session, file *os.File, size int64, remotePath string) error {
	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("cannot open scp stdin: %v", err)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("cannot open scp stdout: %v", err)
	}

	var stderr bytes.Buffer
	session.Stderr = &stderr

	if err := session.Start("scp -t " + remotePath); err != nil {
		return fmt.Errorf("cannot start scp for %q: %v", remotePath, err)
	}

	// fail drains the session and wraps the cause with the remote scp
	// stderr output: closing stdin unblocks a remote scp still waiting
	// for data, and Wait ensures the stderr buffer is fully captured
	// before it is read.
	fail := func(cause error) error {
		_ = stdin.Close()
		_ = session.Wait()
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("%v (remote scp: %s)", cause, msg)
		}
		return cause
	}

	basename := path.Base(remotePath)
	header := fmt.Sprintf("C0644 %d %s\n", size, basename)
	if _, err := io.WriteString(stdin, header); err != nil {
		return fail(fmt.Errorf("cannot send scp header for %q: %v", basename, err))
	}

	ack := make([]byte, 1)
	if _, err := io.ReadFull(stdout, ack); err != nil {
		return fail(fmt.Errorf("scp rejected header for %q: %v", basename, err))
	}
	if ack[0] != 0 {
		return fail(fmt.Errorf("scp rejected header for %q", basename))
	}

	if _, err := io.Copy(stdin, file); err != nil {
		return fail(fmt.Errorf("cannot stream artifact data for %q: %v", basename, err))
	}
	if _, err := stdin.Write([]byte{0}); err != nil {
		return fail(fmt.Errorf("cannot terminate scp transfer for %q: %v", basename, err))
	}

	if _, err := io.ReadFull(stdout, ack); err != nil {
		return fail(fmt.Errorf("scp did not confirm transfer of %q: %v", basename, err))
	}
	if ack[0] != 0 {
		return fail(fmt.Errorf("scp failed to write %q on the target", basename))
	}

	// Signal EOF to the remote scp. A close error here only means the
	// server already closed the channel after the final ack — the
	// transfer is complete either way, and Wait below is the
	// authoritative completion signal.
	_ = stdin.Close()
	if err := session.Wait(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("scp transfer failed for %q: %v (remote scp: %s)", basename, err, msg)
		}
		return fmt.Errorf("scp transfer failed for %q: %v", basename, err)
	}
	return nil
}
