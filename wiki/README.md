# Anvil Adapters Wiki

This wiki is the **usage documentation** for Anvil framework adapters: what they are, which frameworks are supported, and how to use them day-to-day. Project/architecture documentation lives in [`docs/`](../docs/) and is deliberately kept separate.

## What are framework adapters?

An adapter gives Anvil framework-specific behavior on top of the generic Core:

- a **build pipeline** (framework build steps such as dependency installation and asset compilation)
- a **deployment model** (how releases are deployed and activated)
- **activation / rollback phases** (framework commands executed on release activate and rollback)
- **verification checks** (framework file/structure checks run when an artifact is installed)
- **configuration keys** (framework-specific settings under the `framework.<framework>.` namespace)

Adapters are **standalone executables**. Anvil discovers them on `PATH` when a project selects one (see [glossary](glossary.md)). The Core itself stays framework-agnostic.

## Supported frameworks

The table lists the adapters that ship with Anvil releases. It is **not a closed set** — any executable named `anvil-adapter-<name>` that answers the `capabilities` command appears in `anvil adapter list` and is usable, even if it does not ship with Anvil (ADR-020, TS-007-039).

| Framework | Adapter status | Deployment model | Documentation |
|---|---|---|---|
| **Laravel** | Implemented | `server` (activate in place on a server) | [Laravel Adapter Usage Guide](adapters/laravel/) |
| **Flutter** | Implemented | `hybrid` (build + package for distribution) | — (wiki pending) |

> **Flutter status:** `anvil init --framework flutter` works and generates a platform-aware build pipeline template (web, apk, ios targets with `metadata.platforms` / `metadata.target`). The Flutter adapter usage wiki is pending and will live at `adapters/flutter/`.

## Adding a new framework (adapter-only, no Core release)

Framework knowledge lives in adapter binaries (ADR-020): pipeline templates, build phases, and discovery. Adding a framework means **writing an adapter binary** — no Core release is required for the framework to be discoverable, generate its init template, or run server release builds:

1. **Implement the adapter command contract** (`docs/architecture/005-adapter-command-contract.md`) as an executable named `anvil-adapter-<name>`:
   - `capabilities` — the deployment model and declared build phases (required for discovery; the probe that validates the adapter)
   - `template` — the pipeline definitions (build + ci) the Core writes to `.anvil/pipelines/` on `anvil init --framework <name>` / `anvil adapter use <name>`
   - `build` — the build phases executed by `anvil server release build` (with `--target`/`--strict` support)
   - activation / verify / manifest commands as the deployment model requires
2. **Put the binary on PATH** (or install it next to the CLI). It then appears in `anvil adapter list`, is accepted by `anvil adapter use`, and serves server release builds — verified end-to-end for a third-party `anvil-adapter-rails` in ST-007-010.
3. **Remaining Core-embedded pieces** (documented in [limitations](limitations.md) item 8): `anvil init --framework <name>` still validates the name against the built-in whitelist (`laravel`, `flutter`), so a brand-new framework cannot be passed to `init` until the config layer migrates; local `anvil pipeline build` still runs the generated YAML through the generic engine (deferred dispatch, ADR-020 §3).

## Quick links

- [Laravel adapter — usage overview](adapters/laravel/)
- [Limitations and deferrals](limitations.md)
- [Glossary: framework vs adapter vs deployment model](glossary.md)

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
| [Laravel README](adapters/laravel/README.md) | Quick start path for Laravel projects |
| [init.md](adapters/laravel/init.md) | `anvil init --framework laravel` — generated files, framework selection errors |
| [build.md](adapters/laravel/build.md) | Build pipeline template, `--env` behavior, `environments:` overrides |
| [deploy.md](adapters/laravel/deploy.md) | Server deployment model: register, install, activate, rollback |
| [verify.md](adapters/laravel/verify.md) | The 8 verification checks and where they run |
| [manifest.md](adapters/laravel/manifest.md) | Activation/rollback commands stored in the artifact manifest |
| [config.md](adapters/laravel/config.md) | `framework.laravel.*` configuration keys |
| [limitations.md](limitations.md) | Everything that is not implemented yet |

## Accuracy

Every command, flag, file, and behavior in this wiki was verified against the current source code. Anything that is recognized but **not yet effective** is called out explicitly and consolidated in [limitations.md](limitations.md). If you find a discrepancy, it is a bug in this documentation — please flag it.
