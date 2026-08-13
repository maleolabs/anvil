// Package cmd implements the Anvil CLI commands.
//
// Reference: ADR-010 §6.7, EPIC-007, ADR-028, ADR-032
package cmd

import (
	"github.com/spf13/cobra"

	"maleolabs.com/anvil/internal/deprecation"
)

// ── Deprecation Notices (ADR-032, ADR-028) ───────────────────────────
//
// The command surface moved to the standard vocabulary (ADR-032):
// "anvil standard" is the canonical surface (EPIC-014); the legacy
// "adapter" names resolve as aliases during the deprecation window
// (ADR-028 §3, ADR-032 §7) — each emitting a deprecation warning that
// names the replacement on use. Cobra renders these notices after
// "Command %q is deprecated, " and writes them to stderr in the real
// binary (the test harness sees them on stdout; see the buffer NOTE in
// cmd/adapter_deprecation_test.go). Deprecation must not change command
// behavior or exit codes — the alias surface stays fully operational
// until the governed removal fires. Note that the dual-run DISCOVERY
// window closed at the switch-over gate (TS-017-02-02): the aliases now
// resolve through the registry (records + index + executable resolution
// contract); only their removal remains scheduled.
//
// Removal (governed, TS-017-04-02): the aliases are removed when the
// deprecation window closes (ADR-032 §7, EPIC-017 ST-017-04) — an
// event-bounded removal that happens only after the window closes AND
// the migration path is exercised (ANVIL_V2_TRANSITION_PLAN §12.5,
// ADR-028 §3). The window closed at the switch-over gate (T-011,
// GATE-REVIEW PASS); the exercised-path evidence (GATE-REVIEW C2) is
// delivered by T-021. The registration below consults the removal gate
// (internal/deprecation): while the evidence is missing the surface
// stays registered with warnings; flipping
// deprecation.MigrationPathExercised after the evidence lands executes
// the removal — the group stops being registered and invocations fail
// with "unknown command" (announced via the schedule and migration
// guide, never silent).
//
// Reference: ADR-028, ADR-032, EPIC-017, docs/migration-guide-v2.md,
// internal/deprecation

// adapterDeprecationNotice is the cobra deprecation message of the
// legacy "adapter" command group: it names the canonical standard
// surface and the removal schedule, following the legacy "runtime"
// group precedent (TD-007) — replacement name and migration guide
// pointer only, no internal governance jargon.
const adapterDeprecationNotice = `use "anvil standard" commands instead; this group is retained for backward compatibility and will be removed in a future release (see docs/migration-guide-v2.md)`

// adapterListDeprecationNotice is the deprecation message of "anvil
// adapter list": the canonical discovery surface is "anvil standard
// list" (TS-014-02-02).
const adapterListDeprecationNotice = `use "anvil standard list" instead (see docs/migration-guide-v2.md)`

// adapterInspectDeprecationNotice is the deprecation message of "anvil
// adapter inspect": the canonical inspection surface is "anvil standard
// inspect" (TS-014-02-02).
const adapterInspectDeprecationNotice = `use "anvil standard inspect" instead (see docs/migration-guide-v2.md)`

// adapterInstallDeprecationNotice is the deprecation message of "anvil
// adapter install": the canonical adoption flow is "anvil standard
// install" (TS-016-04-01) — the adapter install already resolves
// anvil-standard-<name> through the registry with the same validation
// and trust gates (ADR-022).
const adapterInstallDeprecationNotice = `use "anvil standard install" instead (see docs/migration-guide-v2.md)`

// adapterUseDeprecationNotice is the deprecation message of "anvil
// adapter use": the framework declaration surface is standard-driven —
// "anvil init --framework <name>" resolves the declared framework
// through the installed standard (ADR-026).
const adapterUseDeprecationNotice = `use "anvil init --framework <name>" instead (see docs/migration-guide-v2.md)`

// adapterUninstallDeprecationNotice is the deprecation message of "anvil
// adapter uninstall": the v2 surface has no standard-named replacement
// for the removal of an adapter binary — the command is the v1.x
// binary-removal surface, retained for backward compatibility.
const adapterUninstallDeprecationNotice = `no standard-named replacement exists; this command is retained for backward compatibility and will be removed in a future release (see docs/migration-guide-v2.md)`

// adapterCmd represents the "anvil adapter" parent command group for
// managing framework adapters.
//
// The group is a parent-only namespace (ADR-010 §6.7): it has no RunE,
// Run, or Args — running "anvil adapter" displays the group help listing
// the subcommands below.
//
// DEPRECATED: the "adapter" group is the legacy v1.x command surface.
// "anvil standard" is the canonical surface (ADR-032, EPIC-014). The
// group is retained for backward compatibility during the deprecation
// window (ADR-032 §7) and will be removed when the window closes —
// command behavior and exit codes are unchanged. (The dual-run
// DISCOVERY window closed at the switch-over gate, TS-017-02-02: the
// group resolves through the registry.)
//
// Reference: ADR-010 §6.7, EPIC-007, TS-007-031, TS-007-032, TS-007-033,
// TS-007-037, ADR-028, ADR-032
var adapterCmd = &cobra.Command{
	Use:   "adapter",
	Short: "Manage framework adapters (deprecated)",
	Long: `Discover, inspect, install, and configure framework adapters.

Framework adapters provide platform-specific integrations for
Anvil's release lifecycle engine, allowing projects to define
custom behaviours for packaging, deployment, and activation.

DEPRECATED: The "adapter" command group is deprecated. The "standard"
command group is the canonical command surface. This group is retained
for backward compatibility and will be removed in a future release —
command behavior and exit codes are unchanged.

Migration path (see docs/migration-guide-v2.md):
  anvil adapter list       -> anvil standard list (registry-driven
                              discovery)
  anvil adapter inspect    -> anvil standard inspect (registry-driven
                              inspection)
  anvil adapter install    -> anvil standard install (registry-based
                              adoption with the same validation and
                              trust gates)
  anvil adapter use        -> anvil init --framework <name>
                              (standard-driven framework declaration)
  anvil adapter uninstall  -> no standard-named replacement exists;
                              the v1.x binary-removal surface is
                              retained for backward compatibility

Subcommands:
  list       List available adapters
  inspect    Inspect an adapter's capabilities
  use        Set the active framework for the project
  install    Install an adapter binary from the release
  uninstall  Uninstall an installed adapter binary

Examples:
  anvil adapter list
  anvil adapter inspect laravel
  anvil adapter use laravel
  anvil adapter install laravel
  anvil adapter uninstall laravel`,
	Deprecated: adapterDeprecationNotice,
}

// ── Governed removal gate (TS-017-04-02) ─────────────────────────────
//
// The legacy adapter surface is registered only while the governed
// removal condition does NOT hold (internal/deprecation): the deprecation
// window closed at the switch-over gate (T-011), but the migration-path
// evidence (GATE-REVIEW C2) is deferred to T-021 (Wave 4) — so the
// surface stays registered and warns on every use. Post-evidence,
// flipping deprecation.MigrationPathExercised executes the removal: this
// registration is skipped, invocations fail with "unknown command", and
// the removal is announced through the schedule and migration guide —
// never silent (ADR-028 §3, §12.5).
func init() {
	if !deprecation.RemovalConditionMet() {
		registerAdapterSurface()
	}
}

// registerAdapterSurface wires the legacy adapter group and its
// subcommands into the root command.
func registerAdapterSurface() {
	rootCmd.AddCommand(adapterCmd)
	adapterCmd.AddCommand(adapterListCmd, adapterInspectCmd, adapterUseCmd, adapterInstallCmd, adapterUninstallCmd)
}
