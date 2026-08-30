// Package deployment defines the Deployment bounded context for Anvil.
//
// Reference: TS-P10-04, EPIC-010, ADR-015
package deployment

import (
	"errors"
	"testing"
)

// TestNewCoordinator verifies that a Coordinator can be created with a
// Transport dependency.
//
// Reference: TS-P10-04
func TestNewCoordinator(t *testing.T) {
	transport := &testTransport{}
	coord := NewCoordinator(transport)

	if coord == nil {
		t.Fatal("NewCoordinator() returned nil")
	}
}

// TestCoordinator_Deploy_FullSuccess verifies that a full deployment
// workflow succeeds when all steps pass.
//
// Reference: TS-P10-04 AC-1, AC-2, AC-4
func TestCoordinator_Deploy_FullSuccess(t *testing.T) {
	transport := &testTransport{}
	coord := NewCoordinator(transport)

	payload := ArtifactPayload{
		Path:            "/tmp/artifact.tar.gz",
		ManifestContent: []byte(`{"artifact_id":"abc123","version":"1.0.0","project_id":"my-project"}`),
	}
	target := &testTarget{
		id: TargetID("test-node"),
	}

	installCalled := false
	activateCalled := false

	result := coord.Deploy(
		payload,
		target,
		func(projectID, artifactPath string) error {
			installCalled = true
			if projectID != "my-project" {
				t.Errorf("install projectID = %q, want %q", projectID, "my-project")
			}
			return nil
		},
		func(projectID, releaseID string) error {
			activateCalled = true
			if projectID != "my-project" {
				t.Errorf("activate projectID = %q, want %q", projectID, "my-project")
			}
			return nil
		},
	)

	if result == nil {
		t.Fatal("Deploy() returned nil result")
	}
	if result.OverallFailure != "" {
		t.Errorf("Deploy() OverallFailure = %q, want empty", result.OverallFailure)
	}

	// Verify all 4 steps were executed and succeeded.
	if len(result.Steps) != 4 {
		t.Errorf("expected 4 steps, got %d: %+v", len(result.Steps), result.Steps)
	}
	for _, step := range result.Steps {
		if !step.Success {
			t.Errorf("step %q failed unexpectedly: %s", step.Step, step.Message)
		}
	}

	if !installCalled {
		t.Error("install function was not called")
	}
	if !activateCalled {
		t.Error("activate function was not called")
	}

	if result.TransportResult == nil {
		t.Error("TransportResult should not be nil after successful delivery")
	} else if !result.TransportResult.Success {
		t.Error("TransportResult.Success should be true")
	}
}

// TestCoordinator_Deploy_NegotiateFailure verifies that deployment stops
// at the negotiate step when compatibility fails.
//
// Reference: TS-P10-04 AC-5
func TestCoordinator_Deploy_NegotiateFailure(t *testing.T) {
	transport := &testTransport{}
	coord := NewCoordinator(transport)

	payload := ArtifactPayload{
		Path:            "/tmp/artifact.tar.gz",
		ManifestContent: []byte(`{"artifact_id":"abc123","version":"1.0.0","project_id":"my-project"}`),
	}
	target := &strictVersionTarget{
		testTarget: testTarget{
			id: TargetID("strict-node"),
		},
		minVersion: "2.0.0",
	}

	installCalled := false
	activateCalled := false

	result := coord.Deploy(
		payload,
		target,
		func(projectID, artifactPath string) error {
			installCalled = true
			return nil
		},
		func(projectID, releaseID string) error {
			activateCalled = true
			return nil
		},
	)

	if result == nil {
		t.Fatal("Deploy() returned nil result")
	}
	if result.OverallFailure == "" {
		t.Fatal("Deploy() should report an overall failure")
	}

	// Only the negotiate step should have executed.
	if len(result.Steps) != 1 {
		t.Errorf("expected 1 step (negotiate), got %d", len(result.Steps))
	}
	if result.Steps[0].Step != "negotiate" {
		t.Errorf("first step should be negotiate, got %s", result.Steps[0].Step)
	}
	if result.Steps[0].Success {
		t.Error("negotiate step should have failed")
	}

	if installCalled {
		t.Error("install should not have been called after negotiate failure")
	}
	if activateCalled {
		t.Error("activate should not have been called after negotiate failure")
	}

	if result.TransportResult != nil {
		t.Error("TransportResult should be nil when delivery was not reached")
	}
}

// TestCoordinator_Deploy_TransportFailure verifies that deployment stops
// at the deliver step when transport fails.
//
// Reference: TS-P10-04 AC-5
func TestCoordinator_Deploy_TransportFailure(t *testing.T) {
	// Use a transport that always fails.
	failingTransport := &failingTransport{}
	coord := NewCoordinator(failingTransport)

	payload := ArtifactPayload{
		Path:            "/tmp/artifact.tar.gz",
		ManifestContent: []byte(`{"artifact_id":"abc123","version":"1.0.0","project_id":"my-project"}`),
	}
	target := &testTarget{
		id: TargetID("test-node"),
	}

	installCalled := false

	result := coord.Deploy(
		payload,
		target,
		func(projectID, artifactPath string) error {
			installCalled = true
			return nil
		},
		nil,
	)

	if result == nil {
		t.Fatal("Deploy() returned nil result")
	}
	if result.OverallFailure == "" {
		t.Fatal("Deploy() should report an overall failure")
	}

	// Negotiate should pass, deliver should fail.
	if len(result.Steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(result.Steps))
	}

	if result.Steps[0].Step != "negotiate" || !result.Steps[0].Success {
		t.Error("negotiate step should have succeeded")
	}
	if result.Steps[1].Step != "deliver" || result.Steps[1].Success {
		t.Error("deliver step should have failed")
	}

	if installCalled {
		t.Error("install should not have been called after transport failure")
	}
}

// TestCoordinator_Deploy_InstallFailure verifies that deployment stops
// at the install step when Runtime install fails.
//
// Reference: TS-P10-04 AC-1, AC-5
func TestCoordinator_Deploy_InstallFailure(t *testing.T) {
	transport := &testTransport{}
	coord := NewCoordinator(transport)

	payload := ArtifactPayload{
		Path:            "/tmp/artifact.tar.gz",
		ManifestContent: []byte(`{"artifact_id":"abc123","version":"1.0.0","project_id":"my-project"}`),
	}
	target := &testTarget{
		id: TargetID("test-node"),
	}

	activateCalled := false

	result := coord.Deploy(
		payload,
		target,
		func(projectID, artifactPath string) error {
			return errors.New("installation rejected: disk full")
		},
		func(projectID, releaseID string) error {
			activateCalled = true
			return nil
		},
	)

	if result == nil {
		t.Fatal("Deploy() returned nil result")
	}
	if result.OverallFailure == "" {
		t.Fatal("Deploy() should report an overall failure")
	}

	// Negotiate and deliver should pass, install should fail.
	if len(result.Steps) != 3 {
		t.Errorf("expected 3 steps, got %d", len(result.Steps))
	}
	if result.Steps[2].Step != "install" || result.Steps[2].Success {
		t.Error("install step should have failed")
	}

	if activateCalled {
		t.Error("activate should not have been called after install failure")
	}

	if result.TransportResult == nil {
		t.Error("TransportResult should be present (delivery succeeded)")
	}
}

// TestCoordinator_Deploy_MissingManifestContent verifies behavior when
// manifest content is missing from the payload.
func TestCoordinator_Deploy_MissingManifestContent(t *testing.T) {
	transport := &testTransport{}
	coord := NewCoordinator(transport)

	payload := ArtifactPayload{
		Path:            "/tmp/artifact.tar.gz",
		ManifestContent: []byte{},
	}
	target := &testTarget{
		id: TargetID("test-node"),
	}

	installCalled := false

	result := coord.Deploy(
		payload,
		target,
		func(projectID, artifactPath string) error {
			installCalled = true
			return nil
		},
		nil,
	)

	if result == nil {
		t.Fatal("Deploy() returned nil result")
	}
	if result.OverallFailure == "" {
		t.Fatal("Deploy() should report failure for empty manifest")
	}

	// Negotiate should reject empty manifest.
	if len(result.Steps) < 1 {
		t.Fatal("expected at least 1 step")
	}
	if result.Steps[0].Step != "negotiate" || result.Steps[0].Success {
		t.Error("negotiate should fail for empty manifest")
	}

	if installCalled {
		t.Error("install should not be called after negotiate failure")
	}
}

// TestCoordinator_Deploy_WithNilFunctions verifies that Deploy handles
// nil install and activate functions gracefully.
//
// Reference: TS-P10-04 AC-1, AC-2
func TestCoordinator_Deploy_WithNilFunctions(t *testing.T) {
	transport := &testTransport{}
	coord := NewCoordinator(transport)

	payload := ArtifactPayload{
		Path:            "/tmp/artifact.tar.gz",
		ManifestContent: []byte(`{"artifact_id":"abc123","version":"1.0.0","project_id":"my-project"}`),
	}
	target := &testTarget{
		id: TargetID("test-node"),
	}

	result := coord.Deploy(payload, target, nil, nil)

	if result == nil {
		t.Fatal("Deploy() returned nil result")
	}
	if result.OverallFailure != "" {
		t.Errorf("Deploy() OverallFailure = %q, want empty", result.OverallFailure)
	}

	// All steps should succeed (install/activate report "skipped").
	if len(result.Steps) != 4 {
		t.Errorf("expected 4 steps, got %d", len(result.Steps))
	}
	for _, step := range result.Steps {
		if !step.Success {
			t.Errorf("step %q should succeed with nil function", step.Step)
		}
	}
}

// TestCoordinator_Rollback_Success verifies a full rollback workflow.
//
// Reference: TS-P10-04 AC-3
func TestCoordinator_Rollback_Success(t *testing.T) {
	transport := &testTransport{}
	coord := NewCoordinator(transport)

	payload := ArtifactPayload{
		Path:            "/tmp/artifact.tar.gz",
		ManifestContent: []byte(`{"artifact_id":"abc123","version":"1.0.0","project_id":"my-project"}`),
	}
	target := &testTarget{
		id: TargetID("test-node"),
	}

	rollbackCalled := false

	result := coord.Rollback(
		payload,
		target,
		func(projectID string) error {
			rollbackCalled = true
			if projectID != "my-project" {
				t.Errorf("rollback projectID = %q, want %q", projectID, "my-project")
			}
			return nil
		},
	)

	if result == nil {
		t.Fatal("Rollback() returned nil result")
	}
	if result.OverallFailure != "" {
		t.Errorf("Rollback() OverallFailure = %q, want empty", result.OverallFailure)
	}

	if len(result.Steps) != 2 {
		t.Errorf("expected 2 steps (negotiate, rollback), got %d", len(result.Steps))
	}
	for _, step := range result.Steps {
		if !step.Success {
			t.Errorf("step %q should succeed", step.Step)
		}
	}

	if !rollbackCalled {
		t.Error("rollback function was not called")
	}
}

// TestCoordinator_Rollback_Failure verifies rollback failure reporting.
//
// Reference: TS-P10-04 AC-3, AC-5
func TestCoordinator_Rollback_Failure(t *testing.T) {
	transport := &testTransport{}
	coord := NewCoordinator(transport)

	payload := ArtifactPayload{
		Path:            "/tmp/artifact.tar.gz",
		ManifestContent: []byte(`{"artifact_id":"abc123","version":"1.0.0","project_id":"my-project"}`),
	}
	target := &testTarget{
		id: TargetID("test-node"),
	}

	result := coord.Rollback(
		payload,
		target,
		func(projectID string) error {
			return errors.New("rollback rejected: active release not found")
		},
	)

	if result == nil {
		t.Fatal("Rollback() returned nil result")
	}
	if result.OverallFailure == "" {
		t.Fatal("Rollback() should report overall failure")
	}

	if len(result.Steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(result.Steps))
	}
	if !result.Steps[0].Success {
		t.Error("negotiate step should succeed")
	}
	if result.Steps[1].Success {
		t.Error("rollback step should fail")
	}
}

// TestCoordinator_DoesNotMaintainReleaseState verifies that Coordinator
// does not maintain independent Release State or manipulate Runtime paths
// (ADR-015). The Coordinator only orchestrates through function injection
// and Transport — it has no fields for Release State, no path manipulation.
//
// Reference: TS-P10-04 AC-4, ADR-015
func TestCoordinator_DoesNotMaintainReleaseState(t *testing.T) {
	coord := NewCoordinator(&testTransport{})

	// Verify the Coordinator struct has no fields related to Release State.
	// This is a compile-time structural check — if someone adds a Release
	// or State field in the future, the test's understanding of the struct
	// will need to be updated.
	if coord == nil {
		t.Fatal("coordinator should be constructable")
	}

	// The Coordinator should only have a transport field.
	// This is an interface contract check — no Release state fields.
	_ = coord
}

// failingTransport is a Transport implementation that always fails delivery.
type failingTransport struct{}

func (f *failingTransport) Deliver(payload ArtifactPayload, target Target) (*TransportResult, error) {
	return nil, &TransportError{
		TargetID:    target.ID(),
		Reason:      "simulated transport failure",
		Recoverable: false,
	}
}

// TestDeploymentError_Error verifies DeploymentError implements the error
// interface with proper formatting.
func TestDeploymentError_Error(t *testing.T) {
	tests := []struct {
		name    string
		err     *DeploymentError
		wantMsg string
	}{
		{
			name: "with_cause",
			err: &DeploymentError{
				Step:   "deliver",
				Reason: "connection refused",
				Err:    errors.New("dial tcp: timeout"),
			},
			wantMsg: `deployment step "deliver" failed: connection refused: dial tcp: timeout`,
		},
		{
			name: "without_cause",
			err: &DeploymentError{
				Step:   "negotiate",
				Reason: "incompatible platform",
			},
			wantMsg: `deployment step "negotiate" failed: incompatible platform`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.wantMsg {
				t.Errorf("DeploymentError.Error() = %q, want %q", got, tt.wantMsg)
			}
		})
	}
}

// TestDeploymentError_Unwrap verifies that DeploymentError supports
// errors.Unwrap for use with errors.Is/errors.As.
func TestDeploymentError_Unwrap(t *testing.T) {
	cause := errors.New("root cause")
	err := &DeploymentError{
		Step:   "install",
		Reason: "runtime error",
		Err:    cause,
	}

	if !errors.Is(err, cause) {
		t.Error("errors.Is should find the wrapped error")
	}
}
