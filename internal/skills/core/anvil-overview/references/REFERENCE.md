# Anvil CLI Reference

A compact task-oriented reference for the Anvil CLI. Prefer `anvil <command>
--help` for the authoritative flags and exit codes.

## Project setup and identity

| Task | Command |
|---|---|
| Create a project (writes `anvil.yaml`) | `anvil init <name>` |
| Show the current project's status | `anvil status` |
| Show project identity and lifecycle stage | `anvil project status` |
| Set the project version | `anvil project version set <version>` |
| Bump the project version | `anvil project version bump:patch` / `bump:minor` / `bump:major` |
| Remove the project from the current directory | `anvil project remove` |
| Inspect resolved configuration | `anvil config levels` |
| Validate the resolved configuration | `anvil config validate` |

## Standards (delivery lifecycle)

| Task | Command |
|---|---|
| List standards offered for adoption | `anvil standard list` |
| Inspect a standard's versions and lifecycle state | `anvil standard inspect <id> [version]` |
| Adopt a standard release (explicit) | `anvil standard install <id> <version>` |
| Update an installed standard (explicit) | `anvil standard update <id> <version>` |
| Install a standard from bundled material (offline) | `anvil standard install-bundle <bundle-path>` |

## Artifacts and pipelines

| Task | Command |
|---|---|
| Package an artifact | `anvil artifact package` |
| Verify an artifact's integrity | `anvil artifact verify <artifact-path>` |
| Run the build pipeline (`.anvil/pipelines/build.yaml`) | `anvil pipeline build [--env production] [--output dir] [--target t]` |
| Run the CI pipeline | `anvil pipeline ci` |

## Deployments

| Task | Command |
|---|---|
| Install an artifact and create a runtime release | `anvil deployment install <artifact-path>` |
| Activate a release on a target | `anvil deployment activate <project-id> <release-id>` |
| Roll back the active release | `anvil deployment rollback <project-id>` |
| Show deployment target and delivery context | `anvil deployment info` |
| Upload an artifact to a target | `anvil deployment upload <target-id> <artifact-path>` |

## Server Runtime

| Task | Command |
|---|---|
| Initialize the runtime | `anvil server init` |
| Register a project | `anvil server project register` |
| Install a release from an artifact | `anvil server release install <project-id> <artifact-path>` |
| Build a release via the standard | `anvil server release build <project-id>` |
| Activate / roll back a release | `anvil server release activate <project-id> <release-id>` / `rollback <project-id>` |
| Show release lifecycle history / status | `anvil server release history <project-id> <release-id>` / `status <project-id> [release-id]` |
| Pre-activation readiness | `anvil server readiness <project-id> <release-id>` |
| Platform health state | `anvil server doctor` |

## System and update

| Task | Command |
|---|---|
| Inspect environment / runtime / config / release / deps | `anvil system inspect <component>` |
| Update the CLI binary | `anvil update` |

## AI agent skills

| Task | Command |
|---|---|
| List core and installed skills (offline) | `anvil skill list` |
| Install a skill | `anvil skill install <name> [--agent <a>] [--scope repo\|global]` |
| Update an installed skill (explicit) | `anvil skill update <name>` |
| Uninstall a skill | `anvil skill uninstall <name>` |

## Exit codes (stable contract)

- `0` success · `1` general error · `2` configuration or conflict ·
  `3` runtime (not found) · `4` precondition (missing project, runtime not
  initialized, no agent detected).

## Notes

- Repo-scope operations require an Anvil project (`anvil.yaml` in the current
  directory or a parent).
- Adoption and updates are explicit events; nothing is synced automatically.
- Framework-specific guidance is provided by the installed standard's skills,
  not by the core CLI skills.
