// Package output — spinner.go provides a terminal spinner for interactive
// progress display. The spinner runs in a background goroutine and uses
// carriage return (\r) for in-place line updates when writing to a terminal.
// For non-terminal writers, it falls back to simple line-by-line output.
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
// It is safe for concurrent message updates but Start/Stop must be
// called from the same goroutine.
//
// When the writer is a terminal (supports Fd() and IsTerminal is true),
// the spinner uses \r and ANSI clear-line for in-place updates.
// Otherwise, it prints simple lines without ANSI codes.
type Spinner struct {
	writer      io.Writer
	message     string
	frames      []string
	interval    time.Duration
	stopCh      chan struct{}
	doneCh      chan struct{}
	mu          sync.Mutex
	running     bool
	isTerminal  bool
}

// NewSpinner creates a spinner that writes to w with the given message.
// Call Start() to begin animation; call Stop() to halt and print a final message.
func NewSpinner(w io.Writer, message string) *Spinner {
	return &Spinner{
		writer:     w,
		message:    message,
		frames:     defaultFrames,
		interval:   defaultInterval,
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
		isTerminal: colorEnabled(w),
	}
}

// Start begins the spinner animation in a background goroutine.
// It is a no-op if the spinner is already running.
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
		// Not running — just print the final message.
		fmt.Fprintln(s.writer, finalMessage)
		return
	}
	s.running = false
	close(s.stopCh)
	s.mu.Unlock()

	// Wait for the goroutine to finish.
	<-s.doneCh

	if s.isTerminal {
		// Terminal: clear the spinner line and print the final message.
		fmt.Fprintf(s.writer, "\r\x1b[2K%s\n", finalMessage)
	} else {
		// Non-terminal: just print the final message on a new line.
		fmt.Fprintln(s.writer, finalMessage)
	}
}

// UpdateMessage changes the message displayed next to the spinner frames.
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
			msg := s.message
			s.mu.Unlock()

			if s.isTerminal {
				// Terminal: overwrite the current line.
				fmt.Fprintf(s.writer, "\r\x1b[2K%s %s", frame, msg)
			}
			// Non-terminal: skip intermediate frames (no-op during animation).
			i++
		}
	}
}
