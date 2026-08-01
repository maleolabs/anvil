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
	"os/exec"

	"maleolabs.com/anvil/internal/adapter"
	"maleolabs.com/anvil/internal/contracts"
	"maleolabs.com/anvil/internal/engine"
	"maleolabs.com/anvil/internal/execution"
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
// knows about. It is the single source of truth for "which adapters can
// exist" — the set of frameworks with a registered build pipeline
// template (laravel, flutter) — consistent with the engine template
// registry and config.NewFrameworkProjectConfig. It is a variable so
// tests can stub the known set without touching the engine.
//
// Reference: TS-007-031, TS-007-033
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
