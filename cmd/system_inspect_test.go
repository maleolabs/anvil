// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P9-04
package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/release"
)

// TestSystemInspectCommand_RegistersUnderSystem verifies that the inspect
// parent command is registered under system with all five component
// subcommands.
//
// Reference: ST-P9-04
func TestSystemInspectCommand_RegistersUnderSystem(t *testing.T) {
	systemSub, _, err := rootCmd.Find([]string{"system", "inspect"})
	if err != nil {
		t.Fatalf("rootCmd.Find([\"system\", \"inspect\"]) returned error: %v", err)
	}
	if systemSub == nil {
		t.Fatal("rootCmd.Find([\"system\", \"inspect\"]) returned nil command")
	}
	if systemSub.Use != "inspect" {
		t.Errorf("command Use = %q, want %q", systemSub.Use, "inspect")
	}

	expectedSubs := []string{"environment", "runtime", "config", "release", "deps"}
	registered := make(map[string]bool)
	for _, sub := range systemSub.Commands() {
		registered[sub.Name()] = true
	}
	for _, name := range expectedSubs {
		if !registered[name] {
			t.Errorf("system inspect is missing subcommand %q", name)
		}
	}

	// Verify it is nested under system (not directly under root).
	_, _, err = rootCmd.Find([]string{"inspect"})
	if err == nil {
		t.Error("rootCmd.Find([\"inspect\"]) should have failed (inspect is not a direct subcommand)")
	}
}

// TestSystemInspectCommand_Flags verifies the --server-root and --json
// flags on every inspect subcommand. The config subcommand is
// project-scoped and intentionally has no --server-root flag.
//
// Reference: ST-P9-04
func TestSystemInspectCommand_Flags(t *testing.T) {
	jsonSubs := []string{"environment", "runtime", "config", "release", "deps"}
	serverRootSubs := []string{"environment", "runtime", "release", "deps"}

	for _, name := range jsonSubs {
		cmd, _, err := rootCmd.Find([]string{"system", "inspect", name})
		if err != nil {
			t.Fatalf("failed to find inspect %s: %v", name, err)
		}
		if cmd.Flags().Lookup("json") == nil {
			t.Errorf("inspect %s: --json flag should be registered", name)
		}
	}

	for _, name := range serverRootSubs {
		cmd, _, err := rootCmd.Find([]string{"system", "inspect", name})
		if err != nil {
			t.Fatalf("failed to find inspect %s: %v", name, err)
		}
		if cmd.Flags().Lookup("server-root") == nil {
			t.Errorf("inspect %s: --server-root flag should be registered", name)
		}
	}

	configCmd, _, err := rootCmd.Find([]string{"system", "inspect", "config"})
	if err != nil {
		t.Fatalf("failed to find inspect config: %v", err)
	}
	if configCmd.Flags().Lookup("server-root") != nil {
		t.Error("inspect config: --server-root flag should not be registered (config is project-scoped)")
	}
}

// TestSystemInspectEnvironment_Healthy verifies that environment inspection
// reports the Active Release (symlink target), symlink status, and
// directory integrity.
//
// Reference: ST-P9-04 (ST-009-004 AC1)
func TestSystemInspectEnvironment_Healthy(t *testing.T) {
	dir := t.TempDir()
	setupHealthyServerRoot(t, dir)

	_, stdout, stderr, err := executeCommand("system", "inspect", "environment", "--server-root", dir)
	if err != nil {
		t.Fatalf("inspect environment returned unexpected error: %v", err)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	if !contains(stdout, "active_symlink") || !contains(stdout, "[PASS]") {
		t.Errorf("stdout should report the symlink check as passed, got: %s", stdout)
	}
	if !contains(stdout, "release_directories") {
		t.Errorf("stdout should report the directory integrity check, got: %s", stdout)
	}
	// The Active Release is reported through the symlink target.
	if !contains(stdout, "releases/release-1") {
		t.Errorf("stdout should report the Active Release symlink target, got: %s", stdout)
	}
}

// TestSystemInspectRuntime_Healthy verifies that runtime inspection reports
// the Active Release, shared resource status, and operational status.
//
// Reference: ST-P9-04 (ST-009-004 AC2)
func TestSystemInspectRuntime_Healthy(t *testing.T) {
	dir := t.TempDir()
	setupHealthyServerRoot(t, dir)

	_, stdout, _, err := executeCommand("system", "inspect", "runtime", "--server-root", dir)
	if err != nil {
		t.Fatalf("inspect runtime returned unexpected error: %v", err)
	}

	if !contains(stdout, "active_symlink") {
		t.Errorf("stdout should report the Active Release check, got: %s", stdout)
	}
	if !contains(stdout, "shared_resources") {
		t.Errorf("stdout should report the shared resource status, got: %s", stdout)
	}
	if !contains(stdout, "runtime_config") {
		t.Errorf("stdout should report the operational status, got: %s", stdout)
	}
	if !contains(stdout, "all checks passed") {
		t.Errorf("stdout should report the component passed, got: %s", stdout)
	}
}

// TestSystemInspectConfig_Healthy verifies that configuration inspection
// reports completeness, validity, and resolution checks with the
// configured project values.
//
// Reference: ST-P9-04 (ST-009-004 AC3)
func TestSystemInspectConfig_Healthy(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	dir := t.TempDir()
	configContent := "project:\n  name: inspect-test\n  version: 1.2.3\n"
	if err := os.WriteFile(filepath.Join(dir, "anvil.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current working directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(orig); err != nil {
			t.Errorf("failed to restore original directory %q: %v", orig, err)
		}
	}()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change to project directory: %v", err)
	}

	_, stdout, stderr, err := executeCommand("system", "inspect", "config")
	if err != nil {
		t.Fatalf("inspect config returned unexpected error: %v", err)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	if !contains(stdout, "completeness") || !contains(stdout, "validity") || !contains(stdout, "resolution") {
		t.Errorf("stdout should report completeness/validity/resolution checks, got: %s", stdout)
	}
	if !contains(stdout, "all checks passed") {
		t.Errorf("stdout should report the component passed, got: %s", stdout)
	}
}

// TestSystemInspectConfig_NotAvailable verifies that when no project
// configuration exists, the config inspection reports the component as not
// available and exits with code 0.
//
// Reference: ST-P9-04 (ST-009-004 AC6)
func TestSystemInspectConfig_NotAvailable(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	dir := t.TempDir() // empty — no project configuration anywhere

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current working directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(orig); err != nil {
			t.Errorf("failed to restore original directory %q: %v", orig, err)
		}
	}()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change to empty directory: %v", err)
	}

	_, stdout, stderr, err := executeCommand("system", "inspect", "config")
	if err != nil {
		t.Fatalf("inspect config should exit 0 when the component is not available, got error: %v", err)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	if !contains(stdout, "component not available") || !contains(stdout, "no project configuration found") {
		t.Errorf("stdout should report the component as not available, got: %s", stdout)
	}

	// JSON mode reports availability=false in the standard envelope.
	_, jsonOut, _, err := executeCommand("system", "inspect", "config", "--json")
	if err != nil {
		t.Fatalf("inspect config --json should exit 0 when the component is not available, got error: %v", err)
	}
	if !contains(jsonOut, "\"available\": false") {
		t.Errorf("JSON output should contain available=false, got: %s", jsonOut)
	}
}

// TestSystemInspectConfig_Invalid verifies that a broken project
// configuration is reported with a failing config_load check and exit
// code 1.
//
// Reference: ST-P9-04 (ST-009-004 AC3)
func TestSystemInspectConfig_Invalid(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	dir := t.TempDir()
	invalidConfig := "project:\n  name: 12345\n" // project.name must be a string
	if err := os.WriteFile(filepath.Join(dir, "anvil.yaml"), []byte(invalidConfig), 0644); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current working directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(orig); err != nil {
			t.Errorf("failed to restore original directory %q: %v", orig, err)
		}
	}()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change to project directory: %v", err)
	}

	_, stdout, stderr, err := executeCommand("system", "inspect", "config")
	if err == nil {
		t.Fatal("expected error (exit code 1) for invalid configuration, got nil")
	}

	if !contains(stdout, "[FAIL] config_load") {
		t.Errorf("stdout should report the failing config_load check, got: %s", stdout)
	}
	if !contains(stderr, "config inspection found 1 failed check(s)") {
		t.Errorf("stderr should contain the inspection failure, got: %s", stderr)
	}
}

// TestSystemInspectRelease_Healthy verifies that release inspection reports
// the lifecycle stage and history for the specified Release, plus the
// release infrastructure checks.
//
// Reference: ST-P9-04 (ST-009-004 AC4)
func TestSystemInspectRelease_Healthy(t *testing.T) {
	serverRoot, projectID, releaseID := setupReadinessEnvironment(t, readinessSetupOptions{
		artifactRegistered: true,
		releaseStage:       release.StageReady,
	})

	_, stdout, stderr, err := executeCommand("system", "inspect", "release", projectID, releaseID, "--server-root", serverRoot)
	if err != nil {
		t.Fatalf("inspect release returned unexpected error: %v", err)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	// Infrastructure checks.
	if !contains(stdout, "release_directory") {
		t.Errorf("stdout should report the release directory check, got: %s", stdout)
	}
	if !contains(stdout, "artifact_presence") {
		t.Errorf("stdout should report the artifact presence check, got: %s", stdout)
	}
	if !contains(stdout, "shared_links") {
		t.Errorf("stdout should report the shared links check, got: %s", stdout)
	}

	// Lifecycle stage and history (EPIC-004 state).
	if !contains(stdout, "release_stage") || !contains(stdout, "is in stage ready") {
		t.Errorf("stdout should report the lifecycle stage, got: %s", stdout)
	}
	if !contains(stdout, "release_history") {
		t.Errorf("stdout should report the release history check, got: %s", stdout)
	}
	if !contains(stdout, "no transitions recorded") {
		t.Errorf("stdout should report the history state, got: %s", stdout)
	}
}

// TestSystemInspectRelease_History verifies that release inspection renders
// the recorded transition history of the specified Release.
//
// Reference: ST-P9-04 (ST-009-004 AC4)
func TestSystemInspectRelease_History(t *testing.T) {
	serverRoot, projectID, releaseID := setupReadinessEnvironment(t, readinessSetupOptions{
		artifactRegistered: true,
		releaseStage:       release.StageActive,
	})

	// Append a transition record to the persisted Release.
	rel, err := release.LookupByID(filepath.Join(serverRoot, "projects", projectID), release.ReleaseID(releaseID))
	if err != nil {
		t.Fatalf("lookup release: %v", err)
	}
	rel.Transitions = []release.TransitionRecord{
		{Timestamp: "2026-07-31T00:00:00Z", From: release.StageReady, To: release.StageActivating, Outcome: "success"},
		{Timestamp: "2026-07-31T00:01:00Z", From: release.StageActivating, To: release.StageActive, Outcome: "success"},
	}
	if err := rel.Save(rel.SavePath(filepath.Join(serverRoot, "projects", projectID))); err != nil {
		t.Fatalf("save release JSON: %v", err)
	}

	_, stdout, _, err := executeCommand("system", "inspect", "release", projectID, releaseID, "--server-root", serverRoot)
	if err != nil {
		t.Fatalf("inspect release returned unexpected error: %v", err)
	}

	if !contains(stdout, "2 transition(s) recorded") {
		t.Errorf("stdout should report the transition count, got: %s", stdout)
	}
	if !contains(stdout, "Transitions:") {
		t.Errorf("stdout should render the transitions section, got: %s", stdout)
	}
	if !contains(stdout, "ready") || !contains(stdout, "active") {
		t.Errorf("stdout should render the transition stages, got: %s", stdout)
	}
}

// TestSystemInspectRelease_NotFound verifies that inspecting a Release that
// does not exist reports a clear error (exit code 1).
//
// Reference: ST-P9-04 (ST-009-004 — component not available for identity)
func TestSystemInspectRelease_NotFound(t *testing.T) {
	serverRoot, projectID, _ := setupReadinessEnvironment(t, readinessSetupOptions{
		artifactRegistered: true,
		releaseStage:       release.StageReady,
	})

	_, _, stderr, err := executeCommand("system", "inspect", "release", projectID, "rel-does-not-exist", "--server-root", serverRoot)
	if err == nil {
		t.Fatal("expected error for unknown release, got nil")
	}
	if !contains(stderr, "Release \"rel-does-not-exist\" not found") {
		t.Errorf("stderr should report the missing release, got: %s", stderr)
	}
}

// TestSystemInspectRelease_MissingArgs verifies that running the release
// inspection without project and release identity produces a clear usage
// error.
//
// Reference: ST-P9-04 (ST-009-004 AC4)
func TestSystemInspectRelease_MissingArgs(t *testing.T) {
	dir := t.TempDir()

	_, _, stderr, err := executeCommand("system", "inspect", "release", "--server-root", dir)
	if err == nil {
		t.Fatal("expected usage error without project/release identity, got nil")
	}
	if !contains(stderr, "project-id") || !contains(stderr, "release-id") {
		t.Errorf("stderr should explain the required arguments, got: %s", stderr)
	}
}

// TestSystemInspectRelease_Json verifies that --json includes the lifecycle
// stage and transition history alongside the component checks.
//
// Reference: ST-P9-04 (ST-009-004 AC4)
func TestSystemInspectRelease_Json(t *testing.T) {
	serverRoot, projectID, releaseID := setupReadinessEnvironment(t, readinessSetupOptions{
		artifactRegistered: true,
		releaseStage:       release.StageActive,
	})

	// Record a transition so the history is non-empty and rendered.
	rel, err := release.LookupByID(filepath.Join(serverRoot, "projects", projectID), release.ReleaseID(releaseID))
	if err != nil {
		t.Fatalf("lookup release: %v", err)
	}
	rel.Transitions = []release.TransitionRecord{
		{Timestamp: "2026-07-31T00:00:00Z", From: release.StageReady, To: release.StageActive, Outcome: "success"},
	}
	if err := rel.Save(rel.SavePath(filepath.Join(serverRoot, "projects", projectID))); err != nil {
		t.Fatalf("save release JSON: %v", err)
	}

	_, stdout, _, err := executeCommand("system", "inspect", "release", projectID, releaseID, "--server-root", serverRoot, "--json")
	if err != nil {
		t.Fatalf("inspect release --json returned unexpected error: %v", err)
	}

	if !contains(stdout, "\"version\": \"1\"") {
		t.Errorf("JSON output should contain the envelope version, got: %s", stdout)
	}
	if !contains(stdout, "\"release_id\"") {
		t.Errorf("JSON output should contain the release_id field, got: %s", stdout)
	}
	if !contains(stdout, "\"stage\": \"active\"") {
		t.Errorf("JSON output should contain the lifecycle stage, got: %s", stdout)
	}
	if !contains(stdout, "\"transitions\"") {
		t.Errorf("JSON output should contain the transitions field, got: %s", stdout)
	}
	if !contains(stdout, "release_history") {
		t.Errorf("JSON output should contain the release history check, got: %s", stdout)
	}
}

// TestSystemInspectDeps verifies that dependency inspection reports the
// availability of required external tools.
//
// Reference: ST-P9-04 (ST-009-004 AC5)
func TestSystemInspectDeps(t *testing.T) {
	dir := t.TempDir()

	_, stdout, _, err := executeCommand("system", "inspect", "deps", "--server-root", dir)
	if err != nil {
		t.Fatalf("inspect deps returned unexpected error: %v", err)
	}

	if !contains(stdout, "Dependency Inspection") {
		t.Errorf("stdout should contain the dependency inspection header, got: %s", stdout)
	}
	for _, tool := range []string{"tool_php", "tool_node", "tool_composer", "tool_npm", "tool_git"} {
		if !contains(stdout, tool) {
			t.Errorf("stdout should report tool %q, got: %s", tool, stdout)
		}
	}
	// Every tool is either found (location) or missing (installation guidance).
	if !contains(stdout, "found at") && !contains(stdout, "not found in PATH") {
		t.Errorf("stdout should report tool locations or missing tools, got: %s", stdout)
	}
}

// TestSystemInspectDeps_Json verifies that --json produces the standard
// envelope with the dependency checks.
//
// Reference: ST-P9-04 (ST-009-004 AC5)
func TestSystemInspectDeps_Json(t *testing.T) {
	dir := t.TempDir()

	_, stdout, _, err := executeCommand("system", "inspect", "deps", "--server-root", dir, "--json")
	if err != nil {
		t.Fatalf("inspect deps --json returned unexpected error: %v", err)
	}

	if !contains(stdout, "\"version\": \"1\"") {
		t.Errorf("JSON output should contain the envelope version, got: %s", stdout)
	}
	if !contains(stdout, "\"component\": \"deps\"") {
		t.Errorf("JSON output should contain the component field, got: %s", stdout)
	}
	if !contains(stdout, "\"checks\"") {
		t.Errorf("JSON output should contain the checks field, got: %s", stdout)
	}
	if !contains(stdout, "tool_php") {
		t.Errorf("JSON output should contain the tool checks, got: %s", stdout)
	}
}

// TestSystemInspectEnvironment_Failing verifies that a failing component
// inspection exits with code 1 and reports the failed checks.
//
// Reference: ST-P9-04
func TestSystemInspectEnvironment_Failing(t *testing.T) {
	dir := t.TempDir()
	// Don't set up anything — the runtime environment is missing.

	_, stdout, stderr, err := executeCommand("system", "inspect", "environment", "--server-root", dir)
	if err == nil {
		t.Fatal("expected error (exit code 1) for a failing environment, got nil")
	}

	if !contains(stdout, "[FAIL] active_symlink") {
		t.Errorf("stdout should report the failed symlink check, got: %s", stdout)
	}
	if !contains(stderr, "environment inspection found") {
		t.Errorf("stderr should contain the inspection failure, got: %s", stderr)
	}
}

// TestSystemInspectRuntime_Json verifies that --json produces the standard
// envelope with the component and per-check details.
//
// Reference: ST-P9-04
func TestSystemInspectRuntime_Json(t *testing.T) {
	dir := t.TempDir()
	setupHealthyServerRoot(t, dir)

	_, stdout, _, err := executeCommand("system", "inspect", "runtime", "--server-root", dir, "--json")
	if err != nil {
		t.Fatalf("inspect runtime --json returned unexpected error: %v", err)
	}

	if !contains(stdout, "\"version\": \"1\"") {
		t.Errorf("JSON output should contain the envelope version, got: %s", stdout)
	}
	if !contains(stdout, "\"component\": \"runtime\"") {
		t.Errorf("JSON output should contain the component field, got: %s", stdout)
	}
	if !contains(stdout, "\"checks\"") {
		t.Errorf("JSON output should contain the checks field, got: %s", stdout)
	}
}

// TestSystemInspectRelease_SecureExecution verifies that inspection
// commands do not modify any platform state.
//
// Reference: ST-P9-04 (ST-009-004 AC7)
func TestSystemInspectRelease_SecureExecution(t *testing.T) {
	serverRoot, projectID, releaseID := setupReadinessEnvironment(t, readinessSetupOptions{
		artifactRegistered: true,
		releaseStage:       release.StageReady,
	})

	entriesBefore, err := os.ReadDir(serverRoot)
	if err != nil {
		t.Fatalf("failed to read directory before: %v", err)
	}

	_, _, _, _ = executeCommand("system", "inspect", "release", projectID, releaseID, "--server-root", serverRoot)

	entriesAfter, err := os.ReadDir(serverRoot)
	if err != nil {
		t.Fatalf("failed to read directory after: %v", err)
	}

	if len(entriesBefore) != len(entriesAfter) {
		t.Errorf("directory contents changed: before=%d entries, after=%d entries",
			len(entriesBefore), len(entriesAfter))
	}
}
