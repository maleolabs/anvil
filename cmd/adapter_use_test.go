// Package cmd implements the Anvil CLI commands.
//
// Tests for "anvil adapter use" (TS-007-033): the command sets
// project.framework in anvil.yaml with the validation matrix — no
// project, unknown adapter, already active, already configured (with
// --force override) — and generates the pipeline template when missing.
//
// The tests run in temporary project directories (chdir with restore, no
// t.Parallel) and verify that the map-based YAML update preserves all
// custom fields.
//
// Reference: TS-007-033
package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"maleolabs.com/anvil/internal/project"
)

// testProjectYAML is a valid project config with extra custom fields that
// must survive the framework update.
const testProjectYAML = `project:
  name: my-app
  version: 1.2.3
  description: test project
artifact:
  include:
    - "vendor/**"
  exclude: []
custom_top_level:
  nested: value
`

// setupUseProject creates a project directory with anvil.yaml (testProjectYAML),
// changes the working directory into it, and returns the directory path.
func setupUseProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, project.ConfigFileName)
	if err := os.WriteFile(configPath, []byte(testProjectYAML), 0644); err != nil {
		t.Fatalf("write anvil.yaml: %v", err)
	}
	chdirTo(t, dir)
	return dir
}

// chdirTo changes the working directory to dir and restores it on cleanup.
func chdirTo(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(orig); err != nil {
			t.Errorf("restore working directory %q: %v", orig, err)
		}
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir to %q: %v", dir, err)
	}
}

// projectFramework reads project.framework from the anvil.yaml in dir.
func projectFramework(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, project.ConfigFileName))
	if err != nil {
		t.Fatalf("read anvil.yaml: %v", err)
	}
	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse anvil.yaml: %v", err)
	}
	proj, ok := doc["project"].(map[string]interface{})
	if !ok {
		return ""
	}
	framework, _ := proj["framework"].(string)
	return framework
}

// TestAdapterUse_RequiresProject verifies that the command rejects a
// directory without anvil.yaml with a clear error. Post-gate
// (TS-017-02-02) the adapter must be RECORDED (registry-driven
// installed definition) and its executable probe-validated, so a fake
// laravel standard record and binary are seeded.
//
// Reference: TS-007-033 AC-6, TS-017-02-02
func TestAdapterUse_RequiresProject(t *testing.T) {
	chdirTo(t, t.TempDir())
	seedInstalledAdapter(t, "laravel", "server")

	_, _, stderr, err := executeCommand("adapter", "use", "laravel")
	if err == nil {
		t.Fatal("expected error without a project, got nil")
	}
	if !strings.Contains(stderr, "no Anvil project found") {
		t.Errorf("stderr should report the missing project, got: %s", stderr)
	}
	if !strings.Contains(stderr, "anvil init") {
		t.Errorf("stderr should suggest 'anvil init', got: %s", stderr)
	}
}

// TestAdapterUse_UnknownAdapter verifies that an adapter name whose
// standard is not recorded is rejected before any change. The Core
// carries no known-framework catalog (ADR-026) and performs no binary
// scan (TS-017-02-02): with nothing recorded, the hint reports the
// empty registry state and points at standard adoption.
//
// Reference: TS-007-033 AC-7, TS-007-039, ADR-026, TS-017-02-02
func TestAdapterUse_UnknownAdapter(t *testing.T) {
	dir := setupUseProject(t)
	isolateGlobalConfigDir(t)
	t.Setenv("PATH", "")

	_, _, stderr, err := executeCommand("adapter", "use", "node")
	if err == nil {
		t.Fatal("expected error for unknown adapter, got nil")
	}
	if !strings.Contains(stderr, "unknown adapter") {
		t.Errorf("stderr should mention the unknown adapter, got: %s", stderr)
	}
	if !strings.Contains(stderr, "no adapter is installed through the registry") {
		t.Errorf("stderr should report the empty registry state, got: %s", stderr)
	}
	if got := projectFramework(t, dir); got != "" {
		t.Errorf("project.framework = %q, want unset after rejection", got)
	}
}

// TestAdapterUse_SetsFramework verifies that the command sets
// project.framework in anvil.yaml and preserves custom fields. When the
// installed adapter does not implement the template command, no pipeline
// files are generated and a warning directs the user to install a
// template-capable adapter — the Core owns no generic template data to
// fall back to (TS-015-01-02, ADR-026 decision 1).
//
// Reference: TS-007-033 AC-1, AC-5
func TestAdapterUse_SetsFramework(t *testing.T) {
	dir := setupUseProject(t)
	seedInstalledAdapter(t, "laravel", "server")

	_, stdout, stderr, err := executeCommand("adapter", "use", "laravel")
	if err != nil {
		t.Fatalf("adapter use returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	if got := projectFramework(t, dir); got != "laravel" {
		t.Errorf("project.framework = %q, want %q", got, "laravel")
	}
	if !strings.Contains(stdout, "Adapter laravel is now active") {
		t.Errorf("stdout should confirm activation, got:\n%s", stdout)
	}

	// Custom fields must survive the update.
	data, err := os.ReadFile(filepath.Join(dir, project.ConfigFileName))
	if err != nil {
		t.Fatalf("read anvil.yaml: %v", err)
	}
	content := string(data)
	for _, want := range []string{"name: my-app", "version: 1.2.3", "vendor/**", "custom_top_level", "nested: value"} {
		if !strings.Contains(content, want) {
			t.Errorf("anvil.yaml should preserve %q, got:\n%s", want, content)
		}
	}

	// No pipeline templates are generated: the stub adapter does not
	// implement the template command and the Core owns no generic
	// template data to fall back to (TS-015-01-02). The engine-level
	// warning behavior is covered by the engine tests.
	for _, name := range []string{"build.yaml", "ci.yaml"} {
		path := filepath.Join(dir, ".anvil", "pipelines", name)
		if _, err := os.Stat(path); err == nil {
			t.Errorf("pipeline template %s should not exist without adapter template support (TS-015-01-02)", path)
		}
	}
}

// TestAdapterUse_AlreadyActive verifies that re-using the active adapter
// reports "already active" and does not rewrite anvil.yaml.
//
// Reference: TS-007-033 AC-2
func TestAdapterUse_AlreadyActive(t *testing.T) {
	dir := setupUseProject(t)
	seedInstalledAdapter(t, "laravel", "server")

	// Set the framework first.
	if err := writeProjectFramework(filepath.Join(dir, project.ConfigFileName), "laravel"); err != nil {
		t.Fatalf("seed framework: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(dir, project.ConfigFileName))
	if err != nil {
		t.Fatalf("read anvil.yaml: %v", err)
	}

	_, stdout, stderr, err := executeCommand("adapter", "use", "laravel")
	if err != nil {
		t.Fatalf("adapter use returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "Adapter laravel is already active.") {
		t.Errorf("stdout should report 'already active', got:\n%s", stdout)
	}

	after, err := os.ReadFile(filepath.Join(dir, project.ConfigFileName))
	if err != nil {
		t.Fatalf("read anvil.yaml: %v", err)
	}
	if string(before) != string(after) {
		t.Error("anvil.yaml was rewritten for an already-active adapter")
	}
}

// TestAdapterUse_AlreadyConfigured verifies that switching to a different
// adapter without --force is rejected with the documented message.
//
// Reference: TS-007-033 AC-3
func TestAdapterUse_AlreadyConfigured(t *testing.T) {
	dir := setupUseProject(t)
	seedInstalledStandard(t, "anvil-standard-laravel", "1.0.0")
	seedInstalledStandard(t, "anvil-standard-flutter", "1.0.0")
	binDir := t.TempDir()
	stubAdapterLookup(t, binDir)
	writeFakeAdapter(t, binDir, "anvil-adapter-laravel", adapterCapabilitiesJSON("server"), adapterExtensionJSON("laravel"))
	writeFakeAdapter(t, binDir, "anvil-adapter-flutter", adapterCapabilitiesJSON("hybrid"), adapterExtensionJSON("flutter"))
	configPath := filepath.Join(dir, project.ConfigFileName)
	if err := writeProjectFramework(configPath, "laravel"); err != nil {
		t.Fatalf("seed framework: %v", err)
	}

	_, _, stderr, err := executeCommand("adapter", "use", "flutter")
	if err == nil {
		t.Fatal("expected rejection without --force, got nil")
	}
	if !strings.Contains(stderr, "Adapter laravel is already configured. Use --force to override.") {
		t.Errorf("stderr should contain the documented rejection, got: %s", stderr)
	}
	if got := projectFramework(t, dir); got != "laravel" {
		t.Errorf("project.framework = %q, want unchanged %q", got, "laravel")
	}
}

// TestAdapterUse_ForceOverride verifies that --force overrides an already
// configured adapter.
//
// Reference: TS-007-033 AC-4
func TestAdapterUse_ForceOverride(t *testing.T) {
	dir := setupUseProject(t)
	seedInstalledStandard(t, "anvil-standard-laravel", "1.0.0")
	seedInstalledStandard(t, "anvil-standard-flutter", "1.0.0")
	binDir := t.TempDir()
	stubAdapterLookup(t, binDir)
	writeFakeAdapter(t, binDir, "anvil-adapter-laravel", adapterCapabilitiesJSON("server"), adapterExtensionJSON("laravel"))
	writeFakeAdapter(t, binDir, "anvil-adapter-flutter", adapterCapabilitiesJSON("hybrid"), adapterExtensionJSON("flutter"))
	if err := writeProjectFramework(filepath.Join(dir, project.ConfigFileName), "laravel"); err != nil {
		t.Fatalf("seed framework: %v", err)
	}

	_, stdout, stderr, err := executeCommand("adapter", "use", "flutter", "--force")
	if err != nil {
		t.Fatalf("adapter use --force returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if got := projectFramework(t, dir); got != "flutter" {
		t.Errorf("project.framework = %q, want %q", got, "flutter")
	}
	if !strings.Contains(stdout, "overrode laravel") {
		t.Errorf("stdout should mention the override, got:\n%s", stdout)
	}
}

// TestAdapterUse_PreservesExistingPipeline verifies that template
// generation never overwrites an existing build.yaml (non-destructive).
//
// Reference: TS-007-033 AC-5
func TestAdapterUse_PreservesExistingPipeline(t *testing.T) {
	dir := setupUseProject(t)
	seedInstalledAdapter(t, "laravel", "server")

	pipelinesDir := filepath.Join(dir, ".anvil", "pipelines")
	if err := os.MkdirAll(pipelinesDir, 0755); err != nil {
		t.Fatalf("create pipelines dir: %v", err)
	}
	custom := "custom: pipeline\n"
	buildPath := filepath.Join(pipelinesDir, "build.yaml")
	if err := os.WriteFile(buildPath, []byte(custom), 0644); err != nil {
		t.Fatalf("write existing build.yaml: %v", err)
	}

	_, _, stderr, err := executeCommand("adapter", "use", "laravel")
	if err != nil {
		t.Fatalf("adapter use returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	data, err := os.ReadFile(buildPath)
	if err != nil {
		t.Fatalf("read build.yaml: %v", err)
	}
	if string(data) != custom {
		t.Errorf("existing build.yaml was overwritten; got:\n%s", data)
	}
}

// TestAdapterUse_ConfigRemainsLoadable verifies that writing the
// non-schema project.framework key does not break project.Load() — the
// canonical validation has no unknown-key rejection, so the key is
// silently ignored on unmarshal.
//
// Reference: TS-007-033 §7, project.Load
func TestAdapterUse_ConfigRemainsLoadable(t *testing.T) {
	setupUseProject(t)
	seedInstalledAdapter(t, "laravel", "server")

	_, _, stderr, err := executeCommand("adapter", "use", "laravel")
	if err != nil {
		t.Fatalf("adapter use returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	if _, err := project.Load(); err != nil {
		t.Errorf("project.Load() failed after adapter use: %v", err)
	}
}
