// Tests for the capability declaration contract payloads (TS-P7-07).
package contracts

import (
	"encoding/json"
	"reflect"
	"testing"
)

// fullCapabilityDeclaration returns the fully populated capability
// declaration used across contract tests, with all three categories
// filled.
func fullCapabilityDeclaration() CapabilityDeclaration {
	return CapabilityDeclaration{
		ActivationPhases: []string{"migrate", "config_cache"},
		VerificationChecks: []VerificationCheck{
			{Name: "vendor_present", Description: "validates that the vendor directory exists in the artifact"},
			{Name: "artisan_ok", Description: "validates that artisan boots"},
		},
		DiagnosticCommands: []string{"routes:list", "config:show"},
	}
}

// TestCapabilityDeclaration_RoundTrip verifies that a fully populated
// CapabilityDeclaration survives a JSON marshal/unmarshal round-trip with
// all fields equal.
//
// Reference: TS-P7-07 AC-1, AC-2
func TestCapabilityDeclaration_RoundTrip(t *testing.T) {
	roundTrip(t, fullCapabilityDeclaration())
}

// TestCapabilityDeclaration_EmptyRoundTrip verifies that an empty
// CapabilityDeclaration survives a JSON round-trip — an adapter that
// declares no capabilities is a valid, graceful state.
//
// Reference: TS-P7-07 AC-4
func TestCapabilityDeclaration_EmptyRoundTrip(t *testing.T) {
	roundTrip(t, CapabilityDeclaration{})
}

// TestCapabilityDeclaration_EmptySerializes verifies that marshaling an
// empty declaration succeeds and unmarshals back to the same empty
// value.
//
// Reference: TS-P7-07 AC-4
func TestCapabilityDeclaration_EmptySerializes(t *testing.T) {
	data, err := json.Marshal(CapabilityDeclaration{})
	if err != nil {
		t.Fatalf("json.Marshal(empty CapabilityDeclaration) failed: %v", err)
	}

	var decl CapabilityDeclaration
	if err := json.Unmarshal(data, &decl); err != nil {
		t.Fatalf("json.Unmarshal(empty CapabilityDeclaration) failed: %v", err)
	}
	if !reflect.DeepEqual(decl, CapabilityDeclaration{}) {
		t.Errorf("empty declaration mismatch after round-trip: got %#v", decl)
	}
}

// TestCapabilityDeclaration_JSONKeysExactly verifies that the serialized
// declaration contains exactly the documented JSON keys and no
// framework-specific fields — the contract is generic and must never
// leak framework structure (ADR-009 §9.6).
//
// Reference: TS-P7-07, DoD: automated tests verify the contract structure
func TestCapabilityDeclaration_JSONKeysExactly(t *testing.T) {
	data := roundTrip(t, fullCapabilityDeclaration())

	m := jsonKeys(t, data)
	want := []string{"activation_phases", "verification_checks", "diagnostic_commands"}
	if len(m) != len(want) {
		t.Fatalf("CapabilityDeclaration JSON has %d keys (%v), want exactly %v", len(m), m, want)
	}
	for _, key := range want {
		if _, ok := m[key]; !ok {
			t.Errorf("CapabilityDeclaration JSON missing key %q (got %v)", key, m)
		}
	}
}

// TestCapabilityDeclaration_EmptyOmitsAllKeys verifies the omitempty
// behavior: an empty declaration serializes to an empty JSON object with
// no category keys.
//
// Reference: TS-P7-07 AC-4
func TestCapabilityDeclaration_EmptyOmitsAllKeys(t *testing.T) {
	data, err := json.Marshal(CapabilityDeclaration{})
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	if m := jsonKeys(t, data); len(m) != 0 {
		t.Errorf("empty declaration serialized keys %v, want none", m)
	}
}

// TestCapabilityRequest_RoundTrip verifies that CapabilityRequest
// survives a JSON round-trip with all fields equal.
//
// Reference: TS-P7-07 AC-1
func TestCapabilityRequest_RoundTrip(t *testing.T) {
	roundTrip(t, CapabilityRequest{Framework: "laravel"})
}

// TestCapabilityRequest_JSONKeysExactly verifies that CapabilityRequest
// serializes to exactly the documented JSON keys, with framework always
// present.
//
// Reference: TS-P7-07, DoD: automated tests verify the contract structure
func TestCapabilityRequest_JSONKeysExactly(t *testing.T) {
	data := roundTrip(t, CapabilityRequest{Framework: "laravel"})

	m := jsonKeys(t, data)
	if len(m) != 1 {
		t.Fatalf("CapabilityRequest JSON has %d keys (%v), want exactly [framework]", len(m), m)
	}
	if _, ok := m["framework"]; !ok {
		t.Errorf("CapabilityRequest JSON missing key %q (got %v)", "framework", m)
	}
}

// TestCapabilityResult_RoundTrip verifies that a fully populated
// CapabilityResult survives a JSON round-trip with all fields equal.
//
// Reference: TS-P7-07 AC-1, AC-2
func TestCapabilityResult_RoundTrip(t *testing.T) {
	roundTrip(t, CapabilityResult{Declaration: fullCapabilityDeclaration()})
}

// TestCapabilityResult_EmptyDeclarationRoundTrip verifies that a
// CapabilityResult wrapping an empty declaration survives a JSON
// round-trip.
//
// Reference: TS-P7-07 AC-4
func TestCapabilityResult_EmptyDeclarationRoundTrip(t *testing.T) {
	roundTrip(t, CapabilityResult{})
}

// TestCapabilityResult_JSONKeysExactly verifies that CapabilityResult
// serializes to exactly the documented JSON keys, with the declaration
// nested under "capabilities" and containing exactly the documented
// category keys.
//
// Reference: TS-P7-07, DoD: automated tests verify the contract structure
func TestCapabilityResult_JSONKeysExactly(t *testing.T) {
	data := roundTrip(t, CapabilityResult{Declaration: fullCapabilityDeclaration()})

	m := jsonKeys(t, data)
	if len(m) != 1 {
		t.Fatalf("CapabilityResult JSON has %d keys (%v), want exactly [capabilities]", len(m), m)
	}

	caps := jsonNested(t, data, "capabilities")
	for _, key := range []string{"activation_phases", "verification_checks", "diagnostic_commands"} {
		if _, ok := caps[key]; !ok {
			t.Errorf("capabilities JSON missing key %q (got %v)", key, caps)
		}
	}
	if len(caps) != 3 {
		t.Errorf("capabilities JSON has %d keys (%v), want exactly the three documented category keys", len(caps), caps)
	}
}
