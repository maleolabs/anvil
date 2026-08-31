// Package project provides load-time validation enforcement for Anvil projects.
//
// The enforcement layer ensures that operations are blocked when project
// configuration is invalid. There is no bypass mechanism — all project-dependent
// operations must pass through this gate.
//
// Reference: ST-P1-05, ADR-005 §8.3
package project

// ValidationBlockedError indicates that an operation was blocked due to
// invalid project configuration. It carries all validation error messages.
//
// Callers can use errors.Is or errors.As to detect this error type:
//
//	var blockErr *ValidationBlockedError
//	if errors.As(err, &blockErr) {
//	    // Operation was blocked due to invalid config
//	}
//
// Reference: ST-P1-05, ADR-005 §8.3
type ValidationBlockedError struct {
	// Errors contains all validation error messages collected during validation.
	Errors []string
}

// Error returns a formatted string of all validation errors.
// The format matches FormatProjectErrors() output from ST-P1-04.
func (e *ValidationBlockedError) Error() string {
	if len(e.Errors) == 0 {
		return "project configuration is invalid"
	}
	return FormatProjectErrors(ValidationResult{Valid: false, Errors: e.Errors})
}

// Is allows errors.Is matching for ValidationBlockedError.
// Any *ValidationBlockedError value matches the target type.
func (e *ValidationBlockedError) Is(target error) bool {
	_, ok := target.(*ValidationBlockedError)
	return ok
}

// ValidateLoadedConfig validates the project configuration and enforces
// that operations are blocked when configuration is invalid.
//
// Returns nil when configuration is valid (operation may proceed).
// Returns *ValidationBlockedError when configuration is invalid
// (operation must be blocked).
//
// There is no flag, option, or environment variable to bypass this enforcement.
// All project-dependent operations MUST call this function before proceeding.
//
// Parameters:
//   - cfg: the project configuration to validate
//
// Returns:
//   - nil if the configuration is valid
//   - *ValidationBlockedError with all validation errors if invalid
//
// Reference: ST-P1-05, ADR-005 §8.3
func ValidateLoadedConfig(cfg *ProjectConfig) error {
	result := ValidateProject(cfg)
	if result.Valid {
		return nil
	}
	return &ValidationBlockedError{Errors: result.Errors}
}
