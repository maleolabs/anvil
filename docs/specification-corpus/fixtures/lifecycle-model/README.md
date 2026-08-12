# Lifecycle Model — Fixture Matrix

Fixtures for the machine-readable authority of the lifecycle model:
`docs/specification-corpus/lifecycle-model.schema.json` (TS-013-01-02).

Every fixture is a **lifecycle definition**: a document declaring the release
state machine, activation phase sequence, rollback semantics, installation
semantics, and machine invariants. The schema governs on any disagreement with
`lifecycle-model.md` (ADR-029 §3).

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
| `positive/full-main-path.json` | Full legal main path `Ready → Activating → Active → Rolling Back → Rolled Back → Archived → Removed`; complete 8-state machine with all legal edges; activation with 2 framework phases + 1 verify check |
| `positive/rollback-restore.json` | Rollback restore: `Archived → Active` forward transition declared legal (R5, §5.2); activation with 1 framework phase and empty verify |
| `positive/minimal-activation.json` | Activation with zero framework phases (0..n permitted) and empty verify (no-op gate): sequence exactly `prepare,configure,verify,promote` |

## Negative fixtures (must be rejected)

| Fixture | Rule violated | Rejection mechanism |
|---|---|---|
| `negative/illegal-transition-ready-to-active.json` | §6.4 / R2 — `Ready → Active` direct edge is illegal (ADR-003 §9.7: no stage skipping) | `states.Ready.transitions` item enum rejects `"Active"` |
| `negative/unknown-state.json` | §6.1 — exactly 8 contract states; no invented states | `states` propertyNames enum + `additionalProperties` reject `"Deploying"` |
| `negative/interrupted-state.json` | §6.1 note / ADR-003 §4 — there is NO `interrupted` state; recovery is a rule set (§6.5), not a state | `states` propertyNames enum + `additionalProperties` reject `"interrupted"` |
| `negative/missing-promote.json` | §5.1 / ADR-003 §6.4 — promote is the atomic commitment point, last in the sequence | `activation.phaseSequence` pattern rejects `prepare,configure,verify` |
| `negative/verify-after-promote.json` | §5.1 — verify position is fixed immediately before promote; nothing after the commitment point | `activation.phaseSequence` pattern rejects `prepare,configure,promote,verify` |
| `negative/rollback-target-not-archived.json` | R5 / §5.2 — rollback target must be in state `Archived` | `rollback.targetState` const rejects `"Active"` |
| `negative/installation-creates-not-ready.json` | §6.3 / ADR-003 §4 — installation creates a Release in `Ready`; it is an operation, not a transition | `installation.createsState` const rejects `"Activating"` |

## Deliberate design notes

- The phase sequence is declared as a comma-separated string so that the fixed
  tail rule (verify immediately before promote, promote last) is machine-checkable:
  the schema pattern is `^prepare,configure(,((?!prepare|configure|verify|promote)[^,]+))*,verify,promote$`.
- All 8 states are required in `states`: the contract machine is fixed, so a
  definition omitting a state is non-conforming.
- The one-Active and atomicity invariants span Releases and cannot be proven on
  a single definition document; they are encoded as required `invariants`
  declarations (documented constraints, ADR-003 §9).
