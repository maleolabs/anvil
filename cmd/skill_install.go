// Package cmd implements the Anvil CLI commands.
//
// ── Skill Install (ST-021-01, ST-021-06; ADR-037 D4) ─────────────────
//
// "anvil skill install <name>" is the single core-gated install path for
// AI agent skills (no side channels). Two sources:
//
//   - Core skills materialize from the embedded set
//     (internal/skills/core/) with the provenance header
//     "source: core <cli-version>" and no external gates — the content
//     ships in the binary (ADR-037 D2).
//   - Standard skills run the full adoption pipeline (ADR-037 D4):
//     resolve the pinned standard (installed-standard record + registry
//     metadata skills[] via the TS-021-04 parser) → lifecycle +
//     compatibility gates (TS-014-04-03, pinned order) → trust anchors
//     before the fetch (fail-fast on missing/corrupt anchors) → fetch
//     the per-skill asset from the standard's release channel (hardened
//     https-only client, ADR-030 + TD-008; size-capped BEFORE any
//     extraction — security INFO-2 from T-002) → VerifyAssetDigest
//     fail-closed (TS-014-04-04; NO same-channel checksum fallback —
//     skills are a new category, ADR-037 D4) → strict bundle extraction
//     (TS-021-01) → record (TS-021-03) → report targets.
//
// Batch mode (--all) discovers every available skill (embedded core +
// skills declared by installed standards) and installs each one through
// the same gated pipeline, with per-skill failure isolation: one failing
// skill reports its error and the command continues; the exit code
// reflects any failure.
//
// Targets and scopes follow ADR-037 D5–D7 (--agent auto-detect / all,
// --scope repo|global, conflict/shadow errors with a destructive --force
// escape). Exit codes (TS-P8-07): 0 success; 3 skill or source not
// found; 2 conflict/version conflict; 4 precondition (no agent, repo
// scope without a project); 1 other (invalid release, gate failure,
// checksum mismatch, fetch/extract failure).
//
// Reference: ST-021-01, ST-021-06, ADR-037 D1–D10, TS-021-01/02/03/04
package cmd

import (
	"errors"
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

// skillInstallCmd represents the "anvil skill install" command that
// installs a skill for the given agents at the given scope.
var skillInstallCmd = &cobra.Command{
	Use:   "install [<name>]",
	Short: "Install a skill (core or standard)",
	Long: `Install one AI agent skill (Agent Skills, agentskills.io) for the
selected agents at the selected scope. Installation happens only through
this command — never implicitly and never as a side effect of another
command.

Sources (one gate):
  - core skills — shipped inside this Anvil binary (lockstep with the
    CLI version; installable offline). The installed copy carries a
    provenance header "source: core <cli-version>".
  - standard skills — provided by an installed delivery lifecycle
    standard. The install runs the full adoption pipeline: strict
    metadata parse, lifecycle + compatibility gates, trust anchors
    before the fetch, an https-only fetch of the per-skill asset from
    the standard's release channel, fail-closed verification against
    the standard's attested named-asset digest (no checksum fallback),
    strict extraction, and record. A skill can be installed only when
    its source standard is installed.

Batch mode:
  --all     install every available skill at once (embedded core +
            skills declared by installed standards). Per-skill
            failure isolation: one failing skill reports its error
            and the command continues; in batch mode any per-skill
            failure exits 1. The JSON output is a single success
            envelope with a per-skill results array.

Targets:
  --agent   the agents to install for (default: auto-detect from the
            agent config folders on this machine; "all" = every
            supported agent: claude-code, opencode, codex, gemini,
            cursor, zed, windsurf, cline)
  --scope   repo (default) — the current Anvil project's git root,
            requires an Anvil project; global — your home-level agent
            directories, no project required

A master copy lands at <scope>/.agents/skills/<name>/ for agents that
read it natively; agents with their own native locations (Claude Code,
Cursor) get a symlink (copy on Windows) to it.

Conflicts and shadows abort with actionable errors. --force overrides
them: existing same-name content at the target locations is REMOVED
first — --force is destructive. Use it only when you intend to replace
what is there.

Installing a skill that is already recorded at the same version is
idempotent (re-validated, targets refreshed). Installing a skill
recorded at a different version is rejected: version change is an
update, an explicit event of 'anvil skill update'.

Output formats:
  Default      sectioned install report (identity, scope, agents,
                targets, record path, warnings)
  --json       standard TS-P8-05 envelope on stdout, data:
                {name, source, version, scope, agents,
                already_installed, warnings, targets, record_path}
               (batch --json: data.results[] with per-skill outcomes)

Index and trust anchors resolution (standard skills only):
  1. --index <path> / the ANVIL_REGISTRY_INDEX environment variable /
     the default <user config dir>/anvil/registry
  2. --trust-anchors <path> / the ANVIL_TRUST_ANCHORS environment
     variable / the default <user config dir>/anvil/trust-anchors.json

Exit codes: 0 success; 3 skill or source standard not found; 2 conflict
or version conflict; 4 precondition (no agent detected, repo scope
without an Anvil project); 1 other errors (invalid release, retired
source, gate failure, digest mismatch, fetch/extract failure).

Examples:
  anvil skill install anvil-overview
  anvil skill install anvil-overview --scope global --agent opencode
  anvil skill install anvil-overview --agent all --force
  anvil skill install anvil-overview --json
  anvil skill install --all --scope global --agent opencode`,
	Args: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		if all {
			if len(args) > 0 {
				return fmt.Errorf("the %q command does not accept positional arguments when --all is used\nExample: anvil skill install --all", cmd.CommandPath())
			}
			return nil
		}
		if len(args) != 1 {
			return fmt.Errorf("the %q command requires 1 argument(s): <name>\nExample: anvil skill install anvil-overview", cmd.CommandPath())
		}
		return nil
	},
	SilenceUsage: true,
	RunE:         runSkillInstall,
}

func init() {
	AddJSONFlag(skillInstallCmd)
	addSkillTargetFlags(skillInstallCmd)
	skillInstallCmd.Flags().Bool("all", false, "Install every available skill (embedded core + skills declared by installed standards)")
	skillInstallCmd.Flags().String("index", "", "path to the static registry index directory (default: $ANVIL_REGISTRY_INDEX, else <user config dir>/anvil/registry)")
	skillInstallCmd.Flags().String("trust-anchors", "", "path to the trust anchors allowlist file (default: $ANVIL_TRUST_ANCHORS, else <user config dir>/anvil/trust-anchors.json)")
}

// skillInstallResult is the outcome of one install run, rendered by the
// human-readable and machine-readable surfaces.
type skillInstallResult struct {
	Name             string
	Source           string
	Version          string
	Scope            string
	Agents           []string
	AlreadyInstalled bool
	Warnings         []string
	Targets          []registry.InstalledSkillTarget
	RecordPath       string
}

// skillInstallJSON is the machine-readable install output (TS-P8-05
// data).
type skillInstallJSON struct {
	Name             string            `json:"name"`
	Source           string            `json:"source"`
	Version          string            `json:"version"`
	Scope            string            `json:"scope"`
	Agents           []string          `json:"agents"`
	AlreadyInstalled bool              `json:"already_installed"`
	Warnings         []string          `json:"warnings,omitempty"`
	Targets          []skillTargetJSON `json:"targets"`
	RecordPath       string            `json:"record_path"`
}

// skillBatchResult is the outcome of one skill within a batch install.
type skillBatchResult struct {
	Name   string
	Result *skillInstallResult
	Error  error
}

// skillBatchResultJSON is the machine-readable shape of one batch result.
type skillBatchResultJSON struct {
	Name             string            `json:"name"`
	Success          bool              `json:"success"`
	Error            string            `json:"error,omitempty"`
	ExitCode         int               `json:"exit_code,omitempty"`
	Source           string            `json:"source,omitempty"`
	Version          string            `json:"version,omitempty"`
	Scope            string            `json:"scope,omitempty"`
	Agents           []string          `json:"agents,omitempty"`
	AlreadyInstalled bool              `json:"already_installed,omitempty"`
	Warnings         []string          `json:"warnings,omitempty"`
	Targets          []skillTargetJSON `json:"targets,omitempty"`
	RecordPath       string            `json:"record_path,omitempty"`
}

// skillBatchInstallJSON is the machine-readable batch install output.
type skillBatchInstallJSON struct {
	Results []skillBatchResultJSON `json:"results"`
}

// runSkillInstall executes the install command: resolve the skill
// (core or standard), run the source's gates, materialize the targets,
// and record.
func runSkillInstall(cmd *cobra.Command, args []string) error {
	all, _ := cmd.Flags().GetBool("all")
	if all {
		return runSkillInstallAll(cmd)
	}

	name := args[0]
	if !skillbundle.ValidateName(name) {
		return skillReportError(cmd,
			fmt.Sprintf("skill name %q is invalid", name),
			"skill names match ^[a-z0-9][a-z0-9-]*$ and are at most 64 characters",
			"",
			output.ExitCodeGeneral, nil)
	}

	agents, err := skillAgents(cmd)
	if err != nil {
		return skillReportAgentError(cmd, err)
	}
	scope, err := skillScope(cmd)
	if err != nil {
		return skillReportError(cmd, "invalid --scope", err.Error(), "", output.ExitCodeGeneral, err)
	}
	force, err := skillForce(cmd)
	if err != nil {
		return err
	}

	// Repo-scope precondition classification (exit 4) via typed errors —
	// never error-string matching (MIN-3).
	if _, serr := skillScopeBase(scope); serr != nil {
		return skillReportScopeError(cmd, fmt.Sprintf("cannot resolve the %s scope", scope), serr)
	}

	// Resolve the skill: core embed first (the CLI's own set wins over a
	// same-named standard skill — the structural namespace is
	// skills/<standard-id>/<name>, ADR-037 §7), then installed standards.
	coreSkill, isCore, err := skills.Get(name)
	if err != nil {
		return skillReportError(cmd, fmt.Sprintf("could not read the embedded skill %q", name), err.Error(), "", output.ExitCodeGeneral, err)
	}

	if isCore {
		return runSkillInstallCore(cmd, name, coreSkill, scope, agents, force)
	}
	return runSkillInstallStandard(cmd, name, scope, agents, force)
}

// skillReportAgentError maps an agent-selection failure to its exit
// code via the exported detection primitive: no selectable agent on the
// machine is a precondition (4), anything else (unknown agent,
// unsupported agent) is a general error (1) — never error-string
// matching.
func skillReportAgentError(cmd *cobra.Command, err error) error {
	home, herr := os.UserHomeDir()
	if herr == nil && len(agenttarget.DetectAgentsSelectable(home)) == 0 {
		return skillReportError(cmd,
			"no supported AI agent detected",
			err.Error(),
			"Install one of the supported agents first, or pass --agent <agent> explicitly (all | claude-code | opencode | codex | gemini | cursor | zed | windsurf | cline)",
			output.ExitCodePrecondition, err)
	}
	return skillReportError(cmd, "invalid --agent", err.Error(), "", output.ExitCodeGeneral, err)
}

// ── Batch mode ───────────────────────────────────────────────────────

// runSkillInstallAll installs every available skill for the selected
// agents and scope, with per-skill failure isolation.
func runSkillInstallAll(cmd *cobra.Command) error {
	agents, err := skillAgents(cmd)
	if err != nil {
		return skillReportAgentError(cmd, err)
	}
	scope, err := skillScope(cmd)
	if err != nil {
		return skillReportError(cmd, "invalid --scope", err.Error(), "", output.ExitCodeGeneral, err)
	}
	force, err := skillForce(cmd)
	if err != nil {
		return err
	}

	if _, serr := skillScopeBase(scope); serr != nil {
		return skillReportScopeError(cmd, fmt.Sprintf("cannot resolve the %s scope", scope), serr)
	}

	names, corruptStdCount, err := discoverBatchSkills()
	if err != nil {
		return skillReportError(cmd, "could not discover available skills", err.Error(), "", output.ExitCodeGeneral, err)
	}

	if len(names) == 0 {
		reason := "no embedded core skills and no installed standards declare skills"
		if corruptStdCount > 0 {
			reason = fmt.Sprintf("no skills available — %d installed-standard record(s) could not be read", corruptStdCount)
		}
		return skillReportError(cmd,
			"no skills available to install",
			reason,
			"Install a standard that ships skills first, or check your setup",
			output.ExitCodeRuntime, nil)
	}
	if corruptStdCount > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "Note: %d installed-standard record(s) could not be read and were skipped during skill discovery\n", corruptStdCount)
	}

	jsonOutput, _ := cmd.Flags().GetBool("json")

	var results []skillBatchResult
	var anyFailed bool

	for _, name := range names {
		// Suppress per-skill stdout output in batch mode so that
		// individual skill reports do not pollute the aggregated batch
		// output (human or JSON). StepReporter progress and errors still
		// land on stderr. We set the command's own writer to discard and
		// then clear it (nil) so stdout falls back to the parent's writer
		// — this avoids pinning the command to a specific buffer across
		// test invocations (cobra does not reset subcommand writers).
		cmd.SetOut(io.Discard)
		result, installErr := installOneSkill(cmd, name, scope, agents, force)
		cmd.SetOut(nil)
		if installErr != nil {
			anyFailed = true
			results = append(results, skillBatchResult{Name: name, Error: installErr})
		} else {
			results = append(results, skillBatchResult{Name: name, Result: result})
		}
	}

	if jsonOutput {
		return reportBatchInstallJSON(cmd, results)
	}
	return reportBatchInstall(cmd, results, anyFailed)
}

// discoverBatchSkills enumerates every available skill name: embedded
// core skills first (they win over same-named standard skills, ADR-037
// §7), then skills declared by installed standards. The returned list is
// sorted and deduplicated. It also returns the count of corrupt
// installed-standard records encountered (MIN-5), so the caller can
// surface an advisory note.
func discoverBatchSkills() ([]string, int, error) {
	seen := make(map[string]bool)
	var names []string

	coreNames, err := skills.CoreSkillNames()
	if err != nil {
		return nil, 0, fmt.Errorf("discover core skills: %w", err)
	}
	for _, n := range coreNames {
		if seen[n] {
			continue
		}
		seen[n] = true
		names = append(names, n)
	}

	stdStore, err := skillStandardStore()
	if err != nil {
		return nil, 0, fmt.Errorf("discover standard skills: %w", err)
	}
	records, corrupt, err := stdStore.ListRecords()
	if err != nil {
		return nil, 0, fmt.Errorf("discover standard skills: %w", err)
	}
	for _, rec := range records {
		for _, sk := range rec.Skills {
			if seen[sk.Name] {
				continue
			}
			seen[sk.Name] = true
			names = append(names, sk.Name)
		}
	}

	sort.Strings(names)
	return names, len(corrupt), nil
}

// installOneSkill installs a single skill by name through the existing
// gated pipeline, returning the result or an error. It is the shared
// entry point used by both single-install and batch-install modes.
func installOneSkill(cmd *cobra.Command, name string, scope agenttarget.Scope, agents []agenttarget.Agent, force bool) (*skillInstallResult, error) {
	coreSkill, isCore, err := skills.Get(name)
	if err != nil {
		return nil, err
	}
	if isCore {
		return installCoreSkill(cmd, name, coreSkill, scope, agents, force)
	}
	return installStandardSkill(cmd, name, scope, agents, force)
}

// reportBatchInstall renders the human-readable batch install report.
func reportBatchInstall(cmd *cobra.Command, results []skillBatchResult, anyFailed bool) error {
	w := cmd.OutOrStdout()

	successCount := 0
	skipCount := 0
	failCount := 0
	for _, r := range results {
		switch {
		case r.Error != nil:
			failCount++
		case r.Result != nil && r.Result.AlreadyInstalled:
			skipCount++
		default:
			successCount++
		}
	}

	fmt.Fprintf(w, "Batch install complete: %d installed, %d already installed, %d failed (out of %d)\n\n",
		successCount, skipCount, failCount, len(results))

	for _, r := range results {
		if r.Error != nil {
			fmt.Fprintf(w, "  [FAIL] %s: %s\n", r.Name, r.Error.Error())
		} else if r.Result.AlreadyInstalled {
			fmt.Fprintf(w, "  [OK]   %s: already installed at %s (re-validated)\n", r.Name, r.Result.Version)
		} else {
			fmt.Fprintf(w, "  [OK]   %s: installed %s from %s\n", r.Name, r.Result.Version, r.Result.Source)
		}
	}
	fmt.Fprintln(w)

	if anyFailed {
		return skillReportError(cmd,
			"batch install completed with failures",
			fmt.Sprintf("%d of %d skills failed", failCount, len(results)),
			"Review the failures above and re-run 'anvil skill install --all' or install individual skills",
			output.ExitCodeGeneral, nil)
	}
	return nil
}

// reportBatchInstallJSON renders the machine-readable batch install
// report. It writes the data envelope to stdout and returns an error
// if any skill failed so the process exits with a non-zero code.
func reportBatchInstallJSON(cmd *cobra.Command, results []skillBatchResult) error {
	out := skillBatchInstallJSON{Results: make([]skillBatchResultJSON, 0, len(results))}
	var failCount int
	for _, r := range results {
		row := skillBatchResultJSON{Name: r.Name, Success: r.Error == nil}
		if r.Error != nil {
			failCount++
			row.Error = r.Error.Error()
			row.ExitCode = skillInstallExitCode(r.Error)
		} else if r.Result != nil {
			row.Source = r.Result.Source
			row.Version = r.Result.Version
			row.Scope = r.Result.Scope
			row.Agents = r.Result.Agents
			row.AlreadyInstalled = r.Result.AlreadyInstalled
			row.Warnings = r.Result.Warnings
			row.Targets = skillTargetsJSON(r.Result.Targets)
			row.RecordPath = r.Result.RecordPath
		}
		out.Results = append(out.Results, row)
	}
	if err := WriteJSON(cmd, out); err != nil {
		return err
	}
	if failCount > 0 {
		return fmt.Errorf("batch install completed with %d failure(s) out of %d", failCount, len(results))
	}
	return nil
}

// skillInstallExitCode extracts the deterministic exit code from a
// per-skill install error. If the error carries an *output.AppError,
// its exit code is used; otherwise the fallback is general (1).
func skillInstallExitCode(err error) int {
	if err == nil {
		return output.ExitCodeSuccess
	}
	var appErr *output.AppError
	if errors.As(err, &appErr) {
		return appErr.ExitCode()
	}
	return output.ExitCodeGeneral
}

// ── Core path ────────────────────────────────────────────────────────

// runSkillInstallCore materializes an embedded core skill: validate the
// portable frontmatter, inject the provenance header, write the agent
// targets, and record. No external gates — the content ships in the
// binary (ADR-037 D2).
func runSkillInstallCore(cmd *cobra.Command, name string, core skills.CoreSkill, scope agenttarget.Scope, agents []agenttarget.Agent, force bool) error {
	result, err := installCoreSkill(cmd, name, core, scope, agents, force)
	if err != nil {
		return err
	}
	return reportSkillInstall(cmd, result)
}

// installCoreSkill does the actual core install work and returns the
// result. It is the internal entry point shared by single-install and
// batch-install modes.
func installCoreSkill(cmd *cobra.Command, name string, core skills.CoreSkill, scope agenttarget.Scope, agents []agenttarget.Agent, force bool) (*skillInstallResult, error) {
	reporter := output.NewStepReporter(cmd.ErrOrStderr())
	overallStart := time.Now()
	reporter.Start(fmt.Sprintf("Install skill %s (core)", name))
	reporter.SetTotal(3)

	// Step 1: validate the embedded SKILL.md (portable fields, name
	// match) and inject the provenance header.
	var files map[string][]byte
	err := skillStep(reporter, "Validate embedded skill", func() error {
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
		reporter.Failed(fmt.Sprintf("Install skill %s (core)", name), time.Since(overallStart))
		return nil, skillReportError(cmd, fmt.Sprintf("the embedded core skill %q is invalid", name), err.Error(), "Fix the embedded skill content (internal/skills/core/) or report the broken CLI build", output.ExitCodeGeneral, err)
	}

	// Step 2: version preflight BEFORE any side effect (P2) — a record at
	// a different version is rejected early with exit 2 and nothing is
	// written or fetched.
	store, err := skillStore()
	if err != nil {
		reporter.Failed(fmt.Sprintf("Install skill %s (core)", name), time.Since(overallStart))
		return nil, skillReportStoreError(cmd, "install", name, err)
	}
	existing, err := skillPreflightExisting(store, name, CliVersion)
	if err != nil {
		reporter.Failed(fmt.Sprintf("Install skill %s (core)", name), time.Since(overallStart))
		return nil, skillReportStoreError(cmd, "install", name, err)
	}

	// Step 3: materialize the agent targets. A fresh install writes the
	// resolved set; a re-install of the SAME version refreshes the target
	// set like an update — targets dropped by a narrower --agent/--scope
	// are removed (containment + ownership checked), never orphaned (P3).
	var set *agenttarget.ResolvedSet
	err = skillStep(reporter, "Install targets", func() error {
		if existing != nil {
			refreshed, rerr := skillRefreshTargets(cmd.ErrOrStderr(), scope, name, files, agents, force, *existing)
			set = refreshed
			return rerr
		}
		installed, ierr := (&agenttarget.Installer{}).Install(scope, name, files, agents, force)
		set = installed
		return ierr
	})
	if err != nil {
		reporter.Failed(fmt.Sprintf("Install skill %s (core)", name), time.Since(overallStart))
		return nil, skillReportMaterializeError(cmd, name, err)
	}

	// Step 4: record — Update (installedAt preserved) for a re-install,
	// Record for a fresh install.
	already := existing != nil
	err = skillStep(reporter, "Record", func() error {
		timestamp := now()
		rec := registry.InstalledSkillRecord{
			FormatVersion: registry.InstalledSkillRecordFormatVersion,
			ID:            name,
			Version:       CliVersion,
			Source:        registry.SkillSourceCore,
			Resolution: registry.Resolution{
				Kind:   registry.SkillResolutionKindCore,
				Source: "embedded",
			},
			InstalledAt: timestamp,
			UpdatedAt:   timestamp,
			Targets:     targetsFromResolvedSet(set),
		}
		if existing != nil {
			rec.InstalledAt = existing.InstalledAt
			_, rerr := store.Update(name, rec)
			return rerr
		}
		var rerr error
		already, rerr = recordSkillInstall(store, name, rec)
		return rerr
	})
	if err != nil {
		reporter.Failed(fmt.Sprintf("Install skill %s (core)", name), time.Since(overallStart))
		return nil, skillReportStoreError(cmd, "install", name, err)
	}

	reporter.Complete(fmt.Sprintf("Installed %s", name), time.Since(overallStart))

	return &skillInstallResult{
		Name:             name,
		Source:           registry.SkillSourceCore,
		Version:          CliVersion,
		Scope:            string(scope),
		Agents:           agentIDs(agents),
		AlreadyInstalled: already,
		Targets:          targetsFromResolvedSet(set),
		RecordPath:       storeRecordPath(store, name),
	}, nil
}

// ── Standard path ────────────────────────────────────────────────────

// runSkillInstallStandard installs a standard-sourced skill through the
// full adoption pipeline (ADR-037 D4): resolve pinned standard →
// lifecycle+compat gates → trust anchors before fetch → hardened fetch
// (size-capped) → VerifyAssetDigest fail-closed → strict extract →
// materialize targets → record.
func runSkillInstallStandard(cmd *cobra.Command, name string, scope agenttarget.Scope, agents []agenttarget.Agent, force bool) error {
	result, err := installStandardSkill(cmd, name, scope, agents, force)
	if err != nil {
		return err
	}
	return reportSkillInstall(cmd, result)
}

// installStandardSkill does the actual standard-skill install work and
// returns the result. It is the internal entry point shared by
// single-install and batch-install modes.
func installStandardSkill(cmd *cobra.Command, name string, scope agenttarget.Scope, agents []agenttarget.Agent, force bool) (*skillInstallResult, error) {
	reporter := output.NewStepReporter(cmd.ErrOrStderr())
	overallStart := time.Now()
	reporter.Start(fmt.Sprintf("Install skill %s", name))
	reporter.SetTotal(7)

	// Step 1: resolve the pinned source standard + skill declaration.
	var match *skillStandardMatch
	var notes skillResolutionNotes
	err := skillStep(reporter, "Resolve standard skill", func() error {
		m, n, rerr := resolveStandardSkill(cmd, name)
		match = m
		notes = n
		return rerr
	})
	if err != nil {
		reporter.Failed(fmt.Sprintf("Install skill %s", name), time.Since(overallStart))
		// A genuinely absent skill is "not found" (3); an index/store/
		// ambiguity problem is an environment error (1) — MIN-4.
		exitCode := output.ExitCodeRuntime
		if !skillResolutionNotFound(err) {
			exitCode = output.ExitCodeGeneral
		}
		return nil, skillReportError(cmd,
			fmt.Sprintf("skill %q is not installable", name),
			err.Error(),
			"Install a standard that ships the skill first (see 'anvil standard list'), or check the skill name",
			exitCode, err)
	}
	// Advisory hints when standards were skipped (MIN-5, F-4).
	skillReportResolutionNotes(cmd, notes)

	// Step 2: version preflight BEFORE any fetch or extract (P2): a
	// record at a different version is rejected early with exit 2 and
	// the asset is never downloaded. A record from a DIFFERENT source
	// standard is a different skill — uninstall and re-install to adopt
	// it (never a silent re-parent).
	store, err := skillStore()
	if err != nil {
		reporter.Failed(fmt.Sprintf("Install skill %s", name), time.Since(overallStart))
		return nil, skillReportStoreError(cmd, "install", name, err)
	}
	existing, err := skillPreflightExisting(store, name, match.Skill.Version)
	if err != nil {
		reporter.Failed(fmt.Sprintf("Install skill %s", name), time.Since(overallStart))
		return nil, skillReportStoreError(cmd, "install", name, err)
	}
	if existing != nil && existing.Source != match.Metadata.ID {
		reporter.Failed(fmt.Sprintf("Install skill %s", name), time.Since(overallStart))
		return nil, skillReportError(cmd,
			fmt.Sprintf("skill %q is recorded from %s but is now declared by %s", name, existing.Source, match.Metadata.ID),
			"a skill record keeps its source identity; a changed source is a different skill",
			fmt.Sprintf("Run 'anvil skill uninstall %s' then 'anvil skill install %s' to adopt the new source", name, name),
			output.ExitCodeGeneral, nil)
	}

	// Step 3: lifecycle + compatibility gates (pinned order).
	var warnings []string
	err = skillStep(reporter, "Validate release", func() error {
		_, w, gerr := skillAdoptionGates(cmd, &match.Metadata, false)
		warnings = w
		return gerr
	})
	if err != nil {
		reporter.Failed(fmt.Sprintf("Install skill %s", name), time.Since(overallStart))
		return nil, skillReportError(cmd,
			fmt.Sprintf("skill %q cannot be installed from source standard %s %s", name, match.Metadata.ID, match.Metadata.Version),
			err.Error(),
			"Choose another standard or report the broken release to its publisher",
			output.ExitCodeGeneral, err)
	}

	// Step 3: trust anchors BEFORE the fetch — load the allowlist
	// (fail-fast on missing/corrupt anchors) AND verify the publisher
	// attestation + anchor match of the resolved metadata
	// (registry.VerifyAttestationAnchored, ADR-037 D4: trust anchors
	// before fetch). A metadata document not signed by an anchored
	// publisher — e.g. a tampered index — aborts before any download.
	err = skillStep(reporter, "Verify trust anchors", func() error {
		anchorsPath, aerr := standardTrustAnchorsPath(cmd)
		if aerr != nil {
			return aerr
		}
		anchors, aerr := registry.LoadTrustAnchors(anchorsPath)
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
		reporter.Failed(fmt.Sprintf("Install skill %s", name), time.Since(overallStart))
		return nil, skillReportError(cmd,
			"trust verification failed for the source standard",
			err.Error(),
			"Do not install content that fails verification; configure the correct trust anchors (--trust-anchors <path> or "+registry.EnvTrustAnchors+"), or report the broken release to its publisher",
			output.ExitCodeGeneral, err)
	}

	// Step 4: fetch the skill asset from the standard's release channel
	// (hardened client; size-capped BEFORE extraction).
	var content []byte
	var sha256Hex string
	var contentSource string
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
		reporter.Failed(fmt.Sprintf("Install skill %s", name), time.Since(overallStart))
		return nil, skillReportError(cmd,
			fmt.Sprintf("could not fetch the skill asset of %q from %s", name, match.Metadata.ID),
			err.Error(),
			"If you are the publisher, fix the release asset; otherwise report the broken release",
			output.ExitCodeGeneral, err)
	}

	// Step 5: VerifyAssetDigest — fail-closed (TS-014-04-04; ADR-037 D4:
	// no SHA256SUMS fallback for skills).
	err = skillStep(reporter, "Verify asset digest", func() error {
		attested, verr := registry.VerifyAssetDigest(match.Metadata, match.Skill.Asset, sha256Hex)
		if verr != nil {
			return verr
		}
		if !attested {
			// A parser-valid document always declares a digest for every
			// declared skill asset (the binding is enforced at parse,
			// TS-021-04) — reaching here means the release is broken or
			// was published without the material. Skills are a new
			// category: no same-channel checksum fallback, ever.
			return fmt.Errorf(
				"release %s %s declares no attestation-bound digest for skill asset %q — skills are verified against the attested named digest only, with no checksum fallback (ADR-037 D4); obtain a fresh release from the publisher",
				match.Metadata.ID, match.Metadata.Version, match.Skill.Asset)
		}
		return nil
	})
	if err != nil {
		reporter.Failed(fmt.Sprintf("Install skill %s", name), time.Since(overallStart))
		return nil, skillReportError(cmd,
			fmt.Sprintf("digest verification failed for the skill asset of %q", name),
			err.Error(),
			"Do not install content that fails verification; re-fetch the asset or report the broken release to the publisher",
			output.ExitCodeGeneral, err)
	}

	// Step 6: strict extraction into a staging directory.
	var files map[string][]byte
	staging, err := os.MkdirTemp("", "anvil-skill-*")
	if err != nil {
		reporter.Failed(fmt.Sprintf("Install skill %s", name), time.Since(overallStart))
		return nil, skillReportError(cmd, "could not create a staging directory", err.Error(), "", output.ExitCodeGeneral, err)
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
		reporter.Failed(fmt.Sprintf("Install skill %s", name), time.Since(overallStart))
		return nil, skillReportError(cmd,
			fmt.Sprintf("the skill bundle of %q is rejected by the strict extraction", name),
			err.Error(),
			"Obtain a fresh copy of the bundle from the publisher, or report the broken release",
			output.ExitCodeGeneral, err)
	}

	// Step 7: materialize targets + record. A fresh install writes the
	// resolved set; a re-install of the SAME version refreshes the target
	// set like an update — dropped targets are removed, never orphaned
	// (P3).
	var set *agenttarget.ResolvedSet
	err = skillStep(reporter, "Install targets", func() error {
		if existing != nil {
			refreshed, rerr := skillRefreshTargets(cmd.ErrOrStderr(), scope, name, files, agents, force, *existing)
			set = refreshed
			return rerr
		}
		installed, ierr := (&agenttarget.Installer{}).Install(scope, name, files, agents, force)
		set = installed
		return ierr
	})
	if err != nil {
		reporter.Failed(fmt.Sprintf("Install skill %s", name), time.Since(overallStart))
		return nil, skillReportMaterializeError(cmd, name, err)
	}

	already := existing != nil
	err = skillStep(reporter, "Record", func() error {
		timestamp := now()
		rec := registry.InstalledSkillRecord{
			FormatVersion: registry.InstalledSkillRecordFormatVersion,
			ID:            name,
			Version:       match.Skill.Version,
			Source:        match.Metadata.ID,
			Resolution: registry.Resolution{
				Kind:   registry.SkillResolutionKindDistribution,
				Source: contentSource,
			},
			InstalledAt: timestamp,
			UpdatedAt:   timestamp,
			Targets:     targetsFromResolvedSet(set),
		}
		if existing != nil {
			rec.InstalledAt = existing.InstalledAt
			_, rerr := store.Update(name, rec)
			return rerr
		}
		var rerr error
		already, rerr = recordSkillInstall(store, name, rec)
		return rerr
	})
	if err != nil {
		reporter.Failed(fmt.Sprintf("Install skill %s", name), time.Since(overallStart))
		return nil, skillReportStoreError(cmd, "install", name, err)
	}

	reporter.Complete(fmt.Sprintf("Installed %s", name), time.Since(overallStart))

	return &skillInstallResult{
		Name:             name,
		Source:           match.Metadata.ID,
		Version:          match.Skill.Version,
		Scope:            string(scope),
		Agents:           agentIDs(agents),
		AlreadyInstalled: already,
		Warnings:         warnings,
		Targets:          targetsFromResolvedSet(set),
		RecordPath:       storeRecordPath(store, name),
	}, nil
}

// ── Shared install tail ──────────────────────────────────────────────

// recordSkillInstall persists an installed-skill record after a
// successful materialization (TS-021-03 semantics):
//
//   - fresh record → created, alreadyInstalled=false;
//   - same version recorded → the record is Updated with the refreshed
//     targets (installedAt preserved; re-adoption event), returns
//     alreadyInstalled=true (mirrors the standard install's idempotent
//     re-validated re-install);
//   - different version recorded → ErrSkillRecordVersionConflict — an
//     install is never a version change; update is the explicit event.
func recordSkillInstall(store *registry.InstalledSkillStore, name string, rec registry.InstalledSkillRecord) (alreadyInstalled bool, err error) {
	_, created, err := store.Record(name, rec)
	switch {
	case err == nil && created:
		return false, nil
	case err == nil:
		// Idempotent re-install at the same version: refresh the targets
		// via Update (preserves installedAt; the store enforces it).
		if _, uerr := store.Update(name, rec); uerr != nil {
			return false, fmt.Errorf("install skill %q: refresh the record: %w", name, uerr)
		}
		return true, nil
	case errors.Is(err, registry.ErrSkillRecordVersionConflict):
		return false, fmt.Errorf(
			"%w: skill %q is recorded at a different version — install never changes versions; run 'anvil skill update %s' to re-adopt",
			err, name, name)
	default:
		return false, err
	}
}

// skillReportMaterializeError maps a materialization failure (conflict,
// shadow, or write error) to the right category: conflicts and shadows
// are exit 2 with the --force hint; a scope precondition (repo scope
// without a project/git root) is exit 4; anything else is a general
// error.
func skillReportMaterializeError(cmd *cobra.Command, name string, err error) error {
	var blocked *agenttarget.InstallBlockedError
	if errors.As(err, &blocked) {
		return skillReportError(cmd,
			fmt.Sprintf("skill %q install is blocked", name),
			blocked.Error(),
			"Remove the conflicting content, or run with --force to replace it (destructive)",
			output.ExitCodeConfig, err)
	}
	if exitCode := skillScopeExitCode(err); exitCode != output.ExitCodeGeneral {
		return skillReportScopeError(cmd, fmt.Sprintf("cannot resolve the install scope for skill %q", name), err)
	}
	return skillReportError(cmd,
		fmt.Sprintf("could not install skill %q", name),
		err.Error(),
		"",
		output.ExitCodeGeneral, err)
}

// agentIDs renders the agent set as their canonical IDs, in the order
// given (the table order of the parsed flag).
func agentIDs(agents []agenttarget.Agent) []string {
	ids := make([]string, 0, len(agents))
	for _, a := range agents {
		ids = append(ids, a.ID)
	}
	return ids
}

// storeRecordPath returns the record file path for a skill in a store
// (the store's per-skill file layout, <dir>/<id>.json — TS-021-03).
func storeRecordPath(store *registry.InstalledSkillStore, name string) string {
	return filepath.Join(store.Dir(), name+".json")
}

// skillStep runs one reporter step.
func skillStep(reporter output.StepReporter, name string, fn func() error) error {
	reporter.StepStart(name)
	start := time.Now()
	err := fn()
	if err != nil {
		reporter.StepFailed(name, time.Since(start), err)
	} else {
		reporter.StepComplete(name, time.Since(start))
	}
	return err
}

// reportSkillInstall renders the install outcome (human or JSON).
func reportSkillInstall(cmd *cobra.Command, result *skillInstallResult) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	if jsonOutput {
		return WriteJSON(cmd, skillInstallJSON{
			Name:             result.Name,
			Source:           result.Source,
			Version:          result.Version,
			Scope:            result.Scope,
			Agents:           result.Agents,
			AlreadyInstalled: result.AlreadyInstalled,
			Warnings:         result.Warnings,
			Targets:          skillTargetsJSON(result.Targets),
			RecordPath:       result.RecordPath,
		})
	}
	renderSkillInstall(cmd, result)
	return nil
}

// renderSkillInstall writes the human-readable install report.
func renderSkillInstall(cmd *cobra.Command, result *skillInstallResult) {
	w := cmd.OutOrStdout()
	if result.AlreadyInstalled {
		fmt.Fprintf(w, "Skill %s is already installed at version %s (re-validated).\n", result.Name, result.Version)
	} else {
		fmt.Fprintf(w, "Installed skill: %s %s\n", result.Name, result.Version)
	}
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
}
