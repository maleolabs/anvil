package execution

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"maleolabs.com/anvil/internal/output"
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
//
// An optional ProgressReporter receives real-time execution events for
// UI feedback. When nil, no progress events are emitted (backward compatible).
type PipelineEngine struct {
	runner   Runner
	reporter output.ProgressReporter // optional, nil = no-op
}

// EngineOption configures a PipelineEngine.
type EngineOption func(*PipelineEngine)

// WithProgressReporter sets a ProgressReporter on the engine. The reporter
// receives real-time pipeline/stage/task events during Execute().
// Pass nil or omit to disable progress reporting.
func WithProgressReporter(r output.ProgressReporter) EngineOption {
	return func(e *PipelineEngine) {
		e.reporter = r
	}
}

// NewPipelineEngine creates an engine with the given Runner and optional
// configuration. Options are applied in order; later options override earlier
// ones when they set the same field.
func NewPipelineEngine(runner Runner, opts ...EngineOption) *PipelineEngine {
	e := &PipelineEngine{runner: runner}
	for _, opt := range opts {
		opt(e)
	}
	return e
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
// The pipelineEnv parameter specifies pipeline-level environment variables
// injected into every task's environment. Task-level env vars take precedence
// when keys conflict. Pass nil if no pipeline-level environment is needed.
//
// Stages execute sequentially in the order declared. Tasks within a stage
// execute sequentially by default, or concurrently when Stage.Parallel is true.
//
// If any task fails, remaining stages are marked as "skipped" (fail-fast).
//
// When a ProgressReporter is configured, execution events are emitted in
// real-time for UI feedback.
func (e *PipelineEngine) Execute(ctx context.Context, pipeline *PipelineDefinition, env string, pipelineEnv map[string]string) *ExecutionReport {
	start := time.Now()

	report := &ExecutionReport{
		PipelineName: pipeline.Pipeline.Name,
		Status:       "success",
	}

	// Emit pipeline start event.
	if e.reporter != nil {
		e.reporter.PipelineStart(pipeline.Pipeline.Name, env)
	}

	var failed bool
	var mu sync.Mutex
	totalStages := len(pipeline.Pipeline.Stages)

	for stageIdx, stage := range pipeline.Pipeline.Stages {
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
			// Emit skipped events.
			if e.reporter != nil {
				e.reporter.StageSkipped(stage.Name)
				for _, task := range stage.Tasks {
					e.reporter.TaskSkipped(task.Name)
				}
			}
			report.Stages = append(report.Stages, stageResult)
			continue
		}

		// Emit stage start event.
		if e.reporter != nil {
			e.reporter.StageStart(stage.Name, stageIdx, totalStages)
		}

		stageStart := time.Now()

		if stage.Parallel {
			stageResult = e.executeParallelStage(ctx, stage, env, pipelineEnv)
		} else {
			stageResult = e.executeSequentialStage(ctx, stage, env, pipelineEnv)
		}

		stageDuration := time.Since(stageStart)

		// Emit stage completion event.
		if e.reporter != nil {
			if stageResult.Status == "failure" {
				e.reporter.StageFailed(stage.Name, stageDuration)
			} else {
				e.reporter.StageComplete(stage.Name, stageDuration)
			}
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

	// Emit pipeline completion event.
	if e.reporter != nil {
		if failed {
			e.reporter.PipelineFailed(pipeline.Pipeline.Name, report.Duration)
		} else {
			e.reporter.PipelineComplete(pipeline.Pipeline.Name, report.Duration)
		}
	}

	return report
}

// executeSequentialStage runs tasks one after another. If a task fails,
// remaining tasks in the stage are marked as "skipped".
func (e *PipelineEngine) executeSequentialStage(ctx context.Context, stage PipelineStage, env string, pipelineEnv map[string]string) StageResult {
	result := StageResult{
		Name:   stage.Name,
		Status: "success",
	}

	var failed bool
	totalTasks := len(stage.Tasks)

	for taskIdx, task := range stage.Tasks {
		if failed {
			result.Tasks = append(result.Tasks, TaskResult{
				Name:   task.Name,
				Status: "skipped",
			})
			if e.reporter != nil {
				e.reporter.TaskSkipped(task.Name)
			}
			continue
		}

		// Emit task start event.
		if e.reporter != nil {
			e.reporter.TaskStart(task.Name, taskIdx, totalTasks)
		}

		tr := e.executeTask(ctx, task, env, pipelineEnv)
		result.Tasks = append(result.Tasks, tr)

		// Emit task completion event.
		if e.reporter != nil {
			if tr.Status == "failure" {
				e.reporter.TaskFailed(task.Name, tr.Duration, tr.ExitCode, tr.Stderr)
			} else {
				e.reporter.TaskComplete(task.Name, tr.Duration)
			}
		}

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
//
// Task progress events are emitted: TaskStart before launching goroutines,
// TaskComplete/TaskFailed after all tasks finish (order may vary).
func (e *PipelineEngine) executeParallelStage(ctx context.Context, stage PipelineStage, env string, pipelineEnv map[string]string) StageResult {
	result := StageResult{
		Name:   stage.Name,
		Status: "success",
	}
	result.Tasks = make([]TaskResult, len(stage.Tasks))

	type indexedResult struct {
		index  int
		result TaskResult
	}

	// Emit all task start events before launching goroutines.
	totalTasks := len(stage.Tasks)
	if e.reporter != nil {
		for i, task := range stage.Tasks {
			e.reporter.TaskStart(task.Name, i, totalTasks)
		}
	}

	ch := make(chan indexedResult, len(stage.Tasks))
	var wg sync.WaitGroup

	for i, task := range stage.Tasks {
		wg.Add(1)
		go func(i int, task Task) {
			defer wg.Done()
			tr := e.executeTask(ctx, task, env, pipelineEnv)
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

	// Emit task completion events after all tasks finish.
	if e.reporter != nil {
		for _, tr := range result.Tasks {
			if tr.Status == "failure" {
				e.reporter.TaskFailed(tr.Name, tr.Duration, tr.ExitCode, tr.Stderr)
			} else if tr.Status == "skipped" {
				e.reporter.TaskSkipped(tr.Name)
			} else {
				e.reporter.TaskComplete(tr.Name, tr.Duration)
			}
		}
	}

	if anyFailed {
		result.Status = "failure"
	}

	return result
}

// executeTask applies environment overrides, resolves the timeout, and runs
// the task through the engine's Runner.
//
// Pipeline-level env vars (pipelineEnv) are merged with task-level env vars.
// Task-level vars take precedence when keys conflict. When any env vars are
// present, they are merged with the parent process environment so that
// standard variables like PATH are preserved.
//
// Task args undergo template expansion: references like ${VAR_NAME} are
// replaced with values from the merged environment. Unresolved references
// are left as-is.
func (e *PipelineEngine) executeTask(ctx context.Context, task Task, env string, pipelineEnv map[string]string) TaskResult {
	start := time.Now()

	// Apply environment-specific overrides.
	task = applyOverrides(task, env)

	// Merge pipeline-level env into task env. Task-level vars win on conflict.
	mergedEnv := mergeEnvMaps(pipelineEnv, task.Env)

	// Expand template variables in args (${VAR_NAME} → value from mergedEnv).
	task.Args = expandTemplateVars(task.Args, mergedEnv)

	// Parse timeout; use DefaultTimeout if unset or invalid.
	timeout := parseTaskTimeout(task.Timeout)

	// Create a derived context with the task's timeout.
	taskCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Build the execution request. When custom env vars are present, merge
	// them with the parent process environment so standard vars like PATH
	// are preserved. When no custom env vars exist, inherit the parent
	// environment by passing nil.
	var envSlice []string
	if mergedEnv != nil {
		envSlice = buildInheritedEnv(mergedEnv)
	}

	req, err := NewExecutionRequest(task.Command,
		WithArgs(task.Args),
		WithWorkingDir(task.WorkingDir),
		WithEnv(envSlice),
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

// expandTemplateVars replaces ${VAR_NAME} references in args with values from
// the provided environment map. References to variables not present in the map
// are left unchanged. This enables pipeline tasks to reference dynamic values
// like ${ANVIL_OUTPUT_DIR} in their arguments.
//
// The expansion uses simple string replacement, not shell evaluation. Only
// exact ${KEY} patterns are matched — nested or malformed references are
// preserved as-is.
func expandTemplateVars(args []string, env map[string]string) []string {
	if len(args) == 0 || len(env) == 0 {
		return args
	}

	expanded := make([]string, len(args))
	for i, arg := range args {
		for key, val := range env {
			arg = strings.ReplaceAll(arg, "${"+key+"}", val)
		}
		expanded[i] = arg
	}
	return expanded
}

// buildInheritedEnv creates an environment variable slice that inherits the
// current process environment and overlays the provided custom variables.
// Custom variables override inherited ones when keys conflict.
//
// This is necessary because setting cmd.Env to a non-nil slice in os/exec
// completely replaces the parent environment. By merging with os.Environ(),
// standard variables like PATH and HOME are preserved.
func buildInheritedEnv(customVars map[string]string) []string {
	parentEnv := os.Environ()

	// Build a set of keys that will be overridden by custom vars.
	overrideKeys := make(map[string]bool, len(customVars))
	for k := range customVars {
		overrideKeys[k] = true
	}

	// Start with parent env, excluding keys that custom vars will override.
	result := make([]string, 0, len(parentEnv)+len(customVars))
	for _, entry := range parentEnv {
		key := entry[:strings.IndexByte(entry, '=')]
		if !overrideKeys[key] {
			result = append(result, entry)
		}
	}

	// Append custom vars.
	for k, v := range customVars {
		result = append(result, k+"="+v)
	}

	return result
}
