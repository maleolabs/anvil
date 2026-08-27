// Package output provides shared formatters for consistent CLI output
// across all Anvil commands.
//
// ── StepReporter ─────────────────────────────────────────────────────
//
// StepReporter reports progress for linear step-based workflows.
// Unlike pipeline-specific progress reporters, StepReporter is designed
// for sequential operations like update, install, or provision.
//
// Reference: TS-008-009
package output

import (
	"fmt"
	"io"
	"time"
)

// StepReporter reports progress for linear step-based workflows.
//
// Lifecycle:
//  1. Start(title)       — called once at the beginning
//  2. StepStart(name)    — called before each step
//  3. StepComplete / StepFailed — called after each step
//  4. Complete / Failed  — called once at the end
//
// Reference: TS-008-009
type StepReporter interface {
	// Start is called once at the beginning with a workflow title.
	Start(title string)

	// SetTotal declares the expected number of steps so the tree
	// connector can render "└─" on the last step. When it is not called
	// (or called with a non-positive value), every step renders with the
	// default "├─" connector. It must be called before the last step
	// starts to take effect.
	SetTotal(total int)

	// StepStart is called before a step begins.
	StepStart(name string)

	// StepComplete is called when a step finishes successfully.
	StepComplete(name string, duration time.Duration)

	// StepFailed is called when a step fails.
	StepFailed(name string, duration time.Duration, err error)

	// Complete is called when the entire workflow succeeds.
	Complete(title string, duration time.Duration)

	// Failed is called when the workflow fails.
	Failed(title string, duration time.Duration)
}

// NewStepReporter creates a StepReporter appropriate for the writer.
// For interactive terminals, returns InteractiveStepReporter.
// For non-interactive contexts, returns PlainStepReporter.
//
// Reference: TS-008-009
func NewStepReporter(w io.Writer) StepReporter {
	if colorEnabled(w) {
		return NewInteractiveStepReporter(w)
	}
	return NewPlainStepReporter(w)
}

// FormatDuration formats a duration for human-readable display.
// Examples: "0.3s", "1.2s", "3.0s", "1m5.4s"
func FormatDuration(d time.Duration) string {
	d = d.Truncate(100 * time.Millisecond)
	if d < time.Second {
		return fmt.Sprintf("%.1f", float64(d)/float64(time.Second)) + "s"
	}
	return d.String()
}
