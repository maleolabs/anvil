// Package execution provides the Process Runner abstraction — the single
// mechanism for executing external processes in Anvil.
//
// The package defines:
//   - ExecutionRequest: a complete specification of what to run
//   - Result: the outcome of an execution
//   - Runner: the interface for launching and monitoring processes
//
// # Consumer Integration
//
// EPIC-004 (Release activation/rollback) and EPIC-005 (Runtime operations)
// consume the Process Runner through the Runner interface. Consumers create
// an ExecutionRequest via NewExecutionRequest, submit it to a Runner via
// Execute, and inspect the returned Result to determine the outcome.
//
// Usage pattern:
//
//	runner := execution.NewRunner()
//	req, _ := execution.NewExecutionRequest("command",
//	    execution.WithArgs([]string{"arg1"}),
//	    execution.WithWorkingDir("/path"),
//	    execution.WithTimeout(30*time.Second),
//	)
//	result := runner.Execute(ctx, req)
//	switch result.Status {
//	case execution.StatusSuccess:
//	    // handle success
//	case execution.StatusFailure:
//	    // handle failure with result.ExitCode
//	}
//
// All external process execution must go through this package. No consumer
// may spawn subprocesses directly.
//
// Reference: TS-P6-02, TS-P6-01, ST-P6-01, ADR-008, EPIC-006
package execution

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// DefaultTimeout is the default execution timeout applied when an ExecutionRequest
// does not specify one. Consumers may override this by setting a custom Timeout.
const DefaultTimeout = 5 * time.Minute

// ExecutionRequest fully specifies what to execute and under what conditions.
//
// Every field is explicit. There are no implicit parameters. Once created,
// the request is immutable — it must not be modified after submission.
//
// Reference: TS-P6-02, ADR-008 §6.2
type ExecutionRequest struct {
	// Command is the executable to run. Required: must be non-empty.
	Command string

	// Args is an optional list of arguments passed to the command.
	// Defaults to an empty slice.
	Args []string

	// WorkingDir is the directory in which the process is launched.
	// Empty string means the current process working directory.
	WorkingDir string

	// Env is an optional list of environment variables in "key=value" format.
	// Nil means inherit the parent process environment.
	// An empty slice means no environment variables (minimal environment).
	Env []string

	// Timeout limits how long the process may run before being terminated.
	// Zero or negative values cause DefaultTimeout to be used.
	Timeout time.Duration
}

// NewExecutionRequest creates an ExecutionRequest with the given command and
// optional field overrides. It applies sensible defaults for all unspecified
// fields.
//
// cmd is required (must be non-empty). All other parameters use variadic
// Option functions for optional configuration.
func NewExecutionRequest(cmd string, opts ...RequestOption) (ExecutionRequest, error) {
	if cmd == "" {
		return ExecutionRequest{}, ErrCommandRequired
	}

	req := ExecutionRequest{
		Command:    cmd,
		Args:       []string{},
		WorkingDir: "",
		Env:        nil,
		Timeout:    DefaultTimeout,
	}

	for _, opt := range opts {
		opt(&req)
	}

	return req, nil
}

// RequestOption configures an ExecutionRequest field.
type RequestOption func(*ExecutionRequest)

// WithArgs sets the command arguments.
func WithArgs(args []string) RequestOption {
	return func(req *ExecutionRequest) {
		if args != nil {
			req.Args = args
		}
	}
}

// WithWorkingDir sets the working directory for the process.
func WithWorkingDir(dir string) RequestOption {
	return func(req *ExecutionRequest) {
		req.WorkingDir = dir
	}
}

// WithEnv sets the environment variables for the process.
// Pass nil to inherit the parent environment (default behavior).
// Pass an empty slice to use a minimal environment.
func WithEnv(env []string) RequestOption {
	return func(req *ExecutionRequest) {
		req.Env = env
	}
}

// WithTimeout sets the execution timeout. If zero or negative, DefaultTimeout
// is used instead.
func WithTimeout(timeout time.Duration) RequestOption {
	return func(req *ExecutionRequest) {
		if timeout > 0 {
			req.Timeout = timeout
		}
	}
}

// Validate checks whether the ExecutionRequest is well-formed.
//
// It validates the following fields:
//   - Command: must not be empty and should resolve to an executable
//   - Timeout: must be a positive value
//   - WorkingDir: if specified, must exist and be a directory
//   - Env: each entry must be well-formed ("KEY=VALUE")
//
// When multiple fields are invalid, all errors are collected and returned
// together via errors.Join. Consumers can use errors.Is or errors.As to
// inspect individual validation errors.
//
// Reference: TS-P6-04, ADR-008 §7.1
func (req ExecutionRequest) Validate() error {
	var errs []error

	if req.Command == "" {
		errs = append(errs, ErrCommandRequired)
	} else {
		// Best-effort check: verify the command resolves to an executable.
		// This is a pre-check; the actual execution will still fail with
		// StatusStartupFailure if the command is removed between validation
		// and execution.
		if _, err := exec.LookPath(req.Command); err != nil {
			errs = append(errs, &ValidationError{
				Message: fmt.Sprintf("command not found: %s", req.Command),
			})
		}
	}

	if req.Timeout <= 0 {
		errs = append(errs, ErrInvalidTimeout)
	}

	if req.WorkingDir != "" {
		info, err := os.Stat(req.WorkingDir)
		if err != nil {
			errs = append(errs, &ValidationError{
				Message: fmt.Sprintf("working directory does not exist: %s", req.WorkingDir),
			})
		} else if !info.IsDir() {
			errs = append(errs, &ValidationError{
				Message: fmt.Sprintf("working directory is not a directory: %s", req.WorkingDir),
			})
		}
	}

	for i, env := range req.Env {
		if !strings.Contains(env, "=") || strings.HasPrefix(env, "=") {
			errs = append(errs, &ValidationError{
				Message: fmt.Sprintf("malformed environment variable at index %d: %s (expected KEY=VALUE)", i, env),
			})
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

var (
	// ErrCommandRequired is returned when Command is empty.
	ErrCommandRequired = &ValidationError{"command is required"}

	// ErrInvalidTimeout is returned when Timeout is zero or negative.
	ErrInvalidTimeout = &ValidationError{"timeout must be positive"}
)

// ValidationError indicates an ExecutionRequest validation failure.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// Unwrap returns nil so that errors.Is can match against ValidationError
// sentinels directly. Each ValidationError is a leaf error.
func (e *ValidationError) Unwrap() error {
	return nil
}
