---
name: flutter-delivery
description: "The Flutter delivery lifecycle under Anvil — activation phases for the hybrid deployment model (pub_get before promotion, platform_sync), per-phase failure and rollback semantics, and how to adopt the lifecycle. Use when planning or reviewing a Flutter release activation, rollback, or adoption under Anvil."
license: MIT
---

# Flutter Delivery Lifecycle

The Flutter standard supplies the lifecycle content within the delivery
lifecycle defined by the specification: the standard declares phases, order,
and semantics for the **hybrid deployment model**; Anvil enforces the stage
model and transition rules. This skill is the agent-facing summary of what
activation runs, what rollback reverses, and what to check.

## Hybrid deployment model

The hybrid model has **no server-side install or in-place activation**:
releases are built and packaged for distribution (web bundle, APK, iOS
app) and distributed to their targets. There is no server to initialize,
no release to activate in place, and the runtime does not serve the
release's outputs.

## Activation phases (declared order)

A Flutter release activates through the standard's declared phases, in the
declared order:

1. **`pub_get`** — `flutter pub get`. Dependency resolution runs **before
   promotion**: the release's locked dependency set (pubspec.lock) is
   re-resolved from the artifact, so activation reproduces exactly the set
   the artifact was built from. This is the only reversible phase, and its
   rollback is the same idempotent re-resolution (`flutter pub get`).
2. **`platform_sync`** — `pod install`. Platform steps on the resolved
   dependency set, **conditional**: it runs only when the release contains
   an `ios/` directory (and on the platform the phase requires); without
   `ios/` it is an informational no-op. **Irreversible** — it never blocks
   rollback.

There are no other activation phases: nothing is served by a server
runtime, and the hybrid model declares no queue or worker to restart.

## Failure semantics

- Every phase reports a structured outcome through the activation contract.
- A failing `pub_get` means the dependency set could not be resolved before
  promotion — activation fails.
- A failing `platform_sync` surfaces as a reported failure and activation
  stops — the phase reports its outcome through the activation contract,
  and there is no declared recovery procedure to run.

## Rollback semantics

- Rollback **re-resolves the dependency set** — the idempotent
  `flutter pub get` re-resolution — rather than "swapping served outputs":
  there is nothing served in place to swap.
- `platform_sync` is irreversible: rollback proceeds and reports it as
  informational, never blocking.
- Irreversibility never blocks rollback.

## Adoption

- Register the standard: `anvil standard install anvil-standard-flutter
  <version>`. Adoption pins the version; release metadata, trust anchors,
  and the declared capability are validated at adoption.
- Distribute the packaged artifact (web bundle, APK, iOS app) to its
  targets; deployment in the hybrid model means distribution, not
  in-place activation.
- Where the runtime exposes the lifecycle surface, activation/rollback run
  through the deployment commands (`anvil deployment activate/rollback`)
  or the server-runtime release commands (`anvil server release
  activate/rollback`).
- Check lifecycle state and evidence: `anvil status` / `anvil project
  status` and the verification report record what ran and what passed.

## Verification

- The standard declares six verification checks: the structural checks
  `pubspec_yaml` and `lib_directory` (artifact presence), plus the
  lifecycle-conformity checks `dependency_lockfile`, `dependency_timing`,
  `platform_sync_ready`, and `rollback_behavior` (the declared
  pre-promotion resolution, the platform step at its declared point, and
  the declared rollback surface). Gates are mandatory: a failed check
  blocks acceptance. Never bypass a check to "get the release out".

## When to use this skill

- Planning or executing a Flutter release activation or rollback under Anvil.
- Diagnosing why an activation failed or what a rollback will and will not
  undo for a hybrid release.
- Reviewing whether a Flutter project's release flow matches the standard's
  declared lifecycle.

For project conventions — scaffold, build, config surface, verification
expectations — load `flutter-conventions`.
