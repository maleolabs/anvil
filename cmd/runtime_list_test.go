package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/runtime"
)

// TestListCommand_RegistersUnderRuntime verifies that the list command is
// registered as a subcommand of the runtime command.
//
// Reference: ST-P5-06
func TestListCommand_RegistersUnderRuntime(t *testing.T) {
	runtimeSub, _, err := rootCmd.Find([]string{"runtime", "list"})
	if err != nil {
		t.Fatalf("rootCmd.Find([\"runtime\", \"list\"]) returned error: %v", err)
	}
	if runtimeSub == nil {
		t.Fatal("rootCmd.Find([\"runtime\", \"list\"]) returned nil command")
	}
	if runtimeSub.Use != "list" {
		t.Errorf("command Use = %q, want %q", runtimeSub.Use, "list")
	}

	// Verify it's nested under runtime (not directly under root).
	_, _, err = rootCmd.Find([]string{"list"})
	if err == nil {
		t.Error("rootCmd.Find([\"list\"]) should have failed (list is not a direct subcommand)")
	}
}

// TestListCommand_NoRuntimes verifies that when there are no provisioned
// Runtimes, the command displays "No Runtimes provisioned."
//
// Reference: ST-P5-06 AC-1
func TestListCommand_NoRuntimes(t *testing.T) {
	dir := t.TempDir()
	runtimesPath := filepath.Join(dir, "runtimes.json")

	// Create an empty registry file.
	registry := runtime.NewRuntimeRegistry(runtimesPath)
	if err := registry.Save(); err != nil {
		t.Fatalf("failed to save empty registry: %v", err)
	}

	_, stdout, stderr, err := executeCommand("runtime", "list", "--runtimes-path", runtimesPath)
	if err != nil {
		t.Fatalf("list command returned unexpected error: %v", err)
	}

	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	if !contains(stdout, "No Runtimes provisioned.") {
		t.Errorf("stdout should contain 'No Runtimes provisioned.', got: %s", stdout)
	}
}

// TestListCommand_SingleRuntime verifies that a single Runtime is displayed
// with the correct columns.
//
// Reference: ST-P5-06 AC-2
func TestListCommand_SingleRuntime(t *testing.T) {
	dir := t.TempDir()
	runtimesPath := filepath.Join(dir, "runtimes.json")

	// Create a registry with one entry.
	registry := runtime.NewRuntimeRegistry(runtimesPath)
	entry := runtime.RuntimeEntry{
		ID:          runtime.RuntimeID("a1b2c3d4-e5f6-4789-abcd-ef1234567890"),
		Name:        "web",
		Environment: runtime.EnvProduction,
		InstallPath: filepath.Join(dir, "web"),
		Status:      runtime.StatusActive,
	}
	if err := registry.Register(entry); err != nil {
		t.Fatalf("failed to register entry: %v", err)
	}
	if err := registry.Save(); err != nil {
		t.Fatalf("failed to save registry: %v", err)
	}

	// Create lifecycle and state files in the runtime's install path.
	if err := os.MkdirAll(entry.InstallPath, 0755); err != nil {
		t.Fatalf("failed to create install path: %v", err)
	}

	lifecycle := runtime.NewLifecycle()
	if err := lifecycle.Transition(runtime.StageReady); err != nil {
		t.Fatalf("failed to transition lifecycle: %v", err)
	}
	lifecyclePath := filepath.Join(entry.InstallPath, "lifecycle.json")
	if err := lifecycle.Save(lifecyclePath); err != nil {
		t.Fatalf("failed to save lifecycle: %v", err)
	}

	statePath := filepath.Join(entry.InstallPath, "state.json")
	store := runtime.NewStateStore(statePath)
	store.SetActiveRelease("release-v1.0.0")
	if err := store.Save(); err != nil {
		t.Fatalf("failed to save state: %v", err)
	}

	_, stdout, stderr, err := executeCommand("runtime", "list", "--runtimes-path", runtimesPath)
	if err != nil {
		t.Fatalf("list command returned unexpected error: %v", err)
	}

	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	// Must contain the runtime name.
	if !contains(stdout, "web") {
		t.Errorf("stdout should contain runtime name 'web', got: %s", stdout)
	}

	// Must contain the environment.
	if !contains(stdout, "production") {
		t.Errorf("stdout should contain 'production', got: %s", stdout)
	}

	// Must contain the lifecycle stage.
	if !contains(stdout, "ready") {
		t.Errorf("stdout should contain lifecycle stage 'ready', got: %s", stdout)
	}

	// Must contain the active release.
	if !contains(stdout, "release-v1.0.0") {
		t.Errorf("stdout should contain active release 'release-v1.0.0', got: %s", stdout)
	}

	// Must contain the truncated ID.
	if !contains(stdout, "a1b2c3d4") {
		t.Errorf("stdout should contain truncated ID 'a1b2c3d4', got: %s", stdout)
	}

	// Must NOT contain "No Runtimes provisioned."
	if contains(stdout, "No Runtimes provisioned.") {
		t.Errorf("stdout should not contain empty message when runtimes exist, got: %s", stdout)
	}
}

// TestListCommand_MultipleRuntimes verifies that multiple Runtimes are
// displayed correctly with lifecycle and release info.
//
// Reference: ST-P5-06 AC-3
func TestListCommand_MultipleRuntimes(t *testing.T) {
	dir := t.TempDir()
	runtimesPath := filepath.Join(dir, "runtimes.json")

	registry := runtime.NewRuntimeRegistry(runtimesPath)

	// Runtime 1: web (production, ready, no active release).
	webDir := filepath.Join(dir, "web")
	if err := os.MkdirAll(webDir, 0755); err != nil {
		t.Fatalf("failed to create web dir: %v", err)
	}
	entry1 := runtime.RuntimeEntry{
		ID:          runtime.RuntimeID("b1b2c3d4-e5f6-4789-abcd-ef1234567890"),
		Name:        "web",
		Environment: runtime.EnvProduction,
		InstallPath: webDir,
		Status:      runtime.StatusReady,
	}
	if err := registry.Register(entry1); err != nil {
		t.Fatalf("failed to register web: %v", err)
	}

	lifecycle1 := runtime.NewLifecycle()
	if err := lifecycle1.Transition(runtime.StageReady); err != nil {
		t.Fatalf("failed to transition lifecycle: %v", err)
	}
	if err := lifecycle1.Save(filepath.Join(webDir, "lifecycle.json")); err != nil {
		t.Fatalf("failed to save lifecycle: %v", err)
	}
	// No state file — should show "none" for release.

	// Runtime 2: worker (staging, active, has active release).
	workerDir := filepath.Join(dir, "worker")
	if err := os.MkdirAll(workerDir, 0755); err != nil {
		t.Fatalf("failed to create worker dir: %v", err)
	}
	entry2 := runtime.RuntimeEntry{
		ID:          runtime.RuntimeID("c1b2c3d4-e5f6-4789-abcd-ef1234567890"),
		Name:        "worker",
		Environment: runtime.EnvStaging,
		InstallPath: workerDir,
		Status:      runtime.StatusActive,
	}
	if err := registry.Register(entry2); err != nil {
		t.Fatalf("failed to register worker: %v", err)
	}

	lifecycle2 := runtime.NewLifecycle()
	if err := lifecycle2.Transition(runtime.StageReady); err != nil {
		t.Fatalf("failed to transition lifecycle to ready: %v", err)
	}
	if err := lifecycle2.Transition(runtime.StageActive); err != nil {
		t.Fatalf("failed to transition lifecycle to active: %v", err)
	}
	if err := lifecycle2.Save(filepath.Join(workerDir, "lifecycle.json")); err != nil {
		t.Fatalf("failed to save lifecycle: %v", err)
	}

	store2 := runtime.NewStateStore(filepath.Join(workerDir, "state.json"))
	store2.SetActiveRelease("release-v2.0.0")
	if err := store2.Save(); err != nil {
		t.Fatalf("failed to save state: %v", err)
	}

	if err := registry.Save(); err != nil {
		t.Fatalf("failed to save registry: %v", err)
	}

	_, stdout, stderr, err := executeCommand("runtime", "list", "--runtimes-path", runtimesPath)
	if err != nil {
		t.Fatalf("list command returned unexpected error: %v", err)
	}

	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	// Must contain both runtime names.
	if !contains(stdout, "web") {
		t.Errorf("stdout should contain 'web', got: %s", stdout)
	}
	if !contains(stdout, "worker") {
		t.Errorf("stdout should contain 'worker', got: %s", stdout)
	}

	// Must contain both environments.
	if !contains(stdout, "production") {
		t.Errorf("stdout should contain 'production', got: %s", stdout)
	}
	if !contains(stdout, "staging") {
		t.Errorf("stdout should contain 'staging', got: %s", stdout)
	}

	// Must contain lifecycle stages.
	if !contains(stdout, "ready") {
		t.Errorf("stdout should contain 'ready', got: %s", stdout)
	}
	if !contains(stdout, "active") {
		t.Errorf("stdout should contain 'active', got: %s", stdout)
	}

	// Must contain active release for worker.
	if !contains(stdout, "release-v2.0.0") {
		t.Errorf("stdout should contain 'release-v2.0.0', got: %s", stdout)
	}

	// Must show "none" for web (no state file).
	// The state file doesn't exist, so it shows "none".
	if !contains(stdout, "none") {
		t.Errorf("stdout should contain 'none' release, got: %s", stdout)
	}
}

// TestListCommand_ReadOnly verifies that the list command does not create
// or modify any files.
//
// Reference: ST-P5-06 AC-4
func TestListCommand_ReadOnly(t *testing.T) {
	dir := t.TempDir()
	runtimesPath := filepath.Join(dir, "runtimes.json")

	// Create a registry with one entry.
	registry := runtime.NewRuntimeRegistry(runtimesPath)
	entry := runtime.RuntimeEntry{
		ID:          runtime.RuntimeID("d1b2c3d4-e5f6-4789-abcd-ef1234567890"),
		Name:        "readonly-test",
		Environment: runtime.EnvProduction,
		InstallPath: filepath.Join(dir, "readonly-test"),
		Status:      runtime.StatusProvisioned,
	}
	if err := registry.Register(entry); err != nil {
		t.Fatalf("failed to register entry: %v", err)
	}
	if err := registry.Save(); err != nil {
		t.Fatalf("failed to save registry: %v", err)
	}

	// Create install dir with lifecycle.
	if err := os.MkdirAll(entry.InstallPath, 0755); err != nil {
		t.Fatalf("failed to create install path: %v", err)
	}
	lifecycle := runtime.NewLifecycle()
	if err := lifecycle.Save(filepath.Join(entry.InstallPath, "lifecycle.json")); err != nil {
		t.Fatalf("failed to save lifecycle: %v", err)
	}

	// Capture directory contents before.
	entriesBefore, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read directory before: %v", err)
	}

	// Read the registry file content before.
	contentBefore, err := os.ReadFile(runtimesPath)
	if err != nil {
		t.Fatalf("failed to read registry before: %v", err)
	}

	// Run the list command.
	_, _, _, err = executeCommand("runtime", "list", "--runtimes-path", runtimesPath)
	if err != nil {
		t.Fatalf("list command returned unexpected error: %v", err)
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

	// Verify the registry file content was not modified.
	contentAfter, err := os.ReadFile(runtimesPath)
	if err != nil {
		t.Fatalf("failed to read registry after: %v", err)
	}

	if string(contentBefore) != string(contentAfter) {
		t.Error("registry file content was modified by read-only list command")
	}
}

// TestListCommand_FlagDeduplication verifies that the --runtimes-path flag
// is only on the list subcommand (not on the runtime parent).
//
// Reference: ST-P5-06
func TestListCommand_FlagDeduplication(t *testing.T) {
	// Verify flag is NOT on the runtime parent command.
	runtimeCmdRef, _, err := rootCmd.Find([]string{"runtime"})
	if err != nil {
		t.Fatalf("failed to find runtime command: %v", err)
	}

	flag := runtimeCmdRef.Flags().Lookup("runtimes-path")
	if flag != nil {
		t.Error("runtimes-path flag should not be on the runtime parent command")
	}

	// Verify flag IS on the list subcommand.
	listCmdRef, _, err := rootCmd.Find([]string{"runtime", "list"})
	if err != nil {
		t.Fatalf("failed to find list command: %v", err)
	}

	flag = listCmdRef.Flags().Lookup("runtimes-path")
	if flag == nil {
		t.Error("runtimes-path flag should be on the list subcommand")
	}
}

// TestListCommand_DefaultRuntimesPath verifies that the default
// --runtimes-path value uses the expected default path.
func TestListCommand_DefaultRuntimesPath(t *testing.T) {
	listCmdRef, _, err := rootCmd.Find([]string{"runtime", "list"})
	if err != nil {
		t.Fatalf("failed to find list command: %v", err)
	}

	flag := listCmdRef.Flags().Lookup("runtimes-path")
	if flag == nil {
		t.Fatal("runtimes-path flag not found")
	}

	if flag.DefValue == "" {
		t.Error("default runtimes-path value should not be empty")
	}
}

// TestListCommand_EmptyLifecycleFile verifies graceful handling when a
// Runtime's lifecycle file is missing (shows "unknown" stage).
func TestListCommand_EmptyLifecycleFile(t *testing.T) {
	dir := t.TempDir()
	runtimesPath := filepath.Join(dir, "runtimes.json")

	registry := runtime.NewRuntimeRegistry(runtimesPath)
	entry := runtime.RuntimeEntry{
		ID:          runtime.RuntimeID("e1b2c3d4-e5f6-4789-abcd-ef1234567890"),
		Name:        "no-lifecycle",
		Environment: runtime.EnvDevelopment,
		InstallPath: filepath.Join(dir, "no-lifecycle"),
		Status:      runtime.StatusProvisioned,
	}
	if err := registry.Register(entry); err != nil {
		t.Fatalf("failed to register entry: %v", err)
	}
	if err := registry.Save(); err != nil {
		t.Fatalf("failed to save registry: %v", err)
	}

	// Do NOT create the install dir or lifecycle file.

	_, stdout, stderr, err := executeCommand("runtime", "list", "--runtimes-path", runtimesPath)
	if err != nil {
		t.Fatalf("list command returned unexpected error: %v", err)
	}

	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	// Should show the runtime name.
	if !contains(stdout, "no-lifecycle") {
		t.Errorf("stdout should contain runtime name, got: %s", stdout)
	}

	// Should show "unknown" for the stage (lifecycle file missing).
	if !contains(stdout, "unknown") {
		t.Errorf("stdout should contain 'unknown' stage, got: %s", stdout)
	}
}

// TestListCommand_HeaderShown verifies that the table header is displayed.
func TestListCommand_HeaderShown(t *testing.T) {
	dir := t.TempDir()
	runtimesPath := filepath.Join(dir, "runtimes.json")

	registry := runtime.NewRuntimeRegistry(runtimesPath)
	entry := runtime.RuntimeEntry{
		ID:          runtime.RuntimeID("f1b2c3d4-e5f6-4789-abcd-ef1234567890"),
		Name:        "header-test",
		Environment: runtime.EnvProduction,
		InstallPath: filepath.Join(dir, "header-test"),
		Status:      runtime.StatusProvisioned,
	}
	if err := os.MkdirAll(entry.InstallPath, 0755); err != nil {
		t.Fatalf("failed to create install path: %v", err)
	}
	lifecycle := runtime.NewLifecycle()
	if err := lifecycle.Save(filepath.Join(entry.InstallPath, "lifecycle.json")); err != nil {
		t.Fatalf("failed to save lifecycle: %v", err)
	}

	if err := registry.Register(entry); err != nil {
		t.Fatalf("failed to register entry: %v", err)
	}
	if err := registry.Save(); err != nil {
		t.Fatalf("failed to save registry: %v", err)
	}

	_, stdout, _, err := executeCommand("runtime", "list", "--runtimes-path", runtimesPath)
	if err != nil {
		t.Fatalf("list command returned unexpected error: %v", err)
	}

	// Must contain table header columns.
	if !contains(stdout, "ID") {
		t.Errorf("stdout should contain 'ID' header, got: %s", stdout)
	}
	if !contains(stdout, "Name") {
		t.Errorf("stdout should contain 'Name' header, got: %s", stdout)
	}
	if !contains(stdout, "Environment") {
		t.Errorf("stdout should contain 'Environment' header, got: %s", stdout)
	}
	if !contains(stdout, "Stage") {
		t.Errorf("stdout should contain 'Stage' header, got: %s", stdout)
	}
	if !contains(stdout, "Release") {
		t.Errorf("stdout should contain 'Release' header, got: %s", stdout)
	}
}
