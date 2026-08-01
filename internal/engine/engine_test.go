package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"maleolabs.com/anvil/internal/config"
	"maleolabs.com/anvil/internal/execution"
	"maleolabs.com/anvil/internal/project"
)

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
// dependencies/assets/optimize), stores the framework in anvil.yaml, adds
// vendor/** to the artifact include list, and keeps ci.yaml as the default
// CI pipeline (TS-P7-28 AC-1, TS-P7-29 AC-1/AC-4).
func TestInitialize_WithLaravelFramework(t *testing.T) {
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

	// anvil.yaml must store the framework and the vendor/** include override.
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
	if len(cfg.Artifact.Include) != 1 || cfg.Artifact.Include[0] != "vendor/**" {
		t.Errorf("cfg.Artifact.Include = %v, want [vendor/**]", cfg.Artifact.Include)
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
