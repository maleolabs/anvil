// Package artifact defines the Anvil artifact directory layout and
// packaging primitives.
//
// Reference: TS-P3-09, ADR-004 §7, ADR-006 §5.3/§8.6, EPIC-003
package artifact

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// LifecycleTransitionRecord records a single Artifact lifecycle stage
// transition attempt, including whether it succeeded or failed. All
// transitions (valid and invalid) are recorded for auditability.
//
// Reference: TS-P3-09, ADR-006 §8.4
type LifecycleTransitionRecord struct {
	Timestamp string         `json:"timestamp"` // RFC 3339 timestamp
	From      LifecycleStage `json:"from"`      // Source stage
	To        LifecycleStage `json:"to"`        // Target stage
	Outcome   string         `json:"outcome"`   // "success" or error description
}

// lifecycleMachineState is a serializable representation of the
// LifecycleStateMachine for persistence via Save/Load.
type lifecycleMachineState struct {
	Stage       LifecycleStage              `json:"stage"`
	Transitions []LifecycleTransitionRecord `json:"transitions"`
}

// LifecycleStateMachine tracks an Artifact's progression through its
// lifecycle stages — Created, Verified, Registered, Referenced, Consumed,
// Archived, Removed.
//
// It provides thread-safe stage access via Stage() and validates all
// transitions through Transition(). Every transition attempt is recorded
// in the transition history. The state can be persisted to and restored
// from a JSON file via Save() and Load().
//
// Reference: TS-P3-09, ADR-004 §7, ADR-006 §5.3/§8.6
type LifecycleStateMachine struct {
	mu      sync.Mutex
	stage   LifecycleStage
	history []LifecycleTransitionRecord
}

// NewLifecycleStateMachine creates a LifecycleStateMachine starting at
// the given initial stage. For a newly packaged Artifact, this should
// be StageCreated.
//
// Reference: TS-P3-09
func NewLifecycleStateMachine(initial LifecycleStage) *LifecycleStateMachine {
	return &LifecycleStateMachine{
		stage:   initial,
		history: []LifecycleTransitionRecord{},
	}
}

// Stage returns the current Artifact lifecycle stage.
//
// Stage is safe for concurrent access.
func (lsm *LifecycleStateMachine) Stage() LifecycleStage {
	lsm.mu.Lock()
	defer lsm.mu.Unlock()
	return lsm.stage
}

// History returns a copy of the transition history.
//
// History is safe for concurrent access.
func (lsm *LifecycleStateMachine) History() []LifecycleTransitionRecord {
	lsm.mu.Lock()
	defer lsm.mu.Unlock()
	result := make([]LifecycleTransitionRecord, len(lsm.history))
	copy(result, lsm.history)
	return result
}

// Transition attempts to move the Artifact lifecycle state machine from
// its current stage to the given target stage. Returns an error if the
// transition is not allowed.
//
// All transition attempts — both successful and failed — are recorded in
// the transition history with a timestamp, source stage, target stage,
// and outcome description.
//
// Valid transitions (from ADR-004 §7):
//
//	Created → Verified, Removed
//	Verified → Registered, Removed
//	Registered → Referenced, Archived, Removed
//	Referenced → Consumed, Archived, Removed
//	Consumed → Archived, Removed
//	Archived → Removed
//	Removed → (none)
//
// Reference: TS-P3-09, ADR-004 §7, ADR-006 §6.2
func (lsm *LifecycleStateMachine) Transition(target LifecycleStage) error {
	lsm.mu.Lock()
	defer lsm.mu.Unlock()

	src := lsm.stage
	ts := time.Now().UTC().Format(time.RFC3339)

	if !canTransitionTo(src, target) {
		errMsg := fmt.Sprintf("cannot transition from %s to %s", src.String(), target.String())
		lsm.history = append(lsm.history, LifecycleTransitionRecord{
			Timestamp: ts,
			From:      src,
			To:        target,
			Outcome:   errMsg,
		})
		return errors.New(errMsg)
	}

	lsm.stage = target
	lsm.history = append(lsm.history, LifecycleTransitionRecord{
		Timestamp: ts,
		From:      src,
		To:        target,
		Outcome:   "success",
	})

	return nil
}

// Save persists the current lifecycle state machine state (stage +
// transition history) as JSON to the specified path. The directory
// containing the path must already exist.
//
// Reference: TS-P3-09, ADR-006 §8.6
func (lsm *LifecycleStateMachine) Save(path string) error {
	lsm.mu.Lock()
	state := lifecycleMachineState{
		Stage:       lsm.stage,
		Transitions: lsm.history,
	}
	lsm.mu.Unlock()

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal artifact lifecycle state machine: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write artifact lifecycle state machine to %s: %w", path, err)
	}

	return nil
}

// Load restores the Artifact lifecycle state machine state from a JSON
// file at the specified path. Returns an error if the file does not
// exist or cannot be decoded.
//
// Reference: TS-P3-09, ADR-006 §8.6
func (lsm *LifecycleStateMachine) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("artifact lifecycle state machine file not found: %s", path)
		}
		return fmt.Errorf("read artifact lifecycle state machine from %s: %w", path, err)
	}

	var state lifecycleMachineState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("unmarshal artifact lifecycle state machine: %w", err)
	}

	lsm.mu.Lock()
	lsm.stage = state.Stage
	lsm.history = state.Transitions
	lsm.mu.Unlock()

	return nil
}
