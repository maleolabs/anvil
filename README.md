# Maleo Anvil

**Standalone CLI toolkit for packaging immutable Artifacts, orchestrating delivery, and managing Releases on a Server Runtime.**

Anvil is a single binary with no runtime dependencies that standardizes how applications are packaged, verified, and deployed — regardless of framework, language, or infrastructure.

---

## Table of Contents

- [Problem](#problem)
- [Solution](#solution)
- [Key Capabilities](#key-capabilities)
- [Architecture](#architecture)
- [Quick Start](#quick-start)
- [Command Reference](#command-reference)
  - [Development](#development)
  - [Deployment](#deployment)
  - [Server Runtime](#server-runtime)
  - [Runtime](#runtime)
  - [System](#system)
- [Runtime Model](#runtime-model)
- [License](#license)

---

## Problem

Every software project that deploys to a server faces the same challenges:

- **Ad-hoc release machinery** — deployment logic embedded in CI YAML, project-specific shell scripts, or framework-bound tooling that cannot be reused
- **No standard vocabulary** — inconsistent terminology for build, package, deploy, activate, and rollback across teams
- **Framework lock-in** — deployment tooling tied to specific frameworks that breaks when teams switch stacks
- **Untested rollbacks** — rollback procedures that are undocumented and stressful to execute under pressure
- **Integrity gaps** — no standardized mechanism to verify artifact integrity before deployment

These problems are universal — and solved from scratch every time a new project starts.

---

## Solution

Anvil occupies the gap between application projects and deployment platforms. It is not an application framework, a CI platform, or a server configuration manager. It provides the contracts that connect them.

**Positioned between projects and platforms** — Anvil receives Artifacts from development or CI workflows and delivers them to Runtime environments. Deployment orchestration is a separate context; Server Runtime owns installation and Release lifecycle execution.

**Positioned between frameworks** — Anvil treats all applications the same: a set of files to package, verify, and activate. Framework-specific behavior is handled by adapters, not by the core.

**Positioned between environments** — Anvil uses the same commands and configuration schema regardless of whether the target is a developer laptop, a staging server, or production.

---

## Key Capabilities

| Capability | Description |
|---|---|
| **Project Lifecycle** | Initialize, configure, and manage projects with consistent identity and structure |
| **Configuration Management** | Multi-source configuration with canonical schema validation and environment overrides |
| **Artifact Lifecycle** | Package source into immutable, verified Artifacts with embedded `manifest.json` and integrity checksums |
| **Deployment Orchestration** | Transport Artifacts to targets without reading Runtime internals |
| **Server Runtime Management** | Initialize Runtime config, register projects, manage the Runtime Registry and State |
| **Release Lifecycle** | Install, activate, rollback, and cleanup Releases through defined stages |
| **Verification & Diagnostics** | System health checks, pre-activation readiness, diagnostic reporting |
| **Framework Adapters** | Adapter metadata for framework-specific behavior (extensible) |
| **CLI Experience** | Task-oriented, predictable commands with built-in help and deterministic exit codes |

---

## Architecture

Anvil separates three execution contexts:

| Context | Scope | Requires |
|---|---|---|
| **Development** | Repository-aware build, package, verify | `anvil.yaml` in project root |
| **Deployment** | Transport and orchestrate Artifacts | Published Runtime commands |
| **Server Runtime** | Runtime-aware Release management | `/etc/anvil/` configuration |

### Bounded Domains

Architecture follows four bounded domains with one-way dependency:

```text
Project → Artifact → Deployment → Server Runtime
```

- **Project Domain** — repository-aware initialization, validation, build, package preparation, and CI workflows
- **Artifact Domain** — immutable deployment payloads whose identity comes from embedded `manifest.json`, never from a filename
- **Deployment Domain** — delivery and orchestration between Artifact producers and Runtime targets
- **Server Runtime Domain** — Runtime Registry, Runtime State, Artifact installation, Release lifecycle, activation, rollback, and health

---

## Quick Start

### Install

**Latest version:**

```bash
curl -fsSL https://github.com/maleolabs/anvil/releases/latest/download/install.sh | sh
```

**Specific version:**

```bash
# Install version 1.2.0
curl -fsSL https://github.com/maleolabs/anvil/releases/download/v1.2.0/install.sh | sh

# Install pre-release version
curl -fsSL https://github.com/maleolabs/anvil/releases/download/v1.3.0-alpha.1/install.sh | sh
```

The install script automatically detects your platform (linux/darwin, amd64/arm64) and installs the appropriate binary to `/usr/local/bin/anvil`.

### Update

```bash
anvil update
```

### Basic Workflow

```bash
# 1. Initialize a project (Development context)
anvil init my-project
cd my-project

# 2. Package source into an immutable Artifact
anvil artifact package

# 3. Verify Artifact integrity
anvil artifact verify .anvil/artifacts/<artifact>.tar.gz

# 4. Initialize Server Runtime (on target server)
anvil server init

# 5. Register the project on the server
anvil server project register \
  --project-id my-project \
  --install-root /srv/apps/my-project \
  --non-interactive

# 6. Install the Artifact as a Release
anvil server release install my-project ./<artifact>.tar.gz

# 7. Activate the Release
anvil server release activate my-project <release-id>
```

### Deployment Context (Remote)

```bash
# Upload artifact to a remote target
anvil deployment upload <target-id> ./<artifact>.tar.gz

# Install on remote target
anvil deployment install <target-id> ./<artifact>.tar.gz

# Activate on remote target
anvil deployment activate <target-id> my-project <release-id>
```

---

## Command Reference

### Development

Project initialization, configuration, artifact packaging, and CI workflows. All commands require a valid `anvil.yaml` in the project root.

#### Project Management

| Command | Description | Flags |
|---|---|---|
| `anvil init <name>` | Initialize a new Anvil project | `--path` |
| `anvil status` | Display current project status | — |
| `anvil project status` | Display project lifecycle stage | — |
| `anvil project remove` | Remove project configuration | `--force` |
| `anvil project version` | Display current version | — |

#### Version Management

| Command | Description |
|---|---|
| `anvil project version set <version>` | Set version to an explicit value |
| `anvil project version bump:patch` | Bump patch version (0.0.X) |
| `anvil project version bump:minor` | Bump minor version (0.X.0) |
| `anvil project version bump:major` | Bump major version (X.0.0) |
| `anvil project version generate` | Generate VERSION file for runtime consumption |

#### Configuration

| Command | Description |
|---|---|
| `anvil config get <key>` | Get resolved value for a configuration key |
| `anvil config list` | List all resolved configuration values |
| `anvil config levels` | Display configuration values by scope level |

#### Artifact Lifecycle

| Command | Description | Flags |
|---|---|---|
| `anvil artifact package` | Package project into an immutable Artifact | `--output`/`-o`, `--format`/`-f`, `--json` |
| `anvil artifact verify <path>` | Verify Artifact integrity checksums | — |
| `anvil artifact verify-immutability <path>` | Verify Artifact immutability contract | — |
| `anvil artifact status <identity>` | Display Artifact status by identity | `--state-dir` |

#### Pipeline

| Command | Description | Flags |
|---|---|---|
| `anvil pipeline build` | Execute build pipeline | `--env`, `--output`/`-o` |
| `anvil pipeline ci` | Execute CI pipeline | — |

**Pipeline Build with Output Directory:**

The `--output` flag sets the output directory for build artifacts. The resolved absolute path is injected as `ANVIL_OUTPUT_DIR` into every task's environment, so pipeline tasks can reference it via `${ANVIL_OUTPUT_DIR}`.

```bash
# Build with output directory
anvil pipeline build --output dist/binaries

# Combined with environment
anvil pipeline build -o dist/binaries --env production
```

Example `build.yaml` with cross-platform compilation:

```yaml
pipeline:
    name: build
    stages:
        - name: dependencies
          tasks:
            - name: download
              command: go
              args: [mod, download]
        - name: compile
          tasks:
            - name: linux-amd64
              command: go
              args: [build, -o, ${ANVIL_OUTPUT_DIR}/app-linux-amd64, .]
              env:
                GOOS: linux
                GOARCH: amd64
            - name: darwin-arm64
              command: go
              args: [build, -o, ${ANVIL_OUTPUT_DIR}/app-darwin-arm64, .]
              env:
                GOOS: darwin
                GOARCH: arm64
```

---

### Deployment

Transport and orchestrate Artifacts to Runtime targets. Deployment commands operate on target identifiers and do not read Runtime internals.

> **Note:** `deployment` commands and `server release` commands perform the same underlying operations but serve different user personas:
> - **`deployment`** — target-centric, designed for CI/CD pipelines. Auto-extracts project ID from artifact manifest. Requires `target-id` as first argument.
> - **`server release`** — project-centric, designed for server operators. Requires `project-id` as first argument.
>
> Both delegate to the same `ServerReleaseCoordinator` internally. Choose based on your workflow context.

| Command | Description | Flags |
|---|---|---|
| `anvil deployment info <target-id>` | Display deployment target information | `--server-root`, `--json` |
| `anvil deployment upload <target-id> <artifact-path>` | Upload Artifact to target | `--server-root`, `--json` |
| `anvil deployment install <target-id> <artifact-path>` | Install Artifact on target | `--server-root`, `--json` |
| `anvil deployment activate <target-id> <project-id> <release-id>` | Activate a Release on target | `--server-root`, `--json` |
| `anvil deployment rollback <target-id> <project-id>` | Rollback to previous Release on target | `--server-root`, `--json` |

---

### Server Runtime

Runtime-aware commands for managing projects, Releases, and configuration on the server. Requires Runtime initialization (`anvil server init`).

#### Runtime Configuration

| Command | Description | Flags |
|---|---|---|
| `anvil server init` | Initialize Runtime configuration at `/etc/anvil/` | `--server-root` |
| `anvil server status [<project-id>]` | Display Runtime or project status | `--server-root` |
| `anvil server config get <key>` | Get Runtime configuration value | `--server-root` |
| `anvil server config set <key> <value>` | Set Runtime configuration value | `--server-root` |

#### Project Registration

| Command | Description | Flags |
|---|---|---|
| `anvil server project register` | Register a project in the Runtime Registry | `--project-id`, `--install-root`, `--display-name`, `--adapter`, `--owner`, `--group`, `--shared-link`, `--non-interactive`, `--server-root` |
| `anvil server project get <project-id>` | Look up registered project details | `--server-root` |

#### Release Lifecycle

> **See also:** [Deployment](#deployment) for the target-centric equivalent of these commands.

| Command | Description | Flags |
|---|---|---|
| `anvil server release install <project-id> <artifact-path>` | Install Artifact as a new Release | `--server-root`, `--json` |
| `anvil server release activate <project-id> <release-id>` | Activate a Release | `--server-root` |
| `anvil server release rollback <project-id>` | Rollback to previous Release | `--server-root` |
| `anvil server release cleanup <project-id> <release-id>` | Remove a specific Release | `--server-root` |
| `anvil server release history <project-id> <release-id>` | Display Release history | `--server-root`, `--json` |
| `anvil server release active <project-id>` | Display currently active Release | `--server-root`, `--json` |
| `anvil server doctor` | Platform health assessment (healthy/degraded/unhealthy) | `--server-root`, `--json` |
| `anvil server readiness <project-id> <release-id>` | Pre-activation readiness check for a Release | `--server-root`, `--json` |

---

### Runtime

Provision and inspect Runtime environments. These commands manage Runtime lifecycle independent of Server Runtime configuration.

| Command | Description | Flags |
|---|---|---|
| `anvil runtime provision` | Provision a new Runtime environment | `--name`, `--environment`, `--install-path` |
| `anvil runtime readiness` | Check Runtime readiness | `--install-root` |
| `anvil runtime status` | Display Runtime operational state | `--state-file` |
| `anvil runtime list` | List all provisioned Runtimes | `--runtimes-path` |
| `anvil runtime verify-shared` | Verify shared resources | `--install-root` |

---

### System

| Command | Description | Flags |
|---|---|---|
| `anvil system health` | Check system health | `--server-root`, `--json` |
| `anvil system inspect <component> [<project-id> <release-id>]` | Targeted component inspection (environment, runtime, config, release, deps) | `--server-root`, `--json` |
| `anvil system diagnose` | Context-aware diagnostic report with owner and next action per finding | `--server-root`, `--json` |
| `anvil update` | Update Anvil CLI to latest version | `--check`, `--force` |

---

## Runtime Model

Runtime configuration lives on the server, not in the repository.

### Directory Structure

```text
/etc/anvil/
├── config.yaml          # Global Runtime metadata
└── projects/
    ├── my-project.yaml  # Project Runtime Registry configuration
    └── another-project.yaml
```

- `config.yaml` — global Runtime metadata
- Project files — declarative Runtime Registry configuration: installation root, owner, group, shared paths, adapter metadata, display name

Runtime State is stored separately and contains operational data: installed Releases, activation state, rollback history, and locks.

### Release Lifecycle

```text
Artifact → Install → Release (Ready) → Activate → Running Release
                                                    ↓
                                              Rollback → Previous Release
```

| Stage | Description |
|---|---|
| **Ready** | Artifact installed, verified, and available for activation |
| **Active** | Release is live and serving traffic |
| **Rolled Back** | Release superseded by a rollback to a previous Release |
| **Cleaned Up** | Release artifacts removed from disk |

The embedded `manifest.json` is the authoritative Artifact contract. Artifact filenames and transport paths have no semantic identity.

---

## License

[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

Anvil is licensed under the [Apache License 2.0](LICENSE).

---

*Anvil is a Maleo Labs project.*
