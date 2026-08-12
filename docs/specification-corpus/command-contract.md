# Standard Command Contract (Draft)

## The Delivery Lifecycle Specification — Standard Command Contract

| Metadata | |
|---|---|
| **Document ID** | command-contract |
| **Status** | Draft |
| **Date** | 2026-08-04 |
| **Product** | Anvil |
| **Dependencies** | [PRD-002 §4.1, §5.1](../prd/PRD-002-anvil-v2.md) · [ANVIL_V2_TRANSITION_PLAN §5.8–§5.10](../planning/ANVIL_V2_TRANSITION_PLAN.md) · [ANVIL_MANIFESTO §7](../manifesto/ANVIL_MANIFESTO.md) · [ADR-021](../adr/ADR-021-delivery-lifecycle-standard-model.md) · [ADR-024](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md) · [ADR-029](../adr/ADR-029-specification-publication-format.md) · [ADR-035](../adr/ADR-035-governance-and-identity-reframing-amendments.md) · [007-delivery-lifecycle-standard-specification §3, §5–§7](../architecture/007-delivery-lifecycle-standard-specification.md) · [006-g §2](../architecture/006-g-v1x-to-v2-concept-mapping.md) · [lifecycle-model](lifecycle-model.md) · [verification-contract](verification-contract.md) · [vocabulary](vocabulary.md) |
| **Consumers** | EPIC-015 (the runtime implements the contract) · TS-013-03-02 (machine-checkable schema) · EPIC-013 (conformance harness) · EPIC-018 (standard content authoring) · delivery lifecycle standard authors · registry validation (EPIC-014) |

**Docs/schema authority rule (ADR-029 §3).** The delivery lifecycle specification is published in dual form — human-readable documentation plus a machine-readable JSON Schema. The JSON Schema is the machine-readable authority: **where this document and the schema disagree, the schema governs.** This document describes the contract; it does not describe the engine.

---

## 1. Purpose

This document is the standard command contract part of the delivery lifecycle specification: the definition of the exchange between the Anvil Runtime (Core) and delivery lifecycle standards — the lifecycle-phase exchange (activation and rollback), verification, configuration extension, and the capability declaration ([PRD-002 §4.1, §5.1](../prd/PRD-002-anvil-v2.md); [Transition Plan §5.8](../planning/ANVIL_V2_TRANSITION_PLAN.md)).

It is written for an implementer who has never seen the engine source. Everything in this document is implementable from the contract alone:

- **The exchange surface** — the lifecycle-phase exchange (activation and rollback phases), the verification exchange, the configuration-extension exchange, and the capability declaration; the runtime invokes only declared capability; undeclared capability is never called.
- **The execution model** — standards are standalone executables invoked as subprocesses; distribution is through the registry, never by a runtime release.
- **Continuity** — the contract is the conceptual successor of the v1.x adapter command contract: the subprocess JSON contract, semantics unchanged, vocabulary updated ([Transition Plan §5.9](../planning/ANVIL_V2_TRANSITION_PLAN.md); [ST-012-04](../work-items/stories/ST-012-04-terminology-and-v1x-to-v2-concept-mapping.md)).

**Contract, not engine.** The Anvil Runtime (Core) implements and enforces this contract; this document is the contract the runtime enforces, not a description of the runtime ([ADR-021 §3.1](../adr/ADR-021-delivery-lifecycle-standard-model.md)). The machine-checkable schema (TS-013-03-02) is the authoritative form; this document describes the semantics the schema encodes ([ADR-029 §3](../adr/ADR-029-specification-publication-format.md)).

**Declared capability is the enforcement point.** Documentation alone has no failure mode; an enforced convention rejects the operation that violates it ([Manifesto §6](../manifesto/ANVIL_MANIFESTO.md)). The rules in this document are the definition Anvil encodes and enforces: an invocation that is not declared is never made, and a standard that declares capability it does not implement is rejected, not worked around ([Manifesto §7](../manifesto/ANVIL_MANIFESTO.md); [Transition Plan §5.5](../planning/ANVIL_V2_TRANSITION_PLAN.md)).

---

## 2. Authority, Publication, and Governance

### 2.1 Position in the three-layer model

The standard command contract is part of the **delivery lifecycle specification** — the authority every other layer conforms to ([Transition Plan §5.1](../planning/ANVIL_V2_TRANSITION_PLAN.md); [ADR-021 §3.1](../adr/ADR-021-delivery-lifecycle-standard-model.md)):

```text
Delivery Lifecycle Specification      ← what a legal lifecycle IS (the authority)
        │  implemented by                 this document is part of this layer
        ▼
Anvil Runtime (Core)                    ← the engine: enforces the specification,
        │  executes                      executes standards as subprocesses through
        ▼                                the standard command contract
delivery lifecycle standards         ← framework lifecycle content for one
        (anvil-standard-*)                 framework, exchanged over the contract
```

The specification defines the exchange; the runtime invokes standards through it; standards supply framework lifecycle content against it — lifecycle phases, verification checks, configuration extensions, templates ([Transition Plan §5.1](../planning/ANVIL_V2_TRANSITION_PLAN.md); [007 §3](../architecture/007-delivery-lifecycle-standard-specification.md)). The standard command contract is part of the specification, not of the runtime and not of any standard ([007 §1](../architecture/007-delivery-lifecycle-standard-specification.md); [ADR-021 §3.4](../adr/ADR-021-delivery-lifecycle-standard-model.md)).

### 2.2 Publication format

- The standard command contract is published in **dual form**: this human-readable document plus a machine-readable JSON Schema. The schema is the machine-readable authority; where the two disagree, the schema governs ([ADR-029 §3](../adr/ADR-029-specification-publication-format.md)).
- The specification corpus is authored **engine-path-independent**: it references no engine paths and no engine internals, so a future re-home of the specification is a move, not a rewrite ([ADR-029 §3](../adr/ADR-029-specification-publication-format.md); [Transition Plan §5.2, §5.10](../planning/ANVIL_V2_TRANSITION_PLAN.md)).
- The corpus is authored in the Core repository; there is no separate specification repository ([Transition Plan §5.2](../planning/ANVIL_V2_TRANSITION_PLAN.md)).

### 2.3 Versioning

- The specification carries its own independent semver version line, decoupled from runtime releases; the contract major version is the unit of compatibility ([ADR-024 §3.1](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).
- The runtime implements at most **two concurrently supported contract major versions**; a superseded contract major remains supported for one full contract generation — the deprecation window ([ADR-024 §3.4](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).
- Specification artifacts are published via a separate `spec/` tag line; engine and specification artifacts never share a tag ([ADR-024 §3.5](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).

### 2.4 Governed artifact

- The delivery lifecycle specification is a **governed architecture artifact of the Core repository** ([ADR-035 §3.1](../adr/ADR-035-governance-and-identity-reframing-amendments.md)).
- A breaking change to the standard command contract — any change to the exchange semantics that breaks compatibility — is a **governed event**: it requires an ADR and ships with a Core major version ([ADR-035 §3.1](../adr/ADR-035-governance-and-identity-reframing-amendments.md); [ADR-024 §3.3](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).
- A contract major bump may invalidate standards that target a superseded major; those standards remain runnable for the deprecation window ([ADR-024 §3.7](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).

---

## 3. Terminology

This section uses the vocabulary of the delivery lifecycle specification; the full vocabulary is owned by Core and standards must not redefine its semantics ([PRD-002 §5.5](../prd/PRD-002-anvil-v2.md)). The terms below are reproduced for this document's convenience, and the vocabulary document ([vocabulary](vocabulary.md)) and [007 §3](../architecture/007-delivery-lifecycle-standard-specification.md) prevail on any disagreement.

| Term | Definition | Source |
|---|---|---|
| **standard command contract** | The exchange between the Anvil Runtime and delivery lifecycle standards: lifecycle phases (activation, rollback), verification, configuration extension, and the capability declaration; the subprocess JSON contract, the conceptual successor of the v1.x adapter command contract | Transition Plan §5.1, §5.8–§5.9; ADR-021 §3.4; 006-g §2 |
| **delivery lifecycle standard** | The distributable unit of framework lifecycle knowledge: what a legal lifecycle *contains* for one framework — phases, verification, configuration surface, rollback semantics, templates — packaged, versioned, and verifiable against the delivery lifecycle specification | Transition Plan §5.3; 007 §3 |
| **Anvil Runtime (Core)** | The engine: implements and enforces the specification and executes standards as subprocesses through the standard command contract | Transition Plan §5.1; ADR-021 §3.1 |
| **capability declaration** | What a standard provides — lifecycle phases, verification checks, config extensions, templates — declared so the runtime invokes only declared capability; undeclared capability is never called; gains contract-version and framework-version fields | 007 §3; Manifesto §7; Transition Plan §5.9 |
| **contract version** | The version of the delivery lifecycle specification a standard targets; the compatibility basis between standard and runtime, declared rather than assumed | Transition Plan A2, §5.9; ADR-024 §3 |
| **configuration extension** | Framework-specific configuration keys and their validation rules, under the framework's own namespace | 007 §7 |
| **Lifecycle Definition (part of a standard)** | The part of the standard's structure that carries the framework's lifecycle content: activation phases, rollback semantics, failure semantics | Transition Plan §5.4; 007 §5 |

---

## 4. The Exchange Surface

The standard command contract is the exchange surface between the runtime and a standard: what a standard declares, and what the runtime invokes against that declaration. The contract/knowledge separation is its backbone — the runtime owns the shape of the exchange, the standard supplies the content ([Transition Plan §1.2](../planning/ANVIL_V2_TRANSITION_PLAN.md); [007 §5](../architecture/007-delivery-lifecycle-standard-specification.md)).

### 4.1 The capability declaration: the surface is declared, not assumed

A standard declares its capability surface before any of it is invoked. The capability declaration names what the standard provides — **lifecycle phases, verification checks, config extensions, templates** — so the runtime knows what framework-specific behavior is available without inspecting the standard's internals ([007 §3, §4](../architecture/007-delivery-lifecycle-standard-specification.md); [Manifesto §7](../manifesto/ANVIL_MANIFESTO.md)).

The declaration is the basis of the invocation rule, and of validation:

- **The runtime invokes only declared capability; undeclared capability is never called** ([007 §3](../architecture/007-delivery-lifecycle-standard-specification.md); [Manifesto §7](../manifesto/ANVIL_MANIFESTO.md); [Transition Plan §5.9](../planning/ANVIL_V2_TRANSITION_PLAN.md); [lifecycle-model §5.1](lifecycle-model.md)).
- The declaration gains **contract-version and framework-version fields**, enabling real compatibility validation at adoption ([Transition Plan §5.9](../planning/ANVIL_V2_TRANSITION_PLAN.md); [ADR-021 §3.2](../adr/ADR-021-delivery-lifecycle-standard-model.md)).
- The declaration is validated at adoption by the registry and re-verified at runtime when the standard is executed ([ADR-023 §3](../adr/ADR-023-delivery-lifecycle-standard-registry.md); [ADR-024 §3.6](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).
- A standard may declare nothing in a category, or nothing at all; the runtime proceeds with its generic operations for the lifecycle positions the standard does not cover. Framework-free projects run the generic lifecycle ([PRD-002 §5.10](../prd/PRD-002-anvil-v2.md)); an explicit framework declaration with no installed standard hard-fails with an actionable remediation — a missing standard is never silently degraded ([ADR-026 §3](../adr/ADR-026-core-framework-free-completion.md); [PRD-002 §7.1](../prd/PRD-002-anvil-v2.md)).

### 4.2 Activation phases

The lifecycle-phase exchange carries the standard's framework-specific activation steps — migrations, cache warming, queue recycling — in a **declared sequence**, within the activation phase sequence defined by the specification ([007 §5](../architecture/007-delivery-lifecycle-standard-specification.md); [Manifesto §5.5](../manifesto/ANVIL_MANIFESTO.md)):

```text
prepare → configure → framework phases → verify → promote
```

| Position | Phase | Ownership |
|---|---|---|
| 1 | **prepare** | Runtime-owned preparation before framework work |
| 2 | **configure** | Runtime-owned configuration of shared resources for the new Release |
| 3 | **framework phases** | The standard's declared activation phases, in the declared sequence — exchanged through this contract |
| 4 | **verify** | Post-activation verification phase: the fixed contract-level position, immediately before the atomic promotion |
| 5 | **promote** | The atomic commitment point: the Release becomes active |

Rules of the phase exchange ([lifecycle-model §5.1](lifecycle-model.md)):

- **Phase sequence is the specification's; phase content is the standard's.** The runtime invokes only **declared** phases, in the **declared** order; undeclared capability is never called ([Manifesto §7](../manifesto/ANVIL_MANIFESTO.md); [007 §5](../architecture/007-delivery-lifecycle-standard-specification.md)).
- **Content within a defined lifecycle.** A standard supplies content within the defined lifecycle; it does not invent phases, positions, or stages ([Manifesto §3.1](../manifesto/ANVIL_MANIFESTO.md); [007 §5](../architecture/007-delivery-lifecycle-standard-specification.md)).
- **Failure semantics are declared per phase.** What a failing phase means for activation and for rollback is declared by the standard for each phase ([007 §5](../architecture/007-delivery-lifecycle-standard-specification.md)).

### 4.3 Rollback semantics

Rollback is a first-class lifecycle operation, not a reverse activation ([Manifesto §5.7](../manifesto/ANVIL_MANIFESTO.md); [lifecycle-model §5.2](lifecycle-model.md)). The contract carries the standard's declared rollback semantics:

- **Per-phase reversal.** The standard declares how each of its framework phases is reversed; rollback executes the declared rollback semantics for those phases through the same declared-capability rule as activation ([007 §5](../architecture/007-delivery-lifecycle-standard-specification.md); [lifecycle-model §5.2](lifecycle-model.md)).
- **Irreversibility never blocks rollback.** A phase may be irreversible; the standard documents the irreversibility, and irreversibility never blocks rollback ([007 §5](../architecture/007-delivery-lifecycle-standard-specification.md); [lifecycle-model §5.2](lifecycle-model.md)).
- **Forward transition.** Rollback restores the previously active Release by a forward transition; legality is defined by lifecycle state, not by the contract's shape ([Manifesto §5.7](../manifesto/ANVIL_MANIFESTO.md); [lifecycle-model §5.2](lifecycle-model.md)).

### 4.4 Verification exchange

Verification content is exchanged through this contract, but the gate semantics and evidence requirements it carries belong to the **verification contract**, not to this document and not to any standard ([007 §6](../architecture/007-delivery-lifecycle-standard-specification.md); [ADR-033 §3](../adr/ADR-033-verification-standard-generalization.md); [verification-contract §6.1](verification-contract.md)).

- **Position is fixed, checks are declared.** Verification occupies the fixed contract-level position immediately before the atomic promotion; the checks executing there are declared capability supplied by the standard; a standard may declare zero verify checks, in which case the phase is a no-op gate ([lifecycle-model §5.1](lifecycle-model.md); [verification-contract §4.2](verification-contract.md)).
- **A standard adds checks; it never weakens gates.** Verification content is declared against the verification contract; gates remain mandatory and unskippable ([007 §6](../architecture/007-delivery-lifecycle-standard-specification.md); [ADR-033 §3](../adr/ADR-033-verification-standard-generalization.md)).
- **Outcomes merge into the runtime's verification report** and are recorded as lifecycle evidence — the exchange of verification results is part of this contract's surface; the evidence semantics are the verification contract's ([007 §6](../architecture/007-delivery-lifecycle-standard-specification.md); [verification-contract §5.3](verification-contract.md)).

### 4.5 Configuration extension

The configuration-extension exchange carries framework-specific configuration keys and their validation rules, under the framework's own namespace ([007 §7](../architecture/007-delivery-lifecycle-standard-specification.md); [PRD-002 §4.1](../prd/PRD-002-anvil-v2.md)):

- **Namespace isolation.** Extended configuration lives under the framework's own namespace; the runtime enforces namespace isolation and passes values through ([007 §7](../architecture/007-delivery-lifecycle-standard-specification.md); concept preserved from the v1.x contract, 005 §6).
- **The standard validates its own extended values.** Validation of framework-specific values is standard-supplied content, exchanged through this contract ([007 §7](../architecture/007-delivery-lifecycle-standard-specification.md)).
- **Extensions are distribution content, not engine content.** Configuration extensions travel with the standard, never with the runtime ([Transition Plan A10](../planning/ANVIL_V2_TRANSITION_PLAN.md); [007 §7](../architecture/007-delivery-lifecycle-standard-specification.md)).

### 4.6 Exchange rules

These are the rules that make the exchange enforceable. An implementation must guarantee all of them; an invocation that violates one is rejected, never attempted.

| # | Rule | Source |
|---|---|---|
| C1 | **Only declared capability is invoked.** The runtime invokes only declared capability; undeclared capability is never called | Manifesto §7; 007 §3; Transition Plan §5.9; lifecycle-model §5.1 |
| C2 | **Declared order is the executed order.** Declared phases run in the declared sequence, within the specification-defined activation sequence | 007 §5; Manifesto §5.5; lifecycle-model §5.1 |
| C3 | **Content within a defined lifecycle.** A standard supplies content within the defined lifecycle; it invents neither phases nor positions | Manifesto §3.1; 007 §5 |
| C4 | **Irreversibility never blocks rollback.** Irreversible phases are documented; rollback proceeds regardless | 007 §5; lifecycle-model §5.2 |
| C5 | **Checks add, never weaken.** Verification checks are declared against the verification contract; a standard never weakens gates | 007 §6; ADR-033 §3; verification-contract G4 |
| C6 | **The standard validates its own configuration.** Extended values are validated by the standard; the runtime enforces namespace isolation and passes values through | 007 §7 |
| C7 | **A standard that violates the contract is rejected, not patched.** The registry rejects it at adoption; the runtime does not work around it | Transition Plan §5.5; 007 §2 |

---

## 5. The Execution Model

The contract is executed as a **subprocess exchange**: the runtime invokes the standard as a standalone executable, and the exchange is structured JSON over that subprocess boundary ([Transition Plan §5.8, §5.10](../planning/ANVIL_V2_TRANSITION_PLAN.md); [ADR-021 §3.4](../adr/ADR-021-delivery-lifecycle-standard-model.md)).

- **Standalone executables.** Standards are standalone executables in any language; the runtime invokes them as subprocesses, never in-process ([Transition Plan §5.8, §5.10](../planning/ANVIL_V2_TRANSITION_PLAN.md); [ADR-021 §3.4](../adr/ADR-021-delivery-lifecycle-standard-model.md)).
- **The subprocess contract is the only integration path.** There are no in-process plugin mechanisms and no shared implementation types; the subprocess JSON contract is the only path between the runtime and standards ([ADR-021 §5](../adr/ADR-021-delivery-lifecycle-standard-model.md); [Transition Plan §12.2](../planning/ANVIL_V2_TRANSITION_PLAN.md)).
- **Language-agnostic by design.** Structured JSON over a subprocess contract makes the exchange independent of the runtime's and the standard's implementation languages — any runtime can invoke any standard ([Transition Plan §5.10](../planning/ANVIL_V2_TRANSITION_PLAN.md)).
- **Distribution through the registry, never by a runtime release.** Standards are distributed through the standard registry; a standard is never shipped with, or in, a runtime release ([Transition Plan §5.8](../planning/ANVIL_V2_TRANSITION_PLAN.md); [ADR-021 §3.4](../adr/ADR-021-delivery-lifecycle-standard-model.md); [007 §1](../architecture/007-delivery-lifecycle-standard-specification.md)).
- **Never shipped with the runtime.** Framework knowledge inside the runtime is a defect, not a convenience ([Transition Plan §5.8](../planning/ANVIL_V2_TRANSITION_PLAN.md); [007 §1](../architecture/007-delivery-lifecycle-standard-specification.md)).
- **Independently versioned.** Standard releases and runtime releases are decoupled; a runtime major version may coexist with multiple standard versions during the deprecation window ([Transition Plan §5.8](../planning/ANVIL_V2_TRANSITION_PLAN.md); [ADR-021 §3.4](../adr/ADR-021-delivery-lifecycle-standard-model.md)).
- **Compatibility is negotiated at adoption and re-verified at runtime.** The runtime and the standard negotiate compatibility at adoption time (registry validation plus runtime verification) ([Transition Plan §5.8](../planning/ANVIL_V2_TRANSITION_PLAN.md); [ADR-021 §3.4](../adr/ADR-021-delivery-lifecycle-standard-model.md); [compatibility-matrix §3](compatibility-matrix.md)).

The concrete exchange format — command names, payload shapes, process conventions — is implementation design for EPIC-013 and is encoded by the machine-readable schema (TS-013-03-02); this document defines the contract the schema encodes ([007 §5](../architecture/007-delivery-lifecycle-standard-specification.md); [ADR-029 §3](../adr/ADR-029-specification-publication-format.md)).

---

## 6. Continuity: the Successor of the v1.x Adapter Command Contract

The standard command contract is the **conceptual successor of the v1.x adapter command contract**: the subprocess JSON contract, semantics unchanged, vocabulary updated ([Transition Plan §5.9](../planning/ANVIL_V2_TRANSITION_PLAN.md); [ADR-021 §3.6](../adr/ADR-021-delivery-lifecycle-standard-model.md); [ST-012-04](../work-items/stories/ST-012-04-terminology-and-v1x-to-v2-concept-mapping.md)).

| v1.x term | v2 term | Meaning preserved |
|---|---|---|
| Adapter command contract | Standard command contract (spec) | The subprocess JSON contract; unchanged in semantics |
| Adapter capability declaration | Standard capability declaration | Gains contract-version and framework-version fields |

The full five-row mapping is maintained by [006-g §2](../architecture/006-g-v1x-to-v2-concept-mapping.md) (TS-012-04-02); the rows above are the rows this contract touches ([Transition Plan §5.9](../planning/ANVIL_V2_TRANSITION_PLAN.md); [ADR-021 §3.6](../adr/ADR-021-delivery-lifecycle-standard-model.md)).

What the succession means:

- **Semantics unchanged.** The exchange remains the subprocess JSON contract: activation and rollback phases, verification checks, configuration extension, and the capability declaration — the meaning of the exchange does not change between v1.x and v2 ([Transition Plan §5.9](../planning/ANVIL_V2_TRANSITION_PLAN.md); [006-g §2](../architecture/006-g-v1x-to-v2-concept-mapping.md)).
- **Boundary preserved.** The subprocess contract boundary established by ADR-009 is preserved; ADR-021 supersedes the adapter model in governance and distribution, not in the exchange boundary ([ADR-021 §1, §3](../adr/ADR-021-delivery-lifecycle-standard-model.md); [006-g §2](../architecture/006-g-v1x-to-v2-concept-mapping.md)).
- **Vocabulary updated.** The exchange is renamed from the v1.x adapter command contract to the standard command contract and becomes part of the delivery lifecycle specification ([Transition Plan §5.1, §5.9](../planning/ANVIL_V2_TRANSITION_PLAN.md); [ADR-021 §3.6](../adr/ADR-021-delivery-lifecycle-standard-model.md)).
- **Vocabulary usage rule.** In this corpus, "adapter" terminology appears only in the v1.x→v2 migration context — naming a v1.x term being mapped or describing migration ([ST-012-04 §3](../work-items/stories/ST-012-04-terminology-and-v1x-to-v2-concept-mapping.md); [006-g §3](../architecture/006-g-v1x-to-v2-concept-mapping.md); [vocabulary §2](vocabulary.md)).
- **Distribution changes, exchange does not.** v1.x adapters were a closed set distributed with the runtime's release assets; v2 standards are distributed through the registry — the exchange surface is preserved, the distribution channel is not ([Transition Plan §5.8–§5.9](../planning/ANVIL_V2_TRANSITION_PLAN.md); [006-g §2](../architecture/006-g-v1x-to-v2-concept-mapping.md)).

---

## 7. Consumers

### 7.1 Anvil Runtime (Core)

The Anvil Runtime is the primary consumer of this contract. It implements the standard command contract by:

- **Invoking standards as standalone subprocesses** through the declared-capability rule — only declared capability, in the declared order, at the defined lifecycle positions ([Transition Plan §5.8](../planning/ANVIL_V2_TRANSITION_PLAN.md); §4).
- **Executing the lifecycle-phase exchange** — the standard's declared activation phases at the framework-phases position of the activation sequence, and its declared rollback semantics on rollback ([lifecycle-model §5.1–§5.2](lifecycle-model.md); §4.2–§4.3).
- **Executing the verification exchange** at the fixed verify position and merging outcomes into its verification report ([lifecycle-model §5.1](lifecycle-model.md); [verification-contract §5.3](verification-contract.md)).
- **Enforcing namespace isolation** on configuration extensions and passing values through to the standard's own validation ([007 §7](../architecture/007-delivery-lifecycle-standard-specification.md); §4.5).
- **Re-verifying compatibility** when it executes the standard ([ADR-024 §3.6](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md); [compatibility-matrix §3](compatibility-matrix.md)).
- **Rejecting violations** — undeclared capability is never called; a standard that violates the contract is rejected by the registry, not patched by the runtime ([Transition Plan §5.5](../planning/ANVIL_V2_TRANSITION_PLAN.md); [007 §2](../architecture/007-delivery-lifecycle-standard-specification.md)).

The runtime's implementation of this contract is validated by the conformance harness (EPIC-013), which checks engine behavior against the specification ([ADR-029 §3](../adr/ADR-029-specification-publication-format.md)).

**Source:** [PRD-002 §5.1](../prd/PRD-002-anvil-v2.md); [Transition Plan §5.8](../planning/ANVIL_V2_TRANSITION_PLAN.md); [ADR-021 §3.4](../adr/ADR-021-delivery-lifecycle-standard-model.md); [ADR-024 §3.6](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md); [EPIC-015](../epics/EPIC-015-core-framework-free-refactoring.md).

### 7.2 Machine-checkable schema (TS-013-03-02)

The machine-checkable JSON Schema (TS-013-03-02) encodes the contract defined in this document: the exchange contract — lifecycle-phase exchange (activation, rollback), verification, configuration extension, and capability declaration ([TS-013-03-02 §2](../work-items/technical-stories/TS-013-03-02-standard-command-contract-json-schema.md)). The schema is the machine-readable authority: **where this document and the schema disagree, the schema governs** ([ADR-029 §3](../adr/ADR-029-specification-publication-format.md)).

The schema enables:

- **Automated validation** of conforming exchanges against the contract, without human interpretation.
- **Negative-case enforcement**: undeclared capability invoked and malformed exchanges are rejected with actionable diagnostics ([TS-013-03-02 §2, §4](../work-items/technical-stories/TS-013-03-02-standard-command-contract-json-schema.md)).
- **Independent implementation** of the contract by external runtimes that consume the schema, not engine source ([ADR-029 §3](../adr/ADR-029-specification-publication-format.md); [Transition Plan §5.10](../planning/ANVIL_V2_TRANSITION_PLAN.md)).

**Scope note:** The schema itself is outside the scope of this document (TS-013-03-01). This document defines the contract the schema encodes; the schema is the authoritative machine-readable form.

**Source:** [ADR-029 §3](../adr/ADR-029-specification-publication-format.md); [TS-013-03-02](../work-items/technical-stories/TS-013-03-02-standard-command-contract-json-schema.md).

### 7.3 Delivery lifecycle standards

Standards implement this contract. A standard declares its capability surface — lifecycle phases, verification checks, config extensions, templates — and supplies the content behind the declaration: activation phases and rollback semantics in its Lifecycle Definition part, verification checks in its Verification part, configuration extensions in its Templates part ([007 §4–§7](../architecture/007-delivery-lifecycle-standard-specification.md)). A standard that violates the contract is rejected by the registry, not patched by Core ([Transition Plan §5.5](../planning/ANVIL_V2_TRANSITION_PLAN.md); [007 §2](../architecture/007-delivery-lifecycle-standard-specification.md)).

### 7.4 Registry validation (EPIC-014)

Registry validation consumes the contract at adoption: the declared capability surface and the declared contract version are validated before a standard becomes installable ([ADR-023 §3](../adr/ADR-023-delivery-lifecycle-standard-registry.md); [ADR-024 §3.6](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md); [compatibility-matrix §3](compatibility-matrix.md)).

---

## 8. Scope Boundaries

This document defines the standard command contract only. Related contracts of the specification are separate corpus documents (authored in EPIC-013):

| Not defined here | Where it is defined |
|---|---|
| Activation phase sequence, rollback and recovery semantics, state machine | [lifecycle-model.md](lifecycle-model.md) (ST-013-01) |
| Verification gate semantics and evidence requirements | [verification-contract.md](verification-contract.md) (ST-013-04) |
| Artifact manifest contract (packaging, identity, embedded manifest) | [artifact-manifest.md](artifact-manifest.md) (ST-013-02) |
| Vocabulary definitions | [vocabulary.md](vocabulary.md) (TS-013-01-03); 007 §3 |
| Version line and compatibility record | [version-line.md](version-line.md) (TS-013-05-01); [compatibility-matrix.md](compatibility-matrix.md) |
| Concrete exchange formats (command names, payload shapes, process conventions) | Machine-readable JSON Schema (TS-013-03-02) — the authority per ADR-029 §3; EPIC-013 implementation design |
| Framework-specific phase content, checks, and configuration keys | The standard's parts (Lifecycle Definition, Verification, Templates) — EPIC-018 scope |
| Engine behavior | The runtime (EPIC-015) implements this contract; engine internals are not part of the specification |

---

## 9. Traceability

| Section | Source of truth |
|---|---|
| §1 Purpose | PRD-002 §4.1, §5.1; Transition Plan §5.8–§5.9; Manifesto §6–§7; ADR-021 §3.4; ADR-029 §3; ST-012-04 |
| §2 Authority, publication, governance | ADR-024 §3; ADR-029 §3; ADR-035 §3; Transition Plan §5.1–§5.2, §5.10 |
| §3 Terminology | 007 §3; Transition Plan §5.1, §5.3, §5.9; Manifesto §7; PRD-002 §5.5; ADR-024; vocabulary; TS-013-01-03 |
| §4 The exchange surface | Manifesto §3.1, §5.5, §5.7, §7; 007 §3–§7; Transition Plan §1.2, A10, §5.9; ADR-023 §3; ADR-024 §3.6; ADR-026 §3; ADR-033 §3; PRD-002 §5.10, §7.1; lifecycle-model §5.1–§5.2; verification-contract §4–§5 |
| §5 The execution model | Transition Plan §5.8, §5.10, §12.2; ADR-021 §3.4, §5; ADR-024 §3.6; 007 §1, §5; compatibility-matrix §3 |
| §6 Continuity | Transition Plan §5.1, §5.8–§5.9; ADR-021 §1, §3, §3.6; 006-g §2–§3; ST-012-04; vocabulary §2 |
| §7 Consumers | PRD-002 §5.1; Transition Plan §5.5, §5.8, §5.10; ADR-021 §3.4; ADR-023 §3; ADR-024 §3.6; ADR-029 §3; 007 §2, §4–§7; lifecycle-model §5.1; verification-contract §5.3; compatibility-matrix §3; EPIC-015 |
| §8 Scope boundaries | EPIC-013; ADR-029 §3; TS-013-03-02 |
| §9 Traceability | — |

---

*End of command-contract.md*
