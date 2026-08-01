package execution

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"runtime"
	"syscall"
	"time"
)

// Runner executes external processes and captures their output.
//
// It is the single abstraction for all external process execution in Anvil.
// No other component may execute external processes directly.
//
// Reference: TS-P6-01, ADR-008 §4, ADR-008 §9.1
type Runner interface {
	// Execute launches a process specified by the request and waits for it
	// to complete. It returns a Result containing the exit code, captured
	// output, duration, and any execution error.
	//
	// The ctx parameter allows cancellation and timeout propagation.
	//
	//   - If the context is cancelled, the process is terminated and the
	//     result status is StatusCancelled.
	//   - If the context deadline is exceeded, the process is terminated
	//     and the result status is StatusTimeout.
	Execute(ctx context.Context, req ExecutionRequest) Result
}

// osExecRunner is the default Runner implementation that uses Go's os/exec
// package to launch and manage subprocesses.
type osExecRunner struct{}

// NewRunner creates a new Runner backed by the operating system's process
// execution facilities (os/exec).
func NewRunner() Runner {
	return &osExecRunner{}
}

// Execute launches the process, monitors it, captures output, and returns
// a structured Result.
//
// Timeout enforcement (TS-P6-07):
//   - If req.Timeout > 0, the runner wraps the context with a deadline.
//   - If the context already has a shorter deadline, the existing deadline
//     takes precedence.
//   - If no timeout is specified, the DefaultTimeout (5 min) is applied.
//
// Failure detection (TS-P6-08):
//   - Startup failures (command not found, permission denied) → StatusStartupFailure
//   - Non-zero exit codes → StatusFailure
//   - Timeout → StatusTimeout
//   - Cancellation → StatusCancelled
//   - Signal termination (SIGSEGV, SIGABRT, etc.) → StatusUnexpectedTermination
func (r *osExecRunner) Execute(ctx context.Context, req ExecutionRequest) Result {
	start := time.Now()

	// Apply the request timeout to enforce consistent timeout behavior
	// across all executions (TS-P6-07). If the context already has a
	// stricter deadline, WithTimeout preserves the shorter one.
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	// Check for pre-cancelled or timed-out context before attempting to start.
	if ctx.Err() != nil {
		if isTimeoutError(ctx) {
			return Result{
				Status:   StatusTimeout,
				ExitCode: -1,
				Duration: time.Since(start),
				Err:      ctx.Err(),
			}
		}
		return Result{
			Status:   StatusCancelled,
			ExitCode: -1,
			Duration: time.Since(start),
			Err:      ctx.Err(),
		}
	}

	// Build the exec.Cmd from the request.
	cmd := exec.CommandContext(ctx, req.Command, req.Args...)

	// Set working directory if specified.
	if req.WorkingDir != "" {
		cmd.Dir = req.WorkingDir
	}

	// Set environment if specified. Nil inherits parent environment.
	if req.Env != nil {
		cmd.Env = req.Env
	}

	// Capture stdout and stderr.
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Attempt to start the process.
	if err := cmd.Start(); err != nil {
		return Result{
			Status:   StatusStartupFailure,
			ExitCode: -1,
			Stdout:   "",
			Stderr:   "",
			Duration: time.Since(start),
			Err:      err,
		}
	}

	// Wait for the process to complete.
	err := cmd.Wait()
	duration := time.Since(start)

	// Determine the result based on how the process terminated.
	// Check context state first to distinguish timeout from cancellation,
	// since os/exec may return the same underlying error for both.
	switch {
	case err == nil:
		// Process exited successfully.
		return Result{
			Status:   StatusSuccess,
			ExitCode: 0,
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			Duration: duration,
			Err:      nil,
		}

	case isTimeoutError(ctx):
		// Process exceeded the configured context deadline.
		return Result{
			Status:   StatusTimeout,
			ExitCode: -1,
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			Duration: duration,
			Err:      ctx.Err(),
		}

	case ctx.Err() != nil:
		// Context was cancelled before completion.
		// We check ctx.Err() rather than matching the error string from
		// os/exec because "signal: killed" can occur both from context
		// cancellation AND from external signal sources (TS-P6-08).
		return Result{
			Status:   StatusCancelled,
			ExitCode: -1,
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			Duration: duration,
			Err:      ctx.Err(),
		}

	default:
		// Check whether the process was terminated by a signal
		// (unexpected termination) rather than a normal non-zero exit.
		if isUnexpectedTermination(cmd) {
			return Result{
				Status:   StatusUnexpectedTermination,
				ExitCode: -1,
				Stdout:   stdout.String(),
				Stderr:   stderr.String(),
				Duration: duration,
				Err:      err,
			}
		}

		// Process exited with a non-zero exit code.
		exitCode := extractExitCode(cmd, err)
		return Result{
			Status:   StatusFailure,
			ExitCode: exitCode,
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			Duration: duration,
			Err:      err,
		}
	}
}

// isTimeoutError checks whether the given context has exceeded its deadline.
// This is the authoritative check for timeout detection, since os/exec may
// return "signal: killed" for both cancellation and timeout scenarios.
func isTimeoutError(ctx context.Context) bool {
	if ctx.Err() == nil {
		return false
	}
	return errors.Is(ctx.Err(), context.DeadlineExceeded)
}

// isUnexpectedTermination checks whether the process was terminated by a
// signal or system event, as opposed to a normal non-zero exit.
//
// On Unix, it uses syscall.WaitStatus to check for signaled termination
// (e.g., SIGSEGV, SIGABRT, SIGKILL). On non-Unix platforms, it falls back
// to checking whether the exit code is -1, which is the convention when a
// process does not exit normally.
//
// Reference: TS-P6-08, ADR-008 §8.1
func isUnexpectedTermination(cmd *exec.Cmd) bool {
	if cmd.ProcessState == nil {
		return false
	}

	// On Unix, use WaitStatus to detect signal-induced termination.
	if runtime.GOOS != "windows" {
		if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok {
			return status.Signaled()
		}
	}

	// Fallback: exit code -1 typically indicates the process did not
	// exit normally (killed by signal, crashed, etc.).
	return cmd.ProcessState.ExitCode() == -1
}

// extractExitCode attempts to retrieve the exit code from a finished command.
// Returns -1 if the exit code cannot be determined.
func extractExitCode(cmd *exec.Cmd, err error) int {
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return -1
}
