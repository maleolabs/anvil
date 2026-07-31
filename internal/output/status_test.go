package output

import (
	"bytes"
	"strings"
	"testing"
)

// ── PrintStatus (TS-008-009) ─────────────────────────────────────────

// TestPrintStatus_NonTerminalPlain verifies that PrintStatus writes the
// indicator line without ANSI codes to a non-terminal writer, for both
// PASS and FAIL statuses.
func TestPrintStatus_NonTerminalPlain(t *testing.T) {
	var buf bytes.Buffer
	PrintStatus(&buf, StatusPass, "msg")
	want := "[PASS] msg\n"
	if got := buf.String(); got != want {
		t.Errorf("PrintStatus(PASS) = %q, want %q", got, want)
	}

	buf.Reset()
	PrintStatus(&buf, StatusFail, "msg")
	want = "[FAIL] msg\n"
	if got := buf.String(); got != want {
		t.Errorf("PrintStatus(FAIL) = %q, want %q", got, want)
	}

	if strings.Contains(buf.String(), "\x1b") {
		t.Errorf("PrintStatus() must not contain ANSI escape codes on non-terminal writers, got %q", buf.String())
	}
}
