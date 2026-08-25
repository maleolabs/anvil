package cmd

import (
	"errors"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/output"
)

// ── ReportErrorWithCode ─────────────────────────────────────────────

func TestReportErrorWithCode_SetsExitCode(t *testing.T) {
	_, _, stderr, err := executeCommand("project", "remove")
	// This will fail because there's no project context, but we can
	// test ReportErrorWithCode directly through a dedicated command test.

	// Direct test: create a command that uses ReportErrorWithCode.
	cmd := rootCmd
	bufOut := new(strings.Builder)
	bufErr := new(strings.Builder)
	cmd.SetOut(bufOut)
	cmd.SetErr(bufErr)

	appErr := &output.AppError{
		Message:    "test error",
		Reason:     "test reason",
		Resolution: "test resolution",
	}

	result := ReportErrorWithCode(cmd, appErr, output.ExitCodeConfig)

	// The returned error should be the AppError.
	if result == nil {
		t.Fatal("ReportErrorWithCode should return an error")
	}

	// Verify the error implements ExitCoder with the correct code.
	var exitErr output.ExitCoder
	if !errors.As(result, &exitErr) {
		t.Fatal("returned error should implement ExitCoder")
	}
	if exitErr.ExitCode() != output.ExitCodeConfig {
		t.Errorf("ExitCode() = %d, want %d", exitErr.ExitCode(), output.ExitCodeConfig)
	}

	// Verify error was written to stderr.
	if !strings.Contains(bufErr.String(), "Error: test error.") {
		t.Errorf("stderr should contain error message, got: %s", bufErr.String())
	}

	// Suppress unused variable warning.
	_ = stderr
	_ = err
}

// ── ReportError Propagates ExitCode ─────────────────────────────────

func TestReportError_PropagatesExitCode(t *testing.T) {
	cmd := rootCmd
	bufOut := new(strings.Builder)
	bufErr := new(strings.Builder)
	cmd.SetOut(bufOut)
	cmd.SetErr(bufErr)

	appErr := &output.AppError{
		Message:       "runtime error",
		ExitCodeValue: output.ExitCodeRuntime,
	}

	result := ReportError(cmd, appErr)

	if result == nil {
		t.Fatal("ReportError should return an error")
	}

	var exitErr output.ExitCoder
	if !errors.As(result, &exitErr) {
		t.Fatal("returned error should implement ExitCoder")
	}
	if exitErr.ExitCode() != output.ExitCodeRuntime {
		t.Errorf("ExitCode() = %d, want %d", exitErr.ExitCode(), output.ExitCodeRuntime)
	}
}

// ── ReportError Default ExitCode ────────────────────────────────────

func TestReportError_DefaultExitCode(t *testing.T) {
	cmd := rootCmd
	bufOut := new(strings.Builder)
	bufErr := new(strings.Builder)
	cmd.SetOut(bufOut)
	cmd.SetErr(bufErr)

	appErr := &output.AppError{
		Message: "general error",
		// ExitCode not set — should default to 1.
	}

	result := ReportError(cmd, appErr)

	if result == nil {
		t.Fatal("ReportError should return an error")
	}

	var exitErr output.ExitCoder
	if !errors.As(result, &exitErr) {
		t.Fatal("returned error should implement ExitCoder")
	}
	if exitErr.ExitCode() != output.ExitCodeGeneral {
		t.Errorf("ExitCode() = %d, want %d (default)", exitErr.ExitCode(), output.ExitCodeGeneral)
	}
}

// ── ReportPlainError Does Not Implement ExitCoder ───────────────────

func TestReportPlainError_DoesNotImplementExitCoder(t *testing.T) {
	cmd := rootCmd
	bufOut := new(strings.Builder)
	bufErr := new(strings.Builder)
	cmd.SetOut(bufOut)
	cmd.SetErr(bufErr)

	result := ReportPlainError(cmd, errors.New("plain error"), "plain error")

	if result == nil {
		t.Fatal("ReportPlainError should return an error")
	}

	// Plain errors do NOT implement ExitCoder — they use the default exit code 1.
	var exitErr output.ExitCoder
	if errors.As(result, &exitErr) {
		t.Error("plain error should NOT implement ExitCoder")
	}
}
