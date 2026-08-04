// Package cmd implements the Anvil CLI commands.
//
// ── Adapter Command Shared Machinery (TS-007-031, TS-007-032) ────────
//
// The adapter subcommands invoke adapter executables through the command
// contract (005-adapter-command-contract): the executable name convention
// is "anvil-adapter-<framework>" resolved on PATH via exec.LookPath (§10),
// and capability/config-extension data is fetched through the Process
// Runner (ADR-008) and the adapter.Coordinator (TS-P7-08). The Core never
// imports internal/laravel or internal/flutter (ADR-009 §8.1) — all
// framework-specific values come from the adapter executable through the
// command contract.
//
// The two seams below are package-level variables so tests can inject
// fake executable resolution and a fake Process Runner without touching
// PATH or the filesystem.
//
// Reference: TS-007-031, TS-007-032, 005-adapter-command-contract §10
package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"

	"maleolabs.com/anvil/internal/adapter"
	"maleolabs.com/anvil/internal/contracts"
	"maleolabs.com/anvil/internal/engine"
	"maleolabs.com/anvil/internal/execution"
	"maleolabs.com/anvil/internal/project"
)

// adapterExecutableLookup resolves adapter executable paths by name. The
// production value is exec.LookPath; tests replace it to stub PATH
// resolution. The binary name convention is "anvil-adapter-<framework>"
// (005-adapter-command-contract §10).
var adapterExecutableLookup = exec.LookPath

// adapterRunnerFactory returns the Process Runner used to invoke adapter
// executables (ADR-008). The production value returns the real
// os/exec-backed runner; tests replace it to record or fake adapter
// invocations.
var adapterRunnerFactory = func() execution.Runner { return execution.NewRunner() }

// adapterKnownFrameworks returns the sorted list of frameworks the Core
// knows about. After PATH-based adapter discovery (TS-007-039) it is a
// display-only fallback — the "known adapters" hint in unknown-adapter
// errors when the system scan finds nothing installed (ADR-020 §2,
// AC-5) — and the whitelist for "anvil adapter install"/"uninstall",
// whose distribution scope is intentionally closed to the adapters the
// release ships (laravel, flutter; TS-007-037). It is a variable so
// tests can stub the known set without touching the engine.
//
// Reference: TS-007-031, TS-007-033, TS-007-037, TS-007-039
var adapterKnownFrameworks = engine.KnownFrameworks

// isKnownAdapterFramework reports whether name is a framework the Core
// knows about.
func isKnownAdapterFramework(name string) bool {
	for _, framework := range adapterKnownFrameworks() {
		if framework == name {
			return true
		}
	}
	return false
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
// registry is populated with the framework adapter's declared
// capabilities (TS-P7-07), so verification checks can be planned
// (VerificationChecks) and invoked (InvokeVerification) — the CLI
// counterpart of the server registration helper (internal/server
// registers capabilities + extension; verification consults only the
// capability registry, so the extension command is not dispatched here —
// one less subprocess and one less failure mode in a verification-only
// flow). The executable path is resolved by the caller through
// adapterExecutableLookup. The Core never imports internal/laravel or
// internal/flutter (ADR-009 §8.1) — all framework-specific values come
// from the adapter executable through the command contract.
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
