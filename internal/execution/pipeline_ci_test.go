package execution

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// CICommand Execute Tests
// ---------------------------------------------------------------------------

// TestCICommand_Execute verifies that CICommand.Execute runs a CI pipeline
// and returns a report with the expected stages and tasks.
func TestCICommand_Execute(t *testing.T) {
	mock := newMockRunner()
	engine := NewPipelineEngine(mock)
	ciCmd := NewCICommand(engine)

	pd := DefaultCIPipeline()
	report := ciCmd.Execute(context.Background(), &pd)

	if report.Status != "success" {
		t.Errorf("Status = %q, want %q", report.Status, "success")
	}
	if report.PipelineName != "ci" {
		t.Errorf("PipelineName = %q, want %q", report.PipelineName, "ci")
	}
	if len(report.Stages) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(report.Stages))
	}
	if report.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", report.Duration)
	}

	// Stage names.
	if report.Stages[0].Name != "build" {
		t.Errorf("Stage[0].Name = %q, want %q", report.Stages[0].Name, "build")
	}
	if report.Stages[1].Name != "test" {
		t.Errorf("Stage[1].Name = %q, want %q", report.Stages[1].Name, "test")
	}

	// Verify all stages succeeded.
	for _, s := range report.Stages {
		if s.Status != "success" {
			t.Errorf("stage %q status = %q, want %q", s.Name, s.Status, "success")
		}
	}

	// Verify all tasks were recorded.
	cmds := mock.recordedCommands()
	if len(cmds) != 4 {
		t.Fatalf("expected 4 commands, got %d: %v", len(cmds), cmds)
	}
}

// TestCICommand_StageOrder verifies that in a CI pipeline, the build stage
// executes before the test stage.
func TestCICommand_StageOrder(t *testing.T) {
	mock := newMockRunner()
	engine := NewPipelineEngine(mock)
	ciCmd := NewCICommand(engine)

	pd := DefaultCIPipeline()
	report := ciCmd.Execute(context.Background(), &pd)

	if len(report.Stages) < 2 {
		t.Fatalf("expected at least 2 stages, got %d", len(report.Stages))
	}

	// Build stage must come first.
	if report.Stages[0].Name != "build" {
		t.Errorf("Stage[0].Name = %q, want %q (build must run first)", report.Stages[0].Name, "build")
	}
	if report.Stages[1].Name != "test" {
		t.Errorf("Stage[1].Name = %q, want %q (test must run second)", report.Stages[1].Name, "test")
	}

	// Verify execution order via recorded commands.
	cmds := mock.recordedCommands()
	if len(cmds) != 4 {
		t.Fatalf("expected 4 commands, got %d: %v", len(cmds), cmds)
	}

	// The first command should be the build stage's "echo building..."
	// The remaining three should be the test stage tasks.
	if cmds[0] != "echo" {
		t.Errorf("first command = %q, want %q (build stage should run first)", cmds[0], "echo")
	}
}

// TestCICommand_ExecuteFailure verifies that when a CI pipeline task fails,
// the report status reflects the failure and remaining stages are skipped.
func TestCICommand_ExecuteFailure(t *testing.T) {
	mock := newMockRunner()
	mock.failCommand("echo") // All DefaultCIPipeline tasks use "echo"
	engine := NewPipelineEngine(mock)
	ciCmd := NewCICommand(engine)

	pd := DefaultCIPipeline()
	report := ciCmd.Execute(context.Background(), &pd)

	if report.Status != "failure" {
		t.Errorf("Status = %q, want %q", report.Status, "failure")
	}

	// Build stage had 1 task which failed. Test stage should be skipped.
	if len(report.Stages) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(report.Stages))
	}

	if report.Stages[0].Status != "failure" {
		t.Errorf("Stage[0].Status = %q, want %q", report.Stages[0].Status, "failure")
	}
	if report.Stages[1].Status != "skipped" {
		t.Errorf("Stage[1].Status = %q, want %q (should be skipped on build failure)", report.Stages[1].Status, "skipped")
	}
}

// ---------------------------------------------------------------------------
// LookupCIDefinition Tests
// ---------------------------------------------------------------------------

// TestLookupCIDefinition_Valid verifies that LookupCIDefinition loads a valid
// ci.yaml from the project root's .anvil/pipelines directory.
func TestLookupCIDefinition_Valid(t *testing.T) {
	projectRoot := t.TempDir()
	pipelineDir := filepath.Join(projectRoot, ".anvil", "pipelines")
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}

	ciYAML := `pipeline:
  name: ci
  stages:
    - name: build
      tasks:
        - name: compile
          command: go
          args: ["build", "./..."]
    - name: test
      tasks:
        - name: unit
          command: go
          args: ["test", "./..."]
        - name: lint
          command: golangci-lint
          args: ["run"]
`

	ciPath := filepath.Join(pipelineDir, "ci.yaml")
	if err := os.WriteFile(ciPath, []byte(ciYAML), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}

	def, err := LookupCIDefinition(projectRoot)
	if err != nil {
		t.Fatalf("LookupCIDefinition() = %v, want nil", err)
	}

	if def.Pipeline.Name != "ci" {
		t.Errorf("Pipeline.Name = %q, want %q", def.Pipeline.Name, "ci")
	}
	if len(def.Pipeline.Stages) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(def.Pipeline.Stages))
	}
	if len(def.Pipeline.Stages[1].Tasks) != 2 {
		t.Fatalf("expected 2 tasks in test stage, got %d", len(def.Pipeline.Stages[1].Tasks))
	}
}

// TestLookupCIDefinition_MissingFile verifies that LookupCIDefinition returns
// a friendly error suggesting 'anvil init' when ci.yaml does not exist.
func TestLookupCIDefinition_MissingFile(t *testing.T) {
	projectRoot := t.TempDir()

	_, err := LookupCIDefinition(projectRoot)
	if err == nil {
		t.Fatal("LookupCIDefinition() = nil, want error for missing file")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "CI pipeline definition not found") {
		t.Errorf("error = %q, want to contain 'CI pipeline definition not found'", errMsg)
	}
	if !strings.Contains(errMsg, "anvil init") {
		t.Errorf("error = %q, want to contain 'anvil init' suggestion", errMsg)
	}
}

// TestLookupCIDefinition_InvalidYAML verifies that an invalid CI pipeline
// YAML file returns a parse error.
func TestLookupCIDefinition_InvalidYAML(t *testing.T) {
	projectRoot := t.TempDir()
	pipelineDir := filepath.Join(projectRoot, ".anvil", "pipelines")
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}

	ciPath := filepath.Join(pipelineDir, "ci.yaml")
	if err := os.WriteFile(ciPath, []byte("{{ invalid yaml }}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}

	_, err := LookupCIDefinition(projectRoot)
	if err == nil {
		t.Fatal("LookupCIDefinition() = nil, want parse error")
	}

	if !strings.Contains(err.Error(), "parsing pipeline YAML") {
		t.Errorf("error = %q, want to contain 'parsing pipeline YAML'", err.Error())
	}
}
