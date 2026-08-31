// Package project provides tests for the project loading engine.
//
// Reference: TS-P1-06
package project

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// validConfigYAML is a fully valid Anvil project configuration used
// in TestLoad_ValidProject and other tests that need a valid config.
const validConfigYAML = `project:
  name: test-app
  version: 1.0.0
  description: A test project
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

// invalidConfigYAML is missing the required project.name field
// and should trigger validation errors when loaded.
const invalidConfigYAML = `project:
  version: 1.0.0
`

// invalidYAMLContent is malformed YAML that should fail parsing.
const invalidYAMLContent = `project: [unclosed
`

// --- TS-P1-06 Tests: Project Loading Engine ---

// TestLoad_ValidProject verifies that a valid anvil.yaml loads
// successfully and returns a non-nil *ProjectConfig with the
// expected field values.
//
// Acceptance Criteria: TS-P1-06 AC-1
func TestLoad_ValidProject(t *testing.T) {
	saveCWD(t)

	root := t.TempDir()
	configPath := filepath.Join(root, ConfigFileName)

	if err := os.WriteFile(configPath, []byte(validConfigYAML), 0644); err != nil {
		t.Fatalf("failed to write config file %q: %v", configPath, err)
	}

	if err := os.Chdir(root); err != nil {
		t.Fatalf("failed to change to project root %q: %v", root, err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load() returned nil config")
	}

	// Verify key fields are populated correctly.
	if cfg.Project == nil {
		t.Fatal("Load() returned config with nil Project section")
	}
	if cfg.Project.Name != "test-app" {
		t.Errorf("Load().Project.Name = %q, want %q", cfg.Project.Name, "test-app")
	}
	if cfg.Project.Version != "1.0.0" {
		t.Errorf("Load().Project.Version = %q, want %q", cfg.Project.Version, "1.0.0")
	}
	if cfg.Project.Description != "A test project" {
		t.Errorf("Load().Project.Description = %q, want %q", cfg.Project.Description, "A test project")
	}

	// Verify all sections are populated.
	if cfg.Artifact == nil {
		t.Error("Load() returned config with nil Artifact section")
	}
	if cfg.Release == nil {
		t.Error("Load() returned config with nil Release section")
	}
	if cfg.Runtime == nil {
		t.Error("Load() returned config with nil Runtime section")
	}
	if cfg.Global == nil {
		t.Error("Load() returned config with nil Global section")
	}
}

// TestLoad_InvalidConfig verifies that a config with invalid values
// returns a *ValidationBlockedError with at least one validation error.
//
// Acceptance Criteria: TS-P1-06 AC-2
func TestLoad_InvalidConfig(t *testing.T) {
	saveCWD(t)

	root := t.TempDir()
	configPath := filepath.Join(root, ConfigFileName)

	if err := os.WriteFile(configPath, []byte(invalidConfigYAML), 0644); err != nil {
		t.Fatalf("failed to write config file %q: %v", configPath, err)
	}

	if err := os.Chdir(root); err != nil {
		t.Fatalf("failed to change to project root %q: %v", root, err)
	}

	cfg, err := Load()
	if err == nil {
		t.Fatal("Load() expected error for invalid config, got nil")
	}
	if cfg != nil {
		t.Errorf("Load() returned non-nil config for invalid config: %v", cfg)
	}

	var blockErr *ValidationBlockedError
	if !errors.As(err, &blockErr) {
		t.Fatalf("Load() error = %T, want *ValidationBlockedError", err)
	}
	if len(blockErr.Errors) == 0 {
		t.Error("ValidationBlockedError.Errors is empty, expected at least one validation error")
	}
}

// TestLoad_MissingProject verifies that when no anvil.yaml exists
// in the current directory or any parent, Load returns ErrNoProjectFound.
//
// Acceptance Criteria: TS-P1-06 AC-3
func TestLoad_MissingProject(t *testing.T) {
	saveCWD(t)

	dir := t.TempDir()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change to temp directory %q: %v", dir, err)
	}

	cfg, err := Load()
	if err == nil {
		t.Fatal("Load() expected ErrNoProjectFound, got nil")
	}
	if cfg != nil {
		t.Errorf("Load() returned non-nil config for missing project: %v", cfg)
	}
	if !errors.Is(err, ErrNoProjectFound) {
		t.Errorf("Load() error = %v, want ErrNoProjectFound", err)
	}
}

// TestLoad_InvalidYAML verifies that malformed YAML content returns
// a parse error (not a validation error) with the config file path
// referenced in the error message.
//
// Acceptance Criteria: TS-P1-06 AC-4
func TestLoad_InvalidYAML(t *testing.T) {
	saveCWD(t)

	root := t.TempDir()
	configPath := filepath.Join(root, ConfigFileName)

	if err := os.WriteFile(configPath, []byte(invalidYAMLContent), 0644); err != nil {
		t.Fatalf("failed to write config file %q: %v", configPath, err)
	}

	if err := os.Chdir(root); err != nil {
		t.Fatalf("failed to change to project root %q: %v", root, err)
	}

	cfg, err := Load()
	if err == nil {
		t.Fatal("Load() expected parse error for invalid YAML, got nil")
	}
	if cfg != nil {
		t.Errorf("Load() returned non-nil config for invalid YAML: %v", cfg)
	}

	// Error should reference the config file name.
	errMsg := err.Error()
	if !contains(errMsg, ConfigFileName) {
		t.Errorf("Load() error should mention %q, got: %s", ConfigFileName, errMsg)
	}

	// Error should not be a ValidationBlockedError — it's a parse error.
	var blockErr *ValidationBlockedError
	if errors.As(err, &blockErr) {
		t.Error("Load() should not return ValidationBlockedError for invalid YAML")
	}

	// Verify it's a YAML parse error (not a generic error).
	if !contains(errMsg, "yaml") {
		t.Errorf("Load() error should mention 'yaml', got: %s", errMsg)
	}
}

// TestLoad_TriggersDiscovery verifies that Load calls Discover by
// setting up a project root with a valid anvil.yaml and changing the
// working directory to a nested subdirectory. If Load successfully
// loads the config from the subdirectory, Discover was used to find
// the project root via parent traversal.
//
// Acceptance Criteria: TS-P1-06 AC-5
func TestLoad_TriggersDiscovery(t *testing.T) {
	saveCWD(t)

	root := t.TempDir()
	configPath := filepath.Join(root, ConfigFileName)

	if err := os.WriteFile(configPath, []byte(validConfigYAML), 0644); err != nil {
		t.Fatalf("failed to write config file %q: %v", configPath, err)
	}

	// Change to a nested subdirectory to exercise parent traversal.
	subdir := filepath.Join(root, "nested", "deeply")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("failed to create nested subdirectory %q: %v", subdir, err)
	}

	if err := os.Chdir(subdir); err != nil {
		t.Fatalf("failed to change to subdirectory %q: %v", subdir, err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error from subdirectory: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load() returned nil config from subdirectory")
	}
	if cfg.Project == nil {
		t.Fatal("Load() returned config with nil Project section from subdirectory")
	}
	if cfg.Project.Name != "test-app" {
		t.Errorf("Load().Project.Name = %q, want %q", cfg.Project.Name, "test-app")
	}
}

// TestLoad_TriggersValidation verifies that an invalid config triggers
// the validation error path and returns a *ValidationBlockedError
// carrying the validation error messages.
//
// Acceptance Criteria: TS-P1-06 AC-6
func TestLoad_TriggersValidation(t *testing.T) {
	saveCWD(t)

	root := t.TempDir()
	configPath := filepath.Join(root, ConfigFileName)

	if err := os.WriteFile(configPath, []byte(invalidConfigYAML), 0644); err != nil {
		t.Fatalf("failed to write config file %q: %v", configPath, err)
	}

	if err := os.Chdir(root); err != nil {
		t.Fatalf("failed to change to project root %q: %v", root, err)
	}

	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected error for invalid config, got nil")
	}

	// The error must be a ValidationBlockedError (validation was triggered).
	var blockErr *ValidationBlockedError
	if !errors.As(err, &blockErr) {
		t.Fatalf("Load() error = %T, want *ValidationBlockedError", err)
	}
	if len(blockErr.Errors) == 0 {
		t.Fatal("ValidationBlockedError.Errors is empty, expected validation errors")
	}

	// Error messages should reference project.name since that is
	// the required field missing a non-empty value in the invalid config.
	foundProjectName := false
	for _, e := range blockErr.Errors {
		if contains(e, "project.name") {
			foundProjectName = true
			break
		}
	}
	if !foundProjectName {
		t.Errorf("Validation errors should reference 'project.name', got: %v", blockErr.Errors)
	}
}
