// Platform-aware execution tests (TS-P7-23, ADR-018) and target
// selection / strict mode engine tests (TS-P7-24).
//
// The engine's platform detection hook (WithPlatformDetector) injects
// "linux"/"darwin" deterministically regardless of the test host, and the
// mock runner records which commands actually executed.
package execution

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// flutterTestPipeline builds a pipeline shaped like the Flutter template
// (one build stage, three targets) but with distinct commands per task so
// tests can assert exactly which tasks executed via the mock runner.
func flutterTestPipeline() *PipelineDefinition {
	return &PipelineDefinition{
		Pipeline: Pipeline{
			Name: "build",
			Stages: []PipelineStage{
				{
					Name: "build",
					Tasks: []Task{
						{
							Name:    "flutter-web",
							Command: "cmd-web",
							Args:    []string{"build", "web"},
							Metadata: &TaskMetadata{
								Platforms: []string{"linux", "darwin", "windows"},
								Target:    "web",
							},
						},
						{
							Name:    "flutter-apk",
							Command: "cmd-apk",
							Args:    []string{"build", "apk", "--release"},
							Metadata: &TaskMetadata{
								Platforms: []string{"linux", "darwin", "windows"},
								Target:    "apk",
							},
						},
						{
							Name:    "flutter-ios",
							Command: "cmd-ios",
							Args:    []string{"build", "ios", "--release"},
							Metadata: &TaskMetadata{
								Platforms: []string{"darwin"},
								Target:    "ios",
							},
						},
					},
				},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Platform-aware execution (TS-P7-23)
// ---------------------------------------------------------------------------

// TestEngine_Execute_PlatformMetadataSkipsUnsupported verifies that a task
// whose metadata.platforms does not include the current platform is skipped:
// on injected "linux" the darwin-only ios task is skipped while web and apk
// run; on injected "darwin" all three run.
func TestEngine_Execute_PlatformMetadataSkipsUnsupported(t *testing.T) {
	t.Run("linux skips ios", func(t *testing.T) {
		mock := newMockRunner()
		engine := NewPipelineEngine(mock, WithPlatformDetector(func() string { return "linux" }))

		report := engine.Execute(context.Background(), flutterTestPipeline(), "", nil)

		if report.Status != "success" {
			t.Errorf("Status = %q, want %q", report.Status, "success")
		}
		cmds := mock.recordedCommands()
		want := []string{"cmd-web", "cmd-apk"}
		if len(cmds) != len(want) {
			t.Fatalf("executed %d commands, want %d: %v", len(cmds), len(want), cmds)
		}
		for i := range want {
			if cmds[i] != want[i] {
				t.Errorf("command[%d] = %q, want %q", i, cmds[i], want[i])
			}
		}
	})

	t.Run("darwin runs ios", func(t *testing.T) {
		mock := newMockRunner()
		engine := NewPipelineEngine(mock, WithPlatformDetector(func() string { return "darwin" }))

		report := engine.Execute(context.Background(), flutterTestPipeline(), "", nil)

		if report.Status != "success" {
			t.Errorf("Status = %q, want %q", report.Status, "success")
		}
		cmds := mock.recordedCommands()
		want := []string{"cmd-web", "cmd-apk", "cmd-ios"}
		if len(cmds) != len(want) {
			t.Fatalf("executed %d commands, want %d: %v", len(cmds), len(want), cmds)
		}
	})
}

// TestEngine_Execute_TasksWithoutMetadataAlwaysRun verifies that tasks that
// declare no platform metadata are never filtered by the current platform.
func TestEngine_Execute_TasksWithoutMetadataAlwaysRun(t *testing.T) {
	for _, current := range []string{"linux", "darwin", "windows", "freebsd"} {
		t.Run(current, func(t *testing.T) {
			mock := newMockRunner()
			engine := NewPipelineEngine(mock, WithPlatformDetector(func() string { return current }))

			pd := &PipelineDefinition{
				Pipeline: Pipeline{
					Name: "plain",
					Stages: []PipelineStage{
						{
							Name: "build",
							Tasks: []Task{
								{Name: "task-a", Command: "cmd-a"},
								{Name: "task-b", Command: "cmd-b"},
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
			if len(cmds) != 2 {
				t.Errorf("executed %d commands, want 2: %v", len(cmds), cmds)
			}
		})
	}
}

// TestEngine_Execute_SkipDoesNotFailStage verifies that a stage with one
// succeeded task and one platform-skipped task reports success (graceful
// degradation, ADR-018): skipping must not fail the stage.
func TestEngine_Execute_SkipDoesNotFailStage(t *testing.T) {
	mock := newMockRunner()
	engine := NewPipelineEngine(mock, WithPlatformDetector(func() string { return "linux" }))

	report := engine.Execute(context.Background(), flutterTestPipeline(), "", nil)

	if report.Status != "success" {
		t.Errorf("Status = %q, want %q (skip must not fail the pipeline)", report.Status, "success")
	}
	stage := report.Stages[0]
	if stage.Status != "success" {
		t.Errorf("stage Status = %q, want %q", stage.Status, "success")
	}
	if len(stage.Tasks) != 3 {
		t.Fatalf("stage tasks = %d, want 3", len(stage.Tasks))
	}
	if stage.Tasks[0].Status != "success" {
		t.Errorf("task %q Status = %q, want %q", stage.Tasks[0].Name, stage.Tasks[0].Status, "success")
	}
	if stage.Tasks[2].Status != "skipped" {
		t.Errorf("task %q Status = %q, want %q", stage.Tasks[2].Name, stage.Tasks[2].Status, "skipped")
	}
}

// TestEngine_Execute_AllSkippedStageIsSuccess verifies that a stage where
// every task is skipped reports success with skipped task statuses — a
// graceful no-op, consistent with ADR-009 §9.7.
func TestEngine_Execute_AllSkippedStageIsSuccess(t *testing.T) {
	mock := newMockRunner()
	engine := NewPipelineEngine(mock, WithPlatformDetector(func() string { return "linux" }))

	pd := &PipelineDefinition{
		Pipeline: Pipeline{
			Name: "all-skipped",
			Stages: []PipelineStage{
				{
					Name: "ios-only",
					Tasks: []Task{
						{
							Name:    "flutter-ios",
							Command: "cmd-ios",
							Metadata: &TaskMetadata{
								Platforms: []string{"darwin"},
								Target:    "ios",
							},
						},
					},
				},
			},
		},
	}

	report := engine.Execute(context.Background(), pd, "", nil)

	if report.Status != "success" {
		t.Errorf("Status = %q, want %q (all-skipped pipeline is a graceful no-op success)", report.Status, "success")
	}
	stage := report.Stages[0]
	if stage.Status != "success" {
		t.Errorf("stage Status = %q, want %q", stage.Status, "success")
	}
	if len(stage.Tasks) != 1 {
		t.Fatalf("stage tasks = %d, want 1", len(stage.Tasks))
	}
	if stage.Tasks[0].Status != "skipped" {
		t.Errorf("task Status = %q, want %q", stage.Tasks[0].Status, "skipped")
	}
	if cmds := mock.recordedCommands(); len(cmds) != 0 {
		t.Errorf("executed %d commands, want 0: %v", len(cmds), cmds)
	}
}

// TestEngine_Execute_SkippedTasksReportedWithReason verifies that skipped
// tasks appear in the ExecutionReport with a human-readable skip reason.
func TestEngine_Execute_SkippedTasksReportedWithReason(t *testing.T) {
	mock := newMockRunner()
	engine := NewPipelineEngine(mock, WithPlatformDetector(func() string { return "linux" }))

	report := engine.Execute(context.Background(), flutterTestPipeline(), "", nil)

	ios := report.Stages[0].Tasks[2]
	if ios.Status != "skipped" {
		t.Fatalf("ios Status = %q, want %q", ios.Status, "skipped")
	}
	if ios.SkipReason == "" {
		t.Fatal("SkipReason = empty, want a reason explaining the skip")
	}
	if !strings.Contains(ios.SkipReason, `target "ios"`) {
		t.Errorf("SkipReason = %q, want it to mention the target %q", ios.SkipReason, "ios")
	}
	if !strings.Contains(ios.SkipReason, `"linux"`) {
		t.Errorf("SkipReason = %q, want it to mention the current platform %q", ios.SkipReason, "linux")
	}
	if !strings.Contains(ios.SkipReason, "darwin") {
		t.Errorf("SkipReason = %q, want it to mention the supported platforms", ios.SkipReason)
	}
}

// TestEngine_Execute_SkipEmitsWarningAndTaskSkippedEvent verifies that a
// platform skip emits a human-readable warning (via the warning writer) and
// a TaskSkipped progress event (ADR-018).
func TestEngine_Execute_SkipEmitsWarningAndTaskSkippedEvent(t *testing.T) {
	var warnings bytes.Buffer
	reporter := newMockReporter()
	mock := newMockRunner()
	engine := NewPipelineEngine(mock,
		WithPlatformDetector(func() string { return "linux" }),
		WithWarningWriter(&warnings),
		WithProgressReporter(reporter),
	)

	engine.Execute(context.Background(), flutterTestPipeline(), "", nil)

	// Warning stream contains the task name and the reason.
	warnText := warnings.String()
	if !strings.Contains(warnText, "warning:") {
		t.Errorf("warning output = %q, want it to start with a warning marker", warnText)
	}
	if !strings.Contains(warnText, "flutter-ios") {
		t.Errorf("warning output = %q, want it to mention the skipped task", warnText)
	}
	if !strings.Contains(warnText, "not supported") {
		t.Errorf("warning output = %q, want it to mention the skip reason", warnText)
	}

	// Reporter received TaskSkipped for the ios task.
	taskSkips := filterEvents(reporter.recordedEvents(), "TaskSkipped")
	if len(taskSkips) != 1 || taskSkips[0] != "TaskSkipped(flutter-ios)" {
		t.Errorf("TaskSkipped events = %v, want [TaskSkipped(flutter-ios)]", taskSkips)
	}
}

// TestEngine_Execute_ParallelStageSkipSemantics verifies that a parallel
// stage applies the same platform skip semantics: skipped tasks are
// reported, executed tasks run concurrently, and the stage stays success.
func TestEngine_Execute_ParallelStageSkipSemantics(t *testing.T) {
	mock := newMockRunner()
	mock.delay = 20 * time.Millisecond
	engine := NewPipelineEngine(mock, WithPlatformDetector(func() string { return "linux" }))

	pd := flutterTestPipeline()
	pd.Pipeline.Stages[0].Parallel = true

	report := engine.Execute(context.Background(), pd, "", nil)

	if report.Status != "success" {
		t.Errorf("Status = %q, want %q", report.Status, "success")
	}
	stage := report.Stages[0]
	if stage.Status != "success" {
		t.Errorf("stage Status = %q, want %q", stage.Status, "success")
	}
	if len(stage.Tasks) != 3 {
		t.Fatalf("stage tasks = %d, want 3", len(stage.Tasks))
	}
	// Declaration order preserved: web (success), apk (success), ios (skipped).
	if stage.Tasks[0].Status != "success" || stage.Tasks[1].Status != "success" {
		t.Errorf("expected web+apk success, got %q / %q", stage.Tasks[0].Status, stage.Tasks[1].Status)
	}
	if stage.Tasks[2].Status != "skipped" || stage.Tasks[2].SkipReason == "" {
		t.Errorf("ios = %+v, want skipped with a reason", stage.Tasks[2])
	}
	cmds := mock.recordedCommands()
	if len(cmds) != 2 {
		t.Errorf("executed %d commands, want 2 (web+apk): %v", len(cmds), cmds)
	}
}

// ---------------------------------------------------------------------------
// Target selection + strict mode (TS-P7-24)
// ---------------------------------------------------------------------------

// TestEngine_Execute_TargetFilterRunsOnlyRequestedTargets verifies that
// --target web,apk executes only the web and apk tasks and excludes the ios
// task from the report entirely.
func TestEngine_Execute_TargetFilterRunsOnlyRequestedTargets(t *testing.T) {
	mock := newMockRunner()
	engine := NewPipelineEngine(mock, WithPlatformDetector(func() string { return "linux" }))

	report := engine.ExecuteWithOptions(context.Background(), flutterTestPipeline(), "", nil,
		ExecuteOptions{Targets: []string{"web", "apk"}})

	if report.Status != "success" {
		t.Errorf("Status = %q, want %q", report.Status, "success")
	}
	cmds := mock.recordedCommands()
	want := []string{"cmd-web", "cmd-apk"}
	if len(cmds) != len(want) {
		t.Fatalf("executed %d commands, want %d: %v", len(cmds), len(want), cmds)
	}

	stage := report.Stages[0]
	if len(stage.Tasks) != 2 {
		t.Fatalf("stage tasks = %d, want 2 (ios excluded by --target)", len(stage.Tasks))
	}
	for i, name := range []string{"flutter-web", "flutter-apk"} {
		if stage.Tasks[i].Name != name {
			t.Errorf("task[%d].Name = %q, want %q", i, stage.Tasks[i].Name, name)
		}
	}
}

// TestEngine_Execute_TargetFilterMatchesTaskNameFallback verifies that
// tasks without metadata targets participate in --target selection by their
// task name (documented fallback, TS-P7-24).
func TestEngine_Execute_TargetFilterMatchesTaskNameFallback(t *testing.T) {
	mock := newMockRunner()
	engine := NewPipelineEngine(mock)

	pd := &PipelineDefinition{
		Pipeline: Pipeline{
			Name: "build",
			Stages: []PipelineStage{
				{
					Name: "build",
					Tasks: []Task{
						{Name: "compile", Command: "cmd-compile"},
						{Name: "package", Command: "cmd-package"},
					},
				},
			},
		},
	}

	report := engine.ExecuteWithOptions(context.Background(), pd, "", nil,
		ExecuteOptions{Targets: []string{"compile"}})

	if report.Status != "success" {
		t.Errorf("Status = %q, want %q", report.Status, "success")
	}
	cmds := mock.recordedCommands()
	if len(cmds) != 1 || cmds[0] != "cmd-compile" {
		t.Errorf("executed commands = %v, want [cmd-compile]", cmds)
	}
	if len(report.Stages[0].Tasks) != 1 || report.Stages[0].Tasks[0].Name != "compile" {
		t.Errorf("stage tasks = %+v, want only the compile task", report.Stages[0].Tasks)
	}
}

// TestEngine_Execute_AllTasksFilteredStageIsGracefulNoOp verifies that a
// stage where every task is excluded by --target selection is a graceful
// no-op success with no reported tasks (TS-P7-23, TS-P7-24). The engine
// tolerates requests that match no task even though the CLI validates
// target names before execution.
func TestEngine_Execute_AllTasksFilteredStageIsGracefulNoOp(t *testing.T) {
	mock := newMockRunner()
	engine := NewPipelineEngine(mock, WithPlatformDetector(func() string { return "linux" }))

	report := engine.ExecuteWithOptions(context.Background(), flutterTestPipeline(), "", nil,
		ExecuteOptions{Targets: []string{"windows"}})

	if report.Status != "success" {
		t.Errorf("Status = %q, want %q", report.Status, "success")
	}
	stage := report.Stages[0]
	if stage.Status != "success" {
		t.Errorf("stage Status = %q, want %q", stage.Status, "success")
	}
	if len(stage.Tasks) != 0 {
		t.Errorf("stage tasks = %d, want 0 (all filtered by --target)", len(stage.Tasks))
	}
	if cmds := mock.recordedCommands(); len(cmds) != 0 {
		t.Errorf("executed %d commands, want 0: %v", len(cmds), cmds)
	}
}

// TestEngine_Execute_StrictModeFailsOnUnsupportedTarget verifies that with
// strict mode enabled, a requested target unsupported on the current
// platform produces a task failure with a clear error and fails the
// pipeline (ADR-018, TS-P7-24).
func TestEngine_Execute_StrictModeFailsOnUnsupportedTarget(t *testing.T) {
	mock := newMockRunner()
	engine := NewPipelineEngine(mock, WithPlatformDetector(func() string { return "linux" }))

	report := engine.ExecuteWithOptions(context.Background(), flutterTestPipeline(), "", nil,
		ExecuteOptions{Strict: true})

	if report.Status != "failure" {
		t.Errorf("Status = %q, want %q", report.Status, "failure")
	}
	stage := report.Stages[0]
	if stage.Status != "failure" {
		t.Errorf("stage Status = %q, want %q", stage.Status, "failure")
	}
	ios := stage.Tasks[2]
	if ios.Status != "failure" {
		t.Fatalf("ios Status = %q, want %q (strict mode must fail, not skip)", ios.Status, "failure")
	}
	if ios.Error == "" {
		t.Fatal("ios Error = empty, want a clear strict-mode error")
	}
	if !strings.Contains(ios.Error, `target "ios"`) || !strings.Contains(ios.Error, "not supported") {
		t.Errorf("ios Error = %q, want it to explain the unsupported target", ios.Error)
	}
	if !strings.Contains(ios.Error, "strict mode") {
		t.Errorf("ios Error = %q, want it to mention strict mode", ios.Error)
	}

	// The supported tasks still ran before the strict failure stopped the
	// sequential stage.
	cmds := mock.recordedCommands()
	if len(cmds) != 2 {
		t.Errorf("executed %d commands, want 2 (web+apk before strict failure): %v", len(cmds), cmds)
	}
}

// TestEngine_Execute_StrictModeFilteredTargetNotFailed verifies that in
// strict mode a target excluded by --target selection does not fail the
// pipeline: only requested targets are validated against the platform.
func TestEngine_Execute_StrictModeFilteredTargetNotFailed(t *testing.T) {
	mock := newMockRunner()
	engine := NewPipelineEngine(mock, WithPlatformDetector(func() string { return "linux" }))

	report := engine.ExecuteWithOptions(context.Background(), flutterTestPipeline(), "", nil,
		ExecuteOptions{Targets: []string{"web"}, Strict: true})

	if report.Status != "success" {
		t.Errorf("Status = %q, want %q (unrequested ios must not fail strict mode)", report.Status, "success")
	}
	cmds := mock.recordedCommands()
	if len(cmds) != 1 || cmds[0] != "cmd-web" {
		t.Errorf("executed commands = %v, want [cmd-web]", cmds)
	}
}

// ---------------------------------------------------------------------------
// YAML metadata round trip
// ---------------------------------------------------------------------------

// TestTaskMetadata_YAMLRoundTrip verifies that TaskMetadata serializes and
// deserializes through PipelineDefinition YAML marshaling, and that tasks
// without metadata marshal without a metadata key (byte-compatible with
// existing pipeline files, TS-P7-23).
func TestTaskMetadata_YAMLRoundTrip(t *testing.T) {
	original := PipelineDefinition{
		Pipeline: Pipeline{
			Name: "build",
			Stages: []PipelineStage{
				{
					Name: "build",
					Tasks: []Task{
						{
							Name:    "flutter-ios",
							Command: "flutter",
							Args:    []string{"build", "ios", "--release"},
							Metadata: &TaskMetadata{
								Platforms: []string{"darwin"},
								Target:    "ios",
							},
						},
						{
							Name:    "plain",
							Command: "echo",
						},
					},
				},
			},
		},
	}

	data, err := yaml.Marshal(&original)
	if err != nil {
		t.Fatalf("yaml.Marshal() failed: %v", err)
	}

	// The plain task must not emit a metadata key (omitempty).
	if strings.Count(string(data), "metadata:") != 1 {
		t.Errorf("marshaled YAML has %d metadata keys, want 1:\n%s", strings.Count(string(data), "metadata:"), string(data))
	}

	var decoded PipelineDefinition
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("yaml.Unmarshal() failed: %v", err)
	}

	tasks := decoded.Pipeline.Stages[0].Tasks
	if tasks[0].Metadata == nil {
		t.Fatal("Metadata = nil after round trip, want the metadata block")
	}
	if tasks[0].Metadata.Target != "ios" {
		t.Errorf("Metadata.Target = %q, want %q", tasks[0].Metadata.Target, "ios")
	}
	if len(tasks[0].Metadata.Platforms) != 1 || tasks[0].Metadata.Platforms[0] != "darwin" {
		t.Errorf("Metadata.Platforms = %v, want [darwin]", tasks[0].Metadata.Platforms)
	}
	if tasks[1].Metadata != nil {
		t.Errorf("plain task Metadata = %+v, want nil", tasks[1].Metadata)
	}
}
