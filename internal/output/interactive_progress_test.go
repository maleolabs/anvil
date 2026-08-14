package output

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// ── InteractiveProgressReporter (Phase 2 UX feedback) ───────────────

// TestInteractiveProgressReporter_PipelineStart verifies the pipeline header
// with environment is emitted with tree-style formatting.
func TestInteractiveProgressReporter_PipelineStart_WithEnv(t *testing.T) {
	var buf bytes.Buffer
	r := NewInteractiveProgressReporter(&buf)

	r.PipelineStart("build", "production")

	output := buf.String()
	if !strings.Contains(output, "Pipeline: build") {
		t.Errorf("PipelineStart() missing pipeline name, got %q", output)
	}
	if !strings.Contains(output, "production") {
		t.Errorf("PipelineStart() missing environment, got %q", output)
	}
	if !strings.Contains(output, "▶") {
		t.Errorf("PipelineStart() missing play icon, got %q", output)
	}
}

// TestInteractiveProgressReporter_PipelineStart_NoEnv verifies the pipeline
// header without environment.
func TestInteractiveProgressReporter_PipelineStart_NoEnv(t *testing.T) {
	var buf bytes.Buffer
	r := NewInteractiveProgressReporter(&buf)

	r.PipelineStart("build", "")

	output := buf.String()
	if !strings.Contains(output, "Pipeline: build") {
		t.Errorf("PipelineStart() missing pipeline name, got %q", output)
	}
	if strings.Contains(output, "()") {
		t.Errorf("PipelineStart() should not show empty parens, got %q", output)
	}
}

// TestInteractiveProgressReporter_PipelineComplete verifies the success
// summary with green checkmark.
func TestInteractiveProgressReporter_PipelineComplete(t *testing.T) {
	var buf bytes.Buffer
	r := NewInteractiveProgressReporter(&buf)

	r.PipelineComplete("build", 26400*time.Millisecond)

	output := buf.String()
	if !strings.Contains(output, iconSuccess) {
		t.Errorf("PipelineComplete() missing success icon, got %q", output)
	}
	if !strings.Contains(output, "26.4s") {
		t.Errorf("PipelineComplete() missing duration, got %q", output)
	}
	if !strings.Contains(output, "Pipeline completed") {
		t.Errorf("PipelineComplete() missing completion text, got %q", output)
	}
}

// TestInteractiveProgressReporter_PipelineFailed verifies the failure
// summary with red cross.
func TestInteractiveProgressReporter_PipelineFailed(t *testing.T) {
	var buf bytes.Buffer
	r := NewInteractiveProgressReporter(&buf)

	r.PipelineFailed("build", 5300*time.Millisecond)

	output := buf.String()
	if !strings.Contains(output, iconFailure) {
		t.Errorf("PipelineFailed() missing failure icon, got %q", output)
	}
	if !strings.Contains(output, "5.3s") {
		t.Errorf("PipelineFailed() missing duration, got %q", output)
	}
	if !strings.Contains(output, "Pipeline failed") {
		t.Errorf("PipelineFailed() missing failure text, got %q", output)
	}
}

// TestInteractiveProgressReporter_StageTree verifies that stages use
// correct tree connectors (├─ for non-last, └─ for last).
func TestInteractiveProgressReporter_StageTree(t *testing.T) {
	var buf bytes.Buffer
	r := NewInteractiveProgressReporter(&buf)

	r.PipelineStart("build", "prod")
	r.StageStart("dependencies", 0, 3)
	r.StageStart("compile", 1, 3)
	r.StageStart("optimize", 2, 3)

	output := buf.String()

	// First two stages should use ├─
	if !strings.Contains(output, "├─ Stage: dependencies") {
		t.Errorf("first stage should use ├─ connector, got %q", output)
	}
	if !strings.Contains(output, "├─ Stage: compile") {
		t.Errorf("middle stage should use ├─ connector, got %q", output)
	}

	// Last stage should use └─
	if !strings.Contains(output, "└─ Stage: optimize") {
		t.Errorf("last stage should use └─ connector, got %q", output)
	}
}

// TestInteractiveProgressReporter_TaskTree verifies that tasks use
// correct tree connectors within a stage.
func TestInteractiveProgressReporter_TaskTree(t *testing.T) {
	var buf bytes.Buffer
	r := NewInteractiveProgressReporter(&buf)

	r.PipelineStart("build", "")
	r.StageStart("dependencies", 0, 1)
	r.TaskStart("download", 0, 2)
	r.TaskComplete("download", 2100*time.Millisecond)
	r.TaskStart("verify", 1, 2)
	r.TaskComplete("verify", 800*time.Millisecond)

	output := buf.String()

	// First task should use ├─
	if !strings.Contains(output, "├─") {
		t.Errorf("first task should use ├─ connector, got %q", output)
	}

	// Last task should use └─
	if !strings.Contains(output, "└─") {
		t.Errorf("last task should use └─ connector, got %q", output)
	}

	// Success icon should be present
	if !strings.Contains(output, iconSuccess) {
		t.Errorf("output should contain success icon, got %q", output)
	}

	// Duration should be shown
	if !strings.Contains(output, "2.1s") {
		t.Errorf("output should contain task duration, got %q", output)
	}
}

// TestInteractiveProgressReporter_SkippedStage verifies the skipped stage
// indicator with yellow icon.
func TestInteractiveProgressReporter_SkippedStage(t *testing.T) {
	var buf bytes.Buffer
	r := NewInteractiveProgressReporter(&buf)

	r.PipelineStart("build", "")
	r.StageStart("dependencies", 0, 2)
	r.StageComplete("dependencies", time.Second)
	r.StageSkipped("compile")

	output := buf.String()
	if !strings.Contains(output, iconSkip) {
		t.Errorf("StageSkipped() missing skip icon, got %q", output)
	}
	if !strings.Contains(output, "(skipped)") {
		t.Errorf("StageSkipped() missing skipped text, got %q", output)
	}
	if !strings.Contains(output, "compile") {
		t.Errorf("StageSkipped() missing stage name, got %q", output)
	}
}

// TestInteractiveProgressReporter_SkippedTask verifies the skipped task
// indicator.
func TestInteractiveProgressReporter_SkippedTask(t *testing.T) {
	var buf bytes.Buffer
	r := NewInteractiveProgressReporter(&buf)

	r.PipelineStart("build", "")
	r.StageStart("compile", 0, 1)
	r.TaskStart("build-linux", 0, 2)
	r.TaskComplete("build-linux", time.Second)
	r.TaskSkipped("build-darwin")

	output := buf.String()
	if !strings.Contains(output, iconSkip) {
		t.Errorf("TaskSkipped() missing skip icon, got %q", output)
	}
	if !strings.Contains(output, "build-darwin") {
		t.Errorf("TaskSkipped() missing task name, got %q", output)
	}
}

// TestInteractiveProgressReporter_TaskFailed verifies that task failure
// shows exit code and stderr.
func TestInteractiveProgressReporter_TaskFailed(t *testing.T) {
	var buf bytes.Buffer
	r := NewInteractiveProgressReporter(&buf)

	r.PipelineStart("build", "")
	r.StageStart("test", 0, 1)
	r.TaskStart("unit-tests", 0, 1)
	r.TaskFailed("unit-tests", 500*time.Millisecond, 1, "FAIL: TestFoo\nextra line")

	output := buf.String()
	if !strings.Contains(output, iconFailure) {
		t.Errorf("TaskFailed() missing failure icon, got %q", output)
	}
	if !strings.Contains(output, "Exit code: 1") {
		t.Errorf("TaskFailed() missing exit code, got %q", output)
	}
	if !strings.Contains(output, "FAIL: TestFoo") {
		t.Errorf("TaskFailed() missing stderr first line, got %q", output)
	}
	// Should not contain the second line.
	if strings.Contains(output, "extra line") {
		t.Errorf("TaskFailed() should only show first line of stderr, got %q", output)
	}
}

// TestInteractiveProgressReporter_NoANSICodes verifies that when the writer
// is a non-terminal (bytes.Buffer), no ANSI escape codes are emitted.
func TestInteractiveProgressReporter_NoANSICodes(t *testing.T) {
	var buf bytes.Buffer
	r := NewInteractiveProgressReporter(&buf)

	r.PipelineStart("build", "prod")
	r.StageStart("compile", 0, 1)
	r.TaskStart("build", 0, 1)
	r.TaskComplete("build", time.Second)
	r.StageComplete("compile", time.Second)
	r.PipelineComplete("build", time.Second)

	if strings.Contains(buf.String(), "\x1b") {
		t.Errorf("output must not contain ANSI escape codes for non-terminal writer, got %q", buf.String())
	}
}

// TestInteractiveProgressReporter_TreeStructure verifies the full tree
// output structure matches expected format.
func TestInteractiveProgressReporter_TreeStructure(t *testing.T) {
	var buf bytes.Buffer
	r := NewInteractiveProgressReporter(&buf)

	r.PipelineStart("build", "production")
	r.StageStart("dependencies", 0, 3)
	r.TaskStart("download", 0, 2)
	r.TaskComplete("download", 2100*time.Millisecond)
	r.TaskStart("verify", 1, 2)
	r.TaskComplete("verify", 800*time.Millisecond)
	r.StageComplete("dependencies", 2900*time.Millisecond)

	r.StageStart("compile", 1, 3)
	r.TaskStart("build-linux", 0, 1)
	r.TaskComplete("build-linux", 12000*time.Millisecond)
	r.StageComplete("compile", 12000*time.Millisecond)

	r.StageStart("optimize", 2, 3)
	r.TaskStart("cache-clear", 0, 1)
	r.TaskComplete("cache-clear", 1100*time.Millisecond)
	r.StageComplete("optimize", 1100*time.Millisecond)

	r.PipelineComplete("build", 16000*time.Millisecond)

	output := buf.String()
	lines := strings.Split(output, "\n")

	// Verify key structural elements exist.
	structuralChecks := []struct {
		name    string
		want    string
	}{
		{"play icon", "▶"},
		{"pipeline name", "Pipeline: build"},
		{"first stage connector", "├─ Stage: dependencies"},
		{"middle stage connector", "├─ Stage: compile"},
		{"last stage connector", "└─ Stage: optimize"},
		{"success icon", iconSuccess},
		{"completion text", "Pipeline completed"},
	}

	for _, check := range structuralChecks {
		if !strings.Contains(output, check.want) {
			t.Errorf("tree structure missing %s (%q), output:\n%s", check.name, check.want, output)
		}
	}

	// Verify we have a reasonable number of lines.
	if len(lines) < 10 {
		t.Errorf("expected at least 10 lines, got %d:\n%s", len(lines), output)
	}
}

// TestInteractiveProgressReporter_VerticalLine verifies that vertical
// connector lines (│) appear for non-last stages.
func TestInteractiveProgressReporter_VerticalLine(t *testing.T) {
	var buf bytes.Buffer
	r := NewInteractiveProgressReporter(&buf)

	r.PipelineStart("build", "")
	r.StageStart("dependencies", 0, 2)
	r.TaskStart("download", 0, 1)
	r.TaskComplete("download", time.Second)
	r.StageComplete("dependencies", time.Second)

	output := buf.String()

	// Non-last stage tasks should have vertical line prefix.
	if !strings.Contains(output, treeLine) {
		t.Errorf("output should contain vertical line connector for non-last stage, got %q", output)
	}
}

// TestInteractiveProgressReporter_FullSequence_Failure verifies a realistic
// pipeline failure scenario with skipped stages.
func TestInteractiveProgressReporter_FullSequence_Failure(t *testing.T) {
	var buf bytes.Buffer
	r := NewInteractiveProgressReporter(&buf)

	r.PipelineStart("build", "production")
	r.StageStart("dependencies", 0, 3)
	r.TaskStart("download", 0, 2)
	r.TaskComplete("download", 2100*time.Millisecond)
	r.TaskStart("verify", 1, 2)
	r.TaskFailed("verify", 500*time.Millisecond, 1, "go mod verify failed")
	r.StageFailed("dependencies", 2600*time.Millisecond)
	r.StageSkipped("compile")
	r.StageSkipped("optimize")
	r.PipelineFailed("build", 2700*time.Millisecond)

	output := buf.String()

	// Verify failure elements.
	checks := []string{
		iconFailure,        // failure icon
		"verify",           // failed task name
		"Exit code: 1",     // exit code
		"go mod verify failed", // stderr
		iconSkip,           // skip icon for skipped stages
		"compile (skipped)",
		"optimize (skipped)",
		"Pipeline failed",
	}

	for _, want := range checks {
		if !strings.Contains(output, want) {
			t.Errorf("failure sequence missing %q in output:\n%s", want, output)
		}
	}
}

// TestStageConnector verifies the stageConnector helper.
func TestStageConnector(t *testing.T) {
	tests := []struct {
		name  string
		index int
		total int
		want  string
	}{
		{"first of three", 0, 3, treeTee},
		{"middle of three", 1, 3, treeTee},
		{"last of three", 2, 3, treeCorner},
		{"single stage", 0, 1, treeCorner},
		{"first of two", 0, 2, treeTee},
		{"last of two", 1, 2, treeCorner},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stageConnector(tt.index, tt.total)
			if got != tt.want {
				t.Errorf("stageConnector(%d, %d) = %q, want %q", tt.index, tt.total, got, tt.want)
			}
		})
	}
}

// TestInteractiveProgressReporter_NilSafety verifies that methods don't
// panic when called in unexpected order.
func TestInteractiveProgressReporter_NilSafety(t *testing.T) {
	var buf bytes.Buffer
	r := NewInteractiveProgressReporter(&buf)

	// These should not panic even without prior PipelineStart.
	r.TaskComplete("task", time.Second)
	r.TaskFailed("task", time.Second, 1, "")
	r.TaskSkipped("task")
	r.StageComplete("stage", time.Second)
	r.StageFailed("stage", time.Second)
	r.PipelineComplete("pipeline", time.Second)
	r.PipelineFailed("pipeline", time.Second)
}
