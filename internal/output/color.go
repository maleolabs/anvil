package output

import (
	"io"
	"os"

	"golang.org/x/term"
)

// ── Color (TS-008-009) ────────────────────────────────────────────────

// ANSI color codes (24-bit-safe basic palette; no color library).
const (
	colorReset = "\x1b[0m"
	colorRed   = "\x1b[31m"
	colorGreen = "\x1b[32m"
)

// noColor disables colored output globally. Set from the global.no_color
// configuration key by the cmd layer.
var noColor bool

// SetNoColor enables or disables colored output globally.
//
// Reference: TS-008-009
func SetNoColor(v bool) { noColor = v }

// colorEnabled reports whether ANSI colors may be applied to writes to w.
// Colors are enabled only when: SetNoColor(false), NO_COLOR env is unset,
// and w is an interactive terminal (exposes Fd() and IsTerminal is true).
func colorEnabled(w io.Writer) bool {
	if noColor {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	f, ok := w.(interface{ Fd() uintptr })
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// Red returns s wrapped in red ANSI codes when colors are enabled for w;
// otherwise returns s unchanged.
//
// Reference: TS-008-009, ADR-010 §7.1
func Red(w io.Writer, s string) string {
	if !colorEnabled(w) {
		return s
	}
	return colorRed + s + colorReset
}

// Green returns s wrapped in green ANSI codes when colors are enabled for w;
// otherwise returns s unchanged.
//
// Reference: TS-008-009, ADR-010 §7.1
func Green(w io.Writer, s string) string {
	if !colorEnabled(w) {
		return s
	}
	return colorGreen + s + colorReset
}
