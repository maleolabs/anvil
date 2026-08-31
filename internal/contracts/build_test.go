// Tests for the build contract payloads (TS-P7-14).
package contracts

import (
	"strings"
	"testing"
)

// TestBuildRequest_RoundTrip verifies that BuildRequest survives a JSON
// marshal/unmarshal round-trip with all fields equal.
//
// Reference: TS-P7-14, DoD: automated tests verify the contract structure
func TestBuildRequest_RoundTrip(t *testing.T) {
	in := BuildRequest{
		WorkingDir: "/var/www/acme-shop/releases/rel-1",
	}

	roundTrip(t, in)
}

// TestBuildRequest_TargetsStrictRoundTrip verifies that the additive
// Targets and Strict fields (TS-007-041) survive a JSON round-trip.
//
// Reference: TS-007-041, ADR-018
func TestBuildRequest_TargetsStrictRoundTrip(t *testing.T) {
	in := BuildRequest{
		WorkingDir: "/var/www/acme-shop/releases/rel-1",
		Targets:    []string{"web", "apk"},
		Strict:     true,
	}

	roundTrip(t, in)
}

// TestBuildRequest_EmptyRoundTrip verifies that an empty BuildRequest
// survives a JSON round-trip — the adapter then runs the build phases in
// its current working directory.
//
// Reference: TS-P7-14
func TestBuildRequest_EmptyRoundTrip(t *testing.T) {
	roundTrip(t, BuildRequest{})
}

// TestBuildRequest_JSONFieldNames verifies BuildRequest serializes to the
// expected snake_case JSON field names, with an empty working_dir
// omitted (omitempty).
//
// Reference: TS-P7-14, DoD: automated tests verify the contract structure
func TestBuildRequest_JSONFieldNames(t *testing.T) {
	in := BuildRequest{WorkingDir: "/var/www/acme-shop/releases/rel-1"}

	data := roundTrip(t, in)
	m := jsonKeys(t, data)
	if _, ok := m["working_dir"]; !ok {
		t.Errorf("BuildRequest JSON missing key %q (got %v)", "working_dir", m)
	}

	empty := roundTrip(t, BuildRequest{})
	if m := jsonKeys(t, empty); len(m) != 0 {
		t.Errorf("empty BuildRequest serialized keys %v, want none", m)
	}
}

// TestBuildRequest_TargetsStrictJSONFieldNames verifies that the
// additive Targets/Strict fields serialize to the expected snake_case
// JSON field names and are omitted when unset — an old adapter receiving
// an empty BuildRequest sees exactly the pre-TS-007-041 payload
// (additive compatibility, 005 §8).
//
// Reference: TS-007-041, 005-adapter-command-contract §8
func TestBuildRequest_TargetsStrictJSONFieldNames(t *testing.T) {
	in := BuildRequest{
		WorkingDir: "/var/www/acme-shop/releases/rel-1",
		Targets:    []string{"web", "apk"},
		Strict:     true,
	}

	data := roundTrip(t, in)
	m := jsonKeys(t, data)
	for _, key := range []string{"working_dir", "targets", "strict"} {
		if _, ok := m[key]; !ok {
			t.Errorf("BuildRequest JSON missing key %q (got %v)", key, m)
		}
	}

	// Unset fields must be omitted entirely (omitempty) so old adapters
	// behave exactly as before the additive fields existed.
	empty := roundTrip(t, BuildRequest{WorkingDir: "/var/www/app"})
	if m := jsonKeys(t, empty); len(m) != 1 {
		t.Errorf("BuildRequest without Targets/Strict serialized keys %v, want only working_dir", m)
	}
}

// TestBuildPhaseResult_RoundTrip verifies that BuildPhaseResult survives
// a JSON round-trip for both the success and the failure variant.
//
// Reference: TS-P7-14 AC-2, DoD: automated tests verify the contract
// structure
func TestBuildPhaseResult_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   BuildPhaseResult
	}{
		{
			name: "success",
			in: BuildPhaseResult{
				Phase:   "composer",
				Success: true,
				Output:  "Installing dependencies (lock file)...",
			},
		},
		{
			name: "failure",
			in: BuildPhaseResult{
				Phase:   "npm",
				Success: false,
				Output:  "npm run build started",
				Error:   "npm run build failed: error:0308010C:digital envelope routines::unsupported",
			},
		},
		{
			name: "failure_without_output",
			in: BuildPhaseResult{
				Phase:   "config_cache",
				Success: false,
				Error:   "config cache failed: permission denied",
			},
		},
		{
			name: "skipped",
			in: BuildPhaseResult{
				Phase:   "ios",
				Success: true,
				Skipped: true,
				Warning: `target "ios" is not supported on platform "linux" (supported platforms: darwin)`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			roundTrip(t, tt.in)
		})
	}
}

// TestBuildPhaseResult_JSONFieldNames verifies BuildPhaseResult
// serializes to the expected snake_case JSON field names.
//
// Reference: TS-P7-14, DoD: automated tests verify the contract structure
func TestBuildPhaseResult_JSONFieldNames(t *testing.T) {
	in := BuildPhaseResult{
		Phase:   "composer",
		Success: true,
		Output:  "ok",
	}

	data := roundTrip(t, in)
	m := jsonKeys(t, data)
	for _, key := range []string{"phase", "success", "output"} {
		if _, ok := m[key]; !ok {
			t.Errorf("BuildPhaseResult JSON missing key %q (got %v)", key, m)
		}
	}
	if _, ok := m["error"]; ok {
		t.Errorf("success result must omit empty error field (got %v)", m)
	}
}

// TestBuildPhaseResult_SuccessOmitsEmptyError verifies the omitempty
// behavior: a successful phase result omits the empty output and error
// fields.
//
// Reference: TS-P7-14 §7
func TestBuildPhaseResult_SuccessOmitsEmptyError(t *testing.T) {
	in := BuildPhaseResult{
		Phase:   "route_cache",
		Success: true,
	}

	data := roundTrip(t, in)
	m := jsonKeys(t, data)
	if _, ok := m["output"]; ok {
		t.Errorf("success result must omit empty output field (got %v)", m)
	}
	if _, ok := m["error"]; ok {
		t.Errorf("success result must omit empty error field (got %v)", m)
	}
	if m["phase"] != "route_cache" {
		t.Errorf("phase = %v, want %q", m["phase"], "route_cache")
	}
	if m["success"] != true {
		t.Errorf("success = %v, want true", m["success"])
	}
}

// TestBuildPhaseResult_SkipOmitEmpty verifies the additive Skipped and
// Warning fields (TS-007-041): a phase that ran omits both, and a
// skipped phase carries them — an old Core receiving the skip fields
// ignores them and still sees a successful phase (additive
// compatibility, 005 §8).
//
// Reference: TS-007-041, 005-adapter-command-contract §8
func TestBuildPhaseResult_SkipOmitEmpty(t *testing.T) {
	ran := BuildPhaseResult{Phase: "composer", Success: true}
	data := roundTrip(t, ran)
	m := jsonKeys(t, data)
	if _, ok := m["skipped"]; ok {
		t.Errorf("executed phase must omit skipped field (got %v)", m)
	}
	if _, ok := m["warning"]; ok {
		t.Errorf("executed phase must omit warning field (got %v)", m)
	}

	skipped := BuildPhaseResult{
		Phase:   "ios",
		Success: true,
		Skipped: true,
		Warning: `target "ios" is not supported on platform "linux"`,
	}
	data = roundTrip(t, skipped)
	m = jsonKeys(t, data)
	for _, key := range []string{"phase", "success", "skipped", "warning"} {
		if _, ok := m[key]; !ok {
			t.Errorf("skipped phase JSON missing key %q (got %v)", key, m)
		}
	}
}

// TestBuildResult_RoundTrip verifies that BuildResult survives a JSON
// round-trip for both the all-success and the partial-failure variant.
//
// Reference: TS-P7-14 AC-2, DoD: automated tests verify the contract
// structure
func TestBuildResult_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   BuildResult
	}{
		{
			name: "all_success",
			in: BuildResult{
				Success: true,
				Phases: []BuildPhaseResult{
					{Phase: "composer", Success: true, Output: "dependencies installed"},
					{Phase: "npm", Success: true, Output: "assets built"},
				},
			},
		},
		{
			name: "first_failure",
			in: BuildResult{
				Success: false,
				Phases: []BuildPhaseResult{
					{Phase: "composer", Success: false, Error: "composer install failed: no such file composer.json"},
				},
			},
		},
		{
			name: "empty",
			in:   BuildResult{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			roundTrip(t, tt.in)
		})
	}
}

// TestBuildResult_JSONFieldNames verifies BuildResult serializes to the
// expected snake_case JSON field names, with success always present.
//
// Reference: TS-P7-14, DoD: automated tests verify the contract structure
func TestBuildResult_JSONFieldNames(t *testing.T) {
	in := BuildResult{
		Success: true,
		Phases:  []BuildPhaseResult{{Phase: "composer", Success: true}},
	}

	data := roundTrip(t, in)
	m := jsonKeys(t, data)
	for _, key := range []string{"phases", "success"} {
		if _, ok := m[key]; !ok {
			t.Errorf("BuildResult JSON missing key %q (got %v)", key, m)
		}
	}
}

// TestBuildResult_NoFrameworkSpecificContent verifies the serialized
// build request contains no framework-specific content — the contract is
// generic and framework-agnostic (ADR-009 §9.6).
//
// Reference: TS-P7-14 AC-1
func TestBuildRequest_NoFrameworkSpecificContent(t *testing.T) {
	in := BuildRequest{WorkingDir: "/var/www/acme-shop/releases/rel-1"}

	data := roundTrip(t, in)
	serialized := string(data)

	for _, forbidden := range []string{
		"laravel", "php", "artisan", "composer", "npm",
		"flutter", "dart", "rails", "django", "framework",
	} {
		if strings.Contains(serialized, forbidden) {
			t.Errorf("serialized build request contains framework-specific content %q: %s", forbidden, serialized)
		}
	}
}
