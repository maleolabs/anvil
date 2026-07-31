package engine

import (
	"os"
	"path/filepath"
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
