// Package config provides multi-source configuration loader tests for
// Anvil projects.
//
// Reference: TS-P2-04, ADR-005 §7.5, §10.2
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLoadConfig_DefaultsApplied verifies that when no configuration files
// or environment variables are present (except for required project.name),
// the loader returns a configuration containing compiled defaults for every
// schema key that has a default.
//
// Covers AC: Compiled defaults applied for every schema key.
func TestLoadConfig_DefaultsApplied(t *testing.T) {
	tmpDir := t.TempDir()
	globalRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", globalRoot)
	// Provide the required project.name via env var so validation passes.
	t.Setenv("ANVIL_CFG_PROJECT_NAME", "defaults-test")

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() returned error with only defaults: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadConfig() returned nil config")
	}

	// Verify that schema defaults are present.
	schema := GetSchema()
	for key, entry := range schema.Entries {
		if entry.Default == nil {
			continue
		}
		val, _, err := cfg.Get(key)
		if err != nil {
			t.Errorf("config missing default for key %q: %v", key, err)
			continue
		}
		if val == nil {
			t.Errorf("config[%q] is nil, want default %v", key, entry.Default)
		}
	}
}

// TestLoadConfig_ProjectFileLoaded verifies that values from a project
// YAML configuration file are loaded and override compiled defaults.
//
// Covers AC: Values from project YAML files loaded.
func TestLoadConfig_ProjectFileLoaded(t *testing.T) {
	tmpDir := t.TempDir()
	globalRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", globalRoot)

	yamlContent := `
project:
  name: my-app
  version: 2.0.0
global:
  log_level: debug
`
	if err := os.WriteFile(filepath.Join(tmpDir, "anvil.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() returned error: %v", err)
	}

	v, _, _ := cfg.Get("project.name")
	if v != "my-app" {
		t.Errorf("config[project.name] = %v, want 'my-app'", v)
	}
	v, _, _ = cfg.Get("project.version")
	if v != "2.0.0" {
		t.Errorf("config[project.version] = %v, want '2.0.0'", v)
	}
	v, _, _ = cfg.Get("global.log_level")
	if v != "debug" {
		t.Errorf("config[global.log_level] = %v, want 'debug'", v)
	}
}

// TestLoadConfig_GlobalFileLoaded verifies that values from a global
// YAML configuration file are loaded when no project file exists.
func TestLoadConfig_GlobalFileLoaded(t *testing.T) {
	globalRoot := t.TempDir()
	anvilDir := filepath.Join(globalRoot, "anvil")
	if err := os.MkdirAll(anvilDir, 0755); err != nil {
		t.Fatal(err)
	}

	yamlContent := `
global:
  log_level: warn
  no_color: true
`
	if err := os.WriteFile(filepath.Join(anvilDir, "anvil.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("XDG_CONFIG_HOME", globalRoot)
	// Provide the required project.name via env var.
	t.Setenv("ANVIL_CFG_PROJECT_NAME", "global-test")

	// No project file — use an empty temp dir.
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() returned error: %v", err)
	}

	v, _, _ := cfg.Get("global.log_level")
	if v != "warn" {
		t.Errorf("config[global.log_level] = %v, want 'warn'", v)
	}
	v, _, _ = cfg.Get("global.no_color")
	if v != true {
		t.Errorf("config[global.no_color] = %v, want true", v)
	}
}

// TestLoadConfig_ProjectOverridesGlobal verifies that project file values
// override global file values when both exist.
//
// Covers AC: File-based values override compiled defaults (project > global).
func TestLoadConfig_ProjectOverridesGlobal(t *testing.T) {
	projectDir := t.TempDir()
	globalRoot := t.TempDir()

	// Create global config.
	anvilDir := filepath.Join(globalRoot, "anvil")
	if err := os.MkdirAll(anvilDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(anvilDir, "anvil.yaml"),
		[]byte("global:\n  log_level: warn\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create project config with different values.
	projectYaml := `
project:
  name: override-test
global:
  log_level: debug
`
	if err := os.WriteFile(filepath.Join(projectDir, "anvil.yaml"),
		[]byte(projectYaml), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("XDG_CONFIG_HOME", globalRoot)
	origDir, _ := os.Getwd()
	os.Chdir(projectDir)
	defer os.Chdir(origDir)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() returned error: %v", err)
	}

	// Project value should override global value.
	v, _, _ := cfg.Get("global.log_level")
	if v != "debug" {
		t.Errorf("config[global.log_level] = %v, want 'debug' (project overrides global)", v)
	}
}

// TestLoadConfig_ProjectPathSharingGlobalPrefix verifies that a project
// located in a sibling directory whose name shares the global config
// directory prefix (e.g. <config_home>/anvil-projects/... when the global
// dir is <config_home>/anvil) is classified as a project file, not a global
// file. The classification must respect path-component boundaries.
//
// Covers BUG-009: prefix-colliding project path is classified Project-level.
func TestLoadConfig_ProjectPathSharingGlobalPrefix(t *testing.T) {
	globalRoot := t.TempDir()

	// Project lives in a sibling directory that starts with the global dir
	// name "anvil" — a raw string prefix match would misclassify it.
	projectDir := filepath.Join(globalRoot, "anvil-projects", "demo")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}

	projectYaml := `
project:
  name: prefix-collision-app
  description: project value
global:
  log_level: debug
`
	if err := os.WriteFile(filepath.Join(projectDir, "anvil.yaml"), []byte(projectYaml), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("XDG_CONFIG_HOME", globalRoot)
	origDir, _ := os.Getwd()
	os.Chdir(projectDir)
	defer os.Chdir(origDir)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() returned error: %v", err)
	}

	// project.name has no schema default, so a Project-scope resolution
	// proves the file was loaded at Project level (a Global-level resolution
	// would mean the file was misclassified).
	v, scope, _ := cfg.Get("project.name")
	if v != "prefix-collision-app" {
		t.Errorf("config[project.name] = %v, want 'prefix-collision-app'", v)
	}
	if scope != ScopeProject {
		t.Errorf("config[project.name] scope = %v, want ScopeProject (BUG-009: prefix-colliding path misclassified)", scope)
	}

	v, scope, _ = cfg.Get("project.description")
	if v != "project value" {
		t.Errorf("config[project.description] = %v, want 'project value'", v)
	}
	if scope != ScopeProject {
		t.Errorf("config[project.description] scope = %v, want ScopeProject", scope)
	}

	// global.log_level has a compiled default ("info"); a Project-scope
	// resolution proves the project file value landed at Project level.
	v, scope, _ = cfg.Get("global.log_level")
	if v != "debug" {
		t.Errorf("config[global.log_level] = %v, want 'debug'", v)
	}
	if scope != ScopeProject {
		t.Errorf("config[global.log_level] scope = %v, want ScopeProject", scope)
	}
}

// TestLoadConfig_GlobalFileInsideDirClassifiedGlobal verifies that files
// actually located inside the global config directory are still classified
// as global configuration files.
//
// Covers BUG-009: files inside the global dir remain Global-level.
func TestLoadConfig_GlobalFileInsideDirClassifiedGlobal(t *testing.T) {
	globalRoot := t.TempDir()
	anvilDir := filepath.Join(globalRoot, "anvil")
	if err := os.MkdirAll(anvilDir, 0755); err != nil {
		t.Fatal(err)
	}

	yamlContent := `
global:
  log_level: warn
  no_color: true
`
	if err := os.WriteFile(filepath.Join(anvilDir, "anvil.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("XDG_CONFIG_HOME", globalRoot)
	// Provide the required project.name via env var.
	t.Setenv("ANVIL_CFG_PROJECT_NAME", "global-class-test")

	// No project file — use an empty temp dir.
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() returned error: %v", err)
	}

	// Values from the file inside the global dir must resolve at Global level.
	v, scope, _ := cfg.Get("global.log_level")
	if v != "warn" {
		t.Errorf("config[global.log_level] = %v, want 'warn'", v)
	}
	if scope != ScopeGlobal {
		t.Errorf("config[global.log_level] scope = %v, want ScopeGlobal", scope)
	}

	v, scope, _ = cfg.Get("global.no_color")
	if v != true {
		t.Errorf("config[global.no_color] = %v, want true", v)
	}
	if scope != ScopeGlobal {
		t.Errorf("config[global.no_color] scope = %v, want ScopeGlobal", scope)
	}
}

// TestLoadConfig_EnvVarOverridesFile verifies that environment variables
// override both project file values and compiled defaults.
//
// Covers AC: Environment variables override file-based values.
func TestLoadConfig_EnvVarOverridesFile(t *testing.T) {
	projectDir := t.TempDir()
	globalRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", globalRoot)

	// Create project config with project.name set.
	projectYaml := `
project:
  name: from-file
  version: 2.0.0
`
	if err := os.WriteFile(filepath.Join(projectDir, "anvil.yaml"), []byte(projectYaml), 0644); err != nil {
		t.Fatal(err)
	}

	// Set env var to override project.name.
	t.Setenv("ANVIL_CFG_PROJECT_NAME", "from-env")

	origDir, _ := os.Getwd()
	os.Chdir(projectDir)
	defer os.Chdir(origDir)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() returned error: %v", err)
	}

	// Env var value should override file value.
	v, _, _ := cfg.Get("project.name")
	if v != "from-env" {
		t.Errorf("config[project.name] = %v, want 'from-env' (env overrides file)", v)
	}
	// project.version should still be from file (not overridden by env).
	v, _, _ = cfg.Get("project.version")
	if v != "2.0.0" {
		t.Errorf("config[project.version] = %v, want '2.0.0' (file value preserved)", v)
	}
}

// TestLoadConfig_EnvVarMultipleKeys verifies that multiple environment
// variables are correctly loaded with the ANVIL_CFG_ prefix convention.
//
// Covers AC: Values from env vars loaded with ANVIL_CFG_ prefix.
func TestLoadConfig_EnvVarMultipleKeys(t *testing.T) {
	tmpDir := t.TempDir()
	globalRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", globalRoot)

	t.Setenv("ANVIL_CFG_PROJECT_NAME", "multi-env")
	t.Setenv("ANVIL_CFG_GLOBAL_LOG_LEVEL", "error")
	t.Setenv("ANVIL_CFG_RELEASE_MAX_RETAINED", "3")

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() returned error: %v", err)
	}

	v, _, _ := cfg.Get("project.name")
	if v != "multi-env" {
		t.Errorf("config[project.name] = %v, want 'multi-env'", v)
	}
	v, _, _ = cfg.Get("global.log_level")
	if v != "error" {
		t.Errorf("config[global.log_level] = %v, want 'error'", v)
	}
	v, _, _ = cfg.Get("release.max_retained")
	if v != 3 {
		t.Errorf("config[release.max_retained] = %v, want 3 (int)", v)
	}
}

// TestLoadConfig_FullPrecedenceChain verifies the complete precedence chain:
// defaults < global files < project files < environment variables.
//
// Covers AC: Precedence: env > files > defaults.
func TestLoadConfig_FullPrecedenceChain(t *testing.T) {
	projectDir := t.TempDir()
	globalRoot := t.TempDir()

	// Create global config.
	anvilDir := filepath.Join(globalRoot, "anvil")
	if err := os.MkdirAll(anvilDir, 0755); err != nil {
		t.Fatal(err)
	}
	globalYaml := `
global:
  log_level: warn
  output_format: json
project:
  name: global-project-name
release:
  max_retained: 10
`
	if err := os.WriteFile(filepath.Join(anvilDir, "anvil.yaml"), []byte(globalYaml), 0644); err != nil {
		t.Fatal(err)
	}

	// Create project config (overrides global for some keys).
	projectYaml := `
project:
  name: project-name
global:
  log_level: debug
release:
  max_retained: 20
`
	if err := os.WriteFile(filepath.Join(projectDir, "anvil.yaml"), []byte(projectYaml), 0644); err != nil {
		t.Fatal(err)
	}

	// Set env var (overrides project for max_retained only).
	t.Setenv("XDG_CONFIG_HOME", globalRoot)
	t.Setenv("ANVIL_CFG_RELEASE_MAX_RETAINED", "30")

	origDir, _ := os.Getwd()
	os.Chdir(projectDir)
	defer os.Chdir(origDir)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() returned error: %v", err)
	}

	// project.name: project overrides global, no env var → project value.
	v, _, _ := cfg.Get("project.name")
	if v != "project-name" {
		t.Errorf("config[project.name] = %v, want 'project-name' (project overrides global)", v)
	}

	// global.log_level: project overrides global, no env var → project value (debug).
	v, _, _ = cfg.Get("global.log_level")
	if v != "debug" {
		t.Errorf("config[global.log_level] = %v, want 'debug' (project overrides global)", v)
	}

	// global.output_format: only in global file, no project or env override.
	v, _, _ = cfg.Get("global.output_format")
	if v != "json" {
		t.Errorf("config[global.output_format] = %v, want 'json' (global file value)", v)
	}

	// release.max_retained: env var overrides project overrides global → env value (30).
	v, _, _ = cfg.Get("release.max_retained")
	if v != 30 {
		t.Errorf("config[release.max_retained] = %v, want 30 (env overrides project)", v)
	}

	// project.version: default from schema, no overrides → "1.0.0".
	v, _, _ = cfg.Get("project.version")
	if v != "1.0.0" {
		t.Errorf("config[project.version] = %v, want '1.0.0' (default)", v)
	}
}

// TestLoadConfig_CompletesWithin500ms verifies that the loader completes
// within the 500ms performance budget for a typical configuration.
//
// Covers AC: Completes within 500ms.
func TestLoadConfig_CompletesWithin500ms(t *testing.T) {
	tmpDir := t.TempDir()
	globalRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", globalRoot)

	yamlContent := `
project:
  name: perf-test
  version: 3.0.0
  description: Performance test project
global:
  log_level: info
  output_format: json
  auto_progress: false
release:
  max_retained: 10
  auto_verify: true
artifact:
  output: dist
  manifest: true
runtime:
  install_root: releases
  temp_dir: tmp
`
	if err := os.WriteFile(filepath.Join(tmpDir, "anvil.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	start := time.Now()
	cfg, err := LoadConfig()
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("LoadConfig() returned error: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadConfig() returned nil config")
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("LoadConfig() took %v, want ≤500ms", elapsed)
	}
}

// TestLoadConfig_ValidationFailure verifies that when loaded configuration
// fails validation (e.g., wrong value type), LoadConfig returns an error.
//
// Covers AC: Loaded values passed to ValidateConfig (validation failure case).
func TestLoadConfig_ValidationFailure(t *testing.T) {
	tmpDir := t.TempDir()
	globalRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", globalRoot)

	// Write a config with a type mismatch: release.max_retained should be
	// integer, but we provide a string.
	yamlContent := `
project:
  name: invalid-test
release:
  max_retained: not-an-integer
`
	if err := os.WriteFile(filepath.Join(tmpDir, "anvil.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	_, err := LoadConfig()
	if err == nil {
		t.Fatal("LoadConfig() should return error for invalid configuration (max_retained is string, want integer)")
	}
}

// TestLoadConfig_MissingRequiredKey verifies that when a required key
// (project.name) is missing and has no default, validation catches it.
//
// Covers AC: Loaded values passed to ValidateConfig (required key missing).
func TestLoadConfig_MissingRequiredKey(t *testing.T) {
	tmpDir := t.TempDir()
	globalRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", globalRoot)

	// No project.name in config.
	yamlContent := `project:
  version: 1.0.0
`
	if err := os.WriteFile(filepath.Join(tmpDir, "anvil.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	_, err := LoadConfig()
	if err == nil {
		t.Fatal("LoadConfig() should return error when required key 'project.name' is missing")
	}
}

// TestLoadConfig_EnvVarValidation verifies that env var values that fail
// type validation produce an error.
//
// Covers AC: Invalid env var values caught by validation.
func TestLoadConfig_EnvVarValidation(t *testing.T) {
	tmpDir := t.TempDir()
	globalRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", globalRoot)

	// Set a valid required value via env var.
	t.Setenv("ANVIL_CFG_PROJECT_NAME", "valid-app")

	// Set an invalid type for a boolean key (string that can't be coerced to bool).
	t.Setenv("ANVIL_CFG_RELEASE_AUTO_VERIFY", "not-a-boolean")

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	_, err := LoadConfig()
	if err == nil {
		t.Fatal("LoadConfig() should return error when env var has invalid type for boolean key")
	}
}

// TestLoadConfig_NoEnvVarsSet verifies that when no ANVIL_CFG_ environment
// variables are set, loading proceeds normally with file and default values.
//
// Covers AC: When no env var is set, resolution proceeds from files/defaults.
func TestLoadConfig_NoEnvVarsSet(t *testing.T) {
	tmpDir := t.TempDir()
	globalRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", globalRoot)

	// Set project.name (required) and other values via file.
	projectYaml := `
project:
  name: no-env-test
  version: 2.0.0
global:
  log_level: debug
`
	if err := os.WriteFile(filepath.Join(tmpDir, "anvil.yaml"), []byte(projectYaml), 0644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() returned error: %v", err)
	}
	v, _, _ := cfg.Get("project.name")
	if v != "no-env-test" {
		t.Errorf("config[project.name] = %v, want 'no-env-test'", v)
	}
	v, _, _ = cfg.Get("project.version")
	if v != "2.0.0" {
		t.Errorf("config[project.version] = %v, want '2.0.0'", v)
	}
	v, _, _ = cfg.Get("global.log_level")
	if v != "debug" {
		t.Errorf("config[global.log_level] = %v, want 'debug'", v)
	}
}

// TestLoadConfig_EnvVarKeyConvention verifies that the ANVIL_CFG_ prefix
// followed by underscore-separated category_key produces the correct
// dot-notation configuration key.
//
// Covers AC: The env var naming convention is correct.
func TestLoadConfig_EnvVarKeyConvention(t *testing.T) {
	tmpDir := t.TempDir()
	globalRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", globalRoot)

	// Set multiple env vars to verify key conversion.
	t.Setenv("ANVIL_CFG_PROJECT_NAME", "env-app")
	t.Setenv("ANVIL_CFG_GLOBAL_LOG_LEVEL", "debug")
	t.Setenv("ANVIL_CFG_RELEASE_MAX_RETAINED", "7")

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() returned error: %v", err)
	}

	v, _, _ := cfg.Get("project.name")
	if v != "env-app" {
		t.Errorf("config[project.name] = %v, want 'env-app'", v)
	}
	v, _, _ = cfg.Get("global.log_level")
	if v != "debug" {
		t.Errorf("config[global.log_level] = %v, want 'debug'", v)
	}
	v, _, _ = cfg.Get("release.max_retained")
	if v != 7 {
		t.Errorf("config[release.max_retained] = %v, want 7 (int)", v)
	}
}

// TestLoadConfig_EnvironmentFileLoaded verifies that when ANVIL_ENV is set
// and an environment configuration file exists, its values are loaded into
// the Environment level and override project-level values.
//
// Covers AC: Environment-level configuration files are loaded for the
// specified environment name (TS-P2-08).
func TestLoadConfig_EnvironmentFileLoaded(t *testing.T) {
	projectDir := t.TempDir()
	globalRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", globalRoot)
	t.Setenv("ANVIL_ENV", "production")

	// Create project config.
	projectYaml := `
project:
  name: env-override-test
release:
  max_retained: 5
runtime:
  log_level: info
`
	if err := os.WriteFile(filepath.Join(projectDir, "anvil.yaml"), []byte(projectYaml), 0644); err != nil {
		t.Fatal(err)
	}

	// Create environment config (overrides some project values).
	envDir := filepath.Join(projectDir, "config", "environments")
	if err := os.MkdirAll(envDir, 0755); err != nil {
		t.Fatal(err)
	}
	envYaml := `
release:
  max_retained: 10
runtime:
  log_level: debug
`
	if err := os.WriteFile(filepath.Join(envDir, "production.yaml"), []byte(envYaml), 0644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	os.Chdir(projectDir)
	defer os.Chdir(origDir)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() returned error: %v", err)
	}

	// release.max_retained should be from environment (10), not project (5).
	v, scope, _ := cfg.Get("release.max_retained")
	if v != 10 {
		t.Errorf("config[release.max_retained] = %v, want 10 (environment override)", v)
	}
	if scope != ScopeEnvironment {
		t.Errorf("config[release.max_retained] scope = %v, want ScopeEnvironment", scope)
	}

	// runtime.log_level should be from environment (debug), not project (info).
	v, scope, _ = cfg.Get("runtime.log_level")
	if v != "debug" {
		t.Errorf("config[runtime.log_level] = %v, want 'debug' (environment override)", v)
	}
	if scope != ScopeEnvironment {
		t.Errorf("config[runtime.log_level] scope = %v, want ScopeEnvironment", scope)
	}

	// project.name should still be from project (not overridden by env).
	v, scope, _ = cfg.Get("project.name")
	if v != "env-override-test" {
		t.Errorf("config[project.name] = %v, want 'env-override-test'", v)
	}
	if scope != ScopeProject {
		t.Errorf("config[project.name] scope = %v, want ScopeProject", scope)
	}
}

// TestLoadConfig_EnvironmentFileMissing verifies that when ANVIL_ENV is set
// but no environment file exists, loading succeeds and project-level values
// are used without error.
//
// Covers AC: Missing environment configuration does not cause errors
// (TS-P2-08), and project-level configuration is used without error (ST-P2-07).
func TestLoadConfig_EnvironmentFileMissing(t *testing.T) {
	projectDir := t.TempDir()
	globalRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", globalRoot)
	t.Setenv("ANVIL_ENV", "nonexistent-env")

	// Create project config with required values.
	projectYaml := `
project:
  name: missing-env-test
release:
  max_retained: 42
`
	if err := os.WriteFile(filepath.Join(projectDir, "anvil.yaml"), []byte(projectYaml), 0644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	os.Chdir(projectDir)
	defer os.Chdir(origDir)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() returned error when env file missing: %v", err)
	}

	// Values should resolve from project level (or global defaults).
	v, scope, _ := cfg.Get("release.max_retained")
	if v != 42 {
		t.Errorf("config[release.max_retained] = %v, want 42 (project value)", v)
	}
	if scope != ScopeProject {
		t.Errorf("config[release.max_retained] scope = %v, want ScopeProject", scope)
	}

	// Environment level should be empty.
	envMap := cfg.LevelMap(ScopeEnvironment)
	if len(envMap) != 0 {
		t.Errorf("Environment level should be empty, got %d entries", len(envMap))
	}
}

// TestLoadConfig_FullPrecedenceChainWithEnvironment verifies the complete
// precedence chain: defaults < global files < project files < environment
// files < environment variables.
//
// Covers AC: Full resolution engine evaluates all four levels (TS-P2-08).
func TestLoadConfig_FullPrecedenceChainWithEnvironment(t *testing.T) {
	projectDir := t.TempDir()
	globalRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", globalRoot)
	t.Setenv("ANVIL_ENV", "staging")

	// Create global config.
	anvilDir := filepath.Join(globalRoot, "anvil")
	if err := os.MkdirAll(anvilDir, 0755); err != nil {
		t.Fatal(err)
	}
	globalYaml := `
project:
  name: global-name
release:
  max_retained: 5
`
	if err := os.WriteFile(filepath.Join(anvilDir, "anvil.yaml"), []byte(globalYaml), 0644); err != nil {
		t.Fatal(err)
	}

	// Create project config (overrides global).
	projectYaml := `
project:
  name: project-name
release:
  max_retained: 10
`
	if err := os.WriteFile(filepath.Join(projectDir, "anvil.yaml"), []byte(projectYaml), 0644); err != nil {
		t.Fatal(err)
	}

	// Create environment config (overrides project).
	envDir := filepath.Join(projectDir, "config", "environments")
	if err := os.MkdirAll(envDir, 0755); err != nil {
		t.Fatal(err)
	}
	envYaml := `
release:
  max_retained: 20
`
	if err := os.WriteFile(filepath.Join(envDir, "staging.yaml"), []byte(envYaml), 0644); err != nil {
		t.Fatal(err)
	}

	// Set env var (overrides environment).
	t.Setenv("ANVIL_CFG_RELEASE_MAX_RETAINED", "30")

	origDir, _ := os.Getwd()
	os.Chdir(projectDir)
	defer os.Chdir(origDir)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() returned error: %v", err)
	}

	// project.name: project overrides global, no env/env override → project.
	v, scope, _ := cfg.Get("project.name")
	if v != "project-name" {
		t.Errorf("config[project.name] = %v, want 'project-name'", v)
	}
	if scope != ScopeProject {
		t.Errorf("config[project.name] scope = %v, want ScopeProject", scope)
	}

	// release.max_retained: env var overrides environment overrides project
	// overrides global → env var value (30).
	v, scope, _ = cfg.Get("release.max_retained")
	if v != 30 {
		t.Errorf("config[release.max_retained] = %v, want 30 (env var)", v)
	}
	if scope != ScopeExecution {
		t.Errorf("config[release.max_retained] scope = %v, want ScopeExecution", scope)
	}
}

// TestLoadConfig_EnvironmentLevelNotSet verifies that when ANVIL_ENV is not
// set, the Environment level is empty and values resolve from lower levels.
func TestLoadConfig_EnvironmentLevelNotSet(t *testing.T) {
	tmpDir := t.TempDir()
	globalRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", globalRoot)

	// Verify ANVIL_ENV is not set.
	t.Setenv("ANVIL_ENV", "")

	projectYaml := `
project:
  name: no-env-test
`
	if err := os.WriteFile(filepath.Join(tmpDir, "anvil.yaml"), []byte(projectYaml), 0644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() returned error: %v", err)
	}

	// Environment level should be empty.
	envMap := cfg.LevelMap(ScopeEnvironment)
	if len(envMap) != 0 {
		t.Errorf("Environment level should be empty when ANVIL_ENV is not set, got %d entries", len(envMap))
	}

	// Values should still resolve from project level.
	v, _, _ := cfg.Get("project.name")
	if v != "no-env-test" {
		t.Errorf("config[project.name] = %v, want 'no-env-test'", v)
	}
}

// TestResolveAndValidate_ValidConfig verifies that ResolveAndValidate
// returns no validation errors for a valid configuration.
//
// Reference: TS-012-001
func TestResolveAndValidate_ValidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	globalRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", globalRoot)

	yamlContent := `
project:
  name: resolve-validate-test
  version: 2.0.0
`
	if err := os.WriteFile(filepath.Join(tmpDir, "anvil.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	errs, err := ResolveAndValidate()
	if err != nil {
		t.Fatalf("ResolveAndValidate() returned error for valid configuration: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("ResolveAndValidate() returned %d validation errors for valid configuration: %v", len(errs), errs)
	}
}

// TestResolveAndValidate_InvalidConfig verifies that ResolveAndValidate
// returns the raw structured validation errors (not a formatted string)
// when the resolved configuration is invalid.
//
// Reference: TS-012-001
func TestResolveAndValidate_InvalidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	globalRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", globalRoot)

	// Type mismatch: release.max_retained should be integer, but we
	// provide a string.
	yamlContent := `
project:
  name: invalid-test
release:
  max_retained: not-an-integer
`
	if err := os.WriteFile(filepath.Join(tmpDir, "anvil.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	errs, err := ResolveAndValidate()
	if err != nil {
		t.Fatalf("ResolveAndValidate() returned resolution error, want structured validation errors: %v", err)
	}
	if len(errs) == 0 {
		t.Fatal("ResolveAndValidate() returned no validation errors for invalid configuration")
	}

	found := false
	for _, ve := range errs {
		if ve.Key == "release.max_retained" && ve.Expected == "integer" {
			found = true
		}
	}
	if !found {
		t.Errorf("validation errors should include structured error for release.max_retained (expected integer), got: %v", errs)
	}
}

// TestResolveAndValidate_MatchesLoadConfig verifies that implicit
// (LoadConfig) and explicit (ResolveAndValidate) validation never
// diverge: both must agree on the validity of the same configuration.
//
// Reference: TS-012-001 §5
func TestResolveAndValidate_MatchesLoadConfig(t *testing.T) {
	tests := []struct {
		name        string
		yamlContent string
	}{
		{
			name: "valid configuration",
			yamlContent: `
project:
  name: agreement-test
  version: 1.2.3
`,
		},
		{
			name: "invalid type",
			yamlContent: `
project:
  name: agreement-test
release:
  max_retained: not-an-integer
`,
		},
		{
			name: "missing required key",
			yamlContent: `
project:
  version: 1.2.3
`,
		},
		{
			name: "invalid allowed value",
			yamlContent: `
project:
  name: agreement-test
release:
  retention_policy: keep-all
`,
		},
		{
			name: "invalid format",
			yamlContent: `
project:
  name: agreement-test
  version: not-semver
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			globalRoot := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", globalRoot)

			if err := os.WriteFile(filepath.Join(tmpDir, "anvil.yaml"), []byte(tt.yamlContent), 0644); err != nil {
				t.Fatal(err)
			}

			origDir, _ := os.Getwd()
			os.Chdir(tmpDir)
			defer os.Chdir(origDir)

			_, loadErr := LoadConfig()
			errs, resolveErr := ResolveAndValidate()

			if (loadErr == nil) != (len(errs) == 0) {
				t.Errorf("LoadConfig and ResolveAndValidate disagree on validity: LoadConfig err=%v, ResolveAndValidate errs=%d (%v)",
					loadErr, len(errs), errs)
			}
			if resolveErr != nil {
				t.Errorf("ResolveAndValidate() returned resolution error for %q: %v", tt.name, resolveErr)
			}

			// When LoadConfig reports validation failures, its formatted
			// message contains exactly one line per error (the
			// "configuration validation failed:" prefix plus one newline
			// per formatted error), so the newline count must match the
			// number of structured errors returned by ResolveAndValidate.
			if loadErr != nil {
				want := strings.Count(loadErr.Error(), "\n")
				if want != len(errs) {
					t.Errorf("error count mismatch for %q: LoadConfig reported %d errors, ResolveAndValidate returned %d (%v)",
						tt.name, want, len(errs), errs)
				}
			}
		})
	}
}

// --- helpers ---

// toString converts an interface{} value to its string representation
// for comparison in tests.
func toString(v interface{}) string {
	if v == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%v", v)
}
