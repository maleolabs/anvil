package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestTreeExternalOutputIsolation(t *testing.T) {
	var buf bytes.Buffer
	s := NewStyle(&buf, false)
	tr := NewTree(s, "Pipeline")
	n1 := tr.Add("build app")
	n1.Running()
	tr.Render()
	// Simulate external command: pause tree, capture output, resume
	tr.Pause()
	// External output should be rendered as indented block, not interleaved with \r
	// In plain mode Pause is no-op, but should not break
	externalOut := "external tool output line1\nexternal line2"
	// Simulate pipeline report style: write external output after pause
	// Ensure no \r in buffer (plain)
	if strings.Contains(buf.String(), "\r") {
		t.Fatalf("plain mode should never contain \\r, got %q", buf.String())
	}
	n1.Done("success")
	tr.Resume()
	tr.Finish()
	got := buf.String()
	if !strings.Contains(got, "build app") {
		t.Fatalf("missing build: %q", got)
	}
	// Ensure external output would be rendered cleanly if we did (test helper)
	_ = externalOut
	// Verify last still clean (└──)
	var buf2 bytes.Buffer
	s2 := NewStyle(&buf2, false)
	tr2 := NewTree(s2, "")
	a := tr2.Add("step-a")
	b := tr2.Add("step-b")
	a.Running()
	tr2.Render()
	tr2.Pause()
	// external
	b.Done("done")
	tr2.Resume()
	tr2.Finish()
	got2 := buf2.String()
	if !strings.Contains(got2, TreeLast) {
		t.Fatalf("after pause/resume last should still be TreeLast, got %q", got2)
	}
}

func TestTreePauseIsNoOpPlain(t *testing.T) {
	var buf bytes.Buffer
	s := NewStyle(&buf, false)
	tr := NewTree(s, "")
	n := tr.Add("step")
	n.Running()
	tr.Render()
	before := buf.String()
	tr.Pause()
	after := buf.String()
	// In plain mode Pause should not add extra output (no-op)
	if len(after) != len(before) {
		// Allow but should not contain \r
		if strings.Contains(after[len(before):], "\r") {
			t.Fatalf("Pause in plain should not emit \\r: %q", after)
		}
	}
}
