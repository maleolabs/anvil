package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"maleolabs.com/anvil/internal/contracts"
	"maleolabs.com/anvil/internal/execution"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/project"
	"maleolabs.com/anvil/internal/registry"
)

// executeCommand is a helper that creates a command, sets args, captures
// stdout/stderr, and runs it.
//
// It resets all flags' Changed state on the entire command tree before
// each execution to prevent stale flag values from cross-test leakage.
func executeCommand(args ...string) (*cobra.Command, string, string, error) {
	bufOut := new(bytes.Buffer)
	bufErr := new(bytes.Buffer)

	root := rootCmd

	// Reset all flags' Changed state on the entire command tree to prevent
	// stale flag values from cross-test contamination.
	resetFlags(root)

	root.SetOut(bufOut)
	root.SetErr(bufErr)
	root.SetArgs(args)

	// Use Execute() (not root.Execute()) so that suggestion propagation
	// and other Execute-level setup runs.
	err := Execute()
	return root, bufOut.String(), bufErr.String(), err
}

// resetFlags recursively resets flags on a command and its subcommands
// to their default values and clears the Changed state. This prevents
// stale flag values from leaking between test executions through shared
// cobra command instances.
func resetFlags(cmd *cobra.Command) {
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		f.Changed = false
		_ = f.Value.Set(f.DefValue)
	})
	cmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		f.Changed = false
		_ = f.Value.Set(f.DefValue)
	})
	for _, sub := range cmd.Commands() {
		resetFlags(sub)
	}
}

// TestInitCommand_CreatesProject verifies that running:
//
//	anvil init my-project
//
// creates a new project with the configuration file. No pipeline template
// files are generated: template content is distribution content supplied
// by delivery lifecycle standards, never engine content (TS-015-01-02,
// ADR-026 decision 1).
func TestInitCommand_CreatesProject(t *testing.T) {
	dir := t.TempDir()
	_, _, _, err := executeCommand("init", "test-project", "--path", dir)
	if err != nil {
		t.Fatalf("init command returned unexpected error: %v", err)
	}

	s := project.NewStructure(dir)

	// Config file must exist.
	if !fileExists(s.ConfigFile) {
		t.Errorf("expected config file %s to exist", s.ConfigFile)
	}

	// No pipeline template files without a framework declaration.
	buildPath := filepath.Join(s.PipelinesDir, project.PipelineBuildFileName)
	if fileExists(buildPath) {
		t.Errorf("expected no pipeline config %s — the runtime owns no template content", buildPath)
	}
	ciPath := filepath.Join(s.PipelinesDir, project.PipelineCIFileName)
	if fileExists(ciPath) {
		t.Errorf("expected no pipeline config %s — the runtime owns no template content", ciPath)
	}
}

// TestInitCommand_WithoutName verifies that running:
//
//	anvil init
//
// (without a project name) produces an error.
func TestInitCommand_WithoutName(t *testing.T) {
	_, _, stderr, err := executeCommand("init")
	if err == nil {
		t.Fatal("expected error for missing project name, got nil")
	}
	if !contains(stderr, "requires 1 argument") {
		t.Errorf("expected error about missing project name, got: %s", stderr)
	}
}

// TestInitCommand_InExistingProject verifies that running:
//
//	anvil init my-project
//
// in a directory that already contains a project reports the existing state
// and does not overwrite it.
func TestInitCommand_InExistingProject(t *testing.T) {
	dir := t.TempDir()

	// First init should succeed.
	_, _, _, err := executeCommand("init", "first-project", "--path", dir)
	if err != nil {
		t.Fatalf("first init failed: %v", err)
	}

	configPath := filepath.Join(dir, project.ConfigFileName)
	originalConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config file: %v", err)
	}

	// Second init should fail.
	_, _, stderr, err := executeCommand("init", "second-project", "--path", dir)
	if err == nil {
		t.Fatal("expected error for existing project, got nil")
	}
	if !contains(stderr, "project already exists") {
		t.Errorf("expected 'project already exists' error, got: %s", stderr)
	}

	// Config should not have been modified.
	currentConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config after failed init: %v", err)
	}
	if string(currentConfig) != string(originalConfig) {
		t.Error("config file was modified after failed initialization")
	}
}

// TestInitCommand_InvalidName verifies that:
//
//	anvil init "invalid name!"
//
// produces an error about invalid characters.
func TestInitCommand_InvalidName(t *testing.T) {
	dir := t.TempDir()
	_, _, stderr, err := executeCommand("init", "invalid name!", "--path", dir)
	if err == nil {
		t.Fatal("expected error for invalid project name, got nil")
	}
	if !contains(stderr, "invalid project name") {
		t.Errorf("expected 'invalid project name' error, got: %s", stderr)
	}
	if !contains(stderr, "letters, numbers, hyphens") {
		t.Errorf("expected allowed characters hint, got: %s", stderr)
	}
}

// TestInitCommand_WithCustomPath verifies that:
//
//	anvil init my-project --path /custom/path
//
// creates the project at the specified path. No pipeline template files
// are generated without a framework declaration (TS-015-01-02).
func TestInitCommand_WithCustomPath(t *testing.T) {
	parent := t.TempDir()
	targetPath := filepath.Join(parent, "nested", "dir")

	_, _, _, err := executeCommand("init", "nested-app", "--path", targetPath)
	if err != nil {
		t.Fatalf("init with custom path failed: %v", err)
	}

	s := project.NewStructure(targetPath)
	if !fileExists(s.ConfigFile) {
		t.Error("expected anvil.yaml to exist at custom path")
	}

	buildPath := filepath.Join(s.PipelinesDir, project.PipelineBuildFileName)
	if fileExists(buildPath) {
		t.Errorf("expected no pipeline config %s — the runtime owns no template content", buildPath)
	}
	ciPath := filepath.Join(s.PipelinesDir, project.PipelineCIFileName)
	if fileExists(ciPath) {
		t.Errorf("expected no pipeline config %s — the runtime owns no template content", ciPath)
	}
}

// TestInitCommand_SuccessMessage verifies that a successful init produces
// a user-friendly success message.
func TestInitCommand_SuccessMessage(t *testing.T) {
	dir := t.TempDir()
	_, stdout, _, err := executeCommand("init", "my-app", "--path", dir)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}

	if !contains(stdout, "created") {
		t.Errorf("expected success message containing 'created', got: %s", stdout)
	}
	if !contains(stdout, "Next steps") {
		t.Errorf("expected 'Next steps' guidance, got: %s", stdout)
	}
}

// TestInitCommand_ExitCode verifies that a failed init returns exit code 1.
func TestInitCommand_ExitCode(t *testing.T) {
	// Missing name should return an error (which Cobra converts to exit code 1).
	_, _, _, err := executeCommand("init")
	if err == nil {
		t.Error("expected error for missing name")
	}

	// Invalid name should return an error.
	_, _, _, err = executeCommand("init", "bad/name")
	if err == nil {
		t.Error("expected error for invalid name")
	}
}

// TestInitCommand_WithFrameworkLaravel_NoStandard_HardFails verifies that:
//
//	anvil init app --framework laravel
//
// with no installed delivery lifecycle standard HARD-FAILS initialization
// (TS-015-02-02, ADR-026 decision 3): an explicit framework declaration
// requires the standard recorded in the installed-standard registry. The
// failure is actionable — it names what is missing (the standard for the
// declared framework) and how to resolve it (install the standard) — no
// project files are created, and the process exits non-zero. There is no
// graceful fallback to a generic lifecycle and no silent degradation
// (ADR-026 §4).
func TestInitCommand_WithFrameworkLaravel_NoStandard_HardFails(t *testing.T) {
	isolateGlobalConfigDir(t)
	dir := t.TempDir()
	_, stdout, stderr, err := executeCommand("init", "app", "--framework", "laravel", "--path", dir)
	if err == nil {
		t.Fatal("expected init to hard-fail when the declared framework's standard is not installed, got nil")
	}
	requireExitCode(t, err, output.ExitCodePrecondition)

	// The hard-fail message must state what is missing and how to resolve
	// it: the missing standard id and the install remediation.
	for _, want := range []string{
		"Error:",
		"is not installed",
		"anvil-standard-laravel",
		"anvil standard install anvil-standard-laravel",
		"re-run 'anvil init app --framework laravel'",
	} {
		if !contains(stderr, want) {
			t.Errorf("expected hard-fail message to mention %q, got: %s", want, stderr)
		}
	}

	// No project files may be created on the hard-fail.
	if fileExists(filepath.Join(dir, project.ConfigFileName)) {
		t.Error("config file must not be written when the standard is missing")
	}

	// No success output on the failure path.
	if contains(stdout, "created") {
		t.Errorf("expected no success message on hard-fail, got: %s", stdout)
	}
}

// TestInitCommand_FrameworkDeclaration_NoWhitelistRejection verifies that
// the Core still owns no framework whitelist (TS-015-01-03, ADR-026
// decision 1): a declaration of an unlisted framework fails ONLY because
// its delivery lifecycle standard is not installed — the failure is the
// standard-missing hard-fail (TS-015-02-02) with install remediation, not
// an "unknown framework" rejection.
func TestInitCommand_FrameworkDeclaration_NoWhitelistRejection(t *testing.T) {
	isolateGlobalConfigDir(t)
	dir := t.TempDir()
	_, _, stderr, err := executeCommand("init", "app", "--framework", "symfony", "--path", dir)
	if err == nil {
		t.Fatal("expected init to hard-fail for a declared framework without an installed standard, got nil")
	}
	requireExitCode(t, err, output.ExitCodePrecondition)

	// The failure must be the standard-missing hard-fail with actionable
	// remediation — never an "unknown framework" rejection.
	if contains(stderr, "unknown framework") {
		t.Errorf("the Core must not reject unknown frameworks (no whitelist), got: %s", stderr)
	}
	for _, want := range []string{
		"is not installed",
		"anvil-standard-symfony",
		"anvil standard install anvil-standard-symfony",
	} {
		if !contains(stderr, want) {
			t.Errorf("expected hard-fail message to mention %q, got: %s", want, stderr)
		}
	}

	// No project may be created on the hard-fail.
	if fileExists(filepath.Join(dir, project.ConfigFileName)) {
		t.Error("config file must not be written when the standard is missing")
	}
}

// TestInitCommand_WithFrameworkFlutter verifies that:
//
//	anvil init app --framework flutter
//
// succeeds, reports the framework in the success message, stores the
// framework in anvil.yaml, and generates .anvil/pipelines/build.yaml with
// the Flutter build targets and their platform metadata fetched from the
// adapter's template command (TS-P7-27 AC-1..AC-4, TS-007-038 / ADR-020
// §1). The delivery lifecycle standard anvil-standard-flutter is recorded
// first so the framework declaration passes the standard-missing
// hard-fail gate (TS-015-02-02) and reaches the adapter-driven template
// generation.
func TestInitCommand_WithFrameworkFlutter(t *testing.T) {
	// The engine resolves the adapter through exec.LookPath and invokes
	// its template command (ADR-020 §1): put a stub adapter answering
	// with the Flutter template on PATH.
	adapterDir := t.TempDir()
	writeTemplateAdapter(t, adapterDir, "anvil-adapter-flutter", flutterTemplateJSON(t))

	// Record the installed standard anvil-standard-flutter (EPIC-014):
	// the declaration resolves to it (TS-015-02-01), passing the
	// standard-missing hard-fail (TS-015-02-02).
	seedInstalledStandard(t, "anvil-standard-flutter", "2.0.0")

	dir := t.TempDir()
	_, stdout, stderr, err := executeCommand("init", "app", "--framework", "flutter", "--path", dir)
	if err != nil {
		t.Fatalf("init with framework failed: %v\nstderr: %s", err, stderr)
	}
	if !contains(stdout, "created") {
		t.Errorf("expected success message, got: %s", stdout)
	}
	if !contains(stdout, "flutter") {
		t.Errorf("expected success message to mention framework 'flutter', got: %s", stdout)
	}

	// Framework must be stored in anvil.yaml.
	data, err := os.ReadFile(filepath.Join(dir, project.ConfigFileName))
	if err != nil {
		t.Fatalf("reading config file: %v", err)
	}
	if !contains(string(data), "framework: flutter") {
		t.Errorf("expected 'framework: flutter' in anvil.yaml, got:\n%s", string(data))
	}

	// build.yaml must be the Flutter build template: the three targets
	// with their platform metadata.
	buildData, err := os.ReadFile(filepath.Join(dir, ".anvil", "pipelines", project.PipelineBuildFileName))
	if err != nil {
		t.Fatalf("reading build.yaml: %v", err)
	}
	for _, want := range []string{
		"name: flutter-web",
		"name: flutter-apk",
		"name: flutter-ios",
		"command: flutter",
		"platforms:",
		"target: web",
		"target: apk",
		"target: ios",
	} {
		if !contains(string(buildData), want) {
			t.Errorf("build.yaml should contain %q, got:\n%s", want, string(buildData))
		}
	}
}

// writeTemplateAdapter writes an executable stub adapter named name into
// dir that answers the template command with templateJSON on stdout and
// exits 0 (unknown commands exit 2 per 005-adapter-command-contract
// §10.2), then puts dir first on PATH. The engine resolves adapters via
// exec.LookPath (ADR-020 §1), so the stub must be reachable through
// PATH. The JSON is produced from the real contract types, so the wire
// shape is exercised end to end.
func writeTemplateAdapter(t *testing.T, dir, name, templateJSON string) {
	t.Helper()
	path := filepath.Join(dir, name)
	script := fmt.Sprintf(`#!/bin/sh
# Stub adapter for CLI init tests: answers the template command only.
case "$1" in
  template) printf '%%s\n' '%s' ;;
  *) echo "unknown command $1" >&2; exit 2 ;;
esac
exit 0
`, templateJSON)
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write stub adapter %s: %v", name, err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// flutterTemplateJSON returns the TemplateResult JSON the Flutter adapter
// returns for the template command, marshaled from the real contract
// types so the fixture matches the wire shape exactly (TS-007-038).
func flutterTemplateJSON(t *testing.T) string {
	t.Helper()
	result := contracts.TemplateResult{
		Build: &execution.PipelineDefinition{
			Pipeline: execution.Pipeline{
				Name: "build",
				Stages: []execution.PipelineStage{
					{
						Name: "build",
						Tasks: []execution.Task{
							{Name: "flutter-web", Command: "flutter", Args: []string{"build", "web"}, Timeout: "10m", Metadata: &execution.TaskMetadata{Platforms: []string{"linux", "darwin", "windows"}, Target: "web"}},
							{Name: "flutter-apk", Command: "flutter", Args: []string{"build", "apk", "--release"}, Timeout: "15m", Metadata: &execution.TaskMetadata{Platforms: []string{"linux", "darwin", "windows"}, Target: "apk"}},
							{Name: "flutter-ios", Command: "flutter", Args: []string{"build", "ios", "--release"}, Metadata: &execution.TaskMetadata{Platforms: []string{"darwin"}, Target: "ios"}},
						},
					},
				},
			},
		},
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal flutter template: %v", err)
	}
	return string(data)
}

// TestInitCommand_ResolvedStandard verifies the resolution success case
// (TS-015-02-01 DoD: a declared framework resolves to the installed
// standard): with the delivery lifecycle standard
// anvil-standard-laravel recorded in the installed-standard store
// (EPIC-014), `anvil init --framework laravel` resolves it — the
// resolution is explicit and recorded, the success message names the
// resolved standard id and version, and the framework declaration is
// stored in anvil.yaml.
func TestInitCommand_ResolvedStandard(t *testing.T) {
	isolateGlobalConfigDir(t)
	dir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		t.Fatalf("installed-standards dir: %v", err)
	}
	now := time.Now().UTC()
	rec := registry.InstalledStandardRecord{
		FormatVersion:   registry.RecordFormatVersion,
		ID:              "anvil-standard-laravel",
		Version:         "1.2.3",
		ContractVersion: "1.0.0",
		Resolution:      registry.Resolution{Kind: registry.ResolutionKindIndex, Source: "/registry"},
		InstalledAt:     now,
		UpdatedAt:       now,
		Lifecycle:       registry.Lifecycle{State: registry.LifecycleStatePublished},
	}
	if _, _, err := registry.NewInstalledStandardStore(dir).Record(rec.ID, rec); err != nil {
		t.Fatalf("record installed standard: %v", err)
	}

	projectDir := t.TempDir()
	_, stdout, stderr, err := executeCommand("init", "app", "--framework", "laravel", "--path", projectDir)
	if err != nil {
		t.Fatalf("init with framework failed: %v\nstderr: %s", err, stderr)
	}
	if !contains(stdout, "resolved delivery lifecycle standard anvil-standard-laravel 1.2.3") {
		t.Errorf("expected success message to report the resolved standard 'anvil-standard-laravel 1.2.3', got: %s", stdout)
	}
	if !contains(stdout, "laravel") {
		t.Errorf("expected success message to mention framework 'laravel', got: %s", stdout)
	}

	data, err := os.ReadFile(filepath.Join(projectDir, project.ConfigFileName))
	if err != nil {
		t.Fatalf("reading config file: %v", err)
	}
	if !contains(string(data), "framework: laravel") {
		t.Errorf("expected 'framework: laravel' in anvil.yaml, got:\n%s", string(data))
	}
}

// TestInitCommand_FrameworkNoStandard_HardFails verifies the
// standard-missing hard-fail at the command surface (TS-015-02-02 DoD):
// an explicit framework declaration with no installed standard fails
// initialization with an actionable message — what is missing (the
// standard for the declared framework) and how to resolve it (install the
// standard) — no project files are created, and the failure carries no
// resolution claim. No silent degradation and no graceful fallback
// (ADR-026 §4).
func TestInitCommand_FrameworkNoStandard_HardFails(t *testing.T) {
	isolateGlobalConfigDir(t)
	dir := t.TempDir()
	_, stdout, stderr, err := executeCommand("init", "app", "--framework", "rails", "--path", dir)
	if err == nil {
		t.Fatal("expected init to hard-fail when the declared framework's standard is not installed, got nil")
	}
	requireExitCode(t, err, output.ExitCodePrecondition)
	if contains(stdout, "created") {
		t.Errorf("expected no success message on hard-fail, got: %s", stdout)
	}
	if contains(stdout, "resolved delivery lifecycle standard") {
		t.Errorf("no-match must not claim a resolution, got: %s", stdout)
	}

	// The hard-fail message must state what is missing and how to resolve
	// it: the missing standard id and the install remediation.
	for _, want := range []string{
		"Error:",
		"is not installed",
		"anvil-standard-rails",
		"anvil standard install anvil-standard-rails",
	} {
		if !contains(stderr, want) {
			t.Errorf("expected hard-fail message to mention %q, got: %s", want, stderr)
		}
	}

	// No project files may be created on the hard-fail.
	if fileExists(filepath.Join(dir, project.ConfigFileName)) {
		t.Error("config file must not be written when the standard is missing")
	}
}

// TestInitCommand_NoFrameworkDeclaration_Unaffected verifies that projects
// WITHOUT a framework declaration remain fully functional (TS-015-02-02
// DoD, ADR-026 §12.2 — non-breaking): plain `anvil init` succeeds even
// when the installed-standard store is empty. The standard-missing
// hard-fail applies only to explicit declarations; nothing about a plain
// init reads the registry.
func TestInitCommand_NoFrameworkDeclaration_Unaffected(t *testing.T) {
	isolateGlobalConfigDir(t)
	dir := t.TempDir()
	_, stdout, _, err := executeCommand("init", "plain-app", "--path", dir)
	if err != nil {
		t.Fatalf("plain init must remain fully functional without a framework declaration: %v", err)
	}
	if !contains(stdout, "created") {
		t.Errorf("expected success message, got: %s", stdout)
	}
	if !fileExists(filepath.Join(dir, project.ConfigFileName)) {
		t.Error("expected anvil.yaml to exist for a plain project")
	}
}

// TestInitCommand_ConfigExtensionMerged verifies the config extension
// consuming side at the command surface (TS-015-03-01 DoD: framework
// config keys resolve from the installed standard and their defaults
// merge into the project configuration): with the delivery lifecycle
// standard anvil-standard-laravel recorded with configuration extension
// content, `anvil init --framework laravel` resolves the standard and the
// declared keys and defaults land in anvil.yaml under the framework's own
// namespace (framework.laravel.<key> = default, ADR-005 §4.4) — the
// defaults come from the installed standard, never from the runtime.
func TestInitCommand_ConfigExtensionMerged(t *testing.T) {
	isolateGlobalConfigDir(t)
	dir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		t.Fatalf("installed-standards dir: %v", err)
	}
	now := time.Now().UTC()
	rec := registry.InstalledStandardRecord{
		FormatVersion:   registry.RecordFormatVersion,
		ID:              "anvil-standard-laravel",
		Version:         "1.2.3",
		ContractVersion: "1.0.0",
		Resolution:      registry.Resolution{Kind: registry.ResolutionKindIndex, Source: "/registry"},
		InstalledAt:     now,
		UpdatedAt:       now,
		Lifecycle:       registry.Lifecycle{State: registry.LifecycleStatePublished},
		ConfigExtension: &registry.ConfigExtensionContent{
			Namespace: "laravel",
			Keys: []registry.ConfigExtensionKey{
				{Name: "version", Description: "Laravel version.", Default: "11.0.0"},
				{Name: "cache.store", Description: "Cache store.", Default: "redis"},
			},
		},
	}
	if _, _, err := registry.NewInstalledStandardStore(dir).Record(rec.ID, rec); err != nil {
		t.Fatalf("record installed standard: %v", err)
	}

	projectDir := t.TempDir()
	_, stdout, stderr, err := executeCommand("init", "app", "--framework", "laravel", "--path", projectDir)
	if err != nil {
		t.Fatalf("init with framework failed: %v\nstderr: %s", err, stderr)
	}
	if !contains(stdout, "resolved delivery lifecycle standard anvil-standard-laravel 1.2.3") {
		t.Errorf("expected success message to report the resolved standard, got: %s", stdout)
	}

	data, err := os.ReadFile(filepath.Join(projectDir, project.ConfigFileName))
	if err != nil {
		t.Fatalf("reading config file: %v", err)
	}
	for _, want := range []string{
		"framework:",
		"laravel:",
		"version: 11.0.0",
		"cache.store: redis",
	} {
		if !contains(string(data), want) {
			t.Errorf("expected config extension defaults %q in anvil.yaml, got:\n%s", want, string(data))
		}
	}
}

// TestInitCommand_ConfigExtensionMissing_Warns verifies the
// missing-extension handling at the command surface (TS-015-03-01): a
// resolved standard that declares no configuration extension content is a
// valid state — initialization succeeds and the omission is explicit via
// the same hand-off/warning pattern T-004 established for a missing
// standard (never silent, never a hard-fail; TS-015-02-02 implements the
// hard-fail later). No framework config section is written: the Core owns
// no framework config defaults to fall back to (TS-015-01-03).
func TestInitCommand_ConfigExtensionMissing_Warns(t *testing.T) {
	isolateGlobalConfigDir(t)
	dir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		t.Fatalf("installed-standards dir: %v", err)
	}
	now := time.Now().UTC()
	rec := registry.InstalledStandardRecord{
		FormatVersion:   registry.RecordFormatVersion,
		ID:              "anvil-standard-laravel",
		Version:         "1.2.3",
		ContractVersion: "1.0.0",
		Resolution:      registry.Resolution{Kind: registry.ResolutionKindIndex, Source: "/registry"},
		InstalledAt:     now,
		UpdatedAt:       now,
		Lifecycle:       registry.Lifecycle{State: registry.LifecycleStatePublished},
	}
	if _, _, err := registry.NewInstalledStandardStore(dir).Record(rec.ID, rec); err != nil {
		t.Fatalf("record installed standard: %v", err)
	}

	projectDir := t.TempDir()
	// The engine surfaces the missing-extension warning on the process
	// stderr (the established engine warning pattern — the pipeline
	// template warning writes the same way), so the command's stderr
	// buffer carries only the cobra-level output; capture os.Stderr for
	// the warning assertion.
	processStderr := captureProcessStderr(t, func() {
		_, _, stderr, err := executeCommand("init", "app", "--framework", "laravel", "--path", projectDir)
		if err != nil {
			t.Fatalf("init with framework failed: %v\nstderr: %s", err, stderr)
		}
	})
	for _, want := range []string{
		"Warning:",
		"no configuration extension content",
		"anvil-standard-laravel",
	} {
		if !contains(processStderr, want) {
			t.Errorf("expected missing-extension warning to mention %q, got: %s", want, processStderr)
		}
	}

	data, err := os.ReadFile(filepath.Join(projectDir, project.ConfigFileName))
	if err != nil {
		t.Fatalf("reading config file: %v", err)
	}
	if contains(string(data), "\nframework:") {
		t.Errorf("expected no framework config section in anvil.yaml (declaration only), got:\n%s", string(data))
	}
	if !contains(string(data), "framework: laravel") {
		t.Errorf("expected 'framework: laravel' declaration in anvil.yaml, got:\n%s", string(data))
	}
}

// TestInitCommand_FrameworkCorruptRecord_Fails verifies that a corrupt
// installed-standard record for the declared framework's standard is a
// real failure, never a silent no-match: the store cannot answer whether
// the standard is installed, so initialization fails with an actionable
// error naming the standard (TS-015-02-01 — resolution reads the record
// store; a corrupt record must not be treated as "not installed").
func TestInitCommand_FrameworkCorruptRecord_Fails(t *testing.T) {
	isolateGlobalConfigDir(t)
	dir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		t.Fatalf("installed-standards dir: %v", err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("create store dir: %v", err)
	}
	corrupt := filepath.Join(dir, "anvil-standard-laravel.json")
	if err := os.WriteFile(corrupt, []byte("{not json"), 0644); err != nil {
		t.Fatalf("write corrupt record: %v", err)
	}

	projectDir := t.TempDir()
	_, _, stderr, err := executeCommand("init", "app", "--framework", "laravel", "--path", projectDir)
	if err == nil {
		t.Fatal("expected init to fail on a corrupt installed-standard record, got nil")
	}
	if !contains(stderr, "could not resolve the delivery lifecycle standard") {
		t.Errorf("expected actionable resolution failure, got: %s", stderr)
	}
	if !contains(stderr, "anvil-standard-laravel") {
		t.Errorf("expected the failure to name the standard id, got: %s", stderr)
	}

	// No project may be created on a resolution failure.
	if fileExists(filepath.Join(projectDir, project.ConfigFileName)) {
		t.Error("config file must not be written when resolution failed")
	}
}

// TestInitCommand_FrameworkInvalidName_Fails verifies that a framework
// name that cannot form a safe standard id (the id is the record file
// name, recordIDPattern) fails initialization with user-facing context —
// not the store-internal message. The declaration is not whitelisted
// (TS-015-01-03); the resolution boundary reports the unsafe derived id
// as an invalid standard name (TS-015-02-01).
func TestInitCommand_FrameworkInvalidName_Fails(t *testing.T) {
	isolateGlobalConfigDir(t)
	dir := t.TempDir()
	_, _, stderr, err := executeCommand("init", "app", "--framework", "foo.bar", "--path", dir)
	if err == nil {
		t.Fatal("expected init to fail for a framework name that cannot form a standard id, got nil")
	}
	if !contains(stderr, "framework name \"foo.bar\" is not a valid standard name") {
		t.Errorf("expected user-facing invalid-standard-name error, got: %s", stderr)
	}

	// No project may be created on a resolution failure.
	if fileExists(filepath.Join(dir, project.ConfigFileName)) {
		t.Error("config file must not be written when resolution failed")
	}
}

// TestInitCommand_TemplateContentFromStandard verifies the generation
// flow at the command surface (TS-015-02-03 DoD: generated content
// comes from the installed standard, not the runtime): with the delivery
// lifecycle standard anvil-standard-laravel recorded WITH template
// content, `anvil init --framework laravel` writes
// .anvil/pipelines/build.yaml and .anvil/pipelines/ci.yaml with exactly
// the standard's content — no adapter is on PATH, proving the content
// comes from the installed standard's record, never from the runtime.
func TestInitCommand_TemplateContentFromStandard(t *testing.T) {
	isolateGlobalConfigDir(t)
	dir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		t.Fatalf("installed-standards dir: %v", err)
	}
	now := time.Now().UTC()
	buildYAML := `pipeline:
  name: build
  stages:
    - name: dependencies
      tasks:
        - name: composer-install
          command: composer
          args: [install, --no-dev, --optimize-autoloader]
`
	ciYAML := `pipeline:
  name: ci
  stages:
    - name: test
      tasks:
        - name: unit-tests
          command: echo
          args: [ok]
`
	rec := registry.InstalledStandardRecord{
		FormatVersion:   registry.RecordFormatVersion,
		ID:              "anvil-standard-laravel",
		Version:         "1.2.3",
		ContractVersion: "1.0.0",
		Resolution:      registry.Resolution{Kind: registry.ResolutionKindIndex, Source: "/registry"},
		InstalledAt:     now,
		UpdatedAt:       now,
		Lifecycle:       registry.Lifecycle{State: registry.LifecycleStatePublished},
		Templates: &registry.TemplateContent{
			Namespace: "laravel",
			Templates: []registry.TemplateFile{
				{ID: "build", Description: "Laravel build pipeline.", Content: buildYAML},
				{ID: "ci", Description: "Laravel CI pipeline.", Content: ciYAML},
			},
		},
	}
	if _, _, err := registry.NewInstalledStandardStore(dir).Record(rec.ID, rec); err != nil {
		t.Fatalf("record installed standard: %v", err)
	}

	projectDir := t.TempDir()
	_, stdout, stderr, err := executeCommand("init", "app", "--framework", "laravel", "--path", projectDir)
	if err != nil {
		t.Fatalf("init with framework failed: %v\nstderr: %s", err, stderr)
	}
	if !contains(stdout, "resolved delivery lifecycle standard anvil-standard-laravel 1.2.3") {
		t.Errorf("expected success message to report the resolved standard, got: %s", stdout)
	}

	// build.yaml must be byte-for-byte the installed standard's build
	// template content — no adapter is on PATH, so the content can only
	// have come from the standard's record.
	buildData, err := os.ReadFile(filepath.Join(projectDir, ".anvil", "pipelines", project.PipelineBuildFileName))
	if err != nil {
		t.Fatalf("reading build.yaml: %v", err)
	}
	if string(buildData) != buildYAML {
		t.Errorf("build.yaml content = %q, want the installed standard's content %q", string(buildData), buildYAML)
	}

	// ci.yaml must be byte-for-byte the installed standard's CI template
	// content.
	ciData, err := os.ReadFile(filepath.Join(projectDir, ".anvil", "pipelines", project.PipelineCIFileName))
	if err != nil {
		t.Fatalf("reading ci.yaml: %v", err)
	}
	if string(ciData) != ciYAML {
		t.Errorf("ci.yaml content = %q, want the installed standard's content %q", string(ciData), ciYAML)
	}
}

// TestValidProjectNameRegex verifies the regex pattern used for name validation.
func TestValidProjectNameRegex(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"my-project", true},
		{"my_project", true},
		{"MyProject123", true},
		{"a", true},
		{"", false},
		{"my project", false},
		{"my/project", false},
		{"my.project", false},
		{"my-project!", false},
		{" spaces", false},
	}

	for _, tt := range tests {
		got := validProjectName.MatchString(tt.name)
		if got != tt.valid {
			t.Errorf("validProjectName.MatchString(%q) = %v, want %v", tt.name, got, tt.valid)
		}
	}
}

// --- helpers ---

// captureProcessStderr runs fn with the process os.Stderr redirected to a
// pipe and returns everything written to it. Engine warnings follow the
// engine warning pattern (os.Stderr — the same stream the pipeline
// template warning uses), which the executeCommand harness does not
// capture (it wires the cobra command's writers only); tests assert on
// those warnings through this helper.
func captureProcessStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() failed: %v", err)
	}
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = orig })

	done := make(chan string, 1)
	go func() {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(r)
		done <- buf.String()
	}()

	fn()

	w.Close()
	return <-done
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
