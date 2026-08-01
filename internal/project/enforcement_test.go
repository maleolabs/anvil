// Package project provides load-time validation enforcement tests.
//
// Reference: ST-P1-05, TS-P1-05, ADR-005 §8.3
package project

import (
	"errors"
	"testing"
)

// --- ST-P1-05 Tests: Load-Time Validation Enforcement ---

// TestValidateLoadedConfig_ValidConfig verifies that a valid project
// configuration returns nil (operation may proceed).
//
// Acceptance Criteria: ST-P1-05 AC-1
func TestValidateLoadedConfig_ValidConfig(t *testing.T) {
	cfg := testMinimalValidConfig()
	err := ValidateLoadedConfig(cfg)

	if err != nil {
		t.Errorf("ValidateLoadedConfig() returned error for valid config: %v", err)
	}
}

// TestValidateLoadedConfig_MissingRequiredKey verifies that a config missing
// a required key returns a ValidationBlockedError with the missing key listed.
//
// Acceptance Criteria: ST-P1-05 AC-2
func TestValidateLoadedConfig_MissingRequiredKey(t *testing.T) {
	cfg := testMinimalValidConfig()
	cfg.Project.Name = "" // empty name fails non-empty constraint

	err := ValidateLoadedConfig(cfg)
	if err == nil {
		t.Fatal("ValidateLoadedConfig() returned nil for config with missing required key")
	}

	var blockErr *ValidationBlockedError
	if !errors.As(err, &blockErr) {
		t.Fatalf("ValidateLoadedConfig() returned %T, want *ValidationBlockedError", err)
	}

	if len(blockErr.Errors) == 0 {
		t.Fatal("ValidationBlockedError.Errors is empty, expected at least one error")
	}

	found := false
	for _, e := range blockErr.Errors {
		if contains(e, "project.name") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ValidationBlockedError.Errors should include 'project.name', got: %v", blockErr.Errors)
	}
}

// TestValidateLoadedConfig_InvalidValueType verifies that a config with an
// invalid value type (allowed values violation) returns a ValidationBlockedError
// referencing the invalid key and value.
//
// Acceptance Criteria: ST-P1-05 AC-2
func TestValidateLoadedConfig_InvalidValueType(t *testing.T) {
	cfg := testMinimalValidConfig()
	cfg.Global.LogLevel = "invalid-level"

	err := ValidateLoadedConfig(cfg)
	if err == nil {
		t.Fatal("ValidateLoadedConfig() returned nil for config with invalid value")
	}

	var blockErr *ValidationBlockedError
	if !errors.As(err, &blockErr) {
		t.Fatalf("ValidateLoadedConfig() returned %T, want *ValidationBlockedError", err)
	}

	if len(blockErr.Errors) == 0 {
		t.Fatal("ValidationBlockedError.Errors is empty, expected at least one error")
	}

	if !contains(blockErr.Errors[0], "global.log_level") {
		t.Errorf("error should reference 'global.log_level', got: %s", blockErr.Errors[0])
	}
	if !contains(blockErr.Errors[0], "invalid-level") {
		t.Errorf("error should reference invalid value, got: %s", blockErr.Errors[0])
	}
}

// TestValidateLoadedConfig_MultipleErrors verifies that when multiple
// validation errors exist, all are present in ValidationBlockedError.Errors.
//
// Acceptance Criteria: ST-P1-05 AC-3
func TestValidateLoadedConfig_MultipleErrors(t *testing.T) {
	cfg := &ProjectConfig{
		Project: &ProjectSection{
			Name:        "", // empty — fails non-empty constraint
			Version:     "1.0.0",
			Description: "",
		},
		Global: &GlobalSection{
			LogLevel: "invalid-level", // not in allowed set
		},
	}

	err := ValidateLoadedConfig(cfg)
	if err == nil {
		t.Fatal("ValidateLoadedConfig() returned nil for config with multiple errors")
	}

	var blockErr *ValidationBlockedError
	if !errors.As(err, &blockErr) {
		t.Fatalf("ValidateLoadedConfig() returned %T, want *ValidationBlockedError", err)
	}

	if len(blockErr.Errors) < 2 {
		t.Errorf("ValidationBlockedError has %d errors, expected at least 2", len(blockErr.Errors))
	}
}

// TestValidateLoadedConfig_ErrorsIs verifies that errors.Is can match
// ValidationBlockedError across wrapped error chains.
//
// Acceptance Criteria: ST-P1-05 AC-4
func TestValidateLoadedConfig_ErrorsIs(t *testing.T) {
	cfg := testMinimalValidConfig()
	cfg.Project.Name = ""

	err := ValidateLoadedConfig(cfg)
	if err == nil {
		t.Fatal("ValidateLoadedConfig() returned nil for invalid config")
	}

	// errors.Is should match any *ValidationBlockedError value by type.
	if !errors.Is(err, &ValidationBlockedError{}) {
		t.Error("errors.Is(err, &ValidationBlockedError{}) should be true")
	}

	// Also verify that a different *ValidationBlockedError also matches by type.
	if !errors.Is(err, &ValidationBlockedError{Errors: []string{"some different error"}}) {
		t.Error("errors.Is should match *ValidationBlockedError by type, not by value content")
	}

	// Verify that errors.Is returns false for unrelated error types.
	if errors.Is(err, errors.New("some other error")) {
		t.Error("errors.Is should return false for unrelated error types")
	}
}

// TestValidateLoadedConfig_NoBypass verifies that the function signature
// has no bypass parameter — it takes only *ProjectConfig and returns error.
//
// This is a compile-time check that the enforcement cannot be skipped.
// There is no flag, option, or environment variable parameter.
//
// Acceptance Criteria: ST-P1-05 AC-5
func TestValidateLoadedConfig_NoBypass(t *testing.T) {
	// The function signature is: ValidateLoadedConfig(cfg *ProjectConfig) error
	// There is no flag, option, or env var parameter.
	// We verify this by calling it normally — the compiler guarantees the
	// function takes exactly one argument and returns exactly one value.

	// Valid config — only parameter is the config itself.
	cfg := testMinimalValidConfig()
	err := ValidateLoadedConfig(cfg)
	if err != nil {
		t.Errorf("ValidateLoadedConfig(valid) should return nil, got: %v", err)
	}

	// Invalid config — same signature, no extra bypass parameter.
	cfg.Project.Name = ""
	err = ValidateLoadedConfig(cfg)
	if err == nil {
		t.Error("ValidateLoadedConfig(invalid) should return error — no bypass exists")
	}
}

// TestValidateLoadedConfig_ErrorFormat verifies that the error string
// matches FormatProjectErrors() style (ST-P1-04 format), which includes
// the key path, expected type, actual value, and actionable guidance.
//
// Acceptance Criteria: ST-P1-05 AC-6
func TestValidateLoadedConfig_ErrorFormat(t *testing.T) {
	cfg := testMinimalValidConfig()
	cfg.Global.LogLevel = "bad-value"

	err := ValidateLoadedConfig(cfg)
	if err == nil {
		t.Fatal("ValidateLoadedConfig() returned nil for invalid config")
	}

	errStr := err.Error()

	// Should contain "expected" per ST-P1-04 format: key: expected X, got Y
	if !contains(errStr, "expected") {
		t.Errorf("error should contain 'expected', got: %s", errStr)
	}

	// Should contain the invalid key
	if !contains(errStr, "global.log_level") {
		t.Errorf("error should contain key 'global.log_level', got: %s", errStr)
	}

	// Should contain the invalid value
	if !contains(errStr, "bad-value") {
		t.Errorf("error should contain invalid value 'bad-value', got: %s", errStr)
	}
}

// TestValidateLoadedConfig_NilConfig verifies that a nil config returns
// an error (project config cannot be nil at load time).
//
// Acceptance Criteria: ST-P1-05 AC-7
func TestValidateLoadedConfig_NilConfig(t *testing.T) {
	err := ValidateLoadedConfig(nil)
	if err == nil {
		t.Fatal("ValidateLoadedConfig(nil) should return error")
	}

	var blockErr *ValidationBlockedError
	if !errors.As(err, &blockErr) {
		t.Fatalf("ValidateLoadedConfig(nil) returned %T, want *ValidationBlockedError", err)
	}

	if len(blockErr.Errors) == 0 {
		t.Error("ValidationBlockedError.Errors should not be empty for nil config")
	}
}

// TestValidateLoadedConfig_FormatConsistency verifies that the error format
// follows the convention from ST-P1-04 (key: expected X, got Y).
//
// Acceptance Criteria: ST-P1-05 AC-8
func TestValidateLoadedConfig_FormatConsistency(t *testing.T) {
	cfg := testMinimalValidConfig()
	cfg.Global.LogLevel = "bad-value"

	err := ValidateLoadedConfig(cfg)
	if err == nil {
		t.Fatal("ValidateLoadedConfig() returned nil for invalid config")
	}

	errStr := err.Error()

	// Format should follow: <key>: expected <X>, got <Y>
	if !contains(errStr, ": expected ") {
		t.Errorf("error should follow format 'key: expected X, got Y', got: %s", errStr)
	}
	if !contains(errStr, ", got ") {
		t.Errorf("error should contain ', got ', got: %s", errStr)
	}
}

// TestValidationBlockedError_EmptyErrors verifies that a ValidationBlockedError
// with empty Errors produces a sensible fallback message.
func TestValidationBlockedError_EmptyErrors(t *testing.T) {
	err := &ValidationBlockedError{Errors: nil}
	msg := err.Error()
	if msg == "" {
		t.Error("ValidationBlockedError with nil Errors should not produce empty string")
	}
	if !contains(msg, "project configuration is invalid") {
		t.Errorf("ValidationBlockedError.Error() should return fallback message, got: %q", msg)
	}

	err = &ValidationBlockedError{Errors: []string{}}
	msg = err.Error()
	if msg == "" {
		t.Error("ValidationBlockedError with empty Errors should not produce empty string")
	}
	if !contains(msg, "project configuration is invalid") {
		t.Errorf("ValidationBlockedError.Error() should return fallback message, got: %q", msg)
	}
}

// TestValidationBlockedError_ErrorsIsDirect verifies that errors.Is works
// directly on a ValidationBlockedError without going through ValidateLoadedConfig.
func TestValidationBlockedError_ErrorsIsDirect(t *testing.T) {
	err := &ValidationBlockedError{Errors: []string{"some error"}}

	if !errors.Is(err, &ValidationBlockedError{}) {
		t.Error("errors.Is should match ValidationBlockedError by type")
	}
}

// TestValidateLoadedConfig_ErrorsAs verifies that errors.As can extract
// the ValidationBlockedError with its full error list.
func TestValidateLoadedConfig_ErrorsAs(t *testing.T) {
	cfg := testMinimalValidConfig()
	cfg.Project.Name = ""

	err := ValidateLoadedConfig(cfg)
	if err == nil {
		t.Fatal("ValidateLoadedConfig() returned nil for invalid config")
	}

	var blockErr *ValidationBlockedError
	if !errors.As(err, &blockErr) {
		t.Fatal("errors.As should extract *ValidationBlockedError")
	}

	if blockErr.Errors == nil {
		t.Error("extracted ValidationBlockedError has nil Errors")
	}
}

// TestValidateLoadedConfig_BackwardCompatibility verifies that the existing
// ValidateProject function still works correctly after introducing enforcement.
func TestValidateLoadedConfig_BackwardCompatibility(t *testing.T) {
	// Valid config should still pass ValidateProject.
	cfg := testMinimalValidConfig()
	result := ValidateProject(cfg)
	if !result.Valid {
		t.Errorf("ValidateProject returned invalid after introducing enforcement: %v", result.Errors)
	}

	// Invalid config should still fail ValidateProject.
	cfg.Project.Name = ""
	result = ValidateProject(cfg)
	if result.Valid {
		t.Error("ValidateProject returned valid for invalid config after introducing enforcement")
	}

	// ValidateLoadedConfig should use the same validation logic.
	enforceErr := ValidateLoadedConfig(cfg)
	if enforceErr == nil {
		t.Error("ValidateLoadedConfig should return error for same invalid config")
	}
}
