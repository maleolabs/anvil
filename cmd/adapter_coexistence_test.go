// Package cmd implements the Anvil CLI commands.
//
// Tests for the post-gate state (TS-017-02-02, ADR-028 §3, §7): after
// the switch-over gate closes the dual-run window, the CLOSED-SET
// discovery path is REMOVED and REGISTRY-ONLY discovery remains. These
// tests lock the post-gate state:
//
//   - the registry path resolves lifecycle content: 'anvil standard
//     list'/'inspect' and 'anvil adapter list --available' resolve the
//     published releases; 'anvil adapter list' resolves the RECORDED
//     adapters (the registry-driven installed view) — identity parity
//     (adapter name ↔ anvil-standard-<name>) and version parity (the
//     recorded version) hold;
//   - a bare "anvil-adapter-*" binary that was never adopted through
//     the registry is NOT discovered (no binary scan);
//   - the "installed" marking of the registry view is registry-driven:
//     an adapter is installed when its standard is RECORDED, never
//     because a binary sits on PATH;
//   - the executable resolution contract (anvil-adapter-<name> on PATH,
//     ADR-025 decision 4) keeps the adapter alias surfaces
//     (inspect/use) functional for recorded adapters;
//   - the vocabulary aliases stay registered (EPIC-019,
//     cmd/adapter_deprecation_test.go) — the gate removes the
//     closed-set DISCOVERY, not the command surface.
//
// Reference: TS-017-02-02, TS-017-02-01 (superseded window state),
// ADR-028 §3, §7
package cmd

import (
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/registry"
)

// ── Registry-Only Discovery Resolves Lifecycle Content (ADR-028 §3) ──

// TestPostGate_RegistryOnlyDiscovery_ResolvesLifecycleContent is the
// central post-gate test: the same lifecycle content (the laravel
// adapter / anvil-standard-laravel release) is resolvable through the
// registry surfaces — the adapter view (adapter list default = recorded
// adapters; adapter list --available = offered adapters) and the
// standard view (standard list / inspect) — with consistent identity
// and version. The registry is the ONLY discovery source: the closed-set
// binary scan is gone (TS-017-02-02).
//
// Reference: TS-017-02-02, TS-016-04-01, ADR-028 §3, §7
func TestPostGate_RegistryOnlyDiscovery_ResolvesLifecycleContent(t *testing.T) {
	// Registry side: both standards published in a static index —
	// laravel at 1.2.3, flutter at 2.0.0.
	indexDir := adapterListTestIndex(t,
		adapterListTestRelease(t, "anvil-standard-laravel", "1.2.3", registry.LifecycleStatePublished),
		adapterListTestRelease(t, "anvil-standard-flutter", "2.0.0", registry.LifecycleStatePublished),
	)

	// Identity parity: the adapter name and the standard id are the same
	// lifecycle content under the identity convention (ADR-021 §3.1).
	if got := adapterStandardIDForName("laravel"); got != "anvil-standard-laravel" {
		t.Errorf("adapterStandardIDForName(laravel) = %q, want anvil-standard-laravel", got)
	}

	// ── Adapter view, default mode: recorded adapters only ──
	// laravel is recorded (adopted through the registry); flutter has no
	// record. A bare flutter binary on PATH must NOT be discovered.
	seedInstalledStandard(t, "anvil-standard-laravel", "1.2.3")
	flutterDir := t.TempDir()
	t.Setenv("PATH", flutterDir)
	writeFakeAdapter(t, flutterDir, "anvil-adapter-flutter", adapterCapabilitiesJSON("hybrid"), adapterExtensionJSON("flutter"))

	_, stdout, stderr, err := executeCommand("adapter", "list")
	if err != nil {
		t.Fatalf("adapter list (registry-driven) returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "laravel") || !strings.Contains(stdout, "1.2.3") {
		t.Errorf("registry-driven adapter list should resolve the recorded adapter with its recorded version, got:\n%s", stdout)
	}
	if strings.Contains(stdout, "flutter") {
		t.Errorf("an unadopted PATH binary must not be discovered post-gate, got:\n%s", stdout)
	}

	// ── Registry path: resolves the published releases ──
	_, stdout, stderr, err = executeCommand("adapter", "list", "--available", "--index", indexDir)
	if err != nil {
		t.Fatalf("adapter list --available (registry) returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	for _, want := range []string{"laravel", "1.2.3", "installed", "flutter", "available", "2.0.0"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("registry adapter list --available should contain %q, got:\n%s", want, stdout)
		}
	}

	_, stdout, stderr, err = executeCommand("standard", "list", "--index", indexDir)
	if err != nil {
		t.Fatalf("standard list (registry) returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "anvil-standard-laravel") || !strings.Contains(stdout, "1.2.3") ||
		!strings.Contains(stdout, "anvil-standard-flutter") || !strings.Contains(stdout, "2.0.0") {
		t.Errorf("registry standard list should resolve both published releases, got:\n%s", stdout)
	}

	_, stdout, stderr, err = executeCommand("standard", "inspect", "anvil-standard-laravel", "1.2.3", "--index", indexDir)
	if err != nil {
		t.Fatalf("standard inspect (registry) returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "Standard: anvil-standard-laravel 1.2.3") {
		t.Errorf("registry inspect should resolve the release, got:\n%s", stdout)
	}
}

// ── Installed Marking Is Registry-Driven (TS-017-02-02) ──────────────

// TestPostGate_InstalledMarking_RegistryDriven verifies that the
// registry view's "installed" marking is the registry-driven definition
// — the recorded installed-standard records — in both directions: a
// probe-valid binary WITHOUT a record marks the adapter "available"
// (the closed-set scan is gone; a bare binary is not installed), and a
// RECORD marks the adapter "installed" even when the binary is absent
// (the record is the installed truth).
//
// Reference: TS-017-02-02, ADR-028 §3, §7
func TestPostGate_InstalledMarking_RegistryDriven(t *testing.T) {
	t.Run("binary without record is available", func(t *testing.T) {
		isolateGlobalConfigDir(t) // no records on this machine
		dir := t.TempDir()
		t.Setenv("PATH", "")
		writeFakeAdapter(t, dir, "anvil-adapter-laravel", adapterCapabilitiesJSON("server"), adapterExtensionJSON("laravel"))
		indexDir := adapterListTestIndex(t,
			adapterListTestRelease(t, "anvil-standard-laravel", "1.0.0", registry.LifecycleStatePublished),
		)

		// The registry view marks laravel "available": a bare binary
		// never marks an adapter installed post-gate.
		_, stdout, stderr, err := executeCommand("adapter", "list", "--available", "--index", indexDir)
		if err != nil {
			t.Fatalf("adapter list --available returned unexpected error: %v (stderr: %s)", err, stderr)
		}
		if !strings.Contains(stdout, "laravel") || !strings.Contains(stdout, "available") {
			t.Errorf("registry view should offer laravel as available, got:\n%s", stdout)
		}
		if strings.Contains(stdout, "installed") {
			t.Errorf("registry view must not mark laravel installed without a record, got:\n%s", stdout)
		}
	})

	t.Run("record without binary is installed", func(t *testing.T) {
		seedInstalledStandard(t, "anvil-standard-laravel", "1.0.0") // no binary anywhere
		t.Setenv("PATH", "")
		indexDir := adapterListTestIndex(t,
			adapterListTestRelease(t, "anvil-standard-laravel", "1.0.0", registry.LifecycleStatePublished),
		)

		_, stdout, stderr, err := executeCommand("adapter", "list", "--available", "--index", indexDir)
		if err != nil {
			t.Fatalf("adapter list --available returned unexpected error: %v (stderr: %s)", err, stderr)
		}
		if !strings.Contains(stdout, "laravel") || !strings.Contains(stdout, "installed") {
			t.Errorf("registry view should mark the recorded adapter installed, got:\n%s", stdout)
		}
	})
}

// ── Executable Resolution Contract Keeps Alias Surfaces Working ──────

// TestPostGate_AliasSurfaces_ResolveRecordedAdapters verifies that the
// adapter alias surfaces (inspect/use — the vocabulary aliases stay
// registered per EPIC-019) remain functional for recorded adapters
// through the executable resolution contract: the recorded standard
// gates the identity, the named executable resolves on PATH, and the
// probe validates it — no binary scan anywhere.
//
// Reference: TS-017-02-02, EPIC-019, ADR-025 decision 4
func TestPostGate_AliasSurfaces_ResolveRecordedAdapters(t *testing.T) {
	dir := setupUseProject(t)
	seedInstalledStandard(t, "anvil-standard-laravel", "1.0.0")
	binDir := t.TempDir()
	stubAdapterLookup(t, binDir)
	t.Setenv("PATH", binDir)
	writeFakeAdapter(t, binDir, "anvil-adapter-laravel", adapterCapabilitiesJSON("server"), adapterExtensionJSON("laravel"))

	// Inspect: resolves through the record + the resolution contract.
	_, stdout, stderr, err := executeCommand("adapter", "inspect", "laravel")
	if err != nil {
		t.Fatalf("adapter inspect returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "Adapter: laravel") {
		t.Errorf("inspect should resolve the recorded adapter, got:\n%s", stdout)
	}

	// Use: same gate; project state updates.
	_, stdout, stderr, err = executeCommand("adapter", "use", "laravel")
	if err != nil {
		t.Fatalf("adapter use returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "Adapter laravel is now active") {
		t.Errorf("use should activate the recorded adapter, got:\n%s", stdout)
	}
	if got := projectFramework(t, dir); got != "laravel" {
		t.Errorf("project.framework = %q, want laravel", got)
	}
}
