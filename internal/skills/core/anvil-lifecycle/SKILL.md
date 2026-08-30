---
name: anvil-lifecycle
description: The Anvil release lifecycle — the delivery stages, the governed lifecycle states of standards and releases (published, deprecated, retired), and the commands that drive each stage. Use when explaining or executing an install, activate, rollback, update, or staleness decision with the Anvil CLI.
license: MIT
---

# Anvil Release Lifecycle

Anvil models software delivery as a governed lifecycle. This skill explains
the stages an artifact travels through, the lifecycle states a standard
release can be in, and which Anvil commands drive each step — so an agent can
pick the right command and explain the resulting state correctly.

## Delivery stages

A release travels through these stages:

| Stage | What happens | Driving commands |
|---|---|---|
| **Initialize** | A project is created with a valid configuration. | `anvil init <name>`, `anvil status`, `anvil project status` |
| **Adopt** | A delivery lifecycle standard is explicitly installed for the project's framework. | `anvil standard list`, `anvil standard install <id> <version>` |
| **Build** | The standard's build pipeline produces an artifact. | `anvil pipeline build [--env production]`, `anvil artifact package` |
| **Verify** | Integrity and identity of the artifact are checked before it is trusted. | `anvil artifact verify <artifact-path>`, `anvil config validate` |
| **Release / Install** | The artifact is installed and a runtime release is created on the target. | `anvil deployment install <artifact-path>`, `anvil server release install <project-id> <artifact-path>` |
| **Activate** | The release becomes the active release on the target. | `anvil deployment activate <project-id> <release-id>`, `anvil server release activate <project-id> <release-id>` |
| **Observe** | Lifecycle stage, history, readiness, and platform health are queried. | `anvil deployment info`, `anvil server release status/history`, `anvil server readiness`, `anvil server doctor`, `anvil system inspect ...` |
| **Roll back** | The active release is reverted to a previous state. | `anvil deployment rollback <project-id>`, `anvil server release rollback <project-id>` |

Verification is an unskippable gate: an artifact is verified before it is
installed and trusted. Identity comes from the artifact's embedded manifest,
never from its filename.

## Standard release lifecycle states

Every standard release in the registry carries one governed lifecycle state
(ADR-023 §3, ADR-027 §3). The CLI's behavior changes by state:

| State | Meaning | CLI behavior |
|---|---|---|
| `published` | The release is current and offered for adoption. | As an **installed** standard: receives updates (`standard update`). As an update **target**: adopted cleanly. |
| `deprecated` | The release is still available but announced for removal; it receives **no further updates**. | As an **installed** standard: no updates — the no-updates rule rejects the update. As an update **target**: adoptable — the update succeeds with a deprecation warning and records the deprecated state, so it receives no further updates. |
| `retired` | The release has been removed from the registry. | Not offered for adoption: not resolvable as an update target; existing adoptions follow the documented migration path. |

Consequences an agent should state correctly:

- `anvil standard update <id> <version>` succeeds only when the INSTALLED
  standard is `published` — a `deprecated` installed standard receives no
  updates (the no-updates rule applies to the installed standard, ADR-023
  §3). The target version may be `published` (clean update) or `deprecated`
  (the update succeeds with a warning and records the deprecated state, so
  it receives no further updates); a `retired` target is not offered for
  adoption and is not resolvable.
- `anvil standard install` accepts `published` and `deprecated` releases; a
  deprecated install proceeds with a warning that no updates will come.
- `anvil standard inspect <id> [version]` shows the lifecycle state — use it
  before recommending an install or update.

## Staleness and explicit updates

Adoption is always an explicit event. Nothing is updated or synced
implicitly:

- **Standards:** update only through `anvil standard update <id> <version>`.
- **CLI:** `anvil update` refreshes the binary; it never re-adopts standards
  or re-installs skills automatically.
- **Skills:** core skills ship in lockstep with the CLI. After `anvil update`,
  `anvil skill list` flags core skills installed by an older CLI as `stale`;
  `anvil skill update <name>` refreshes them. A skill whose source standard is
  missing, `deprecated`, or `retired` is likewise flagged `stale` — stale
  entries are kept, never silently deleted.
- **Installing a skill that is already recorded at a different version is
  rejected:** version change is an update, an explicit event.

## Choosing the right command

- "I need to release this artifact" → `anvil deployment install <artifact-path>`
  (or `anvil server release install <project-id> <artifact-path>` for the
  server runtime).
- "I need to promote the staged release" → `anvil deployment activate` /
  `anvil server release activate`.
- "I need to undo the activation" → `anvil deployment rollback <project-id>` /
  `anvil server release rollback <project-id>`.
- "Is the target ready?" → `anvil server readiness <project-id> <release-id>`
  (informational; findings are reported, never gating).
- "What is the active release?" → `anvil server release active <project-id>` /
  `anvil server status`.

## Exit codes

Exit codes tell automation which failure class occurred: `0` success, `1`
general, `2` configuration/conflict, `3` runtime (not found), `4`
precondition (for example: runtime not initialized, repo scope without a
project). Always branch on the code, never on error message wording.

## References

See [the reference guide](references/REFERENCE.md) for the stage → command
table and lifecycle-state decision summary.
