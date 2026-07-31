package execution

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestDefaultBuildPipeline_ValidStructure verifies that DefaultBuildPipeline
// returns a PipelineDefinition with the expected structure (name: "build",
// empty stages list).
func TestDefaultBuildPipeline_ValidStructure(t *testing.T) {
	pd := DefaultBuildPipeline()

	if pd.Pipeline.Name != "build" {
		t.Errorf("Pipeline.Name = %q, want %q", pd.Pipeline.Name, "build")
	}
	if len(pd.Pipeline.Stages) != 0 {
		t.Errorf("expected empty stages, got %d", len(pd.Pipeline.Stages))
	}
	if pd.Pipeline.Env != nil {
		t.Errorf("expected nil Env, got %v", pd.Pipeline.Env)
	}
}

// TestDefaultCIPipeline_ValidStructure verifies that DefaultCIPipeline
// returns a PipelineDefinition with build + test stages, each containing
// the expected tasks.
func TestDefaultCIPipeline_ValidStructure(t *testing.T) {
	pd := DefaultCIPipeline()

	if pd.Pipeline.Name != "ci" {
		t.Errorf("Pipeline.Name = %q, want %q", pd.Pipeline.Name, "ci")
	}

	if len(pd.Pipeline.Stages) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(pd.Pipeline.Stages))
	}

	// Stage 0: build.
	stage0 := pd.Pipeline.Stages[0]
	if stage0.Name != "build" {
		t.Errorf("Stage[0].Name = %q, want %q", stage0.Name, "build")
	}
	if len(stage0.Tasks) != 1 {
		t.Fatalf("Stage[0] expected 1 task, got %d", len(stage0.Tasks))
	}
	if stage0.Tasks[0].Name != "build" {
		t.Errorf("Stage[0].Task[0].Name = %q, want %q", stage0.Tasks[0].Name, "build")
	}
	if stage0.Tasks[0].Command != "echo" {
		t.Errorf("Stage[0].Task[0].Command = %q, want %q", stage0.Tasks[0].Command, "echo")
	}

	// Stage 1: test.
	stage1 := pd.Pipeline.Stages[1]
	if stage1.Name != "test" {
		t.Errorf("Stage[1].Name = %q, want %q", stage1.Name, "test")
	}
	if len(stage1.Tasks) != 3 {
		t.Fatalf("Stage[1] expected 3 tasks, got %d", len(stage1.Tasks))
	}
	expectedTasks := []string{"unit-tests", "static-analysis", "linting"}
	for i, name := range expectedTasks {
		if stage1.Tasks[i].Name != name {
			t.Errorf("Stage[1].Task[%d].Name = %q, want %q", i, stage1.Tasks[i].Name, name)
		}
		if stage1.Tasks[i].Command != "echo" {
			t.Errorf("Stage[1].Task[%d].Command = %q, want %q", i, stage1.Tasks[i].Command, "echo")
		}
	}
}

// TestPipelineDefinition_YAML encases that PipelineDefinition can be
// marshaled to YAML and unmarshaled back without data loss.
func TestPipelineDefinition_YAML(t *testing.T) {
	pd := DefaultCIPipeline()

	data, err := yaml.Marshal(&pd)
	if err != nil {
		t.Fatalf("yaml.Marshal() failed: %v", err)
	}

	var decoded PipelineDefinition
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("yaml.Unmarshal() failed: %v", err)
	}

	if decoded.Pipeline.Name != pd.Pipeline.Name {
		t.Errorf("Pipeline.Name = %q, want %q", decoded.Pipeline.Name, pd.Pipeline.Name)
	}
	if len(decoded.Pipeline.Stages) != len(pd.Pipeline.Stages) {
		t.Fatalf("stages count = %d, want %d", len(decoded.Pipeline.Stages), len(pd.Pipeline.Stages))
	}
	for i := range decoded.Pipeline.Stages {
		if decoded.Pipeline.Stages[i].Name != pd.Pipeline.Stages[i].Name {
			t.Errorf("Stage[%d].Name = %q, want %q", i, decoded.Pipeline.Stages[i].Name, pd.Pipeline.Stages[i].Name)
		}
		if len(decoded.Pipeline.Stages[i].Tasks) != len(pd.Pipeline.Stages[i].Tasks) {
			t.Fatalf("Stage[%d] tasks count = %d, want %d", i, len(decoded.Pipeline.Stages[i].Tasks), len(pd.Pipeline.Stages[i].Tasks))
		}
		for j := range decoded.Pipeline.Stages[i].Tasks {
			got := decoded.Pipeline.Stages[i].Tasks[j]
			want := pd.Pipeline.Stages[i].Tasks[j]
			if got.Name != want.Name {
				t.Errorf("Stage[%d].Task[%d].Name = %q, want %q", i, j, got.Name, want.Name)
			}
			if got.Command != want.Command {
				t.Errorf("Stage[%d].Task[%d].Command = %q, want %q", i, j, got.Command, want.Command)
			}
		}
	}
}

// TestPipelineDefinition_YAMLRoundTripWithOverrides verifies that
// TaskOverride fields survive a YAML marshal/unmarshal round trip.
func TestPipelineDefinition_YAMLRoundTripWithOverrides(t *testing.T) {
	original := PipelineDefinition{
		Pipeline: Pipeline{
			Name: "test-overrides",
			Stages: []PipelineStage{
				{
					Name: "deploy",
					Tasks: []Task{
						{
							Name:    "deploy-app",
							Command: "echo",
							Args:    []string{"deploying..."},
							Environments: map[string]TaskOverride{
								"production": {
									Command:    "deploy-prod",
									Args:       []string{"--env=prod"},
									WorkingDir: "/app/prod",
									Env:        map[string]string{"APP_ENV": "production"},
									Timeout:    "120s",
								},
							},
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

	var decoded PipelineDefinition
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("yaml.Unmarshal() failed: %v", err)
	}

	task := decoded.Pipeline.Stages[0].Tasks[0]
	override, ok := task.Environments["production"]
	if !ok {
		t.Fatal("Environments[\"production\"] not found after round trip")
	}
	if override.Command != "deploy-prod" {
		t.Errorf("Override.Command = %q, want %q", override.Command, "deploy-prod")
	}
	if len(override.Args) != 1 || override.Args[0] != "--env=prod" {
		t.Errorf("Override.Args = %v, want %v", override.Args, []string{"--env=prod"})
	}
	if override.WorkingDir != "/app/prod" {
		t.Errorf("Override.WorkingDir = %q, want %q", override.WorkingDir, "/app/prod")
	}
	if override.Env["APP_ENV"] != "production" {
		t.Errorf("Override.Env[\"APP_ENV\"] = %q, want %q", override.Env["APP_ENV"], "production")
	}
	if override.Timeout != "120s" {
		t.Errorf("Override.Timeout = %q, want %q", override.Timeout, "120s")
	}
}

// TestTaskOverride_FieldsAreSet verifies that TaskOverride fields can be
// populated and accessed correctly.
func TestTaskOverride_FieldsAreSet(t *testing.T) {
	to := TaskOverride{
		Command:    "custom-cmd",
		Args:       []string{"arg1", "arg2"},
		WorkingDir: "/custom/dir",
		Env:        map[string]string{"KEY": "val"},
		Timeout:    "60s",
	}

	if to.Command != "custom-cmd" {
		t.Errorf("Command = %q, want %q", to.Command, "custom-cmd")
	}
	if len(to.Args) != 2 || to.Args[0] != "arg1" || to.Args[1] != "arg2" {
		t.Errorf("Args = %v, want %v", to.Args, []string{"arg1", "arg2"})
	}
	if to.WorkingDir != "/custom/dir" {
		t.Errorf("WorkingDir = %q, want %q", to.WorkingDir, "/custom/dir")
	}
	if to.Env["KEY"] != "val" {
		t.Errorf("Env[\"KEY\"] = %q, want %q", to.Env["KEY"], "val")
	}
	if to.Timeout != "60s" {
		t.Errorf("Timeout = %q, want %q", to.Timeout, "60s")
	}
}

// TestTaskOverride_EmptyDefaults verifies that zero-value TaskOverride
// fields are empty (no panics on access).
func TestTaskOverride_EmptyDefaults(t *testing.T) {
	var to TaskOverride

	if to.Command != "" {
		t.Errorf("expected empty Command, got %q", to.Command)
	}
	if to.Args != nil {
		t.Errorf("expected nil Args, got %v", to.Args)
	}
	if to.WorkingDir != "" {
		t.Errorf("expected empty WorkingDir, got %q", to.WorkingDir)
	}
	if to.Env != nil {
		t.Errorf("expected nil Env, got %v", to.Env)
	}
	if to.Timeout != "" {
		t.Errorf("expected empty Timeout, got %q", to.Timeout)
	}
}

// TestPipelineDefinition_Validate_Valid verifies that a well-formed pipeline
// definition passes validation.
func TestPipelineDefinition_Validate_Valid(t *testing.T) {
	pd := PipelineDefinition{
		Pipeline: Pipeline{
			Name: "test",
			Stages: []PipelineStage{
				{
					Name: "stage1",
					Tasks: []Task{
						{Name: "task1", Command: "echo"},
					},
				},
			},
		},
	}
	if err := pd.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

// TestPipelineDefinition_Validate_MissingName verifies that a pipeline
// without a name is rejected.
func TestPipelineDefinition_Validate_MissingName(t *testing.T) {
	pd := PipelineDefinition{
		Pipeline: Pipeline{
			Name: "",
			Stages: []PipelineStage{
				{
					Name: "stage1",
					Tasks: []Task{
						{Name: "task1", Command: "echo"},
					},
				},
			},
		},
	}
	err := pd.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want error")
	}
	if !strings.Contains(err.Error(), "pipeline name is required") {
		t.Errorf("error = %v, want to contain 'pipeline name is required'", err)
	}
}

// TestPipelineDefinition_Validate_MissingStageName verifies that a stage
// without a name is rejected.
func TestPipelineDefinition_Validate_MissingStageName(t *testing.T) {
	pd := PipelineDefinition{
		Pipeline: Pipeline{
			Name: "test",
			Stages: []PipelineStage{
				{
					Name: "",
					Tasks: []Task{
						{Name: "task1", Command: "echo"},
					},
				},
			},
		},
	}
	err := pd.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want error")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("error = %v, want to contain 'name is required'", err)
	}
}

// TestPipelineDefinition_Validate_MissingTaskCommand verifies that a task
// without a command is rejected.
func TestPipelineDefinition_Validate_MissingTaskCommand(t *testing.T) {
	pd := PipelineDefinition{
		Pipeline: Pipeline{
			Name: "test",
			Stages: []PipelineStage{
				{
					Name: "stage1",
					Tasks: []Task{
						{Name: "task1", Command: ""},
					},
				},
			},
		},
	}
	err := pd.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want error")
	}
	if !strings.Contains(err.Error(), "command is required") {
		t.Errorf("error = %v, want to contain 'command is required'", err)
	}
}
