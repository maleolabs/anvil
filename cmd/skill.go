// Package cmd implements the Anvil CLI commands.
//
// ── Skill Group (ST-021-01; ADR-037) ─────────────────────────────────
//
// "anvil skill" is the single core-gated install path for AI agent
// skills (ADR-037 D4 — no side channels): list, install, update, and
// uninstall skills for AI coding agents at a repo or global scope.
//
// Two sources, one gate:
//
//   - Core skills ship inside the Anvil binary (go:embed,
//     internal/skills/core/) and are lockstep with the CLI version they
//     describe (ADR-037 D2). Installing one materializes the embedded
//     content with a provenance header "source: core <cli-version>" and
//     records it (installed-skills store, TS-021-03).
//   - Standard skills are per-skill release assets declared in an
//     installed standard's registry metadata (skills[], TS-021-04) and
//     covered by the attested named-asset digests (TS-014-04-04). The
//     install gate is the full adoption pipeline: resolve the pinned
//     standard → strict parse → lifecycle + compatibility gates → trust
//     anchors before the fetch → fetch from the standard's release
//     channel (hardened https-only client, ADR-030 + TD-008) →
//     VerifyAssetDigest fail-closed → strict bundle extraction
//     (TS-021-01) → record.
//
// Targets and scopes (ADR-037 D5–D7): --agent selects the agents
// (default: auto-detect from installed config folders; "all" = every
// selectable agent), --scope selects repo (the current Anvil project's
// git root; requires an Anvil project) or global (the user's agent
// directories; no project required). One master copy lands at
// <scope>/.agents/skills/<name>/ for agents that read it natively;
// agents with native locations (Claude Code, Cursor) get a symlink (or
// copy on Windows) to it. Conflicts and shadows abort with actionable
// errors; --force overrides them — --force is destructive and replaces
// same-name content at the target locations.
//
// Updates are explicit-only (ADR-037 D8): 'anvil skill update' is the
// only surface that changes an installed skill, and 'anvil update' never
// syncs skills. Core skills materialized before a CLI update are flagged
// stale by 'anvil skill list'.
//
// Supply-chain posture (ADR-037 D10): skills committed to a repo are
// visible to every developer's agent in that repo — the intended team
// distribution workflow. Anvil guarantees the integrity and provenance
// of what it installs (attested digests, provenance headers); it never
// executes skill content.
//
// Reference: ST-021-01, ADR-037 D1–D10, TS-021-01/02/03/04, TS-P8-05,
// TS-P8-07
package cmd

import (
	"github.com/spf13/cobra"
)

// skillCmd represents the "anvil skill" parent command group for
// installing and managing AI agent skills.
//
// The group is a parent-only namespace (ADR-010 §6.7): it has no RunE —
// running "anvil skill" displays the group help listing the subcommands.
var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Install and manage AI agent skills",
	Long: `Install and manage AI agent skills (Agent Skills, agentskills.io) for
the AI coding agents you use with Anvil.

Skills are structured guidance agents load on demand. Anvil distributes
them from two sources through one core-gated install path:

  - core skills — shipped inside the Anvil binary (lockstep with the
    CLI version they describe; installable offline);
  - standard skills — per-skill release assets of an installed
    delivery lifecycle standard, declared in its registry metadata and
    covered by the standard's attested named-asset digests. Installing
    one runs the full adoption pipeline: lifecycle + compatibility
    gates, trust anchors, an https-only fetch from the standard's
    release channel, fail-closed digest verification, and strict
    extraction.

Targets: --agent selects the agents to install for (default: auto-detect
from the agent config folders on this machine; "all" = every supported
agent). --scope selects where skills land: "repo" installs into the
current Anvil project's git root (requires an Anvil project) and is the
default; "global" installs into your home-level agent directories (no
project required). A master copy is written to <scope>/.agents/skills/
<name>/ for agents that read it natively; agents with their own native
locations (Claude Code, Cursor) get a symlink (copy on Windows) to it.

--force replaces same-name skills already present at the target
locations and ignores shadow warnings. It is destructive: user content
at those paths is removed first. Use it only when you intend to replace
what is there.

Updates are explicit: 'anvil skill update' is the only way to change an
installed skill, and 'anvil update' never syncs skills. Core skills
materialized before a CLI update are flagged stale by 'anvil skill
list'.

Supply chain: skills committed to a repo are visible to every
developer's agent in that repo — the intended team workflow. Anvil
guarantees the integrity and provenance of what it installs (attested
digests, provenance headers) but never executes skill content.

Subcommands:
  list        List embedded core skills and installed skills
  install     Install a skill (core or standard)
  update      Update an installed skill (explicit re-adoption)
  uninstall   Remove an installed skill (content and record)

Examples:
  anvil skill list
  anvil skill list --json
  anvil skill install anvil-overview
  anvil skill install anvil-overview --scope global --agent opencode
  anvil skill install anvil-overview --force
  anvil skill update anvil-overview
  anvil skill uninstall anvil-overview`,
}

func init() {
	rootCmd.AddCommand(skillCmd)
	skillCmd.AddCommand(skillListCmd, skillInstallCmd, skillUpdateCmd, skillUninstallCmd)
}
