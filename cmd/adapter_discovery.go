// Package cmd implements the Anvil CLI commands.
//
// ── Registry-Driven Adapter Resolution (TS-017-02-02) ────────────────
//
// After the switch-over gate (ADR-028 §3, §7; TS-017-02-02), discovery
// is REGISTRY-ONLY: the closed-set discovery path — scanning the system
// for "anvil-adapter-<name>" executables (the former PATH/CLI-dir scan,
// TS-007-039) — is REMOVED together with the vocabulary it served
// (ADR-028 §7). The adapter command surface (aliases, EPIC-019) keeps
// working, but it resolves lifecycle content exclusively through the
// registry client:
//
//   - the INSTALLED view comes from the installed-standard records
//     (the registry client store, EPIC-014): an adapter is installed
//     when its standard anvil-standard-<name> is recorded (the
//     identity convention, ADR-021 §3.1);
//   - the OFFERED view comes from the static registry index
//     ("anvil adapter list --available");
//   - the executable itself still resolves through the executable
//     resolution contract — anvil-adapter-<name> on PATH (ADR-025
//     decision 4, 005-adapter-command-contract §10) — a NAMED
//     resolution, never a system scan. A binary that was never adopted
//     through the registry is not discovered: the registry trust
//     validation (ADR-022) is the only path into the trusted surface
//     post-gate.
//
// The Core still carries no known-framework catalog (ADR-026) and
// performs no binary scanning: framework identity comes exclusively
// from the registry client.
//
// Reference: TS-017-02-02, TS-016-04-01, ADR-028 §3, §7, ADR-021 §3.1,
// ADR-022, ADR-025 decision 4, ADR-026, 005-adapter-command-contract §10
package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"maleolabs.com/anvil/internal/registry"
)

// installedAdapterVersions returns the adapter view of the installed
// delivery lifecycle standards: adapter name → installed version for
// every recorded standard following the identity convention
// (anvil-standard-<name>, ADR-021 §3.1). Records outside the convention
// carry no adapter identity and are skipped — the canonical "anvil
// standard" surface still lists them. This is the registry-driven
// "installed" definition of the post-gate adapter surface: an adapter
// is installed when its standard is RECORDED (adopted through the
// registry, trust-validated — ADR-022), never because a binary happens
// to sit on PATH.
//
// The error is NON-NIL when the store cannot be resolved or read, or
// when it contains corrupt records — callers must NOT treat that as
// "nothing installed" (team review F5): the listing surfaces a warning,
// the name-resolving surfaces (inspect/use) fail with a distinct error
// naming the corrupt store.
//
// Reference: TS-017-02-02, TS-016-04-01, ADR-021 §3.1, ADR-022
func installedAdapterVersions() (map[string]string, error) {
	dir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		return nil, fmt.Errorf("could not resolve the installed-standard store: %w", err)
	}
	store := registry.NewInstalledStandardStore(dir)
	summaries, corrupt, err := store.List()
	if err != nil {
		return nil, fmt.Errorf("could not read the installed-standard store: %w", err)
	}
	if len(corrupt) > 0 {
		return nil, fmt.Errorf(
			"the installed-standard store contains %d corrupt record(s) — e.g. %s (%s); fix or remove the corrupt record file, or re-adopt the standard",
			len(corrupt), corrupt[0].Path, corrupt[0].Error)
	}
	installed := make(map[string]string, len(summaries))
	for _, summary := range summaries {
		name, ok := strings.CutPrefix(summary.ID, registry.StandardIDPrefix)
		if !ok || name == "" {
			continue // no adapter identity — the standard surface lists it
		}
		installed[name] = summary.Version
	}
	return installed, nil
}

// resolveAdapterExecutable resolves the named adapter executable through
// the executable resolution contract (005-adapter-command-contract §10.1;
// ADR-025 decision 4). This is NAMED resolution, not discovery: the
// caller has already established the adapter's identity through the
// registry (installedAdapterVersions), so a bare binary that was never
// adopted is never resolved by the discovery surfaces.
//
// Resolution order (team review F2): the CLI install directory first —
// a single named-file check, NOT a scan — then PATH via exec.LookPath.
// This preserves the pre-gate precedence (installed adapters next to the
// CLI resolve even when the directory is not on PATH) without restoring
// any system scan.
//
// Security guard (team review F1): the name is validated against the
// identifier pattern BEFORE any lookup. A framework/adapter name is a
// plain identifier (letters, digits, '-' and '_' only); anything else —
// path separators included — is rejected so a malicious project
// declaration (project.framework) can never steer exec.LookPath at a
// working-directory-relative executable (arbitrary code execution).
//
// Reference: TS-017-02-02, ADR-025 decision 4, 005-adapter-command-
// contract §10.1
func resolveAdapterExecutable(name string) (string, error) {
	if !isInstalledAdapterName(name) {
		return "", fmt.Errorf("invalid adapter name %q: adapter names are identifiers (letters, digits, '-' and '_' only)", name)
	}

	// CLI install directory first: the named binary next to the CLI
	// (install.sh / 'anvil adapter install' place binaries there). A
	// single-file stat with an executability check — named lookup, never
	// a directory scan.
	if dir, err := adapterInstallDir(); err == nil {
		candidate := filepath.Join(dir, adapterBinaryName(name))
		if isExecutableAdapterBinary(candidate) {
			return candidate, nil
		}
	}

	return adapterExecutableLookup(adapterBinaryName(name))
}

// isExecutableAdapterBinary reports whether path is a regular file with
// the executable bit set. Symlinks to executables qualify (os.Stat
// follows links). Used by the named CLI-install-dir resolution check
// (F2) — a non-executable file next to the CLI is never a resolvable
// adapter.
func isExecutableAdapterBinary(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0
}

// probeAdapterDeploymentModel returns the deployment model declared by
// the adapter executable of name, or "" when the executable cannot be
// resolved or does not answer the capabilities command. It is
// display-only: post-gate the installed truth is the registry record,
// so a missing or broken binary never fails the caller — the model
// renders as "-" and the adapter stays listed.
//
// Reference: TS-017-02-02, TS-P7-07
func probeAdapterDeploymentModel(ctx context.Context, name string) string {
	executable, err := resolveAdapterExecutable(name)
	if err != nil {
		return ""
	}
	result, err := invokeAdapterCapabilities(ctx, name, executable)
	if err != nil {
		return ""
	}
	return result.Declaration.DeploymentModel
}

// adapterResolutionHint builds the "reason" text for unknown-adapter
// errors from registry state: the recorded delivery lifecycle standards
// when any exist, else the registry adoption pointer. Post-gate the
// runtime performs no binary scan (TS-017-02-02) and carries no
// known-framework catalog (ADR-026) — framework identity comes
// exclusively from the registry client (EPIC-014).
func adapterResolutionHint() string {
	standards := installedStandardIDs()
	if len(standards) > 0 {
		return fmt.Sprintf("installed delivery lifecycle standards: %s (install the matching adapter binary with 'anvil adapter install <name>')", strings.Join(standards, ", "))
	}
	return "no adapter is installed through the registry; install a standard with 'anvil standard install <id> <version>' and its adapter with 'anvil adapter install <name>' (registry-based, trust-validated)"
}
