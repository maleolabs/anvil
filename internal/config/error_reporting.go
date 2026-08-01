// Package config provides configuration validation error reporting for Anvil
// projects. Error reporting formats validation errors into user-facing output
// that includes key path, invalid value, expected format, and source location.
//
// Reference: ST-P2-03, ADR-010, ADR-005, EPIC-002
package config

import "fmt"

// FormatValidationErrors formats a slice of ValidationError into a
// human-readable string. All errors are displayed together (non-fail-fast).
// Returns an empty string when there are no errors.
//
// Each error includes:
//   - The configuration key path
//   - The invalid value (if applicable, nil for missing keys)
//   - The expected type or format
//   - The source location (file path and line number, if known)
//
// Format: <source>: <key>: expected <expected>, got <value>
// When source is unknown, source prefix is omitted.
//
// Reference: ST-P2-03, ADR-010 §3.4
func FormatValidationErrors(errs []ValidationError) string {
	if len(errs) == 0 {
		return ""
	}

	var result string
	for i, err := range errs {
		if i > 0 {
			result += "\n"
		}
		result += formatError(err)
	}
	return result
}

// formatError formats a single ValidationError into a human-readable string.
func formatError(err ValidationError) string {
	if err.Source != "" {
		return fmt.Sprintf("%s: %s: expected %s, got %v", err.Source, err.Key, err.Expected, err.Actual)
	}
	return fmt.Sprintf("%s: expected %s, got %v", err.Key, err.Expected, err.Actual)
}

// ValidationSummary provides a human-readable summary of validation results.
// If validation passes, it returns an empty string (silent pass-through).
// If validation fails, it returns all formatted errors.
//
// This is the primary entry point for consumers that need to display
// validation results to the user.
//
// Reference: ST-P2-03
func ValidationSummary(errs []ValidationError) string {
	return FormatValidationErrors(errs)
}
