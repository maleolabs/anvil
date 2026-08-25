// Package deployment defines the Deployment bounded context for Anvil.
//
// Reference: TS-P10-02, EPIC-010, ADR-015, Decision 006
package deployment

import "fmt"

// ArtifactPayload represents the complete immutable artifact to be
// transported to a deployment target.
//
// ManifestContent preserves the raw manifest.json bytes so that
// content identity is derived from the manifest itself, not from
// filenames (Decision 006, ADR-015). Transport implementations
// deliver the payload as-is without inspecting or mutating it.
//
// Reference: TS-P10-02 AC-1, AC-2, Decision 006
type ArtifactPayload struct {
	// Path is the local filesystem path to the packaged artifact file.
	Path string `json:"path"`

	// ManifestContent is the raw JSON content of the artifact's manifest.
	// Preserving raw content ensures manifest integrity and prevents
	// identity derivation from filenames.
	ManifestContent []byte `json:"manifest_content"`
}

// TransportResult reports the outcome of an artifact delivery operation.
//
// Reference: TS-P10-02
type TransportResult struct {
	// Success indicates whether delivery completed successfully.
	Success bool `json:"success"`

	// TargetID identifies which target received the artifact.
	TargetID TargetID `json:"target_id"`

	// RemotePath is the location on the target where the artifact
	// was delivered (if applicable). The format is implementation-
	// specific (SSH path, HTTP URL, etc.).
	RemotePath string `json:"remote_path,omitempty"`
}

// TransportErrorKind classifies a transport failure so callers can
// present actionable guidance per failure class (EPIC-011 §7.6).
//
// 6Kind matrix (AC3, ADR local-deploy-transport):
//
//	KindTimeout                     → timeout / i/o timeout (Recoverable, exit 1)
//	KindConnectionRefused/Unreachable → dial refused / no route / unreachable (Recoverable, exit 1)
//	KindAuthenticationFailed        → auth rejected (Not recoverable, exit 4)
//	KindPermissionDenied            → permission denied (Recoverable, exit 4 guidance, but retry may not help)
//	KindTransferFailed              → transfer failed / partial write (Recoverable, exit 1)
//	KindHostKeyVerificationFailed   → host key mismatch (Not recoverable, exit 4)
//	KindConfiguration               → invalid config (Not recoverable, exit 2)
//	KindUnknown                     → unclassified (Recoverable, exit 1)
//
// Reference: TS-P11-04, TS-011-004, EPIC-011 §7.6, sto:local-deploy-transport AC3
type TransportErrorKind string

// Transport failure classes (EPIC-011 §7.6) — 6+1 kinds with actionable Guidance.
const (
	// KindUnknown is the zero value: an unclassified transport failure.
	KindUnknown TransportErrorKind = ""
	// KindTimeout: SSH dial or transfer timed out (Recoverable).
	KindTimeout TransportErrorKind = "timeout"
	// KindConnectionRefused: the server is not reachable / unreachable
	// (connection refused, no route to host).
	KindConnectionRefused TransportErrorKind = "connection_refused"
	// KindUnreachable is an alias for unreachable network failures
	// (no route, network unreachable). Kept distinct for AC3 taxonomy.
	KindUnreachable TransportErrorKind = "unreachable"
	// KindAuthenticationFailed: SSH authentication with the provided
	// credentials was rejected.
	KindAuthenticationFailed TransportErrorKind = "authentication_failed"
	// KindPermissionDenied: the authenticated user lacks permission for
	// the remote operation (key not authorized or filesystem denied).
	KindPermissionDenied TransportErrorKind = "permission_denied"
	// KindTransferFailed: the transfer itself failed (network error
	// during transfer, remote disk full, remote scp failure, partial write).
	KindTransferFailed TransportErrorKind = "transfer_failed"
	// KindConfiguration: the transport configuration or inputs are
	// invalid; retrying with the same inputs cannot succeed.
	KindConfiguration TransportErrorKind = "configuration"
	// KindHostKeyVerificationFailed: the server's host key could not be
	// verified against the configured known-hosts file (TD-004). The
	// host is unknown or its key changed, which can signal a
	// man-in-the-middle attack; retrying with the same inputs cannot
	// succeed.
	KindHostKeyVerificationFailed TransportErrorKind = "host_key_verification_failed"
)

// TransportError represents a failure during artifact transport.
// Transport failures are reported without mutating Runtime State
// (ADR-015, Decision 006). The Recoverable field indicates whether
// the operation can be retried.
//
// Reference: TS-P10-02 AC-3, ADR-015, Decision 006
type TransportError struct {
	// TargetID identifies which target experienced the failure.
	TargetID TargetID `json:"target_id"`

	// Reason describes the cause of the transport failure.
	Reason string `json:"reason"`

	// Recoverable indicates whether the transport operation can be
	// retried (e.g., true for transient network errors, false for
	// authentication failures).
	Recoverable bool `json:"recoverable"`

	// Kind classifies the failure for actionable guidance (TS-P11-04).
	// Empty when the failure is unclassified.
	Kind TransportErrorKind `json:"kind,omitempty"`
}

// Error implements the error interface for TransportError.
func (e *TransportError) Error() string {
	return fmt.Sprintf("transport to %s failed: %s", e.TargetID, e.Reason)
}

// Guidance returns actionable recovery guidance for the failure class,
// per EPIC-011 §7.6. Callers surface this guidance to the user so that
// common SSH failures (unreachable server, bad credentials, denied
// permission, failed transfer) always include a concrete next step.
//
// 6Kind exit code mapping (stable, AC3):
//   timeout / connection_refused / unreachable / transfer_failed / unknown → exit 1 (general, retryable)
//   configuration                                              → exit 2 (config)
//   authentication_failed / permission_denied / host_key_verification_failed → exit 4 (precondition/auth)
// Reference: TS-P11-04, TS-011-004 AC-1..AC-4, EPIC-011 §7.6, sto:local-deploy-transport AC3
func (e *TransportError) Guidance() string {
	switch e.Kind {
	case KindTimeout:
		return "Connection timed out — check network connectivity, firewall, and that the SSH server is reachable, then retry"
	case KindConnectionRefused, KindUnreachable:
		return "Check that the server address and port are correct and that the SSH service is running and reachable"
	case KindAuthenticationFailed:
		return "Check that the SSH user and key are correct and that the key is authorized on the server"
	case KindPermissionDenied:
		return "Check that the SSH key is authorized on the server and that the remote user has permission to write to the upload directory"
	case KindTransferFailed:
		return "Check network connectivity and disk space on the server, then retry the operation"
	case KindConfiguration:
		return "Check the deployment configuration and environment variables"
	case KindHostKeyVerificationFailed:
		return "The server's host key does not match the configured known_hosts file; this can indicate a man-in-the-middle attack or a changed server identity — verify the server identity, then update the known_hosts file only if the change is expected"
	default:
		return "Check the error details and retry the operation"
	}
}

// ExitCode returns the deterministic exit code for this transport failure
// (stable, automation-safe, AC3). Mapping:
//
//	KindTimeout, KindConnectionRefused, KindUnreachable, KindTransferFailed, KindUnknown → 1 (ExitCodeGeneral)
//	KindConfiguration → 2 (ExitCodeConfig)
//	KindAuthenticationFailed, KindPermissionDenied, KindHostKeyVerificationFailed → 4 (ExitCodePrecondition)
//
// Reference: sto:local-deploy-transport AC3, EPIC-011 §7.6
func (e *TransportError) ExitCode() int {
	switch e.Kind {
	case KindConfiguration:
		return 2
	case KindAuthenticationFailed, KindPermissionDenied, KindHostKeyVerificationFailed:
		return 4
	default:
		return 1
	}
}

// AllKinds returns the 6+ documented kinds for table-driven tests and docs.
func AllKinds() []TransportErrorKind {
	return []TransportErrorKind{
		KindTimeout,
		KindConnectionRefused,
		KindUnreachable,
		KindAuthenticationFailed,
		KindPermissionDenied,
		KindTransferFailed,
		KindHostKeyVerificationFailed,
		KindConfiguration,
	}
}

// Transport delivers an immutable artifact to a deployment target.
// Implementations are transport-technology-specific (SSH, HTTP, etc.)
// and must not mutate Runtime State (ADR-015, Decision 006).
//
// The Deliver method receives the complete ArtifactPayload and writes
// it to the target location. The caller is responsible for interpreting
// the TransportResult and handling any TransportError.
//
// Reference: TS-P10-02 AC-1, AC-3, AC-4, ADR-015, Decision 006
type Transport interface {
	// Deliver sends the artifact payload to the target.
	// Returns the result of the delivery or an error if transport fails.
	Deliver(payload ArtifactPayload, target Target) (*TransportResult, error)
}
