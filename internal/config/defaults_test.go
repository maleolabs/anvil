package config

import (
	"os"
	"path/filepath"
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
