// Package config provides configuration schema validation for Anvil projects.
//
// Reference: TS-P2-02
package config

import (
	"testing"
)

// testSchema returns a minimal schema for use in validation tests.
func testSchema() Schema {
	return Schema{
		Version: "1.0.0",
		Entries: map[string]SchemaEntry{
			"project.name": {
				Key:      "project.name",
				Type:     TypeString,
				Required: true,
			},
			"project.version": {
				Key:     "project.version",
				Type:    TypeString,
				Default: "1.0.0",
			},
			"release.max_retained": {
				Key:  "release.max_retained",
				Type: TypeInteger,
			},
			"release.auto_verify": {
				Key:  "release.auto_verify",
				Type: TypeBoolean,
			},
			"artifact.include": {
				Key:  "artifact.include",
				Type: TypeArray,
			},
			"global.log_level": {
				Key:           "global.log_level",
				Type:          TypeString,
				Default:       "info",
				AllowedValues: []string{"debug", "info", "warn", "error"},
			},
			"global.no_color": {
				Key:  "global.no_color",
				Type: TypeBoolean,
			},
		},
	}
}

// TestValidate_ValidConfig verifies that configuration matching the schema
// passes validation without errors.
func TestValidate_ValidConfig(t *testing.T) {
	s := testSchema()
	config := map[string]interface{}{
		"project.name":         "my-app",
		"project.version":      "2.0.0",
		"release.max_retained": 10,
		"release.auto_verify":  true,
		"artifact.include":     []interface{}{"**/*.go"},
		"global.log_level":     "debug",
		"global.no_color":      false,
	}

	errs := Validate(s, config)
	if len(errs) != 0 {
		t.Errorf("Validate() returned %d errors for valid config: %v", len(errs), errs)
	}
}

// TestValidate_MissingRequiredKey verifies that a missing required key
// produces an error identifying the key.
func TestValidate_MissingRequiredKey(t *testing.T) {
	s := testSchema()
	config := map[string]interface{}{
		// "project.name" is intentionally omitted (required)
		"project.version": "1.0.0",
	}

	errs := Validate(s, config)
	if len(errs) == 0 {
		t.Fatal("Validate() returned no errors for missing required key")
	}

	found := false
	for _, err := range errs {
		if err.Key == "project.name" {
			found = true
			if err.Actual != nil {
				t.Errorf("missing key error should have nil Actual, got %v", err.Actual)
			}
			break
		}
	}
	if !found {
		t.Errorf("Validate() errors do not include 'project.name': %v", errs)
	}
}

// TestValidate_WrongType verifies that an incorrect value type produces
// an error identifying the key, expected type, and actual value.
func TestValidate_WrongType(t *testing.T) {
	s := testSchema()

	tests := []struct {
		name  string
		key   string
		value interface{}
	}{
		{"string instead of integer", "release.max_retained", "five"},
		{"integer instead of string", "project.name", 42},
		{"string instead of boolean", "release.auto_verify", "yes"},
		{"integer instead of boolean", "global.no_color", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := map[string]interface{}{
				"project.name":    "my-app",
				"project.version": "1.0.0",
				tt.key:            tt.value,
			}

			errs := Validate(s, config)
			found := false
			for _, err := range errs {
				if err.Key == tt.key {
					found = true
					if err.Expected == "" {
						t.Error("validation error should include expected type")
					}
					break
				}
			}
			if !found {
				t.Errorf("Validate() errors do not include key %q: %v", tt.key, errs)
			}
		})
	}
}

// TestValidate_AllowedValues verifies that a value outside the allowed set
// produces an error.
func TestValidate_AllowedValues(t *testing.T) {
	s := testSchema()
	config := map[string]interface{}{
		"project.name":     "my-app",
		"global.log_level": "verbose", // not in allowed set
	}

	errs := Validate(s, config)
	found := false
	for _, err := range errs {
		if err.Key == "global.log_level" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Validate() errors do not include 'global.log_level' with invalid value: %v", errs)
	}
}

// TestValidate_AllowedValueInSet verifies that a value within the allowed set
// passes validation.
func TestValidate_AllowedValueInSet(t *testing.T) {
	s := testSchema()

	validValues := []string{"debug", "info", "warn", "error"}
	for _, val := range validValues {
		t.Run(val, func(t *testing.T) {
			config := map[string]interface{}{
				"project.name":     "my-app",
				"global.log_level": val,
			}
			errs := Validate(s, config)
			for _, err := range errs {
				if err.Key == "global.log_level" {
					t.Errorf("unexpected error for allowed value %q: %v", val, err)
				}
			}
		})
	}
}

// TestValidate_CollectsAllErrors verifies that all validation errors are
// collected and returned together (non-fail-fast behavior).
func TestValidate_CollectsAllErrors(t *testing.T) {
	s := testSchema()
	config := map[string]interface{}{
		// "project.name" is missing (required)
		// "project.version" is missing (optional, no error expected)
		"release.max_retained": "not-a-number", // wrong type
		"global.log_level":     "invalid",      // not in allowed set
	}

	errs := Validate(s, config)
	if len(errs) < 2 {
		t.Errorf("Validate() returned %d errors, expected at least 2 (missing required + wrong type + invalid value)", len(errs))
	}

	// Verify multiple distinct errors are present.
	keys := make(map[string]bool)
	for _, err := range errs {
		keys[err.Key] = true
	}

	if !keys["project.name"] {
		t.Error("expected error for 'project.name' (missing required)")
	}
	if !keys["release.max_retained"] {
		t.Error("expected error for 'release.max_retained' (wrong type)")
	}
	if !keys["global.log_level"] {
		t.Error("expected error for 'global.log_level' (invalid allowed value)")
	}
}

// TestValidate_NoModification verifies that the validation engine does not
// modify the configuration values it validates.
func TestValidate_NoModification(t *testing.T) {
	s := testSchema()
	config := map[string]interface{}{
		"project.name":    "my-app",
		"project.version": "1.0.0",
	}

	originalVersion := config["project.version"]
	_ = Validate(s, config)
	if config["project.version"] != originalVersion {
		t.Errorf("Validate() modified config value: was %v, now %v", originalVersion, config["project.version"])
	}
}

// TestValidate_EmptyConfig verifies that an empty config produces errors
// for required keys but not for optional keys.
func TestValidate_EmptyConfig(t *testing.T) {
	s := testSchema()
	config := map[string]interface{}{}

	errs := Validate(s, config)
	if len(errs) == 0 {
		t.Fatal("Validate() returned no errors for empty config with required keys")
	}

	// Should only error on required keys.
	for _, err := range errs {
		entry, ok := s.Entries[err.Key]
		if !ok {
			t.Errorf("error references unknown key %q", err.Key)
			continue
		}
		if !entry.Required {
			t.Errorf("error for optional key %q should not appear for empty config", err.Key)
		}
	}
}

// TestValidate_OptionalKeyOmitted verifies that omitting an optional key
// does not produce an error.
func TestValidate_OptionalKeyOmitted(t *testing.T) {
	s := testSchema()
	config := map[string]interface{}{
		"project.name": "my-app",
		// "project.version" is optional, omitted here
	}

	errs := Validate(s, config)
	for _, err := range errs {
		if err.Key == "project.version" {
			t.Error("optional key 'project.version' should not produce an error when omitted")
		}
	}
}

// TestValidate_IntegerAcceptableTypes verifies that integer values in various
// Go representations are accepted (int, int64, float64 whole numbers).
func TestValidate_IntegerAcceptableTypes(t *testing.T) {
	s := testSchema()

	tests := []struct {
		name  string
		value interface{}
	}{
		{"int", int(5)},
		{"int64", int64(5)},
		{"float64 whole", float64(5.0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := map[string]interface{}{
				"project.name":         "my-app",
				"release.max_retained": tt.value,
			}
			errs := Validate(s, config)
			for _, err := range errs {
				if err.Key == "release.max_retained" {
					t.Errorf("unexpected error for integer value %v (type %T): %v", tt.value, tt.value, err)
				}
			}
		})
	}
}

// TestValidationError_ErrorString verifies that ValidationError.Error()
// returns a useful human-readable message.
func TestValidationError_ErrorString(t *testing.T) {
	ve := ValidationError{
		Key:      "project.version",
		Expected: "string",
		Actual:   42,
	}
	msg := ve.Error()
	if msg == "" {
		t.Error("ValidationError.Error() must not return empty string")
	}
	if !substringInString(msg, "project.version") {
		t.Errorf("ValidationError.Error() = %q, should contain 'project.version'", msg)
	}

	// Test with source.
	veWithSource := ValidationError{
		Key:      "project.name",
		Expected: "string",
		Actual:   nil,
		Source:   "anvil.yaml:3",
	}
	msg = veWithSource.Error()
	if !substringInString(msg, "anvil.yaml:3") {
		t.Errorf("ValidationError.Error() with source = %q, should contain source location", msg)
	}
}

// TestValidate_SemverValid verifies that valid SemVer version strings pass
// validation without errors.
//
// AC: Valid SemVer version does not produce a validation error.
//
// Reference: ST-P1-03
func TestValidate_SemverValid(t *testing.T) {
	s := testSchema()

	validVersions := []string{
		"1.0.0",
		"0.0.1",
		"999.999.999",
		"2.1.0",
		"10.20.30",
	}

	for _, v := range validVersions {
		t.Run(v, func(t *testing.T) {
			config := map[string]interface{}{
				"project.name":    "my-app",
				"project.version": v,
			}
			errs := Validate(s, config)
			for _, err := range errs {
				if err.Key == "project.version" {
					t.Errorf("unexpected error for valid semver %q: %v", v, err)
				}
			}
		})
	}
}

// TestValidate_SemverInvalid verifies that invalid version strings produce
// a validation error for the "project.version" key.
//
// AC: Invalid version produces validation error identifying field, invalid
// value, and expected SemVer format.
//
// Reference: ST-P1-03
func TestValidate_SemverInvalid(t *testing.T) {
	s := testSchema()

	invalidVersions := []struct {
		name  string
		value string
	}{
		{"missing patch", "1.0"},
		{"missing minor and patch", "1"},
		{"non-numeric major", "a.b.c"},
		{"empty string", ""},
		{"with prefix", "v1.0.0"},
		{"with build metadata", "1.0.0+build1"},
		{"with pre-release", "1.0.0-rc1"},
		{"negative numbers", "-1.0.0"},
		{"extra dot", "1.0.0.0"},
	}

	for _, tt := range invalidVersions {
		t.Run(tt.name, func(t *testing.T) {
			config := map[string]interface{}{
				"project.name":    "my-app",
				"project.version": tt.value,
			}
			errs := Validate(s, config)

			found := false
			for _, err := range errs {
				if err.Key == "project.version" {
					found = true
					// Verify the error includes expected format guidance.
					if !contains(err.Expected, "SemVer") && !contains(err.Expected, "semver") {
						t.Errorf("semver error should mention SemVer format, got Expected=%q", err.Expected)
					}
					break
				}
			}
			if tt.value != "" {
				// Empty string passes type check but doesn't fail semver (skip check).
				// Non-empty invalid versions should produce semver errors.
				if !found {
					t.Errorf("expected semver validation error for %q, got none", tt.value)
				}
			}
		})
	}
}

// TestValidate_SemverEmptyString verifies that an empty version string does
// not fail semver validation (empty is handled by type/required checks).
func TestValidate_SemverEmptyString(t *testing.T) {
	s := testSchema()
	config := map[string]interface{}{
		"project.name":    "my-app",
		"project.version": "",
	}

	errs := Validate(s, config)
	for _, err := range errs {
		if err.Key == "project.version" {
			// Empty string passed type check (it is a string),
			// and semver check skips empty values.
			t.Errorf("unexpected error for empty version: %v", err)
		}
	}
}

// TestValidate_SemverErrorMessage verifies that the error message for an
// invalid SemVer includes the field key, the invalid value, and a description
// of the expected format.
//
// AC: Error message identifies the field, the invalid value, and the expected
// format (semver).
//
// Reference: ST-P1-03
func TestValidate_SemverErrorMessage(t *testing.T) {
	s := testSchema()
	config := map[string]interface{}{
		"project.name":    "my-app",
		"project.version": "v1.0.0",
	}

	errs := Validate(s, config)
	found := false
	for _, err := range errs {
		if err.Key == "project.version" {
			found = true
			// Verify the error mentions all required information.
			msg := err.Error()
			if !contains(msg, "project.version") {
				t.Errorf("error should contain field key 'project.version', got: %s", msg)
			}
			if !contains(msg, "v1.0.0") {
				t.Errorf("error should contain invalid value 'v1.0.0', got: %s", msg)
			}
			if !contains(msg, "SemVer") && !contains(msg, "semver") {
				t.Errorf("error should mention SemVer format, got: %s", msg)
			}
			break
		}
	}
	if !found {
		t.Error("expected validation error for invalid semver 'v1.0.0', got none")
	}
}

// --- ValidateConfig tests (TS-P2-05) ---

// TestValidateConfig_ValidConfig verifies that ValidateConfig returns
// the unchanged config and no errors when configuration is valid.
func TestValidateConfig_ValidConfig(t *testing.T) {
	s := testSchema()
	config := map[string]interface{}{
		"project.name":         "my-app",
		"project.version":      "2.0.0",
		"release.max_retained": 10,
		"release.auto_verify":  true,
		"global.log_level":     "info",
		"global.no_color":      false,
	}

	result, errs := ValidateConfig(s, config)
	if len(errs) != 0 {
		t.Errorf("ValidateConfig() returned %d errors for valid config: %v", len(errs), errs)
	}
	if result == nil {
		t.Error("ValidateConfig() returned nil config for valid config")
	}
	if result["project.name"] != "my-app" {
		t.Errorf("ValidateConfig() returned modified config: project.name = %v", result["project.name"])
	}
}

// TestValidateConfig_InvalidConfig verifies that ValidateConfig returns
// nil config and all errors when configuration contains violations.
func TestValidateConfig_InvalidConfig(t *testing.T) {
	s := testSchema()
	config := map[string]interface{}{
		// "project.name" is missing (required)
		"global.log_level": "invalid",
	}

	result, errs := ValidateConfig(s, config)
	if len(errs) == 0 {
		t.Fatal("ValidateConfig() returned no errors for invalid config")
	}
	if result != nil {
		t.Error("ValidateConfig() should return nil config when validation fails")
	}

	// Verify errors include missing required key.
	foundMissing := false
	for _, err := range errs {
		if err.Key == "project.name" {
			foundMissing = true
			break
		}
	}
	if !foundMissing {
		t.Error("ValidateConfig() errors should include missing required key 'project.name'")
	}
}

// TestValidateConfig_RequiredValueEnforcement verifies that required keys
// missing from config produce errors at load time.
func TestValidateConfig_RequiredValueEnforcement(t *testing.T) {
	s := testSchema()
	config := map[string]interface{}{
		// "project.name" is required but omitted
	}

	_, errs := ValidateConfig(s, config)
	found := false
	for _, err := range errs {
		if err.Key == "project.name" {
			found = true
			if err.Actual != nil {
				t.Errorf("missing required key error should have nil Actual, got %v", err.Actual)
			}
			break
		}
	}
	if !found {
		t.Error("ValidateConfig() should produce error for missing required key 'project.name'")
	}
}

// TestValidateConfig_NoModification verifies that ValidateConfig does not
// modify the configuration values it validates.
func TestValidateConfig_NoModification(t *testing.T) {
	s := testSchema()
	config := map[string]interface{}{
		"project.name":    "my-app",
		"project.version": "1.0.0",
	}

	originalVersion := config["project.version"]
	_, _ = ValidateConfig(s, config)
	if config["project.version"] != originalVersion {
		t.Errorf("ValidateConfig() modified config value: was %v, now %v", originalVersion, config["project.version"])
	}
}

// TestValidate_AcceptsFrameworkExtensionKeys verifies the schema-validity
// guarantee of standard-driven configuration extension (TS-015-03-01 DoD:
// anvil.yaml remains schema-valid): framework configuration extension keys
// (framework.<name>.<key>, ADR-005 §4.4) merged into the project config
// from the installed standard pass validation — the canonical schema is
// untouched and extension keys are never rejected as unknown. The Core
// validates only Core-owned keys; the standard validates its own extended
// values (C6, TS-015-03-02).
func TestValidate_AcceptsFrameworkExtensionKeys(t *testing.T) {
	schema := GetSchema()
	config := map[string]interface{}{
		"project.name":                  "app",
		"project.version":               "1.0.0",
		"framework.laravel.version":     "11.0.0",
		"framework.laravel.cache.store": "redis",
	}
	errs := Validate(schema, config)
	if len(errs) != 0 {
		t.Fatalf("Validate() = %v, want no errors for framework extension keys", errs)
	}
}

// --- helpers ---
func substringInString(s, substr string) bool {
	return len(s) >= len(substr) && searchStringForward(s, substr)
}

func searchStringForward(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
