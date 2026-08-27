// Package artifact defines the Anvil artifact directory layout and
// packaging primitives.
//
// Reference: TS-P3-09, ADR-004 §7, ADR-006 §5.3/§8.6, EPIC-003
package artifact

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNewLifecycleStateMachine verifies that a newly created state machine
// starts at the given initial stage with an empty history.
func TestNewLifecycleStateMachine(t *testing.T) {
	lsm := NewLifecycleStateMachine(StageCreated)
	if lsm.Stage() != StageCreated {
		t.Errorf("expected initial stage Created, got %s", lsm.Stage())
	}
	if len(lsm.History()) != 0 {
		t.Errorf("expected empty history, got %d records", len(lsm.History()))
	}
}

// TestLifecycleStateMachine_Transition_Success verifies that a valid
// transition updates the stage and records a success in history.
func TestLifecycleStateMachine_Transition_Success(t *testing.T) {
	lsm := NewLifecycleStateMachine(StageCreated)

	err := lsm.Transition(StageVerified)
	if err != nil {
		t.Fatalf("Transition(Created → Verified) returned error: %v", err)
	}

	if lsm.Stage() != StageVerified {
		t.Errorf("expected stage Verified after transition, got %s", lsm.Stage())
	}

	history := lsm.History()
	if len(history) != 1 {
		t.Fatalf("expected 1 history record, got %d", len(history))
	}
	if history[0].Outcome != "success" {
		t.Errorf("expected success outcome, got %q", history[0].Outcome)
	}
	if history[0].From != StageCreated {
		t.Errorf("expected From=Created, got %s", history[0].From)
	}
	if history[0].To != StageVerified {
		t.Errorf("expected To=Verified, got %s", history[0].To)
	}
	if history[0].Timestamp == "" {
		t.Error("expected non-empty timestamp")
	}
}

// TestLifecycleStateMachine_FullProgression verifies a complete valid
// progression through all lifecycle stages.
func TestLifecycleStateMachine_FullProgression(t *testing.T) {
	lsm := NewLifecycleStateMachine(StageCreated)

	transitions := []struct {
		target LifecycleStage
		name   string
	}{
		{StageVerified, "Created → Verified"},
		{StageRegistered, "Verified → Registered"},
		{StageReferenced, "Registered → Referenced"},
		{StageConsumed, "Referenced → Consumed"},
		{StageArchived, "Consumed → Archived"},
		{StageRemoved, "Archived → Removed"},
	}

	for _, tt := range transitions {
		t.Run(tt.name, func(t *testing.T) {
			if err := lsm.Transition(tt.target); err != nil {
				t.Fatalf("Transition(%s) failed: %v", tt.name, err)
			}
			if lsm.Stage() != tt.target {
				t.Errorf("expected stage %s, got %s", tt.target, lsm.Stage())
			}
		})
	}

	// Verify all 6 transitions were recorded.
	history := lsm.History()
	if len(history) != 6 {
		t.Errorf("expected 6 transition records, got %d", len(history))
	}
	for _, rec := range history {
		if rec.Outcome != "success" {
			t.Errorf("unexpected failed transition: %s → %s: %s", rec.From, rec.To, rec.Outcome)
		}
	}
}

// TestLifecycleStateMachine_InvalidTransition verifies that an invalid
// transition returns an error and does not change the stage, but still
// records the attempt in history.
func TestLifecycleStateMachine_InvalidTransition(t *testing.T) {
	lsm := NewLifecycleStateMachine(StageCreated)

	// Attempting to skip directly to Registered should fail.
	err := lsm.Transition(StageRegistered)
	if err == nil {
		t.Fatal("expected error for invalid transition Created → Registered, got nil")
	}

	if !strings.Contains(err.Error(), "cannot transition from created to registered") {
		t.Errorf("unexpected error message: %v", err)
	}

	// Stage should remain Created.
	if lsm.Stage() != StageCreated {
		t.Errorf("expected stage to remain Created, got %s", lsm.Stage())
	}

	// The failed attempt should still be recorded.
	history := lsm.History()
	if len(history) != 1 {
		t.Fatalf("expected 1 history record, got %d", len(history))
	}
	if history[0].Outcome == "success" {
		t.Error("expected failed outcome in history")
	}
	if !strings.Contains(history[0].Outcome, "cannot transition") {
		t.Errorf("expected error message in outcome, got %q", history[0].Outcome)
	}
}

// TestLifecycleStateMachine_TransitionFromRemoved verifies that no
// transitions are allowed from the terminal Removed stage.
func TestLifecycleStateMachine_TransitionFromRemoved(t *testing.T) {
	lsm := NewLifecycleStateMachine(StageRemoved)

	err := lsm.Transition(StageCreated)
	if err == nil {
		t.Fatal("expected error for transition from Removed, got nil")
	}

	if !strings.Contains(err.Error(), "cannot transition") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestLifecycleStateMachine_SaveAndLoad verifies that state can be
// persisted to disk and restored correctly, surviving a process restart.
func TestLifecycleStateMachine_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "artifact-lifecycle.json")

	// Create and progress the state machine.
	lsm := NewLifecycleStateMachine(StageCreated)
	if err := lsm.Transition(StageVerified); err != nil {
		t.Fatalf("Transition to Verified: %v", err)
	}
	if err := lsm.Transition(StageRegistered); err != nil {
		t.Fatalf("Transition to Registered: %v", err)
	}

	// Save state.
	if err := lsm.Save(statePath); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify the file exists and is non-empty.
	info, err := os.Stat(statePath)
	if err != nil {
		t.Fatalf("Stat saved file: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("saved file is empty")
	}

	// Load into a new state machine (simulating process restart).
	lsm2 := NewLifecycleStateMachine(StageCreated)
	if err := lsm2.Load(statePath); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Verify restored state.
	if lsm2.Stage() != StageRegistered {
		t.Errorf("expected restored stage Registered, got %s", lsm2.Stage())
	}

	// Verify transition history is restored.
	history := lsm2.History()
	if len(history) != 2 {
		t.Fatalf("expected 2 history records after load, got %d", len(history))
	}
	if history[0].From != StageCreated || history[0].To != StageVerified {
		t.Errorf("unexpected first transition: %s → %s", history[0].From, history[0].To)
	}
	if history[1].From != StageVerified || history[1].To != StageRegistered {
		t.Errorf("unexpected second transition: %s → %s", history[1].From, history[1].To)
	}

	// Verify the loaded state machine can continue transitioning.
	if err := lsm2.Transition(StageReferenced); err != nil {
		t.Fatalf("Transition after load (to Referenced): %v", err)
	}
	if lsm2.Stage() != StageReferenced {
		t.Errorf("expected stage Referenced after transition, got %s", lsm2.Stage())
	}
}

// TestLifecycleStateMachine_Load_NonExistentFile verifies that Load returns
// an error for a non-existent file.
func TestLifecycleStateMachine_Load_NonExistentFile(t *testing.T) {
	lsm := NewLifecycleStateMachine(StageCreated)
	err := lsm.Load("/tmp/nonexistent-lifecycle-state-99999.json")
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

// TestLifecycleStateMachine_HistoryIsIndependent verifies that the History()
// method returns a copy that does not affect the internal state.
func TestLifecycleStateMachine_HistoryIsIndependent(t *testing.T) {
	lsm := NewLifecycleStateMachine(StageCreated)
	if err := lsm.Transition(StageVerified); err != nil {
		t.Fatalf("Transition: %v", err)
	}

	history := lsm.History()
	history[0].Outcome = "tampered"

	internalHistory := lsm.History()
	if internalHistory[0].Outcome == "tampered" {
		t.Error("History() should return a copy, not a reference to internal state")
	}
}

// TestLifecycleStateMachine_ConcurrentAccess verifies that concurrent reads
// and writes to the state machine do not cause data races.
func TestLifecycleStateMachine_ConcurrentAccess(t *testing.T) {
	lsm := NewLifecycleStateMachine(StageCreated)

	done := make(chan bool)
	go func() {
		for i := 0; i < 100; i++ {
			lsm.Stage()
			lsm.History()
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			_ = lsm.Transition(StageVerified)
			_ = lsm.Transition(StageRegistered)
		}
		done <- true
	}()

	<-done
	<-done
}
