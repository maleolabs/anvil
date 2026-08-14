package execution

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestLifecycle_StageDefinitions verifies that all five lifecycle stages are
// defined with distinct values and human-readable names.
// Reference: TS-P6-03 AC1
func TestLifecycle_StageDefinitions(t *testing.T) {
	stages := []struct {
		stage Stage
		name  string
	}{
		{StageRequested, "requested"},
		{StagePrepared, "prepared"},
		{StageRunning, "running"},
		{StageCompleted, "completed"},
		{StageResultAvailable, "result available"},
	}

	seen := make(map[Stage]bool)
	for _, s := range stages {
		if seen[s.stage] {
			t.Errorf("duplicate stage value: %v", s.stage)
		}
		seen[s.stage] = true

		if got := s.stage.String(); got != s.name {
			t.Errorf("Stage(%d).String() = %q, want %q", s.stage, got, s.name)
		}
	}
}

// TestLifecycle_NewLifecycleRunner verifies that NewLifecycleRunner returns
// a non-nil Lifecycle with the initial stage set to StageRequested.
func TestLifecycle_NewLifecycleRunner(t *testing.T) {
	runner := NewRunner()
	lc := NewLifecycleRunner(runner)

	if lc == nil {
		t.Fatal("NewLifecycleRunner() returned nil")
	}
	if lc.Stage() != StageRequested {
		t.Errorf("initial Stage() = %v, want %v", lc.Stage(), StageRequested)
	}
}

// TestLifecycle_Execute_Success verifies that a successful execution
// progresses through all lifecycle stages in the correct order.
// Reference: TS-P6-03 AC1, AC3
func TestLifecycle_Execute_Success(t *testing.T) {
	lc := NewLifecycleRunner(NewRunner())

	req, err := NewExecutionRequest("echo", WithArgs([]string{"hello"}))
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	// Before Execute: stage must be Requested.
	if got := lc.Stage(); got != StageRequested {
		t.Errorf("before Execute: Stage() = %v, want %v", got, StageRequested)
	}

	result := lc.Execute(context.Background(), req)

	// After successful execution: stage must be ResultAvailable.
	if got := lc.Stage(); got != StageResultAvailable {
		t.Errorf("after Execute: Stage() = %v, want %v", got, StageResultAvailable)
	}

	// Verify result is well-formed.
	if result.Status != StatusSuccess {
		t.Errorf("Result.Status = %v, want %v", result.Status, StatusSuccess)
	}
	if result.ExitCode != 0 {
		t.Errorf("Result.ExitCode = %d, want 0", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "hello") {
		t.Errorf("Result.Stdout = %q, want to contain %q", result.Stdout, "hello")
	}
	if result.Duration <= 0 {
		t.Errorf("Result.Duration = %v, want > 0", result.Duration)
	}
}

// TestLifecycle_Execute_Failure verifies that a failing execution still
// progresses through the correct lifecycle stages.
// Reference: TS-P6-03 AC1
func TestLifecycle_Execute_Failure(t *testing.T) {
	lc := NewLifecycleRunner(NewRunner())

	req, err := NewExecutionRequest("false")
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	result := lc.Execute(context.Background(), req)

	if got := lc.Stage(); got != StageResultAvailable {
		t.Errorf("after Execute: Stage() = %v, want %v", got, StageResultAvailable)
	}
	if result.Status != StatusFailure {
		t.Errorf("Result.Status = %v, want %v", result.Status, StatusFailure)
	}
	if result.ExitCode != 1 {
		t.Errorf("Result.ExitCode = %d, want 1", result.ExitCode)
	}
}

// TestLifecycle_Execute_StartupFailure verifies lifecycle progression on
// startup failure (non-existent command).
// Reference: TS-P6-03 AC1
func TestLifecycle_Execute_StartupFailure(t *testing.T) {
	lc := NewLifecycleRunner(NewRunner())

	req, err := NewExecutionRequest("nonexistent-command-test-54321")
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	result := lc.Execute(context.Background(), req)

	if got := lc.Stage(); got != StageResultAvailable {
		t.Errorf("after Execute: Stage() = %v, want %v", got, StageResultAvailable)
	}
	if result.Status != StatusStartupFailure {
		t.Errorf("Result.Status = %v, want %v", result.Status, StatusStartupFailure)
	}
	if result.ExitCode != -1 {
		t.Errorf("Result.ExitCode = %d, want -1", result.ExitCode)
	}
	if result.Err == nil {
		t.Error("Result.Err should be non-nil for startup failure")
	}
}

// TestLifecycle_Execute_Timeout verifies lifecycle progression on timeout.
// Reference: TS-P6-03 AC1
func TestLifecycle_Execute_Timeout(t *testing.T) {
	lc := NewLifecycleRunner(NewRunner())

	req, err := NewExecutionRequest("sleep", WithArgs([]string{"30"}))
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	result := lc.Execute(ctx, req)

	if got := lc.Stage(); got != StageResultAvailable {
		t.Errorf("after Execute: Stage() = %v, want %v", got, StageResultAvailable)
	}
	if result.Status != StatusTimeout {
		t.Errorf("Result.Status = %v, want %v", result.Status, StatusTimeout)
	}
}

// TestLifecycle_Execute_Cancellation verifies lifecycle progression on
// cancellation.
// Reference: TS-P6-03 AC1
func TestLifecycle_Execute_Cancellation(t *testing.T) {
	lc := NewLifecycleRunner(NewRunner())

	req, err := NewExecutionRequest("sleep", WithArgs([]string{"30"}))
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	// Cancel context immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := lc.Execute(ctx, req)

	if got := lc.Stage(); got != StageResultAvailable {
		t.Errorf("after Execute: Stage() = %v, want %v", got, StageResultAvailable)
	}
	if result.Status != StatusCancelled {
		t.Errorf("Result.Status = %v, want %v", result.Status, StatusCancelled)
	}
}

// TestLifecycle_StageConcurrentAccess verifies that Stage() is safe for
// concurrent access during execution.
// Reference: TS-P6-03 AC2
func TestLifecycle_StageConcurrentAccess(t *testing.T) {
	lc := NewLifecycleRunner(NewRunner())

	req, err := NewExecutionRequest("sleep", WithArgs([]string{"0.1"}))
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)

	// Execute in a goroutine so we can query stage concurrently.
	go func() {
		defer wg.Done()
		lc.Execute(context.Background(), req)
	}()

	// Query stage repeatedly during execution — must not race.
	for i := 0; i < 50; i++ {
		stage := lc.Stage()
		// Stage must be a valid value.
		if stage < StageRequested || stage > StageResultAvailable {
			t.Errorf("invalid stage value: %v", stage)
		}
		time.Sleep(time.Millisecond)
	}

	wg.Wait()

	// After completion, stage must be ResultAvailable.
	if got := lc.Stage(); got != StageResultAvailable {
		t.Errorf("final Stage() = %v, want %v", got, StageResultAvailable)
	}
}

// TestLifecycle_StageProgressionDuringExecution verifies that stage
// transitions are observable during execution. It confirms that the
// Running stage is visible while the process is executing.
// Reference: TS-P6-03 AC2, AC3
func TestLifecycle_StageProgressionDuringExecution(t *testing.T) {
	lc := NewLifecycleRunner(NewRunner())

	req, err := NewExecutionRequest("sleep", WithArgs([]string{"0.2"}))
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	stageCh := make(chan Stage, 10)

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		// Record stage before starting.
		stageCh <- lc.Stage()

		lc.Execute(context.Background(), req)

		// Record stage after completion.
		stageCh <- lc.Stage()
	}()

	// Give the goroutine time to start and observe initial stages.
	time.Sleep(50 * time.Millisecond)

	// Query stage during execution — should observe Running.
	observedRunning := false
	for i := 0; i < 10; i++ {
		if lc.Stage() == StageRunning {
			observedRunning = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	wg.Wait()
	close(stageCh)

	if !observedRunning {
		t.Error("did not observe StageRunning during execution")
	}

	// Collect recorded stages.
	var recorded []Stage
	for s := range stageCh {
		recorded = append(recorded, s)
	}

	// Verify initial stage was Requested.
	if len(recorded) > 0 && recorded[0] != StageRequested {
		t.Errorf("initial recorded stage = %v, want %v", recorded[0], StageRequested)
	}

	// Verify final stage was ResultAvailable.
	if len(recorded) > 1 && recorded[len(recorded)-1] != StageResultAvailable {
		t.Errorf("final recorded stage = %v, want %v", recorded[len(recorded)-1], StageResultAvailable)
	}
}

// TestLifecycle_Execute_InvalidRequest verifies lifecycle progression when
// the execution request is invalid (empty command).
func TestLifecycle_Execute_InvalidRequest(t *testing.T) {
	lc := NewLifecycleRunner(NewRunner())

	// Create an invalid request with empty command.
	_, err := NewExecutionRequest("")
	if err == nil {
		t.Fatal("expected error for empty command, got nil")
	}

	// Even though NewExecutionRequest failed, we can still test with a
	// manually constructed invalid request.
	invalidReq := ExecutionRequest{
		Command: "",
		Timeout: DefaultTimeout,
	}

	result := lc.Execute(context.Background(), invalidReq)

	if got := lc.Stage(); got != StageResultAvailable {
		t.Errorf("after Execute: Stage() = %v, want %v", got, StageResultAvailable)
	}
	if result.Status != StatusStartupFailure {
		t.Errorf("Result.Status = %v, want %v", result.Status, StatusStartupFailure)
	}
	if result.Err == nil {
		t.Error("Result.Err should be non-nil for invalid request")
	}
}

// TestLifecycle_Result verifies that Result() returns the correct result
// and can be called multiple times.
func TestLifecycle_Result(t *testing.T) {
	lc := NewLifecycleRunner(NewRunner())

	req, err := NewExecutionRequest("echo", WithArgs([]string{"result-test"}))
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	// Execute synchronously.
	_ = lc.Execute(context.Background(), req)

	// First call to Result() should return immediately with the result.
	r1 := lc.Result()
	if r1.Status != StatusSuccess {
		t.Errorf("first Result().Status = %v, want %v", r1.Status, StatusSuccess)
	}
	if !strings.Contains(r1.Stdout, "result-test") {
		t.Errorf("first Result().Stdout = %q, want to contain %q", r1.Stdout, "result-test")
	}

	// Second call should return the same result without blocking.
	r2 := lc.Result()
	if r2.Status != r1.Status {
		t.Errorf("second Result().Status = %v, want %v", r2.Status, r1.Status)
	}
	if r2.Stdout != r1.Stdout {
		t.Errorf("second Result().Stdout = %q, want %q", r2.Stdout, r1.Stdout)
	}
}

// TestLifecycle_IntegrationWithRunner verifies that the lifecycle integrates
// with TS-006-001 (Process Runner) correctly — the Lifecycle wraps a Runner
// and delegates execution to it.
// Reference: TS-P6-03 AC4
func TestLifecycle_IntegrationWithRunner(t *testing.T) {
	// Create a Runner and wrap it in a Lifecycle.
	baseRunner := NewRunner()
	lc := NewLifecycleRunner(baseRunner)

	req, err := NewExecutionRequest("echo", WithArgs([]string{"integration-test"}))
	if err != nil {
		t.Fatalf("NewExecutionRequest() failed: %v", err)
	}

	// Execute through the lifecycle.
	result := lc.Execute(context.Background(), req)

	// Verify the result matches what the underlying Runner would produce.
	if result.Status != StatusSuccess {
		t.Errorf("Result.Status = %v, want %v", result.Status, StatusSuccess)
	}
	if result.ExitCode != 0 {
		t.Errorf("Result.ExitCode = %d, want 0", result.ExitCode)
	}
	if strings.TrimSpace(result.Stdout) != "integration-test" {
		t.Errorf("Result.Stdout = %q, want %q", strings.TrimSpace(result.Stdout), "integration-test")
	}

	// Verify lifecycle stage reached ResultAvailable.
	if got := lc.Stage(); got != StageResultAvailable {
		t.Errorf("final Stage() = %v, want %v", got, StageResultAvailable)
	}
}

// TestLifecycle_StageString_Unknown verifies that an undefined stage value
// returns "unknown".
func TestLifecycle_StageString_Unknown(t *testing.T) {
	var s Stage = 99
	if got := s.String(); got != "unknown" {
		t.Errorf("Stage(99).String() = %q, want %q", got, "unknown")
	}
}
