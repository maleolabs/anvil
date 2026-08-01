package output

import (
	"fmt"
	"io"
)

// ── List ──────────────────────────────────────────────────────────────

// List renders a collection of items as a bullet or numbered list.
//
// Usage:
//
//	l := output.NewList(output.BulletList)
//	l.AddItem("First item")
//	l.AddItem("Second item")
//	l.Format(cmd.OutOrStdout())
type List struct {
	style ListStyle
	items []string
}

// NewList creates a List with the given style (BulletList or NumberedList).
func NewList(style ListStyle) *List {
	return &List{
		style: style,
		items: nil,
	}
}

// AddItem appends an item to the list.
func (l *List) AddItem(item string) {
	l.items = append(l.items, item)
}

// Format writes the list to w.
func (l *List) Format(w io.Writer) {
	if len(l.items) == 0 {
		return
	}

	switch l.style {
	case NumberedList:
		for i, item := range l.items {
			fmt.Fprintf(w, "  %d. %s\n", i+1, item)
		}
	default:
		for _, item := range l.items {
			fmt.Fprintf(w, "  - %s\n", item)
		}
	}
}
