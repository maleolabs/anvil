package output

import (
	"bytes"
	"strings"
	"testing"
)

// ── Red / Green (TS-008-009) ─────────────────────────────────────────

// TestRed_NonTerminalPlain verifies that Red returns the message unchanged
// when the writer is not an interactive terminal (no ANSI codes).
func TestRed_NonTerminalPlain(t *testing.T) {
	var buf bytes.Buffer
	got := Red(&buf, "msg")
	if got != "msg" {
		t.Errorf("Red() = %q, want %q (plain, no ANSI)", got, "msg")
	}
	if strings.Contains(got, "\x1b") {
		t.Errorf("Red() must not contain ANSI escape codes, got %q", got)
	}
}

// TestGreen_NonTerminalPlain verifies that Green returns the message
// unchanged when the writer is not an interactive terminal (no ANSI codes).
func TestGreen_NonTerminalPlain(t *testing.T) {
	var buf bytes.Buffer
	got := Green(&buf, "msg")
	if got != "msg" {
		t.Errorf("Green() = %q, want %q (plain, no ANSI)", got, "msg")
	}
	if strings.Contains(got, "\x1b") {
		t.Errorf("Green() must not contain ANSI escape codes, got %q", got)
	}
}

// TestSetNoColorDisablesColors verifies that SetNoColor(true) forces plain
// output even when colors would otherwise be enabled.
func TestSetNoColorDisablesColors(t *testing.T) {
	SetNoColor(true)
	t.Cleanup(func() { SetNoColor(false) })

	var buf bytes.Buffer
	if got := Red(&buf, "msg"); got != "msg" {
		t.Errorf("Red() with noColor = %q, want %q (plain)", got, "msg")
	}
	if got := Green(&buf, "msg"); got != "msg" {
		t.Errorf("Green() with noColor = %q, want %q (plain)", got, "msg")
	}
}

// TestNoColorEnvDisablesColors verifies that the NO_COLOR environment
// variable forces plain output even when colors would otherwise be enabled.
func TestNoColorEnvDisablesColors(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	var buf bytes.Buffer
	if got := Red(&buf, "msg"); got != "msg" {
		t.Errorf("Red() with NO_COLOR = %q, want %q (plain)", got, "msg")
	}
	if got := Green(&buf, "msg"); got != "msg" {
		t.Errorf("Green() with NO_COLOR = %q, want %q (plain)", got, "msg")
	}
}
