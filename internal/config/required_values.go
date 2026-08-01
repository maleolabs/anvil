// Package config provides required value enforcement for Anvil projects.
// Required value enforcement ensures that configuration values marked as
// required in the canonical schema are present before any capability uses
// the configuration. If required values are missing, the operation is blocked.
//
// Reference: ST-P2-04, ADR-005, EPIC-002
package config

// RequiredValueResult represents the outcome of required value enforcement.
//
// When Blocked is true, the operation must not proceed. The Summary field
// contains the formatted validation errors describing which required values
// are missing and what types are expected.
//
// When Blocked is false, all required values are present and the operation
// may proceed. Summary is empty.
type RequiredValueResult struct {
	// Blocked indicates whether the operation should be blocked because
	// one or more required values are missing from all sources.
	Blocked bool

	// Summary contains the formatted validation error summary.
	// Empty when Blocked is false.
	Summary string
}

// EnforceRequiredValues checks that all required configuration values are
// present and valid. If any required value is missing from all sources, the
// operation is blocked (Blocked=true) and the Summary contains the formatted
// errors identifying each missing key and its expected type.
//
// This function is the ST-P2-04 enforcement point. It wraps ValidateConfig
// with explicit "blocked operation" semantics, making it clear to callers
// that the operation must not proceed when required values are missing.
//
// The function checks all sources in order: project files, global files,
// environment variables, then compiled defaults. If none provides the
// required value, it is treated as missing.
//
// Parameters:
//   - schema: the canonical schema defining which keys are required
//   - config: flat map of dot-notation key paths to their values, as
//     produced by the configuration loader after combining all sources
//
// Returns:
//   - RequiredValueResult with Blocked=false when all required values are
//     present from at least one source
//   - RequiredValueResult with Blocked=true + error Summary when one or more
//     required values are missing from all sources
//
// The function does not modify the schema or the configuration values.
//
// Reference: ST-P2-04, ADR-005 §8.3, §8.7
func EnforceRequiredValues(schema Schema, config map[string]interface{}) RequiredValueResult {
	_, errs := ValidateConfig(schema, config)
	if len(errs) == 0 {
		return RequiredValueResult{Blocked: false, Summary: ""}
	}
	return RequiredValueResult{
		Blocked: true,
		Summary: FormatValidationErrors(errs),
	}
}
