// Package cmd implements the Anvil CLI commands.
//
// Tests for PATH-based adapter discovery (TS-007-039): PATH scanning
// (executability, unreadable directories, duplicate entries, relative
// entries) and probe-based validation of the adapter set (foreign or
// broken "anvil-adapter-*" binaries are excluded). The fake-binary
// helpers from cmd/adapter_test.go (writeFakeAdapter, writeFailingAdapter)
// are reused, so the real Process Runner executes the fake scripts.
//
// Reference: TS-007-039
package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// adapterCapabilitiesJSON returns a capabilities document declaring the
// given deployment model — the shape the reference adapters answer on
// the capabilities command (internal/laravel, internal/flutter).
func adapterCapabilitiesJSON(model string) string {
	return fmt.Sprintf(`{"capabilities":{"deployment_model":%q}}`, model)
}

// adapterExtensionJSON returns an extension document for the framework.
func adapterExtensionJSON(framework string) string {
	return fmt.Sprintf(`{"extension":{"framework":%q,"keys":[]}}`, framework)
}

// stubInstalledAdapter places a fake adapter binary for name into a
// stubbed CLI install directory with PATH cleared, making discovery
// deterministic: the fake is the only detected adapter on the system.
// It returns the directory (the fake lives at <dir>/anvil-adapter-<name>).
func stubInstalledAdapter(t *testing.T, name, capabilitiesJSON, extensionJSON string) string {
	t.Helper()
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	t.Setenv("PATH", "")
	writeFakeAdapter(t, dir, "anvil-adapter-"+name, capabilitiesJSON, extensionJSON)
	return dir
}

// stubInstalledAdapters places fake adapter binaries for the given names
// (server deployment model) into one stubbed CLI install directory with
// PATH cleared, making discovery deterministic. It returns the directory.
func stubInstalledAdapters(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	t.Setenv("PATH", "")
	for _, name := range names {
		writeFakeAdapter(t, dir, "anvil-adapter-"+name, adapterCapabilitiesJSON("server"), adapterExtensionJSON(name))
	}
	return dir
}

// ── PATH Scanning (TS-007-039 §3) ────────────────────────────────────

// TestScanPathAdapters_FindsAdaptersOnPath verifies that a fake
// anvil-adapter-rails binary on PATH appears in the scan result with its
// executable path.
//
// Reference: TS-007-039 AC-1
func TestScanPathAdapters_FindsAdaptersOnPath(t *testing.T) {
	dir := t.TempDir()
	writeFakeAdapter(t, dir, "anvil-adapter-rails", adapterCapabilitiesJSON("server"), adapterExtensionJSON("rails"))
	t.Setenv("PATH", dir)

	adapters, err := scanPathAdapters()
	if err != nil {
		t.Fatalf("scanPathAdapters returned error: %v", err)
	}
	if len(adapters) != 1 {
		t.Fatalf("got %d adapters, want 1: %v", len(adapters), adapters)
	}
	if want := filepath.Join(dir, "anvil-adapter-rails"); adapters["rails"] != want {
		t.Errorf("rails executable = %q, want %q", adapters["rails"], want)
	}
}

// TestScanPathAdapters_SkipsNonExecutable verifies that a file carrying
// the "anvil-adapter-" prefix without the executable bit is not a
// discovered adapter, while real executables still are.
func TestScanPathAdapters_SkipsNonExecutable(t *testing.T) {
	dir := t.TempDir()
	writeFakeAdapter(t, dir, "anvil-adapter-rails", adapterCapabilitiesJSON("server"), adapterExtensionJSON("rails"))
	if err := os.WriteFile(filepath.Join(dir, "anvil-adapter-node"), []byte("not an adapter"), 0644); err != nil {
		t.Fatalf("write non-executable prefixed file: %v", err)
	}
	t.Setenv("PATH", dir)

	adapters, err := scanPathAdapters()
	if err != nil {
		t.Fatalf("scanPathAdapters returned error: %v", err)
	}
	if _, ok := adapters["node"]; ok {
		t.Errorf("non-executable prefixed file should be skipped, got: %v", adapters)
	}
	if adapters["rails"] == "" {
		t.Errorf("executable adapter should be discovered, got: %v", adapters)
	}
}

// TestScanPathAdapters_SkipsUnreadableDirectory verifies that an
// unreadable PATH entry is skipped without failing the scan — the other
// directories are still scanned.
func TestScanPathAdapters_SkipsUnreadableDirectory(t *testing.T) {
	goodDir := t.TempDir()
	writeFakeAdapter(t, goodDir, "anvil-adapter-rails", adapterCapabilitiesJSON("server"), adapterExtensionJSON("rails"))

	blockedDir := t.TempDir()
	if err := os.Chmod(blockedDir, 0); err != nil {
		t.Fatalf("chmod blocked dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(blockedDir, 0755) })

	t.Setenv("PATH", blockedDir+string(os.PathListSeparator)+goodDir)

	adapters, err := scanPathAdapters()
	if err != nil {
		t.Fatalf("scanPathAdapters returned error: %v", err)
	}
	if adapters["rails"] == "" {
		t.Errorf("adapter in the readable dir should be discovered despite the unreadable entry, got: %v", adapters)
	}
}

// TestScanPathAdapters_DedupesDuplicateEntries verifies that the same
// directory listed twice on PATH yields one adapter entry.
func TestScanPathAdapters_DedupesDuplicateEntries(t *testing.T) {
	dir := t.TempDir()
	writeFakeAdapter(t, dir, "anvil-adapter-rails", adapterCapabilitiesJSON("server"), adapterExtensionJSON("rails"))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+dir)

	adapters, err := scanPathAdapters()
	if err != nil {
		t.Fatalf("scanPathAdapters returned error: %v", err)
	}
	if len(adapters) != 1 {
		t.Errorf("duplicate PATH entries should dedupe, got %d adapters: %v", len(adapters), adapters)
	}
}

// TestScanPathAdapters_HandlesRelativeEntry verifies that a relative PATH
// entry resolves against the working directory to an absolute executable
// path instead of failing the scan.
func TestScanPathAdapters_HandlesRelativeEntry(t *testing.T) {
	dir := t.TempDir()
	writeFakeAdapter(t, dir, "anvil-adapter-rails", adapterCapabilitiesJSON("server"), adapterExtensionJSON("rails"))
	chdirTo(t, dir)
	t.Setenv("PATH", ".")

	adapters, err := scanPathAdapters()
	if err != nil {
		t.Fatalf("scanPathAdapters returned error: %v", err)
	}
	want := filepath.Join(dir, "anvil-adapter-rails")
	if adapters["rails"] != want {
		t.Errorf("rails executable = %q, want %q (relative entry resolved absolutely)", adapters["rails"], want)
	}
}

// ── Probe Validation (TS-007-039 §7) ────────────────────────────────

// TestResolveAdapterSet_FiltersByProbe verifies that the probe-validated
// set keeps adapters whose capabilities command succeeds and excludes
// binaries that fail the probe — foreign "anvil-adapter-*" executables
// are never valid adapters (AC-4).
//
// Reference: TS-007-039 §7, AC-4
func TestResolveAdapterSet_FiltersByProbe(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	t.Setenv("PATH", "")
	writeFakeAdapter(t, dir, "anvil-adapter-laravel", adapterCapabilitiesJSON("server"), adapterExtensionJSON("laravel"))
	writeFailingAdapter(t, dir, "anvil-adapter-rails") // foreign/broken binary

	adapters, err := resolveAdapterSet(context.Background())
	if err != nil {
		t.Fatalf("resolveAdapterSet returned error: %v", err)
	}
	if len(adapters) != 1 {
		t.Fatalf("got %d adapters, want 1 (probe-failing binary excluded): %v", len(adapters), adapters)
	}
	if adapters["laravel"] == "" {
		t.Errorf("probed adapter should be included, got: %v", adapters)
	}
	if _, ok := adapters["rails"]; ok {
		t.Errorf("probe-failing binary should be excluded, got: %v", adapters)
	}
}

// ── Adapter List (TS-007-031, TS-007-039) ────────────────────────────

// TestAdapterList_PathAdapterAppears verifies AC-1: a fake
// anvil-adapter-rails binary on PATH appears in "anvil adapter list"
// with its deployment model.
//
// Reference: TS-007-039 AC-1
func TestAdapterList_PathAdapterAppears(t *testing.T) {
	pathDir := t.TempDir()
	stubAdapterInstallDirAt(t, t.TempDir()) // CLI dir has nothing installed
	t.Setenv("PATH", pathDir)
	writeFakeAdapter(t, pathDir, "anvil-adapter-rails", adapterCapabilitiesJSON("server"), adapterExtensionJSON("rails"))

	_, stdout, stderr, err := executeCommand("adapter", "list")
	if err != nil {
		t.Fatalf("adapter list returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "rails") || !strings.Contains(stdout, "server") {
		t.Errorf("stdout should list the PATH adapter with its model, got:\n%s", stdout)
	}
}

// TestAdapterList_ForeignBinaryExcludedOnPath verifies AC-4: a foreign
// anvil-adapter-* executable on PATH that fails the capabilities probe is
// not listed, while a valid adapter still is.
//
// Reference: TS-007-039 AC-4
func TestAdapterList_ForeignBinaryExcludedOnPath(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	t.Setenv("PATH", dir)
	writeFailingAdapter(t, dir, "anvil-adapter-rails") // foreign: fails the probe
	writeFakeAdapter(t, dir, "anvil-adapter-laravel", adapterCapabilitiesJSON("server"), adapterExtensionJSON("laravel"))

	_, stdout, stderr, err := executeCommand("adapter", "list")
	if err != nil {
		t.Fatalf("adapter list returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if strings.Contains(stdout, "rails") {
		t.Errorf("stdout should exclude the foreign binary, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "laravel") {
		t.Errorf("stdout should still list the valid adapter, got:\n%s", stdout)
	}
}

// ── Adapter Use (TS-007-033, TS-007-039) ─────────────────────────────

// TestAdapterUse_PathInstalledAdapterSucceeds verifies AC-2: "anvil
// adapter use rails" proceeds when the rails adapter is installed on PATH
// and probes successfully.
//
// Reference: TS-007-039 AC-2
func TestAdapterUse_PathInstalledAdapterSucceeds(t *testing.T) {
	dir := setupUseProject(t)
	pathDir := t.TempDir()
	stubAdapterInstallDirAt(t, t.TempDir()) // CLI dir has nothing installed
	t.Setenv("PATH", pathDir)
	writeFakeAdapter(t, pathDir, "anvil-adapter-rails", adapterCapabilitiesJSON("server"), adapterExtensionJSON("rails"))

	_, stdout, stderr, err := executeCommand("adapter", "use", "rails")
	if err != nil {
		t.Fatalf("adapter use returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if got := projectFramework(t, dir); got != "rails" {
		t.Errorf("project.framework = %q, want %q", got, "rails")
	}
	if !strings.Contains(stdout, "Adapter rails is now active") {
		t.Errorf("stdout should confirm activation, got:\n%s", stdout)
	}
}

// TestAdapterUse_UnknownNameWithoutDiscovery verifies AC-3: "anvil
// adapter use <unknown>" fails cleanly with the KnownFrameworks fallback
// hint when nothing is on PATH, without touching the project.
//
// Reference: TS-007-039 AC-3, AC-5
func TestAdapterUse_UnknownNameWithoutDiscovery(t *testing.T) {
	dir := setupUseProject(t)
	stubKnownFrameworks(t, []string{"laravel", "flutter"})
	stubAdapterInstallDirAt(t, t.TempDir()) // nothing installed
	t.Setenv("PATH", "")

	_, _, stderr, err := executeCommand("adapter", "use", "rails")
	if err == nil {
		t.Fatal("expected error for undiscovered adapter, got nil")
	}
	if !strings.Contains(stderr, `unknown adapter "rails"`) {
		t.Errorf("stderr should name the unknown adapter, got: %s", stderr)
	}
	if !strings.Contains(stderr, "known adapters") {
		t.Errorf("stderr should fall back to the known adapters hint, got: %s", stderr)
	}
	if got := projectFramework(t, dir); got != "" {
		t.Errorf("project.framework = %q, want unset after rejection", got)
	}
}

// TestAdapterUse_UnknownNameListsDiscoveredAdapters verifies that the
// unknown-adapter error lists the discovered (probe-validated) adapters
// when the scan found something — the probed set, not the known list.
//
// Reference: TS-007-039 AC-3
func TestAdapterUse_UnknownNameListsDiscoveredAdapters(t *testing.T) {
	setupUseProject(t)
	stubInstalledAdapter(t, "rails", adapterCapabilitiesJSON("server"), adapterExtensionJSON("rails"))

	_, _, stderr, err := executeCommand("adapter", "use", "node")
	if err == nil {
		t.Fatal("expected error for unknown adapter, got nil")
	}
	if !strings.Contains(stderr, `unknown adapter "node"`) {
		t.Errorf("stderr should name the unknown adapter, got: %s", stderr)
	}
	if !strings.Contains(stderr, "available adapters: rails") {
		t.Errorf("stderr should list the discovered adapters, got: %s", stderr)
	}
}

// ── Adapter Inspect (TS-007-032, TS-007-039) ─────────────────────────

// TestAdapterInspect_PathAdapterSucceeds verifies that inspecting a
// PATH-discovered, probe-validated adapter works.
//
// Reference: TS-007-039 AC-1, AC-2
func TestAdapterInspect_PathAdapterSucceeds(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	stubAdapterLookup(t, dir)
	t.Setenv("PATH", "")
	writeFakeAdapter(t, dir, "anvil-adapter-rails", adapterCapabilitiesJSON("server"), adapterExtensionJSON("rails"))

	_, stdout, stderr, err := executeCommand("adapter", "inspect", "rails")
	if err != nil {
		t.Fatalf("adapter inspect returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "Adapter: rails") || !strings.Contains(stdout, "  server") {
		t.Errorf("stdout should render the discovered adapter, got:\n%s", stdout)
	}
}

// TestAdapterInspect_UnknownNameWithoutDiscovery verifies that inspecting
// an adapter that is not on the system fails cleanly with the
// KnownFrameworks fallback hint.
//
// Reference: TS-007-039 AC-3, AC-5
func TestAdapterInspect_UnknownNameWithoutDiscovery(t *testing.T) {
	stubKnownFrameworks(t, []string{"laravel", "flutter"})
	stubAdapterInstallDirAt(t, t.TempDir()) // nothing installed
	t.Setenv("PATH", "")

	_, _, stderr, err := executeCommand("adapter", "inspect", "rails")
	if err == nil {
		t.Fatal("expected error for undiscovered adapter, got nil")
	}
	if !strings.Contains(stderr, `unknown adapter "rails"`) {
		t.Errorf("stderr should name the unknown adapter, got: %s", stderr)
	}
	if !strings.Contains(stderr, "known adapters") {
		t.Errorf("stderr should fall back to the known adapters hint, got: %s", stderr)
	}
}
