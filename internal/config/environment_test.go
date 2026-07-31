// Package config provides environment configuration tests for Anvil projects.
//
// Reference: TS-P2-08, ST-P2-07, ADR-005 §7.5
package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEnvironmentConfigDir_ReturnsExpectedPath verifies that
// EnvironmentConfigDir returns <root>/config/environments.
//
// Covers AC: The environment configuration file location is documented
// and predictable (ST-P2-07).
func TestEnvironmentConfigDir_ReturnsExpectedPath(t *testing.T) {
	root := "/tmp/test-project"
	expected := "/tmp/test-project/config/environments"

	got := EnvironmentConfigDir(root)
	if got != expected {
		t.Errorf("EnvironmentConfigDir(%q) = %q, want %q", root, got, expected)
	}
}

// TestLoadEnvironmentConfig_FileExists verifies that when a valid
// environment YAML file exists, it is loaded and flattened correctly.
//
// Covers AC: Creating an environment configuration file in the well-known
// location overrides project-level values (ST-P2-07).
func TestLoadEnvironmentConfig_FileExists(t *testing.T) {
	tmpDir := t.TempDir()
	envDir := filepath.Join(tmpDir, "config", "environments")
	if err := os.MkdirAll(envDir, 0755); err != nil {
		t.Fatal(err)
	}

	yamlContent := `
release:
  max_retained: 10
runtime:
  log_level: debug
`
	if err := os.WriteFile(filepath.Join(envDir, "production.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadEnvironmentConfig(tmpDir, "production")
	if err != nil {
		t.Fatalf("LoadEnvironmentConfig() returned error: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadEnvironmentConfig() returned nil map")
	}

	// Verify flattened keys.
	if v, ok := cfg["release.max_retained"]; !ok {
		t.Error("missing key 'release.max_retained'")
	} else if v != 10 {
		t.Errorf("release.max_retained = %v, want 10", v)
	}

	if v, ok := cfg["runtime.log_level"]; !ok {
		t.Error("missing key 'runtime.log_level'")
	} else if v != "debug" {
		t.Errorf("runtime.log_level = %v, want 'debug'", v)
	}
}

// TestLoadEnvironmentConfig_FileNotFound verifies that when no environment
// file exists, an empty map is returned without error.
//
// Covers AC: When no environment file exists for the specified environment,
// project-level configuration is used without error (ST-P2-07).
func TestLoadEnvironmentConfig_FileNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	cfg, err := LoadEnvironmentConfig(tmpDir, "nonexistent")
	if err != nil {
		t.Fatalf("LoadEnvironmentConfig() should not return error for missing file: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadEnvironmentConfig() returned nil map")
	}
	if len(cfg) != 0 {
		t.Errorf("LoadEnvironmentConfig() returned %d entries, want 0", len(cfg))
	}
}

// TestLoadEnvironmentConfig_PartialOverride verifies that an environment
// file with only a subset of keys overrides only those keys — unspecified
// keys are not present in the returned map (they fall through to lower
// levels during resolution).
//
// Covers AC: An environment file with only a subset of keys overrides only
// those keys — unspecified keys resolve from the project level (ST-P2-07).
func TestLoadEnvironmentConfig_PartialOverride(t *testing.T) {
	tmpDir := t.TempDir()
	envDir := filepath.Join(tmpDir, "config", "environments")
	if err := os.MkdirAll(envDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Only override release.max_retained, leave project.name unspecified.
	yamlContent := `release:
  max_retained: 20
`
	if err := os.WriteFile(filepath.Join(envDir, "staging.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadEnvironmentConfig(tmpDir, "staging")
	if err != nil {
		t.Fatalf("LoadEnvironmentConfig() returned error: %v", err)
	}

	// The override key should be present.
	if _, ok := cfg["release.max_retained"]; !ok {
		t.Error("missing key 'release.max_retained'")
	}

	// Keys not in the env file should NOT be in the env map.
	if _, ok := cfg["project.name"]; ok {
		t.Error("key 'project.name' should not be in env config (not specified in file)")
	}
}

// TestLoadEnvironmentConfig_MultipleEnvironments verifies that multiple
// environment files can coexist without interfering with each other.
//
// Covers AC: Multiple environment files can coexist without interfering
// with each other (ST-P2-07).
func TestLoadEnvironmentConfig_MultipleEnvironments(t *testing.T) {
	tmpDir := t.TempDir()
	envDir := filepath.Join(tmpDir, "config", "environments")
	if err := os.MkdirAll(envDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create two environment files.
	prodContent := `release:
  max_retained: 50
`
	stagingContent := `release:
  max_retained: 10
`
	if err := os.WriteFile(filepath.Join(envDir, "production.yaml"), []byte(prodContent), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(envDir, "staging.yaml"), []byte(stagingContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Load production — should have production values only.
	prodCfg, err := LoadEnvironmentConfig(tmpDir, "production")
	if err != nil {
		t.Fatalf("LoadEnvironmentConfig('production') returned error: %v", err)
	}
	if v := prodCfg["release.max_retained"]; v != 50 {
		t.Errorf("production release.max_retained = %v, want 50", v)
	}

	// Load staging — should have staging values only.
	stgCfg, err := LoadEnvironmentConfig(tmpDir, "staging")
	if err != nil {
		t.Fatalf("LoadEnvironmentConfig('staging') returned error: %v", err)
	}
	if v := stgCfg["release.max_retained"]; v != 10 {
		t.Errorf("staging release.max_retained = %v, want 10", v)
	}
}

// TestLoadEnvironmentConfig_InvalidYAML verifies that a malformed YAML
// file returns an error rather than silently returning an empty map.
func TestLoadEnvironmentConfig_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	envDir := filepath.Join(tmpDir, "config", "environments")
	if err := os.MkdirAll(envDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write invalid YAML content.
	if err := os.WriteFile(filepath.Join(envDir, "broken.yaml"), []byte(": invalid yaml :"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadEnvironmentConfig(tmpDir, "broken")
	if err == nil {
		t.Fatal("LoadEnvironmentConfig() should return error for invalid YAML")
	}
}

// TestLoadEnvironmentConfig_FileInProjectRoot verifies that environment
// config is correctly loaded when config/environments/ exists under the
// project root with the env YAML file.
func TestLoadEnvironmentConfig_FileInProjectRoot(t *testing.T) {
	tmpDir := t.TempDir()
	envDir := filepath.Join(tmpDir, "config", "environments")
	if err := os.MkdirAll(envDir, 0755); err != nil {
		t.Fatal(err)
	}

	yamlContent := `global:
  log_level: error
`
	if err := os.WriteFile(filepath.Join(envDir, "production.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadEnvironmentConfig(tmpDir, "production")
	if err != nil {
		t.Fatalf("LoadEnvironmentConfig() returned error: %v", err)
	}

	if v, ok := cfg["global.log_level"]; !ok {
		t.Error("missing key 'global.log_level'")
	} else if v != "error" {
		t.Errorf("global.log_level = %v, want 'error'", v)
	}
}
