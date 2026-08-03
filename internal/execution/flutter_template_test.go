package execution

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// FlutterBuildPipeline Tests
// ---------------------------------------------------------------------------

// TestFlutterBuildPipeline_Valid verifies that the Flutter template produced
// by FlutterBuildPipeline passes validation and has the expected structure:
// pipeline "build" with a single "build" stage.
func TestFlutterBuildPipeline_Valid(t *testing.T) {
	def := FlutterBuildPipeline()

	if err := def.Validate(); err != nil {
		t.Fatalf("FlutterBuildPipeline() validation failed: %v", err)
	}

	if def.Pipeline.Name != "build" {
		t.Errorf("Pipeline.Name = %q, want %q", def.Pipeline.Name, "build")
	}

	stages := def.Pipeline.Stages
	if len(stages) != 1 {
		t.Fatalf("expected 1 stage, got %d", len(stages))
	}
	if stages[0].Name != "build" {
		t.Errorf("stage name = %q, want %q", stages[0].Name, "build")
	}
	if stages[0].Parallel {
		t.Errorf("stage %q Parallel = true, want false", stages[0].Name)
	}
}

// TestFlutterBuildPipeline_Tasks verifies the three Flutter build tasks:
// flutter-web, flutter-apk, flutter-ios with their commands and arguments
// (MVP-002 §3.5).
func TestFlutterBuildPipeline_Tasks(t *testing.T) {
	def := FlutterBuildPipeline()
	tasks := def.Pipeline.Stages[0].Tasks

	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}

	wantTasks := []struct {
		name    string
		command string
		args    []string
		timeout string
	}{
		{name: "flutter-web", command: "flutter", args: []string{"build", "web"}, timeout: "10m"},
		{name: "flutter-apk", command: "flutter", args: []string{"build", "apk", "--release"}, timeout: "15m"},
		{name: "flutter-ios", command: "flutter", args: []string{"build", "ios", "--release"}, timeout: ""},
	}

	for i, want := range wantTasks {
		task := tasks[i]
		if task.Name != want.name {
			t.Errorf("task %d name = %q, want %q", i, task.Name, want.name)
		}
		if task.Command != want.command {
			t.Errorf("task %q command = %q, want %q", task.Name, task.Command, want.command)
		}
		if !slices.Equal(task.Args, want.args) {
			t.Errorf("task %q args = %v, want %v", task.Name, task.Args, want.args)
		}
		// Build tasks must set an explicit timeout: Flutter builds (first
		// Gradle run in particular) exceed the engine's 5-minute default.
		// Found by E2E verification (ST-007-005).
		if task.Timeout != want.timeout {
			t.Errorf("task %q Timeout = %q, want %q", task.Name, task.Timeout, want.timeout)
		}
	}
}

// TestFlutterBuildPipeline_TaskMetadata verifies the platform metadata of
// each Flutter build task: web and apk support linux/darwin/windows, ios
// supports darwin only (ADR-018, TS-P7-27 AC-3).
func TestFlutterBuildPipeline_TaskMetadata(t *testing.T) {
	def := FlutterBuildPipeline()
	tasks := def.Pipeline.Stages[0].Tasks

	wantTargets := map[string]struct {
		target    string
		platforms []string
	}{
		"flutter-web": {target: "web", platforms: []string{"linux", "darwin", "windows"}},
		"flutter-apk": {target: "apk", platforms: []string{"linux", "darwin", "windows"}},
		"flutter-ios": {target: "ios", platforms: []string{"darwin"}},
	}

	for _, task := range tasks {
		want, ok := wantTargets[task.Name]
		if !ok {
			t.Errorf("unexpected task %q in template", task.Name)
			continue
		}
		if task.Metadata == nil {
			t.Errorf("task %q Metadata = nil, want platform metadata", task.Name)
			continue
		}
		if task.Metadata.Target != want.target {
			t.Errorf("task %q Metadata.Target = %q, want %q", task.Name, task.Metadata.Target, want.target)
		}
		if !slices.Equal(task.Metadata.Platforms, want.platforms) {
			t.Errorf("task %q Metadata.Platforms = %v, want %v", task.Name, task.Metadata.Platforms, want.platforms)
		}
	}
}

// ---------------------------------------------------------------------------
// MarshalPipeline Tests
// ---------------------------------------------------------------------------

// TestFlutterPipeline_MarshalValidYAML verifies that marshaling the Flutter
// template produces valid YAML that round-trips: unmarshaling restores the
// three tasks with their commands, args, and platform metadata (TS-P7-27
// AC-4).
func TestFlutterPipeline_MarshalValidYAML(t *testing.T) {
	def := FlutterBuildPipeline()

	data, err := MarshalPipeline(def)
	if err != nil {
		t.Fatalf("MarshalPipeline() failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("MarshalPipeline() returned empty data")
	}

	yamlText := string(data)
	for _, want := range []string{
		"name: build",
		"name: flutter-web",
		"name: flutter-apk",
		"name: flutter-ios",
		"command: flutter",
		"timeout: 10m",
		"timeout: 15m",
		"metadata:",
		"platforms:",
		"target: web",
		"target: apk",
		"target: ios",
	} {
		if !strings.Contains(yamlText, want) {
			t.Errorf("marshaled YAML does not contain %q:\n%s", want, yamlText)
		}
	}

	// The YAML must unmarshal back into an equivalent definition.
	var decoded PipelineDefinition
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("yaml.Unmarshal() failed: %v", err)
	}
	if err := decoded.Validate(); err != nil {
		t.Fatalf("decoded definition failed validation: %v", err)
	}

	tasks := decoded.Pipeline.Stages[0].Tasks
	if len(tasks) != 3 {
		t.Fatalf("decoded tasks = %d, want 3", len(tasks))
	}
	for _, task := range tasks {
		if task.Metadata == nil {
			t.Errorf("decoded task %q Metadata = nil, want platform metadata", task.Name)
		}
	}
}

// TestFlutterPipeline_RoundTripToBuildYAML verifies the acceptance criterion
// "template stored in .anvil/pipelines/build.yaml" (TS-P7-27): marshaling
// the Flutter template and writing it to a project's .anvil/pipelines/build.yaml
// produces YAML that LookupBuildDefinition loads and validates successfully,
// with platform metadata intact.
func TestFlutterPipeline_RoundTripToBuildYAML(t *testing.T) {
	projectRoot := t.TempDir()
	pipelineDir := filepath.Join(projectRoot, ".anvil", "pipelines")
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}

	data, err := MarshalPipeline(FlutterBuildPipeline())
	if err != nil {
		t.Fatalf("MarshalPipeline() failed: %v", err)
	}

	buildPath := filepath.Join(pipelineDir, "build.yaml")
	if err := os.WriteFile(buildPath, data, 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}

	def, err := LookupBuildDefinition(projectRoot)
	if err != nil {
		t.Fatalf("LookupBuildDefinition() = %v, want nil", err)
	}

	if def.Pipeline.Name != "build" {
		t.Errorf("Pipeline.Name = %q, want %q", def.Pipeline.Name, "build")
	}
	if len(def.Pipeline.Stages) != 1 {
		t.Fatalf("expected 1 stage, got %d", len(def.Pipeline.Stages))
	}
	tasks := def.Pipeline.Stages[0].Tasks
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
	if tasks[2].Name != "flutter-ios" {
		t.Errorf("task 2 name = %q, want %q", tasks[2].Name, "flutter-ios")
	}
	if tasks[2].Metadata == nil || tasks[2].Metadata.Target != "ios" {
		t.Errorf("flutter-ios Metadata = %+v, want target %q", tasks[2].Metadata, "ios")
	}
}
