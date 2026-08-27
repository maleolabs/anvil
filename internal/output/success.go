package output

import (
	"fmt"
	"io"
)

// ── Success Confirmation (TS-008-009) ────────────────────────────────

// PrintSuccess writes a success confirmation message to w. The message is
// rendered in green when w is an interactive terminal and colors are enabled;
// plain text otherwise.
//
// Reference: TS-008-009, Platform-010 §7.1
func PrintSuccess(w io.Writer, message string) {
	fmt.Fprintf(w, "%s\n", Green(w, message))
}

// PrintSuccessf formats a success confirmation message and writes it to w.
//
// Reference: TS-008-009, Platform-010 §7.1
func PrintSuccessf(w io.Writer, format string, args ...interface{}) {
	PrintSuccess(w, fmt.Sprintf(format, args...))
}
