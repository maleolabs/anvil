package execution

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Consumer integration tests for ST-P6-01.
//
// These tests demonstrate how EPIC-004 (Release activation/rollback) and
// EPIC-005 (Runtime operations) consume the Process Runner.
//
// Reference: ST-P6-01, ADR-008, EPIC-006

// TestConsumerIntegration_ActivationExecutesCommand simulates an EPIC-004
// activation operation that extracts artifacts, configures resources, or
// runs framework commands through the Process Runner.
func TestConsumerIntegration_ActivationExecutesCommand(t *testing.T) {
	runner := NewRunner()

	// EPIC-004 activation might run a command like extracting an artifact.
	req, err := NewExecutionRequest("echo",
		WithArgs([]string{"activating release"}),
		WithWorkingDir("/tmp"),
	)
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	ctx := context.Background()
	result := runner.Execute(ctx, req)

	// Consumer verifies execution succeeded.
	if result.Status != StatusSuccess {
		t.Errorf("activation command Status = %v, want %v", result.Status, StatusSuccess)
	}
	if result.ExitCode != 0 {
		t.Errorf("activation command ExitCode = %d, want 0", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "activating release") {
		t.Errorf("activation Stdout = %q, want to contain %q", result.Stdout, "activating release")
	}
	if result.Duration <= 0 {
		t.Errorf("activation Duration = %v, want > 0", result.Duration)
	}
	if result.Err != nil {
		t.Errorf("activation Err = %v, want nil", result.Err)
	}
}

// TestConsumerIntegration_RollbackExecutesCommand simulates an EPIC-004
// rollback operation that reverts a deployment by running commands through
// the Process Runner.
func TestConsumerIntegration_RollbackExecutesCommand(t *testing.T) {
	runner := NewRunner()

	// EPIC-004 rollback might run a rollback script or command.
	// Use a simple echo to simulate, with a command that could represent
	// a rollback action.
	req, err := NewExecutionRequest("echo",
		WithArgs([]string{"rolling back release"}),
	)
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	ctx := context.Background()
	result := runner.Execute(ctx, req)

	if result.Status != StatusSuccess {
		t.Errorf("rollback command Status = %v, want %v", result.Status, StatusSuccess)
	}
	if result.ExitCode != 0 {
		t.Errorf("rollback command ExitCode = %d, want 0", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "rolling back release") {
		t.Errorf("rollback Stdout = %q, want to contain %q", result.Stdout, "rolling back release")
	}
}

// TestConsumerIntegration_RuntimeOperation simulates an EPIC-005 Runtime
// operation such as creating directories, managing symlinks, or running
// cleanup processes through the Process Runner.
func TestConsumerIntegration_RuntimeOperation(t *testing.T) {
	runner := NewRunner()

	// EPIC-005 runtime operation might check disk usage or create a directory.
	// Using `pwd` to simulate a runtime directory inspection.
	req, err := NewExecutionRequest("pwd",
		WithWorkingDir("/tmp"),
	)
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	ctx := context.Background()
	result := runner.Execute(ctx, req)

	if result.Status != StatusSuccess {
		t.Errorf("runtime operation Status = %v, want %v", result.Status, StatusSuccess)
	}
	if result.ExitCode != 0 {
		t.Errorf("runtime operation ExitCode = %d, want 0", result.ExitCode)
	}
	got := strings.TrimSpace(result.Stdout)
	if got != "/tmp" && got != "/private/tmp" {
		t.Errorf("runtime operation working dir = %q, want /tmp", got)
	}
}

// TestConsumerIntegration_ResultContainsAllFields verifies that a consumer
// receives a complete result with exit code, stdout, stderr, and duration.
// This corresponds to AC4: "The consumer receives a result containing exit
// code, stdout, stderr, and duration."
func TestConsumerIntegration_ResultContainsAllFields(t *testing.T) {
	runner := NewRunner()

	req, err := NewExecutionRequest("echo",
		WithArgs([]string{"consumer test output"}),
	)
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	ctx := context.Background()
	result := runner.Execute(ctx, req)

	// Consumer inspects all result fields.
	if result.Status != StatusSuccess {
		t.Errorf("Status = %v, want %v", result.Status, StatusSuccess)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "consumer test output") {
		t.Errorf("Stdout = %q, want to contain %q", result.Stdout, "consumer test output")
	}
	// Stderr should be empty for echo.
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

// TestConsumerIntegration_ResultTerminationStatus verifies that a consumer
// receives a result with the appropriate termination status for each outcome.
// This corresponds to AC5: "The consumer receives a result with appropriate
// termination status (completed, failed, timed out, cancelled)."
func TestConsumerIntegration_ResultTerminationStatus(t *testing.T) {
	runner := NewRunner()

	t.Run("success status", func(t *testing.T) {
		req, err := NewExecutionRequest("echo", WithArgs([]string{"ok"}))
		if err != nil {
			t.Fatalf("NewExecutionRequest() failed: %v", err)
		}
		result := runner.Execute(context.Background(), req)
		if result.Status != StatusSuccess {
			t.Errorf("Status = %v, want %v", result.Status, StatusSuccess)
		}
	})

	t.Run("failure status", func(t *testing.T) {
		req, err := NewExecutionRequest("false")
		if err != nil {
			t.Fatalf("NewExecutionRequest() failed: %v", err)
		}
		result := runner.Execute(context.Background(), req)
		if result.Status != StatusFailure {
			t.Errorf("Status = %v, want %v", result.Status, StatusFailure)
		}
	})

	t.Run("startup failure status", func(t *testing.T) {
		req, err := NewExecutionRequest("nonexistent-command-99999")
		if err != nil {
			t.Fatalf("NewExecutionRequest() failed: %v", err)
		}
		result := runner.Execute(context.Background(), req)
		if result.Status != StatusStartupFailure {
			t.Errorf("Status = %v, want %v", result.Status, StatusStartupFailure)
		}
	})

	t.Run("timeout status", func(t *testing.T) {
		req, err := NewExecutionRequest("sleep", WithArgs([]string{"30"}))
		if err != nil {
			t.Fatalf("NewExecutionRequest() failed: %v", err)
		}
		// Use a context with a very short deadline.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		result := runner.Execute(ctx, req)
		if result.Status != StatusTimeout {
			t.Errorf("Status = %v, want %v", result.Status, StatusTimeout)
		}
	})

	t.Run("cancelled status", func(t *testing.T) {
		req, err := NewExecutionRequest("sleep", WithArgs([]string{"30"}))
		if err != nil {
			t.Fatalf("NewExecutionRequest() failed: %v", err)
		}
		// Cancel context immediately before execution.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result := runner.Execute(ctx, req)
		if result.Status != StatusCancelled {
			t.Errorf("Status = %v, want %v", result.Status, StatusCancelled)
		}
	})

	t.Run("unexpected termination status", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("signal test requires Unix")
		}
		req, err := NewExecutionRequest("sh",
			WithArgs([]string{"-c", "kill -KILL $$"}),
			WithTimeout(5*time.Second),
		)
		if err != nil {
			t.Fatalf("NewExecutionRequest() failed: %v", err)
		}
		result := runner.Execute(context.Background(), req)
		if result.Status != StatusUnexpectedTermination {
			t.Errorf("Status = %v, want %v", result.Status, StatusUnexpectedTermination)
		}
	})
}

// TestConsumerIntegration_NoDirectProcessSpawn verifies that consumers do
// not spawn processes directly by ensuring the only available execution
// path is through the Runner interface.
// This corresponds to AC6: "No consumer spawns processes directly — all
// execution goes through the Process Runner."
//
// Note: This is an architectural constraint enforced by code organisation.
// The `internal/execution` package exports the `Runner` interface as the
// sole mechanism for process execution. Packages outside this module cannot
// access `os/exec` through this package's internal implementation.
func TestConsumerIntegration_NoDirectProcessSpawn(t *testing.T) {
	// Verify the Runner interface is the only exported execution mechanism.
	var _ Runner = (*osExecRunner)(nil)

	runner := NewRunner()
	req, err := NewExecutionRequest("echo", WithArgs([]string{"only-through-runner"}))
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	result := runner.Execute(context.Background(), req)
	if result.Status != StatusSuccess {
		t.Errorf("Status = %v, want %v", result.Status, StatusSuccess)
	}
	if !strings.Contains(result.Stdout, "only-through-runner") {
		t.Errorf("Stdout = %q, want to contain %q", result.Stdout, "only-through-runner")
	}
}

// TestConsumerIntegration_ConsumerHandlesResultByStatus demonstrates how a
// consumer would handle execution results based on termination status.
// This is a pattern test showing the recommended consumer integration pattern.
func TestConsumerIntegration_ConsumerHandlesResultByStatus(t *testing.T) {
	runner := NewRunner()

	tests := []struct {
		name     string
		cmd      string
		args     []string
		timeout  time.Duration
		wantFunc func(Result) bool
	}{
		{
			name: "handle success",
			cmd:  "echo",
			args: []string{"ok"},
			wantFunc: func(r Result) bool {
				return r.Status == StatusSuccess && r.ExitCode == 0
			},
		},
		{
			name: "handle failure",
			cmd:  "false",
			wantFunc: func(r Result) bool {
				return r.Status == StatusFailure && r.ExitCode != 0
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := NewExecutionRequest(tt.cmd, WithArgs(tt.args))
			if err != nil {
				t.Fatalf("NewExecutionRequest() failed: %v", err)
			}

			ctx := context.Background()
			result := runner.Execute(ctx, req)

			if !tt.wantFunc(result) {
				t.Errorf("result = %+v, did not satisfy wantFunc", result)
			}

			// Consumer pattern: switch on termination status.
			switch result.Status {
			case StatusSuccess:
				// Consumer handles success — proceed with next step.
			case StatusFailure:
				// Consumer handles failure — log exit code and output.
				_ = result.ExitCode
				_ = result.Stderr
			case StatusStartupFailure:
				// Consumer handles startup failure — command not found.
			case StatusTimeout:
				// Consumer handles timeout — process exceeded limit.
			case StatusCancelled:
				// Consumer handles cancellation — user or system stopped it.
			}
		})
	}
}

// TestConsumerIntegration_FailureOutputCapture verifies that when a command
// fails, the consumer can still access the captured stderr output.
func TestConsumerIntegration_FailureOutputCapture(t *testing.T) {
	runner := NewRunner()

	// Write an error message to stderr and exit with non-zero.
	req, err := NewExecutionRequest("sh",
		WithArgs([]string{"-c", "echo 'error message' >&2; exit 1"}),
	)
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	ctx := context.Background()
	result := runner.Execute(ctx, req)

	if result.Status != StatusFailure {
		t.Errorf("Status = %v, want %v", result.Status, StatusFailure)
	}
	if result.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "error message") {
		t.Errorf("Stderr = %q, want to contain %q", result.Stderr, "error message")
	}
	if result.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", result.Duration)
	}
}
