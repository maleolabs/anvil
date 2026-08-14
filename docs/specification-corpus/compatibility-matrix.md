# Compatibility Matrix (Draft)

## The Compatibility Matrix of the Delivery Lifecycle Specification

| Metadata | |
|---|---|
| **Document ID** | compatibility-matrix |
| **Status** | Draft |
| **Date** | 2026-08-04 |
| **Product** | Anvil |
| **Dependencies** | [PRD-002 §5.1, §5.8](../prd/PRD-002-anvil-v2.md) · [ANVIL_V2_TRANSITION_PLAN §3 (A2), §5.9, §12.3](../planning/ANVIL_V2_TRANSITION_PLAN.md) · [ANVIL_MANIFESTO §3.6](../manifesto/ANVIL_MANIFESTO.md) · [ADR-021](../adr/ADR-021-delivery-lifecycle-standard-model.md) · [ADR-023](../adr/ADR-023-delivery-lifecycle-standard-registry.md) · [ADR-024](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md) · [ADR-029](../adr/ADR-029-specification-publication-format.md) · [ADR-035](../adr/ADR-035-governance-and-identity-reframing-amendments.md) · [007-delivery-lifecycle-standard-specification §8, §10](../architecture/007-delivery-lifecycle-standard-specification.md) · [version-line](version-line.md) · [vocabulary](vocabulary.md) |
| **Consumers** | registry validation (EPIC-014) · delivery lifecycle standard authors · EPIC-013 (conformance harness) · EPIC-015 (the runtime implements the specification) · Core release tooling |

**Recorded, not assumed (ADR-024 §3.6).** This document is the corpus's compatibility matrix: it records which contract versions the Anvil Runtime implements. The record follows the version line declared in [version-line](version-line.md) — that document is the declaration point (its **Version** and **Supported contract majors** metadata rows are the machine-readable declaration consumed by the version tooling); this matrix consumes and records that declared state. The matrix records declared compatibility; it never assumes it.

---

## 1. Purpose

This document is the compatibility matrix part of the delivery lifecycle specification corpus: the recorded statement of which contract versions the Anvil Runtime implements, and the reference that a delivery lifecycle standard's declared target contract version is checked against ([PRD-002 §5.1, §5.8](../prd/PRD-002-anvil-v2.md); [007 §8, §10](../architecture/007-delivery-lifecycle-standard-specification.md)).

It is written for everyone who declares or validates compatibility:

- **Registry validation (EPIC-014)** — checks a standard's declared contract version against this matrix at adoption ([ADR-023 §3](../adr/ADR-023-delivery-lifecycle-standard-registry.md)).
- **Delivery lifecycle standard authors** — declare a target contract version that is valid against the implemented set recorded here ([007 §4, §8](../architecture/007-delivery-lifecycle-standard-specification.md)).
- **The conformance harness (EPIC-013)** — validates engine behavior against the declared contract versions ([ADR-024 §6](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).
- **Runtime implementers (EPIC-015)** — the implemented set recorded here is the runtime's compatibility basis.

Consistent with the corpus:

- **Engine-path-independent.** This document records compatibility for the corpus, not how the Anvil Runtime implements it; it references no engine paths and no engine internals ([ADR-029 §3](../adr/ADR-029-specification-publication-format.md)).
- **No new policy.** Every rule referenced here is drawn from ADR-024 and the version line (TS-013-05-01); the matrix records within those bounds, it does not extend them.
- **A record, not a declaration.** The version line declares; this matrix records. The matrix is not a second declaration of the version line — it is a consumer of [version-line](version-line.md) and must stay consistent with it.

## 2. The matrix

The matrix records the contract versions the Anvil Runtime implements. The **contract major version is the unit of compatibility** ([ADR-024 §3.1](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)); minor and patch releases within a contract major are backward compatible and do not change the unit, so the matrix records the implemented contract version and the supported contract major set.

**Recorded state (current).** The implemented contract version and the supported major set are exactly the declaration in [version-line](version-line.md)'s metadata: Version **1.0.0**, supported contract majors **{1}**.

| Implemented contract version | Status | Notes |
|---|---|---|
| **1.0.0** | **Current** | The initial contract version of the delivery lifecycle specification: the version line opens at 1.0.0 and contract major 1 is the initial contract major ([version-line §2](version-line.md); [ADR-024 §3.3](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)). Supported contract majors: {1}. |

**Schema change note (recorded decision, TS-021-04; ADR-037 §6).** The `skills[]` extension to the registry metadata format is **additive-only** (one optional section; no change to required fields, types, constraints, or trust semantics — see [registry-metadata §4.8](registry-metadata.md)). Additive extensions within a contract major do not change the compatibility unit ([ADR-024 §3.1](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)), so the declared contract version and the supported major set are **unchanged**: still **1.0.0**, supported contract majors **{1}**. The parser tolerance for the extension is the deprecation-window decision recorded in [registry-metadata §4.8](registry-metadata.md): older parsers accept and ignore unknown-but-optional sections; core fields stay strict; malformed skill declarations are rejected. No contract-major row changes here; the next governed major bump would advance this matrix under the ADR-024 §3.4 bounds (§4).

**The supported-majors window.** The matrix always records a supported-major set within the bounds of ADR-024 §3.4: at most two concurrently supported contract majors, with a superseded major supported for one full contract generation — the deprecation window. The window mechanics, illustrated (the numbers are an illustration of the mechanics, not an additional rule — consistent with [version-line §3](version-line.md)):

| Line state | Declared version | Supported contract majors | Deprecation window |
|---|---|---|---|
| Initial line (current) | 1.x.y | 1 | — |
| First major bump ships | 2.x.y | 1, 2 | major 1 superseded, still supported |
| Next major bump ships | 3.x.y | 2, 3 | major 1 window closes; major 2 superseded, still supported |

The recorded state of the matrix is the first row: the runtime implements contract version 1.0.0 (contract major 1). The other rows are the mechanics the matrix will record when a governed major bump ships (§4).

## 3. Declaration flow

Compatibility is declared, validated, and recorded — not assumed ([ADR-024 §3.6](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md); [Transition Plan §12.3](../planning/ANVIL_V2_TRANSITION_PLAN.md)):

- **The runtime implements.** The Anvil Runtime implements contract version 1.0.0; the implemented set is recorded in this matrix ([ADR-024 §3.2](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).
- **Standards declare.** A delivery lifecycle standard declares the contract version it targets — in the Manifest part and the Compatibility part of the standard structure ([007 §4, §8](../architecture/007-delivery-lifecycle-standard-specification.md); [ADR-021](../adr/ADR-021-delivery-lifecycle-standard-model.md)). A standard that does not declare compatibility is rejected ([PRD-002 §5.8](../prd/PRD-002-anvil-v2.md)).
- **Validated at adoption.** The registry validates the declaration at adoption: adoption-time validation checks the declared contract version against this matrix — the reference those declarations are checked against ([ADR-023 §3](../adr/ADR-023-delivery-lifecycle-standard-registry.md)).
- **Re-verified at runtime.** The runtime re-verifies compatibility when it executes the standard ([007 §8](../architecture/007-delivery-lifecycle-standard-specification.md); [ADR-024 §3.6](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).
- **Recorded.** This matrix records the declared compatibility; it never assumes it ([ADR-024 §3.6](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).

| Step | Actor | Action | Source |
|---|---|---|---|
| 1 | Anvil Runtime (Core) | Implements contract version 1.0.0; the implemented set is recorded in this matrix | ADR-024 §3.2 |
| 2 | delivery lifecycle standard | Declares the contract version it targets (Manifest + Compatibility parts) | 007 §4, §8; ADR-021 |
| 3 | Registry | Validates the declared contract version at adoption against this matrix | ADR-023 §3 |
| 4 | Anvil Runtime | Re-verifies compatibility when it executes the standard | ADR-024 §3.6; 007 §8 |
| 5 | This matrix | Records the declared compatibility — never assumes it | ADR-024 §3.6 |

## 4. Maintenance path

- **The matrix follows the version line.** The state this matrix records — Version 1.0.0, supported contract majors {1} — is declared in [version-line](version-line.md) and validated automatically against the ADR-024 bounds by the version tooling (TS-013-05-01; [version-line §6](version-line.md)). The version tooling depends on no compatibility matrix file; the matrix is a consumer of the version line, so it cannot drift from it ([version-line §6](version-line.md)).
- **When the matrix updates.** The matrix updates when a contract major is introduced or superseded under the bounds of TS-013-05-01: at most two concurrently supported contract majors; a superseded major remains supported for one full contract generation — the deprecation window ([ADR-024 §3.4](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)). Minor and patch bumps of the version line within a contract major do not change the compatibility unit ([ADR-024 §3.1](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)); they keep the record consistent with the declaration but do not change its status rows.
- **How the matrix changes on a major bump.** A breaking (major) contract change is a governed event: it requires an ADR and ships with a Core major version ([ADR-024 §3.3](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)). When it ships, the release tooling advances the version-line declaration by one generation — the old major enters the deprecation window ([version-line §5, §6](version-line.md)) — and this matrix is updated to record the new state. The mechanics, illustrated (illustration of the mechanics, not an additional rule):

| Line state | What the matrix records |
|---|---|
| Initial line (current) | 1.0.0 — current; supported majors {1} |
| First major bump ships | 2.0.0 — current; major 1 — superseded, still supported (deprecation window); supported majors {1, 2} |
| Next major bump ships | 3.0.0 — current; major 2 — superseded, still supported; major 1 window closes; supported majors {2, 3} |

- **Corpus release process.** The matrix is maintained as part of the corpus release process: it is published with the rest of the corpus via the separate `spec/` tag line, which never shares a tag with engine artifacts ([ADR-024 §3.5](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md); [version-line §4](version-line.md)).
- **Consistency is the maintenance rule.** The matrix updates in the same release step that advances the version-line declaration; the two documents never drift apart, because the matrix records exactly the state the declaration moves to.

## 5. Machine-readable form

The machine-readable form of the matrix is the corpus file [compatibility-matrix.json](compatibility-matrix.json) — the same record as valid JSON, engine-path-independent and versioned with the corpus through the same release process (§4). The record is kept minimal: it contains only the facts this document records, with no derived or invented fields.

The record shape:

| Field | Value | Meaning |
|---|---|---|
| `document_id` | `compatibility-matrix` | The corpus identity of this record |
| `contract_version` | `1.0.0` | The current contract version of the delivery lifecycle specification, recorded from the version-line declaration — the contract version the Anvil Runtime implements |
| `supported_majors` | `[1]` | The supported contract major set, recorded from the version-line declaration; within the ADR-024 §3.4 bounds |
| `maintained_under` | (note) | The maintenance path: the matrix is maintained as part of the corpus release process, follows the version-line declaration, and updates only under the ADR-024 §3.4 bounds (TS-013-05-01) |

The record is the machine-readable reference for registry validation (EPIC-014) and any automated compatibility check. Note: the matrix is a record, not a contract — its machine-readable form is the JSON record itself, not a JSON Schema. The docs/schema authority rule of [ADR-029 §3](../adr/ADR-029-specification-publication-format.md) governs the specification's contract documents; the matrix's two forms record the same facts and are kept consistent by the maintenance path (§4).

## 6. Scope Boundaries

| Not defined here | Where it is defined |
|---|---|
| The version line (declared version, supported-majors declaration, bounds) | [version-line.md](version-line.md) (TS-013-05-01) |
| The compatibility bounds (at most two concurrent majors; one-generation window) | ADR-024 §3.4 — the matrix records within the bounds; it does not set them |
| The standard's Manifest and Compatibility parts (target contract version declaration) | 007 §4, §8; ADR-021 |
| Registry validation at adoption | ADR-023 §3; EPIC-014 |
| Runtime verification at execution | ADR-024 §3.6; 007 §8 |
| Conformance of engine behavior to the declared contract versions | EPIC-013 (conformance harness) |
| Engine behavior | The runtime (EPIC-015) implements the specification; engine internals are not part of the specification |

## 7. Traceability

| Section | Source of truth |
|---|---|
| §1 Purpose | PRD-002 §5.1, §5.8; Transition Plan §3 (A2), §5.9, §12.3; Manifesto §3.6; ADR-024 §3, §6; ADR-029 §3; 007 §8, §10; TS-013-05-01 |
| §2 The matrix | ADR-024 §3.1, §3.3, §3.4; version-line §2, §3 |
| §3 Declaration flow | ADR-024 §3.2, §3.6; ADR-023 §3; ADR-021; 007 §4, §8; PRD-002 §5.8; Transition Plan §12.3 |
| §4 Maintenance path | ADR-024 §3.1, §3.3, §3.4, §3.5; version-line §4, §5, §6; TS-013-05-01 |
| §5 Machine-readable form | ADR-024 §3.5; ADR-029 §3; TS-013-05-02 |
| §6 Scope boundaries | EPIC-013; ADR-029 §3; ADR-024 §3.4 |
| §7 Traceability | — |

---

*End of compatibility-matrix.md*
