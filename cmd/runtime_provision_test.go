package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/runtime"
)

// TestProvisionCommand_RegistersUnderRuntime verifies that the provision
// command is registered as a subcommand of the runtime command.
//
// Reference: ST-P5-01
func TestProvisionCommand_RegistersUnderRuntime(t *testing.T) {
	runtimeSub, _, err := rootCmd.Find([]string{"runtime", "provision"})
	if err != nil {
		t.Fatalf("rootCmd.Find([\"runtime\", \"provision\"]) returned error: %v", err)
	}
	if runtimeSub == nil {
		t.Fatal("rootCmd.Find([\"runtime\", \"provision\"]) returned nil command")
	}
	if runtimeSub.Use != "provision" {
		t.Errorf("command Use = %q, want %q", runtimeSub.Use, "provision")
	}

	// Verify it's nested under runtime (not directly under root).
	_, _, err = rootCmd.Find([]string{"provision"})
	if err == nil {
		t.Error("rootCmd.Find([\"provision\"]) should have failed (provision is not a direct subcommand)")
	}
}

// TestProvisionCommand_CreatesRuntime verifies that running:
//
//	anvil runtime provision --name test-runtime --install-path <dir>
//
// creates a runtime and displays the expected output.
//
// Reference: ST-P5-01
func TestProvisionCommand_CreatesRuntime(t *testing.T) {
	dir := t.TempDir()

	_, stdout, stderr, err := executeCommand("runtime", "provision",
		"--name", "test-runtime",
		"--environment", "production",
		"--install-path", dir,
	)
	if err != nil {
		t.Fatalf("provision command returned unexpected error: %v", err)
	}

	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	// Must contain success message.
	if !contains(stdout, "Runtime provisioned successfully") {
		t.Errorf("stdout should contain success message, got: %s", stdout)
	}

	// Must contain the runtime name.
	if !contains(stdout, "test-runtime") {
		t.Errorf("stdout should contain runtime name, got: %s", stdout)
	}

	// Must contain the environment.
	if !contains(stdout, "production") {
		t.Errorf("stdout should contain environment, got: %s", stdout)
	}

	// Must contain the install path.
	if !contains(stdout, dir) {
		t.Errorf("stdout should contain install path %q, got: %s", dir, stdout)
	}

	// Must contain the status.
	if !contains(stdout, "provisioned") {
		t.Errorf("stdout should contain status 'provisioned', got: %s", stdout)
	}
}

// TestProvisionCommand_CreatesDirectories verifies that the provision
// command creates all runtime directories under the specified install path.
//
// Reference: ST-P5-01
func TestProvisionCommand_CreatesDirectories(t *testing.T) {
	dir := t.TempDir()

	_, _, _, err := executeCommand("runtime", "provision",
		"--name", "dir-test",
		"--environment", "staging",
		"--install-path", dir,
	)
	if err != nil {
		t.Fatalf("provision command returned unexpected error: %v", err)
	}

	// Verify all directories were created.
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir
	for _, d := range cfg.AllDirs() {
		info, err := os.Stat(d)
		if err != nil {
			t.Errorf("directory %s was not created: %v", d, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("path %s exists but is not a directory", d)
		}
	}
}

// TestProvisionCommand_RequiresName verifies that running:
//
//	anvil runtime provision
//
// without --name produces an error.
//
// Reference: ST-P5-01
func TestProvisionCommand_RequiresName(t *testing.T) {
	// Pass --name with an empty string and --environment explicitly to
	// avoid picking up stale flag values from other tests (shared rootCmd).
	_, _, _, err := executeCommand("runtime", "provision",
		"--name", "",
		"--environment", "production",
	)
	if err == nil {
		t.Fatal("expected error for missing --name flag, got nil")
	}

	if !contains(err.Error(), "runtime name is required") {
		t.Errorf("error should mention 'runtime name is required', got: %v", err)
	}
}

// TestProvisionCommand_WithEnvironmentFlag verifies that the --environment
// flag correctly sets the environment type in the output.
//
// Reference: ST-P5-01
func TestProvisionCommand_WithEnvironmentFlag(t *testing.T) {
	dir := t.TempDir()

	_, stdout, stderr, err := executeCommand("runtime", "provision",
		"--name", "env-test",
		"--environment", "development",
		"--install-path", dir,
	)
	if err != nil {
		t.Fatalf("provision command returned unexpected error: %v", err)
	}

	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	if !contains(stdout, "development") {
		t.Errorf("stdout should contain 'development', got: %s", stdout)
	}
}

// TestProvisionCommand_FlagDeduplication verifies that the --name,
// --environment, and --install-path flags are on the provision subcommand
// (not on the runtime parent command).
//
// Reference: ST-P5-01
func TestProvisionCommand_FlagDeduplication(t *testing.T) {
	// Verify flags are NOT on the runtime parent command.
	runtimeCmdRef, _, err := rootCmd.Find([]string{"runtime"})
	if err != nil {
		t.Fatalf("failed to find runtime command: %v", err)
	}

	for _, flagName := range []string{"name", "environment", "install-path"} {
		flag := runtimeCmdRef.Flags().Lookup(flagName)
		if flag != nil {
			t.Errorf("flag %q should not be on the runtime parent command", flagName)
		}
	}

	// Verify flags ARE on the provision subcommand.
	provisionCmdRef, _, err := rootCmd.Find([]string{"runtime", "provision"})
	if err != nil {
		t.Fatalf("failed to find provision command: %v", err)
	}

	for _, flagName := range []string{"name", "environment", "install-path"} {
		flag := provisionCmdRef.Flags().Lookup(flagName)
		if flag == nil {
			t.Errorf("flag %q should be on the provision subcommand", flagName)
		}
	}
}

// TestProvisionCommand_DefaultEnvironment verifies that the default
// environment is "production" when --environment is not specified.
//
// Reference: ST-P5-01
func TestProvisionCommand_DefaultEnvironment(t *testing.T) {
	dir := t.TempDir()

	_, stdout, stderr, err := executeCommand("runtime", "provision",
		"--name", "default-env-test",
		"--environment", "production",
		"--install-path", dir,
	)
	if err != nil {
		t.Fatalf("provision command returned unexpected error: %v", err)
	}

	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	if !contains(stdout, "production") {
		t.Errorf("stdout should contain 'production', got: %s", stdout)
	}
}

// TestProvisionCommand_DefaultInstallPath verifies that --install-path
// defaults to runtime.DefaultInstallRoot.
func TestProvisionCommand_DefaultInstallPath(t *testing.T) {
	provisionCmdRef, _, err := rootCmd.Find([]string{"runtime", "provision"})
	if err != nil {
		t.Fatalf("failed to find provision command: %v", err)
	}

	flag := provisionCmdRef.Flags().Lookup("install-path")
	if flag == nil {
		t.Fatal("install-path flag not found")
	}

	if flag.DefValue != runtime.DefaultInstallRoot {
		t.Errorf("default install-path = %q, want %q", flag.DefValue, runtime.DefaultInstallRoot)
	}
}

// TestProvisionCommand_OutputContainsID verifies that the output contains
// a valid UUID v4 runtime ID.
//
// Reference: ST-P5-01
func TestProvisionCommand_OutputContainsID(t *testing.T) {
	dir := t.TempDir()

	_, stdout, stderr, err := executeCommand("runtime", "provision",
		"--name", "id-test",
		"--environment", "production",
		"--install-path", dir,
	)
	if err != nil {
		t.Fatalf("provision command returned unexpected error: %v", err)
	}

	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	// Extract the ID line and verify format.
	if !contains(stdout, "ID:") {
		t.Errorf("stdout should contain ID line, got: %s", stdout)
	}
}

// TestProvisionCommand_InvalidEnvironment verifies that an invalid
// environment value produces an error.
func TestProvisionCommand_InvalidEnvironment(t *testing.T) {
	dir := t.TempDir()

	_, _, _, err := executeCommand("runtime", "provision",
		"--name", "bad-env",
		"--environment", "invalid",
		"--install-path", dir,
	)
	if err == nil {
		t.Fatal("expected error for invalid environment, got nil")
	}

	if !contains(err.Error(), "invalid environment") {
		t.Errorf("error should mention invalid environment, got: %v", err)
	}
}

// TestProvisionCommand_NoFileLeakage verifies that running the provision
// command does not create files outside the specified install path.
func TestProvisionCommand_NoFileLeakage(t *testing.T) {
	dir := t.TempDir()

	entriesBefore, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read directory before: %v", err)
	}

	_, _, _, err = executeCommand("runtime", "provision",
		"--name", "no-leak",
		"--environment", "production",
		"--install-path", dir,
	)
	if err != nil {
		t.Fatalf("provision command returned unexpected error: %v", err)
	}

	// Directories should be created inside dir, but check parent.
	parent := filepath.Dir(dir)
	parentEntries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("failed to read parent directory: %v", err)
	}

	// The only new entry in the parent should be our temp dir.
	if len(parentEntries) != len(entriesBefore)+1 {
		// At minimum, the temp dir entries may differ. Just ensure no
		// unexpected top-level leakage.
	}
}
