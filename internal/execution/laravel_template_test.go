package execution

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// LaravelBuildPipeline Tests
// ---------------------------------------------------------------------------

// TestLaravelBuildPipeline_Valid verifies that the Laravel template produced
// by LaravelBuildPipeline passes validation and has the expected structure.
func TestLaravelBuildPipeline_Valid(t *testing.T) {
	def := LaravelBuildPipeline()

	if err := def.Validate(); err != nil {
		t.Fatalf("LaravelBuildPipeline() validation failed: %v", err)
	}

	if def.Pipeline.Name != "build" {
		t.Errorf("Pipeline.Name = %q, want %q", def.Pipeline.Name, "build")
	}

	stages := def.Pipeline.Stages
	if len(stages) != 3 {
		t.Fatalf("expected 3 stages, got %d", len(stages))
	}

	// Stages must appear in order: dependencies, assets, optimize.
	wantStages := []string{"dependencies", "assets", "optimize"}
	for i, want := range wantStages {
		if stages[i].Name != want {
			t.Errorf("stage %d name = %q, want %q", i, stages[i].Name, want)
		}
	}

	if stages[0].Parallel {
		t.Errorf("stage %q Parallel = true, want false", stages[0].Name)
	}
	if stages[1].Parallel {
		t.Errorf("stage %q Parallel = true, want false", stages[1].Name)
	}
	if stages[2].Parallel {
		t.Errorf("stage %q Parallel = true, want false", stages[2].Name)
	}
}

// TestLaravelBuildPipeline_DependenciesStage verifies the composer task of
// the dependencies stage.
func TestLaravelBuildPipeline_DependenciesStage(t *testing.T) {
	def := LaravelBuildPipeline()
	stage := def.Pipeline.Stages[0]

	if stage.Name != "dependencies" {
		t.Fatalf("stage name = %q, want %q", stage.Name, "dependencies")
	}
	if len(stage.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(stage.Tasks))
	}

	task := stage.Tasks[0]
	if task.Name != "composer-install" {
		t.Errorf("task name = %q, want %q", task.Name, "composer-install")
	}
	if task.Command != "composer" {
		t.Errorf("task command = %q, want %q", task.Command, "composer")
	}
	wantArgs := []string{"install", "--no-dev", "--optimize-autoloader"}
	if !equalStringSlices(task.Args, wantArgs) {
		t.Errorf("task args = %v, want %v", task.Args, wantArgs)
	}
}

// TestLaravelBuildPipeline_AssetsStage verifies the npm task of the assets
// stage.
func TestLaravelBuildPipeline_AssetsStage(t *testing.T) {
	def := LaravelBuildPipeline()
	stage := def.Pipeline.Stages[1]

	if stage.Name != "assets" {
		t.Fatalf("stage name = %q, want %q", stage.Name, "assets")
	}
	if len(stage.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(stage.Tasks))
	}

	task := stage.Tasks[0]
	if task.Name != "npm-build" {
		t.Errorf("task name = %q, want %q", task.Name, "npm-build")
	}
	if task.Command != "npm" {
		t.Errorf("task command = %q, want %q", task.Command, "npm")
	}
	wantArgs := []string{"run", "build"}
	if !equalStringSlices(task.Args, wantArgs) {
		t.Errorf("task args = %v, want %v", task.Args, wantArgs)
	}
}

// TestLaravelBuildPipeline_OptimizeStage verifies the artisan cache tasks of
// the optimize stage.
func TestLaravelBuildPipeline_OptimizeStage(t *testing.T) {
	def := LaravelBuildPipeline()
	stage := def.Pipeline.Stages[2]

	if stage.Name != "optimize" {
		t.Fatalf("stage name = %q, want %q", stage.Name, "optimize")
	}
	if len(stage.Tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(stage.Tasks))
	}

	wantTasks := []struct {
		name    string
		command string
		args    []string
	}{
		{name: "cache-config", command: "php", args: []string{"artisan", "config:cache"}},
		{name: "cache-route", command: "php", args: []string{"artisan", "route:cache"}},
		{name: "cache-view", command: "php", args: []string{"artisan", "view:cache"}},
	}

	for i, want := range wantTasks {
		task := stage.Tasks[i]
		if task.Name != want.name {
			t.Errorf("task %d name = %q, want %q", i, task.Name, want.name)
		}
		if task.Command != want.command {
			t.Errorf("task %q command = %q, want %q", task.Name, task.Command, want.command)
		}
		if !equalStringSlices(task.Args, want.args) {
			t.Errorf("task %q args = %v, want %v", task.Name, task.Args, want.args)
		}
	}
}

// ---------------------------------------------------------------------------
// MarshalPipeline Tests
// ---------------------------------------------------------------------------

// TestMarshalPipeline_ValidYAML verifies that MarshalPipeline serializes the
// definition into YAML containing the expected stage names and commands.
func TestMarshalPipeline_ValidYAML(t *testing.T) {
	def := LaravelBuildPipeline()

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
		"name: dependencies",
		"name: assets",
		"name: optimize",
		"command: composer",
		"--optimize-autoloader",
		"command: npm",
		"command: php",
		"config:cache",
		"route:cache",
		"view:cache",
	} {
		if !strings.Contains(yamlText, want) {
			t.Errorf("marshaled YAML does not contain %q:\n%s", want, yamlText)
		}
	}
}

// TestLaravelPipeline_RoundTripToBuildYAML verifies the acceptance criterion
// "template stored in .anvil/pipelines/build.yaml": marshaling the Laravel
// template and writing it to a project's .anvil/pipelines/build.yaml produces
// YAML that LookupBuildDefinition loads and validates successfully.
func TestLaravelPipeline_RoundTripToBuildYAML(t *testing.T) {
	projectRoot := t.TempDir()
	pipelineDir := filepath.Join(projectRoot, ".anvil", "pipelines")
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}

	data, err := MarshalPipeline(LaravelBuildPipeline())
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
	if len(def.Pipeline.Stages) != 3 {
		t.Fatalf("expected 3 stages, got %d", len(def.Pipeline.Stages))
	}
	if def.Pipeline.Stages[0].Name != "dependencies" {
		t.Errorf("stage 0 name = %q, want %q", def.Pipeline.Stages[0].Name, "dependencies")
	}
	if def.Pipeline.Stages[1].Name != "assets" {
		t.Errorf("stage 1 name = %q, want %q", def.Pipeline.Stages[1].Name, "assets")
	}
	if def.Pipeline.Stages[2].Name != "optimize" {
		t.Errorf("stage 2 name = %q, want %q", def.Pipeline.Stages[2].Name, "optimize")
	}
	if len(def.Pipeline.Stages[2].Tasks) != 3 {
		t.Fatalf("optimize stage: expected 3 tasks, got %d", len(def.Pipeline.Stages[2].Tasks))
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// equalStringSlices reports whether two string slices are equal in order.
func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
