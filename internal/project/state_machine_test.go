package project

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestStageString verifies that Stage.String() returns the expected
// lowercase human-readable names.
//
// Reference: TS-P1-07
func TestStageString(t *testing.T) {
	tests := []struct {
		stage Stage
		want  string
	}{
		{StageCreated, "created"},
		{StageActive, "active"},
		{StageModified, "modified"},
		{StageRemoved, "removed"},
		{Stage(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.stage.String(); got != tt.want {
				t.Errorf("Stage(%d).String() = %q, want %q", tt.stage, got, tt.want)
			}
		})
	}
}

// TestValidTransitions verifies that all valid stage transitions succeed.
//
// Reference: TS-P1-07
func TestValidTransitions(t *testing.T) {
	tests := []struct {
		name    string
		from    Stage
		to      Stage
		setupFn func() *StateMachine
	}{
		{
			name: "Created_to_Active",
			from: StageCreated,
			to:   StageActive,
			setupFn: func() *StateMachine {
				return NewStateMachine(StageCreated)
			},
		},
		{
			name: "Active_to_Modified",
			from: StageActive,
			to:   StageModified,
			setupFn: func() *StateMachine {
				return NewStateMachine(StageActive)
			},
		},
		{
			name: "Modified_to_Active",
			from: StageModified,
			to:   StageActive,
			setupFn: func() *StateMachine {
				return NewStateMachine(StageModified)
			},
		},
		{
			name: "Active_to_Removed",
			from: StageActive,
			to:   StageRemoved,
			setupFn: func() *StateMachine {
				return NewStateMachine(StageActive)
			},
		},
		{
			name: "Modified_to_Removed",
			from: StageModified,
			to:   StageRemoved,
			setupFn: func() *StateMachine {
				return NewStateMachine(StageModified)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := tt.setupFn()
			if err := sm.Transition(tt.to); err != nil {
				t.Errorf("Transition(%s) from %s returned unexpected error: %v",
					tt.to, tt.from, err)
			}
			if got := sm.Stage(); got != tt.to {
				t.Errorf("Stage() after transition = %s, want %s", got, tt.to)
			}
		})
	}
}

// TestInvalidTransitions verifies that disallowed transitions return an error
// and the stage remains unchanged.
//
// Reference: TS-P1-07
func TestInvalidTransitions(t *testing.T) {
	tests := []struct {
		name    string
		from    Stage
		to      Stage
		setupFn func() *StateMachine
	}{
		{
			name: "Created_to_Removed_blocked",
			from: StageCreated,
			to:   StageRemoved,
			setupFn: func() *StateMachine {
				return NewStateMachine(StageCreated)
			},
		},
		{
			name: "Created_to_Modified_blocked",
			from: StageCreated,
			to:   StageModified,
			setupFn: func() *StateMachine {
				return NewStateMachine(StageCreated)
			},
		},
		{
			name: "Removed_to_Active_from_terminal",
			from: StageRemoved,
			to:   StageActive,
			setupFn: func() *StateMachine {
				return NewStateMachine(StageRemoved)
			},
		},
		{
			name: "Removed_to_any_stage_from_terminal",
			from: StageRemoved,
			to:   StageModified,
			setupFn: func() *StateMachine {
				return NewStateMachine(StageRemoved)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := tt.setupFn()
			before := sm.Stage()

			err := sm.Transition(tt.to)
			if err == nil {
				t.Errorf("Transition(%s) from %s should have returned an error", tt.to, tt.from)
			}

			after := sm.Stage()
			if after != before {
				t.Errorf("Stage changed after failed transition: before=%s, after=%s", before, after)
			}
		})
	}
}

// TestInvalidTransitionErrorMessage verifies that invalid transition errors
// identify both the current stage and the attempted target stage.
//
// Reference: TS-P1-07
func TestInvalidTransitionErrorMessage(t *testing.T) {
	sm := NewStateMachine(StageCreated)

	err := sm.Transition(StageRemoved)
	if err == nil {
		t.Fatal("Transition(StageRemoved) from StageCreated should have returned an error")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "created") {
		t.Errorf("error should mention current stage 'created', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "removed") {
		t.Errorf("error should mention target stage 'removed', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "cannot transition from") {
		t.Errorf("error should indicate 'cannot transition from', got: %s", errMsg)
	}
}

// TestTransitionRecording verifies that each valid transition records
// a TransitionRecord with timestamp, from, to, and outcome fields.
//
// Reference: TS-P1-07
func TestTransitionRecording(t *testing.T) {
	sm := NewStateMachine(StageCreated)

	// Perform a valid transition.
	if err := sm.Transition(StageActive); err != nil {
		t.Fatalf("Transition(Active) unexpected error: %v", err)
	}

	history := sm.History()
	if len(history) != 1 {
		t.Fatalf("expected 1 transition record, got %d", len(history))
	}

	rec := history[0]
	if rec.Timestamp == "" {
		t.Error("transition record timestamp must not be empty")
	}
	if rec.From != StageCreated {
		t.Errorf("transition record From = %s, want %s", rec.From, StageCreated)
	}
	if rec.To != StageActive {
		t.Errorf("transition record To = %s, want %s", rec.To, StageActive)
	}
	if rec.Outcome != "success" {
		t.Errorf("transition record Outcome = %q, want %q", rec.Outcome, "success")
	}
}

// TestTransitionRecording_Failed verifies that failed transitions are also
// recorded in the history with the error as outcome.
//
// Reference: TS-P1-07
func TestTransitionRecording_Failed(t *testing.T) {
	sm := NewStateMachine(StageCreated)

	// Attempt an invalid transition.
	err := sm.Transition(StageRemoved)
	if err == nil {
		t.Fatal("expected an error for invalid transition")
	}

	history := sm.History()
	if len(history) != 1 {
		t.Fatalf("expected 1 transition record, got %d", len(history))
	}

	rec := history[0]
	if rec.Timestamp == "" {
		t.Error("transition record timestamp must not be empty")
	}
	if rec.From != StageCreated {
		t.Errorf("transition record From = %s, want %s", rec.From, StageCreated)
	}
	if rec.To != StageRemoved {
		t.Errorf("transition record To = %s, want %s", rec.To, StageRemoved)
	}
	if rec.Outcome != err.Error() {
		t.Errorf("transition record Outcome = %q, want %q", rec.Outcome, err.Error())
	}
}

// TestTransitionHistory verifies that multiple transitions accumulate
// in the transition history in order.
//
// Reference: TS-P1-07
func TestTransitionHistory(t *testing.T) {
	sm := NewStateMachine(StageCreated)

	// Perform a chain of valid transitions: Created → Active → Modified
	if err := sm.Transition(StageActive); err != nil {
		t.Fatalf("Transition(Active) unexpected error: %v", err)
	}
	if err := sm.Transition(StageModified); err != nil {
		t.Fatalf("Transition(Modified) unexpected error: %v", err)
	}

	history := sm.History()
	if len(history) != 2 {
		t.Fatalf("expected 2 transition records, got %d", len(history))
	}

	// Verify first transition.
	if history[0].From != StageCreated {
		t.Errorf("record[0] From = %s, want %s", history[0].From, StageCreated)
	}
	if history[0].To != StageActive {
		t.Errorf("record[0] To = %s, want %s", history[0].To, StageActive)
	}
	if history[0].Outcome != "success" {
		t.Errorf("record[0] Outcome = %q, want %q", history[0].Outcome, "success")
	}

	// Verify second transition.
	if history[1].From != StageActive {
		t.Errorf("record[1] From = %s, want %s", history[1].From, StageActive)
	}
	if history[1].To != StageModified {
		t.Errorf("record[1] To = %s, want %s", history[1].To, StageModified)
	}
	if history[1].Outcome != "success" {
		t.Errorf("record[1] Outcome = %q, want %q", history[1].Outcome, "success")
	}

	// Current stage must be Modified.
	if sm.Stage() != StageModified {
		t.Errorf("Stage() after transition chain = %s, want %s", sm.Stage(), StageModified)
	}
}

// TestSaveLoad_RoundTrip verifies that saving a StateMachine to a JSON file
// and loading it back preserves the stage and transition history.
//
// Reference: TS-P1-07
func TestSaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lifecycle.json")

	// Create state machine, perform transitions, save.
	sm := NewStateMachine(StageCreated)
	if err := sm.Transition(StageActive); err != nil {
		t.Fatalf("Transition(Active) unexpected error: %v", err)
	}
	if err := sm.Transition(StageModified); err != nil {
		t.Fatalf("Transition(Modified) unexpected error: %v", err)
	}

	if err := sm.Save(path); err != nil {
		t.Fatalf("Save() returned unexpected error: %v", err)
	}

	// Load into a new state machine.
	loaded := NewStateMachine(StageCreated)
	if err := loaded.Load(path); err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}

	// Verify stage.
	if got := loaded.Stage(); got != StageModified {
		t.Errorf("after Load, Stage() = %s, want %s", got, StageModified)
	}

	// Verify history length.
	history := loaded.History()
	if len(history) != 2 {
		t.Fatalf("expected 2 transition records, got %d", len(history))
	}

	// Verify history content.
	if history[0].From != StageCreated || history[0].To != StageActive {
		t.Errorf("record[0] = %s -> %s, want %s -> %s",
			history[0].From, history[0].To, StageCreated, StageActive)
	}
	if history[1].From != StageActive || history[1].To != StageModified {
		t.Errorf("record[1] = %s -> %s, want %s -> %s",
			history[1].From, history[1].To, StageActive, StageModified)
	}
}

// TestLoad_NonExistentFile verifies that Load returns an error when the
// specified file does not exist.
//
// Reference: TS-P1-07
func TestLoad_NonExistentFile(t *testing.T) {
	sm := NewStateMachine(StageCreated)
	err := sm.Load("/nonexistent/path/lifecycle.json")
	if err == nil {
		t.Fatal("Load() should have returned an error for non-existent file")
	}
	if !strings.Contains(err.Error(), "state machine file not found") {
		t.Errorf("error should indicate 'state machine file not found', got: %v", err)
	}
}
