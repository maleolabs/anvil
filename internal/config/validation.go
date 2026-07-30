// Package config provides configuration schema validation for Anvil projects.
// The validation engine validates configuration values against the canonical
// schema, collecting all errors before returning.
//
// Reference: TS-P2-02, ADR-005, ADR-002, EPIC-002
package config

import (
	"fmt"
	"regexp"
)

// ValidationError represents a single configuration validation failure.
type ValidationError struct {
	// Key is the fully qualified dot-notation path of the invalid key.
	Key string

	// Expected describes the type or format that was expected.
	Expected string

	// Actual is the value that was found (may be nil for missing keys).
	Actual interface{}

	// Source is the source location of the value (file and line, if known).
	Source string
}

// Error returns a human-readable validation error message.
func (ve ValidationError) Error() string {
	if ve.Source != "" {
		return fmt.Sprintf("%s: %s: expected %s, got %v", ve.Source, ve.Key, ve.Expected, ve.Actual)
	}
	return fmt.Sprintf("%s: expected %s, got %v", ve.Key, ve.Expected, ve.Actual)
}

// Validate checks the provided configuration values against the canonical
// schema. It returns a slice of ValidationError for every violation found.
//
// The function collects ALL errors before returning (non-fail-fast). If no
// violations are found, it returns an empty slice.
//
// config is a flat map of dot-notation key paths to their values, as produced
// by the configuration loader after combining all sources.
//
// The function does not modify the schema or the configuration values.
func Validate(schema Schema, config map[string]interface{}) []ValidationError {
	var errs []ValidationError

	for key, entry := range schema.Entries {
		value, present := config[key]

		// Check required key presence.
		if entry.Required && !present {
			errs = append(errs, ValidationError{
				Key:      key,
				Expected: fmt.Sprintf("required %s value", entry.Type),
				Actual:   nil,
			})
			continue
		}

		// Skip optional keys that are not present (defaults apply).
		if !present {
			continue
		}

		// Type checking.
		if err := checkType(entry.Type, value); err != nil {
			errs = append(errs, ValidationError{
				Key:      key,
				Expected: entry.Type.String(),
				Actual:   value,
			})
			continue
		}

		// Allowed value enforcement (string type only).
		if len(entry.AllowedValues) > 0 && entry.Type == TypeString {
			strVal, ok := value.(string)
			if ok && !isAllowed(strVal, entry.AllowedValues) {
				errs = append(errs, ValidationError{
					Key:      key,
					Expected: fmt.Sprintf("one of %v", entry.AllowedValues),
					Actual:   value,
				})
			}
		}

		// Format validation for specific well-known keys.
		if key == "project.version" {
			strVal, ok := value.(string)
			if ok && strVal != "" {
				if err := validateSemver(strVal); err != nil {
					errs = append(errs, ValidationError{
						Key:      key,
						Expected: "valid SemVer string (e.g. \"1.2.3\")",
						Actual:   strVal,
					})
				}
			}
		}
	}

	return errs
}

// semverPattern matches a basic MAJOR.MINOR.PATCH SemVer 2.0.0 version.
// This covers the core numeric portion; pre-release and build metadata are
// not required for basic validation but the format allows future extension.
var semverPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// validateSemver checks whether the given string follows the basic SemVer
// format (MAJOR.MINOR.PATCH, e.g. "1.2.3").
//
// Returns nil for valid SemVer strings, or an error describing the format
// requirement for invalid strings.
func validateSemver(version string) error {
	if !semverPattern.MatchString(version) {
		return fmt.Errorf("version %q is not valid SemVer (expected MAJOR.MINOR.PATCH, e.g. \"1.2.3\")", version)
	}
	return nil
}

// checkType verifies that value matches the expected schema type.
func checkType(expected ValueType, value interface{}) error {
	switch expected {
	case TypeString:
		_, ok := value.(string)
		if !ok {
			return fmt.Errorf("expected string, got %T", value)
		}
	case TypeInteger:
		// JSON/YAML unmarshalling may produce int or float64.
		switch v := value.(type) {
		case int:
			_ = v
		case int64:
			_ = v
		case float64:
			// Allow whole-number floats (e.g. 5.0) as integers.
			if v != float64(int(v)) {
				return fmt.Errorf("expected integer, got float %v", v)
			}
		default:
			return fmt.Errorf("expected integer, got %T", value)
		}
	case TypeBoolean:
		_, ok := value.(bool)
		if !ok {
			return fmt.Errorf("expected boolean, got %T", value)
		}
	case TypeArray:
		_, ok := value.([]interface{})
		if !ok {
			// Also accept typed slices (e.g. []string) from code-level config.
			return fmt.Errorf("expected array, got %T", value)
		}
	case TypeObject:
		_, ok := value.(map[string]interface{})
		if !ok {
			return fmt.Errorf("expected object, got %T", value)
		}
	}
	return nil
}

// isAllowed checks whether value is in the allowed set.
func isAllowed(value string, allowed []string) bool {
	for _, a := range allowed {
		if value == a {
			return true
		}
	}
	return false
}

// ValidateConfig validates the provided configuration values against the
// canonical schema and returns the validated configuration (or nil) together
// with any validation errors.
//
// This function is the load-time validation integration point (TS-P2-05). It
// wraps the Validate engine and enforces that validation always occurs during
// configuration loading. It is called by the multi-source loader (TS-P2-04)
// after configuration is loaded from all sources.
//
// Parameters:
//   - schema: the canonical schema to validate against
//   - config: flat map of dot-notation key paths to their values
//
// Returns:
//   - validated config (unchanged) when no errors found
//   - nil config + all validation errors when validation fails
//
// The function does not modify the schema or the configuration values.
func ValidateConfig(schema Schema, config map[string]interface{}) (map[string]interface{}, []ValidationError) {
	errs := Validate(schema, config)
	if len(errs) > 0 {
		return nil, errs
	}
	return config, nil
}
