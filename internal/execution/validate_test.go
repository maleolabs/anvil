package execution

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// TestValidate_EmptyCommand verifies that an empty command is rejected with
// a clear error (AC1).
func TestValidate_EmptyCommand_AC1(t *testing.T) {
	req := ExecutionRequest{Command: "", Timeout: DefaultTimeout}

	err := req.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want error")
	}
	if !errors.Is(err, ErrCommandRequired) {
		t.Errorf("Validate() error should contain ErrCommandRequired, got: %v", err)
	}
}

// TestValidate_NonexistentWorkingDir verifies that a non-existent working
// directory is rejected with a clear error (AC2).
func TestValidate_NonexistentWorkingDir_AC2(t *testing.T) {
	req := ExecutionRequest{
		Command:    "echo",
		Timeout:    DefaultTimeout,
		WorkingDir: "/tmp/nonexistent-path-ts-p6-04-12345",
	}

	err := req.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want error for nonexistent working directory")
	}
	if !strings.Contains(err.Error(), "working directory does not exist") {
		t.Errorf("Validate() error should mention working directory, got: %v", err)
	}
}

// TestValidate_WorkingDirIsFile verifies that a working directory pointing to
// a file (not a directory) is rejected.
func TestValidate_WorkingDirIsFile(t *testing.T) {
	// Create a temporary file to use as an invalid working directory.
	tmpFile, err := os.CreateTemp("", "validate-test-*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	req := ExecutionRequest{
		Command:    "echo",
		Timeout:    DefaultTimeout,
		WorkingDir: tmpPath,
	}

	err = req.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want error for file-as-working-directory")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("Validate() error should mention 'not a directory', got: %v", err)
	}
}

// TestValidate_ZeroTimeout verifies that a zero timeout is rejected (AC3).
func TestValidate_ZeroTimeout_AC3(t *testing.T) {
	req := ExecutionRequest{Command: "echo", Timeout: 0}

	err := req.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want error")
	}
	if !errors.Is(err, ErrInvalidTimeout) {
		t.Errorf("Validate() error should contain ErrInvalidTimeout, got: %v", err)
	}
}

// TestValidate_NegativeTimeout verifies that a negative timeout is rejected (AC3).
func TestValidate_NegativeTimeout(t *testing.T) {
	req := ExecutionRequest{Command: "echo", Timeout: -5 * time.Second}

	err := req.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want error")
	}
	if !errors.Is(err, ErrInvalidTimeout) {
		t.Errorf("Validate() error should contain ErrInvalidTimeout, got: %v", err)
	}
}

// TestValidate_MalformedEnvVar verifies that malformed environment variables
// are rejected (AC4).
func TestValidate_MalformedEnvVar_AC4(t *testing.T) {
	req := ExecutionRequest{
		Command: "echo",
		Timeout: DefaultTimeout,
		Env:     []string{"VALID=ok", "MALFORMED_NO_EQUALS", "=empty-key"},
	}

	err := req.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want error for malformed env vars")
	}
	if !strings.Contains(err.Error(), "MALFORMED_NO_EQUALS") {
		t.Errorf("Validate() error should mention malformed env var, got: %v", err)
	}
	if !strings.Contains(err.Error(), "=empty-key") {
		t.Errorf("Validate() error should mention empty-key env var, got: %v", err)
	}
}

// TestValidate_ValidContext verifies that a valid execution context passes
// validation without errors (AC5).
func TestValidate_ValidContext_AC5(t *testing.T) {
	req := ExecutionRequest{
		Command:    "echo",
		Args:       []string{"hello"},
		WorkingDir: "/tmp",
		Env:        []string{"FOO=bar"},
		Timeout:    30 * time.Second,
	}

	if err := req.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

// TestValidate_MultipleErrors verifies that when multiple fields are invalid,
// all errors are collected and returned together (AC6).
func TestValidate_MultipleErrors_AC6(t *testing.T) {
	req := ExecutionRequest{
		Command:    "",
		Timeout:    0,
		WorkingDir: "/tmp/nonexistent-ts-p6-04-99999",
		Env:        []string{"BAD_ENV"},
	}

	err := req.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want multiple errors")
	}

	// Verify all four error types are present.
	if !errors.Is(err, ErrCommandRequired) {
		t.Error("combined error should contain ErrCommandRequired")
	}
	if !errors.Is(err, ErrInvalidTimeout) {
		t.Error("combined error should contain ErrInvalidTimeout")
	}
	if !strings.Contains(err.Error(), "working directory does not exist") {
		t.Error("combined error should mention working directory")
	}
	if !strings.Contains(err.Error(), "malformed environment variable") {
		t.Error("combined error should mention malformed env var")
	}
}

// TestValidate_ValidationBeforeLaunch verifies that validation occurs before
// the process is launched (AC7). This is tested through Lifecycle integration.
func TestValidate_ValidationBeforeLaunch_AC7(t *testing.T) {
	lc := NewLifecycleRunner(NewRunner())

	invalidReq := ExecutionRequest{
		Command: "",
		Timeout: DefaultTimeout,
	}

	result := lc.Execute(context.Background(), invalidReq)

	// The lifecycle should reach ResultAvailable without ever reaching
	// StageRunning, because validation fails before the process starts.
	if got := lc.Stage(); got != StageResultAvailable {
		t.Errorf("Stage() after invalid request = %v, want %v", got, StageResultAvailable)
	}
	if result.Status != StatusStartupFailure {
		t.Errorf("Result.Status = %v, want %v (validation should fail before launch)",
			result.Status, StatusStartupFailure)
	}
	if result.Err == nil {
		t.Error("Result.Err should be non-nil for validation failure")
	}
}

// TestValidate_CommandNotFound verifies that a command not in PATH is rejected.
func TestValidate_CommandNotFound(t *testing.T) {
	req := ExecutionRequest{
		Command: "nonexistent-command-ts-p6-04-test",
		Timeout: DefaultTimeout,
	}

	err := req.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want error for non-existent command")
	}
	if !strings.Contains(err.Error(), "command not found") {
		t.Errorf("Validate() error should mention 'command not found', got: %v", err)
	}
}

// TestValidate_CommandWithPath verifies that a command with an absolute path
// is resolved correctly.
func TestValidate_CommandWithPath(t *testing.T) {
	req := ExecutionRequest{
		Command: "/bin/echo",
		Timeout: DefaultTimeout,
	}

	if err := req.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil for /bin/echo", err)
	}
}

// TestValidate_NilEnvPasses verifies that nil environment (inherit parent)
// passes validation.
func TestValidate_NilEnvPasses(t *testing.T) {
	req := ExecutionRequest{
		Command: "echo",
		Timeout: DefaultTimeout,
		Env:     nil,
	}

	if err := req.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil for nil Env", err)
	}
}

// TestValidate_EmptyEnvPasses verifies that an empty environment slice
// (minimal environment) passes validation.
func TestValidate_EmptyEnvPasses(t *testing.T) {
	req := ExecutionRequest{
		Command: "echo",
		Timeout: DefaultTimeout,
		Env:     []string{},
	}

	if err := req.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil for empty Env", err)
	}
}

// TestValidate_EmptyWorkingDirPasses verifies that an empty working directory
// (inherit current directory) passes validation.
func TestValidate_EmptyWorkingDirPasses(t *testing.T) {
	req := ExecutionRequest{
		Command:    "echo",
		Timeout:    DefaultTimeout,
		WorkingDir: "",
	}

	if err := req.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil for empty WorkingDir", err)
	}
}

// TestValidate_ExistingWorkingDirPasses verifies that an existing working
// directory passes validation.
func TestValidate_ExistingWorkingDirPasses(t *testing.T) {
	req := ExecutionRequest{
		Command:    "echo",
		Timeout:    DefaultTimeout,
		WorkingDir: "/tmp",
	}

	if err := req.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil for /tmp", err)
	}
}
