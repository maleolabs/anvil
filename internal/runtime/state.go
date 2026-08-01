// Package runtime provides models and utilities for managing Anvil Runtime
// instances — their configuration, lifecycle state machines, readiness
// assessment, runtime identity, and runtime state tracking.
//
// Reference: CH-P5-01, TS-P5-01, TS-P5-02, TS-P5-03, TS-P5-04, EPIC-005, ADR-003 §8.5
package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// RuntimeCondition represents the operational condition of a Runtime.
// This is distinct from the lifecycle-derived OperationalStatus in identity.go
// (which tracks lifecycle stage). RuntimeCondition tracks the health/readiness
// condition of a running Runtime.
//
// Reference: TS-P5-04
type RuntimeCondition string

const (
	// ConditionNormal indicates the Runtime is operating normally.
	ConditionNormal RuntimeCondition = "normal"
	// ConditionDegraded indicates the Runtime is operating but with reduced
	// capability.
	ConditionDegraded RuntimeCondition = "degraded"
	// ConditionOffline indicates the Runtime is not operating.
	ConditionOffline RuntimeCondition = "offline"
)

// SharedResourceStatus represents the condition of shared resources.
//
// Reference: TS-P5-04
type SharedResourceStatus string

const (
	// ResourceAccessible indicates shared resources are accessible.
	ResourceAccessible SharedResourceStatus = "accessible"
	// ResourceInaccessible indicates shared resources are not accessible.
	ResourceInaccessible SharedResourceStatus = "inaccessible"
)

// RuntimeState represents the current operational state of a Runtime.
// This is distinct from Registry configuration per PLAN-EPIC-005-AM-002.
// It tracks what IS happening, not what SHOULD exist.
//
// Reference: TS-P5-04
type RuntimeState struct {
	ActiveReleaseID      string               `json:"active_release_id"`
	RuntimeCondition     RuntimeCondition     `json:"runtime_condition"`
	SharedResourceStatus SharedResourceStatus `json:"shared_resource_status"`
	LastUpdated          time.Time            `json:"last_updated"`
}

// StateStore manages persistence of RuntimeState to a JSON file.
// It is thread-safe via sync.Mutex.
//
// Reference: TS-P5-04
type StateStore struct {
	mu    sync.Mutex
	path  string
	state RuntimeState
}

// NewStateStore creates a StateStore that persists state to the given path.
//
// Reference: TS-P5-04
func NewStateStore(path string) *StateStore {
	return &StateStore{
		path: path,
		state: RuntimeState{
			RuntimeCondition:     ConditionNormal,
			SharedResourceStatus: ResourceAccessible,
			LastUpdated:          time.Now(),
		},
	}
}

// State returns a copy of the current RuntimeState.
//
// State is safe for concurrent access.
//
// Reference: TS-P5-04
func (s *StateStore) State() RuntimeState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// SetActiveRelease records which Release is currently Active.
// An empty string clears the Active Release.
//
// Reference: TS-P5-04
func (s *StateStore) SetActiveRelease(releaseID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.ActiveReleaseID = releaseID
	s.state.LastUpdated = time.Now()
}

// ClearActiveRelease removes the Active Release record.
//
// Reference: TS-P5-04
func (s *StateStore) ClearActiveRelease() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.ActiveReleaseID = ""
	s.state.LastUpdated = time.Now()
}

// SetRuntimeCondition updates the runtime condition.
//
// Reference: TS-P5-04
func (s *StateStore) SetRuntimeCondition(condition RuntimeCondition) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.RuntimeCondition = condition
	s.state.LastUpdated = time.Now()
}

// SetSharedResourceStatus updates the shared resource condition.
//
// Reference: TS-P5-04
func (s *StateStore) SetSharedResourceStatus(status SharedResourceStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.SharedResourceStatus = status
	s.state.LastUpdated = time.Now()
}

// stateFile is a serializable wrapper for JSON persistence.
type stateFile struct {
	ActiveReleaseID      string               `json:"active_release_id"`
	RuntimeCondition     RuntimeCondition     `json:"runtime_condition"`
	SharedResourceStatus SharedResourceStatus `json:"shared_resource_status"`
	LastUpdated          time.Time            `json:"last_updated"`
}

// Save persists the current RuntimeState as JSON to the configured path.
//
// Reference: TS-P5-04 AC-1
func (s *StateStore) Save() error {
	s.mu.Lock()
	sf := stateFile{
		ActiveReleaseID:      s.state.ActiveReleaseID,
		RuntimeCondition:     s.state.RuntimeCondition,
		SharedResourceStatus: s.state.SharedResourceStatus,
		LastUpdated:          s.state.LastUpdated,
	}
	s.mu.Unlock()

	data, err := json.Marshal(sf)
	if err != nil {
		return fmt.Errorf("marshal runtime state: %w", err)
	}

	if err := os.WriteFile(s.path, data, 0644); err != nil {
		return fmt.Errorf("write runtime state to %s: %w", s.path, err)
	}

	return nil
}

// Load restores RuntimeState from the configured JSON path.
//
// Reference: TS-P5-04 AC-2
func (s *StateStore) Load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("runtime state file not found: %s", s.path)
		}
		return fmt.Errorf("read runtime state from %s: %w", s.path, err)
	}

	var sf stateFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return fmt.Errorf("unmarshal runtime state: %w", err)
	}

	s.mu.Lock()
	s.state = RuntimeState{
		ActiveReleaseID:      sf.ActiveReleaseID,
		RuntimeCondition:     sf.RuntimeCondition,
		SharedResourceStatus: sf.SharedResourceStatus,
		LastUpdated:          sf.LastUpdated,
	}
	s.mu.Unlock()

	return nil
}
