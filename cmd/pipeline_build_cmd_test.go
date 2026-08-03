package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// writeBuildPipeline writes a custom build.yaml into the given directory
// and returns the project root. The pipeline mirrors the Flutter template
// shape (three targets with platform metadata) but uses echo commands so
// no external toolchain is required.
func writeBuildPipeline(t *testing.T, dir string, yamlContent string) {
	t.Helper()
	pipelineDir := filepath.Join(dir, ".anvil", "pipelines")
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	buildPath := filepath.Join(pipelineDir, "build.yaml")
	if err := os.WriteFile(buildPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}
}

// unsupportedPlatform returns a platform identifier that is guaranteed NOT
// to be the host platform, so tests can declare tasks that are unsupported
// deterministically on any host (ADR-018 mock platform injection).
func unsupportedPlatform() string {
	if runtime.GOOS == "darwin" {
		return "linux"
	}
	return "darwin"
}

// TestPipelineBuildCommand_TargetFlagRunsOnlyRequestedTasks verifies that
// "anvil pipeline build --target web,apk" executes only the web and apk
// tasks: the report shows those tasks and does not show the excluded ios
// task (TS-P7-24 AC-1).
func TestPipelineBuildCommand_TargetFlagRunsOnlyRequestedTasks(t *testing.T) {
	dir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() failed: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	writeBuildPipeline(t, dir, `pipeline:
  name: build
  stages:
    - name: build
      tasks:
        - name: flutter-web
          command: echo
          args: ["web"]
          metadata:
            platforms: [linux, darwin, windows]
            target: web
        - name: flutter-apk
          command: echo
          args: ["apk"]
          metadata:
            platforms: [linux, darwin, windows]
            target: apk
        - name: flutter-ios
          command: echo
          args: ["ios"]
          metadata:
            platforms: [darwin]
            target: ios
`)

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() failed: %v", err)
	}

	_, stdout, stderr, err := executeCommand("pipeline", "build", "--target", "web,apk")
	if err != nil {
		t.Errorf("pipeline build --target web,apk returned error: %v", err)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}
	if !contains(stdout, "Status: success") {
		t.Errorf("output should contain 'Status: success', got: %s", stdout)
	}
	if !contains(stdout, "flutter-web") {
		t.Errorf("output should contain the web task, got: %s", stdout)
	}
	if !contains(stdout, "flutter-apk") {
		t.Errorf("output should contain the apk task, got: %s", stdout)
	}
	if contains(stdout, "flutter-ios") {
		t.Errorf("output should NOT contain the excluded ios task, got: %s", stdout)
	}
}

// TestPipelineBuildCommand_UnknownTargetErrorsBeforeExecution verifies that
// an unknown --target name is rejected before any task executes, with an
// error listing the known targets (TS-P7-24 AC-3).
func TestPipelineBuildCommand_UnknownTargetErrorsBeforeExecution(t *testing.T) {
	dir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() failed: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	writeBuildPipeline(t, dir, `pipeline:
  name: build
  stages:
    - name: build
      tasks:
        - name: flutter-web
          command: echo
          args: ["web"]
          metadata:
            platforms: [linux, darwin, windows]
            target: web
`)

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() failed: %v", err)
	}

	_, stdout, stderr, err := executeCommand("pipeline", "build", "--target", "xyz")
	if err == nil {
		t.Fatal("expected error for unknown target, got nil")
	}
	if !contains(stderr, `unknown target "xyz"`) {
		t.Errorf("expected 'unknown target \"xyz\"' error, got: %s", stderr)
	}
	if !contains(stderr, "known targets: web") {
		t.Errorf("expected known targets list in error, got: %s", stderr)
	}
	if contains(stdout, "Status:") {
		t.Errorf("pipeline should not run with an invalid target (no report expected), got: %s", stdout)
	}
}

// TestPipelineBuildCommand_StrictFlagFailsOnUnsupportedTarget verifies that
// "anvil pipeline build --strict" fails the pipeline when a requested target
// is unsupported on the current platform, instead of skipping it (TS-P7-24
// AC-2, ADR-018). The unsupported platform is derived from the host so the
// test is deterministic on any machine.
func TestPipelineBuildCommand_StrictFlagFailsOnUnsupportedTarget(t *testing.T) {
	dir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() failed: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	unsupported := unsupportedPlatform()
	writeBuildPipeline(t, dir, `pipeline:
  name: build
  stages:
    - name: build
      tasks:
        - name: flutter-ios
          command: echo
          args: ["ios"]
          metadata:
            platforms: [`+unsupported+`]
            target: ios
`)

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() failed: %v", err)
	}

	_, stdout, stderr, err := executeCommand("pipeline", "build", "--strict")
	if err == nil {
		t.Fatal("expected error for unsupported target in strict mode, got nil")
	}
	if !contains(stderr, "build pipeline failed") {
		t.Errorf("expected 'build pipeline failed' error, got: %s", stderr)
	}
	if !contains(stdout, "Status: failure") {
		t.Errorf("output should contain 'Status: failure', got: %s", stdout)
	}
	if !contains(stdout, "not supported") {
		t.Errorf("output should show the unsupported-target error, got: %s", stdout)
	}
}

// TestPipelineBuildCommand_TargetStrictFlagsInHelp verifies that --target
// and --strict are documented in the build command help output (TS-P7-24).
func TestPipelineBuildCommand_TargetStrictFlagsInHelp(t *testing.T) {
	_, stdout, _, err := executeCommand("pipeline", "build", "--help")
	if err != nil {
		t.Fatalf("pipeline build --help returned error: %v", err)
	}
	if !contains(stdout, "--target") {
		t.Errorf("help output should mention --target flag, got: %s", stdout)
	}
	if !contains(stdout, "--strict") {
		t.Errorf("help output should mention --strict flag, got: %s", stdout)
	}
}

// TestParseTargetList verifies the comma-separated --target parsing.
func TestParseTargetList(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{name: "empty", input: "", want: nil},
		{name: "single", input: "web", want: []string{"web"}},
		{name: "comma separated", input: "web,apk", want: []string{"web", "apk"}},
		{name: "spaces trimmed", input: "web, apk,ios", want: []string{"web", "apk", "ios"}},
		{name: "whitespace only", input: " , , ", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTargetList(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("parseTargetList(%q) = %v, want %v", tt.input, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseTargetList(%q) = %v, want %v", tt.input, got, tt.want)
				}
			}
		})
	}
}
