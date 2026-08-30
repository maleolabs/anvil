package config

import (
	"testing"
)

// TestNewMetadata_Accessors verifies that NewMetadata creates a Metadata value
// and that the accessors return the expected name and version.
func TestNewMetadata_Accessors(t *testing.T) {
	m := NewMetadata("my-app", "2.0.0")

	if m.Name() != "my-app" {
		t.Errorf("Metadata.Name() = %q, want %q", m.Name(), "my-app")
	}
	if m.Version() != "2.0.0" {
		t.Errorf("Metadata.Version() = %q, want %q", m.Version(), "2.0.0")
	}
}

// TestNewMetadata_EmptyFields verifies that Metadata accepts empty strings
// for name and version without error — validation is the config system's job.
func TestNewMetadata_EmptyFields(t *testing.T) {
	m := NewMetadata("", "")

	if m.Name() != "" {
		t.Errorf("Metadata.Name() with empty name = %q, want empty string", m.Name())
	}
	if m.Version() != "" {
		t.Errorf("Metadata.Version() with empty version = %q, want empty string", m.Version())
	}
}

// TestMetadata_Immutability verifies that once created, the metadata values
// cannot be changed through the Metadata type.
func TestMetadata_Immutability(t *testing.T) {
	m := NewMetadata("fixed-app", "1.5.0")

	// Verify accessors return the original values.
	if m.Name() != "fixed-app" {
		t.Errorf("Metadata.Name() = %q, want %q", m.Name(), "fixed-app")
	}
	if m.Version() != "1.5.0" {
		t.Errorf("Metadata.Version() = %q, want %q", m.Version(), "1.5.0")
	}

	// Compile-time check: the type has no exported fields, only accessors.
	var _ interface {
		Name() string
		Version() string
		String() string
	} = m
	_ = m
}

// TestMetadata_String verifies that String() returns the expected
// human-readable representation "name vversion".
func TestMetadata_String(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{"my-app", "2.0.0", "my-app v2.0.0"},
		{"a", "1.0.0", "a v1.0.0"},
		{"project-name", "0.0.1", "project-name v0.0.1"},
	}

	for _, tt := range tests {
		m := NewMetadata(tt.name, tt.version)
		got := m.String()
		if got != tt.want {
			t.Errorf("Metadata(%q, %q).String() = %q, want %q", tt.name, tt.version, got, tt.want)
		}
	}
}

// TestProjectConfig_Metadata verifies that ProjectConfig.Metadata() returns
// a Metadata value matching the project name and version stored in the config.
func TestProjectConfig_Metadata(t *testing.T) {
	cfg := NewProjectConfig("test-app")

	m := cfg.Metadata()

	if m.Name() != "test-app" {
		t.Errorf("Metadata.Name() = %q, want %q", m.Name(), "test-app")
	}
	if m.Version() != DefaultVersion {
		t.Errorf("Metadata.Version() = %q, want %q", m.Version(), DefaultVersion)
	}
}

// TestProjectConfig_Metadata_WithCustomVersion verifies that Metadata returns
// the custom version when set on ProjectConfigSection.
func TestProjectConfig_Metadata_WithCustomVersion(t *testing.T) {
	cfg := ProjectConfig{
		Project: ProjectConfigSection{
			Name:    "custom-version-app",
			Version: "3.0.0",
		},
	}

	m := cfg.Metadata()

	if m.Name() != "custom-version-app" {
		t.Errorf("Metadata.Name() = %q, want %q", m.Name(), "custom-version-app")
	}
	if m.Version() != "3.0.0" {
		t.Errorf("Metadata.Version() = %q, want %q", m.Version(), "3.0.0")
	}
}

// TestProvisionConfig_ProjectMetadata verifies that ProjectMetadata() returns
// the correct Metadata when both project.name and project.version are present
// in the resolved configuration.
func TestProvisionConfig_ProjectMetadata(t *testing.T) {
	resolver := NewResolver(
		map[string]interface{}{"project.name": "resolved-app"},
		map[string]interface{}{"project.version": "4.0.0"},
		nil,
		nil,
	)
	pc := NewProvisionConfig(resolver)

	m, err := pc.ProjectMetadata()
	if err != nil {
		t.Fatalf("ProjectMetadata() returned unexpected error: %v", err)
	}

	if m.Name() != "resolved-app" {
		t.Errorf("Metadata.Name() = %q, want %q", m.Name(), "resolved-app")
	}
	if m.Version() != "4.0.0" {
		t.Errorf("Metadata.Version() = %q, want %q", m.Version(), "4.0.0")
	}
}

// TestProvisionConfig_ProjectMetadata_DefaultVersion verifies that
// ProjectMetadata() works with the default version "1.0.0" when version
// is resolved from the global defaults (as LoadConfig would provide).
func TestProvisionConfig_ProjectMetadata_DefaultVersion(t *testing.T) {
	resolver := NewResolver(
		map[string]interface{}{
			"project.name":    "default-version-app",
			"project.version": "1.0.0",
		},
		nil, nil, nil,
	)
	pc := NewProvisionConfig(resolver)

	m, err := pc.ProjectMetadata()
	if err != nil {
		t.Fatalf("ProjectMetadata() returned unexpected error: %v", err)
	}

	if m.Name() != "default-version-app" {
		t.Errorf("Metadata.Name() = %q, want %q", m.Name(), "default-version-app")
	}
	if m.Version() != "1.0.0" {
		t.Errorf("Metadata.Version() = %q, want %q", m.Version(), "1.0.0")
	}
}

// TestProvisionConfig_ProjectMetadata_MissingName verifies that
// ProjectMetadata() returns an error when project.name is missing from
// the resolved configuration.
func TestProvisionConfig_ProjectMetadata_MissingName(t *testing.T) {
	resolver := NewResolver(
		map[string]interface{}{"project.version": "1.0.0"},
		nil, nil, nil,
	)
	pc := NewProvisionConfig(resolver)

	_, err := pc.ProjectMetadata()
	if err == nil {
		t.Fatal("ProjectMetadata() expected error for missing project.name, got nil")
	}
}

// TestProvisionConfig_ProjectMetadata_MissingVersion verifies that
// ProjectMetadata() returns an error when project.version is missing from
// the resolved configuration.
func TestProvisionConfig_ProjectMetadata_MissingVersion(t *testing.T) {
	resolver := NewResolver(
		map[string]interface{}{"project.name": "no-version-app"},
		nil, nil, nil,
	)
	pc := NewProvisionConfig(resolver)

	_, err := pc.ProjectMetadata()
	if err == nil {
		t.Fatal("ProjectMetadata() expected error for missing project.version, got nil")
	}
}

// TestProjectConfig_Metadata_MatchesIdentity verifies that the name returned
// by Metadata() matches the name returned by Identity() for the same config.
func TestProjectConfig_Metadata_MatchesIdentity(t *testing.T) {
	cfg := NewProjectConfig("my-unified-app")

	meta := cfg.Metadata()
	id := cfg.Identity()

	if meta.Name() != id.Name() {
		t.Errorf("Metadata.Name() = %q, Identity.Name() = %q, want them to match", meta.Name(), id.Name())
	}
}

// TestProvisionConfig_ProjectMetadata_WrongType verifies that ProjectMetadata()
// returns an error when a key has an unexpected type (e.g. int instead of string).
func TestProvisionConfig_ProjectMetadata_WrongType(t *testing.T) {
	resolver := NewResolver(
		map[string]interface{}{"project.name": 42},
		nil, nil, nil,
	)
	pc := NewProvisionConfig(resolver)

	_, err := pc.ProjectMetadata()
	if err == nil {
		t.Error("ProjectMetadata() expected error for non-string project.name, got nil")
	}
}

// TestProvisionConfig_ProjectMetadata_String verifies that the Metadata
// returned from ProjectMetadata() produces the correct String() output.
func TestProvisionConfig_ProjectMetadata_String(t *testing.T) {
	resolver := NewResolver(
		map[string]interface{}{"project.name": "string-test"},
		map[string]interface{}{"project.version": "1.2.3"},
		nil, nil,
	)
	pc := NewProvisionConfig(resolver)

	m, err := pc.ProjectMetadata()
	if err != nil {
		t.Fatalf("ProjectMetadata() returned unexpected error: %v", err)
	}

	want := "string-test v1.2.3"
	if got := m.String(); got != want {
		t.Errorf("Metadata.String() = %q, want %q", got, want)
	}
}
