// Package project defines the Anvil project lifecycle stage management.
//
// The Stage type represents the current phase in a Project's lifecycle.
// Transitions between stages are validated by canTransitionTo() and enforced
// by StateMachine.
//
// Reference: TS-P1-07
package project

// Stage represents a stage in the Project lifecycle.
//
// A Project progresses through a defined set of stages from Created (initial)
// through to Removed (terminal). Each stage has specific valid transitions
// that are enforced by the StateMachine.
//
// Reference: TS-P1-07
type Stage int

const (
	// StageCreated is the initial stage for any new Project.
	// Valid transitions: Active
	StageCreated Stage = iota

	// StageActive indicates the Project is active and available.
	// Valid transitions: Modified, Removed
	StageActive

	// StageModified indicates the Project has been modified.
	// Valid transitions: Active, Removed
	StageModified

	// StageRemoved indicates the Project has been removed.
	// This is a terminal stage — no further transitions are allowed.
	StageRemoved
)

// String returns a human-readable lowercase name for the lifecycle stage.
func (s Stage) String() string {
	switch s {
	case StageCreated:
		return "created"
	case StageActive:
		return "active"
	case StageModified:
		return "modified"
	case StageRemoved:
		return "removed"
	default:
		return "unknown"
	}
}

// transitionMap defines the allowed transitions from each stage.
// A nil slice means no transitions are allowed from that stage.
//
// Reference: TS-P1-07
var transitionMap = map[Stage][]Stage{
	StageCreated:  {StageActive},
	StageActive:   {StageModified, StageRemoved},
	StageModified: {StageActive, StageRemoved},
	StageRemoved:  nil,
}

// canTransitionTo reports whether a transition from src to target is valid
// according to the project lifecycle stage transition rules.
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
