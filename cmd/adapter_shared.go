// Package cmd implements the Anvil CLI commands.
//
// ── Adapter Command Shared Machinery (TS-007-031, TS-007-032) ────────
//
// The adapter subcommands invoke adapter executables through the command
// contract (005-adapter-command-contract): the executable name convention
// is "anvil-adapter-<framework>" resolved on PATH via exec.LookPath (§10),
// and capability/config-extension data is fetched through the Process
// Runner (ADR-008) and the adapter.Coordinator (TS-P7-08). The Core never
// imports framework packages (ADR-009 §8.1; the framework packages left
// the Core module in TS-016-01-01 and TS-016-02-01, ADR-025 §6.2) — all
// framework-specific values come from the standard executable through the
// command contract (ADR-025: framework knowledge lives in the standard
// repositories).
//
// The Core carries no known-framework catalog (ADR-026): framework
// identity comes exclusively from installed delivery lifecycle standards
// resolved through the registry client (EPIC-014), so the shared helpers
// below resolve installed-standard state (installedStandardIDs) instead
// of any runtime-side framework knowledge.
//
// The two seams below are package-level variables so tests can inject
// fake executable resolution and a fake Process Runner without touching
// PATH or the filesystem.
//
// Reference: TS-007-031, TS-007-032, 005-adapter-command-contract §10,
// ADR-026
package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"

	"maleolabs.com/anvil/internal/adapter"
	"maleolabs.com/anvil/internal/contracts"
	"maleolabs.com/anvil/internal/execution"
	"maleolabs.com/anvil/internal/project"
	"maleolabs.com/anvil/internal/registry"
)

// adapterExecutableLookup resolves adapter executable paths by name. The
// production value is exec.LookPath; tests replace it to stub PATH
// resolution. The binary name convention is "anvil-adapter-<framework>"
// (005-adapter-command-contract §10).
var adapterExecutableLookup = exec.LookPath

// highestAdoptableVersion returns the highest ADOPTABLE version of the
// standard id offered in the registry index (TS-016-04-01), or "" when
// nothing adoptable is offered. It is the shared version-selection rule
// of the registry-based discovery surfaces:
//
//   - 'anvil adapter list --available' shows each adapter's version;
//   - 'anvil adapter install' adopts the highest adoptable version when
//     the standard is not already recorded.
//
// Versions are ordered semantically (1.9.0 before 1.10.0 — the index
// client's lexical order is a documented index-client scope, display
// ordering is the discovery surface's). Entries that fail strict registry
// validation or declare a non-adoptable lifecycle state (retired, unknown
// — ADR-027 §3) are not offered for adoption and are skipped, mirroring
// 'anvil standard list'.
func highestAdoptableVersion(ix *registry.Index, id string) string {
	sorted := sortStandardVersions(ix.Versions(id))
	for i := len(sorted) - 1; i >= 0; i-- {
		entry, err := ix.Resolve(id, sorted[i])
		if err != nil {
			continue
		}
		md, _, err := parseStandardEntry(entry)
		if err != nil {
			continue // invalid documents are not offered for adoption
		}
		if registry.LifecycleAdoptable(md.Lifecycle.State) {
			return sorted[i]
		}
	}
	return ""
}

// adapterRunnerFactory returns the Process Runner used to invoke adapter
// executables (ADR-008). The production value returns the real
// os/exec-backed runner; tests replace it to record or fake adapter
// invocations.
var adapterRunnerFactory = func() execution.Runner { return execution.NewRunner() }

// installedStandardIDs returns the ids of the delivery lifecycle
// standards recorded in the installed-standard store (EPIC-014, ADR-023),
// sorted ascending. Framework identity comes exclusively from installed
// standards (ADR-026): this is the registry-client source the adapter
// command hints resolve against (post-gate, TS-017-02-02, the closed-set
// binary scan is removed — the hints are registry-driven). A missing or
// unreadable store yields no ids — callers treat an empty result as "no
// standard installed".
func installedStandardIDs() []string {
	dir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		return nil
	}
	store := registry.NewInstalledStandardStore(dir)
	summaries, _, err := store.List()
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		ids = append(ids, summary.ID)
	}
	return ids
}

// adapterCoordinator builds the invocation machinery for one adapter: the
// Process Runner (ADR-008) and a fresh CapabilityRegistry to register the
// adapter's declared capabilities into (TS-P7-07). Registries are fresh
// per invocation — adapters are stateless (ADR-009 §9.8).
func adapterCoordinator() *adapter.Coordinator {
	return adapter.NewCoordinator(adapterRunnerFactory(), adapter.NewCapabilityRegistry())
}

// invokeAdapterCapabilities requests the adapter executable's declared
// capabilities through the capabilities command (TS-P7-07, TS-P7-08).
func invokeAdapterCapabilities(ctx context.Context, framework, executable string) (contracts.CapabilityResult, error) {
	return adapterCoordinator().InvokeCapabilities(ctx, framework, executable)
}

// invokeAdapterConfigExtension requests the adapter executable's declared
// configuration extension keys through the extension command (TS-P7-03,
// TS-P7-12).
func invokeAdapterConfigExtension(ctx context.Context, framework, executable string) (contracts.ConfigExtensionResult, error) {
	return adapterCoordinator().InvokeConfigExtension(ctx, framework, executable)
}

// invokeAdapterManifestCommands requests the adapter executable's
// activation and rollback command strings for the artifact manifest
// through the manifest command (TS-P7-15, TS-P7-16,
// 005-adapter-command-contract §10.10).
func invokeAdapterManifestCommands(ctx context.Context, framework, executable string) (contracts.ManifestCommandResult, error) {
	return adapterCoordinator().InvokeManifestCommands(ctx, framework, executable)
}

// adapterVerificationCoordinator builds a Coordinator whose capability
// registry is populated with the framework standard's declared
// capabilities (TS-P7-07), so verification checks can be planned
// (VerificationChecks) and invoked (InvokeVerification) — the CLI
// counterpart of the server registration helper (internal/server
// registers capabilities + extension; verification consults only the
// capability registry, so the extension command is not dispatched here —
// one less subprocess and one less failure mode in a verification-only
// flow). The executable path is resolved by the caller through
// adapterExecutableLookup. The Core never imports framework packages
// (ADR-009 §8.1) — all framework-specific values come from the framework
// standard executable through the command contract.
//
// Reference: TS-P7-07, TS-P7-08, TS-P7-11, ST-007-004
func adapterVerificationCoordinator(ctx context.Context, framework, executable string) (*adapter.Coordinator, error) {
	runner := adapterRunnerFactory()
	capabilities := adapter.NewCapabilityRegistry()
	coord := adapter.NewCoordinator(runner, capabilities)

	capResult, err := coord.InvokeCapabilities(ctx, framework, executable)
	if err != nil {
		return nil, fmt.Errorf("cannot collect capabilities from adapter %q for framework %q: %w", executable, framework, err)
	}
	if err := capabilities.Register(framework, capResult.Declaration); err != nil {
		return nil, fmt.Errorf("cannot register capabilities from adapter %q for framework %q: %w", executable, framework, err)
	}
	return coord, nil
}

// activeProjectFramework returns the project's active framework from
// project.framework in anvil.yaml, or "" when the project section or the
// key is absent. The key is not part of the canonical config schema
// (project.Load silently ignores it), so the value is read from the raw
// YAML document — the same pattern as cmd/adapter_use.go
// (readProjectFramework). A missing or unreadable config is reported as
// "" — the callers then package without manifest commands and verify
// without framework checks (backward compatible).
//
// Reference: TS-P7-15, TS-P7-16, ST-007-004
func activeProjectFramework() string {
	root, err := project.Discover()
	if err != nil {
		return ""
	}
	framework, err := readProjectFramework(filepath.Join(root, project.ConfigFileName))
	if err != nil {
		return ""
	}
	return framework
}
