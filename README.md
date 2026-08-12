# Maleo Anvil

**The software delivery standard.**

Anvil defines, distributes, verifies, and enforces how software is built, released, and deployed consistently across projects — the release lifecycle: the sequence of operations that turns source code into a running, verified, replaceable release — for any framework, on any server, invoked by any CI platform.

> **Read the philosophy first:** [The Anvil Manifesto](docs/manifesto/ANVIL_MANIFESTO.md) is the authoritative statement of what Anvil is and the delivery domain it owns. This README is the gateway; the manifesto is the source of truth.

---

## Why Anvil exists

Every engineering team recreates the same delivery lifecycle for every project:

- package the application into a deployable object
- verify its integrity before it is trusted
- install it on a target environment
- activate it without downtime
- roll it back when something goes wrong
- know, at any moment, what is active and what can be restored

This lifecycle is repeated per project, per framework, per environment — as scripts, CI YAML, and tribal knowledge. The structure is always the same; only the details differ.

Anvil exists to make the lifecycle a **shared, enforced object** instead of a per-project re-implementation. Projects that use the same framework share the same lifecycle convention automatically.

## The problem, in three layers

| Layer | What it is | Who owns it |
|---|---|---|
| **Execution** | Running commands at the right time, on the right runner | CI platforms (GitHub Actions, GitLab CI, Jenkins, …) |
| **Mechanics** | Moving and activating software on a target: copy, symlink switch, restart | Deployment tools (Deployer, Kamal, Capistrano, …) |
| **Convention** | What a *legal* lifecycle is: order, verification gates, state, rollback semantics, framework-specific steps | **Anvil** |

CI platforms execute whatever YAML you write — they define no lifecycle. Deployment tools encode conventions for one ecosystem (PHP, containers). Kubernetes defines conventions — for containers only. The non-container, cross-framework, CI-agnostic lifecycle convention layer belongs to Anvil. The Convention layer is exactly what Anvil productizes — as the three artifacts below.

## Core philosophy

- **Convention before implementation** — the lifecycle is defined before it is implemented, per framework.
- **Verification before trust** — verification is an unskippable lifecycle gate, not an optional command.
- **State before assumptions** — lifecycle facts (what is active, what can roll back) are persisted, queryable state; transitions are validated against a defined graph.
- **Identity from content** — an artifact's identity comes from its embedded manifest, never from its filename or path.
- **Core owns contracts, standards own knowledge** — the Core defines the shape of the lifecycle; delivery lifecycle standards supply the content per framework.
- **Opinionated where necessary, minimal where possible** — enforce the invariants that prevent production failures; nothing more.
- **CI-agnostic** — the lifecycle does not depend on which CI platform triggers it.

## The three-layer model

Anvil is the standard — the system of specification, runtime, and standards ([Transition Plan §5.1](docs/planning/ANVIL_V2_TRANSITION_PLAN.md), [ADR-021](docs/adr/ADR-021-delivery-lifecycle-standard-model.md)):

```text
Delivery Lifecycle Specification      ← what a legal lifecycle IS (the authority)
        │  implemented by
        ▼
Anvil Runtime (Core)                    ← the engine: enforces the specification,
        │  executes                        invokes the standards
        ▼
delivery lifecycle standards         ← framework lifecycle content for one
        (anvil-standard-laravel,          framework (Laravel, Flutter, ...)
         anvil-standard-flutter)
```

| Layer | What it is | Owned by |
|---|---|---|
| **Delivery Lifecycle Specification** | The authority every other layer conforms to: lifecycle model, manifest contract, standard command contract, verification contract, vocabulary (docs + JSON Schema), authored and versioned in the Core repository | Core governance (ADR process) |
| **Anvil Runtime (Core)** | The engine: implements and enforces the specification; executes standards as standalone executables through the standard command contract | Core team |
| **delivery lifecycle standards** | The distributable unit of framework lifecycle knowledge: what a legal lifecycle *contains* for one framework — phases, verification, configuration surface, rollback semantics, templates — packaged in seven parts (Manifest, Lifecycle Definition, Verification, Templates, Compatibility, Documentation, Tests), independently versioned | Standard maintainers |

**The standard registry** distributes the standards: discovery, adoption with trust validation, installation, and update — an open, validated, registry-driven path that replaces the v1.x closed-set adapter discovery ([Transition Plan §5.9](docs/planning/ANVIL_V2_TRANSITION_PLAN.md), [ADR-023](docs/adr/ADR-023-delivery-lifecycle-standard-registry.md), [ADR-030](docs/adr/ADR-030-registry-distribution-channel.md)).

## Architecture at a glance

Anvil is organized as four bounded domains with one-way dependency:

```text
Project → Artifact → Deployment → Server Runtime
```

- **Project** — repository-aware development: initialization, build/pipeline execution, packaging preparation.
- **Artifact** — immutable deployment payloads; the embedded `manifest.json` is the canonical contract (identity, version, checksum).
- **Deployment** — delivery between producers and targets (e.g., SSH transport); never reads runtime internals.
- **Server Runtime** — lifecycle state: artifact installation, release stages, activation, rollback, health.

Anvil **owns** lifecycle convention, lifecycle state, lifecycle verification, lifecycle vocabulary, and lifecycle contracts. It intentionally does **not** own CI execution, deployment mechanics, infrastructure provisioning, monitoring, container orchestration, or framework implementation — see the [Manifesto §4](docs/manifesto/ANVIL_MANIFESTO.md).

## Relationship with existing tools

Anvil coexists with the ecosystem by design — it occupies the layer none of them own:

- **GitHub Actions / GitLab CI / Jenkins** execute pipelines; Anvil is invoked by them. Anvil does not schedule.
- **Deployer / Forge / Kamal / Capistrano** mechanize deployment; Anvil governs the lifecycle in which deployment is legal.
- **Kubernetes** owns lifecycle conventions for containers; Anvil owns the layer Kubernetes does not serve.
- **Ansible / Terraform** provision servers; Anvil manages lifecycle state, not server configuration.

## Why delivery lifecycle standards exist

Every framework has different lifecycle knowledge — what a build produces, which activation steps must run (migrations, cache warming, queue recycling), what verification can check, what rollback can reverse. Laravel and Flutter differ in content; they do not differ in the shape of the lifecycle.

Delivery lifecycle standards are the distribution mechanism for that knowledge. The Core defines the contracts (the specification); each standard (a standalone executable implementing the standard command contract) supplies the framework-specific content. **Adding a framework means authoring a delivery lifecycle standard — never a Core change.** Projects sharing a framework share the same lifecycle content by adopting the same standard.

**Supported frameworks:** Laravel (`server` model) · Flutter (`hybrid` model). Since the repository split (ADR-025), the framework lifecycle content lives in its own repositories — [maleolabs/anvil-standard-laravel](https://github.com/maleolabs/anvil-standard-laravel) and [maleolabs/anvil-standard-flutter](https://github.com/maleolabs/anvil-standard-flutter) — and is built and released from there (TS-016-01-01, TS-016-02-01); the Core contains no framework knowledge. See the [Wiki](wiki/README.md).

> **v1.x term:** framework *adapters* are the v1.x name for delivery lifecycle standards ([Transition Plan §5.9](docs/planning/ANVIL_V2_TRANSITION_PLAN.md)). The v2 CLI maps the `adapter` vocabulary to `standard` (ADR-032); migration paths: [v2 Migration Guide](docs/migration-guide-v2.md).

## Quick start

```bash
# Install the CLI (Linux/macOS)
curl -fsSL https://github.com/maleolabs/anvil/releases/latest/download/install.sh | sh

# Install standard executables at install time (from the STANDARD
# repositories' releases — the registry distribution channel; each binary
# is verified against the attestation-bound digest declared in the release's
# registry metadata)
curl -fsSL https://github.com/maleolabs/anvil/releases/latest/download/install.sh | sh -s -- --with-adapters laravel,flutter

# ── Registry prerequisites ─────────────────────────────────────────
# The registry adoption step below needs two things in place first:
#   1. A static registry index — resolved via --index, $ANVIL_REGISTRY_INDEX,
#      or the default <user config dir>/anvil/registry. The index is
#      published in the standard repositories' releases (the registry
#      distribution channel; setup: docs/migration-guide-v2.md §5.4.1).
#   2. Trust anchors — resolved via --trust-anchors, $ANVIL_TRUST_ANCHORS,
#      or the default <user config dir>/anvil/trust-anchors.json. Adoption
#      is fail-closed without them (ADR-022; precondition:
#      docs/migration-guide-v2.md §5.4).
# Note: `install.sh --with-adapters` installs standard executables but does
# NOT create installed-standard records — `anvil init --framework` requires
# the explicit registry adoption flow below first.
# ────────────────────────────────────────────────────────────────────

# Adopt a standard through the registry (full trust validation) and
# install its executable — the explicit registry flow. The v1.x "adapter"
# command names are deprecated and retained for the dual-run window;
# migration paths: the v2 migration guide (docs/migration-guide-v2.md)
anvil standard install anvil-standard-laravel 1.0.0
anvil adapter install laravel          # deprecated alias — resolves anvil-standard-laravel

# Initialize a project with a framework
anvil init my-app --framework laravel

# Build and package (Development context)
anvil pipeline build
anvil artifact package

# Verify the artifact's integrity
anvil artifact verify .anvil/artifacts/<artifact>.tar.gz

# Transfer the artifact to the target server — e.g. with scp, or with Anvil's
# own SSH transport: `anvil deployment upload <target> ./<artifact>.tar.gz`
# (documented below)
scp .anvil/artifacts/<artifact>.tar.gz user@server:~

# On the target server: initialize the runtime, register the project
# (`anvil server init` writes the runtime configuration under /etc/anvil —
# root required, or pass --server-root to place it elsewhere)
anvil server init
anvil server project register --project-id my-app --install-root /srv/apps/my-app --non-interactive

# Install the artifact as a release, then activate it
anvil server release install my-app ./<artifact>.tar.gz
anvil server release activate my-app <release-id>

# Rollback to the previous release when needed
anvil server release rollback my-app
```

**From CI:** `anvil deployment upload <target> ./<artifact>.tar.gz` delivers the artifact over SSH; credentials come from environment variables (`DEPLOY_SERVER_HOST`, `DEPLOY_SERVER_USER`, `DEPLOY_SSH_KEY`) — never from configuration. Upload is the **only SSH transport command**: it works on a fresh runner with env-only configuration and requires no local server state. See the [SSH transport documentation](wiki/ci-cd/README.md).

**Deployment vs server commands:** `anvil deployment install/activate/rollback/info` are local target-centric aliases of the server command surface (install/activate/rollback alias the `server release` operations; info aliases `server status` — all running on the local server runtime via the `ServerReleaseCoordinator`); `anvil deployment upload` is the only command that leaves the machine. The local target-centric commands take no target argument — the operation runs on the local server runtime (the phantom `<target-id>` argument was removed at the end of the announced deprecation window). Choose one style per workflow.

**Standard management:** `anvil standard list` · `anvil standard inspect <id> [version]` · `anvil standard install <id> <version>` · `anvil standard update <id> <version>` · `anvil standard install-bundle <bundle-path>` — the canonical surface for discovering, adopting, and updating delivery lifecycle standards (registry-driven, trust-validated). Since the installer/discovery switch-over, standard executables resolve from the standard repositories' releases through the registry — never from Core releases (ADR-025 §3.5, ADR-030). The v1.x `adapter` command names are deprecated: `adapter list`/`inspect`/`install` mirror the corresponding `standard` commands; `adapter use` maps to `anvil init --framework <name>` (there is no `use`-named command in the v2 surface); `adapter uninstall` has no standard-named replacement. The aliases remain registered until the window closes (ADR-032, ADR-028); migrate per the [v2 Migration Guide](docs/migration-guide-v2.md).

Full command reference: [Command Reference](wiki/) and `anvil --help`.

## Current project status

Anvil is under active development as an open-source project by [Maleo Labs](https://github.com/maleolabs).

- **Version:** 1.5.0 — release lifecycle engine, artifact system, pipeline engine, Laravel and Flutter delivery lifecycle standards, SSH transport. The v1.x line is in bugfix-only maintenance; the v2 identity — the three-layer model and the standard registry above — is the current product direction ([Transition Plan §4.7](docs/planning/ANVIL_V2_TRANSITION_PLAN.md)).
- The core lifecycle (install → activate → rollback) was hardened in the MVP-002-S7 release integrity sprint; release gate (ST-INT-01..03) executed with all checks passing.
- Upgrade path from v1.4.x: [Migration Guide](docs/migration-guide-v1.5.md). The legacy `runtime` command group was **removed** at the end of its announced deprecation window (ADR-032 D12) — the `server` group is the sole Server Runtime surface. v1.5.x→v2 migration (including the adapter → standard alias removal schedule): [v2 Migration Guide](docs/migration-guide-v2.md).

## Documentation

- **[The Anvil Manifesto](docs/manifesto/ANVIL_MANIFESTO.md)** — the authoritative philosophy and domain definition
- **[Architecture & ADRs](docs/architecture/)** — four-domain model, decisions, and rationale
- **[Delivery Lifecycle Specification](docs/specification-corpus/)** — the published specification corpus: lifecycle model, contracts, vocabulary
- **[Delivery Lifecycle Standard Authoring Guide](docs/authoring-guide/)** — how to create, validate, publish, and maintain delivery lifecycle standards
- **[Wiki](wiki/)** — usage guides, delivery lifecycle standards, CI/CD integration, glossary
- **[Reviews](docs/reviews/)** — architecture, planning, and product reviews

## License

Apache 2.0 — see [LICENSE](LICENSE).
