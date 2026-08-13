# Standard Command Contract — Fixture Matrix

Fixtures for the machine-readable authority of the standard command contract:
`docs/specification-corpus/command-contract.schema.json` (TS-013-03-02).

Every fixture is a **command contract declaration**: a document in which a
delivery lifecycle standard declares its exchange surface against the contract
— the declared capability surface (lifecycle phases, verification checks, config
extensions, templates, plus the contract-version and framework-version fields),
the lifecycle-phase exchange (activation sequence within
prepare → configure → framework phases → verify → promote, per-phase failure and
rollback semantics, rollback as a forward transition), the verification exchange
(fixed pre-promote position, no-op gate bound, adds-never-weakens), the
configuration-extension exchange (framework namespace isolation), and the
exchange rules C1–C7 plus the declared-surface bound. The schema governs on any
disagreement with `command-contract.md` (ADR-029 §3).

## Running the validation

```bash
bash scripts/validate-schemas.sh
```

Rules enforced by the harness:

- every fixture under `positive/` MUST validate against the schema;
- every fixture under `negative/` MUST fail validation;
- exit code is non-zero on any unexpected result.

## Positive fixtures (must validate)

| Fixture | Exercises |
|---|---|
| `positive/full-declaration.json` | Full conforming declaration: 2 lifecycle phases with declared failure + rollback semantics, 3 verification checks in both contract categories (structural + lifecycle-conformity), 1 config extension under the `rails` namespace with 2 keys, 2 templates; activation sequence `prepare,configure,migrate,cache-warm,verify,promote`; the complete exchange rule set C1–C7 plus the declared-surface bound |
| `positive/minimal-empty-declaration.json` | Declares nothing in every capability category with sequence exactly `prepare,configure,verify,promote`: the declared-surface bound (§4.1) — a standard may declare nothing at all; the runtime proceeds with its generic operations; the verify position remains a no-op gate, not an open door (§4.4; verification-contract §4.4) |
| `positive/verify-only-declaration.json` | Asymmetric declaration: capability declared in exactly one category (verification checks), lifecycle phases / config extensions / templates empty — the declared-surface bound per category |
| `positive/config-only-declaration.json` | Asymmetric declaration: capability declared in exactly one category (config extensions under the `rails` namespace with required/default keys); the configuration exchange affirms namespace isolation |
| `positive/irreversible-rollback-declaration.json` | Irreversible framework phase (`db-schema-change` with `irreversible: true`): the standard documents irreversibility; irreversibility never blocks rollback (C4; lifecycle-model §5.2) |
| `positive/prefix-named-phases.json` | Prefix-named framework phases (`prepared`, `verify-notes`) in the sequence `prepare,configure,prepared,verify-notes,verify,promote`: a prefix of a reserved phase name is not a collision — the reserved-name rule rejects only a full-token collision with prepare, configure, verify, promote (case-insensitive, §4.2) |

## Negative fixtures (must be rejected)

| Fixture | Rule violated | Rejection mechanism |
|---|---|---|
| `negative/undeclared-capability-section.json` | C1 / §4.1 — top-level `diagnosticExchange` section added (a v1.x adapter capability, 005 §5.1, not declared by the v2 contract) | root `additionalProperties` rejects the section |
| `negative/undeclared-capability-category.json` | C1 / §4.1 — capability category `diagnosticCommands` invented; exactly four contract categories exist | `capability` `additionalProperties` rejects the category |
| `negative/undeclared-capability-check-category.json` | C1 / §4.4 — check category `integrity` invented; only structural and lifecycle-conformity are contract categories (integrity is owned by the artifact manifest contract) | `verificationChecks[].category` enum rejects `"integrity"` |
| `negative/invented-phase-position.json` | C2 / C3 / §4.2 — phase `cleanup` declared after promote; a standard invents neither phases nor positions | `phaseSequence` pattern rejects the post-promote phase |
| `negative/reserved-phase-name.json` | C3 / §4.2 — framework phase named `prepare`, colliding with the reserved runtime-owned position | `phaseSequence` pattern rejects the reserved-name collision |
| `negative/missing-capability-declaration.json` | §4.1 — the declared surface is omitted; the declaration is the invocation boundary | root `required` rejects the missing `capability` |
| `negative/missing-contract-version.json` | §4.1 / ADR-024 §3 — compatibility is not declared; a standard that does not declare compatibility is rejected (PRD-002 §5.8) | root `required` rejects the missing `contractVersion` |
| `negative/malformed-contract-version.json` | ADR-024 §3.1 — `contractVersion` must be semver; the major is the compatibility unit | `contractVersion` pattern rejects `"1.0"` |
| `negative/malformed-framework-version.json` | §4.1 / 006-h §3.5 — framework-version support scope entries must be semver | `frameworkVersion` items pattern rejects `"8"` |
| `negative/missing-lifecycle-exchange.json` | §4.2–§4.3 — the lifecycle-phase exchange is omitted; invocation is assumed rather than declared | root `required` rejects the missing `lifecycleExchange` |
| `negative/missing-verification-exchange.json` | §4.4 — the verification exchange is omitted; verification is a lifecycle gate, not an optional command | root `required` rejects the missing `verificationExchange` |
| `negative/missing-configuration-exchange.json` | §4.5 — the configuration-extension exchange is omitted; isolation is assumed rather than declared | root `required` rejects the missing `configurationExchange` |
| `negative/missing-exchange-rules.json` | §4.6 — the exchange rules C1–C7 are omitted; the rule set is a floor, not a ceiling | root `required` rejects the missing `exchangeRules` |
| `negative/malformed-phase-sequence.json` | §4.2 / lifecycle-model §5.1 — `verify` omitted; verify is fixed immediately before promote | `phaseSequence` pattern rejects the sequence |
| `negative/phase-without-rollback-semantics.json` | §4.3 / 007 §5 — per-phase reversal is not declared; the standard declares how each phase is reversed | `lifecyclePhases[]` required rejects the omission |
| `negative/phase-without-failure-semantics.json` | §4.2 / 007 §5 — per-phase failure meaning is not declared; content is never assumed (C3) | `lifecyclePhases[]` required rejects the omission |
| `negative/config-extension-dotted-namespace.json` | §4.5 / 007 §7 — namespace `my.rails` is multi-segment; extended configuration lives under the framework's own single-segment namespace (005 §6.1, preserved) | `configExtensions[].namespace` pattern rejects the value |
| `negative/config-extension-missing-namespace.json` | §4.5 / 007 §7 — the namespace (the isolation boundary) is omitted | `configExtensions[]` required rejects the omission |
| `negative/exchange-rule-weakening.json` | C1 — `onlyDeclaredCapabilityInvoked: false`; undeclared capability may be invoked | `exchangeRules` const rejects `false` |
| `negative/verification-gate-weakening.json` | C5 / G4 — `standardAddsChecksNeverWeakensGates: false`; a standard never weakens gates | `verificationExchange.rules` const rejects `false` |
| `negative/no-op-gate-weakening.json` | §4.4 / verification-contract §4.4 — `zeroDeclaredChecksKeepsNoOpGate: false`; declaring nothing opens the gate | `verificationExchange.rules` const rejects `false` |
| `negative/empty-declaration-weakening.json` | §4.1 declared-surface bound — `emptyDeclarationProceedsWithGenericOperations: false`; an empty declaration must not block the generic operations | `exchangeRules` const rejects `false` |
| `negative/invented-exchange-rule.json` | §4.6 / C2 — invented rule `phasesSkippable: true`; the rule set is fixed, an invented weakening rule is undeclared capability | `exchangeRules` `additionalProperties` rejects the rule |
| `negative/invented-verification-position.json` | §4.4 / C3 — position `postPromote`; the position is fixed immediately before promote | `verificationExchange.position` const rejects the value |
| `negative/rollback-forward-transition-weakening.json` | §4.3 / Manifesto §5.7 — `isForwardTransition: false`; rollback is a forward transition, not a reverse activation | `lifecycleExchange.rollback` const rejects `false` |

## Deliberate design notes

- **Rule weakening is rejected by construction.** Every exchange rule — C1–C7
  (command-contract.md §4.6), the rollback affirmations (§4.3), the verification
  exchange rules (§4.4), and the configuration exchange rules (§4.5) — is a
  required `const: true` declaration: a declaration cannot weaken a rule
  silently — it must either omit it (required rejects) or assert `false` (const
  rejects).
- **The declared-surface bound is a first-class exchange rule**
  (`emptyDeclarationProceedsWithGenericOperations`), the reviewer-flagged
  consistency with the verification contract's no-op gate concept
  (verification-contract §4.4): a standard may declare nothing in a category, or
  nothing at all, and the runtime proceeds with its generic operations (§4.1;
  ADR-026 §3) — `positive/minimal-empty-declaration.json` is conforming — while
  an empty declaration is a no-op, never an open door. The verification exchange
  encodes the same bound per exchange
  (`zeroDeclaredChecksKeepsNoOpGate`), mirroring
  `gates.emptyDeclarationKeepsGate` in verification-contract.schema.json.
- **Undeclared capability is rejected by construction of the surface.**
  The declaration shape is fixed: exactly the four capability categories
  (lifecycle phases, verification checks, config extensions, templates), exactly
  the two check categories (structural, lifecycle-conformity), exactly the fixed
  activation sequence positions, exactly the `verify` position, exactly the
  fixed rule sets. Content outside the shape — an invented section, category,
  position, rule, or phase — is undeclared capability and is rejected by
  `additionalProperties`, `enum`, `const`, and the sequence `pattern`.
- **Single source per declared surface.** The lifecycle-phase category carries
  the full phase declarations (failure + rollback semantics); the exchange's
  activation sequence declares the order of those phases. Draft-07 cannot
  cross-validate references between arrays (sequence names against declared
  phase ids, extension key namespaces against declared namespaces), so
  declared-capability resolution is enforced by the runtime at adoption and
  re-verified at execution (ADR-024 §3.6) — undeclared capability is never
  called (C1); the schema enforces the declaration surface: shape, categories,
  positions, sequence bounds, and the rule floors.
- **The verification exchange stays in its lane.** The command contract encodes
  the exchange-level facts — fixed position, no-op gate bound, adds-never-weakens
  (C5 / G4), outcomes merge into the runtime's verification report — while the
  full gate set (G1–G6) and evidence requirements (E1–E5) belong to the
  verification contract (command-contract.md §4.4; ADR-033 §3) and are not
  re-encoded here; the declared checks reference the verification contract's
  categories.
- **Formats are deliberately not specified.** Concrete exchange formats —
  command names, payload shapes, process conventions — are deferred to EPIC-013
  implementation design (command-contract.md §5, §8); the schema defines the
  declaration shape, not the formats.
- **Engine-path-independent.** The schema references no engine paths, no engine
  internals, and no standard content — a re-home of the specification is a
  move, not a rewrite (ADR-029 §3; Transition Plan §5.2, §5.10).
