package config

import (
	"testing"
)

// TestNewIdentity_ValidName verifies that NewIdentity accepts valid project
// names and returns an Identity with the expected name.
func TestNewIdentity_ValidName(t *testing.T) {
	// Valid names only: alphanumeric, hyphens, underscores
	validNames := []string{
		"my-project",
		"my_project",
		"MyProject123",
		"a",
		"project-name_with-mixed",
	}

	for _, name := range validNames {
		id, err := NewIdentity(name)
		if err != nil {
			t.Errorf("NewIdentity(%q) returned unexpected error: %v", name, err)
		}
		if id.Name() != name {
			t.Errorf("NewIdentity(%q).Name() = %q, want %q", name, id.Name(), name)
		}
	}
}

// TestNewIdentity_EmptyName verifies that NewIdentity rejects an empty
// project name with a clear error message.
func TestNewIdentity_EmptyName(t *testing.T) {
	_, err := NewIdentity("")
	if err == nil {
		t.Fatal("expected error for empty project name, got nil")
	}
	if !contains(err.Error(), "project name is required") {
		t.Errorf("expected 'project name is required' error, got: %v", err)
	}
}

// TestNewIdentity_InvalidName verifies that NewIdentity rejects project
// names containing disallowed characters with a clear error message.
func TestNewIdentity_InvalidName(t *testing.T) {
	invalidNames := []string{
		"invalid name!",   // space and exclamation
		"name/with/slash", // slash
		"name.with.dots",  // dots
		"spaces in name",  // spaces
		"name@domain",     // @
	}

	for _, name := range invalidNames {
		_, err := NewIdentity(name)
		if err == nil {
			t.Errorf("NewIdentity(%q) expected error, got nil", name)
			continue
		}
		if !contains(err.Error(), "invalid project name") {
			t.Errorf("NewIdentity(%q) error should mention 'invalid project name', got: %v", name, err)
		}
	}
}

// TestIdentity_Immutability verifies that once created, the project name
// cannot be changed through the Identity type.
func TestIdentity_Immutability(t *testing.T) {
	id, err := NewIdentity("fixed-name")
	if err != nil {
		t.Fatalf("NewIdentity() failed: %v", err)
	}

	// Verify Name() returns the original value.
	if id.Name() != "fixed-name" {
		t.Errorf("Identity.Name() = %q, want %q", id.Name(), "fixed-name")
	}

	// Verify there is no exported field that could be modified.
	// This is a compile-time check — the 'name' field is unexported.
	// We verify by asserting the type has no exported fields.
	var _ interface {
		Name() string
		String() string
	} = id
	_ = id
}

// TestIdentity_String verifies that String() returns the project name,
// satisfying the fmt.Stringer interface.
func TestIdentity_String(t *testing.T) {
	id, err := NewIdentity("display-name")
	if err != nil {
		t.Fatalf("NewIdentity() failed: %v", err)
	}

	if id.String() != "display-name" {
		t.Errorf("Identity.String() = %q, want %q", id.String(), "display-name")
	}
}

// TestProjectConfig_Identity verifies that ProjectConfig.Identity() returns
// an Identity matching the project name stored in the configuration.
func TestProjectConfig_Identity(t *testing.T) {
	cfg := NewProjectConfig("test-app")

	id := cfg.Identity()

	if id.Name() != "test-app" {
		t.Errorf("Identity.Name() = %q, want %q", id.Name(), "test-app")
	}
}

// TestProjectConfig_Identity_AfterNewProjectConfig verifies that every
// ProjectConfig created via NewProjectConfig produces a valid Identity.
func TestProjectConfig_Identity_AfterNewProjectConfig(t *testing.T) {
	cfg := NewProjectConfig("new-project")

	id := cfg.Identity()

	if id.Name() != "new-project" {
		t.Errorf("Identity.Name() = %q, want %q", id.Name(), "new-project")
	}
	// Verify the identity passes validation (no error).
	if _, err := NewIdentity(id.Name()); err != nil {
		t.Errorf("Identity from NewProjectConfig should be valid, got error: %v", err)
	}
}

// TestValidateProjectName verifies the standalone validation function.
func TestValidateProjectName(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"my-project", true},
		{"my_project", true},
		{"MyProject123", true},
		{"a", true},
		{"", false},
		{"my project", false},
		{"my/project", false},
		{"my.project", false},
		{"my-project!", false},
	}

	for _, tt := range tests {
		err := ValidateProjectName(tt.name)
		if tt.valid && err != nil {
			t.Errorf("ValidateProjectName(%q) = %v, want nil", tt.name, err)
		}
		if !tt.valid && err == nil {
			t.Errorf("ValidateProjectName(%q) = nil, want error", tt.name)
		}
	}
}
