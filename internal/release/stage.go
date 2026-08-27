// Package release defines the Release model and lifecycle stage management
// for Anvil Runtime Releases.
//
// The Stage type represents the current phase in a Release's lifecycle
// as defined by ADR-003 §4. Transitions between stages are validated by
// canTransitionTo() and enforced by StateMachine.
//
// Reference: TS-P4-04, ADR-003 §4
package release

// Stage represents a stage in the Release lifecycle.
//
// A Release progresses through a defined set of stages from Ready (initial)
// through to Removed (terminal). Each stage has specific valid transitions
// that are enforced by the StateMachine.
//
// Reference: ADR-003 §4, TS-P4-04
type Stage int

const (
	// StageReady is the initial stage for any new Release.
	// Valid transitions: Activating
	StageReady Stage = iota

	// StageActivating indicates the Release is being activated on the
	// target Runtime. Valid transitions: Active, RollingBack, Failed
	StageActivating

	// StageActive indicates the Release is actively deployed and serving.
	// Valid transitions: RollingBack, Archived
	StageActive

	// StageRollingBack indicates the Release is being rolled back.
	// Valid transitions: RolledBack, Failed
	StageRollingBack

	// StageRolledBack indicates the Release has been rolled back.
	// Valid transitions: Archived
	StageRolledBack

	// StageArchived indicates the Release has been archived.
	// Valid transitions: Active (for rollback), Removed
	StageArchived

	// StageRemoved indicates the Release has been removed.
	// This is a terminal stage — no further transitions are allowed.
	StageRemoved

	// StageFailed indicates the Release activation failed.
	// Valid transitions: Archived, Removed
	StageFailed
)

// String returns a human-readable lowercase name for the lifecycle stage.
func (s Stage) String() string {
	switch s {
	case StageReady:
		return "ready"
	case StageActivating:
		return "activating"
	case StageActive:
		return "active"
	case StageRollingBack:
		return "rolling back"
	case StageRolledBack:
		return "rolled back"
	case StageArchived:
		return "archived"
	case StageRemoved:
		return "removed"
	case StageFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// transitionMap defines the allowed transitions from each stage.
// A nil slice means no transitions are allowed from that stage.
//
// Reference: ADR-003 §4
var transitionMap = map[Stage][]Stage{
	StageReady:       {StageActivating},
	StageActivating:  {StageActive, StageRollingBack, StageFailed},
	StageActive:      {StageRollingBack, StageArchived},
	StageRollingBack: {StageRolledBack, StageFailed},
	StageRolledBack:  {StageArchived},
	StageArchived:    {StageActive, StageRemoved},
	StageRemoved:     nil,
	StageFailed:      {StageArchived, StageRemoved},
}

// canTransitionTo reports whether a transition from src to target is valid
// according to the lifecycle stage transition rules (ADR-003 §4).
func canTransitionTo(src, target Stage) bool {
	allowed, ok := transitionMap[src]
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
