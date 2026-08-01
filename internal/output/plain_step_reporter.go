package output

import (
	"fmt"
	"io"
	"time"
)

// PlainStepReporter implements StepReporter with line-by-line output
// suitable for non-interactive contexts (pipes, CI, log files).
//
// Output format:
//
//	Update Anvil CLI
//	  Step: Check latest version...
//	  Step: Check latest version ✓ (0.8s)
//	  Step: Downloading v0.6.0...
//	  Step: Downloading v0.6.0 ✓ (2.1s)
//	Updated to v0.6.0 (3.3s)
//
// Reference: TS-008-009
type PlainStepReporter struct {
	w io.Writer
}

// NewPlainStepReporter creates a PlainStepReporter that writes to w.
//
// Reference: TS-008-009
func NewPlainStepReporter(w io.Writer) *PlainStepReporter {
	return &PlainStepReporter{w: w}
}

// Start writes the workflow title.
func (r *PlainStepReporter) Start(title string) {
	fmt.Fprintf(r.w, "%s\n", title)
}

// StepStart writes a "Step: ..." line indicating the step is beginning.
func (r *PlainStepReporter) StepStart(name string) {
	fmt.Fprintf(r.w, "  Step: %s...\n", name)
}

// StepComplete writes a "Step: ... ✓ (duration)" line.
func (r *PlainStepReporter) StepComplete(name string, duration time.Duration) {
	fmt.Fprintf(r.w, "  Step: %s ✓ (%s)\n", name, FormatDuration(duration))
}

// StepFailed writes a "Step: ... ✗ (duration)" line with the error.
func (r *PlainStepReporter) StepFailed(name string, duration time.Duration, err error) {
	fmt.Fprintf(r.w, "  Step: %s ✗ (%s): %v\n", name, FormatDuration(duration), err)
}

// Complete writes a success summary line.
func (r *PlainStepReporter) Complete(title string, duration time.Duration) {
	fmt.Fprintf(r.w, "%s (%s)\n", title, FormatDuration(duration))
}

// Failed writes a failure summary line.
func (r *PlainStepReporter) Failed(title string, duration time.Duration) {
	fmt.Fprintf(r.w, "%s failed (%s)\n", title, FormatDuration(duration))
}
