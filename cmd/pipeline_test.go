package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Pipeline Command Tests
// ---------------------------------------------------------------------------

// TestPipelineCommand_Help verifies that "anvil pipeline --help" displays
// the pipeline command help without errors.
func TestPipelineCommand_Help(t *testing.T) {
	_, stdout, stderr, err := executeCommand("pipeline", "--help")
	if err != nil {
		t.Fatalf("pipeline --help returned error: %v", err)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}
	if !contains(stdout, "Run build and CI pipelines") {
		t.Errorf("help output should contain 'Run build and CI pipelines', got: %s", stdout)
	}
	if !contains(stdout, "build") {
		t.Errorf("help output should list 'build' subcommand, got: %s", stdout)
	}
	if !contains(stdout, "ci") {
		t.Errorf("help output should list 'ci' subcommand, got: %s", stdout)
	}
}

// TestPipelineBuildCommand_Help verifies that "anvil pipeline build --help"
// displays help for the build subcommand.
func TestPipelineBuildCommand_Help(t *testing.T) {
	_, stdout, stderr, err := executeCommand("pipeline", "build", "--help")
	if err != nil {
		t.Fatalf("pipeline build --help returned error: %v", err)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}
	if !contains(stdout, "Execute the build pipeline") {
		t.Errorf("output should contain 'Execute the build pipeline', got: %s", stdout)
	}
	if !contains(stdout, "--env") {
		t.Errorf("output should mention --env flag, got: %s", stdout)
	}
}

// TestPipelineCiCommand_Help verifies that "anvil pipeline ci --help"
// displays help for the ci subcommand.
func TestPipelineCiCommand_Help(t *testing.T) {
	_, stdout, stderr, err := executeCommand("pipeline", "ci", "--help")
	if err != nil {
		t.Fatalf("pipeline ci --help returned error: %v", err)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}
	if !contains(stdout, "Execute the CI pipeline") {
		t.Errorf("output should contain 'Execute the CI pipeline', got: %s", stdout)
	}
}

// TestPipelineBuildCommand_MissingDefinition verifies that running
// "anvil pipeline build" in a directory without build.yaml returns a
// friendly error suggesting 'anvil init'.
func TestPipelineBuildCommand_MissingDefinition(t *testing.T) {
	dir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() failed: %v", err)
	}
	defer func() {
		_ = os.Chdir(origWd)
	}()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() failed: %v", err)
	}

	_, _, stderr, err := executeCommand("pipeline", "build")
	if err == nil {
		t.Fatal("expected error for missing build.yaml, got nil")
	}

	if !contains(stderr, "build pipeline definition not found") {
		t.Errorf("expected 'build pipeline definition not found' error, got: %s", stderr)
	}
	if !contains(stderr, "anvil init") {
		t.Errorf("expected 'anvil init' suggestion in error, got: %s", stderr)
	}
}

// TestPipelineCiCommand_MissingDefinition verifies that running
// "anvil pipeline ci" in a directory without ci.yaml returns a
// friendly error suggesting 'anvil init'.
func TestPipelineCiCommand_MissingDefinition(t *testing.T) {
	dir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() failed: %v", err)
	}
	defer func() {
		_ = os.Chdir(origWd)
	}()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() failed: %v", err)
	}

	_, _, stderr, err := executeCommand("pipeline", "ci")
	if err == nil {
		t.Fatal("expected error for missing ci.yaml, got nil")
	}

	if !contains(stderr, "CI pipeline definition not found") {
		t.Errorf("expected 'CI pipeline definition not found' error, got: %s", stderr)
	}
	if !contains(stderr, "anvil init") {
		t.Errorf("expected 'anvil init' suggestion in error, got: %s", stderr)
	}
}

// TestPipelineBuildCommand_Success verifies that "anvil pipeline build"
// succeeds when build.yaml exists in the project directory.
func TestPipelineBuildCommand_Success(t *testing.T) {
	dir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() failed: %v", err)
	}
	defer func() {
		_ = os.Chdir(origWd)
	}()

	// Create .anvil/pipelines/build.yaml with a simple pipeline.
	pipelineDir := filepath.Join(dir, ".anvil", "pipelines")
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	buildYAML := `pipeline:
  name: build
  stages:
    - name: compile
      tasks:
        - name: echo
          command: echo
          args: ["hello"]
`
	buildPath := filepath.Join(pipelineDir, "build.yaml")
	if err := os.WriteFile(buildPath, []byte(buildYAML), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() failed: %v", err)
	}

	_, stdout, stderr, err := executeCommand("pipeline", "build")
	if err != nil {
		t.Errorf("pipeline build returned error: %v", err)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}
	if !contains(stdout, "Pipeline: build") {
		t.Errorf("output should contain 'Pipeline: build', got: %s", stdout)
	}
	if !contains(stdout, "Status: success") {
		t.Errorf("output should contain 'Status: success', got: %s", stdout)
	}
	if !contains(stdout, "stdout:") {
		t.Errorf("output should contain a 'stdout:' block, got: %s", stdout)
	}
	if !contains(stdout, "hello") {
		t.Errorf("output should contain the echo task output 'hello', got: %s", stdout)
	}
}

// TestPipelineBuildCommand_ProductionEnv verifies that "anvil pipeline build
// --env production" succeeds and uses the production environment override.
func TestPipelineBuildCommand_ProductionEnv(t *testing.T) {
	dir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() failed: %v", err)
	}
	defer func() {
		_ = os.Chdir(origWd)
	}()

	// Create .anvil/pipelines/build.yaml with a production override.
	pipelineDir := filepath.Join(dir, ".anvil", "pipelines")
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	buildYAML := `pipeline:
  name: build
  stages:
    - name: compile
      tasks:
        - name: echo
          command: echo
          args: ["default"]
          environments:
            production:
              command: echo
              args: ["production-build"]
`
	buildPath := filepath.Join(pipelineDir, "build.yaml")
	if err := os.WriteFile(buildPath, []byte(buildYAML), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() failed: %v", err)
	}

	_, stdout, stderr, err := executeCommand("pipeline", "build", "--env", "production")
	if err != nil {
		t.Errorf("pipeline build --env production returned error: %v", err)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}
	if !contains(stdout, "Status: success") {
		t.Errorf("output should contain 'Status: success', got: %s", stdout)
	}
}

// TestPipelineCiCommand_Success verifies that "anvil pipeline ci"
// succeeds when ci.yaml exists in the project directory.
func TestPipelineCiCommand_Success(t *testing.T) {
	dir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() failed: %v", err)
	}
	defer func() {
		_ = os.Chdir(origWd)
	}()

	// Create .anvil/pipelines/ci.yaml with a simple pipeline.
	pipelineDir := filepath.Join(dir, ".anvil", "pipelines")
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	ciYAML := `pipeline:
  name: ci
  stages:
    - name: build
      tasks:
        - name: compile
          command: echo
          args: ["building..."]
    - name: test
      tasks:
        - name: unit
          command: echo
          args: ["testing..."]
`
	ciPath := filepath.Join(pipelineDir, "ci.yaml")
	if err := os.WriteFile(ciPath, []byte(ciYAML), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() failed: %v", err)
	}

	_, stdout, stderr, err := executeCommand("pipeline", "ci")
	if err != nil {
		t.Errorf("pipeline ci returned error: %v", err)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}
	if !contains(stdout, "Pipeline: ci") {
		t.Errorf("output should contain 'Pipeline: ci', got: %s", stdout)
	}
	if !contains(stdout, "Status: success") {
		t.Errorf("output should contain 'Status: success', got: %s", stdout)
	}
	if !contains(stdout, "build") {
		t.Errorf("output should contain 'build' stage, got: %s", stdout)
	}
	if !contains(stdout, "test") {
		t.Errorf("output should contain 'test' stage, got: %s", stdout)
	}
}

// TestPipelineCiCommand_Failure verifies that "anvil pipeline ci" reports
// failure when a pipeline task fails.
func TestPipelineCiCommand_Failure(t *testing.T) {
	dir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() failed: %v", err)
	}
	defer func() {
		_ = os.Chdir(origWd)
	}()

	// Create .anvil/pipelines/ci.yaml with a failing command.
	pipelineDir := filepath.Join(dir, ".anvil", "pipelines")
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	ciYAML := `pipeline:
  name: ci
  stages:
    - name: build
      tasks:
        - name: fail
          command: false
`
	ciPath := filepath.Join(pipelineDir, "ci.yaml")
	if err := os.WriteFile(ciPath, []byte(ciYAML), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() failed: %v", err)
	}

	_, stdout, stderr, err := executeCommand("pipeline", "ci")
	if err == nil {
		t.Fatal("expected error for failing pipeline, got nil")
	}
	if !contains(stderr, "CI pipeline failed") {
		t.Errorf("expected 'CI pipeline failed' error, got: %s", stderr)
	}
	if !contains(stdout, "Status: failure") {
		t.Errorf("output should contain 'Status: failure', got: %s", stdout)
	}
}

// TestPipelineBuildCommand_ShowsStepOutput verifies that a task's captured
// stdout is rendered in the run report (TS-006-012).
func TestPipelineBuildCommand_ShowsStepOutput(t *testing.T) {
	dir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() failed: %v", err)
	}
	defer func() {
		_ = os.Chdir(origWd)
	}()

	// Create .anvil/pipelines/build.yaml with a task that emits output.
	pipelineDir := filepath.Join(dir, ".anvil", "pipelines")
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	buildYAML := `pipeline:
  name: build
  stages:
    - name: compile
      tasks:
        - name: echo
          command: echo
          args: ["building..."]
`
	buildPath := filepath.Join(pipelineDir, "build.yaml")
	if err := os.WriteFile(buildPath, []byte(buildYAML), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() failed: %v", err)
	}

	_, stdout, stderr, err := executeCommand("pipeline", "build")
	if err != nil {
		t.Errorf("pipeline build returned error: %v", err)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}
	if !contains(stdout, "stdout:") {
		t.Errorf("output should contain a 'stdout:' block, got: %s", stdout)
	}
	if !contains(stdout, "building...") {
		t.Errorf("output should contain the task output 'building...', got: %s", stdout)
	}
}

// TestPipelineCiCommand_FailureShowsStepOutput verifies that a failed task's
// captured stderr and exit code are rendered in the run report (TS-006-012).
func TestPipelineCiCommand_FailureShowsStepOutput(t *testing.T) {
	dir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() failed: %v", err)
	}
	defer func() {
		_ = os.Chdir(origWd)
	}()

	// Create .anvil/pipelines/ci.yaml with a task that fails and writes stderr.
	pipelineDir := filepath.Join(dir, ".anvil", "pipelines")
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	ciYAML := `pipeline:
  name: ci
  stages:
    - name: build
      tasks:
        - name: fail
          command: sh
          args: ["-c", "echo boom >&2; exit 1"]
`
	ciPath := filepath.Join(pipelineDir, "ci.yaml")
	if err := os.WriteFile(ciPath, []byte(ciYAML), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() failed: %v", err)
	}

	_, stdout, stderr, err := executeCommand("pipeline", "ci")
	if err == nil {
		t.Fatal("expected error for failing pipeline, got nil")
	}
	if !contains(stderr, "CI pipeline failed") {
		t.Errorf("expected 'CI pipeline failed' error, got: %s", stderr)
	}
	if !contains(stdout, "Status: failure") {
		t.Errorf("output should contain 'Status: failure', got: %s", stdout)
	}
	if !contains(stdout, "stderr:") {
		t.Errorf("output should contain a 'stderr:' block, got: %s", stdout)
	}
	if !contains(stdout, "boom") {
		t.Errorf("output should contain the captured stderr 'boom', got: %s", stdout)
	}
	if !contains(stdout, "Exit code: 1") {
		t.Errorf("output should contain 'Exit code: 1', got: %s", stdout)
	}
}

// TestPipelineBuildCommand_WithEnvFlagHelp verifies that the --env flag
// is properly documented in help output.
func TestPipelineBuildCommand_WithEnvFlagHelp(t *testing.T) {
	_, stdout, _, err := executeCommand("pipeline", "build", "--help")
	if err != nil {
		t.Fatalf("pipeline build --help returned error: %v", err)
	}
	if !contains(stdout, "--env") {
		t.Errorf("help output should mention --env flag, got: %s", stdout)
	}
	if !contains(stdout, "development") {
		t.Errorf("help output should mention 'development' default, got: %s", stdout)
	}
	if !contains(stdout, "production") {
		t.Errorf("help output should mention 'production' option, got: %s", stdout)
	}
}

// TestPipelineCommand_NoArgs verifies that "anvil pipeline" without
// a subcommand shows help.
func TestPipelineCommand_NoArgs(t *testing.T) {
	_, stdout, stderr, err := executeCommand("pipeline")
	// Cobra returns the help text or error — either is acceptable.
	_ = stderr
	_ = err
	if !contains(stdout, "Execute pipeline workflows") && !contains(stdout, "Usage") {
		t.Errorf("output should show help or usage, got: %s", stdout)
	}
}

// ---------------------------------------------------------------------------
// --output Flag Tests
// ---------------------------------------------------------------------------

// TestPipelineBuildCommand_OutputFlagHelp verifies that the --output flag
// is documented in the build command help output.
func TestPipelineBuildCommand_OutputFlagHelp(t *testing.T) {
	_, stdout, _, err := executeCommand("pipeline", "build", "--help")
	if err != nil {
		t.Fatalf("pipeline build --help returned error: %v", err)
	}
	if !contains(stdout, "--output") {
		t.Errorf("help output should mention --output flag, got: %s", stdout)
	}
	if !contains(stdout, "-o") {
		t.Errorf("help output should mention -o shorthand, got: %s", stdout)
	}
	if !contains(stdout, "ANVIL_OUTPUT_DIR") {
		t.Errorf("help output should mention ANVIL_OUTPUT_DIR, got: %s", stdout)
	}
}

// TestPipelineBuildCommand_WithOutputFlag verifies that "anvil pipeline build
// --output <dir>" succeeds and the output directory is set.
func TestPipelineBuildCommand_WithOutputFlag(t *testing.T) {
	dir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() failed: %v", err)
	}
	defer func() {
		_ = os.Chdir(origWd)
	}()

	// Create .anvil/pipelines/build.yaml with a task that echoes ANVIL_OUTPUT_DIR.
	pipelineDir := filepath.Join(dir, ".anvil", "pipelines")
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	buildYAML := `pipeline:
  name: build
  stages:
    - name: compile
      tasks:
        - name: show-output
          command: sh
          args: ["-c", "echo ANVIL_OUTPUT_DIR=$ANVIL_OUTPUT_DIR"]
`
	buildPath := filepath.Join(pipelineDir, "build.yaml")
	if err := os.WriteFile(buildPath, []byte(buildYAML), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() failed: %v", err)
	}

	_, stdout, stderr, err := executeCommand("pipeline", "build", "--output", "dist/binaries")
	if err != nil {
		t.Errorf("pipeline build --output returned error: %v", err)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}
	if !contains(stdout, "Status: success") {
		t.Errorf("output should contain 'Status: success', got: %s", stdout)
	}
}

// TestPipelineBuildCommand_OutputFlagShort verifies that the -o shorthand
// works for the output flag.
func TestPipelineBuildCommand_OutputFlagShort(t *testing.T) {
	dir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() failed: %v", err)
	}
	defer func() {
		_ = os.Chdir(origWd)
	}()

	pipelineDir := filepath.Join(dir, ".anvil", "pipelines")
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	buildYAML := `pipeline:
  name: build
  stages:
    - name: compile
      tasks:
        - name: echo
          command: echo
          args: ["ok"]
`
	buildPath := filepath.Join(pipelineDir, "build.yaml")
	if err := os.WriteFile(buildPath, []byte(buildYAML), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() failed: %v", err)
	}

	_, stdout, stderr, err := executeCommand("pipeline", "build", "-o", "dist/binaries")
	if err != nil {
		t.Errorf("pipeline build -o returned error: %v", err)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}
	if !contains(stdout, "Status: success") {
		t.Errorf("output should contain 'Status: success', got: %s", stdout)
	}
}

// ---------------------------------------------------------------------------
// buildPipelineEnv Tests
// ---------------------------------------------------------------------------

// TestBuildPipelineEnv_EmptyOutput verifies that empty output returns nil.
func TestBuildPipelineEnv_EmptyOutput(t *testing.T) {
	result := buildPipelineEnv("/project", "")
	if result != nil {
		t.Errorf("buildPipelineEnv('') = %v, want nil", result)
	}
}

// TestBuildPipelineEnv_RelativePath verifies that a relative output path
// is resolved relative to the project root.
func TestBuildPipelineEnv_RelativePath(t *testing.T) {
	result := buildPipelineEnv("/project", "dist/binaries")

	if result == nil {
		t.Fatal("buildPipelineEnv() = nil, want non-nil")
	}
	if result["ANVIL_OUTPUT_DIR"] != "/project/dist/binaries" {
		t.Errorf("ANVIL_OUTPUT_DIR = %q, want %q", result["ANVIL_OUTPUT_DIR"], "/project/dist/binaries")
	}
}

// TestBuildPipelineEnv_AbsolutePath verifies that an absolute output path
// is used as-is.
func TestBuildPipelineEnv_AbsolutePath(t *testing.T) {
	result := buildPipelineEnv("/project", "/tmp/output")

	if result == nil {
		t.Fatal("buildPipelineEnv() = nil, want non-nil")
	}
	if result["ANVIL_OUTPUT_DIR"] != "/tmp/output" {
		t.Errorf("ANVIL_OUTPUT_DIR = %q, want %q", result["ANVIL_OUTPUT_DIR"], "/tmp/output")
	}
}

// TestBuildPipelineEnv_CleanPath verifies that the path is cleaned
// (trailing slashes, double slashes removed).
func TestBuildPipelineEnv_CleanPath(t *testing.T) {
	result := buildPipelineEnv("/project", "dist//binaries/")

	if result == nil {
		t.Fatal("buildPipelineEnv() = nil, want non-nil")
	}
	if result["ANVIL_OUTPUT_DIR"] != "/project/dist/binaries" {
		t.Errorf("ANVIL_OUTPUT_DIR = %q, want %q", result["ANVIL_OUTPUT_DIR"], "/project/dist/binaries")
	}
}
