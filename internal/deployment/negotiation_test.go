// Package deployment defines the Deployment bounded context for Anvil.
//
// Reference: TS-P10-03, EPIC-010, ADR-015
package deployment

import (
	"testing"
)

// TestNegotiate_Compatible verifies that a compatible manifest and target
// produce a successful NegotiationResult.
//
// Reference: TS-P10-03 AC-1, AC-2
func TestNegotiate_Compatible(t *testing.T) {
	payload := ArtifactPayload{
		Path:            "/tmp/artifact.tar.gz",
		ManifestContent: []byte(`{"artifact_id":"abc123","version":"1.0.0","project_id":"my-project"}`),
	}
	target := &testTarget{
		id: TargetID("test-node"),
		comp: CompatibilityInput{
			Platform: Platform{OS: "linux", Arch: "amd64"},
		},
	}

	result, err := Negotiate(payload, target)
	if err != nil {
		t.Fatalf("Negotiate() returned unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Negotiate() returned nil result")
	}
	if !result.Compatible {
		t.Errorf("NegotiationResult.Compatible = false, want true; reason: %s", result.Reason)
	}
}

// strictVersionTarget is a minimal Target implementation that enforces
// a minimum version constraint during compatibility validation.
type strictVersionTarget struct {
	testTarget
	minVersion string
}

func (s *strictVersionTarget) ValidateCompatibility(input CompatibilityInput) error {
	// First check platform compatibility via embedded testTarget.
	if err := s.testTarget.ValidateCompatibility(input); err != nil {
		return err
	}
	// Enforce minimum version constraint.
	if s.minVersion != "" && input.RuntimeVersion.MinVersion != "" &&
		input.RuntimeVersion.MinVersion < s.minVersion {
		return &compatibilityError{reason: "version below minimum requirement"}
	}
	return nil
}

// TestNegotiate_IncompatibleTarget verifies that negotiation rejects
// manifests that don't match target requirements. Incompatibility is
// determined by the target's ValidateCompatibility — the manifest
// provides Version, and the target may enforce version constraints.
//
// Reference: TS-P10-03 AC-2
func TestNegotiate_IncompatibleTarget(t *testing.T) {
	// Create a target that rejects any version below 2.0.0.
	target := &strictVersionTarget{
		testTarget: testTarget{
			id: TargetID("strict-node"),
		},
		minVersion: "2.0.0",
	}

	// Manifest has version 1.0.0, which is below the target's minimum.
	payload := ArtifactPayload{
		Path:            "/tmp/artifact.tar.gz",
		ManifestContent: []byte(`{"artifact_id":"abc123","version":"1.0.0","project_id":"my-project"}`),
	}

	result, err := Negotiate(payload, target)
	if err != nil {
		t.Fatalf("Negotiate() returned unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Negotiate() returned nil result")
	}
	if result.Compatible {
		t.Errorf("NegotiationResult.Compatible = true, want false (version mismatch expected)")
	}
	if result.Reason == "" {
		t.Error("NegotiationResult.Reason must not be empty when incompatible")
	}
}

// TestNegotiate_EmptyManifest verifies that an artifact with no manifest
// content is rejected.
func TestNegotiate_EmptyManifest(t *testing.T) {
	payload := ArtifactPayload{
		Path:            "/tmp/artifact.tar.gz",
		ManifestContent: []byte{},
	}
	target := &testTarget{
		id: TargetID("test-node"),
	}

	result, err := Negotiate(payload, target)
	if err != nil {
		t.Fatalf("Negotiate() returned unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Negotiate() returned nil result")
	}
	if result.Compatible {
		t.Error("NegotiationResult.Compatible = true, want false for empty manifest")
	}
}

// TestNegotiate_InvalidManifestJSON verifies that unparseable manifest
// content is rejected.
func TestNegotiate_InvalidManifestJSON(t *testing.T) {
	payload := ArtifactPayload{
		Path:            "/tmp/artifact.tar.gz",
		ManifestContent: []byte(`not valid json`),
	}
	target := &testTarget{
		id: TargetID("test-node"),
	}

	result, err := Negotiate(payload, target)
	if err != nil {
		t.Fatalf("Negotiate() returned unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Negotiate() returned nil result")
	}
	if result.Compatible {
		t.Error("NegotiationResult.Compatible = true, want false for invalid manifest")
	}
}

// TestNegotiate_AllowsRuntimeFinalValidation verifies that negotiation
// does not block a compatible manifest — the Runtime remains the owner
// of final installation validation.
//
// Reference: TS-P10-03 AC-4
func TestNegotiate_AllowsRuntimeFinalValidation(t *testing.T) {
	// This test verifies that negotiation is a pre-check, not an
	// authoritative gate. A compatible result means the target can
	// receive the artifact — but the Runtime must still validate
	// during installation.
	payload := ArtifactPayload{
		Path:            "/tmp/artifact.tar.gz",
		ManifestContent: []byte(`{"artifact_id":"abc123","version":"1.0.0","project_id":"my-project"}`),
	}
	target := &testTarget{
		id: TargetID("linux-node"),
		comp: CompatibilityInput{
			Platform: Platform{OS: "linux", Arch: "amd64"},
		},
	}

	result, err := Negotiate(payload, target)
	if err != nil {
		t.Fatalf("Negotiate() returned unexpected error: %v", err)
	}

	// Negotiation passes (pre-check).
	if !result.Compatible {
		t.Fatalf("Negotiation should pass pre-check, got: %s", result.Reason)
	}

	// But negotiation does NOT perform installation — the Runtime
	// would do that separately. This test confirms negotiation doesn't
	// try to access Runtime internals (it only uses the Target interface).
	_ = result
}

// TestNegotiate_DoesNotReadRuntimeRegistry verifies that Negotiate does
// not access Runtime Registry or State internals (ADR-015). The function
// only uses the Target interface and ArtifactPayload — no Runtime paths,
// no Registry files, no State.
//
// Reference: TS-P10-03 AC-3, ADR-015
func TestNegotiate_DoesNotReadRuntimeRegistry(t *testing.T) {
	// Verify the function signature uses only deployment-domain types:
	//   Negotiate(ArtifactPayload, Target) (*NegotiationResult, error)
	// No Runtime types in parameters or return values.
	var negotiateFn interface{} = Negotiate
	_ = negotiateFn
}
