package inspection

import (
	"testing"
)

// TestNewInspectionResult verifies that NewInspectionResult creates a result
// with the correct component name, empty checks, and passed=true.
//
// Reference: TS-009-005
func TestNewInspectionResult(t *testing.T) {
	result := NewInspectionResult("runtime")

	if result.Component != "runtime" {
		t.Errorf("Component = %q, want %q", result.Component, "runtime")
	}
	if !result.Passed {
		t.Error("new result.Passed = false, want true")
	}
	if len(result.Checks) != 0 {
		t.Errorf("len(Checks) = %d, want 0", len(result.Checks))
	}
	if result.Checks == nil {
		t.Error("Checks is nil, want empty slice")
	}
}

// TestInspectionResult_AddCheck_Passing verifies that adding a passing check
// keeps the result as passed.
//
// Reference: TS-009-005
func TestInspectionResult_AddCheck_Passing(t *testing.T) {
	result := NewInspectionResult("runtime")
	result.AddCheck("directories", true, "all directories exist")

	if !result.Passed {
		t.Error("result.Passed = false after passing check, want true")
	}
	if len(result.Checks) != 1 {
		t.Fatalf("len(Checks) = %d, want 1", len(result.Checks))
	}

	c := result.Checks[0]
	if c.Name != "directories" {
		t.Errorf("check.Name = %q, want %q", c.Name, "directories")
	}
	if !c.Passed {
		t.Error("check.Passed = false, want true")
	}
	if c.Details != "all directories exist" {
		t.Errorf("check.Details = %q, want %q", c.Details, "all directories exist")
	}
}

// TestInspectionResult_AddCheck_Failing verifies that adding a failing check
// sets the overall result to failed.
//
// Reference: TS-009-005
func TestInspectionResult_AddCheck_Failing(t *testing.T) {
	result := NewInspectionResult("runtime")
	result.AddCheck("symlink", false, "active symlink does not exist")

	if result.Passed {
		t.Error("result.Passed = true after failing check, want false")
	}
	if len(result.Checks) != 1 {
		t.Fatalf("len(Checks) = %d, want 1", len(result.Checks))
	}

	c := result.Checks[0]
	if c.Passed {
		t.Error("check.Passed = true, want false")
	}
	if c.Details != "active symlink does not exist" {
		t.Errorf("check.Details = %q, want %q", c.Details, "active symlink does not exist")
	}
}

// TestInspectionResult_AddCheck_MixedResults verifies that a mix of passing
// and failing checks results in overall failure.
//
// Reference: TS-009-005
func TestInspectionResult_AddCheck_MixedResults(t *testing.T) {
	result := NewInspectionResult("runtime")
	result.AddCheck("directories", true, "all directories exist")
	result.AddCheck("symlink", false, "symlink missing")
	result.AddCheck("config", true, "config valid")

	if result.Passed {
		t.Error("result.Passed = true with mixed results, want false")
	}
	if len(result.Checks) != 3 {
		t.Errorf("len(Checks) = %d, want 3", len(result.Checks))
	}

	// First check passed.
	if !result.Checks[0].Passed {
		t.Error("Checks[0].Passed = false, want true")
	}
	// Second check failed.
	if result.Checks[1].Passed {
		t.Error("Checks[1].Passed = true, want false")
	}
	// Third check passed.
	if !result.Checks[2].Passed {
		t.Error("Checks[2].Passed = false, want true")
	}
}

// TestInspectionResult_AllPassing verifies that all passing checks produce
// an overall passing result.
//
// Reference: TS-009-005
func TestInspectionResult_AllPassing(t *testing.T) {
	result := NewInspectionResult("config")
	result.AddCheck("completeness", true, "all required values present")
	result.AddCheck("validity", true, "all values valid")
	result.AddCheck("resolution", true, "no conflicts")

	if !result.Passed {
		t.Error("result.Passed = false with all passing, want true")
	}
}

// TestInspectionResult_AllFailing verifies that all failing checks produce
// an overall failing result.
//
// Reference: TS-009-005
func TestInspectionResult_AllFailing(t *testing.T) {
	result := NewInspectionResult("config")
	result.AddCheck("completeness", false, "missing keys")
	result.AddCheck("validity", false, "invalid values")

	if result.Passed {
		t.Error("result.Passed = true with all failing, want false")
	}
}

// TestInspectionResult_EmptyChecks verifies that a result with no checks
// remains passed.
//
// Reference: TS-009-005
func TestInspectionResult_EmptyChecks(t *testing.T) {
	result := NewInspectionResult("runtime")

	if !result.Passed {
		t.Error("result.Passed = false with no checks, want true")
	}
}
