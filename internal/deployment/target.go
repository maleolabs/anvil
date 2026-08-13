// Package deployment defines the Deployment bounded context for Anvil.
//
// Reference: TS-P10-01, EPIC-010, ADR-015
package deployment

// TargetID is a typed string representing a deployment target identity.
//
// Reference: TS-P10-01 AC-1
type TargetID string

// String returns the string representation of the TargetID.
func (id TargetID) String() string {
	return string(id)
}

// Platform describes the operating system and architecture of a target.
// This is part of the compatibility contract — Runtime implementations
// validate their platform against this input during deployment.
//
// Reference: TS-P10-01
type Platform struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

// RuntimeVersionConstraint defines version compatibility requirements
// for a target Runtime. Both fields are optional — an empty value means
// no constraint.
//
// Reference: TS-P10-01
type RuntimeVersionConstraint struct {
	MinVersion string `json:"min_version,omitempty"`
	MaxVersion string `json:"max_version,omitempty"`
}

// TargetMetadata describes a deployment target's identity and descriptive
// characteristics. It is independent from Runtime filesystem layout per
// ADR-015 — the Deployment domain never reads Runtime Registry or State
// internals.
//
// Reference: TS-P10-01 AC-2, ADR-015
type TargetMetadata struct {
	ID       TargetID `json:"id"`
	Name     string   `json:"name"`
	Address  string   `json:"address,omitempty"`
	Platform Platform `json:"platform,omitempty"`
}

// CompatibilityInput defines what a deployment requires from a target.
// It captures runtime version constraints and platform requirements that
// a Target must satisfy for a deployment to proceed.
//
// Reference: TS-P10-01 AC-1, AC-3
type CompatibilityInput struct {
	RuntimeVersion RuntimeVersionConstraint `json:"runtime_version"`
	Platform       Platform                 `json:"platform"`
}

// Target represents a deployment target that can receive artifact
// deployments. It publishes identity, compatibility requirements,
// and connection metadata without exposing Runtime Registry or
// State internals (ADR-015).
//
// Implementations live in the Server Runtime domain and satisfy
// this contract so that Deployment can pass target information to
// Runtime through a published interface.
//
// Reference: TS-P10-01 AC-3, AC-4, ADR-015
type Target interface {
	// ID returns the unique identity of this deployment target.
	ID() TargetID

	// Metadata returns the descriptive metadata for this target.
	Metadata() TargetMetadata

	// CompatibilityInput returns the compatibility requirements
	// that the target enforces for deployments. Deployment uses
	// this to negotiate whether a deployment can proceed.
	CompatibilityInput() CompatibilityInput

	// ValidateCompatibility checks whether the given input satisfies
	// the target's compatibility requirements. Returns nil if the
	// input is compatible, or an error describing the incompatibility.
	ValidateCompatibility(input CompatibilityInput) error
}
