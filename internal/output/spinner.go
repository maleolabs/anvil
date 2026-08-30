// Package output — spinner.go provides a terminal spinner for interactive
// progress display.
//
// # Standard Usage Pattern
//
// The spinner follows a consistent pattern across all Anvil commands:
//
//  1. Create spinner with prefix (tree connector) and message (step name)
//  2. Start spinner animation
//  3. When step completes, stop spinner with final status line
//
// Example for linear workflows (StepReporter):
//
//	prefix := "  ├─ "
//	r.spinner = NewSpinner(r.w, prefix, "Download v1.0.0")
//	r.spinner.Start()
//	// ... do work ...
//	r.spinner.Stop("  ├─ ✓ Download v1.0.0 (2.1s)")
//
// Example for pipeline workflows (ProgressReporter):
//
//	prefix := "  │  ├─ "
//	r.spinner = NewSpinner(r.w, prefix, "compile...")
//	r.spinner.Start()
//	// ... do work ...
//	r.spinner.Stop("  │  ├─ ✓ compile (5.2s)")
//
// # Terminal vs Non-Terminal
//
// - Terminal: Spinner animates with Braille frames on a single line
// - Non-terminal: Spinner is silent during animation, only prints final message
//
// # Reference
//
// Used by:
//   - InteractiveStepReporter (anvil update)
//   - InteractiveProgressReporter (anvil pipeline build/ci)
//
// Reference: Phase 2 UX feedback mechanism
package output

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// defaultFrames are the Braille pattern spinner frames.
var defaultFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// defaultInterval is the time between spinner frame updates.
const defaultInterval = 80 * time.Millisecond

// Spinner renders an animated spinner on a single terminal line.
//
// The spinner accepts a prefix (e.g., tree connector "  ├─ ") and a
// message (e.g., "Download v1.0.0"). During animation, it displays:
//
//	├─ ⠋ Download v1.0.0
//
// When stopped, it replaces the line with a final message.
//
// For non-terminal writers, the spinner is silent during animation
// and only prints the final message when stopped.
type Spinner struct {
	writer     io.Writer
	prefix     string // e.g., "  ├─ "
	message    string // e.g., "Download v1.0.0"
	frames     []string
	interval   time.Duration
	stopCh     chan struct{}
	doneCh     chan struct{}
	mu         sync.Mutex
	running    bool
	isTerminal bool
}

// NewSpinner creates a spinner with the given prefix and message.
// The prefix is typically the tree connector (e.g., "  ├─ "), and
// the message is the step description.
func NewSpinner(w io.Writer, prefix, message string) *Spinner {
	return &Spinner{
		writer:     w,
		prefix:     prefix,
		message:    message,
		frames:     defaultFrames,
		interval:   defaultInterval,
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
		isTerminal: colorEnabled(w),
	}
}

// Start begins the spinner animation in a background goroutine.
func (s *Spinner) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return
	}
	s.running = true
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})

	go s.run()
}

// Stop halts the spinner and replaces the current line with finalMessage.
// It blocks until the background goroutine exits.
func (s *Spinner) Stop(finalMessage string) {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		fmt.Fprintln(s.writer, finalMessage)
		return
	}
	s.running = false
	close(s.stopCh)
	s.mu.Unlock()

	<-s.doneCh

	if s.isTerminal {
		// Clear the line and print final message
		fmt.Fprintf(s.writer, "\r\x1b[2K%s\n", finalMessage)
	} else {
		fmt.Fprintln(s.writer, finalMessage)
	}
}

// UpdateMessage changes the message displayed next to the spinner frame.
func (s *Spinner) UpdateMessage(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.message = msg
}

// run is the background goroutine that cycles through spinner frames.
func (s *Spinner) run() {
	defer close(s.doneCh)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	i := 0
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.mu.Lock()
			frame := s.frames[i%len(s.frames)]
			prefix := s.prefix
			msg := s.message
			s.mu.Unlock()

			if s.isTerminal {
				// Build the full line: prefix + frame + message
				// e.g., "  ├─ ⠋ Download v1.0.0"
				line := fmt.Sprintf("%s%s %s", prefix, frame, msg)
				fmt.Fprintf(s.writer, "\r\x1b[2K%s", line)
			}
			i++
		}
	}
}
