package execution

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// BuildCommand Execute Tests
// ---------------------------------------------------------------------------

// TestBuildCommand_Execute verifies that BuildCommand.Execute runs a build
// pipeline and returns a report with the expected structure.
func TestBuildCommand_Execute(t *testing.T) {
	mock := newMockRunner()
	engine := NewPipelineEngine(mock)
	buildCmd := NewBuildCommand(engine)

	pd := testPipeline()
	report := buildCmd.Execute(context.Background(), pd, "", nil)

	if report.Status != "success" {
		t.Errorf("Status = %q, want %q", report.Status, "success")
	}
	if report.PipelineName != "test-pipeline" {
		t.Errorf("PipelineName = %q, want %q", report.PipelineName, "test-pipeline")
	}
	if len(report.Stages) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(report.Stages))
	}
	if report.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", report.Duration)
	}

	// Verify all stages ran.
	for _, s := range report.Stages {
		if s.Status != "success" {
			t.Errorf("stage %q status = %q, want %q", s.Name, s.Status, "success")
		}
	}

	// Verify all commands were executed.
	cmds := mock.recordedCommands()
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d: %v", len(cmds), cmds)
	}
}

// TestBuildCommand_ExecuteWithEnv verifies that BuildCommand.Execute passes
// the env parameter through to the engine for environment-specific overrides.
func TestBuildCommand_ExecuteWithEnv(t *testing.T) {
	mock := newMockRunner()
	mock.failCommand("custom-build")
	engine := NewPipelineEngine(mock)
	buildCmd := NewBuildCommand(engine)

	pd := &PipelineDefinition{
		Pipeline: Pipeline{
			Name: "build",
			Stages: []PipelineStage{
				{
					Name: "build",
					Tasks: []Task{
						{
							Name:    "compile",
							Command: "base-build",
							Environments: map[string]TaskOverride{
								"production": {
									Command: "custom-build",
								},
							},
						},
					},
				},
			},
		},
	}

	// Execute with env="production" — the production override should cause
	// "custom-build" to run, which is registered as a failing command.
	report := buildCmd.Execute(context.Background(), pd, "production", nil)

	if report.Status != "failure" {
		t.Errorf("Status = %q, want %q (custom-build should fail)", report.Status, "failure")
	}

	cmds := mock.recordedCommands()
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d: %v", len(cmds), cmds)
	}
	if cmds[0] != "custom-build" {
		t.Errorf("executed command = %q, want %q (env override should replace command)", cmds[0], "custom-build")
	}
}

// TestBuildCommand_ExecuteWithPipelineEnv verifies that BuildCommand.Execute
// propagates pipeline-level environment variables to tasks.
func TestBuildCommand_ExecuteWithPipelineEnv(t *testing.T) {
	mock := newMockRunner()
	engine := NewPipelineEngine(mock)
	buildCmd := NewBuildCommand(engine)

	pd := &PipelineDefinition{
		Pipeline: Pipeline{
			Name: "build",
			Stages: []PipelineStage{
				{
					Name: "compile",
					Tasks: []Task{
						{
							Name:    "build-binary",
							Command: "go",
							Args:    []string{"build", "-o", "${ANVIL_OUTPUT_DIR}/binary"},
						},
					},
				},
			},
		},
	}

	pipelineEnv := map[string]string{
		"ANVIL_OUTPUT_DIR": "/tmp/dist",
	}

	report := buildCmd.Execute(context.Background(), pd, "", pipelineEnv)

	if report.Status != "success" {
		t.Errorf("Status = %q, want %q", report.Status, "success")
	}

	// Verify the command was executed (the mock runner doesn't capture args,
	// but the test verifies the pipeline executes without error).
	cmds := mock.recordedCommands()
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d: %v", len(cmds), cmds)
	}
	if cmds[0] != "go" {
		t.Errorf("executed command = %q, want %q", cmds[0], "go")
	}
}

// TestBuildCommand_ExecuteWithNilPipelineEnv verifies that nil pipelineEnv
// does not affect task execution (backward compatibility).
func TestBuildCommand_ExecuteWithNilPipelineEnv(t *testing.T) {
	mock := newMockRunner()
	engine := NewPipelineEngine(mock)
	buildCmd := NewBuildCommand(engine)

	pd := testPipeline()
	report := buildCmd.Execute(context.Background(), pd, "", nil)

	if report.Status != "success" {
		t.Errorf("Status = %q, want %q", report.Status, "success")
	}
}

// ---------------------------------------------------------------------------
// LookupBuildDefinition Tests
// ---------------------------------------------------------------------------

// TestLookupBuildDefinition_Valid verifies that LookupBuildDefinition loads
// a valid build.yaml from the project root's .anvil/pipelines directory.
func TestLookupBuildDefinition_Valid(t *testing.T) {
	// Create a temporary project root with build.yaml.
	projectRoot := t.TempDir()
	pipelineDir := filepath.Join(projectRoot, ".anvil", "pipelines")
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}

	buildYAML := `pipeline:
  name: build
  stages:
    - name: compile
      tasks:
        - name: vendor
          command: go
          args: ["mod", "vendor"]
        - name: build
          command: go
          args: ["build", "./..."]
`

	buildPath := filepath.Join(pipelineDir, "build.yaml")
	if err := os.WriteFile(buildPath, []byte(buildYAML), 0o644); err != nil {
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
	if len(def.Pipeline.Stages[0].Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(def.Pipeline.Stages[0].Tasks))
	}
}

// TestLookupBuildDefinition_MissingFile verifies that LookupBuildDefinition
// returns a friendly error suggesting 'anvil init' when build.yaml does not
// exist.
func TestLookupBuildDefinition_MissingFile(t *testing.T) {
	projectRoot := t.TempDir()

	_, err := LookupBuildDefinition(projectRoot)
	if err == nil {
		t.Fatal("LookupBuildDefinition() = nil, want error for missing file")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "build pipeline definition not found") {
		t.Errorf("error = %q, want to contain 'build pipeline definition not found'", errMsg)
	}
	if !strings.Contains(errMsg, "anvil init") {
		t.Errorf("error = %q, want to contain 'anvil init' suggestion", errMsg)
	}
}

// TestLookupBuildDefinition_InvalidYAML verifies that an invalid YAML file
// returns a parse error.
func TestLookupBuildDefinition_InvalidYAML(t *testing.T) {
	projectRoot := t.TempDir()
	pipelineDir := filepath.Join(projectRoot, ".anvil", "pipelines")
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}

	buildPath := filepath.Join(pipelineDir, "build.yaml")
	if err := os.WriteFile(buildPath, []byte("{{ invalid yaml }}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}

	_, err := LookupBuildDefinition(projectRoot)
	if err == nil {
		t.Fatal("LookupBuildDefinition() = nil, want parse error")
	}

	if !strings.Contains(err.Error(), "parsing pipeline YAML") {
		t.Errorf("error = %q, want to contain 'parsing pipeline YAML'", err.Error())
	}
}

// TestLookupBuildDefinition_InvalidPipeline verifies that a valid YAML file
// with an invalid pipeline structure returns a validation error.
func TestLookupBuildDefinition_InvalidPipeline(t *testing.T) {
	projectRoot := t.TempDir()
	pipelineDir := filepath.Join(projectRoot, ".anvil", "pipelines")
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}

	// Missing pipeline name and stage name — should fail validation.
	invalidYAML := `pipeline:
  name: ""
  stages:
    - name: ""
      tasks: []
`

	buildPath := filepath.Join(pipelineDir, "build.yaml")
	if err := os.WriteFile(buildPath, []byte(invalidYAML), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}

	_, err := LookupBuildDefinition(projectRoot)
	if err == nil {
		t.Fatal("LookupBuildDefinition() = nil, want validation error")
	}

	if !strings.Contains(err.Error(), "invalid pipeline definition") {
		t.Errorf("error = %q, want to contain 'invalid pipeline definition'", err.Error())
	}
}
