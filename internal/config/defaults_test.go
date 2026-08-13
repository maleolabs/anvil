package config

import (
	"os"
	"path/filepath"
	"slices"
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

// TestNewProjectConfig_FrameworkAgnosticDefaults verifies that the compiled
// project config defaults are framework-agnostic (TS-015-01-03 DoD — no
// framework config defaults remain in the runtime): NewProjectConfig applies
// no framework-specific adjustments (the NewFrameworkProjectConfig switch is
// removed), and the schema registry carries no framework-named keys.
// Framework config keys and defaults come from the installed delivery
// lifecycle standard (TS-015-03-01), never from Core config knowledge
// (ADR-026 decision 1).
func TestNewProjectConfig_FrameworkAgnosticDefaults(t *testing.T) {
	cfg := NewProjectConfig("app")

	// The plain constructor stays framework-agnostic: no framework, no
	// artifact filter overrides. (The previous Laravel path dropped
	// "vendor/**" from the excludes; that knowledge is no longer Core-owned.)
	if cfg.Project.Framework != "" {
		t.Errorf("Framework = %q, want empty", cfg.Project.Framework)
	}
	if len(cfg.Artifact.Include) != 0 {
		t.Errorf("Include = %v, want empty", cfg.Artifact.Include)
	}
	if len(cfg.Artifact.Exclude) != 0 {
		t.Errorf("Exclude = %v, want empty (schema defaults apply at load time)", cfg.Artifact.Exclude)
	}

	// The schema's compiled artifact.exclude default must remain the generic,
	// framework-agnostic list — including "vendor/**". A framework-specific
	// adjustment here would be Core-owned framework knowledge.
	entry, ok := GetSchema().Entries["artifact.exclude"]
	if !ok {
		t.Fatal("GetSchema() has no artifact.exclude entry")
	}
	defaults, ok := entry.Default.([]string)
	if !ok {
		t.Fatalf("artifact.exclude default type = %T, want []string", entry.Default)
	}
	if !slices.Contains(defaults, "vendor/**") {
		t.Errorf("artifact.exclude compiled default = %v, must contain vendor/** (generic default untouched)", defaults)
	}

	// No framework-named schema keys may exist in the Core registry: every
	// key is a Core-owned generic contract key.
	for key := range GetSchema().Entries {
		if strings.HasPrefix(key, "laravel.") || strings.HasPrefix(key, "flutter.") {
			t.Errorf("schema contains framework-named key %q — framework config keys come from the installed standard (TS-015-03-01)", key)
		}
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

// TestProjectConfig_FrameworkSectionMarshals verifies the framework
// configuration extension section of the project config (TS-015-03-01):
// the standard-supplied defaults marshal as framework.<name>.<key> =
// default under the framework's own namespace (ADR-005 §4.4), and the
// written section round-trips through the YAML reader the loader uses
// (readProjectFrameworkVersion reads doc["framework"][framework]
// ["version"] — the same shape).
func TestProjectConfig_FrameworkSectionMarshals(t *testing.T) {
	cfg := NewProjectConfig("app")
	cfg.Framework = map[string]map[string]string{
		"laravel": {
			"version":     "11.0.0",
			"cache.store": "redis",
		},
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("yaml.Marshal() failed: %v", err)
	}
	raw := string(data)
	for _, want := range []string{
		"framework:",
		"laravel:",
		"version: 11.0.0",
		"cache.store: redis",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("marshaled YAML must contain %q, got:\n%s", want, raw)
		}
	}

	// Round-trip: the flat loader shape (framework.laravel.version) must
	// be reachable from the marshaled document.
	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	frameworks, ok := doc["framework"].(map[string]interface{})
	if !ok {
		t.Fatalf("framework section must be a map, got: %T", doc["framework"])
	}
	section, ok := frameworks["laravel"].(map[string]interface{})
	if !ok {
		t.Fatalf("framework.laravel section must be a map, got: %T", frameworks["laravel"])
	}
	if got := section["version"]; got != "11.0.0" {
		t.Errorf("framework.laravel.version = %v, want 11.0.0", got)
	}
}
