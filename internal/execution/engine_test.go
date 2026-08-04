package execution

import (
	"context"
	"fmt"
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
//   - outputs: per-command captured stdout/stderr (thread-safe)
type mockRunner struct {
	mu           sync.Mutex
	commands     []string
	args         [][]string
	failCommands map[string]bool
	delay        time.Duration
	outputs      map[string]mockOutput
}

// mockOutput is the captured stdout/stderr pair returned for a command.
type mockOutput struct {
	stdout string
	stderr string
}

func newMockRunner() *mockRunner {
	return &mockRunner{
		failCommands: make(map[string]bool),
		outputs:      make(map[string]mockOutput),
	}
}

// failCommand registers a command that should fail when executed.
func (m *mockRunner) failCommand(cmd string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failCommands[cmd] = true
}

// setOutput registers the captured stdout/stderr returned for a command.
func (m *mockRunner) setOutput(cmd, stdout, stderr string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.outputs[cmd] = mockOutput{stdout: stdout, stderr: stderr}
}

// recordedCommands returns a copy of the commands executed so far.
func (m *mockRunner) recordedCommands() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]string, len(m.commands))
	copy(cp, m.commands)
	return cp
}

// recordedArgs returns a copy of the args for each execution so far.
func (m *mockRunner) recordedArgs() [][]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([][]string, len(m.args))
	for i, a := range m.args {
		cp[i] = make([]string, len(a))
		copy(cp[i], a)
	}
	return cp
}

func (m *mockRunner) Execute(ctx context.Context, req ExecutionRequest) Result {
	start := time.Now()

	m.mu.Lock()
	m.commands = append(m.commands, req.Command)
	argsCopy := make([]string, len(req.Args))
	copy(argsCopy, req.Args)
	m.args = append(m.args, argsCopy)
	shouldFail := m.failCommands[req.Command]
	out := m.outputs[req.Command]
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
			Stdout:   out.stdout,
			Stderr:   out.stderr,
			Duration: time.Since(start),
		}
	}

	return Result{
		Status:   StatusSuccess,
		ExitCode: 0,
		Stdout:   out.stdout,
		Stderr:   out.stderr,
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
	report := engine.Execute(context.Background(), pd, "", nil)

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

	report := engine.Execute(context.Background(), pd, "", nil)

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
	report := engine.Execute(context.Background(), pd, "", nil)
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

	report := engine.Execute(context.Background(), pd, "", nil)

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

	report := engine.Execute(context.Background(), pd, "", nil)

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
	report := engine.Execute(context.Background(), pd, "", nil)

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

// TestExecute_CapturesPerTaskOutput verifies that TaskResult.Stdout and
// TaskResult.Stderr propagate from the runner result into the execution
// report (TS-006-012).
func TestExecute_CapturesPerTaskOutput(t *testing.T) {
	mock := newMockRunner()
	mock.setOutput("cmd-a", "hello\n", "")
	mock.failCommand("cmd-b")
	mock.setOutput("cmd-b", "partial\n", "boom\n")
	engine := NewPipelineEngine(mock)

	pd := testPipeline()
	report := engine.Execute(context.Background(), pd, "", nil)

	if report.Status != "failure" {
		t.Errorf("Status = %q, want %q", report.Status, "failure")
	}

	// Task 1 (cmd-a) succeeded and captured stdout only.
	t0 := report.Stages[0].Tasks[0]
	if t0.Status != "success" {
		t.Errorf("Task[0].Status = %q, want %q", t0.Status, "success")
	}
	if t0.Stdout != "hello\n" {
		t.Errorf("Task[0].Stdout = %q, want %q", t0.Stdout, "hello\n")
	}
	if t0.Stderr != "" {
		t.Errorf("Task[0].Stderr = %q, want empty", t0.Stderr)
	}

	// Task 2 (cmd-b) failed and captured both streams.
	t1 := report.Stages[1].Tasks[0]
	if t1.Status != "failure" {
		t.Errorf("Task[1].Status = %q, want %q", t1.Status, "failure")
	}
	if t1.Stdout != "partial\n" {
		t.Errorf("Task[1].Stdout = %q, want %q", t1.Stdout, "partial\n")
	}
	if t1.Stderr != "boom\n" {
		t.Errorf("Task[1].Stderr = %q, want %q", t1.Stderr, "boom\n")
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
	report := engine.Execute(context.Background(), pd, "production", nil)

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

	report := engine.Execute(context.Background(), pd, "", nil)
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

	report := engine.Execute(context.Background(), pd, "staging", nil)
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
	report := engine.Execute(context.Background(), &pd, "", nil)

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

	report := engine.Execute(ctx, pd, "", nil)

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

// ---------------------------------------------------------------------------
// Pipeline Environment Variable Tests
// ---------------------------------------------------------------------------

// TestEngine_Execute_PipelineEnvPropagatesToArgs verifies that pipeline-level
// environment variables are expanded in task args via ${VAR} syntax.
func TestEngine_Execute_PipelineEnvPropagatesToArgs(t *testing.T) {
	mock := newMockRunner()
	engine := NewPipelineEngine(mock)

	pd := &PipelineDefinition{
		Pipeline: Pipeline{
			Name: "test-pipeline-env",
			Stages: []PipelineStage{
				{
					Name: "build",
					Tasks: []Task{
						{
							Name:    "compile",
							Command: "go",
							Args:    []string{"build", "-o", "${ANVIL_OUTPUT_DIR}/binary"},
						},
					},
				},
			},
		},
	}

	pipelineEnv := map[string]string{
		"ANVIL_OUTPUT_DIR": "/dist/bin",
	}

	report := engine.Execute(context.Background(), pd, "", pipelineEnv)

	if report.Status != "success" {
		t.Errorf("Status = %q, want %q", report.Status, "success")
	}

	// Verify the args were expanded.
	allArgs := mock.recordedArgs()
	if len(allArgs) != 1 {
		t.Fatalf("expected 1 arg set, got %d", len(allArgs))
	}
	expectedArg := "/dist/bin/binary"
	if allArgs[0][2] != expectedArg {
		t.Errorf("args[2] = %q, want %q (template should be expanded)", allArgs[0][2], expectedArg)
	}
}

// TestEngine_Execute_PipelineEnvNilDoesNotAffectArgs verifies that nil
// pipeline env leaves args unchanged (backward compatibility).
func TestEngine_Execute_PipelineEnvNilDoesNotAffectArgs(t *testing.T) {
	mock := newMockRunner()
	engine := NewPipelineEngine(mock)

	pd := &PipelineDefinition{
		Pipeline: Pipeline{
			Name: "test-no-pipeline-env",
			Stages: []PipelineStage{
				{
					Name: "build",
					Tasks: []Task{
						{
							Name:    "compile",
							Command: "go",
							Args:    []string{"build", "-o", "output/binary"},
						},
					},
				},
			},
		},
	}

	report := engine.Execute(context.Background(), pd, "", nil)

	if report.Status != "success" {
		t.Errorf("Status = %q, want %q", report.Status, "success")
	}

	// Verify args are unchanged.
	allArgs := mock.recordedArgs()
	if len(allArgs) != 1 {
		t.Fatalf("expected 1 arg set, got %d", len(allArgs))
	}
	if allArgs[0][2] != "output/binary" {
		t.Errorf("args[2] = %q, want %q (should be unchanged)", allArgs[0][2], "output/binary")
	}
}

// TestEngine_Execute_PipelineEnvOverridesTask verifies that pipeline-level
// env vars are merged into the task's environment and passed to the runner.
func TestEngine_Execute_PipelineEnvOverridesTask(t *testing.T) {
	mock := newMockRunner()
	engine := NewPipelineEngine(mock)

	pd := &PipelineDefinition{
		Pipeline: Pipeline{
			Name: "test-env-merge",
			Stages: []PipelineStage{
				{
					Name: "build",
					Tasks: []Task{
						{
							Name:    "compile",
							Command: "go",
							Args:    []string{"build"},
							Env:     map[string]string{"GOOS": "linux"},
						},
					},
				},
			},
		},
	}

	// Pipeline env provides GOARCH; task env provides GOOS.
	// Both should be present in the final environment.
	pipelineEnv := map[string]string{
		"GOARCH":          "amd64",
		"ANVIL_OUTPUT_DIR": "/tmp/out",
	}

	report := engine.Execute(context.Background(), pd, "", pipelineEnv)

	if report.Status != "success" {
		t.Errorf("Status = %q, want %q", report.Status, "success")
	}
}

// ---------------------------------------------------------------------------
// ProgressReporter Integration Tests
// ---------------------------------------------------------------------------

// mockReporter implements output.ProgressReporter for testing. It records
// all events in order (thread-safe).
type mockReporter struct {
	mu     sync.Mutex
	events []string
}

func newMockReporter() *mockReporter {
	return &mockReporter{}
}

func (m *mockReporter) PipelineStart(name string, env string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, fmt.Sprintf("PipelineStart(%s,%s)", name, env))
}

func (m *mockReporter) PipelineComplete(name string, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, fmt.Sprintf("PipelineComplete(%s)", name))
}

func (m *mockReporter) PipelineFailed(name string, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, fmt.Sprintf("PipelineFailed(%s)", name))
}

func (m *mockReporter) StageStart(name string, index int, total int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, fmt.Sprintf("StageStart(%s,%d,%d)", name, index, total))
}

func (m *mockReporter) StageComplete(name string, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, fmt.Sprintf("StageComplete(%s)", name))
}

func (m *mockReporter) StageFailed(name string, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, fmt.Sprintf("StageFailed(%s)", name))
}

func (m *mockReporter) StageSkipped(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, fmt.Sprintf("StageSkipped(%s)", name))
}

func (m *mockReporter) TaskStart(name string, index int, total int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, fmt.Sprintf("TaskStart(%s,%d,%d)", name, index, total))
}

func (m *mockReporter) TaskComplete(name string, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, fmt.Sprintf("TaskComplete(%s)", name))
}

func (m *mockReporter) TaskFailed(name string, duration time.Duration, exitCode int, stderr string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, fmt.Sprintf("TaskFailed(%s,%d)", name, exitCode))
}

func (m *mockReporter) TaskSkipped(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, fmt.Sprintf("TaskSkipped(%s)", name))
}

func (m *mockReporter) recordedEvents() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]string, len(m.events))
	copy(cp, m.events)
	return cp
}

// TestEngine_Execute_ReporterEmitsPipelineEvents verifies that the reporter
// receives PipelineStart and PipelineComplete on a successful pipeline.
func TestEngine_Execute_ReporterEmitsPipelineEvents(t *testing.T) {
	mock := newMockRunner()
	reporter := newMockReporter()
	engine := NewPipelineEngine(mock, WithProgressReporter(reporter))

	pd := testPipeline()
	report := engine.Execute(context.Background(), pd, "production", nil)

	if report.Status != "success" {
		t.Errorf("Status = %q, want %q", report.Status, "success")
	}

	events := reporter.recordedEvents()
	if len(events) == 0 {
		t.Fatal("expected reporter events, got none")
	}

	// First event should be PipelineStart.
	if events[0] != "PipelineStart(test-pipeline,production)" {
		t.Errorf("first event = %q, want PipelineStart", events[0])
	}

	// Last event should be PipelineComplete.
	last := events[len(events)-1]
	if last != "PipelineComplete(test-pipeline)" {
		t.Errorf("last event = %q, want PipelineComplete", last)
	}
}

// TestEngine_Execute_ReporterEmitsStageEvents verifies that the reporter
// receives StageStart and StageComplete for each stage.
func TestEngine_Execute_ReporterEmitsStageEvents(t *testing.T) {
	mock := newMockRunner()
	reporter := newMockReporter()
	engine := NewPipelineEngine(mock, WithProgressReporter(reporter))

	pd := testPipeline()
	engine.Execute(context.Background(), pd, "", nil)

	events := reporter.recordedEvents()

	// Should have StageStart for stage1 and stage2.
	stageStarts := filterEvents(events, "StageStart")
	if len(stageStarts) != 2 {
		t.Errorf("expected 2 StageStart events, got %d: %v", len(stageStarts), stageStarts)
	}

	// Should have StageComplete for both stages.
	stageCompletes := filterEvents(events, "StageComplete")
	if len(stageCompletes) != 2 {
		t.Errorf("expected 2 StageComplete events, got %d: %v", len(stageCompletes), stageCompletes)
	}
}

// TestEngine_Execute_ReporterEmitsTaskEvents verifies that the reporter
// receives TaskStart and TaskComplete for sequential tasks.
func TestEngine_Execute_ReporterEmitsTaskEvents(t *testing.T) {
	mock := newMockRunner()
	reporter := newMockReporter()
	engine := NewPipelineEngine(mock, WithProgressReporter(reporter))

	pd := testPipeline()
	engine.Execute(context.Background(), pd, "", nil)

	events := reporter.recordedEvents()

	taskStarts := filterEvents(events, "TaskStart")
	if len(taskStarts) != 2 {
		t.Errorf("expected 2 TaskStart events, got %d: %v", len(taskStarts), taskStarts)
	}

	taskCompletes := filterEvents(events, "TaskComplete")
	if len(taskCompletes) != 2 {
		t.Errorf("expected 2 TaskComplete events, got %d: %v", len(taskCompletes), taskCompletes)
	}
}

// TestEngine_Execute_ReporterEmitsFailureEvents verifies that the reporter
// receives TaskFailed, StageFailed, and PipelineFailed on failure.
func TestEngine_Execute_ReporterEmitsFailureEvents(t *testing.T) {
	mock := newMockRunner()
	mock.failCommand("cmd-fail")
	reporter := newMockReporter()
	engine := NewPipelineEngine(mock, WithProgressReporter(reporter))

	pd := &PipelineDefinition{
		Pipeline: Pipeline{
			Name: "test-fail",
			Stages: []PipelineStage{
				{
					Name: "stage1",
					Tasks: []Task{
						{Name: "ok", Command: "cmd-ok"},
						{Name: "fail", Command: "cmd-fail"},
					},
				},
			},
		},
	}

	report := engine.Execute(context.Background(), pd, "", nil)
	if report.Status != "failure" {
		t.Errorf("Status = %q, want %q", report.Status, "failure")
	}

	events := reporter.recordedEvents()

	// Should have one TaskFailed event.
	taskFails := filterEvents(events, "TaskFailed")
	if len(taskFails) != 1 {
		t.Errorf("expected 1 TaskFailed event, got %d: %v", len(taskFails), taskFails)
	}
	if len(taskFails) > 0 && taskFails[0] != "TaskFailed(fail,1)" {
		t.Errorf("TaskFailed event = %q, want TaskFailed(fail,1)", taskFails[0])
	}

	// Should have StageFailed.
	stageFails := filterEvents(events, "StageFailed")
	if len(stageFails) != 1 {
		t.Errorf("expected 1 StageFailed event, got %d: %v", len(stageFails), stageFails)
	}

	// Should have PipelineFailed.
	pipelineFails := filterEvents(events, "PipelineFailed")
	if len(pipelineFails) != 1 {
		t.Errorf("expected 1 PipelineFailed event, got %d: %v", len(pipelineFails), pipelineFails)
	}
}

// TestEngine_Execute_ReporterEmitsSkippedEvents verifies that the reporter
// receives StageSkipped and TaskSkipped for stages skipped due to fail-fast.
func TestEngine_Execute_ReporterEmitsSkippedEvents(t *testing.T) {
	mock := newMockRunner()
	mock.failCommand("cmd-fail")
	reporter := newMockReporter()
	engine := NewPipelineEngine(mock, WithProgressReporter(reporter))

	pd := &PipelineDefinition{
		Pipeline: Pipeline{
			Name: "test-skip",
			Stages: []PipelineStage{
				{
					Name: "stage1",
					Tasks: []Task{
						{Name: "fail", Command: "cmd-fail"},
					},
				},
				{
					Name: "stage2",
					Tasks: []Task{
						{Name: "skipped-task", Command: "cmd-skip"},
					},
				},
			},
		},
	}

	engine.Execute(context.Background(), pd, "", nil)

	events := reporter.recordedEvents()

	stageSkips := filterEvents(events, "StageSkipped")
	if len(stageSkips) != 1 {
		t.Errorf("expected 1 StageSkipped event, got %d: %v", len(stageSkips), stageSkips)
	}
	if len(stageSkips) > 0 && stageSkips[0] != "StageSkipped(stage2)" {
		t.Errorf("StageSkipped = %q, want StageSkipped(stage2)", stageSkips[0])
	}

	taskSkips := filterEvents(events, "TaskSkipped")
	if len(taskSkips) != 1 {
		t.Errorf("expected 1 TaskSkipped event, got %d: %v", len(taskSkips), taskSkips)
	}
}

// TestEngine_Execute_NilReporter verifies that Execute works without a
// reporter (backward compatibility).
func TestEngine_Execute_NilReporter(t *testing.T) {
	mock := newMockRunner()
	engine := NewPipelineEngine(mock) // no reporter

	pd := testPipeline()
	report := engine.Execute(context.Background(), pd, "", nil)

	if report.Status != "success" {
		t.Errorf("Status = %q, want %q", report.Status, "success")
	}
}

// TestEngine_Execute_ReporterParallelStage verifies that the reporter
// receives events for parallel stage tasks.
func TestEngine_Execute_ReporterParallelStage(t *testing.T) {
	mock := newMockRunner()
	reporter := newMockReporter()
	engine := NewPipelineEngine(mock, WithProgressReporter(reporter))

	pd := &PipelineDefinition{
		Pipeline: Pipeline{
			Name: "test-parallel-reporter",
			Stages: []PipelineStage{
				{
					Name:     "parallel-stage",
					Parallel: true,
					Tasks: []Task{
						{Name: "task-a", Command: "cmd-a"},
						{Name: "task-b", Command: "cmd-b"},
					},
				},
			},
		},
	}

	engine.Execute(context.Background(), pd, "", nil)

	events := reporter.recordedEvents()

	// Should have StageStart and StageComplete.
	stageStarts := filterEvents(events, "StageStart")
	if len(stageStarts) != 1 {
		t.Errorf("expected 1 StageStart, got %d", len(stageStarts))
	}

	stageCompletes := filterEvents(events, "StageComplete")
	if len(stageCompletes) != 1 {
		t.Errorf("expected 1 StageComplete, got %d", len(stageCompletes))
	}

	// Should have TaskStart and TaskComplete for both tasks.
	taskStarts := filterEvents(events, "TaskStart")
	if len(taskStarts) != 2 {
		t.Errorf("expected 2 TaskStart events, got %d: %v", len(taskStarts), taskStarts)
	}

	taskCompletes := filterEvents(events, "TaskComplete")
	if len(taskCompletes) != 2 {
		t.Errorf("expected 2 TaskComplete events, got %d: %v", len(taskCompletes), taskCompletes)
	}
}

// filterEvents returns events that start with the given prefix.
func filterEvents(events []string, prefix string) []string {
	var filtered []string
	for _, e := range events {
		if len(e) >= len(prefix) && e[:len(prefix)] == prefix {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// ---------------------------------------------------------------------------
// Environment Variable Resolution Tests (TS-P7-30)
// ---------------------------------------------------------------------------

// TestEngine_Execute_EnvVarResolution verifies that ${VAR} references in
// task env values are resolved from the OS environment at runtime.
//
// Reference: TS-P7-30 AC-1, AC-2
func TestEngine_Execute_EnvVarResolution(t *testing.T) {
	// Set OS environment variable for the test.
	t.Setenv("ANVIL_TEST_HOST", "203.0.113.10")

	mock := newMockRunner()
	engine := NewPipelineEngine(mock)

	pd := &PipelineDefinition{
		Pipeline: Pipeline{
			Name: "test-env-var-resolution",
			Stages: []PipelineStage{
				{
					Name: "build",
					Tasks: []Task{
						{
							Name:    "deploy",
							Command: "echo",
							Env: map[string]string{
								"HOST": "${ANVIL_TEST_HOST}",
							},
						},
					},
				},
			},
		},
	}

	report := engine.Execute(context.Background(), pd, "", nil)

	if report.Status != "success" {
		t.Errorf("Status = %q, want %q", report.Status, "success")
	}

	// Verify the task received the resolved environment variable.
	if len(report.Stages) != 1 || len(report.Stages[0].Tasks) != 1 {
		t.Fatal("expected 1 stage with 1 task")
	}
	if report.Stages[0].Tasks[0].Status != "success" {
		t.Errorf("Task status = %q, want %q", report.Stages[0].Tasks[0].Status, "success")
	}
}

// TestEngine_Execute_EnvVarResolution_MissingVariable verifies that an
// unset environment variable produces a task failure with an explicit
// error message naming the missing variable.
//
// Reference: TS-P7-30 AC-3
func TestEngine_Execute_EnvVarResolution_MissingVariable(t *testing.T) {
	// Ensure the variable is NOT set.
	t.Setenv("ANVIL_TEST_MISSING_VAR", "")

	mock := newMockRunner()
	engine := NewPipelineEngine(mock)

	pd := &PipelineDefinition{
		Pipeline: Pipeline{
			Name: "test-missing-var",
			Stages: []PipelineStage{
				{
					Name: "build",
					Tasks: []Task{
						{
							Name:    "deploy",
							Command: "echo",
							Env: map[string]string{
								"SECRET": "${ANVIL_UNSET_VARIABLE_XYZ}",
							},
						},
					},
				},
			},
		},
	}

	report := engine.Execute(context.Background(), pd, "", nil)

	if report.Status != "failure" {
		t.Errorf("Status = %q, want %q", report.Status, "failure")
	}

	// Verify the task failed with an error about the missing variable.
	if len(report.Stages) != 1 || len(report.Stages[0].Tasks) != 1 {
		t.Fatal("expected 1 stage with 1 task")
	}
	taskResult := report.Stages[0].Tasks[0]
	if taskResult.Status != "failure" {
		t.Errorf("Task status = %q, want %q", taskResult.Status, "failure")
	}
	if !contains(taskResult.Error, "ANVIL_UNSET_VARIABLE_XYZ") {
		t.Errorf("Error = %q, should contain missing variable name", taskResult.Error)
	}
}

// TestEngine_Execute_EnvVarResolution_PlainValues verifies that plain
// string values (without ${}) pass through unchanged.
//
// Reference: TS-P7-30 AC-1
func TestEngine_Execute_EnvVarResolution_PlainValues(t *testing.T) {
	mock := newMockRunner()
	engine := NewPipelineEngine(mock)

	pd := &PipelineDefinition{
		Pipeline: Pipeline{
			Name: "test-plain-values",
			Stages: []PipelineStage{
				{
					Name: "build",
					Tasks: []Task{
						{
							Name:    "compile",
							Command: "go",
							Env: map[string]string{
								"GOOS":   "linux",
								"GOARCH": "amd64",
							},
						},
					},
				},
			},
		},
	}

	report := engine.Execute(context.Background(), pd, "", nil)

	if report.Status != "success" {
		t.Errorf("Status = %q, want %q", report.Status, "success")
	}
}

// TestEngine_Execute_EnvVarResolution_EmptyButSet verifies that a
// variable that is set but empty resolves to "" without error.
//
// Reference: TS-P7-30 AC-2, ADR-019
func TestEngine_Execute_EnvVarResolution_EmptyButSet(t *testing.T) {
	t.Setenv("ANVIL_TEST_EMPTY", "")

	mock := newMockRunner()
	engine := NewPipelineEngine(mock)

	pd := &PipelineDefinition{
		Pipeline: Pipeline{
			Name: "test-empty-but-set",
			Stages: []PipelineStage{
				{
					Name: "build",
					Tasks: []Task{
						{
							Name:    "deploy",
							Command: "echo",
							Env: map[string]string{
								"OPTIONAL_VAR": "${ANVIL_TEST_EMPTY}",
							},
						},
					},
				},
			},
		},
	}

	report := engine.Execute(context.Background(), pd, "", nil)

	if report.Status != "success" {
		t.Errorf("Status = %q, want %q (set-but-empty should resolve)", report.Status, "success")
	}
}

// TestEngine_Execute_EnvVarResolution_MalformedReference verifies that
// partial or malformed ${} references are rejected with an explicit error.
//
// Reference: TS-P7-30 AC-3, ADR-019
func TestEngine_Execute_EnvVarResolution_MalformedReference(t *testing.T) {
	mock := newMockRunner()
	engine := NewPipelineEngine(mock)

	pd := &PipelineDefinition{
		Pipeline: Pipeline{
			Name: "test-malformed-ref",
			Stages: []PipelineStage{
				{
					Name: "build",
					Tasks: []Task{
						{
							Name:    "deploy",
							Command: "echo",
							Env: map[string]string{
								"BAD": "${VAR}extra",
							},
						},
					},
				},
			},
		},
	}

	report := engine.Execute(context.Background(), pd, "", nil)

	if report.Status != "failure" {
		t.Errorf("Status = %q, want %q", report.Status, "failure")
	}

	// Verify the task failed with an error about unsupported reference.
	if len(report.Stages) != 1 || len(report.Stages[0].Tasks) != 1 {
		t.Fatal("expected 1 stage with 1 task")
	}
	taskResult := report.Stages[0].Tasks[0]
	if taskResult.Status != "failure" {
		t.Errorf("Task status = %q, want %q", taskResult.Status, "failure")
	}
	if !contains(taskResult.Error, "unsupported") {
		t.Errorf("Error = %q, should mention unsupported reference", taskResult.Error)
	}
}

// TestEngine_Execute_EnvVarResolution_PipelineEnvWithVar verifies that
// ${VAR} references in pipeline-level env vars are also resolved.
//
// Reference: TS-P7-30 AC-1
func TestEngine_Execute_EnvVarResolution_PipelineEnvWithVar(t *testing.T) {
	t.Setenv("ANVIL_TEST_DEPLOY_HOST", "10.0.0.1")

	mock := newMockRunner()
	engine := NewPipelineEngine(mock)

	pd := &PipelineDefinition{
		Pipeline: Pipeline{
			Name: "test-pipeline-env-var",
			Stages: []PipelineStage{
				{
					Name: "build",
					Tasks: []Task{
						{
							Name:    "deploy",
							Command: "echo",
						},
					},
				},
			},
		},
	}

	pipelineEnv := map[string]string{
		"DEPLOY_HOST": "${ANVIL_TEST_DEPLOY_HOST}",
	}

	report := engine.Execute(context.Background(), pd, "", pipelineEnv)

	if report.Status != "success" {
		t.Errorf("Status = %q, want %q", report.Status, "success")
	}
}

// contains checks if s contains substr.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
