# AI Agent Skills (`anvil skill`)

This page is the usage reference for the `anvil skill` command group: installing and managing AI agent skills (Agent Skills, agentskills.io) for the AI coding agents you use with Anvil. The group is the **single core-gated install path** (ADR-037 D4) — skills are never installed implicitly and never as a side effect of another command.

Reference: [ADR-037](../docs/adr/ADR-037-ai-agent-skills-distribution.md) (D1–D10), [TS-021-01/02/03/04](../docs/work-items/technical-stories/), [exit code conventions](../docs/operations/exit-codes.md).

## Command surface

| Command | Purpose |
|---|---|
| `anvil skill list` | List the skills available to install and the skills already installed (core section + standard section; status `available` / `installed` / `stale` / `unavailable`, target paths, stale/unavailable hints, unreadable records — never silently dropped). Read-only. |
| `anvil skill install <name>` | Install a skill (core or standard) for the selected agents at the selected scope. Idempotent re-install at the same version refreshes the targets; a different recorded version is rejected — version change is an update. |
| `anvil skill update <name>` | Re-adopt an installed skill against its **recorded** source and refresh every recorded target (stale files pruned, dropped targets removed). The only surface that changes an installed skill — `anvil update` never syncs skills (ADR-037 D8). |
| `anvil skill uninstall <name>` | Remove an installed skill: every recorded target path (containment-checked) and the installed-skills record. Uninstalling a skill that is not installed is graceful (exit 0). |

### Flags

| Flag | Commands | Meaning |
|---|---|---|
| `--agent <value>` | install, update, uninstall | `all \| claude-code \| opencode \| codex \| gemini \| cursor \| zed \| windsurf \| cline`. Default: auto-detect from the agent config folders present on this machine. On uninstall it filters which recorded targets are removed. |
| `--scope <value>` | install, update, uninstall | `repo` (default) — the current Anvil project's git root; requires an Anvil project inside a git repository. `global` — your home-level agent directories; no project required. On uninstall it filters the removed targets. |
| `--force` | install, update, uninstall | Replace existing same-name skills at the target locations and ignore shadow warnings. **Destructive:** the replaced content is removed first. On uninstall it is accepted for surface consistency and has no effect. |
| `--json` | all four | Standard TS-P8-05 envelope on stdout for machine consumption. |
| `--index <path>` | install, update | Registry index resolution override (`$ANVIL_REGISTRY_INDEX`, else `<user config dir>/anvil/registry`) — standard skills only. |
| `--trust-anchors <path>` | install, update | Trust anchors allowlist override (`$ANVIL_TRUST_ANCHORS`, else `<user config dir>/anvil/trust-anchors.json`) — standard skills only. |

### Sources (one gate)

- **Core skills** ship inside the Anvil binary (`go:embed`) and are lockstep with the CLI version they describe (ADR-037 D2). Installing one materializes the embedded content with a provenance header and records it; no external gates — the content ships in the binary.
- **Standard skills** are per-skill release assets declared in an installed standard's registry metadata (`skills[]`) and covered by the standard's attested named-asset digests. Installing one runs the full adoption pipeline: resolve the pinned standard → strict parse → lifecycle + compatibility gates → **trust anchors before the fetch** → https-only fetch from the standard's release channel → fail-closed `VerifyAssetDigest` (no checksum fallback) → strict bundle extraction → record. A skill can be installed only when its source standard is installed — **the installed-standard record IS the skill registry** (ADR-037 D3): discovery resolves the record's `skills[]` declarations, never a search over the registry index. Deprecated/retired standards receive no skill updates (ADR-023 §3); their installed skills are surfaced as `stale` by `anvil skill list`.

### Listing semantics (`anvil skill list`)

The standard section iterates the installed-standard records — the record is the registry (ADR-037 D3):

- **`available`** — declared by an installed standard (shown with its source standard and declared version) but not recorded as installed. Installable.
- **`installed`** — recorded and current.
- **`stale`** — recorded but out of date: a core skill version skew vs the CLI version, or a missing/deprecated/retired/corrupt source standard. Actionable hints; never deleted automatically.
- **`unavailable`** — declared by a **retired** standard: not offered for installation (ADR-027 §3, D4 gates) with an actionable message. A **deprecated** standard's skills stay `available` with a deprecation hint (install proceeds with a warning, ADR-023 §3).
- Standard uninstall removes the record, so its declared-but-not-installed skills disappear from the listing; installed ones stay, flagged `stale` by TS-021-03.
- Each standard's skills live under its own namespace `skills/<standard-id>/<name>` — the same skill name declared by two standards yields one row per source.
- **JSON shape** (`--json`): `{cli_version, skills: [{name, source, version, status, installed_at, targets, hints}], corrupt_records: [{path, error}], corrupt_standard_records: [{path, error}]}` — unreadable installed-skill records land in `corrupt_records`, unreadable installed-standard records (whose declared skills cannot be enumerated) in `corrupt_standard_records`; both are surfaced, never silently dropped (MIN-1/F-5).

### Resolution (`skill install`/`update`, standard skills)

Standard-skill resolution is **record-based** (ST-021-04, ADR-037 D3): `skill install <name>` iterates the installed-standard records and matches the record's `skills[]` declarations — the installed-standard record is the registry, and a skill is installable only from a standard that is installed. The registry index is consulted only for the **matched** standard's pinned release metadata (asset URL, attested digests, lifecycle/compatibility declarations) — never for discovery. The error surface:

- **Not provided** (exit 3) — no installed standard declares the skill, with actionable context: the standards that do declare skills, or the legacy-record hint ("the installed standard(s) X declare no skills; a standard's skills are registered at its install or update — refresh the record with `anvil standard install/update <id> <version>`") when the standard is installed but its record carries no declarations (a pre-ST-021-04 format-1 record or a skill-less release).
- **Ambiguity** (exit 1) — the same skill name is declared by multiple installed standards: an environment error, not a missing skill. Each standard's skills live under its own namespace `skills/<standard-id>/<name>`; install the standard that owns the skill, or uninstall the standard whose skill you do not want. Duplicate declarations within a single record are diagnosed as an inconsistent record, not a fake multi-standard ambiguity.
- **Divergence** (exit 1) — the record declaration and the strict-parsed release metadata of the pinned version disagree (record carries a skill the metadata no longer declares, or a different version/asset): re-install or update the standard to refresh the declarations. A matched standard whose pinned release metadata is missing/invalid in the index cannot provide skills and fails with an actionable error (exit 1).

## Supported agents

The selectable `--agent` values, their native skill locations, and their conflict precedence (source: `internal/agenttarget/agents.go`; pinned by tests, ADR-037 §9.3):

| `--agent` | Agent | Reads `.agents/skills` natively | Native location (symlink on POSIX, copy on Windows) | Auto-detect folder | Precedence |
|---|---|---|---|---|---|
| `claude-code` | Claude Code | No | `<scope>/.claude/skills/<name>` | `~/.claude` | global > repo (personal shadows project) |
| `opencode` | OpenCode | Yes | — | `~/.config/opencode` (XDG) | repo > global |
| `codex` | Codex | Yes | — | `~/.codex` | repo > global |
| `gemini` | Gemini CLI | Yes | — | `~/.gemini` | repo > global |
| `cursor` | Cursor | Yes (since 2026) | `<scope>/.cursor/skills/<name>` (compatibility) | `~/.cursor` | repo > global |
| `zed` | Zed | Yes | — | `~/.config/zed` (XDG) | repo > global |
| `windsurf` | Windsurf | Yes | — | not auto-detectable in v1 | repo > global |
| `cline` | Cline | Yes | — | `~/.cline` | global > repo |
| `roo` | Roo Code | Yes | — | `~/.roo` | repo > global (not selectable — see below) |

`all` = every selectable agent above (all except `roo`).

## Scope semantics

| Scope | Base | Requires | Master copy location |
|---|---|---|---|
| `repo` (default) | The current Anvil project's git root | Anvil project + git repository (else exit 4) | `<git-root>/.agents/skills/<name>/` — when ≥1 selected agent reads `.agents/skills` natively (or the selection spans >1 agent) |
| `global` | The user's home directory | Nothing | `~/.agents/skills/<name>/` (Linux/macOS) · `%USERPROFILE%\.agents\skills\<name>\` (Windows) — when ≥1 selected agent reads `.agents/skills` natively (or the selection spans >1 agent) |

A master copy is written to `<scope>/.agents/skills/<name>/` — the de-facto universal directory most agents read natively — whenever at least one selected agent reads `.agents/skills` natively, or the selection spans more than one agent. A **lone native-only selection** (`--agent claude-code` / `--agent cursor`) writes no master: only that agent's native location is written (symlink/copy), so the skill stays private to that agent (ADR-037 D6). Agents with native locations that do **not** read `.agents/skills` (Claude Code, Cursor) get a symlink from their native location to the master when one exists; on Windows (no symlink privilege) a copy fallback is used (ADR-037 D6).

### Conflicts, shadows, and precedence

- **Precedence differs per agent** (ADR-037 D7): Claude Code personal > project; Cline global > project; most others project > global. An installer must never guess.
- **Conflict:** an existing same-name skill at a target location aborts with an actionable error; `--force` overrides it (destructive).
- **Shadow:** installing a skill that would be shadowed by a user's higher-precedence copy is reported, never silently accepted.
- **Uninstall safety:** a filesystem path is removed only when every recorded target referencing it is matched — the shared master copy survives a partial `--agent` uninstall, so other agents' symlinks never dangle. Target paths are verified (absolute-path + containment) before any removal.

## Provenance header semantics (ADR-037 D10)

Every installed `SKILL.md` carries a provenance comment line in its frontmatter identifying the exact source of the content:

| Source | Header |
|---|---|
| Core skill | `# source: core <cli-version>` (lockstep with the CLI version that shipped it) |
| Standard skill | `# source: <standard-id> <version>` (the installed source standard's pinned release) |

The header is injected by the installer (and by the bundle extractor for downloaded bundles) and is replaced on re-install/update. It makes the installed copy's origin auditable: the skill's integrity is guaranteed by the install pipeline (attested digests for standard skills; embedded content for core skills).

## Supply-chain posture (ADR-037 D10)

- **Repo-committed skills are the intended team workflow.** A skill committed to a repository's `.agents/skills/` (or an agent's native location, e.g. `.claude/skills/`) is visible to every developer's agent in that repo — the same trust boundary as `AGENTS.md` or in-repo `.claude/skills`. Teams distribute shared guidance by committing skills to the repo; every developer's agent picks them up without any per-machine install.
- **Anvil's guarantee is integrity + provenance, not execution.** Anvil verifies what it installs (attested digests, strict extraction, provenance headers) and records where it landed. **Anvil never executes skill content** — skills are markdown guidance loaded by the agent, and Anvil does not run them.
- Skills installed by `anvil skill install` are recorded in the installed-skills store (`installed-skills/<name>.json`, TS-021-03) with identity, version, source, resolution, and `targets[]`.

## Unsupported agents

`--agent continue`, `--agent aider`, and `--agent copilot` are **rejected with a notice** — these are instruction-only agent tools out of scope for v1 (ADR-037 §7); skills are not converted for them. `--agent roo` is also rejected with a notice: Roo Code reads the `.agents/skills` master copy natively and is covered by any scope-level install, but it is not a selectable `--agent` value in v1.

## Exit codes

| Command | 0 | 1 | 2 | 3 | 4 |
|---|---|---|---|---|---|
| `skill list` | Success (stale/unreadable records reported, not gated) | Core set or store read failure | — | — | — |
| `skill install` | Success (incl. idempotent same-version re-install) | Other errors (invalid release, gate failure, digest mismatch, fetch/extract) | Conflict / version conflict | Skill or source standard not found | Precondition (no agent, repo scope without project) |
| `skill update` | Success | Other errors (deprecated/retired source, gate failure, digest mismatch, fetch/extract) | Conflict | Not installed / source not found | Precondition |
| `skill uninstall` | Success (incl. graceful not-installed) | Errors (unreadable record, removal failure, containment rejection) | — | Filter matches no recorded target | Precondition (scope base unresolvable) |

The full contract lives in [docs/operations/exit-codes.md](../docs/operations/exit-codes.md) and is also printed by `anvil help exit-codes`.

## Examples

```bash
# List embedded core skills and installed skills
anvil skill list
anvil skill list --json

# Install a core skill (repo scope, auto-detected agents)
anvil skill install anvil-overview

# Install for one agent at global scope
anvil skill install anvil-overview --scope global --agent opencode

# Replace same-name content at the target locations (destructive)
anvil skill install anvil-overview --agent all --force

# Re-adopt an installed skill and refresh every target
anvil skill update anvil-overview

# Remove an installed skill (or only its repo-scope targets)
anvil skill uninstall anvil-overview
anvil skill uninstall anvil-overview --scope repo
```
