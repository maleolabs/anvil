# Lifecycle Model (Draft)

## The Delivery Lifecycle Specification — Lifecycle Model

| Metadata | |
|---|---|
| **Document ID** | lifecycle-model |
| **Status** | Draft |
| **Date** | 2026-08-04 |
| **Product** | Anvil |
| **Dependencies** | [PRD-002 §5.1–§5.2, §5.5](../prd/PRD-002-anvil-v2.md) · [ANVIL_V2_TRANSITION_PLAN §1.2, §2.3, §5.1–§5.2](../planning/ANVIL_V2_TRANSITION_PLAN.md) · [ANVIL_MANIFESTO §5](../manifesto/ANVIL_MANIFESTO.md) · [ADR-003](../adr/ADR-003-runtime-and-release-lifecycle.md) · [ADR-021](../adr/ADR-021-delivery-lifecycle-standard-model.md) · [ADR-024](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md) · [ADR-029](../adr/ADR-029-specification-publication-format.md) · [ADR-035](../adr/ADR-035-governance-and-identity-reframing-amendments.md) · [007-delivery-lifecycle-standard-specification §3, §5](../architecture/007-delivery-lifecycle-standard-specification.md) |
| **Consumers** | EPIC-013 (schema authoring · vocabulary authoring · conformance harness) · EPIC-015 (the runtime implements the specification) · delivery lifecycle standard authors · registry validation (EPIC-014) |

**Docs/schema authority rule (ADR-029 §3).** The delivery lifecycle specification is published in dual form — human-readable documentation plus a machine-readable JSON Schema. The JSON Schema is the machine-readable authority: **where this document and the schema disagree, the schema governs.** This document describes the contract; it does not describe the engine.

---

## 1. Purpose

This document is the lifecycle model part of the delivery lifecycle specification: the definition of what a legal lifecycle *is* — the stage model, the phase sequence, the transition legality rules, and the state machine semantics ([PRD-002 §5.1](../prd/PRD-002-anvil-v2.md); [Transition Plan §5.1](../planning/ANVIL_V2_TRANSITION_PLAN.md)).

It is written for an implementer who has never seen the engine source. Everything in this document is implementable from the contract alone:

- The **stage model** defines the sequence of stages a release passes through, and which layer owns each stage.
- The **phase sequence** defines the ordered steps inside activation (and the shape of rollback and recovery).
- The **transition legality rules** define what must be true before a transition is legal — the rules that make transitions enforceable rather than advisory.
- The **state machine semantics** define the states a release can be in and the invariants the enforcement layer must guarantee.

**Contract, not engine.** The Anvil Runtime (Core) implements and enforces this specification; this document is the contract the runtime enforces, not a description of the runtime ([ADR-021 §3.1](../adr/ADR-021-delivery-lifecycle-standard-model.md)). A delivery lifecycle standard supplies content *within* the defined lifecycle — it does not invent a lifecycle ([Manifesto §3.1](../manifesto/ANVIL_MANIFESTO.md); [007 §5](../architecture/007-delivery-lifecycle-standard-specification.md)).

**Enforceability is the point.** Documentation alone has no failure mode; an enforced convention rejects the operation that violates it. The rules in this document are the definition Anvil encodes and enforces — illegal transitions are rejected, mandatory gates are unskippable, atomicity is structural ([Manifesto §6](../manifesto/ANVIL_MANIFESTO.md); [PRD-002 §5.2](../prd/PRD-002-anvil-v2.md)).

---

## 2. Authority, Publication, and Governance

### 2.1 Position in the three-layer model

The lifecycle model is part of the **delivery lifecycle specification** — the authority every other layer conforms to ([Transition Plan §5.1](../planning/ANVIL_V2_TRANSITION_PLAN.md); [ADR-021 §3.1](../adr/ADR-021-delivery-lifecycle-standard-model.md)):

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

The specification defines what a legal lifecycle *is*: stage model, transitions, contracts, vocabulary. The runtime defines none of it; standards define what a legal lifecycle *contains* for one framework, within this model ([Transition Plan §5.1](../planning/ANVIL_V2_TRANSITION_PLAN.md)).

### 2.2 Publication format

- The lifecycle model is published in **dual form**: this human-readable document plus a machine-readable JSON Schema. The schema is the machine-readable authority; where the two disagree, the schema governs ([ADR-029 §3](../adr/ADR-029-specification-publication-format.md)).
- The specification corpus is authored **engine-path-independent**: it references no engine paths and no engine internals, so a future re-home of the specification is a move, not a rewrite ([ADR-029 §3](../adr/ADR-029-specification-publication-format.md); [Transition Plan §5.2, §5.10](../planning/ANVIL_V2_TRANSITION_PLAN.md)).
- The corpus is authored in the Core repository; there is no separate specification repository ([Transition Plan §5.2](../planning/ANVIL_V2_TRANSITION_PLAN.md)).

### 2.3 Versioning

- The specification carries its own independent semver version line, decoupled from runtime releases; the contract major version is the unit of compatibility ([ADR-024 §3.1](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).
- The runtime implements at most **two concurrently supported contract major versions**; a superseded contract major remains supported for one full contract generation — the deprecation window ([ADR-024 §3.4](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).
- Specification artifacts are published via a separate `spec/` tag line; engine and specification artifacts never share a tag ([ADR-024 §3.5](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).

### 2.4 Governed artifact

- The delivery lifecycle specification is a **governed architecture artifact of the Core repository** ([ADR-035 §3.1](../adr/ADR-035-governance-and-identity-reframing-amendments.md)).
- A breaking change to the lifecycle model — any change to the stage model, phase sequence, transition legality, or state machine semantics that breaks compatibility — is a **governed event**: it requires an ADR and ships with a Core major version ([ADR-035 §3.1](../adr/ADR-035-governance-and-identity-reframing-amendments.md); [ADR-024 §3.3](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).
- A contract major bump may invalidate standards that target a superseded major; those standards remain runnable for the deprecation window ([ADR-024 §3.7](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).
- Ecosystem-proposed contract changes enter the ADR process through a defined channel (mechanics deferred to EPIC-020) ([ADR-035 §3.4](../adr/ADR-035-governance-and-identity-reframing-amendments.md)).

---

## 3. Terminology

This section uses the vocabulary of the delivery lifecycle specification; the full vocabulary is owned by Core and standards must not redefine its semantics ([PRD-002 §5.5](../prd/PRD-002-anvil-v2.md)). The lifecycle terms (artifact, release, activation, rollback, standard, contract version) are defined in the specification corpus vocabulary document (TS-013-01-03); the terms below are reproduced in abbreviated form for this document's convenience, and the vocabulary document prevails on any disagreement.

| Term | Definition | Source |
|---|---|---|
| **delivery lifecycle specification** | What a legal lifecycle *is* — the authority every other layer conforms to: lifecycle model, manifest contract, standard command contract, verification contract, vocabulary (docs + JSON Schema) | Transition Plan §5.1, §5.2 |
| **Anvil Runtime (Core)** | The engine: implements and enforces the Specification and executes standards as subprocesses; must not be confused with the Server Runtime Domain inside the runtime | Transition Plan §5.1 |
| **delivery lifecycle standard** | The distributable unit of framework lifecycle knowledge: what a legal lifecycle *contains* for one framework — phases, verification, configuration surface, rollback semantics, templates — packaged, versioned, and verifiable against the specification | Transition Plan §5.3 |
| **Contract version** | The version of the delivery lifecycle specification a standard targets; the compatibility basis between standard and runtime, declared rather than assumed | Transition Plan A2, §5.9 |
| **Artifact** | The first object Anvil defines: produced by packaging under the manifest contract, with content-derived identity and embedded verification evidence | Manifesto §5.2; PRD-002 §5.9 |
| **Release** | The lifecycle entity created when a verified artifact is installed into a runtime; tracks lifecycle state from Ready onward | Manifesto §5.4; PRD-002 §7.7 |

**Stage vs phase.** The corpus distinguishes two levels:

| Level | Meaning | Owned by |
|---|---|---|
| **Stage** | A position in the lifecycle model; the **Anvil-owned** stages are Package, Verify, Install, Activate, Rollback, Recover — Source, Build, and Operate are owned outside Anvil (§4.1) | The Specification; a standard does not invent stages (Manifesto §3.1) |
| **Phase** | An ordered step within activation (prepare, configure, framework phases, verify, promote — ADR-003 §6.4) | Phase *sequence*: the Specification; phase *content*: the standard's declared activation phases (007 §5; Manifesto §5.5, §7) |

The stage table above lists the **Anvil-owned** stages only; §4.1 defines the full stage model, including the stages Anvil does not own (Source, Build, Operate).

---

## 4. Stage Model

### 4.1 The lifecycle stages

The lifecycle Anvil defines, in concept terms ([Manifesto §5](../manifesto/ANVIL_MANIFESTO.md)):

```text
Source → Build → Package → Verify → Install → Activate → Operate → Rollback → Recover
  │        │        │         │         │         │          │         │          │
 CI       CI      ANVIL     ANVIL     ANVIL     ANVIL      external   ANVIL     ANVIL
 owns     owns     owns      owns      owns      owns      (monitor)   owns      owns
```

Anvil's convention begins at **Package**; Source and Build are owned by the project and its CI; Operate is owned by monitoring platforms and the application itself ([Manifesto §5.1, §5.2, §5.6](../manifesto/ANVIL_MANIFESTO.md)).

The chain `package → verify → install → activate → rollback → recover` is the **Anvil-owned span of the delivery lifecycle** ([Manifesto §5](../manifesto/ANVIL_MANIFESTO.md)); its sequence is preserved from v1.x ([Transition Plan §2.3](../planning/ANVIL_V2_TRANSITION_PLAN.md); [ADR-003 §4](../adr/ADR-003-runtime-and-release-lifecycle.md)).

The **release lifecycle proper** — `install → activate → rollback → recover` — begins when a verified artifact is installed and a Release enters **Ready** ([PRD-002 §4.1, §5.10](../prd/PRD-002-anvil-v2.md); [ADR-003 §4](../adr/ADR-003-runtime-and-release-lifecycle.md)). **Package and Verify are stages of the Artifact lifecycle, not of the release lifecycle** ([ADR-004 §7](../adr/ADR-004-artifact-architecture.md)); their semantics (content-derived identity, embedded manifest, integrity evidence) are owned by the Artifact Manifest contract (ST-013-02 / TS-013-02-01). The ownership decomposition was settled by decision 004-review-resolutions D3, not by this document.

### 4.2 Stage definitions

| Stage | Definition | Source |
|---|---|---|
| **Source** | The project's source; owned by the project and its CI. Anvil neither schedules nor builds | Manifesto §5.1 |
| **Build** | Owned by the project and its CI; Anvil may execute build pipelines on request, but the convention for what a build contains is per-project, per-CI | Manifesto §5.1 |
| **Package** | Produces the deployable object under a defined contract: content-derived identity, embedded manifest, deterministic output. The Artifact is the first object Anvil defines | Manifesto §5.2; PRD-002 §5.9 |
| **Verify** | The first gate. The Artifact is checked against its contract — integrity, identity, structure. Verification is mandatory before any lifecycle operation may consume the Artifact; the evidence is embedded in the Artifact, not held in the memory of the verifying process | Manifesto §5.3; PRD-002 §5.3 |
| **Install** | Adopts the verified Artifact into a runtime: stored, registered by manifest identity, becomes a Release in the **Ready** state. Installation is idempotent by Artifact identity — the same Artifact installed twice is one Release, not two | Manifesto §5.4; PRD-002 §7.7 |
| **Activate** | The commitment operation: the Release becomes the live version. Phase-based (prepare, configure, framework phases, verify, promote) and atomic from the observer's perspective. Exactly one Release is active | ADR-003 §6.4; Manifesto §5.5; PRD-002 §5.10 |
| **Operate** | Running and observing the application belongs to monitoring platforms and the application; the lifecycle's verification is point-in-time, at transition boundaries | Manifesto §5.6 |
| **Rollback** | A first-class lifecycle operation, not a reverse activation: restores the previously active Release by a forward transition, preserving the rolled-back Release for inspection. Legality is defined by state | Manifesto §5.7; PRD-002 §7.8 |
| **Recover** | Reconciliation of interrupted operations: state, pointers, and the filesystem converge before the next lifecycle operation is accepted; an interrupted operation is never silently reported as success | Manifesto §5.8; PRD-002 §5.2 |

### 4.3 Model rules

- The stage model, phase sequence, and transition rules are **architectural objects, not emergent behavior**: a framework does not get to invent a lifecycle, and a project does not get to redefine what activation means ([Manifesto §3.1](../manifesto/ANVIL_MANIFESTO.md); [007 §5](../architecture/007-delivery-lifecycle-standard-specification.md)).
- The release lifecycle is enforced for **any project**: framework-aware projects through their standard, framework-free projects through the generic lifecycle ([PRD-002 §5.10](../prd/PRD-002-anvil-v2.md)).
- The four-domain lifecycle model of v1.x (Project → Artifact → Deployment → Server Runtime) is preserved; it is the domain decomposition the runtime operates in, distinct from the stage model above ([PRD-002 §9](../prd/PRD-002-anvil-v2.md)).

---

## 5. Phase Sequence

### 5.1 Activation phase sequence

Activation is the commitment operation. It is phase-based, in this defined sequence ([ADR-003 §6.4](../adr/ADR-003-runtime-and-release-lifecycle.md); [Manifesto §5.5](../manifesto/ANVIL_MANIFESTO.md)):

```text
prepare → configure → framework phases → verify → promote
```

| Position | Phase | Ownership |
|---|---|---|
| 1 | **prepare** | Runtime-owned preparation before framework work |
| 2 | **configure** | Runtime-owned configuration of shared resources for the new Release |
| 3 | **framework phases** | The standard's declared activation phases (e.g., migrations, cache warming, queue recycling), in the declared sequence |
| 4 | **verify** | Post-activation verification phase: the contract-level position, immediately before the atomic promotion, where activation-time verification content executes; the position is fixed, the checks are declared capability supplied by the standard (007 §6; ADR-033) |
| 5 | **promote** | The atomic commitment point: the Release becomes active |

**Phase-level authority.** This section is the phase-level authority for the activation sequence, sourced to ADR-003 §6.4 (the only normative phase-level definition; Accepted, unsuperseded). Concept-level enumerations elsewhere in the corpus (Manifesto §5.5, PRD-002 §5.10, 007 §5) list fewer phases; they are summaries of the shape of activation, not exhaustive phase-level contracts, and they do not remove Verify. Verify is a gate before the atomic commitment point — a mandatory, unskippable transition in the lifecycle ([Manifesto §3.2](../manifesto/ANVIL_MANIFESTO.md)).

Rules that make the sequence enforceable:

- **Contract/knowledge separation.** The phase *sequence* is defined by the Specification; the phase *content* is supplied by the standard's Lifecycle Definition part ([Transition Plan §1.2](../planning/ANVIL_V2_TRANSITION_PLAN.md); [007 §5](../architecture/007-delivery-lifecycle-standard-specification.md)). The runtime invokes only **declared** phases, in the **declared** order; undeclared capability is never called ([Manifesto §7](../manifesto/ANVIL_MANIFESTO.md)).
- **Verification precedes activation.** Activation must not proceed from an Artifact whose integrity has not been established ([Manifesto §3.2, §5.5](../manifesto/ANVIL_MANIFESTO.md)).
- **Verify position is fixed, content is declared.** Verify occupies the fixed position immediately before promote; the checks executing there are declared capability supplied by the standard — a standard may declare zero verify checks, in which case the phase is a no-op gate ([ADR-003 §6.4](../adr/ADR-003-runtime-and-release-lifecycle.md); [007 §6](../architecture/007-delivery-lifecycle-standard-specification.md)).
- **Atomic commitment.** From the observer's perspective, activation is atomic: either the new Release is active, or the previous one remains active — never an intermediate state ([Manifesto §3.9](../manifesto/ANVIL_MANIFESTO.md); [PRD-002 §5.2](../prd/PRD-002-anvil-v2.md)).
- **Failure semantics.** What a failing phase means for activation and for rollback is declared per phase by the standard; a phase may be irreversible, the standard documents the irreversibility, and **irreversibility never blocks rollback** ([007 §5](../architecture/007-delivery-lifecycle-standard-specification.md)).
- **Nothing after the commitment point is assumed.** If the operation reports failure, nothing after the commitment point may be assumed to have happened ([Manifesto §5.5](../manifesto/ANVIL_MANIFESTO.md)).

### 5.2 Rollback sequence

Rollback is a first-class lifecycle operation, not a reverse activation ([Manifesto §5.7](../manifesto/ANVIL_MANIFESTO.md); [PRD-002 §7.8](../prd/PRD-002-anvil-v2.md); [ADR-003 §7.1](../adr/ADR-003-runtime-and-release-lifecycle.md)):

- It **restores the previously active Release** by a **forward transition** — from the system's perspective, the previously active Release is restored to Active ([ADR-003 §7.1](../adr/ADR-003-runtime-and-release-lifecycle.md)).
- It **preserves the rolled-back Release** for inspection.
- Its **legality is defined by state**: the rollback target must exist and must be eligible; the operation is rejected otherwise (R5).
- It executes the standard's **declared rollback semantics for its framework phases** — how each phase is reversed, declared per phase by the standard — through the same declared-capability rule as activation; a phase may be irreversible, the standard documents the irreversibility, and irreversibility never blocks rollback ([007 §5](../architecture/007-delivery-lifecycle-standard-specification.md); [ADR-003 §7.4](../adr/ADR-003-runtime-and-release-lifecycle.md)).

**Rollback target and eligibility (operational definition).** The rollback target is the Release **last superseded** by the current Active Release. Operationally, the engine selects the **Archived Release with the newest archival timestamp among Releases that were previously Active** ([ADR-003 §7.3](../adr/ADR-003-runtime-and-release-lifecycle.md): if no explicit target is specified, the most recent release that was previously Active is used). Eligibility is defined by state:

- The target **exists** — a Release selected by the rule above (or an explicit previously active Release).
- The target is in state **Archived** — the state every superseded Active Release enters when a new Release is activated ([ADR-003 §4.6, §9.1](../adr/ADR-003-runtime-and-release-lifecycle.md)).

If either condition fails, the rollback operation is rejected. The machine-readable schema encodes this eligibility ([ADR-029 §3](../adr/ADR-029-specification-publication-format.md)).

### 5.3 Recovery sequence

Recovery is the reconciliation of interrupted operations ([Manifesto §5.8](../manifesto/ANVIL_MANIFESTO.md)):

- A crash mid-transition must be **observable and reconcilable**: state, pointers, and the filesystem must converge before the next lifecycle operation is accepted.
- An interrupted operation is **never silently reported as success** ([Manifesto §3.10](../manifesto/ANVIL_MANIFESTO.md); [PRD-002 §5.2](../prd/PRD-002-anvil-v2.md)).

---

## 6. State Machine Semantics

The state machine is the enforcement surface of the lifecycle: transitions are validated against a defined graph, and illegal transitions are rejected, not advised against ([Manifesto §3.3](../manifesto/ANVIL_MANIFESTO.md); [PRD-002 §5.2](../prd/PRD-002-anvil-v2.md)). Lifecycle decisions derive from persisted, queryable, authoritative state — never from process memory or filesystem inference ([Manifesto §3.3](../manifesto/ANVIL_MANIFESTO.md); [PRD-002 §5.4](../prd/PRD-002-anvil-v2.md)).

### 6.1 Release states

A Release progresses through these states. The state machine (one-Active invariant, atomic activation, forward rollback, automatic archival of the superseded Release) is preserved unchanged from v1.x, as defined architecturally by [ADR-003 §4, §7, §9](../adr/ADR-003-runtime-and-release-lifecycle.md) ([Transition Plan §1.2, §2.3](../planning/ANVIL_V2_TRANSITION_PLAN.md); [Manifesto §5.4](../manifesto/ANVIL_MANIFESTO.md)):

```text
Main path (v1.x, preserved):
Ready → Activating → Active → Rolling Back → Rolled Back → Archived → Removed

Error state (not a normal stage):
Activating ─┐
            ├──→ Failed ──→ Ready | Archived | Removed
Rolling Back┘

Rollback restore (the forward transition rollback executes on the target):
Archived → Active

Other transitions:
Ready → Archived | Removed          (set aside / deleted before activation)
Rolled Back → Archived | Removed    (preserved for reference / deleted)
```

| State | Semantics |
|---|---|
| **Ready** | The Release exists, installed from a verified Artifact; deployable, awaiting activation; not yet active ([ADR-003 §4.4](../adr/ADR-003-runtime-and-release-lifecycle.md); [Manifesto §5.4](../manifesto/ANVIL_MANIFESTO.md); [PRD-002 §7.7](../prd/PRD-002-anvil-v2.md)) |
| **Activating** | Activation in progress; activation phases executing; promotion not yet committed. A transitional stage — a Release must not remain in it indefinitely ([ADR-003 §4.5, §9.8](../adr/ADR-003-runtime-and-release-lifecycle.md)) |
| **Active** | The live version; exactly one Release is Active at a time ([ADR-003 §4.6, §9.1](../adr/ADR-003-runtime-and-release-lifecycle.md); [Manifesto §5.5](../manifesto/ANVIL_MANIFESTO.md); [PRD-002 §5.2](../prd/PRD-002-anvil-v2.md)) |
| **Rolling Back** | Rollback in progress, restoring the previously active Release. A transitional stage — a Release must not remain in it indefinitely ([ADR-003 §4.7, §9.8](../adr/ADR-003-runtime-and-release-lifecycle.md)) |
| **Rolled Back** | The Release was once active and has been rolled back; preserved for inspection ([ADR-003 §4.8](../adr/ADR-003-runtime-and-release-lifecycle.md); [Manifesto §5.7](../manifesto/ANVIL_MANIFESTO.md)) |
| **Archived** | Preserved for reference, not deployable; ineligible for new activation. The only path out of Archived is rollback restore (the Release last superseded returns to Active) or removal ([ADR-003 §4.9, §7.1, §7.3](../adr/ADR-003-runtime-and-release-lifecycle.md)) |
| **Failed** | Error state, not a normal stage: a lifecycle operation failed and the Release cannot continue its lifecycle. The failure may be recoverable (retry to Ready) or terminal (archive or remove) ([ADR-003 §4.11](../adr/ADR-003-runtime-and-release-lifecycle.md)) |
| **Removed** | Cleanup complete; only the historical record remains. Terminal state — no further transitions ([ADR-003 §4.10](../adr/ADR-003-runtime-and-release-lifecycle.md)) |

### 6.2 Transition legality rules

These are the rules that make transitions enforceable. An implementation must guarantee all of them; an operation that violates one is rejected, never advised against.

| # | Rule | Source |
|---|---|---|
| R1 | **Verification is a mandatory gate.** No lifecycle operation proceeds from unverified inputs; verification is a transition that cannot be skipped, not an optional command | Manifesto §3.2; PRD-002 §5.2, §5.3 |
| R2 | **Transitions are graph-validated.** Every transition is validated against the defined state graph; illegal transitions are rejected, not advised against | Manifesto §3.3; PRD-002 §5.2 |
| R3 | **Exactly one Release is active.** At any time, at most one Release is in the Active state | Manifesto §5.5; PRD-002 §5.2; ADR-003 §9.1 |
| R4 | **Activation is atomic from the observer's perspective.** Either the new Release serves, or the previous one still does; nothing after the commitment point is assumed if the operation reports failure | Manifesto §3.9, §5.5; PRD-002 §5.2 |
| R5 | **Rollback is a forward transition with state-defined legality.** The rollback target is the Release last superseded by the current Active Release — operationally, the Archived Release with the newest archival timestamp among Releases that were previously Active — and it must be in state **Archived**; the operation is rejected otherwise | Manifesto §5.7; PRD-002 §7.8; ADR-003 §7.1, §7.3 |
| R6 | **No silent success.** An interrupted operation is recorded as interrupted and reconciled explicitly; recovery converges state, pointers, and the filesystem before the next operation is accepted | Manifesto §3.10, §5.8; PRD-002 §5.2 |
| R7 | **Installation is idempotent by content identity.** The same Artifact installed twice is one Release, not two | Manifesto §3.4, §5.4; PRD-002 §5.9 |
| R8 | **Decisions derive from state.** Lifecycle facts (what is active, what is installed, what can roll back, what stage a Release is in) are persisted, queryable, and authoritative | Manifesto §3.3; PRD-002 §5.4 |

### 6.3 The installation operation (not a state transition)

Installation is an **operation, not a state-machine transition**: it creates a Release, it does not move one. Per [ADR-003 §4](../adr/ADR-003-runtime-and-release-lifecycle.md), artifact installation creates a Runtime Release directly in **Ready**; there is no separate public Release creation operation. The upload/install target is the verified Artifact, never the reverse — activation targets the resulting Release ([ADR-003 §6.1](../adr/ADR-003-runtime-and-release-lifecycle.md)).

| Operation | Legality condition | Effect | Source |
|---|---|---|---|
| Verified Artifact → Install | Verification evidence present; verification is never skippable (R1); the Artifact is adopted by manifest identity | Creates a Release in state **Ready**; idempotent by content identity (R7) — the same Artifact installed twice is one Release, not two | Manifesto §5.3, §5.4; ADR-003 §4; PRD-002 §5.3, §7.7 |

### 6.4 Transition legality per state-machine edge

Every edge of the state machine, with its legality condition. The edge set is the v1.x release state machine, verified against [ADR-003 §4 and §7](../adr/ADR-003-runtime-and-release-lifecycle.md).

| Transition | Legality condition | Source |
|---|---|---|
| Ready → Activating | Activation requested for an installed Release; activation targets the existing Release, never an uploaded Artifact; verification preceded installation (R1) | ADR-003 §4.4, §6.1, §9.3; Manifesto §5.5; PRD-002 §7.7 |
| Ready → Archived | Release set aside without activation | ADR-003 §4.4 |
| Ready → Removed | Release deleted before activation | ADR-003 §4.4 |
| Activating → Active | All activation phases completed successfully; promotion commits; activation is atomic (R3, R4) | ADR-003 §4.5, §6.3; Manifesto §5.5; PRD-002 §5.2 |
| Activating → Rolling Back | Activation failed; rollback initiated to restore the state before activation began | ADR-003 §4.5, §8.1, §8.4 |
| Activating → Failed | Activation failed and no rollback is possible, or the rollback also failed | ADR-003 §4.5, §8.4 |
| Active → Rolling Back | Rollback requested; the rollback target exists and is eligible (R5) | ADR-003 §4.6, §7.1; Manifesto §5.7; PRD-002 §7.8 |
| Active → Archived | **Automatic** when a new Release is activated: the one-Active invariant requires the previously active Release to become Archived (ADR-003 §9.1); also explicit, for a Release retired without being superseded | ADR-003 §4.6, §9.1 |
| Rolling Back → Rolled Back | Rollback completed successfully as a forward transition; the target restored to Active; the rolled-back Release preserved for inspection (R5) | ADR-003 §4.7; Manifesto §5.7 |
| Rolling Back → Failed | Rollback failed | ADR-003 §4.7 |
| Rolled Back → Archived | Release preserved for reference | ADR-003 §4.8 |
| Rolled Back → Removed | Release deleted | ADR-003 §4.8 |
| Archived → Active | **Rollback restore**: rollback restores the Release last superseded — the rollback target in state Archived — to Active; this is the forward transition the rollback executes on the target (R5) | ADR-003 §7.1, §7.3 |
| Archived → Removed | Explicit cleanup (or retention policy) | ADR-003 §4.9 |
| Failed → Ready | Retry activation after verification passed; the failure was recoverable | ADR-003 §4.11 |
| Failed → Archived | Release retired after failure | ADR-003 §4.11 |
| Failed → Removed | Release deleted after failure | ADR-003 §4.11 |
| Removed → (none) | Terminal state; no further transitions | ADR-003 §4.10 |

> **Note on the state set.** The states and the transition graph above are the v1.x release state machine, which v2 preserves unchanged (Transition Plan §1.2, §2.3); [ADR-003 §4, §7, §9](../adr/ADR-003-runtime-and-release-lifecycle.md) is the architectural definition of that machine — 8 states, with **Failed retained as an error state, not a normal stage** (ADR-003 §4). There is no "interrupted" state in the machine; interruptions are handled by the recovery rules below. The machine-readable schema of this contract is authoritative over the table above where the two disagree (ADR-029 §3).

### 6.5 Recovery rules (not transitions)

Recovery is expressed as **rules applied to the last persisted stage** — not as edges of the state machine ([ADR-003 §4](../adr/ADR-003-runtime-and-release-lifecycle.md)):

- **Interrupted activation.** A crash mid-activation leaves the Release in the last persisted stage, Activating. On recovery, a **status check** determines the outcome: if promotion completed, the Release transitions to Active; if not, it transitions to Failed or is retried ([ADR-003 §8.3](../adr/ADR-003-runtime-and-release-lifecycle.md)).
- **Interrupted rollback.** A Release stuck in Rolling Back is reconciled: if the rollback completed, it transitions to Rolled Back; if the rollback failed, it transitions to Failed ([ADR-003 §4.7, §9.8](../adr/ADR-003-runtime-and-release-lifecycle.md)).
- **Transitional stages are ephemeral.** Activating and Rolling Back are transitional; a Release must not remain in a transitional stage indefinitely — it transitions to Failed or is recovered through explicit intervention ([ADR-003 §9.8](../adr/ADR-003-runtime-and-release-lifecycle.md)).
- **No silent success.** The interruption is recorded and reconciled explicitly; recovery converges state, pointers, and the filesystem before the next lifecycle operation is accepted (R6; [Manifesto §3.10, §5.8](../manifesto/ANVIL_MANIFESTO.md); [PRD-002 §5.2](../prd/PRD-002-anvil-v2.md)).

---

## 7. Scope Boundaries

This document defines the lifecycle model only. Related contracts of the specification are separate corpus documents (authored in EPIC-013):

| Not defined here | Where it is defined |
|---|---|
| Artifact manifest contract (packaging, identity, embedded manifest) | Its own specification corpus document (ST-013-02 / TS-013-02-01) |
| Standard command contract (runtime ↔ standard exchange) | Its own specification corpus document (ST-013-03) |
| Verification contract (gate semantics, evidence requirements) | Its own specification corpus document (ST-013-04) |
| Vocabulary definitions | Specification corpus vocabulary document (TS-013-01-03) |
| Machine-readable form of this model | JSON Schema (TS-013-01-02) — the authority per ADR-029 §3 |
| Engine behavior | The runtime (EPIC-015) implements this contract; engine internals are not part of the specification |

---

## 8. Traceability

| Section | Source of truth |
|---|---|
| §1 Purpose | PRD-002 §5.1; Transition Plan §5.1; ADR-021 §3.1; Manifesto §3.1, §6 |
| §2 Authority, publication, governance | ADR-024 §3; ADR-029 §3; ADR-035 §3; Transition Plan §5.1–§5.2, §5.10 |
| §3 Terminology | 007 §3; Transition Plan §5.1, §5.3; Manifesto §5; PRD-002 §5.5; TS-013-01-03 |
| §4 Stage model | Manifesto §5; PRD-002 §4.1, §5.10, §7.7–§7.8; Transition Plan §2.3; ADR-003 §4; ADR-004 §7 |
| §5 Phase sequence | Manifesto §3.9, §5.5, §5.7, §5.8, §7; 007 §5–§6; PRD-002 §7.7–§7.8; Transition Plan §1.2; ADR-003 §6–§7; ADR-033 |
| §6 State machine semantics | Manifesto §3.2–§3.4, §3.9–§3.10, §5.4–§5.8; PRD-002 §5.2–§5.4, §5.9, §7.7–§7.8; Transition Plan §1.2, §2.3; ADR-003 §4, §7–§9 |
| §7 Scope boundaries | EPIC-013; ADR-029 §3 |
| §8 Traceability | — |

---

*End of lifecycle-model.md*
