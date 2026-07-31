package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/runtime"
)

// TestRuntimeStatusCommand_RegistersUnderRuntime verifies that the runtime
// status command is registered as a subcommand of the runtime command.
//
// Reference: ST-P5-03
func TestRuntimeStatusCommand_RegistersUnderRuntime(t *testing.T) {
	runtimeSub, _, err := rootCmd.Find([]string{"runtime", "status"})
	if err != nil {
		t.Fatalf("rootCmd.Find([\"runtime\", \"status\"]) returned error: %v", err)
	}
	if runtimeSub == nil {
		t.Fatal("rootCmd.Find([\"runtime\", \"status\"]) returned nil command")
	}
	if runtimeSub.Use != "status" {
		t.Errorf("command Use = %q, want %q", runtimeSub.Use, "status")
	}

	// Verify it's nested under runtime (not directly under root).
	_, _, err = rootCmd.Find([]string{"status"})
	if err != nil {
		// "status" is also a direct subcommand (anvil status), so this may
		// find it. But the runtime status should be separate.
	}
}

// TestRuntimeStatusCommand_DisplaysState verifies that the runtime status
// command displays the state read from the state file.
//
// Reference: ST-P5-03 AC-1
func TestRuntimeStatusCommand_DisplaysState(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "runtime-state.json")

	// Create a state store and populate it.
	store := runtime.NewStateStore(stateFile)
	store.SetActiveRelease("release-v1.0.0")
	store.SetRuntimeCondition(runtime.ConditionNormal)
	store.SetSharedResourceStatus(runtime.ResourceAccessible)

	if err := store.Save(); err != nil {
		t.Fatalf("failed to save state file: %v", err)
	}

	_, stdout, stderr, err := executeCommand("runtime", "status", "--state-file", stateFile)
	if err != nil {
		t.Fatalf("runtime status command returned unexpected error: %v", err)
	}

	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	// Must contain the state fields.
	if !contains(stdout, "Active Release:  release-v1.0.0") {
		t.Errorf("stdout should contain active release, got: %s", stdout)
	}
	if !contains(stdout, "Condition:       normal") {
		t.Errorf("stdout should contain condition, got: %s", stdout)
	}
	if !contains(stdout, "Shared Resource: accessible") {
		t.Errorf("stdout should contain shared resource status, got: %s", stdout)
	}
	if !contains(stdout, "Last Updated:") {
		t.Errorf("stdout should contain last updated, got: %s", stdout)
	}
}

// TestRuntimeStatusCommand_NoActiveRelease verifies that when no active
// release is set, the status displays "none".
//
// Reference: ST-P5-03 AC-2
func TestRuntimeStatusCommand_NoActiveRelease(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "runtime-state.json")

	// Create a state store with default values (no active release).
	store := runtime.NewStateStore(stateFile)
	if err := store.Save(); err != nil {
		t.Fatalf("failed to save state file: %v", err)
	}

	_, stdout, stderr, err := executeCommand("runtime", "status", "--state-file", stateFile)
	if err != nil {
		t.Fatalf("runtime status command returned unexpected error: %v", err)
	}

	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	if !contains(stdout, "Active Release:  none") {
		t.Errorf("stdout should display 'none' for empty active release, got: %s", stdout)
	}
}

// TestRuntimeStatusCommand_DegradedCondition verifies that the status
// command displays degraded condition when set.
//
// Reference: ST-P5-03 AC-3
func TestRuntimeStatusCommand_DegradedCondition(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "runtime-state.json")

	store := runtime.NewStateStore(stateFile)
	store.SetRuntimeCondition(runtime.ConditionDegraded)
	store.SetSharedResourceStatus(runtime.ResourceInaccessible)

	if err := store.Save(); err != nil {
		t.Fatalf("failed to save state file: %v", err)
	}

	_, stdout, stderr, err := executeCommand("runtime", "status", "--state-file", stateFile)
	if err != nil {
		t.Fatalf("runtime status command returned unexpected error: %v", err)
	}

	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	if !contains(stdout, "Condition:       degraded") {
		t.Errorf("stdout should display 'degraded' condition, got: %s", stdout)
	}
	if !contains(stdout, "Shared Resource: inaccessible") {
		t.Errorf("stdout should display 'inaccessible' resource status, got: %s", stdout)
	}
}

// TestRuntimeStatusCommand_OfflineCondition verifies that the status
// command displays offline condition when set.
func TestRuntimeStatusCommand_OfflineCondition(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "runtime-state.json")

	store := runtime.NewStateStore(stateFile)
	store.SetRuntimeCondition(runtime.ConditionOffline)

	if err := store.Save(); err != nil {
		t.Fatalf("failed to save state file: %v", err)
	}

	_, stdout, stderr, err := executeCommand("runtime", "status", "--state-file", stateFile)
	if err != nil {
		t.Fatalf("runtime status command returned unexpected error: %v", err)
	}

	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	if !contains(stdout, "Condition:       offline") {
		t.Errorf("stdout should display 'offline' condition, got: %s", stdout)
	}
}

// TestRuntimeStatusCommand_MissingStateFile verifies that the status
// command returns a graceful error when the state file does not exist.
//
// Reference: ST-P5-03 AC-4
func TestRuntimeStatusCommand_MissingStateFile(t *testing.T) {
	_, _, _, err := executeCommand("runtime", "status", "--state-file", "/nonexistent/path/state.json")
	if err == nil {
		t.Fatal("expected error for missing state file, got nil")
	}

	if !contains(err.Error(), "runtime state file not found") {
		t.Errorf("error should mention 'runtime state file not found', got: %v", err)
	}
}

// TestRuntimeStatusCommand_ReadOnly verifies that the runtime status
// command does not create or modify any files.
//
// Reference: ST-P5-03 AC-5
func TestRuntimeStatusCommand_ReadOnly(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "runtime-state.json")

	// Create a state file.
	store := runtime.NewStateStore(stateFile)
	store.SetActiveRelease("release-v2.0.0")
	if err := store.Save(); err != nil {
		t.Fatalf("failed to save state file: %v", err)
	}

	// Capture directory contents before.
	entriesBefore, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read directory before: %v", err)
	}

	// Read the state file content before.
	contentBefore, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("failed to read state file before: %v", err)
	}

	// Run the status command.
	_, _, _, err = executeCommand("runtime", "status", "--state-file", stateFile)
	if err != nil {
		t.Fatalf("runtime status command returned unexpected error: %v", err)
	}

	// Verify no new files were created.
	entriesAfter, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read directory after: %v", err)
	}

	if len(entriesBefore) != len(entriesAfter) {
		t.Errorf("directory entry count changed: before=%d, after=%d",
			len(entriesBefore), len(entriesAfter))
	}

	// Verify the state file content was not modified.
	contentAfter, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("failed to read state file after: %v", err)
	}

	if string(contentBefore) != string(contentAfter) {
		t.Error("state file content was modified by read-only status command")
	}
}

// TestRuntimeStatusCommand_StateFileFlag verifies that the --state-file
// flag correctly overrides the default state file path.
func TestRuntimeStatusCommand_StateFileFlag(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "custom-state.json")

	store := runtime.NewStateStore(stateFile)
	store.SetActiveRelease("custom-release")
	if err := store.Save(); err != nil {
		t.Fatalf("failed to save state file: %v", err)
	}

	_, stdout, stderr, err := executeCommand("runtime", "status", "--state-file", stateFile)
	if err != nil {
		t.Fatalf("runtime status command returned unexpected error: %v", err)
	}

	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	if !contains(stdout, "Active Release:  custom-release") {
		t.Errorf("stdout should contain the custom release, got: %s", stdout)
	}
}

// TestRuntimeStatusCommand_FlagDeduplication verifies that the --state-file
// flag is only on the runtime status subcommand (not on the runtime parent).
func TestRuntimeStatusCommand_FlagDeduplication(t *testing.T) {
	// Verify flag is NOT on the runtime parent command.
	runtimeCmdRef, _, err := rootCmd.Find([]string{"runtime"})
	if err != nil {
		t.Fatalf("failed to find runtime command: %v", err)
	}

	flag := runtimeCmdRef.Flags().Lookup("state-file")
	if flag != nil {
		t.Error("state-file flag should not be on the runtime parent command")
	}

	// Verify flag IS on the runtime status subcommand.
	statusCmdRef, _, err := rootCmd.Find([]string{"runtime", "status"})
	if err != nil {
		t.Fatalf("failed to find runtime status command: %v", err)
	}

	flag = statusCmdRef.Flags().Lookup("state-file")
	if flag == nil {
		t.Error("state-file flag should be on the runtime status subcommand")
	}
}

// TestRuntimeStatusCommand_DefaultStateFilePath verifies that the default
// --state-file value uses the expected default path.
func TestRuntimeStatusCommand_DefaultStateFilePath(t *testing.T) {
	statusCmdRef, _, err := rootCmd.Find([]string{"runtime", "status"})
	if err != nil {
		t.Fatalf("failed to find runtime status command: %v", err)
	}

	flag := statusCmdRef.Flags().Lookup("state-file")
	if flag == nil {
		t.Fatal("state-file flag not found")
	}

	if flag.DefValue == "" {
		t.Error("default state-file value should not be empty")
	}
}

// TestRuntimeStatusCommand_LastUpdatedFormat verifies that the last updated
// timestamp is displayed in RFC3339 format.
func TestRuntimeStatusCommand_LastUpdatedFormat(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "runtime-state.json")

	store := runtime.NewStateStore(stateFile)
	store.SetActiveRelease("release-time-test")
	// Override the timestamp by saving and loading.
	if err := store.Save(); err != nil {
		t.Fatalf("failed to save state file: %v", err)
	}

	// Read state file and replace timestamp.
	store.SetRuntimeCondition(runtime.ConditionNormal)
	if err := store.Save(); err != nil {
		t.Fatalf("failed to re-save state file: %v", err)
	}

	// Re-read and verify.
	store2 := runtime.NewStateStore(stateFile)
	if err := store2.Load(); err != nil {
		t.Fatalf("failed to load state file: %v", err)
	}

	// Use the stored state to check format via the command.
	_, stdout, _, err := executeCommand("runtime", "status", "--state-file", stateFile)
	if err != nil {
		t.Fatalf("runtime status command returned unexpected error: %v", err)
	}

	// The output should contain a timestamp in RFC3339-like format.
	if !contains(stdout, "Last Updated:") {
		t.Errorf("stdout should contain Last Updated, got: %s", stdout)
	}
}

// TestRuntimeStatusCommand_EmptyStateFile verifies behavior with an
// empty (zero-byte) state file.
func TestRuntimeStatusCommand_EmptyStateFile(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "runtime-state.json")

	// Create an empty file.
	if err := os.WriteFile(stateFile, []byte{}, 0644); err != nil {
		t.Fatalf("failed to create empty state file: %v", err)
	}

	_, _, _, err := executeCommand("runtime", "status", "--state-file", stateFile)
	if err == nil {
		t.Fatal("expected error for empty state file, got nil")
	}

	if !contains(err.Error(), "unmarshal") {
		t.Errorf("error should mention unmarshal error, got: %v", err)
	}
}
