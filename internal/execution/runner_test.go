package execution

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestRunner_Interface verifies that NewRunner returns a non-nil Runner.
func TestRunner_Interface(t *testing.T) {
	r := NewRunner()
	if r == nil {
		t.Fatal("NewRunner() returned nil")
	}
}

// TestExecute_Success verifies that a successful command returns a Result with
// StatusSuccess, the correct captured output, and a non-zero duration.
func TestExecute_Success(t *testing.T) {
	r := NewRunner()
	req, err := NewExecutionRequest("echo", WithArgs([]string{"hello world"}))
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	ctx := context.Background()
	result := r.Execute(ctx, req)

	if result.Status != StatusSuccess {
		t.Errorf("Status = %v, want %v", result.Status, StatusSuccess)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
	if strings.TrimSpace(result.Stdout) != "hello world" {
		t.Errorf("Stdout = %q, want %q", result.Stdout, "hello world\n")
	}
	if result.Stderr != "" {
		t.Errorf("Stderr = %q, want empty", result.Stderr)
	}
	if result.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", result.Duration)
	}
	if result.Err != nil {
		t.Errorf("Err = %v, want nil", result.Err)
	}
}

// TestExecute_Failure verifies that a command with a non-zero exit returns
// StatusFailure and captures stderr appropriately.
func TestExecute_Failure(t *testing.T) {
	r := NewRunner()

	// Use a command that fails with a non-zero exit code.
	// On Unix: "false" exits with 1.
	req, err := NewExecutionRequest("false")
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	ctx := context.Background()
	result := r.Execute(ctx, req)

	if result.Status != StatusFailure {
		t.Errorf("Status = %v, want %v", result.Status, StatusFailure)
	}
	if result.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", result.ExitCode)
	}
	if result.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", result.Duration)
	}
}

// TestExecute_StartupFailure verifies that a non-existent command returns
// StatusStartupFailure.
func TestExecute_StartupFailure(t *testing.T) {
	r := NewRunner()

	req, err := NewExecutionRequest("nonexistent-command-12345")
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	ctx := context.Background()
	result := r.Execute(ctx, req)

	if result.Status != StatusStartupFailure {
		t.Errorf("Status = %v, want %v", result.Status, StatusStartupFailure)
	}
	if result.ExitCode != -1 {
		t.Errorf("ExitCode = %d, want -1", result.ExitCode)
	}
	if result.Err == nil {
		t.Error("Err should be non-nil for startup failure")
	}
}

// TestExecute_WorkingDir verifies that the process runs in the specified
// working directory by executing pwd (or its platform equivalent).
func TestExecute_WorkingDir(t *testing.T) {
	r := NewRunner()

	// Use different commands on different platforms to get the current
	// working directory.
	var cmd string
	var args []string
	if runtime.GOOS == "windows" {
		cmd = "cmd"
		args = []string{"/c", "cd"}
	} else {
		cmd = "pwd"
		args = []string{}
	}

	req, err := NewExecutionRequest(cmd,
		WithArgs(args),
		WithWorkingDir("/tmp"),
	)
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	ctx := context.Background()
	result := r.Execute(ctx, req)

	if result.Status != StatusSuccess {
		t.Fatalf("Status = %v, want %v: stderr=%q", result.Status, StatusSuccess, result.Stderr)
	}

	got := strings.TrimSpace(result.Stdout)
	if got != "/tmp" {
		// On some platforms /tmp may be a symlink; accept /private/tmp on macOS.
		if got != "/private/tmp" {
			t.Errorf("working directory = %q, want /tmp", got)
		}
	}
}

// TestExecute_Environment verifies that the process receives the specified
// environment variables.
func TestExecute_Environment(t *testing.T) {
	r := NewRunner()

	req, err := NewExecutionRequest("sh",
		WithArgs([]string{"-c", "echo $ANVIL_TEST_VAR"}),
		WithEnv([]string{"ANVIL_TEST_VAR=hello-from-execution"}),
	)
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	ctx := context.Background()
	result := r.Execute(ctx, req)

	if result.Status != StatusSuccess {
		t.Fatalf("Status = %v, want %v: stderr=%q", result.Status, StatusSuccess, result.Stderr)
	}

	got := strings.TrimSpace(result.Stdout)
	if got != "hello-from-execution" {
		t.Errorf("ANVIL_TEST_VAR = %q, want %q", got, "hello-from-execution")
	}
}

// TestExecute_StdoutCapture verifies that stdout is captured for commands
// that produce multi-line output.
func TestExecute_StdoutCapture(t *testing.T) {
	r := NewRunner()

	req, err := NewExecutionRequest("echo",
		WithArgs([]string{"line1\nline2\nline3"}),
	)
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	ctx := context.Background()
	result := r.Execute(ctx, req)

	if result.Status != StatusSuccess {
		t.Fatalf("Status = %v, want %v", result.Status, StatusSuccess)
	}

	lines := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	if len(lines) < 3 {
		t.Errorf("Stdout lines = %d, want >= 3", len(lines))
	}
}

// TestExecute_StderrCapture verifies that stderr is captured.
func TestExecute_StderrCapture(t *testing.T) {
	r := NewRunner()

	req, err := NewExecutionRequest("sh",
		WithArgs([]string{"-c", "echo error-output >&2"}),
	)
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	ctx := context.Background()
	result := r.Execute(ctx, req)

	if result.Status != StatusSuccess {
		t.Fatalf("Status = %v, want %v", result.Status, StatusSuccess)
	}

	got := strings.TrimSpace(result.Stderr)
	if got != "error-output" {
		t.Errorf("Stderr = %q, want %q", got, "error-output")
	}
}

// TestExecute_Duration verifies that the duration is measured and is
// approximately correct for a command that takes a known amount of time.
func TestExecute_Duration(t *testing.T) {
	r := NewRunner()

	sleepTime := 50 * time.Millisecond
	req, err := NewExecutionRequest("sleep",
		WithArgs([]string{"0.05"}),
	)
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	ctx := context.Background()
	result := r.Execute(ctx, req)

	if result.Status != StatusSuccess {
		t.Fatalf("Status = %v, want %v", result.Status, StatusSuccess)
	}

	if result.Duration < sleepTime {
		t.Errorf("Duration = %v, want >= %v", result.Duration, sleepTime)
	}
}

// TestExecute_Cancellation verifies that cancelling the context terminates
// the process and returns StatusCancelled.
func TestExecute_Cancellation(t *testing.T) {
	r := NewRunner()

	req, err := NewExecutionRequest("sleep", WithArgs([]string{"30"}))
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately before execution starts.

	result := r.Execute(ctx, req)

	if result.Status != StatusCancelled {
		t.Errorf("Status = %v, want %v", result.Status, StatusCancelled)
	}
}

// TestExecute_ResultStruct verifies that the returned Result contains all
// expected fields with correct types.
func TestExecute_ResultStruct(t *testing.T) {
	r := NewRunner()

	req, err := NewExecutionRequest("echo", WithArgs([]string{"test"}))
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	ctx := context.Background()
	result := r.Execute(ctx, req)

	// Verify all fields are accessible.
	_ = result.Status
	_ = result.ExitCode
	_ = result.Stdout
	_ = result.Stderr
	_ = result.Duration
	_ = result.Err
}

// TestExecute_TimeoutViaRequest verifies that timeout enforcement via
// req.Timeout terminates a long-running process (TS-P6-07 AC1, AC4).
func TestExecute_TimeoutViaRequest(t *testing.T) {
	r := NewRunner()

	// Create a request with a very short timeout for a process that
	// would run much longer. The timeout from req.Timeout should be
	// enforced by the runner.
	req, err := NewExecutionRequest("sleep",
		WithArgs([]string{"30"}),
		WithTimeout(10*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	// Use background context (no deadline) — the runner must apply
	// req.Timeout automatically.
	ctx := context.Background()
	result := r.Execute(ctx, req)

	if result.Status != StatusTimeout {
		t.Errorf("Status = %v, want %v", result.Status, StatusTimeout)
	}
	if result.ExitCode != -1 {
		t.Errorf("ExitCode = %d, want -1", result.ExitCode)
	}
	if result.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", result.Duration)
	}
	if result.Err == nil {
		t.Error("Err should be non-nil for timeout")
	}
}

// TestExecute_TimeoutPreservesOutput verifies that output produced before
// a timeout is preserved (TS-P6-07 AC3).
func TestExecute_TimeoutPreservesOutput(t *testing.T) {
	r := NewRunner()

	// Use a command that produces output before sleeping, so output
	// should be captured even when the process is killed by timeout.
	req, err := NewExecutionRequest("sh",
		WithArgs([]string{"-c", "echo 'output-before-timeout'; sleep 30"}),
		WithTimeout(100*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	ctx := context.Background()
	result := r.Execute(ctx, req)

	if result.Status != StatusTimeout {
		t.Errorf("Status = %v, want %v", result.Status, StatusTimeout)
	}
	if !strings.Contains(result.Stdout, "output-before-timeout") {
		t.Errorf("Stdout = %q, want to contain %q", result.Stdout, "output-before-timeout")
	}
	if result.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", result.Duration)
	}
}

// TestExecute_CustomTimeoutShorterThanContextDeadline verifies that when
// req.Timeout is shorter than an existing context deadline, the runner
// uses the more restrictive timeout (TS-P6-07).
func TestExecute_CustomTimeoutShorterThanContextDeadline(t *testing.T) {
	r := NewRunner()

	// Create a context with a 10-second deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create a request with a much shorter timeout (10ms).
	req, err := NewExecutionRequest("sleep",
		WithArgs([]string{"30"}),
		WithTimeout(10*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	result := r.Execute(ctx, req)

	if result.Status != StatusTimeout {
		t.Errorf("Status = %v, want %v", result.Status, StatusTimeout)
	}
}

// TestExecute_UnexpectedTermination verifies that a process terminated by
// a signal is reported as StatusUnexpectedTermination (TS-P6-08 AC5).
func TestExecute_UnexpectedTermination(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal test requires Unix")
	}

	r := NewRunner()

	// Start a process that kills itself with SIGKILL. The runner should
	// detect this as an unexpected termination (signal), not a normal
	// non-zero exit.
	req, err := NewExecutionRequest("sh",
		WithArgs([]string{"-c", "kill -KILL $$"}),
		WithTimeout(5*time.Second),
	)
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	ctx := context.Background()
	result := r.Execute(ctx, req)

	if result.Status != StatusUnexpectedTermination {
		t.Errorf("Status = %v, want %v", result.Status, StatusUnexpectedTermination)
	}
	if result.ExitCode != -1 {
		t.Errorf("ExitCode = %d, want -1", result.ExitCode)
	}
	if result.Err == nil {
		t.Error("Err should be non-nil for unexpected termination")
	}
}

// TestExecute_NonZeroExitIsNotUnexpectedTermination verifies that a normal
// non-zero exit (e.g., "false") is reported as StatusFailure, not as
// StatusUnexpectedTermination (TS-P6-08 AC2).
func TestExecute_NonZeroExitIsNotUnexpectedTermination(t *testing.T) {
	r := NewRunner()

	req, err := NewExecutionRequest("false")
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	ctx := context.Background()
	result := r.Execute(ctx, req)

	if result.Status != StatusFailure {
		t.Errorf("Status = %v, want %v", result.Status, StatusFailure)
	}
	if result.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", result.ExitCode)
	}
	if result.Status == StatusUnexpectedTermination {
		t.Error("non-zero exit should not be classified as unexpected termination")
	}
}
