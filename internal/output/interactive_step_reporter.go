package output

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// InteractiveStepReporter implements StepReporter with tree-style output
// including spinner animation and ANSI colors for interactive terminals.
//
// The spinner animates on a single line while a step is running. When
// the step completes, the spinner line is replaced with the final
// status (✓ or ✗) and duration.
//
// Output format (terminal):
//
//	▶ Update Anvil CLI
//
//	  ├─ ✓ Check latest version              (0.8s)
//	  ├─ ⠋ Download v0.6.0...               ← spinner animating
//	  └─ ✓ Install to /usr/local/bin/anvil   (0.1s)
//
//	✓ Updated to v0.6.0 (1.2s)
//
// Output format (non-terminal / piped):
//
//	Update Anvil CLI
//	  ├─ ✓ Check latest version              (0.8s)
//	  ├─ ✓ Download v0.6.0                   (2.1s)
//	  └─ ✓ Install to /usr/local/bin/anvil   (0.1s)
//
//	Updated to v0.6.0 (1.2s)
type InteractiveStepReporter struct {
	w       io.Writer
	mu      sync.Mutex
	stepIdx int
	total   int // -1 if unknown
	spinner *Spinner
}

// NewInteractiveStepReporter creates an InteractiveStepReporter that writes to w.
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

	if colorEnabled(r.w) {
		fmt.Fprintf(r.w, "\n%s %s\n\n", Bold(r.w, "▶"), Bold(r.w, title))
	} else {
		fmt.Fprintf(r.w, "%s\n", title)
	}
}

// SetTotal declares the expected number of steps so the tree connector
// renders "└─" on the last step (stepIdx == total-1).
func (r *InteractiveStepReporter) SetTotal(total int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.total = total
}

// StepStart starts a spinner for the running step.
// On terminal: the spinner animates on a single line.
// On non-terminal: nothing is printed (deferred to StepComplete).
func (r *InteractiveStepReporter) StepStart(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	connector := r.connector()

	if colorEnabled(r.w) {
		// Terminal: start spinner animation
		// Prefix: "  ├─ " (tree connector with indent)
		// Message: "Download v1.0.0" (step name)
		prefix := fmt.Sprintf("  %s ", connector)
		r.spinner = NewSpinner(r.w, prefix, name)
		r.spinner.Start()
	}
	// Non-terminal: don't print anything yet, wait for StepComplete
}

// StepComplete stops the spinner and writes the final status with check mark.
func (r *InteractiveStepReporter) StepComplete(name string, duration time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	connector := r.connector()
	checkmark := "✓"
	if colorEnabled(r.w) {
		checkmark = Green(r.w, "✓")
	}
	finalLine := fmt.Sprintf("  %s %s %-40s (%s)", connector, checkmark, name, FormatDuration(duration))

	if r.spinner != nil {
		// Terminal: stop spinner and replace with final status
		r.spinner.Stop(finalLine)
		r.spinner = nil
	} else {
		// Non-terminal: just print the final status
		fmt.Fprintln(r.w, finalLine)
	}
	r.stepIdx++
}

// StepFailed stops the spinner and writes the final status with cross mark.
func (r *InteractiveStepReporter) StepFailed(name string, duration time.Duration, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	connector := r.connector()
	cross := "✗"
	if colorEnabled(r.w) {
		cross = Red(r.w, "✗")
	}

	var finalLine string
	if err != nil {
		finalLine = fmt.Sprintf("  %s %s %-40s (%s) %v", connector, cross, name, FormatDuration(duration), err)
	} else {
		finalLine = fmt.Sprintf("  %s %s %-40s (%s)", connector, cross, name, FormatDuration(duration))
	}

	if r.spinner != nil {
		// Terminal: stop spinner and replace with final status
		r.spinner.Stop(finalLine)
		r.spinner = nil
	} else {
		// Non-terminal: just print the final status
		fmt.Fprintln(r.w, finalLine)
	}
	r.stepIdx++
}

// Complete writes a success summary with check mark.
func (r *InteractiveStepReporter) Complete(title string, duration time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	checkmark := "✓"
	if colorEnabled(r.w) {
		checkmark = Green(r.w, "✓")
	}
	fmt.Fprintf(r.w, "\n%s %s (%s)\n\n", checkmark, title, FormatDuration(duration))
}

// Failed writes a failure summary with cross mark.
func (r *InteractiveStepReporter) Failed(title string, duration time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cross := "✗"
	if colorEnabled(r.w) {
		cross = Red(r.w, "✗")
	}
	fmt.Fprintf(r.w, "\n%s %s (%s)\n\n", cross, title, FormatDuration(duration))
}

// connector returns the tree-drawing connector for the current step.
func (r *InteractiveStepReporter) connector() string {
	if r.total > 0 && r.stepIdx == r.total-1 {
		return "└─"
	}
	return "├─"
}
