// Package inspection provides read-only diagnostic inspection capabilities
// for Anvil Runtime environments and configuration. Inspectors examine the
// current state without modifying any resources.
//
// Reference: TS-009-005, TS-009-006, ADR-003 §8.5, ADR-005 §7.5/§8.3
package inspection

// InspectionCheck represents the result of a single inspection check.
// Each check has a descriptive name, a pass/fail status, and human-readable
// details explaining the outcome.
//
// Reference: TS-009-005, ADR-006 §5.2
type InspectionCheck struct {
	// Name identifies the inspection check (e.g. "active_symlink").
	Name string `json:"name"`

	// Passed indicates whether the check succeeded.
	Passed bool `json:"passed"`

	// Details provides a human-readable explanation of the check outcome.
	Details string `json:"details"`
}

// InspectionResult represents the consolidated inspection result for a
// component. It aggregates multiple individual checks into a single
// pass/fail assessment.
//
// Reference: TS-009-005, TS-009-006, ADR-006 §5.2
type InspectionResult struct {
	// Component identifies the inspected component (e.g. "runtime", "config").
	Component string `json:"component"`

	// Checks contains all individual inspection check results.
	Checks []InspectionCheck `json:"checks"`

	// Passed indicates whether ALL checks passed. False if any check failed.
	Passed bool `json:"passed"`
}

// NewInspectionResult creates a new InspectionResult for the given component
// with an empty checks slice. The result starts as passed; individual checks
// may set it to false.
//
// Reference: TS-009-005, ADR-006 §5.2
func NewInspectionResult(component string) *InspectionResult {
	return &InspectionResult{
		Component: component,
		Checks:    make([]InspectionCheck, 0),
		Passed:    true,
	}
}

// AddCheck appends an inspection check to the result. If the check has
// Passed=false, the overall result is set to false.
//
// Reference: TS-009-005, ADR-006 §5.2
func (r *InspectionResult) AddCheck(name string, passed bool, details string) {
	r.Checks = append(r.Checks, InspectionCheck{
		Name:    name,
		Passed:  passed,
		Details: details,
	})
	if !passed {
		r.Passed = false
	}
}
