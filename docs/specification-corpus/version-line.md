# Version Line (Draft)

## The Version Line of the Delivery Lifecycle Specification

| Metadata | |
|---|---|
| **Document ID** | version-line |
| **Status** | Draft |
| **Date** | 2026-08-04 |
| **Product** | Anvil |
| **Version** | 1.0.0 |
| **Supported contract majors** | 1 |
| **Dependencies** | [PRD-002 §5.1, §5.5](../prd/PRD-002-anvil-v2.md) · [ANVIL_V2_TRANSITION_PLAN §3 (A2), §5.1–§5.2, §8, §11.1–§11.2, §12.1](../planning/ANVIL_V2_TRANSITION_PLAN.md) · [ANVIL_MANIFESTO §3.6](../manifesto/ANVIL_MANIFESTO.md) · [ADR-021](../adr/ADR-021-delivery-lifecycle-standard-model.md) · [ADR-023](../adr/ADR-023-delivery-lifecycle-standard-registry.md) · [ADR-024](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md) · [ADR-029](../adr/ADR-029-specification-publication-format.md) · [ADR-035](../adr/ADR-035-governance-and-identity-reframing-amendments.md) · [007-delivery-lifecycle-standard-specification §3](../architecture/007-delivery-lifecycle-standard-specification.md) · [vocabulary](vocabulary.md) · [lifecycle-model](lifecycle-model.md) |
| **Consumers** | EPIC-013 (specification corpus · conformance harness) · EPIC-015 (the runtime implements the specification) · delivery lifecycle standard authors · registry validation (EPIC-014) · Core release tooling |

**Version declaration (ADR-024 §3).** This document is the corpus's version-line declaration point: the **Version** and **Supported contract majors** metadata rows above are the machine-readable declaration of the specification's version line, consumed by the version tooling. The declaration is validated automatically against the ADR-024 bounds; it is not a matter of documentation taste.

---

## 1. Purpose

This document defines the delivery lifecycle specification's version line: the independent semver line (major.minor.patch) the specification carries, decoupled from Anvil Runtime (Core) releases; the compatibility bounds the line enforces; and the mechanics by which the specification is versioned and published ([ADR-024 §3](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md); [Transition Plan §3 (A2)](../planning/ANVIL_V2_TRANSITION_PLAN.md)).

It is written for everyone who versions or consumes the specification: Core maintainers running the release tooling, delivery lifecycle standard authors declaring a target contract version, runtime implementers validating compatibility, and the registry validating standards at adoption ([ADR-023 §3](../adr/ADR-023-delivery-lifecycle-standard-registry.md); [ADR-021 §3](../adr/ADR-021-delivery-lifecycle-standard-model.md)).

Consistent with the corpus:

- **Engine-path-independent.** This document defines the version line for the corpus, not how the Anvil Runtime implements it; it references no engine paths and no engine internals (ADR-029 §3).
- **No new policy.** Every rule in this document is drawn from ADR-024; the mechanics section (publication, tooling) implements those rules, it does not extend them.

## 2. The version line

- The specification carries its own **independent semver version line** (major.minor.patch), decoupled from Anvil Runtime (Core) releases; the **contract major version is the unit of compatibility** ([ADR-024 §3.1](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)). A standard's declared contract version and a runtime's implemented contract major are the compatibility basis between them — declared, validated, and recorded, not assumed (A2; ADR-024 §3.6).
- Within a contract major, minor and patch releases are backward compatible: they do not change the compatibility unit (ADR-024 §3.1).
- The version line opens at **1.0.0** — contract major 1 is the initial contract major, and the **first contract major bump is a post-v2.0 governed event** ([ADR-024 §3.3](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md); [Transition Plan §12.1](../planning/ANVIL_V2_TRANSITION_PLAN.md)).
- The current version and the supported contract majors are declared in this document's metadata and validated automatically by the version tooling (§6).

## 3. Compatibility bounds

- The Anvil Runtime implements **at most TWO concurrently supported contract major versions** ([ADR-024 §3.4](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).
- A **superseded contract major remains supported for one full contract generation** — until the next contract major ships and Core moves to it. This is the **deprecation window** (ADR-024 §3.4).
- The supported majors are therefore always the current major and its immediate predecessor: when the next major ships, the oldest supported major drops out of the window (ADR-024 §3.4). The `Supported contract majors` declaration always lists exactly this set — at most two majors, consecutive.
- Standards targeting a superseded contract major remain runnable for the deprecation window; a contract major bump may invalidate standards that have not migrated to a supported major ([ADR-024 §3.7](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).

The window mechanics, illustrated (the numbers are an illustration of the mechanics, not an additional rule):

| Line state | Declared version | Supported contract majors | Deprecation window |
|---|---|---|---|
| Initial line (current) | 1.x.y | 1 | — |
| First major bump ships | 2.x.y | 1, 2 | major 1 superseded, still supported |
| Next major bump ships | 3.x.y | 2, 3 | major 1 window closes; major 2 superseded, still supported |

## 4. Publication: the `spec/` tag line

- Specification artifacts are published via a **separate `spec/` tag line** in the Core repository (co-versioning mechanics); **engine and specification artifacts never share a tag** ([ADR-024 §3.5](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).
- The tag format is `spec/<major>.<minor>.<patch>` (e.g., `spec/1.0.0`). The `v*` tag format is reserved for engine releases; the two tag lines are disjoint and never overlap (ADR-024 §3.5).
- The specification is published in **dual form** — human-readable documentation plus a machine-readable JSON Schema; the schema is the machine-readable authority where the two disagree ([ADR-029 §3](../adr/ADR-029-specification-publication-format.md)).
- The corpus is authored in the Core repository and engine-path-independent, so a future re-home is a move, not a rewrite ([Transition Plan §5.2](../planning/ANVIL_V2_TRANSITION_PLAN.md); ADR-029 §3).
- Release mechanics: the Core repository's release tooling bumps the declaration in this document and creates the `spec/` tag (`scripts/spec-release.sh`); a release workflow publishes the corpus artifacts on `spec/*` tags (`.github/workflows/spec-release.yml`). The engine release line (`v*` tags, `scripts/bump.sh`) is untouched by the `spec/` mechanics; the public-mirror tag sync mirrors `v*` tags only, so `spec/` tags remain in the Core repository.

## 5. Governance of version changes

- A **breaking (major) contract change is a governed event**: it requires an ADR and ships with a Core major version ([ADR-024 §3.3](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md); [ANVIL_MANIFESTO §3.6](../manifesto/ANVIL_MANIFESTO.md); [ADR-035 §3.1](../adr/ADR-035-governance-and-identity-reframing-amendments.md)). The first contract major bump is post-v2.0 (Transition Plan §12.1; ADR-024 §3.3).
- Minor and patch changes within a contract major are backward compatible and do not trigger the governed-event process (ADR-024 §3.1).
- A contract major bump may invalidate standards that have not migrated; they remain runnable for the deprecation window (ADR-024 §3.7).
- On a major bump, the release tooling advances the `Supported contract majors` declaration by one generation (old major enters the deprecation window); it does not decide the bump — the governed decision is made before the tooling runs.

## 6. Version tooling

- **Automatic validation.** The Core repository's `scripts/validate-contract-version.sh` validates the declaration in this document against the bounds of §2–§3: well-formed semver; the declared contract major within the supported range; at most two supported contract majors; consecutive generations; and consistency with the released `spec/` tag line (a major bump advances one generation at a time; the line opens at contract major 1). The validation is wired into the repository's CI quality stage and runs before every spec release, so the declaration cannot drift silently ([ADR-024 §6](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md): version tooling must validate compatibility automatically; [Transition Plan §11.1](../planning/ANVIL_V2_TRANSITION_PLAN.md): release-stall mitigation).
- **Self-contained.** The validation depends on no compatibility matrix file (the machine-readable matrix is a separate work item, TS-013-05-02, which consumes this version line) and on no JSON Schema.
- **Release execution.** `scripts/spec-release.sh` bumps the declared version, updates the supported-majors declaration on a major bump, validates the result, commits, and creates the `spec/` tag — never a `v*` tag.

## 7. Traceability

| Section | Source of truth |
|---|---|
| §1 Purpose | ADR-024 §3; Transition Plan §3 (A2), §5.1; ADR-021 §3; ADR-023 §3 |
| §2 The version line | ADR-024 §3.1, §3.3, §3.6; Transition Plan A2, §12.1 |
| §3 Compatibility bounds | ADR-024 §3.4, §3.7 |
| §4 Publication | ADR-024 §3.5; ADR-029 §3; Transition Plan §5.2 |
| §5 Governance | ADR-024 §3.3, §3.7; Manifesto §3.6; ADR-035 §3.1; Transition Plan §12.1 |
| §6 Version tooling | ADR-024 §6; Transition Plan §11.1; TS-013-05-02 |
| §7 Traceability | — |

---

*End of version-line.md*
