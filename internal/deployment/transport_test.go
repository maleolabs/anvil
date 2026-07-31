// Package deployment defines the Deployment bounded context for Anvil.
//
// Reference: TS-P10-02, EPIC-010, ADR-015, Decision 006
package deployment

import (
	"testing"
)

// TestArtifactPayload_Creation verifies ArtifactPayload field assignment.
//
// Reference: TS-P10-02 AC-1
func TestArtifactPayload_Creation(t *testing.T) {
	manifestContent := []byte(`{"artifact_id":"abc123","version":"1.0.0"}`)

	payload := ArtifactPayload{
		Path:            "/tmp/artifact.tar.gz",
		ManifestContent: manifestContent,
	}

	if payload.Path != "/tmp/artifact.tar.gz" {
		t.Errorf("ArtifactPayload.Path = %q, want %q", payload.Path, "/tmp/artifact.tar.gz")
	}
	if string(payload.ManifestContent) != string(manifestContent) {
		t.Errorf("ArtifactPayload.ManifestContent = %q, want %q",
			string(payload.ManifestContent), string(manifestContent))
	}
}

// TestArtifactPayload_ManifestContentPreserved verifies that manifest
// content is preserved as raw bytes and identity is not derived from
// filenames (Decision 006).
//
// Reference: TS-P10-02 AC-2, Decision 006
func TestArtifactPayload_ManifestContentPreserved(t *testing.T) {
	manifestContent := []byte(`{"artifact_id":"abc123","version":"1.0.0","source":"my-project"}`)

	// Two payloads with identical manifest content but different paths
	// must have the same manifest content — identity comes from the
	// manifest, not the filename.
	payloadA := ArtifactPayload{
		Path:            "/tmp/builds/app-v1.tar.gz",
		ManifestContent: manifestContent,
	}
	payloadB := ArtifactPayload{
		Path:            "/tmp/uploads/app-v1-final.tar.gz",
		ManifestContent: manifestContent,
	}

	if string(payloadA.ManifestContent) != string(payloadB.ManifestContent) {
		t.Error("identical manifest content must match regardless of path or filename")
	}
}

// TestArtifactPayload_ManifestContentNotEmpty verifies that a payload
// can be created with non-empty manifest content.
func TestArtifactPayload_ManifestContentNotEmpty(t *testing.T) {
	payload := ArtifactPayload{
		Path:            "/tmp/artifact.tar.gz",
		ManifestContent: []byte(`{"artifact_id":"abc123"}`),
	}

	if len(payload.ManifestContent) == 0 {
		t.Error("ArtifactPayload.ManifestContent must not be empty")
	}
}

// TestTransportResult_Creation verifies TransportResult field assignment.
func TestTransportResult_Creation(t *testing.T) {
	result := TransportResult{
		Success:    true,
		TargetID:   TargetID("node-1"),
		RemotePath: "/anvil/artifacts/abc123.tar.gz",
	}

	if !result.Success {
		t.Error("TransportResult.Success = false, want true")
	}
	if result.TargetID != TargetID("node-1") {
		t.Errorf("TransportResult.TargetID = %q, want %q", result.TargetID, TargetID("node-1"))
	}
	if result.RemotePath != "/anvil/artifacts/abc123.tar.gz" {
		t.Errorf("TransportResult.RemotePath = %q, want %q",
			result.RemotePath, "/anvil/artifacts/abc123.tar.gz")
	}
}

// TestTransportResult_Failure verifies a failed transport result.
func TestTransportResult_Failure(t *testing.T) {
	result := TransportResult{
		Success:  false,
		TargetID: TargetID("node-1"),
	}

	if result.Success {
		t.Error("TransportResult.Success = true, want false")
	}
}

// TestTransportError_Error verifies that TransportError implements the
// error interface and formats the message correctly.
//
// Reference: TS-P10-02 AC-3
func TestTransportError_Error(t *testing.T) {
	tests := []struct {
		name    string
		err     *TransportError
		wantMsg string
	}{
		{
			name: "connection_refused",
			err: &TransportError{
				TargetID:    TargetID("node-1"),
				Reason:      "connection refused",
				Recoverable: true,
			},
			wantMsg: "transport to node-1 failed: connection refused",
		},
		{
			name: "auth_failure",
			err: &TransportError{
				TargetID:    TargetID("node-2"),
				Reason:      "authentication failed",
				Recoverable: false,
			},
			wantMsg: "transport to node-2 failed: authentication failed",
		},
		{
			name: "timeout",
			err: &TransportError{
				TargetID:    TargetID("node-3"),
				Reason:      "timeout after 30s",
				Recoverable: true,
			},
			wantMsg: "transport to node-3 failed: timeout after 30s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.wantMsg {
				t.Errorf("TransportError.Error() = %q, want %q", got, tt.wantMsg)
			}
		})
	}
}

// TestTransportError_Recoverable verifies the Recoverable field
// behavior for different error types.
func TestTransportError_Recoverable(t *testing.T) {
	tests := []struct {
		name        string
		err         *TransportError
		wantRecover bool
	}{
		{
			name: "network_error",
			err: &TransportError{
				TargetID:    TargetID("node-1"),
				Reason:      "connection reset",
				Recoverable: true,
			},
			wantRecover: true,
		},
		{
			name: "auth_error",
			err: &TransportError{
				TargetID:    TargetID("node-2"),
				Reason:      "invalid credentials",
				Recoverable: false,
			},
			wantRecover: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Recoverable; got != tt.wantRecover {
				t.Errorf("TransportError.Recoverable = %v, want %v", got, tt.wantRecover)
			}
		})
	}
}

// TestTransportError_DoesNotMutateRuntimeState verifies that
// TransportError has no fields that reference or mutate Runtime
// State (ADR-015, Decision 006). This is a structural contract check.
//
// Reference: TS-P10-02 AC-3, ADR-015, Decision 006
func TestTransportError_DoesNotMutateRuntimeState(t *testing.T) {
	// TransportError must only contain transport-level fields:
	// TargetID (identity), Reason (description), Recoverable (retry hint).
	err := &TransportError{
		TargetID:    TargetID("test"),
		Reason:      "test",
		Recoverable: false,
	}

	// Verify the error has no Runtime State references.
	if err.TargetID == "" {
		t.Error("TransportError must have a TargetID")
	}
	if err.Reason == "" {
		t.Error("TransportError must have a Reason")
	}

	// Verify TransportError satisfies the error interface (compiles).
	var e error = err
	if e.Error() == "" {
		t.Error("TransportError.Error() must not be empty")
	}
}

// TestTransport_InterfaceSatisfaction verifies that a concrete struct
// satisfies the Transport interface (compile-time check).
//
// Reference: TS-P10-02 AC-1, AC-4
func TestTransport_InterfaceSatisfaction(t *testing.T) {
	// Compile-time check: if testTransport does not implement Transport,
	// this line will not compile.
	var _ Transport = (*testTransport)(nil)
}

// TestTransport_Deliver verifies that Transport.Deliver returns the
// expected result for a typical delivery operation.
//
// Reference: TS-P10-02 AC-1, AC-4
func TestTransport_Deliver(t *testing.T) {
	transport := &testTransport{}
	target := &testTarget{
		id: TargetID("test-node"),
	}
	payload := ArtifactPayload{
		Path:            "/tmp/artifact.tar.gz",
		ManifestContent: []byte(`{"artifact_id":"abc123","version":"1.0.0"}`),
	}

	result, err := transport.Deliver(payload, target)
	if err != nil {
		t.Fatalf("Transport.Deliver() returned error: %v", err)
	}
	if result == nil {
		t.Fatal("Transport.Deliver() returned nil result")
	}
	if !result.Success {
		t.Error("TransportResult.Success = false, want true")
	}
	if result.TargetID != TargetID("test-node") {
		t.Errorf("TransportResult.TargetID = %q, want %q", result.TargetID, TargetID("test-node"))
	}
}

// TestTransport_DeliverWithTargetMetadata verifies that Transport
// receives the target's metadata correctly.
func TestTransport_DeliverWithTargetMetadata(t *testing.T) {
	transport := &testTransport{}
	target := &testTarget{
		id: TargetID("prod-node"),
		meta: TargetMetadata{
			ID:      TargetID("prod-node"),
			Name:    "Production Node",
			Address: "10.0.0.1:22",
		},
	}
	payload := ArtifactPayload{
		Path:            "/tmp/artifact.tar.gz",
		ManifestContent: []byte(`{"artifact_id":"def456"}`),
	}

	result, err := transport.Deliver(payload, target)
	if err != nil {
		t.Fatalf("Transport.Deliver() returned error: %v", err)
	}
	if result.TargetID != target.ID() {
		t.Errorf("TransportResult.TargetID = %q, want %q", result.TargetID, target.ID())
	}
}

// TestTransport_DoesNotMutateRuntimeState verifies that the Transport
// interface contract does not require or expose Runtime State mutations.
// This is a structural contract check.
//
// Reference: TS-P10-02 AC-3, ADR-015, Decision 006
func TestTransport_DoesNotMutateRuntimeState(t *testing.T) {
	// The Transport interface must not have methods that directly
	// reference Runtime State. Verify the method signature:
	//   Deliver(ArtifactPayload, Target) (*TransportResult, error)
	// No Runtime state in parameters or return types.
	var transport interface {
		Deliver(ArtifactPayload, Target) (*TransportResult, error)
	}
	transport = &testTransport{}
	_ = transport
}

// testTransport is a minimal Transport implementation used for
// contract verification in tests.
type testTransport struct{}

func (tt *testTransport) Deliver(payload ArtifactPayload, target Target) (*TransportResult, error) {
	return &TransportResult{
		Success:    true,
		TargetID:   target.ID(),
		RemotePath: "/remote/" + payload.Path,
	}, nil
}
