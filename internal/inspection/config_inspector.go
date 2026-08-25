// Package inspection provides read-only diagnostic inspection capabilities
// for Anvil Runtime environments and configuration.
//
// Reference: TS-009-005, TS-009-006, ADR-003 §8.5, ADR-005 §7.5/§8.3
package inspection

import (
	"fmt"
	"strings"

	"maleolabs.com/anvil/internal/config"
)

// ConfigInspector performs read-only diagnostic inspections on Anvil
// configuration. It examines completeness, validity, and resolution
// conflicts without modifying any configuration state.
//
// Reference: TS-009-006, ADR-005 §7.5/§8.3, ADR-006 §5.2
type ConfigInspector struct {
	schema config.Schema
}

// NewConfigInspector creates a ConfigInspector that inspects configuration
// against the given schema.
//
// Reference: TS-009-006
func NewConfigInspector(schema config.Schema) *ConfigInspector {
	return &ConfigInspector{schema: schema}
}

// NewDefaultConfigInspector creates a ConfigInspector using the canonical
// CoreSchema.
//
// Reference: TS-009-006
func NewDefaultConfigInspector() *ConfigInspector {
	return &ConfigInspector{schema: config.CoreSchema()}
}

// InspectCompleteness checks whether all required configuration values are
// present in the resolved configuration. Uses config.EnforceRequiredValues
// to identify missing keys.
//
// Reference: TS-009-006, ADR-005 §8.3
func (ci *ConfigInspector) InspectCompleteness(resolvedConfig map[string]interface{}) InspectionCheck {
	result := config.EnforceRequiredValues(ci.schema, resolvedConfig)

	if !result.Blocked {
		// Count required keys for informative details.
		requiredCount := 0
		for _, entry := range ci.schema.Entries {
			if entry.Required {
				requiredCount++
			}
		}
		return InspectionCheck{
			Name:    "completeness",
			Passed:  true,
			Details: fmt.Sprintf("all %d required values present", requiredCount),
		}
	}

	return InspectionCheck{
		Name:    "completeness",
		Passed:  false,
		Details: fmt.Sprintf("missing required values:\n%s", result.Summary),
	}
}

// InspectValidity checks whether all configuration values conform to the
// schema (type, allowed values, format). Uses config.Validate to identify
// all violations.
//
// Reference: TS-009-006, ADR-005 §8.3
func (ci *ConfigInspector) InspectValidity(resolvedConfig map[string]interface{}) InspectionCheck {
	errs := config.Validate(ci.schema, resolvedConfig)

	if len(errs) == 0 {
		return InspectionCheck{
			Name:    "validity",
			Passed:  true,
			Details: "all configuration values are valid",
		}
	}

	var details []string
	for _, e := range errs {
		details = append(details, fmt.Sprintf("%s: expected %s, got %v", e.Key, e.Expected, e.Actual))
	}

	return InspectionCheck{
		Name:    "validity",
		Passed:  false,
		Details: fmt.Sprintf("%d invalid value(s): %s", len(errs), strings.Join(details, "; ")),
	}
}

// ResolutionConflict represents a diagnostic finding where the same
// configuration key has different values at different scope levels.
// This is informational — conflicts are expected in the config hierarchy
// (higher precedence levels intentionally override lower ones).
//
// Reference: TS-009-006, ADR-005 §7.5
type ResolutionConflict struct {
	Key    string
	Values map[config.ScopeLevel]interface{}
}

// InspectResolution examines the resolver's scope levels for configuration
// keys that have different values at different levels. This detects
// intentional overrides and potential misconfigurations for diagnostic
// purposes.
//
// For each schema key, the method checks all four scope levels. If a key
// exists at multiple levels with different values, a conflict is reported.
// Conflicts are informational — they indicate overrides in the config
// hierarchy, which is expected behavior.
//
// Reference: TS-009-006, ADR-005 §7.5
func (ci *ConfigInspector) InspectResolution(resolver *config.Resolver) InspectionCheck {
	levels := []config.ScopeLevel{
		config.ScopeGlobal,
		config.ScopeProject,
		config.ScopeEnvironment,
		config.ScopeExecution,
	}

	var conflicts []ResolutionConflict

	for key := range ci.schema.Entries {
		levelValues := make(map[config.ScopeLevel]interface{})
		for _, level := range levels {
			levelMap := resolver.LevelMap(level)
			if levelMap == nil {
				continue
			}
			if val, ok := levelMap[key]; ok {
				levelValues[level] = val
			}
		}

		// A conflict exists when the key appears at 2+ levels with
		// different values.
		if len(levelValues) >= 2 {
			if hasValueDifferences(levelValues) {
				conflicts = append(conflicts, ResolutionConflict{
					Key:    key,
					Values: levelValues,
				})
			}
		}
	}

	if len(conflicts) == 0 {
		return InspectionCheck{
			Name:    "resolution",
			Passed:  true,
			Details: "no cross-level conflicts detected",
		}
	}

	var details []string
	for _, c := range conflicts {
		var levelDetails []string
		for level, val := range c.Values {
			levelDetails = append(levelDetails, fmt.Sprintf("%s=%v", level, val))
		}
		details = append(details, fmt.Sprintf("%s: %s", c.Key, strings.Join(levelDetails, ", ")))
	}

	return InspectionCheck{
		Name:    "resolution",
		Passed:  true, // Conflicts are informational, not errors.
		Details: fmt.Sprintf("%d cross-level conflict(s) detected: %s", len(conflicts), strings.Join(details, "; ")),
	}
}

// hasValueDifferences checks whether the values in the map are not all equal.
func hasValueDifferences(values map[config.ScopeLevel]interface{}) bool {
	var first interface{}
	var firstSet bool
	for _, v := range values {
		if !firstSet {
			first = v
			firstSet = true
			continue
		}
		if fmt.Sprintf("%v", first) != fmt.Sprintf("%v", v) {
			return true
		}
	}
	return false
}

// Inspect runs all configuration inspection checks and returns a
// consolidated result. All checks are read-only — no configuration state
// is modified.
//
// Reference: TS-009-006, ADR-005 §7.5/§8.3, ADR-006 §5.2
func (ci *ConfigInspector) Inspect(resolver *config.Resolver) InspectionResult {
	result := NewInspectionResult("config")

	resolved := resolver.ResolveAll()

	checks := []InspectionCheck{
		ci.InspectCompleteness(resolved),
		ci.InspectValidity(resolved),
		ci.InspectResolution(resolver),
	}

	for _, c := range checks {
		result.AddCheck(c.Name, c.Passed, c.Details)
	}

	return *result
}
