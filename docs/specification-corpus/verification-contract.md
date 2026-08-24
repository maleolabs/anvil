# Verification Contract (Draft)

## The Delivery Lifecycle Specification — Verification Contract

| Metadata | |
|---|---|
| **Document ID** | verification-contract |
| **Status** | Draft |
| **Date** | 2026-08-04 |
| **Product** | Anvil |
| **Dependencies** | [PRD-002 §4.1, §5.3](../prd/PRD-002-anvil-v2.md) · [ANVIL_V2_TRANSITION_PLAN §3 A8, §5.1, §5.4](../planning/ANVIL_V2_TRANSITION_PLAN.md) · [ANVIL_MANIFESTO §3.2, §4.1, §5.3, §6, §7, §10](../manifesto/ANVIL_MANIFESTO.md) · [ADR-021](../adr/ADR-021-delivery-lifecycle-standard-model.md) · [ADR-024](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md) · [ADR-029](../adr/ADR-029-specification-publication-format.md) · [ADR-033](../adr/ADR-033-verification-standard-generalization.md) · [ADR-035](../adr/ADR-035-governance-and-identity-reframing-amendments.md) · [007-delivery-lifecycle-standard-specification §6](../architecture/007-delivery-lifecycle-standard-specification.md) · [lifecycle-model](lifecycle-model.md) · [artifact-manifest](artifact-manifest.md) |
| **Consumers** | EPIC-015 (the runtime implements the contract) · TS-013-04-02 (machine-checkable schema) · EPIC-013 (conformance harness) · EPIC-018 (verification content authoring) · delivery lifecycle standard authors · registry validation (EPIC-014) |

**Docs/schema authority rule (ADR-029 §3).** The delivery lifecycle specification is published in dual form — human-readable documentation plus a machine-readable JSON Schema. The JSON Schema is the machine-readable authority: **where this document and the schema disagree, the schema governs.** This document describes the contract; it does not describe the engine.

---

## 1. Purpose

This document is the verification contract part of the delivery lifecycle specification: the definition of verification as a lifecycle gate — its gate semantics and its evidence requirements ([PRD-002 §5.3](../prd/PRD-002-anvil-v2.md); [Transition Plan §5.1](../planning/ANVIL_V2_TRANSITION_PLAN.md)). It is the contract that belongs to the specification, not to any standard ([007 §6](../architecture/007-delivery-lifecycle-standard-specification.md); [ADR-033 §3](../adr/ADR-033-verification-standard-generalization.md)).

It is written for an implementer who has never seen the engine source. Everything in this document is implementable from the contract alone:

- **Gate semantics** — verification is a lifecycle gate: mandatory and unskippable before a release may advance; a standard adds checks, it never weakens gates.
- **Evidence requirements** — evidence is re-checkable, not merely claimed; a claim is not evidence; verification outcomes merge into the runtime's verification report and are recorded as lifecycle evidence.
- **The contract/content boundary** — the contract belongs to the delivery lifecycle specification; standards supply verification content — structural checks and lifecycle-conformity checks — against it.

**Contract, not engine.** The Anvil Runtime (Core) implements and enforces this contract; this document is the contract the runtime enforces, not a description of the runtime ([ADR-021 §3.1](../adr/ADR-021-delivery-lifecycle-standard-model.md)). The machine-checkable schema (TS-013-04-02) is the authoritative form; this document describes the semantics the schema encodes ([ADR-029 §3](../adr/ADR-029-specification-publication-format.md)).

**Enforceability is the point.** Verification is the proof mechanism of the standard ([Transition Plan §3 A8](../planning/ANVIL_V2_TRANSITION_PLAN.md); [ADR-033 §1](../adr/ADR-033-verification-standard-generalization.md)): documentation is a claim; an enforced, verified convention has a failure mode ([Manifesto §6](../manifesto/ANVIL_MANIFESTO.md); [ADR-033 §5](../adr/ADR-033-verification-standard-generalization.md)). The rules in this document are what the runtime encodes and enforces — a gate that fails rejects the operation that violates it, it does not warn.

---

## 2. Authority, Publication, and Governance

### 2.1 Position in the three-layer model

The verification contract is part of the **delivery lifecycle specification** — the authority every other layer conforms to ([Transition Plan §5.1](../planning/ANVIL_V2_TRANSITION_PLAN.md); [ADR-021 §3.1](../adr/ADR-021-delivery-lifecycle-standard-model.md)):

```text
Delivery Lifecycle Specification      ← what a legal lifecycle IS (the authority)
        │  implemented by                 this document is part of this layer
        ▼
Anvil Runtime (Core)                    ← the engine: enforces the specification,
        │  executes                      executes declared verification content
        ▼
delivery lifecycle standards         ← framework lifecycle content for one
        (anvil-standard-*)                 framework, including its Verification
                                           part (structural + lifecycle-conformity
                                           checks)
```

The specification defines the verification contract — gate semantics, evidence requirements; the runtime enforces it; standards supply verification content against it, within the defined contract ([Transition Plan §5.1](../planning/ANVIL_V2_TRANSITION_PLAN.md); [ADR-033 §3](../adr/ADR-033-verification-standard-generalization.md)).

### 2.2 Publication format

- The verification contract is published in **dual form**: this human-readable document plus a machine-readable JSON Schema. The schema is the machine-readable authority; where the two disagree, the schema governs ([ADR-029 §3](../adr/ADR-029-specification-publication-format.md)).
- The specification corpus is authored **engine-path-independent**: it references no engine paths and no engine internals, so a future re-home of the specification is a move, not a rewrite ([ADR-029 §3](../adr/ADR-029-specification-publication-format.md); [Transition Plan §5.2, §5.10](../planning/ANVIL_V2_TRANSITION_PLAN.md)).
- The corpus is authored in the Core repository; there is no separate specification repository ([Transition Plan §5.2](../planning/ANVIL_V2_TRANSITION_PLAN.md)).

### 2.3 Versioning

- The specification carries its own independent semver version line, decoupled from runtime releases; the contract major version is the unit of compatibility ([ADR-024 §3.1](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).
- The runtime implements at most **two concurrently supported contract major versions**; a superseded contract major remains supported for one full contract generation — the deprecation window ([ADR-024 §3.4](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).
- Specification artifacts are published via a separate `spec/` tag line; engine and specification artifacts never share a tag ([ADR-024 §3.5](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).

### 2.4 Governed artifact

- The delivery lifecycle specification is a **governed architecture artifact of the Core repository** ([ADR-035 §3.1](../adr/ADR-035-governance-and-identity-reframing-amendments.md)).
- A breaking change to the verification contract — any change to gate semantics or evidence requirements that breaks compatibility — is a **governed event**: it requires an ADR and ships with a Core major version ([ADR-035 §3.1](../adr/ADR-035-governance-and-identity-reframing-amendments.md); [ADR-024 §3.3](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).
- A contract major bump may invalidate standards that target a superseded major; those standards remain runnable for the deprecation window ([ADR-024 §3.7](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).

---

## 3. Terminology

This section uses the vocabulary of the delivery lifecycle specification; the full vocabulary is owned by Core and standards must not redefine its semantics ([PRD-002 §5.5](../prd/PRD-002-anvil-v2.md)). The terms below are reproduced for this document's convenience, and the vocabulary document (TS-013-01-03) prevails on any disagreement.

| Term | Definition | Source |
|---|---|---|
| **verification** | The integrity and conformity gates that releases must pass before advancing; a lifecycle gate, not an optional command | Manifesto §4.1; PRD-002 §5.3 |
| **verification gate** | A mandatory, unskippable transition: verification is enforced as a transition, not requested as an option; a gate that fails rejects the operation | Manifesto §3.2, §4.1; PRD-002 §5.2–§5.3 |
| **evidence** | The re-checkable output of a verification operation — checksums, manifests, verification report entries — embedded or recorded so any consumer can re-verify it; not a claim | Manifesto §3.2; PRD-002 §5.3 |
| **verification report** | The runtime's record of verification outcomes; verification outcomes merge into it | 007 §6 |
| **lifecycle evidence** | Verification outcomes recorded as lifecycle evidence — part of the persisted, queryable, authoritative lifecycle state | 007 §6; PRD-002 §5.4 |
| **structural check** | Verification content that the artifact contains what the framework requires (files, structures); the verified v1.x surface, preserved | 007 §6; ADR-033 §3 |
| **lifecycle-conformity check** | Verification content about framework behavior at lifecycle points: shared-resource wiring, migration timing relative to promotion, queue restart, rollback behavior; the v2 verification depth | 007 §6; ADR-033 §3; Review 19 §3.3 |
| **Verification part (of a standard)** | The part of the standard's structure that carries the framework's verification rules — structural checks and lifecycle-conformity checks | Transition Plan §5.4; 007 §6 |
| **verification declaration** | What a standard declares as its verification content against this contract; the runtime invokes only declared capability | Manifesto §7; Transition Plan §5.9; TS-013-04-02 |

---

## 4. Gate Semantics

### 4.1 Verification is a gate, not an option

Verification is a lifecycle gate, not an optional command ([PRD-002 §5.3](../prd/PRD-002-anvil-v2.md); [Manifesto §3.2](../manifesto/ANVIL_MANIFESTO.md)):

- Gates are **mandatory and unskippable** before a release may advance: verification is enforced as a transition, not requested as an option ([Manifesto §4.1](../manifesto/ANVIL_MANIFESTO.md); [PRD-002 §5.2](../prd/PRD-002-anvil-v2.md); [ADR-033 §3](../adr/ADR-033-verification-standard-generalization.md)).
- The lifecycle cannot be bypassed: verification gates are mandatory, and illegal transitions are rejected, not advised against ([PRD-002 §5.2](../prd/PRD-002-anvil-v2.md); [lifecycle-model §6.2 R2](lifecycle-model.md)).
- **Verification precedes advancement.** Activation must not proceed from an artifact whose integrity has not been established; no lifecycle operation proceeds from unverified inputs ([Manifesto §3.2, §5.5](../manifesto/ANVIL_MANIFESTO.md); [lifecycle-model §5.1, §6.2 R1](lifecycle-model.md)).
- Verification is **point-in-time, at transition boundaries** — not ongoing observation of running applications ([Manifesto §5.6](../manifesto/ANVIL_MANIFESTO.md); [PRD-002 §8](../prd/PRD-002-anvil-v2.md)).

### 4.2 Gate positions in the lifecycle

Verification applies at defined points of the lifecycle; the positions are fixed by the specification, the checks executing there are declared content:

| Gate position | When it applies | What it verifies | Contract owner |
|---|---|---|---|
| **Verify stage** (artifact lifecycle) | Before any lifecycle operation may consume the artifact | Integrity: the content hash matches the hash declared in the embedded manifest — the integrity gate ([artifact-manifest §5](artifact-manifest.md)); structural and lifecycle-conformity verification follow if the integrity gate passes ([artifact-manifest §5.2](artifact-manifest.md)) | Artifact manifest contract (integrity); this contract (structural, lifecycle-conformity) |
| **verify phase** (activation sequence) | At the fixed contract-level position immediately before the atomic promotion | Activation-time verification content: the checks executing there are declared capability supplied by the standard; a standard may declare zero verify checks, in which case the phase is a no-op gate ([lifecycle-model §5.1](lifecycle-model.md)) | This contract (position); the standard (checks) |

The stage model, phase sequence, and transition rules are architectural objects: a standard supplies content within the defined lifecycle; it does not invent gates or move gate positions ([Manifesto §3.1](../manifesto/ANVIL_MANIFESTO.md); [007 §5](../architecture/007-delivery-lifecycle-standard-specification.md)).

### 4.3 Gate rules

These are the rules that make verification enforceable. An implementation must guarantee all of them; an operation that violates one is rejected, never advised against.

| # | Rule | Source |
|---|---|---|
| G1 | **Verification is mandatory.** No lifecycle operation proceeds from unverified inputs; verification is a transition that cannot be skipped, not an optional command | Manifesto §3.2; PRD-002 §5.2–§5.3; lifecycle-model §6.2 R1 |
| G2 | **Gates are enforced, not requested.** Verification is enforced as a transition; the lifecycle cannot be bypassed; illegal transitions are rejected | Manifesto §4.1; PRD-002 §5.2; lifecycle-model §6.2 R2 |
| G3 | **Verification precedes advancement.** Verification is mandatory before a release may advance — before any lifecycle operation consumes the artifact, and before the atomic promotion | Manifesto §3.2, §5.5; PRD-002 §5.3; artifact-manifest §5.1; lifecycle-model §5.1 |
| G4 | **A standard adds checks; it never weakens gates.** A standard declares verification content against this contract; it may not remove, skip, or relax a gate | 007 §6; ADR-033 §3 |
| G5 | **All verification layers are mandatory.** Integrity, structural, and lifecycle-conformity verification are all gates; none may be skipped | artifact-manifest §5.2 |
| G6 | **Failed verification rejects the operation.** An artifact that fails verification is rejected; no lifecycle operation proceeds from unverified inputs | artifact-manifest §5.3; PRD-002 §5.2 |

### 4.4 A standard adds checks; it never weakens gates

The gate set is a **floor, not a ceiling**: a standard may add checks against this contract, and the contract's gates remain invariant ([007 §6](../architecture/007-delivery-lifecycle-standard-specification.md); [ADR-033 §3](../adr/ADR-033-verification-standard-generalization.md)):

- **Declaring no content does not open the gate.** A standard may declare zero verification checks; the gate position remains and the gate remains — a no-op gate, not an open door ([lifecycle-model §5.1](lifecycle-model.md)).
- **Content that weakens a gate is invalid.** Declarations that weaken gates are rejected — the machine-readable schema encodes this rejection ([TS-013-04-02 §2](../work-items/technical-stories/TS-013-04-02-verification-contract-json-schema.md); [ADR-029 §3](../adr/ADR-029-specification-publication-format.md)).
- **Weakening a gate is a contract change.** Any change that removes or relaxes a gate is a breaking change to gate semantics — a governed event requiring an ADR, not a standard decision ([ADR-035 §3.1](../adr/ADR-035-governance-and-identity-reframing-amendments.md); §2.4).

---

## 5. Evidence Requirements

### 5.1 A claim is not evidence

Evidence is embedded and re-checkable — a claim is not evidence ([PRD-002 §5.3](../prd/PRD-002-anvil-v2.md); [Manifesto §3.2](../manifesto/ANVIL_MANIFESTO.md)):

- Documentation is a claim; enforcement is a fact. Verification is how the convention is proven: an enforced, verified convention has a failure mode; a documented one does not ([Manifesto §6](../manifesto/ANVIL_MANIFESTO.md); [007 §9](../architecture/007-delivery-lifecycle-standard-specification.md); [ADR-033 §5](../adr/ADR-033-verification-standard-generalization.md)).
- The artifact manifest contract demonstrates the distinction in its concrete form: the manifest declares the expected hash — the claim; the verification operation recomputes the hash from the artifact's content and compares — the evidence ([artifact-manifest §5.1](artifact-manifest.md)).

### 5.2 Evidence is re-checkable

- Evidence (checksums, manifests) must be re-checkable, not merely claimed ([Manifesto §3.2](../manifesto/ANVIL_MANIFESTO.md); [PRD-002 §5.3](../prd/PRD-002-anvil-v2.md)).
- The evidence is embedded in the artifact, not held in the memory of the verifying process ([Manifesto §5.3](../manifesto/ANVIL_MANIFESTO.md); [artifact-manifest §5.1](artifact-manifest.md)).
- Any consumer — the runtime, a CI pipeline, a human operator — can re-verify using the same evidence ([artifact-manifest §5.1](artifact-manifest.md)).
- Re-checkability is what makes conformance objective: the conformance harness validates runtime behavior against the specification in CI ([ADR-029 §3](../adr/ADR-029-specification-publication-format.md)).

### 5.3 Outcomes merge into the verification report and are recorded as lifecycle evidence

Verification outcomes merge into the runtime's verification report and are recorded as lifecycle evidence ([007 §6](../architecture/007-delivery-lifecycle-standard-specification.md)):

- Lifecycle facts are persisted and queryable: what is active, what is installed, what can roll back, what stage each release is in. Decisions derive from state, never from memory or filesystem inference ([PRD-002 §5.4](../prd/PRD-002-anvil-v2.md); [lifecycle-model §6.2 R8](lifecycle-model.md)).
- Verification outcomes are part of those lifecycle facts: recorded at verification time, queryable afterward, re-checkable by any consumer.

### 5.4 Evidence rules

| # | Rule | Source |
|---|---|---|
| E1 | **A claim is not evidence.** Declaring a property is not proving it; evidence is the re-checkable output of a verification operation | PRD-002 §5.3; Manifesto §3.2 |
| E2 | **Evidence is re-checkable.** Any consumer can re-verify the same evidence; evidence is embedded and recorded, not held in process memory | Manifesto §3.2, §5.3; artifact-manifest §5.1 |
| E3 | **Outcomes merge into the runtime's verification report.** Verification outcomes are not left in the verifying process; they merge into the report the runtime keeps | 007 §6 |
| E4 | **Outcomes are recorded as lifecycle evidence.** Verification outcomes are recorded as lifecycle evidence — part of the persisted, queryable, authoritative lifecycle state from which decisions derive | 007 §6; PRD-002 §5.4; lifecycle-model §6.2 R8 |
| E5 | **Evidence that cannot be re-checked is invalid.** Declarations whose evidence cannot be re-checked are rejected — the machine-readable schema encodes this rejection | TS-013-04-02 §2; ADR-029 §3 |

---

## 6. Contract/Content Boundary

### 6.1 Ownership: the contract belongs to the specification

- The verification contract — gate semantics and evidence requirements — belongs to the **delivery lifecycle specification**, not to any standard ([007 §6](../architecture/007-delivery-lifecycle-standard-specification.md); [ADR-033 §3](../adr/ADR-033-verification-standard-generalization.md)).
- The specification is the authority every other layer conforms to and is Core-governed; standards do not own the contract ([Transition Plan §5.1](../planning/ANVIL_V2_TRANSITION_PLAN.md); [ADR-033 §4](../adr/ADR-033-verification-standard-generalization.md)).
- The contract/content separation mirrors the three-layer model: the specification owns the contract, delivery lifecycle standards supply the content ([ADR-033 §5](../adr/ADR-033-verification-standard-generalization.md)).
- The runtime remains an **engine, not content**: it enforces the contract and executes declared verification content; it supplies none itself ([ADR-033 §6](../adr/ADR-033-verification-standard-generalization.md)).

### 6.2 Standards supply verification content

Verification rules are standard-supplied content against the verification contract ([ADR-033 §3](../adr/ADR-033-verification-standard-generalization.md)). The Verification part of a standard carries the framework's verification rules in two categories ([Transition Plan §5.4](../planning/ANVIL_V2_TRANSITION_PLAN.md); [007 §6](../architecture/007-delivery-lifecycle-standard-specification.md)):

| Category | What it verifies | Origin |
|---|---|---|
| **Structural checks** | The artifact contains what the framework requires (files, structures) | The verified v1.x surface, preserved (007 §6) |
| **Lifecycle-conformity checks** | Framework behavior at lifecycle points: shared-resource wiring, migration timing relative to promotion, queue restart, rollback behavior | The v2 verification depth (Transition Plan §3 A8; Review 19 §3.3; EPIC-018) |

Content rules:

- **Content is declared capability.** The runtime invokes only declared verification checks, at the declared positions; undeclared capability is never called ([Manifesto §7](../manifesto/ANVIL_MANIFESTO.md); [Transition Plan §5.9](../planning/ANVIL_V2_TRANSITION_PLAN.md); [lifecycle-model §5.1](lifecycle-model.md)).
- **Authoring verification content is EPIC-018 scope.** The concrete depth items (shared-resource wiring, migration timing, queue restart, rollback behavior) are executed in EPIC-018, not in this contract ([ADR-033 §7](../adr/ADR-033-verification-standard-generalization.md); [EPIC-018](../epics/EPIC-018-standard-repository-authoring.md)).
- **Registry acceptance validates coverage.** A standard's verification coverage and tests are validated at registry acceptance ([ADR-033 §7](../adr/ADR-033-verification-standard-generalization.md); [ADR-034](../adr/ADR-034-community-standard-contribution-policy.md)).
- **Violations are rejected, not patched.** A standard that violates this contract is rejected by the registry, not patched by Core ([Transition Plan §5.5](../planning/ANVIL_V2_TRANSITION_PLAN.md); [007 §2](../architecture/007-delivery-lifecycle-standard-specification.md)).

### 6.3 What this contract does not define

| Not defined here | Why | Where it is handled |
|---|---|---|
| Concrete verification checks for any framework | Framework lifecycle knowledge is standard content, not contract | The standard's Verification part — EPIC-018 scope (ADR-033 §7) |
| Check declaration and evidence formats | Format-level decisions are deferred to Phase 3 implementation design | EPIC-013 (007 §6; ADR-029) |
| The machine-readable form | The schema is a separate corpus artifact, authoritative per ADR-029 §3 | TS-013-04-02 (Wave 2) |

---

## 7. Consumers

### 7.1 Anvil Runtime (Core)

The Anvil Runtime is the primary consumer of this contract. It implements the verification contract by:

- **Enforcing gate semantics** — verification gates are mandatory and unskippable; a release cannot advance past a gate that has not passed ([PRD-002 §5.2](../prd/PRD-002-anvil-v2.md); §4).
- **Executing declared verification content** at the defined lifecycle positions — the fixed verify position immediately before the atomic promotion, and the checks that follow the integrity gate ([lifecycle-model §5.1](lifecycle-model.md); [artifact-manifest §5.2](artifact-manifest.md)).
- **Merging outcomes into its verification report** and **recording them as lifecycle evidence** ([007 §6](../architecture/007-delivery-lifecycle-standard-specification.md); §5.3).
- **Rejecting violations** — operations that skip a gate, content that weakens a gate, and evidence that cannot be re-checked.

The runtime's implementation of this contract is validated by the conformance harness (EPIC-013), which checks engine behavior against the specification ([ADR-029 §3](../adr/ADR-029-specification-publication-format.md)).

**Source:** [PRD-002 §5.3](../prd/PRD-002-anvil-v2.md); [EPIC-015](../epics/EPIC-015-core-framework-free-refactoring.md); [007 §6](../architecture/007-delivery-lifecycle-standard-specification.md).

### 7.2 Machine-checkable schema (TS-013-04-02)

The machine-checkable JSON Schema (TS-013-04-02) encodes the contract defined in this document: the shape of verification declarations and the evidence requirements ([TS-013-04-02 §2](../work-items/technical-stories/TS-013-04-02-verification-contract-json-schema.md)). The schema is the machine-readable authority: **where this document and the schema disagree, the schema governs** ([ADR-029 §3](../adr/ADR-029-specification-publication-format.md)).

The schema enables:

- **Automated validation** of verification declarations and evidence against the contract, without human interpretation.
- **Negative-case enforcement**: declarations that weaken gates and evidence that cannot be re-checked are rejected ([TS-013-04-02 §2](../work-items/technical-stories/TS-013-04-02-verification-contract-json-schema.md)).
- **Independent implementation** of the contract by external runtimes that consume the schema, not engine source.

**Scope note:** The schema itself is outside the scope of this document (TS-013-04-01). This document defines the contract the schema encodes; the schema is the authoritative machine-readable form.

**Source:** [ADR-029 §3](../adr/ADR-029-specification-publication-format.md); [TS-013-04-02](../work-items/technical-stories/TS-013-04-02-verification-contract-json-schema.md).

### 7.3 Delivery lifecycle standards

Standards declare verification content against this contract — structural checks and lifecycle-conformity checks in their Verification part (007 §6; ADR-033 §3). They add checks; they never weaken gates (G4). A standard that violates the verification contract is rejected by the registry, not patched by Core ([Transition Plan §5.5](../planning/ANVIL_V2_TRANSITION_PLAN.md); [007 §2](../architecture/007-delivery-lifecycle-standard-specification.md)).

### 7.4 Registry validation (EPIC-014)

Registry validation consumes the contract at adoption: a standard's verification coverage and tests are part of the standard's obligations, validated at registry acceptance ([ADR-033 §7](../adr/ADR-033-verification-standard-generalization.md); [ADR-034](../adr/ADR-034-community-standard-contribution-policy.md)).

---

## 8. Scope Boundaries

This document defines the verification contract only. Related contracts of the specification are separate corpus documents (authored in EPIC-013):

| Not defined here | Where it is defined |
|---|---|
| Integrity gate (content hash verification) | [artifact-manifest.md §5](artifact-manifest.md) |
| Lifecycle model (Verify stage, activation sequence, state machine semantics) | [lifecycle-model.md](lifecycle-model.md) |
| Standard command contract (runtime ↔ standard exchange) | Its own specification corpus document (ST-013-03) |
| Vocabulary definitions | Specification corpus vocabulary document (TS-013-01-03) |
| Concrete verification checks for a framework | The standard's Verification part — EPIC-018 scope (ADR-033 §7) |
| Check declaration and evidence formats | EPIC-013 implementation design (007 §6; deferred) |
| Machine-readable form of this contract | JSON Schema (TS-013-04-02) — the authority per ADR-029 §3 |
| Engine behavior | The runtime (EPIC-015) implements this contract; engine internals are not part of the specification |

---

## 9. Traceability

| Section | Source of truth |
|---|---|
| §1 Purpose | PRD-002 §4.1, §5.3; Transition Plan §3 A8, §5.1; Manifesto §3.2, §6; 007 §6; ADR-021 §3.1; ADR-029 §3; ADR-033 §1, §5 |
| §2 Authority, publication, governance | ADR-024 §3; ADR-029 §3; ADR-035 §3; Transition Plan §5.1–§5.2, §5.10 |
| §3 Terminology | 007 §3, §6; PRD-002 §5.5; Manifesto §4.1, §7; Transition Plan §5.4, §5.9; ADR-033; TS-013-01-03 |
| §4 Gate semantics | PRD-002 §5.2–§5.3; Manifesto §3.1–§3.2, §4.1, §5.5–§5.6; 007 §6; ADR-033 §3; lifecycle-model §5.1, §6.2; artifact-manifest §5 |
| §5 Evidence requirements | PRD-002 §5.3–§5.4; Manifesto §3.2, §5.3, §6; 007 §6, §9; ADR-033 §5; ADR-029 §3; artifact-manifest §5.1; lifecycle-model §6.2 R8; TS-013-04-02 |
| §6 Contract/content boundary | ADR-033 §3–§7; 007 §2, §6; Transition Plan §3 A8, §5.4–§5.5, §5.9; Manifesto §7; EPIC-018; ADR-034; Review 19 §3.3 |
| §7 Consumers | PRD-002 §5.2–§5.3; ADR-029 §3; ADR-033 §7; TS-013-04-02; 007 §2, §6; EPIC-015; EPIC-018 |
| §8 Scope boundaries | EPIC-013; ADR-029 §3; ADR-033 §7; EPIC-018 |
| §9 Traceability | — |

---

*End of verification-contract.md*
