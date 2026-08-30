package output

import "io"

// Style carries the presentation context for one command execution.
type Style struct {
	Color   bool
	TTY     bool
	Verbose bool
	W       io.Writer
	Width   int
	raw     io.Writer
}

// NewStyle builds the Style for one command execution against w.
func NewStyle(w io.Writer, verbose bool) *Style {
	color := colorEnabled(w)
	if noColor {
		color = false
	}
	return &Style{
		Color:   color,
		TTY:     IsTTY(w),
		Verbose: verbose,
		W:       newMarginWriter(w),
		Width:   terminalWidth(w),
		raw:     w,
	}
}

// Raw returns the UNWRAPPED writer.
func (s *Style) Raw() io.Writer {
	if s.raw != nil {
		return s.raw
	}
	return s.W
}

func (s *Style) paint(code, text string) string {
	if !s.Color {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func (s *Style) Info(text string) string     { return s.paint(ColorInfo, text) }
func (s *Style) Success(text string) string  { return s.paint(ColorSuccess, text) }
func (s *Style) Warning(text string) string  { return s.paint(ColorWarning, text) }
func (s *Style) Error(text string) string    { return s.paint(ColorError, text) }
func (s *Style) Progress(text string) string { return s.paint(ColorProgress, text) }
func (s *Style) Dim(text string) string      { return s.paint(ColorDim, text) }
func (s *Style) Accent(text string) string   { return s.paint(ColorAccent, text) }
