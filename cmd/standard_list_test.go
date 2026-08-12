// Package cmd implements the Anvil CLI commands.
//
// Tests for "anvil standard list" (TS-014-02-02): the published and
// deprecated listing with warning and removal date, the retired
// exclusion (not offered for fresh adoption, ADR-027 §3), the explicit
// invalid marker for entries that fail strict registry validation, the
// machine-readable JSON shape, and the empty/missing index states.
//
// Reference: TS-014-02-02, TS-014-01-02, TS-014-01-03, ADR-023, ADR-030
package cmd

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/registry"
)

// standardTestIndex assembles a fixture index in a temp dir with the
// published laravel release, the deprecated flutter release (with
// removal date), the retired legacy release, and the invalid docs
// release. It returns the index directory.
func standardTestIndex(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeStandardIndexDoc(t, dir, "anvil-standard-laravel/1.2.3.json",
		standardFixtureDoc("anvil-standard-laravel", "1.2.3", registry.LifecycleStatePublished, ""))
	writeStandardIndexDoc(t, dir, "anvil-standard-flutter/2.0.0.json",
		standardFixtureDoc("anvil-standard-flutter", "2.0.0", registry.LifecycleStateDeprecated, "2027-01-31T00:00:00Z"))
	writeStandardIndexDoc(t, dir, "anvil-standard-legacy/1.0.0.json",
		standardFixtureDoc("anvil-standard-legacy", "1.0.0", registry.LifecycleStateRetired, ""))
	writeStandardIndexDoc(t, dir, "anvil-standard-docs/0.9.0.json",
		standardInvalidDoc("anvil-standard-docs", "0.9.0"))
	return dir
}

// TestStandardList_ShowsDoDColumns verifies the listing shows the DoD
// columns for a published release: name, version, declared contract
// version, and capability (framework versions).
//
// Reference: TS-014-02-02 DoD
func TestStandardList_ShowsDoDColumns(t *testing.T) {
	dir := standardTestIndex(t)

	_, stdout, stderr, err := executeCommand("standard", "list", "--index", dir)
	if err != nil {
		t.Fatalf("standard list returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	for _, want := range []string{
		"Standard", "Version", "Contract", "Capability", "Status",
		"anvil-standard-laravel", "1.2.3", "1.0.0", "5.1.0, 5.2.0, 5.3.0", "published",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout should contain %q, got:\n%s", want, stdout)
		}
	}
}

// TestStandardList_DeprecatedCarriesWarningAndRemovalDate verifies a
// deprecated release is listed with its announced removal date in the
// status and its warning text in the Warnings section.
//
// Reference: TS-014-02-02 DoD, TS-014-01-03
func TestStandardList_DeprecatedCarriesWarningAndRemovalDate(t *testing.T) {
	dir := standardTestIndex(t)

	_, stdout, stderr, err := executeCommand("standard", "list", "--index", dir)
	if err != nil {
		t.Fatalf("standard list returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	if !strings.Contains(stdout, "deprecated (removal 2027-01-31T00:00:00Z)") {
		t.Errorf("stdout should show the deprecated status with the removal date, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Warnings:") {
		t.Errorf("stdout should render a Warnings section, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "this standard release is deprecated: removal announced for 2027-01-31T00:00:00Z; it will receive no updates") {
		t.Errorf("stdout should carry the deprecation warning text, got:\n%s", stdout)
	}
}

// TestStandardList_RetiredExcluded verifies retired releases are not
// offered for fresh adoption: they are excluded from the listing
// entirely (ADR-027 §3) — both in human and machine-readable output.
//
// Reference: TS-014-02-02 DoD, TS-014-01-03
func TestStandardList_RetiredExcluded(t *testing.T) {
	dir := standardTestIndex(t)

	_, stdout, stderr, err := executeCommand("standard", "list", "--index", dir)
	if err != nil {
		t.Fatalf("standard list returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if strings.Contains(stdout, "anvil-standard-legacy") {
		t.Errorf("stdout should exclude the retired standard, got:\n%s", stdout)
	}
	// The word "retired" may legitimately appear in a validation problem
	// text (the lifecycle enum lists all three states); the assertion is
	// on the retired entry's identity, not the word.

	_, stdout, stderr, err = executeCommand("standard", "list", "--index", dir, "--json")
	if err != nil {
		t.Fatalf("standard list --json returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if strings.Contains(stdout, "anvil-standard-legacy") {
		t.Errorf("JSON output should exclude the retired standard, got:\n%s", stdout)
	}
}

// TestStandardList_InvalidEntryMarked verifies an entry that fails strict
// registry validation is surfaced with an explicit invalid marker and
// its validation problem — never silently dropped (TS-014-01-02).
//
// Reference: TS-014-02-02 (product hand-off T-002)
func TestStandardList_InvalidEntryMarked(t *testing.T) {
	dir := standardTestIndex(t)

	_, stdout, stderr, err := executeCommand("standard", "list", "--index", dir)
	if err != nil {
		t.Fatalf("standard list returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	if !strings.Contains(stdout, "anvil-standard-docs") {
		t.Errorf("stdout should surface the invalid entry (not silently drop it), got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "invalid") {
		t.Errorf("stdout should mark the invalid entry, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Invalid entries (not offered for adoption):") {
		t.Errorf("stdout should render the invalid-entries section, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "lifecycle.state") || !strings.Contains(stdout, "not a supported value") {
		t.Errorf("stdout should carry the validation problem (lifecycle.state enum), got:\n%s", stdout)
	}
}

// TestStandardList_JSON verifies the machine-readable shape: every list
// entry carries id, version, contract version, capability, structured
// lifecycle state and removal date, distribution location, trust
// presence, and warnings; retired releases are excluded and invalid
// releases carry the invalid marker with the validation problem.
//
// Reference: TS-014-02-02 (PM decision: machine-readable output surface)
func TestStandardList_JSON(t *testing.T) {
	dir := standardTestIndex(t)

	_, stdout, stderr, err := executeCommand("standard", "list", "--index", dir, "--json")
	if err != nil {
		t.Fatalf("standard list --json returned unexpected error: %v (stderr: %s)", err, stderr)
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
	var entries []standardListEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("envelope data is not a standard list: %v\n%s", err, raw)
	}

	// Published + deprecated + invalid = 3 entries; retired excluded.
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3 (retired excluded): %s", len(entries), raw)
	}

	byID := make(map[string]standardListEntry)
	for _, entry := range entries {
		byID[entry.ID] = entry
	}

	laravel, ok := byID["anvil-standard-laravel"]
	if !ok {
		t.Fatalf("published laravel entry missing: %s", raw)
	}
	if laravel.Version != "1.2.3" || laravel.ContractVersion != "1.0.0" {
		t.Errorf("laravel identity/contract = %q %q, want 1.2.3 / 1.0.0", laravel.Version, laravel.ContractVersion)
	}
	if len(laravel.Capability) != 3 || laravel.Capability[0] != "5.1.0" {
		t.Errorf("laravel capability = %v, want [5.1.0 5.2.0 5.3.0]", laravel.Capability)
	}
	if laravel.Lifecycle == nil || laravel.Lifecycle.State != registry.LifecycleStatePublished {
		t.Errorf("laravel lifecycle = %+v, want published", laravel.Lifecycle)
	}
	if laravel.Lifecycle.RemovalDate != "" {
		t.Errorf("laravel removal_date = %q, want empty", laravel.Lifecycle.RemovalDate)
	}
	if laravel.Distribution == nil || laravel.Distribution.Location == "" ||
		!strings.HasPrefix(laravel.Distribution.Location, "https://") {
		t.Errorf("laravel distribution = %+v, want an https location", laravel.Distribution)
	}
	if laravel.TrustPresence == nil || !laravel.TrustPresence.ContentDigests || !laravel.TrustPresence.Attestation {
		t.Errorf("laravel trust_presence = %+v, want both present", laravel.TrustPresence)
	}
	if len(laravel.Warnings) != 0 {
		t.Errorf("laravel warnings = %v, want none", laravel.Warnings)
	}
	if laravel.Invalid {
		t.Errorf("laravel invalid = true, want false")
	}
	if laravel.Source == "" {
		t.Error("laravel source should name the index document")
	}

	flutter, ok := byID["anvil-standard-flutter"]
	if !ok {
		t.Fatalf("deprecated flutter entry missing: %s", raw)
	}
	if flutter.Lifecycle == nil || flutter.Lifecycle.State != registry.LifecycleStateDeprecated {
		t.Errorf("flutter lifecycle = %+v, want deprecated", flutter.Lifecycle)
	}
	if flutter.Lifecycle.RemovalDate != "2027-01-31T00:00:00Z" {
		t.Errorf("flutter removal_date = %q, want 2027-01-31T00:00:00Z", flutter.Lifecycle.RemovalDate)
	}
	foundWarning := false
	for _, warning := range flutter.Warnings {
		if strings.Contains(warning, "removal announced for 2027-01-31T00:00:00Z") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Errorf("flutter warnings = %v, want the deprecation warning with removal date", flutter.Warnings)
	}

	docs, ok := byID["anvil-standard-docs"]
	if !ok {
		t.Fatalf("invalid docs entry missing: %s", raw)
	}
	if !docs.Invalid {
		t.Errorf("docs invalid = false, want true (parse-failing entries are marked)")
	}
	if !strings.Contains(docs.ValidationError, "lifecycle.state") {
		t.Errorf("docs validation_error = %q, want the lifecycle.state problem", docs.ValidationError)
	}
	if docs.Lifecycle != nil {
		t.Errorf("docs lifecycle = %+v, want nil (invalid entries carry no trustworthy state)", docs.Lifecycle)
	}
	if docs.Source == "" {
		t.Error("docs source should name the index document")
	}
}

// TestStandardList_SemverOrderingAndDeterminism verifies the listing is
// ordered deterministically: standards ascending by id and each
// standard's versions ordered semantically (1.2.3 before 1.10.0, not
// lexically) — in both human and machine-readable output (product gap
// 2, CR finding 5b).
func TestStandardList_SemverOrderingAndDeterminism(t *testing.T) {
	dir := t.TempDir()
	// Insertion order deliberately does not match the display order.
	writeStandardIndexDoc(t, dir, "anvil-standard-zeta/1.10.0.json",
		standardFixtureDoc("anvil-standard-zeta", "1.10.0", registry.LifecycleStatePublished, ""))
	writeStandardIndexDoc(t, dir, "anvil-standard-zeta/1.2.3.json",
		standardFixtureDoc("anvil-standard-zeta", "1.2.3", registry.LifecycleStatePublished, ""))
	writeStandardIndexDoc(t, dir, "anvil-standard-alpha/2.0.0.json",
		standardFixtureDoc("anvil-standard-alpha", "2.0.0", registry.LifecycleStatePublished, ""))
	writeStandardIndexDoc(t, dir, "anvil-standard-alpha/1.9.0.json",
		standardFixtureDoc("anvil-standard-alpha", "1.9.0", registry.LifecycleStatePublished, ""))
	writeStandardIndexDoc(t, dir, "anvil-standard-alpha/1.10.0.json",
		standardFixtureDoc("anvil-standard-alpha", "1.10.0", registry.LifecycleStatePublished, ""))

	_, stdout, stderr, err := executeCommand("standard", "list", "--index", dir)
	if err != nil {
		t.Fatalf("standard list returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	// Expected row order: alpha 1.9.0, alpha 1.10.0, alpha 2.0.0, zeta
	// 1.2.3, zeta 1.10.0.
	order := []string{
		"anvil-standard-alpha", "1.9.0",
		"anvil-standard-alpha", "1.10.0",
		"anvil-standard-alpha", "2.0.0",
		"anvil-standard-zeta", "1.2.3",
		"anvil-standard-zeta", "1.10.0",
	}
	last := -1
	for _, want := range order {
		pos := strings.Index(stdout[last+1:], want)
		if pos < 0 {
			t.Fatalf("stdout should contain %q after the previous entry, got:\n%s", want, stdout)
		}
		pos += last + 1
		if pos < last {
			t.Errorf("stdout ordering violation: %q appears before an earlier entry (semver/lexical order broken), got:\n%s", want, stdout)
		}
		last = pos
	}
	// The zeta sequence (1.2.3 then 1.10.0) pins semver ordering: lexical
	// order would place 1.10.0 before 1.2.3.

	_, stdout, stderr, err = executeCommand("standard", "list", "--index", dir, "--json")
	if err != nil {
		t.Fatalf("standard list --json returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	var envelope output.OutputEnvelope
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	raw, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("marshal envelope data: %v", err)
	}
	var entries []standardListEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("envelope data is not a standard list: %v\n%s", err, raw)
	}
	if len(entries) != 5 {
		t.Fatalf("got %d entries, want 5: %s", len(entries), raw)
	}
	wantKeys := []string{
		"anvil-standard-alpha@1.9.0",
		"anvil-standard-alpha@1.10.0",
		"anvil-standard-alpha@2.0.0",
		"anvil-standard-zeta@1.2.3",
		"anvil-standard-zeta@1.10.0",
	}
	for i, want := range wantKeys {
		got := entries[i].ID + "@" + entries[i].Version
		if got != want {
			t.Errorf("entry %d = %q, want %q (deterministic order)", i, got, want)
		}
	}
}

// TestStandardList_ExitCodes verifies the not-found exit code contract:
// a missing index directory exits 3 (runtime not found, TS-P8-07 /
// ADR-010 §8.1), while a listing that surfaces invalid entries still
// exits 0 (data-quality signals, not command failures — CR finding 2).
func TestStandardList_ExitCodes(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	_, _, _, err := executeCommand("standard", "list", "--index", missing)
	requireExitCode(t, err, output.ExitCodeRuntime)

	// Invalid entries in the listing do not fail the command (exit 0).
	dir := standardTestIndex(t)
	_, _, stderr, err := executeCommand("standard", "list", "--index", dir)
	if err != nil {
		t.Fatalf("standard list with invalid entries should exit 0, got error: %v (stderr: %s)", err, stderr)
	}
}

// TestStandardList_InvalidContractFallback verifies the human-readable
// table shows "-" (not an empty cell) for the Contract column of invalid
// entries, which carry no trustworthy declared contract version (CR
// finding 4).
func TestStandardList_InvalidContractFallback(t *testing.T) {
	dir := standardTestIndex(t)

	_, stdout, stderr, err := executeCommand("standard", "list", "--index", dir)
	if err != nil {
		t.Fatalf("standard list returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	// The invalid row (anvil-standard-docs 0.9.0) must not contain an
	// empty cell between the version and capability cells: each table
	// cell renders at least a "-".
	re := regexp.MustCompile(`\| 0\.9\.0\s+\|\s+\|`)
	if match := re.FindString(stdout); match != "" {
		t.Errorf("invalid row has an empty contract cell (want '-'), got %q in:\n%s", match, stdout)
	}
	if !strings.Contains(stdout, "│ -") {
		t.Errorf("stdout should render '-' cells for the invalid row, got:\n%s", stdout)
	}
}

// TestStandardList_EmptyIndex verifies the empty state: an index without
// adoptable releases produces an informative message (exit 0), and the
// JSON output is an empty array.
func TestStandardList_EmptyIndex(t *testing.T) {
	_, stdout, stderr, err := executeCommand("standard", "list", "--index", t.TempDir())
	if err != nil {
		t.Fatalf("standard list returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "No standards available in the registry index.") {
		t.Errorf("stdout should contain the empty message, got:\n%s", stdout)
	}

	_, stdout, stderr, err = executeCommand("standard", "list", "--index", t.TempDir(), "--json")
	if err != nil {
		t.Fatalf("standard list --json returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	var envelope output.OutputEnvelope
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	raw, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("marshal envelope data: %v", err)
	}
	if !strings.Contains(string(raw), "[]") {
		t.Errorf("empty index JSON data should be an empty array, got: %s", raw)
	}
}

// TestStandardList_MissingIndex verifies a missing index directory
// produces an actionable error naming the path and hinting at the index
// resolution.
func TestStandardList_MissingIndex(t *testing.T) {
	missing := t.TempDir() + "/does-not-exist"

	_, _, stderr, err := executeCommand("standard", "list", "--index", missing)
	if err == nil {
		t.Fatal("expected an error for a missing index, got nil")
	}
	if !strings.Contains(stderr, "registry index not found") {
		t.Errorf("stderr should report the missing index, got: %s", stderr)
	}
	if !strings.Contains(stderr, missing) {
		t.Errorf("stderr should name the missing path %q, got: %s", missing, stderr)
	}
	if !strings.Contains(stderr, "--index") {
		t.Errorf("stderr should hint at the --index flag, got: %s", stderr)
	}
}
