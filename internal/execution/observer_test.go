package execution

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestObserver_StatusQueryableDuringExecution verifies that execution status
// can be queried while the execution is in progress (AC1).
//
// Reference: TS-P6-06 AC1
func TestObserver_StatusQueryableDuringExecution(t *testing.T) {
	observer := NewMutableObserver(NewRunner())

	req, err := NewExecutionRequest("sleep", WithArgs([]string{"0.3"}))
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	ctx := context.Background()
	id, err := observer.Start(ctx, req)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// Verify ID is non-empty.
	if id == "" {
		t.Fatal("Start() returned empty execution ID")
	}

	// Query status during execution.
	var observedStages []Stage
	for i := 0; i < 20; i++ {
		stage, err := observer.GetStatus(id)
		if err != nil {
			t.Fatalf("GetStatus() failed: %v", err)
		}
		observedStages = append(observedStages, stage)

		// Check if we can observe Running.
		if stage == StageRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for completion.
	result, err := observer.GetResult(id)
	if err != nil {
		t.Fatalf("GetResult() failed: %v", err)
	}
	if result.Status != StatusSuccess {
		t.Errorf("Result.Status = %v, want %v", result.Status, StatusSuccess)
	}

	// We should have observed at least some stages.
	if len(observedStages) == 0 {
		t.Error("no stages observed during execution")
	}
}

// TestObserver_ResultRetrievableAfterCompletion verifies that the execution
// result can be retrieved after the execution completes (AC2).
//
// Reference: TS-P6-06 AC2
func TestObserver_ResultRetrievableAfterCompletion(t *testing.T) {
	observer := NewMutableObserver(NewRunner())

	req, err := NewExecutionRequest("echo", WithArgs([]string{"hello from observer"}))
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	ctx := context.Background()
	id, err := observer.Start(ctx, req)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// Retrieve result (blocks until complete).
	result, err := observer.GetResult(id)
	if err != nil {
		t.Fatalf("GetResult() failed: %v", err)
	}

	// Verify result contains expected data.
	if result.Status != StatusSuccess {
		t.Errorf("Result.Status = %v, want %v", result.Status, StatusSuccess)
	}
	if result.ExitCode != 0 {
		t.Errorf("Result.ExitCode = %d, want 0", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "hello from observer") {
		t.Errorf("Result.Stdout = %q, want to contain %q", result.Stdout, "hello from observer")
	}
	if result.Duration <= 0 {
		t.Errorf("Result.Duration = %v, want > 0", result.Duration)
	}
}

// TestObserver_StatusReturnsCorrectStage verifies that status queries return
// the correct lifecycle stage at each point (AC3).
//
// Reference: TS-P6-06 AC3
func TestObserver_StatusReturnsCorrectStage(t *testing.T) {
	observer := NewMutableObserver(NewRunner())

	req, err := NewExecutionRequest("sleep", WithArgs([]string{"0.1"}))
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	ctx := context.Background()
	id, err := observer.Start(ctx, req)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// Immediately after Start, stage should be Requested or Prepared or Running.
	initialStage, err := observer.GetStatus(id)
	if err != nil {
		t.Fatalf("GetStatus() failed: %v", err)
	}
	if initialStage < StageRequested || initialStage > StageRunning {
		t.Errorf("initial stage = %v, want between Requested and Running", initialStage)
	}

	// Wait for completion.
	_, err = observer.GetResult(id)
	if err != nil {
		t.Fatalf("GetResult() failed: %v", err)
	}

	// After completion, stage must be ResultAvailable.
	finalStage, err := observer.GetStatus(id)
	if err != nil {
		t.Fatalf("GetStatus() failed: %v", err)
	}
	if finalStage != StageResultAvailable {
		t.Errorf("final stage = %v, want %v", finalStage, StageResultAvailable)
	}
}

// TestObserver_ResultRetrievalReturnsExitCodeOutputDuration verifies that
// the retrieved result contains exit code, output, and duration (AC4).
//
// Reference: TS-P6-06 AC4
func TestObserver_ResultRetrievalReturnsExitCodeOutputDuration(t *testing.T) {
	observer := NewMutableObserver(NewRunner())

	req, err := NewExecutionRequest("sh",
		WithArgs([]string{"-c", "echo 'standard output'; echo 'error output' >&2; exit 7"}),
	)
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	ctx := context.Background()
	id, err := observer.Start(ctx, req)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	result, err := observer.GetResult(id)
	if err != nil {
		t.Fatalf("GetResult() failed: %v", err)
	}

	// Verify exit code.
	if result.ExitCode != 7 {
		t.Errorf("ExitCode = %d, want 7", result.ExitCode)
	}

	// Verify stdout.
	if !strings.Contains(result.Stdout, "standard output") {
		t.Errorf("Stdout = %q, want to contain %q", result.Stdout, "standard output")
	}

	// Verify stderr.
	if !strings.Contains(result.Stderr, "error output") {
		t.Errorf("Stderr = %q, want to contain %q", result.Stderr, "error output")
	}

	// Verify duration.
	if result.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", result.Duration)
	}
}

// TestObserver_InterfaceUsableByProgrammaticConsumers verifies that the
// Observer interface can be used by programmatic consumers to track
// execution progress (AC5).
//
// Reference: TS-P6-06 AC5
func TestObserver_InterfaceUsableByProgrammaticConsumers(t *testing.T) {
	// This test demonstrates the programmatic consumer pattern:
	// a consumer starts an execution, polls for completion, and
	// retrieves the result.
	observer := NewMutableObserver(NewRunner())

	req, err := NewExecutionRequest("echo", WithArgs([]string{"consumer ready"}))
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	ctx := context.Background()
	id, err := observer.Start(ctx, req)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// Programmatic consumer polls for completion.
	for {
		if observer.IsComplete(id) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Verify execution is no longer running.
	if observer.IsRunning(id) {
		t.Error("IsRunning() = true after completion, want false")
	}

	// Retrieve the result.
	result, err := observer.GetResult(id)
	if err != nil {
		t.Fatalf("GetResult() failed: %v", err)
	}

	if result.Status != StatusSuccess {
		t.Errorf("Result.Status = %v, want %v", result.Status, StatusSuccess)
	}
	if !strings.Contains(result.Stdout, "consumer ready") {
		t.Errorf("Result.Stdout = %q, want to contain %q", result.Stdout, "consumer ready")
	}
}

// TestObserver_MultipleExecutions verifies that MutableObserver can track
// multiple concurrent executions with distinct IDs.
func TestObserver_MultipleExecutions(t *testing.T) {
	observer := NewMutableObserver(NewRunner())

	// Start multiple executions.
	id1, err := observer.Start(context.Background(), mustCreateRequest(t, "echo", "execution-one"))
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	id2, err := observer.Start(context.Background(), mustCreateRequest(t, "echo", "execution-two"))
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// IDs must be distinct.
	if id1 == id2 {
		t.Errorf("execution IDs are identical: %s == %s", id1, id2)
	}

	// Retrieve results for both.
	result1, err := observer.GetResult(id1)
	if err != nil {
		t.Fatalf("GetResult(%s) failed: %v", id1, err)
	}
	result2, err := observer.GetResult(id2)
	if err != nil {
		t.Fatalf("GetResult(%s) failed: %v", id2, err)
	}

	// Verify results are correct and independent.
	if result1.Status != StatusSuccess {
		t.Errorf("Result1.Status = %v, want %v", result1.Status, StatusSuccess)
	}
	if result2.Status != StatusSuccess {
		t.Errorf("Result2.Status = %v, want %v", result2.Status, StatusSuccess)
	}
	if !strings.Contains(result1.Stdout, "execution-one") {
		t.Errorf("Result1.Stdout = %q, want to contain 'execution-one'", result1.Stdout)
	}
	if !strings.Contains(result2.Stdout, "execution-two") {
		t.Errorf("Result2.Stdout = %q, want to contain 'execution-two'", result2.Stdout)
	}
}

// TestObserver_UnknownID verifies that querying an unknown execution ID
// returns appropriate errors/defaults.
func TestObserver_UnknownID(t *testing.T) {
	observer := NewMutableObserver(NewRunner())
	unknownID := ExecutionID("nonexistent-id-12345")

	// GetStatus should return error.
	_, err := observer.GetStatus(unknownID)
	if err == nil {
		t.Error("GetStatus() for unknown ID: expected error, got nil")
	}

	// GetResult should return error.
	_, err = observer.GetResult(unknownID)
	if err == nil {
		t.Error("GetResult() for unknown ID: expected error, got nil")
	}

	// IsRunning should return false.
	if observer.IsRunning(unknownID) {
		t.Error("IsRunning() for unknown ID: expected false, got true")
	}

	// IsComplete should return false.
	if observer.IsComplete(unknownID) {
		t.Error("IsComplete() for unknown ID: expected false, got true")
	}
}

// TestObserver_ConcurrentAccess verifies that MutableObserver is safe for
// concurrent access from multiple goroutines.
func TestObserver_ConcurrentAccess(t *testing.T) {
	observer := NewMutableObserver(NewRunner())

	var wg sync.WaitGroup
	numExecutions := 5

	// Start multiple concurrent executions.
	ids := make([]ExecutionID, numExecutions)
	for i := 0; i < numExecutions; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id, err := observer.Start(
				context.Background(),
				mustCreateRequest(t, "echo", "concurrent-test"),
			)
			if err != nil {
				t.Errorf("Start() failed: %v", err)
				return
			}
			ids[idx] = id
		}(i)
	}
	wg.Wait()

	// All IDs should be non-empty.
	for i, id := range ids {
		if id == "" {
			t.Errorf("ids[%d] is empty", i)
		}
	}

	// Retrieve results concurrently.
	var resultWg sync.WaitGroup
	for _, id := range ids {
		if id == "" {
			continue
		}
		resultWg.Add(1)
		go func(eid ExecutionID) {
			defer resultWg.Done()
			result, err := observer.GetResult(eid)
			if err != nil {
				t.Errorf("GetResult(%s) failed: %v", eid, err)
				return
			}
			if result.Status != StatusSuccess {
				t.Errorf("Result.Status = %v, want %v", result.Status, StatusSuccess)
			}
		}(id)
	}
	resultWg.Wait()
}

// TestObserver_StartWithInvalidRequest verifies that Start returns an error
// for invalid execution requests.
func TestObserver_StartWithInvalidRequest(t *testing.T) {
	observer := NewMutableObserver(NewRunner())

	// Empty command should fail validation.
	invalidReq := ExecutionRequest{
		Command: "",
		Timeout: DefaultTimeout,
	}

	_, err := observer.Start(context.Background(), invalidReq)
	if err == nil {
		t.Error("Start() with empty command: expected error, got nil")
	}
}

// mustCreateRequest is a test helper that creates a valid ExecutionRequest
// or fails the test.
func mustCreateRequest(t *testing.T, cmd string, args ...string) ExecutionRequest {
	t.Helper()

	var opts []RequestOption
	if len(args) > 0 {
		opts = append(opts, WithArgs(args))
	}

	req, err := NewExecutionRequest(cmd, opts...)
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}
	return req
}
