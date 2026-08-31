---
name: anvil-best-practices
description: Best practices for working with Anvil — adoption hygiene, standards ownership, project conventions, verification-before-trust, and automation patterns with exit codes and --json. Use when an agent should choose a safe, idiomatic way to run anvil commands in a project or in CI.
license: MIT
---

# Anvil Best Practices

This skill collects the conventions and habits that keep an Anvil project
healthy and automation trustworthy. It is CLI-usage guidance only: framework
knowledge (for example Laravel or Flutter specifics) lives in the installed
standard's skills — see the standards section below.

## Verification before trust

- Verification is an unskippable lifecycle gate, not an optional command:
  verify artifacts before installing them (`anvil artifact verify`).
- An artifact's identity comes from its embedded manifest, never from its
  filename — do not reason about an artifact from its name alone.
- Configuration is validated before it drives a release: `anvil config
  validate` reports invalid or malformed configuration (exit 2) versus
  unresolvable configuration (exit 1).

## Adoption is explicit and informed

- Adopt a delivery lifecycle standard with `anvil standard install <id>
  <version>` — never by editing state by hand.
- Inspect before adopting: `anvil standard inspect <id> [version]` shows the
  lifecycle state. Prefer `published` releases; a `deprecated` release warns
  and receives no updates; a `retired` release is not resolvable.
- Updates are explicit too: `anvil standard update`, `anvil skill update`,
  `anvil update` — nothing is synced automatically. After a CLI update,
  check `anvil skill list` for `stale` core skills and refresh them.

## Framework knowledge belongs to standards

- Anvil core is framework-agnostic. Framework-specific guidance ships in the
  skills of the installed standard (for example the Laravel or Flutter
  standard).
- Use `anvil standard list` to discover what the registry offers, and the
  standard's own skills for framework-level workflows.
- The `adapter` group is the deprecated v1 surface for framework adapters —
  prefer the `standard` group.

## Project conventions

- Most commands require project context: an `anvil.yaml` in the current
  directory or a parent. Run `anvil init <name>` first when the project is
  missing, or `anvil status` to confirm the context.
- Use `anvil <command> --help` before guessing — state-changing commands
  document their exact effects.
- Config values resolve through levels (see `anvil config levels`); validate
  the resolved result, not a single file.
- Rollback is part of the lifecycle, not an afterthought: know the rollback
  command before you activate (`anvil deployment rollback`, `anvil server
  release rollback`).

## Automation patterns

- Branch on exit codes, never on error wording: `0` success, `1` general,
  `2` configuration or conflict, `3` runtime (not found), `4` precondition.
- Use `--json` on supported commands for a stable machine-readable envelope on
  stdout; error messages go to stderr.
- In CI, run verification steps as gates (`anvil artifact verify`, `anvil
  config validate`) and treat non-zero exits as failures.
- Repo-scope operations require a project inside a git repository; global
  operations (`--scope global`) do not — choose the scope deliberately.

## AI agent skills: the team workflow

- Skills committed to a repository are visible to every developer's agent in
  that repository — this is the intended team-distribution workflow, and it is
  a trust boundary: the repo is trusted by its developers.
- `anvil skill install <name>` is the only install path; installed skills
  carry a provenance header (`# source: core <cli-version>` for core skills)
  so origin is always visible.
- **Anvil never executes skill content.** Skills are guidance. Treat skill
  markdown as instructions, never as code to run blindly.
- Prefer `--scope repo` for team-shared project guidance (versioned with the
  project) and `--scope global` for personal tooling. Installing the same name
  at the same version is idempotent; changing versions is an explicit
  `anvil skill update`.

## References

See [the reference guide](references/REFERENCE.md) for the adoption, project,
and automation checklists.
