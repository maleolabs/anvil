package execution

import (
	"fmt"
	"time"
)

// ExitStatus represents how a process terminated.
type ExitStatus int

const (
	// StatusSuccess indicates the process exited with code 0.
	StatusSuccess ExitStatus = iota

	// StatusFailure indicates the process exited with a non-zero code.
	StatusFailure

	// StatusStartupFailure indicates the process could not be launched.
	StatusStartupFailure

	// StatusTimeout indicates the process was terminated due to timeout.
	StatusTimeout

	// StatusCancelled indicates the execution was cancelled before completion.
	StatusCancelled

	// StatusUnexpectedTermination indicates the process was terminated
	// by a signal or system event (e.g., SIGSEGV, SIGABRT, SIGKILL) that
	// was not requested by the consumer.
	//
	// Reference: TS-P6-08, ADR-008 §8.1
	StatusUnexpectedTermination
)

// String returns a human-readable name for the exit status.
func (s ExitStatus) String() string {
	switch s {
	case StatusSuccess:
		return "success"
	case StatusFailure:
		return "failure"
	case StatusStartupFailure:
		return "startup failure"
	case StatusTimeout:
		return "timeout"
	case StatusCancelled:
		return "cancelled"
	case StatusUnexpectedTermination:
		return "unexpected termination"
	default:
		return "unknown"
	}
}

// Result captures the complete outcome of a single process execution.
//
// It contains everything a consumer needs to determine what happened:
// the exit code, captured output streams, execution duration, and an
// overall status classification.
//
// Reference: TS-P6-01, ADR-008 §8.2
type Result struct {
	// Status classifies the execution outcome.
	Status ExitStatus

	// ExitCode is the process exit code. Only meaningful when Status is
	// StatusSuccess or StatusFailure.
	ExitCode int

	// Stdout contains the full standard output produced by the process.
	Stdout string

	// Stderr contains the full standard error produced by the process.
	Stderr string

	// Duration is the wall-clock time the process was executing.
	Duration time.Duration

	// Err contains the underlying error when the process could not be
	// started or was terminated abnormally. Nil on normal completion.
	Err error
}

// String returns a human-readable summary of the execution result.
//
// The format is suitable for logging and display purposes.
//
// Reference: TS-P6-05
func (r Result) String() string {
	return fmt.Sprintf(
		"status=%s exitCode=%d duration=%s stdout=%q stderr=%q",
		r.Status, r.ExitCode, r.Duration, r.Stdout, r.Stderr,
	)
}

// Success returns true when the execution completed with StatusSuccess.
//
// This is a convenience method for consumers that only need to check
// for successful completion.
//
// Reference: TS-P6-05
func (r Result) Success() bool {
	return r.Status == StatusSuccess
}

// Failed returns true when the execution did not complete successfully.
//
// This covers all non-success outcomes: StatusFailure,
// StatusStartupFailure, StatusTimeout, StatusCancelled,
// and StatusUnexpectedTermination.
//
// Reference: TS-P6-05
func (r Result) Failed() bool {
	return r.Status != StatusSuccess
}

// TerminationStatus represents the user-facing status of an execution.
//
// It provides human-readable names for execution outcomes as described
// in ST-P6-02. This is separate from the internal ExitStatus — it is the
// consumer-facing representation.
//
// Reference: ST-P6-02
type TerminationStatus int

const (
	// TermCompleted indicates the process completed successfully (exit code 0).
	TermCompleted TerminationStatus = iota

	// TermFailed indicates the process exited with a non-zero exit code.
	TermFailed

	// TermStartupFailure indicates the process could not be launched.
	TermStartupFailure

	// TermTimedOut indicates the process was terminated due to exceeding
	// the configured timeout.
	TermTimedOut

	// TermCancelled indicates the execution was cancelled before completion.
	TermCancelled

	// TermUnexpectedTermination indicates the process was terminated by
	// a signal or system event (e.g., SIGSEGV, SIGABRT, SIGKILL).
	//
	// Reference: TS-P6-08, ADR-008 §8.1
	TermUnexpectedTermination
)

// String returns the user-facing name of the termination status.
func (s TerminationStatus) String() string {
	switch s {
	case TermCompleted:
		return "completed"
	case TermFailed:
		return "failed"
	case TermStartupFailure:
		return "startup failure"
	case TermTimedOut:
		return "timed out"
	case TermCancelled:
		return "cancelled"
	case TermUnexpectedTermination:
		return "unexpected termination"
	default:
		return "unknown"
	}
}

// TerminationStatus returns the user-facing termination status.
//
// This maps the internal ExitStatus to the consumer-facing
// TerminationStatus as defined in ST-P6-02:
//   - StatusSuccess → TermCompleted
//   - StatusFailure → TermFailed
//   - StatusStartupFailure → TermStartupFailure
//   - StatusTimeout → TermTimedOut
//   - StatusCancelled → TermCancelled
//   - StatusUnexpectedTermination → TermUnexpectedTermination
//
// Reference: ST-P6-02, TS-P6-08
func (r Result) TerminationStatus() TerminationStatus {
	switch r.Status {
	case StatusSuccess:
		return TermCompleted
	case StatusFailure:
		return TermFailed
	case StatusStartupFailure:
		return TermStartupFailure
	case StatusTimeout:
		return TermTimedOut
	case StatusCancelled:
		return TermCancelled
	case StatusUnexpectedTermination:
		return TermUnexpectedTermination
	default:
		return TermCompleted
	}
}
