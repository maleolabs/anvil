package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestWriteConfig_RoundTrip verifies that a ProjectConfig with an empty
// Description is written and read back correctly — Description must be ""
// (empty string, not nil) and Name/Version must match the original values.
func TestWriteConfig_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "anvil.yaml")

	cfg := ProjectConfig{
		Project: ProjectConfigSection{
			Name:        "test-app",
			Version:     "1.0.0",
			Description: "", // empty — the bug trigger
		},
	}

	if err := WriteConfig(cfg, path); err != nil {
		t.Fatalf("WriteConfig() returned unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading written config file: %v", err)
	}

	var decoded ProjectConfig
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("yaml.Unmarshal() failed: %v\nraw content:\n%s", err, string(data))
	}

	if decoded.Project.Name != "test-app" {
		t.Errorf("Name = %q, want %q", decoded.Project.Name, "test-app")
	}
	if decoded.Project.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", decoded.Project.Version, "1.0.0")
	}
	// This is the critical assertion: Description must be "" not nil.
	if decoded.Project.Description != "" {
		t.Errorf("Description = %q (len=%d), want empty string", decoded.Project.Description, len(decoded.Project.Description))
	}
}

// TestWriteConfig_NonEmptyDescription verifies that a non-empty description
// is serialized and deserialized correctly.
func TestWriteConfig_NonEmptyDescription(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "anvil.yaml")

	cfg := ProjectConfig{
		Project: ProjectConfigSection{
			Name:        "my-service",
			Version:     "2.0.0",
			Description: "A microservice for user management",
		},
	}

	if err := WriteConfig(cfg, path); err != nil {
		t.Fatalf("WriteConfig() returned unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading written config file: %v", err)
	}

	var decoded ProjectConfig
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("yaml.Unmarshal() failed: %v\nraw content:\n%s", err, string(data))
	}

	if decoded.Project.Name != "my-service" {
		t.Errorf("Name = %q, want %q", decoded.Project.Name, "my-service")
	}
	if decoded.Project.Version != "2.0.0" {
		t.Errorf("Version = %q, want %q", decoded.Project.Version, "2.0.0")
	}
	if decoded.Project.Description != "A microservice for user management" {
		t.Errorf("Description = %q, want %q", decoded.Project.Description, "A microservice for user management")
	}
}

// TestWriteConfig_FilePermissions verifies that the config file is created
// with the expected permissions (0644).
func TestWriteConfig_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "anvil.yaml")

	cfg := NewProjectConfig("perm-test")
	if err := WriteConfig(cfg, path); err != nil {
		t.Fatalf("WriteConfig() returned unexpected error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config file: %v", err)
	}

	// Verify file permissions are 0644 (owner rw, group r, other r).
	want := os.FileMode(0644)
	got := info.Mode().Perm()
	if got != want {
		t.Errorf("config file permissions = %o, want %o", got, want)
	}
}

// TestNewFrameworkProjectConfig_Laravel verifies that the "laravel" framework
// sets Project.Framework and adds "vendor/**" to Artifact.Include (TS-P7-29
// AC-4). The include pattern overrides the compiled exclude, keeping vendor/
// runtime-critical for Laravel; the compiled default exclude list is not
// modified.
func TestNewFrameworkProjectConfig_Laravel(t *testing.T) {
	cfg, err := NewFrameworkProjectConfig("app", "laravel")
	if err != nil {
		t.Fatalf("NewFrameworkProjectConfig() returned unexpected error: %v", err)
	}

	if cfg.Project.Name != "app" {
		t.Errorf("Name = %q, want %q", cfg.Project.Name, "app")
	}
	if cfg.Project.Framework != "laravel" {
		t.Errorf("Framework = %q, want %q", cfg.Project.Framework, "laravel")
	}
	if len(cfg.Artifact.Include) != 1 || cfg.Artifact.Include[0] != "vendor/**" {
		t.Errorf("Include = %v, want [vendor/**]", cfg.Artifact.Include)
	}
	if len(cfg.Artifact.Exclude) != 0 {
		t.Errorf("Exclude = %v, want empty", cfg.Artifact.Exclude)
	}
}

// TestNewFrameworkProjectConfig_EmptyFramework verifies that an empty
// framework returns the plain config (no framework, no include overrides)
// without an error.
func TestNewFrameworkProjectConfig_EmptyFramework(t *testing.T) {
	cfg, err := NewFrameworkProjectConfig("app", "")
	if err != nil {
		t.Fatalf("NewFrameworkProjectConfig() returned unexpected error: %v", err)
	}

	if cfg.Project.Framework != "" {
		t.Errorf("Framework = %q, want empty", cfg.Project.Framework)
	}
	if len(cfg.Artifact.Include) != 0 {
		t.Errorf("Include = %v, want empty", cfg.Artifact.Include)
	}
	if len(cfg.Artifact.Exclude) != 0 {
		t.Errorf("Exclude = %v, want empty", cfg.Artifact.Exclude)
	}
}

// TestNewFrameworkProjectConfig_FlutterNotSupported verifies that "flutter" —
// a known roadmap framework whose template is not available yet (TS-P7-27) —
// returns a clear "not yet supported" error rather than an "unknown
// framework" error.
func TestNewFrameworkProjectConfig_FlutterNotSupported(t *testing.T) {
	_, err := NewFrameworkProjectConfig("app", "flutter")
	if err == nil {
		t.Fatal("expected error for flutter framework, got nil")
	}
	if !strings.Contains(err.Error(), "not yet supported") {
		t.Errorf("error = %q, want it to mention 'not yet supported'", err.Error())
	}
}

// TestNewFrameworkProjectConfig_UnknownFramework verifies that an unsupported
// framework name produces an "unknown framework" error.
func TestNewFrameworkProjectConfig_UnknownFramework(t *testing.T) {
	_, err := NewFrameworkProjectConfig("app", "symfony")
	if err == nil {
		t.Fatal("expected error for symfony framework, got nil")
	}
	if !strings.Contains(err.Error(), "unknown framework") {
		t.Errorf("error = %q, want it to mention 'unknown framework'", err.Error())
	}
}

// TestNewProjectConfig_NoFrameworkKey verifies that NewProjectConfig remains
// unchanged (backward compat): Include/Exclude stay empty and the marshaled
// YAML must NOT contain a "framework:" key (omitempty).
func TestNewProjectConfig_NoFrameworkKey(t *testing.T) {
	cfg := NewProjectConfig("app")

	if cfg.Project.Framework != "" {
		t.Errorf("Framework = %q, want empty", cfg.Project.Framework)
	}
	if len(cfg.Artifact.Include) != 0 {
		t.Errorf("Include = %v, want empty", cfg.Artifact.Include)
	}
	if len(cfg.Artifact.Exclude) != 0 {
		t.Errorf("Exclude = %v, want empty", cfg.Artifact.Exclude)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("yaml.Marshal() failed: %v", err)
	}
	if strings.Contains(string(data), "framework:") {
		t.Errorf("marshaled YAML contains framework key, want it omitted:\n%s", string(data))
	}
}
