package engine

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
	"maleolabs.com/anvil/internal/config"
	"maleolabs.com/anvil/internal/contracts"
	"maleolabs.com/anvil/internal/execution"
	"maleolabs.com/anvil/internal/project"
	"maleolabs.com/anvil/internal/registry"
)

// stubAdapterTemplate replaces the fetchAdapterTemplate seam for the test
// and restores it on cleanup. Tests use it to exercise the engine's
// adapter-template generation without a real adapter binary on PATH.
//
// Reference: TS-007-038
func stubAdapterTemplate(t *testing.T, fn func(ctx context.Context, framework string) (contracts.TemplateResult, error)) {
	t.Helper()
	orig := fetchAdapterTemplate
	fetchAdapterTemplate = fn
	t.Cleanup(func() { fetchAdapterTemplate = orig })
}

// captureStderr runs fn with os.Stderr redirected to a pipe and returns
// everything written to stderr. Restores os.Stderr on cleanup.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() failed: %v", err)
	}
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = orig })
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("closing stderr pipe: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading stderr pipe: %v", err)
	}
	return string(data)
}

// laravelTemplate returns the adapter-owned Laravel build template the
// tests stub fetchAdapterTemplate with — the same definition the Laravel
// standard's template command returns (TS-007-038; the standard content
// now lives in the anvil-standard-laravel repository, TS-016-01-01).
func laravelTemplate() contracts.TemplateResult {
	return contracts.TemplateResult{
		Build: &execution.PipelineDefinition{
			Pipeline: execution.Pipeline{
				Name: "build",
				Stages: []execution.PipelineStage{
					{
						Name: "dependencies",
						Tasks: []execution.Task{
							{Name: "composer-install", Command: "composer", Args: []string{"install", "--no-dev", "--optimize-autoloader"}},
						},
					},
					{
						Name: "assets",
						Tasks: []execution.Task{
							{Name: "npm-build", Command: "npm", Args: []string{"run", "build"}},
						},
					},
					{
						Name: "optimize",
						Tasks: []execution.Task{
							{Name: "cache-config", Command: "php", Args: []string{"artisan", "config:cache"}},
							{Name: "cache-route", Command: "php", Args: []string{"artisan", "route:cache"}},
							{Name: "cache-view", Command: "php", Args: []string{"artisan", "view:cache"}},
						},
					},
				},
			},
		},
	}
}

// flutterTemplate returns the adapter-owned Flutter build template the
// tests stub fetchAdapterTemplate with — the same definition the Flutter
// standard's template command returns (now served from the
// anvil-standard-flutter repository, TS-016-02-01), including the
// ADR-018 platform metadata (TS-007-038).
func flutterTemplate() contracts.TemplateResult {
	return contracts.TemplateResult{
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
}

// laravelTemplateYAML returns the Laravel build template as pipeline
// file content (the YAML the adapter's template result marshals to —
// validateAdapterDefinition output). It is the parity fixture of
// TS-015-02-03: the installed standard's template content is expected to
// be the same pipeline file content the adapter-driven path previously
// wrote, so lifecycle behavior stays green (contract-stable migration,
// Transition Plan §6.5).
func laravelTemplateYAML(t *testing.T) string {
	t.Helper()
	data, err := yaml.Marshal(laravelTemplate().Build)
	if err != nil {
		t.Fatalf("marshal laravel template: %v", err)
	}
	return string(data)
}

// TestInitialize_CreatesProject verifies that running Initialize in an empty
// directory creates the project configuration file and lifecycle state. No
// pipeline files are generated: template content is distribution content
// supplied by delivery lifecycle standards, never engine content (A10,
// TS-015-01-02) — a project without a framework declaration has no
// standard content to generate from.
func TestInitialize_CreatesProject(t *testing.T) {
	dir := t.TempDir()
	name := "test-project"

	result, err := Initialize(name, dir)
	if err != nil {
		t.Fatalf("Initialize() returned unexpected error: %v", err)
	}
	if result != ResultCreated {
		t.Fatalf("Initialize() = %v, want %v", result, ResultCreated)
	}

	s := project.NewStructure(dir)

	// Verify config file was created.
	if !fileExists(s.ConfigFile) {
		t.Errorf("expected config file %s to exist", s.ConfigFile)
	}
}

// TestInitialize_NoPipelineFilesWithoutFramework verifies that
// initializing without a framework declaration writes no pipeline
// template files: the runtime owns no pipeline template content and the
// .anvil/pipelines/ directory is not created (TS-015-01-02, ADR-026
// decision 1).
func TestInitialize_NoPipelineFilesWithoutFramework(t *testing.T) {
	dir := t.TempDir()

	if _, err := Initialize("app", dir); err != nil {
		t.Fatalf("Initialize() returned unexpected error: %v", err)
	}

	s := project.NewStructure(dir)
	if fileExists(filepath.Join(s.PipelinesDir, project.PipelineBuildFileName)) {
		t.Error("build.yaml should not be generated without a framework — the runtime owns no template content")
	}
	if fileExists(filepath.Join(s.PipelinesDir, project.PipelineCIFileName)) {
		t.Error("ci.yaml should not be generated without a framework — the runtime owns no template content")
	}
	if isDir(s.PipelinesDir) {
		t.Error(".anvil/pipelines/ should not be created when no pipeline file is generated")
	}
}

// TestInitialize_DetectsExistingProject verifies that running Initialize in a
// directory that already contains a project returns an error and does not
// modify existing files.
func TestInitialize_DetectsExistingProject(t *testing.T) {
	dir := t.TempDir()
	name := "first-project"

	// Create the project first.
	_, err := Initialize(name, dir)
	if err != nil {
		t.Fatalf("first Initialize() failed: %v", err)
	}

	// Read config contents to verify they are not modified later.
	configPath := filepath.Join(dir, project.ConfigFileName)
	originalConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config file: %v", err)
	}

	// Attempt to initialize again.
	result, err := Initialize("second-project", dir)
	if err == nil {
		t.Fatal("expected error when initializing in existing project, got nil")
	}
	if result != ResultAlreadyExists {
		t.Fatalf("Initialize() = %v, want %v", result, ResultAlreadyExists)
	}
	if err != ErrProjectAlreadyExists {
		t.Fatalf("Initialize() error = %v, want %v", err, ErrProjectAlreadyExists)
	}

	// Verify no files were modified.
	currentConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config file after failed init: %v", err)
	}
	if string(currentConfig) != string(originalConfig) {
		t.Error("config file was modified after failed initialization")
	}
}

// TestInitialize_EmptyName verifies that an empty project name returns an
// appropriate error.
func TestInitialize_EmptyName(t *testing.T) {
	dir := t.TempDir()

	_, err := Initialize("", dir)
	if err == nil {
		t.Fatal("expected error for empty project name, got nil")
	}
	if err != ErrNameRequired {
		t.Fatalf("Initialize() error = %v, want %v", err, ErrNameRequired)
	}
}

// TestInitialize_CreatesConfigWithCorrectValues verifies that the generated
// configuration contains the expected keys and values. In particular, it
// ensures that an empty Description is serialised as "" (empty string) not
// null, which would cause downstream validation to fail.
func TestInitialize_CreatesConfigWithCorrectValues(t *testing.T) {
	dir := t.TempDir()
	name := "my-app"

	_, err := Initialize(name, dir)
	if err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}

	configPath := filepath.Join(dir, project.ConfigFileName)
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config file: %v", err)
	}

	content := string(data)

	// Verify project name is present.
	if !contains(content, "name: my-app") {
		t.Errorf("config missing project name 'my-app':\n%s", content)
	}

	// Verify default version is present.
	if !contains(content, "version: 1.0.0") {
		t.Errorf("config missing default version:\n%s", content)
	}

	// Verify description key is present.
	if !contains(content, "description:") {
		t.Errorf("config missing description key:\n%s", content)
	}

	// --- Structured verification: unmarshal and check field types/values ---
	// This catches the BUG-001 regression: empty Description must be ""
	// (empty string, typed as Go string) not nil (untyped YAML null).
	var cfg config.ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal config: %v\nraw content:\n%s", err, content)
	}

	if cfg.Project.Name != "my-app" {
		t.Errorf("cfg.Project.Name = %q, want %q", cfg.Project.Name, "my-app")
	}
	if cfg.Project.Version != "1.0.0" {
		t.Errorf("cfg.Project.Version = %q, want %q", cfg.Project.Version, "1.0.0")
	}
	// Critical assertion for BUG-001: Description must be empty string, not nil.
	if cfg.Project.Description != "" {
		t.Errorf("cfg.Project.Description = %q (len=%d), want empty string", cfg.Project.Description, len(cfg.Project.Description))
	}
}

// TestInitialize_Idempotent verifies that repeated initialization attempts
// consistently return the same error without side effects.
func TestInitialize_Idempotent(t *testing.T) {
	dir := t.TempDir()

	// Create project.
	_, err := Initialize("app", dir)
	if err != nil {
		t.Fatalf("first Initialize() failed: %v", err)
	}

	// Run multiple detection attempts.
	for i := 0; i < 5; i++ {
		result, err := Initialize("another", dir)
		if result != ResultAlreadyExists {
			t.Errorf("attempt %d: result = %v, want %v", i, result, ResultAlreadyExists)
		}
		if err != ErrProjectAlreadyExists {
			t.Errorf("attempt %d: error = %v, want %v", i, err, ErrProjectAlreadyExists)
		}
	}
}

// TestInitialize_WithCustomPath verifies the engine accepts and creates
// a project at a specified target path.
func TestInitialize_WithCustomPath(t *testing.T) {
	parent := t.TempDir()
	targetPath := filepath.Join(parent, "nested", "project")
	name := "nested-app"

	result, err := Initialize(name, targetPath)
	if err != nil {
		t.Fatalf("Initialize() at custom path failed: %v", err)
	}
	if result != ResultCreated {
		t.Fatalf("Initialize() = %v, want %v", result, ResultCreated)
	}

	s := project.NewStructure(targetPath)
	if !fileExists(s.ConfigFile) {
		t.Error("expected anvil.yaml to exist at custom path")
	}
}

// TestInitialize_NoRuntimeState verifies that runtime state directories
// (releases, shared) are NOT created during initialization — only the
// project configuration, identity, and lifecycle state live inside .anvil/.
// The .anvil/pipelines/ directory is not created either: without a
// framework declaration no pipeline template content is generated
// (TS-015-01-02).
func TestInitialize_NoRuntimeState(t *testing.T) {
	dir := t.TempDir()

	_, err := Initialize("no-state", dir)
	if err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}

	s := project.NewStructure(dir)

	// .anvil/ directory should exist (identity + lifecycle state).
	if !isDir(s.AnvilDir) {
		t.Error(".anvil/ directory should exist — identity and lifecycle state are created during init")
	}

	// No pipeline directory without a framework declaration.
	if isDir(s.PipelinesDir) {
		t.Error(".anvil/pipelines/ should not exist — the runtime owns no pipeline template content")
	}

	// Runtime state directories must NOT exist.
	// Note: StateDir (.anvil/state/) IS created during init because it
	// stores the project lifecycle state (TS-P1-07).
	if isDir(s.ReleasesDir) {
		t.Error("releases directory should not exist during init")
	}
	if isDir(s.SharedDir) {
		t.Error("shared directory should not exist during init")
	}
}

// TestInitialize_NoCoreOwnedTemplateContent verifies that initialization
// without a framework declaration leaves no pipeline template content in
// the runtime output: no .anvil/pipelines/ directory and no pipeline
// YAML files are written (TS-015-01-02 DoD — no framework build or
// pipeline template content remains in the runtime; templates are
// distribution content supplied by delivery lifecycle standards).
func TestInitialize_NoCoreOwnedTemplateContent(t *testing.T) {
	dir := t.TempDir()

	if _, err := Initialize("test-app", dir); err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}

	s := project.NewStructure(dir)

	// The pipelines directory must not exist at all — nothing to put in
	// it: the Core owns no default build/CI template data anymore.
	if isDir(s.PipelinesDir) {
		t.Fatal(".anvil/pipelines/ should not exist after plain init")
	}

	// Neither build.yaml nor ci.yaml may exist anywhere in the project.
	for _, name := range []string{project.PipelineBuildFileName, project.PipelineCIFileName} {
		matches, err := filepath.Glob(filepath.Join(dir, ".anvil", "**", name))
		if err != nil {
			t.Fatalf("glob for %s: %v", name, err)
		}
		if len(matches) != 0 {
			t.Errorf("found %s in project (Core-owned template content must not be generated): %v", name, matches)
		}
	}

	// The project remains valid and lifecycle-ready apart from pipeline
	// templates: config file and lifecycle state exist.
	if !fileExists(s.ConfigFile) {
		t.Error("anvil.yaml should still be created during init")
	}
	if !fileExists(s.LifecycleStateFilePath()) {
		t.Error("lifecycle state should still be created during init")
	}
}

// TestInitialize_DoesNotOverwritePipelineFiles verifies that re-initialization
// after manually modifying pipeline files does not overwrite them.
func TestInitialize_DoesNotOverwritePipelineFiles(t *testing.T) {
	dir := t.TempDir()

	// First init creates the project. Plain initialization writes no
	// pipeline templates (TS-015-01-02), so the pipeline file is seeded
	// manually below.
	if _, err := Initialize("test-app", dir); err != nil {
		t.Fatalf("first Initialize() failed: %v", err)
	}

	s := project.NewStructure(dir)
	buildPath := filepath.Join(s.PipelinesDir, project.PipelineBuildFileName)

	// Seed an existing pipeline file (user-owned content).
	if err := os.MkdirAll(s.PipelinesDir, 0755); err != nil {
		t.Fatalf("creating pipelines dir: %v", err)
	}

	// Modify build.yaml with custom content.
	customContent := []byte("custom: content\n")
	if err := os.WriteFile(buildPath, customContent, 0644); err != nil {
		t.Fatalf("writing custom build.yaml: %v", err)
	}

	// Attempt to re-initialize. This should NOT overwrite build.yaml.
	result, err := Initialize("test-app", dir)
	if err == nil {
		t.Fatal("expected error for existing project, got nil")
	}
	if result != ResultAlreadyExists {
		t.Fatalf("Initialize() = %v, want %v", result, ResultAlreadyExists)
	}

	// Verify build.yaml still contains custom content.
	data, err := os.ReadFile(buildPath)
	if err != nil {
		t.Fatalf("reading build.yaml: %v", err)
	}
	if string(data) != string(customContent) {
		t.Errorf("build.yaml was overwritten:\ngot:  %q\nwant: %q", string(data), string(customContent))
	}
}

// TestInitialize_WithLaravelFramework verifies that initializing with
// WithFramework("laravel") writes the Laravel build template (stages
// dependencies/assets/optimize) fetched from the adapter's template
// command (ADR-020 §1), stores the framework declaration in anvil.yaml,
// and applies framework-agnostic compiled defaults: artifact.include stays
// empty and artifact.exclude carries the compiled default list untouched —
// including "vendor/**" — because framework-specific config defaults are no
// longer Core-owned (TS-015-01-03, ADR-026 decision 1); framework config
// keys and defaults come from the installed delivery lifecycle standard
// (TS-015-03-01). When the adapter provides no CI definition, no ci.yaml is
// written — the Core no longer falls back to Core-owned CI template data
// (TS-015-01-02, ADR-026 decision 1; TS-P7-28 AC-1, TS-P7-29 AC-1/AC-4).
func TestInitialize_WithLaravelFramework(t *testing.T) {
	stubAdapterTemplate(t, func(_ context.Context, framework string) (contracts.TemplateResult, error) {
		if framework != "laravel" {
			t.Errorf("fetchAdapterTemplate framework = %q, want %q", framework, "laravel")
		}
		return laravelTemplate(), nil
	})

	dir := t.TempDir()
	name := "laravel-app"

	result, err := Initialize(name, dir, WithFramework("laravel"))
	if err != nil {
		t.Fatalf("Initialize() returned unexpected error: %v", err)
	}
	if result != ResultCreated {
		t.Fatalf("Initialize() = %v, want %v", result, ResultCreated)
	}

	// build.yaml must be the Laravel build template.
	def, err := execution.LookupBuildDefinition(dir)
	if err != nil {
		t.Fatalf("LookupBuildDefinition() failed: %v", err)
	}
	if def.Pipeline.Name != "build" {
		t.Errorf("pipeline name = %q, want %q", def.Pipeline.Name, "build")
	}
	if len(def.Pipeline.Stages) != 3 {
		t.Fatalf("build pipeline stages = %d, want 3", len(def.Pipeline.Stages))
	}
	wantStages := []string{"dependencies", "assets", "optimize"}
	for i, want := range wantStages {
		if def.Pipeline.Stages[i].Name != want {
			t.Errorf("stage %d name = %q, want %q", i, def.Pipeline.Stages[i].Name, want)
		}
	}

	// anvil.yaml must store the framework declaration and carry no
	// framework-specific artifact overrides: the written config is exactly
	// the framework-agnostic NewProjectConfig output (TS-015-01-03 — the
	// old Laravel path wrote a vendor/**-minus exclude list; that knowledge
	// is no longer Core-owned). The compiled schema defaults (including
	// "vendor/**") apply at config load time.
	data, err := os.ReadFile(filepath.Join(dir, project.ConfigFileName))
	if err != nil {
		t.Fatalf("reading config file: %v", err)
	}
	var cfg config.ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal config: %v\nraw content:\n%s", err, string(data))
	}
	if cfg.Project.Framework != "laravel" {
		t.Errorf("cfg.Project.Framework = %q, want %q", cfg.Project.Framework, "laravel")
	}
	want := config.NewProjectConfig(name)
	want.Project.Framework = "laravel"
	if !slices.Equal(cfg.Artifact.Include, want.Artifact.Include) {
		t.Errorf("cfg.Artifact.Include = %v, want %v (framework-agnostic)", cfg.Artifact.Include, want.Artifact.Include)
	}
	if !slices.Equal(cfg.Artifact.Exclude, want.Artifact.Exclude) {
		t.Errorf("cfg.Artifact.Exclude = %v, want %v (no framework adjustment)", cfg.Artifact.Exclude, want.Artifact.Exclude)
	}
	if len(cfg.Artifact.Exclude) != 0 {
		t.Errorf("cfg.Artifact.Exclude = %v, want empty in the written config (compiled defaults apply at load time)", cfg.Artifact.Exclude)
	}

	// The adapter provides no CI definition, so ci.yaml must not be
	// written: the Core owns no CI template data to fall back to
	// (TS-015-01-02).
	if fileExists(filepath.Join(dir, ".anvil", "pipelines", project.PipelineCIFileName)) {
		t.Error("ci.yaml should not be generated when the adapter provides no CI definition")
	}
}

// TestInitialize_WithFlutterFramework verifies that initializing with
// WithFramework("flutter") writes the Flutter build template (single build
// stage with flutter-web/flutter-apk/flutter-ios tasks carrying platform
// metadata) fetched from the adapter's template command (ADR-020 §1),
// stores the framework in anvil.yaml, and writes no ci.yaml when the
// adapter provides no CI definition — the Core owns no CI template data
// (TS-015-01-02; TS-P7-27 AC-1..AC-3, ADR-018).
func TestInitialize_WithFlutterFramework(t *testing.T) {
	stubAdapterTemplate(t, func(_ context.Context, framework string) (contracts.TemplateResult, error) {
		if framework != "flutter" {
			t.Errorf("fetchAdapterTemplate framework = %q, want %q", framework, "flutter")
		}
		return flutterTemplate(), nil
	})

	dir := t.TempDir()
	name := "flutter-app"

	result, err := Initialize(name, dir, WithFramework("flutter"))
	if err != nil {
		t.Fatalf("Initialize() returned unexpected error: %v", err)
	}
	if result != ResultCreated {
		t.Fatalf("Initialize() = %v, want %v", result, ResultCreated)
	}

	// build.yaml must be the Flutter build template.
	def, err := execution.LookupBuildDefinition(dir)
	if err != nil {
		t.Fatalf("LookupBuildDefinition() failed: %v", err)
	}
	if def.Pipeline.Name != "build" {
		t.Errorf("pipeline name = %q, want %q", def.Pipeline.Name, "build")
	}
	if len(def.Pipeline.Stages) != 1 {
		t.Fatalf("build pipeline stages = %d, want 1", len(def.Pipeline.Stages))
	}
	stage := def.Pipeline.Stages[0]
	if stage.Name != "build" {
		t.Errorf("stage name = %q, want %q", stage.Name, "build")
	}
	if len(stage.Tasks) != 3 {
		t.Fatalf("build stage tasks = %d, want 3", len(stage.Tasks))
	}

	// Each task must carry its platform metadata (web/apk: all platforms,
	// ios: darwin only — ADR-018).
	wantTasks := []struct {
		name      string
		target    string
		platforms []string
	}{
		{name: "flutter-web", target: "web", platforms: []string{"linux", "darwin", "windows"}},
		{name: "flutter-apk", target: "apk", platforms: []string{"linux", "darwin", "windows"}},
		{name: "flutter-ios", target: "ios", platforms: []string{"darwin"}},
	}
	for i, want := range wantTasks {
		task := stage.Tasks[i]
		if task.Name != want.name {
			t.Errorf("task %d name = %q, want %q", i, task.Name, want.name)
		}
		if task.Metadata == nil {
			t.Fatalf("task %q Metadata = nil, want platform metadata", task.Name)
		}
		if task.Metadata.Target != want.target {
			t.Errorf("task %q Metadata.Target = %q, want %q", task.Name, task.Metadata.Target, want.target)
		}
		if !slices.Equal(task.Metadata.Platforms, want.platforms) {
			t.Errorf("task %q Metadata.Platforms = %v, want %v", task.Name, task.Metadata.Platforms, want.platforms)
		}
	}

	// anvil.yaml must store the framework.
	data, err := os.ReadFile(filepath.Join(dir, project.ConfigFileName))
	if err != nil {
		t.Fatalf("reading config file: %v", err)
	}
	var cfg config.ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal config: %v\nraw content:\n%s", err, string(data))
	}
	if cfg.Project.Framework != "flutter" {
		t.Errorf("cfg.Project.Framework = %q, want %q", cfg.Project.Framework, "flutter")
	}
	// The Flutter framework must not add artifact include overrides.
	if len(cfg.Artifact.Include) != 0 {
		t.Errorf("cfg.Artifact.Include = %v, want empty", cfg.Artifact.Include)
	}

	// The adapter provides no CI definition, so ci.yaml must not be
	// written: the Core owns no CI template data to fall back to
	// (TS-015-01-02).
	if fileExists(filepath.Join(dir, ".anvil", "pipelines", project.PipelineCIFileName)) {
		t.Error("ci.yaml should not be generated when the adapter provides no CI definition")
	}
}

// TestInitialize_FrameworkDeclarationPassesThrough verifies that a framework
// declaration is stored verbatim without Core-side whitelist validation
// (TS-015-01-03, ADR-026 decision 1): the Core owns no framework catalog, so
// an unlisted framework like "symfony" initializes successfully and is stored
// in anvil.yaml with framework-agnostic compiled defaults. Framework
// resolution and standard-missing semantics are standard-driven
// (TS-015-02-01, TS-015-02-02) and land in a later work item.
func TestInitialize_FrameworkDeclarationPassesThrough(t *testing.T) {
	dir := t.TempDir()

	result, err := Initialize("app", dir, WithFramework("symfony"))
	if err != nil {
		t.Fatalf("Initialize() returned unexpected error: %v", err)
	}
	if result != ResultCreated {
		t.Fatalf("Initialize() = %v, want %v", result, ResultCreated)
	}

	// The declaration must be stored and no framework-specific defaults
	// applied (the old "unknown framework" rejection is gone).
	data, err := os.ReadFile(filepath.Join(dir, project.ConfigFileName))
	if err != nil {
		t.Fatalf("reading config file: %v", err)
	}
	var cfg config.ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal config: %v\nraw content:\n%s", err, string(data))
	}
	if cfg.Project.Framework != "symfony" {
		t.Errorf("cfg.Project.Framework = %q, want %q", cfg.Project.Framework, "symfony")
	}
	if len(cfg.Artifact.Include) != 0 {
		t.Errorf("cfg.Artifact.Include = %v, want empty", cfg.Artifact.Include)
	}
	if len(cfg.Artifact.Exclude) != 0 {
		t.Errorf("cfg.Artifact.Exclude = %v, want empty (compiled defaults apply at load time)", cfg.Artifact.Exclude)
	}
}

// TestInitialize_DoesNotOverwriteExistingBuildYAMLWithFramework verifies
// non-destructive behavior: a build.yaml that already exists before
// initializing with a framework template must be preserved (TS-P7-28 AC-3).
func TestInitialize_DoesNotOverwriteExistingBuildYAMLWithFramework(t *testing.T) {
	dir := t.TempDir()
	s := project.NewStructure(dir)

	pipelinesDir := s.PipelinesDir
	if err := os.MkdirAll(pipelinesDir, 0755); err != nil {
		t.Fatalf("creating pipelines dir: %v", err)
	}

	buildPath := filepath.Join(pipelinesDir, project.PipelineBuildFileName)
	sentinel := []byte("sentinel: content\n")
	if err := os.WriteFile(buildPath, sentinel, 0644); err != nil {
		t.Fatalf("writing sentinel build.yaml: %v", err)
	}

	if _, err := Initialize("app", dir, WithFramework("laravel")); err != nil {
		t.Fatalf("Initialize() returned unexpected error: %v", err)
	}

	data, err := os.ReadFile(buildPath)
	if err != nil {
		t.Fatalf("reading build.yaml: %v", err)
	}
	if string(data) != string(sentinel) {
		t.Errorf("build.yaml was overwritten:\ngot:  %q\nwant: %q", string(data), string(sentinel))
	}
}

// TestInitialize_AdapterMissing_WritesNoPipelineFiles verifies the
// adapter-missing path after the generic fallback removal (TS-015-01-02,
// ADR-026 decision 1): when the adapter is missing or its template
// command fails, initialization still succeeds and warns on stderr
// directing the user to install the adapter, but writes NO pipeline files
// — the Core no longer owns generic template data to fall back to.
// Adapters are optional (ADR-009 §9.7); pipeline files come from the
// installed standard's content (TS-015-02-03).
func TestInitialize_AdapterMissing_WritesNoPipelineFiles(t *testing.T) {
	stubAdapterTemplate(t, func(_ context.Context, _ string) (contracts.TemplateResult, error) {
		return contracts.TemplateResult{}, errors.New(`adapter executable "anvil-adapter-laravel" not found on PATH`)
	})

	dir := t.TempDir()
	var stderr string
	var result Result
	var err error
	stderr = captureStderr(t, func() {
		result, err = Initialize("app", dir, WithFramework("laravel"))
	})
	if err != nil {
		t.Fatalf("Initialize() returned unexpected error: %v", err)
	}
	if result != ResultCreated {
		t.Fatalf("Initialize() = %v, want %v", result, ResultCreated)
	}

	// The warning must reach stderr in the CLI warning format and direct
	// the user to install the adapter and regenerate.
	if !strings.Contains(stderr, "Warning:") || !strings.Contains(stderr, "anvil adapter use laravel") {
		t.Errorf("stderr = %q, want a warning directing the user to install the adapter", stderr)
	}
	if strings.Contains(stderr, "generic pipeline") {
		t.Errorf("stderr = %q, must not mention a generic pipeline fallback (TS-015-01-02)", stderr)
	}

	// No pipeline files may be written — the runtime owns no template
	// content to fall back to.
	if fileExists(filepath.Join(dir, ".anvil", "pipelines", project.PipelineBuildFileName)) {
		t.Error("build.yaml should not be written when the adapter is missing")
	}
	if fileExists(filepath.Join(dir, ".anvil", "pipelines", project.PipelineCIFileName)) {
		t.Error("ci.yaml should not be written when the adapter is missing")
	}
}

// TestInitialize_InvalidAdapterOutput_NotWritten verifies that an
// adapter returning a pipeline definition that fails the pipeline loader
// validation is never written (ADR-020 §1 — never write unvalidated
// adapter output): the invalid build.yaml is skipped with a warning and
// no Core-owned fallback is written, while a valid CI definition from
// the same adapter is still used (TS-015-01-02).
func TestInitialize_InvalidAdapterOutput_NotWritten(t *testing.T) {
	stubAdapterTemplate(t, func(_ context.Context, _ string) (contracts.TemplateResult, error) {
		// The build definition has no stages — it fails Validate().
		return contracts.TemplateResult{
			Build: &execution.PipelineDefinition{
				Pipeline: execution.Pipeline{Name: "build"},
			},
			CI: &execution.PipelineDefinition{
				Pipeline: execution.Pipeline{
					Name: "ci",
					Stages: []execution.PipelineStage{
						{Name: "test", Tasks: []execution.Task{{Name: "unit-tests", Command: "echo", Args: []string{"ok"}}}},
					},
				},
			},
		}, nil
	})

	dir := t.TempDir()
	var stderr string
	stderr = captureStderr(t, func() {
		if _, err := Initialize("app", dir, WithFramework("laravel")); err != nil {
			t.Fatalf("Initialize() returned unexpected error: %v", err)
		}
	})

	// The invalid build definition must not be written — and no generic
	// Core-owned fallback may replace it (TS-015-01-02).
	if fileExists(filepath.Join(dir, ".anvil", "pipelines", project.PipelineBuildFileName)) {
		t.Error("build.yaml should not be written — invalid adapter output must not be written and no fallback exists")
	}
	if !strings.Contains(stderr, "Warning:") || !strings.Contains(stderr, "failed validation") {
		t.Errorf("stderr = %q, want a warning about the invalid adapter output", stderr)
	}

	// The valid CI definition from the same adapter is still used.
	ciData, err := os.ReadFile(filepath.Join(dir, ".anvil", "pipelines", project.PipelineCIFileName))
	if err != nil {
		t.Fatalf("reading ci.yaml: %v", err)
	}
	var ciDef execution.PipelineDefinition
	if err := yaml.Unmarshal(ciData, &ciDef); err != nil {
		t.Fatalf("yaml.Unmarshal ci.yaml: %v", err)
	}
	if ciDef.Pipeline.Name != "ci" {
		t.Errorf("ci pipeline name = %q, want %q", ciDef.Pipeline.Name, "ci")
	}
	if len(ciDef.Pipeline.Stages) != 1 || ciDef.Pipeline.Stages[0].Name != "test" {
		t.Errorf("ci pipeline stages = %#v, want the adapter's test stage", ciDef.Pipeline.Stages)
	}
}

// TestGenerateFrameworkPipelineConfigs_AdapterTemplateUsed verifies the
// public entry point used by 'anvil adapter use' (TS-007-033): with the
// adapter present, build.yaml and ci.yaml are generated from the
// adapter-owned definitions (ADR-020 §1) and pass the pipeline loader.
func TestGenerateFrameworkPipelineConfigs_AdapterTemplateUsed(t *testing.T) {
	stubAdapterTemplate(t, func(_ context.Context, _ string) (contracts.TemplateResult, error) {
		result := laravelTemplate()
		result.CI = &execution.PipelineDefinition{
			Pipeline: execution.Pipeline{
				Name: "ci",
				Stages: []execution.PipelineStage{
					{Name: "build", Tasks: []execution.Task{{Name: "build", Command: "echo", Args: []string{"building..."}}}},
				},
			},
		}
		return result, nil
	})

	dir := t.TempDir()
	if err := GenerateFrameworkPipelineConfigs(dir, "laravel"); err != nil {
		t.Fatalf("GenerateFrameworkPipelineConfigs() returned unexpected error: %v", err)
	}

	// build.yaml is the adapter-owned Laravel template and passes the
	// pipeline loader used at execution time.
	def, err := execution.LookupBuildDefinition(dir)
	if err != nil {
		t.Fatalf("LookupBuildDefinition() failed: %v", err)
	}
	wantStages := []string{"dependencies", "assets", "optimize"}
	for i, want := range wantStages {
		if def.Pipeline.Stages[i].Name != want {
			t.Errorf("stage %d name = %q, want %q", i, def.Pipeline.Stages[i].Name, want)
		}
	}

	// ci.yaml is the adapter-owned CI definition, not the Core default.
	ciData, err := os.ReadFile(filepath.Join(dir, ".anvil", "pipelines", project.PipelineCIFileName))
	if err != nil {
		t.Fatalf("reading ci.yaml: %v", err)
	}
	var ciDef execution.PipelineDefinition
	if err := yaml.Unmarshal(ciData, &ciDef); err != nil {
		t.Fatalf("yaml.Unmarshal ci.yaml: %v", err)
	}
	if len(ciDef.Pipeline.Stages) != 1 || ciDef.Pipeline.Stages[0].Name != "build" {
		t.Errorf("ci pipeline stages = %#v, want the adapter's build stage", ciDef.Pipeline.Stages)
	}
}

// TestInitialize_NoOptions_NoFrameworkKey verifies that initializing without
// options writes no pipeline files and anvil.yaml contains no framework key
// (omitempty — TS-P7-29 AC-4). Without a framework declaration there is no
// standard content to generate pipeline files from (TS-015-01-02).
func TestInitialize_NoOptions_NoFrameworkKey(t *testing.T) {
	dir := t.TempDir()

	if _, err := Initialize("app", dir); err != nil {
		t.Fatalf("Initialize() returned unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, project.ConfigFileName))
	if err != nil {
		t.Fatalf("reading config file: %v", err)
	}
	if strings.Contains(string(data), "framework:") {
		t.Errorf("anvil.yaml contains framework key, want it omitted:\n%s", string(data))
	}

	var cfg config.ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal config: %v\nraw content:\n%s", err, string(data))
	}
	if cfg.Project.Framework != "" {
		t.Errorf("cfg.Project.Framework = %q, want empty", cfg.Project.Framework)
	}
	if len(cfg.Artifact.Include) != 0 {
		t.Errorf("cfg.Artifact.Include = %v, want empty", cfg.Artifact.Include)
	}

	// No pipeline template files: the runtime owns no default pipeline
	// template content (TS-015-01-02).
	if fileExists(filepath.Join(dir, ".anvil", "pipelines", project.PipelineBuildFileName)) {
		t.Error("build.yaml should not be generated without a framework")
	}
	if fileExists(filepath.Join(dir, ".anvil", "pipelines", project.PipelineCIFileName)) {
		t.Error("ci.yaml should not be generated without a framework")
	}
}

// --- helpers ---

// TestInitialize_StandardTemplateContent_GeneratesPipelines verifies the
// generation success case (TS-015-02-03 DoD: generated content comes
// from the installed standard, not the runtime): when the resolved
// installed delivery lifecycle standard's record carries template
// content, initialization writes .anvil/pipelines/build.yaml and
// .anvil/pipelines/ci.yaml with EXACTLY the standard's content — and the
// adapter's template command is never consulted (the standard is the
// authoritative source when it supplies content, ADR-026 decision 2).
func TestInitialize_StandardTemplateContent_GeneratesPipelines(t *testing.T) {
	dir := t.TempDir()
	rec := sampleInstalledStandardRecord("anvil-standard-laravel", "1.2.3")
	rec.Templates = &registry.TemplateContent{
		Namespace: "laravel",
		Templates: []registry.TemplateFile{
			{ID: "build", Description: "Laravel build pipeline.", Content: laravelTemplateYAML(t)},
			{ID: "ci", Description: "Laravel CI pipeline.", Content: ciTemplateYAML(t)},
		},
	}

	// The adapter must NOT be consulted: the standard supplies the
	// content, so the adapter seam is not invoked.
	stubAdapterTemplate(t, func(ctx context.Context, framework string) (contracts.TemplateResult, error) {
		t.Errorf("fetchAdapterTemplate must not be consulted when the standard supplies template content (framework %q)", framework)
		return contracts.TemplateResult{}, errors.New("adapter must not be consulted")
	})

	result, err := Initialize("app", dir, WithFramework("laravel"), WithFrameworkStandard(rec))
	if err != nil {
		t.Fatalf("Initialize() returned unexpected error: %v", err)
	}
	if result != ResultCreated {
		t.Fatalf("Initialize() = %v, want %v", result, ResultCreated)
	}

	// build.yaml must be byte-for-byte the standard's build template
	// content.
	buildData, err := os.ReadFile(filepath.Join(dir, ".anvil", "pipelines", project.PipelineBuildFileName))
	if err != nil {
		t.Fatalf("reading build.yaml: %v", err)
	}
	if string(buildData) != rec.Templates.Templates[0].Content {
		t.Errorf("build.yaml content = %q, want the standard's template content %q", string(buildData), rec.Templates.Templates[0].Content)
	}

	// ci.yaml must be byte-for-byte the standard's CI template content.
	ciData, err := os.ReadFile(filepath.Join(dir, ".anvil", "pipelines", project.PipelineCIFileName))
	if err != nil {
		t.Fatalf("reading ci.yaml: %v", err)
	}
	if string(ciData) != rec.Templates.Templates[1].Content {
		t.Errorf("ci.yaml content = %q, want the standard's template content %q", string(ciData), rec.Templates.Templates[1].Content)
	}
}

// TestInitialize_StandardTemplateContent_ParityWithAdapterOutput verifies
// the output parity guarantee of TS-015-02-03 (contract-stable
// migration, Transition Plan §6.5): the standard's template content is
// the same pipeline file content the adapter-driven path previously
// wrote (the parity fixture laravelTemplateYAML), and the generated
// project is lifecycle-ready — the written build.yaml loads through the
// same pipeline loader used at execution time and carries the same
// structure the adapter-driven generation produced.
func TestInitialize_StandardTemplateContent_ParityWithAdapterOutput(t *testing.T) {
	dir := t.TempDir()
	rec := sampleInstalledStandardRecord("anvil-standard-laravel", "1.2.3")
	rec.Templates = &registry.TemplateContent{
		Namespace: "laravel",
		Templates: []registry.TemplateFile{
			{ID: "build", Content: laravelTemplateYAML(t)},
		},
	}
	stubAdapterTemplate(t, func(ctx context.Context, framework string) (contracts.TemplateResult, error) {
		t.Errorf("fetchAdapterTemplate must not be consulted when the standard supplies template content")
		return contracts.TemplateResult{}, errors.New("adapter must not be consulted")
	})

	if _, err := Initialize("app", dir, WithFramework("laravel"), WithFrameworkStandard(rec)); err != nil {
		t.Fatalf("Initialize() returned unexpected error: %v", err)
	}

	// The generated build.yaml must be lifecycle-ready: it loads through
	// LookupBuildDefinition (the execution-time loader) with the same
	// structure the adapter-driven path produced in
	// TestInitialize_WithLaravelFramework.
	def, err := execution.LookupBuildDefinition(dir)
	if err != nil {
		t.Fatalf("LookupBuildDefinition() failed: %v", err)
	}
	if def.Pipeline.Name != "build" {
		t.Errorf("pipeline name = %q, want %q", def.Pipeline.Name, "build")
	}
	wantStages := []string{"dependencies", "assets", "optimize"}
	if len(def.Pipeline.Stages) != len(wantStages) {
		t.Fatalf("build pipeline stages = %d, want %d", len(def.Pipeline.Stages), len(wantStages))
	}
	for i, want := range wantStages {
		if def.Pipeline.Stages[i].Name != want {
			t.Errorf("stage %d name = %q, want %q", i, def.Pipeline.Stages[i].Name, want)
		}
	}
}

// TestInitialize_StandardTemplateContent_InvalidContent_FailsBeforeFilesystem
// verifies the no-partial-generation property of the generation path
// (TS-015-02-03 DoD: generation produces a valid, lifecycle-ready
// project): template content in the installed standard's record that
// fails the pipeline loader is a broken installed-standard record — a
// real failure with the reinstall remediation, and it fails BEFORE any
// filesystem work (no config file, no pipelines directory). The runtime
// never writes unvalidated content (ADR-007) and never falls back to
// runtime-owned template data (TS-015-01-02).
func TestInitialize_StandardTemplateContent_InvalidContent_FailsBeforeFilesystem(t *testing.T) {
	dir := t.TempDir()
	rec := sampleInstalledStandardRecord("anvil-standard-laravel", "1.2.3")
	rec.Templates = &registry.TemplateContent{
		Namespace: "laravel",
		Templates: []registry.TemplateFile{
			// No stages: fails the pipeline loader validation.
			{ID: "build", Content: "pipeline:\n  name: build\n"},
		},
	}

	_, err := Initialize("app", dir, WithFramework("laravel"), WithFrameworkStandard(rec))
	if err == nil {
		t.Fatal("Initialize() expected an error for invalid standard template content, got nil")
	}
	if !strings.Contains(err.Error(), "fails the pipeline loader validation") {
		t.Errorf("expected an actionable validation failure, got: %v", err)
	}

	// No partial generation: the failure happened before any filesystem
	// work — no config file and no pipelines directory.
	if fileExists(filepath.Join(dir, project.ConfigFileName)) {
		t.Error("config file must not be written when the standard's template content is invalid")
	}
	if isDir(filepath.Join(dir, ".anvil", "pipelines")) {
		t.Error("pipelines directory must not be created when the standard's template content is invalid")
	}
}

// TestInitialize_StandardTemplateContent_UnknownTemplateID_FailsBeforeFilesystem
// verifies that a template id the runtime has no pipeline position for is
// a record inconsistency, rejected with an actionable error before any
// filesystem work (TS-015-02-03; C7 — rejected, never patched): the
// runtime owns the build and CI pipeline positions (ADR-007) and never
// invents a position for undeclared content.
func TestInitialize_StandardTemplateContent_UnknownTemplateID_FailsBeforeFilesystem(t *testing.T) {
	dir := t.TempDir()
	rec := sampleInstalledStandardRecord("anvil-standard-laravel", "1.2.3")
	rec.Templates = &registry.TemplateContent{
		Namespace: "laravel",
		Templates: []registry.TemplateFile{
			{ID: "build", Content: laravelTemplateYAML(t)},
			{ID: "deploy", Content: "pipeline:\n  name: deploy\n  stages: []\n"},
		},
	}

	_, err := Initialize("app", dir, WithFramework("laravel"), WithFrameworkStandard(rec))
	if err == nil {
		t.Fatal("Initialize() expected an error for an unknown template id, got nil")
	}
	if !strings.Contains(err.Error(), "deploy") || !strings.Contains(err.Error(), "build, ci") {
		t.Errorf("expected an actionable error naming the unknown id and the supported positions, got: %v", err)
	}
	if fileExists(filepath.Join(dir, project.ConfigFileName)) {
		t.Error("config file must not be written when the standard declares an unknown template id")
	}
}

// TestInitialize_StandardTemplateContent_NamespaceMismatch_Fails verifies
// that a namespace violation inside the resolved standard's record is a
// real failure, never a silent pass-through (TS-015-02-03): template
// content is namespace-isolated (C6, command-contract §4.5) — content
// declaring a namespace different from the declared framework is
// inconsistent with the standard it belongs to and aborts initialization
// before any filesystem work.
func TestInitialize_StandardTemplateContent_NamespaceMismatch_Fails(t *testing.T) {
	dir := t.TempDir()
	rec := sampleInstalledStandardRecord("anvil-standard-laravel", "1.2.3")
	rec.Templates = &registry.TemplateContent{
		Namespace: "rails",
		Templates: []registry.TemplateFile{
			{ID: "build", Content: laravelTemplateYAML(t)},
		},
	}

	_, err := Initialize("app", dir, WithFramework("laravel"), WithFrameworkStandard(rec))
	if err == nil {
		t.Fatal("Initialize() expected an error for a template content namespace mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "namespace") || !strings.Contains(err.Error(), "rails") {
		t.Errorf("namespace mismatch error should name the violation, got: %v", err)
	}
	if fileExists(filepath.Join(dir, project.ConfigFileName)) {
		t.Error("config file must not be written when the template content namespace violates isolation")
	}
}

// TestInitialize_StandardTemplateContent_Missing_AdapterFallback verifies
// the interim fallback (TS-015-02-03): a resolved standard whose record
// declares no template content is a valid state (command-contract §4.1)
// — initialization succeeds, the omission is explicit via the same
// hand-off/warning pattern T-004 established (never silent), and the
// pipeline files come from the installed adapter's template command (the
// interim distribution path, ADR-020) exactly as before this work item.
func TestInitialize_StandardTemplateContent_Missing_AdapterFallback(t *testing.T) {
	dir := t.TempDir()
	rec := sampleInstalledStandardRecord("anvil-standard-laravel", "1.2.3")

	stubAdapterTemplate(t, func(_ context.Context, framework string) (contracts.TemplateResult, error) {
		if framework != "laravel" {
			t.Errorf("fetchAdapterTemplate framework = %q, want %q", framework, "laravel")
		}
		return laravelTemplate(), nil
	})

	var stderr string
	stderr = captureStderr(t, func() {
		result, err := Initialize("app", dir, WithFramework("laravel"), WithFrameworkStandard(rec))
		if err != nil {
			t.Fatalf("Initialize() returned unexpected error: %v", err)
		}
		if result != ResultCreated {
			t.Fatalf("Initialize() = %v, want %v", result, ResultCreated)
		}
	})

	// The omission is explicit: the missing-template-content warning
	// reaches stderr in the CLI warning format.
	if !strings.Contains(stderr, "Warning:") || !strings.Contains(stderr, "no template content") {
		t.Errorf("expected an explicit missing-template-content warning, stderr: %s", stderr)
	}

	// The interim adapter path supplies the pipeline files (unchanged
	// behavior): build.yaml is the adapter-owned Laravel template.
	def, err := execution.LookupBuildDefinition(dir)
	if err != nil {
		t.Fatalf("LookupBuildDefinition() failed: %v", err)
	}
	wantStages := []string{"dependencies", "assets", "optimize"}
	if len(def.Pipeline.Stages) != len(wantStages) {
		t.Fatalf("build pipeline stages = %d, want %d", len(def.Pipeline.Stages), len(wantStages))
	}
	for i, want := range wantStages {
		if def.Pipeline.Stages[i].Name != want {
			t.Errorf("stage %d name = %q, want %q", i, def.Pipeline.Stages[i].Name, want)
		}
	}
}

// ciTemplateYAML returns a CI pipeline template in the pipeline file
// format for engine tests.
func ciTemplateYAML(t *testing.T) string {
	t.Helper()
	result := contracts.TemplateResult{
		CI: &execution.PipelineDefinition{
			Pipeline: execution.Pipeline{
				Name: "ci",
				Stages: []execution.PipelineStage{
					{Name: "test", Tasks: []execution.Task{{Name: "unit-tests", Command: "echo", Args: []string{"ok"}}}},
				},
			},
		},
	}
	data, err := yaml.Marshal(result.CI)
	if err != nil {
		t.Fatalf("marshal ci template: %v", err)
	}
	return string(data)
}

// sampleInstalledStandardRecord returns a structurally valid
// installed-standard record (registry.InstalledStandardRecord) for the
// resolution option tests: identity, pinned version, declared contract
// version, and an explicit resolution (ADR-022 §3).
func sampleInstalledStandardRecord(id, version string) registry.InstalledStandardRecord {
	return registry.InstalledStandardRecord{
		FormatVersion:   registry.RecordFormatVersion,
		ID:              id,
		Version:         version,
		ContractVersion: "1.0.0",
		Resolution: registry.Resolution{
			Kind:   registry.ResolutionKindIndex,
			Source: "/home/operator/registry-index",
		},
		InstalledAt: time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC),
		Lifecycle: registry.Lifecycle{
			State: registry.LifecycleStatePublished,
		},
	}
}

// TestInitialize_WithResolvedStandard verifies that initialization
// accepts the explicitly resolved installed delivery lifecycle standard
// (TS-015-02-01): the resolution is recorded at the initialization
// boundary via WithFrameworkStandard — the record's id matches the
// declared framework's standard id (anvil-standard-laravel) and the
// project is created normally with the framework declaration stored.
func TestInitialize_WithResolvedStandard(t *testing.T) {
	dir := t.TempDir()
	rec := sampleInstalledStandardRecord("anvil-standard-laravel", "1.2.3")

	result, err := Initialize("app", dir, WithFramework("laravel"), WithFrameworkStandard(rec))
	if err != nil {
		t.Fatalf("Initialize() returned unexpected error: %v", err)
	}
	if result != ResultCreated {
		t.Fatalf("Initialize() = %v, want %v", result, ResultCreated)
	}

	data, err := os.ReadFile(filepath.Join(dir, project.ConfigFileName))
	if err != nil {
		t.Fatalf("reading config file: %v", err)
	}
	var cfg config.ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal config: %v", err)
	}
	if cfg.Project.Framework != "laravel" {
		t.Errorf("cfg.Project.Framework = %q, want %q", cfg.Project.Framework, "laravel")
	}
}

// TestInitialize_ResolvedStandardMismatch verifies the coherence guard of
// the resolution boundary: a resolved standard whose id is not the
// declared framework's standard id (registry.StandardIDForFramework) is a
// caller defect and fails fast before any filesystem work — the engine
// never initializes against a resolution that does not match the
// declaration (TS-015-02-01).
func TestInitialize_ResolvedStandardMismatch(t *testing.T) {
	dir := t.TempDir()
	rec := sampleInstalledStandardRecord("anvil-standard-flutter", "2.0.0")

	_, err := Initialize("app", dir, WithFramework("laravel"), WithFrameworkStandard(rec))
	if err == nil {
		t.Fatal("Initialize() expected an error for a mismatched resolved standard, got nil")
	}
	if !strings.Contains(err.Error(), "anvil-standard-flutter") || !strings.Contains(err.Error(), "anvil-standard-laravel") {
		t.Errorf("mismatch error should name both standard ids, got: %v", err)
	}

	// No filesystem work happened: the project must not exist.
	if fileExists(filepath.Join(dir, project.ConfigFileName)) {
		t.Error("config file must not be written when the resolution is incoherent")
	}
}

// TestInitialize_ResolvedStandardWithoutFramework verifies the coherence
// guard for the second caller defect: a resolved standard provided
// without a framework declaration is rejected — the resolution is only
// meaningful for framework-declared initialization (TS-015-02-01).
func TestInitialize_ResolvedStandardWithoutFramework(t *testing.T) {
	dir := t.TempDir()
	rec := sampleInstalledStandardRecord("anvil-standard-laravel", "1.2.3")

	_, err := Initialize("app", dir, WithFrameworkStandard(rec))
	if err == nil {
		t.Fatal("Initialize() expected an error for a resolved standard without a framework declaration, got nil")
	}
	if fileExists(filepath.Join(dir, project.ConfigFileName)) {
		t.Error("config file must not be written when the resolution has no framework declaration")
	}
}

// TestInitialize_ConfigExtensionMerged verifies the config extension
// consuming side (TS-015-03-01 DoD: framework config keys resolve from
// the installed standard; defaults come from the standard, not the
// runtime): when the resolved installed delivery lifecycle standard
// carries configuration extension content, initialization merges the
// declared keys and their defaults into the project configuration under
// the framework's own namespace (framework.<name>.<key> = default,
// ADR-005 §4.4). The values come from the record — never from runtime
// knowledge (ADR-026 decision 1).
func TestInitialize_ConfigExtensionMerged(t *testing.T) {
	dir := t.TempDir()
	rec := sampleInstalledStandardRecord("anvil-standard-laravel", "1.2.3")
	rec.ConfigExtension = &registry.ConfigExtensionContent{
		Namespace: "laravel",
		Keys: []registry.ConfigExtensionKey{
			{Name: "version", Description: "Laravel version.", Default: "11.0.0"},
			{Name: "cache.store", Description: "Cache store.", Default: "redis"},
			{Name: "migrations.path", Description: "Migrations path.", Default: "database/migrations"},
			// No default: user-provided value, never written.
			{Name: "build_args", Description: "Extra build args.", Required: true},
		},
	}
	// The record also carries template content: resolved content in both
	// categories must merge/generate without warnings (TS-015-02-03 —
	// the missing-template-content warning only fires when the standard
	// declares nothing in the category).
	rec.Templates = &registry.TemplateContent{
		Namespace: "laravel",
		Templates: []registry.TemplateFile{
			{ID: "build", Content: laravelTemplateYAML(t)},
		},
	}

	// No adapter template: stub it out so pipeline generation is a no-op.
	stubAdapterTemplate(t, func(ctx context.Context, framework string) (contracts.TemplateResult, error) {
		return contracts.TemplateResult{}, nil
	})

	stderr := captureStderr(t, func() {
		result, err := Initialize("app", dir, WithFramework("laravel"), WithFrameworkStandard(rec))
		if err != nil {
			t.Fatalf("Initialize() returned unexpected error: %v", err)
		}
		if result != ResultCreated {
			t.Fatalf("Initialize() = %v, want %v", result, ResultCreated)
		}
	})
	if strings.Contains(stderr, "Warning:") {
		t.Errorf("config extension merge must not warn on resolved content, stderr: %s", stderr)
	}

	data, err := os.ReadFile(filepath.Join(dir, project.ConfigFileName))
	if err != nil {
		t.Fatalf("reading config file: %v", err)
	}
	var cfg config.ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal config: %v", err)
	}
	if cfg.Project.Framework != "laravel" {
		t.Errorf("cfg.Project.Framework = %q, want %q", cfg.Project.Framework, "laravel")
	}

	want := map[string]string{
		"version":         "11.0.0",
		"cache.store":     "redis",
		"migrations.path": "database/migrations",
	}
	got := cfg.Framework["laravel"]
	if len(got) != len(want) {
		t.Errorf("cfg.Framework[laravel] = %v, want %v", got, want)
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("cfg.Framework[laravel][%q] = %q, want %q", key, got[key], value)
		}
	}
	// A key without a declared default must not be written.
	if _, ok := got["build_args"]; ok {
		t.Errorf("cfg.Framework[laravel][build_args] must not be written without a default, got: %v", got)
	}
}

// TestInitialize_ConfigExtensionMissing_HandsOff verifies the
// missing-extension handling (TS-015-03-01): a resolved standard that
// declares no configuration extension content is a valid state (a
// standard may declare nothing in a category, command-contract §4.1) —
// initialization succeeds, no framework config section is merged (the
// Core owns no framework config defaults to fall back to, TS-015-01-03),
// and the omission is explicit via the same hand-off/warning pattern
// T-004 established for a missing standard — never silent, never a
// hard-fail (TS-015-02-02 implements the hard-fail later).
func TestInitialize_ConfigExtensionMissing_HandsOff(t *testing.T) {
	dir := t.TempDir()
	rec := sampleInstalledStandardRecord("anvil-standard-laravel", "1.2.3")

	stubAdapterTemplate(t, func(ctx context.Context, framework string) (contracts.TemplateResult, error) {
		return contracts.TemplateResult{}, nil
	})

	stderr := captureStderr(t, func() {
		result, err := Initialize("app", dir, WithFramework("laravel"), WithFrameworkStandard(rec))
		if err != nil {
			t.Fatalf("Initialize() returned unexpected error: %v", err)
		}
		if result != ResultCreated {
			t.Fatalf("Initialize() = %v, want %v", result, ResultCreated)
		}
	})
	if !strings.Contains(stderr, "Warning:") || !strings.Contains(stderr, "no configuration extension content") {
		t.Errorf("expected an explicit config-extension-missing warning, stderr: %s", stderr)
	}

	data, err := os.ReadFile(filepath.Join(dir, project.ConfigFileName))
	if err != nil {
		t.Fatalf("reading config file: %v", err)
	}
	var cfg config.ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal config: %v", err)
	}
	// The top-level framework section (config extension values) must be
	// omitted; project.framework: laravel is the declaration and must stay.
	if cfg.Framework != nil {
		t.Errorf("cfg.Framework must be nil when the standard declares no config extension content, got: %v", cfg.Framework)
	}
	if cfg.Project.Framework != "laravel" {
		t.Errorf("cfg.Project.Framework = %q, want %q (the declaration pass-through must be preserved)", cfg.Project.Framework, "laravel")
	}
}

// TestInitialize_ConfigExtensionNamespaceMismatch_Fails verifies that a
// namespace violation inside the resolved standard's record is a real
// failure, never a silent pass-through (TS-015-03-01): configuration
// extension content is namespace-isolated (C6, command-contract §4.5) —
// content declaring a namespace different from the declared framework is
// inconsistent with the standard it belongs to and aborts initialization
// before any config extension values are merged.
func TestInitialize_ConfigExtensionNamespaceMismatch_Fails(t *testing.T) {
	dir := t.TempDir()
	rec := sampleInstalledStandardRecord("anvil-standard-laravel", "1.2.3")
	rec.ConfigExtension = &registry.ConfigExtensionContent{
		Namespace: "rails",
		Keys: []registry.ConfigExtensionKey{
			{Name: "version", Description: "Version.", Default: "7.1.0"},
		},
	}

	_, err := Initialize("app", dir, WithFramework("laravel"), WithFrameworkStandard(rec))
	if err == nil {
		t.Fatal("Initialize() expected an error for a config extension namespace mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "namespace") || !strings.Contains(err.Error(), "rails") {
		t.Errorf("namespace mismatch error should name the violation, got: %v", err)
	}
	if fileExists(filepath.Join(dir, project.ConfigFileName)) {
		t.Error("config file must not be written when the config extension namespace violates isolation")
	}
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
