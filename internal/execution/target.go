package execution

import (
	"fmt"
	"slices"
	"strings"
)

// taskTargetName returns the build target name of a task: the metadata
// target when declared, otherwise the task name (TS-P7-24).
func taskTargetName(task Task) string {
	if task.Metadata != nil && task.Metadata.Target != "" {
		return task.Metadata.Target
	}
	return task.Name
}

// KnownTargets returns the ordered, de-duplicated build target names
// declared by the pipeline definition (TS-P7-24). For each task the
// metadata target is used when declared; otherwise the task name is the
// fallback target. Tasks without target metadata therefore participate
// in --target selection by their task name.
//
// Reference: TS-P7-24 AC-3
func KnownTargets(def *PipelineDefinition) []string {
	seen := make(map[string]bool)
	var targets []string
	for _, stage := range def.Pipeline.Stages {
		for _, task := range stage.Tasks {
			name := taskTargetName(task)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			targets = append(targets, name)
		}
	}
	return targets
}

// ValidateTargets verifies that every requested target is known to the
// pipeline definition. It returns an error listing the unknown targets
// and the known ones, e.g.:
//
//	unknown target "xyz"; known targets: web, apk, ios
//
// A nil or empty request is valid (no filtering). The CLI calls this
// before execution so invalid targets fail before any task runs.
//
// Reference: TS-P7-24 AC-3
func ValidateTargets(def *PipelineDefinition, targets []string) error {
	if len(targets) == 0 {
		return nil
	}

	known := KnownTargets(def)
	knownSet := make(map[string]bool, len(known))
	for _, k := range known {
		knownSet[k] = true
	}

	var unknown []string
	for _, t := range targets {
		if !knownSet[t] {
			unknown = append(unknown, t)
		}
	}
	if len(unknown) == 0 {
		return nil
	}

	quoted := make([]string, len(unknown))
	for i, u := range unknown {
		quoted[i] = fmt.Sprintf("%q", u)
	}
	noun := "target"
	if len(unknown) > 1 {
		noun = "targets"
	}
	return fmt.Errorf("unknown %s %s; known targets: %s", noun, strings.Join(quoted, ", "), strings.Join(known, ", "))
}

// taskMode describes how a task participates in the current run after
// target selection and platform filtering (ADR-018).
type taskMode int

const (
	// taskRun executes the task normally.
	taskRun taskMode = iota

	// taskExclude drops the task from the run and the report entirely:
	// it was not requested by --target selection (TS-P7-24).
	taskExclude

	// taskSkip records the task as skipped with a reason: it is
	// unsupported on the current platform and graceful degradation
	// applies (ADR-018, TS-P7-23).
	taskSkip

	// taskStrictFail records the task as failed: it is unsupported on
	// the current platform and strict mode is enabled (ADR-018,
	// TS-P7-24).
	taskStrictFail
)

// planTask applies target selection (--target) and platform filtering
// (ADR-018) to a single task and decides how it participates in the
// run:
//
//   - taskRun: the task executes normally.
//   - taskExclude: the task is not requested by --target selection; it
//     is dropped from the run and the report.
//   - taskSkip: the task is unsupported on the current platform; the
//     returned TaskResult records a "skipped" status with the reason.
//   - taskStrictFail: the task is unsupported on the current platform in
//     strict mode; the returned TaskResult records a "failure" status
//     with a clear error.
//
// Reference: TS-P7-23, TS-P7-24, ADR-018
func (e *PipelineEngine) planTask(cfg runConfig, task Task) (taskMode, TaskResult) {
	if len(cfg.targets) > 0 && !slices.Contains(cfg.targets, taskTargetName(task)) {
		return taskExclude, TaskResult{}
	}

	if reason := e.platformSkipReason(task); reason != "" {
		if cfg.strict {
			return taskStrictFail, TaskResult{
				Name:   task.Name,
				Status: "failure",
				Error:  reason + " (strict mode)",
			}
		}
		return taskSkip, TaskResult{
			Name:       task.Name,
			Status:     "skipped",
			SkipReason: reason,
		}
	}

	return taskRun, TaskResult{}
}

// platformSkipReason returns a human-readable reason why the task cannot
// run on the current platform, or "" when the task is supported (or
// declares no platform restriction).
//
// The reason identifies the target (metadata target when declared,
// otherwise the task name), the current platform, and the supported
// platforms, e.g.:
//
//	target "ios" is not supported on platform "linux" (supported platforms: darwin)
//
// Reference: TS-P7-23, ADR-018
func (e *PipelineEngine) platformSkipReason(task Task) string {
	md := task.Metadata
	if md == nil || len(md.Platforms) == 0 {
		return ""
	}

	current := e.platformDetector()
	if slices.Contains(md.Platforms, current) {
		return ""
	}

	supported := strings.Join(md.Platforms, ", ")
	if md.Target != "" {
		return fmt.Sprintf("target %q is not supported on platform %q (supported platforms: %s)",
			md.Target, current, supported)
	}
	return fmt.Sprintf("task %q is not supported on platform %q (supported platforms: %s)",
		task.Name, current, supported)
}
