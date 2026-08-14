# Vocabulary

## The Lifecycle Vocabulary of the Delivery Lifecycle Specification

| Metadata | |
|---|---|
| **Document ID** | specification-corpus/vocabulary |
| **Status** | Draft |
| **Date** | 2026-08-04 |
| **Product** | Anvil |
| **Dependencies** | [PRD-002 §5.5](../prd/PRD-002-anvil-v2.md) · [007-delivery-lifecycle-standard-specification §3](../architecture/007-delivery-lifecycle-standard-specification.md) · [ANVIL_V2_TRANSITION_PLAN §4.5, §5.1](../planning/ANVIL_V2_TRANSITION_PLAN.md) · [ADR-024](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md) · [ADR-029](../adr/ADR-029-specification-publication-format.md) · [ADR-035](../adr/ADR-035-governance-and-identity-reframing-amendments.md) · [ST-012-04](../work-items/stories/ST-012-04-terminology-and-v1x-to-v2-concept-mapping.md) (EPIC-012 terminology) |
| **Consumers** | Standard authors · Anvil Runtime implementers · specification corpus documents (lifecycle model, contracts) · EPIC-013 · EPIC-019 |

---

## 1. Purpose

This document is the **vocabulary** part of the delivery lifecycle specification corpus. It defines the terms of the delivery lifecycle — **artifact, release, activation, rollback, standard, contract version** — so that every layer of the product speaks one vocabulary (PRD-002 §5.5).

The vocabulary is part of the delivery lifecycle specification, the authority every other layer conforms to: lifecycle model, manifest contract, standard command contract, verification contract, and vocabulary (Transition Plan §5.1; 007 §3).

Authoring constraints, consistent with the corpus:

- **Engine-path-independent.** This document defines what the terms mean for the corpus, not how the Anvil Runtime implements them; it references no engine paths and no engine internals (ADR-029 §3).
- **One meaning per term.** Terminology is consistent everywhere the lifecycle is described; a term's meaning here is the meaning everywhere (Manifesto §4.1).
- **No new vocabulary.** Every term in this document is drawn from the approved sources: PRD-002 §5.5, 007 §3, ST-012-04, and the sources they cite.

The lifecycle model documentation is published as a sibling corpus document; the vocabulary here is the shared reference for the terms that model uses (ST-013-01).

## 2. Relationship to the EPIC-012 terminology work

The standard model's terminology is defined at concept level by EPIC-012 (ST-012-04); this corpus document is the **published form** of that terminology within the delivery lifecycle specification (TS-013-01-03).

> **Note on terminology ownership.** TS-012-04-01 (Sprint ANVIL-V2-S2) is the formal owner of the standard-model term definitions. This vocabulary is consistent with the EPIC-012 story (ST-012-04) and will be finalized consistent with the Sprint 2 output. Until then, this document publishes the lifecycle vocabulary grounded directly in PRD-002 §5.5, 007 §3, and the Transition Plan.

The complete standard-model terminology table is maintained in [007 §3](../architecture/007-delivery-lifecycle-standard-specification.md); the v1.x→v2 term mapping is maintained in [Transition Plan §5.9](../planning/ANVIL_V2_TRANSITION_PLAN.md) and documented by the EPIC-012 mapping work (TS-012-04-02). Neither is duplicated here. Consistent with ST-012-04, v1.x adapter terminology appears in this corpus only in the v1.x→v2 migration context.

## 3. Lifecycle vocabulary

The six lifecycle terms, defined in corpus language:

| Term | Definition | Source |
|---|---|---|
| **artifact** | The immutable deployable object produced by packaging under the artifact manifest contract: content-derived identity, embedded manifest, deterministic output. Filenames and paths carry no identity. An artifact becomes part of the lifecycle when a verified artifact is adopted as a release. | PRD-002 §5.9; Manifesto §3.4, §5.2; 006 §4.2 |
| **release** | The lifecycle object that carries a verified artifact through the release lifecycle — install, activate, rollback, recover. A release is not an artifact: the artifact is the immutable payload; the release is the adopted instance with lifecycle state. The same artifact installed twice is one release, not two. | PRD-002 §5.9, §5.10; Manifesto §4.1 |
| **activation** | The lifecycle transition that makes a release the active release: the lifecycle phases run, followed by an atomic promotion, so that exactly one release is active at a time. Activation is not deployment: deployment transports the artifact; activation changes lifecycle state. | PRD-002 §5.2, §5.10, §7.7; Manifesto §4.1, §5.4–5.5 |
| **rollback** | The lifecycle transition that restores the previously active release as the active release. Rollback is a forward transition — the lifecycle moves forward to a known, recorded state — and the rolled-back release is preserved for inspection. Legality is defined by lifecycle state: the rollback target must exist and be eligible; the operation is rejected otherwise. | Manifesto §5.7; PRD-002 §5.10, §7.8; 007 §5 |
| **standard** | Two scopes, deliberately shared (Transition Plan §5.1): **the** delivery lifecycle standard — Anvil as a whole, specification plus runtime, the identity; **a** delivery lifecycle standard — the distributable unit of framework lifecycle knowledge: what a legal lifecycle *contains* for one framework — phases, verification, configuration surface, rollback semantics, templates — packaged, versioned, and verifiable against the delivery lifecycle specification. Context disambiguates the two scopes. | Transition Plan §5.1, §5.3; 007 §3 |
| **contract version** | The version of the delivery lifecycle specification a standard targets; the compatibility basis between a standard and the Anvil Runtime, declared rather than assumed. The contract major version is the unit of compatibility; a contract major change is a governed event. | 007 §3; Transition Plan A2, §5.9; ADR-024 §3 |

**Distinctions.** The distinctions between terms are part of the vocabulary (Manifesto §4.1):

- A release is not an artifact.
- Activation is not deployment.
- Rollback is a forward transition: it restores the previous active release; it does not reverse the lifecycle.

## 4. Related standard-model terms

The remaining standard-model terms are defined in the terminology table of 007 §3 and owned by the EPIC-012 terminology work (ST-012-04; TS-012-04-01). One-line forms are reproduced here for completeness of the published vocabulary; on any disagreement, the definitions in 007 §3 and the EPIC-012 terminology output prevail:

| Term | Definition | Source |
|---|---|---|
| **delivery lifecycle specification** | What a legal lifecycle *is* — the authority every other layer conforms to: lifecycle model, manifest contract, standard command contract, verification contract, vocabulary. Authored, versioned, and published from the Core repository. | 007 §3; Transition Plan §5.1, §5.2 |
| **Anvil Runtime (Core)** | The engine: implements and enforces the specification and executes standards as subprocesses. The same artifact as Core; must not be confused with the Server Runtime Domain (one of the four bounded contexts inside the runtime). | 007 §3; Transition Plan §5.1 |
| **standard content** | The distributable framework lifecycle knowledge a standard carries — the seven parts of the standard structure. | 007 §3; Transition Plan §5.4 |
| **capability declaration** | What a standard provides — lifecycle phases, verification checks, config extensions, templates — declared so the runtime invokes only declared capability; undeclared capability is never called. | 007 §3; Manifesto §7; Transition Plan §5.9 |
| **framework-version support scope** | The framework versions a standard supports, declared per release; enables real compatibility validation. | 007 §3; Transition Plan A5, §5.4 |

## 5. Ownership of the vocabulary

- **The vocabulary is owned by Core; standards must not redefine its semantics** (PRD-002 §5.5; ST-012-04).
- Ownership is architectural, not editorial: lifecycle vocabulary is one of the surfaces of the delivery domain Anvil owns (PRD-002 §2.3) and one of Core's responsibilities (006 §5); the ownership boundary is recorded in the ownership table (Transition Plan §4.5; 008 §4).
- **What this means for standards.** A standard documents its framework's lifecycle behavior but must not redefine the semantics of the lifecycle terms (007 §9). Standards supply content within the defined lifecycle; they do not invent vocabulary (Transition Plan §5.5). A standard that violates the specification's contracts — vocabulary included — is rejected by the registry, not patched by Core (Transition Plan §5.5; 007 §2).

## 6. Governance of vocabulary changes

- The vocabulary is part of the delivery lifecycle specification, a **governed architecture artifact of the Core repository**: authored in Core; contract-breaking changes require an ADR; consumed by ADRs, the Architecture Overview, delivery lifecycle standards, and the conformance harness (ADR-035 §3.1).
- A change to the meaning of a lifecycle term is a **contract change** — a Core-scale governed event (Manifesto §3.6; ADR-035 §3.1) — and therefore follows the specification's versioning policy: the specification carries its own independent version line, and the contract major version is the unit of compatibility (ADR-024 §3).
- Vocabulary changes enter the corpus under the specification's governance: authored in the Core repository and published in the defined publication format — human-readable documentation plus the machine-readable authority (ADR-029 §3).
- Authoring constraint: vocabulary changes must preserve the corpus's engine-path independence — the vocabulary documents meaning for the corpus, never engine paths or internals (ADR-029 §3).

## 7. Traceability

| Section | Source of truth |
|---|---|
| §1 Purpose | PRD-002 §5.5; Manifesto §4.1; Transition Plan §5.1; ADR-029; 007 §3 |
| §2 Relationship to EPIC-012 | ST-012-04; TS-012-04-01; 007 §3; Transition Plan §5.9 |
| §3 Lifecycle vocabulary | PRD-002 §5.5, §5.9, §5.10, §7.7, §7.8; Manifesto §3.4, §4.1, §5.2, §5.4–5.5, §5.7; 007 §3, §5; Transition Plan §5.1, §5.3, A2; ADR-024 |
| §4 Related standard-model terms | ST-012-04; 007 §3; Transition Plan §5.1, §5.4, §5.9; Manifesto §7 |
| §5 Ownership | PRD-002 §5.5, §2.3; ST-012-04; Transition Plan §4.5, §5.5; 006 §5; 007 §2, §9; 008 §4 |
| §6 Governance | ADR-035 §3; ADR-024 §3; ADR-029 §3; Manifesto §3.6 |
| §7 Traceability | — |

---

*End of specification-corpus/vocabulary*
