// Tests for the adapter lifecycle state machine (TS-P7-05).
package adapter

import (
	"errors"
	"testing"
)

// TestLifecycle_NewStartsDiscovered verifies that a new lifecycle starts
// at StageDiscovered — an adapter enters the system via discovery.
//
// Reference: TS-P7-05 AC-1
func TestLifecycle_NewStartsDiscovered(t *testing.T) {
	lc := NewLifecycle()

	if got := lc.Stage(); got != StageDiscovered {
		t.Errorf("NewLifecycle().Stage() = %q, want %q", got, StageDiscovered)
	}
}

// TestLifecycle_AdvanceWalksValidChain verifies that Advance walks the
// linear chain Discovered → Validated → Ready → Participating → Completed
// → Removed, and that Stage() reports each stage along the way.
//
// Reference: TS-P7-05 AC-2..AC-6
func TestLifecycle_AdvanceWalksValidChain(t *testing.T) {
	lc := NewLifecycle()

	want := []Stage{
		StageDiscovered,
		StageValidated,
		StageReady,
		StageParticipating,
		StageCompleted,
		StageRemoved,
	}

	for i, w := range want {
		if got := lc.Stage(); got != w {
			t.Fatalf("stage after %d advances = %q, want %q", i, got, w)
		}
		if i < len(want)-1 {
			if err := lc.Advance(); err != nil {
				t.Fatalf("Advance from %q failed: %v", w, err)
			}
		}
	}
}

// TestLifecycle_AdvanceFromRemovedRejected verifies that advancing from
// StageRemoved is rejected with an error wrapping ErrInvalidTransition and
// leaves the stage unchanged — StageRemoved is terminal.
//
// Reference: TS-P7-05 AC-7
func TestLifecycle_AdvanceFromRemovedRejected(t *testing.T) {
	lc := NewLifecycle()
	for i := 0; i < 5; i++ {
		if err := lc.Advance(); err != nil {
			t.Fatalf("Advance %d failed: %v", i, err)
		}
	}
	if got := lc.Stage(); got != StageRemoved {
		t.Fatalf("stage = %q, want %q", got, StageRemoved)
	}

	err := lc.Advance()
	if err == nil {
		t.Fatal("Advance from StageRemoved succeeded, want error")
	}
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("Advance from StageRemoved error = %v, want wrapping ErrInvalidTransition", err)
	}
	if got := lc.Stage(); got != StageRemoved {
		t.Errorf("stage after rejected Advance = %q, want %q (rejected transitions must not change the stage)", got, StageRemoved)
	}
}

// TestLifecycle_StageQuery verifies Stage() reports the current stage
// after each transition.
//
// Reference: TS-P7-05
func TestLifecycle_StageQuery(t *testing.T) {
	lc := NewLifecycle()

	tests := []struct {
		before Stage
		after  Stage
	}{
		{before: StageDiscovered, after: StageValidated},
		{before: StageValidated, after: StageReady},
		{before: StageReady, after: StageParticipating},
		{before: StageParticipating, after: StageCompleted},
		{before: StageCompleted, after: StageRemoved},
	}

	for _, tt := range tests {
		if got := lc.Stage(); got != tt.before {
			t.Fatalf("Stage() = %q, want %q", got, tt.before)
		}
		if err := lc.Advance(); err != nil {
			t.Fatalf("Advance from %q failed: %v", tt.before, err)
		}
		if got := lc.Stage(); got != tt.after {
			t.Errorf("Stage() after Advance = %q, want %q", got, tt.after)
		}
	}
}

// TestStage_Values verifies the stage constant values match the contract
// strings (TS-007-005 §7).
//
// Reference: TS-P7-05 AC-1
func TestStage_Values(t *testing.T) {
	tests := []struct {
		stage Stage
		want  string
	}{
		{StageDiscovered, "discovered"},
		{StageValidated, "validated"},
		{StageReady, "ready"},
		{StageParticipating, "participating"},
		{StageCompleted, "completed"},
		{StageRemoved, "removed"},
	}

	for _, tt := range tests {
		if got := string(tt.stage); got != tt.want {
			t.Errorf("Stage constant = %q, want %q", got, tt.want)
		}
	}
}
