package output

import (
	"fmt"
	"io"
	"strings"
)

// ── Table ─────────────────────────────────────────────────────────────

// TableStyle selects the border rendering for a Table.
type TableStyle int

const (
	// TableStyleSimple renders the classic aligned columns with a dashed
	// separator under the header (default, backward compatible).
	TableStyleSimple TableStyle = iota

	// TableStyleRounded renders a modern ASCII box table with rounded
	// corners (┌ ┐ └ ┘), clean column dividers (│ ─), and a separator
	// row between the header and the data.
	TableStyleRounded
)

// Table renders aligned columns with an optional header row.
//
// Column widths are automatically determined from the widest cell in each
// column (including the header).
//
// Usage:
//
//	t := output.NewTable("Name", "Version", "Status")
//	t.AddRow("my-app", "1.0.0", "active")
//	t.AddRow("other-app", "2.0.0", "inactive")
//	t.Format(cmd.OutOrStdout())
type Table struct {
	headers []string
	rows    [][]string
	widths  []int
	style   TableStyle
}

// NewTable creates a Table with the given column headers.
// If no headers are provided the table renders without a header row.
func NewTable(headers ...string) *Table {
	t := &Table{
		headers: make([]string, len(headers)),
		widths:  make([]int, len(headers)),
	}
	copy(t.headers, headers)
	for i, h := range headers {
		t.widths[i] = len(h)
	}
	return t
}

// NewRoundedTable creates a Table with the given column headers rendered
// with the modern rounded-corner ASCII box style.
func NewRoundedTable(headers ...string) *Table {
	t := NewTable(headers...)
	t.style = TableStyleRounded
	return t
}

// SetStyle changes the border rendering style of the table.
func (t *Table) SetStyle(style TableStyle) {
	t.style = style
}

// AddRow appends a data row. The number of cells should match the number of
// columns (headers). If no headers were provided the column count is
// determined from the first row. Extra cells are silently truncated;
// missing cells are padded as empty.
func (t *Table) AddRow(cells ...string) {
	// Auto-detect column count from the first row when no headers are set.
	if len(t.headers) == 0 && len(t.rows) == 0 {
		t.headers = make([]string, len(cells))
		t.widths = make([]int, len(cells))
		for i, cell := range cells {
			t.widths[i] = len(cell)
		}
		t.rows = append(t.rows, cells)
		return
	}

	// Normalise to the expected column count.
	if len(cells) > len(t.headers) {
		cells = cells[:len(t.headers)]
	}
	for len(cells) < len(t.headers) {
		cells = append(cells, "")
	}

	for i, cell := range cells {
		if len(cell) > t.widths[i] {
			t.widths[i] = len(cell)
		}
	}
	t.rows = append(t.rows, cells)
}

// Format writes the table to w using the configured style.
func (t *Table) Format(w io.Writer) {
	if len(t.headers) == 0 && len(t.rows) == 0 {
		return
	}
	if t.style == TableStyleRounded {
		t.formatRounded(w)
		return
	}
	t.formatSimple(w)
}

// formatSimple renders the classic aligned-column layout.
func (t *Table) formatSimple(w io.Writer) {
	// Build the format string with calculated column widths.
	fmtStr := t.buildFormatString()

	// Header row.
	if len(t.headers) > 0 {
		cells := make([]any, len(t.headers))
		for i, h := range t.headers {
			cells[i] = h
		}
		fmt.Fprintf(w, fmtStr, cells...)

		// Separator row.
		seps := make([]string, len(t.headers))
		for i := range t.widths {
			seps[i] = strings.Repeat("-", t.widths[i])
		}
		sepCells := make([]any, len(seps))
		for i, s := range seps {
			sepCells[i] = s
		}
		fmt.Fprintf(w, fmtStr, sepCells...)
	}

	// Data rows.
	for _, row := range t.rows {
		cells := make([]any, len(row))
		for i, cell := range row {
			cells[i] = cell
		}
		fmt.Fprintf(w, fmtStr, cells...)
	}
}

// formatRounded renders a modern ASCII box table with rounded corners and
// clean column dividers:
//
//	┌──────────┬──────────────────┬─────────┐
//	│ Name     │ Deployment Model │ Version │
//	├──────────┼──────────────────┼─────────┤
//	│ laravel  │ server           │ -       │
//	└──────────┴──────────────────┴─────────┘
func (t *Table) formatRounded(w io.Writer) {
	// Interior column width: widest cell (header or data) + 1 padding each side.
	inner := make([]int, len(t.widths))
	for i, wd := range t.widths {
		inner[i] = wd + 2
	}

	// Border rows: ┌─┬─┐ / ├─┼─┤ / └─┴─┘.
	divider := func(left, mid, right string) string {
		var sb strings.Builder
		sb.WriteString(left)
		for i, wd := range inner {
			if i > 0 {
				sb.WriteString(mid)
			}
			sb.WriteString(strings.Repeat("─", wd))
		}
		sb.WriteString(right)
		return sb.String()
	}
	top := divider("┌", "┬", "┐")
	mid := divider("├", "┼", "┤")
	bot := divider("└", "┴", "┘")

	// Cell row: "│ %-<w>s │ %-<w>s │".
	cellRow := func(cells []string) string {
		parts := make([]string, len(cells))
		for i, cell := range cells {
			parts[i] = fmt.Sprintf("%-*s", inner[i]-2, cell)
		}
		return fmt.Sprintf("│ %s │\n", strings.Join(parts, " │ "))
	}

	fmt.Fprintln(w, top)
	if len(t.headers) > 0 {
		fmt.Fprint(w, cellRow(t.headers))
		fmt.Fprintln(w, mid)
	}
	for _, row := range t.rows {
		fmt.Fprint(w, cellRow(row))
	}
	fmt.Fprintln(w, bot)
}

// buildFormatString constructs the fmt.Sprintf format string that pads
// each column to its calculated width with two spaces of separation.
func (t *Table) buildFormatString() string {
	var parts []string
	for _, w := range t.widths {
		parts = append(parts, fmt.Sprintf("%%-%ds", w))
	}
	return "  " + strings.Join(parts, "  ") + "\n"
}
