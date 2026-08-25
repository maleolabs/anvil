---
name: flutter-conventions
description: "Flutter project conventions under the Anvil delivery lifecycle standard — hybrid build model, scaffold, build pipeline, configuration surface, and verification expectations. Use when working on a Flutter project managed by Anvil: check the declared standard capability before assuming framework-version support, and follow the generated hybrid pipeline rather than inventing build steps."
license: MIT
---

# Flutter Conventions

The Flutter delivery lifecycle standard (`anvil-standard-flutter`) owns all
Flutter-specific knowledge in Anvil: the hybrid deployment lifecycle phases,
verification rules, build templates, and configuration surface. Anvil itself
is framework-agnostic — it runs the standard as a subprocess and never
embeds Flutter behavior.

## Project identity

- The Anvil project is a directory tree with `anvil.yaml` at its root. Run
  `anvil init <name>` to create one, then register the Flutter standard:
  `anvil standard install anvil-standard-flutter <version>`.
- The standard declares its **framework-version support scope** in its
  registry metadata (`capability.frameworkVersion`). Before writing
  Flutter-version-specific guidance, check the installed standard's declared
  scope (`anvil standard list`) — a mismatch between the project's Flutter
  version and the standard's support scope is a compatibility problem, not a
  convention to work around.
- The standard targets a specific contract version of the delivery lifecycle
  specification. Follow the declared contract surface; the standard supplies
  content within the lifecycle, it does not redefine it.

## Hybrid build model

- Flutter projects under Anvil use the **hybrid deployment model**: releases
  are built and packaged for distribution (web bundle, APK, iOS app) and
  distributed to their targets — there is no server-side install or
  in-place activation, and the runtime does not serve the release's
  outputs.
- Build outputs are generated from the **installed standard** (A10), never
  from Anvil core. Do not hand-write a parallel build; extend or adjust the
  generated hybrid pipeline through the standard's configuration surface.
- Flutter-specific configuration keys live under the framework's own
  namespace (`framework.flutter.*`, e.g. `framework.flutter.targets`,
  `framework.flutter.build_args`) and are validated by the standard itself.
  Anvil enforces namespace isolation and passes values through; it does not
  interpret them.

## Build conventions

The generated pipeline reflects the Flutter hybrid build shape:

- dependency resolution (`flutter pub get` with the lockfile committed) and
  the declared build steps producing the packaged outputs, each
  reproducible from a clean checkout;
- the standard's `flutter build` invocation follows the declared targets
  and build arguments (`framework.flutter.targets`,
  `framework.flutter.build_args`);
- verification steps that run before a release is accepted (see below).

## Verification expectations

- The standard declares **six verification checks**. Two are structural
  presence checks: `pubspec_yaml` (`pubspec.yaml` present) and
  `lib_directory` (`lib/` present). Four are lifecycle-conformity checks:
  `dependency_lockfile` (the locked dependency set is present so
  activation's `pub_get` re-resolves the built set), `dependency_timing`
  (the pre-promotion resolution timing evidence), `platform_sync_ready`
  (the platform step's input — `ios/Podfile` — present when the release
  carries an `ios/` directory), and `rollback_behavior` (the declared
  rollback surface stays coherent with the phase table).
- Gates are mandatory and unskippable: the standard adds checks, it never
  weakens them. Verification outcomes are recorded as lifecycle evidence;
  treat a failed check as a release blocker, not a warning.

## Configuration surface

- Read the standard's declared configuration extension before setting
  Flutter options: keys under `framework.flutter.*` are validated by the
  standard, and unknown or mis-typed values are rejected with actionable
  errors.
- Do not invent keys outside the declared namespace; Anvil rejects unknown
  scalar values it does not recognize.

## When to use this skill

- Orienting in a Flutter project managed by Anvil (what is generated, what
  is verified, what the configuration surface is).
- Writing or editing build configuration for a Flutter project under Anvil,
  especially hybrid outputs.
- Checking whether a Flutter feature is supported by the installed standard's
  capability declaration.

For the lifecycle itself — activation, rollback, failure semantics — load
`flutter-delivery`.
