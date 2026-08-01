package output

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// spinnerFrames are the Braille animation frames for the spinner.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// InteractiveStepReporter implements StepReporter with tree-style output
// including spinner animation and ANSI colors for interactive terminals.
//
// Output format:
//
//	▶ Update Anvil CLI
//
//	  ├─ ✓ Check latest version              (0.8s)
//	  ├─ ⠋ Downloading v0.6.0...            ← spinner
//	  ├─ ✓ Verify checksum                   (0.3s)
//	  └─ ✓ Install to /usr/local/bin/anvil   (0.1s)
//
//	✓ Updated to v0.6.0 (1.2s)
//
// Reference: TS-008-009
type InteractiveStepReporter struct {
	w       io.Writer
	mu      sync.Mutex
	stepIdx int
	total   int // -1 if unknown
}

// NewInteractiveStepReporter creates an InteractiveStepReporter that writes to w.
//
// Reference: TS-008-009
func NewInteractiveStepReporter(w io.Writer) *InteractiveStepReporter {
	return &InteractiveStepReporter{
		w:     w,
		total: -1,
	}
}

// Start writes the workflow title with ▶ prefix.
func (r *InteractiveStepReporter) Start(title string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	fmt.Fprintf(r.w, "%s %s\n\n", Bold(r.w, "▶"), Bold(r.w, title))
}

// StepStart writes a tree connector with spinner frame indicating the
// step is in progress.
func (r *InteractiveStepReporter) StepStart(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	connector := r.connector()
	frame := spinnerFrames[r.stepIdx%len(spinnerFrames)]
	fmt.Fprintf(r.w, "  %s %s %s...\n", connector, Yellow(r.w, frame), name)
}

// StepComplete writes a tree connector with check mark and duration.
func (r *InteractiveStepReporter) StepComplete(name string, duration time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	connector := r.connector()
	checkmark := Green(r.w, "✓")
	fmt.Fprintf(r.w, "  %s %s %-40s %s\n", connector, checkmark, name, FormatDuration(duration))
	r.stepIdx++
}

// StepFailed writes a tree connector with cross mark and error.
func (r *InteractiveStepReporter) StepFailed(name string, duration time.Duration, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	connector := r.connector()
	cross := Red(r.w, "✗")
	if err != nil {
		fmt.Fprintf(r.w, "  %s %s %-40s %s: %v\n", connector, cross, name, FormatDuration(duration), err)
	} else {
		fmt.Fprintf(r.w, "  %s %s %-40s %s\n", connector, cross, name, FormatDuration(duration))
	}
	r.stepIdx++
}

// Complete writes a success summary with check mark.
func (r *InteractiveStepReporter) Complete(title string, duration time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	fmt.Fprintf(r.w, "\n%s %s (%s)\n", Green(r.w, "✓"), Bold(r.w, title), FormatDuration(duration))
}

// Failed writes a failure summary with cross mark.
func (r *InteractiveStepReporter) Failed(title string, duration time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	fmt.Fprintf(r.w, "\n%s %s (%s)\n", Red(r.w, "✗"), Bold(r.w, title), FormatDuration(duration))
}

// connector returns the tree-drawing connector for the current step.
// Uses ├─ for middle steps and └─ for the last step (when total is known).
func (r *InteractiveStepReporter) connector() string {
	if r.total > 0 && r.stepIdx == r.total-1 {
		return "└─"
	}
	return "├─"
}
