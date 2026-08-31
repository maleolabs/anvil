package spklocaldeployrace

import (
	"fmt"

	"maleolabs.com/anvil/internal/deployment"
)

// MockTarget implements deployment.Target for the spike.
// ValidateCompatibility PASS when not configured to fail — mirrors deployment model server capability check (AC2).
type MockTarget struct {
	id         deployment.TargetID
	metadata   deployment.TargetMetadata
	input      deployment.CompatibilityInput
	shouldFail bool
	failReason string
}

func NewMockTarget(id string) *MockTarget {
	tid := deployment.TargetID(id)
	return &MockTarget{
		id: tid,
		metadata: deployment.TargetMetadata{
			ID:       tid,
			Name:     "spike-local-target",
			Platform: deployment.Platform{OS: "linux", Arch: "amd64"},
		},
		input: deployment.CompatibilityInput{
			RuntimeVersion: deployment.RuntimeVersionConstraint{MinVersion: "0.0.0"},
			Platform:       deployment.Platform{OS: "linux", Arch: "amd64"},
		},
	}
}

func (m *MockTarget) ID() deployment.TargetID                           { return m.id }
func (m *MockTarget) Metadata() deployment.TargetMetadata               { return m.metadata }
func (m *MockTarget) CompatibilityInput() deployment.CompatibilityInput { return m.input }
func (m *MockTarget) ValidateCompatibility(input deployment.CompatibilityInput) error {
	if m.shouldFail {
		reason := m.failReason
		if reason == "" {
			reason = "mock target rejects all compatibility inputs"
		}
		return fmt.Errorf("%s", reason)
	}
	// Accept any input that has a non-empty version (the harness derives it from manifest).
	// This keeps Negotiate PASS for AC2 while still proving the contract was invoked.
	if input.RuntimeVersion.MinVersion == "" && input.RuntimeVersion.MaxVersion == "" {
		return fmt.Errorf("target requires version constraint")
	}
	return nil
}

// AsFailing returns a target that always fails compatibility (for negative negotiation test).
func (m *MockTarget) AsFailing(reason string) *MockTarget {
	n := *m
	n.shouldFail = true
	n.failReason = reason
	return &n
}

// FailingTarget is a convenience: always fails.
func NewFailingTarget(id, reason string) *MockTarget {
	t := NewMockTarget(id)
	t.shouldFail = true
	t.failReason = reason
	return t
}
