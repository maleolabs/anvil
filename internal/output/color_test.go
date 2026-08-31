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

// ── Yellow / Cyan / Bold (Phase 2) ──────────────────────────────────

// TestYellow_NonTerminalPlain verifies that Yellow returns the message
// unchanged when the writer is not an interactive terminal.
func TestYellow_NonTerminalPlain(t *testing.T) {
	var buf bytes.Buffer
	got := Yellow(&buf, "msg")
	if got != "msg" {
		t.Errorf("Yellow() = %q, want %q (plain, no ANSI)", got, "msg")
	}
	if strings.Contains(got, "\x1b") {
		t.Errorf("Yellow() must not contain ANSI escape codes, got %q", got)
	}
}

// TestCyan_NonTerminalPlain verifies that Cyan returns the message
// unchanged when the writer is not an interactive terminal.
func TestCyan_NonTerminalPlain(t *testing.T) {
	var buf bytes.Buffer
	got := Cyan(&buf, "msg")
	if got != "msg" {
		t.Errorf("Cyan() = %q, want %q (plain, no ANSI)", got, "msg")
	}
	if strings.Contains(got, "\x1b") {
		t.Errorf("Cyan() must not contain ANSI escape codes, got %q", got)
	}
}

// TestBold_NonTerminalPlain verifies that Bold returns the message
// unchanged when the writer is not an interactive terminal.
func TestBold_NonTerminalPlain(t *testing.T) {
	var buf bytes.Buffer
	got := Bold(&buf, "msg")
	if got != "msg" {
		t.Errorf("Bold() = %q, want %q (plain, no ANSI)", got, "msg")
	}
	if strings.Contains(got, "\x1b") {
		t.Errorf("Bold() must not contain ANSI escape codes, got %q", got)
	}
}

// TestYellow_WithNoColor verifies that Yellow respects SetNoColor.
func TestYellow_WithNoColor(t *testing.T) {
	SetNoColor(true)
	t.Cleanup(func() { SetNoColor(false) })

	var buf bytes.Buffer
	if got := Yellow(&buf, "msg"); got != "msg" {
		t.Errorf("Yellow() with noColor = %q, want %q (plain)", got, "msg")
	}
}

// TestCyan_WithNoColor verifies that Cyan respects SetNoColor.
func TestCyan_WithNoColor(t *testing.T) {
	SetNoColor(true)
	t.Cleanup(func() { SetNoColor(false) })

	var buf bytes.Buffer
	if got := Cyan(&buf, "msg"); got != "msg" {
		t.Errorf("Cyan() with noColor = %q, want %q (plain)", got, "msg")
	}
}

// TestBold_WithNoColor verifies that Bold respects SetNoColor.
func TestBold_WithNoColor(t *testing.T) {
	SetNoColor(true)
	t.Cleanup(func() { SetNoColor(false) })

	var buf bytes.Buffer
	if got := Bold(&buf, "msg"); got != "msg" {
		t.Errorf("Bold() with noColor = %q, want %q (plain)", got, "msg")
	}
}

// TestYellow_WithNoColorEnv verifies that Yellow respects NO_COLOR env.
func TestYellow_WithNoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	var buf bytes.Buffer
	if got := Yellow(&buf, "msg"); got != "msg" {
		t.Errorf("Yellow() with NO_COLOR = %q, want %q (plain)", got, "msg")
	}
}

// TestCyan_WithNoColorEnv verifies that Cyan respects NO_COLOR env.
func TestCyan_WithNoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	var buf bytes.Buffer
	if got := Cyan(&buf, "msg"); got != "msg" {
		t.Errorf("Cyan() with NO_COLOR = %q, want %q (plain)", got, "msg")
	}
}

// TestBold_WithNoColorEnv verifies that Bold respects NO_COLOR env.
func TestBold_WithNoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	var buf bytes.Buffer
	if got := Bold(&buf, "msg"); got != "msg" {
		t.Errorf("Bold() with NO_COLOR = %q, want %q (plain)", got, "msg")
	}
}
