// Tests for adapter compatibility validation (TS-P7-06).
package adapter

import (
	"errors"
	"strings"
	"testing"
)

// TestValidate_Table covers the compatibility matrix: in/out of range for
// Core and framework versions, missing framework version, absent
// constraints, lifecycle advancement on success and blocking on failure.
//
// Reference: TS-P7-06 AC-1..AC-6
func TestValidate_Table(t *testing.T) {
	tests := []struct {
		name             string
		coreRange        VersionRange
		fwRange          VersionRange
		coreVersion      string
		frameworkVersion string
		wantCompatible   bool
		wantCoreOK       bool
		wantFwOK         bool
		wantErrors       int
		wantStage        Stage
	}{
		{
			name:             "core_and_framework_in_range",
			coreRange:        VersionRange{Min: "1.0.0", Max: "2.0.0"},
			fwRange:          VersionRange{Min: "10.0.0", Max: "11.99.99"},
			coreVersion:      "1.4.2",
			frameworkVersion: "11.0.0",
			wantCompatible:   true,
			wantCoreOK:       true,
			wantFwOK:         true,
			wantErrors:       0,
			wantStage:        StageReady,
		},
		{
			name:             "core_below_min",
			coreRange:        VersionRange{Min: "1.0.0", Max: "2.0.0"},
			fwRange:          VersionRange{Min: "10.0.0", Max: "11.99.99"},
			coreVersion:      "0.9.0",
			frameworkVersion: "11.0.0",
			wantCompatible:   false,
			wantCoreOK:       false,
			wantFwOK:         true,
			wantErrors:       1,
			wantStage:        StageDiscovered,
		},
		{
			name:             "core_above_max",
			coreRange:        VersionRange{Min: "1.0.0", Max: "2.0.0"},
			fwRange:          VersionRange{Min: "10.0.0", Max: "11.99.99"},
			coreVersion:      "2.1.0",
			frameworkVersion: "11.0.0",
			wantCompatible:   false,
			wantCoreOK:       false,
			wantFwOK:         true,
			wantErrors:       1,
			wantStage:        StageDiscovered,
		},
		{
			name:             "core_equals_min",
			coreRange:        VersionRange{Min: "1.0.0", Max: "2.0.0"},
			fwRange:          VersionRange{Min: "10.0.0", Max: "11.99.99"},
			coreVersion:      "1.0.0",
			frameworkVersion: "11.0.0",
			wantCompatible:   true,
			wantCoreOK:       true,
			wantFwOK:         true,
			wantErrors:       0,
			wantStage:        StageReady,
		},
		{
			name:             "core_equals_max",
			coreRange:        VersionRange{Min: "1.0.0", Max: "2.0.0"},
			fwRange:          VersionRange{Min: "10.0.0", Max: "11.99.99"},
			coreVersion:      "2.0.0",
			frameworkVersion: "11.0.0",
			wantCompatible:   true,
			wantCoreOK:       true,
			wantFwOK:         true,
			wantErrors:       0,
			wantStage:        StageReady,
		},
		{
			name:             "framework_below_min",
			coreRange:        VersionRange{Min: "1.0.0", Max: "2.0.0"},
			fwRange:          VersionRange{Min: "10.0.0", Max: "11.99.99"},
			coreVersion:      "1.4.2",
			frameworkVersion: "9.0.0",
			wantCompatible:   false,
			wantCoreOK:       true,
			wantFwOK:         false,
			wantErrors:       1,
			wantStage:        StageDiscovered,
		},
		{
			name:             "framework_above_max",
			coreRange:        VersionRange{Min: "1.0.0", Max: "2.0.0"},
			fwRange:          VersionRange{Min: "10.0.0", Max: "11.99.99"},
			coreVersion:      "1.4.2",
			frameworkVersion: "12.0.0",
			wantCompatible:   false,
			wantCoreOK:       true,
			wantFwOK:         false,
			wantErrors:       1,
			wantStage:        StageDiscovered,
		},
		{
			name:             "both_out_of_range",
			coreRange:        VersionRange{Min: "1.0.0", Max: "2.0.0"},
			fwRange:          VersionRange{Min: "10.0.0", Max: "11.99.99"},
			coreVersion:      "3.0.0",
			frameworkVersion: "12.0.0",
			wantCompatible:   false,
			wantCoreOK:       false,
			wantFwOK:         false,
			wantErrors:       2,
			wantStage:        StageDiscovered,
		},
		{
			name:             "no_constraints_declared",
			coreRange:        VersionRange{},
			fwRange:          VersionRange{},
			coreVersion:      "0.1.0",
			frameworkVersion: "1.2.3",
			wantCompatible:   true,
			wantCoreOK:       true,
			wantFwOK:         true,
			wantErrors:       0,
			wantStage:        StageReady,
		},
		{
			name:             "core_min_only_in_range",
			coreRange:        VersionRange{Min: "1.0.0"},
			fwRange:          VersionRange{},
			coreVersion:      "2.5.0",
			frameworkVersion: "11.0.0",
			wantCompatible:   true,
			wantCoreOK:       true,
			wantFwOK:         true,
			wantErrors:       0,
			wantStage:        StageReady,
		},
		{
			name:             "core_min_only_out_of_range",
			coreRange:        VersionRange{Min: "1.0.0"},
			fwRange:          VersionRange{},
			coreVersion:      "0.5.0",
			frameworkVersion: "11.0.0",
			wantCompatible:   false,
			wantCoreOK:       false,
			wantFwOK:         true,
			wantErrors:       1,
			wantStage:        StageDiscovered,
		},
		{
			name:             "framework_version_empty_with_constraints",
			coreRange:        VersionRange{Min: "1.0.0", Max: "2.0.0"},
			fwRange:          VersionRange{Min: "10.0.0", Max: "11.99.99"},
			coreVersion:      "1.4.2",
			frameworkVersion: "",
			wantCompatible:   false,
			wantCoreOK:       true,
			wantFwOK:         false,
			wantErrors:       1,
			wantStage:        StageDiscovered,
		},
		{
			name:             "numeric_major_comparison",
			coreRange:        VersionRange{Min: "1.0.0", Max: "2.0.0"},
			fwRange:          VersionRange{Min: "10.0.0", Max: "11.99.99"},
			coreVersion:      "1.10.0",
			frameworkVersion: "10.9.0",
			wantCompatible:   true,
			wantCoreOK:       true,
			wantFwOK:         true,
			wantErrors:       0,
			wantStage:        StageReady,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := AdapterInfo{
				Framework:        "laravel",
				Name:             "Laravel Adapter",
				ConfigNamespace:  "framework.laravel",
				CoreVersion:      tt.coreRange,
				FrameworkVersion: tt.fwRange,
			}

			lc := NewLifecycle()
			result, err := Validate(a, lc, tt.coreVersion, tt.frameworkVersion)

			if err != nil {
				t.Fatalf("Validate returned unexpected error: %v", err)
			}
			if result.Compatible != tt.wantCompatible {
				t.Errorf("Compatible = %v, want %v", result.Compatible, tt.wantCompatible)
			}
			if result.CoreVersionCompatible != tt.wantCoreOK {
				t.Errorf("CoreVersionCompatible = %v, want %v", result.CoreVersionCompatible, tt.wantCoreOK)
			}
			if result.FrameworkVersionCompatible != tt.wantFwOK {
				t.Errorf("FrameworkVersionCompatible = %v, want %v", result.FrameworkVersionCompatible, tt.wantFwOK)
			}
			if len(result.Errors) != tt.wantErrors {
				t.Errorf("len(Errors) = %d, want %d (errors: %v)", len(result.Errors), tt.wantErrors, result.Errors)
			}
			if got := lc.Stage(); got != tt.wantStage {
				t.Errorf("lifecycle stage = %q, want %q", got, tt.wantStage)
			}
		})
	}
}

// TestValidate_MalformedCoreVersion verifies that a malformed core version
// returns an error wrapping ErrMalformedVersion.
//
// Reference: TS-P7-06 AC-3
func TestValidate_MalformedCoreVersion(t *testing.T) {
	a := AdapterInfo{
		Framework:   "laravel",
		CoreVersion: VersionRange{Min: "1.0.0", Max: "2.0.0"},
	}

	tests := []string{"1.2", "v1.2.3", "1.2.3.4", "", "one.two.three"}

	for _, version := range tests {
		t.Run(version, func(t *testing.T) {
			lc := NewLifecycle()
			_, err := Validate(a, lc, version, "11.0.0")
			if err == nil {
				t.Fatalf("Validate with core version %q succeeded, want error", version)
			}
			if !errors.Is(err, ErrMalformedVersion) {
				t.Errorf("error = %v, want wrapping ErrMalformedVersion", err)
			}
			if got := lc.Stage(); got != StageDiscovered {
				t.Errorf("lifecycle stage = %q, want %q (malformed input must not advance the lifecycle)", got, StageDiscovered)
			}
		})
	}
}

// TestValidate_MalformedFrameworkVersion verifies that a malformed
// framework version returns an error wrapping ErrMalformedVersion.
//
// Reference: TS-P7-06 AC-3
func TestValidate_MalformedFrameworkVersion(t *testing.T) {
	a := AdapterInfo{
		Framework:        "laravel",
		FrameworkVersion: VersionRange{Min: "10.0.0", Max: "11.99.99"},
	}

	lc := NewLifecycle()
	_, err := Validate(a, lc, "1.4.2", "eleven")
	if err == nil {
		t.Fatal("Validate with malformed framework version succeeded, want error")
	}
	if !errors.Is(err, ErrMalformedVersion) {
		t.Errorf("error = %v, want wrapping ErrMalformedVersion", err)
	}
}

// TestValidate_MalformedDeclaredConstraint verifies that a malformed
// constraint bound declared by the adapter returns an error wrapping
// ErrMalformedVersion — the constraint itself must be valid SemVer.
//
// Reference: TS-P7-06 AC-3
func TestValidate_MalformedDeclaredConstraint(t *testing.T) {
	a := AdapterInfo{
		Framework:   "laravel",
		CoreVersion: VersionRange{Min: "latest", Max: "2.0.0"},
	}

	lc := NewLifecycle()
	_, err := Validate(a, lc, "1.4.2", "11.0.0")
	if err == nil {
		t.Fatal("Validate with malformed declared constraint succeeded, want error")
	}
	if !errors.Is(err, ErrMalformedVersion) {
		t.Errorf("error = %v, want wrapping ErrMalformedVersion", err)
	}
}

// TestValidate_FrameworkVersionMissingMessage verifies that a missing
// framework version with declared constraints reports a clear,
// descriptive message and blocks the adapter.
//
// Reference: TS-P7-06 AC-4, AC-5
func TestValidate_FrameworkVersionMissingMessage(t *testing.T) {
	a := AdapterInfo{
		Framework:        "laravel",
		FrameworkVersion: VersionRange{Min: "10.0.0", Max: "11.99.99"},
	}

	lc := NewLifecycle()
	result, err := Validate(a, lc, "1.4.2", "")
	if err != nil {
		t.Fatalf("Validate returned unexpected error: %v", err)
	}
	if result.Compatible {
		t.Error("Compatible = true, want false")
	}
	if result.FrameworkVersionCompatible {
		t.Error("FrameworkVersionCompatible = true, want false")
	}
	if len(result.Errors) != 1 {
		t.Fatalf("len(Errors) = %d, want 1 (errors: %v)", len(result.Errors), result.Errors)
	}
	for _, wantSub := range []string{"laravel", "no framework version was provided", "10.0.0", "11.99.99"} {
		if !strings.Contains(result.Errors[0], wantSub) {
			t.Errorf("error message %q does not contain %q", result.Errors[0], wantSub)
		}
	}
}

// TestValidate_OutOfRangeMessage verifies that an out-of-range version
// reports a clear, descriptive error naming the adapter and the range.
//
// Reference: TS-P7-06 AC-4
func TestValidate_OutOfRangeMessage(t *testing.T) {
	a := AdapterInfo{
		Framework:   "laravel",
		CoreVersion: VersionRange{Min: "1.0.0", Max: "2.0.0"},
	}

	lc := NewLifecycle()
	result, err := Validate(a, lc, "3.0.0", "11.0.0")
	if err != nil {
		t.Fatalf("Validate returned unexpected error: %v", err)
	}
	if result.Compatible {
		t.Error("Compatible = true, want false")
	}
	if len(result.Errors) != 1 {
		t.Fatalf("len(Errors) = %d, want 1 (errors: %v)", len(result.Errors), result.Errors)
	}
	for _, wantSub := range []string{"laravel", "1.0.0", "2.0.0", "3.0.0"} {
		if !strings.Contains(result.Errors[0], wantSub) {
			t.Errorf("error message %q does not contain %q", result.Errors[0], wantSub)
		}
	}
}

// TestValidate_CompatibleAdvancesLifecycleToReady verifies that a
// compatible adapter advances from Discovered to Ready — validation
// advances the lifecycle state.
//
// Reference: TS-P7-06 AC-6
func TestValidate_CompatibleAdvancesLifecycleToReady(t *testing.T) {
	a := AdapterInfo{
		Framework:        "laravel",
		CoreVersion:      VersionRange{Min: "1.0.0", Max: "2.0.0"},
		FrameworkVersion: VersionRange{Min: "10.0.0", Max: "11.99.99"},
	}

	lc := NewLifecycle()
	result, err := Validate(a, lc, "1.4.2", "11.0.0")
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	if !result.Compatible {
		t.Fatalf("Compatible = false, want true (result: %#v)", result)
	}
	if got := lc.Stage(); got != StageReady {
		t.Errorf("lifecycle stage = %q, want %q", got, StageReady)
	}
}

// TestValidate_IncompatibleStaysDiscovered verifies that an incompatible
// adapter stays at StageDiscovered — blocked from participation.
//
// Reference: TS-P7-06 AC-5
func TestValidate_IncompatibleStaysDiscovered(t *testing.T) {
	a := AdapterInfo{
		Framework:   "laravel",
		CoreVersion: VersionRange{Min: "1.0.0", Max: "2.0.0"},
	}

	lc := NewLifecycle()
	result, err := Validate(a, lc, "0.1.0", "11.0.0")
	if err != nil {
		t.Fatalf("Validate returned unexpected error: %v", err)
	}
	if result.Compatible {
		t.Error("Compatible = true, want false")
	}
	if got := lc.Stage(); got != StageDiscovered {
		t.Errorf("lifecycle stage = %q, want %q (incompatible adapter must stay blocked)", got, StageDiscovered)
	}
}

// TestValidate_NoConstraintsPasses verifies that an adapter declaring no
// version constraints is compatible with any version.
//
// Reference: TS-P7-06 AC-3
func TestValidate_NoConstraintsPasses(t *testing.T) {
	a := AdapterInfo{
		Framework: "laravel",
	}

	lc := NewLifecycle()
	result, err := Validate(a, lc, "0.0.1", "0.0.1")
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	if !result.Compatible {
		t.Errorf("Compatible = false, want true (result: %#v)", result)
	}
	if !result.CoreVersionCompatible || !result.FrameworkVersionCompatible {
		t.Errorf("dimensions = (core %v, framework %v), want both true", result.CoreVersionCompatible, result.FrameworkVersionCompatible)
	}
	if got := lc.Stage(); got != StageReady {
		t.Errorf("lifecycle stage = %q, want %q", got, StageReady)
	}
}

// TestValidate_RejectsRepeatedValidation verifies that validating an
// adapter whose lifecycle already advanced past Discovered returns an
// error — a lifecycle can only be advanced by validation once.
//
// Reference: TS-P7-06 AC-6
func TestValidate_RejectsRepeatedValidation(t *testing.T) {
	a := AdapterInfo{
		Framework:   "laravel",
		CoreVersion: VersionRange{Min: "1.0.0", Max: "2.0.0"},
	}

	lc := NewLifecycle()
	if _, err := Validate(a, lc, "1.4.2", "11.0.0"); err != nil {
		t.Fatalf("first Validate failed: %v", err)
	}

	if _, err := Validate(a, lc, "1.4.2", "11.0.0"); err == nil {
		t.Fatal("second Validate on advanced lifecycle succeeded, want error")
	}
}

// TestValidate_NilLifecycleRejected verifies that Validate with a nil
// lifecycle returns an error regardless of compatibility.
//
// Reference: TS-P7-06 AC-6
func TestValidate_NilLifecycleRejected(t *testing.T) {
	a := AdapterInfo{
		Framework:   "laravel",
		CoreVersion: VersionRange{Min: "1.0.0", Max: "2.0.0"},
	}

	if _, err := Validate(a, nil, "1.4.2", "11.0.0"); err == nil {
		t.Fatal("Validate with nil lifecycle succeeded, want error")
	}
}

// TestValidate_InvertedRangeRejected verifies that an adapter declaring an
// inverted version range (Min greater than Max) fails with a descriptive
// error.
//
// Reference: TS-P7-06 AC-1
func TestValidate_InvertedRangeRejected(t *testing.T) {
	a := AdapterInfo{
		Framework:   "laravel",
		CoreVersion: VersionRange{Min: "2.0.0", Max: "1.0.0"},
	}

	lc := NewLifecycle()
	_, err := Validate(a, lc, "1.4.2", "")
	if err == nil {
		t.Fatal("Validate with inverted range succeeded, want error")
	}
	if !strings.Contains(err.Error(), "inverted") {
		t.Errorf("error %q does not mention the inverted range", err)
	}
}

// TestValidate_OverflowingVersionComponentRejected verifies that a version
// component that overflows int is reported as malformed instead of being
// silently compared.
//
// Reference: TS-P7-06 AC-1
func TestValidate_OverflowingVersionComponentRejected(t *testing.T) {
	a := AdapterInfo{
		Framework:   "laravel",
		CoreVersion: VersionRange{Min: "1.0.0", Max: "2.0.0"},
	}

	lc := NewLifecycle()
	_, err := Validate(a, lc, "99999999999999999999.0.0", "")
	if err == nil {
		t.Fatal("Validate with overflowing version component succeeded, want error")
	}
}
