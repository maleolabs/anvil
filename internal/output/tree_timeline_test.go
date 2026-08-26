package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestTreePlain(t *testing.T) {
	var buf bytes.Buffer
	s := NewStyle(&buf, false)
	tr := NewTree(s, "Root")
	n := tr.Add("step-one")
	n.Done("done detail")
	tr.Render()
	got := buf.String()
	if !strings.Contains(got, "Root") || !strings.Contains(got, "step-one") {
		t.Fatalf("Tree missing content: %q", got)
	}
	// Single node is last -> should use TreeLast
	if !strings.Contains(got, TreeLast) {
		t.Fatalf("Tree single node should use TreeLast (└──), got %q", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("non-TTY should be plain: %q", got)
	}
}

func TestTimeline(t *testing.T) {
	var buf bytes.Buffer
	s := NewStyle(&buf, false)
	tl := NewTimeline(s)
	tl.Add("▸", "first", nil)
	tl.Add("•", "second", s.Info)
	tl.Render()
	got := buf.String()
	if !strings.Contains(got, "first") || !strings.Contains(got, "second") {
		t.Fatalf("Timeline missing: %q", got)
	}
}

func TestHeader(t *testing.T) {
	var buf bytes.Buffer
	s := NewStyle(&buf, false)
	h := NewHeader(s, "Repository")
	h.Add("Name", "myproj").Add("Namespace", "anvil-cli").Pipeline("Bootstrap")
	h.Render()
	got := buf.String()
	if !strings.Contains(got, "Repository") || !strings.Contains(got, "myproj") || !strings.Contains(got, "Bootstrap") {
		t.Fatalf("Header missing: %q", got)
	}
	if !strings.HasPrefix(got, "\n") {
		t.Fatalf("Header missing leading blank: %q", got)
	}
}

func TestTreeLastSymbolMulti(t *testing.T) {
	var buf bytes.Buffer
	s := NewStyle(&buf, false)
	tr := NewTree(s, "Root")
	n1 := tr.Add("step-one")
	n2 := tr.Add("step-two")
	n1.Done("")
	n2.Done("")
	tr.Render()
	got := buf.String()
	// First should be ├──, last should be └──
	if strings.Count(got, TreeBranch) != 1 {
		t.Fatalf("expected 1 TreeBranch for first, got %q count %d", got, strings.Count(got, TreeBranch))
	}
	if strings.Count(got, TreeLast) != 1 {
		t.Fatalf("expected 1 TreeLast for last, got %q count %d", got, strings.Count(got, TreeLast))
	}
	// Detail under last should use spaces not │
	var buf2 bytes.Buffer
	s2 := NewStyle(&buf2, false)
	tr2 := NewTree(s2, "")
	a := tr2.Add("a")
	b := tr2.Add("b")
	a.Done("detail a")
	b.Done("detail b")
	tr2.Render()
	got2 := buf2.String()
	// detail under b (last) should not contain │
	lines := strings.Split(got2, "\n")
	found := false
	for _, l := range lines {
		if strings.Contains(l, "detail b") {
			found = true
			if strings.Contains(l, TreeVert) {
				t.Fatalf("detail under last should use spaces not │, got %q", l)
			}
		}
	}
	if !found {
		t.Fatalf("detail b not found")
	}
}
