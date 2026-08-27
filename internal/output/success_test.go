package output

import (
	"bytes"
	"strings"
	"testing"
)

// ── PrintSuccess / PrintSuccessf (TS-008-009) ────────────────────────

// TestPrintSuccess_NonTerminalPlain verifies that PrintSuccess writes the
// message followed by a newline, without ANSI codes, to a non-terminal
// writer.
func TestPrintSuccess_NonTerminalPlain(t *testing.T) {
	var buf bytes.Buffer
	PrintSuccess(&buf, "msg")
	want := "msg\n"
	if got := buf.String(); got != want {
		t.Errorf("PrintSuccess() = %q, want %q", got, want)
	}
	if strings.Contains(buf.String(), "\x1b") {
		t.Errorf("PrintSuccess() must not contain ANSI escape codes, got %q", buf.String())
	}
}

// TestPrintSuccessf verifies that PrintSuccessf formats the message before
// writing it.
func TestPrintSuccessf(t *testing.T) {
	var buf bytes.Buffer
	PrintSuccessf(&buf, "Project '%s' created.", "my-app")
	want := "Project 'my-app' created.\n"
	if got := buf.String(); got != want {
		t.Errorf("PrintSuccessf() = %q, want %q", got, want)
	}
}
