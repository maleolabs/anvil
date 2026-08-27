// Package config provides configuration schema definitions for Anvil projects.
//
// Reference: TS-P2-01
package config

import (
	"testing"
)

// TestCoreSchema_HasVersion verifies the schema includes a version identifier.
func TestCoreSchema_HasVersion(t *testing.T) {
	s := CoreSchema()
	if s.Version == "" {
		t.Error("CoreSchema() Version must not be empty")
	}
	if s.Version != SchemaVersion {
		t.Errorf("CoreSchema() Version = %q, want %q", s.Version, SchemaVersion)
	}
}

// TestCoreSchema_HasAllDomainKeys verifies that keys from all five
// configuration domains are registered in the schema.
func TestCoreSchema_HasAllDomainKeys(t *testing.T) {
	s := CoreSchema()

	expectedDomains := []struct {
		name string
		keys []string
	}{
		{"project metadata (EPIC-001)", []string{
			"project.name",
			"project.version",
			"project.description",
		}},
		{"artifact packaging (EPIC-003)", []string{
			"artifact.include",
			"artifact.exclude",
			"artifact.output",
			"artifact.manifest",
		}},
		{"release lifecycle (EPIC-004)", []string{
			"release.max_retained",
			"release.retention_policy",
			"release.auto_verify",
			"release.version_schema",
		}},
		{"runtime paths (EPIC-005)", []string{
			"runtime.install_root",
			"runtime.shared_resources",
			"runtime.active_symlink",
			"runtime.temp_dir",
		}},
		{"global settings (EPIC-008)", []string{
			"global.log_level",
			"global.output_format",
			"global.no_color",
			"global.auto_progress",
		}},
	}

	for _, domain := range expectedDomains {
		t.Run(domain.name, func(t *testing.T) {
			for _, key := range domain.keys {
				entry, ok := s.Entries[key]
				if !ok {
					t.Errorf("missing key %q", key)
					continue
				}
				if entry.Key != key {
					t.Errorf("entry.Key = %q, want %q", entry.Key, key)
				}
			}
		})
	}
}

// TestCoreSchema_RequiredKeys verifies that keys marked as required
// are correctly identified.
func TestCoreSchema_RequiredKeys(t *testing.T) {
	s := CoreSchema()

	entry, ok := s.Entries["project.name"]
	if !ok {
		t.Fatal("missing required key 'project.name'")
	}
	if !entry.Required {
		t.Error("'project.name' must be marked as required")
	}

	// Verify optional keys are not marked required.
	optionalKeys := []string{
		"project.version",
		"artifact.include",
		"release.max_retained",
		"runtime.install_root",
		"global.log_level",
	}

	for _, key := range optionalKeys {
		entry, ok := s.Entries[key]
		if !ok {
			t.Errorf("missing optional key %q", key)
			continue
		}
		if entry.Required {
			t.Errorf("optional key %q must not be marked required", key)
		}
	}
}

// TestCoreSchema_KeyTypes verifies that every key has a valid type assigned.
func TestCoreSchema_KeyTypes(t *testing.T) {
	s := CoreSchema()

	for key, entry := range s.Entries {
		if entry.Type < TypeString || entry.Type > TypeObject {
			t.Errorf("key %q has invalid type %v", key, entry.Type)
		}
	}
}

// TestCoreSchema_KeyScopes verifies that every key has a scope level assigned.
func TestCoreSchema_KeyScopes(t *testing.T) {
	s := CoreSchema()

	for key, entry := range s.Entries {
		if entry.Scope < ScopeGlobal || entry.Scope > ScopeExecution {
			t.Errorf("key %q has invalid scope %v", key, entry.Scope)
		}
	}

	// Verify global scoped keys.
	globalKeys := []string{
		"global.log_level",
		"global.output_format",
		"global.no_color",
		"global.auto_progress",
	}
	for _, key := range globalKeys {
		entry, ok := s.Entries[key]
		if !ok {
			t.Errorf("missing global key %q", key)
			continue
		}
		if entry.Scope != ScopeGlobal {
			t.Errorf("key %q scope = %v, want ScopeGlobal", key, entry.Scope)
		}
	}

	// Verify project scoped keys.
	projectKeys := []string{
		"project.name",
		"artifact.include",
		"release.max_retained",
		"runtime.install_root",
	}
	for _, key := range projectKeys {
		entry, ok := s.Entries[key]
		if !ok {
			t.Errorf("missing project key %q", key)
			continue
		}
		if entry.Scope != ScopeProject {
			t.Errorf("key %q scope = %v, want ScopeProject", key, entry.Scope)
		}
	}
}

// TestCoreSchema_DefaultsConsistent verifies that keys with defaults
// have type-consistent default values.
func TestCoreSchema_DefaultsConsistent(t *testing.T) {
	s := CoreSchema()

	for key, entry := range s.Entries {
		if entry.Default == nil {
			continue // required-user-input key
		}
		switch entry.Type {
		case TypeString:
			_, ok := entry.Default.(string)
			if !ok {
				t.Errorf("key %q default is not string: %T (%v)", key, entry.Default, entry.Default)
			}
		case TypeInteger:
			switch entry.Default.(type) {
			case int, int64, float64:
				// acceptable integer representations
			default:
				t.Errorf("key %q default is not integer: %T (%v)", key, entry.Default, entry.Default)
			}
		case TypeBoolean:
			_, ok := entry.Default.(bool)
			if !ok {
				t.Errorf("key %q default is not bool: %T (%v)", key, entry.Default, entry.Default)
			}
		case TypeArray:
			_, ok := entry.Default.([]string)
			if !ok {
				t.Errorf("key %q default is not []string: %T (%v)", key, entry.Default, entry.Default)
			}
		}
	}
}

// TestCoreSchema_AllowedValues verifies that keys with allowed values
// include the default in the allowed set (if a default exists).
func TestCoreSchema_AllowedValues(t *testing.T) {
	s := CoreSchema()

	keysWithAllowedValues := []string{
		"release.retention_policy",
		"release.version_schema",
		"global.log_level",
		"global.output_format",
	}

	for _, key := range keysWithAllowedValues {
		entry, ok := s.Entries[key]
		if !ok {
			t.Errorf("missing key %q", key)
			continue
		}
		if len(entry.AllowedValues) == 0 {
			t.Errorf("key %q has empty AllowedValues", key)
			continue
		}
		if entry.Default != nil {
			defaultStr, ok := entry.Default.(string)
			if !ok {
				t.Errorf("key %q default is not string for AllowedValues check", key)
				continue
			}
			if !containsString(entry.AllowedValues, defaultStr) {
				t.Errorf("key %q default %q not in AllowedValues %v", key, defaultStr, entry.AllowedValues)
			}
		}
	}
}

// TestGetSchema_ReturnsCoreSchema verifies that GetSchema returns the
// same schema as CoreSchema.
func TestGetSchema_ReturnsCoreSchema(t *testing.T) {
	s1 := GetSchema()
	s2 := CoreSchema()

	if s1.Version != s2.Version {
		t.Errorf("GetSchema() Version = %q, CoreSchema() Version = %q", s1.Version, s2.Version)
	}
	if len(s1.Entries) != len(s2.Entries) {
		t.Errorf("GetSchema() has %d entries, CoreSchema() has %d", len(s1.Entries), len(s2.Entries))
	}
	for key := range s1.Entries {
		if _, ok := s2.Entries[key]; !ok {
			t.Errorf("GetSchema() has key %q not in CoreSchema()", key)
		}
	}
}

// TestValueType_String verifies that ValueType.String() returns expected
// human-readable names.
func TestValueType_String(t *testing.T) {
	tests := []struct {
		vt   ValueType
		want string
	}{
		{TypeString, "string"},
		{TypeInteger, "integer"},
		{TypeBoolean, "boolean"},
		{TypeArray, "array"},
		{TypeObject, "object"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.vt.String(); got != tt.want {
				t.Errorf("ValueType(%d).String() = %q, want %q", tt.vt, got, tt.want)
			}
		})
	}
}

// TestScopeLevel_String verifies that ScopeLevel.String() returns expected
// human-readable names.
func TestScopeLevel_String(t *testing.T) {
	tests := []struct {
		sl   ScopeLevel
		want string
	}{
		{ScopeGlobal, "global"},
		{ScopeProject, "project"},
		{ScopeEnvironment, "environment"},
		{ScopeExecution, "execution"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.sl.String(); got != tt.want {
				t.Errorf("ScopeLevel(%d).String() = %q, want %q", tt.sl, got, tt.want)
			}
		})
	}
}

// TestSchemaEntry_KeyNonEmpty verifies that every schema entry has a non-empty Key.
func TestSchemaEntry_KeyNonEmpty(t *testing.T) {
	s := CoreSchema()
	for key, entry := range s.Entries {
		if entry.Key == "" {
			t.Errorf("entry for key %q has empty Key field", key)
		}
		if entry.Key != key {
			t.Errorf("entry for key %q has mismatched Key field %q", key, entry.Key)
		}
	}
}

// --- helpers ---

func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
