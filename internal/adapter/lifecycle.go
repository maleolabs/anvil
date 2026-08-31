// Lifecycle and stage definitions for the adapter lifecycle state machine
// (TS-P7-05). Adapters are stateless (ADR-009 §9.8); lifecycle tracking is
// Core-side, in-memory, per execution context.
package adapter

import (
	"errors"
	"fmt"
	"sync"
)

// Stage identifies one stage of the adapter lifecycle (ADR-009 §7):
// Discovery → Validation → Participation → Completion → Removal.
//
// Reference: TS-P7-05 AC-1
type Stage string

const (
	// StageDiscovered: the adapter entered the system via discovery.
	// This is the initial stage of every lifecycle.
	StageDiscovered Stage = "discovered"

	// StageValidated: the adapter passed compatibility validation
	// (advanced by TS-P7-06).
	StageValidated Stage = "validated"

	// StageReady: the adapter is compatible and ready for participation.
	StageReady Stage = "ready"

	// StageParticipating: the adapter is actively providing phases and
	// checks during operations (advanced by TS-P7-08).
	StageParticipating Stage = "participating"

	// StageCompleted: the adapter's participation has ended.
	StageCompleted Stage = "completed"

	// StageRemoved: the adapter has been removed from the system. This
	// is a terminal stage — no transitions leave it.
	StageRemoved Stage = "removed"
)

// ErrInvalidTransition is returned by Lifecycle.Advance when the current
// stage has no valid successor, e.g. advancing from StageRemoved.
//
// Reference: TS-P7-05 AC-7
var ErrInvalidTransition = errors.New("adapter: invalid lifecycle transition")

// stageOrder is the linear chain of valid transitions (TS-007-005 §7):
// Discovered → Validated → Ready → Participating → Completed → Removed.
//
// Reference: TS-P7-05 AC-2..AC-7
var stageOrder = []Stage{
	StageDiscovered,
	StageValidated,
	StageReady,
	StageParticipating,
	StageCompleted,
	StageRemoved,
}

// Lifecycle tracks the lifecycle stage of one adapter within an execution
// context. Lifecycle state is Core-side and in-memory; adapters themselves
// are stateless (ADR-009 §9.8). The lifecycle is thread-safe, following
// the repository convention for shared state (see internal/runtime).
//
// Reference: TS-P7-05
type Lifecycle struct {
	mu    sync.Mutex
	stage Stage
}

// NewLifecycle returns a lifecycle starting at StageDiscovered — an
// adapter enters the system via discovery (ADR-009 §7.1).
//
// Reference: TS-P7-05 AC-1
func NewLifecycle() *Lifecycle {
	return &Lifecycle{stage: StageDiscovered}
}

// Stage returns the adapter's current lifecycle stage.
//
// Reference: TS-P7-05
func (l *Lifecycle) Stage() Stage {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.stage
}

// Advance moves the lifecycle to the next valid stage. It returns an
// error wrapping ErrInvalidTransition when the current stage has no valid
// successor (e.g. advancing from StageRemoved, a terminal stage) or when
// the stage is unknown. Invalid transitions are rejected without changing
// the current stage.
//
// Reference: TS-P7-05 AC-7
func (l *Lifecycle) Advance() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, s := range stageOrder {
		if s == l.stage {
			if i == len(stageOrder)-1 {
				return fmt.Errorf("%w: cannot advance from stage %q (terminal stage)", ErrInvalidTransition, l.stage)
			}
			l.stage = stageOrder[i+1]
			return nil
		}
	}
	return fmt.Errorf("%w: unknown stage %q", ErrInvalidTransition, l.stage)
}
