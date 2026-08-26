# Anvil Best Practices — Checklists

## Adoption checklist

- [ ] `anvil standard inspect <id> [version]` — confirm the lifecycle state before adopting.
- [ ] Prefer `published` releases; accept `deprecated` only with the warning in mind (no updates).
- [ ] `anvil standard install <id> <version>` — explicit adoption; never edit state by hand.
- [ ] After `anvil update`, run `anvil skill list` and `anvil skill update` for `stale` core skills.
- [ ] Framework guidance comes from the standard's skills, not from core skills.

## Project workflow checklist

- [ ] `anvil init <name>` when the project context is missing (`anvil.yaml`).
- [ ] `anvil status` / `anvil project status` — confirm identity and stage before acting.
- [ ] `anvil config validate` — validate the resolved configuration before release work.
- [ ] `anvil artifact verify <artifact-path>` — verify before install (unskippable gate).
- [ ] Know the rollback path before activating: `anvil deployment rollback <project-id>`.
- [ ] `anvil <command> --help` — read exact effects for every state-changing command.

## Automation checklist

- [ ] Branch on exit codes: 0 success · 1 general · 2 config/conflict · 3 runtime (not found) · 4 precondition.
- [ ] Never string-match error messages — wording is not a stable contract.
- [ ] Parse `--json` envelopes from stdout; treat stderr as diagnostics.
- [ ] Run verification steps as CI gates (`artifact verify`, `config validate`).
- [ ] Choose scope deliberately: `--scope repo` (needs an Anvil project + git root) vs `--scope global`.

## Skills checklist

- [ ] `anvil skill list` — inspect core + installed skills and their status (offline, embedded).
- [ ] `anvil skill install <name>` — the only install path; re-install at the same version is idempotent.
- [ ] Version change is an update: `anvil skill update <name>`.
- [ ] Never execute skill content — skills are guidance (provenance header shows the source).
- [ ] Repo-committed skills are a documented team workflow; the repo is the trust boundary.
