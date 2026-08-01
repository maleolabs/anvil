package execution

import (
	"errors"
	"fmt"
)

// PipelineDefinition is the top-level YAML structure for pipeline files.
type PipelineDefinition struct {
	Pipeline Pipeline `yaml:"pipeline"`
}

// Pipeline represents a complete workflow with one or more PipelineStages.
type Pipeline struct {
	Name   string            `yaml:"name"`
	Stages []PipelineStage   `yaml:"stages"`
	Env    map[string]string `yaml:"env,omitempty"`
}

// PipelineStage groups Tasks that share an execution context or dependency boundary.
//
// Note: Named PipelineStage to avoid conflict with the lifecycle Stage type
// defined in lifecycle.go.
type PipelineStage struct {
	Name     string `yaml:"name"`
	Parallel bool   `yaml:"parallel,omitempty"`
	Tasks    []Task `yaml:"tasks"`
}

// Task is the atomic unit of execution.
type Task struct {
	Name       string            `yaml:"name"`
	Command    string            `yaml:"command"`
	Args       []string          `yaml:"args,omitempty"`
	WorkingDir string            `yaml:"working_dir,omitempty"`
	Env        map[string]string `yaml:"env,omitempty"`
	Timeout    string            `yaml:"timeout,omitempty"` // duration string like "30s"

	// Environments holds environment-aware overrides keyed by environment name
	// (e.g., "development", "production"). When set, values override/replace
	// the base fields for that specific environment.
	Environments map[string]TaskOverride `yaml:"environments,omitempty"`
}

// TaskOverride allows environment-specific overrides for a Task.
type TaskOverride struct {
	Command    string            `yaml:"command,omitempty"`
	Args       []string          `yaml:"args,omitempty"`
	WorkingDir string            `yaml:"working_dir,omitempty"`
	Env        map[string]string `yaml:"env,omitempty"`
	Timeout    string            `yaml:"timeout,omitempty"`
}

// Validate checks that the PipelineDefinition has all required fields filled in.
//
// It validates:
//   - Pipeline name must be non-empty
//   - At least one stage must be defined
//   - Each stage must have a non-empty name
//   - Each stage must have at least one task
//   - Each task must have a non-empty name and command
//
// When multiple fields are invalid, all errors are collected and returned
// together via errors.Join.
func (pd PipelineDefinition) Validate() error {
	var errs []error

	if pd.Pipeline.Name == "" {
		errs = append(errs, &ValidationError{
			Message: "pipeline name is required",
		})
	}

	if len(pd.Pipeline.Stages) == 0 {
		errs = append(errs, &ValidationError{
			Message: "pipeline must have at least one stage",
		})
	}

	for i, stage := range pd.Pipeline.Stages {
		if stage.Name == "" {
			errs = append(errs, &ValidationError{
				Message: fmt.Sprintf("stage %d: name is required", i),
			})
		}
		if len(stage.Tasks) == 0 {
			name := stage.Name
			if name == "" {
				name = fmt.Sprintf("%d", i)
			}
			errs = append(errs, &ValidationError{
				Message: fmt.Sprintf("stage %q: must have at least one task", name),
			})
		}
		for j, task := range stage.Tasks {
			if task.Name == "" {
				errs = append(errs, &ValidationError{
					Message: fmt.Sprintf("stage %q task %d: name is required", stage.Name, j),
				})
			}
			if task.Command == "" {
				errs = append(errs, &ValidationError{
					Message: fmt.Sprintf("stage %q task %q: command is required", stage.Name, task.Name),
				})
			}
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// DefaultBuildPipeline returns a PipelineDefinition with an empty stages list
// (name: "build").
func DefaultBuildPipeline() PipelineDefinition {
	return PipelineDefinition{
		Pipeline: Pipeline{
			Name:   "build",
			Stages: []PipelineStage{},
		},
	}
}

// DefaultCIPipeline returns a PipelineDefinition with:
//   - name: "ci"
//   - Stage 1: "build" — runs the build command (single task)
//   - Stage 2: "test" — three placeholder tasks: "unit-tests", "static-analysis", "linting"
func DefaultCIPipeline() PipelineDefinition {
	return PipelineDefinition{
		Pipeline: Pipeline{
			Name: "ci",
			Stages: []PipelineStage{
				{
					Name: "build",
					Tasks: []Task{
						{
							Name:    "build",
							Command: "echo",
							Args:    []string{"building..."},
						},
					},
				},
				{
					Name: "test",
					Tasks: []Task{
						{
							Name:    "unit-tests",
							Command: "echo",
							Args:    []string{"running unit tests..."},
						},
						{
							Name:    "static-analysis",
							Command: "echo",
							Args:    []string{"running static analysis..."},
						},
						{
							Name:    "linting",
							Command: "echo",
							Args:    []string{"running linter..."},
						},
					},
				},
			},
		},
	}
}

// applyOverrides overlays environment-specific overrides from Environments[env]
// onto the base Task. If env is empty or no override exists, the task is
// returned unchanged.
func applyOverrides(task Task, env string) Task {
	if env == "" {
		return task
	}
	override, ok := task.Environments[env]
	if !ok {
		return task
	}
	if override.Command != "" {
		task.Command = override.Command
	}
	if override.Args != nil {
		task.Args = override.Args
	}
	if override.WorkingDir != "" {
		task.WorkingDir = override.WorkingDir
	}
	if override.Env != nil {
		task.Env = override.Env
	}
	if override.Timeout != "" {
		task.Timeout = override.Timeout
	}
	return task
}

// envMapToSlice converts a map[string]string environment to a []string in
// "KEY=VALUE" format. Returns nil if the input map is nil.
func envMapToSlice(env map[string]string) []string {
	if env == nil {
		return nil
	}
	result := make([]string, 0, len(env))
	for k, v := range env {
		result = append(result, k+"="+v)
	}
	return result
}

// mergeEnvMaps combines pipeline-level and task-level environment variables
// into a single map. Task-level values take precedence when both maps contain
// the same key. Returns nil if both inputs are nil.
func mergeEnvMaps(pipelineEnv, taskEnv map[string]string) map[string]string {
	if pipelineEnv == nil && taskEnv == nil {
		return nil
	}
	if pipelineEnv == nil {
		return taskEnv
	}
	if taskEnv == nil {
		return pipelineEnv
	}

	merged := make(map[string]string, len(pipelineEnv)+len(taskEnv))
	for k, v := range pipelineEnv {
		merged[k] = v
	}
	for k, v := range taskEnv {
		merged[k] = v
	}
	return merged
}
