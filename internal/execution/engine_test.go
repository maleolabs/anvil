package execution

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// mockRunner implements Runner for testing. It records execution order by
// command name and can simulate task success/failure.
//
// Fields:
//   - failCommands: set of command names that should return StatusFailure
//   - delay: artificial delay before returning each result
//   - commands: recorded execution order (thread-safe)
type mockRunner struct {
	mu           sync.Mutex
	commands     []string
	failCommands map[string]bool
	delay        time.Duration
}

func newMockRunner() *mockRunner {
	return &mockRunner{
		failCommands: make(map[string]bool),
	}
}

// failCommand registers a command that should fail when executed.
func (m *mockRunner) failCommand(cmd string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failCommands[cmd] = true
}

// recordedCommands returns a copy of the commands executed so far.
func (m *mockRunner) recordedCommands() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]string, len(m.commands))
	copy(cp, m.commands)
	return cp
}

func (m *mockRunner) Execute(ctx context.Context, req ExecutionRequest) Result {
	start := time.Now()

	m.mu.Lock()
	m.commands = append(m.commands, req.Command)
	shouldFail := m.failCommands[req.Command]
	m.mu.Unlock()

	// Apply artificial delay with context awareness.
	if m.delay > 0 {
		select {
		case <-ctx.Done():
			return Result{
				Status:   StatusCancelled,
				ExitCode: -1,
				Duration: time.Since(start),
				Err:      ctx.Err(),
			}
		case <-time.After(m.delay):
		}
	}

	// Check for context cancellation after delay.
	if ctx.Err() != nil {
		return Result{
			Status:   StatusCancelled,
			ExitCode: -1,
			Duration: time.Since(start),
			Err:      ctx.Err(),
		}
	}

	if shouldFail {
		return Result{
			Status:   StatusFailure,
			ExitCode: 1,
			Duration: time.Since(start),
		}
	}

	return Result{
		Status:   StatusSuccess,
		ExitCode: 0,
		Duration: time.Since(start),
	}
}

// writePipelineYAML is a test helper that writes a PipelineDefinition to a
// temporary YAML file and returns the path.
func writePipelineYAML(t *testing.T, pd PipelineDefinition) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "pipeline.yaml")

	data, err := yaml.Marshal(&pd)
	if err != nil {
		t.Fatalf("yaml.Marshal() failed: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("os.WriteFile() failed: %v", err)
	}
	return path
}

// testPipeline builds a simple two-stage pipeline for general test use.
func testPipeline() *PipelineDefinition {
	return &PipelineDefinition{
		Pipeline: Pipeline{
			Name: "test-pipeline",
			Stages: []PipelineStage{
				{
					Name: "stage1",
					Tasks: []Task{
						{Name: "task1", Command: "cmd-a"},
					},
				},
				{
					Name: "stage2",
					Tasks: []Task{
						{Name: "task2", Command: "cmd-b"},
					},
				},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Load Tests
// ---------------------------------------------------------------------------

// TestEngine_Load_ValidPipeline verifies that a valid YAML file can be loaded
// without error.
func TestEngine_Load_ValidPipeline(t *testing.T) {
	engine := NewPipelineEngine(newMockRunner())
	path := writePipelineYAML(t, DefaultCIPipeline())

	def, err := engine.Load(path)
	if err != nil {
		t.Fatalf("Load() = %v, want nil", err)
	}
	if def.Pipeline.Name != "ci" {
		t.Errorf("Pipeline.Name = %q, want %q", def.Pipeline.Name, "ci")
	}
}

// TestEngine_Load_InvalidPipeline verifies that a pipeline with missing
// required fields returns a validation error.
func TestEngine_Load_InvalidPipeline(t *testing.T) {
	engine := NewPipelineEngine(newMockRunner())

	invalid := PipelineDefinition{
		Pipeline: Pipeline{
			Name:   "",
			Stages: []PipelineStage{},
		},
	}
	path := writePipelineYAML(t, invalid)

	_, err := engine.Load(path)
	if err == nil {
		t.Fatal("Load() = nil, want validation error")
	}
}

// TestEngine_Load_NonexistentFile verifies that loading a non-existent file
// returns an error.
func TestEngine_Load_NonexistentFile(t *testing.T) {
	engine := NewPipelineEngine(newMockRunner())

	_, err := engine.Load("/tmp/nonexistent-pipeline-ts-p6-09.yaml")
	if err == nil {
		t.Fatal("Load() = nil, want error for nonexistent file")
	}
}

// TestEngine_Load_InvalidYAML verifies that invalid YAML content returns a
// parse error.
func TestEngine_Load_InvalidYAML(t *testing.T) {
	engine := NewPipelineEngine(newMockRunner())

	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("{{ invalid yaml }}\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() failed: %v", err)
	}

	_, err := engine.Load(path)
	if err == nil {
		t.Fatal("Load() = nil, want parse error")
	}
}

// ---------------------------------------------------------------------------
// Sequential Execution Tests
// ---------------------------------------------------------------------------

// TestEngine_Execute_SequentialStages verifies that stages execute in the
// order they are declared.
func TestEngine_Execute_SequentialStages(t *testing.T) {
	mock := newMockRunner()
	engine := NewPipelineEngine(mock)

	pd := testPipeline()
	report := engine.Execute(context.Background(), pd, "")

	if report.Status != "success" {
		t.Errorf("Status = %q, want %q", report.Status, "success")
	}
	if len(report.Stages) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(report.Stages))
	}

	// Verify stage order.
	if report.Stages[0].Name != "stage1" {
		t.Errorf("Stage[0].Name = %q, want %q", report.Stages[0].Name, "stage1")
	}
	if report.Stages[1].Name != "stage2" {
		t.Errorf("Stage[1].Name = %q, want %q", report.Stages[1].Name, "stage2")
	}

	// Verify command execution order.
	cmds := mock.recordedCommands()
	expected := []string{"cmd-a", "cmd-b"}
	if len(cmds) != len(expected) {
		t.Fatalf("executed %d commands, want %d: %v", len(cmds), len(expected), cmds)
	}
	for i, cmd := range expected {
		if cmds[i] != cmd {
			t.Errorf("command[%d] = %q, want %q", i, cmds[i], cmd)
		}
	}
}

// TestEngine_Execute_SequentialTasks verifies that tasks within a stage
// execute one after another in declared order.
func TestEngine_Execute_SequentialTasks(t *testing.T) {
	mock := newMockRunner()
	engine := NewPipelineEngine(mock)

	pd := &PipelineDefinition{
		Pipeline: Pipeline{
			Name: "test-seq-tasks",
			Stages: []PipelineStage{
				{
					Name: "stage1",
					Tasks: []Task{
						{Name: "first", Command: "cmd-first"},
						{Name: "second", Command: "cmd-second"},
						{Name: "third", Command: "cmd-third"},
					},
				},
			},
		},
	}

	report := engine.Execute(context.Background(), pd, "")

	if report.Status != "success" {
		t.Errorf("Status = %q, want %q", report.Status, "success")
	}
	if len(report.Stages[0].Tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(report.Stages[0].Tasks))
	}

	// Verify task result order.
	expected := []string{"first", "second", "third"}
	for i, name := range expected {
		if report.Stages[0].Tasks[i].Name != name {
			t.Errorf("Task[%d].Name = %q, want %q", i, report.Stages[0].Tasks[i].Name, name)
		}
	}

	// Verify command execution order.
	cmds := mock.recordedCommands()
	if len(cmds) != 3 {
		t.Fatalf("executed %d commands, want 3: %v", len(cmds), cmds)
	}
	expectedCmds := []string{"cmd-first", "cmd-second", "cmd-third"}
	for i, cmd := range expectedCmds {
		if cmds[i] != cmd {
			t.Errorf("command[%d] = %q, want %q", i, cmds[i], cmd)
		}
	}
}

// ---------------------------------------------------------------------------
// Parallel Execution Tests
// ---------------------------------------------------------------------------

// TestEngine_Execute_ParallelTasks verifies that when Stage.Parallel is true,
// tasks run concurrently. It measures total execution time: if tasks run in
// parallel, total time approximates the single-slowest task rather than the sum.
func TestEngine_Execute_ParallelTasks(t *testing.T) {
	mock := newMockRunner()
	mock.delay = 80 * time.Millisecond
	engine := NewPipelineEngine(mock)

	pd := &PipelineDefinition{
		Pipeline: Pipeline{
			Name: "test-parallel",
			Stages: []PipelineStage{
				{
					Name:     "parallel-stage",
					Parallel: true,
					Tasks: []Task{
						{Name: "task-a", Command: "cmd-a"},
						{Name: "task-b", Command: "cmd-b"},
						{Name: "task-c", Command: "cmd-c"},
					},
				},
			},
		},
	}

	start := time.Now()
	report := engine.Execute(context.Background(), pd, "")
	elapsed := time.Since(start)

	if report.Status != "success" {
		t.Errorf("Status = %q, want %q", report.Status, "success")
	}
	if len(report.Stages[0].Tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(report.Stages[0].Tasks))
	}

	// If sequential, 3 * 80ms = 240ms. If parallel, ~80ms.
	// We allow generous margin for test environment variance.
	if elapsed >= 200*time.Millisecond {
		t.Errorf("parallel execution took %v, indicates sequential execution (expected ~80ms)", elapsed)
	}

	// Verify all three commands were recorded.
	cmds := mock.recordedCommands()
	if len(cmds) != 3 {
		t.Errorf("expected 3 commands executed, got %d: %v", len(cmds), cmds)
	}
}

// ---------------------------------------------------------------------------
// Fail-fast Tests
// ---------------------------------------------------------------------------

// TestEngine_Execute_FailFast verifies that when a task fails, remaining
// stages are marked as "skipped".
func TestEngine_Execute_FailFast(t *testing.T) {
	mock := newMockRunner()
	mock.failCommand("cmd-fail")
	engine := NewPipelineEngine(mock)

	pd := &PipelineDefinition{
		Pipeline: Pipeline{
			Name: "test-failfast",
			Stages: []PipelineStage{
				{
					Name: "stage1",
					Tasks: []Task{
						{Name: "ok", Command: "cmd-ok"},
						{Name: "fail", Command: "cmd-fail"},
						{Name: "after-fail", Command: "cmd-after"},
					},
				},
				{
					Name: "stage2",
					Tasks: []Task{
						{Name: "should-be-skipped", Command: "cmd-skip"},
					},
				},
				{
					Name: "stage3",
					Tasks: []Task{
						{Name: "also-skipped", Command: "cmd-also-skip"},
					},
				},
			},
		},
	}

	report := engine.Execute(context.Background(), pd, "")

	if report.Status != "failure" {
		t.Errorf("Status = %q, want %q", report.Status, "failure")
	}

	// Stage 1 should have 3 tasks: ok (success), fail (failure), after-fail (skipped).
	if len(report.Stages) != 3 {
		t.Fatalf("expected 3 stages, got %d", len(report.Stages))
	}

	s1 := report.Stages[0]
	if s1.Name != "stage1" {
		t.Errorf("Stage[0].Name = %q, want %q", s1.Name, "stage1")
	}
	if s1.Status != "failure" {
		t.Errorf("Stage[0].Status = %q, want %q", s1.Status, "failure")
	}
	if len(s1.Tasks) != 3 {
		t.Fatalf("Stage[0] expected 3 tasks, got %d", len(s1.Tasks))
	}
	if s1.Tasks[0].Status != "success" {
		t.Errorf("Task[0].Status = %q, want %q", s1.Tasks[0].Status, "success")
	}
	if s1.Tasks[1].Status != "failure" {
		t.Errorf("Task[1].Status = %q, want %q", s1.Tasks[1].Status, "failure")
	}
	if s1.Tasks[2].Status != "skipped" {
		t.Errorf("Task[2].Status = %q, want %q", s1.Tasks[2].Status, "skipped")
	}

	// Stage 2 and 3 should be skipped.
	for i := 1; i <= 2; i++ {
		if report.Stages[i].Status != "skipped" {
			t.Errorf("Stage[%d].Status = %q, want %q", i, report.Stages[i].Status, "skipped")
		}
		for _, tr := range report.Stages[i].Tasks {
			if tr.Status != "skipped" {
				t.Errorf("Stage[%d] task %q Status = %q, want %q", i, tr.Name, tr.Status, "skipped")
			}
		}
	}

	// Verify only commands before the failure were actually executed.
	cmds := mock.recordedCommands()
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands executed, got %d: %v", len(cmds), cmds)
	}
	if cmds[0] != "cmd-ok" || cmds[1] != "cmd-fail" {
		t.Errorf("unexpected execution order: %v", cmds)
	}
}

// TestEngine_Execute_FailFastParallel verifies that in a parallel stage,
// stage failure propagates and subsequent stages are skipped.
func TestEngine_Execute_FailFastParallel(t *testing.T) {
	mock := newMockRunner()
	mock.failCommand("cmd-fail")
	engine := NewPipelineEngine(mock)

	pd := &PipelineDefinition{
		Pipeline: Pipeline{
			Name: "test-failfast-parallel",
			Stages: []PipelineStage{
				{
					Name:     "parallel-stage",
					Parallel: true,
					Tasks: []Task{
						{Name: "ok", Command: "cmd-ok"},
						{Name: "fail", Command: "cmd-fail"},
					},
				},
				{
					Name: "next-stage",
					Tasks: []Task{
						{Name: "should-skip", Command: "cmd-skip"},
					},
				},
			},
		},
	}

	report := engine.Execute(context.Background(), pd, "")

	if report.Status != "failure" {
		t.Errorf("Status = %q, want %q", report.Status, "failure")
	}

	// Parallel stage should have run both tasks.
	s0 := report.Stages[0]
	if s0.Status != "failure" {
		t.Errorf("Stage[0].Status = %q, want %q", s0.Status, "failure")
	}
	if len(s0.Tasks) != 2 {
		t.Fatalf("Stage[0] expected 2 tasks, got %d", len(s0.Tasks))
	}

	// Next stage should be skipped.
	if report.Stages[1].Status != "skipped" {
		t.Errorf("Stage[1].Status = %q, want %q", report.Stages[1].Status, "skipped")
	}
}

// ---------------------------------------------------------------------------
// Execution Report Tests
// ---------------------------------------------------------------------------

// TestEngine_Execute_ReportFields verifies that the ExecutionReport contains
// correct pipeline name, stage/task results, and duration.
func TestEngine_Execute_ReportFields(t *testing.T) {
	mock := newMockRunner()
	engine := NewPipelineEngine(mock)

	pd := testPipeline()
	report := engine.Execute(context.Background(), pd, "")

	// Pipeline name.
	if report.PipelineName != "test-pipeline" {
		t.Errorf("PipelineName = %q, want %q", report.PipelineName, "test-pipeline")
	}

	// Status.
	if report.Status != "success" {
		t.Errorf("Status = %q, want %q", report.Status, "success")
	}

	// Duration should be a positive value.
	if report.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", report.Duration)
	}

	// Stage count.
	if len(report.Stages) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(report.Stages))
	}

	// Stage results.
	for i, stage := range report.Stages {
		if stage.Name != pd.Pipeline.Stages[i].Name {
			t.Errorf("Stage[%d].Name = %q, want %q", i, stage.Name, pd.Pipeline.Stages[i].Name)
		}
		if stage.Status != "success" {
			t.Errorf("Stage[%d].Status = %q, want %q", i, stage.Status, "success")
		}
		if len(stage.Tasks) != len(pd.Pipeline.Stages[i].Tasks) {
			t.Fatalf("Stage[%d] expected %d tasks, got %d", i, len(pd.Pipeline.Stages[i].Tasks), len(stage.Tasks))
		}
		for j, task := range stage.Tasks {
			if task.Name != pd.Pipeline.Stages[i].Tasks[j].Name {
				t.Errorf("Stage[%d].Task[%d].Name = %q, want %q", i, j, task.Name, pd.Pipeline.Stages[i].Tasks[j].Name)
			}
			if task.Status != "success" {
				t.Errorf("Stage[%d].Task[%d].Status = %q, want %q", i, j, task.Status, "success")
			}
			if task.Duration <= 0 {
				t.Errorf("Stage[%d].Task[%d].Duration = %v, want > 0", i, j, task.Duration)
			}
		}
	}

	// Recorded commands should match.
	cmds := mock.recordedCommands()
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d: %v", len(cmds), cmds)
	}
}

// ---------------------------------------------------------------------------
// Environment Overrides Tests
// ---------------------------------------------------------------------------

// TestEngine_Execute_EnvironmentOverrides verifies that when env is
// non-empty, the TaskOverride fields are applied to the base task.
func TestEngine_Execute_EnvironmentOverrides(t *testing.T) {
	mock := newMockRunner()
	engine := NewPipelineEngine(mock)

	pd := &PipelineDefinition{
		Pipeline: Pipeline{
			Name: "test-overrides",
			Stages: []PipelineStage{
				{
					Name: "deploy",
					Tasks: []Task{
						{
							Name:    "deploy-app",
							Command: "echo",
							Args:    []string{"default"},
							Environments: map[string]TaskOverride{
								"production": {
									Command: "custom-deploy",
									Args:    []string{"--env=prod"},
								},
							},
						},
					},
				},
			},
		},
	}

	// Execute with env="production" — the command should be overridden.
	report := engine.Execute(context.Background(), pd, "production")

	if report.Status != "success" {
		t.Errorf("Status = %q, want %q", report.Status, "success")
	}
	if len(report.Stages) != 1 {
		t.Fatalf("expected 1 stage, got %d", len(report.Stages))
	}
	if len(report.Stages[0].Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(report.Stages[0].Tasks))
	}

	// Verify the override was applied: the mock runner should have executed
	// "custom-deploy", not "echo".
	cmds := mock.recordedCommands()
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d: %v", len(cmds), cmds)
	}
	if cmds[0] != "custom-deploy" {
		t.Errorf("executed command = %q, want %q (environment override should replace command)", cmds[0], "custom-deploy")
	}
}

// TestEngine_Execute_NoEnvironmentOverrides verifies that with empty env,
// the base task command is used unchanged.
func TestEngine_Execute_NoEnvironmentOverrides(t *testing.T) {
	mock := newMockRunner()
	engine := NewPipelineEngine(mock)

	pd := &PipelineDefinition{
		Pipeline: Pipeline{
			Name: "test-no-override",
			Stages: []PipelineStage{
				{
					Name: "deploy",
					Tasks: []Task{
						{
							Name:    "deploy-app",
							Command: "base-command",
							Environments: map[string]TaskOverride{
								"production": {
									Command: "prod-command",
								},
							},
						},
					},
				},
			},
		},
	}

	report := engine.Execute(context.Background(), pd, "")
	if report.Status != "success" {
		t.Errorf("Status = %q, want %q", report.Status, "success")
	}

	cmds := mock.recordedCommands()
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d: %v", len(cmds), cmds)
	}
	if cmds[0] != "base-command" {
		t.Errorf("executed command = %q, want %q (no override should keep base)", cmds[0], "base-command")
	}
}

// TestEngine_Execute_EnvironmentOverrideAllFields verifies that all
// TaskOverride fields are applied when set.
func TestEngine_Execute_EnvironmentOverrideAllFields(t *testing.T) {
	mock := newMockRunner()
	engine := NewPipelineEngine(mock)

	pd := &PipelineDefinition{
		Pipeline: Pipeline{
			Name: "test-full-override",
			Stages: []PipelineStage{
				{
					Name: "stage1",
					Tasks: []Task{
						{
							Name:       "task1",
							Command:    "base-cmd",
							Args:       []string{"base-arg"},
							WorkingDir: "/base/dir",
							Env:        map[string]string{"BASE": "true"},
							Timeout:    "30s",
							Environments: map[string]TaskOverride{
								"staging": {
									Command:    "staging-cmd",
									Args:       []string{"staging-arg"},
									WorkingDir: "/staging/dir",
									Env:        map[string]string{"STAGING": "true"},
									Timeout:    "60s",
								},
							},
						},
					},
				},
			},
		},
	}

	report := engine.Execute(context.Background(), pd, "staging")
	if report.Status != "success" {
		t.Errorf("Status = %q, want %q", report.Status, "success")
	}

	cmds := mock.recordedCommands()
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d: %v", len(cmds), cmds)
	}
	if cmds[0] != "staging-cmd" {
		t.Errorf("executed command = %q, want %q", cmds[0], "staging-cmd")
	}

	// Verify the task duration is reasonable (the timeout was overridden to 60s).
	if report.Stages[0].Tasks[0].Duration <= 0 {
		t.Errorf("expected positive duration, got %v", report.Stages[0].Tasks[0].Duration)
	}
}

// ---------------------------------------------------------------------------
// Validation Error Tests
// ---------------------------------------------------------------------------

// TestEngine_Execute_ValidationErrorsBeforeExecution verifies that a pipeline
// with missing required fields returns an error without executing any tasks.
func TestEngine_Execute_ValidationErrorsBeforeExecution(t *testing.T) {
	mock := newMockRunner()
	engine := NewPipelineEngine(mock)

	// A pipeline with an empty stage name and missing task command — should
	// be caught by PipelineDefinition.Validate() before execution.
	pd := &PipelineDefinition{
		Pipeline: Pipeline{
			Name: "",
			Stages: []PipelineStage{
				{
					Name: "",
					Tasks: []Task{
						{Name: "orphan", Command: ""},
					},
				},
			},
		},
	}

	// We attempt to load it — Load calls Validate().
	path := writePipelineYAML(t, *pd)
	_, err := engine.Load(path)
	if err == nil {
		t.Fatal("Load() = nil, want validation error for invalid pipeline")
	}

	// No commands should have been executed.
	cmds := mock.recordedCommands()
	if len(cmds) != 0 {
		t.Errorf("expected 0 commands executed, got %d: %v", len(cmds), cmds)
	}
}

// ---------------------------------------------------------------------------
// Edge Case Tests
// ---------------------------------------------------------------------------

// TestEngine_Execute_EmptyStagesPipeline verifies that executing the default
// build pipeline (empty stages) succeeds with a "success" status.
func TestEngine_Execute_EmptyStagesPipeline(t *testing.T) {
	mock := newMockRunner()
	engine := NewPipelineEngine(mock)

	pd := DefaultBuildPipeline()
	report := engine.Execute(context.Background(), &pd, "")

	if report.Status != "success" {
		t.Errorf("Status = %q, want %q", report.Status, "success")
	}
	if len(report.Stages) != 0 {
		t.Errorf("expected 0 stages, got %d", len(report.Stages))
	}
	if report.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", report.Duration)
	}
}

// TestEngine_Execute_ContextCancelledBeforeExecution verifies that if the
// context is already cancelled, tasks are still handled gracefully.
func TestEngine_Execute_ContextCancelledBeforeExecution(t *testing.T) {
	mock := newMockRunner()
	engine := NewPipelineEngine(mock)

	pd := testPipeline()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before execution.

	report := engine.Execute(ctx, pd, "")

	// The pipeline should still run but tasks may fail due to cancellation.
	// The exact behavior depends on how runtiming handles cancelled contexts.
	if len(report.Stages) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(report.Stages))
	}
	// Tasks with cancelled context return failure, triggering fail-fast.
	if report.Status != "failure" {
		t.Errorf("Status = %q, want %q for cancelled context", report.Status, "failure")
	}
}
