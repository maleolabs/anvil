// Tests for the configuration extension contract payloads (TS-P7-03).
package contracts

import (
	"strings"
	"testing"
)

// TestConfigKey_RoundTrip verifies that ConfigKey survives a JSON
// round-trip with all fields equal, for required and optional variants.
//
// Reference: TS-P7-03 AC-1
func TestConfigKey_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   ConfigKey
	}{
		{
			name: "required_key",
			in: ConfigKey{
				Name:        "framework.laravel.php_version",
				Description: "PHP version used to build the artifact",
				Required:    true,
			},
		},
		{
			name: "optional_key_with_default",
			in: ConfigKey{
				Name:        "framework.laravel.composer_flags",
				Description: "Extra flags passed to composer",
				Default:     "--no-dev",
			},
		},
		{
			name: "minimal_key",
			in: ConfigKey{
				Name: "framework.laravel.php_version",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			roundTrip(t, tt.in)
		})
	}
}

// TestConfigKey_JSONFieldNames verifies ConfigKey serializes to the
// expected JSON field names.
//
// Reference: TS-P7-03, DoD: automated tests verify the contract structure
func TestConfigKey_JSONFieldNames(t *testing.T) {
	in := ConfigKey{
		Name:        "framework.laravel.php_version",
		Description: "PHP version used to build the artifact",
		Default:     "8.3",
		Required:    true,
	}

	data := roundTrip(t, in)
	m := jsonKeys(t, data)
	for _, key := range []string{"name", "description", "default", "required"} {
		if _, ok := m[key]; !ok {
			t.Errorf("ConfigKey JSON missing key %q (got %v)", key, m)
		}
	}
}

// TestConfigKey_OmitsEmptyDefaultAndRequired verifies the omitempty
// behavior: an empty Default and a false Required are omitted from the
// serialized JSON.
//
// Reference: TS-P7-03, DoD: automated tests verify the contract structure
func TestConfigKey_OmitsEmptyDefaultAndRequired(t *testing.T) {
	in := ConfigKey{
		Name: "framework.laravel.php_version",
	}

	data := roundTrip(t, in)
	m := jsonKeys(t, data)
	for _, key := range []string{"default", "required"} {
		if _, ok := m[key]; ok {
			t.Errorf("empty field %q must be omitted (got %v)", key, m)
		}
	}
	if _, ok := m["name"]; !ok {
		t.Errorf("name must always be serialized (got %v)", m)
	}
}

// TestConfigExtension_RoundTrip verifies that ConfigExtension survives a
// JSON round-trip with all fields equal.
//
// Reference: TS-P7-03 AC-1
func TestConfigExtension_RoundTrip(t *testing.T) {
	in := ConfigExtension{
		Framework: "laravel",
		Keys: []ConfigKey{
			{
				Name:        "framework.laravel.php_version",
				Description: "PHP version used to build the artifact",
				Required:    true,
			},
			{
				Name:        "framework.laravel.composer_flags",
				Description: "Extra flags passed to composer",
				Default:     "--no-dev",
			},
		},
	}

	roundTrip(t, in)
}

// TestConfigExtension_JSONFieldNames verifies ConfigExtension serializes
// to the expected JSON field names and that Keys is always serialized.
//
// Reference: TS-P7-03, DoD: automated tests verify the contract structure
func TestConfigExtension_JSONFieldNames(t *testing.T) {
	in := ConfigExtension{
		Framework: "laravel",
		Keys: []ConfigKey{
			{Name: "framework.laravel.php_version"},
		},
	}

	data := roundTrip(t, in)
	m := jsonKeys(t, data)
	for _, key := range []string{"framework", "keys"} {
		if _, ok := m[key]; !ok {
			t.Errorf("ConfigExtension JSON missing key %q (got %v)", key, m)
		}
	}
}

// TestConfigExtensionRequest_RoundTrip verifies that ConfigExtensionRequest
// survives a JSON round-trip with all fields equal.
//
// Reference: TS-P7-03 AC-1
func TestConfigExtensionRequest_RoundTrip(t *testing.T) {
	in := ConfigExtensionRequest{Framework: "laravel"}

	roundTrip(t, in)
}

// TestConfigExtensionRequest_JSONFieldNames verifies ConfigExtensionRequest
// serializes to the expected JSON field names, with framework always
// serialized.
//
// Reference: TS-P7-03, DoD: automated tests verify the contract structure
func TestConfigExtensionRequest_JSONFieldNames(t *testing.T) {
	in := ConfigExtensionRequest{Framework: "laravel"}

	data := roundTrip(t, in)
	m := jsonKeys(t, data)
	if _, ok := m["framework"]; !ok {
		t.Errorf("ConfigExtensionRequest JSON missing key %q (got %v)", "framework", m)
	}
}

// TestConfigExtensionRequest_NoFrameworkSpecificContent verifies the
// serialized extension request contains no framework-specific content —
// the payload is generic and framework-agnostic. Note: the word
// "framework" itself is the contract's own selector field name, not
// framework-specific content, so it is not in the forbidden list.
//
// Reference: TS-P7-03 AC-1
func TestConfigExtensionRequest_NoFrameworkSpecificContent(t *testing.T) {
	in := ConfigExtensionRequest{Framework: "example"}

	data := roundTrip(t, in)
	serialized := string(data)

	for _, forbidden := range []string{
		"laravel", "php", "artisan", "composer", "npm",
		"flutter", "dart", "rails", "django",
	} {
		if strings.Contains(serialized, forbidden) {
			t.Errorf("serialized extension request contains framework-specific content %q: %s", forbidden, serialized)
		}
	}
}

// TestConfigExtensionResult_RoundTrip verifies that ConfigExtensionResult
// survives a JSON round-trip with all fields equal.
//
// Reference: TS-P7-03 AC-1
func TestConfigExtensionResult_RoundTrip(t *testing.T) {
	in := ConfigExtensionResult{
		Extension: ConfigExtension{
			Framework: "laravel",
			Keys: []ConfigKey{
				{
					Name:        "framework.laravel.php_version",
					Description: "PHP version used to build the artifact",
					Required:    true,
				},
			},
		},
	}

	roundTrip(t, in)
}

// TestConfigExtensionResult_JSONFieldNames verifies ConfigExtensionResult
// serializes to the expected JSON field names, with extension always
// serialized.
//
// Reference: TS-P7-03, DoD: automated tests verify the contract structure
func TestConfigExtensionResult_JSONFieldNames(t *testing.T) {
	in := ConfigExtensionResult{
		Extension: ConfigExtension{
			Framework: "laravel",
			Keys:      []ConfigKey{{Name: "framework.laravel.php_version"}},
		},
	}

	data := roundTrip(t, in)
	m := jsonKeys(t, data)
	if _, ok := m["extension"]; !ok {
		t.Errorf("ConfigExtensionResult JSON missing key %q (got %v)", "extension", m)
	}

	extension := jsonNested(t, data, "extension")
	for _, key := range []string{"framework", "keys"} {
		if _, ok := extension[key]; !ok {
			t.Errorf("ConfigExtension JSON missing key %q (got %v)", key, extension)
		}
	}
}

// TestConfigValue_RoundTrip verifies that ConfigValue survives a JSON
// round-trip with all fields equal.
//
// Reference: TS-P7-03 AC-4
func TestConfigValue_RoundTrip(t *testing.T) {
	in := ConfigValue{
		Key:   "framework.laravel.php_version",
		Value: "8.3",
	}

	roundTrip(t, in)
}

// TestConfigValue_JSONFieldNames verifies ConfigValue serializes to the
// expected JSON field names, with key and value always serialized.
//
// Reference: TS-P7-03, DoD: automated tests verify the contract structure
func TestConfigValue_JSONFieldNames(t *testing.T) {
	in := ConfigValue{
		Key:   "framework.laravel.php_version",
		Value: "8.3",
	}

	data := roundTrip(t, in)
	m := jsonKeys(t, data)
	for _, key := range []string{"key", "value"} {
		if _, ok := m[key]; !ok {
			t.Errorf("ConfigValue JSON missing key %q (got %v)", key, m)
		}
	}
}

// TestConfigValidationRequest_RoundTrip verifies that ConfigValidationRequest
// survives a JSON round-trip with all fields equal.
//
// Reference: TS-P7-03 AC-4
func TestConfigValidationRequest_RoundTrip(t *testing.T) {
	in := ConfigValidationRequest{
		Values: []ConfigValue{
			{Key: "framework.laravel.php_version", Value: "8.3"},
			{Key: "framework.laravel.composer_flags", Value: "--no-dev"},
		},
	}

	roundTrip(t, in)
}

// TestConfigValidationRequest_JSONFieldNames verifies ConfigValidationRequest
// serializes to the expected JSON field names, with values always
// serialized.
//
// Reference: TS-P7-03, DoD: automated tests verify the contract structure
func TestConfigValidationRequest_JSONFieldNames(t *testing.T) {
	in := ConfigValidationRequest{
		Values: []ConfigValue{{Key: "framework.laravel.php_version", Value: "8.3"}},
	}

	data := roundTrip(t, in)
	m := jsonKeys(t, data)
	if _, ok := m["values"]; !ok {
		t.Errorf("ConfigValidationRequest JSON missing key %q (got %v)", "values", m)
	}
}

// TestConfigValidationRequest_NoFrameworkSpecificContent verifies the
// serialized validation request contains no framework-specific content —
// the payload is generic and framework-agnostic. Note: the word
// "framework" itself is part of the fully-qualified key namespace of the
// contract, not framework-specific content, so it is not in the forbidden
// list.
//
// Reference: TS-P7-03 AC-4
func TestConfigValidationRequest_NoFrameworkSpecificContent(t *testing.T) {
	in := ConfigValidationRequest{
		Values: []ConfigValue{
			{Key: "example.timeout", Value: "60"},
			{Key: "example.workers", Value: "4"},
		},
	}

	data := roundTrip(t, in)
	serialized := string(data)

	for _, forbidden := range []string{
		"laravel", "php", "artisan", "composer", "npm",
		"flutter", "dart", "rails", "django",
	} {
		if strings.Contains(serialized, forbidden) {
			t.Errorf("serialized validation request contains framework-specific content %q: %s", forbidden, serialized)
		}
	}
}

// TestConfigValidationResult_RoundTrip verifies that ConfigValidationResult
// survives a JSON round-trip for valid and invalid variants.
//
// Reference: TS-P7-03 AC-4
func TestConfigValidationResult_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   ConfigValidationResult
	}{
		{
			name: "valid",
			in: ConfigValidationResult{
				Valid: true,
			},
		},
		{
			name: "invalid_with_errors",
			in: ConfigValidationResult{
				Valid:  false,
				Errors: []string{"framework.laravel.php_version: version 8 is not supported; expected 8.1 or newer"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			roundTrip(t, tt.in)
		})
	}
}

// TestConfigValidationResult_JSONFieldNames verifies ConfigValidationResult
// serializes to the expected JSON field names.
//
// Reference: TS-P7-03, DoD: automated tests verify the contract structure
func TestConfigValidationResult_JSONFieldNames(t *testing.T) {
	in := ConfigValidationResult{
		Valid:  false,
		Errors: []string{"framework.laravel.php_version: version 8 is not supported"},
	}

	data := roundTrip(t, in)
	m := jsonKeys(t, data)
	for _, key := range []string{"valid", "errors"} {
		if _, ok := m[key]; !ok {
			t.Errorf("ConfigValidationResult JSON missing key %q (got %v)", key, m)
		}
	}
}

// TestConfigValidationResult_ValidOmitsEmptyErrors verifies the omitempty
// behavior: a valid result omits the empty errors field, and valid is
// always serialized.
//
// Reference: TS-P7-03 AC-4
func TestConfigValidationResult_ValidOmitsEmptyErrors(t *testing.T) {
	in := ConfigValidationResult{Valid: true}

	data := roundTrip(t, in)
	m := jsonKeys(t, data)

	if _, ok := m["errors"]; ok {
		t.Errorf("valid result must omit empty errors field (got %v)", m)
	}
	if m["valid"] != true {
		t.Errorf("valid = %v, want true", m["valid"])
	}
}

// TestConfigValidationResult_InvalidCarriesErrors verifies that an invalid
// result serializes and round-trips its error details.
//
// Reference: TS-P7-03 AC-4
func TestConfigValidationResult_InvalidCarriesErrors(t *testing.T) {
	in := ConfigValidationResult{
		Valid:  false,
		Errors: []string{"framework.laravel.php_version: version 8 is not supported"},
	}

	data := roundTrip(t, in)
	m := jsonKeys(t, data)

	if m["valid"] != false {
		t.Errorf("valid = %v, want false", m["valid"])
	}
	if got, ok := m["errors"].([]any); !ok || len(got) != 1 || got[0] != in.Errors[0] {
		t.Errorf("errors = %v, want %v", m["errors"], in.Errors)
	}
}
