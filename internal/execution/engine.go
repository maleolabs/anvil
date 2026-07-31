package execution

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// ExecutionReport captures the result of a full pipeline execution.
type ExecutionReport struct {
	PipelineName string        `json:"pipeline_name"`
	Status       string        `json:"status"` // "success", "failure"
	Stages       []StageResult `json:"stages"`
	Duration     time.Duration `json:"duration"`
}

// StageResult captures the result of a single stage execution.
type StageResult struct {
	Name   string       `json:"name"`
	Status string       `json:"status"` // "success", "failure", "skipped"
	Tasks  []TaskResult `json:"tasks"`
}

// TaskResult captures the result of a single task execution.
type TaskResult struct {
	Name     string        `json:"name"`
	Status   string        `json:"status"`
	ExitCode int           `json:"exit_code,omitempty"`
	Stdout   string        `json:"stdout,omitempty"`
	Stderr   string        `json:"stderr,omitempty"`
	Duration time.Duration `json:"duration"`
	Error    string        `json:"error,omitempty"`
}

// PipelineEngine orchestrates pipeline execution.
//
// It loads pipeline definitions from YAML files and drives execution through
// the configured Runner. Stages execute sequentially; tasks within a stage
// execute sequentially by default or concurrently when PipelineStage.Parallel is true.
//
// Fail-fast semantics: when any task fails, remaining stages are marked as
// "skipped" in the report.
type PipelineEngine struct {
	runner Runner
}

// NewPipelineEngine creates an engine with the given Runner.
func NewPipelineEngine(runner Runner) *PipelineEngine {
	return &PipelineEngine{runner: runner}
}

// Load reads and parses a pipeline YAML file, validating the structure.
//
// It returns an error if the file cannot be read, the YAML cannot be parsed,
// or the pipeline definition fails validation.
func (e *PipelineEngine) Load(path string) (*PipelineDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading pipeline file: %w", err)
	}

	var def PipelineDefinition
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("parsing pipeline YAML: %w", err)
	}

	if err := def.Validate(); err != nil {
		return nil, fmt.Errorf("invalid pipeline definition: %w", err)
	}

	return &def, nil
}

// Execute runs the loaded pipeline with the given context and environment.
//
// The env parameter selects environment-specific overrides (e.g., "development",
// "production"). Empty string means no environment overrides are applied.
//
// Stages execute sequentially in the order declared. Tasks within a stage
// execute sequentially by default, or concurrently when Stage.Parallel is true.
//
// If any task fails, remaining stages are marked as "skipped" (fail-fast).
func (e *PipelineEngine) Execute(ctx context.Context, pipeline *PipelineDefinition, env string) *ExecutionReport {
	start := time.Now()

	report := &ExecutionReport{
		PipelineName: pipeline.Pipeline.Name,
		Status:       "success",
	}

	var failed bool
	var mu sync.Mutex

	for _, stage := range pipeline.Pipeline.Stages {
		stageResult := StageResult{
			Name: stage.Name,
		}

		// Check for pipeline failure from a previous stage.
		mu.Lock()
		pipelineFailed := failed
		mu.Unlock()

		if pipelineFailed {
			stageResult.Status = "skipped"
			for _, task := range stage.Tasks {
				stageResult.Tasks = append(stageResult.Tasks, TaskResult{
					Name:   task.Name,
					Status: "skipped",
				})
			}
			report.Stages = append(report.Stages, stageResult)
			continue
		}

		if stage.Parallel {
			stageResult = e.executeParallelStage(ctx, stage, env)
		} else {
			stageResult = e.executeSequentialStage(ctx, stage, env)
		}

		report.Stages = append(report.Stages, stageResult)

		if stageResult.Status == "failure" {
			mu.Lock()
			failed = true
			mu.Unlock()
		}
	}

	if failed {
		report.Status = "failure"
	}

	report.Duration = time.Since(start)
	return report
}

// executeSequentialStage runs tasks one after another. If a task fails,
// remaining tasks in the stage are marked as "skipped".
func (e *PipelineEngine) executeSequentialStage(ctx context.Context, stage PipelineStage, env string) StageResult {
	result := StageResult{
		Name:   stage.Name,
		Status: "success",
	}

	var failed bool

	for _, task := range stage.Tasks {
		if failed {
			result.Tasks = append(result.Tasks, TaskResult{
				Name:   task.Name,
				Status: "skipped",
			})
			continue
		}

		tr := e.executeTask(ctx, task, env)
		result.Tasks = append(result.Tasks, tr)

		if tr.Status == "failure" {
			failed = true
		}
	}

	if failed {
		result.Status = "failure"
	}

	return result
}

// executeParallelStage runs all tasks concurrently using goroutines.
// After all tasks complete, the stage status is "failure" if any task failed.
func (e *PipelineEngine) executeParallelStage(ctx context.Context, stage PipelineStage, env string) StageResult {
	result := StageResult{
		Name:   stage.Name,
		Status: "success",
	}
	result.Tasks = make([]TaskResult, len(stage.Tasks))

	type indexedResult struct {
		index  int
		result TaskResult
	}

	ch := make(chan indexedResult, len(stage.Tasks))
	var wg sync.WaitGroup

	for i, task := range stage.Tasks {
		wg.Add(1)
		go func(i int, task Task) {
			defer wg.Done()
			tr := e.executeTask(ctx, task, env)
			ch <- indexedResult{index: i, result: tr}
		}(i, task)
	}

	wg.Wait()
	close(ch)

	var anyFailed bool
	for ir := range ch {
		result.Tasks[ir.index] = ir.result
		if ir.result.Status == "failure" {
			anyFailed = true
		}
	}

	if anyFailed {
		result.Status = "failure"
	}

	return result
}

// executeTask applies environment overrides, resolves the timeout, and runs
// the task through the engine's Runner.
func (e *PipelineEngine) executeTask(ctx context.Context, task Task, env string) TaskResult {
	start := time.Now()

	// Apply environment-specific overrides.
	task = applyOverrides(task, env)

	// Parse timeout; use DefaultTimeout if unset or invalid.
	timeout := parseTaskTimeout(task.Timeout)

	// Create a derived context with the task's timeout.
	taskCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Build the execution request.
	req, err := NewExecutionRequest(task.Command,
		WithArgs(task.Args),
		WithWorkingDir(task.WorkingDir),
		WithEnv(envMapToSlice(task.Env)),
		WithTimeout(timeout),
	)
	if err != nil {
		return TaskResult{
			Name:     task.Name,
			Status:   "failure",
			Duration: time.Since(start),
			Error:    err.Error(),
		}
	}

	result := e.runner.Execute(taskCtx, req)

	tr := TaskResult{
		Name:     task.Name,
		Duration: result.Duration,
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		ExitCode: result.ExitCode,
	}

	if result.Success() {
		tr.Status = "success"
	} else {
		tr.Status = "failure"
		if result.Err != nil {
			tr.Error = result.Err.Error()
		}
	}

	return tr
}

// parseTaskTimeout parses a duration string like "30s" and returns the
// corresponding time.Duration. If the string is empty or cannot be parsed,
// DefaultTimeout is returned.
func parseTaskTimeout(s string) time.Duration {
	if s == "" {
		return DefaultTimeout
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return DefaultTimeout
	}
	return d
}
