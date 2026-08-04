# Maleo Anvil

**The engineering lifecycle convention engine.**

Anvil defines, encodes, enforces, and observes the release lifecycle — the sequence of operations that turns source code into a running, verified, replaceable release — for any framework, on any server, invoked by any CI platform.

> **Read the philosophy first:** [The Anvil Manifesto](docs/manifesto/ANVIL_MANIFESTO.md) is the authoritative statement of what Anvil is and the engineering domain it owns. This README is the gateway; the manifesto is the source of truth.

---

## Why Anvil exists

Every engineering team recreates the same engineering lifecycle for every project:

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

CI platforms execute whatever YAML you write — they define no lifecycle. Deployment tools encode conventions for one ecosystem (PHP, containers). Kubernetes defines conventions — for containers only. The non-container, cross-framework, CI-agnostic lifecycle convention layer belongs to Anvil.

## Core philosophy

- **Convention before implementation** — the lifecycle is defined before it is implemented, per framework.
- **Verification before trust** — verification is an unskippable lifecycle gate, not an optional command.
- **State before assumptions** — lifecycle facts (what is active, what can roll back) are persisted, queryable state; transitions are validated against a defined graph.
- **Identity from content** — an artifact's identity comes from its embedded manifest, never from its filename or path.
- **Core owns contracts, adapters own knowledge** — the Core defines the shape of the lifecycle; adapters supply the content per framework.
- **Opinionated where necessary, minimal where possible** — enforce the invariants that prevent production failures; nothing more.
- **CI-agnostic** — the lifecycle does not depend on which CI platform triggers it.

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

## Why framework adapters exist

Every framework has different lifecycle knowledge — what a build produces, which activation steps must run (migrations, cache warming, queue recycling), what verification can check, what rollback can reverse. Laravel and Flutter differ in content; they do not differ in the shape of the lifecycle.

Adapters are the distribution mechanism for that knowledge. The Core defines the contracts; each adapter (a standalone executable) supplies the framework-specific content. **Adding a framework means adding an adapter — never a Core change.** Projects sharing a framework share the same lifecycle content by adopting the same adapter.

**Supported frameworks:** Laravel (`server` model) · Flutter (`hybrid` model). Guides: [Adapters Wiki](wiki/adapters/laravel/).

## Quick start

```bash
# Install the CLI (Linux/macOS)
curl -fsSL https://github.com/maleolabs/anvil/releases/latest/download/install.sh | sh

# Initialize a project with a framework
anvil init my-app --framework laravel

# Build and package (Development context)
anvil pipeline build
anvil artifact package

# Verify the artifact's integrity
anvil artifact verify .anvil/artifacts/<artifact>.tar.gz

# On the target server: initialize the runtime, register the project
anvil server init
anvil server project register --project-id my-app --install-root /srv/apps/my-app --non-interactive

# Install the artifact as a release, then activate it
anvil server release install my-app ./<artifact>.tar.gz
anvil server release activate my-app <release-id>

# Rollback to the previous release when needed
anvil server release rollback my-app
```

**From CI:** `anvil deployment upload <target> ./<artifact>.tar.gz` delivers the artifact over SSH; credentials come from environment variables (`DEPLOY_SERVER_HOST`, `DEPLOY_SERVER_USER`, `DEPLOY_SSH_KEY`) — never from configuration. Upload is the **only SSH transport command**: it works on a fresh runner with env-only configuration and requires no local server state. See the [SSH transport documentation](wiki/ci-cd/README.md).

**Deployment vs server commands:** `anvil deployment install/activate/rollback/info` are local target-centric aliases of the server command surface (install/activate/rollback alias the `server release` operations; info aliases `server status` — all running on the local server runtime via the `ServerReleaseCoordinator`); `anvil deployment upload` is the only command that leaves the machine. The `<target-id>` argument is a correlation label echoed in output/JSON — it does not select a target. Choose one style per workflow.

**Adapter management:** `anvil adapter list` · `anvil adapter install <name>` · `anvil adapter use <name>`.

Full command reference: [Command Reference](wiki/) and `anvil --help`.

## Current project status

Anvil is under active development as an open-source project by [Maleo Labs](https://github.com/maleolabs).

- **Version:** 1.5.0 — release lifecycle engine, artifact system, pipeline engine, Laravel and Flutter adapters, SSH transport.
- The core lifecycle (install → activate → rollback) was hardened in the MVP-002-S7 release integrity sprint; release gate (ST-INT-01..03) executed with all checks passing.
- Upgrade path from v1.4.x: [Migration Guide](docs/migration-guide-v1.5.md). The legacy `runtime` command group is deprecated in favor of the canonical `server` surface (ADR-021).

## Documentation

- **[The Anvil Manifesto](docs/manifesto/ANVIL_MANIFESTO.md)** — the authoritative philosophy and domain definition
- **[Architecture & ADRs](docs/architecture/)** — four-domain model, decisions, and rationale
- **[Wiki](wiki/)** — usage guides, adapters, CI/CD integration, glossary
- **[Reviews](docs/reviews/)** — architecture, planning, and product reviews

## License

Apache 2.0 — see [LICENSE](LICENSE).
