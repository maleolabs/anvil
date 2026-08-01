// Package deployment defines the Deployment bounded context for Anvil.
//
// Reference: TS-P10-03, EPIC-010, ADR-015
package deployment

import (
	"encoding/json"
	"fmt"

	"maleolabs.com/anvil/internal/artifact"
)

// NegotiationResult reports whether a deployment target is compatible with
// an artifact's metadata. It is a pre-check result — the Runtime remains
// the owner of final installation validation (ADR-015).
//
// Reference: TS-P10-03 AC-2, AC-4
type NegotiationResult struct {
	// Compatible indicates whether the target can receive the artifact.
	Compatible bool `json:"compatible"`

	// Reason describes why the negotiation passed or failed.
	Reason string `json:"reason"`
}

// Negotiate checks whether a deployment target is compatible with the given
// artifact payload. It reads compatibility metadata from the artifact's
// manifest and validates it against the target's compatibility requirements.
//
// The negotiation sequence:
//  1. Parse manifest content from the ArtifactPayload.
//  2. Derive CompatibilityInput from manifest fields (version, project ID).
//  3. Call target.ValidateCompatibility(input) using the existing contract.
//  4. Return a NegotiationResult indicating pass/fail.
//
// Negotiation does NOT read Runtime Registry or State internals (ADR-015).
// The Runtime remains the owner of final installation validation — this is
// a pre-check, not an authoritative gate.
//
// Reference: TS-P10-03 AC-1, AC-2, AC-3, AC-4, ADR-015
func Negotiate(payload ArtifactPayload, target Target) (*NegotiationResult, error) {
	// Step 1: Parse manifest content from the ArtifactPayload.
	if len(payload.ManifestContent) == 0 {
		return &NegotiationResult{
			Compatible: false,
			Reason:     "artifact payload has no manifest content",
		}, nil
	}

	var manifest artifact.Manifest
	if err := json.Unmarshal(payload.ManifestContent, &manifest); err != nil {
		return &NegotiationResult{
			Compatible: false,
			Reason:     fmt.Sprintf("invalid manifest JSON: %v", err),
		}, nil
	}

	// Step 2: Derive CompatibilityInput from manifest fields.
	// The manifest carries Version and ProjectID which are the primary
	// compatibility signals. Platform-level constraints are enforced
	// by the Runtime as final validation.
	input := CompatibilityInput{
		RuntimeVersion: RuntimeVersionConstraint{
			MinVersion: manifest.Version,
			MaxVersion: manifest.Version,
		},
	}

	// Step 3: Call target.ValidateCompatibility(input) using the existing
	// contract. This delegates the actual compatibility check to the target
	// implementation — the Deployment domain only facilitates the negotiation.
	if err := target.ValidateCompatibility(input); err != nil {
		return &NegotiationResult{
			Compatible: false,
			Reason:     fmt.Sprintf("target rejected compatibility: %v", err),
		}, nil
	}

	// Step 4: Return a successful NegotiationResult.
	return &NegotiationResult{
		Compatible: true,
		Reason:     "target is compatible with artifact",
	}, nil
}
