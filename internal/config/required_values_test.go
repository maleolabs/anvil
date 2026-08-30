// Package config provides required value enforcement tests for Anvil projects.
//
// Reference: ST-P2-04
package config

import (
	"strings"
	"testing"
)

// TestEnforceRequiredValues_AllPresent verifies that when all required
// configuration keys are present from at least one source, enforcement
// passes (Blocked=false, no errors).
//
// Covers AC 3: When all required keys are present from any source, no
// missing-key error is produced.
func TestEnforceRequiredValues_AllPresent(t *testing.T) {
	s := testSchema()
	config := map[string]interface{}{
		"project.name":    "my-app",
		"project.version": "2.0.0",
	}

	result := EnforceRequiredValues(s, config)
	if result.Blocked {
		t.Errorf("EnforceRequiredValues() blocked valid config: summary=%q", result.Summary)
	}
	if result.Summary != "" {
		t.Errorf("EnforceRequiredValues() returned non-empty summary for valid config: %q", result.Summary)
	}
}

// TestEnforceRequiredValues_MissingRequiredKey verifies that a single missing
// required key produces a blocked result with an error identifying the key
// and its expected type.
//
// Covers AC 1: When a required key is missing from all sources, a load-time
// error is produced identifying the key and its expected type.
func TestEnforceRequiredValues_MissingRequiredKey(t *testing.T) {
	s := testSchema()
	config := map[string]interface{}{
		// "project.name" is required but intentionally omitted
		"project.version": "1.0.0",
	}

	result := EnforceRequiredValues(s, config)
	if !result.Blocked {
		t.Fatal("EnforceRequiredValues() should block when required key is missing")
	}
	if result.Summary == "" {
		t.Fatal("EnforceRequiredValues() should return error summary for missing required key")
	}

	// Verify the error identifies the missing key.
	if !strings.Contains(result.Summary, "project.name") {
		t.Errorf("error summary should identify missing key 'project.name', got: %s", result.Summary)
	}

	// Verify the error identifies the expected type.
	if !strings.Contains(result.Summary, "string") {
		t.Errorf("error summary should identify expected type, got: %s", result.Summary)
	}
}

// TestEnforceRequiredValues_MultipleMissingKeys verifies that when multiple
// required keys are missing, all are reported together (non-fail-fast).
//
// Covers AC 2: When multiple required keys are missing, all are reported
// together.
func TestEnforceRequiredValues_MultipleMissingKeys(t *testing.T) {
	s := Schema{
		Version: "1.0.0",
		Entries: map[string]SchemaEntry{
			"project.name": {
				Key:      "project.name",
				Type:     TypeString,
				Required: true,
			},
			"project.version": {
				Key:      "project.version",
				Type:     TypeString,
				Required: true,
			},
			"release.max_retained": {
				Key:      "release.max_retained",
				Type:     TypeInteger,
				Required: true,
			},
		},
	}
	config := map[string]interface{}{
		// All three required keys are intentionally missing
	}

	result := EnforceRequiredValues(s, config)
	if !result.Blocked {
		t.Fatal("EnforceRequiredValues() should block when multiple required keys are missing")
	}

	// Verify all missing keys are reported together.
	missingKeys := []string{"project.name", "project.version", "release.max_retained"}
	for _, key := range missingKeys {
		if !strings.Contains(result.Summary, key) {
			t.Errorf("error summary should include missing key %q, got: %s", key, result.Summary)
		}
	}
}

// TestEnforceRequiredValues_OptionalKeyOmitted verifies that missing optional
// keys do not produce errors or block the operation. Optional keys that are
// missing fall back to defaults.
//
// Covers AC 5: Optional keys that are missing do not produce errors.
func TestEnforceRequiredValues_OptionalKeyOmitted(t *testing.T) {
	s := testSchema()
	config := map[string]interface{}{
		"project.name": "my-app",
		// "project.version" is optional, intentionally omitted
		// "release.max_retained" is optional, intentionally omitted
	}

	result := EnforceRequiredValues(s, config)
	if result.Blocked {
		t.Errorf("EnforceRequiredValues() should not block when only optional keys are missing: summary=%q", result.Summary)
	}
	if result.Summary != "" {
		t.Errorf("EnforceRequiredValues() should return empty summary when only optional keys are missing: %q", result.Summary)
	}
}

// TestEnforceRequiredValues_BlockedUntilValuesProvided verifies that the
// operation is blocked when values are missing and proceeds after they are
// provided. This simulates the user adding missing required values and
// re-running.
//
// Covers AC 4 + AC 6: The operation is blocked until all required values are
// provided. Providing the missing required values and re-running the command
// allows the operation to proceed.
func TestEnforceRequiredValues_BlockedUntilValuesProvided(t *testing.T) {
	s := testSchema()

	// Step 1: Missing required key -> blocked.
	configWithoutName := map[string]interface{}{
		// "project.name" is required but omitted
	}
	result := EnforceRequiredValues(s, configWithoutName)
	if !result.Blocked {
		t.Fatal("EnforceRequiredValues() should block when required value is missing")
	}

	// Step 2: Provide the missing value -> operation proceeds.
	configWithName := map[string]interface{}{
		"project.name": "my-app",
	}
	result = EnforceRequiredValues(s, configWithName)
	if result.Blocked {
		t.Errorf("EnforceRequiredValues() should not block after providing missing required value: summary=%q", result.Summary)
	}
	if result.Summary != "" {
		t.Errorf("EnforceRequiredValues() should return empty summary after providing missing value: %q", result.Summary)
	}
}

// TestEnforceRequiredValues_EmptyConfig verifies that an empty configuration
// (no values from any source) produces a blocked result with errors for all
// required keys but not for optional keys.
func TestEnforceRequiredValues_EmptyConfig(t *testing.T) {
	s := testSchema()
	config := map[string]interface{}{}

	result := EnforceRequiredValues(s, config)
	if !result.Blocked {
		t.Fatal("EnforceRequiredValues() should block for empty config with required keys")
	}

	// Should only contain errors for required keys.
	// In testSchema, only "project.name" is required.
	if !strings.Contains(result.Summary, "project.name") {
		t.Errorf("error summary should include required key 'project.name', got: %s", result.Summary)
	}
}

// TestEnforceRequiredValues_NoModification verifies that EnforceRequiredValues
// does not modify the configuration values it validates.
func TestEnforceRequiredValues_NoModification(t *testing.T) {
	s := testSchema()
	config := map[string]interface{}{
		"project.name":    "my-app",
		"project.version": "1.0.0",
	}

	originalVersion := config["project.version"]
	_ = EnforceRequiredValues(s, config)
	if config["project.version"] != originalVersion {
		t.Errorf("EnforceRequiredValues() modified config value: was %v, now %v", originalVersion, config["project.version"])
	}
}
