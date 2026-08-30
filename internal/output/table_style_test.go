package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestStyledTableRender(t *testing.T) {
	var buf bytes.Buffer
	s := NewStyle(&buf, false)
	tbl := NewStyledTable(s, "Name", "Version")
	tbl.AddRow([]string{"my-app", "1.0.0"}, nil)
	tbl.AddRow([]string{"other-app", "2.0.0"}, []func(string) string{s.Info, s.Dim})
	tbl.Render()
	got := buf.String()
	if !strings.Contains(got, "Name") || !strings.Contains(got, "my-app") {
		t.Fatalf("StyledTable render missing content: %q", got)
	}
	// Should have margin 2 via Style.W
	if !strings.Contains(got, "  Name") {
		t.Fatalf("StyledTable missing Margin 2: %q", got)
	}
	// Header should be via Accent (plain non-TTY, so still text)
	if !strings.Contains(got, IconLine) {
		t.Fatalf("StyledTable missing Dim underline IconLine: %q", got)
	}
	// Non-TTY determinism: no ANSI
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("non-TTY should be plain without ANSI: %q", got)
	}
	// Empty table renders nothing
	var buf2 bytes.Buffer
	s2 := NewStyle(&buf2, false)
	NewStyledTable(s2, "H").Render()
	if buf2.String() != "" {
		t.Fatalf("empty table should render nothing, got %q", buf2.String())
	}
}
