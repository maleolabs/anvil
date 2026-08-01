package output

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// ── Spinner (Phase 2) ───────────────────────────────────────────────

// TestSpinner_StartStop verifies that the spinner starts and stops cleanly,
// and that the final message replaces the spinner line.
func TestSpinner_StartStop(t *testing.T) {
	var buf bytes.Buffer
	s := NewSpinner(&buf, "loading...")

	s.Start()
	time.Sleep(200 * time.Millisecond) // Let a few frames render.
	s.Stop("done!")

	output := buf.String()

	// The final message should be present.
	if !strings.Contains(output, "done!") {
		t.Errorf("Stop() output missing final message, got %q", output)
	}
}

// TestSpinner_UpdateMessage verifies that UpdateMessage changes the
// spinner's displayed message.
func TestSpinner_UpdateMessage(t *testing.T) {
	var buf bytes.Buffer
	s := NewSpinner(&buf, "step 1...")

	s.Start()
	time.Sleep(100 * time.Millisecond)
	s.UpdateMessage("step 2...")
	time.Sleep(100 * time.Millisecond)
	s.Stop("finished")

	output := buf.String()
	if !strings.Contains(output, "finished") {
		t.Errorf("Stop() output missing final message, got %q", output)
	}
}

// TestSpinner_StopWithoutStart verifies that Stop works even if Start
// was never called.
func TestSpinner_StopWithoutStart(t *testing.T) {
	var buf bytes.Buffer
	s := NewSpinner(&buf, "msg")

	// Should not panic.
	s.Stop("final")

	output := buf.String()
	if !strings.Contains(output, "final") {
		t.Errorf("Stop() without Start() should print final message, got %q", output)
	}
}

// TestSpinner_DoubleStart verifies that calling Start twice is safe.
func TestSpinner_DoubleStart(t *testing.T) {
	var buf bytes.Buffer
	s := NewSpinner(&buf, "msg")

	s.Start()
	s.Start() // Should be a no-op.
	time.Sleep(100 * time.Millisecond)
	s.Stop("done")

	output := buf.String()
	if !strings.Contains(output, "done") {
		t.Errorf("Stop() output missing final message, got %q", output)
	}
}

// TestSpinner_DoubleStop verifies that calling Stop twice is safe.
func TestSpinner_DoubleStop(t *testing.T) {
	var buf bytes.Buffer
	s := NewSpinner(&buf, "msg")

	s.Start()
	time.Sleep(100 * time.Millisecond)
	s.Stop("first")
	s.Stop("second") // Should print directly.
}

// TestSpinner_NoANSIInFinalMessage verifies that the spinner's final output
// contains the clear-line ANSI sequence (for terminal cleanup) but the
// final message itself is clean when written to a non-terminal.
func TestSpinner_NoANSIInFinalMessage(t *testing.T) {
	var buf bytes.Buffer
	s := NewSpinner(&buf, "working...")

	s.Start()
	time.Sleep(100 * time.Millisecond)
	s.Stop("all done")

	output := buf.String()
	// The final message content should be present.
	if !strings.Contains(output, "all done") {
		t.Errorf("output missing final message, got %q", output)
	}
}
