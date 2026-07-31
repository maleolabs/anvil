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
}

// Error implements the error interface for TransportError.
func (e *TransportError) Error() string {
	return fmt.Sprintf("transport to %s failed: %s", e.TargetID, e.Reason)
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
