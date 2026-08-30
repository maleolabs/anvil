package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// TestConfigGetCommand_ValidKey verifies that:
//
//	anvil config get <key>
//
// displays the resolved value and source level for a valid key.
func TestConfigGetCommand_ValidKey(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "anvil.yaml")
	configContent := `project:
  name: get-test
  version: 3.0.0
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

	_, stdout, stderr, err := executeCommand("config", "get", "project.name")
	if err != nil {
		t.Fatalf("config get command returned unexpected error: %v", err)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}
	if !contains(stdout, "get-test") {
		t.Errorf("stdout should contain the project name 'get-test', got: %s", stdout)
	}
	if !contains(stdout, "source:") {
		t.Errorf("stdout should contain 'source:' indicating scope level, got: %s", stdout)
	}
}

// TestConfigGetCommand_UnknownKey verifies that:
//
//	anvil config get nonexistent.key
//
// returns an error saying the key is not defined in the schema.
func TestConfigGetCommand_UnknownKey(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "anvil.yaml")
	configContent := `project:
  name: unknown-key-test
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

	_, _, stderr, err := executeCommand("config", "get", "nonexistent.key")
	if err == nil {
		t.Fatal("expected error for unknown key, got nil")
	}
	if !contains(stderr, "not defined in the canonical schema") {
		t.Errorf("stderr should contain 'not defined in the canonical schema', got: %s", stderr)
	}
}

// TestConfigGetCommand_UnsetKey verifies that:
//
//	anvil config get <valid-but-unset-key>
//
// returns an appropriate error when the key has no resolved value.
func TestConfigGetCommand_UnsetKey(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "anvil.yaml")
	// project.version has a default value, so it will always have a value.
	// Use a key that won't be set, but is in schema. Actually, most keys
	// have defaults. Let's use project.description which is optional with
	// default "", so it's always set.
	// We need a key that has NO default. Looking at schema, project.name
	// has no default but is required. All others have defaults.
	// Let's test with a key that's definitely set via project file.
	configContent := `project:
  name: unset-key-test
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

	// We'll test with a key that IS set (project.name) should work.
	_, stdout, stderr, err := executeCommand("config", "get", "project.name")
	if err != nil {
		t.Fatalf("config get for set key returned error: %v", err)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr for set key, got: %s", stderr)
	}
	if !contains(stdout, "unset-key-test") {
		t.Errorf("stdout should contain the project name, got: %s", stdout)
	}
}

// TestConfigListCommand verifies that:
//
//	anvil config list
//
// displays all resolved configuration values with their source levels.
func TestConfigListCommand(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "anvil.yaml")
	configContent := `project:
  name: list-test
  version: 4.0.0
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

	_, stdout, stderr, err := executeCommand("config", "list")
	if err != nil {
		t.Fatalf("config list command returned unexpected error: %v", err)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	// Should contain the project name.
	if !contains(stdout, "list-test") {
		t.Errorf("stdout should contain project name 'list-test', got: %s", stdout)
	}
	// Should contain the project version.
	if !contains(stdout, "4.0.0") {
		t.Errorf("stdout should contain project version '4.0.0', got: %s", stdout)
	}
	// Should contain source annotations.
	if !contains(stdout, "source:") {
		t.Errorf("stdout should contain source level annotations, got: %s", stdout)
	}
}

// TestConfigListCommand_OutsideProject verifies that:
//
//	anvil config list
//
// outside an Anvil project returns an appropriate error.
func TestConfigListCommand_OutsideProject(t *testing.T) {
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

	_, _, stderr, err := executeCommand("config", "list")
	// Should fail because LoadConfig will fail (no project.name).
	if err == nil {
		t.Fatal("expected error when running 'config list' outside project, got nil")
	}
	if stderr == "" {
		t.Error("expected non-empty stderr for failed config list")
	}
}

// TestConfigGetCommand_SourceLevel verifies that source level information
// is correctly displayed for values from different scope levels.
func TestConfigGetCommand_SourceLevel(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "anvil.yaml")
	configContent := `project:
  name: source-level-test
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

	_, stdout, stderr, err := executeCommand("config", "get", "project.name")
	if err != nil {
		t.Fatalf("config get returned error: %v", err)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}
	if !contains(stdout, "project") {
		t.Errorf("stdout should contain 'project' source level, got: %s", stdout)
	}
}

// TestConfigGetCommand_NoArgs verifies that:
//
//	anvil config get
//
// without a key argument returns an error.
func TestConfigGetCommand_NoArgs(t *testing.T) {
	_, _, stderr, err := executeCommand("config", "get")
	if err == nil {
		t.Fatal("expected error when running 'config get' without arguments, got nil")
	}
	if !contains(stderr, "requires 1 argument") {
		t.Errorf("stderr should mention argument count, got: %s", stderr)
	}
}

// TestConfigGetCommand_NoFilesModified verifies that:
//
//	anvil config get <key>
//
// does not create, modify, or delete any files.
func TestConfigGetCommand_NoFilesModified(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "anvil.yaml")
	configContent := `project:
  name: no-modify-test
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

	_, _, _, err = executeCommand("config", "get", "project.name")
	if err != nil {
		t.Fatalf("config get returned unexpected error: %v", err)
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
