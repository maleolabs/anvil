# Maleo Anvil

**Artifact, Deployment & Server Runtime Toolkit**

Anvil is a standalone, framework-agnostic CLI toolkit for packaging and verifying immutable Artifacts, orchestrating delivery, and managing Releases on a Server Runtime.

Anvil separates repository-aware development from server-side Runtime management. Development commands work with project source and `anvil.yaml`; Server Runtime commands work with registered projects and Runtime configuration under `/etc/anvil`.

---

## Table of Contents

- [Problem](#problem)
- [Solution](#solution)
- [Key Capabilities](#key-capabilities)
- [Architecture](#architecture)
- [Quick Start](#quick-start)
- [Runtime Model](#runtime-model)
- [Project Status](#project-status)
- [License](#license)

---

## Problem

Every software project that deploys to a server faces the same set of challenges:

- **Ad-hoc release machinery** — deployment logic embedded in CI workflow YAML, project-specific shell scripts, or framework-bound tooling that cannot be reused across projects
- **No standard vocabulary** — inconsistent terminology for build, package, deploy, activate, and rollback across teams and projects
- **Framework lock-in** — deployment tooling tied to specific frameworks (Laravel, Rails, Django, etc.) that breaks when teams switch technology stacks
- **Untested rollbacks** — rollback procedures that are undocumented, poorly tested, and stressful to execute under pressure
- **Integrity gaps** — no standardized mechanism to verify artifact integrity before deployment

These problems are not unique to any team, framework, or infrastructure. They are universal. And they are solved from scratch every time a new project starts.

---

## Solution

Anvil occupies the gap between application projects and deployment platforms. It is not an application framework, a CI platform, or a server configuration manager. It provides the contracts that connect them.

**Positioned between projects and platforms** — Anvil receives Artifacts from development or CI workflows and delivers them to Runtime environments. Deployment orchestration is a separate context; Server Runtime owns installation and Release lifecycle execution.

**Positioned between frameworks** — Anvil treats all applications the same: a set of files to package, verify, and activate. Framework-specific behavior is handled by adapters, not by the core.

**Positioned between environments** — Anvil uses the same commands and configuration schema regardless of whether the target is a developer laptop, a staging server, or a production environment.

The result is a single binary with no runtime dependencies that works on Linux and macOS for development workflows and Linux Server Runtime administration.

---

## Key Capabilities

| Capability | Description |
|---|---|
| **Project Lifecycle Management** | Initialize, configure, and manage Anvil-managed projects with consistent identity and structure |
| **Configuration Management** | Multi-source configuration system with canonical schema validation, environment variable overrides, and load-time error reporting |
| **Artifact Lifecycle Management** | Package application source into immutable, verified Artifacts identified by embedded `manifest.json` metadata and integrity checksums |
| **Deployment Orchestration** | Provide the boundary for Artifact delivery, targets, authentication, and orchestration without reading Runtime internals |
| **Server Runtime Management** | Initialize Runtime configuration, register projects, inspect readiness, and manage the Runtime Registry and Runtime State |
| **Release Lifecycle Management** | Install Artifacts into `Ready` Runtime Releases; activation and rollback are the next Runtime lifecycle capabilities |
| **Execution Foundation** | Unified Process Runner abstraction for executing external commands with timeout, capture, and deterministic outcome reporting |
| **Framework Adapter Integration** | Adapter metadata is supported in the MVP; executable framework-specific behavior remains post-MVP |
| **Command-Line Experience** | Task-oriented, predictable, and discoverable CLI with built-in help, actionable error messages, and deterministic exit codes |
| **Verification & Diagnostics** | System health assessment, pre-activation readiness checks, diagnostic reporting with actionable recommendations |

---

## Architecture

Anvil is organized into four bounded domains with one-way dependency flow:

```text
Project → Artifact → Deployment → Server Runtime
```

- **Project Domain** — repository-aware initialization, validation, build, package preparation, and CI workflows.
- **Artifact Domain** — immutable deployment payloads whose identity comes from embedded `manifest.json`, never from a filename.
- **Deployment Domain** — delivery and orchestration between Artifact producers and Runtime targets; it does not read Runtime Registry or filesystem layout.
- **Server Runtime Domain** — Runtime Registry, Runtime State, Artifact installation, Release lifecycle, activation, rollback, locking, and health.

Detailed architecture documentation and Architectural Decision Records (ADRs) are maintained internally.

---

## Quick Start

```bash
# Install Anvil as a single binary with no runtime dependencies.

# Development context: initialize a project.
anvil init my-project
cd my-project

# Development context: create and verify an Artifact.
anvil artifact package
anvil artifact verify .anvil/artifacts/<artifact>.tar.gz

# Server Runtime context: initialize Runtime configuration.
# Production root: /etc/anvil
# The override is intended for testing and isolated environments.
anvil server init --server-root /tmp/anvil-runtime

# Register a project without reading repository source.
anvil server project register \
  --project-id my-project \
  --install-root /srv/apps/my-project \
  --server-root /tmp/anvil-runtime \
  --non-interactive

# Inspect Runtime readiness.
anvil server status my-project --server-root /tmp/anvil-runtime
```

Runtime installation creates a `Ready` Runtime Release from an Artifact. Runtime activation and rollback operate on existing Releases and are scheduled for subsequent implementation work.

## Runtime Model

Runtime configuration is stored on the server, not in the repository:

```text
/etc/anvil/
├── config.yaml
└── projects/
    ├── my-project.yaml
    └── another-project.yaml
```

`config.yaml` contains global Runtime metadata. Each project file contains declarative Runtime Registry configuration such as installation root, owner, group, shared paths, adapter metadata, and display name. Runtime State is stored separately and contains operational data such as installed Releases, activation state, rollback history, and locks.

The Runtime lifecycle is:

```text
Artifact → Runtime Install → Runtime Release (Ready) → Runtime Activate → Running Release
```

The embedded `manifest.json` is the authoritative Artifact contract. Artifact filenames and transport paths have no semantic identity.

---

## Project Status

Anvil has completed Sprint 7 and is entering the next Runtime installation and activation phase.

| Milestone | Status |
|---|---|
| Product Requirements (PRD) | Complete |
| Architecture & ADRs | Complete |
| Epic Definitions | Complete |
| Work Item Planning | Complete |
| Sprints 1–6 | Complete and historical |
| Sprint 7 — Runtime Bootstrap and Registry | Done |
| Runtime installation and activation | Planned for Sprint 8 |

---

## License

[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

---

*Anvil is a Maleo Labs project.*
