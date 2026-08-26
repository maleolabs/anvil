package output

import (
	"io"
	"os"

	"golang.org/x/term"
)

// ── Color (platform theme - soft 256-color) ─────────────────────────

// ANSI SGR 256-color palette (soft, low-contrast). These are the only
// colors the CLI may emit. Colors are decoration — icons and text carry
// meaning.
const (
	colorReset    = "\x1b[0m"
	colorRed      = "\x1b[38;5;167m" // Error  muted red
	colorGreen    = "\x1b[38;5;114m" // Success soft green
	colorYellow   = "\x1b[38;5;214m" // Warning amber
	colorCyan     = "\x1b[38;5;75m"  // Info soft blue (Cyan maps to Info)
	colorDim      = "\x1b[38;5;245m" // Dim gray
	colorProgress = "\x1b[38;5;80m"  // Progress soft cyan
	boldStart     = "\x1b[1m"
)

// Exported palette constants for Style-less helpers parity with eka-cli.
const (
	ColorInfo     = "38;5;75"
	ColorSuccess  = "38;5;114"
	ColorWarning  = "38;5;214"
	ColorError    = "38;5;167"
	ColorProgress = "38;5;80"
	ColorDim      = "38;5;245"
	ColorAccent   = ColorInfo
)

// noColor disables colored output globally. Set from the global.no_color
// configuration key by the cmd layer.
var noColor bool

// SetNoColor enables or disables colored output globally.
//
// Reference: TS-008-009
func SetNoColor(v bool) { noColor = v }

// IsTTY reports whether w is a terminal.
func IsTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

func terminalWidth(w io.Writer) int {
	f, ok := w.(*os.File)
	if !ok {
		return 0
	}
	width, _, err := term.GetSize(int(f.Fd()))
	if err != nil {
		return 0
	}
	return width
}

// colorEnabled reports whether ANSI colors may be applied to writes to w.
// Colors are enabled only when: SetNoColor(false), NO_COLOR env is unset,
// TERM != dumb, and w is an interactive terminal.
func colorEnabled(w io.Writer) bool {
	if noColor {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	if !IsTTY(w) {
		return false
	}
	return true
}

// Red returns s wrapped in red (Error 167) ANSI codes when colors are enabled for w;
// otherwise returns s unchanged.
func Red(w io.Writer, s string) string {
	if !colorEnabled(w) {
		return s
	}
	return colorRed + s + colorReset
}

// Green returns s wrapped in green (Success 114) ANSI codes when colors are enabled for w;
// otherwise returns s unchanged.
func Green(w io.Writer, s string) string {
	if !colorEnabled(w) {
		return s
	}
	return colorGreen + s + colorReset
}

// Yellow returns s wrapped in yellow (Warning 214) ANSI codes when colors are enabled for w;
// otherwise returns s unchanged.
func Yellow(w io.Writer, s string) string {
	if !colorEnabled(w) {
		return s
	}
	return colorYellow + s + colorReset
}

// Cyan returns s wrapped in cyan (Info 75) ANSI codes when colors are enabled for w;
// otherwise returns s unchanged.
func Cyan(w io.Writer, s string) string {
	if !colorEnabled(w) {
		return s
	}
	return colorCyan + s + colorReset
}

// Dim returns s wrapped in dim (245) ANSI codes when colors are enabled for w;
// otherwise returns s unchanged.
func Dim(w io.Writer, s string) string {
	if !colorEnabled(w) {
		return s
	}
	return colorDim + s + colorReset
}

// Progress returns s wrapped in progress (80) ANSI codes when colors are enabled for w;
// otherwise returns s unchanged.
func Progress(w io.Writer, s string) string {
	if !colorEnabled(w) {
		return s
	}
	return colorProgress + s + colorReset
}

// Bold returns s wrapped in bold ANSI codes when colors are enabled for w;
// otherwise returns s unchanged.
func Bold(w io.Writer, s string) string {
	if !colorEnabled(w) {
		return s
	}
	return boldStart + s + colorReset
}
