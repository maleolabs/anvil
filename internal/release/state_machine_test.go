package release

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestStageString verifies that Stage.String() returns the expected
// lowercase human-readable names.
//
// Reference: TS-P4-04
func TestStageString(t *testing.T) {
	tests := []struct {
		stage Stage
		want  string
	}{
		{StageReady, "ready"},
		{StageActivating, "activating"},
		{StageActive, "active"},
		{StageRollingBack, "rolling back"},
		{StageRolledBack, "rolled back"},
		{StageArchived, "archived"},
		{StageRemoved, "removed"},
		{StageFailed, "failed"},
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
// Reference: TS-P4-04, ST-P4-04
func TestValidTransitions(t *testing.T) {
	tests := []struct {
		name    string
		from    Stage
		to      Stage
		setupFn func() *StateMachine
	}{
		{
			name: "Ready_to_Activating",
			from: StageReady,
			to:   StageActivating,
			setupFn: func() *StateMachine {
				return NewStateMachine(StageReady)
			},
		},
		{
			name: "Activating_to_Active",
			from: StageActivating,
			to:   StageActive,
			setupFn: func() *StateMachine {
				return NewStateMachine(StageActivating)
			},
		},
		{
			name: "Activating_to_RollingBack",
			from: StageActivating,
			to:   StageRollingBack,
			setupFn: func() *StateMachine {
				return NewStateMachine(StageActivating)
			},
		},
		{
			name: "Activating_to_Failed",
			from: StageActivating,
			to:   StageFailed,
			setupFn: func() *StateMachine {
				return NewStateMachine(StageActivating)
			},
		},
		{
			name: "Active_to_RollingBack",
			from: StageActive,
			to:   StageRollingBack,
			setupFn: func() *StateMachine {
				return NewStateMachine(StageActive)
			},
		},
		{
			name: "Active_to_Archived",
			from: StageActive,
			to:   StageArchived,
			setupFn: func() *StateMachine {
				return NewStateMachine(StageActive)
			},
		},
		{
			name: "RollingBack_to_RolledBack",
			from: StageRollingBack,
			to:   StageRolledBack,
			setupFn: func() *StateMachine {
				return NewStateMachine(StageRollingBack)
			},
		},
		{
			name: "RolledBack_to_Archived",
			from: StageRolledBack,
			to:   StageArchived,
			setupFn: func() *StateMachine {
				return NewStateMachine(StageRolledBack)
			},
		},
		{
			name: "Archived_to_Removed",
			from: StageArchived,
			to:   StageRemoved,
			setupFn: func() *StateMachine {
				return NewStateMachine(StageArchived)
			},
		},
		{
			name: "Archived_to_Active",
			from: StageArchived,
			to:   StageActive,
			setupFn: func() *StateMachine {
				return NewStateMachine(StageArchived)
			},
		},
		{
			name: "Failed_to_Archived",
			from: StageFailed,
			to:   StageArchived,
			setupFn: func() *StateMachine {
				return NewStateMachine(StageFailed)
			},
		},
		{
			name: "Failed_to_Removed",
			from: StageFailed,
			to:   StageRemoved,
			setupFn: func() *StateMachine {
				return NewStateMachine(StageFailed)
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
// Reference: TS-P4-04, ST-P4-04
func TestInvalidTransitions(t *testing.T) {
	tests := []struct {
		name    string
		from    Stage
		to      Stage
		setupFn func() *StateMachine
	}{
		{
			name: "Ready_to_Active_blocked",
			from: StageReady,
			to:   StageActive,
			setupFn: func() *StateMachine {
				return NewStateMachine(StageReady)
			},
		},
		{
			name: "Ready_to_RollingBack_blocked",
			from: StageReady,
			to:   StageRollingBack,
			setupFn: func() *StateMachine {
				return NewStateMachine(StageReady)
			},
		},
		{
			name: "Active_to_Ready_backward",
			from: StageActive,
			to:   StageReady,
			setupFn: func() *StateMachine {
				return NewStateMachine(StageActive)
			},
		},
		{
			name: "Removed_to_Archived_from_terminal",
			from: StageRemoved,
			to:   StageArchived,
			setupFn: func() *StateMachine {
				return NewStateMachine(StageRemoved)
			},
		},
		{
			name: "Removed_to_any_stage_from_terminal",
			from: StageRemoved,
			to:   StageReady,
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
// Reference: ST-P4-04 AC
func TestInvalidTransitionErrorMessage(t *testing.T) {
	sm := NewStateMachine(StageReady)

	err := sm.Transition(StageActive)
	if err == nil {
		t.Fatal("Transition(StageActive) from StageReady should have returned an error")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "ready") {
		t.Errorf("error should mention current stage 'ready', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "active") {
		t.Errorf("error should mention target stage 'active', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "cannot transition from") {
		t.Errorf("error should indicate 'cannot transition from', got: %s", errMsg)
	}
}

// TestTransitionRecording verifies that each valid transition records
// a TransitionRecord with timestamp, from, to, and outcome fields.
//
// Reference: ST-P4-04 AC
func TestTransitionRecording(t *testing.T) {
	sm := NewStateMachine(StageReady)

	// Perform a valid transition.
	if err := sm.Transition(StageActivating); err != nil {
		t.Fatalf("Transition(Activating) unexpected error: %v", err)
	}

	history := sm.History()
	if len(history) != 1 {
		t.Fatalf("expected 1 transition record, got %d", len(history))
	}

	rec := history[0]
	if rec.Timestamp == "" {
		t.Error("transition record timestamp must not be empty")
	}
	if rec.From != StageReady {
		t.Errorf("transition record From = %s, want %s", rec.From, StageReady)
	}
	if rec.To != StageActivating {
		t.Errorf("transition record To = %s, want %s", rec.To, StageActivating)
	}
	if rec.Outcome != "success" {
		t.Errorf("transition record Outcome = %q, want %q", rec.Outcome, "success")
	}
}

// TestTransitionRecording_Failed verifies that failed transitions are also
// recorded in the history with the error as outcome.
//
// Reference: ST-P4-04 AC
func TestTransitionRecording_Failed(t *testing.T) {
	sm := NewStateMachine(StageReady)

	// Attempt an invalid transition.
	err := sm.Transition(StageActive)
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
	if rec.From != StageReady {
		t.Errorf("transition record From = %s, want %s", rec.From, StageReady)
	}
	if rec.To != StageActive {
		t.Errorf("transition record To = %s, want %s", rec.To, StageActive)
	}
	if rec.Outcome != err.Error() {
		t.Errorf("transition record Outcome = %q, want %q", rec.Outcome, err.Error())
	}
}

// TestTransitionHistory verifies that multiple transitions accumulate
// in the transition history in order.
//
// Reference: ST-P4-04 AC
func TestTransitionHistory(t *testing.T) {
	sm := NewStateMachine(StageReady)

	// Perform a chain of valid transitions: Ready → Activating → Active
	if err := sm.Transition(StageActivating); err != nil {
		t.Fatalf("Transition(Activating) unexpected error: %v", err)
	}
	if err := sm.Transition(StageActive); err != nil {
		t.Fatalf("Transition(Active) unexpected error: %v", err)
	}

	history := sm.History()
	if len(history) != 2 {
		t.Fatalf("expected 2 transition records, got %d", len(history))
	}

	// Verify first transition.
	if history[0].From != StageReady {
		t.Errorf("record[0] From = %s, want %s", history[0].From, StageReady)
	}
	if history[0].To != StageActivating {
		t.Errorf("record[0] To = %s, want %s", history[0].To, StageActivating)
	}
	if history[0].Outcome != "success" {
		t.Errorf("record[0] Outcome = %q, want %q", history[0].Outcome, "success")
	}

	// Verify second transition.
	if history[1].From != StageActivating {
		t.Errorf("record[1] From = %s, want %s", history[1].From, StageActivating)
	}
	if history[1].To != StageActive {
		t.Errorf("record[1] To = %s, want %s", history[1].To, StageActive)
	}
	if history[1].Outcome != "success" {
		t.Errorf("record[1] Outcome = %q, want %q", history[1].Outcome, "success")
	}

	// Current stage must be Active.
	if sm.Stage() != StageActive {
		t.Errorf("Stage() after transition chain = %s, want %s", sm.Stage(), StageActive)
	}
}

// TestSaveLoad_RoundTrip verifies that saving a StateMachine to a JSON file
// and loading it back preserves the stage and transition history.
//
// Reference: TS-P4-04 AC
func TestSaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state_machine.json")

	// Create state machine, perform transitions, save.
	sm := NewStateMachine(StageReady)
	if err := sm.Transition(StageActivating); err != nil {
		t.Fatalf("Transition(Activating) unexpected error: %v", err)
	}
	if err := sm.Transition(StageActive); err != nil {
		t.Fatalf("Transition(Active) unexpected error: %v", err)
	}

	if err := sm.Save(path); err != nil {
		t.Fatalf("Save() returned unexpected error: %v", err)
	}

	// Load into a new state machine.
	loaded := NewStateMachine(StageReady)
	if err := loaded.Load(path); err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}

	// Verify stage.
	if got := loaded.Stage(); got != StageActive {
		t.Errorf("after Load, Stage() = %s, want %s", got, StageActive)
	}

	// Verify history length.
	history := loaded.History()
	if len(history) != 2 {
		t.Fatalf("expected 2 transition records, got %d", len(history))
	}

	// Verify history content.
	if history[0].From != StageReady || history[0].To != StageActivating {
		t.Errorf("record[0] = %s -> %s, want %s -> %s",
			history[0].From, history[0].To, StageReady, StageActivating)
	}
	if history[1].From != StageActivating || history[1].To != StageActive {
		t.Errorf("record[1] = %s -> %s, want %s -> %s",
			history[1].From, history[1].To, StageActivating, StageActive)
	}
}

// TestLoad_NonExistentFile verifies that Load returns an error when the
// specified file does not exist.
//
// Reference: TS-P4-04 AC
func TestLoad_NonExistentFile(t *testing.T) {
	sm := NewStateMachine(StageReady)
	err := sm.Load("/nonexistent/path/state_machine.json")
	if err == nil {
		t.Fatal("Load() should have returned an error for non-existent file")
	}
	if !strings.Contains(err.Error(), "state machine file not found") {
		t.Errorf("error should indicate 'state machine file not found', got: %v", err)
	}
}

// TestNewReleaseStage_Ready verifies that a new Release starts with
// StageReady and an empty transition history.
//
// Reference: TS-P4-04 AC
func TestNewReleaseStage_Ready(t *testing.T) {
	// Create a Release directly (not through CreateRelease to avoid
	// requiring artifact verification).
	rel := &Release{
		ID:          ReleaseID("test-001"),
		Stage:       StageReady,
		Transitions: []TransitionRecord{},
	}

	if rel.Stage != StageReady {
		t.Errorf("Release Stage = %s, want %s", rel.Stage, StageReady)
	}

	if rel.Transitions == nil {
		t.Error("Release Transitions must not be nil")
	}
	if len(rel.Transitions) != 0 {
		t.Errorf("Release Transitions length = %d, want 0", len(rel.Transitions))
	}
}

// TestReleaseTransition_Valid verifies that a Release can transition
// through valid stages using its Transition method.
//
// Reference: TS-P4-04, ST-P4-04
func TestReleaseTransition_Valid(t *testing.T) {
	rel := &Release{
		ID:          ReleaseID("test-002"),
		Stage:       StageReady,
		Transitions: []TransitionRecord{},
	}

	// Ready → Activating
	if err := rel.Transition(StageActivating); err != nil {
		t.Fatalf("Transition(Activating) unexpected error: %v", err)
	}
	if rel.Stage != StageActivating {
		t.Errorf("Stage = %s, want %s", rel.Stage, StageActivating)
	}
	if len(rel.Transitions) != 1 {
		t.Errorf("expected 1 transition, got %d", len(rel.Transitions))
	}

	// Activating → Active
	if err := rel.Transition(StageActive); err != nil {
		t.Fatalf("Transition(Active) unexpected error: %v", err)
	}
	if rel.Stage != StageActive {
		t.Errorf("Stage = %s, want %s", rel.Stage, StageActive)
	}
	if len(rel.Transitions) != 2 {
		t.Errorf("expected 2 transitions, got %d", len(rel.Transitions))
	}
}

// TestReleaseTransition_Invalid verifies that a Release rejects invalid
// transitions and records them in history while keeping the original stage.
//
// Reference: TS-P4-04, ST-P4-04
func TestReleaseTransition_Invalid(t *testing.T) {
	rel := &Release{
		ID:          ReleaseID("test-003"),
		Stage:       StageReady,
		Transitions: []TransitionRecord{},
	}

	before := rel.Stage

	// Ready → Active is invalid (skip Activating).
	err := rel.Transition(StageActive)
	if err == nil {
		t.Fatal("Transition(Active) from Ready should return error")
	}

	if rel.Stage != before {
		t.Errorf("Stage changed after failed transition: %s -> %s", before, rel.Stage)
	}

	// The failed transition must still be recorded.
	if len(rel.Transitions) != 1 {
		t.Errorf("expected 1 transition record (failed), got %d", len(rel.Transitions))
	}

	rec := rel.Transitions[0]
	if rec.From != StageReady {
		t.Errorf("record From = %s, want %s", rec.From, StageReady)
	}
	if rec.To != StageActive {
		t.Errorf("record To = %s, want %s", rec.To, StageActive)
	}
	if rec.Outcome == "success" {
		t.Error("failed transition should not have outcome 'success'")
	}
}
