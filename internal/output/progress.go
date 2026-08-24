// Package output provides shared formatters for consistent CLI output
// across all Anvil commands.
//
// This file defines the ProgressReporter interface for real-time pipeline
// execution feedback, the PlainProgressReporter (non-interactive), and a
// factory function that automatically selects the appropriate reporter
// based on terminal capabilities.
//
// Reference: Phase 1 & 2 UX feedback mechanism
package output

import (
	"fmt"
	"io"
	"strings"
	"time"

	"golang.org/x/term"
)

// ProgressReporter emits structured events during pipeline execution.
//
// Implementations must be safe for single-goroutine use only — the engine
// calls these methods from its execution goroutine. The interface is designed
// to be extended in Phase 2 with an InteractiveReporter that uses ANSI
// cursor control.
//
// A nil ProgressReporter is a valid no-op (the engine checks before calling).
type ProgressReporter interface {
	// PipelineStart is called once before any stages execute.
	PipelineStart(name string, env string)

	// PipelineComplete is called when all stages finish successfully.
	PipelineComplete(name string, duration time.Duration)

	// PipelineFailed is called when the pipeline finishes with at least
	// one failed stage.
	PipelineFailed(name string, duration time.Duration)

	// StageStart is called before a stage's tasks execute.
	StageStart(name string, index int, total int)

	// StageComplete is called when all tasks in a stage succeed.
	StageComplete(name string, duration time.Duration)

	// StageFailed is called when at least one task in a stage fails.
	StageFailed(name string, duration time.Duration)

	// StageSkipped is called when a stage is skipped due to a previous failure.
	StageSkipped(name string)

	// TaskStart is called before a task executes.
	TaskStart(name string, index int, total int)

	// TaskComplete is called when a task exits with code 0.
	TaskComplete(name string, duration time.Duration)

	// TaskFailed is called when a task exits with a non-zero code or
	// encounters an execution error.
	TaskFailed(name string, duration time.Duration, exitCode int, stderr string)

	// TaskSkipped is called when a task is skipped due to a previous
	// failure in the same stage or a prior stage.
	TaskSkipped(name string)
}

// PlainProgressReporter writes human-readable, line-by-line progress output
// to an io.Writer. It uses no ANSI codes, no colors, no spinners — just
// plain text lines that work in any terminal, CI log, or piped output.
//
// Format:
//
//	Pipeline: build (production)
//	  Stage: dependencies
//	    Task: download...
//	    Task: download ✓ (2.1s)
//	    Task: verify ✗ (0.5s) - exit code 1
//	      permission denied
//	  Stage: compile (skipped)
//	Pipeline completed in 26.4s
type PlainProgressReporter struct {
	w io.Writer
}

// NewPlainProgressReporter creates a reporter that writes to w.
// If w is nil, all methods become no-ops (safe to use but produces no output).
func NewPlainProgressReporter(w io.Writer) *PlainProgressReporter {
	return &PlainProgressReporter{w: w}
}

// PipelineStart emits the pipeline header line.
func (r *PlainProgressReporter) PipelineStart(name string, env string) {
	if r.w == nil {
		return
	}
	if env != "" {
		fmt.Fprintf(r.w, "Pipeline: %s (%s)\n", name, env)
	} else {
		fmt.Fprintf(r.w, "Pipeline: %s\n", name)
	}
}

// PipelineComplete emits the success summary line.
func (r *PlainProgressReporter) PipelineComplete(name string, duration time.Duration) {
	if r.w == nil {
		return
	}
	fmt.Fprintf(r.w, "Pipeline completed in %s\n", formatDuration(duration))
}

// PipelineFailed emits the failure summary line.
func (r *PlainProgressReporter) PipelineFailed(name string, duration time.Duration) {
	if r.w == nil {
		return
	}
	fmt.Fprintf(r.w, "Pipeline failed in %s\n", formatDuration(duration))
}

// StageStart emits a stage header line.
func (r *PlainProgressReporter) StageStart(name string, index int, total int) {
	if r.w == nil {
		return
	}
	fmt.Fprintf(r.w, "  Stage: %s\n", name)
}

// StageComplete emits a stage completion line.
func (r *PlainProgressReporter) StageComplete(name string, duration time.Duration) {
	if r.w == nil {
		return
	}
	fmt.Fprintf(r.w, "  Stage: %s done (%s)\n", name, formatDuration(duration))
}

// StageFailed emits a stage failure line.
func (r *PlainProgressReporter) StageFailed(name string, duration time.Duration) {
	if r.w == nil {
		return
	}
	fmt.Fprintf(r.w, "  Stage: %s failed (%s)\n", name, formatDuration(duration))
}

// StageSkipped emits a stage skipped line.
func (r *PlainProgressReporter) StageSkipped(name string) {
	if r.w == nil {
		return
	}
	fmt.Fprintf(r.w, "  Stage: %s (skipped)\n", name)
}

// TaskStart emits a task start line with an ellipsis indicator.
func (r *PlainProgressReporter) TaskStart(name string, index int, total int) {
	if r.w == nil {
		return
	}
	fmt.Fprintf(r.w, "    Task: %s...\n", name)
}

// TaskComplete emits a task success line with checkmark and duration.
func (r *PlainProgressReporter) TaskComplete(name string, duration time.Duration) {
	if r.w == nil {
		return
	}
	fmt.Fprintf(r.w, "    Task: %s ✓ (%s)\n", name, formatDuration(duration))
}

// TaskFailed emits a task failure line with cross mark, duration, and exit code.
// When stderr is non-empty, its first line is printed indented below the
// failure line so the failure reason is visible in CI logs and piped output.
func (r *PlainProgressReporter) TaskFailed(name string, duration time.Duration, exitCode int, stderr string) {
	if r.w == nil {
		return
	}
	fmt.Fprintf(r.w, "    Task: %s ✗ (%s) - exit code %d\n", name, formatDuration(duration), exitCode)
	if reason := firstLine(stderr); reason != "" {
		fmt.Fprintf(r.w, "      %s\n", reason)
	}
}

// firstLine returns the first line of s with surrounding whitespace trimmed,
// or "" when s is empty or has no non-blank first line. It keeps the reason
// block bounded to a single line, mirroring InteractiveProgressReporter.
func firstLine(s string) string {
	line := s
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = line[:idx]
	}
	return strings.TrimSpace(line)
}

// TaskSkipped emits a task skipped line.
func (r *PlainProgressReporter) TaskSkipped(name string) {
	if r.w == nil {
		return
	}
	fmt.Fprintf(r.w, "    Task: %s (skipped)\n", name)
}

// formatDuration formats a duration for human-readable display.
// Durations under 1 second show milliseconds (e.g., "450ms").
// Durations 1 second and above show seconds with one decimal (e.g., "2.1s").
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// ── Factory & Options ────────────────────────────────────────────────

// reporterConfig holds configuration for the reporter factory.
type reporterConfig struct {
	interactive *bool // nil = auto-detect
	colors      *bool // nil = auto-detect
}

// ReporterOption configures the NewProgressReporter factory.
type ReporterOption func(*reporterConfig)

// WithInteractive forces interactive (true) or plain (false) mode.
// When not set, the factory auto-detects based on terminal capabilities.
func WithInteractive(enabled bool) ReporterOption {
	return func(c *reporterConfig) {
		c.interactive = &enabled
	}
}

// WithColors forces color output on (true) or off (false).
// When not set, the factory auto-detects based on terminal capabilities
// and the NO_COLOR environment variable.
func WithColors(enabled bool) ReporterOption {
	return func(c *reporterConfig) {
		c.colors = &enabled
	}
}

// NewProgressReporter creates the appropriate ProgressReporter based on
// terminal capabilities.
//
// Auto-detection (default when options are omitted):
//   - Interactive + colors enabled → InteractiveProgressReporter
//   - Otherwise → PlainProgressReporter
//
// The writer w is inspected for terminal capabilities. If w implements
// Fd() uintptr (like *os.File), the factory checks term.IsTerminal.
// The NO_COLOR environment variable (https://no-color.org/) is respected.
func NewProgressReporter(w io.Writer, opts ...ReporterOption) ProgressReporter {
	var cfg reporterConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	interactive := detectInteractive(w)
	if cfg.interactive != nil {
		interactive = *cfg.interactive
	}

	if interactive {
		return NewInteractiveProgressReporter(w)
	}

	return NewPlainProgressReporter(w)
}

// detectInteractive checks if w is an interactive terminal.
// It returns true only when w supports Fd() and term.IsTerminal returns true.
func detectInteractive(w io.Writer) bool {
	f, ok := w.(interface{ Fd() uintptr })
	if !ok {
		return false
	}
	fd := int(f.Fd())
	return term.IsTerminal(fd)
}
