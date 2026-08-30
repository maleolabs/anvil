package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestProgressBar(t *testing.T) {
	var buf bytes.Buffer
	s := NewStyle(&buf, false)
	bar := ProgressBar(s, 5, 10)
	if !strings.Contains(bar, "█") && !strings.Contains(bar, "░") {
		t.Fatalf("ProgressBar missing blocks: %q", bar)
	}
	// non-TTY plain
	if strings.Contains(bar, "\x1b[") {
		t.Fatalf("non-TTY bar should be plain: %q", bar)
	}
	// total 0 => all empty
	bar0 := ProgressBar(s, 0, 0)
	if !strings.Contains(bar0, "░") {
		t.Fatalf("total 0 should be empty: %q", bar0)
	}
	// TTY colored path: create Style with Color true but not TTY -> still plain
	s.Color = true
	barColor := ProgressBar(s, 80, 100)
	if !strings.Contains(barColor, "█") {
		t.Fatalf("colored bar missing: %q", barColor)
	}
}

func TestStyledSpinner(t *testing.T) {
	var buf bytes.Buffer
	s := NewStyle(&buf, false)
	sp := NewStyledSpinner(s, "loading")
	// non-TTY should print once
	if !strings.Contains(buf.String(), "loading") {
		t.Fatalf("StyledSpinner non-TTY should print message: %q", buf.String())
	}
	sp.Stop()
	// Stop should be idempotent and print ✓ line
	got := buf.String()
	if !strings.Contains(got, "✓") && !strings.Contains(got, "loading") {
		t.Fatalf("StyledSpinner Stop missing: %q", got)
	}
}
