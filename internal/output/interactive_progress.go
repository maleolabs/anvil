// Package output — interactive_progress.go implements ProgressReporter with
// ANSI colors, spinner animation, and tree-style box-drawing output for
// interactive terminal sessions.
//
// Reference: Phase 2 UX feedback mechanism
package output

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// Tree-drawing glyphs.
const (
	treeTee    = "├─" // non-last item connector
	treeCorner = "└─" // last item connector
	treeLine   = "│"  // vertical connector
)

// Status indicators.
const (
	iconSuccess = "✓"
	iconFailure = "✗"
	iconSkip    = "⊘"
	iconRunning = "⠋"
)

// InteractiveProgressReporter renders pipeline progress with ANSI colors,
// spinner animation for running tasks, and tree-style box-drawing characters.
//
// It is designed for interactive terminal sessions. For non-interactive
// contexts (CI, piped output), use PlainProgressReporter instead.
//
// Visual output example:
//
//	▶ Pipeline: build (production)
//
//	  ├─ Stage: dependencies
//	  │  ├─ ✓ download                      (2.1s)
//	  │  └─ ✓ verify                        (0.8s)
//	  │
//	  └─ Stage: compile
//	     └─ ✓ cache-clear                   (1.1s)
//
//	✓ Pipeline completed in 4.0s
type InteractiveProgressReporter struct {
	w       io.Writer
	spinner *Spinner

	mu sync.Mutex

	// Pipeline state
	pipelineName string
	env          string

	// Stage tracking for tree rendering
	stageIdx   int
	stageTotal int
	isLastStage bool

	// Task tracking within current stage
	taskIdx   int
	taskTotal int
}

// NewInteractiveProgressReporter creates a reporter that writes interactive
// tree-style output to w with ANSI colors and spinner animation.
func NewInteractiveProgressReporter(w io.Writer) *InteractiveProgressReporter {
	return &InteractiveProgressReporter{
		w: w,
	}
}

// PipelineStart emits the pipeline header with a play icon.
func (r *InteractiveProgressReporter) PipelineStart(name string, env string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.pipelineName = name
	r.env = env

	if env != "" {
		header := fmt.Sprintf("▶ Pipeline: %s (%s)", name, env)
		fmt.Fprintf(r.w, "\n%s\n\n", Bold(r.w, header))
	} else {
		header := fmt.Sprintf("▶ Pipeline: %s", name)
		fmt.Fprintf(r.w, "\n%s\n\n", Bold(r.w, header))
	}
}

// PipelineComplete emits the success summary with green checkmark.
func (r *InteractiveProgressReporter) PipelineComplete(name string, duration time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	msg := fmt.Sprintf("%s Pipeline completed in %s", iconSuccess, formatDuration(duration))
	fmt.Fprintf(r.w, "\n%s\n\n", Green(r.w, msg))
}

// PipelineFailed emits the failure summary with red cross.
func (r *InteractiveProgressReporter) PipelineFailed(name string, duration time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	msg := fmt.Sprintf("%s Pipeline failed in %s", iconFailure, formatDuration(duration))
	fmt.Fprintf(r.w, "\n%s\n\n", Red(r.w, msg))
}

// StageStart records a new stage and emits the stage header with tree connector.
func (r *InteractiveProgressReporter) StageStart(name string, index int, total int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stageIdx = index
	r.stageTotal = total
	r.isLastStage = index == total-1
	r.taskIdx = 0
	r.taskTotal = 0

	// Emit blank line separator between stages (except the first).
	if index > 0 {
		fmt.Fprintln(r.w)
	}

	connector := stageConnector(index, total)
	stageLabel := fmt.Sprintf("Stage: %s", name)
	fmt.Fprintf(r.w, "  %s %s\n", connector, Bold(r.w, stageLabel))
}

// StageComplete is a no-op for the interactive reporter — tasks already
// show their individual status. The tree structure is self-documenting.
func (r *InteractiveProgressReporter) StageComplete(name string, duration time.Duration) {
	// No additional output needed — task lines already show status.
}

// StageFailed is a no-op for the interactive reporter — the task failure
// line already shows the failure indicator.
func (r *InteractiveProgressReporter) StageFailed(name string, duration time.Duration) {
	// No additional output needed — the failed task line shows ✗.
}

// StageSkipped emits a skipped stage line with tree connector.
func (r *InteractiveProgressReporter) StageSkipped(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Emit blank line separator if there were previous stages.
	if r.stageIdx > 0 {
		fmt.Fprintln(r.w)
	}

	connector := stageConnector(r.stageIdx, r.stageTotal)
	icon := Yellow(r.w, iconSkip)
	msg := fmt.Sprintf("%s %s (skipped)", icon, name)
	fmt.Fprintf(r.w, "  %s %s\n", connector, msg)

	r.stageIdx++
	r.isLastStage = r.stageIdx >= r.stageTotal-1
}

// TaskStart starts a spinner for the running task.
func (r *InteractiveProgressReporter) TaskStart(name string, index int, total int) {
	r.mu.Lock()
	r.taskIdx = index
	r.taskTotal = total
	r.mu.Unlock()

	prefix := r.buildTaskPrefix()
	msg := fmt.Sprintf("%s...", name)

	r.spinner = NewSpinner(r.w, prefix, msg)
	r.spinner.Start()
}

// TaskComplete stops the spinner and emits a success line with green checkmark.
func (r *InteractiveProgressReporter) TaskComplete(name string, duration time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	prefix := r.buildTaskPrefix()
	icon := Green(r.w, iconSuccess)
	durationStr := fmt.Sprintf("(%s)", formatDuration(duration))
	line := fmt.Sprintf("%s%-2s %-28s %s", prefix, icon, name, durationStr)

	if r.spinner != nil {
		r.spinner.Stop(line)
		r.spinner = nil
	} else {
		fmt.Fprintln(r.w, line)
	}

	r.taskIdx++
}

// TaskFailed stops the spinner, emits a failure line with red cross, and
// prints error details indented below.
func (r *InteractiveProgressReporter) TaskFailed(name string, duration time.Duration, exitCode int, stderr string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	prefix := r.buildTaskPrefix()
	icon := Red(r.w, iconFailure)
	durationStr := fmt.Sprintf("(%s)", formatDuration(duration))
	line := fmt.Sprintf("%s%-2s %-28s %s", prefix, icon, name, durationStr)

	if r.spinner != nil {
		r.spinner.Stop(line)
		r.spinner = nil
	} else {
		fmt.Fprintln(r.w, line)
	}

	// Print error details indented below the task line.
	indent := r.buildTaskIndent()
	if exitCode != 0 {
		fmt.Fprintf(r.w, "%s   %s\n", indent, Red(r.w, fmt.Sprintf("Exit code: %d", exitCode)))
	}
	if stderr != "" {
		firstLine := stderr
		if idx := strings.IndexByte(stderr, '\n'); idx >= 0 {
			firstLine = stderr[:idx]
		}
		fmt.Fprintf(r.w, "%s   %s\n", indent, Red(r.w, firstLine))
	}

	r.taskIdx++
}

// TaskSkipped emits a skipped task line with tree connector.
func (r *InteractiveProgressReporter) TaskSkipped(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	prefix := r.buildTaskPrefix()
	icon := Yellow(r.w, iconSkip)
	fmt.Fprintf(r.w, "%s%-2s %-28s\n", prefix, icon, name)

	r.taskIdx++
}

// buildTaskPrefix returns the tree prefix for a task line.
// The prefix includes vertical lines for parent stages and the task connector.
func (r *InteractiveProgressReporter) buildTaskPrefix() string {
	if r.isLastStage {
		// Last stage: no vertical line from parent, just spacing.
		if r.taskIdx == r.taskTotal-1 {
			return "     " + treeCorner + " "
		}
		return "     " + treeTee + " "
	}

	// Non-last stage: vertical line from parent stage continues.
	if r.taskIdx == r.taskTotal-1 {
		return "  " + treeLine + " " + treeCorner + " "
	}
	return "  " + treeLine + " " + treeTee + " "
}

// buildTaskIndent returns the indentation prefix for error details below a task.
func (r *InteractiveProgressReporter) buildTaskIndent() string {
	if r.isLastStage {
		return "        "
	}
	return "  " + treeLine + "     "
}

// stageConnector returns the tree connector for a stage line.
// Uses ├─ for non-last stages, └─ for the last stage.
func stageConnector(index, total int) string {
	if index == total-1 {
		return treeCorner
	}
	return treeTee
}
