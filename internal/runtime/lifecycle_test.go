package runtime

import (
	"path/filepath"
	"testing"
)

// TestNewLifecycle_StartsAtProvisioned verifies that a newly created
// Lifecycle starts at StageProvisioned.
//
// Reference: TS-P5-01 AC-1
func TestNewLifecycle_StartsAtProvisioned(t *testing.T) {
	lc := NewLifecycle()
	if got := lc.Stage(); got != StageProvisioned {
		t.Errorf("NewLifecycle().Stage() = %s, want %s", got, StageProvisioned)
	}
}

// TestValidTransitions verifies all valid stage transitions succeed.
//
// Reference: TS-P5-01 AC-2
func TestValidTransitions(t *testing.T) {
	tests := []struct {
		name   string
		from   Stage
		to     Stage
		setup  func() *Lifecycle
	}{
		{
			name: "Provisioned_to_Ready",
			from: StageProvisioned,
			to:   StageReady,
			setup: func() *Lifecycle {
				return NewLifecycle()
			},
		},
		{
			name: "Ready_to_Active",
			from: StageReady,
			to:   StageActive,
			setup: func() *Lifecycle {
				lc := NewLifecycle()
				lc.setStage(StageReady)
				return lc
			},
		},
		{
			name: "Ready_to_Retired",
			from: StageReady,
			to:   StageRetired,
			setup: func() *Lifecycle {
				lc := NewLifecycle()
				lc.setStage(StageReady)
				return lc
			},
		},
		{
			name: "Active_to_Retired",
			from: StageActive,
			to:   StageRetired,
			setup: func() *Lifecycle {
				lc := NewLifecycle()
				lc.setStage(StageActive)
				return lc
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lc := tt.setup()
			if err := lc.Transition(tt.to); err != nil {
				t.Errorf("Transition(%s) from %s returned unexpected error: %v",
					tt.to, tt.from, err)
			}
			if got := lc.Stage(); got != tt.to {
				t.Errorf("Stage() after transition = %s, want %s", got, tt.to)
			}
		})
	}
}

// TestInvalidTransitions verifies that disallowed transitions return an error
// and the stage remains unchanged.
//
// Reference: TS-P5-01 AC-2
func TestInvalidTransitions(t *testing.T) {
	tests := []struct {
		name  string
		from  Stage
		to    Stage
		setup func() *Lifecycle
	}{
		{
			name: "Provisioned_to_Active_blocked",
			from: StageProvisioned,
			to:   StageActive,
			setup: func() *Lifecycle {
				return NewLifecycle()
			},
		},
		{
			name: "Provisioned_to_Retired_blocked",
			from: StageProvisioned,
			to:   StageRetired,
			setup: func() *Lifecycle {
				return NewLifecycle()
			},
		},
		{
			name: "Ready_to_Provisioned_backward",
			from: StageReady,
			to:   StageProvisioned,
			setup: func() *Lifecycle {
				lc := NewLifecycle()
				lc.setStage(StageReady)
				return lc
			},
		},
		{
			name: "Active_to_Ready_backward",
			from: StageActive,
			to:   StageReady,
			setup: func() *Lifecycle {
				lc := NewLifecycle()
				lc.setStage(StageActive)
				return lc
			},
		},
		{
			name: "Active_to_Provisioned_backward",
			from: StageActive,
			to:   StageProvisioned,
			setup: func() *Lifecycle {
				lc := NewLifecycle()
				lc.setStage(StageActive)
				return lc
			},
		},
		{
			name: "Retired_to_any_unknown",
			from: StageRetired,
			to:   StageReady,
			setup: func() *Lifecycle {
				lc := NewLifecycle()
				lc.setStage(StageRetired)
				return lc
			},
		},
		{
			name: "Retired_to_Active_unknown",
			from: StageRetired,
			to:   StageActive,
			setup: func() *Lifecycle {
				lc := NewLifecycle()
				lc.setStage(StageRetired)
				return lc
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lc := tt.setup()
			before := lc.Stage()

			err := lc.Transition(tt.to)
			if err == nil {
				t.Errorf("Transition(%s) from %s should have returned an error", tt.to, tt.from)
			}

			after := lc.Stage()
			if after != before {
				t.Errorf("Stage changed after failed transition: before=%s, after=%s", before, after)
			}
		})
	}
}

// TestStageString verifies that Stage.String() returns the expected
// lowercase human-readable names.
//
// Reference: TS-P5-01 AC-3
func TestStageString(t *testing.T) {
	tests := []struct {
		stage Stage
		want  string
	}{
		{StageProvisioned, "provisioned"},
		{StageReady, "ready"},
		{StageActive, "active"},
		{StageRetired, "retired"},
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

// TestSaveLoad_RoundTrip verifies that saving a lifecycle stage to a JSON
// file and loading it back preserves the stage value.
//
// Reference: TS-P5-01 AC-4
func TestSaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lifecycle.json")

	lc := NewLifecycle()
	lc.setStage(StageActive)

	if err := lc.Save(path); err != nil {
		t.Fatalf("Save() returned unexpected error: %v", err)
	}

	loaded := NewLifecycle()
	if err := loaded.Load(path); err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}

	if got := loaded.Stage(); got != StageActive {
		t.Errorf("after Load, Stage() = %s, want %s", got, StageActive)
	}
}

// TestLoad_NonExistentFile verifies that Load returns an error when the
// specified file does not exist.
//
// Reference: TS-P5-01 AC-5
func TestLoad_NonExistentFile(t *testing.T) {
	lc := NewLifecycle()
	err := lc.Load("/nonexistent/path/lifecycle.json")
	if err == nil {
		t.Fatal("Load() should have returned an error for non-existent file")
	}
}
