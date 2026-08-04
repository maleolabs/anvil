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

	"gopkg.in/yaml.v3"
	"maleolabs.com/anvil/internal/config"
	"maleolabs.com/anvil/internal/contracts"
	"maleolabs.com/anvil/internal/execution"
	"maleolabs.com/anvil/internal/project"
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
// tests stub fetchAdapterTemplate with — the same definition the
// internal/laravel adapter's template command returns (TS-007-038).
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
// tests stub fetchAdapterTemplate with — the same definition the
// internal/flutter adapter's template command returns, including the
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

// TestInitialize_CreatesProject verifies that running Initialize in an empty
// directory creates the project configuration file and pipeline configs.
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

	// Verify pipeline files were created inside .anvil/pipelines/.
	if !fileExists(filepath.Join(s.PipelinesDir, project.PipelineBuildFileName)) {
		t.Errorf("expected %s to exist", filepath.Join(s.PipelinesDir, project.PipelineBuildFileName))
	}
	if !fileExists(filepath.Join(s.PipelinesDir, project.PipelineCIFileName)) {
		t.Errorf("expected %s to exist", filepath.Join(s.PipelinesDir, project.PipelineCIFileName))
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
// pipeline configuration and lifecycle state directories inside .anvil/.
func TestInitialize_NoRuntimeState(t *testing.T) {
	dir := t.TempDir()

	_, err := Initialize("no-state", dir)
	if err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}

	s := project.NewStructure(dir)

	// .anvil/ directory should exist (it contains pipeline configs).
	if !isDir(s.AnvilDir) {
		t.Error(".anvil/ directory should exist — pipeline configs are created during init")
	}

	// Pipeline config directories must exist.
	if !isDir(s.PipelinesDir) {
		t.Error(".anvil/pipelines/ directory should exist")
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

// TestInitialize_CreatesPipelineDirectory verifies that the .anvil/pipelines/
// directory is created during initialization.
func TestInitialize_CreatesPipelineDirectory(t *testing.T) {
	dir := t.TempDir()

	_, err := Initialize("test-app", dir)
	if err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}

	s := project.NewStructure(dir)
	if !isDir(s.PipelinesDir) {
		t.Error(".anvil/pipelines/ directory should exist after init")
	}
}

// TestInitialize_CreatesBuildYAML verifies that build.yaml is created with
// valid pipeline YAML content.
func TestInitialize_CreatesBuildYAML(t *testing.T) {
	dir := t.TempDir()

	_, err := Initialize("test-app", dir)
	if err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}

	s := project.NewStructure(dir)
	buildPath := filepath.Join(s.PipelinesDir, project.PipelineBuildFileName)

	data, err := os.ReadFile(buildPath)
	if err != nil {
		t.Fatalf("reading build.yaml: %v", err)
	}

	var def execution.PipelineDefinition
	if err := yaml.Unmarshal(data, &def); err != nil {
		t.Fatalf("yaml.Unmarshal build.yaml: %v\nraw content:\n%s", err, string(data))
	}

	if def.Pipeline.Name != "build" {
		t.Errorf("pipeline name = %q, want %q", def.Pipeline.Name, "build")
	}
	if len(def.Pipeline.Stages) != 0 {
		t.Errorf("build pipeline stages = %d, want 0 (empty stages)", len(def.Pipeline.Stages))
	}
}

// TestInitialize_CreatesCIYAML verifies that ci.yaml is created with
// valid pipeline YAML content including default build + test stages.
func TestInitialize_CreatesCIYAML(t *testing.T) {
	dir := t.TempDir()

	_, err := Initialize("test-app", dir)
	if err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}

	s := project.NewStructure(dir)
	ciPath := filepath.Join(s.PipelinesDir, project.PipelineCIFileName)

	data, err := os.ReadFile(ciPath)
	if err != nil {
		t.Fatalf("reading ci.yaml: %v", err)
	}

	var def execution.PipelineDefinition
	if err := yaml.Unmarshal(data, &def); err != nil {
		t.Fatalf("yaml.Unmarshal ci.yaml: %v\nraw content:\n%s", err, string(data))
	}

	if def.Pipeline.Name != "ci" {
		t.Errorf("pipeline name = %q, want %q", def.Pipeline.Name, "ci")
	}
	if len(def.Pipeline.Stages) != 2 {
		t.Fatalf("ci pipeline stages = %d, want 2", len(def.Pipeline.Stages))
	}
	if def.Pipeline.Stages[0].Name != "build" {
		t.Errorf("stage 0 name = %q, want %q", def.Pipeline.Stages[0].Name, "build")
	}
	if def.Pipeline.Stages[1].Name != "test" {
		t.Errorf("stage 1 name = %q, want %q", def.Pipeline.Stages[1].Name, "test")
	}
}

// TestInitialize_DoesNotOverwritePipelineFiles verifies that re-initialization
// after manually modifying pipeline files does not overwrite them.
func TestInitialize_DoesNotOverwritePipelineFiles(t *testing.T) {
	dir := t.TempDir()

	// First init creates default pipeline files.
	_, err := Initialize("test-app", dir)
	if err != nil {
		t.Fatalf("first Initialize() failed: %v", err)
	}

	s := project.NewStructure(dir)
	buildPath := filepath.Join(s.PipelinesDir, project.PipelineBuildFileName)

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
// command (ADR-020 §1), stores the framework in anvil.yaml, keeps
// artifact.include empty (a non-empty include list is a strict whitelist
// in the artifact filter), and carries the compiled default exclude list
// minus "vendor/**" so vendor/ stays packaged (runtime-critical for
// Laravel, ADR-017), and keeps ci.yaml as the default CI pipeline when
// the adapter provides no CI definition (TS-P7-28 AC-1, TS-P7-29
// AC-1/AC-4, ADR-020 §1 fallback).
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

	// anvil.yaml must store the framework, keep artifact.include empty (a
	// non-empty include list acts as a whitelist in the artifact filter),
	// and carry the compiled default exclude list minus "vendor/**".
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
	if len(cfg.Artifact.Include) != 0 {
		t.Errorf("cfg.Artifact.Include = %v, want empty", cfg.Artifact.Include)
	}
	if slices.Contains(cfg.Artifact.Exclude, "vendor/**") {
		t.Errorf("cfg.Artifact.Exclude = %v, must not contain vendor/**", cfg.Artifact.Exclude)
	}
	for _, want := range []string{".git/**", "node_modules/**"} {
		if !slices.Contains(cfg.Artifact.Exclude, want) {
			t.Errorf("cfg.Artifact.Exclude = %v, want it to contain %q", cfg.Artifact.Exclude, want)
		}
	}

	// ci.yaml must remain the default CI pipeline.
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
	if len(ciDef.Pipeline.Stages) != 2 {
		t.Errorf("ci pipeline stages = %d, want 2", len(ciDef.Pipeline.Stages))
	}
}

// TestInitialize_WithFlutterFramework verifies that initializing with
// WithFramework("flutter") writes the Flutter build template (single build
// stage with flutter-web/flutter-apk/flutter-ios tasks carrying platform
// metadata) fetched from the adapter's template command (ADR-020 §1),
// stores the framework in anvil.yaml, and keeps ci.yaml as the default CI
// pipeline (TS-P7-27 AC-1..AC-3, ADR-018).
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

	// ci.yaml must remain the default CI pipeline.
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
}

// TestInitialize_UnknownFramework verifies that an unsupported framework
// fails before any file is written: the project config file must not exist
// after the error (TS-P7-28 AC-4).
func TestInitialize_UnknownFramework(t *testing.T) {
	dir := t.TempDir()

	_, err := Initialize("app", dir, WithFramework("symfony"))
	if err == nil {
		t.Fatal("expected error for unknown framework, got nil")
	}
	if !strings.Contains(err.Error(), "unknown framework") {
		t.Errorf("error = %q, want it to mention 'unknown framework'", err.Error())
	}

	// Nothing must have been written — not even the config file.
	if fileExists(filepath.Join(dir, project.ConfigFileName)) {
		t.Error("anvil.yaml should not exist after failed initialization")
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

// TestInitialize_AdapterMissingFallsBackToGeneric verifies the ADR-020 §1
// fallback: when the adapter is missing or its template command fails,
// initialization still succeeds with the generic build pipeline and a
// warning on stderr directing the user to install the adapter — adapters
// are optional (ADR-009 §9.7).
func TestInitialize_AdapterMissingFallsBackToGeneric(t *testing.T) {
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

	// The fallback warning must reach stderr in the CLI warning format.
	if !strings.Contains(stderr, "Warning:") || !strings.Contains(stderr, "anvil adapter use laravel") {
		t.Errorf("stderr = %q, want a warning directing the user to install the adapter", stderr)
	}

	// build.yaml must be the generic default (name "build", no stages).
	buildData, err := os.ReadFile(filepath.Join(dir, ".anvil", "pipelines", project.PipelineBuildFileName))
	if err != nil {
		t.Fatalf("reading build.yaml: %v", err)
	}
	var def execution.PipelineDefinition
	if err := yaml.Unmarshal(buildData, &def); err != nil {
		t.Fatalf("yaml.Unmarshal build.yaml: %v", err)
	}
	if def.Pipeline.Name != "build" {
		t.Errorf("pipeline name = %q, want %q", def.Pipeline.Name, "build")
	}
	if len(def.Pipeline.Stages) != 0 {
		t.Errorf("build pipeline stages = %d, want 0 (generic default)", len(def.Pipeline.Stages))
	}
}

// TestInitialize_InvalidAdapterOutputFallsBackToGeneric verifies that an
// adapter returning a pipeline definition that fails the pipeline loader
// validation is never written (ADR-020 §1 — never write unvalidated
// adapter output): build.yaml falls back to the generic default with a
// warning, while a valid CI definition from the same adapter is still
// used.
func TestInitialize_InvalidAdapterOutputFallsBackToGeneric(t *testing.T) {
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

	// The invalid build definition must not be written: build.yaml is the
	// generic default (0 stages).
	buildData, err := os.ReadFile(filepath.Join(dir, ".anvil", "pipelines", project.PipelineBuildFileName))
	if err != nil {
		t.Fatalf("reading build.yaml: %v", err)
	}
	var def execution.PipelineDefinition
	if err := yaml.Unmarshal(buildData, &def); err != nil {
		t.Fatalf("yaml.Unmarshal build.yaml: %v", err)
	}
	if len(def.Pipeline.Stages) != 0 {
		t.Errorf("build pipeline stages = %d, want 0 — invalid adapter output must not be written", len(def.Pipeline.Stages))
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
// options behaves exactly as before: the default build pipeline is generated
// and anvil.yaml contains no framework key (omitempty — TS-P7-29 AC-4).
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

	// Default build pipeline (name "build", no stages). Read the file
	// directly: LookupBuildDefinition validates and rejects the empty
	// default pipeline, so parse like the other pipeline tests do.
	buildData, err := os.ReadFile(filepath.Join(dir, ".anvil", "pipelines", project.PipelineBuildFileName))
	if err != nil {
		t.Fatalf("reading build.yaml: %v", err)
	}
	var def execution.PipelineDefinition
	if err := yaml.Unmarshal(buildData, &def); err != nil {
		t.Fatalf("yaml.Unmarshal build.yaml: %v", err)
	}
	if def.Pipeline.Name != "build" {
		t.Errorf("pipeline name = %q, want %q", def.Pipeline.Name, "build")
	}
	if len(def.Pipeline.Stages) != 0 {
		t.Errorf("build pipeline stages = %d, want 0", len(def.Pipeline.Stages))
	}
}

// --- helpers ---

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
