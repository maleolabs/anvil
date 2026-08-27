package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestContainerVerticalPadding(t *testing.T) {
	var out bytes.Buffer
	st := NewStyle(&out, false)
	Container(st, "hello\nworld")
	got := out.String()
	if !strings.Contains(got, "  hello") || !strings.Contains(got, "  world") {
		t.Fatalf("Container missing margin: %q", got)
	}
	if !strings.HasPrefix(got, "\n") {
		t.Fatalf("Container missing leading blank line: %q", got)
	}
	if !strings.HasSuffix(got, "\n\n") {
		t.Fatalf("Container missing trailing blank line: %q", got)
	}
}

func TestStyleWidthFallback(t *testing.T) {
	var buf bytes.Buffer
	s := NewStyle(&buf, false)
	if s.Width != 0 {
		t.Fatalf("non-TTY Width must be 0, got %d", s.Width)
	}
}

func TestStyleRawForMachineJSON(t *testing.T) {
	var buf bytes.Buffer
	s := NewStyle(&buf, false)
	if s.Raw() == s.W {
		t.Fatalf("Raw should be unwrapped (different from W) for machine JSON bypass")
	}
	var out2 bytes.Buffer
	s2 := &Style{W: newMarginWriter(&out2), raw: &out2}
	s2.Raw().Write([]byte("json"))
	if out2.String() != "json" {
		t.Fatalf("Raw write should be plain without margin, got %q", out2.String())
	}
	var out3 bytes.Buffer
	s3 := NewStyle(&out3, false)
	s3.W.Write([]byte("human\n"))
	if !strings.HasPrefix(out3.String(), "  human") {
		t.Fatalf("W should have margin, got %q", out3.String())
	}
}
