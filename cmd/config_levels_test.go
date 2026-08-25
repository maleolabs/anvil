package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// TestConfigLevelsCommand_Basic verifies that:
//
//	anvil config levels
//
// displays configuration organized by scope level with Global, Project,
// Environment, and Execution sections.
func TestConfigLevelsCommand_Basic(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "anvil.yaml")
	configContent := `project:
  name: levels-test
  version: 2.0.0
global:
  log_level: debug
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

	_, stdout, stderr, err := executeCommand("config", "levels")
	if err != nil {
		t.Fatalf("config levels command returned unexpected error: %v", err)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	// Should contain level headers.
	if !contains(stdout, "Global Level") {
		t.Errorf("stdout should contain 'Global Level', got: %s", stdout)
	}
	if !contains(stdout, "Project Level") {
		t.Errorf("stdout should contain 'Project Level', got: %s", stdout)
	}
	if !contains(stdout, "Environment Level") {
		t.Errorf("stdout should contain 'Environment Level', got: %s", stdout)
	}
	if !contains(stdout, "Execution Level") {
		t.Errorf("stdout should contain 'Execution Level', got: %s", stdout)
	}

	// Should contain project values.
	if !contains(stdout, "levels-test") {
		t.Errorf("stdout should contain 'levels-test' (project name), got: %s", stdout)
	}
	if !contains(stdout, "2.0.0") {
		t.Errorf("stdout should contain '2.0.0' (project version), got: %s", stdout)
	}
	if !contains(stdout, "debug") {
		t.Errorf("stdout should contain 'debug' (log level), got: %s", stdout)
	}

	// Should contain Resolved Values section.
	if !contains(stdout, "Resolved Values") {
		t.Errorf("stdout should contain 'Resolved Values' section, got: %s", stdout)
	}
}

// TestConfigLevelsCommand_WithEnvironment verifies that:
//
//	anvil config levels
//
// displays the Environment level with values when ANVIL_ENV is set.
func TestConfigLevelsCommand_WithEnvironment(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "anvil.yaml")
	configContent := `project:
  name: env-levels-test
  version: 1.0.0
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	// Create environment config file.
	envDir := filepath.Join(dir, "config", "environments")
	if err := os.MkdirAll(envDir, 0755); err != nil {
		t.Fatalf("failed to create env config directory: %v", err)
	}
	envContent := `release:
  max_retained: 99
`
	if err := os.WriteFile(filepath.Join(envDir, "staging.yaml"), []byte(envContent), 0644); err != nil {
		t.Fatalf("failed to write env config file: %v", err)
	}

	t.Setenv("ANVIL_ENV", "staging")

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

	_, stdout, stderr, err := executeCommand("config", "levels")
	if err != nil {
		t.Fatalf("config levels command returned unexpected error: %v", err)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	// Should show environment name in the header.
	if !contains(stdout, "Environment Level (staging)") {
		t.Errorf("stdout should contain 'Environment Level (staging)', got: %s", stdout)
	}

	// Should show environment value in the resolved section.
	if !contains(stdout, "release.max_retained: 99") {
		t.Errorf("stdout should contain 'release.max_retained: 99', got: %s", stdout)
	}
}

// TestConfigLevelsCommand_EmptyLevels verifies that:
//
//	anvil config levels
//
// shows "(not configured)" for levels with no configuration.
func TestConfigLevelsCommand_EmptyLevels(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "anvil.yaml")
	configContent := `project:
  name: empty-levels-test
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

	_, stdout, stderr, err := executeCommand("config", "levels")
	if err != nil {
		t.Fatalf("config levels command returned unexpected error: %v", err)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	// When no ANVIL_ENV is set and no env vars, Execution and Environment
	// levels should show "(not configured)".
	if !contains(stdout, "not configured") {
		t.Errorf("stdout should contain '(not configured)' for empty levels, got: %s", stdout)
	}
}

// TestConfigLevelsCommand_NoFilesModified verifies that:
//
//	anvil config levels
//
// does not create, modify, or delete any files.
func TestConfigLevelsCommand_NoFilesModified(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "anvil.yaml")
	configContent := `project:
  name: no-modify-levels
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

	// Capture directory state before running command.
	entriesBefore, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to list directory before: %v", err)
	}

	_, _, _, err = executeCommand("config", "levels")
	if err != nil {
		t.Fatalf("config levels command returned unexpected error: %v", err)
	}

	// Capture directory state after running command.
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

// TestConfigLevelsCommand_OutsideProject verifies that:
//
//	anvil config levels
//
// outside an Anvil project returns an error.
func TestConfigLevelsCommand_OutsideProject(t *testing.T) {
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

	_, _, stderr, err := executeCommand("config", "levels")
	if err == nil {
		t.Fatal("expected error when running 'config levels' outside project, got nil")
	}
	if stderr == "" {
		t.Error("expected non-empty stderr for failed config levels")
	}
}
