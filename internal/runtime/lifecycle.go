package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// Stage represents a stage in the Runtime lifecycle.
//
// The Runtime progresses through the following stages:
//   - Provisioned: Runtime initialized but not configured.
//   - Ready:       Runtime configured and ready to accept Releases.
//   - Active:      Runtime actively hosting Releases.
//   - Retired:     Runtime taken out of service (terminal).
//
// Valid transitions:
//
//	Provisioned → Ready
//	Ready → Active
//	Ready → Retired
//	Active → Retired
//
// Reference: TS-P5-01, EPIC-005, ADR-003 §8.5
type Stage int

const (
	// StageProvisioned indicates the Runtime has been initialized but not
	// yet configured. This is the initial stage for any new Runtime.
	StageProvisioned Stage = iota

	// StageReady indicates the Runtime is fully configured and ready to
	// accept Releases.
	StageReady

	// StageActive indicates the Runtime is actively hosting Releases.
	StageActive

	// StageRetired indicates the Runtime has been taken out of service.
	// This is a terminal stage — no further transitions are allowed.
	StageRetired
)

// String returns a human-readable lowercase name for the lifecycle stage.
func (s Stage) String() string {
	switch s {
	case StageProvisioned:
		return "provisioned"
	case StageReady:
		return "ready"
	case StageActive:
		return "active"
	case StageRetired:
		return "retired"
	default:
		return "unknown"
	}
}

// transitionMap defines the allowed transitions from each stage.
// A nil slice means no transitions are allowed from that stage.
var transitionMap = map[Stage][]Stage{
	StageProvisioned: {StageReady},
	StageReady:       {StageActive, StageRetired},
	StageActive:      {StageRetired},
	StageRetired:     nil,
}

// canTransitionTo reports whether a transition from src to target is valid.
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

// Lifecycle tracks the Runtime's progression through its lifecycle stages.
//
// It provides thread-safe stage access via Stage() and validates all
// transitions through Transition(). The lifecycle state can be persisted
// to and restored from a JSON file via Save() and Load().
//
// Reference: TS-P5-01
type Lifecycle struct {
	mu    sync.Mutex
	stage Stage
}

// NewLifecycle creates a Lifecycle starting at StageProvisioned.
func NewLifecycle() *Lifecycle {
	return &Lifecycle{
		stage: StageProvisioned,
	}
}

// Stage returns the current lifecycle stage.
//
// Stage is safe for concurrent access.
func (l *Lifecycle) Stage() Stage {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.stage
}

// setStage atomically updates the lifecycle stage.
func (l *Lifecycle) setStage(s Stage) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stage = s
}

// Transition attempts to move the lifecycle from its current stage to the
// given target stage. Returns an error if the transition is not allowed.
//
// Valid transitions:
//   - Provisioned → Ready
//   - Ready → Active
//   - Ready → Retired
//   - Active → Retired
//
// Reference: TS-P5-01 AC-1, AC-2, AC-3
func (l *Lifecycle) Transition(target Stage) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	src := l.stage

	if !canTransitionTo(src, target) {
		return fmt.Errorf(
			"cannot transition from %s to %s",
			src.String(), target.String(),
		)
	}

	l.stage = target
	return nil
}

// stageState is a serializable representation of the lifecycle state for
// persistence via Save/Load.
type stageState struct {
	Stage Stage `json:"stage"`
}

// Save persists the current lifecycle stage as JSON to the specified path.
// The directory containing the path must already exist.
//
// Reference: TS-P5-01 AC-4
func (l *Lifecycle) Save(path string) error {
	l.mu.Lock()
	state := stageState{Stage: l.stage}
	l.mu.Unlock()

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal lifecycle state: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write lifecycle state to %s: %w", path, err)
	}

	return nil
}

// Load restores the lifecycle stage from a JSON file at the specified path.
// Returns an error if the file does not exist or cannot be decoded.
//
// Reference: TS-P5-01 AC-5
func (l *Lifecycle) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("lifecycle state file not found: %s", path)
		}
		return fmt.Errorf("read lifecycle state from %s: %w", path, err)
	}

	var state stageState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("unmarshal lifecycle state: %w", err)
	}

	l.mu.Lock()
	l.stage = state.Stage
	l.mu.Unlock()

	return nil
}
