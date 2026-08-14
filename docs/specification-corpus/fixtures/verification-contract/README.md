# Verification Contract — Fixture Matrix

Fixtures for the machine-readable authority of the verification contract:
`docs/specification-corpus/verification-contract.schema.json` (TS-013-04-02).

Every fixture is a **verification declaration**: a document in which a delivery
lifecycle standard declares its verification content against the contract — the
gate semantics it is bound to (G1–G6, including the no-op gate nuance), the
evidence requirements its checks must satisfy (E1–E5), the checks it declares
as capability (structural and lifecycle-conformity), and the position bindings
where those checks execute. The schema governs on any disagreement with
`verification-contract.md` (ADR-029 §3).

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
| `positive/full-declaration.json` | Full conforming declaration: 5 checks in both contract categories (structural + lifecycle-conformity) bound to both fixed gate positions (Verify stage, verify phase); the complete gate and evidence affirmation sets (G1–G6 + no-op nuance; E1–E5) |
| `positive/minimal-no-op-gate.json` | Zero declared checks with empty position bindings: the gate positions remain and the gates remain — a no-op gate, not an open door (§4.4; lifecycle-model §5.1) |
| `positive/verify-phase-only.json` | Asymmetric declaration: empty Verify stage binding (the stage gate remains — integrity is owned by the artifact manifest contract), lifecycle-conformity checks bound to the verify phase only |

## Negative fixtures (must be rejected)

| Fixture | Rule violated | Rejection mechanism |
|---|---|---|
| `negative/gate-weakening-skippable.json` | G1/G4 — verification declared skippable (`verificationIsMandatory: false`) | `gates.verificationIsMandatory` const rejects `false` |
| `negative/gate-weakening-optional-check.json` | G4 — a declared check is marked optional (`skippable: true`) | `checks[].skippable` rejected by `additionalProperties` (the contract has no optional-check concept) |
| `negative/gate-omission.json` | G4 — the gate floor is not affirmed (`standardAddsChecksNeverWeakensGates` omitted) | `gates` required rejects the omission |
| `negative/empty-declaration-opens-gate.json` | §4.4 — an empty declaration asserted to open the gate (`emptyDeclarationKeepsGate: false`) | `gates.emptyDeclarationKeepsGate` const rejects `false` |
| `negative/undeclared-capability-category.json` | §6.2 / Manifesto §7 — check category `integrity` invented; only structural and lifecycle-conformity are contract categories | `checks[].category` enum rejects `"integrity"` |
| `negative/undeclared-capability-section.json` | §6.2 / Manifesto §7 — unknown top-level section `customVerification`; undeclared capability is never called | root `additionalProperties` rejects the section |
| `negative/invented-gate-position.json` | §4.2 — checks bound to an invented position `postPromote`; a standard does not invent gates or move gate positions | `positions` `additionalProperties` rejects `"postPromote"` |
| `negative/non-recheckable-evidence.json` | E2/E5 — evidence declared `recheckable: false`; evidence that cannot be re-checked is invalid | `checks[].evidence.recheckable` const rejects `false` |
| `negative/claim-is-not-evidence.json` | E1 — evidence declared as a textual claim instead of the evidence declaration | `checks[].evidence` `additionalProperties` + required reject the claim object |
| `negative/malformed-evidence.json` | E3/E4 — evidence omits `recorded`; outcomes must merge into the verification report and be recorded as lifecycle evidence | `checks[].evidence.recorded` required rejects the omission |
| `negative/evidence-omission.json` | §5 — the evidence requirements section is omitted entirely | root `required` rejects the missing `evidence` section |
| `negative/empty-check-id.json` | §6.2 — declared check with an empty `id`; an empty identifier is not declared capability | `checks[].id` pattern rejects `""` |

## Deliberate design notes

- **Gate weakening is rejected by construction.** Every gate rule (G1–G6) and
  evidence rule (E1–E5) is a required `const: true` declaration: a declaration
  cannot weaken a gate silently — it must either omit the rule (required
  rejects) or assert `false` (const rejects).
- **The no-op gate nuance is a first-class gate declaration**
  (`emptyDeclarationKeepsGate`): declaring zero checks leaves a gate, not an
  open door (§4.4; lifecycle-model §5.1). Both position bindings may be empty
  and `checks` may be an empty array.
- **Checks are declared capability.** A single `checks` array with a `category`
  enum (`structural` | `lifecycle-conformity`) keeps the two contract
  categories exclusive and rejects invented categories; position bindings
  reference checks by id. Draft-07 cannot cross-validate id references between
  arrays, so id-to-declared-check resolution is enforced by the runtime at
  adoption (undeclared capability is never called — Manifesto §7); the schema
  enforces the declaration surface: shape, categories, and positions.
- **Evidence is encoded at two levels.** The contract-level evidence rules
  (E1–E5) and a per-check evidence declaration (`recheckable`,
  `embeddedOrRecorded`, `recorded`) — a check whose evidence cannot be
  re-checked is rejected at declaration time (E5).
- **Formats are deliberately not specified.** Concrete check declaration and
  evidence formats are deferred to EPIC-013 implementation design
  (verification-contract.md §6.3); the schema defines the declaration shape,
  not the formats.
- **Engine-path-independent.** The schema references no engine paths, no engine
  internals, and no standard content — a re-home of the specification is a
  move, not a rewrite (ADR-029 §3; Transition Plan §5.2, §5.10).
