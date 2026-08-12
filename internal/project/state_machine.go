package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// TransitionRecord records a single stage transition attempt, including
// whether it succeeded or failed. All transitions (valid and invalid) are
// recorded for auditability.
//
// Reference: TS-P1-07
type TransitionRecord struct {
	Timestamp string `json:"timestamp"` // RFC 3339 timestamp
	From      Stage  `json:"from"`      // Source stage
	To        Stage  `json:"to"`        // Target stage
	Outcome   string `json:"outcome"`   // "success" or error description
}

// stateMachineState is a serializable representation of the StateMachine
// for persistence via Save/Load.
type stateMachineState struct {
	Stage       Stage              `json:"stage"`
	Transitions []TransitionRecord `json:"transitions"`
}

// StateMachine tracks a Project's progression through its lifecycle stages.
//
// It provides thread-safe stage access via Stage() and validates all
// transitions through Transition(). Every transition attempt is recorded
// in the transition history. The state can be persisted to and restored
// from a JSON file via Save() and Load().
//
// Reference: TS-P1-07
type StateMachine struct {
	mu      sync.Mutex
	stage   Stage
	history []TransitionRecord
}

// NewStateMachine creates a StateMachine starting at the given initial stage.
func NewStateMachine(initial Stage) *StateMachine {
	return &StateMachine{
		stage:   initial,
		history: []TransitionRecord{},
	}
}

// Stage returns the current lifecycle stage.
//
// Stage is safe for concurrent access.
func (sm *StateMachine) Stage() Stage {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.stage
}

// History returns a copy of the transition history.
//
// History is safe for concurrent access.
func (sm *StateMachine) History() []TransitionRecord {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	result := make([]TransitionRecord, len(sm.history))
	copy(result, sm.history)
	return result
}

// Transition attempts to move the state machine from its current stage to
// the given target stage. Returns an error if the transition is not allowed.
//
// All transition attempts — both successful and failed — are recorded in
// the transition history with a timestamp, source stage, target stage,
// and outcome description.
//
// Valid transitions:
//
//	Created → Active
//	Active → Modified, Removed
//	Modified → Active, Removed
//
// Reference: TS-P1-07
func (sm *StateMachine) Transition(target Stage) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	src := sm.stage
	ts := time.Now().UTC().Format(time.RFC3339)

	if !canTransitionTo(src, target) {
		errMsg := fmt.Sprintf("cannot transition from %s to %s", src.String(), target.String())
		sm.history = append(sm.history, TransitionRecord{
			Timestamp: ts,
			From:      src,
			To:        target,
			Outcome:   errMsg,
		})
		return errors.New(errMsg)
	}

	sm.stage = target
	sm.history = append(sm.history, TransitionRecord{
		Timestamp: ts,
		From:      src,
		To:        target,
		Outcome:   "success",
	})

	return nil
}

// Save persists the current state machine state (stage + transition history)
// as JSON to the specified path. The directory containing the path must
// already exist.
//
// Reference: TS-P1-07
func (sm *StateMachine) Save(path string) error {
	sm.mu.Lock()
	state := stateMachineState{
		Stage:       sm.stage,
		Transitions: sm.history,
	}
	sm.mu.Unlock()

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal state machine: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write state machine to %s: %w", path, err)
	}

	return nil
}

// Load restores the state machine state from a JSON file at the specified
// path. Returns an error if the file does not exist or cannot be decoded.
//
// Reference: TS-P1-07
func (sm *StateMachine) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("state machine file not found: %s", path)
		}
		return fmt.Errorf("read state machine from %s: %w", path, err)
	}

	var state stateMachineState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("unmarshal state machine: %w", err)
	}

	sm.mu.Lock()
	sm.stage = state.Stage
	sm.history = state.Transitions
	sm.mu.Unlock()

	return nil
}
