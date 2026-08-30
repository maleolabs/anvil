---
name: anvil-overview
description: An orientation to the Anvil CLI — what Anvil is, its command groups, the project context every command needs, and the quick-start workflow. Use when answering questions about Anvil itself, when a task requires an anvil command, or when an agent first encounters an Anvil project.
license: MIT
---

# Anvil Overview

Anvil is a release lifecycle engine for single-server deployments. It is a
framework-agnostic command-line tool that defines, distributes, verifies, and
enforces how software is built, released, and deployed — consistently, across
every project and every framework.

This skill gives an AI coding agent a compact orientation to the Anvil CLI so
it can answer questions about Anvil and write correct commands inside an Anvil
project.

## What Anvil is

- A **standard**: a governed specification of the delivery lifecycle
  (build → verify → release → activate → roll back), plus the runtime and
  standards that implement it.
- **Framework-agnostic**: Anvil knows how to release software; the framework
  (for example Laravel or Flutter) is owned by an installable delivery
  lifecycle **standard**, not by the CLI core.
- **Single-server deployments**: Anvil models one runtime per deployment
  target — install, activate, roll back, and inspect releases on that target.

## Project context

Most commands run inside an Anvil project: a directory tree with an
`anvil.yaml` at its root. Commands that need a project discover it by walking
up from the current directory.

- `anvil init <name>` creates a new project (writes `anvil.yaml`).
- `anvil status` shows the current project's identity; `anvil project status`
  adds the lifecycle stage and config validity.
- Run `anvil <command> --help` before guessing — every state-changing command
  documents its exact effects.

## Command groups

The CLI is organized into product domains (see `anvil --help`):

| Command | Purpose | Key commands |
|---|---|---|
| `anvil init` | Create a new Anvil project | `init <name>` |
| `anvil project` | Project identity, metadata, versioning | `project status`, `project version set`, `project version bump:*`, `project remove` |
| `anvil config` | Configuration inspection and validation | `config get`, `config list`, `config validate`, `config levels` |
| `anvil artifact` | Package and verify artifacts | `artifact package`, `artifact verify <path>` |
| `anvil pipeline` | Run build and CI workflows from `.anvil/pipelines/` | `pipeline build`, `pipeline ci` |
| `anvil standard` | Discover, install, and update delivery lifecycle standards | `standard list`, `standard install <id> <version>`, `standard update <id> <version>`, `standard inspect <id> [version]` |
| `anvil skill` | Install and manage AI agent skills | `skill list`, `skill install <name>`, `skill update <name>`, `skill uninstall <name>` |
| `anvil deployment` | Manage deployments on targets | `deployment install <artifact-path>`, `deployment activate`, `deployment rollback`, `deployment info`, `deployment upload` |
| `anvil server` | Server-side runtime: init, projects, releases, readiness | `server init`, `server project register`, `server release install/activate/rollback`, `server readiness` |
| `anvil system` | System inspection (demoted diagnostics) | `system inspect environment\|runtime\|config\|release\|deps` |
| `anvil update` | Update the Anvil CLI binary itself | `update` |

The `adapter` group is the deprecated v1 surface for framework adapters; use
`standard` instead. Framework knowledge lives in the installed standard's
skills — see below.

## Quick start

A typical first workflow, end to end:

```sh
# 1. Create a project (writes anvil.yaml).
anvil init my-app

# 2. Discover and adopt a delivery lifecycle standard for your framework.
anvil standard list
anvil standard install <id> <version>

# 3. Build a release artifact through the standard's build pipeline.
anvil pipeline build

# 4. Install, activate, and (when needed) roll back the release on a target.
anvil deployment install <artifact-path>
anvil deployment activate <project-id> <release-id>
anvil deployment rollback <project-id>
```

Every adoption (standard install/update) is an explicit event and runs
verification before trust. Anvil never updates or syncs content implicitly:
after `anvil update`, installed skills that shipped with an older CLI are
flagged `stale` by `anvil skill list` until you run `anvil skill update`.

## Using Anvil's AI agent skills

- `anvil skill list` shows the skills embedded in this binary ("available",
  "installed", or "stale") and skills installed from standards.
- `anvil skill install <name>` materializes a skill into the agent
  directories of your choice (`--agent`, `--scope repo|global`).
- Installed skills carry a provenance header (`# source: core <cli-version>`
  for core skills) so their origin is always visible.
- **Skills are guidance only. Anvil never executes skill content.**
- For framework-specific guidance (for example Laravel or Flutter), install
  and use the skills shipped with the corresponding standard — framework
  knowledge is not part of the core skills.

## Getting help

- `anvil --help` — domain-organized command listing.
- `anvil <command> --help` — exact flags, effects, and exit codes per command.
- Exit codes are a stable contract: 0 success; 1 general; 2 configuration or
  conflict; 3 runtime (not found); 4 precondition. Automation branches on the
  code, never on error wording.
- `--json` on supported commands returns a standard machine-readable envelope.

## References

See [the reference guide](references/REFERENCE.md) for a compact command
reference grouped by task.
