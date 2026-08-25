package cmd

// CLI-level exit code contract tests (BUG-006).
//
// The documented exit code contract (0/1/2/3/4 — Success/General/Config/
// Runtime/Precondition; TS-P8-07, ADR-010 §8.1) requires categorized
// errors to exit with their category code instead of the default 1.
// main.go translates an error implementing output.ExitCoder into the
// process exit code (os.Exit(exitErr.ExitCode())), so asserting the
// ExitCoder contract on the error returned by executeCommand verifies
// the process exit code a real invocation would produce.
//
// Reference: BUG-006, TS-P8-07, ADR-010 §8.1

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/output"
)

// requireExitCode asserts that err produces the process exit code wanted
// by the documented contract. main.go exits with the error's ExitCode()
// when it implements output.ExitCoder, and falls back to the default code
// 1 (ExitCodeGeneral) otherwise — both paths are valid for the general
// category.
func requireExitCode(t *testing.T, err error, want int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error, got nil (wanted exit code %d)", want)
	}
	var exitErr output.ExitCoder
	if !errors.As(err, &exitErr) {
		// main.go falls back to ExitCodeGeneral for errors that do not
		// implement ExitCoder.
		if want != output.ExitCodeGeneral {
			t.Fatalf("returned error %T does not implement output.ExitCoder; the process would exit with the default code 1, want %d (BUG-006)", err, want)
		}
		return
	}
	if got := exitErr.ExitCode(); got != want {
		t.Errorf("ExitCode() = %d, want %d", got, want)
	}
}

// TestExitCodes_Precondition_ServerRuntimeNotInitialized verifies the
// documented example: running a server command against an uninitialized
// Runtime exits with code 4 (precondition error). This is the exact
// reproduction from the bug report:
//
//	anvil server release install demo x.tar.gz --server-root <dir>
//
// Reference: BUG-006 Validation step 1, TS-P8-07
func TestExitCodes_Precondition_ServerRuntimeNotInitialized(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "x.tar.gz")
	if err := os.WriteFile(artifact, []byte("dummy artifact"), 0644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	// No 'server init' was run — the Runtime is not initialized.
	_, _, stderr, err := executeCommand(
		"server", "release", "install", "demo", artifact, "--server-root", dir,
	)
	requireExitCode(t, err, output.ExitCodePrecondition)
	if !contains(stderr, "not initialized") {
		t.Errorf("stderr should report the Runtime is not initialized, got: %s", stderr)
	}
}

// TestExitCodes_Precondition_ServerProjectRegisterNotInitialized verifies
// that "anvil server project register" against an uninitialized Runtime
// exits with code 4 (precondition), per the Registration Automation
// Contract ("Runtime not initialized" applies to register and get).
//
// Reference: BUG-006, TS-P8-07, Registration Automation Contract
func TestExitCodes_Precondition_ServerProjectRegisterNotInitialized(t *testing.T) {
	dir := t.TempDir()

	_, _, _, err := executeCommand(
		"server", "project", "register",
		"--server-root", dir,
		"--project-id", "no-init",
		"--install-root", "/srv/no-init",
		"--non-interactive",
	)
	requireExitCode(t, err, output.ExitCodePrecondition)
}

// TestExitCodes_Config_ServerProjectRegisterDuplicate verifies that
// registering a duplicate project ID exits with code 2 (configuration
// error — conflicting registration), per the Registration Automation
// Contract.
//
// Reference: BUG-006 Validation step 2, TS-P8-07, ADR-010 §8.1
func TestExitCodes_Config_ServerProjectRegisterDuplicate(t *testing.T) {
	dir := t.TempDir()

	if _, _, _, err := executeCommand("server", "init", "--server-root", dir); err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	if _, _, _, err := executeCommand(
		"server", "project", "register",
		"--server-root", dir,
		"--project-id", "dup-project",
		"--install-root", "/srv/dup-project",
		"--non-interactive",
	); err != nil {
		t.Fatalf("first registration failed: %v", err)
	}

	_, _, stderr, err := executeCommand(
		"server", "project", "register",
		"--server-root", dir,
		"--project-id", "dup-project",
		"--install-root", "/srv/dup-project",
		"--non-interactive",
	)
	requireExitCode(t, err, output.ExitCodeConfig)
	if !contains(stderr, "already registered") {
		t.Errorf("stderr should report the duplicate project ID, got: %s", stderr)
	}
}

// TestExitCodes_Config_ConfigValidateInvalid verifies that an invalid
// project configuration exits with code 2 (configuration error) — the
// representative config error scenario from the bug report's validation
// steps.
//
// Reference: BUG-006 Validation step 2, TS-012-001, TS-P8-07
func TestExitCodes_Config_ConfigValidateInvalid(t *testing.T) {
	isolateConfigEnvironment(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "anvil.yaml")
	configContent := `project:
  name: validate-test
release:
  max_retained: not-an-integer
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

	_, _, _, err = executeCommand("config", "validate")
	requireExitCode(t, err, output.ExitCodeConfig)
}

// TestExitCodes_Runtime_ServerProjectGetNotFound verifies that looking up
// an unregistered project exits with code 3 (runtime error — resource not
// found), per the Registration Automation Contract.
//
// Reference: BUG-006 Validation step 3, TS-P8-07, ADR-010 §8.1
func TestExitCodes_Runtime_ServerProjectGetNotFound(t *testing.T) {
	dir := t.TempDir()

	if _, _, _, err := executeCommand("server", "init", "--server-root", dir); err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	_, _, stderr, err := executeCommand(
		"server", "project", "get",
		"nonexistent-project",
		"--server-root", dir,
	)
	requireExitCode(t, err, output.ExitCodeRuntime)
	if !contains(stderr, "not found") {
		t.Errorf("stderr should report the project is not found, got: %s", stderr)
	}
}

// TestExitCodes_General_ServerProjectRegisterValidation verifies that a
// validation error (missing required flags) exits with code 1 — the
// general category. Validation errors stay at 1 per the documented
// contract; only config/runtime/precondition categories use 2/3/4.
//
// Reference: BUG-006 Validation step 4, TS-P8-07
func TestExitCodes_General_ServerProjectRegisterValidation(t *testing.T) {
	dir := t.TempDir()

	if _, _, _, err := executeCommand("server", "init", "--server-root", dir); err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// Missing both --project-id and --install-root.
	_, _, stderr, err := executeCommand(
		"server", "project", "register",
		"--server-root", dir,
		"--non-interactive",
	)
	requireExitCode(t, err, output.ExitCodeGeneral)
	if !contains(stderr, "project.id is required") {
		t.Errorf("stderr should report the missing required value, got: %s", stderr)
	}
}

// TestExitCodes_Success_ServerProjectGet verifies that successful
// execution produces no error (process exit code 0).
//
// Reference: BUG-006 Validation step 5, TS-P8-07
func TestExitCodes_Success_ServerProjectGet(t *testing.T) {
	dir := t.TempDir()

	if _, _, _, err := executeCommand("server", "init", "--server-root", dir); err != nil {
		t.Fatalf("server init failed: %v", err)
	}
	if _, _, _, err := executeCommand(
		"server", "project", "register",
		"--server-root", dir,
		"--project-id", "ok-project",
		"--install-root", "/srv/ok-project",
		"--non-interactive",
	); err != nil {
		t.Fatalf("registration failed: %v", err)
	}

	_, _, _, err := executeCommand(
		"server", "project", "get",
		"ok-project",
		"--server-root", dir,
	)
	if err != nil {
		t.Fatalf("expected success (exit code 0), got error: %v", err)
	}
}
