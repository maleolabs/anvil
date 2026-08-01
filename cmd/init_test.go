package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"maleolabs.com/anvil/internal/project"
)

// executeCommand is a helper that creates a command, sets args, captures
// stdout/stderr, and runs it.
//
// It resets all flags' Changed state on the entire command tree before
// each execution to prevent stale flag values from cross-test leakage.
func executeCommand(args ...string) (*cobra.Command, string, string, error) {
	bufOut := new(bytes.Buffer)
	bufErr := new(bytes.Buffer)

	root := rootCmd

	// Reset all flags' Changed state on the entire command tree to prevent
	// stale flag values from cross-test contamination.
	resetFlags(root)

	root.SetOut(bufOut)
	root.SetErr(bufErr)
	root.SetArgs(args)

	// Use Execute() (not root.Execute()) so that suggestion propagation
	// and other Execute-level setup runs.
	err := Execute()
	return root, bufOut.String(), bufErr.String(), err
}

// resetFlags recursively resets flags on a command and its subcommands
// to their default values and clears the Changed state. This prevents
// stale flag values from leaking between test executions through shared
// cobra command instances.
func resetFlags(cmd *cobra.Command) {
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		f.Changed = false
		_ = f.Value.Set(f.DefValue)
	})
	cmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		f.Changed = false
		_ = f.Value.Set(f.DefValue)
	})
	for _, sub := range cmd.Commands() {
		resetFlags(sub)
	}
}

// TestInitCommand_CreatesProject verifies that running:
//
//	anvil init my-project
//
// creates a new project with the configuration file and default pipeline
// configuration files.
func TestInitCommand_CreatesProject(t *testing.T) {
	dir := t.TempDir()
	_, _, _, err := executeCommand("init", "test-project", "--path", dir)
	if err != nil {
		t.Fatalf("init command returned unexpected error: %v", err)
	}

	s := project.NewStructure(dir)

	// Config file must exist.
	if !fileExists(s.ConfigFile) {
		t.Errorf("expected config file %s to exist", s.ConfigFile)
	}

	// Pipeline config files must exist.
	buildPath := filepath.Join(s.PipelinesDir, project.PipelineBuildFileName)
	if !fileExists(buildPath) {
		t.Errorf("expected pipeline config %s to exist", buildPath)
	}
	ciPath := filepath.Join(s.PipelinesDir, project.PipelineCIFileName)
	if !fileExists(ciPath) {
		t.Errorf("expected pipeline config %s to exist", ciPath)
	}
}

// TestInitCommand_WithoutName verifies that running:
//
//	anvil init
//
// (without a project name) produces an error.
func TestInitCommand_WithoutName(t *testing.T) {
	_, _, stderr, err := executeCommand("init")
	if err == nil {
		t.Fatal("expected error for missing project name, got nil")
	}
	if !contains(stderr, "requires 1 argument") {
		t.Errorf("expected error about missing project name, got: %s", stderr)
	}
}

// TestInitCommand_InExistingProject verifies that running:
//
//	anvil init my-project
//
// in a directory that already contains a project reports the existing state
// and does not overwrite it.
func TestInitCommand_InExistingProject(t *testing.T) {
	dir := t.TempDir()

	// First init should succeed.
	_, _, _, err := executeCommand("init", "first-project", "--path", dir)
	if err != nil {
		t.Fatalf("first init failed: %v", err)
	}

	configPath := filepath.Join(dir, project.ConfigFileName)
	originalConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config file: %v", err)
	}

	// Second init should fail.
	_, _, stderr, err := executeCommand("init", "second-project", "--path", dir)
	if err == nil {
		t.Fatal("expected error for existing project, got nil")
	}
	if !contains(stderr, "project already exists") {
		t.Errorf("expected 'project already exists' error, got: %s", stderr)
	}

	// Config should not have been modified.
	currentConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config after failed init: %v", err)
	}
	if string(currentConfig) != string(originalConfig) {
		t.Error("config file was modified after failed initialization")
	}
}

// TestInitCommand_InvalidName verifies that:
//
//	anvil init "invalid name!"
//
// produces an error about invalid characters.
func TestInitCommand_InvalidName(t *testing.T) {
	dir := t.TempDir()
	_, _, stderr, err := executeCommand("init", "invalid name!", "--path", dir)
	if err == nil {
		t.Fatal("expected error for invalid project name, got nil")
	}
	if !contains(stderr, "invalid project name") {
		t.Errorf("expected 'invalid project name' error, got: %s", stderr)
	}
	if !contains(stderr, "letters, numbers, hyphens") {
		t.Errorf("expected allowed characters hint, got: %s", stderr)
	}
}

// TestInitCommand_WithCustomPath verifies that:
//
//	anvil init my-project --path /custom/path
//
// creates the project at the specified path with pipeline configs.
func TestInitCommand_WithCustomPath(t *testing.T) {
	parent := t.TempDir()
	targetPath := filepath.Join(parent, "nested", "dir")

	_, _, _, err := executeCommand("init", "nested-app", "--path", targetPath)
	if err != nil {
		t.Fatalf("init with custom path failed: %v", err)
	}

	s := project.NewStructure(targetPath)
	if !fileExists(s.ConfigFile) {
		t.Error("expected anvil.yaml to exist at custom path")
	}

	buildPath := filepath.Join(s.PipelinesDir, project.PipelineBuildFileName)
	if !fileExists(buildPath) {
		t.Errorf("expected pipeline config %s to exist at custom path", buildPath)
	}
	ciPath := filepath.Join(s.PipelinesDir, project.PipelineCIFileName)
	if !fileExists(ciPath) {
		t.Errorf("expected pipeline config %s to exist at custom path", ciPath)
	}
}

// TestInitCommand_SuccessMessage verifies that a successful init produces
// a user-friendly success message.
func TestInitCommand_SuccessMessage(t *testing.T) {
	dir := t.TempDir()
	_, stdout, _, err := executeCommand("init", "my-app", "--path", dir)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}

	if !contains(stdout, "created") {
		t.Errorf("expected success message containing 'created', got: %s", stdout)
	}
	if !contains(stdout, "Next steps") {
		t.Errorf("expected 'Next steps' guidance, got: %s", stdout)
	}
}

// TestInitCommand_ExitCode verifies that a failed init returns exit code 1.
func TestInitCommand_ExitCode(t *testing.T) {
	// Missing name should return an error (which Cobra converts to exit code 1).
	_, _, _, err := executeCommand("init")
	if err == nil {
		t.Error("expected error for missing name")
	}

	// Invalid name should return an error.
	_, _, _, err = executeCommand("init", "bad/name")
	if err == nil {
		t.Error("expected error for invalid name")
	}
}

// TestInitCommand_WithFrameworkLaravel verifies that:
//
//	anvil init app --framework laravel
//
// succeeds, reports the framework in the success message, and stores the
// framework in anvil.yaml (TS-P7-29 AC-1/AC-4).
func TestInitCommand_WithFrameworkLaravel(t *testing.T) {
	dir := t.TempDir()
	_, stdout, stderr, err := executeCommand("init", "app", "--framework", "laravel", "--path", dir)
	if err != nil {
		t.Fatalf("init with framework failed: %v\nstderr: %s", err, stderr)
	}
	if !contains(stdout, "created") {
		t.Errorf("expected success message, got: %s", stdout)
	}
	if !contains(stdout, "laravel") {
		t.Errorf("expected success message to mention framework 'laravel', got: %s", stdout)
	}

	// Framework must be stored in anvil.yaml.
	data, err := os.ReadFile(filepath.Join(dir, project.ConfigFileName))
	if err != nil {
		t.Fatalf("reading config file: %v", err)
	}
	if !contains(string(data), "framework: laravel") {
		t.Errorf("expected 'framework: laravel' in anvil.yaml, got:\n%s", string(data))
	}
}

// TestInitCommand_UnknownFramework verifies that an unsupported framework
// produces an error mentioning "unknown framework".
func TestInitCommand_UnknownFramework(t *testing.T) {
	dir := t.TempDir()
	_, _, stderr, err := executeCommand("init", "app", "--framework", "symfony", "--path", dir)
	if err == nil {
		t.Fatal("expected error for unknown framework, got nil")
	}
	if !contains(stderr, "unknown framework") {
		t.Errorf("expected 'unknown framework' error, got: %s", stderr)
	}
}

// TestInitCommand_FlutterFrameworkNotSupported verifies that flutter — a
// known roadmap framework without a template yet (TS-P7-27) — produces a
// clear "not yet supported" error rather than an "unknown framework" error.
func TestInitCommand_FlutterFrameworkNotSupported(t *testing.T) {
	dir := t.TempDir()
	_, _, stderr, err := executeCommand("init", "app", "--framework", "flutter", "--path", dir)
	if err == nil {
		t.Fatal("expected error for flutter framework, got nil")
	}
	if !contains(stderr, "not yet supported") {
		t.Errorf("expected 'not yet supported' error, got: %s", stderr)
	}
}

// TestValidProjectNameRegex verifies the regex pattern used for name validation.
func TestValidProjectNameRegex(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"my-project", true},
		{"my_project", true},
		{"MyProject123", true},
		{"a", true},
		{"", false},
		{"my project", false},
		{"my/project", false},
		{"my.project", false},
		{"my-project!", false},
		{" spaces", false},
	}

	for _, tt := range tests {
		got := validProjectName.MatchString(tt.name)
		if got != tt.valid {
			t.Errorf("validProjectName.MatchString(%q) = %v, want %v", tt.name, got, tt.valid)
		}
	}
}

// --- helpers ---

func isDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
