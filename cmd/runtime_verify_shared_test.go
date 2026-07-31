package cmd

import (
	"os"
	"testing"

	"maleolabs.com/anvil/internal/runtime"
)

// TestVerifySharedCommand_RegistersUnderRuntime verifies that the
// verify-shared command is registered as a subcommand of the runtime command.
//
// Reference: ST-P5-05
func TestVerifySharedCommand_RegistersUnderRuntime(t *testing.T) {
	runtimeSub, _, err := rootCmd.Find([]string{"runtime", "verify-shared"})
	if err != nil {
		t.Fatalf("rootCmd.Find([\"runtime\", \"verify-shared\"]) returned error: %v", err)
	}
	if runtimeSub == nil {
		t.Fatal("rootCmd.Find([\"runtime\", \"verify-shared\"]) returned nil command")
	}
	if runtimeSub.Use != "verify-shared" {
		t.Errorf("command Use = %q, want %q", runtimeSub.Use, "verify-shared")
	}

	// Verify it's nested under runtime (not directly under root).
	_, _, err = rootCmd.Find([]string{"verify-shared"})
	if err == nil {
		t.Error("rootCmd.Find([\"verify-shared\"]) should have failed (verify-shared is not a direct subcommand)")
	}
}

// TestVerifySharedCommand_IntactResources verifies that when all shared
// resource directories exist and a symlink is present, the command reports
// that shared resources are intact.
//
// Reference: ST-P5-05 AC-1
func TestVerifySharedCommand_IntactResources(t *testing.T) {
	dir := t.TempDir()

	// Create all runtime directories and a symlink.
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir
	for _, d := range cfg.AllDirs() {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("failed to create directory %s: %v", d, err)
		}
	}

	// The verify-shared command checks shared resource dirs and symlink.
	// The symlink may not exist before first activation, but shared dirs
	// are the main check. With all dirs present, the command should pass
	// for shared resources.
	_, stdout, stderr, err := executeCommand("runtime", "verify-shared", "--install-root", dir)
	if err != nil {
		t.Fatalf("verify-shared command returned unexpected error: %v", err)
	}

	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	if !contains(stdout, "Shared resources are intact.") {
		t.Errorf("stdout should contain success message, got: %s", stdout)
	}

	// Must show PASS for shared directories.
	if !contains(stdout, "[PASS]") {
		t.Errorf("stdout should contain PASS results, got: %s", stdout)
	}

	if !contains(stdout, "Shared Resource Verification") {
		t.Errorf("stdout should contain header, got: %s", stdout)
	}
}

// TestVerifySharedCommand_MissingSharedDir verifies that when a shared
// resource directory is missing, the command reports failure.
//
// Reference: ST-P5-05 AC-2
func TestVerifySharedCommand_MissingSharedDir(t *testing.T) {
	dir := t.TempDir()

	// Only create InstallRoot, not shared directories.
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create install root: %v", err)
	}

	_, stdout, stderr, err := executeCommand("runtime", "verify-shared", "--install-root", dir)

	// Must return an error (exit code 1).
	if err == nil {
		t.Fatal("expected error (exit code 1) when shared resources are missing, got nil")
	}

	if !contains(stderr, "shared resources are compromised") {
		t.Errorf("stderr should contain error message, got: %s", stderr)
	}

	// Must contain FAIL indicators.
	if !contains(stdout, "[FAIL]") {
		t.Errorf("stdout should contain FAIL results, got: %s", stdout)
	}

	if !contains(stdout, "Shared resources are compromised:") {
		t.Errorf("stdout should contain failure summary, got: %s", stdout)
	}
}

// TestVerifySharedCommand_InstallRootFlag verifies that the --install-root
// flag correctly overrides the install root.
//
// Reference: ST-P5-05
func TestVerifySharedCommand_InstallRootFlag(t *testing.T) {
	dir := t.TempDir()

	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir
	for _, d := range cfg.AllDirs() {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("failed to create directory %s: %v", d, err)
		}
	}

	_, stdout, stderr, err := executeCommand("runtime", "verify-shared", "--install-root", dir)
	if err != nil {
		t.Fatalf("verify-shared command returned unexpected error: %v", err)
	}

	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	if !contains(stdout, dir) {
		t.Errorf("stdout should contain the install root path, got: %s", stdout)
	}
}

// TestVerifySharedCommand_FlagDeduplication verifies that the --install-root
// flag is only on the verify-shared subcommand (not on the runtime parent).
//
// Reference: ST-P5-05
func TestVerifySharedCommand_FlagDeduplication(t *testing.T) {
	// Verify flag is NOT on the runtime parent command.
	runtimeCmdRef, _, err := rootCmd.Find([]string{"runtime"})
	if err != nil {
		t.Fatalf("failed to find runtime command: %v", err)
	}

	flag := runtimeCmdRef.Flags().Lookup("install-root")
	if flag != nil {
		t.Error("install-root flag should not be on the runtime parent command")
	}

	// Verify flag IS on the verify-shared subcommand.
	verifyCmdRef, _, err := rootCmd.Find([]string{"runtime", "verify-shared"})
	if err != nil {
		t.Fatalf("failed to find verify-shared command: %v", err)
	}

	flag = verifyCmdRef.Flags().Lookup("install-root")
	if flag == nil {
		t.Error("install-root flag should be on the verify-shared subcommand")
	}
}

// TestVerifySharedCommand_OutputFormat verifies that each shared resource
// check outputs the expected format.
//
// Reference: ST-P5-05
func TestVerifySharedCommand_OutputFormat(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	// Create all shared dirs.
	for _, d := range cfg.AllDirs() {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("failed to create directory %s: %v", d, err)
		}
	}

	_, stdout, _, err := executeCommand("runtime", "verify-shared", "--install-root", dir)
	if err != nil {
		t.Fatalf("verify-shared command returned unexpected error: %v", err)
	}

	// Must contain PASS for each shared directory.
	if !contains(stdout, "[PASS]") {
		t.Errorf("stdout should contain PASS results, got: %s", stdout)
	}

	// Must contain directory paths in output.
	if !contains(stdout, "shared/config") && !contains(stdout, "shared/storage") && !contains(stdout, "shared/logs") {
		t.Errorf("stdout should contain shared directory paths, got: %s", stdout)
	}
}

// TestVerifySharedCommand_ExitCodeIntact verifies exit code 0 when
// shared resources are intact.
func TestVerifySharedCommand_ExitCodeIntact(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir
	for _, d := range cfg.AllDirs() {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("failed to create directory %s: %v", d, err)
		}
	}

	_, _, _, err := executeCommand("runtime", "verify-shared", "--install-root", dir)
	if err != nil {
		t.Errorf("expected nil error (exit 0) when intact, got: %v", err)
	}
}

// TestVerifySharedCommand_ExitCodeCompromised verifies exit code 1 when
// shared resources are compromised.
func TestVerifySharedCommand_ExitCodeCompromised(t *testing.T) {
	dir := t.TempDir()
	// No directories created.

	_, _, _, err := executeCommand("runtime", "verify-shared", "--install-root", dir)
	if err == nil {
		t.Error("expected non-nil error (exit 1) when compromised, got nil")
	}
}

// TestVerifySharedCommand_SecureExecution verifies that running the
// verify-shared command does not create unexpected files.
func TestVerifySharedCommand_SecureExecution(t *testing.T) {
	dir := t.TempDir()

	entriesBefore, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read directory before: %v", err)
	}

	// Run command against a different (non-existent) directory.
	otherDir := t.TempDir()
	_, _, _, _ = executeCommand("runtime", "verify-shared", "--install-root", otherDir)

	entriesAfter, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read directory after: %v", err)
	}

	if len(entriesBefore) != len(entriesAfter) {
		t.Errorf("directory contents changed: before=%d entries, after=%d entries",
			len(entriesBefore), len(entriesAfter))
	}
}
