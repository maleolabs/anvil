// Package cmd implements the Anvil CLI commands.
//
// ── Skill Update (ST-021-01; ADR-037 D8) ─────────────────────────────
//
// "anvil skill update <name>" is the only surface that changes an
// installed skill (explicit-only; 'anvil update' never syncs skills).
// Update is a re-adoption against the RECORDED source and a full target
// refresh:
//
//   - core skills: re-materialize the current embedded content (the
//     record's version moves to the current CLI version);
//   - standard skills: re-resolve the pinned source standard's release
//     (registry metadata skills[]), re-run the lifecycle + compatibility
//     gates (updates accept only PUBLISHED sources — the
//     deprecated/retired no-updates rule propagates to skills, ADR-023
//     §3), reload the trust anchors, re-fetch the asset (hardened
//     client, size-capped), re-verify the attested digest fail-closed,
//     and re-extract;
//   - targets: every recorded target is refreshed — the new content is
//     materialized in full and stale files (present in the old content,
//     absent in the new) are PRUNED, never left behind (re-extract penuh
//   - prune, not overwrite-only — ticket item 6 / T-004 N-4); a
//     recorded target no longer in the resolved set (--agent/--scope
//     overridden) is removed with the containment check;
//   - the record is Updated (installedAt preserved, TS-021-03).
//
// Reference: ST-021-01, ADR-037 D4/D8, TS-021-01/02/03/04
package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/agenttarget"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/registry"
	"maleolabs.com/anvil/internal/skillbundle"
	"maleolabs.com/anvil/internal/skills"
)

// skillUpdateCmd represents the "anvil skill update" command that
// re-adopts an installed skill and refreshes every target.
var skillUpdateCmd = &cobra.Command{
	Use:   "update <name>",
	Short: "Update an installed skill (explicit re-adoption)",
	Long: `Update an installed skill to the current version of its source and
refresh every installed target. Updates are explicit: this command is
the only surface that changes an installed skill — 'anvil update' never
syncs skills (ADR-037 D8).

  - core skills: the current embedded content of this Anvil binary is
    re-materialized (core skills are lockstep with the CLI version).
  - standard skills: the source standard's pinned release is re-adopted
    through the full pipeline — lifecycle + compatibility gates, trust
    anchors, https-only fetch, fail-closed digest verification, strict
    extraction. Only PUBLISHED sources can be updated: deprecated or
    retired standards receive no skill updates (ADR-023 §3); their
    installed skills are surfaced as stale by 'anvil skill list'.

Every recorded target is refreshed: the new content is written in full
and stale files (files of the old version no longer present in the new
one) are pruned — an update never leaves outdated files behind. A
recorded target that is no longer in the resolved set is removed (with
the containment check). Targets resolve from the record by default;
pass --agent / --scope to re-target the skill (old targets are removed,
new ones created).

Conflicts with same-name content at the target locations abort with an
actionable error; --force overrides them (destructive). The record keeps
its original installedAt; updatedAt reflects this adoption event.

Output formats:
  Default      sectioned update report (identity, scope, agents,
               targets, record path)
  --json       standard TS-P8-05 envelope on stdout, data:
               {name, source, version, scope, agents, targets,
               updated_at, record_path}

Index and trust anchors resolution (standard skills only) follow the
same order as 'anvil skill install' (--index / --trust-anchors flags,
then the ANVIL_REGISTRY_INDEX / ANVIL_TRUST_ANCHORS environment
variables, then the defaults under <user config dir>/anvil).

Exit codes: 0 success; 3 skill not installed or source not found; 2
conflict; 4 precondition (no selectable agent detected, repo scope
without an Anvil project/git root); 1 other errors (deprecated/retired
source, gate failure, digest mismatch, fetch/extract failure).

Examples:
  anvil skill update anvil-overview
  anvil skill update anvil-overview --agent all
  anvil skill update anvil-overview --json`,
	Args:         RangeArgsWithUsage(1, 1, "anvil skill update anvil-overview", "name"),
	SilenceUsage: true,
	RunE:         runSkillUpdate,
}

func init() {
	AddJSONFlag(skillUpdateCmd)
	addSkillTargetFlags(skillUpdateCmd)
	skillUpdateCmd.Flags().String("index", "", "path to the static registry index directory (default: $ANVIL_REGISTRY_INDEX, else <user config dir>/anvil/registry)")
	skillUpdateCmd.Flags().String("trust-anchors", "", "path to the trust anchors allowlist file (default: $ANVIL_TRUST_ANCHORS, else <user config dir>/anvil/trust-anchors.json)")
}

// skillUpdateJSON is the machine-readable update output (TS-P8-05 data).
type skillUpdateJSON struct {
	Name       string            `json:"name"`
	Source     string            `json:"source"`
	Version    string            `json:"version"`
	Scope      string            `json:"scope"`
	Agents     []string          `json:"agents"`
	Targets    []skillTargetJSON `json:"targets"`
	UpdatedAt  string            `json:"updated_at"`
	RecordPath string            `json:"record_path"`
}

// runSkillUpdate executes the update command: read the record, resolve
// the target scope/agents (flags override the record), re-adopt the
// content, refresh every target, and Update the record.
func runSkillUpdate(cmd *cobra.Command, args []string) error {
	name := args[0]
	if !skillbundle.ValidateName(name) {
		return skillReportError(cmd,
			fmt.Sprintf("skill name %q is invalid", name),
			"skill names match ^[a-z0-9][a-z0-9-]*$ and are at most 64 characters",
			"",
			output.ExitCodeGeneral, nil)
	}

	store, err := skillStore()
	if err != nil {
		return skillReportStoreError(cmd, "update", name, err)
	}
	rec, err := store.Get(name)
	if err != nil {
		return skillReportStoreError(cmd, "update", name, err)
	}

	// Target resolution: --agent/--scope override the record; otherwise
	// the recorded scope and agents are re-resolved (refresh EVERY
	// recorded target).
	scope, err := skillScope(cmd)
	if err != nil {
		return skillReportError(cmd, "invalid --scope", err.Error(), "", output.ExitCodeGeneral, err)
	}
	scopeChanged := FlagIsSet(cmd, "scope")
	agentsChanged := FlagIsSet(cmd, "agent")
	if !scopeChanged {
		scope = agenttarget.Scope(rec.Targets[0].Scope)
	}
	var agents []agenttarget.Agent
	if agentsChanged {
		agents, err = skillAgents(cmd)
		if err != nil {
			return skillReportAgentError(cmd, err)
		}
	} else {
		agents, err = recordedSkillAgents(rec)
		if err != nil {
			return skillReportError(cmd, "cannot re-resolve the recorded agents", err.Error(), "", output.ExitCodeGeneral, err)
		}
	}
	force, err := skillForce(cmd)
	if err != nil {
		return err
	}

	if rec.Source == registry.SkillSourceCore {
		return runSkillUpdateCore(cmd, name, rec, store, scope, agents, force)
	}
	return runSkillUpdateStandard(cmd, name, rec, store, scope, agents, force)
}

// recordedSkillAgents re-derives the agent set from a record's targets
// (unique agent IDs, "all" excluded — it is the master placeholder, not
// a selectable agent).
func recordedSkillAgents(rec registry.InstalledSkillRecord) ([]agenttarget.Agent, error) {
	seen := map[string]bool{}
	var ids []string
	for _, t := range rec.Targets {
		if t.Agent == "all" || seen[t.Agent] {
			continue
		}
		seen[t.Agent] = true
		ids = append(ids, t.Agent)
	}
	sort.Strings(ids)
	var agents []agenttarget.Agent
	for _, id := range ids {
		parsed, err := agenttarget.ParseAgentFlag(id)
		if err != nil {
			return nil, fmt.Errorf("recorded agent %q is not a selectable agent (the record may be hand-edited): %w", id, err)
		}
		agents = append(agents, parsed...)
	}
	if len(agents) == 0 {
		return nil, fmt.Errorf("the record carries no selectable agents (the record may be hand-edited)")
	}
	return agents, nil
}

// ── Core update ──────────────────────────────────────────────────────

// runSkillUpdateCore refreshes a core skill: re-materialize the current
// embedded content and refresh every target. No external gates — the
// content ships in the binary.
func runSkillUpdateCore(cmd *cobra.Command, name string, rec registry.InstalledSkillRecord, store *registry.InstalledSkillStore, scope agenttarget.Scope, agents []agenttarget.Agent, force bool) error {
	core, ok, err := skills.Get(name)
	if err != nil {
		return skillReportError(cmd, fmt.Sprintf("could not read the embedded skill %q", name), err.Error(), "", output.ExitCodeGeneral, err)
	}
	if !ok {
		return skillReportError(cmd,
			fmt.Sprintf("core skill %q is recorded but no longer ships in this CLI", name),
			"the embedded core set does not contain this skill in the current binary",
			fmt.Sprintf("Run 'anvil skill uninstall %s' to remove the installed copy", name),
			output.ExitCodeGeneral, nil)
	}

	reporter := output.NewStepReporter(cmd.ErrOrStderr())
	overallStart := time.Now()
	reporter.Start(fmt.Sprintf("Update skill %s (core)", name))
	reporter.SetTotal(3)

	var files map[string][]byte
	err = skillStep(reporter, "Validate embedded skill", func() error {
		if _, err := validateCoreSkillContent(name, core.Files["SKILL.md"]); err != nil {
			return err
		}
		injected, err := injectCoreProvenance(core.Files["SKILL.md"], CliVersion)
		if err != nil {
			return err
		}
		files = make(map[string][]byte, len(core.Files))
		for rel, data := range core.Files {
			files[rel] = data
		}
		files["SKILL.md"] = injected
		return nil
	})
	if err != nil {
		reporter.Failed(fmt.Sprintf("Update skill %s (core)", name), time.Since(overallStart))
		return skillReportError(cmd, fmt.Sprintf("the embedded core skill %q is invalid", name), err.Error(), "Fix the embedded skill content (internal/skills/core/) or report the broken CLI build", output.ExitCodeGeneral, err)
	}

	var newSet *agenttarget.ResolvedSet
	err = skillStep(reporter, "Refresh targets", func() error {
		set, rerr := skillRefreshTargets(cmd.ErrOrStderr(), scope, name, files, agents, force, rec)
		newSet = set
		return rerr
	})
	if err != nil {
		reporter.Failed(fmt.Sprintf("Update skill %s (core)", name), time.Since(overallStart))
		return skillReportMaterializeError(cmd, name, err)
	}

	var updated registry.InstalledSkillRecord
	err = skillStep(reporter, "Record", func() error {
		updated, err = store.Update(name, registry.InstalledSkillRecord{
			FormatVersion: registry.InstalledSkillRecordFormatVersion,
			ID:            name,
			Version:       CliVersion,
			Source:        registry.SkillSourceCore,
			Resolution: registry.Resolution{
				Kind:   registry.SkillResolutionKindCore,
				Source: "embedded",
			},
			InstalledAt: rec.InstalledAt,
			UpdatedAt:   now(),
			Targets:     targetsFromResolvedSet(newSet),
		})
		return err
	})
	if err != nil {
		reporter.Failed(fmt.Sprintf("Update skill %s (core)", name), time.Since(overallStart))
		return skillReportStoreError(cmd, "update", name, err)
	}

	reporter.Complete(fmt.Sprintf("Updated %s", name), time.Since(overallStart))
	return reportSkillUpdate(cmd, skillUpdateResult{
		Name:       name,
		Source:     registry.SkillSourceCore,
		Version:    updated.Version,
		Scope:      string(scope),
		Agents:     agentIDs(agents),
		Targets:    updated.Targets,
		UpdatedAt:  updated.UpdatedAt,
		RecordPath: storeRecordPath(store, name),
	})
}

// ── Standard update ──────────────────────────────────────────────────

// runSkillUpdateStandard re-adopts a standard-sourced skill against its
// RECORDED source release: gates (published-only + compatibility), trust
// anchors, fetch, digest verification, extraction, full target refresh.
func runSkillUpdateStandard(cmd *cobra.Command, name string, rec registry.InstalledSkillRecord, store *registry.InstalledSkillStore, scope agenttarget.Scope, agents []agenttarget.Agent, force bool) error {
	reporter := output.NewStepReporter(cmd.ErrOrStderr())
	overallStart := time.Now()
	reporter.Start(fmt.Sprintf("Update skill %s", name))
	// 8 rendered phases: resolve, validate release, verify trust
	// anchors, fetch, verify digest, extract, refresh targets, record —
	// the count drives the "└─" connector on the last step.
	reporter.SetTotal(8)

	// Step 1: re-resolve the source standard + declaration.
	var match *skillStandardMatch
	var notes skillResolutionNotes
	err := skillStep(reporter, "Resolve standard skill", func() error {
		m, n, rerr := resolveStandardSkill(cmd, name)
		match = m
		notes = n
		return rerr
	})
	if err != nil {
		reporter.Failed(fmt.Sprintf("Update skill %s", name), time.Since(overallStart))
		// A genuinely absent skill is "not found" (3); an index/store/
		// ambiguity problem is an environment error (1) — MIN-4.
		exitCode := output.ExitCodeRuntime
		if !skillResolutionNotFound(err) {
			exitCode = output.ExitCodeGeneral
		}
		return skillReportError(cmd,
			fmt.Sprintf("skill %q cannot be updated", name),
			err.Error(),
			"The source standard must still be installed and its pinned release present in the index; otherwise uninstall the skill",
			exitCode, err)
	}
	// Advisory hints when standards were skipped (MIN-5, F-4).
	skillReportResolutionNotes(cmd, notes)
	// The recorded source identity must be the one being re-adopted: a
	// skill that moved to a different standard is a different skill.
	if match.Metadata.ID != rec.Source {
		reporter.Failed(fmt.Sprintf("Update skill %s", name), time.Since(overallStart))
		return skillReportError(cmd,
			fmt.Sprintf("skill %q changed source standard: recorded as %s, now declared by %s", name, rec.Source, match.Metadata.ID),
			"the recorded release is the update target; a changed source is a different skill",
			fmt.Sprintf("Run 'anvil skill uninstall %s' then 'anvil skill install %s' to adopt the new source", name, name),
			output.ExitCodeGeneral, nil)
	}

	// Step 2: gates — updates accept only PUBLISHED sources (the
	// no-updates rule for deprecated/retired propagates), then
	// compatibility.
	var warnings []string
	err = skillStep(reporter, "Validate release", func() error {
		_, w, gerr := skillAdoptionGates(cmd, &match.Metadata, true)
		warnings = w
		return gerr
	})
	if err != nil {
		reporter.Failed(fmt.Sprintf("Update skill %s", name), time.Since(overallStart))
		return skillReportError(cmd,
			fmt.Sprintf("skill %q cannot be updated from source standard %s %s", name, match.Metadata.ID, match.Metadata.Version),
			err.Error(),
			"Re-adopt the standard or uninstall the skill",
			output.ExitCodeGeneral, err)
	}

	// Steps 3–5: anchors → fetch → digest (same pipeline as install).
	var content []byte
	var sha256Hex string
	var contentSource string
	err = skillStep(reporter, "Verify trust anchors", func() error {
		anchorsPath, aerr := standardTrustAnchorsPath(cmd)
		if aerr != nil {
			return aerr
		}
		anchors, aerr := loadTrustAnchorsConfigured(anchorsPath)
		if aerr != nil {
			return aerr
		}
		trust := registry.VerifyAttestationAnchored(match.Metadata, anchors)
		if !trust.Valid {
			return fmt.Errorf(
				"the metadata of source standard %s %s fails trust verification: %s",
				match.Metadata.ID, match.Metadata.Version, strings.Join(trust.Errors, "; "))
		}
		return nil
	})
	if err != nil {
		reporter.Failed(fmt.Sprintf("Update skill %s", name), time.Since(overallStart))
		return skillReportError(cmd, "trust verification failed for the source standard", err.Error(),
			"Do not install content that fails verification; configure the correct trust anchors (--trust-anchors <path> or "+registry.EnvTrustAnchors+"), or report the broken release to its publisher", output.ExitCodeGeneral, err)
	}
	err = skillStep(reporter, "Fetch skill asset", func() error {
		assetURL, uerr := skillAssetURL(&match.Metadata, match.Skill)
		if uerr != nil {
			return uerr
		}
		c, h, src, ferr := skillAssetFetch(assetURL)
		content, sha256Hex, contentSource = c, h, src
		return ferr
	})
	if err != nil {
		reporter.Failed(fmt.Sprintf("Update skill %s", name), time.Since(overallStart))
		return skillReportError(cmd, fmt.Sprintf("could not fetch the skill asset of %q from %s", name, match.Metadata.ID),
			err.Error(), "If you are the publisher, fix the release asset; otherwise report the broken release", output.ExitCodeGeneral, err)
	}
	err = skillStep(reporter, "Verify asset digest", func() error {
		attested, verr := registry.VerifyAssetDigest(match.Metadata, match.Skill.Asset, sha256Hex)
		if verr != nil {
			return verr
		}
		if !attested {
			return fmt.Errorf(
				"release %s %s declares no attestation-bound digest for skill asset %q — skills are verified against the attested named digest only, with no checksum fallback (ADR-037 D4); obtain a fresh release from the publisher",
				match.Metadata.ID, match.Metadata.Version, match.Skill.Asset)
		}
		return nil
	})
	if err != nil {
		reporter.Failed(fmt.Sprintf("Update skill %s", name), time.Since(overallStart))
		return skillReportError(cmd, fmt.Sprintf("digest verification failed for the skill asset of %q", name),
			err.Error(), "Do not install content that fails verification; report the broken release to the publisher", output.ExitCodeGeneral, err)
	}

	// Step 6: re-extract in full (fresh staging).
	var files map[string][]byte
	staging, err := os.MkdirTemp("", "anvil-skill-*")
	if err != nil {
		reporter.Failed(fmt.Sprintf("Update skill %s", name), time.Since(overallStart))
		return skillReportError(cmd, "could not create a staging directory", err.Error(), "", output.ExitCodeGeneral, err)
	}
	defer os.RemoveAll(staging)

	err = skillStep(reporter, "Extract content", func() error {
		ext, xerr := skillbundle.Extract(content, staging)
		if xerr != nil {
			return xerr
		}
		if ext.Manifest.Name != name {
			return fmt.Errorf(
				"the downloaded bundle carries skill %q, not the requested %q — the release asset does not match its declaration; report the broken release to the publisher",
				ext.Manifest.Name, name)
		}
		files, xerr = skillFilesFromExtraction(ext)
		return xerr
	})
	if err != nil {
		reporter.Failed(fmt.Sprintf("Update skill %s", name), time.Since(overallStart))
		return skillReportError(cmd, fmt.Sprintf("the skill bundle of %q is rejected by the strict extraction", name),
			err.Error(), "Obtain a fresh copy of the bundle from the publisher, or report the broken release", output.ExitCodeGeneral, err)
	}

	// Step 7: full target refresh (re-extract penuh + prune stale files,
	// dropped targets removed) then record Update.
	var newSet *agenttarget.ResolvedSet
	err = skillStep(reporter, "Refresh targets", func() error {
		set, rerr := skillRefreshTargets(cmd.ErrOrStderr(), scope, name, files, agents, force, rec)
		newSet = set
		return rerr
	})
	if err != nil {
		reporter.Failed(fmt.Sprintf("Update skill %s", name), time.Since(overallStart))
		return skillReportMaterializeError(cmd, name, err)
	}

	var updated registry.InstalledSkillRecord
	err = skillStep(reporter, "Record", func() error {
		updated, err = store.Update(name, registry.InstalledSkillRecord{
			FormatVersion: registry.InstalledSkillRecordFormatVersion,
			ID:            name,
			Version:       match.Skill.Version,
			Source:        rec.Source,
			Resolution: registry.Resolution{
				Kind:   registry.SkillResolutionKindDistribution,
				Source: contentSource,
			},
			InstalledAt: rec.InstalledAt,
			UpdatedAt:   now(),
			Targets:     targetsFromResolvedSet(newSet),
		})
		return err
	})
	if err != nil {
		reporter.Failed(fmt.Sprintf("Update skill %s", name), time.Since(overallStart))
		return skillReportStoreError(cmd, "update", name, err)
	}

	reporter.Complete(fmt.Sprintf("Updated %s", name), time.Since(overallStart))
	return reportSkillUpdate(cmd, skillUpdateResult{
		Name:       name,
		Source:     updated.Source,
		Version:    updated.Version,
		Scope:      string(scope),
		Agents:     agentIDs(agents),
		Targets:    updated.Targets,
		UpdatedAt:  updated.UpdatedAt,
		RecordPath: storeRecordPath(store, name),
		Warnings:   warnings,
	})
}

// ── Target refresh ───────────────────────────────────────────────────

// skillRefreshTargets materializes the new content for the resolved set
// and refreshes every RECORDED target:
//
//   - pre-clean first: a recorded target whose materialization KIND
//     changes (lone-native/copy directory → symlink, symlink → copy)
//     must be removed BEFORE the install — the agent-target writer never
//     overwrites a differently-formed occupant, even its own, and would
//     refuse the transition. Only our OWN recorded targets are removed;
//   - then materialize (conflict/shadow gate + writer; our own previous
//     installs are idempotent) — a conflict failure here writes nothing
//     new;
//   - kept targets (recorded path present in the new set) are pruned of
//     stale files: every entry not part of the new content is removed,
//     never left behind (install-then-prune, so a prune failure leaves
//     the new content present instead of a half-emptied target);
//   - dropped targets (recorded path absent from the new set — the
//     --agent/--scope flags narrowed the target set) are removed with
//     the containment check against each target's OWN scope base
//     (MIN-8) AND the ownership check: a dropped path the user replaced
//     with their own content (no Anvil ownership marker) is NEVER
//     removed — it is skipped with a warning to warn (security LOW,
//     fix-round 2), because removing it would delete user content.
//
// warn receives advisory notes (non-fatal).
func skillRefreshTargets(warn io.Writer, scope agenttarget.Scope, name string, files map[string][]byte, agents []agenttarget.Agent, force bool, rec registry.InstalledSkillRecord) (*agenttarget.ResolvedSet, error) {
	newBase, err := skillScopeBase(scope)
	if err != nil {
		return nil, fmt.Errorf("resolve the %s scope base: %w", scope, err)
	}

	// The pure resolution of the new target set (no writes) — used for
	// the kind-transition pre-clean; the installer re-resolves the same
	// set during Install.
	newSet, err := agenttarget.Resolve(agents, scope, newBase, name)
	if err != nil {
		return nil, fmt.Errorf("resolve the updated targets: %w", err)
	}

	// Pre-clean kind transitions before the install. A failure after this
	// step leaves the affected target empty — the record is not updated,
	// and a re-run of the update recreates it.
	for _, t := range rec.Targets {
		if err := skillPreCleanTransition(t, name, newSet, newBase, rec); err != nil {
			return nil, err
		}
	}

	// Pre-clean file↔directory SHAPE conflicts inside kept targets
	// (LOW-2): the writer cannot overwrite across shapes, and the
	// stale-file prune runs after the install — the conflicting entries
	// must go first. Only our own targets are touched.
	for _, t := range rec.Targets {
		if !resolvedSetHasPath(newSet, t.Path) {
			continue // dropped — removed after the install
		}
		if err := skillPreCleanShapeConflicts(t.Path, newBase, name, files, rec); err != nil {
			return nil, fmt.Errorf("pre-clean shape conflicts in %s: %w", t.Path, err)
		}
	}

	installed, err := (&agenttarget.Installer{}).Install(scope, name, files, agents, force)
	if err != nil {
		return nil, err
	}
	newSet = installed

	keep := make(map[string]bool, len(files))
	for rel := range files {
		keep[rel] = true
	}
	newPaths := make(map[string]bool)
	for _, p := range agenttarget.ReadAllTargets(newSet) {
		newPaths[p] = true
	}

	// The containment base of a dropped target derives from its OWN
	// recorded scope (MIN-8), resolved with the typed classification and
	// memoized per scope value.
	baseCache := make(map[string]string)
	baseFor := func(t registry.InstalledSkillTarget) (string, error) {
		if b, ok := baseCache[t.Scope]; ok {
			return b, nil
		}
		b, berr := skillScopeBase(agenttarget.Scope(t.Scope))
		if berr != nil {
			return "", berr
		}
		baseCache[t.Scope] = b
		return b, nil
	}

	for _, t := range rec.Targets {
		if newPaths[t.Path] {
			if err := pruneStaleFiles(t.Path, newBase, name, keep); err != nil {
				return nil, fmt.Errorf("prune stale files in %s: %w", t.Path, err)
			}
			continue
		}
		dropBase, berr := baseFor(t)
		if berr != nil {
			return nil, fmt.Errorf("dropped target %s: %w", t.Path, berr)
		}
		if err := skillTargetContainment(t.Path, dropBase, name); err != nil {
			return nil, fmt.Errorf("dropped target %s: %w", t.Path, err)
		}
		if !skillTargetIsOurs(t.Path, name, rec) {
			// The user replaced this recorded path with their own content
			// (the Anvil ownership marker is gone): removing it would
			// delete user content without --force semantics. It is
			// skipped with an actionable note — ownership is never
			// overridden by the target-set refresh.
			fmt.Fprintf(warn,
				"Note: dropped target %s is not an Anvil-managed install (no ownership marker) and was NOT removed — remove it manually, or run 'anvil skill uninstall %s' to remove all recorded targets (--force overrides conflicts, never ownership)\n",
				t.Path, name)
			continue
		}
		if err := os.RemoveAll(t.Path); err != nil {
			return nil, fmt.Errorf("remove dropped target %s: %w", t.Path, err)
		}
	}
	return newSet, nil
}

// skillPreCleanTransition removes a recorded target BEFORE the install
// when its materialization KIND changes (a lone-native/copy directory →
// symlink, or a symlink → copy) and the occupant is our own — the
// agent-target writer never overwrites a differently-formed occupant,
// even its own, and would refuse the transition. Dropped targets (absent
// from the new set) are handled after the install; a target the user
// replaced with their own content stays for the writer's
// conflict/--force gate.
func skillPreCleanTransition(t registry.InstalledSkillTarget, name string, newSet *agenttarget.ResolvedSet, newBase string, rec registry.InstalledSkillRecord) error {
	if !resolvedSetHasPath(newSet, t.Path) {
		return nil // dropped target — removed after the install
	}
	kind := resolvedTargetKind(newSet, t.Path)
	info, err := os.Lstat(t.Path)
	if err != nil {
		return nil // not present — nothing to pre-clean
	}
	transition := false
	if info.Mode()&os.ModeSymlink != 0 && kind != agenttarget.TargetKindSymlink {
		transition = true // symlink → directory materialization
	}
	if info.Mode()&os.ModeSymlink == 0 && info.IsDir() && kind == agenttarget.TargetKindSymlink {
		transition = true // directory (copy/lone-native) → symlink
	}
	if !transition {
		return nil
	}
	if !skillTargetIsOurs(t.Path, name, rec) {
		return nil // not our own — the writer's conflict/--force gate owns it
	}
	if err := skillTargetContainment(t.Path, newBase, name); err != nil {
		return fmt.Errorf("pre-clean target %s: %w", t.Path, err)
	}
	if err := os.RemoveAll(t.Path); err != nil {
		return fmt.Errorf("pre-clean target %s (kind transition): %w", t.Path, err)
	}
	return nil
}

// resolvedSetHasPath reports whether a resolved set contains a target
// path (master included).
func resolvedSetHasPath(set *agenttarget.ResolvedSet, path string) bool {
	if set.Master != "" && path == set.Master {
		return true
	}
	for _, t := range set.Targets {
		if t.Path == path {
			return true
		}
	}
	return false
}

// resolvedTargetKind returns the materialization kind of a path in a
// resolved set, or "" when the path is not a target of the set.
func resolvedTargetKind(set *agenttarget.ResolvedSet, path string) agenttarget.TargetKind {
	if set.Master != "" && path == set.Master {
		return agenttarget.TargetKindMaster
	}
	for _, t := range set.Targets {
		if t.Path == path {
			return t.Kind
		}
	}
	return ""
}

// skillTargetIsOurs reports whether a path carries OUR ownership of the
// skill: a directory with our ownership marker, or a symlink pointing at
// one of the record's own targets (the old master copy). It mirrors the
// ownership recognition of the agent-target writer (the .anvil-install
// marker contract) so the update pre-clean never removes user content.
func skillTargetIsOurs(path, skillName string, rec registry.InstalledSkillRecord) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return false
		}
		for _, r := range rec.Targets {
			if filepath.Clean(target) == filepath.Clean(r.Path) {
				return true
			}
		}
		return false
	}
	if !info.IsDir() {
		return false
	}
	data, err := os.ReadFile(filepath.Join(path, skillInstallMarkerName))
	if err != nil {
		return false
	}
	var marker struct {
		Skill string `json:"skill"`
	}
	if json.Unmarshal(data, &marker) != nil {
		return false
	}
	return marker.Skill == skillName
}

// ── Output ───────────────────────────────────────────────────────────

// skillUpdateResult is the outcome of one update run.
type skillUpdateResult struct {
	Name       string
	Source     string
	Version    string
	Scope      string
	Agents     []string
	Targets    []registry.InstalledSkillTarget
	UpdatedAt  time.Time
	RecordPath string
	Warnings   []string
}

// reportSkillUpdate renders the update outcome (human or JSON).
func reportSkillUpdate(cmd *cobra.Command, result skillUpdateResult) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	if jsonOutput {
		return WriteJSON(cmd, skillUpdateJSON{
			Name:       result.Name,
			Source:     result.Source,
			Version:    result.Version,
			Scope:      result.Scope,
			Agents:     result.Agents,
			Targets:    skillTargetsJSON(result.Targets),
			UpdatedAt:  result.UpdatedAt.UTC().Format(time.RFC3339),
			RecordPath: result.RecordPath,
		})
	}
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "Updated skill: %s %s\n", result.Name, result.Version)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Source:")
	fmt.Fprintf(w, "  %s\n", result.Source)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Scope:")
	fmt.Fprintf(w, "  %s\n", result.Scope)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Agents:")
	fmt.Fprintf(w, "  %s\n", strings.Join(result.Agents, ", "))
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Targets:")
	for _, t := range result.Targets {
		fmt.Fprintf(w, "  %s (%s): %s\n", t.Agent, t.Scope, t.Path)
	}
	fmt.Fprintln(w)
	if len(result.Warnings) > 0 {
		fmt.Fprintln(w, "Warnings:")
		for _, message := range result.Warnings {
			fmt.Fprintf(w, "  %s\n", message)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, "Record:")
	fmt.Fprintf(w, "  %s\n", result.RecordPath)
	return nil
}
