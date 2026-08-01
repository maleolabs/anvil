package execution

import (
	"os"
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

// ---------------------------------------------------------------------------
// mergeEnvMaps Tests
// ---------------------------------------------------------------------------

// TestMergeEnvMaps_BothNil verifies that merging two nil maps returns nil.
func TestMergeEnvMaps_BothNil(t *testing.T) {
	result := mergeEnvMaps(nil, nil)
	if result != nil {
		t.Errorf("mergeEnvMaps(nil, nil) = %v, want nil", result)
	}
}

// TestMergeEnvMaps_PipelineNil verifies that nil pipeline env returns task env.
func TestMergeEnvMaps_PipelineNil(t *testing.T) {
	taskEnv := map[string]string{"A": "1"}
	result := mergeEnvMaps(nil, taskEnv)
	if len(result) != 1 || result["A"] != "1" {
		t.Errorf("mergeEnvMaps(nil, task) = %v, want {A: 1}", result)
	}
}

// TestMergeEnvMaps_TaskNil verifies that nil task env returns pipeline env.
func TestMergeEnvMaps_TaskNil(t *testing.T) {
	pipelineEnv := map[string]string{"B": "2"}
	result := mergeEnvMaps(pipelineEnv, nil)
	if len(result) != 1 || result["B"] != "2" {
		t.Errorf("mergeEnvMaps(pipeline, nil) = %v, want {B: 2}", result)
	}
}

// TestMergeEnvMaps_TaskOverridesPipeline verifies that task-level vars
// take precedence when keys conflict.
func TestMergeEnvMaps_TaskOverridesPipeline(t *testing.T) {
	pipelineEnv := map[string]string{"SHARED": "pipeline", "PIPELINE_ONLY": "p"}
	taskEnv := map[string]string{"SHARED": "task", "TASK_ONLY": "t"}

	result := mergeEnvMaps(pipelineEnv, taskEnv)

	if result["SHARED"] != "task" {
		t.Errorf("SHARED = %q, want %q (task should override)", result["SHARED"], "task")
	}
	if result["PIPELINE_ONLY"] != "p" {
		t.Errorf("PIPELINE_ONLY = %q, want %q", result["PIPELINE_ONLY"], "p")
	}
	if result["TASK_ONLY"] != "t" {
		t.Errorf("TASK_ONLY = %q, want %q", result["TASK_ONLY"], "t")
	}
	if len(result) != 3 {
		t.Errorf("result has %d keys, want 3", len(result))
	}
}

// ---------------------------------------------------------------------------
// expandTemplateVars Tests
// ---------------------------------------------------------------------------

// TestExpandTemplateVars_NilEnv verifies that nil env returns args unchanged.
func TestExpandTemplateVars_NilEnv(t *testing.T) {
	args := []string{"${VAR}", "plain"}
	result := expandTemplateVars(args, nil)
	if len(result) != 2 || result[0] != "${VAR}" || result[1] != "plain" {
		t.Errorf("expandTemplateVars(args, nil) = %v, want unchanged", result)
	}
}

// TestExpandTemplateVars_EmptyArgs verifies that empty args returns empty args.
func TestExpandTemplateVars_EmptyArgs(t *testing.T) {
	env := map[string]string{"VAR": "val"}
	result := expandTemplateVars(nil, env)
	if result != nil {
		t.Errorf("expandTemplateVars(nil, env) = %v, want nil", result)
	}
}

// TestExpandTemplateVars_Expansion verifies that ${VAR} references are
// replaced with values from the environment map.
func TestExpandTemplateVars_Expansion(t *testing.T) {
	env := map[string]string{
		"OUTPUT_DIR": "/dist/bin",
		"PLATFORM":   "linux",
	}
	args := []string{
		"-o",
		"${OUTPUT_DIR}/anvil-${PLATFORM}-amd64",
		"plain-arg",
		"${UNKNOWN}",
	}

	result := expandTemplateVars(args, env)

	if result[0] != "-o" {
		t.Errorf("args[0] = %q, want %q", result[0], "-o")
	}
	if result[1] != "/dist/bin/anvil-linux-amd64" {
		t.Errorf("args[1] = %q, want %q", result[1], "/dist/bin/anvil-linux-amd64")
	}
	if result[2] != "plain-arg" {
		t.Errorf("args[2] = %q, want %q", result[2], "plain-arg")
	}
	if result[3] != "${UNKNOWN}" {
		t.Errorf("args[3] = %q, want %q (unresolved should stay as-is)", result[3], "${UNKNOWN}")
	}
}

// TestExpandTemplateVars_MultipleOccurrences verifies that multiple
// occurrences of the same variable in a single arg are all expanded.
func TestExpandTemplateVars_MultipleOccurrences(t *testing.T) {
	env := map[string]string{"X": "hello"}
	args := []string{"${X}-${X}"}

	result := expandTemplateVars(args, env)
	if result[0] != "hello-hello" {
		t.Errorf("args[0] = %q, want %q", result[0], "hello-hello")
	}
}

// TestExpandTemplateVars_NoEnvVarsInArgs verifies that args without
// variable references are returned unchanged.
func TestExpandTemplateVars_NoEnvVarsInArgs(t *testing.T) {
	env := map[string]string{"X": "val"}
	args := []string{"arg1", "arg2", "--flag=value"}

	result := expandTemplateVars(args, env)
	for i, arg := range args {
		if result[i] != arg {
			t.Errorf("args[%d] = %q, want %q (should be unchanged)", i, result[i], arg)
		}
	}
}

// ---------------------------------------------------------------------------
// buildInheritedEnv Tests
// ---------------------------------------------------------------------------

// TestBuildInheritedEnv_NilCustomVars verifies that nil custom vars returns
// the parent environment unchanged.
func TestBuildInheritedEnv_NilCustomVars(t *testing.T) {
	result := buildInheritedEnv(nil)
	parentEnv := os.Environ()

	if len(result) != len(parentEnv) {
		t.Errorf("result has %d entries, want %d (same as parent)", len(result), len(parentEnv))
	}
}

// TestBuildInheritedEnv_MergesWithParent verifies that custom vars are
// merged with the parent environment.
func TestBuildInheritedEnv_MergesWithParent(t *testing.T) {
	customVars := map[string]string{
		"ANVIL_TEST_CUSTOM": "custom-value",
	}

	result := buildInheritedEnv(customVars)

	// Find our custom var.
	found := false
	for _, entry := range result {
		if entry == "ANVIL_TEST_CUSTOM=custom-value" {
			found = true
			break
		}
	}
	if !found {
		t.Error("custom var ANVIL_TEST_CUSTOM not found in result")
	}

	// Verify PATH is still present (parent env preserved).
	hasPATH := false
	for _, entry := range result {
		if strings.HasPrefix(entry, "PATH=") {
			hasPATH = true
			break
		}
	}
	if !hasPATH {
		t.Error("PATH not found in result — parent environment should be preserved")
	}
}

// TestBuildInheritedEnv_CustomOverridesParent verifies that custom vars
// override parent environment variables with the same key.
func TestBuildInheritedEnv_CustomOverridesParent(t *testing.T) {
	// Use PATH as a known parent env var.
	customVars := map[string]string{
		"PATH": "/custom/bin",
	}

	result := buildInheritedEnv(customVars)

	// There should be exactly one PATH entry, and it should be our custom value.
	pathCount := 0
	for _, entry := range result {
		if strings.HasPrefix(entry, "PATH=") {
			pathCount++
			if entry != "PATH=/custom/bin" {
				t.Errorf("PATH entry = %q, want %q", entry, "PATH=/custom/bin")
			}
		}
	}
	if pathCount != 1 {
		t.Errorf("found %d PATH entries, want 1", pathCount)
	}
}
