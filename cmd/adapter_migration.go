// Package cmd implements the Anvil CLI commands.
//
// ── Installed Adapter Recognition and Migration (TS-017-01-02, T-004) ─
//
// Per ADR-028 §3 and Transition Plan §12.3, installed v1.x adapters are
// recognized at adoption time and migrated to the corresponding delivery
// lifecycle standard via the authoritative mapping table
// (docs/planning/ANVIL_V2_ADAPTER_STANDARD_MAPPING.md, TS-017-01-01):
// when a project with a declared framework and an installed v1.x adapter
// is used, the runtime identifies the installed adapter, maps it to the
// standard, and records the migration outcome — compatibility is
// declared, validated, and recorded, never assumed (A2). Migration
// switches resolution to the standard (the engine's standard-driven
// paths — TS-015-02-01, TS-015-02-03 — resolve through the installed
// standard) while preserving project state (anvil.yaml is never
// modified) and lifecycle behavior (the v1.x lifecycle keeps working
// through the recognized adapter during the dual-run window).
//
// The hook is additive by design: recognition never changes the
// resolution semantics of the calling command — the standard-missing
// hard-fail of ADR-026 decision 3 (TS-015-02-02) is untouched — and the
// migration outcome is recorded in the outcome store, surfaced
// explicitly, and never silently skipped. The mapping artifact is
// located and stat-checked BEFORE any probe: a missing artifact (no
// ANVIL_ADAPTER_STANDARD_MAPPING, no corpus at the default location) is
// a silent no-op — recognition is not configured on that system and no
// subprocess is spawned; an artifact that exists but cannot be read
// (broken table) is surfaced with a warning when a probe-validated
// adapter is present. Either way the caller's resolution path is
// unaffected.
//
// Contract-version VALIDATION at migration (TS-017-01-03, T-007): when
// the mapped standard is installed, its declared contract version (the
// contractVersion recorded in the installed-standard record at install,
// from the standard's registry metadata document — registry-metadata
// §4.3) is validated against the runtime's supported contract major
// set, READ from the compatibility matrix record at runtime
// (docs/specification-corpus/compatibility-matrix.json — the corpus
// reference declared contract versions are checked against, ADR-029
// §3; supported majors are never silently defaulted, PM binding
// decision 3). A valid match completes the migration (status migrated,
// contract_version recorded). A mismatch NEVER silently passes: the
// outcome is recorded as NOT completed (status recognized — the v1.x
// adapter keeps working, ADR-028 §12.3) with the declared contract
// version recorded, and an actionable report states the declared
// version, the unsupported major, the supported set, and the
// remediation. A compatibility matrix that cannot be read — missing,
// corrupt, or structurally invalid — means validation cannot run, and
// recognition is skipped with an explicit warning: a migration outcome
// is never recorded WITHOUT contract-version validation (declared,
// validated, and recorded — never assumed, ADR-024 §3.6).
//
// Recognition mechanism (RFC-P7, resolved): the project's declared
// framework (project.framework, the v1.x declaration; project.standard
// is the canonical v2 key, TS-019-02-01) is matched against the mapping
// table by the adapter_name lookup key, and recognition is confirmed
// through the probe-validated executable identity (the executable
// resolution contract — anvil-adapter-<framework> on PATH — plus the
// capabilities probe, TS-007-039 §7: a binary counts as an adapter only
// when it answers the capabilities command; post-gate, TS-017-02-02,
// the closed-set system scan is removed and recognition resolves the
// NAMED executable). The mapping table supplies the mapping data only —
// standard identity is never hard-coded in code (§7).
//
// Reference: TS-017-01-02, TS-017-01-01 §7, ADR-028 §3, §12.3,
// ADR-026 decision 3, ADR-029 §3
package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/registry"
)

// recognizeAndMigrateInstalledAdapterAtAdoption runs the adoption-time
// installed v1.x adapter recognition and migration (TS-017-01-02,
// ADR-028 §3, §12.3) for a project's declared framework:
//
//   - the authoritative mapping (TS-017-01-01 §7) is consumed as data —
//     read at adoption time, never hardcoded (the corpus pattern of
//     ADR-029 §3, mirroring the compatibility matrix). The artifact is
//     located and stat-checked FIRST: on systems where no mapping is
//     configured (no ANVIL_ADAPTER_STANDARD_MAPPING and no artifact at
//     the default corpus location — the normal state for installed
//     binaries and v2-fresh environments), recognition is a no-op and
//     no probe subprocess is ever spawned;
//   - only when the artifact exists is recognition confirmed through
//     the probe-validated executable identity (RFC-P7; TS-007-039 §7):
//     the binary is resolved by its NAME through the executable
//     resolution contract (anvil-adapter-<framework> on PATH — post-gate
//     the closed-set system scan is removed, TS-017-02-02) and must
//     answer the capabilities command. A framework with no
//     probe-validated adapter has nothing to recognize;
//   - with a probe-validated adapter installed, the mapping is loaded.
//     An artifact that exists but cannot be read (broken table) is
//     surfaced explicitly — recognition is unavailable, and the
//     caller's resolution path is unaffected;
//   - the declared framework must be a first-party adapter identity in
//     the mapping (adapter_name lookup key, §7) — a framework with no
//     row is not a first-party adapter and is not recognized
//     (third-party adapters are out of scope, §7);
//   - the runtime's supported contract major set is READ from the
//     compatibility matrix record (supportedContractMajors — the corpus
//     reference declared contract versions are checked against, ADR-029
//     §3). A matrix that cannot be read means contract-version
//     validation at migration cannot run, and recognition is skipped
//     with an explicit warning — a migration outcome is never recorded
//     WITHOUT contract-version validation (ADR-024 §3.6; supported
//     majors are never silently defaulted, PM binding decision 3);
//   - contract-version validation runs at migration
//     (registry.RecognizeInstalledAdapter, TS-017-01-03): the mapped
//     standard's declared contract version is validated against the
//     supported majors. A valid match completes the migration; a
//     mismatch is recorded (status recognized — the migration did NOT
//     complete) and reported with actionable remediation — never silent
//     acceptance (ADR-028 §3);
//   - the migration outcome is recorded (registry.RecognizeInstalledAdapter)
//     and surfaced explicitly when a NEW outcome state is persisted —
//     the first recognition or a state change (e.g. recognized →
//     migrated once the standard is installed and validated, or a
//     declared contract version change); re-confirmed states are
//     not re-announced.
//
// The hook is best-effort by design: recognition is a transition
// mechanism (ADR-028 §12.3), not a resolution gate. Failures to read
// the mapping artifact or the stores are surfaced as warnings and never
// block the caller's resolution path (the standard-missing hard-fail
// semantics of ADR-026 decision 3 are unchanged). Project state is
// preserved: anvil.yaml is never modified.
func recognizeAndMigrateInstalledAdapterAtAdoption(cmd *cobra.Command, ctx context.Context, framework string) {
	if framework == "" {
		return // no framework declaration — nothing to recognize
	}

	// Locate and stat the authoritative mapping artifact FIRST: no
	// artifact at the resolved location means recognition is not
	// configured on this system — a normal no-op state that must not
	// spawn a probe subprocess (the common case on installed binaries
	// and v2-fresh environments).
	path, err := registry.ResolveAdapterMappingPath("", os.Getenv)
	if err != nil {
		return // no mapping location resolvable — recognition is a no-op
	}
	if _, err := os.Stat(path); err != nil {
		return // no artifact at the resolved location — recognition is a no-op
	}

	// Recognition is confirmed through the probe-validated executable
	// identity (RFC-P7; TS-007-039 §7): the binary is resolved by its
	// NAME through the executable resolution contract (post-gate the
	// closed-set system scan is removed, TS-017-02-02) and must answer
	// the capabilities command. Nothing to recognize when no
	// probe-validated adapter is resolvable.
	executable, ok := probeInstalledAdapter(ctx, framework)
	if !ok {
		return
	}

	// A probe-validated adapter IS installed: consume the authoritative
	// mapping to identify it. The artifact exists (stat passed) but may
	// still be unreadable — a broken mapping with an adapter present is
	// surfaced explicitly: recognition is unavailable, and the
	// resolution path is unaffected.
	mapping, err := registry.LoadAdapterMapping(path)
	if err != nil {
		adapterMigrationWarning(cmd, "installed adapter %q could not be recognized: %v", framework, err)
		return
	}

	// The declared framework must be a first-party adapter identity in
	// the authoritative mapping (adapter_name lookup key, §7).
	if _, ok := mapping.LookupByAdapterName(framework); !ok {
		return // not a first-party adapter identity — out of scope (§7)
	}

	// Contract-version validation at migration (TS-017-01-03) requires
	// the runtime's supported contract major set, READ from the
	// compatibility matrix record — the corpus reference declared
	// contract versions are checked against (ADR-029 §3). A matrix that
	// cannot be read means validation cannot run, and recognition is
	// skipped with an explicit warning: a migration outcome is never
	// recorded WITHOUT contract-version validation (declared, validated,
	// and recorded — never assumed, ADR-024 §3.6; supported majors are
	// never silently defaulted, PM binding decision 3). The caller's
	// resolution path is unaffected (the hook stays best-effort).
	// Fail-closed layering: this cmd-level skip (NO outcome recorded at
	// all) is deliberate — the engine-level (RecognizeInstalledAdapter)
	// fail-closed path records recognized+mismatch for an empty or
	// unreadable supported-majors set, but the cmd layer never lets an
	// unreadable matrix reach it; the two layers close different gaps.
	supportedMajors, err := supportedContractMajors()
	if err != nil {
		adapterMigrationWarning(cmd,
			"installed adapter %q could not be recognized: contract-version validation at migration requires the compatibility matrix, which could not be read: %v",
			framework, err)
		return
	}

	stdDir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		adapterMigrationWarning(cmd, "%v", err)
		return
	}
	migDir, err := registry.DefaultAdapterMigrationsDir()
	if err != nil {
		adapterMigrationWarning(cmd, "%v", err)
		return
	}

	result, err := registry.RecognizeInstalledAdapter(
		framework,
		map[string]string{framework: executable},
		mapping,
		registry.NewInstalledStandardStore(stdDir),
		registry.NewAdapterMigrationStore(migDir),
		supportedMajors,
		time.Now,
	)
	if err != nil {
		adapterMigrationWarning(cmd, "%v", err)
		return
	}
	if !result.Recorded {
		return // outcome state unchanged — re-confirmed, not re-announced
	}

	// A new migration outcome state was recorded: surface it explicitly
	// (recorded, never silent — A2). The message states what was
	// recognized, what it maps to, the contract-version validation
	// outcome, and how to complete the migration.
	outcome := result.Outcome
	if outcome.Status == registry.MigrationStatusMigrated {
		fmt.Fprintf(cmd.ErrOrStderr(), FmtWarning(
			"installed v1.x adapter %q recognized; resolution switched to delivery lifecycle standard %s %s (declared contract version %s validated against supported contract major(s) %s; migration outcome recorded — TS-017-01-03, ADR-024 §3.6)"),
			outcome.AdapterName, outcome.StandardID, outcome.StandardVersion,
			outcome.ContractVersion, registry.FormatContractMajors(supportedMajors))
		return
	}
	if result.ContractVersionValidated && !result.ContractVersionCompatible {
		// The standard IS installed but its declared contract version
		// is not supported by this runtime: the migration did NOT
		// complete — never silent acceptance (ADR-028 §3). The report
		// is actionable: what was declared, why it fails, and how to
		// resolve it.
		fmt.Fprintf(cmd.ErrOrStderr(), FmtWarning(
			"installed v1.x adapter %q recognized; it maps to delivery lifecycle standard %q, whose declared contract version %s is not supported by this runtime (supported contract major(s): %s; ADR-024 §3.4) — the migration did NOT complete and the v1.x adapter keeps working (dual-run window, ADR-028 §12.3). Update the standard to a release declaring a supported contract version ('anvil standard update %s <version>'), or upgrade the runtime (mismatch outcome recorded — TS-017-01-03, ADR-024 §3.6)"),
			outcome.AdapterName, outcome.StandardID, outcome.ContractVersion,
			registry.FormatContractMajors(supportedMajors), outcome.StandardID)
		return
	}
	fmt.Fprintf(cmd.ErrOrStderr(), FmtWarning(
		"installed v1.x adapter %q recognized; it maps to delivery lifecycle standard %q (migration outcome recorded; install the standard with 'anvil standard install %s <version>' to complete the migration — TS-017-01-02, ADR-028 §12.3)"),
		outcome.AdapterName, outcome.StandardID, outcome.StandardID)
}

// probeInstalledAdapter resolves a probe-validated adapter binary for
// framework through the EXECUTABLE RESOLUTION CONTRACT: the binary is
// resolved by its name (anvil-adapter-<framework>, CLI install dir
// first then PATH, 005-adapter-command-contract §10.1; ADR-025 decision
// 4) and must answer the capabilities command (TS-007-039 §7 — a binary
// counts as an adapter only when the probe succeeds). Post-gate
// (TS-017-02-02) the closed-set system scan is removed: recognition
// resolves the NAMED executable of the project's declared framework —
// the migration path for installed v1.x adapters (ADR-028 §12.3) stays
// functional — it never scans the system for what is installed (that is
// the registry records' job).
//
// Security guard (team review F1): the name goes through
// resolveAdapterExecutable, which validates the identifier pattern
// BEFORE any lookup — a malicious project.framework (path separators
// etc.) can never steer exec.LookPath at a working-directory-relative
// executable; recognition simply no-ops. The returned path is the
// executable that recognized the adapter.
func probeInstalledAdapter(ctx context.Context, framework string) (string, bool) {
	executable, err := resolveAdapterExecutable(framework)
	if err != nil {
		return "", false
	}
	if _, err := invokeAdapterCapabilities(ctx, framework, executable); err != nil {
		return "", false
	}
	return executable, true
}

// adapterMigrationWarning surfaces an adapter-recognition unavailability
// explicitly (never silent): recognition is a transition mechanism
// (ADR-028 §12.3), not a resolution gate — when the mapping artifact or
// a store cannot be read, recognition is skipped with a warning and the
// caller's resolution path is unaffected (the standard-missing hard-fail
// semantics of ADR-026 decision 3 are unchanged).
func adapterMigrationWarning(cmd *cobra.Command, format string, args ...interface{}) {
	fmt.Fprintf(cmd.ErrOrStderr(), FmtWarning("adapter recognition unavailable: "+format), args...)
}
