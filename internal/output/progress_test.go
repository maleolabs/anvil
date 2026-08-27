package output

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// ── PlainProgressReporter (Phase 1 UX feedback) ─────────────────────

// TestPlainProgressReporter_PipelineStart verifies that PipelineStart writes
// the pipeline header with environment when provided.
func TestPlainProgressReporter_PipelineStart_WithEnv(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainProgressReporter(&buf)

	r.PipelineStart("build", "production")

	want := "Pipeline: build (production)\n"
	if got := buf.String(); got != want {
		t.Errorf("PipelineStart() = %q, want %q", got, want)
	}
}

// TestPlainProgressReporter_PipelineStart_NoEnv verifies that PipelineStart
// writes the pipeline header without environment when env is empty.
func TestPlainProgressReporter_PipelineStart_NoEnv(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainProgressReporter(&buf)

	r.PipelineStart("build", "")

	want := "Pipeline: build\n"
	if got := buf.String(); got != want {
		t.Errorf("PipelineStart() = %q, want %q", got, want)
	}
}

// TestPlainProgressReporter_PipelineComplete verifies the success summary.
func TestPlainProgressReporter_PipelineComplete(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainProgressReporter(&buf)

	r.PipelineComplete("build", 26400*time.Millisecond)

	want := "Pipeline completed in 26.4s\n"
	if got := buf.String(); got != want {
		t.Errorf("PipelineComplete() = %q, want %q", got, want)
	}
}

// TestPlainProgressReporter_PipelineFailed verifies the failure summary.
func TestPlainProgressReporter_PipelineFailed(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainProgressReporter(&buf)

	r.PipelineFailed("build", 5300*time.Millisecond)

	want := "Pipeline failed in 5.3s\n"
	if got := buf.String(); got != want {
		t.Errorf("PipelineFailed() = %q, want %q", got, want)
	}
}

// TestPlainProgressReporter_StageStart verifies the stage header line.
func TestPlainProgressReporter_StageStart(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainProgressReporter(&buf)

	r.StageStart("dependencies", 0, 3)

	want := "  Stage: dependencies\n"
	if got := buf.String(); got != want {
		t.Errorf("StageStart() = %q, want %q", got, want)
	}
}

// TestPlainProgressReporter_StageComplete verifies the stage completion line.
func TestPlainProgressReporter_StageComplete(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainProgressReporter(&buf)

	r.StageComplete("compile", 1200*time.Millisecond)

	want := "  Stage: compile done (1.2s)\n"
	if got := buf.String(); got != want {
		t.Errorf("StageComplete() = %q, want %q", got, want)
	}
}

// TestPlainProgressReporter_StageFailed verifies the stage failure line.
func TestPlainProgressReporter_StageFailed(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainProgressReporter(&buf)

	r.StageFailed("test", 800*time.Millisecond)

	want := "  Stage: test failed (800ms)\n"
	if got := buf.String(); got != want {
		t.Errorf("StageFailed() = %q, want %q", got, want)
	}
}

// TestPlainProgressReporter_StageSkipped verifies the stage skipped line.
func TestPlainProgressReporter_StageSkipped(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainProgressReporter(&buf)

	r.StageSkipped("deploy")

	want := "  Stage: deploy (skipped)\n"
	if got := buf.String(); got != want {
		t.Errorf("StageSkipped() = %q, want %q", got, want)
	}
}

// TestPlainProgressReporter_TaskStart verifies the task start line with
// ellipsis indicator.
func TestPlainProgressReporter_TaskStart(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainProgressReporter(&buf)

	r.TaskStart("download", 0, 2)

	want := "    Task: download...\n"
	if got := buf.String(); got != want {
		t.Errorf("TaskStart() = %q, want %q", got, want)
	}
}

// TestPlainProgressReporter_TaskComplete verifies the task success line
// with checkmark and duration.
func TestPlainProgressReporter_TaskComplete(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainProgressReporter(&buf)

	r.TaskComplete("download", 2100*time.Millisecond)

	want := "    Task: download ✓ (2.1s)\n"
	if got := buf.String(); got != want {
		t.Errorf("TaskComplete() = %q, want %q", got, want)
	}
}

// TestPlainProgressReporter_TaskFailed verifies the task failure line
// with cross mark, duration, exit code, and the first line of stderr
// indented below as the failure reason.
func TestPlainProgressReporter_TaskFailed(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainProgressReporter(&buf)

	r.TaskFailed("verify", 500*time.Millisecond, 1, "permission denied\n")

	want := "    Task: verify ✗ (500ms) - exit code 1\n      permission denied\n"
	if got := buf.String(); got != want {
		t.Errorf("TaskFailed() = %q, want %q", got, want)
	}
}

// TestPlainProgressReporter_TaskFailed_MultiLineStderr verifies that only
// the first line of stderr is printed as the failure reason.
func TestPlainProgressReporter_TaskFailed_MultiLineStderr(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainProgressReporter(&buf)

	r.TaskFailed("verify", 500*time.Millisecond, 1, "FAIL: TestFoo\nextra line\n")

	want := "    Task: verify ✗ (500ms) - exit code 1\n      FAIL: TestFoo\n"
	if got := buf.String(); got != want {
		t.Errorf("TaskFailed() = %q, want %q", got, want)
	}
}

// TestPlainProgressReporter_TaskFailed_EmptyStderr verifies that no reason
// block is printed when stderr is empty.
func TestPlainProgressReporter_TaskFailed_EmptyStderr(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainProgressReporter(&buf)

	r.TaskFailed("verify", 500*time.Millisecond, 1, "")

	want := "    Task: verify ✗ (500ms) - exit code 1\n"
	if got := buf.String(); got != want {
		t.Errorf("TaskFailed() = %q, want %q", got, want)
	}
}

// TestPlainProgressReporter_TaskSkipped verifies the task skipped line.
func TestPlainProgressReporter_TaskSkipped(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainProgressReporter(&buf)

	r.TaskSkipped("deploy")

	want := "    Task: deploy (skipped)\n"
	if got := buf.String(); got != want {
		t.Errorf("TaskSkipped() = %q, want %q", got, want)
	}
}

// TestPlainProgressReporter_NilWriter verifies that all methods are no-ops
// when the writer is nil (should not panic).
func TestPlainProgressReporter_NilWriter(t *testing.T) {
	r := NewPlainProgressReporter(nil)

	// None of these should panic.
	r.PipelineStart("build", "prod")
	r.PipelineComplete("build", time.Second)
	r.PipelineFailed("build", time.Second)
	r.StageStart("s", 0, 1)
	r.StageComplete("s", time.Second)
	r.StageFailed("s", time.Second)
	r.StageSkipped("s")
	r.TaskStart("t", 0, 1)
	r.TaskComplete("t", time.Second)
	r.TaskFailed("t", time.Second, 1, "")
	r.TaskSkipped("t")
}

// TestPlainProgressReporter_FullSequence verifies a realistic full pipeline
// execution produces the expected multi-line output.
func TestPlainProgressReporter_FullSequence(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainProgressReporter(&buf)

	r.PipelineStart("build", "production")
	r.StageStart("dependencies", 0, 2)
	r.TaskStart("download", 0, 2)
	r.TaskComplete("download", 2100*time.Millisecond)
	r.TaskStart("verify", 1, 2)
	r.TaskFailed("verify", 500*time.Millisecond, 1, "checksum mismatch")
	r.StageFailed("dependencies", 2600*time.Millisecond)
	r.StageSkipped("compile")
	r.PipelineFailed("build", 2700*time.Millisecond)

	output := buf.String()
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")

	expectedLines := []string{
		"Pipeline: build (production)",
		"  Stage: dependencies",
		"    Task: download...",
		"    Task: download ✓ (2.1s)",
		"    Task: verify...",
		"    Task: verify ✗ (500ms) - exit code 1",
		"      checksum mismatch",
		"  Stage: dependencies failed (2.6s)",
		"  Stage: compile (skipped)",
		"Pipeline failed in 2.7s",
	}

	if len(lines) != len(expectedLines) {
		t.Fatalf("expected %d lines, got %d:\n%s", len(expectedLines), len(lines), output)
	}

	for i, want := range expectedLines {
		if lines[i] != want {
			t.Errorf("line %d = %q, want %q", i, lines[i], want)
		}
	}
}

// ── Factory Function (Phase 2) ──────────────────────────────────────

// TestNewProgressReporter_NonTerminal_ReturnsPlain verifies that the factory
// returns PlainProgressReporter for non-terminal writers (bytes.Buffer).
func TestNewProgressReporter_NonTerminal_ReturnsPlain(t *testing.T) {
	var buf bytes.Buffer
	r := NewProgressReporter(&buf)

	if _, ok := r.(*PlainProgressReporter); !ok {
		t.Errorf("expected *PlainProgressReporter for non-terminal writer, got %T", r)
	}
}

// TestNewProgressReporter_WithInteractive_ForcesInteractive verifies that
// WithInteractive(true) forces InteractiveProgressReporter even for non-terminals.
func TestNewProgressReporter_WithInteractive_ForcesInteractive(t *testing.T) {
	var buf bytes.Buffer
	r := NewProgressReporter(&buf, WithInteractive(true))

	if _, ok := r.(*InteractiveProgressReporter); !ok {
		t.Errorf("expected *InteractiveProgressReporter with WithInteractive(true), got %T", r)
	}
}

// TestNewProgressReporter_WithInteractive_False_ForcesPlain verifies that
// WithInteractive(false) forces PlainProgressReporter.
func TestNewProgressReporter_WithInteractive_False_ForcesPlain(t *testing.T) {
	var buf bytes.Buffer
	r := NewProgressReporter(&buf, WithInteractive(false))

	if _, ok := r.(*PlainProgressReporter); !ok {
		t.Errorf("expected *PlainProgressReporter with WithInteractive(false), got %T", r)
	}
}

// TestNewProgressReporter_NilWriter verifies that the factory handles nil
// writers gracefully.
func TestNewProgressReporter_NilWriter(t *testing.T) {
	r := NewProgressReporter(nil)

	// Should return a valid reporter (PlainProgressReporter with nil writer).
	if r == nil {
		t.Fatal("NewProgressReporter(nil) returned nil")
	}

	// Should not panic.
	r.PipelineStart("build", "prod")
	r.PipelineComplete("build", time.Second)
}

// TestPlainProgressReporter_NoANSICodes verifies that the output contains
// no ANSI escape codes (plain text only).
func TestPlainProgressReporter_NoANSICodes(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainProgressReporter(&buf)

	r.PipelineStart("build", "prod")
	r.TaskComplete("download", time.Second)
	r.PipelineComplete("build", time.Second)

	if strings.Contains(buf.String(), "\x1b") {
		t.Errorf("output must not contain ANSI escape codes, got %q", buf.String())
	}
}

// TestFormatDuration verifies the duration formatting helper.
func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"sub-second ms", 450 * time.Millisecond, "450ms"},
		{"exactly 1s", 1000 * time.Millisecond, "1.0s"},
		{"decimal seconds", 2100 * time.Millisecond, "2.1s"},
		{"large duration", 65400 * time.Millisecond, "65.4s"},
		{"zero", 0, "0s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDuration(tt.d)
			if got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}
