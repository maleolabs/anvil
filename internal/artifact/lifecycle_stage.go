// Package artifact defines the Anvil artifact directory layout and
// packaging primitives.
//
// Reference: TS-P3-09, ADR-004 §7, ADR-006 §5.3, EPIC-003
package artifact

// LifecycleStage represents a stage in the Artifact lifecycle.
//
// An Artifact progresses through a defined set of stages from Created
// (initial) through to Removed (terminal). Each stage has specific valid
// transitions that are enforced by the LifecycleStateMachine.
//
// Reference: ADR-004 §7, TS-P3-09
type LifecycleStage int

const (
	// StageCreated is the initial stage for any newly packaged Artifact.
	// The Artifact has been packaged from application source and exists
	// as a distributable unit with content and metadata.
	// Valid transitions: Verified, Removed
	StageCreated LifecycleStage = iota

	// StageVerified indicates the Artifact has passed integrity verification.
	// Its content matches its metadata and all configured verification checks
	// have passed.
	// Valid transitions: Registered, Removed
	StageVerified

	// StageRegistered indicates the Artifact is recorded in the Artifact
	// registry and is discoverable by identity. Registration makes the
	// Artifact available to be referenced by Releases.
	// Valid transitions: Referenced, Archived, Removed
	StageRegistered

	// StageReferenced indicates one or more Releases reference this Artifact.
	// The Artifact is actively used in the deployment process.
	// Valid transitions: Consumed, Archived, Removed
	StageReferenced

	// StageConsumed indicates the Artifact has been consumed by at least
	// one Runtime and its content has been extracted and activated.
	// Valid transitions: Archived, Removed
	StageConsumed

	// StageArchived indicates the Artifact is preserved for historical
	// reference but is no longer available for creating new Releases or
	// activating on Runtimes.
	// Valid transitions: Removed
	StageArchived

	// StageRemoved indicates the Artifact has been deleted from the system.
	// Only its metadata record may remain. This is a terminal stage — no
	// further transitions are allowed.
	// Valid transitions: none
	StageRemoved
)

// String returns a human-readable lowercase name for the lifecycle stage.
func (s LifecycleStage) String() string {
	switch s {
	case StageCreated:
		return "created"
	case StageVerified:
		return "verified"
	case StageRegistered:
		return "registered"
	case StageReferenced:
		return "referenced"
	case StageConsumed:
		return "consumed"
	case StageArchived:
		return "archived"
	case StageRemoved:
		return "removed"
	default:
		return "unknown"
	}
}

// artifactTransitionMap defines the allowed transitions from each Artifact
// lifecycle stage.
//
// Reference: ADR-004 §7, TS-P3-09
var artifactTransitionMap = map[LifecycleStage][]LifecycleStage{
	StageCreated:    {StageVerified, StageRemoved},
	StageVerified:   {StageRegistered, StageRemoved},
	StageRegistered: {StageReferenced, StageArchived, StageRemoved},
	StageReferenced: {StageConsumed, StageArchived, StageRemoved},
	StageConsumed:   {StageArchived, StageRemoved},
	StageArchived:   {StageRemoved},
	StageRemoved:    nil,
}

// canTransitionTo reports whether a transition from src to target is valid
// according to the Artifact lifecycle stage transition rules (ADR-004 §7).
func canTransitionTo(src, target LifecycleStage) bool {
	allowed, ok := artifactTransitionMap[src]
	if !ok || allowed == nil {
		return false
	}
	for _, s := range allowed {
		if s == target {
			return true
		}
	}
	return false
}
