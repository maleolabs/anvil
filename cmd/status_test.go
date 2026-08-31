package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/project"
)

// TestStatusCommand_NoProjectFound verifies that running:
//
//	anvil status
//
// outside an Anvil project produces the missing-project error message
// with searched directories and guidance.
//
// Acceptance Criteria: ST-P1-06 AC-1, AC-2, AC-3, AC-4
func TestStatusCommand_NoProjectFound(t *testing.T) {
	dir := t.TempDir()

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
		t.Fatalf("failed to change to temp directory %q: %v", dir, err)
	}

	_, stdout, stderr, err := executeCommand("status")

	// Must return an error (non-zero exit code).
	if err == nil {
		t.Fatal("expected error when running 'status' outside project, got nil")
	}

	// Stdout should be empty (all output goes to stderr on failure).
	if stdout != "" {
		t.Errorf("expected empty stdout, got: %s", stdout)
	}

	// Stderr must contain the missing-project message.
	if !contains(stderr, "no Anvil project found") {
		t.Errorf("stderr should contain 'no Anvil project found', got: %s", stderr)
	}

	// Stderr must contain the searched directories (CWD).
	if !contains(stderr, dir) {
		t.Errorf("stderr should contain the searched directory %q, got: %s", dir, stderr)
	}

	// Stderr must contain init guidance.
	if !contains(stderr, "anvil init") {
		t.Errorf("stderr should contain 'anvil init' guidance, got: %s", stderr)
	}

	// Stderr must contain navigation guidance.
	if !contains(stderr, "navigate") {
		t.Errorf("stderr should contain navigation guidance, got: %s", stderr)
	}
}

// TestStatusCommand_InProject verifies that running:
//
//	anvil status
//
// inside a valid Anvil project displays the project information.
func TestStatusCommand_InProject(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, project.ConfigFileName)
	configContent := `project:
  name: test-status
  version: 2.0.0
  description: A test project for status command
artifact:
  include:
    - "**/*"
  exclude:
    - ".git/**"
  output: .anvil/artifacts
  manifest: true
release:
  max_retained: 5
  retention_policy: keep-last
  auto_verify: true
  version_schema: semver
runtime:
  install_root: .anvil/releases
  shared_resources: .anvil/shared
  active_symlink: .anvil/active
  temp_dir: .anvil/tmp
global:
  log_level: info
  output_format: human
  no_color: false
  auto_progress: true
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
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
		t.Fatalf("failed to change to project directory %q: %v", dir, err)
	}

	_, stdout, stderr, err := executeCommand("status")
	if err != nil {
		t.Fatalf("status command returned unexpected error: %v", err)
	}

	if !contains(stdout, "test-status") {
		t.Errorf("stdout should contain project name 'test-status', got: %s", stdout)
	}
	if !contains(stdout, "2.0.0") {
		t.Errorf("stdout should contain version '2.0.0', got: %s", stdout)
	}
	if !contains(stdout, "A test project for status command") {
		t.Errorf("stdout should contain description, got: %s", stdout)
	}

	if stderr != "" {
		t.Errorf("expected empty stderr for successful status, got: %s", stderr)
	}
}

// TestStatusCommand_ExitCode verifies that running:
//
//	anvil status
//
// outside an Anvil project returns a non-zero exit code.
//
// Acceptance Criteria: ST-P1-06 AC-5
func TestStatusCommand_ExitCode(t *testing.T) {
	dir := t.TempDir()

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
		t.Fatalf("failed to change to temp directory %q: %v", dir, err)
	}

	_, _, _, err = executeCommand("status")

	// Cobra converts a non-nil error from RunE to exit code 1.
	if err == nil {
		t.Error("expected non-nil error (exit code 1) when running status outside project")
	}
}

// TestStatusCommand_NoFilesModified verifies that running:
//
//	anvil status
//
// outside an Anvil project does not create, modify, or delete any files.
//
// Acceptance Criteria: ST-P1-06 AC-6
func TestStatusCommand_NoFilesModified(t *testing.T) {
	dir := t.TempDir()

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
		t.Fatalf("failed to change to temp directory %q: %v", dir, err)
	}

	// Capture directory state before running status.
	entriesBefore, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to list directory before: %v", err)
	}

	_, _, _, err = executeCommand("status")
	if err == nil {
		t.Fatal("expected error when running status outside project, got nil")
	}

	// Capture directory state after running status.
	entriesAfter, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to list directory after: %v", err)
	}

	if len(entriesBefore) != len(entriesAfter) {
		t.Errorf("directory entry count changed: before=%d, after=%d",
			len(entriesBefore), len(entriesAfter))
	}

	for i := range entriesBefore {
		if entriesBefore[i].Name() != entriesAfter[i].Name() {
			t.Errorf("entry %d name changed: before=%q, after=%q",
				i, entriesBefore[i].Name(), entriesAfter[i].Name())
		}
	}
}
