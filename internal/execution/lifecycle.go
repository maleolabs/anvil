package execution

import (
	"context"
	"sync"
	"time"
)

// Stage represents a stage in the execution lifecycle.
//
// Every execution progresses through Requested → Prepared → Running →
// Completed → Result Available. Stages are sequential and observable.
//
// Reference: TS-P6-03, EPIC-006 §7, ADR-008 §4.5
type Stage int

const (
	// StageRequested indicates the execution has been submitted to the
	// Process Runner but has not yet started.
	StageRequested Stage = iota

	// StagePrepared indicates the execution context has been validated
	// and the process is about to start.
	StagePrepared

	// StageRunning indicates the process has been launched and is executing.
	StageRunning

	// StageCompleted indicates the process has terminated. Exit code and
	// output have been captured.
	StageCompleted

	// StageResultAvailable indicates the result is ready for consumers
	// to retrieve.
	StageResultAvailable
)

// String returns a human-readable name for the lifecycle stage.
func (s Stage) String() string {
	switch s {
	case StageRequested:
		return "requested"
	case StagePrepared:
		return "prepared"
	case StageRunning:
		return "running"
	case StageCompleted:
		return "completed"
	case StageResultAvailable:
		return "result available"
	default:
		return "unknown"
	}
}

// Lifecycle tracks the execution progress of a Process Runner execution
// through its defined lifecycle stages.
//
// It wraps a Runner and provides observability into execution progress.
// The current stage can be queried at any time via Stage(), including
// during execution when observed from a concurrent goroutine.
//
// Each Lifecycle can optionally be associated with an ExecutionID for
// identification in multi-execution scenarios (see MutableObserver).
//
// Usage pattern:
//
//	lifecycle := execution.NewLifecycleRunner(execution.NewRunner())
//	go lifecycle.Execute(ctx, req)
//	// Concurrently:
//	stage := lifecycle.Stage() // e.g., "running"
//	result := lifecycle.Result() // blocks until available
//
// Reference: TS-P6-03, TS-P6-06, EPIC-006 §7, ADR-008 §4.5
type Lifecycle struct {
	mu     sync.Mutex
	stage  Stage
	runner Runner
	id     ExecutionID

	resultCh chan Result
	result   Result
	done     bool
}

// NewLifecycleRunner creates a Lifecycle that wraps the given Runner with
// execution lifecycle stage tracking.
//
// The returned Lifecycle provides both the Execute method (same signature
// as Runner) and the Stage method for querying current execution progress.
func NewLifecycleRunner(runner Runner) *Lifecycle {
	return &Lifecycle{
		runner:   runner,
		stage:    StageRequested,
		resultCh: make(chan Result, 1),
	}
}

// ExecutionID returns the unique identifier for this lifecycle execution.
//
// Returns an empty string if no ID has been assigned.
// The ID is typically set by MutableObserver when starting an execution.
//
// Reference: TS-P6-06
func (l *Lifecycle) ExecutionID() ExecutionID {
	return l.id
}

// setExecutionID assigns a unique identifier to this lifecycle.
// This is intended for use by MutableObserver and similar orchestration
// components that manage multiple executions.
func (l *Lifecycle) setExecutionID(id ExecutionID) {
	l.id = id
}

// Stage returns the current lifecycle stage of the most recent execution.
//
// Stage is safe for concurrent access. It can be called from any goroutine
// to observe execution progress while Execute is running.
func (l *Lifecycle) Stage() Stage {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.stage
}

// setStage atomically updates the lifecycle stage.
func (l *Lifecycle) setStage(s Stage) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stage = s
}

// Result returns the execution result, blocking until it is available.
//
// Result may be called multiple times; subsequent calls return the same
// result without blocking.
func (l *Lifecycle) Result() Result {
	if l.done {
		return l.result
	}
	r := <-l.resultCh
	l.mu.Lock()
	l.result = r
	l.done = true
	l.mu.Unlock()
	return r
}

// Execute launches the process through the wrapped Runner and tracks
// lifecycle stage transitions throughout execution.
//
// Stage transitions:
//   - StageRequested — initial stage when Execute is called
//   - StagePrepared — context validated, process about to start
//   - StageRunning — process has been launched (observable concurrently)
//   - StageCompleted — process terminated, output captured
//   - StageResultAvailable — result ready for retrieval
//
// The method blocks until execution completes, but the lifecycle stage
// can be observed from other goroutines during execution.
//
// Reference: TS-P6-03 AC1, AC2, AC3, AC4
func (l *Lifecycle) Execute(ctx context.Context, req ExecutionRequest) Result {
	start := time.Now()
	l.setStage(StageRequested)

	// Verify request is well-formed before proceeding.
	if err := req.Validate(); err != nil {
		l.setStage(StageCompleted)
		result := Result{
			Status:   StatusStartupFailure,
			ExitCode: -1,
			Duration: time.Since(start),
			Err:      err,
		}
		l.setStage(StageResultAvailable)
		l.resultCh <- result
		return result
	}

	l.setStage(StagePrepared)

	// Check for pre-cancelled or timed-out context before attempting to
	// launch the process.
	if ctx.Err() != nil {
		l.setStage(StageCompleted)
		var result Result
		if isTimeoutError(ctx) {
			result = Result{
				Status:   StatusTimeout,
				ExitCode: -1,
				Duration: time.Since(start),
				Err:      ctx.Err(),
			}
		} else {
			result = Result{
				Status:   StatusCancelled,
				ExitCode: -1,
				Duration: time.Since(start),
				Err:      ctx.Err(),
			}
		}
		l.setStage(StageResultAvailable)
		l.resultCh <- result
		return result
	}

	// Stage is now Running — observable from concurrent goroutines via Stage().
	l.setStage(StageRunning)

	// Delegate to the wrapped Runner for actual process execution.
	// This blocks until the process completes, but Stage() remains queryable.
	result := l.runner.Execute(ctx, req)

	// Process has terminated — output and exit code captured.
	l.setStage(StageCompleted)
	l.setStage(StageResultAvailable)

	l.resultCh <- result
	return result
}
