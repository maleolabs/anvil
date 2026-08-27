package execution

import (
	"context"
	"strings"
	"testing"
	"time"
)

// ST-P6-02: Execution result inspection.
//
// These tests validate all six acceptance criteria using the Observer
// interface as the consumer entry point.

// TestResultInspection_SuccessfulExecution verifies AC1:
// After a successful execution, the consumer can retrieve a result with
// exit code 0, captured output, duration, and "Completed" status.
//
// Reference: ST-P6-02 AC1
func TestResultInspection_SuccessfulExecution(t *testing.T) {
	observer := NewMutableObserver(NewRunner())

	req, err := NewExecutionRequest("echo", WithArgs([]string{"success output"}))
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	id, err := observer.Start(context.Background(), req)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	result, err := observer.GetResult(id)
	if err != nil {
		t.Fatalf("GetResult() failed: %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "success output") {
		t.Errorf("Stdout = %q, want to contain %q", result.Stdout, "success output")
	}
	if result.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", result.Duration)
	}
	if got := result.TerminationStatus(); got != TermCompleted {
		t.Errorf("TerminationStatus() = %v, want %v", got, TermCompleted)
	}
}

// TestResultInspection_FailedExecution verifies AC2:
// After a failed execution (non-zero exit), the consumer can retrieve a
// result with the exit code, output, and "Failed" status.
//
// Reference: ST-P6-02 AC2
func TestResultInspection_FailedExecution(t *testing.T) {
	observer := NewMutableObserver(NewRunner())

	req, err := NewExecutionRequest("sh",
		WithArgs([]string{"-c", "echo 'error message' >&2; exit 42"}),
	)
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	id, err := observer.Start(context.Background(), req)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	result, err := observer.GetResult(id)
	if err != nil {
		t.Fatalf("GetResult() failed: %v", err)
	}

	if result.ExitCode != 42 {
		t.Errorf("ExitCode = %d, want 42", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "error message") {
		t.Errorf("Stderr = %q, want to contain %q", result.Stderr, "error message")
	}
	if got := result.TerminationStatus(); got != TermFailed {
		t.Errorf("TerminationStatus() = %v, want %v", got, TermFailed)
	}
}

// TestResultInspection_TimedOutExecution verifies AC3:
// After a timed-out execution, the consumer can retrieve a result with
// partial output, duration equal to timeout, and "Timed Out" status.
//
// Reference: ST-P6-02 AC3
func TestResultInspection_TimedOutExecution(t *testing.T) {
	observer := NewMutableObserver(NewRunner())

	req, err := NewExecutionRequest("sh",
		WithArgs([]string{"-c", "echo 'partial-output-before-timeout'; sleep 30"}),
	)
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	timeout := 100 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	id, err := observer.Start(ctx, req)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	result, err := observer.GetResult(id)
	if err != nil {
		t.Fatalf("GetResult() failed: %v", err)
	}

	// Partial output should be preserved.
	if !strings.Contains(result.Stdout, "partial-output-before-timeout") {
		t.Errorf("Stdout = %q, want to contain %q", result.Stdout, "partial-output-before-timeout")
	}

	// Duration should be at least the timeout (allowing small overhead).
	if result.Duration < timeout {
		t.Errorf("Duration = %v, want >= %v", result.Duration, timeout)
	}

	if got := result.TerminationStatus(); got != TermTimedOut {
		t.Errorf("TerminationStatus() = %v, want %v", got, TermTimedOut)
	}
}

// TestResultInspection_CancelledExecution verifies AC4:
// After a cancelled execution, the consumer can retrieve a result with
// partial output and "Cancelled" status.
//
// Reference: ST-P6-02 AC4
func TestResultInspection_CancelledExecution(t *testing.T) {
	observer := NewMutableObserver(NewRunner())

	req, err := NewExecutionRequest("sh",
		WithArgs([]string{"-c", "echo 'partial-output-before-cancel'; sleep 30"}),
	)
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancel after a short delay to let the process start and produce output.
	time.AfterFunc(50*time.Millisecond, cancel)

	id, err := observer.Start(ctx, req)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	result, err := observer.GetResult(id)
	if err != nil {
		t.Fatalf("GetResult() failed: %v", err)
	}

	// Partial output should be preserved.
	if !strings.Contains(result.Stdout, "partial-output-before-cancel") {
		t.Errorf("Stdout = %q, want to contain %q", result.Stdout, "partial-output-before-cancel")
	}

	if got := result.TerminationStatus(); got != TermCancelled {
		t.Errorf("TerminationStatus() = %v, want %v", got, TermCancelled)
	}
}

// TestResultInspection_StartupFailure verifies AC5:
// After a startup failure, the consumer receives the failure reason.
//
// The Observer validates execution requests before starting them, so a
// nonexistent command is caught at the Start() level with a validation
// error containing the failure reason, rather than producing a Result
// with StatusStartupFailure.
//
// Reference: ST-P6-02 AC5
func TestResultInspection_StartupFailure(t *testing.T) {
	observer := NewMutableObserver(NewRunner())

	req, err := NewExecutionRequest("nonexistent-command-99999")
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	_, err = observer.Start(context.Background(), req)
	if err == nil {
		t.Fatal("Start() expected error for nonexistent command, got nil")
	}

	// Verify the error contains the failure reason.
	if !strings.Contains(err.Error(), "nonexistent-command-99999") {
		t.Errorf("Start() err = %q, want to contain command name", err.Error())
	}
	if !strings.Contains(err.Error(), "command not found") {
		t.Errorf("Start() err = %q, want to contain 'command not found'", err.Error())
	}
}

// TestResultInspection_ResultIsValueType verifies AC6:
// The consumer cannot modify the execution result.
//
// Result is a value type (struct, not a pointer), so any copy made by
// assignment or function call is independent. Modifying a copy does not
// affect the original.
//
// Reference: ST-P6-02 AC6
func TestResultInspection_ResultIsValueType(t *testing.T) {
	observer := NewMutableObserver(NewRunner())

	req, err := NewExecutionRequest("echo", WithArgs([]string{"immutable test"}))
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	id, err := observer.Start(context.Background(), req)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	original, err := observer.GetResult(id)
	if err != nil {
		t.Fatalf("GetResult() failed: %v", err)
	}

	// Copy the result and attempt to modify the copy.
	copy := original
	copy.ExitCode = 99
	copy.Stdout = "modified"

	// The original must remain unchanged.
	if original.ExitCode != 0 {
		t.Errorf("original ExitCode = %d, want 0 (was modified via copy)", original.ExitCode)
	}
	if original.Stdout != "" && !strings.Contains(original.Stdout, "immutable test") {
		t.Errorf("original Stdout = %q, want to contain %q", original.Stdout, "immutable test")
	}
	if original.TerminationStatus() != TermCompleted {
		t.Errorf("original TerminationStatus() = %v, want %v (was modified via copy)",
			original.TerminationStatus(), TermCompleted)
	}

	// Verify the copy is indeed different.
	if copy.ExitCode != 99 {
		t.Errorf("copy ExitCode = %d, want 99 (copy was not independent)", copy.ExitCode)
	}
	if copy.Stdout != "modified" {
		t.Errorf("copy Stdout = %q, want %q (copy was not independent)", copy.Stdout, "modified")
	}
}

// TestResultInspection_TerminationStatusMapping verifies the complete mapping
// from internal ExitStatus to user-facing TerminationStatus for all values.
//
// Reference: ST-P6-02
func TestResultInspection_TerminationStatusMapping(t *testing.T) {
	tests := []struct {
		status   ExitStatus
		expected TerminationStatus
		name     string
	}{
		{StatusSuccess, TermCompleted, "success → completed"},
		{StatusFailure, TermFailed, "failure → failed"},
		{StatusStartupFailure, TermStartupFailure, "startup failure → startup failure"},
		{StatusTimeout, TermTimedOut, "timeout → timed out"},
		{StatusCancelled, TermCancelled, "cancelled → cancelled"},
		{StatusUnexpectedTermination, TermUnexpectedTermination, "unexpected termination → unexpected termination"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Result{Status: tt.status}
			if got := r.TerminationStatus(); got != tt.expected {
				t.Errorf("TerminationStatus() = %v, want %v for ExitStatus %v",
					got, tt.expected, tt.status)
			}
		})
	}
}

// TestResultInspection_TerminationStatusStrings verifies that all
// TerminationStatus values produce the expected human-readable string.
//
// Reference: ST-P6-02
func TestResultInspection_TerminationStatusStrings(t *testing.T) {
	tests := []struct {
		status TerminationStatus
		want   string
	}{
		{TermCompleted, "completed"},
		{TermFailed, "failed"},
		{TermStartupFailure, "startup failure"},
		{TermTimedOut, "timed out"},
		{TermCancelled, "cancelled"},
		{TermUnexpectedTermination, "unexpected termination"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.status.String(); got != tt.want {
				t.Errorf("%v.String() = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

// TestResultInspection_UnknownTerminationStatus verifies that an undefined
// TerminationStatus value returns "unknown" from String().
func TestResultInspection_UnknownTerminationStatus(t *testing.T) {
	var s TerminationStatus = 99
	if got := s.String(); got != "unknown" {
		t.Errorf("TerminationStatus(99).String() = %q, want %q", got, "unknown")
	}
}
