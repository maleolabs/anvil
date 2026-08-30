// Package contracts defines the stable command contract exchanged between
// the Anvil Core and Framework Adapters via subprocess invocation.
//
// Reference: TS-P7-01, TS-P7-02, EPIC-007 C-1
package contracts

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// roundTrip marshals in to JSON, unmarshals it back, and asserts the
// result equals in (field equality after round-trip). It returns the
// JSON bytes for additional assertions.
func roundTrip[T any](t *testing.T, in T) []byte {
	t.Helper()

	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("json.Marshal(%T) failed: %v", in, err)
	}

	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("json.Unmarshal(%T) failed: %v", in, err)
	}

	if !reflect.DeepEqual(in, out) {
		t.Errorf("round-trip mismatch:\n in: %#v\nout: %#v", in, out)
	}

	return data
}

// jsonKeys returns the top-level keys of a JSON document.
func jsonKeys(t *testing.T, data []byte) map[string]any {
	t.Helper()

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal into map failed: %v", err)
	}
	return m
}

// jsonNested returns the nested object under the given key.
func jsonNested(t *testing.T, data []byte, key string) map[string]any {
	t.Helper()

	nested, ok := jsonKeys(t, data)[key].(map[string]any)
	if !ok {
		t.Fatalf("JSON key %q is not an object", key)
	}
	return nested
}

// TestPhaseOperation_Constants verifies the phase operation constants and
// that the contract supports both activation and rollback operations.
//
// Reference: TS-P7-01 AC-3
func TestPhaseOperation_Constants(t *testing.T) {
	if PhaseOperationActivate != "activate" {
		t.Errorf("PhaseOperationActivate = %q, want %q", PhaseOperationActivate, "activate")
	}
	if PhaseOperationRollback != "rollback" {
		t.Errorf("PhaseOperationRollback = %q, want %q", PhaseOperationRollback, "rollback")
	}
	if PhaseOperationActivate == PhaseOperationRollback {
		t.Error("PhaseOperationActivate and PhaseOperationRollback must be distinct")
	}
}

// TestReleaseContext_RoundTrip verifies that ReleaseContext survives a
// JSON marshal/unmarshal round-trip with all fields equal.
//
// Reference: TS-P7-01, DoD: automated tests verify the contract structure
func TestReleaseContext_RoundTrip(t *testing.T) {
	in := ReleaseContext{
		ProjectID:   "acme-shop",
		ReleaseID:   "rel-20260731-01",
		Environment: "production",
		WorkingDir:  "/var/www/acme-shop/current",
	}

	roundTrip(t, in)
}

// TestReleaseContext_JSONFieldNames verifies ReleaseContext serializes to
// the expected snake_case JSON field names.
//
// Reference: TS-P7-01, DoD: automated tests verify the contract structure
func TestReleaseContext_JSONFieldNames(t *testing.T) {
	in := ReleaseContext{
		ProjectID:   "acme-shop",
		ReleaseID:   "rel-20260731-01",
		Environment: "production",
		WorkingDir:  "/var/www/acme-shop/current",
	}

	data := roundTrip(t, in)
	m := jsonKeys(t, data)

	for _, key := range []string{"project_id", "release_id", "environment", "working_dir"} {
		if _, ok := m[key]; !ok {
			t.Errorf("ReleaseContext JSON missing key %q (got %v)", key, m)
		}
	}
}

// TestReleaseContext_ProjectIDAlwaysSerialized verifies that project_id
// is the only required field: it is always serialized, while empty
// optional fields are omitted.
//
// Reference: TS-P7-01 §7
func TestReleaseContext_ProjectIDAlwaysSerialized(t *testing.T) {
	in := ReleaseContext{ProjectID: "acme-shop"}

	data := roundTrip(t, in)
	m := jsonKeys(t, data)

	if _, ok := m["project_id"]; !ok {
		t.Errorf("project_id must always be serialized (got %v)", m)
	}
	for _, key := range []string{"release_id", "environment", "working_dir"} {
		if _, ok := m[key]; ok {
			t.Errorf("empty optional field %q must be omitted (got %v)", key, m)
		}
	}
}

// TestActivationRequest_RoundTrip verifies that ActivationRequest
// survives a JSON round-trip with all fields equal.
//
// Reference: TS-P7-01, DoD: automated tests verify the contract structure
func TestActivationRequest_RoundTrip(t *testing.T) {
	in := ActivationRequest{
		Phase:     "migrate",
		Operation: PhaseOperationActivate,
		Release: ReleaseContext{
			ProjectID:   "acme-shop",
			ReleaseID:   "rel-20260731-01",
			Environment: "production",
			WorkingDir:  "/var/www/acme-shop/current",
		},
	}

	roundTrip(t, in)
}

// TestActivationRequest_ActivateOperation verifies that an activate
// request serializes to "operation": "activate" and parses back to
// PhaseOperationActivate.
//
// Reference: TS-P7-01 AC-3
func TestActivationRequest_ActivateOperation(t *testing.T) {
	in := ActivationRequest{
		Phase:     "migrate",
		Operation: PhaseOperationActivate,
		Release:   ReleaseContext{ProjectID: "acme-shop"},
	}

	data := roundTrip(t, in)
	if m := jsonKeys(t, data); m["operation"] != "activate" {
		t.Errorf("operation = %v, want %q", m["operation"], "activate")
	}

	var out ActivationRequest
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if out.Operation != PhaseOperationActivate {
		t.Errorf("parsed Operation = %q, want %q", out.Operation, PhaseOperationActivate)
	}
}

// TestActivationRequest_RollbackOperation verifies that a rollback
// request serializes to "operation": "rollback" and parses back to
// PhaseOperationRollback.
//
// Reference: TS-P7-01 AC-3
func TestActivationRequest_RollbackOperation(t *testing.T) {
	in := ActivationRequest{
		Phase:     "migrate",
		Operation: PhaseOperationRollback,
		Release:   ReleaseContext{ProjectID: "acme-shop"},
	}

	data := roundTrip(t, in)
	if m := jsonKeys(t, data); m["operation"] != "rollback" {
		t.Errorf("operation = %v, want %q", m["operation"], "rollback")
	}

	var out ActivationRequest
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if out.Operation != PhaseOperationRollback {
		t.Errorf("parsed Operation = %q, want %q", out.Operation, PhaseOperationRollback)
	}
}

// TestActivationRequest_JSONFieldNames verifies ActivationRequest and its
// nested ReleaseContext serialize to the expected snake_case field names.
//
// Reference: TS-P7-01, DoD: automated tests verify the contract structure
func TestActivationRequest_JSONFieldNames(t *testing.T) {
	in := ActivationRequest{
		Phase:     "migrate",
		Operation: PhaseOperationActivate,
		Release: ReleaseContext{
			ProjectID:   "acme-shop",
			ReleaseID:   "rel-20260731-01",
			Environment: "production",
			WorkingDir:  "/var/www/acme-shop/current",
		},
	}

	data := roundTrip(t, in)
	m := jsonKeys(t, data)
	for _, key := range []string{"phase", "operation", "release"} {
		if _, ok := m[key]; !ok {
			t.Errorf("ActivationRequest JSON missing key %q (got %v)", key, m)
		}
	}

	release := jsonNested(t, data, "release")
	for _, key := range []string{"project_id", "release_id", "environment", "working_dir"} {
		if _, ok := release[key]; !ok {
			t.Errorf("ReleaseContext JSON missing key %q (got %v)", key, release)
		}
	}
}

// TestActivationRequest_NoFrameworkSpecificContent verifies the serialized
// activation request contains no framework-specific content — the contract
// is generic and framework-agnostic.
//
// Reference: TS-P7-01 AC-2
func TestActivationRequest_NoFrameworkSpecificContent(t *testing.T) {
	in := ActivationRequest{
		Phase:     "migrate",
		Operation: PhaseOperationRollback,
		Release: ReleaseContext{
			ProjectID:   "acme-shop",
			ReleaseID:   "rel-20260731-01",
			Environment: "production",
			WorkingDir:  "/var/www/acme-shop/current",
		},
	}

	data := roundTrip(t, in)
	serialized := string(data)

	for _, forbidden := range []string{
		"laravel", "php", "artisan", "composer", "npm",
		"flutter", "dart", "rails", "django", "framework",
	} {
		if strings.Contains(serialized, forbidden) {
			t.Errorf("serialized activation request contains framework-specific content %q: %s", forbidden, serialized)
		}
	}
}

// TestActivationResult_RoundTrip verifies that ActivationResult survives a
// JSON round-trip with all fields equal.
//
// Reference: TS-P7-01, DoD: automated tests verify the contract structure
func TestActivationResult_RoundTrip(t *testing.T) {
	in := ActivationResult{
		Success: true,
		Output:  "migrations applied",
	}

	roundTrip(t, in)
}

// TestActivationResult_SuccessOmitsEmptyError verifies that a successful
// result omits the empty error field (and empty output) from the
// serialized JSON (omitempty behavior).
//
// Reference: TS-P7-01 §7
func TestActivationResult_SuccessOmitsEmptyError(t *testing.T) {
	in := ActivationResult{
		Success: true,
	}

	data := roundTrip(t, in)
	m := jsonKeys(t, data)

	if _, ok := m["error"]; ok {
		t.Errorf("success result must omit empty error field (got %v)", m)
	}
	if _, ok := m["output"]; ok {
		t.Errorf("success result must omit empty output field (got %v)", m)
	}
	if m["success"] != true {
		t.Errorf("success = %v, want true", m["success"])
	}
}

// TestActivationResult_FailureCarriesErrorDetails verifies that a failed
// result serializes and round-trips its error details.
//
// Reference: TS-P7-01 §7
func TestActivationResult_FailureCarriesErrorDetails(t *testing.T) {
	in := ActivationResult{
		Success: false,
		Output:  "migrations started",
		Error:   "migration 2026_07_31_000001 failed: table users already exists",
	}

	data := roundTrip(t, in)
	m := jsonKeys(t, data)

	if m["success"] != false {
		t.Errorf("success = %v, want false", m["success"])
	}
	if got, ok := m["error"].(string); !ok || got != in.Error {
		t.Errorf("error = %v, want %q", m["error"], in.Error)
	}
}

// TestActivationResult_JSONFieldNames verifies ActivationResult
// serializes to the expected snake_case field names.
//
// Reference: TS-P7-01, DoD: automated tests verify the contract structure
func TestActivationResult_JSONFieldNames(t *testing.T) {
	in := ActivationResult{
		Success: false,
		Output:  "migrations started",
		Error:   "migration failed",
	}

	data := roundTrip(t, in)
	m := jsonKeys(t, data)
	for _, key := range []string{"success", "output", "error"} {
		if _, ok := m[key]; !ok {
			t.Errorf("ActivationResult JSON missing key %q (got %v)", key, m)
		}
	}
}

// TestActivationResult_FailureWithoutOutput verifies that a failed result
// with no output serializes correctly: error details are carried, output
// is omitted (omitempty), and the round-trip preserves the failure state.
//
// Reference: TS-P7-01 §7
func TestActivationResult_FailureWithoutOutput(t *testing.T) {
	in := ActivationResult{
		Success: false,
		Error:   "phase startup failed",
	}

	data := roundTrip(t, in)
	m := jsonKeys(t, data)

	if m["success"] != false {
		t.Errorf("success = %v, want false", m["success"])
	}
	if _, ok := m["output"]; ok {
		t.Errorf("failed result must omit empty output field (got %v)", m)
	}
	if got, ok := m["error"].(string); !ok || got != in.Error {
		t.Errorf("error = %v, want %q", m["error"], in.Error)
	}
}

// TestActivationRequest_IgnoresUnknownFields verifies that an activation
// request with an additional unknown field unmarshals successfully and
// preserves the known fields — the additive evolution guarantee of the
// contract (005-adapter-command-contract §7).
//
// Reference: 005-adapter-command-contract §7
func TestActivationRequest_IgnoresUnknownFields(t *testing.T) {
	raw := `{
		"phase": "migrate",
		"operation": "activate",
		"release": {"project_id": "acme-shop"},
		"future_field": {"nested": true}
	}`

	var out ActivationRequest
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("json.Unmarshal with unknown field failed: %v", err)
	}
	if out.Phase != "migrate" {
		t.Errorf("Phase = %q, want %q", out.Phase, "migrate")
	}
	if out.Operation != PhaseOperationActivate {
		t.Errorf("Operation = %q, want %q", out.Operation, PhaseOperationActivate)
	}
	if out.Release.ProjectID != "acme-shop" {
		t.Errorf("Release.ProjectID = %q, want %q", out.Release.ProjectID, "acme-shop")
	}
}

// TestVerificationOutcome_IgnoresUnknownFields verifies that a
// verification outcome with an additional unknown field unmarshals
// successfully and preserves the known fields — the additive evolution
// guarantee of the contract (005-adapter-command-contract §7).
//
// Reference: 005-adapter-command-contract §7
func TestVerificationOutcome_IgnoresUnknownFields(t *testing.T) {
	raw := `{
		"name": "vendor_present",
		"passed": false,
		"details": "vendor directory not found",
		"future_field": "unknown"
	}`

	var out VerificationOutcome
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("json.Unmarshal with unknown field failed: %v", err)
	}
	if out.Name != "vendor_present" {
		t.Errorf("Name = %q, want %q", out.Name, "vendor_present")
	}
	if out.Passed {
		t.Error("Passed = true, want false")
	}
	if out.Details != "vendor directory not found" {
		t.Errorf("Details = %q, want %q", out.Details, "vendor directory not found")
	}
}

// TestVerificationCheck_RoundTrip verifies that VerificationCheck
// survives a JSON round-trip with all fields equal.
//
// Reference: TS-P7-02, DoD: automated tests verify the contract structure
func TestVerificationCheck_RoundTrip(t *testing.T) {
	in := VerificationCheck{
		Name:        "vendor_present",
		Description: "validates that the vendor directory exists in the artifact",
	}

	roundTrip(t, in)
}

// TestVerificationCheck_JSONFieldNames verifies VerificationCheck
// serializes to the expected snake_case field names.
//
// Reference: TS-P7-02, DoD: automated tests verify the contract structure
func TestVerificationCheck_JSONFieldNames(t *testing.T) {
	in := VerificationCheck{
		Name:        "vendor_present",
		Description: "validates that the vendor directory exists in the artifact",
	}

	data := roundTrip(t, in)
	m := jsonKeys(t, data)
	for _, key := range []string{"name", "description"} {
		if _, ok := m[key]; !ok {
			t.Errorf("VerificationCheck JSON missing key %q (got %v)", key, m)
		}
	}
}

// TestVerificationRequest_RoundTrip verifies that VerificationRequest
// survives a JSON round-trip with all fields equal.
//
// Reference: TS-P7-02, DoD: automated tests verify the contract structure
func TestVerificationRequest_RoundTrip(t *testing.T) {
	in := VerificationRequest{
		Check:        "vendor_present",
		ArtifactPath: "/var/anvil/artifacts/app-v1.0.0.tar.gz",
	}

	roundTrip(t, in)
}

// TestVerificationRequest_JSONFieldNames verifies VerificationRequest
// serializes to the expected snake_case field names.
//
// Reference: TS-P7-02, DoD: automated tests verify the contract structure
func TestVerificationRequest_JSONFieldNames(t *testing.T) {
	in := VerificationRequest{
		Check:        "vendor_present",
		ArtifactPath: "/var/anvil/artifacts/app-v1.0.0.tar.gz",
	}

	data := roundTrip(t, in)
	m := jsonKeys(t, data)
	for _, key := range []string{"check", "artifact_path"} {
		if _, ok := m[key]; !ok {
			t.Errorf("VerificationRequest JSON missing key %q (got %v)", key, m)
		}
	}
}

// TestVerificationRequest_NoFrameworkSpecificContent verifies the
// serialized verification request contains no framework-specific
// content — the contract is generic and framework-agnostic.
//
// Reference: TS-P7-02 AC-3
func TestVerificationRequest_NoFrameworkSpecificContent(t *testing.T) {
	in := VerificationRequest{
		Check:        "vendor_present",
		ArtifactPath: "/var/anvil/artifacts/app-v1.0.0.tar.gz",
	}

	data := roundTrip(t, in)
	serialized := string(data)

	for _, forbidden := range []string{
		"laravel", "php", "artisan", "composer", "npm",
		"flutter", "dart", "rails", "django", "framework",
	} {
		if strings.Contains(serialized, forbidden) {
			t.Errorf("serialized verification request contains framework-specific content %q: %s", forbidden, serialized)
		}
	}
}

// TestVerificationOutcome_RoundTrip verifies that VerificationOutcome
// survives a JSON round-trip for both pass and fail variants.
//
// Reference: TS-P7-02, DoD: automated tests verify the contract structure
func TestVerificationOutcome_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   VerificationOutcome
	}{
		{
			name: "pass",
			in: VerificationOutcome{
				Name:    "vendor_present",
				Passed:  true,
				Details: "vendor directory found",
			},
		},
		{
			name: "fail",
			in: VerificationOutcome{
				Name:    "vendor_present",
				Passed:  false,
				Details: "vendor directory not found in artifact",
			},
		},
		{
			name: "fail_without_details",
			in: VerificationOutcome{
				Name:   "vendor_present",
				Passed: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			roundTrip(t, tt.in)
		})
	}
}

// TestVerificationOutcome_PassVariant verifies a passing outcome
// constructs and serializes correctly, omitting empty details.
//
// Reference: TS-P7-02 AC-2
func TestVerificationOutcome_PassVariant(t *testing.T) {
	in := VerificationOutcome{
		Name:    "vendor_present",
		Passed:  true,
		Details: "vendor directory found",
	}

	data := roundTrip(t, in)
	m := jsonKeys(t, data)

	if m["name"] != "vendor_present" {
		t.Errorf("name = %v, want %q", m["name"], "vendor_present")
	}
	if m["passed"] != true {
		t.Errorf("passed = %v, want true", m["passed"])
	}
	if got, ok := m["details"].(string); !ok || got != "vendor directory found" {
		t.Errorf("details = %v, want %q", m["details"], "vendor directory found")
	}
}

// TestVerificationOutcome_FailVariant verifies a failing outcome
// constructs and round-trips with passed=false and its details.
//
// Reference: TS-P7-02 AC-2
func TestVerificationOutcome_FailVariant(t *testing.T) {
	in := VerificationOutcome{
		Name:    "vendor_present",
		Passed:  false,
		Details: "vendor directory not found in artifact",
	}

	data := roundTrip(t, in)
	m := jsonKeys(t, data)

	if m["passed"] != false {
		t.Errorf("passed = %v, want false", m["passed"])
	}
	if got, ok := m["details"].(string); !ok || got != in.Details {
		t.Errorf("details = %v, want %q", m["details"], in.Details)
	}
}

// TestVerificationOutcome_OmitsEmptyDetails verifies the omitempty
// behavior: empty details are omitted from the serialized JSON.
//
// Reference: TS-P7-02 AC-2
func TestVerificationOutcome_OmitsEmptyDetails(t *testing.T) {
	in := VerificationOutcome{
		Name:   "vendor_present",
		Passed: false,
	}

	data := roundTrip(t, in)
	if _, ok := jsonKeys(t, data)["details"]; ok {
		t.Errorf("empty details must be omitted (got %s)", string(data))
	}
}

// TestVerificationOutcome_JSONFieldNames verifies VerificationOutcome
// serializes to the expected snake_case field names aligned with
// artifact.CheckResult.
//
// Reference: TS-P7-02 AC-2, EPIC-003 §7.5
func TestVerificationOutcome_JSONFieldNames(t *testing.T) {
	in := VerificationOutcome{
		Name:    "vendor_present",
		Passed:  true,
		Details: "vendor directory found",
	}

	data := roundTrip(t, in)
	m := jsonKeys(t, data)
	for _, key := range []string{"name", "passed", "details"} {
		if _, ok := m[key]; !ok {
			t.Errorf("VerificationOutcome JSON missing key %q (got %v)", key, m)
		}
	}
}
