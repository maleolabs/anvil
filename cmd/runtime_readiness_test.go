package cmd

import (
	"os"
	"testing"

	"maleolabs.com/anvil/internal/runtime"
)

// TestReadinessCommand_RegistersUnderRuntime verifies that the readiness
// command is registered as a subcommand of the runtime command.
//
// Reference: ST-P5-02 AC-1
func TestReadinessCommand_RegistersUnderRuntime(t *testing.T) {
	runtimeSub, _, err := rootCmd.Find([]string{"runtime", "readiness"})
	if err != nil {
		t.Fatalf("rootCmd.Find([\"runtime\", \"readiness\"]) returned error: %v", err)
	}
	if runtimeSub == nil {
		t.Fatal("rootCmd.Find([\"runtime\", \"readiness\"]) returned nil command")
	}
	if runtimeSub.Use != "readiness" {
		t.Errorf("command Use = %q, want %q", runtimeSub.Use, "readiness")
	}

	// Verify it's nested under runtime (not directly under root).
	_, _, err = rootCmd.Find([]string{"readiness"})
	if err == nil {
		t.Error("rootCmd.Find([\"readiness\"]) should have failed (readiness is not a direct subcommand)")
	}
}

// TestReadinessCommand_Ready verifies that when all runtime directories
// exist, the readiness command outputs the ready message and exits with
// code 0.
//
// Reference: ST-P5-02 AC-2
func TestReadinessCommand_Ready(t *testing.T) {
	dir := t.TempDir()

	// Create all runtime directories under the temp dir.
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir
	for _, d := range cfg.AllDirs() {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("failed to create directory %s: %v", d, err)
		}
	}

	_, stdout, stderr, err := executeCommand("runtime", "readiness", "--install-root", dir)
	if err != nil {
		t.Fatalf("readiness command returned unexpected error: %v", err)
	}

	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	// Must contain the ready summary.
	if !contains(stdout, "Runtime is ready to accept releases.") {
		t.Errorf("stdout should contain ready summary, got: %s", stdout)
	}

	// Must contain the check names.
	if !contains(stdout, "[PASS] directories") {
		t.Errorf("stdout should contain directory check, got: %s", stdout)
	}
	if !contains(stdout, "[PASS] config") {
		t.Errorf("stdout should contain config check, got: %s", stdout)
	}
}

// TestReadinessCommand_NotReady verifies that when runtime directories are
// missing, the readiness command reports not ready and exits with code 1.
//
// Reference: ST-P5-02 AC-3
func TestReadinessCommand_NotReady(t *testing.T) {
	dir := t.TempDir()
	// Do NOT create any directories — they do not exist.

	_, stdout, stderr, err := executeCommand("runtime", "readiness", "--install-root", dir)

	// Must return an error (exit code 1).
	if err == nil {
		t.Fatal("expected error (exit code 1) when runtime is not ready, got nil")
	}

	// stderr should contain the error message from cobra.
	if !contains(stderr, "runtime is not ready") {
		t.Errorf("stderr should contain 'runtime is not ready', got: %s", stderr)
	}

	// Must contain the not-ready header.
	if !contains(stdout, "Runtime is not ready:") {
		t.Errorf("stdout should contain not-ready header, got: %s", stdout)
	}

	// Must list failed check details.
	if !contains(stdout, "directories") {
		t.Errorf("stdout should contain 'directories' check details, got: %s", stdout)
	}
}

// TestReadinessCommand_AllChecksDisplayed verifies that all readiness checks
// (both passing and failing) are displayed in the output and that check names
// are visible.
//
// Reference: ST-P5-02 AC-4
func TestReadinessCommand_AllChecksDisplayed(t *testing.T) {
	dir := t.TempDir()

	// Create only InstallRoot (not all subdirs) so directories check fails
	// but we still see all checks.
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create install root: %v", err)
	}

	_, stdout, stderr, err := executeCommand("runtime", "readiness", "--install-root", dir)

	// Should fail because not all directories exist.
	if err == nil {
		t.Fatal("expected error (exit code 1) when directories are missing, got nil")
	}

	// stderr should contain the error message from cobra.
	if !contains(stderr, "runtime is not ready") {
		t.Errorf("stderr should contain 'runtime is not ready', got: %s", stderr)
	}

	// Both check names must appear in output.
	if !contains(stdout, "directories") {
		t.Errorf("stdout should contain 'directories' check, got: %s", stdout)
	}
	if !contains(stdout, "config") {
		t.Errorf("stdout should contain 'config' check, got: %s", stdout)
	}

	// PASS/FAIL labels must appear.
	if !contains(stdout, "[FAIL]") {
		t.Errorf("stdout should contain FAIL labels, got: %s", stdout)
	}
}

// TestReadinessCommand_ReadinessFileCreateAndRead verifies readiness
// does not require a project context — it runs successfully in a temp
// directory without any project configuration.
func TestReadinessCommand_NoProjectContextRequired(t *testing.T) {
	dir := t.TempDir()

	// Run readiness with --install-root pointing to a non-existent path.
	// The command should still execute without project errors.
	_, stdout, stderr, err := executeCommand("runtime", "readiness", "--install-root", dir)

	// We expect an error (not ready) but NOT a project-related error.
	if err == nil {
		t.Fatal("expected error (not ready), got nil")
	}

	if stderr != "" {
		// The error should be about readiness, not missing project.
		if contains(stderr, "no Anvil project found") {
			t.Errorf("readiness command should not require project context, got: %s", stderr)
		}
	}

	if !contains(stdout, "Runtime is not ready:") {
		t.Errorf("stdout should contain not-ready message, got: %s", stdout)
	}
}

// TestReadinessCommand_InstallRootFlag verifies that the --install-root
// flag correctly overrides the default install root.
func TestReadinessCommand_InstallRootFlag(t *testing.T) {
	dir := t.TempDir()

	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir
	for _, d := range cfg.AllDirs() {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("failed to create directory %s: %v", d, err)
		}
	}

	_, stdout, stderr, err := executeCommand("runtime", "readiness", "--install-root", dir)
	if err != nil {
		t.Fatalf("readiness command returned unexpected error: %v", err)
	}

	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	if !contains(stdout, "Runtime is ready") {
		t.Errorf("stdout should contain ready message, got: %s", stdout)
	}
}

// TestReadinessCommand_OutputFormat verifies that each check outputs the
// expected format: [PASS|FAIL] check_name followed by details.
func TestReadinessCommand_OutputFormat(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	// Create all dirs for a passing test.
	for _, d := range cfg.AllDirs() {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("failed to create directory %s: %v", d, err)
		}
	}

	_, stdout, _, err := executeCommand("runtime", "readiness", "--install-root", dir)
	if err != nil {
		t.Fatalf("readiness command returned unexpected error: %v", err)
	}

	// Check format: [PASS] check_name on one line, details on next indented.
	expectedDirLine := "[PASS] directories"
	expectedConfigLine := "[PASS] config"
	if !contains(stdout, expectedDirLine) || !contains(stdout, expectedConfigLine) {
		t.Errorf("stdout should contain %q and %q, got:\n%s",
			expectedDirLine, expectedConfigLine, stdout)
	}

	// Details should be indented with two spaces.
	if !contains(stdout, "  all directories exist") {
		t.Errorf("stdout should contain directory details, got: %s", stdout)
	}
	if !contains(stdout, "  config values are valid") {
		t.Errorf("stdout should contain config details, got: %s", stdout)
	}
}

// TestReadinessCommand_ExitCodeReady verifies exit code 0 when ready.
func TestReadinessCommand_ExitCodeReady(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir
	for _, d := range cfg.AllDirs() {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("failed to create directory %s: %v", d, err)
		}
	}

	_, _, _, err := executeCommand("runtime", "readiness", "--install-root", dir)
	if err != nil {
		t.Errorf("expected nil error (exit 0) when ready, got: %v", err)
	}
}

// TestReadinessCommand_ExitCodeNotReady verifies exit code 1 when not ready.
func TestReadinessCommand_ExitCodeNotReady(t *testing.T) {
	dir := t.TempDir()
	// No directories created.

	_, _, _, err := executeCommand("runtime", "readiness", "--install-root", dir)
	if err == nil {
		t.Error("expected non-nil error (exit 1) when not ready, got nil")
	}
}

// TestReadinessCommand_OutputContainsCheckNames verifies that all check
// names are present in the output regardless of pass/fail status.
func TestReadinessCommand_OutputContainsCheckNames(t *testing.T) {
	dir := t.TempDir()
	// Do NOT create any directories — both directories and config checks appear
	// in the output. The config check will also fail since InstallRoot is
	// non-empty (we set it to dir), and EnvironmentName is valid from defaults.

	_, stdout, _, err := executeCommand("runtime", "readiness", "--install-root", dir)
	if err == nil {
		t.Fatal("expected error (exit 1) when directories are missing, got nil")
	}

	// Both check names must be present in the output.
	if !contains(stdout, "directories") {
		t.Errorf("stdout should contain 'directories' check name, got: %s", stdout)
	}
	if !contains(stdout, "config") {
		t.Errorf("stdout should contain 'config' check name, got: %s", stdout)
	}
}

// TestReadinessCommand_EmptyInstallRootFlag verifies that providing
// an empty --install-root does not break the command (defaults are used).
func TestReadinessCommand_EmptyInstallRootFlag(t *testing.T) {
	// Use an empty string for install-root; command should not crash.
	_, _, _, err := executeCommand("runtime", "readiness", "--install-root", "")
	if err == nil {
		// May or may not be ready depending on /opt/anvil existence.
		// The important thing is it doesn't crash.
		t.Log("readiness command succeeded with empty install-root (using default)")
	}
}

// TestReadinessCommand_SecureExecution verifies that running the
// readiness command does not create unexpected files in a temp directory.
func TestReadinessCommand_SecureExecution(t *testing.T) {
	dir := t.TempDir()

	entriesBefore, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read directory before: %v", err)
	}

	_, _, _, err = executeCommand("runtime", "readiness", "--install-root", dir)
	if err == nil {
		t.Error("expected error (dirs don't exist), got nil")
	}

	entriesAfter, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read directory after: %v", err)
	}

	if len(entriesBefore) != len(entriesAfter) {
		t.Errorf("directory contents changed: before=%d entries, after=%d entries",
			len(entriesBefore), len(entriesAfter))
	}
}

// TestReadinessCommand_FlagDeduplication verifies that the --install-root
// flag name does not conflict with other commands.
func TestReadinessCommand_FlagDeduplication(t *testing.T) {
	// Verify the flag is only on the readiness command, not on runtime parent.
	runtimeCmdRef, _, err := rootCmd.Find([]string{"runtime"})
	if err != nil {
		t.Fatalf("failed to find runtime command: %v", err)
	}

	flag := runtimeCmdRef.Flags().Lookup("install-root")
	if flag != nil {
		t.Error("install-root flag should not be on the runtime parent command")
	}

	// Verify the flag is on the readiness subcommand.
	readinessCmdRef, _, err := rootCmd.Find([]string{"runtime", "readiness"})
	if err != nil {
		t.Fatalf("failed to find readiness command: %v", err)
	}

	flag = readinessCmdRef.Flags().Lookup("install-root")
	if flag == nil {
		t.Error("install-root flag should be on the readiness subcommand")
	}
}

// TestReadinessCommand_DefaultConfigNoOverride verifies that without the
// --install-root flag, the default config values are used.
func TestReadinessCommand_DefaultConfigNoOverride(t *testing.T) {
	// Run readiness without any flags to verify default behavior doesn't crash.
	_, stdout, stderr, err := executeCommand("runtime", "readiness")

	// stderr may contain an error message from cobra when /opt/anvil does not
	// exist. The important thing is the command doesn't panic.
	if contains(stderr, "panic") {
		t.Errorf("stderr should not contain panic, got: %s", stderr)
	}

	// Should work without crashing — default /opt/anvil may or may not exist.
	if err != nil {
		// Error is expected (likely not ready), but should be a readiness error.
		if !contains(stdout, "Runtime is not ready:") && !contains(stdout, "Runtime is ready") {
			t.Errorf("stdout should contain readiness summary, got: %s", stdout)
		}
	} else {
		// If it passes, must show ready message.
		if !contains(stdout, "Runtime is ready") {
			t.Errorf("stdout should contain ready message, got: %s", stdout)
		}
	}
}
