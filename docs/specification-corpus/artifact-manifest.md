# Artifact Manifest Contract (Draft)

## The Delivery Lifecycle Specification — Artifact Manifest Contract

| Metadata | |
|---|---|
| **Document ID** | artifact-manifest |
| **Status** | Draft |
| **Date** | 2026-08-04 |
| **Product** | Anvil |
| **Dependencies** | [PRD-002 §5.3, §5.9](../prd/PRD-002-anvil-v2.md) · [ANVIL_V2_TRANSITION_PLAN §2.3, §6.4](../planning/ANVIL_V2_TRANSITION_PLAN.md) · [ANVIL_MANIFESTO §3.4, §5.2](../manifesto/ANVIL_MANIFESTO.md) · [ADR-004](../adr/ADR-004-artifact-architecture.md) · [ADR-021](../adr/ADR-021-delivery-lifecycle-standard-model.md) · [ADR-024](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md) · [ADR-029](../adr/ADR-029-specification-publication-format.md) · [ADR-033](../adr/ADR-033-verification-standard-generalization.md) · [ADR-035](../adr/ADR-035-governance-and-identity-reframing-amendments.md) · [007-delivery-lifecycle-standard-specification §4](../architecture/007-delivery-lifecycle-standard-specification.md) · [lifecycle-model](lifecycle-model.md) |
| **Consumers** | EPIC-015 (the runtime implements the contract) · TS-013-02-02 (machine-checkable schema) · EPIC-013 (conformance harness) · artifact producers · artifact consumers |

**Docs/schema authority rule (ADR-029 §3).** The delivery lifecycle specification is published in dual form — human-readable documentation plus a machine-readable JSON Schema. The JSON Schema is the machine-readable authority: **where this document and the schema disagree, the schema governs.** This document describes the contract; it does not describe the engine.

---

## 1. Purpose

This document is the artifact manifest contract part of the delivery lifecycle specification: the definition of how artifacts are identified, how identity is carried, and how integrity is established before any lifecycle operation may consume the artifact ([PRD-002 §5.9](../prd/PRD-002-anvil-v2.md); [Transition Plan §6.4](../planning/ANVIL_V2_TRANSITION_PLAN.md)).

It is written for an implementer who has never seen the engine source. Everything in this document is implementable from the contract alone:

- **Content-derived identity** — the same artifact installed twice is one release, not two; identity comes from content, not from names or paths.
- **Embedded manifest** — the manifest is carried inside the artifact, not stored separately.
- **Deterministic output** — identical inputs produce byte-identical artifacts.
- **Verification-before-trust** — the artifact is verified for integrity before any lifecycle operation consumes it.

**Contract, not engine.** The Anvil Runtime (Core) implements and enforces this contract; this document is the contract the runtime enforces, not a description of the runtime ([ADR-021 §3.1](../adr/ADR-021-delivery-lifecycle-standard-model.md)). The machine-checkable schema (TS-013-02-02) is the authoritative form; this document describes the semantics the schema encodes.

**Relationship to the lifecycle model.** The artifact manifest contract governs the **Artifact lifecycle stages** — Package and Verify — which precede the release lifecycle proper that begins at Install ([lifecycle-model §4.1](lifecycle-model.md)). The release lifecycle (Install → Activate → Rollback → Recover) is defined by the lifecycle model document; this document defines the contract artifacts satisfy before entering that lifecycle. The ownership decomposition was settled by decision 004-review-resolutions D3, not by this document.

---

## 2. Authority, Publication, and Governance

### 2.1 Position in the three-layer model

The artifact manifest contract is part of the **delivery lifecycle specification** — the authority every other layer conforms to ([Transition Plan §5.1](../planning/ANVIL_V2_TRANSITION_PLAN.md); [ADR-021 §3.1](../adr/ADR-021-delivery-lifecycle-standard-model.md)):

```text
Delivery Lifecycle Specification      ← what a legal lifecycle IS (the authority)
        │  implemented by                 this document is part of this layer
        ▼
Anvil Runtime (Core)                    ← the engine: enforces the specification
        │  executes
        ▼
delivery lifecycle standards         ← framework lifecycle content for one
        (anvil-standard-*)                 framework
```

The specification defines how artifacts are identified and verified; the runtime implements this contract; standards may extend verification with framework-specific checks, but they do not redefine artifact identity or manifest semantics ([Transition Plan §5.1](../planning/ANVIL_V2_TRANSITION_PLAN.md); [007 §6](../architecture/007-delivery-lifecycle-standard-specification.md)).

### 2.2 Publication format

- The artifact manifest contract is published in **dual form**: this human-readable document plus a machine-readable JSON Schema. The schema is the machine-readable authority; where the two disagree, the schema governs ([ADR-029 §3](../adr/ADR-029-specification-publication-format.md)).
- The specification corpus is authored **engine-path-independent**: it references no engine paths and no engine internals, so a future re-home of the specification is a move, not a rewrite ([ADR-029 §3](../adr/ADR-029-specification-publication-format.md); [Transition Plan §5.2, §5.10](../planning/ANVIL_V2_TRANSITION_PLAN.md)).
- The corpus is authored in the Core repository; there is no separate specification repository ([Transition Plan §5.2](../planning/ANVIL_V2_TRANSITION_PLAN.md)).

### 2.3 Versioning

- The specification carries its own independent semver version line, decoupled from runtime releases; the contract major version is the unit of compatibility ([ADR-024 §3.1](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).
- The runtime implements at most **two concurrently supported contract major versions**; a superseded contract major remains supported for one full contract generation — the deprecation window ([ADR-024 §3.4](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).
- Specification artifacts are published via a separate `spec/` tag line; engine and specification artifacts never share a tag ([ADR-024 §3.5](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).

### 2.4 Governed artifact

- The delivery lifecycle specification is a **governed architecture artifact of the Core repository** ([ADR-035 §3.1](../adr/ADR-035-governance-and-identity-reframing-amendments.md)).
- A breaking change to the artifact manifest contract — any change to identity semantics, manifest structure, or verification rules that breaks compatibility — is a **governed event**: it requires an ADR and ships with a Core major version ([ADR-035 §3.1](../adr/ADR-035-governance-and-identity-reframing-amendments.md); [ADR-024 §3.3](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).
- A contract major bump may invalidate standards that target a superseded major; those standards remain runnable for the deprecation window ([ADR-024 §3.7](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).

---

## 3. Terminology

This section uses the vocabulary of the delivery lifecycle specification; the full vocabulary is owned by Core and standards must not redefine its semantics ([PRD-002 §5.5](../prd/PRD-002-anvil-v2.md)). The terms below are reproduced for this document's convenience, and the vocabulary document prevails on any disagreement.

| Term | Definition | Source |
|---|---|---|
| **Artifact** | The first object Anvil defines: produced by packaging under the manifest contract, with content-derived identity and embedded verification evidence | Manifesto §5.2; PRD-002 §5.9 |
| **Manifest** | The structured metadata embedded in an artifact that declares the artifact's identity, content hash, and verification evidence | PRD-002 §5.9; Transition Plan §6.4 |
| **Content-derived identity** | An artifact's identity is determined by its content — specifically, by a cryptographic hash of the artifact's payload — not by its filename, path, or creation timestamp | PRD-002 §5.9; Manifesto §3.4 |
| **Deterministic output** | Given identical inputs (source content, manifest contract version, packaging parameters), the packaging operation produces byte-identical output regardless of when or where it runs | PRD-002 §5.9 |
| **Verification** | The integrity gate: the artifact is checked against its embedded manifest before any lifecycle operation may consume it | PRD-002 §5.3; Manifesto §3.2 |
| **Release** | The lifecycle entity created when a verified artifact is installed into a runtime; tracks lifecycle state from Ready onward | Manifesto §5.4; PRD-002 §7.7 |
| **Contract version** | The version of the delivery lifecycle specification an artifact's manifest conforms to | Transition Plan A2, §5.9 |

---

## 4. Contract Semantics

The artifact manifest contract defines four invariant properties. Every artifact produced under this contract satisfies all four; an object that does not is not an Anvil artifact.

### 4.1 Content-derived identity

An artifact's identity is derived from its content, not from any external naming convention.

**Rules:**

- The artifact's identity is a cryptographic hash of the artifact's payload — the content the artifact carries, excluding the manifest itself.
- The hash algorithm and encoding are declared in the manifest; the schema is the machine-readable authority for the algorithm specification.
- **The same artifact installed twice is one release, not two.** If two installations produce the same content hash, they resolve to the same identity and the second installation is idempotent — no new release is created ([PRD-002 §5.9](../prd/PRD-002-anvil-v2.md); [Manifesto §3.4, §5.4](../manifesto/ANVIL_MANIFESTO.md)).
- Identity is content-addressable: the identity uniquely identifies the content, and the content uniquely determines the identity.

**Source:** [PRD-002 §5.9](../prd/PRD-002-anvil-v2.md); [Manifesto §3.4, §5.2](../manifesto/ANVIL_MANIFESTO.md); [ADR-004](../adr/ADR-004-artifact-architecture.md); [lifecycle-model §4.2 (Install stage)](lifecycle-model.md).

### 4.2 Embedded manifest

The manifest is carried inside the artifact, not stored separately or referenced externally.

**Rules:**

- The manifest is embedded at a defined, discoverable location within the artifact structure. The exact location is schema-defined.
- The manifest declares: the content hash (identity), the manifest contract version, and any verification evidence the artifact carries.
- The manifest is produced at packaging time and travels with the artifact through every stage of the delivery lifecycle — packaging, distribution, installation, and verification.
- A manifest that is missing, malformed, or declares an unsupported contract version makes the artifact invalid; the runtime rejects it before any lifecycle operation.

**Rationale:** Embedding ensures that the artifact is self-describing: no external registry lookup, no sidecar file, no filesystem convention is required to determine the artifact's identity or to verify its integrity. The artifact carries everything needed to trust it.

**Source:** [PRD-002 §5.9](../prd/PRD-002-anvil-v2.md); [Manifesto §5.2](../manifesto/ANVIL_MANIFESTO.md); [Transition Plan §6.4](../planning/ANVIL_V2_TRANSITION_PLAN.md).

### 4.3 Deterministic output

Given identical inputs, the packaging operation produces byte-identical output.

**Rules:**

- Identical inputs means: the same source content, the same manifest contract version, and the same packaging parameters (if any).
- The packaging operation is a pure function of its inputs: no timestamps, no random values, no machine-specific data enter the artifact payload or the manifest's content hash.
- Two packaging runs on different machines, at different times, with the same inputs, produce artifacts with the same content hash — and therefore the same identity.
- If the packaging operation cannot be made deterministic for a given input set (for example, a dependency that embeds a build timestamp), the packaging operation fails rather than producing a non-deterministic artifact.

**Rationale:** Determinism enables reproducibility and auditability. A release can be rebuilt from source and verified to match the original artifact; a deployment can be traced to a specific, reproducible packaging operation. Non-determinism would make identity untrustworthy: two artifacts with different bytes would claim different identities, even if they carry the same logical content.

**Source:** [PRD-002 §5.9](../prd/PRD-002-anvil-v2.md); [Manifesto §5.2](../manifesto/ANVIL_MANIFESTO.md).

### 4.4 Filenames and paths carry no identity

An artifact's identity is determined solely by its content hash. Filenames, directory paths, and other filesystem metadata carry no identity information.

**Rules:**

- An artifact's identity is invariant under renaming or relocation: moving or renaming the artifact file does not change its identity.
- The runtime never uses filenames or paths to determine identity, compatibility, or contract version. All identity information is in the embedded manifest.
- Packaging operations do not encode identity in the filename; filenames are conventions for human convenience, not for machine trust.
- If two artifact files have different names but the same content hash, they are the same artifact. If they have the same name but different content hashes, they are different artifacts.

**Rationale:** Path-independence makes artifacts portable: an artifact can be stored in any directory, served from any URL, cached under any name, and its identity remains stable and verifiable. This property is required for registry-based distribution, offline/bundled installs, and the repository split ([Transition Plan §4](../planning/ANVIL_V2_TRANSITION_PLAN.md); [ADR-025](../adr/ADR-025-repository-split-core-vs-standards.md)).

**Source:** [PRD-002 §5.9](../prd/PRD-002-anvil-v2.md); [Manifesto §3.4](../manifesto/ANVIL_MANIFESTO.md).

---

## 5. Verification Before Trust

Verification is a lifecycle gate, not an optional command. An artifact is verified for integrity before any lifecycle operation — install, activate, or any subsequent stage — may consume it ([PRD-002 §5.3](../prd/PRD-002-anvil-v2.md); [Manifesto §3.2](../manifesto/ANVIL_MANIFESTO.md)).

### 5.1 The verification rule

- An artifact is **verified** when its content hash matches the hash declared in its embedded manifest. This is the integrity gate: the artifact's content has not been altered since packaging.
- Verification is **mandatory before any lifecycle operation consumes the artifact**. The runtime does not install, activate, or otherwise process an artifact whose integrity has not been established ([PRD-002 §5.3](../prd/PRD-002-anvil-v2.md); [Manifesto §3.2](../manifesto/ANVIL_MANIFESTO.md)).
- Verification is **re-checkable**: the evidence is embedded in the artifact (the manifest), not held in the memory of the verifying process. Any consumer — the runtime, a CI pipeline, a human operator — can re-verify the artifact at any time using the same embedded evidence ([PRD-002 §5.3](../prd/PRD-002-anvil-v2.md)).
- A **claim is not evidence**: the manifest declares the expected hash; the verification operation recomputes the hash from the artifact's content and compares. The embedded manifest is the claim; the recomputation is the evidence.

### 5.2 Verification scope

The artifact manifest contract defines the **integrity gate** — content hash verification. Two additional verification layers exist in the specification but are outside this contract:

| Verification layer | Defined by | Scope |
|---|---|---|
| **Integrity verification** (this contract) | Artifact manifest contract | Content hash matches manifest declaration |
| **Structural verification** | Verification contract (ST-013-04) | Artifact contains what the framework requires (files, structures) |
| **Lifecycle-conformity verification** | Verification contract + standard's Verification part | Framework behavior at lifecycle points (shared-resource wiring, migration timing, queue restart) |

All three layers are mandatory gates; none may be skipped. The runtime executes integrity verification first; structural and lifecycle-conformity verification follow if the integrity gate passes ([PRD-002 §5.3](../prd/PRD-002-anvil-v2.md); [007 §6](../architecture/007-delivery-lifecycle-standard-specification.md)).

### 5.3 Verification and the lifecycle model

Verification occurs at the **Verify stage** of the artifact lifecycle, which precedes the release lifecycle that begins at Install ([lifecycle-model §4.1](lifecycle-model.md)):

```text
Package → Verify → Install → Activate → ...
  │         │         │
  │         │         └── release lifecycle begins (lifecycle-model)
  │         └── integrity gate (this contract)
  └── artifact production (this contract)
```

An artifact that fails verification is rejected; no lifecycle operation proceeds from unverified inputs (R1 in [lifecycle-model §6.2](lifecycle-model.md)).

**Source:** [PRD-002 §5.3](../prd/PRD-002-anvil-v2.md); [Manifesto §3.2, §5.3](../manifesto/ANVIL_MANIFESTO.md); [ADR-033](../adr/ADR-033-verification-standard-generalization.md); [lifecycle-model §4.2 (Verify stage)](lifecycle-model.md).

---

## 6. Consumers

### 6.1 Anvil Runtime (Core)

The Anvil Runtime is the primary consumer of this contract. It implements the artifact manifest contract by:

- **Producing artifacts** that satisfy all four contract semantics (content-derived identity, embedded manifest, deterministic output, path-independence).
- **Verifying artifacts** before any lifecycle operation consumes them — the verification-before-trust gate.
- **Installing artifacts** idempotently by content identity — the same artifact installed twice is one release, not two ([lifecycle-model §6.2 R7](lifecycle-model.md); [lifecycle-model §6.3](lifecycle-model.md)).
- **Rejecting artifacts** that violate the contract: missing or malformed manifest, unsupported contract version, content hash mismatch, non-deterministic output.

The runtime's implementation of this contract is validated by the conformance harness (EPIC-013), which checks engine behavior against the specification ([ADR-029 §3](../adr/ADR-029-specification-publication-format.md)).

**Source:** [PRD-002 §5.9](../prd/PRD-002-anvil-v2.md); [EPIC-015](../epics/EPIC-015-core-framework-free-refactoring.md).

### 6.2 Machine-checkable schema (TS-013-02-02)

The machine-checkable JSON Schema (TS-013-02-02, lands as T-009) encodes the contract defined in this document. The schema is the machine-readable authority: where this document and the schema disagree, the schema governs ([ADR-029 §3](../adr/ADR-029-specification-publication-format.md)).

The schema enables:

- **Automated validation** of artifact manifests against the contract, without human interpretation.
- **Conformance checking** of runtime behavior against the specification, in CI.
- **Independent implementation** of the contract by external runtimes that consume the schema, not engine source.

**Scope note:** The schema itself is outside the scope of this document (TS-013-02-01). This document defines the contract the schema encodes; the schema is the authoritative machine-readable form.

**Source:** [ADR-029 §3](../adr/ADR-029-specification-publication-format.md); [TS-013-02-02](../work-items/technical-stories/TS-013-02-02-artifact-manifest-contract-json-schema.md).

### 6.3 Delivery lifecycle standards

Standards are indirect consumers: they extend verification with framework-specific structural and lifecycle-conformity checks (007 §6), but they do not redefine artifact identity or manifest semantics. A standard that violates the artifact manifest contract is rejected by the registry, not patched by Core ([007 §2](../architecture/007-delivery-lifecycle-standard-specification.md); [Transition Plan §5.5](../planning/ANVIL_V2_TRANSITION_PLAN.md)).

---

## 7. Scope Boundaries

This document defines the artifact manifest contract only. Related contracts of the specification are separate corpus documents (authored in EPIC-013):

| Not defined here | Where it is defined |
|---|---|
| Lifecycle model (stages, transitions, state machine semantics) | [lifecycle-model.md](lifecycle-model.md) |
| Standard command contract (runtime ↔ standard exchange) | Its own specification corpus document (ST-013-03) |
| Verification contract (gate semantics, evidence requirements beyond integrity) | Its own specification corpus document (ST-013-04) |
| Vocabulary definitions | Specification corpus vocabulary document (TS-013-01-03) |
| Machine-readable form of this contract | JSON Schema (TS-013-02-02) — the authority per ADR-029 §3 |
| Engine behavior | The runtime (EPIC-015) implements this contract; engine internals are not part of the specification |

---

## 8. Traceability

| Section | Source of truth |
|---|---|
| §1 Purpose | PRD-002 §5.9; Transition Plan §6.4; Manifesto §3.4, §5.2; ADR-004 |
| §2 Authority, publication, governance | ADR-024 §3; ADR-029 §3; ADR-035 §3; Transition Plan §5.1–§5.2, §5.10 |
| §3 Terminology | 007 §3; Transition Plan §5.1, §5.9; Manifesto §5; PRD-002 §5.5, §5.9; TS-013-01-03 |
| §4 Contract semantics | PRD-002 §5.9; Manifesto §3.4, §5.2; ADR-004; Transition Plan §2.3, §6.4 |
| §5 Verification before trust | PRD-002 §5.3; Manifesto §3.2, §5.3; ADR-033; 007 §6; lifecycle-model §4.1–§4.2, §6.2 |
| §6 Consumers | PRD-002 §5.9; ADR-029 §3; 007 §2; Transition Plan §5.5; EPIC-015 |
| §7 Scope boundaries | EPIC-013; ADR-029 §3 |
| §8 Traceability | — |

---

*End of artifact-manifest.md*
