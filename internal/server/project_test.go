// Package server provides models and utilities for managing Anvil Server
// Runtime configuration.
//
// Reference: TS-P5-11, TS-P5-12, ADR-013
package server

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// TestDefaultProjectRegistry verifies that DefaultProjectRegistry returns the
// expected compiled-in defaults.
func TestDefaultProjectRegistry(t *testing.T) {
	cfg := DefaultProjectRegistry()

	if cfg.Project.ID != "" {
		t.Errorf("DefaultProjectRegistry().Project.ID = %q, want empty string",
			cfg.Project.ID)
	}
	if cfg.Project.DisplayName != "" {
		t.Errorf("DefaultProjectRegistry().Project.DisplayName = %q, want empty string",
			cfg.Project.DisplayName)
	}
	if cfg.Project.InstallRoot != "" {
		t.Errorf("DefaultProjectRegistry().Project.InstallRoot = %q, want empty string",
			cfg.Project.InstallRoot)
	}
	if cfg.Project.Adapter != "" {
		t.Errorf("DefaultProjectRegistry().Project.Adapter = %q, want empty string",
			cfg.Project.Adapter)
	}
	if cfg.Project.Owner != "" {
		t.Errorf("DefaultProjectRegistry().Project.Owner = %q, want empty string",
			cfg.Project.Owner)
	}
	if cfg.Project.Group != "" {
		t.Errorf("DefaultProjectRegistry().Project.Group = %q, want empty string",
			cfg.Project.Group)
	}
}

// TestValidateProjectRegistry_Valid verifies that a valid project config
// passes validation without error.
func TestValidateProjectRegistry_Valid(t *testing.T) {
	cfg := ProjectRegistry{
		Project: ProjectSection{
			ID:          "my-project",
			DisplayName: "My Project",
			InstallRoot: "/var/www/my-project",
			Adapter:     "laravel",
			Owner:       "deploy",
			Group:       "www-data",
		},
	}

	if err := ValidateProjectRegistry(cfg); err != nil {
		t.Errorf("ValidateProjectRegistry(valid config) returned unexpected error: %v", err)
	}
}

// TestValidateProjectRegistry_MissingID verifies that a project config with
// an empty project.id returns an error.
func TestValidateProjectRegistry_MissingID(t *testing.T) {
	cfg := ProjectRegistry{
		Project: ProjectSection{
			ID:          "",
			InstallRoot: "/var/www/my-project",
		},
	}

	err := ValidateProjectRegistry(cfg)
	if err == nil {
		t.Error("ValidateProjectRegistry expected error for empty ID, got nil")
	}
	if err != ErrProjectIDRequired {
		t.Errorf("ValidateProjectRegistry returned %v, want ErrProjectIDRequired", err)
	}
}

// TestValidateProjectRegistry_MissingInstallRoot verifies that a project
// config with an empty project.install_root returns an error.
func TestValidateProjectRegistry_MissingInstallRoot(t *testing.T) {
	cfg := ProjectRegistry{
		Project: ProjectSection{
			ID:          "my-project",
			InstallRoot: "",
		},
	}

	err := ValidateProjectRegistry(cfg)
	if err == nil {
		t.Error("ValidateProjectRegistry expected error for empty InstallRoot, got nil")
	}
	if err != ErrInstallRootRequired {
		t.Errorf("ValidateProjectRegistry returned %v, want ErrInstallRootRequired", err)
	}
}

// TestValidateProjectRegistry_AllFields verifies that a project config with
// all fields set passes validation.
func TestValidateProjectRegistry_AllFields(t *testing.T) {
	cfg := ProjectRegistry{
		Project: ProjectSection{
			ID:          "full-project",
			DisplayName: "Full Project",
			InstallRoot: "/opt/projects/full",
			Adapter:     "node",
			Owner:       "admin",
			Group:       "admin",
		},
	}

	if err := ValidateProjectRegistry(cfg); err != nil {
		t.Errorf("ValidateProjectRegistry(all fields) returned unexpected error: %v", err)
	}
}

// TestProjectRegistry_ValidateMethod verifies the convenience Validate method
// on ProjectRegistry delegates correctly.
func TestProjectRegistry_ValidateMethod(t *testing.T) {
	valid := ProjectRegistry{
		Project: ProjectSection{
			ID:          "validate-test",
			InstallRoot: "/opt/test",
		},
	}
	if err := valid.Validate(); err != nil {
		t.Errorf("Validate() on valid project registry returned unexpected error: %v", err)
	}

	invalid := ProjectRegistry{
		Project: ProjectSection{
			ID:          "",
			InstallRoot: "",
		},
	}
	if err := invalid.Validate(); err == nil {
		t.Error("Validate() on invalid project registry should return error, got nil")
	}
}

// TestProjectRegistry_String verifies the String method produces a readable
// summary.
func TestProjectRegistry_String(t *testing.T) {
	cfg := ProjectRegistry{
		Project: ProjectSection{
			ID:          "str-test",
			DisplayName: "String Test",
			InstallRoot: "/opt/str-test",
			Adapter:     "laravel",
			Owner:       "dev",
			Group:       "dev",
		},
	}

	s := cfg.String()
	if s == "" {
		t.Error("String() returned empty string")
	}
}

// TestProjectRegistry_MarshalUnmarshal verifies that ProjectRegistry can be
// marshaled to YAML and unmarshaled back without data loss.
func TestProjectRegistry_MarshalUnmarshal(t *testing.T) {
	original := ProjectRegistry{
		Project: ProjectSection{
			ID:          "test-project",
			DisplayName: "Test Project",
			InstallRoot: "/var/www/test",
			Adapter:     "laravel",
			Owner:       "deploy",
			Group:       "www-data",
		},
	}

	data, err := yaml.Marshal(&original)
	if err != nil {
		t.Fatalf("yaml.Marshal returned unexpected error: %v", err)
	}

	var restored ProjectRegistry
	if err := yaml.Unmarshal(data, &restored); err != nil {
		t.Fatalf("yaml.Unmarshal returned unexpected error: %v", err)
	}

	if restored.Project.ID != original.Project.ID {
		t.Errorf("ID after round-trip = %q, want %q",
			restored.Project.ID, original.Project.ID)
	}
	if restored.Project.DisplayName != original.Project.DisplayName {
		t.Errorf("DisplayName after round-trip = %q, want %q",
			restored.Project.DisplayName, original.Project.DisplayName)
	}
	if restored.Project.InstallRoot != original.Project.InstallRoot {
		t.Errorf("InstallRoot after round-trip = %q, want %q",
			restored.Project.InstallRoot, original.Project.InstallRoot)
	}
	if restored.Project.Adapter != original.Project.Adapter {
		t.Errorf("Adapter after round-trip = %q, want %q",
			restored.Project.Adapter, original.Project.Adapter)
	}
	if restored.Project.Owner != original.Project.Owner {
		t.Errorf("Owner after round-trip = %q, want %q",
			restored.Project.Owner, original.Project.Owner)
	}
	if restored.Project.Group != original.Project.Group {
		t.Errorf("Group after round-trip = %q, want %q",
			restored.Project.Group, original.Project.Group)
	}
}

// TestProjectRegistry_MarshalUnmarshal_OmitEmpty verifies that marshaling
// a project config with empty optional fields omits them from YAML output.
func TestProjectRegistry_MarshalUnmarshal_OmitEmpty(t *testing.T) {
	cfg := ProjectRegistry{
		Project: ProjectSection{
			ID:          "minimal-project",
			InstallRoot: "/opt/minimal",
			// DisplayName, Adapter, Owner, Group omitted — should be
			// omitted from YAML output.
		},
	}

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		t.Fatalf("yaml.Marshal returned unexpected error: %v", err)
	}

	// Verify that optional fields are not present in the YAML output.
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatalf("yaml.Unmarshal to map returned unexpected error: %v", err)
	}

	projectSection, ok := raw["project"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'project' key in marshaled YAML")
	}

	if _, exists := projectSection["display_name"]; exists {
		t.Error("display_name should be omitted when empty, but was present in YAML output")
	}
	if _, exists := projectSection["adapter"]; exists {
		t.Error("adapter should be omitted when empty, but was present in YAML output")
	}
	if _, exists := projectSection["owner"]; exists {
		t.Error("owner should be omitted when empty, but was present in YAML output")
	}
	if _, exists := projectSection["group"]; exists {
		t.Error("group should be omitted when empty, but was present in YAML output")
	}
}
