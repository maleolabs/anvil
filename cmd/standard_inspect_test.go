// Package cmd implements the Anvil CLI commands.
//
// Tests for "anvil standard inspect" (TS-014-02-02): the pinned release
// inspection (found, missing version, missing standard, retired state,
// malformed entry), the multi-version overview, and the machine-readable
// shapes.
//
// Reference: TS-014-02-02, TS-014-01-02, TS-014-01-03, ADR-027 §3
package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/registry"
)

// TestStandardInspect_PinnedPublished verifies the pinned inspection of a
// published release renders the full detail: identity, declared contract
// version, capability, lifecycle state, distribution, and trust
// presence.
//
// Reference: TS-014-02-02
func TestStandardInspect_PinnedPublished(t *testing.T) {
	dir := standardTestIndex(t)

	_, stdout, stderr, err := executeCommand("standard", "inspect", "anvil-standard-laravel", "1.2.3", "--index", dir)
	if err != nil {
		t.Fatalf("standard inspect returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	for _, want := range []string{
		"Standard: anvil-standard-laravel 1.2.3",
		"Contract Version:",
		"  1.0.0",
		"Capability:",
		"  5.1.0, 5.2.0, 5.3.0",
		"Lifecycle:",
		"  published",
		"Distribution:",
		"  github-releases",
		"https://github.com/maleolabs/anvil-standard-laravel/releases/download/v1.2.3/anvil-standard-laravel.tar.gz",
		"Trust:",
		"  content digests: yes",
		"  attestation: yes",
		"Index Document:",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout should contain %q, got:\n%s", want, stdout)
		}
	}
}

// TestStandardInspect_PinnedDeprecated verifies the pinned inspection of
// a deprecated release surfaces the warning text and the announced
// removal date.
//
// Reference: TS-014-02-02, TS-014-01-03
func TestStandardInspect_PinnedDeprecated(t *testing.T) {
	dir := standardTestIndex(t)

	_, stdout, stderr, err := executeCommand("standard", "inspect", "anvil-standard-flutter", "2.0.0", "--index", dir)
	if err != nil {
		t.Fatalf("standard inspect returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	for _, want := range []string{
		"Standard: anvil-standard-flutter 2.0.0",
		"  deprecated (removal 2027-01-31T00:00:00Z)",
		"Removal Date:",
		"  2027-01-31T00:00:00Z",
		"this standard release is deprecated: removal announced for 2027-01-31T00:00:00Z; it will receive no updates",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout should contain %q, got:\n%s", want, stdout)
		}
	}
}

// TestStandardInspect_PinnedRetired verifies inspection of a retired
// release by explicit id and version shows its retired state — inspection
// is not adoption (ADR-027 §3) — and states that retired standards are
// not offered for fresh adoption.
//
// Reference: TS-014-02-02, TS-014-01-03
func TestStandardInspect_PinnedRetired(t *testing.T) {
	dir := standardTestIndex(t)

	_, stdout, stderr, err := executeCommand("standard", "inspect", "anvil-standard-legacy", "1.0.0", "--index", dir)
	if err != nil {
		t.Fatalf("standard inspect returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	if !strings.Contains(stdout, "  retired") {
		t.Errorf("stdout should show the retired state, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "not offered for fresh adoption") {
		t.Errorf("stdout should note that retired standards are not offered for fresh adoption, got:\n%s", stdout)
	}
}

// TestStandardInspect_SemverOrderingOverview verifies the overview lists
// the standard's versions in semantic order (1.2.3 before 1.10.0, not
// lexically) in both human and machine-readable output (product gap 2).
func TestStandardInspect_SemverOrderingOverview(t *testing.T) {
	dir := t.TempDir()
	writeStandardIndexDoc(t, dir, "anvil-standard-laravel/1.10.0.json",
		standardFixtureDoc("anvil-standard-laravel", "1.10.0", registry.LifecycleStatePublished, ""))
	writeStandardIndexDoc(t, dir, "anvil-standard-laravel/1.2.3.json",
		standardFixtureDoc("anvil-standard-laravel", "1.2.3", registry.LifecycleStatePublished, ""))
	writeStandardIndexDoc(t, dir, "anvil-standard-laravel/1.9.0.json",
		standardFixtureDoc("anvil-standard-laravel", "1.9.0", registry.LifecycleStatePublished, ""))

	_, stdout, stderr, err := executeCommand("standard", "inspect", "anvil-standard-laravel", "--index", dir)
	if err != nil {
		t.Fatalf("standard inspect returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	order := []string{"1.2.3", "1.9.0", "1.10.0"}
	last := -1
	for _, want := range order {
		pos := strings.Index(stdout, want)
		if pos < 0 {
			t.Fatalf("stdout should contain %q, got:\n%s", want, stdout)
		}
		if pos < last {
			t.Errorf("stdout ordering violation for %q (semver order broken), got:\n%s", want, stdout)
		}
		last = pos
	}

	_, stdout, stderr, err = executeCommand("standard", "inspect", "anvil-standard-laravel", "--index", dir, "--json")
	if err != nil {
		t.Fatalf("standard inspect --json returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	var envelope output.OutputEnvelope
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	raw, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("marshal envelope data: %v", err)
	}
	var overview standardOverviewJSON
	if err := json.Unmarshal(raw, &overview); err != nil {
		t.Fatalf("envelope data is not an overview result: %v\n%s", err, raw)
	}
	if len(overview.Versions) != 3 {
		t.Fatalf("versions = %d entries, want 3: %s", len(overview.Versions), raw)
	}
	for i, want := range order {
		if got := overview.Versions[i].Version; got != want {
			t.Errorf("version %d = %q, want %q (semver order)", i, got, want)
		}
	}
}

// TestStandardInspect_JSONErrorEnvelope_MissingStandard verifies the
// --json error path for a standard that is not in the index: the error
// is conveyed through the machine-readable envelope (status "error",
// TS-P8-05) with the standard named, AND the process still exits
// non-zero — a failure must never exit 0 (TS-019-03-02).
func TestStandardInspect_JSONErrorEnvelope_MissingStandard(t *testing.T) {
	dir := standardTestIndex(t)

	_, stdout, _, err := executeCommand("standard", "inspect", "anvil-standard-unknown", "--index", dir, "--json")
	if err == nil {
		t.Fatal("standard inspect --json should return an error for a missing standard (exit non-zero), got nil")
	}
	var envelope output.OutputEnvelope
	if jerr := json.Unmarshal([]byte(stdout), &envelope); jerr != nil {
		t.Fatalf("stdout is not a valid JSON error envelope: %v\n%s", jerr, stdout)
	}
	if envelope.Status != "error" {
		t.Errorf("envelope status = %q, want %q", envelope.Status, "error")
	}
	if !strings.Contains(envelope.Error, "anvil-standard-unknown") || !strings.Contains(envelope.Error, "not found") {
		t.Errorf("envelope error = %q, want the missing standard named", envelope.Error)
	}
}

// TestStandardInspect_JSONErrorEnvelope_MalformedEntry verifies the
// --json error path for a pinned release that fails strict registry
// validation: the error is conveyed through the machine-readable
// envelope (status "error", TS-P8-05) naming the validation problem, and
// the process still exits non-zero (TS-019-03-02).
func TestStandardInspect_JSONErrorEnvelope_MalformedEntry(t *testing.T) {
	dir := standardTestIndex(t)

	_, stdout, _, err := executeCommand("standard", "inspect", "anvil-standard-docs", "0.9.0", "--index", dir, "--json")
	if err == nil {
		t.Fatal("standard inspect --json should return an error for a malformed entry (exit non-zero), got nil")
	}
	var envelope output.OutputEnvelope
	if jerr := json.Unmarshal([]byte(stdout), &envelope); jerr != nil {
		t.Fatalf("stdout is not a valid JSON error envelope: %v\n%s", jerr, stdout)
	}
	if envelope.Status != "error" {
		t.Errorf("envelope status = %q, want %q", envelope.Status, "error")
	}
	if !strings.Contains(envelope.Error, "lifecycle.state") {
		t.Errorf("envelope error = %q, want the validation problem named", envelope.Error)
	}
}

// TestStandardInspect_ExitCodes verifies the not-found exit code
// contract: a missing standard and a missing version exit 3 (runtime
// not found, TS-P8-07 / ADR-010 §8.1), while an invalid pinned release
// is a general error (exit 1).
func TestStandardInspect_ExitCodes(t *testing.T) {
	dir := standardTestIndex(t)

	_, _, _, err := executeCommand("standard", "inspect", "anvil-standard-unknown", "--index", dir)
	requireExitCode(t, err, output.ExitCodeRuntime)

	_, _, _, err = executeCommand("standard", "inspect", "anvil-standard-laravel", "9.9.9", "--index", dir)
	requireExitCode(t, err, output.ExitCodeRuntime)

	_, _, _, err = executeCommand("standard", "inspect", "anvil-standard-docs", "0.9.0", "--index", dir)
	requireExitCode(t, err, output.ExitCodeGeneral)
}

// TestStandardInspect_MissingVersion verifies a version that is not in
// the index produces an actionable error listing the available versions.
//
// Reference: TS-014-02-02, TS-014-02-01 (actionable errors)
func TestStandardInspect_MissingVersion(t *testing.T) {
	dir := standardTestIndex(t)

	_, _, stderr, err := executeCommand("standard", "inspect", "anvil-standard-laravel", "9.9.9", "--index", dir)
	if err == nil {
		t.Fatal("expected an error for a missing version, got nil")
	}
	for _, want := range []string{
		"anvil-standard-laravel", "9.9.9", "not found",
		"1.2.3", // available versions listed
		"anvil standard inspect anvil-standard-laravel",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr should contain %q, got: %s", want, stderr)
		}
	}
}

// TestStandardInspect_MissingStandard verifies a standard id that is not
// in the index produces an actionable error.
func TestStandardInspect_MissingStandard(t *testing.T) {
	dir := standardTestIndex(t)

	_, _, stderr, err := executeCommand("standard", "inspect", "anvil-standard-unknown", "--index", dir)
	if err == nil {
		t.Fatal("expected an error for a missing standard, got nil")
	}
	for _, want := range []string{"anvil-standard-unknown", "not found", "anvil standard list"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr should contain %q, got: %s", want, stderr)
		}
	}
}

// TestStandardInspect_MalformedEntry verifies the pinned inspection of an
// entry that fails strict registry validation returns an actionable
// error identifying the entry and the validation problem.
//
// Reference: TS-014-02-02 (product hand-off T-002)
func TestStandardInspect_MalformedEntry(t *testing.T) {
	dir := standardTestIndex(t)

	_, _, stderr, err := executeCommand("standard", "inspect", "anvil-standard-docs", "0.9.0", "--index", dir)
	if err == nil {
		t.Fatal("expected an error for a malformed entry, got nil")
	}
	for _, want := range []string{
		"anvil-standard-docs", "0.9.0", "is invalid",
		"lifecycle.state", "not a supported value",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr should contain %q, got: %s", want, stderr)
		}
	}
}

// TestStandardInspect_Overview verifies the multi-version overview lists
// every release of the standard — including retired releases (inspection
// is not adoption) and invalid releases (marked with their problem).
func TestStandardInspect_Overview(t *testing.T) {
	dir := t.TempDir()
	writeStandardIndexDoc(t, dir, "anvil-standard-laravel/1.0.0.json",
		standardFixtureDoc("anvil-standard-laravel", "1.0.0", registry.LifecycleStateRetired, ""))
	writeStandardIndexDoc(t, dir, "anvil-standard-laravel/1.2.3.json",
		standardFixtureDoc("anvil-standard-laravel", "1.2.3", registry.LifecycleStatePublished, ""))
	writeStandardIndexDoc(t, dir, "anvil-standard-laravel/1.3.0.json",
		standardInvalidDoc("anvil-standard-laravel", "1.3.0"))

	_, stdout, stderr, err := executeCommand("standard", "inspect", "anvil-standard-laravel", "--index", dir)
	if err != nil {
		t.Fatalf("standard inspect returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	for _, want := range []string{
		"Standard: anvil-standard-laravel",
		"Versions (3):",
		"1.0.0", "retired",
		"1.2.3", "published",
		"1.3.0", "invalid",
		"lifecycle.state",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout should contain %q, got:\n%s", want, stdout)
		}
	}
}

// TestStandardInspect_JSON verifies the pinned machine-readable shape:
// id, version, contract version, capability, structured lifecycle state
// and removal date, distribution location, trust presence, warnings, and
// the source document.
//
// Reference: TS-014-02-02 (PM decision: machine-readable output surface)
func TestStandardInspect_JSON(t *testing.T) {
	dir := standardTestIndex(t)

	_, stdout, stderr, err := executeCommand("standard", "inspect", "anvil-standard-flutter", "2.0.0", "--index", dir, "--json")
	if err != nil {
		t.Fatalf("standard inspect --json returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	var envelope output.OutputEnvelope
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	if envelope.Status != "success" {
		t.Errorf("envelope status = %q, want %q", envelope.Status, "success")
	}

	raw, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("marshal envelope data: %v", err)
	}
	var inspect standardInspectJSON
	if err := json.Unmarshal(raw, &inspect); err != nil {
		t.Fatalf("envelope data is not an inspect result: %v\n%s", err, raw)
	}

	if inspect.ID != "anvil-standard-flutter" || inspect.Version != "2.0.0" {
		t.Errorf("identity = %q %q, want anvil-standard-flutter 2.0.0", inspect.ID, inspect.Version)
	}
	if inspect.ContractVersion != "1.0.0" {
		t.Errorf("contract_version = %q, want 1.0.0", inspect.ContractVersion)
	}
	if len(inspect.Capability) != 3 || inspect.Capability[0] != "5.1.0" {
		t.Errorf("capability = %v, want [5.1.0 5.2.0 5.3.0]", inspect.Capability)
	}
	if inspect.Lifecycle.State != registry.LifecycleStateDeprecated {
		t.Errorf("lifecycle.state = %q, want deprecated", inspect.Lifecycle.State)
	}
	if inspect.Lifecycle.RemovalDate != "2027-01-31T00:00:00Z" {
		t.Errorf("lifecycle.removal_date = %q, want 2027-01-31T00:00:00Z", inspect.Lifecycle.RemovalDate)
	}
	if inspect.Distribution.Type != registry.DistributionTypeGitHubReleases ||
		!strings.HasPrefix(inspect.Distribution.Location, "https://") {
		t.Errorf("distribution = %+v, want github-releases with https location", inspect.Distribution)
	}
	if !inspect.TrustPresence.ContentDigests || !inspect.TrustPresence.Attestation {
		t.Errorf("trust_presence = %+v, want both present", inspect.TrustPresence)
	}
	foundWarning := false
	for _, warning := range inspect.Warnings {
		if strings.Contains(warning, "removal announced for 2027-01-31T00:00:00Z") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Errorf("warnings = %v, want the deprecation warning with removal date", inspect.Warnings)
	}
	if inspect.Source == "" {
		t.Error("source should name the index document")
	}
}

// TestStandardInspect_OverviewJSON verifies the overview machine-readable
// shape: the standard id and every version as list entries, including
// retired releases and the invalid marker.
func TestStandardInspect_OverviewJSON(t *testing.T) {
	dir := t.TempDir()
	writeStandardIndexDoc(t, dir, "anvil-standard-laravel/1.0.0.json",
		standardFixtureDoc("anvil-standard-laravel", "1.0.0", registry.LifecycleStateRetired, ""))
	writeStandardIndexDoc(t, dir, "anvil-standard-laravel/1.2.3.json",
		standardFixtureDoc("anvil-standard-laravel", "1.2.3", registry.LifecycleStatePublished, ""))
	writeStandardIndexDoc(t, dir, "anvil-standard-laravel/1.3.0.json",
		standardInvalidDoc("anvil-standard-laravel", "1.3.0"))

	_, stdout, stderr, err := executeCommand("standard", "inspect", "anvil-standard-laravel", "--index", dir, "--json")
	if err != nil {
		t.Fatalf("standard inspect --json returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	var envelope output.OutputEnvelope
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}

	raw, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("marshal envelope data: %v", err)
	}
	var overview standardOverviewJSON
	if err := json.Unmarshal(raw, &overview); err != nil {
		t.Fatalf("envelope data is not an overview result: %v\n%s", err, raw)
	}
	if overview.ID != "anvil-standard-laravel" {
		t.Errorf("id = %q, want anvil-standard-laravel", overview.ID)
	}
	if len(overview.Versions) != 3 {
		t.Fatalf("versions = %d entries, want 3 (retired and invalid included in inspection): %s", len(overview.Versions), raw)
	}

	states := make(map[string]standardListEntry)
	for _, entry := range overview.Versions {
		states[entry.Version] = entry
	}
	if v := states["1.0.0"]; v.Lifecycle == nil || v.Lifecycle.State != registry.LifecycleStateRetired {
		t.Errorf("version 1.0.0 lifecycle = %+v, want retired", v.Lifecycle)
	}
	if v := states["1.3.0"]; !v.Invalid || !strings.Contains(v.ValidationError, "lifecycle.state") {
		t.Errorf("version 1.3.0 invalid = %t, validation_error = %q, want the invalid marker with the problem", v.Invalid, v.ValidationError)
	}
}
