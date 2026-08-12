# Anvil Wiki

This wiki is the **usage documentation** for Anvil: the delivery lifecycle specification, the Anvil Runtime (Core), and the delivery lifecycle standards it distributes. Project/architecture documentation lives in [`docs/`](../docs/) and is deliberately kept separate.

> **Historical note (v1.x):** this wiki was previously the *Adapters Wiki* — usage documentation for v1.x framework adapters. Since the v2 vocabulary alignment (ADR-032), framework lifecycle content is delivered as **delivery lifecycle standards**, and "adapter" appears in this wiki only in v1.x→v2 mapping contexts ([Transition Plan §5.9](../docs/planning/ANVIL_V2_TRANSITION_PLAN.md), [006-g — v1.x→v2 concept mapping](../docs/architecture/006-g-v1x-to-v2-concept-mapping.md)).

## What is a delivery lifecycle standard?

A delivery lifecycle standard gives Anvil framework-specific behavior on top of the generic Core:

- a **build pipeline** (framework build steps such as dependency installation and asset compilation)
- a **deployment model** (how releases are deployed and activated)
- **activation / rollback phases** (framework commands executed on release activate and rollback)
- **verification checks** (framework file/structure checks run when an artifact is installed)
- **configuration keys** (framework-specific settings under the `framework.<framework>.` namespace)

Standards are **standalone executables** implementing the standard command contract — the subprocess JSON contract, part of the [delivery lifecycle specification](../docs/specification-corpus/command-contract.md). Anvil resolves a project's declared framework to an installed standard through the installed-standard records; discovery is registry-driven (`anvil standard list`/`inspect`), never PATH-scanning (the v1.x closed-set discovery was removed at the switch-over gate, TS-017-02-02). The Core itself stays framework-agnostic. (v1.x term: *framework adapter*.)

> **Vocabulary (v1.x→v2 mapping):** the `anvil adapter ...` command names in v1.x documentation are the **deprecated v1.x command surface** of the canonical `standard` commands (ADR-032): retained as aliases until the window closes (ADR-028). `adapter list`/`inspect`/`install` map to the corresponding `standard` commands — they are not functional aliases (`adapter list` was the v1.x closed-set PATH discovery; `standard list` is the registry-driven index surface) — `adapter use` maps to `anvil init --framework <name>`, and `adapter uninstall` has no standard-named replacement. The complete mapping: [glossary — v1.x → v2 term mapping](glossary.md). Migration paths for existing users and CI workflows: [docs/migration-guide-v2.md](../docs/migration-guide-v2.md).

## Supported frameworks

The table lists the first-party delivery lifecycle standards (ADR-021/ADR-025) and where their binaries are distributed from. Since the repository split (ADR-025 §6.2), standard binaries are built and released from their own standard repositories — Core releases ship runtime artifacts only (ADR-025 §3.5, §4.7; TS-016-03-03). The set is **not closed** — the registry is an open, validated distribution path (ADR-023, ADR-030): first-party standards are adoptable today, and community-standard contribution is gated on the review 20 §2.4 outcomes (ADR-034 — preparation only, not activated). The executable keeps the v1.x resolution name `anvil-adapter-<name>` (ADR-025 decision 4).

| Framework | Standard status | Deployment model | Documentation |
|---|---|---|---|
| **Laravel** | Implemented | `server` (activate in place on a server) | [anvil-standard-laravel repository](https://github.com/maleolabs/anvil-standard-laravel) — Laravel lifecycle documentation moved out of Core (TS-016-01-01, ADR-025 §6.2) |
| **Flutter** | Implemented | `hybrid` (build + package for distribution) | [anvil-standard-flutter repository](https://github.com/maleolabs/anvil-standard-flutter) — Flutter lifecycle documentation moved out of Core (TS-016-02-01, ADR-025 §6.2) |

> **Flutter status:** `anvil init --framework flutter` works and generates a platform-aware build pipeline template (web, apk, ios targets with `metadata.platforms` / `metadata.target`). The Flutter lifecycle content — including the standard's own documentation — lives in its repository, `maleolabs/anvil-standard-flutter` (the Flutter delivery lifecycle standard, ADR-021); the Core contains no Flutter framework knowledge (ADR-025 §6.1, §6.2). The v1.x plan for a usage wiki at `adapters/flutter/` is historical: usage documentation for a standard is owned by the standard's repository.

> **Laravel status:** the Laravel lifecycle content — including its usage documentation — lives in its own repository, `maleolabs/anvil-standard-laravel` (the Laravel delivery lifecycle standard, ADR-021). The Core contains no Laravel framework knowledge (ADR-025 §6.1, §6.2). The Laravel executable is built and released from that repository.

## Adding a new framework (standard-only, no Core release)

Framework knowledge lives in delivery lifecycle standards (ADR-020, ADR-021): pipeline templates, build phases, and discovery. Adding a framework means **authoring a delivery lifecycle standard** — no Core release is required for the framework to be discoverable, generate its init template, or run server release builds:

1. **Implement the standard command contract** ([delivery lifecycle specification — command contract](../docs/specification-corpus/command-contract.md); the v1.x formulation is [005-adapter-command-contract](../docs/architecture/005-adapter-command-contract.md), historical) as an executable resolving as `anvil-adapter-<name>` (ADR-025 decision 4):
   - `capabilities` — the deployment model and declared build phases (required for discovery; the probe that validates the standard)
   - `template` — the pipeline definitions (build + ci) the Core writes to `.anvil/pipelines/` on `anvil init --framework <name>`
   - `build` — the build phases executed by `anvil server release build` (with `--target`/`--strict` support)
   - activation / verify / manifest commands as the deployment model requires
2. **Adopt the standard through the registry** — `anvil standard install <id> <version>` (full trust validation, ADR-022), or `install-bundle` for offline installs. The standard then appears in `anvil standard list`/`inspect`, is accepted by `anvil init --framework <name>`, and serves server release builds — the subprocess contract was verified end-to-end for a third-party executable in ST-007-010 (v1.x PATH-discovery era; the contract is unchanged in v2).
3. **Remaining Core-embedded piece** (documented in [limitations](limitations.md) item 8): local `anvil pipeline build` still runs the generated YAML through the generic engine instead of dispatching to the standard executable (deferred dispatch, ADR-020 §3). Framework declarations on `anvil init --framework <name>` require the installed delivery lifecycle standard (standard-missing HARD-FAIL, TS-015-02-02) — framework resolution, config extension, and template generation are standard-driven (TS-015-02, TS-015-03; the standard's `template` command remains the interim generation path while the standard declares no template content, see [limitations](limitations.md) item 8).

## Quick links

- [Laravel standard repository](https://github.com/maleolabs/anvil-standard-laravel) (Laravel lifecycle documentation moved out of Core)
- [Delivery lifecycle specification corpus](../docs/specification-corpus/)
- [Delivery Lifecycle Standard Authoring Guide](../docs/authoring-guide/) (how to create, validate, publish, and maintain delivery lifecycle standards)
- [Limitations and deferrals](limitations.md)
- [Glossary: v2 vocabulary and the v1.x→v2 term mapping](glossary.md)

## CI/CD Integration

Anvil is a runtime orchestrator, not a CI platform. These guides show how to integrate Anvil into your existing CI/CD pipelines for build, package, and deploy workflows.

| Platform | Documentation |
|---|---|
| **GitHub Actions** | [ci-cd/github-actions.md](ci-cd/github-actions.md) |
| **GitLab CI** | [ci-cd/gitlab-ci.md](ci-cd/gitlab-ci.md) |
| **Jenkins** | [ci-cd/jenkins.md](ci-cd/jenkins.md) |

See [CI/CD overview](ci-cd/README.md) for the responsibility boundary, common commands, and workflow diagram.

## Documentation map

| Page | Contents |
|---|---|
| [Laravel standard repository](https://github.com/maleolabs/anvil-standard-laravel) | Laravel quick start, init, build, deploy, verify, manifest, config guides |
| [Delivery lifecycle specification corpus](../docs/specification-corpus/) | Lifecycle model, contracts, and vocabulary of the delivery lifecycle specification |
| [limitations.md](limitations.md) | Everything that is not implemented yet |

## Accuracy

Every command, flag, file, and behavior in this wiki was verified against the current source code. Anything that is recognized but **not yet effective** is called out explicitly and consolidated in [limitations.md](limitations.md). If you find a discrepancy, it is a bug in this documentation — please flag it.
