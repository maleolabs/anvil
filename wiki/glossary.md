# Glossary

User-facing terms used across the Anvil documentation.

> **Domain alignment (2026-08-03):** Anvil belongs to the **Software Delivery** domain, not the Engineering domain. Knowledge OS owns Engineering; Anvil owns Software Delivery. Historical terminology mapping: see [25-domain-language-alignment](../docs/reviews/25-domain-language-alignment.md).

## Engineering

The domain owned by **Knowledge OS**: planning, specification, architecture, design, implementation, collaboration, and AI-agent orchestration. Engineering is intentionally **outside** Anvil's scope.

Note: "engineering team" / "engineering work" refer to the *people and activities* that practice and operate delivery; they are not a claim that Anvil participates in the Engineering domain.

## Software Delivery

The domain owned by **Anvil**: build, package, versioning, artifact management, release, deployment, activation, rollback, delivery verification, and delivery lifecycle standardization. Software delivery begins **after software implementation has been completed**.

```text
Knowledge OS → Engineering → Delivery Intent → Anvil → Software Delivery → Infrastructure
```

## Delivery Lifecycle

The lifecycle Anvil defines, encodes, enforces, and observes: package → verify → install → activate → rollback → recover, with verification gates, atomic activation, one-Active state, and forward rollback. (Historical term: "engineering lifecycle".)

## Engineering Lifecycle

Historical term (v1.x and the review corpus) for the lifecycle Anvil owns. Superseded by **Delivery Lifecycle**; retained in historical documents and the historical terminology mapping only.

## Delivery Runtime

The executable engine of Anvil (the Core): implements and enforces the Delivery Lifecycle Specification, executes Delivery Lifecycle Standards as standalone executables, and operates the delivery lifecycle domains (Project → Artifact → Deployment → Server Runtime). Also called **Anvil Runtime (Core)**. Not to be confused with the **Server Runtime Domain**, one of the four bounded contexts inside the runtime. (Historical term: "engineering runtime".)

## Software Delivery Platform

The product-level positioning of Anvil: a software delivery standard that defines, distributes, verifies, and enforces how software is built, released, and deployed consistently across projects. "Software Delivery Platform" is used for organization-facing descriptions; "Delivery Runtime" for the engine. (Historical terms: "engineering platform", "engineering lifecycle platform".)

## Delivery Lifecycle Standard

The distributable unit of framework delivery knowledge: what a legal delivery lifecycle *contains* for one framework — phases, verification, configuration surface, rollback semantics, templates — packaged as seven parts (Manifest, Lifecycle Definition, Verification, Templates, Compatibility, Documentation, Tests), independently versioned, governed, and distributed through the registry. (Historical term: "engineering lifecycle standard" / "engineering standard"; "adapter" in v1.x.)

## Delivery Lifecycle Specification

The published, versioned authority defining what a legal delivery lifecycle *is*: lifecycle model, artifact manifest contract, standard command contract, verification contract, and vocabulary (docs + JSON Schema), authored in the Core repository. (Historical term: "engineering lifecycle specification".)

## Knowledge OS

The complementary product that owns the **Engineering** domain. Knowledge OS produces **delivery intent**; Anvil executes delivery. Anvil's value does not depend on Knowledge OS, and Anvil must be chosen by it on merit.

## Anvil

The software delivery standard: the system of Delivery Lifecycle Specification and Delivery Runtime, plus the Delivery Lifecycle Standards it distributes. Anvil is not a CI platform, deployment platform, infrastructure platform, or container platform; it owns the delivery convention layer.

---

## Framework

The application framework a project is built with (e.g. **Laravel**, **Flutter**). In Anvil, a project records its framework in `anvil.yaml` (`project.framework`) and on the server in the project registry. Anvil itself is framework-agnostic — the framework only selects behavior provided by a Delivery Lifecycle Standard.

## Adapter (v1.x term)

Historical term for the component that provided framework-specific behavior to the v1.x Core. Superseded by **Delivery Lifecycle Standard** in v2. Adapters are recognized, mapped, and migrated to standards during the v2 transition ([migration-guide-v2](../docs/migration-guide-v2.md)).

## Adapter executable (v1.x term)

The standalone binary implementing the v1.x adapter command contract (`anvil-adapter-<framework>`). The executable-resolution contract is preserved in v2 ([ADR-025](../docs/adr/ADR-025-repository-split-core-vs-standards.md)): a standard's executable keeps the v1.x resolution name `anvil-adapter-<name>` (ADR-025 decision 4). The v2 term is **delivery lifecycle standard** — see the v1.x → v2 term mapping below.

## v1.x → v2 term mapping

The authoritative concept mapping is [Transition Plan §5.9](../docs/planning/ANVIL_V2_TRANSITION_PLAN.md), governed by [ADR-021 §3.6](../docs/adr/ADR-021-delivery-lifecycle-standard-model.md), and documented in depth by [006-g — v1.x→v2 concept mapping](../docs/architecture/006-g-v1x-to-v2-concept-mapping.md). **Usage rule:** "adapter" appears in this glossary and across the corpus only in the v1.x→v2 migration context (006-g §3).

### Concept mapping (Transition Plan §5.9)

| v1.x term | v2 term | Meaning preserved |
|---|---|---|
| Framework adapter | delivery lifecycle standard | Standalone executable implementing the command contract |
| Convention pack | Standard content | The distributable framework lifecycle knowledge |
| Adapter command contract | Standard command contract (spec) | The subprocess JSON contract; unchanged in semantics |
| Adapter capability declaration | Standard capability declaration | Gains contract-version and framework-version fields |
| Adapter registry (closed set) | Standard registry | Open, validated, registry-driven distribution |

### CLI and configuration surface mapping (ADR-032; migration-guide-v2 §5.3)

| v1.x surface | v2 surface | Notes |
|---|---|---|
| `anvil adapter list` | `anvil standard list` | v1.x: closed-set PATH discovery; v2: registry-driven index surface |
| `anvil adapter inspect <name>` | `anvil standard inspect <id> [version]` | Registry-driven inspection |
| `anvil adapter install <name>` | `anvil standard install <id> <version>` | Same validation and trust gates (ADR-022) |
| `anvil adapter use <name>` | `anvil init --framework <name>` | Framework declaration is standard-driven (ADR-026); there is no `use`-named command in the v2 surface |
| `anvil adapter uninstall <name>` | — (no standard-named replacement) | v1.x binary-removal surface, retained; `standard uninstall` is not part of the v2 surface (ADR-032) |
| `project.adapter` (config key) | `project.standard` | Canonical key (TS-019-02-01); the legacy key is read as a deprecated alias with a warning; declaring both is rejected (ADR-032 §3) |

Migration paths: [docs/migration-guide-v2.md](../docs/migration-guide-v2.md).

## Deployment model

Declared by the standard in its capability declaration. Three models exist (ADR-016):

| Model | Meaning |
|---|---|
| **`server`** | Releases deploy to a server and are **activated in place** — the release directory becomes the live application. Used by the Laravel standard. |
| **`hybrid`** | Build and package the release for distribution *outside* the server. Used by the Flutter standard. |
| **`package`** | Build and distribute without server-side activation; reserved for future use. |

## Capability declaration

The contract a standard publishes (via its `capabilities` command): the deployment model, the activation phases it supports, the build phases, and the verification checks it provides. The runtime invokes **only** declared capabilities — anything not declared is not called.

## Activation phase

One unit of work executed when a release is activated. For Laravel: `migrate` (`php artisan migrate --force`), `config_cache`, `route_cache`, `event_cache`. A phase may be **reversible** (has a rollback command) or **irreversible** (rollback reports an informational result and does not block).

## Verification check

One file/structure or lifecycle-conformity assertion a standard runs against an artifact before it is installed on a server (structural checks and, in v2, lifecycle-conformity checks — [ADR-033](../docs/adr/ADR-033-verification-standard-generalization.md)).

## Artifact manifest

The embedded `manifest.json` inside every packaged artifact — the authoritative contract for the artifact (identity, version, checksum). The manifest contract is part of the Delivery Lifecycle Specification.

## Manifest commands

The full command strings stored in the artifact manifest (`activation_commands`, `rollback_commands`) that an orchestrator — Anvil or an external runner — executes during release activation/rollback. They are the *metadata* form of the standard's phase table.

## Config extension

The standard-declared configuration keys under the `framework.<framework>.` namespace (e.g. `framework.laravel.*`). The runtime enforces namespace isolation; the standard owns value validation.

## Release lifecycle (server model)

| Stage | Meaning |
|---|---|
| **Ready** | Artifact installed, verified, available for activation |
| **Active** | Release is live and serving traffic |
| **Rolled Back** | Release superseded by a rollback to a previous release |
| **Cleaned Up** | Release artifacts removed from disk |

Back to [Wiki](README.md) · [Limitations](limitations.md)
