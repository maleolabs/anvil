// Package cmd implements the Anvil CLI commands.
//
// Tests for the post-gate registry-driven adapter resolution
// (TS-017-02-02, ADR-028 §3, §7): after the switch-over gate the
// closed-set binary scan is REMOVED — the installed view comes from the
// installed-standard records (the registry client store), the offered
// view from the registry index, and the executable resolves through the
// executable resolution contract (anvil-adapter-<name> on PATH). The
// fake-binary helpers from cmd/adapter_test.go (writeFakeAdapter,
// writeFailingAdapter) are reused, so the real Process Runner executes
// the fake scripts.
//
// Reference: TS-017-02-02, TS-016-04-01, ADR-028 §3, §7
package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"maleolabs.com/anvil/internal/registry"
)

// adapterCapabilitiesJSON returns a capabilities document declaring the
// given deployment model — the shape the reference standard executables
// answer on the capabilities command (the anvil-standard-laravel and
// anvil-standard-flutter repositories; the framework packages left the
// Core module in TS-016-01-01 and TS-016-02-01, ADR-025 §6.2).
func adapterCapabilitiesJSON(model string) string {
	return fmt.Sprintf(`{"capabilities":{"deployment_model":%q}}`, model)
}

// adapterExtensionJSON returns an extension document for the framework.
func adapterExtensionJSON(framework string) string {
	return fmt.Sprintf(`{"extension":{"framework":%q,"keys":[]}}`, framework)
}

// stubInstalledAdapter places a fake adapter binary for name into a
// stubbed CLI install directory with PATH cleared, making the
// executable-resolution seams deterministic: the fake lives at
// <dir>/anvil-adapter-<name>. Used by the recognition tests (T-004) and
// by tests that resolve the executable through the stubbed lookup. It
// returns the directory (the fake lives at <dir>/anvil-adapter-<name>).
func stubInstalledAdapter(t *testing.T, name, capabilitiesJSON, extensionJSON string) string {
	t.Helper()
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	t.Setenv("PATH", "")
	writeFakeAdapter(t, dir, "anvil-adapter-"+name, capabilitiesJSON, extensionJSON)
	return dir
}

// seedInstalledAdapter installs the post-gate "installed adapter"
// combination: the standard anvil-standard-<name> is RECORDED (the
// registry-driven installed definition) and a probe-valid fake binary is
// resolvable through the stubbed executable lookup. PATH is cleared so
// the engine's adapter template fetch (its own exec.LookPath) cannot
// find a real adapter binary on the ambient PATH — the fake binary is
// the only adapter on the system. It returns the fake binary directory.
func seedInstalledAdapter(t *testing.T, name, model string) string {
	t.Helper()
	seedInstalledStandard(t, "anvil-standard-"+name, "1.0.0")
	dir := t.TempDir()
	stubAdapterLookup(t, dir)
	t.Setenv("PATH", "")
	writeFakeAdapter(t, dir, "anvil-adapter-"+name, adapterCapabilitiesJSON(model), adapterExtensionJSON(name))
	return dir
}

// seedInstalledStandardBatch records several installed standards into
// ONE isolated store (seedInstalledStandard isolates the config dir on
// every call, so batching needs a single isolation) — pairs is
// id → version.
func seedInstalledStandardBatch(t *testing.T, pairs map[string]string) {
	t.Helper()
	isolateGlobalConfigDir(t)
	dir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		t.Fatalf("installed-standards dir: %v", err)
	}
	now := time.Now().UTC()
	for id, version := range pairs {
		rec := registry.InstalledStandardRecord{
			FormatVersion:   registry.RecordFormatVersion,
			ID:              id,
			Version:         version,
			ContractVersion: "1",
			Resolution:      registry.Resolution{Kind: registry.ResolutionKindIndex, Source: "/registry"},
			InstalledAt:     now,
			UpdatedAt:       now,
			Lifecycle:       registry.Lifecycle{State: registry.LifecycleStatePublished},
		}
		if _, _, err := registry.NewInstalledStandardStore(dir).Record(id, rec); err != nil {
			t.Fatalf("record installed standard %s: %v", id, err)
		}
	}
}

// ── Installed View: Registry Records (TS-017-02-02) ──────────────────

// TestInstalledAdapterVersions_FromRecords verifies the registry-driven
// installed definition: the adapters whose standards are RECORDED in the
// installed-standard store, mapped by the identity convention
// (anvil-standard-<name> → <name>, ADR-021 §3.1), with the recorded
// version. Records outside the convention carry no adapter identity and
// are skipped (the standard surface still lists them).
func TestInstalledAdapterVersions_FromRecords(t *testing.T) {
	seedInstalledStandardBatch(t, map[string]string{
		"anvil-standard-laravel": "1.2.0",
		"anvil-standard-flutter": "2.0.0",
		"custom-delivery":        "0.1.0", // no adapter identity
	})

	installed, err := installedAdapterVersions()
	if err != nil {
		t.Fatalf("installedAdapterVersions returned error: %v", err)
	}
	if len(installed) != 2 {
		t.Fatalf("installedAdapterVersions = %v, want 2 adapters", installed)
	}
	if installed["laravel"] != "1.2.0" {
		t.Errorf("laravel version = %q, want 1.2.0", installed["laravel"])
	}
	if installed["flutter"] != "2.0.0" {
		t.Errorf("flutter version = %q, want 2.0.0", installed["flutter"])
	}
	if _, ok := installed["custom-delivery"]; ok {
		t.Errorf("non-convention record must not map to an adapter, got: %v", installed)
	}
}

// TestInstalledAdapterVersions_EmptyStore verifies that an isolated
// machine with no records yields no installed adapters.
func TestInstalledAdapterVersions_EmptyStore(t *testing.T) {
	isolateGlobalConfigDir(t)
	got, err := installedAdapterVersions()
	if err != nil {
		t.Fatalf("installedAdapterVersions returned error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("installedAdapterVersions = %v, want empty", got)
	}
}

// ── Executable Resolution Contract (ADR-025 decision 4) ──────────────

// TestResolveAdapterExecutable_ByName verifies the post-gate executable
// resolution contract: a NAMED adapter executable
// (anvil-adapter-<name>) resolves on PATH — the contract preserved by
// ADR-025 decision 4 — and a name without an executable fails.
func TestResolveAdapterExecutable_ByName(t *testing.T) {
	dir := t.TempDir()
	writeFakeAdapter(t, dir, "anvil-adapter-rails", adapterCapabilitiesJSON("server"), adapterExtensionJSON("rails"))
	t.Setenv("PATH", dir)

	executable, err := resolveAdapterExecutable("rails")
	if err != nil {
		t.Fatalf("resolveAdapterExecutable(rails) failed: %v", err)
	}
	if want := filepath.Join(dir, "anvil-adapter-rails"); executable != want {
		t.Errorf("executable = %q, want %q", executable, want)
	}

	t.Setenv("PATH", "")
	if _, err := resolveAdapterExecutable("rails"); err == nil {
		t.Error("resolveAdapterExecutable must fail when the executable is not on PATH")
	}
}

// TestProbeAdapterDeploymentModel verifies the display-only model probe:
// a probe-valid executable yields its declared model; a missing
// executable yields "" (the registry record is the installed truth —
// a missing binary never fails the caller).
func TestProbeAdapterDeploymentModel(t *testing.T) {
	dir := t.TempDir()
	stubAdapterLookup(t, dir)
	writeFakeAdapter(t, dir, "anvil-adapter-laravel", adapterCapabilitiesJSON("server"), adapterExtensionJSON("laravel"))
	writeFailingAdapter(t, dir, "anvil-adapter-flutter") // broken binary

	if got := probeAdapterDeploymentModel(context.Background(), "laravel"); got != "server" {
		t.Errorf("probeAdapterDeploymentModel(laravel) = %q, want server", got)
	}
	if got := probeAdapterDeploymentModel(context.Background(), "flutter"); got != "" {
		t.Errorf("probeAdapterDeploymentModel(flutter) = %q, want \"\" (broken binary)", got)
	}
	if got := probeAdapterDeploymentModel(context.Background(), "missing"); got != "" {
		t.Errorf("probeAdapterDeploymentModel(missing) = %q, want \"\"", got)
	}
}

// ── Resolution Hint (ADR-026, TS-017-02-02) ──────────────────────────

// TestAdapterResolutionHint_InstalledStandards verifies the hint
// resolves the recorded delivery lifecycle standards through the
// registry client when any are installed.
func TestAdapterResolutionHint_InstalledStandards(t *testing.T) {
	seedInstalledStandard(t, "laravel-delivery", "1.2.0")

	hint := adapterResolutionHint()
	if !strings.Contains(hint, "installed delivery lifecycle standards: laravel-delivery") {
		t.Errorf("hint should list the recorded standard, got: %s", hint)
	}
	if !strings.Contains(hint, "anvil adapter install") {
		t.Errorf("hint should point at the registry adoption path, got: %s", hint)
	}
}

// TestAdapterResolutionHint_Empty verifies the hint points at the
// registry adoption path when nothing is installed.
func TestAdapterResolutionHint_Empty(t *testing.T) {
	isolateGlobalConfigDir(t)

	hint := adapterResolutionHint()
	if !strings.Contains(hint, "no adapter is installed through the registry") {
		t.Errorf("hint should report the empty registry state, got: %s", hint)
	}
	if !strings.Contains(hint, "anvil standard install") || !strings.Contains(hint, "anvil adapter install") {
		t.Errorf("hint should point at registry adoption, got: %s", hint)
	}
}

// ── Adapter List (TS-007-031, TS-017-02-02) ──────────────────────────

// TestAdapterList_RecordedAdaptersListed verifies AC-1 post-gate: an
// adapter whose standard is RECORDED appears in "anvil adapter list"
// with its recorded version and its probed deployment model.
//
// Reference: TS-007-031 AC-1, TS-017-02-02
func TestAdapterList_RecordedAdaptersListed(t *testing.T) {
	seedInstalledAdapter(t, "laravel", "server")

	_, stdout, stderr, err := executeCommand("adapter", "list")
	if err != nil {
		t.Fatalf("adapter list returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "laravel") || !strings.Contains(stdout, "server") || !strings.Contains(stdout, "1.0.0") {
		t.Errorf("stdout should list the recorded adapter with model and version, got:\n%s", stdout)
	}
}

// TestAdapterList_IgnoresUnadoptedPathBinary verifies the registry-only
// post-gate state: a bare "anvil-adapter-*" binary on PATH whose
// standard was NEVER adopted through the registry is NOT listed — the
// closed-set scan is removed, discovery is registry-driven.
//
// Reference: TS-017-02-02, ADR-028 §3, §7
func TestAdapterList_IgnoresUnadoptedPathBinary(t *testing.T) {
	isolateGlobalConfigDir(t) // no records on this machine
	pathDir := t.TempDir()
	stubAdapterInstallDirAt(t, t.TempDir())
	t.Setenv("PATH", pathDir)
	writeFakeAdapter(t, pathDir, "anvil-adapter-rails", adapterCapabilitiesJSON("server"), adapterExtensionJSON("rails"))

	_, stdout, stderr, err := executeCommand("adapter", "list")
	if err != nil {
		t.Fatalf("adapter list returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if strings.Contains(stdout, "rails") {
		t.Errorf("an unadopted PATH binary must not be discovered post-gate, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "No adapters installed.") {
		t.Errorf("stdout should report the empty installed state, got:\n%s", stdout)
	}
}

// TestAdapterList_RecordedWithBrokenBinaryStillListed verifies that a
// recorded adapter whose binary is broken stays listed (the registry
// record is the installed truth) with the model rendered as "-" — the
// closed-set probe-failure exclusion no longer applies because nothing
// is probed to define the installed set.
//
// Reference: TS-017-02-02
func TestAdapterList_RecordedWithBrokenBinaryStillListed(t *testing.T) {
	seedInstalledStandard(t, "anvil-standard-laravel", "1.0.0")
	dir := t.TempDir()
	stubAdapterLookup(t, dir)
	writeFailingAdapter(t, dir, "anvil-adapter-laravel") // broken binary

	_, stdout, stderr, err := executeCommand("adapter", "list")
	if err != nil {
		t.Fatalf("adapter list returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "laravel") {
		t.Errorf("recorded adapter must stay listed, got:\n%s", stdout)
	}
	if strings.Contains(stdout, "unknown") {
		t.Errorf("stdout must not show an 'unknown' state, got:\n%s", stdout)
	}
}

// ── Adapter Use (TS-007-033, TS-017-02-02) ───────────────────────────

// TestAdapterUse_RecordedAdapterSucceeds verifies AC-2 post-gate: "anvil
// adapter use laravel" proceeds when the adapter's standard is recorded
// and its executable answers the capabilities probe through the
// resolution contract.
//
// Reference: TS-007-033 AC-2, TS-017-02-02
func TestAdapterUse_RecordedAdapterSucceeds(t *testing.T) {
	dir := setupUseProject(t)
	seedInstalledAdapter(t, "laravel", "server")

	_, stdout, stderr, err := executeCommand("adapter", "use", "laravel")
	if err != nil {
		t.Fatalf("adapter use returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if got := projectFramework(t, dir); got != "laravel" {
		t.Errorf("project.framework = %q, want %q", got, "laravel")
	}
	if !strings.Contains(stdout, "Adapter laravel is now active") {
		t.Errorf("stdout should confirm activation, got:\n%s", stdout)
	}
}

// TestAdapterUse_RecordedWithoutBinaryRejected verifies that a recorded
// standard whose adapter binary is not resolvable is rejected with the
// actionable adoption path (the executable resolution contract is not
// the closed-set scan).
//
// Reference: TS-017-02-02, ADR-025 decision 4
func TestAdapterUse_RecordedWithoutBinaryRejected(t *testing.T) {
	dir := setupUseProject(t)
	seedInstalledStandard(t, "anvil-standard-laravel", "1.0.0")
	t.Setenv("PATH", "")

	_, _, stderr, err := executeCommand("adapter", "use", "laravel")
	if err == nil {
		t.Fatal("expected error for a recorded standard without a binary, got nil")
	}
	if !strings.Contains(stderr, "its binary is not resolvable") {
		t.Errorf("stderr should report the unresolvable binary, got: %s", stderr)
	}
	if !strings.Contains(stderr, "anvil adapter install") {
		t.Errorf("stderr should point at the registry adoption path, got: %s", stderr)
	}
	if got := projectFramework(t, dir); got != "" {
		t.Errorf("project.framework = %q, want unset after rejection", got)
	}
}

// TestAdapterUse_UnknownNameWithoutRecords verifies AC-3 post-gate:
// "anvil adapter use <unknown>" fails cleanly when no standard is
// recorded, without touching the project. The Core carries no
// known-framework catalog (ADR-026) and performs no binary scan
// (TS-017-02-02) — the hint reports the empty registry state and points
// at standard adoption.
//
// Reference: TS-007-039 AC-3, ADR-026, TS-017-02-02
func TestAdapterUse_UnknownNameWithoutRecords(t *testing.T) {
	dir := setupUseProject(t)
	isolateGlobalConfigDir(t)
	t.Setenv("PATH", "")

	_, _, stderr, err := executeCommand("adapter", "use", "rails")
	if err == nil {
		t.Fatal("expected error for undiscovered adapter, got nil")
	}
	if !strings.Contains(stderr, `unknown adapter "rails"`) {
		t.Errorf("stderr should name the unknown adapter, got: %s", stderr)
	}
	if !strings.Contains(stderr, "no adapter is installed through the registry") {
		t.Errorf("stderr should report the empty registry state, got: %s", stderr)
	}
	if !strings.Contains(stderr, "anvil standard install") {
		t.Errorf("stderr should point at standard adoption, got: %s", stderr)
	}
	if got := projectFramework(t, dir); got != "" {
		t.Errorf("project.framework = %q, want unset after rejection", got)
	}
}

// TestAdapterUse_UnknownNameListsInstalledStandards verifies the hint
// resolves the installed delivery lifecycle standards through the
// registry client (EPIC-014, ADR-026) instead of a runtime-known
// framework catalog.
//
// Reference: TS-007-039 AC-3, ADR-026
func TestAdapterUse_UnknownNameListsInstalledStandards(t *testing.T) {
	dir := setupUseProject(t)
	seedInstalledStandard(t, "laravel-delivery", "1.2.0")
	t.Setenv("PATH", "")

	_, _, stderr, err := executeCommand("adapter", "use", "rails")
	if err == nil {
		t.Fatal("expected error for undiscovered adapter, got nil")
	}
	if !strings.Contains(stderr, "installed delivery lifecycle standards: laravel-delivery") {
		t.Errorf("stderr should list the installed delivery lifecycle standard, got: %s", stderr)
	}
	if got := projectFramework(t, dir); got != "" {
		t.Errorf("project.framework = %q, want unset after rejection", got)
	}
}

// ── Adapter Inspect (TS-007-032, TS-017-02-02) ───────────────────────

// TestAdapterInspect_RecordedAdapterSucceeds verifies that inspecting a
// recorded adapter whose executable resolves works.
//
// Reference: TS-007-039 AC-1, AC-2, TS-017-02-02
func TestAdapterInspect_RecordedAdapterSucceeds(t *testing.T) {
	seedInstalledAdapter(t, "rails", "server")

	_, stdout, stderr, err := executeCommand("adapter", "inspect", "rails")
	if err != nil {
		t.Fatalf("adapter inspect returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "Adapter: rails") || !strings.Contains(stdout, "  server") {
		t.Errorf("stdout should render the recorded adapter, got:\n%s", stdout)
	}
}

// TestAdapterInspect_UnknownNameWithoutRecords verifies that inspecting
// an adapter whose standard is not recorded fails cleanly. With the
// known-framework catalog removed (ADR-026) and no installed standard
// recorded, the hint reports the empty registry state and points at
// standard adoption.
//
// Reference: TS-007-039 AC-3, ADR-026, TS-017-02-02
func TestAdapterInspect_UnknownNameWithoutRecords(t *testing.T) {
	isolateGlobalConfigDir(t)
	t.Setenv("PATH", "")

	_, _, stderr, err := executeCommand("adapter", "inspect", "rails")
	if err == nil {
		t.Fatal("expected error for undiscovered adapter, got nil")
	}
	if !strings.Contains(stderr, `unknown adapter "rails"`) {
		t.Errorf("stderr should name the unknown adapter, got: %s", stderr)
	}
	if !strings.Contains(stderr, "no adapter is installed through the registry") {
		t.Errorf("stderr should report the empty registry state, got: %s", stderr)
	}
}

// TestAdapterInspect_RecordedWithoutBinaryError verifies that a recorded
// standard whose adapter binary is missing produces an actionable error
// naming the executable and the adoption path.
//
// Reference: TS-007-032 AC-4, TS-017-02-02
func TestAdapterInspect_RecordedWithoutBinaryError(t *testing.T) {
	seedInstalledStandard(t, "anvil-standard-laravel", "1.0.0")
	t.Setenv("PATH", "")

	_, _, stderr, err := executeCommand("adapter", "inspect", "laravel")
	if err == nil {
		t.Fatal("expected error for missing executable, got nil")
	}
	if !strings.Contains(stderr, "no adapter binary found") {
		t.Errorf("stderr should report the missing binary, got: %s", stderr)
	}
	if !strings.Contains(stderr, "anvil-adapter-laravel") {
		t.Errorf("stderr should name the expected binary, got: %s", stderr)
	}
	if !strings.Contains(stderr, "anvil adapter install") {
		t.Errorf("stderr should point at the adoption path, got: %s", stderr)
	}
}

// ── F1 Security Guard: Identifier-Gated Lookup (team review F1) ──────

// placeLookPathTrap plants the F1 attack surface in root: a directory
// anvil-adapter-x containing an executable "evil" that touches marker on
// execution. If any code path resolves the framework name "x/evil"
// through exec.LookPath (Go resolves names containing '/' relative to
// the working directory), the marker file appears — proving arbitrary
// code execution from a malicious project declaration.
func placeLookPathTrap(t *testing.T, root, marker string) {
	t.Helper()
	trapDir := filepath.Join(root, "anvil-adapter-x")
	if err := os.MkdirAll(trapDir, 0755); err != nil {
		t.Fatalf("create trap directory: %v", err)
	}
	evil := filepath.Join(trapDir, "evil")
	script := fmt.Sprintf("#!/bin/sh\ntouch '%s'\n", filepath.Join(root, marker))
	if err := os.WriteFile(evil, []byte(script), 0755); err != nil {
		t.Fatalf("write trap executable: %v", err)
	}
}

// assertTrapNotExecuted fails the test when the F1 trap marker exists —
// the CWD-relative executable was resolved and executed.
func assertTrapNotExecuted(t *testing.T, root, marker string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, marker)); !os.IsNotExist(err) {
		t.Fatal("the CWD-relative trap executable was executed — path traversal into exec.LookPath (F1)")
	}
}

// TestProbeInstalledAdapter_RejectsNonIdentifierFrameworkName is the
// migration-probe regression test for team review F1 (security
// blocker): a framework name with a path separator must be rejected
// BEFORE any lookup — the CWD-relative trap executable is never
// resolved and never executed. The adapterExecutableLookup seam stays
// the REAL exec.LookPath so a regression of the guard would execute the
// trap and fail this test.
func TestProbeInstalledAdapter_RejectsNonIdentifierFrameworkName(t *testing.T) {
	root := t.TempDir()
	chdirTo(t, root)
	placeLookPathTrap(t, root, "pwned-probe")

	if exe, ok := probeInstalledAdapter(context.Background(), "x/evil"); ok || exe != "" {
		t.Fatalf("probeInstalledAdapter must reject the slash name, got ok=%v exe=%q", ok, exe)
	}
	assertTrapNotExecuted(t, root, "pwned-probe")
}

// ── F2 Resolution Order: CLI Install Dir First (team review F2) ──────

// TestResolveAdapterExecutable_CliInstallDirFirst verifies the named
// resolution order: the binary next to the CLI resolves even when the
// CLI directory is NOT on PATH (the pre-gate precedence preserved
// without a scan). The stubbed lookup is never consulted.
func TestResolveAdapterExecutable_CliInstallDirFirst(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	t.Setenv("PATH", "")
	writeFakeAdapter(t, dir, "anvil-adapter-rails", adapterCapabilitiesJSON("server"), adapterExtensionJSON("rails"))

	orig := adapterExecutableLookup
	adapterExecutableLookup = func(name string) (string, error) {
		t.Fatalf("PATH lookup must not be consulted when the CLI dir has the binary, got %q", name)
		return "", os.ErrNotExist
	}
	t.Cleanup(func() { adapterExecutableLookup = orig })

	executable, err := resolveAdapterExecutable("rails")
	if err != nil {
		t.Fatalf("resolveAdapterExecutable(rails) failed: %v", err)
	}
	if want := filepath.Join(dir, "anvil-adapter-rails"); executable != want {
		t.Errorf("executable = %q, want %q", executable, want)
	}
}

// TestResolveAdapterExecutable_CliDirNonExecutableFallsBack verifies
// that a non-executable file next to the CLI is NOT resolvable — the
// named CLI-dir check requires an executable regular file (F2), then
// PATH is consulted.
func TestResolveAdapterExecutable_CliDirNonExecutableFallsBack(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "anvil-adapter-rails"), []byte("not an adapter"), 0644); err != nil {
		t.Fatalf("write non-executable prefixed file: %v", err)
	}
	pathDir := t.TempDir()
	t.Setenv("PATH", pathDir)
	writeFakeAdapter(t, pathDir, "anvil-adapter-rails", adapterCapabilitiesJSON("server"), adapterExtensionJSON("rails"))

	executable, err := resolveAdapterExecutable("rails")
	if err != nil {
		t.Fatalf("resolveAdapterExecutable(rails) failed: %v", err)
	}
	if want := filepath.Join(pathDir, "anvil-adapter-rails"); executable != want {
		t.Errorf("executable = %q, want %q (PATH fallback)", executable, want)
	}
}

// TestAdapterInspect_CliDirBinaryNotOnPath verifies the F2 behavior at
// command level: a binary in the CLI install dir that is NOT on PATH is
// resolvable by the alias surfaces (inspect) through the record gate —
// no lookup stub, no PATH entry.
func TestAdapterInspect_CliDirBinaryNotOnPath(t *testing.T) {
	seedInstalledStandard(t, "anvil-standard-laravel", "1.0.0")
	binDir := t.TempDir()
	stubAdapterInstallDirAt(t, binDir)
	t.Setenv("PATH", "")
	writeFakeAdapter(t, binDir, "anvil-adapter-laravel", adapterCapabilitiesJSON("server"), adapterExtensionJSON("laravel"))

	_, stdout, stderr, err := executeCommand("adapter", "inspect", "laravel")
	if err != nil {
		t.Fatalf("adapter inspect returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "Adapter: laravel") || !strings.Contains(stdout, "  server") {
		t.Errorf("stdout should render the CLI-dir adapter, got:\n%s", stdout)
	}
}

// ── F5 Store Errors Surfaced (team review F5) ────────────────────────

// corruptInstalledStandardStore writes an unreadable record file into
// the isolated installed-standard store, simulating a corrupt store that
// must never be read as "nothing installed".
func corruptInstalledStandardStore(t *testing.T) {
	t.Helper()
	isolateGlobalConfigDir(t)
	dir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		t.Fatalf("installed-standards dir: %v", err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("create store dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "anvil-standard-laravel.json"), []byte("{not a record"), 0644); err != nil {
		t.Fatalf("write corrupt record: %v", err)
	}
}

// TestAdapterList_CorruptStoreWarns verifies that a corrupt
// installed-standard store is surfaced as a warning on stderr instead of
// silently reading as "nothing installed" — the listing still shows the
// empty view, but the corruption is explicit (F5).
func TestAdapterList_CorruptStoreWarns(t *testing.T) {
	corruptInstalledStandardStore(t)
	t.Setenv("PATH", "")

	_, stdout, stderr, err := executeCommand("adapter", "list")
	if err != nil {
		t.Fatalf("adapter list returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stderr, "Warning:") || !strings.Contains(stderr, "installed-standard store") {
		t.Errorf("stderr should warn about the corrupt store, got: %s", stderr)
	}
	if !strings.Contains(stdout, "No adapters installed.") {
		t.Errorf("stdout should show the empty view, got:\n%s", stdout)
	}
}

// TestAdapterUse_CorruptStoreErrors verifies that the name-resolving
// surface fails with a distinct error naming the corrupt store instead
// of "unknown adapter" — a corrupt store is never silently empty (F5).
func TestAdapterUse_CorruptStoreErrors(t *testing.T) {
	dir := setupUseProject(t)
	corruptInstalledStandardStore(t)
	t.Setenv("PATH", "")

	_, _, stderr, err := executeCommand("adapter", "use", "laravel")
	if err == nil {
		t.Fatal("expected error for a corrupt store, got nil")
	}
	if !strings.Contains(stderr, "installed-standard store") {
		t.Errorf("stderr should name the corrupt store, got: %s", stderr)
	}
	if got := projectFramework(t, dir); got != "" {
		t.Errorf("project.framework = %q, want unset after rejection", got)
	}
}

// TestAdapterInspect_CorruptStoreErrors verifies the inspect side of the
// corrupt-store handling (F5).
func TestAdapterInspect_CorruptStoreErrors(t *testing.T) {
	corruptInstalledStandardStore(t)
	t.Setenv("PATH", "")

	_, _, stderr, err := executeCommand("adapter", "inspect", "laravel")
	if err == nil {
		t.Fatal("expected error for a corrupt store, got nil")
	}
	if !strings.Contains(stderr, "installed-standard store") {
		t.Errorf("stderr should name the corrupt store, got: %s", stderr)
	}
}
