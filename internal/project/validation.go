// Package project provides project configuration validation for Anvil projects.
//
// The validation engine validates project configuration against the canonical
// schema, collects all errors before returning (non-fail-fast), and formats
// errors with actionable guidance for the user.
//
// Reference: TS-P1-04, ST-P1-04, ADR-005, ADR-002, EPIC-001
package project

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"maleolabs.com/anvil/internal/config"
)

// ValidationResult holds the outcome of project configuration validation.
type ValidationResult struct {
	// Valid indicates whether the configuration passed all validation checks.
	Valid bool

	// Errors contains all validation errors found. Empty when Valid is true.
	Errors []string
}

// ProjectConfig represents a full project configuration structure, mirroring
// the schema domains. This is the runtime representation after loading from
// all configuration sources.
//
// Some fields may be nil or zero-valued if not provided by the user — the
// schema defaults and validation handle missing optional values.
type ProjectConfig struct {
	// Project section (EPIC-001).
	Project *ProjectSection `yaml:"project,omitempty"`

	// Artifact section (EPIC-003).
	Artifact *ArtifactSection `yaml:"artifact,omitempty"`

	// Release section (EPIC-004).
	Release *ReleaseSection `yaml:"release,omitempty"`

	// Runtime section (EPIC-005).
	Runtime *RuntimeSection `yaml:"runtime,omitempty"`

	// Global section (EPIC-008).
	Global *GlobalSection `yaml:"global,omitempty"`
}

// ProjectSection holds project identity and metadata configuration.
type ProjectSection struct {
	Name        string `yaml:"name"`
	Version     string `yaml:"version"`
	Description string `yaml:"description"`
}

// ArtifactSection holds artifact packaging configuration.
type ArtifactSection struct {
	Include  []string `yaml:"include"`
	Exclude  []string `yaml:"exclude"`
	Output   string   `yaml:"output"`
	Manifest bool     `yaml:"manifest"`
}

// ReleaseSection holds release lifecycle configuration.
type ReleaseSection struct {
	MaxRetained   int    `yaml:"max_retained"`
	Retention     string `yaml:"retention_policy"`
	AutoVerify    bool   `yaml:"auto_verify"`
	VersionSchema string `yaml:"version_schema"`
}

// RuntimeSection holds runtime path configuration.
type RuntimeSection struct {
	InstallRoot     string `yaml:"install_root"`
	SharedResources string `yaml:"shared_resources"`
	ActiveSymlink   string `yaml:"active_symlink"`
	TempDir         string `yaml:"temp_dir"`
}

// GlobalSection holds global Anvil settings.
type GlobalSection struct {
	LogLevel     string `yaml:"log_level"`
	OutputFormat string `yaml:"output_format"`
	NoColor      bool   `yaml:"no_color"`
	AutoProgress bool   `yaml:"auto_progress"`
}

// ValidateProject validates a project configuration against the canonical
// schema and project-specific constraints. It collects ALL errors before
// returning (non-fail-fast).
//
// Parameters:
//   - cfg: the project configuration to validate
//
// Returns:
//   - ValidationResult with Valid=true when all checks pass
//   - ValidationResult with Valid=false + Errors when violations are found
//
// The function does not modify the configuration or the schema.
//
// Reference: TS-P1-04, ADR-005 §3.1, ADR-005 §8.1, ADR-005 §8.3
func ValidateProject(cfg *ProjectConfig) ValidationResult {
	schema := config.GetSchema()

	// Handle nil config gracefully — validate empty config.
	var flatConfig map[string]interface{}
	if cfg != nil {
		flatConfig = flattenProjectConfig(cfg)
	} else {
		flatConfig = make(map[string]interface{})
	}

	// Run schema-level validation.
	schemaErrs := config.Validate(schema, flatConfig)

	// Run project-specific constraint validation.
	constraintErrs := validateConstraints(schema, flatConfig)

	allErrs := append(schemaErrs, constraintErrs...)

	if len(allErrs) == 0 {
		return ValidationResult{Valid: true, Errors: nil}
	}

	return ValidationResult{
		Valid:  false,
		Errors: formatAllErrors(allErrs),
	}
}

// ValidateIdentityImmutability checks that the project name in the loaded
// configuration matches the stored identity from project initialization.
//
// The stored identity is read from .anvil/project-identity.json, which is
// written once during anvil init. If the file does not exist (e.g. migrated
// projects), the check is skipped (backwards compatibility).
//
// Returns nil if the identity matches or cannot be verified, or an error
// describing the mismatch.
//
// Reference: ST-P1-03
func ValidateIdentityImmutability(root string, name string) error {
	s := NewStructure(root)
	path := s.IdentityFilePath()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Identity file does not exist — likely a project created before
			// this check was introduced. Skip immutability enforcement for
			// backwards compatibility.
			return nil
		}
		return fmt.Errorf("read identity file: %w", err)
	}

	var stored struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		return fmt.Errorf("parse identity file: %w", err)
	}

	if stored.Name != name {
		return fmt.Errorf(
			"project name has changed: stored identity is %q but configuration has %q; "+
				"the project name is immutable after initialization",
			stored.Name, name,
		)
	}

	return nil
}

// Identity returns the project identity extracted from the configuration.
//
// The identity is derived from the project name. This method provides a stable
// access point for downstream consumers (EPIC-003, EPIC-004, EPIC-005) that
// need to reference the project identity without depending on the struct layout.
//
// Returns an empty/default Identity when Project section is nil (caller should
// treat this as invalid configuration, which would be caught by validation).
//
// Reference: ST-P1-02, ADR-005 §3.1
func (cfg *ProjectConfig) Identity() config.Identity {
	if cfg.Project == nil {
		id, _ := config.NewIdentity("")
		return id
	}
	id, _ := config.NewIdentity(cfg.Project.Name)
	return id
}

// Metadata returns the project metadata (name and version) from the configuration.
//
// This method provides a stable access point for downstream consumers (EPIC-003,
// EPIC-004) that need to reference both the project name and version without
// depending on the struct layout.
//
// Returns an empty Metadata when Project section is nil (caller should treat
// this as invalid configuration, which would be caught by validation).
//
// Reference: ST-P1-03, ADR-005 §7.2
func (cfg *ProjectConfig) Metadata() config.Metadata {
	if cfg.Project == nil {
		return config.NewMetadata("", "")
	}
	return config.NewMetadata(cfg.Project.Name, cfg.Project.Version)
}

// flattenProjectConfig converts a ProjectConfig into a flat dot-notation map
// that the schema validation engine can process.
//
// Only non-nil sections are included. Nil sections produce no entries.
func flattenProjectConfig(cfg *ProjectConfig) map[string]interface{} {
	result := make(map[string]interface{})

	if cfg.Project != nil {
		result["project.name"] = cfg.Project.Name
		result["project.version"] = cfg.Project.Version
		result["project.description"] = cfg.Project.Description
	}

	if cfg.Artifact != nil {
		result["artifact.include"] = toInterfaceSlice(cfg.Artifact.Include)
		result["artifact.exclude"] = toInterfaceSlice(cfg.Artifact.Exclude)
		result["artifact.output"] = cfg.Artifact.Output
		result["artifact.manifest"] = cfg.Artifact.Manifest
	}

	if cfg.Release != nil {
		result["release.max_retained"] = cfg.Release.MaxRetained
		result["release.retention_policy"] = cfg.Release.Retention
		result["release.auto_verify"] = cfg.Release.AutoVerify
		result["release.version_schema"] = cfg.Release.VersionSchema
	}

	if cfg.Runtime != nil {
		result["runtime.install_root"] = cfg.Runtime.InstallRoot
		result["runtime.shared_resources"] = cfg.Runtime.SharedResources
		result["runtime.active_symlink"] = cfg.Runtime.ActiveSymlink
		result["runtime.temp_dir"] = cfg.Runtime.TempDir
	}

	if cfg.Global != nil {
		result["global.log_level"] = cfg.Global.LogLevel
		result["global.output_format"] = cfg.Global.OutputFormat
		result["global.no_color"] = cfg.Global.NoColor
		result["global.auto_progress"] = cfg.Global.AutoProgress
	}

	return result
}

// toInterfaceSlice converts a []string to []interface{} for schema validation.
func toInterfaceSlice(s []string) []interface{} {
	if s == nil {
		return nil
	}
	result := make([]interface{}, len(s))
	for i, v := range s {
		result[i] = v
	}
	return result
}

// validateConstraints performs project-specific constraint validation that
// goes beyond schema type checking. It validates constraints like:
//   - Non-empty strings for required fields
//   - Positive integers for numeric fields
//   - Domain-specific value constraints
//
// Reference: TS-P1-04 §7
func validateConstraints(schema config.Schema, cfg map[string]interface{}) []config.ValidationError {
	var errs []config.ValidationError

	for key, entry := range schema.Entries {
		value, present := cfg[key]
		if !present {
			continue
		}

		switch entry.Type {
		case config.TypeString:
			strVal, ok := value.(string)
			if ok {
				// Non-empty constraint: if the schema entry has no default and
				// the description suggests a non-empty value, enforce it.
				if entry.Required && strVal == "" {
					errs = append(errs, config.ValidationError{
						Key:      key,
						Expected: fmt.Sprintf("non-empty %s value", entry.Type),
						Actual:   value,
					})
				}
				continue
			}

		case config.TypeInteger:
			// Positive integer constraint for count-based fields.
			intVal, ok := toInt(value)
			if ok && intVal < 0 {
				errs = append(errs, config.ValidationError{
					Key:      key,
					Expected: fmt.Sprintf("positive %s value", entry.Type),
					Actual:   value,
				})
			}

		case config.TypeArray:
			// Glob pattern validation for artifact include/exclude patterns.
			if key == "artifact.include" || key == "artifact.exclude" {
				arrVal, ok := value.([]interface{})
				if !ok {
					continue
				}
				for i, v := range arrVal {
					strVal, ok := v.(string)
					if !ok {
						continue
					}
					if err := validateGlobPattern(strVal); err != nil {
						errs = append(errs, config.ValidationError{
							Key:      fmt.Sprintf("%s[%d]", key, i),
							Expected: "valid glob pattern",
							Actual:   strVal,
						})
					}
				}
			}
		}
	}

	return errs
}

// validateGlobPattern checks whether a pattern is a syntactically valid glob.
// It uses filepath.Match to detect syntax errors in the pattern.
//
// Reference: ST-P3-02
func validateGlobPattern(pattern string) error {
	// An empty pattern is allowed (no-op).
	if pattern == "" {
		return nil
	}

	// Use filepath.Match on a placeholder path to detect syntax errors.
	// filepath.Match returns ErrBadPattern for invalid patterns.
	_, err := filepath.Match(pattern, "placeholder")
	return err
}

// toInt attempts to convert a value to int, handling common Go types.
func toInt(value interface{}) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	default:
		return 0, false
	}
}

// formatAllErrors formats a slice of ValidationError into user-facing error
// messages with actionable guidance. All errors are formatted together
// (non-fail-fast).
//
// Each error includes:
//   - The configuration key path
//   - The invalid value (if applicable)
//   - The expected type or format
//   - The source location (when available)
//   - Actionable guidance on how to fix the issue
//
// Reference: ST-P1-04, ADR-010 §3.4
func formatAllErrors(errs []config.ValidationError) []string {
	if len(errs) == 0 {
		return nil
	}

	result := make([]string, 0, len(errs))
	for _, err := range errs {
		result = append(result, formatSingleError(err))
	}
	return result
}

// formatSingleError formats a single ValidationError with actionable guidance.
func formatSingleError(err config.ValidationError) string {
	var msg string

	if err.Source != "" {
		msg = fmt.Sprintf("%s: %s: expected %s, got %v", err.Source, err.Key, err.Expected, err.Actual)
	} else {
		msg = fmt.Sprintf("%s: expected %s, got %v", err.Key, err.Expected, err.Actual)
	}

	// Add actionable guidance based on the error context.
	guidance := buildGuidance(err)
	if guidance != "" {
		msg += "\n  " + guidance
	}

	return msg
}

// buildGuidance creates actionable guidance for a validation error.
//
// The guidance tells the user what to do to fix the error,
// not just what is wrong.
//
// Reference: ST-P1-04, ADR-010 §3.4
func buildGuidance(err config.ValidationError) string {
	switch {
	case err.Actual == nil && contains(err.Expected, "required"):
		// Missing required key.
		return fmt.Sprintf("Add the required key '%s' with a valid value.", err.Key)

	case err.Actual == nil:
		// Missing key (not marked as required in expected, but still missing).
		return fmt.Sprintf("Provide a value for key '%s'.", err.Key)

	case contains(err.Expected, "one of"):
		// Allowed values violation.
		return fmt.Sprintf("Update '%s' to one of the allowed values: %s.", err.Key, err.Expected)

	case contains(err.Expected, "non-empty"):
		// Non-empty constraint violation.
		return fmt.Sprintf("Key '%s' requires a non-empty value.", err.Key)

	case contains(err.Expected, "positive"):
		// Positive value constraint.
		return fmt.Sprintf("Key '%s' must be a positive number.", err.Key)

	default:
		// Type mismatch or other constraint.
		return fmt.Sprintf("Update '%s' to match the expected format: %s.", err.Key, err.Expected)
	}
}

// contains reports whether substr is within s.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchForward(s, substr)
}

// searchForward performs a simple substring search.
func searchForward(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// FormatProjectErrors formats a validation result into a human-readable string.
//
// When validation passes, it returns an empty string (silent pass-through).
// When validation fails, it returns all errors formatted with actionable
// guidance, separated by newlines.
//
// Reference: ST-P1-04
func FormatProjectErrors(result ValidationResult) string {
	if result.Valid || len(result.Errors) == 0 {
		return ""
	}

	var output string
	for i, err := range result.Errors {
		if i > 0 {
			output += "\n"
		}
		output += err
	}
	return output
}

// ValidateProjectConfig validates a config.ProjectConfig (minimal init config)
// against the canonical schema. This is a convenience wrapper that converts
// the minimal config into a ProjectConfig and validates it.
//
// Reference: TS-P1-04
func ValidateProjectConfig(cfg config.ProjectConfig) ValidationResult {
	return ValidateProject(&ProjectConfig{
		Project: &ProjectSection{
			Name:        cfg.Project.Name,
			Version:     cfg.Project.Version,
			Description: cfg.Project.Description,
		},
	})
}
