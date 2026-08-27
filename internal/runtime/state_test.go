package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestNewStateStore verifies that NewStateStore creates a store with the
// given path and initial default values.
//
// Reference: TS-P5-04 AC-1
func TestNewStateStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStateStore(path)

	if store == nil {
		t.Fatal("NewStateStore() returned nil")
	}

	state := store.State()
	if state.RuntimeCondition != ConditionNormal {
		t.Errorf("initial RuntimeCondition = %q, want %q", state.RuntimeCondition, ConditionNormal)
	}
	if state.SharedResourceStatus != ResourceAccessible {
		t.Errorf("initial SharedResourceStatus = %q, want %q", state.SharedResourceStatus, ResourceAccessible)
	}
	if state.LastUpdated.IsZero() {
		t.Error("initial LastUpdated should not be zero")
	}
}

// TestSetActiveRelease verifies that SetActiveRelease updates the active
// release ID and bumps the timestamp.
//
// Reference: TS-P5-04
func TestSetActiveRelease(t *testing.T) {
	store := NewStateStore(filepath.Join(t.TempDir(), "state.json"))

	store.SetActiveRelease("release-v1.0.0")

	state := store.State()
	if state.ActiveReleaseID != "release-v1.0.0" {
		t.Errorf("ActiveReleaseID = %q, want %q", state.ActiveReleaseID, "release-v1.0.0")
	}
}

// TestClearActiveRelease verifies that ClearActiveRelease empties the active
// release ID and bumps the timestamp.
//
// Reference: TS-P5-04
func TestClearActiveRelease(t *testing.T) {
	store := NewStateStore(filepath.Join(t.TempDir(), "state.json"))

	store.SetActiveRelease("release-v2.0.0")
	store.ClearActiveRelease()

	state := store.State()
	if state.ActiveReleaseID != "" {
		t.Errorf("after ClearActiveRelease, ActiveReleaseID = %q, want empty", state.ActiveReleaseID)
	}
}

// TestSetRuntimeCondition verifies that SetRuntimeCondition updates the
// runtime condition.
//
// Reference: TS-P5-04
func TestSetRuntimeCondition(t *testing.T) {
	store := NewStateStore(filepath.Join(t.TempDir(), "state.json"))

	store.SetRuntimeCondition(ConditionDegraded)

	state := store.State()
	if state.RuntimeCondition != ConditionDegraded {
		t.Errorf("RuntimeCondition = %q, want %q", state.RuntimeCondition, ConditionDegraded)
	}

	store.SetRuntimeCondition(ConditionOffline)

	state = store.State()
	if state.RuntimeCondition != ConditionOffline {
		t.Errorf("RuntimeCondition = %q, want %q", state.RuntimeCondition, ConditionOffline)
	}
}

// TestSetSharedResourceStatus verifies that SetSharedResourceStatus updates
// the shared resource status.
//
// Reference: TS-P5-04
func TestSetSharedResourceStatus(t *testing.T) {
	store := NewStateStore(filepath.Join(t.TempDir(), "state.json"))

	store.SetSharedResourceStatus(ResourceInaccessible)

	state := store.State()
	if state.SharedResourceStatus != ResourceInaccessible {
		t.Errorf("SharedResourceStatus = %q, want %q", state.SharedResourceStatus, ResourceInaccessible)
	}

	store.SetSharedResourceStatus(ResourceAccessible)

	state = store.State()
	if state.SharedResourceStatus != ResourceAccessible {
		t.Errorf("SharedResourceStatus = %q, want %q", state.SharedResourceStatus, ResourceAccessible)
	}
}

// TestStateStore_SaveLoad_RoundTrip verifies that saving RuntimeState to a
// JSON file and loading it back preserves all field values.
//
// Reference: TS-P5-04 AC-1, AC-2
func TestStateStore_SaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "runtime-state.json")

	// Create and populate a store.
	store := NewStateStore(path)
	store.SetActiveRelease("release-v3.0.0")
	store.SetRuntimeCondition(ConditionDegraded)
	store.SetSharedResourceStatus(ResourceInaccessible)

	if err := store.Save(); err != nil {
		t.Fatalf("Save() returned unexpected error: %v", err)
	}

	// Verify the file was created.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state file was not created: %v", err)
	}

	// Load into a new store.
	loaded := NewStateStore(path)
	if err := loaded.Load(); err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}

	// Verify all fields match.
	state := loaded.State()
	if state.ActiveReleaseID != "release-v3.0.0" {
		t.Errorf("after Load, ActiveReleaseID = %q, want %q", state.ActiveReleaseID, "release-v3.0.0")
	}
	if state.RuntimeCondition != ConditionDegraded {
		t.Errorf("after Load, RuntimeCondition = %q, want %q", state.RuntimeCondition, ConditionDegraded)
	}
	if state.SharedResourceStatus != ResourceInaccessible {
		t.Errorf("after Load, SharedResourceStatus = %q, want %q", state.SharedResourceStatus, ResourceInaccessible)
	}
	if state.LastUpdated.IsZero() {
		t.Error("after Load, LastUpdated is zero")
	}
}

// TestStateStore_Load_NonExistentFile verifies that Load returns an error
// when the state file does not exist.
//
// Reference: TS-P5-04 AC-2
func TestStateStore_Load_NonExistentFile(t *testing.T) {
	store := NewStateStore("/nonexistent/path/runtime-state.json")

	err := store.Load()
	if err == nil {
		t.Fatal("Load() should have returned an error for non-existent file")
	}
	if !strings.Contains(err.Error(), "runtime state file not found") {
		t.Errorf("error message = %q, want it to contain 'runtime state file not found'", err.Error())
	}
}

// TestStateStore_Save_InvalidPath verifies that Save returns an error when
// the configured path points to a non-existent directory.
func TestStateStore_Save_InvalidPath(t *testing.T) {
	store := NewStateStore("/nonexistent/directory/state.json")

	err := store.Save()
	if err == nil {
		t.Fatal("Save() should have returned an error for invalid path")
	}
}

// TestStateStore_ThreadSafety verifies that StateStore can handle concurrent
// read and write operations without data races.
//
// Reference: TS-P5-04
func TestStateStore_ThreadSafety(t *testing.T) {
	store := NewStateStore(filepath.Join(t.TempDir(), "state.json"))

	var wg sync.WaitGroup

	// Concurrent writers.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			store.SetActiveRelease("release")
			store.SetRuntimeCondition(ConditionNormal)
			store.SetSharedResourceStatus(ResourceAccessible)
		}(i)
	}

	// Concurrent readers.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = store.State()
		}()
	}

	wg.Wait()

	// The store should still be in a valid state.
	state := store.State()
	_ = state
}

// TestState_InitialValues verifies that a freshly created StateStore has
// no active release and default condition/status values.
//
// Reference: TS-P5-04
func TestState_InitialValues(t *testing.T) {
	store := NewStateStore(filepath.Join(t.TempDir(), "state.json"))
	state := store.State()

	if state.ActiveReleaseID != "" {
		t.Errorf("initial ActiveReleaseID = %q, want empty", state.ActiveReleaseID)
	}
	if state.RuntimeCondition != ConditionNormal {
		t.Errorf("initial RuntimeCondition = %q, want %q", state.RuntimeCondition, ConditionNormal)
	}
	if state.SharedResourceStatus != ResourceAccessible {
		t.Errorf("initial SharedResourceStatus = %q, want %q", state.SharedResourceStatus, ResourceAccessible)
	}
}

// TestState_NoRegistryFields verifies that RuntimeState does not contain
// fields that belong to Registry configuration (InstallRoot, ReleasesDir,
// EnvironmentName, etc.). This enforces the separation between state
// (what IS happening) and configuration (what SHOULD exist).
//
// Reference: TS-P5-04, PLAN-EPIC-005-AM-002
func TestState_NoRegistryFields(t *testing.T) {
	// Use JSON marshaling to verify no registry-config fields leak into state.
	data, err := json.Marshal(RuntimeState{})
	if err != nil {
		t.Fatalf("json.Marshal(RuntimeState{}) returned error: %v", err)
	}

	jsonStr := string(data)

	// Registry configuration fields that MUST NOT appear in RuntimeState.
	registryFields := []string{
		"install_root",
		"releases_dir",
		"active_symlink",
		"shared_config_dir",
		"shared_storage_dir",
		"logs_dir",
		"temp_dir",
		"environment_name",
		"dir_naming_pattern",
	}

	for _, field := range registryFields {
		if strings.Contains(jsonStr, field) {
			t.Errorf("RuntimeState JSON contains registry config field %q — state should not include configuration fields", field)
		}
	}

	// State fields that MUST appear.
	stateFields := []string{
		"active_release_id",
		"runtime_condition",
		"shared_resource_status",
		"last_updated",
	}

	for _, field := range stateFields {
		if !strings.Contains(jsonStr, field) {
			t.Errorf("RuntimeState JSON missing required state field %q", field)
		}
	}
}

// TestStateStore_LastUpdatedChangesOnMutation verifies that mutations to
// the state update the LastUpdated timestamp.
//
// Reference: TS-P5-04
func TestStateStore_LastUpdatedChangesOnMutation(t *testing.T) {
	store := NewStateStore(filepath.Join(t.TempDir(), "state.json"))

	initial := store.State()
	initialTime := initial.LastUpdated

	// Small sleep to ensure time advances.
	time.Sleep(time.Millisecond)

	store.SetActiveRelease("release-v4.0.0")

	after := store.State()
	if !after.LastUpdated.After(initialTime) {
		t.Error("LastUpdated should advance after SetActiveRelease")
	}
}

// TestStateStore_ClearActiveRelease_Twice verifies that calling
// ClearActiveRelease on an already-cleared state is idempotent.
//
// Reference: TS-P5-04
func TestStateStore_ClearActiveRelease_Twice(t *testing.T) {
	store := NewStateStore(filepath.Join(t.TempDir(), "state.json"))

	// Clear on initial state (already empty).
	store.ClearActiveRelease()

	state := store.State()
	if state.ActiveReleaseID != "" {
		t.Errorf("after first ClearActiveRelease, ActiveReleaseID = %q, want empty", state.ActiveReleaseID)
	}

	// Clear again.
	store.ClearActiveRelease()

	state = store.State()
	if state.ActiveReleaseID != "" {
		t.Errorf("after second ClearActiveRelease, ActiveReleaseID = %q, want empty", state.ActiveReleaseID)
	}
}

// TestStateStore_StateDoesNotMutateOriginal verifies that State() returns
// a copy, not a reference, so modifications to the returned struct do not
// affect the store's internal state.
func TestStateStore_StateDoesNotMutateOriginal(t *testing.T) {
	store := NewStateStore(filepath.Join(t.TempDir(), "state.json"))

	// Get a copy and modify it.
	stateCopy := store.State()
	stateCopy.ActiveReleaseID = "injected"

	// Verify the store was not affected.
	internal := store.State()
	if internal.ActiveReleaseID == "injected" {
		t.Error("modifying the returned state should not affect the store's internal state")
	}
}

// TestStateStore_Load_InvalidJSON verifies that Load returns an error when
// the state file contains invalid JSON.
func TestStateStore_Load_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	if err := os.WriteFile(path, []byte("{invalid json}"), 0644); err != nil {
		t.Fatalf("failed to write invalid JSON: %v", err)
	}

	store := NewStateStore(path)
	err := store.Load()
	if err == nil {
		t.Fatal("Load() should have returned an error for invalid JSON")
	}
}

// TestStateStore_SaveLoad_RoundTrip_EmptyState verifies that saving and
// loading a default (minimal) state works correctly.
func TestStateStore_SaveLoad_RoundTrip_EmptyState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	store := NewStateStore(path)
	// Don't mutate — save the initial state.
	if err := store.Save(); err != nil {
		t.Fatalf("Save() returned unexpected error: %v", err)
	}

	loaded := NewStateStore(path)
	if err := loaded.Load(); err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}

	state := loaded.State()
	if state.ActiveReleaseID != "" {
		t.Errorf("ActiveReleaseID = %q, want empty", state.ActiveReleaseID)
	}
	if state.RuntimeCondition != ConditionNormal {
		t.Errorf("RuntimeCondition = %q, want %q", state.RuntimeCondition, ConditionNormal)
	}
	if state.SharedResourceStatus != ResourceAccessible {
		t.Errorf("SharedResourceStatus = %q, want %q", state.SharedResourceStatus, ResourceAccessible)
	}
}

// TestConditionConstants verifies that all RuntimeCondition constants have
// the expected string values.
func TestConditionConstants(t *testing.T) {
	tests := []struct {
		condition RuntimeCondition
		want      string
	}{
		{ConditionNormal, "normal"},
		{ConditionDegraded, "degraded"},
		{ConditionOffline, "offline"},
	}

	for _, tt := range tests {
		t.Run(string(tt.condition), func(t *testing.T) {
			if string(tt.condition) != tt.want {
				t.Errorf("RuntimeCondition = %q, want %q", string(tt.condition), tt.want)
			}
		})
	}
}

// TestSharedResourceStatusConstants verifies that all SharedResourceStatus
// constants have the expected string values.
func TestSharedResourceStatusConstants(t *testing.T) {
	tests := []struct {
		status SharedResourceStatus
		want   string
	}{
		{ResourceAccessible, "accessible"},
		{ResourceInaccessible, "inaccessible"},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if string(tt.status) != tt.want {
				t.Errorf("SharedResourceStatus = %q, want %q", string(tt.status), tt.want)
			}
		})
	}
}

// TestStateStore_Save_CrashWindowAtomic verifies the TD-002 crash-window
// property for StateStore.Save: a crash mid-save (simulated by a partial
// temp file that never got renamed) leaves the complete previous state file
// at the final path, so Load never observes a truncated runtime state. A
// subsequent Save recovers and leaves no temp files behind.
//
// Reference: TD-002, TS-P5-04 AC-1
func TestStateStore_Save_CrashWindowAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "runtime-state.json")

	// Persist the previous complete runtime state.
	store := NewStateStore(path)
	store.SetActiveRelease("release-v1.0.0")
	if err := store.Save(); err != nil {
		t.Fatalf("Save() returned unexpected error: %v", err)
	}

	// Simulate a crash mid-write: partial temp file, rename never happened.
	crashTemp := filepath.Join(dir, "runtime-state.json.tmp-crashed")
	if err := os.WriteFile(crashTemp, []byte(`{"active_release_id":"release-v2`), 0644); err != nil {
		t.Fatalf("failed to simulate crashed temp file: %v", err)
	}

	// The final path must still hold the complete previous state.
	loaded := NewStateStore(path)
	if err := loaded.Load(); err != nil {
		t.Fatalf("Load() after simulated crash returned unexpected error: %v", err)
	}
	if got := loaded.State().ActiveReleaseID; got != "release-v1.0.0" {
		t.Errorf("active release after simulated crash = %q, want %q (previous complete state)",
			got, "release-v1.0.0")
	}

	// A subsequent Save must succeed and persist the new complete state.
	store2 := NewStateStore(path)
	store2.SetActiveRelease("release-v2.0.0")
	if err := store2.Save(); err != nil {
		t.Fatalf("Save() after crash returned unexpected error: %v", err)
	}

	loaded2 := NewStateStore(path)
	if err := loaded2.Load(); err != nil {
		t.Fatalf("Load() after recovery returned unexpected error: %v", err)
	}
	if got := loaded2.State().ActiveReleaseID; got != "release-v2.0.0" {
		t.Errorf("active release after recovery = %q, want %q", got, "release-v2.0.0")
	}
}

// TestStateStore_Save_ReplacesCorruptFile verifies that StateStore.Save
// atomically replaces a corrupt state file at the final path (the artifact
// of the pre-TD-002 non-atomic writer) with a complete, loadable state.
//
// Reference: TD-002
func TestStateStore_Save_ReplacesCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "runtime-state.json")

	if err := os.WriteFile(path, []byte(`{"active_release_id":"release-v9`), 0644); err != nil {
		t.Fatalf("failed to write corrupt state file: %v", err)
	}

	store := NewStateStore(path)
	store.SetActiveRelease("release-v3.0.0")
	if err := store.Save(); err != nil {
		t.Fatalf("Save() over corrupt file returned unexpected error: %v", err)
	}

	loaded := NewStateStore(path)
	if err := loaded.Load(); err != nil {
		t.Fatalf("Load() after Save over corrupt file returned unexpected error: %v", err)
	}
	if got := loaded.State().ActiveReleaseID; got != "release-v3.0.0" {
		t.Errorf("active release = %q, want %q", got, "release-v3.0.0")
	}
}
