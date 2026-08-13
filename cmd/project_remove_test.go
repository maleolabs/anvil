package cmd

import (
	"bytes"
	"os"
	"testing"

	"maleolabs.com/anvil/internal/project"
)

// TestRemove_WithForceFlag tests removal by initializing a project in a
// specific directory and removing it using --force, verifying the directory cleanup.
func TestRemove_WithForceFlag(t *testing.T) {
	dir := t.TempDir()

	// Create a project.
	_, _, _, err := executeCommand("init", "my-app", "--path", dir)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}

	s := project.NewStructure(dir)

	// Verify project exists before removal.
	if !fileExists(s.ConfigFile) {
		t.Fatal("expected project config to exist before removal")
	}

	// Since project.Discover() walks from cwd, we need to chdir into the
	// project directory for discovery to find it.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	defer func() {
		_ = os.Chdir(origDir)
	}()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir to project dir: %v", err)
	}

	// Remove with --force flag.
	_, stdout, stderr, err := executeCommand("project", "remove", "--force")
	if err != nil {
		t.Fatalf("remove command failed: %v\nstderr: %s", err, stderr)
	}

	// Verify success message.
	if !contains(stdout, "has been removed") {
		t.Errorf("expected success message containing 'has been removed', got: %s", stdout)
	}

	// Verify project directory is gone.
	if fileExists(dir) {
		t.Error("expected project directory to be removed, but it still exists")
	}
}

// TestRemove_NonExistentProject verifies that running:
//
//	anvil project remove
//
// in a directory without an Anvil project produces a guidance error
// via RequireProject.
func TestRemove_NonExistentProject(t *testing.T) {
	dir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	defer func() {
		_ = os.Chdir(origDir)
	}()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir to temp dir: %v", err)
	}

	_, _, stderr, err := executeCommand("project", "remove")
	if err == nil {
		t.Fatal("expected error for non-existent project, got nil")
	}
	if !contains(stderr, "no anvil project found") {
		t.Errorf("expected guidance about missing project, got: %s", stderr)
	}
}

// TestRemove_WithoutForceFlag_ShowsWarning verifies that running:
//
//	anvil project remove
//
// without --force shows the confirmation prompt and, when declined,
// does not remove the project.
func TestRemove_WithoutForceFlag_ShowsWarning(t *testing.T) {
	dir := t.TempDir()

	// Create a project.
	_, _, _, err := executeCommand("init", "test-project", "--path", dir)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}

	s := project.NewStructure(dir)

	// Verify project exists before removal.
	if !fileExists(s.ConfigFile) {
		t.Fatal("expected project config to exist before removal")
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	defer func() {
		_ = os.Chdir(origDir)
	}()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir to project dir: %v", err)
	}

	// Run remove without --force, providing "n" as stdin input.
	rootCmd.SetIn(bytes.NewBufferString("n\n"))
	_, stdout, stderr, err := executeCommand("project", "remove")
	// Reset stdin after use.
	rootCmd.SetIn(nil)

	// The command should NOT return an error — cancellation is not an error.
	if err != nil {
		t.Errorf("expected no error for cancelled removal, got: %v\nstderr: %s", err, stderr)
	}

	// Verify cancellation message.
	if !contains(stdout, "Removal cancelled") {
		t.Errorf("expected cancellation message, got: %s", stdout)
	}

	// Verify project still exists.
	if !fileExists(s.ConfigFile) {
		t.Error("expected project to still exist after cancelled removal")
	}
	if !fileExists(dir) {
		t.Error("expected project directory to still exist after cancelled removal")
	}
}

// TestRemove_MultiProjectIsolation verifies that removing one project
// does not affect another project in a different directory.
func TestRemove_MultiProjectIsolation(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	// Initialize two projects.
	_, _, _, err := executeCommand("init", "project-one", "--path", dir1)
	if err != nil {
		t.Fatalf("init project-one failed: %v", err)
	}

	_, _, _, err = executeCommand("init", "project-two", "--path", dir2)
	if err != nil {
		t.Fatalf("init project-two failed: %v", err)
	}

	s1 := project.NewStructure(dir1)
	s2 := project.NewStructure(dir2)

	// Verify both exist.
	if !fileExists(s1.ConfigFile) {
		t.Fatal("expected project-one config to exist")
	}
	if !fileExists(s2.ConfigFile) {
		t.Fatal("expected project-two config to exist")
	}

	// Remove project-one by changing into its directory and running remove.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	defer func() {
		_ = os.Chdir(origDir)
	}()

	if err := os.Chdir(dir1); err != nil {
		t.Fatalf("failed to chdir to project-one: %v", err)
	}

	_, stdout, stderr, err := executeCommand("project", "remove", "--force")
	if err != nil {
		t.Fatalf("remove project-one failed: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, "has been removed") {
		t.Errorf("expected success message, got: %s", stdout)
	}

	// Verify project-one directory is removed.
	if fileExists(dir1) {
		t.Error("expected project-one directory to be removed")
	}

	// Verify project-two still exists untouched.
	if !fileExists(s2.ConfigFile) {
		t.Error("expected project-two config to still exist")
	}
	if !fileExists(dir2) {
		t.Error("expected project-two directory to still exist")
	}
}

// TestRemove_WithForceFlag_WithoutProject checks that using --force in a
// directory without a project still produces an error (RequireProject runs
// before the force check).
func TestRemove_WithForceFlag_WithoutProject(t *testing.T) {
	dir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	defer func() {
		_ = os.Chdir(origDir)
	}()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir to temp dir: %v", err)
	}

	_, _, stderr, err := executeCommand("project", "remove", "--force")
	if err == nil {
		t.Fatal("expected error for non-existent project with --force, got nil")
	}
	if !contains(stderr, "no anvil project found") {
		t.Errorf("expected guidance about missing project, got: %s", stderr)
	}
}

// TestRemove_NonInteractiveWithoutForceRefused verifies that when running
// in a non-interactive context (empty stdin / EOF) without the --force flag,
// the removal is refused with an error message telling the user to use --force.
func TestRemove_NonInteractiveWithoutForceRefused(t *testing.T) {
	dir := t.TempDir()

	// Create a project.
	_, _, _, err := executeCommand("init", "non-interactive-test", "--path", dir)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}

	s := project.NewStructure(dir)

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	defer func() {
		_ = os.Chdir(origDir)
	}()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir to project dir: %v", err)
	}

	// Simulate empty stdin (EOF immediately — non-interactive context).
	rootCmd.SetIn(bytes.NewBufferString(""))
	_, _, stderr, err := executeCommand("project", "remove")
	rootCmd.SetIn(nil)

	// Non-interactive without --force should return an error.
	if err == nil {
		t.Fatal("expected error for non-interactive removal without --force, got nil")
	}

	if !contains(stderr, "requires --force") {
		t.Errorf("expected error about requiring --force, got: %s", stderr)
	}

	// Project should still exist (removal was refused).
	if !fileExists(s.ConfigFile) {
		t.Error("expected project to still exist after refused removal")
	}
	if !fileExists(dir) {
		t.Error("expected project directory to still exist after refused removal")
	}
}
