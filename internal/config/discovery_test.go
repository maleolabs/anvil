// Package config provides configuration discovery tests for Anvil projects.
//
// Reference: TS-P2-03, ADR-005 §7.2, §7.1
package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDiscoverConfigFiles_ProjectFileFound verifies that when an anvil.yaml
// file exists in the project root directory, it is discovered.
//
// Covers AC: Config files in project root are discovered.
func TestDiscoverConfigFiles_ProjectFileFound(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "anvil.yaml")
	if err := os.WriteFile(configPath, []byte("project:\n  name: test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	paths := DiscoverConfigFiles()
	if len(paths) == 0 {
		t.Fatal("DiscoverConfigFiles() returned no paths when project file exists")
	}
	if paths[0] != configPath {
		t.Errorf("DiscoverConfigFiles() = %v, want first path %s", paths, configPath)
	}
}

// TestDiscoverConfigFiles_ProjectYmlExtension verifies that an anvil.yml
// file (alternative extension) is also discovered in the project root.
//
// Covers AC: Config files in project root are discovered (.yml extension).
func TestDiscoverConfigFiles_ProjectYmlExtension(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "anvil.yml")
	if err := os.WriteFile(configPath, []byte("project:\n  name: test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	paths := DiscoverConfigFiles()
	if len(paths) == 0 {
		t.Fatal("DiscoverConfigFiles() returned no paths when project anvil.yml exists")
	}
	if paths[0] != configPath {
		t.Errorf("DiscoverConfigFiles() = %v, want first path %s", paths, configPath)
	}
}

// TestDiscoverConfigFiles_BothExtensionsInProject verifies that when both
// anvil.yaml and anvil.yml exist in the project root, both are discovered
// with .yaml first (the canonical extension).
//
// Covers AC: Config files in project root are discovered (both extensions).
func TestDiscoverConfigFiles_BothExtensionsInProject(t *testing.T) {
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "anvil.yaml")
	ymlPath := filepath.Join(tmpDir, "anvil.yml")
	if err := os.WriteFile(yamlPath, []byte("project:\n  name: yaml\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ymlPath, []byte("project:\n  name: yml\n"), 0644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	paths := DiscoverConfigFiles()
	if len(paths) < 2 {
		t.Fatalf("DiscoverConfigFiles() = %v, want at least 2 paths", paths)
	}
	// .yaml should come before .yml (canonical extension first).
	if filepath.Base(paths[0]) != "anvil.yaml" {
		t.Errorf("first path should be anvil.yaml, got %s", paths[0])
	}
	if filepath.Base(paths[1]) != "anvil.yml" {
		t.Errorf("second path should be anvil.yml, got %s", paths[1])
	}
}

// TestDiscoverConfigFiles_GlobalFileFound verifies that when a configuration
// file exists in the global config directory, it is discovered.
//
// Uses XDG_CONFIG_HOME to control the global config directory location.
//
// Covers AC: Config files in global directory are discovered.
func TestDiscoverConfigFiles_GlobalFileFound(t *testing.T) {
	// Create a temporary global config directory.
	globalRoot := t.TempDir()
	anvilDir := filepath.Join(globalRoot, "anvil")
	if err := os.MkdirAll(anvilDir, 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(anvilDir, "anvil.yaml")
	if err := os.WriteFile(configPath, []byte("global:\n  log_level: debug\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Set XDG_CONFIG_HOME to our temp dir so os.UserConfigDir() points there.
	t.Setenv("XDG_CONFIG_HOME", globalRoot)

	// Change to a temp directory without a project.
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	paths := DiscoverConfigFiles()
	if len(paths) == 0 {
		t.Fatal("DiscoverConfigFiles() returned no paths when global config exists")
	}
	if paths[0] != configPath {
		t.Errorf("DiscoverConfigFiles() = %v, want first path %s", paths, configPath)
	}
}

// TestDiscoverConfigFiles_ProjectBeforeGlobal verifies that project
// configuration files are discovered before global configuration files
// when both exist.
//
// Covers AC: Search order: project before global.
func TestDiscoverConfigFiles_ProjectBeforeGlobal(t *testing.T) {
	// Create project directory with anvil.yaml.
	projectDir := t.TempDir()
	projectConfig := filepath.Join(projectDir, "anvil.yaml")
	if err := os.WriteFile(projectConfig, []byte("project:\n  name: project\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create global config directory with anvil.yaml.
	globalRoot := t.TempDir()
	anvilDir := filepath.Join(globalRoot, "anvil")
	if err := os.MkdirAll(anvilDir, 0755); err != nil {
		t.Fatal(err)
	}
	globalConfig := filepath.Join(anvilDir, "anvil.yaml")
	if err := os.WriteFile(globalConfig, []byte("global:\n  log_level: warn\n"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("XDG_CONFIG_HOME", globalRoot)

	origDir, _ := os.Getwd()
	os.Chdir(projectDir)
	defer os.Chdir(origDir)

	paths := DiscoverConfigFiles()
	if len(paths) < 2 {
		t.Fatalf("DiscoverConfigFiles() = %v, want at least 2 paths (project + global)", paths)
	}
	// Project path should come first.
	if paths[0] != projectConfig {
		t.Errorf("project config should be first, got %s", paths[0])
	}
	if paths[1] != globalConfig {
		t.Errorf("global config should be second, got %s", paths[1])
	}
}

// TestDiscoverConfigFiles_NoFilesFound verifies that an empty slice (not an
// error) is returned when no configuration files exist in any location.
//
// Covers AC: Empty list returned when no config found (not error).
func TestDiscoverConfigFiles_NoFilesFound(t *testing.T) {
	// Create empty project directory (no anvil.yaml).
	tmpDir := t.TempDir()
	// Set global config to another empty temp dir.
	globalRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", globalRoot)

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	paths := DiscoverConfigFiles()
	if paths == nil {
		t.Fatal("DiscoverConfigFiles() returned nil, want empty slice")
	}
	if len(paths) != 0 {
		t.Errorf("DiscoverConfigFiles() = %v, want empty slice", paths)
	}
}

// TestDiscoverConfigFiles_CompletesWithin100ms verifies that discovery
// completes within the 100ms performance budget for typical configurations.
//
// Covers AC: Discovery completes within 100ms.
func TestDiscoverConfigFiles_CompletesWithin100ms(t *testing.T) {
	// Create a project with a config file.
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "anvil.yaml"), []byte("project:\n  name: perf\n"), 0644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	start := time.Now()
	paths := DiscoverConfigFiles()
	elapsed := time.Since(start)

	if elapsed > 100*time.Millisecond {
		t.Errorf("DiscoverConfigFiles() took %v, want ≤100ms", elapsed)
	}
	if len(paths) == 0 {
		t.Error("DiscoverConfigFiles() should have found the config file")
	}
}

// TestDiscoverConfigFiles_CompletesWithin100msNoFile verifies that discovery
// still completes within 100ms when no configuration file exists.
//
// Covers AC: Discovery completes within 100ms (no-config case).
func TestDiscoverConfigFiles_CompletesWithin100msNoFile(t *testing.T) {
	tmpDir := t.TempDir()
	globalRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", globalRoot)

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	start := time.Now()
	_ = DiscoverConfigFiles()
	elapsed := time.Since(start)

	if elapsed > 100*time.Millisecond {
		t.Errorf("DiscoverConfigFiles() took %v, want ≤100ms", elapsed)
	}
}
