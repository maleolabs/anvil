// Package server provides models and utilities for managing Anvil Server
// Runtime configuration.
//
// Reference: TS-P5-11, ST-P5-07, ADR-013
package server

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// TestDefaultServerConfig verifies that DefaultServerConfig returns the
// expected compiled-in defaults.
func TestDefaultServerConfig(t *testing.T) {
	cfg := DefaultServerConfig()

	if cfg.Runtime.SchemaVersion != 1 {
		t.Errorf("DefaultServerConfig().Runtime.SchemaVersion = %d, want 1",
			cfg.Runtime.SchemaVersion)
	}
	if cfg.Runtime.ID != "" {
		t.Errorf("DefaultServerConfig().Runtime.ID = %q, want empty string",
			cfg.Runtime.ID)
	}
	if cfg.Runtime.DisplayName != "" {
		t.Errorf("DefaultServerConfig().Runtime.DisplayName = %q, want empty string",
			cfg.Runtime.DisplayName)
	}
}

// TestValidateServerConfig_Valid verifies that a valid config passes
// validation without error.
func TestValidateServerConfig_Valid(t *testing.T) {
	cfg := ServerConfig{
		Runtime: RuntimeSection{
			SchemaVersion: 1,
			ID:            "my-server",
			DisplayName:   "Production Server",
		},
	}

	if err := ValidateServerConfig(cfg); err != nil {
		t.Errorf("ValidateServerConfig(valid config) returned unexpected error: %v", err)
	}
}

// TestValidateServerConfig_MissingSchemaVersion verifies that a config
// with schema_version != 1 returns an error.
func TestValidateServerConfig_MissingSchemaVersion(t *testing.T) {
	tests := []struct {
		name          string
		schemaVersion int
	}{
		{"zero value", 0},
		{"wrong version", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ServerConfig{
				Runtime: RuntimeSection{
					SchemaVersion: tt.schemaVersion,
					ID:            "my-server",
				},
			}

			err := ValidateServerConfig(cfg)
			if err == nil {
				t.Error("ValidateServerConfig expected error for schema_version != 1, got nil")
			}
			if err != ErrSchemaVersionRequired {
				t.Errorf("ValidateServerConfig returned %v, want ErrSchemaVersionRequired", err)
			}
		})
	}
}

// TestValidateServerConfig_MissingID verifies that a config with an empty
// runtime.id returns an error.
func TestValidateServerConfig_MissingID(t *testing.T) {
	cfg := ServerConfig{
		Runtime: RuntimeSection{
			SchemaVersion: 1,
			ID:            "",
		},
	}

	err := ValidateServerConfig(cfg)
	if err == nil {
		t.Error("ValidateServerConfig expected error for empty ID, got nil")
	}
	if err != ErrIDRequired {
		t.Errorf("ValidateServerConfig returned %v, want ErrIDRequired", err)
	}
}

// TestValidateServerConfig_EmptyID verifies that a config with an
// explicitly empty runtime.id returns an error.
func TestValidateServerConfig_EmptyID(t *testing.T) {
	cfg := ServerConfig{
		Runtime: RuntimeSection{
			SchemaVersion: 1,
			ID:            "",
			DisplayName:   "Test",
		},
	}

	err := ValidateServerConfig(cfg)
	if err == nil {
		t.Error("ValidateServerConfig expected error for empty ID, got nil")
	}
}

// TestValidateServerConfig_DisplayNameOptional verifies that display_name
// is not required and configs without it still pass validation.
func TestValidateServerConfig_DisplayNameOptional(t *testing.T) {
	cfg := ServerConfig{
		Runtime: RuntimeSection{
			SchemaVersion: 1,
			ID:            "my-server",
			// DisplayName omitted — should still be valid
		},
	}

	if err := ValidateServerConfig(cfg); err != nil {
		t.Errorf("ValidateServerConfig without display_name returned unexpected error: %v", err)
	}
}

// TestServerConfig_MarshalUnmarshal verifies that ServerConfig can be
// marshaled to YAML and unmarshaled back without data loss.
func TestServerConfig_MarshalUnmarshal(t *testing.T) {
	original := ServerConfig{
		Runtime: RuntimeSection{
			SchemaVersion: 1,
			ID:            "test-server",
			DisplayName:   "Test Server",
		},
	}

	data, err := yaml.Marshal(&original)
	if err != nil {
		t.Fatalf("yaml.Marshal returned unexpected error: %v", err)
	}

	var restored ServerConfig
	if err := yaml.Unmarshal(data, &restored); err != nil {
		t.Fatalf("yaml.Unmarshal returned unexpected error: %v", err)
	}

	if restored.Runtime.SchemaVersion != original.Runtime.SchemaVersion {
		t.Errorf("SchemaVersion after round-trip = %d, want %d",
			restored.Runtime.SchemaVersion, original.Runtime.SchemaVersion)
	}
	if restored.Runtime.ID != original.Runtime.ID {
		t.Errorf("ID after round-trip = %q, want %q",
			restored.Runtime.ID, original.Runtime.ID)
	}
	if restored.Runtime.DisplayName != original.Runtime.DisplayName {
		t.Errorf("DisplayName after round-trip = %q, want %q",
			restored.Runtime.DisplayName, original.Runtime.DisplayName)
	}
}

// TestServerConfig_MarshalUnmarshal_OmitEmpty verifies that marshaling
// a config with an empty DisplayName omits the field from YAML output.
func TestServerConfig_MarshalUnmarshal_OmitEmpty(t *testing.T) {
	cfg := ServerConfig{
		Runtime: RuntimeSection{
			SchemaVersion: 1,
			ID:            "minimal-server",
			DisplayName:   "",
		},
	}

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		t.Fatalf("yaml.Marshal returned unexpected error: %v", err)
	}

	// Verify that display_name is not present in the YAML output.
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatalf("yaml.Unmarshal to map returned unexpected error: %v", err)
	}

	runtimeSection, ok := raw["runtime"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'runtime' key in marshaled YAML")
	}

	if _, exists := runtimeSection["display_name"]; exists {
		t.Error("display_name should be omitted when empty, but was present in YAML output")
	}
}

// TestServerConfig_ValidateMethod verifies the convenience Validate method
// on ServerConfig delegates correctly.
func TestServerConfig_ValidateMethod(t *testing.T) {
	valid := ServerConfig{
		Runtime: RuntimeSection{
			SchemaVersion: 1,
			ID:            "validate-test",
		},
	}
	if err := valid.Validate(); err != nil {
		t.Errorf("Validate() on valid config returned unexpected error: %v", err)
	}

	invalid := ServerConfig{
		Runtime: RuntimeSection{
			SchemaVersion: 0,
			ID:            "",
		},
	}
	if err := invalid.Validate(); err == nil {
		t.Error("Validate() on invalid config should return error, got nil")
	}
}

// TestServerConfig_String verifies the String method produces a readable
// summary.
func TestServerConfig_String(t *testing.T) {
	cfg := ServerConfig{
		Runtime: RuntimeSection{
			SchemaVersion: 1,
			ID:            "str-test",
			DisplayName:   "String Test",
		},
	}

	s := cfg.String()
	if s == "" {
		t.Error("String() returned empty string")
	}
}
