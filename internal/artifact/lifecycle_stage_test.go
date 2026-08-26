// Package artifact defines the Anvil artifact directory layout and
// packaging primitives.
//
// Reference: TS-P3-09, ADR-004 §7, EPIC-003
package artifact

import (
	"testing"
)

// TestLifecycleStageString verifies that each LifecycleStage returns the
// expected human-readable name.
func TestLifecycleStageString(t *testing.T) {
	tests := []struct {
		stage    LifecycleStage
		expected string
	}{
		{StageCreated, "created"},
		{StageVerified, "verified"},
		{StageRegistered, "registered"},
		{StageReferenced, "referenced"},
		{StageConsumed, "consumed"},
		{StageArchived, "archived"},
		{StageRemoved, "removed"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.stage.String(); got != tt.expected {
				t.Errorf("LifecycleStage.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestLifecycleStageString_Unknown verifies that an invalid LifecycleStage
// value returns "unknown".
func TestLifecycleStageString_Unknown(t *testing.T) {
	var unknown LifecycleStage = 99
	if got := unknown.String(); got != "unknown" {
		t.Errorf("LifecycleStage.String() = %q, want %q", got, "unknown")
	}
}

// TestCanTransitionTo_ValidTransitions verifies all valid transitions
// defined in ADR-004 §7 are accepted by canTransitionTo.
func TestCanTransitionTo_ValidTransitions(t *testing.T) {
	tests := []struct {
		src    LifecycleStage
		target LifecycleStage
		name   string
	}{
		{StageCreated, StageVerified, "Created → Verified"},
		{StageCreated, StageRemoved, "Created → Removed"},
		{StageVerified, StageRegistered, "Verified → Registered"},
		{StageVerified, StageRemoved, "Verified → Removed"},
		{StageRegistered, StageReferenced, "Registered → Referenced"},
		{StageRegistered, StageArchived, "Registered → Archived"},
		{StageRegistered, StageRemoved, "Registered → Removed"},
		{StageReferenced, StageConsumed, "Referenced → Consumed"},
		{StageReferenced, StageArchived, "Referenced → Archived"},
		{StageReferenced, StageRemoved, "Referenced → Removed"},
		{StageConsumed, StageArchived, "Consumed → Archived"},
		{StageConsumed, StageRemoved, "Consumed → Removed"},
		{StageArchived, StageRemoved, "Archived → Removed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !canTransitionTo(tt.src, tt.target) {
				t.Errorf("canTransitionTo(%s, %s) = false, want true", tt.src, tt.target)
			}
		})
	}
}

// TestCanTransitionTo_InvalidTransitions verifies that invalid transitions
// are rejected by canTransitionTo.
func TestCanTransitionTo_InvalidTransitions(t *testing.T) {
	tests := []struct {
		src    LifecycleStage
		target LifecycleStage
		name   string
	}{
		{StageCreated, StageRegistered, "Created → Registered (bypasses Verified)"},
		{StageCreated, StageReferenced, "Created → Referenced (bypasses Verified+Registered)"},
		{StageCreated, StageConsumed, "Created → Consumed (bypasses all)"},
		{StageCreated, StageArchived, "Created → Archived"},
		{StageVerified, StageCreated, "Verified → Created (regression)"},
		{StageVerified, StageReferenced, "Verified → Referenced (bypasses Registered)"},
		{StageVerified, StageConsumed, "Verified → Consumed"},
		{StageVerified, StageArchived, "Verified → Archived"},
		{StageRegistered, StageCreated, "Registered → Created (regression)"},
		{StageRegistered, StageVerified, "Registered → Verified (regression)"},
		{StageRegistered, StageConsumed, "Registered → Consumed (bypasses Referenced)"},
		{StageReferenced, StageCreated, "Referenced → Created"},
		{StageReferenced, StageVerified, "Referenced → Verified"},
		{StageReferenced, StageRegistered, "Referenced → Registered (regression)"},
		{StageConsumed, StageCreated, "Consumed → Created"},
		{StageConsumed, StageVerified, "Consumed → Verified"},
		{StageConsumed, StageRegistered, "Consumed → Registered"},
		{StageConsumed, StageReferenced, "Consumed → Referenced (regression)"},
		{StageArchived, StageCreated, "Archived → Created"},
		{StageArchived, StageVerified, "Archived → Verified"},
		{StageArchived, StageRegistered, "Archived → Registered"},
		{StageArchived, StageReferenced, "Archived → Referenced"},
		{StageArchived, StageConsumed, "Archived → Consumed"},
		{StageRemoved, StageCreated, "Removed → Created (terminal)"},
		{StageRemoved, StageVerified, "Removed → Verified (terminal)"},
		{StageRemoved, StageRegistered, "Removed → Registered (terminal)"},
		{StageRemoved, StageReferenced, "Removed → Referenced (terminal)"},
		{StageRemoved, StageConsumed, "Removed → Consumed (terminal)"},
		{StageRemoved, StageArchived, "Removed → Archived (terminal)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if canTransitionTo(tt.src, tt.target) {
				t.Errorf("canTransitionTo(%s, %s) = true, want false", tt.src, tt.target)
			}
		})
	}
}

// TestCanTransitionTo_UnknownSource verifies that an unknown source stage
// returns false.
func TestCanTransitionTo_UnknownSource(t *testing.T) {
	var unknown LifecycleStage = 99
	if canTransitionTo(unknown, StageCreated) {
		t.Error("canTransitionTo(unknown, Created) = true, want false")
	}
}
