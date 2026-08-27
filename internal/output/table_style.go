package output

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// StyledTable is the Style-aware table primitive parity with eka-cli/cmd/ui/table.go
type StyledTable struct {
	s         *Style
	headers   []string
	rows      [][]string
	rowColors [][]func(string) string
}

// NewStyledTable starts an aligned table for the given style and headers.
func NewStyledTable(s *Style, headers ...string) *StyledTable {
	return &StyledTable{s: s, headers: headers}
}

// AddRow appends one row of cells. colors may be nil or partial; a nil color renders plain.
func (t *StyledTable) AddRow(cells []string, colors []func(string) string) *StyledTable {
	t.rows = append(t.rows, cells)
	t.rowColors = append(t.rowColors, colors)
	return t
}

// Render prints the table: accent header + dim underline + rows via s.W
func (t *StyledTable) Render() {
	if len(t.rows) == 0 {
		return
	}
	s := t.s
	widths := make([]int, len(t.headers))
	for i, h := range t.headers {
		widths[i] = utf8.RuneCountInString(h)
	}
	for _, row := range t.rows {
		for i, cell := range row {
			if i < len(widths) && utf8.RuneCountInString(cell) > widths[i] {
				widths[i] = utf8.RuneCountInString(cell)
			}
		}
	}
	var header strings.Builder
	for i, h := range t.headers {
		if i > 0 {
			header.WriteString("  ")
		}
		header.WriteString(fmt.Sprintf("%-*s", widths[i], h))
	}
	fmt.Fprintln(s.W, s.Accent(header.String()))
	total := 0
	for i, w := range widths {
		if i > 0 {
			total += 2
		}
		total += w
	}
	fmt.Fprintln(s.W, s.Dim(strings.Repeat(IconLine, total)))
	for ri, row := range t.rows {
		var line strings.Builder
		for i, cell := range row {
			if i > 0 {
				line.WriteString("  ")
			}
			text := fmt.Sprintf("%-*s", widths[i], cell)
			if ri < len(t.rowColors) && i < len(t.rowColors[ri]) && t.rowColors[ri][i] != nil {
				text = t.rowColors[ri][i](text)
			}
			line.WriteString(text)
		}
		fmt.Fprintln(s.W, line.String())
	}
}
