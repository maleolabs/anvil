# Glossary

User-facing terms used across the adapters documentation.

## Framework

The application framework a project is built with (e.g. **Laravel**, **Flutter**). In Anvil, a project records its framework in `anvil.yaml` (`project.framework`) and on the server in the project registry (`adapter` field). Anvil itself is framework-agnostic — the framework only selects behavior provided by an adapter.

## Adapter

The component that provides framework-specific behavior for the Core: build pipeline, deployment model, activation/rollback phases, verification checks, and configuration keys. Adapters are **standalone executables** (see below). Anvil ships one adapter per framework; adding a framework means adding an adapter, not changing the Core.

## Adapter executable

The standalone binary that implements the adapter command contract. Anvil invokes it as `<adapter-executable> <command> <json-payload>` and reads a JSON result from stdout. The binary name convention is **`anvil-adapter-<framework>`** — e.g. `anvil-adapter-laravel` — resolved on `PATH` (via `exec.LookPath`). Supported commands: `capabilities`, `build`, `activate`, `verify`, `extension`, `validate`.

The adapter is *stateless*: all state (releases, caches) lives on the Core side; the adapter only executes commands and reports results.

## Deployment model

Declared by the adapter in its capability declaration. Three models exist (ADR-016):

| Model | Meaning |
|---|---|
| **`server`** | Releases deploy to a server and are **activated in place** — the release directory becomes the live application. Used by the Laravel adapter. |
| **`hybrid`** | Build and package the release for distribution *outside* the server (planned for Flutter). |
| **`package`** | Build and distribute without server-side activation; reserved for future use. |

## Capability declaration

The contract an adapter publishes (via its `capabilities` command): the deployment model, the activation phases it supports, the build phases, and the verification checks it provides. The Core invokes **only** declared capabilities — anything not declared is not called.

## Activation phase

One unit of work executed when a release is activated. For Laravel: `migrate` (`php artisan migrate --force`), `config_cache`, `route_cache`, `event_cache`. A phase may be **reversible** (has a rollback command) or **irreversible** (rollback reports an informational result and does not block). See [deploy.md](adapters/laravel/deploy.md).

## Verification check

One file/structure assertion an adapter runs against an artifact before it is installed on a server. The Laravel adapter declares 8 checks (`vendor_present`, `bootstrap_structure`, `config_files`, `artisan_file`, `composer_json`, `env_file`, `app_directory`, `routes_directory`). See [verify.md](adapters/laravel/verify.md).

## Artifact manifest

The embedded `manifest.json` inside every packaged artifact — the authoritative contract for the artifact (identity, version, checksum). Per ADR-017 it can also carry **activation_commands** / **rollback_commands** metadata. See [manifest.md](adapters/laravel/manifest.md).

## Manifest commands

The full command strings stored in the artifact manifest (`activation_commands`, `rollback_commands`) that an orchestrator — Anvil or an external runner — executes during release activation/rollback. They are the *metadata* form of the adapter's phase table. See [manifest.md](adapters/laravel/manifest.md).

## Config extension

The adapter-declared configuration keys under the `framework.<framework>.` namespace (e.g. `framework.laravel.*`). The Core enforces namespace isolation; the adapter owns value validation. See [config.md](adapters/laravel/config.md).

## Release lifecycle (server model)

| Stage | Meaning |
|---|---|
| **Ready** | Artifact installed, verified, available for activation |
| **Active** | Release is live and serving traffic |
| **Rolled Back** | Release superseded by a rollback to a previous release |
| **Cleaned Up** | Release artifacts removed from disk |

Back to [Adapters Wiki](README.md) · [Limitations](limitations.md)
