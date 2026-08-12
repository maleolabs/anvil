# Anvil V2 Adapter-to-Standard Mapping

## The authoritative mapping from v1.x adapters to delivery lifecycle standards

| Metadata | |
|---|---|
| **Document ID** | ANVIL_V2_ADAPTER_STANDARD_MAPPING |
| **Status** | Draft — maintained artifact (TS-017-01-01); rows stay in sync with the standard repositories (EPIC-016 outcomes) and any later first-party standards |
| **Date** | 2026-08-07 |
| **Product** | Anvil |
| **Dependencies** | [ADR-028 §3](../adr/ADR-028-migration-and-compatibility-framework.md) · [Transition Plan §12.3](ANVIL_V2_TRANSITION_PLAN.md) · [ADR-025 §3, §6.2](../adr/ADR-025-repository-split-core-vs-standards.md) · [ADR-024 §3](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md) · [registry-metadata §4.3](../specification-corpus/registry-metadata.md) · [compatibility-matrix §2](../specification-corpus/compatibility-matrix.md) · [migration-guide-v2](../migration-guide-v2.md) |
| **Consumers** | v2 migration guide (human readers) · installed-adapter recognition logic (TS-017-01-02, T-004 Wave 2) · contract-version validation (TS-017-01-03) |

---

## 1. Purpose

This document is the adapter-to-standard mapping table required by [ADR-028 §3](../adr/ADR-028-migration-and-compatibility-framework.md) and [Transition Plan §12.3](ANVIL_V2_TRANSITION_PLAN.md): installed v1.x adapters are **recognized, migrated, and validated against the declared contract version**; compatibility is declared, validated, and recorded — not assumed (A2). ADR-028 §3 refers to §12.3 of the Transition Plan, which is the section that states the mapping requirement.

The table covers the first-party v1.x adapters (Laravel, Flutter) and their corresponding delivery lifecycle standards (`anvil-standard-laravel`, `anvil-standard-flutter`), per the naming and repository outcomes of EPIC-016 ([ADR-025 §3](../adr/ADR-025-repository-split-core-vs-standards.md)).

The table is a **maintained artifact**:

- it is the **authoritative mapping** consumed by the runtime recognition logic (TS-017-01-02);
- it is **carried by the v2 migration guide** ([migration-guide-v2 §3](../migration-guide-v2.md)) for human readers;
- it stays **in sync with the split repositories** (EPIC-016) and with any later first-party standards.

## 2. Relationship to the migration guide (document choice)

The mapping table is authored **here, as a single-source artifact**, and referenced by the migration guide — it is not inlined into the guide. This choice is deliberate:

- **Maintained artifact vs compiled guide.** migration-guide-v2 is a compiled consumer document ("compiled from approved sources... introduces no new architecture"). The mapping table is a maintained artifact with a sync contract (rows change when the split repositories change) and a machine-consumption contract (§7). Keeping it in one place avoids editing a compiled guide on every repository-outcome change.
- **Machine consumption.** TS-017-01-02 consumes the table as the authoritative mapping; a dedicated artifact with a documented structure is the cleaner consumption point than a consumer-facing guide.
- **Corpus division of labor.** The corpus pattern is "one mapping in one place; consumer documents reference it" ([006-g §1](../architecture/006-g-v1x-to-v2-concept-mapping.md); [review 23](../reviews/23-adr-documentation-review.md): consumer docs reprint mappings as reference material, they do not host them).

The guide's publication work item (TS-017-05-01) includes the mapping by reference to this artifact; this artifact is the authoritative form.

## 3. The mapping table

| adapter_name | adapter_executable | adapter_source | standard_id | standard_repository | standard_executable | framework | version_relationship | contract_version |
|---|---|---|---|---|---|---|---|---|
| laravel | anvil-adapter-laravel | internal/laravel; cmd/laravel-adapter | anvil-standard-laravel | maleolabs/anvil-standard-laravel | anvil-adapter-laravel | Laravel | independent-lines | declared by the standard's lifecycle-model contract |
| flutter | anvil-adapter-flutter | internal/flutter; cmd/flutter-adapter | anvil-standard-flutter | maleolabs/anvil-standard-flutter | anvil-adapter-flutter | Flutter | independent-lines | declared by the standard's lifecycle-model contract |

Notes:

- The framework build templates (`execution/laravel_template.go`, `execution/flutter_template.go`) and the framework branch of `config/defaults.go` also left Core with their standard ([migration-guide-v2 §4](../migration-guide-v2.md); [ADR-025 §6.2](../adr/ADR-025-repository-split-core-vs-standards.md)). The `adapter_source` column lists the two named packages per framework; the template and config-defaults entries follow the same standard.
- `standard_executable` keeps the v1.x resolution name **by default** (see §4 column semantics). The value shown is the current default; a change to the standard executable naming/resolution contract is a governed breaking event ([ADR-025 §3.4, §12.1](../adr/ADR-025-repository-split-core-vs-standards.md); [migration-guide-v2 §3](../migration-guide-v2.md)).

## 4. Column semantics (stable contract)

The header row is the machine contract (see §7). Column names are lowercase snake_case and **must not change**; each column is defined here with its source.

| Column | Meaning | Source |
|---|---|---|
| `adapter_name` | The v1.x adapter identifier: the `<name>` argument of the v1.x CLI surface (`anvil adapter inspect <name>` / `anvil adapter use <name>` / `anvil init --framework <name>`) and the value of `project.framework` in `anvil.yaml` (v1.x). Unique per row. | migration-guide-v1.5; `internal/config/defaults.go` |
| `adapter_executable` | The v1.x adapter executable name: `anvil-adapter-<framework>` resolved on PATH / next to the CLI by closed-set discovery; a binary counts as an adapter only when it answers the `capabilities` probe. Unique per row. | `cmd/adapter_shared.go`; `cmd/adapter_list.go`; wiki |
| `adapter_source` | The v1.x monorepo locations that became the standard, `;`-separated. | ADR-025 §6.2; migration-guide-v2 §4; TS-016-01-01, TS-016-02-01 |
| `standard_id` | The registry standard identity: `anvil-standard-<framework>`, stable across releases of the standard. | ADR-025 §3; registry-metadata §4.1 (`id`) |
| `standard_repository` | The standard's repository under the `maleolabs` namespace. | ADR-025 §3 |
| `standard_executable` | The installed executable of the standard. The non-breaking default preserves the v1.x executable resolution contract (`anvil-adapter-<framework>`); changing this contract is a governed breaking event, so this column may change only through that process. | ADR-025 §3.4, §12.1–12.2; migration-guide-v2 §3 |
| `framework` | The framework the standard carries (Laravel, Flutter) — the natural-language anchor of the row. | v1.x adapter set (MVP-002) |
| `version_relationship` | The version relationship between the v1.x adapter line and the standard line. Value is the stable enum `independent-lines`; the schema is defined in §5. | ADR-025 §3, §4.7; registry-metadata §4.2; ADR-024 §3 |
| `contract_version` | The declared contract version each standard targets. Concrete values are **declared by the standard's lifecycle-model contract** (the per-release `contractVersion` field of the standard's registry metadata document, authored in the standard repository) and are not authored in Core; the relationship schema is defined in §6. | ADR-024 §3; registry-metadata §4.3; compatibility-matrix §2 |

## 5. Version relationship

The mapping is **identity-based, not version-based**. The correspondence v1.x adapter → standard holds for the identity (`adapter_name` / `adapter_executable` → `standard_id`), independent of any installed version. The two version lines are independent, which is what `version_relationship: independent-lines` records:

| Line | Behavior | Source |
|---|---|---|
| v1.x adapter line | Adapter binaries ship with the Core monorepo releases (v1.4.x, v1.5.0); after v1.5.0 the v1.x line moves to bugfix-only maintenance (v1.5.x). No adapter-specific version line exists. | migration-guide-v2 §5.1; ADR-028 §7; migration-guide-v1.5 |
| Standard line | Each standard carries its own independent semver line (major.minor.patch), released from its own repository with its own cadence; a standard update never requires a Core release and a Core update never silently breaks a standard that declares a supported contract version. | ADR-025 §3, §4.7; registry-metadata §4.2; ADR-024 §3 |
| Relationship | No 1:1 version correspondence exists between adapter releases and standard releases. Any installed first-party v1.x adapter maps to exactly one standard regardless of its installed version. Version compatibility between runtime and standard is negotiated at adoption (registry validation plus runtime verification) against the declared contract version — never assumed from version numbers. During the deprecation window, a runtime major may coexist with multiple standard versions. | Transition Plan §12.3; ADR-024 §3.6; ADR-023 §3; registry-metadata §4.2; ADR-021 §3.4 |

## 6. Declared contract version

**Policy (ADR-024 §3).** The delivery lifecycle specification carries its own independent semver version line, decoupled from runtime releases; the **contract major version is the unit of compatibility**. Every delivery lifecycle standard **declares which contract version it targets** (the Manifest/registry metadata declaration); a standard that does not declare compatibility is rejected. The Anvil Runtime implements at most two concurrently supported contract majors; the first contract major bump is a post-v2.0 governed event.

**Where the values live.** The concrete per-release contract version of each standard is the `contractVersion` field of the standard's registry metadata document ([registry-metadata §4.3](../specification-corpus/registry-metadata.md)) — authored in the standard's own repository (EPIC-016), not in Core. The `contract_version` column therefore records the relationship — **`declared by the standard's lifecycle-model contract`** — and does not invent values. The migration validation (TS-017-01-03, T-007) validates declared values at migration against the compatibility matrix and records the outcome (match and mismatch); declared values are additionally re-verified at runtime (ADR-024 §3.6).

**The reference the values are checked against.** The corpus records which contract versions the Anvil Runtime implements in the [compatibility matrix §2](../specification-corpus/compatibility-matrix.md): currently contract version **1.0.0**, supported contract majors **{1}** (recorded from the [version-line declaration](../specification-corpus/version-line.md)). This is the Core-side reference; the standards' declared values are validated against it — they are not asserted here.

## 7. Machine consumption contract (TS-017-01-02)

The §3 table is the **authoritative mapping** for the installed-adapter recognition and migration logic (TS-017-01-02) and for contract-version validation (TS-017-01-03). The consumption contract:

- **Header row is the field contract.** Column names are stable (`adapter_name`, `adapter_executable`, `adapter_source`, `standard_id`, `standard_repository`, `standard_executable`, `framework`, `version_relationship`, `contract_version`). Renaming a column is a breaking change for consumers and must be coordinated with TS-017-01-02.
- **Row semantics.** One row per first-party v1.x adapter; `adapter_name` and `adapter_executable` are unique per row and serve as lookup keys; multi-value cells are `;`-separated; cell values never contain `|` or newlines; row order is not part of the contract.
- **The recognition mechanism is not decided here.** Which identity source the runtime uses to recognize an installed adapter (manifest, config, or both) is an open implementation decision recorded as RFC-P7 in the [traceability matrix](ANVIL_V2_TRACEABILITY_MATRIX.md) (§5, ST-017-01). This table supplies the mapping data only.
- **Scope.** The table covers the first-party v1.x adapters. The v1.x closed set is not closed (any `anvil-adapter-<name>` answering the `capabilities` probe is discoverable, including third-party adapters); third-party adapters have no row here and are out of scope for the first-party mapping.
- **No hard-coding.** Consumers must not hard-code standard identity in code; this table is the single source, kept in sync with EPIC-016 outcomes (§8).

## 8. Sync and maintenance

- **Sync with the split repositories (EPIC-016).** Rows change only when an EPIC-016 outcome changes a repository or its naming (ADR-025 §3); the row set and the `standard_id` / `standard_repository` / `standard_executable` values are kept identical to the standard repositories' declared identity.
- **Later standards.** When a new first-party standard is created (after EPIC-016), a row is added here following the same column contract. Community standards are governed by ADR-034 and are not first-party rows.
- **Version and contract-version value changes** in the standard repositories (new releases, new declared `contractVersion`) do **not** change this table: those values are declared by the standard's lifecycle-model contract (§6), not authored here.
- **The migration guide references this artifact** ([migration-guide-v2 §3, §5.4, References](../migration-guide-v2.md)); guide prose does not duplicate the table.

## 9. Traceability

| Section | Source of truth |
|---|---|
| §1 Purpose | ADR-028 §3; Transition Plan §12.3; ADR-025 §3; TS-017-01-01; PRD-002 §5.12, §7.10 |
| §2 Document choice | migration-guide-v2 (compiled-document note); 006-g §1 (division of labor); TS-017-05-01 |
| §3 Mapping table | ADR-025 §3, §6.2; migration-guide-v2 §4; TS-016-01-01; TS-016-02-01; registry-metadata §4.1 |
| §4 Column semantics | ADR-025; registry-metadata §4.1–4.3; compatibility-matrix §2; migration-guide-v1.5; v1.x CLI/config surface |
| §5 Version relationship | ADR-025 §3, §4.7; registry-metadata §4.2; ADR-024 §3; ADR-021 §3.4; migration-guide-v2 §5.1 |
| §6 Declared contract version | ADR-024 §3; registry-metadata §4.3; compatibility-matrix §2; version-line §2; TS-017-01-03 |
| §7 Machine consumption contract | TS-017-01-02; ANVIL_V2_TRACEABILITY_MATRIX §5 (RFC-P7); wiki (closed-set discovery) |
| §8 Sync and maintenance | EPIC-016; ADR-025; ADR-034 |
| §9 Traceability | — |

---

*End of ANVIL_V2_ADAPTER_STANDARD_MAPPING.md*

*This document is compiled from approved sources (ADR-024, ADR-025, ADR-028, Transition Plan §12.3, specification corpus, migration guides). It introduces no new architecture, philosophy, or capabilities. Concrete contract version numbers are declared by the standard repositories' lifecycle-model contracts, not invented here.*
