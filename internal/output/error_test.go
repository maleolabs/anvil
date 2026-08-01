package output

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// ── AppError.Error() ─────────────────────────────────────────────────

func TestAppError_Error_MessageOnly(t *testing.T) {
	e := &AppError{Message: "project not found"}
	got := e.Error()
	if got != "project not found" {
		t.Errorf("Error() = %q, want %q", got, "project not found")
	}
}

func TestAppError_Error_WithUnderlying(t *testing.T) {
	inner := errors.New("no such file")
	e := &AppError{Message: "could not load config", Err: inner}
	got := e.Error()
	if !strings.Contains(got, "could not load config") {
		t.Errorf("Error() should contain message, got %q", got)
	}
	if !strings.Contains(got, "no such file") {
		t.Errorf("Error() should contain underlying error, got %q", got)
	}
}

// ── AppError.Unwrap() ────────────────────────────────────────────────

func TestAppError_Unwrap_Nil(t *testing.T) {
	e := &AppError{Message: "no error underneath"}
	if e.Unwrap() != nil {
		t.Errorf("Unwrap() should return nil when Err is nil")
	}
}

func TestAppError_Unwrap_ReturnsInner(t *testing.T) {
	inner := errors.New("inner")
	e := &AppError{Message: "outer", Err: inner}
	if e.Unwrap() != inner {
		t.Errorf("Unwrap() should return the underlying error")
	}
}

// ── FormatAppError ───────────────────────────────────────────────────

func TestFormatAppError_FullThreeParts(t *testing.T) {
	e := &AppError{
		Message:    "could not load project",
		Reason:     "anvil.yaml was not found in the current directory",
		Resolution: "Run 'anvil init' to create a new project, or navigate to an existing project directory",
	}
	got := FormatAppError(e)

	if !strings.HasPrefix(got, "Error: could not load project.\n") {
		t.Errorf("should start with 'Error: ...', got %q", got)
	}
	if !strings.Contains(got, "Reason: anvil.yaml was not found") {
		t.Errorf("should contain Reason line, got %q", got)
	}
	if !strings.Contains(got, "Resolution: Run 'anvil init'") {
		t.Errorf("should contain Resolution line, got %q", got)
	}
}

func TestFormatAppError_NoReason(t *testing.T) {
	e := &AppError{
		Message:    "could not package artifact",
		Resolution: "Check that the source directory exists and is readable",
	}
	got := FormatAppError(e)

	if strings.Contains(got, "Reason:") {
		t.Errorf("should not contain Reason line when empty, got %q", got)
	}
	if !strings.HasPrefix(got, "Error: could not package artifact.\n") {
		t.Errorf("should start with Error line, got %q", got)
	}
	if !strings.Contains(got, "Resolution: Check that the source") {
		t.Errorf("should contain Resolution line, got %q", got)
	}
}

func TestFormatAppError_NoResolution(t *testing.T) {
	e := &AppError{
		Message: "installation failed",
		Reason:  "artifact manifest is corrupted",
	}
	got := FormatAppError(e)

	if strings.Contains(got, "Resolution:") {
		t.Errorf("should not contain Resolution line when empty, got %q", got)
	}
	if !strings.HasPrefix(got, "Error: installation failed.\n") {
		t.Errorf("should start with Error line, got %q", got)
	}
	if !strings.Contains(got, "Reason: artifact manifest is corrupted.") {
		t.Errorf("should contain Reason line, got %q", got)
	}
}

func TestFormatAppError_MessageOnly(t *testing.T) {
	e := &AppError{Message: "something went wrong"}
	got := FormatAppError(e)

	if !strings.HasPrefix(got, "Error: something went wrong.\n") {
		t.Errorf("should start with Error line, got %q", got)
	}
	// No empty Reason/Resolution lines should appear.
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 line, got %d: %q", len(lines), got)
	}
}

func TestFormatAppError_NoEmptyLines(t *testing.T) {
	e := &AppError{
		Message:    "test error",
		Reason:     "",
		Resolution: "",
	}
	got := FormatAppError(e)
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "Reason:") || strings.HasPrefix(line, "Resolution:") {
			t.Errorf("should not render empty Reason/Resolution lines, got %q in: %s", line, got)
		}
	}
}

func TestFormatAppError_VisuallyDistinct(t *testing.T) {
	e := &AppError{
		Message:    "artifact verification failed",
		Reason:     "checksum mismatch",
		Resolution: "Re-package the artifact with 'anvil artifact package'",
	}
	got := FormatAppError(e)

	// First line must be the Error line — visually distinct, first thing user sees.
	firstLine := strings.SplitN(got, "\n", 2)[0]
	if !strings.HasPrefix(firstLine, "Error:") {
		t.Errorf("first line must start with 'Error:', got %q", firstLine)
	}
}

// ── FormatPlainError ─────────────────────────────────────────────────

func TestFormatPlainError(t *testing.T) {
	got := FormatPlainError("could not load config")
	want := "Error: could not load config.\n"
	if got != want {
		t.Errorf("FormatPlainError() = %q, want %q", got, want)
	}
}

func TestFormatPlainErrorf(t *testing.T) {
	got := FormatPlainErrorf("could not load %s: %v", "config", errors.New("not found"))
	want := "Error: could not load config: not found.\n"
	if got != want {
		t.Errorf("FormatPlainErrorf() = %q, want %q", got, want)
	}
}

// ── WriteAppError ────────────────────────────────────────────────────

func TestWriteAppError_NilNoOutput(t *testing.T) {
	var buf bytes.Buffer
	WriteAppError(&buf, nil)
	if buf.Len() != 0 {
		t.Errorf("nil AppError should produce no output, got %q", buf.String())
	}
}

func TestWriteAppError_FullOutput(t *testing.T) {
	var buf bytes.Buffer
	e := &AppError{
		Message:    "could not verify artifact",
		Reason:     "the checksum does not match",
		Resolution: "Re-package the artifact with 'anvil artifact package'",
	}
	WriteAppError(&buf, e)
	got := buf.String()

	if !strings.HasPrefix(got, "Error: could not verify artifact.\n") {
		t.Errorf("should start with Error line, got %q", got)
	}
	if !strings.Contains(got, "Reason: the checksum does not match.") {
		t.Errorf("should contain Reason line, got %q", got)
	}
	if !strings.Contains(got, "Resolution: Re-package") {
		t.Errorf("should contain Resolution line, got %q", got)
	}
}

func TestWritePlainError(t *testing.T) {
	var buf bytes.Buffer
	WritePlainError(&buf, "something failed")
	got := buf.String()
	want := "Error: something failed.\n"
	if got != want {
		t.Errorf("WritePlainError() = %q, want %q", got, want)
	}
}

// TestWriteErrors_NonTerminalNoAnsi verifies that the error writer helpers
// produce no ANSI escape codes when writing to a non-terminal buffer
// (TS-008-009).
func TestWriteErrors_NonTerminalNoAnsi(t *testing.T) {
	var buf bytes.Buffer
	e := &AppError{
		Message:    "could not verify artifact",
		Reason:     "the checksum does not match",
		Resolution: "Re-package the artifact",
	}
	WriteAppError(&buf, e)
	if strings.Contains(buf.String(), "\x1b") {
		t.Errorf("WriteAppError() must not contain ANSI escape codes on non-terminal writers, got %q", buf.String())
	}

	buf.Reset()
	WritePlainError(&buf, "something failed")
	if strings.Contains(buf.String(), "\x1b") {
		t.Errorf("WritePlainError() must not contain ANSI escape codes on non-terminal writers, got %q", buf.String())
	}
}

// ── AppError.ExitCode() ─────────────────────────────────────────────

func TestAppError_ExitCode_Default(t *testing.T) {
	e := &AppError{Message: "general error"}
	if e.ExitCode() != 1 {
		t.Errorf("ExitCode() = %d, want 1 (default general)", e.ExitCode())
	}
}

func TestAppError_ExitCode_ExplicitGeneral(t *testing.T) {
	e := &AppError{Message: "validation error", ExitCodeValue: ExitCodeGeneral}
	if e.ExitCode() != 1 {
		t.Errorf("ExitCode() = %d, want 1", e.ExitCode())
	}
}

func TestAppError_ExitCode_Config(t *testing.T) {
	e := &AppError{Message: "duplicate project", ExitCodeValue: ExitCodeConfig}
	if e.ExitCode() != 2 {
		t.Errorf("ExitCode() = %d, want 2", e.ExitCode())
	}
}

func TestAppError_ExitCode_Runtime(t *testing.T) {
	e := &AppError{Message: "not found", ExitCodeValue: ExitCodeRuntime}
	if e.ExitCode() != 3 {
		t.Errorf("ExitCode() = %d, want 3", e.ExitCode())
	}
}

func TestAppError_ExitCode_Precondition(t *testing.T) {
	e := &AppError{Message: "not initialized", ExitCodeValue: ExitCodePrecondition}
	if e.ExitCode() != 4 {
		t.Errorf("ExitCode() = %d, want 4", e.ExitCode())
	}
}

// TestAppError_ExitCode_ImplementsExitCoder verifies that *AppError
// satisfies the ExitCoder interface via errors.As-style type assertion.
func TestAppError_ExitCode_ImplementsExitCoder(t *testing.T) {
	var ec ExitCoder = &AppError{Message: "test", ExitCodeValue: ExitCodeRuntime}
	if ec.ExitCode() != 3 {
		t.Errorf("ExitCoder.ExitCode() = %d, want 3", ec.ExitCode())
	}
}
