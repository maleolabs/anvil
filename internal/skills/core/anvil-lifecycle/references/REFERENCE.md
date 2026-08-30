# Anvil Lifecycle Reference

## Delivery stage → command table

| Stage | Command | Notes |
|---|---|---|
| Initialize | `anvil init <name>` | Writes `anvil.yaml`; project context for everything else. |
| Adopt | `anvil standard list` / `install <id> <version>` | Explicit adoption; deprecated installs warn. |
| Build | `anvil pipeline build [--env production] [--output dir] [--target t]` | Uses `.anvil/pipelines/build.yaml`. |
| Verify | `anvil artifact verify <artifact-path>` / `anvil config validate` | Verification before trust, unskippable. |
| Release / Install | `anvil deployment install <artifact-path>` / `anvil server release install <project-id> <artifact-path>` | Creates a runtime release. |
| Activate | `anvil deployment activate <project-id> <release-id>` / `anvil server release activate <project-id> <release-id>` | Promotes the staged release. |
| Observe | `anvil deployment info` · `anvil server release status/history/active` · `anvil server readiness` · `anvil server doctor` · `anvil system inspect <component>` | Informational; findings do not gate (demoted diagnostics, ADR-036). |
| Roll back | `anvil deployment rollback <project-id>` / `anvil server release rollback <project-id>` | Reverts the active release. |

## Lifecycle state decision summary

| State | As installed standard — update allowed? | As update target — resolvable? | Warning? |
|---|---|---|---|
| `published` | Yes | Yes (clean update) | No |
| `deprecated` | **No** (no-updates rule — the gate is on the INSTALLED standard) | Yes — update succeeds with a warning, records the deprecated state, receives no further updates | Yes (removal announced or not) |
| `retired` | No | No (not offered for adoption) | — (migration path for existing adoptions) |

> Source vs target: the update gate is on the **installed** standard — only a
> `published` installed standard receives updates. The **target** version may
> be `published` (clean) or `deprecated` (succeeds with a warning); a
> `retired` target is not resolvable.

## Staleness decision (skills)

| Condition | `skill list` status | Action |
|---|---|---|
| Core skill installed by the current CLI | `installed` | — |
| Core skill installed by an older CLI (version skew) | `stale` | `anvil skill update <name>` |
| Source standard missing / `deprecated` / `retired` | `stale` | Update or uninstall the skill; fix the source standard |
| Record at a different version during install | error (exit 2) | `anvil skill update <name>` |

## Exit code categories

| Code | Meaning |
|---|---|
| `0` | Success |
| `1` | General error |
| `2` | Configuration or conflict (including version conflict) |
| `3` | Runtime (not found) |
| `4` | Precondition (project missing, runtime not initialized, no agent detected) |
