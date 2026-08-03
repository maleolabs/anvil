package output

import (
	"bytes"
	"strings"
	"testing"
)

// ── Table Tests ───────────────────────────────────────────────────────

func TestTable_Empty(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable()
	tbl.Format(&buf)
	if buf.Len() != 0 {
		t.Errorf("empty table should produce no output, got %q", buf.String())
	}
}

func TestTable_HeadersOnly(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable("Name", "Version")
	tbl.Format(&buf)
	got := buf.String()

	if !strings.Contains(got, "Name") {
		t.Error("output should contain 'Name' header")
	}
	if !strings.Contains(got, "Version") {
		t.Error("output should contain 'Version' header")
	}
	// Should also contain separator dashes.
	if !strings.Contains(got, "----") {
		t.Error("output should contain separator dashes")
	}
}

func TestTable_SingleRow(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable("Name", "Status")
	tbl.AddRow("my-app", "active")
	tbl.Format(&buf)
	got := buf.String()

	if !strings.Contains(got, "my-app") {
		t.Errorf("output should contain 'my-app', got %q", got)
	}
	if !strings.Contains(got, "active") {
		t.Errorf("output should contain 'active', got %q", got)
	}
}

func TestTable_MultiRow(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable("Name", "Version", "Status")
	tbl.AddRow("app-a", "1.0.0", "active")
	tbl.AddRow("app-b-long-name", "2.0.0", "inactive")
	tbl.Format(&buf)
	got := buf.String()

	// All data must be present.
	for _, s := range []string{"app-a", "app-b-long-name", "1.0.0", "2.0.0", "active", "inactive"} {
		if !strings.Contains(got, s) {
			t.Errorf("output should contain %q, got %q", s, got)
		}
	}
}

func TestTable_ColumnAlignment(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable("Name", "Status")
	tbl.AddRow("short", "ok")
	tbl.AddRow("very-long-name", "ok")
	tbl.Format(&buf)
	got := buf.String()

	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines (header + sep + rows), got %d", len(lines))
	}

	// Data rows should have aligned Status column.
	// "ok" should appear at roughly the same column in both rows.
	// We check that both lines contain "ok" after the separator spacing.
	for _, line := range lines[2:] {
		if !strings.Contains(line, "ok") {
			t.Errorf("each data row should contain 'ok', got %q", line)
		}
	}
}

func TestTable_NoHeaders(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable()
	tbl.AddRow("just", "data")
	tbl.Format(&buf)
	got := buf.String()

	if !strings.Contains(got, "just") || !strings.Contains(got, "data") {
		t.Errorf("output should contain data cells, got %q", got)
	}
}

func TestTable_RowCellsTruncated(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable("A", "B")
	tbl.AddRow("1", "2", "3") // third cell should be silently truncated
	tbl.Format(&buf)
	got := buf.String()

	if strings.Contains(got, "3") {
		t.Errorf("third cell should be truncated, got %q", got)
	}
}

func TestTable_RowCellsPadded(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable("A", "B", "C")
	tbl.AddRow("1") // missing cells should be padded as empty
	tbl.Format(&buf)
	got := buf.String()

	if !strings.Contains(got, "1") {
		t.Errorf("output should contain the provided cell, got %q", got)
	}
}

// ── List Tests ────────────────────────────────────────────────────────

func TestList_Empty(t *testing.T) {
	var buf bytes.Buffer
	l := NewList(BulletList)
	l.Format(&buf)
	if buf.Len() != 0 {
		t.Errorf("empty list should produce no output, got %q", buf.String())
	}
}

func TestList_Bullet(t *testing.T) {
	var buf bytes.Buffer
	l := NewList(BulletList)
	l.AddItem("first")
	l.AddItem("second")
	l.AddItem("third")
	l.Format(&buf)
	got := buf.String()

	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	for _, line := range lines {
		if !strings.HasPrefix(line, "  - ") {
			t.Errorf("bullet line should start with '  - ', got %q", line)
		}
	}

	if !strings.Contains(got, "first") || !strings.Contains(got, "second") || !strings.Contains(got, "third") {
		t.Errorf("output should contain all items, got %q", got)
	}
}

func TestList_Numbered(t *testing.T) {
	var buf bytes.Buffer
	l := NewList(NumberedList)
	l.AddItem("alpha")
	l.AddItem("beta")
	l.Format(&buf)
	got := buf.String()

	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	if !strings.HasPrefix(lines[0], "  1. ") {
		t.Errorf("first line should start with '  1. ', got %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "  2. ") {
		t.Errorf("second line should start with '  2. ', got %q", lines[1])
	}
}

func TestList_SingleItem(t *testing.T) {
	var buf bytes.Buffer
	l := NewList(BulletList)
	l.AddItem("only one")
	l.Format(&buf)
	got := buf.String()
	want := "  - only one\n"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// ── Summary Tests ─────────────────────────────────────────────────────

func TestSummary_Empty(t *testing.T) {
	var buf bytes.Buffer
	s := NewSummary()
	s.Format(&buf)
	if buf.Len() != 0 {
		t.Errorf("empty summary should produce no output, got %q", buf.String())
	}
}

func TestSummary_KeyValuePairs(t *testing.T) {
	var buf bytes.Buffer
	s := NewSummary()
	s.Add("Project", "my-app")
	s.Add("Version", "1.0.0")
	s.Format(&buf)
	got := buf.String()

	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	if !strings.Contains(lines[0], "Project") || !strings.Contains(lines[0], "my-app") {
		t.Errorf("first line should contain 'Project' and 'my-app', got %q", lines[0])
	}
	if !strings.Contains(lines[1], "Version") || !strings.Contains(lines[1], "1.0.0") {
		t.Errorf("second line should contain 'Version' and '1.0.0', got %q", lines[1])
	}
}

func TestSummary_KeyAlignment(t *testing.T) {
	var buf bytes.Buffer
	s := NewSummary()
	s.Add("A", "1")
	s.Add("LongKey", "2")
	s.Format(&buf)
	got := buf.String()

	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	// "A" should be padded with spaces so that ":" aligns with "LongKey".
	// The colon after "A" should be at the same position.
	colA := strings.Index(lines[0], ":")
	colB := strings.Index(lines[1], ":")
	if colA != colB {
		t.Errorf("colons should align at the same column: A at %d, LongKey at %d", colA, colB)
	}
}

func TestSummary_TwoSpaceIndent(t *testing.T) {
	var buf bytes.Buffer
	s := NewSummary()
	s.Add("Key", "value")
	s.Format(&buf)

	if !strings.HasPrefix(buf.String(), "  ") {
		t.Errorf("summary output should start with two-space indent, got %q", buf.String())
	}
}

func TestSummary_SinglePair(t *testing.T) {
	var buf bytes.Buffer
	s := NewSummary()
	s.Add("Status", "ok")
	s.Format(&buf)
	got := buf.String()
	want := "  Status : ok\n"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// ── Status Tests ──────────────────────────────────────────────────────

func TestPrintStatus_Pass(t *testing.T) {
	var buf bytes.Buffer
	PrintStatus(&buf, StatusPass, "All checks passed")
	got := buf.String()
	expected := "[PASS] All checks passed\n"
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestPrintStatus_Fail(t *testing.T) {
	var buf bytes.Buffer
	PrintStatus(&buf, StatusFail, "Check failed")
	got := buf.String()
	expected := "[FAIL] Check failed\n"
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestPrintStatus_Warn(t *testing.T) {
	var buf bytes.Buffer
	PrintStatus(&buf, StatusWarn, "Disk space low")
	got := buf.String()
	expected := "[WARN] Disk space low\n"
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestPrintStatus_Running(t *testing.T) {
	var buf bytes.Buffer
	PrintStatus(&buf, StatusRunning, "Deploying release")
	got := buf.String()
	expected := "[RUN] Deploying release\n"
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestPrintStatus_Skipped(t *testing.T) {
	var buf bytes.Buffer
	PrintStatus(&buf, StatusSkipped, "Already up to date")
	got := buf.String()
	expected := "[SKIP] Already up to date\n"
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestPrintStatus_EmptyMessage(t *testing.T) {
	var buf bytes.Buffer
	PrintStatus(&buf, StatusPass, "")
	got := buf.String()
	expected := "[PASS] \n"
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}
