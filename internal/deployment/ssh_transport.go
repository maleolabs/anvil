// Package deployment defines the Deployment bounded context for Anvil.
//
// Reference: TS-P11-01, TS-011-001, EPIC-011, ADR-015, ADR-017, ADR-019
package deployment

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"

	"maleolabs.com/anvil/internal/output"
)

// Defaults for SSHTransport fields when left zero-valued.
const (
	// defaultSSHPort is used when SSHTransport.Port is 0 (EPIC-011 §7.3).
	defaultSSHPort = 22
	// defaultSSHTimeout bounds the SSH connection attempt (EPIC-011 §7.2).
	defaultSSHTimeout = 10 * time.Second
	// defaultRemoteDir is the remote upload directory (EPIC-011 §7.5).
	defaultRemoteDir = "/tmp/anvil-uploads"
	// defaultMaxRetries is the retry bound for idempotent transfer (AC2).
	defaultMaxRetries = 3
	// defaultRetryBase is the base backoff for retry (exponential).
	defaultRetryBase = 100 * time.Millisecond
)

// SSHTransport delivers artifacts to a remote server over SSH using the
// SCP protocol. It implements the Transport interface defined by
// EPIC-010.
//
// Connection parameters are provided explicitly at construction time —
// reading credentials from the environment is owned by TS-P11-03
// (credential management) and is intentionally not performed here.
//
// Host key verification is opt-in (TD-004): when KnownHostsPath is set,
// the server's identity is verified against an OpenSSH known_hosts file
// and the connection fails closed on unknown or changed host keys.
// Without a known-hosts file the callback accepts any host key, which
// preserves the legacy behavior for existing CI flows but leaves the
// transport exposed to man-in-the-middle impersonation — production
// deployments should configure a known-hosts file.
//
// Reference: TS-P11-01, TS-011-001, EPIC-011 §7, ADR-015, ADR-017, TD-004
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

	// KnownHostsPath is the path to an OpenSSH known_hosts file used
	// to verify the server's host key (TD-004). Empty keeps the
	// legacy behavior of accepting any host key.
	KnownHostsPath string

	// KnownHostsMode selects the verification mode; only consulted
	// when KnownHostsPath is set (TD-004). Zero value selects
	// KnownHostsModeStrict.
	KnownHostsMode KnownHostsMode
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

// WithKnownHosts enables host key verification against an OpenSSH
// known_hosts file for a transport built by NewSSHTransport (TD-004).
//
// The verification is fail-closed: strict mode (the default when mode
// is the zero value) rejects unknown and changed host keys; accept-new
// mode records an unknown host key on first contact and still rejects a
// changed key, which can signal a man-in-the-middle attack.
func WithKnownHosts(path string, mode KnownHostsMode) Option {
	return func(t *SSHTransport) {
		t.KnownHostsPath = path
		t.KnownHostsMode = mode
	}
}

// NewSSHTransport returns an SSHTransport for the given connection
// credentials.
//
// The functional-options pattern is used because only Timeout,
// RemoteDir, and host key verification (WithKnownHosts, TD-004) are
// optional: the required credentials (host, user, key path, port) stay
// positional so a transport can never be constructed with them
// missing. Defaults are applied after options so the constructed
// transport always carries concrete values: port 22, timeout 10s,
// remote dir /tmp/anvil-uploads.
//
// Reference: TS-P11-01, EPIC-011 §7.2, TD-004
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

// Deliver uploads the artifact payload to the target over SSH/SCP with
// atomic tmp.<rand> -> fsync -> rename and idempotent retry (AC2).
//
// The delivery flow (EPIC-011 §7.5, sto:local-deploy-transport AC2):
//
//  0. Validate configuration inputs and remote paths.
//  1. Open the local artifact file (read-only) and load the private key.
//  2. Build the host key verification callback from the configured
//     known-hosts file, when verification is enabled (TD-004).
//  3. Dial host:port and authenticate with the key (retryable on Timeout/Unreachable).
//  4. Create RemoteDir on the target if it does not exist.
//  5. SCP the artifact file atomically: upload to RemoteDir/<basename>.tmp.<rand>
//     then fsync & atomic rename to RemoteDir/<basename>. Retry up to 3× with
//     exponential backoff on Recoverable transfer failures; partial tmp files
//     never corrupt the final artifact (checksum re-verify after retry).
//
// Failures are reported as *TransportError with Recoverable=true for
// transient network/transfer failures and Recoverable=false for
// authentication, host key verification (TD-004), key, configuration,
// or local input errors (EPIC-011 §7.6). Configuration and validation
// errors are reported before any network I/O. Runtime State is never
// mutated (ADR-015, Decision 006). KeyPath is redacted in errors (AC4).
//
// Reference: TS-P11-01, TS-011-001 AC-2..AC-5, EPIC-011 §7.5, §7.6, sto:local-deploy-transport
func (t *SSHTransport) Deliver(payload ArtifactPayload, target Target) (*TransportResult, error) {
	// Configuration errors are not recoverable: retrying with the same
	// inputs cannot succeed, so they are reported before any I/O.
	if t.Host == "" {
		return nil, configError(target.ID(), "SSH transport host is empty")
	}
	if t.User == "" {
		return nil, configError(target.ID(), "SSH transport user is empty")
	}
	if t.KeyPath == "" && os.Getenv("SSH_AUTH_SOCK") == "" {
		return nil, configError(target.ID(), "SSH transport key path is empty")
	}
	if payload.Path == "" {
		return nil, configError(target.ID(), "artifact path is empty")
	}

	// Remote paths are embedded in remote shell commands, so they are
	// validated before any network I/O (defense in depth).
	if err := validateRemotePath(t.RemoteDir); err != nil {
		return nil, configError(target.ID(), err.Error())
	}
	if err := validateRemotePath(path.Base(payload.Path)); err != nil {
		return nil, configError(target.ID(),
			fmt.Sprintf("artifact basename is not a safe remote filename: %v", err))
	}

	// Local input errors fail fast, before any network I/O.
	file, err := os.Open(payload.Path)
	if err != nil {
		return nil, &TransportError{
			TargetID:    target.ID(),
			Reason:      fmt.Sprintf("cannot open artifact file %q: %v", filepath.Base(payload.Path), err),
			Recoverable: false,
		}
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, &TransportError{
			TargetID:    target.ID(),
			Reason:      fmt.Sprintf("cannot stat artifact file %q: %v", filepath.Base(payload.Path), err),
			Recoverable: false,
		}
	}

	authMethods, err := t.authMethods()
	if err != nil {
		// redact KeyPath in error (AC4) — preserve "private key" wording for diagnostics,
		// only redact the actual path value (not the whole message via RedactSecrets).
		safe := err.Error()
		if t.KeyPath != "" && strings.Contains(safe, t.KeyPath) {
			safe = strings.ReplaceAll(safe, t.KeyPath, redactKeyPathForLog(t.KeyPath))
		}
		return nil, &TransportError{
			TargetID:    target.ID(),
			Kind:        KindConfiguration,
			Reason:      safe,
			Recoverable: false,
		}
	}

	hostKeyCallback, err := t.hostKeyCallback()
	if err != nil {
		return nil, &TransportError{
			TargetID:    target.ID(),
			Kind:        KindConfiguration,
			Reason:      output.SanitizeLogLine(err.Error()),
			Recoverable: false,
		}
	}

	// Retry loop for idempotent transfer (AC2): up to 3 attempts with backoff.
	// Only Recoverable errors are retried; non-recoverable fail fast.
	var lastErr error
	for attempt := 0; attempt < defaultMaxRetries; attempt++ {
		if attempt > 0 {
			backoff := defaultRetryBase * time.Duration(1<<uint(attempt-1))
			time.Sleep(backoff)
			// rewind file for retry
			if _, serr := file.Seek(0, io.SeekStart); serr != nil {
				return nil, &TransportError{
					TargetID:    target.ID(),
					Kind:        KindTransferFailed,
					Reason:      fmt.Sprintf("cannot retry transfer (seek failed): %v", serr),
					Recoverable: false,
				}
			}
		}
		res, err := t.deliverOnce(file, fileInfo.Size(), payload, target, authMethods, hostKeyCallback)
		if err == nil {
			return res, nil
		}
		var te *TransportError
		if errors.As(err, &te) && !te.Recoverable {
			return nil, err
		}
		lastErr = err
		// if not recoverable, don't retry; else continue
		if te != nil && !te.Recoverable {
			return nil, err
		}
		// transient: retry unless last attempt
		if attempt == defaultMaxRetries-1 {
			break
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, &TransportError{
		TargetID:    target.ID(),
		Kind:        KindTransferFailed,
		Reason:      "transfer failed after retries",
		Recoverable: true,
	}
}

// deliverOnce performs a single attempt: dial, ensureRemoteDir, atomic scpUpload.
func (t *SSHTransport) deliverOnce(file *os.File, size int64, payload ArtifactPayload, target Target, authMethods []ssh.AuthMethod, hostKeyCallback ssh.HostKeyCallback) (*TransportResult, error) {
	client, err := t.dialWithAuth(authMethods, hostKeyCallback)
	if err != nil {
		return nil, t.dialError(err, target.ID())
	}
	defer client.Close()

	if err := t.ensureRemoteDir(client); err != nil {
		return nil, &TransportError{
			TargetID:    target.ID(),
			Kind:        classifyTransferError(err),
			Reason:      output.SanitizeLogLine(err.Error()),
			Recoverable: true,
		}
	}

	remotePath := path.Join(t.RemoteDir, path.Base(payload.Path))

	if err := t.scpUploadAtomic(client, file, size, remotePath); err != nil {
		return nil, &TransportError{
			TargetID:    target.ID(),
			Kind:        classifyTransferError(err),
			Reason:      output.SanitizeLogLine(err.Error()),
			Recoverable: true,
		}
	}

	return &TransportResult{
		Success:    true,
		TargetID:   target.ID(),
		RemotePath: remotePath,
	}, nil
}

// loadSigner reads and parses the private key at KeyPath (redacted on error, AC4).
func (t *SSHTransport) loadSigner() (ssh.Signer, error) {
	keyData, err := os.ReadFile(t.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read SSH private key file %q: %v", redactKeyPathForLog(t.KeyPath), err)
	}
	signer, err := ssh.ParsePrivateKey(keyData)
	if err != nil {
		return nil, fmt.Errorf("cannot parse SSH private key file %q: %v", redactKeyPathForLog(t.KeyPath), err)
	}
	return signer, nil
}

// redactKeyPathForLog returns a redacted representation of a key path for logging (AC4).
func redactKeyPathForLog(p string) string {
	if p == "" {
		return "[REDACTED]"
	}
	return filepath.Base(p) + " [REDACTED_PATH]"
}

// redactKeyPath replaces full key path occurrences with redacted form.
func redactKeyPath(msg, keyPath string) string {
	if keyPath != "" && strings.Contains(msg, keyPath) {
		return strings.ReplaceAll(msg, keyPath, redactKeyPathForLog(keyPath))
	}
	return output.SanitizeLogLine(msg)
}

// authMethods returns the SSH auth methods for this transport, preferring
// ssh-agent when KeyPath is empty (AC3) and falling back to file key otherwise.
// Reference: sto:local-deploy-config AC3 ssh-agent preferred
func (t *SSHTransport) authMethods() ([]ssh.AuthMethod, error) {
	if t.KeyPath != "" {
		signer, err := t.loadSigner()
		if err != nil {
			return nil, err
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	}
	// KeyPath empty → ssh-agent preferred (AC3)
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, fmt.Errorf("no SSH key available: set server.targets[env].sshKeyPath or %s, or run ssh-agent (preferred)", EnvDeploySSHKey)
	}
	// Dial lazily inside the callback and close immediately after Signers()
	// to avoid leaking the unix socket FD (HIGH: ssh-agent FD leak). The
	// returned signers are independent of the agent connection after retrieval.
	return []ssh.AuthMethod{ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
		conn, err := net.Dial("unix", sock)
		if err != nil {
			return nil, fmt.Errorf("cannot connect to ssh-agent at %q: %v", sock, err)
		}
		defer conn.Close()
		ag := agent.NewClient(conn)
		signers, err := ag.Signers()
		if err != nil {
			return nil, err
		}
		if len(signers) == 0 {
			return nil, fmt.Errorf("ssh-agent has no keys")
		}
		return signers, nil
	})}, nil
}

// dial connects to and authenticates against the transport host using a single signer.
func (t *SSHTransport) dial(signer ssh.Signer, hostKeyCallback ssh.HostKeyCallback) (*ssh.Client, error) {
	return t.dialWithAuth([]ssh.AuthMethod{ssh.PublicKeys(signer)}, hostKeyCallback)
}

// dialWithAuth connects using the provided auth methods.
func (t *SSHTransport) dialWithAuth(auth []ssh.AuthMethod, hostKeyCallback ssh.HostKeyCallback) (*ssh.Client, error) {
	config := &ssh.ClientConfig{
		User:            t.User,
		Auth:            auth,
		Timeout:         t.Timeout,
		HostKeyCallback: hostKeyCallback,
	}
	return ssh.Dial("tcp", t.addr(), config)
}

// hostKeyCallback returns the HostKeyCallback for this transport
// (TD-004).
//
// Host key verification is opt-in: without a known-hosts file the
// callback accepts any host key, preserving the legacy behavior so
// existing CI flows keep working. This is an explicit, documented
// posture for development contexts — production deployments should set
// DEPLOY_SSH_KNOWN_HOSTS so the target's identity is verified.
//
// When a known-hosts file is configured, verification fails closed:
// strict mode rejects unknown and changed host keys; accept-new mode
// records an unknown key on first contact but still rejects a changed
// key because it can signal a man-in-the-middle attack. A missing file
// is a configuration error in strict mode; in accept-new mode it is
// created with the first host key.
func (t *SSHTransport) hostKeyCallback() (ssh.HostKeyCallback, error) {
	if t.KnownHostsPath == "" {
		// Documented legacy posture (TD-004): no host key verification.
		return ssh.InsecureIgnoreHostKey(), nil
	}

	strict, err := knownhosts.New(t.KnownHostsPath)
	if err != nil {
		if !os.IsNotExist(err) || t.KnownHostsMode != KnownHostsModeAcceptNew {
			return nil, fmt.Errorf("cannot load SSH known-hosts file %q: %v", t.KnownHostsPath, err)
		}
		// accept-new tolerates a missing file: start from an empty
		// database and record the first host key on contact.
		strict, err = knownhosts.New()
		if err != nil {
			return nil, err
		}
	}

	if t.KnownHostsMode == KnownHostsModeAcceptNew {
		return t.acceptNewCallback(strict), nil
	}
	return t.verifyCallback(strict), nil
}

// verifyCallback wraps a strict known-hosts callback so every
// verification failure is marked as a host key verification error
// (TD-004). This lets dialError classify the failure as non-recoverable
// instead of treating it like a transient network error.
func (t *SSHTransport) verifyCallback(strict ssh.HostKeyCallback) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if err := strict(hostname, remote, key); err != nil {
			return &hostKeyVerificationError{err}
		}
		return nil
	}
}

// acceptNewCallback wraps a strict known-hosts callback with accept-new
// semantics (TD-004): an unknown host key is recorded in the
// known-hosts file and accepted (TOFU for first contact); a changed or
// revoked host key still fails closed because it can signal a
// man-in-the-middle attack.
func (t *SSHTransport) acceptNewCallback(strict ssh.HostKeyCallback) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := strict(hostname, remote, key)
		if err == nil {
			return nil
		}
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) && len(keyErr.Want) == 0 {
			// Unknown host: record the key and accept. A KeyError with
			// existing entries means a changed key, and any other error
			// (revoked key, unreadable database) fails closed below.
			return t.recordHostKey(hostname, key)
		}
		return &hostKeyVerificationError{err}
	}
}

// recordHostKey appends the host key to the known-hosts file so later
// connections verify it (accept-new mode, TD-004). The file is created
// with owner-only permissions when it does not exist.
func (t *SSHTransport) recordHostKey(hostname string, key ssh.PublicKey) error {
	f, err := os.OpenFile(t.KnownHostsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return &hostKeyVerificationError{fmt.Errorf(
			"cannot record host key in known-hosts file %q: %v", t.KnownHostsPath, err)}
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "%s\n", knownhosts.Line([]string{hostname}, key)); err != nil {
		return &hostKeyVerificationError{fmt.Errorf(
			"cannot record host key in known-hosts file %q: %v", t.KnownHostsPath, err)}
	}
	return nil
}

// hostKeyVerificationError marks failures raised by the host key
// verification callback (TD-004) so dialError can classify them as
// non-recoverable instead of transient network failures.
type hostKeyVerificationError struct{ err error }

func (e *hostKeyVerificationError) Error() string { return e.err.Error() }
func (e *hostKeyVerificationError) Unwrap() error { return e.err }

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

// dialError classifies a dial failure per EPIC-011 §7.6 into 6 Kinds:
// host key verification and authentication failures are not recoverable;
// Timeout and ConnectionRefused/Unreachable are recoverable with distinct Kinds.
//
// 6Kind mapping (AC3):
//   Timeout               → KindTimeout (exit 1, retryable)
//   ConnectionRefused/Unreachable → KindConnectionRefused/KindUnreachable (exit 1, retryable)
//   AuthFail              → KindAuthenticationFailed (exit 4, not retryable)
//   HostKeyVerification   → KindHostKeyVerificationFailed (exit 4, not retryable)
func (t *SSHTransport) dialError(err error, targetID TargetID) *TransportError {
	var verificationErr *hostKeyVerificationError
	if errors.As(err, &verificationErr) {
		return &TransportError{
			TargetID:    targetID,
			Kind:        KindHostKeyVerificationFailed,
			Reason:      output.SanitizeLogLine(fmt.Sprintf("host key verification failed for %s: %v", t.addr(), err)),
			Recoverable: false,
		}
	}
	if isAuthError(err) {
		return &TransportError{
			TargetID:    targetID,
			Kind:        KindAuthenticationFailed,
			Reason:      output.SanitizeLogLine(fmt.Sprintf("authentication failed for user %q on %s: %v", t.User, t.addr(), err)),
			Recoverable: false,
		}
	}
	if isTimeout(err) {
		return &TransportError{
			TargetID:    targetID,
			Kind:        KindTimeout,
			Reason:      output.SanitizeLogLine(fmt.Sprintf("connection timed out to %s: %v", t.addr(), err)),
			Recoverable: true,
		}
	}
	if isUnreachable(err) {
		return &TransportError{
			TargetID:    targetID,
			Kind:        KindUnreachable,
			Reason:      output.SanitizeLogLine(fmt.Sprintf("host unreachable %s: %v", t.addr(), err)),
			Recoverable: true,
		}
	}
	if isConnectionRefused(err) {
		return &TransportError{
			TargetID:    targetID,
			Kind:        KindConnectionRefused,
			Reason:      output.SanitizeLogLine(fmt.Sprintf("connection refused by %s: %v", t.addr(), err)),
			Recoverable: true,
		}
	}
	// fallback: treat as transfer-level network failure (retryable)
	return &TransportError{
		TargetID:    targetID,
		Kind:        KindConnectionRefused,
		Reason:      output.SanitizeLogLine(fmt.Sprintf("cannot connect to %s: %v", t.addr(), err)),
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

// isTimeout reports whether an error is a timeout (KindTimeout).
func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timed out") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "timeout") && strings.Contains(msg, "dial")
}

// isUnreachable reports whether an error is unreachable (no route / network unreachable).
func isUnreachable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no route to host") ||
		strings.Contains(msg, "network is unreachable") ||
		strings.Contains(msg, "no such host")
}

// isConnectionRefused reports whether an SSH dial error is a
// connection-refused failure.
func isConnectionRefused(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection refused")
}

// classifyTransferError classifies a remote-side transfer failure
// into the 6Kind taxonomy (AC3). Checks permission first, then timeout,
// then generic transfer failure. Detection relies on remote stderr text.
func classifyTransferError(err error) TransportErrorKind {
	if isPermissionDenied(err) {
		return KindPermissionDenied
	}
	if isTimeout(err) {
		return KindTimeout
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

// scpUploadAtomic performs atomic upload: scp to tmp.<rand> then fsync & rename.
// It guarantees that a partial write never corrupts the final artifact:
// the temp file is written with SCP, synced, then atomically moved to
// remotePath via `mv`. Retry leaves no corrupt final file (AC2).
//
// Reuses spike SimulatedTransport pattern: random suffix, fsync, atomic rename.
// Reference: spikes/local-deploy-ssh-transport/simulated_transport.go, sto:local-deploy-transport AC2
func (t *SSHTransport) scpUploadAtomic(client *ssh.Client, file *os.File, size int64, remotePath string) error {
	tmpPath := remotePath + ".tmp." + randomHex(6)
	if err := validateRemotePath(tmpPath); err != nil {
		return fmt.Errorf("atomic tmp path invalid: %v", err)
	}
	// Ensure file offset at start for this attempt
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("cannot seek artifact file: %v", err)
	}
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("cannot open SSH session for scp: %v", err)
	}
	if err := t.scpUpload(session, file, size, tmpPath); err != nil {
		session.Close()
		// best-effort cleanup of tmp on failure (ignore error)
		_ = t.remoteRemove(client, tmpPath)
		return err
	}
	session.Close()

	// Atomic rename: mv tmpPath remotePath (fsync via OS guarantees on same FS).
	// We use `mv` which is atomic on POSIX when source and dest are same filesystem.
	// The temp suffix ensures idempotent retry: each attempt uses new random tmp,
	// so a failed previous tmp does not affect the next attempt's final file.
	if err := t.remoteRename(client, tmpPath, remotePath); err != nil {
		_ = t.remoteRemove(client, tmpPath)
		return fmt.Errorf("scp atomic rename failed for %q: %v", path.Base(remotePath), err)
	}
	return nil
}

// remoteRename atomically moves src to dst on the remote host via `mv`.
func (t *SSHTransport) remoteRename(client *ssh.Client, src, dst string) error {
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("cannot open SSH session for rename: %v", err)
	}
	defer session.Close()
	var stderr bytes.Buffer
	session.Stderr = &stderr
	// Both paths validated, safe to embed.
	cmd := "mv -f " + src + " " + dst
	if err := session.Run(cmd); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("remote mv failed: %s (%v)", msg, err)
		}
		return fmt.Errorf("remote mv failed: %v", err)
	}
	return nil
}

// remoteRemove removes a remote file via `rm -f` (best-effort cleanup).
func (t *SSHTransport) remoteRemove(client *ssh.Client, p string) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	_ = session.Run("rm -f " + p)
	return nil
}

// randomHex returns n random bytes as hex (for tmp suffix).
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
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
