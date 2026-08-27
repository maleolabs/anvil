// Package config provides configuration validation error reporting tests.
//
// Reference: ST-P2-03
package config

import (
	"testing"
)

// TestFormatValidationErrors_Empty verifies that FormatValidationErrors
// returns an empty string when there are no errors.
func TestFormatValidationErrors_Empty(t *testing.T) {
	result := FormatValidationErrors(nil)
	if result != "" {
		t.Errorf("FormatValidationErrors(nil) = %q, want empty string", result)
	}

	result = FormatValidationErrors([]ValidationError{})
	if result != "" {
		t.Errorf("FormatValidationErrors([]) = %q, want empty string", result)
	}
}

// TestFormatValidationErrors_TypeMismatch verifies that a type mismatch error
// includes the key path, the actual value, the expected type, and the source
// location.
func TestFormatValidationErrors_TypeMismatch(t *testing.T) {
	errs := []ValidationError{
		{
			Key:      "release.max_retained",
			Expected: "integer",
			Actual:   "five",
			Source:   "anvil.yaml:10",
		},
	}

	result := FormatValidationErrors(errs)
	if result == "" {
		t.Fatal("FormatValidationErrors() returned empty string for errors")
	}

	if !contains(result, "release.max_retained") {
		t.Errorf("error message should contain key path, got: %s", result)
	}
	if !contains(result, "five") {
		t.Errorf("error message should contain actual value, got: %s", result)
	}
	if !contains(result, "integer") {
		t.Errorf("error message should contain expected type, got: %s", result)
	}
	if !contains(result, "anvil.yaml:10") {
		t.Errorf("error message should contain source location, got: %s", result)
	}
}

// TestFormatValidationErrors_UnknownKey verifies that an unknown key error
// identifies the key and its location.
func TestFormatValidationErrors_UnknownKey(t *testing.T) {
	errs := []ValidationError{
		{
			Key:      "project.invalid_key",
			Expected: "remove unknown key",
			Actual:   "some_value",
			Source:   "anvil.yaml:5",
		},
	}

	result := FormatValidationErrors(errs)
	if !contains(result, "project.invalid_key") {
		t.Errorf("error message should contain unknown key, got: %s", result)
	}
	if !contains(result, "anvil.yaml:5") {
		t.Errorf("error message should contain source location, got: %s", result)
	}
}

// TestFormatValidationErrors_AllowedValueViolation verifies that an allowed
// value violation error includes the value, the allowed values, and the
// location.
func TestFormatValidationErrors_AllowedValueViolation(t *testing.T) {
	errs := []ValidationError{
		{
			Key:      "global.log_level",
			Expected: "one of [debug info warn error]",
			Actual:   "verbose",
			Source:   "anvil.yaml:15",
		},
	}

	result := FormatValidationErrors(errs)
	if !contains(result, "global.log_level") {
		t.Errorf("error message should contain key, got: %s", result)
	}
	if !contains(result, "verbose") {
		t.Errorf("error message should contain invalid value, got: %s", result)
	}
	if !contains(result, "debug") {
		t.Errorf("error message should show allowed values, got: %s", result)
	}
	if !contains(result, "anvil.yaml:15") {
		t.Errorf("error message should contain source location, got: %s", result)
	}
}

// TestFormatValidationErrors_MultipleErrors verifies that all errors are
// displayed together (non-fail-fast).
func TestFormatValidationErrors_MultipleErrors(t *testing.T) {
	errs := []ValidationError{
		{
			Key:      "project.name",
			Expected: "required string value",
			Actual:   nil,
			Source:   "anvil.yaml:1",
		},
		{
			Key:      "release.max_retained",
			Expected: "integer",
			Actual:   "not-a-number",
			Source:   "anvil.yaml:12",
		},
		{
			Key:      "global.log_level",
			Expected: "one of [debug info warn error]",
			Actual:   "verbose",
			Source:   "anvil.yaml:18",
		},
	}

	result := FormatValidationErrors(errs)
	if !contains(result, "project.name") {
		t.Errorf("result should contain first error key, got: %s", result)
	}
	if !contains(result, "release.max_retained") {
		t.Errorf("result should contain second error key, got: %s", result)
	}
	if !contains(result, "global.log_level") {
		t.Errorf("result should contain third error key, got: %s", result)
	}

	// Verify multiple lines (newlines between errors).
	lineCount := 0
	for _, c := range result {
		if c == '\n' {
			lineCount++
		}
	}
	if lineCount < 2 {
		t.Errorf("expected at least 2 newlines for 3 errors, got %d", lineCount)
	}
}

// TestFormatValidationErrors_NoSource verifies that errors without a source
// location still produce a useful message.
func TestFormatValidationErrors_NoSource(t *testing.T) {
	errs := []ValidationError{
		{
			Key:      "project.name",
			Expected: "required string value",
			Actual:   nil,
			// Source is empty (unknown)
		},
	}

	result := FormatValidationErrors(errs)
	if !contains(result, "project.name") {
		t.Errorf("error without source should still contain key, got: %s", result)
	}
}

// TestValidationSummary_ValidConfig verifies that ValidationSummary returns
// an empty string when no errors exist (silent pass-through).
func TestValidationSummary_ValidConfig(t *testing.T) {
	result := ValidationSummary(nil)
	if result != "" {
		t.Errorf("ValidationSummary(nil) = %q, want empty string for valid config", result)
	}

	result = ValidationSummary([]ValidationError{})
	if result != "" {
		t.Errorf("ValidationSummary([]) = %q, want empty string for valid config", result)
	}
}

// TestValidationSummary_InvalidConfig verifies that ValidationSummary returns
// formatted errors when validation fails.
func TestValidationSummary_InvalidConfig(t *testing.T) {
	errs := []ValidationError{
		{
			Key:      "project.name",
			Expected: "required string value",
			Actual:   nil,
			Source:   "anvil.yaml:1",
		},
	}

	result := ValidationSummary(errs)
	if result == "" {
		t.Fatal("ValidationSummary() returned empty string for invalid config")
	}
	if !contains(result, "project.name") {
		t.Errorf("ValidationSummary() should include error details, got: %s", result)
	}
}

// TestFormatValidationErrors_ErrorFormat verifies that the error format
// follows the project conventions (ADR-010 §3.4): source: key: expected
// type, got value.
func TestFormatValidationErrors_ErrorFormat(t *testing.T) {
	errs := []ValidationError{
		{
			Key:      "project.version",
			Expected: "string",
			Actual:   42,
			Source:   "anvil.yaml:8",
		},
	}

	result := FormatValidationErrors(errs)
	// Expected format: anvil.yaml:8: project.version: expected string, got 42
	expectedParts := []string{"anvil.yaml:8", "project.version", "expected string", "got 42"}
	for _, part := range expectedParts {
		if !contains(result, part) {
			t.Errorf("error message should contain %q, got: %s", part, result)
		}
	}
}

// --- helpers ---

// contains reports whether substr is within s.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchForward(s, substr)
}

func searchForward(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
