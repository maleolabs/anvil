// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P3-08, EPIC-003
package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/artifact"
)

// ---------------------------------------------------------------------------
// Artifact Status Command Tests — ST-P3-08
// ---------------------------------------------------------------------------

// TestArtifactStatusCmd_Registered verifies that the status subcommand is
// registered under the artifact command.
func TestArtifactStatusCmd_Registered(t *testing.T) {
	var artifactSub *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Use == "artifact" {
			artifactSub = c
			break
		}
	}

	if artifactSub == nil {
		t.Fatal("artifact command not found")
	}

	found := false
	for _, c := range artifactSub.Commands() {
		if c.Use == "status <identity>" {
			found = true
			break
		}
	}

	if !found {
		t.Error("status subcommand not found under artifact command")
	}
}

// TestArtifactStatusCmd_Usage verifies the status command has expected usage.
func TestArtifactStatusCmd_Usage(t *testing.T) {
	if artifactStatusCmd.Short == "" {
		t.Error("status command short description is empty")
	}

	if artifactStatusCmd.Long == "" {
		t.Error("status command long description is empty")
	}

	if artifactStatusCmd.Use != "status <identity>" {
		t.Errorf("status command Use = %q, want %q", artifactStatusCmd.Use, "status <identity>")
	}
}

// TestArtifactStatusCmd_RunE verifies the status command has a RunE handler set.
func TestArtifactStatusCmd_RunE(t *testing.T) {
	if artifactStatusCmd.RunE == nil {
		t.Error("status command RunE handler is nil")
	}
}

// TestArtifactStatusCmd_ExactArgs verifies the status command requires exactly 1 arg.
func TestArtifactStatusCmd_ExactArgs(t *testing.T) {
	if artifactStatusCmd.Args == nil {
		t.Error("status command Args validator is nil, expected cobra.ExactArgs(1)")
		return
	}

	cmd := &cobra.Command{Use: "status"}

	// 0 args should fail.
	err := artifactStatusCmd.Args(cmd, []string{})
	if err == nil {
		t.Error("expected error for 0 arguments, got nil")
	}

	// 1 arg should pass.
	err = artifactStatusCmd.Args(cmd, []string{"some-artifact-id"})
	if err != nil {
		t.Errorf("expected no error for 1 argument, got: %v", err)
	}

	// 2 args should fail.
	err = artifactStatusCmd.Args(cmd, []string{"a", "b"})
	if err == nil {
		t.Error("expected error for 2 arguments, got nil")
	}
}

// TestArtifactStatusCmd_NotRegistered verifies the output when the artifact
// is not registered and no lifecycle state file exists.
func TestArtifactStatusCmd_NotRegistered(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".anvil", "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	_, stdout, stderr, err := executeCommand(
		"artifact", "status", "unknown-artifact",
		"--state-dir", stateDir,
	)
	if err != nil {
		t.Fatalf("execute command returned unexpected error: %v", err)
	}

	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	if !strings.Contains(stdout, "unknown-artifact") {
		t.Errorf("stdout should contain artifact identity, got: %s", stdout)
	}
	if !strings.Contains(stdout, "not registered") {
		t.Errorf("stdout should indicate 'not registered', got: %s", stdout)
	}
}

// TestArtifactStatusCmd_RegisteredArtifact verifies the output for a
// registered artifact.
func TestArtifactStatusCmd_RegisteredArtifact(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".anvil", "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Create a registration store with a registered artifact.
	regPath := filepath.Join(stateDir, "registration-index.json")
	store := artifact.NewRegistrationStore(regPath)

	manifest := &artifact.Manifest{
		ArtifactID:   "registered-artifact-1",
		Version:      "1.0.0",
		ProjectID:    "test-project",
		Checksum:     "abc123",
		ChecksumType: artifact.ChecksumAlgorithmSHA256,
		CreatedAt:    "2026-07-25T12:00:00Z",
		Source:       "test-project",
	}

	if _, err := store.Register(manifest, "passed"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	_, stdout, stderr, err := executeCommand(
		"artifact", "status", "registered-artifact-1",
		"--state-dir", stateDir,
	)
	if err != nil {
		t.Fatalf("execute command returned unexpected error: %v", err)
	}

	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	if !strings.Contains(stdout, "registered-artifact-1") {
		t.Errorf("stdout should contain artifact identity, got: %s", stdout)
	}
	if !strings.Contains(stdout, "registered") {
		t.Errorf("stdout should indicate 'registered', got: %s", stdout)
	}
	if !strings.Contains(stdout, "1.0.0") {
		t.Errorf("stdout should contain version '1.0.0', got: %s", stdout)
	}
	if !strings.Contains(stdout, "test-project") {
		t.Errorf("stdout should contain project ID, got: %s", stdout)
	}
	if !strings.Contains(stdout, "registered (default)") {
		t.Errorf("stdout should indicate 'registered (default)' lifecycle, got: %s", stdout)
	}
}

// TestArtifactStatusCmd_WithLifecycle verifies the output when a lifecycle
// state machine file exists for the artifact.
func TestArtifactStatusCmd_WithLifecycle(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".anvil", "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Create a registration store with a registered artifact.
	regPath := filepath.Join(stateDir, "registration-index.json")
	store := artifact.NewRegistrationStore(regPath)

	manifest := &artifact.Manifest{
		ArtifactID:   "lifecycle-artifact",
		Version:      "2.0.0",
		ProjectID:    "test-project",
		Checksum:     "def456",
		ChecksumType: artifact.ChecksumAlgorithmSHA256,
		CreatedAt:    "2026-07-25T12:00:00Z",
		Source:       "test-project",
	}

	if _, err := store.Register(manifest, "passed"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Create a lifecycle state machine with some transitions.
	lsm := artifact.NewLifecycleStateMachine(artifact.StageCreated)
	if err := lsm.Transition(artifact.StageVerified); err != nil {
		t.Fatalf("Transition to Verified: %v", err)
	}
	if err := lsm.Transition(artifact.StageRegistered); err != nil {
		t.Fatalf("Transition to Registered: %v", err)
	}
	// Also attempt an invalid transition (for history display).
	_ = lsm.Transition(artifact.StageConsumed)

	lifecyclePath := filepath.Join(stateDir, "lifecycle-artifact.json")
	if err := lsm.Save(lifecyclePath); err != nil {
		t.Fatalf("Save lifecycle: %v", err)
	}

	_, stdout, stderr, err := executeCommand(
		"artifact", "status", "lifecycle-artifact",
		"--state-dir", stateDir,
	)
	if err != nil {
		t.Fatalf("execute command returned unexpected error: %v", err)
	}

	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	// Should show the lifecycle stage.
	if !strings.Contains(stdout, "registered") {
		t.Errorf("stdout should contain 'registered' stage, got: %s", stdout)
	}

	// Should show transition history.
	if !strings.Contains(stdout, "Transitions:") {
		t.Errorf("stdout should contain 'Transitions:', got: %s", stdout)
	}

	// Should show the failed transition as well.
	if !strings.Contains(stdout, "✗") {
		t.Errorf("stdout should contain failed transition marker, got: %s", stdout)
	}
}

// TestArtifactStatusCmd_MissingStateDir verifies error handling when the
// state directory does not exist.
func TestArtifactStatusCmd_MissingStateDir(t *testing.T) {
	_, _, stderr, err := executeCommand(
		"artifact", "status", "some-artifact",
		"--state-dir", "/nonexistent/path/state",
	)

	if err == nil {
		t.Fatal("expected error for non-existent state dir, got nil")
	}

	if !strings.Contains(stderr, "Error:") {
		t.Errorf("expected error in stderr, got: %s", stderr)
	}
}

// TestArtifactStatusCmd_ReadOnly verifies that the status command does not
// create or modify any files.
func TestArtifactStatusCmd_ReadOnly(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".anvil", "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Create a registration store with a registered artifact.
	regPath := filepath.Join(stateDir, "registration-index.json")
	store := artifact.NewRegistrationStore(regPath)

	manifest := &artifact.Manifest{
		ArtifactID:   "readonly-artifact",
		Version:      "1.0.0",
		ProjectID:    "test-project",
		Checksum:     "xxyyzz",
		ChecksumType: artifact.ChecksumAlgorithmSHA256,
		CreatedAt:    "2026-07-25T12:00:00Z",
		Source:       "test-project",
	}

	if _, err := store.Register(manifest, "passed"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Capture directory contents before.
	entriesBefore, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatalf("ReadDir before: %v", err)
	}

	// Read the registration file content before.
	regContentBefore, err := os.ReadFile(regPath)
	if err != nil {
		t.Fatalf("ReadFile before: %v", err)
	}

	// Run the status command.
	_, _, _, err = executeCommand(
		"artifact", "status", "readonly-artifact",
		"--state-dir", stateDir,
	)
	if err != nil {
		t.Fatalf("execute command returned unexpected error: %v", err)
	}

	// Verify no new files were created.
	entriesAfter, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatalf("ReadDir after: %v", err)
	}

	if len(entriesBefore) != len(entriesAfter) {
		t.Errorf("directory entry count changed: before=%d, after=%d",
			len(entriesBefore), len(entriesAfter))
	}

	// Verify the registration file content was not modified.
	regContentAfter, err := os.ReadFile(regPath)
	if err != nil {
		t.Fatalf("ReadFile after: %v", err)
	}

	if string(regContentBefore) != string(regContentAfter) {
		t.Error("registration file was modified by read-only status command")
	}
}

// TestArtifactStatusCmd_StateDirFlag verifies the --state-dir flag.
func TestArtifactStatusCmd_StateDirFlag(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "custom-state-dir")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Create a registration store.
	regPath := filepath.Join(stateDir, "registration-index.json")
	store := artifact.NewRegistrationStore(regPath)

	manifest := &artifact.Manifest{
		ArtifactID:   "flag-test-artifact",
		Version:      "3.0.0",
		ProjectID:    "flag-project",
		Checksum:     "flag123",
		ChecksumType: artifact.ChecksumAlgorithmSHA256,
	}

	if _, err := store.Register(manifest, "passed"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	_, stdout, stderr, err := executeCommand(
		"artifact", "status", "flag-test-artifact",
		"--state-dir", stateDir,
	)
	if err != nil {
		t.Fatalf("execute command returned unexpected error: %v", err)
	}

	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	if !strings.Contains(stdout, "flag-test-artifact") {
		t.Errorf("stdout should contain artifact identity, got: %s", stdout)
	}
	if !strings.Contains(stdout, "3.0.0") {
		t.Errorf("stdout should contain version '3.0.0', got: %s", stdout)
	}
}

// TestArtifactStatusCmd_UnregisteredWithLifecycle verifies output when an
// artifact has a lifecycle state file but is not in the registration store.
func TestArtifactStatusCmd_UnregisteredWithLifecycle(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".anvil", "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Create a lifecycle state machine but no registration.
	lsm := artifact.NewLifecycleStateMachine(artifact.StageVerified)
	lifecyclePath := filepath.Join(stateDir, "orphan-artifact.json")
	if err := lsm.Save(lifecyclePath); err != nil {
		t.Fatalf("Save lifecycle: %v", err)
	}

	_, stdout, stderr, err := executeCommand(
		"artifact", "status", "orphan-artifact",
		"--state-dir", stateDir,
	)
	if err != nil {
		t.Fatalf("execute command returned unexpected error: %v", err)
	}

	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	if !strings.Contains(stdout, "not registered") {
		t.Errorf("stdout should indicate 'not registered', got: %s", stdout)
	}

	// Should still show the lifecycle state.
	if !strings.Contains(stdout, "verified") {
		t.Errorf("stdout should show lifecycle state 'verified', got: %s", stdout)
	}
}
