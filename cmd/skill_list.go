// Package cmd implements the Anvil CLI commands.
//
// ── Skill List (ST-021-01, ST-021-04; ADR-037 D3/D8) ─────────────────
//
// "anvil skill list" shows the skill universe: the embedded core skills
// (available or installed), the standard skills declared by the
// installed-standard records (available, installed, or unavailable), every
// installed skill (core or standard-sourced) with its stale status and
// target paths, and any unreadable record files (never silently dropped).
//
// The standard section iterates the installed-standard records and their
// Skills declarations (ST-021-04 — the record IS the skill registry,
// ADR-037 D3): a declared skill that is not recorded as installed is
// shown as "available" (installable), with its source standard and
// declared version. A declared skill of a RETIRED standard is shown as
// "unavailable" with an actionable message — retired releases are not
// offered for fresh adoption (ADR-027 §3, D4 gates); a DEPRECATED
// standard's skills stay available (install proceeds with a warning,
// ADR-023 §3) with a deprecation hint. Standard uninstall removes the
// record, so its declared-but-not-installed skills disappear from the
// listing; installed ones stay, flagged stale by TS-021-03.
//
// Status semantics (TS-021-03): available = not recorded as installed;
// installed = recorded and current; stale = recorded but out of date (core
// skill version skew vs the CLI version, or a missing/deprecated/retired
// source standard) with actionable hints; unavailable = declared by a
// retired standard, not offered for installation. Stale records are kept —
// never silently deleted.
//
// Reference: ST-021-01, ST-021-04, ADR-037 D3/D8, TS-021-03, TS-P8-05
package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/registry"
	"maleolabs.com/anvil/internal/skills"
)

// skillListCmd represents the "anvil skill list" command that displays
// the embedded core skills and installed skills.
var skillListCmd = &cobra.Command{
	Use:   "list",
	Short: "List embedded core skills and installed skills",
	Long: `List the skills available to install and the skills already installed.

Two sections are shown:

  Core skills    the skills shipped inside this Anvil binary
                 (lockstep with the CLI version). Each is
                 "available" (not installed), "installed", or "stale"
                 (installed by an older CLI — run 'anvil skill update'
                 to refresh).
  Standard skills the skills of the installed delivery lifecycle
                 standards — the installed-standard record IS the
                 skill registry (ADR-037 D3). Each is "available"
                 (declared by an installed standard, not installed),
                 "installed", "stale" (recorded but out of date: the
                 source standard is missing, deprecated, retired, or
                 corrupt), or "unavailable" (declared by a retired
                 standard — not offered for installation, ADR-027 §3).
                 A deprecated source standard's skills stay available
                 with a deprecation hint. Each standard's skills live
                 under its own namespace (skills/<standard-id>/<name>),
                 so the same skill name declared by two standards
                 yields one row per source.

Installed entries list every target path (agent + scope + path). Stale
and unavailable entries carry actionable hints and are never deleted
automatically. Unreadable installed-skill records AND unreadable
installed-standard records (whose declared skills cannot be enumerated)
are reported, never silently dropped.

Output formats:
  Default      sectioned listing (Name, Version, Status, Paths)
  --json       standard TS-P8-05 envelope on stdout, data:
               {cli_version, skills: [{name, source, version, status,
               installed_at, targets: [{agent, scope, path}], hints}],
               corrupt_records: [{path, error}],
               corrupt_standard_records: [{path, error}]}

Exit codes: 0 success (including listings that surface stale,
unavailable, or unreadable records); 1 store/core-set read failure.

This is a read-only command — it does not modify any state.

Examples:
  anvil skill list
  anvil skill list --json`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runSkillList,
}

func init() {
	AddJSONFlag(skillListCmd)
}

// skillListEntry is one row of the listing: identity, source, installed
// version, status, install timestamp, target paths, and stale hints.
type skillListEntry struct {
	Name        string
	Source      string
	Version     string
	Status      string // available | installed | stale | unavailable
	InstalledAt string
	Targets     []registry.InstalledSkillTarget
	Hints       []string
}

// skillListJSON is the machine-readable listing shape (TS-P8-05 data).
type skillListJSON struct {
	CLIVersion             string               `json:"cli_version"`
	Skills                 []skillListRow       `json:"skills"`
	CorruptRecords         []corruptSkillRecord `json:"corrupt_records,omitempty"`
	CorruptStandardRecords []corruptSkillRecord `json:"corrupt_standard_records,omitempty"`
}

// corruptSkillRecord is one installed-skills record file that could not
// be read during the listing (MIN-1/F-5: unreadable records surface in
// --json too, never silently dropped).
type corruptSkillRecord struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

// skillListRow is one machine-readable skill entry.
type skillListRow struct {
	Name        string            `json:"name"`
	Source      string            `json:"source"`
	Version     string            `json:"version"`
	Status      string            `json:"status"`
	InstalledAt string            `json:"installed_at,omitempty"`
	Targets     []skillTargetJSON `json:"targets,omitempty"`
	Hints       []string          `json:"hints,omitempty"`
}

// runSkillList executes the list command: the embedded core set plus
// every installed-skill record with its stale status, merged with the
// standard skills declared by the installed-standard records (available /
// unavailable), into a stable listing (core first, then standard,
// alphabetical within each group).
func runSkillList(cmd *cobra.Command, args []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	cliVersion := CliVersion

	coreSet, err := skills.ListCoreSkills()
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not read the embedded core skills: %v", err)
	}

	store, err := skillStore()
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not open the installed-skills store: %v", err)
	}
	stdStore, err := skillStandardStore()
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not open the installed-standards store: %v", err)
	}
	statuses, corrupt, err := store.ListStatuses(cliVersion, stdStore)
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not read the installed skills: %v", err)
	}
	// The standard skill declarations live on the full installed-standard
	// records (ST-021-04 — the record IS the skill registry, ADR-037 D3):
	// enumerate them for the available/unavailable rows. Unreadable
	// standard records are surfaced, never silently dropped.
	stdRecords, corruptStd, err := stdStore.ListRecords()
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not read the installed standards: %v", err)
	}

	// Best-effort lifecycle cross-check source for the declared rows: the
	// registry index metadata of the source standard (the same document
	// the install gate reads, ADR-037 D4). The record's lifecycle is
	// frozen at adoption; the index metadata is the current truth when it
	// resolves. An unavailable index (or an unresolvable pinned version)
	// is NOT a listing failure — the declared rows fall back to the
	// record's lifecycle with a verify hint (PM decision, reviewer #1 /
	// security LOW-1).
	var stdIndex *registry.Index
	if indexPath, ierr := standardIndexPath(cmd); ierr == nil {
		if loaded, lerr := registry.LoadIndex(indexPath); lerr == nil {
			stdIndex = loaded
		}
	}

	entries := collectSkillListEntries(coreSet, statuses, stdRecords, stdIndex, cliVersion)

	if jsonOutput {
		return WriteJSON(cmd, skillListJSONFromEntries(cliVersion, entries, corrupt, corruptStd))
	}
	renderSkillList(cmd, entries, corrupt, corruptStd)
	return nil
}

// collectSkillListEntries merges the embedded core set, the installed
// records, and the installed-standard record declarations into the
// listing:
//
//   - core section: every embedded core skill (available/installed/stale
//     from its record) plus any recorded core-sourced skill no longer in
//     the embedded set (it is installed and, with a version skew, stale);
//   - standard section: every recorded standard-sourced skill
//     (installed/stale) plus every skill DECLARED by an installed
//     standard record that is not recorded from that same standard —
//     "available" (installable), or "unavailable" with an actionable
//     hint when its source standard is retired (ADR-027 §3, D4 gates);
//     a deprecated source standard's declared skills stay available with
//     a deprecation hint (install proceeds with a warning, ADR-023 §3).
//     Each standard's skills live under its own namespace
//     (skills/<standard-id>/<name>), so the same skill name declared by
//     two standards yields one row per source.
//
// stdIndex is the best-effort registry index used to cross-check the
// declared rows' lifecycle against the current release metadata (the
// record's lifecycle is frozen at adoption); nil falls back to the record
// lifecycle with a verify hint.
func collectSkillListEntries(coreSet []skills.CoreSkill, statuses []registry.InstalledSkillStatus, stdRecords []registry.InstalledStandardRecord, stdIndex *registry.Index, cliVersion string) []skillListEntry {
	// Records indexed by name.
	byName := make(map[string]registry.InstalledSkillStatus, len(statuses))
	for _, s := range statuses {
		byName[s.Record.ID] = s
	}

	var core []skillListEntry
	seenCore := map[string]bool{}
	for _, c := range coreSet {
		seenCore[c.Name] = true
		core = append(core, skillEntryFromCore(c.Name, cliVersion, byName[c.Name]))
	}
	// Recorded core-sourced skills that are no longer in the embedded
	// set: still listed as installed (the CLI removed/renamed the skill).
	for name, st := range byName {
		if st.Record.Source != registry.SkillSourceCore || seenCore[name] {
			continue
		}
		core = append(core, skillEntryFromRecord(st))
	}
	sort.Slice(core, func(i, j int) bool { return core[i].Name < core[j].Name })

	var standard []skillListEntry
	// Installed standard-sourced skills (recorded; stale hints from
	// TS-021-03). A declaration whose skill is already recorded from the
	// SAME standard is represented by this row — no available duplicate.
	recorded := make(map[string]bool) // "source\x00name" of recorded standard-sourced skills
	for _, st := range byName {
		if st.Record.Source == registry.SkillSourceCore {
			continue
		}
		standard = append(standard, skillEntryFromRecord(st))
		recorded[st.Record.Source+"\x00"+st.Record.ID] = true
	}
	// Declared-but-not-installed standard skills (available/unavailable)
	// from the installed-standard records — the record is the registry
	// (ST-021-04 / ADR-037 D3).
	for _, rec := range stdRecords {
		for _, sk := range rec.Skills {
			if recorded[rec.ID+"\x00"+sk.Name] {
				continue
			}
			standard = append(standard, skillEntryFromDeclaration(rec, sk, stdIndex))
		}
	}
	sort.Slice(standard, func(i, j int) bool { return standard[i].Name < standard[j].Name })

	return append(core, standard...)
}

// skillEntryFromDeclaration builds the listing entry for a skill declared
// by an installed standard record but not recorded as installed. The
// status follows the D4 gates and ADR-027 §3: a retired source standard's
// skills are "unavailable" with an actionable message; a deprecated
// source standard's skills stay "available" (install proceeds with a
// warning, ADR-023 §3) with a deprecation hint.
//
// The lifecycle is cross-checked against the registry index when
// stdIndex is given and resolves the standard's pinned version (the
// record's lifecycle is frozen at adoption; the index metadata is the
// current truth — reviewer #1 / security LOW-1). When the index is
// unavailable or the pinned version is unresolvable, the record's frozen
// lifecycle is used and a verify hint is added.
func skillEntryFromDeclaration(rec registry.InstalledStandardRecord, sk registry.SkillDeclaration, stdIndex *registry.Index) skillListEntry {
	entry := skillListEntry{
		Name:    sk.Name,
		Source:  rec.ID,
		Version: sk.Version,
		Status:  "available",
	}

	lifecycleState := rec.Lifecycle.State
	removalDate := strings.TrimSpace(rec.Lifecycle.RemovalDate)
	indexResolved := false
	if stdIndex != nil {
		if stdEntry, err := stdIndex.Resolve(rec.ID, rec.Version); err == nil {
			if md, _, err := parseStandardEntry(stdEntry); err == nil {
				lifecycleState = md.Lifecycle.State
				removalDate = strings.TrimSpace(md.Lifecycle.RemovalDate)
				indexResolved = true
			}
		}
	}

	switch lifecycleState {
	case registry.LifecycleStateRetired:
		entry.Status = "unavailable"
		entry.Hints = append(entry.Hints, fmt.Sprintf(
			"source standard %s is retired — its skills are not offered for installation (ADR-027 §3); re-adopt a supported release of the standard or uninstall it",
			rec.ID))
	case registry.LifecycleStateDeprecated:
		removalPhrase := "no removal date announced"
		if removalDate != "" {
			removalPhrase = "removal " + removalDate
		}
		entry.Hints = append(entry.Hints, fmt.Sprintf(
			"source standard %s is deprecated (%s) — installation proceeds with a warning and the installed skill receives no updates once the standard is retired (ADR-023 §3)",
			rec.ID, removalPhrase))
	}
	if !indexResolved {
		entry.Hints = append(entry.Hints, fmt.Sprintf(
			"the registry index could not be resolved for %s %s — the lifecycle shown is the installed record's; verify with the registry index before installing this skill",
			rec.ID, rec.Version))
	}
	return entry
}

// skillEntryFromCore builds the listing entry for an embedded core skill
// (available when not recorded, otherwise installed/stale from the
// record).
func skillEntryFromCore(name, cliVersion string, st registry.InstalledSkillStatus) skillListEntry {
	entry := skillListEntry{
		Name:    name,
		Source:  registry.SkillSourceCore,
		Version: cliVersion,
		Status:  "available",
	}
	if st.Record.ID == name {
		entry = skillEntryFromRecord(st)
	}
	return entry
}

// skillEntryFromRecord builds the listing entry from an installed record
// (installed/stale with targets, timestamp, and hints).
func skillEntryFromRecord(st registry.InstalledSkillStatus) skillListEntry {
	status := "installed"
	if st.Stale {
		status = "stale"
	}
	return skillListEntry{
		Name:        st.Record.ID,
		Source:      st.Record.Source,
		Version:     st.Record.Version,
		Status:      status,
		InstalledAt: st.Record.InstalledAt.UTC().Format(time.RFC3339),
		Targets:     st.Record.Targets,
		Hints:       st.Hints,
	}
}

// skillListJSONFromEntries converts the listing into the machine-readable
// shape, including unreadable record files (MIN-1/F-5) and unreadable
// installed-standard records (their declared skills cannot be enumerated —
// surfaced, never silently dropped).
func skillListJSONFromEntries(cliVersion string, entries []skillListEntry, corrupt []registry.CorruptSkillRecord, corruptStd []registry.CorruptRecord) skillListJSON {
	out := skillListJSON{CLIVersion: cliVersion}
	for _, e := range entries {
		out.Skills = append(out.Skills, skillListRow{
			Name:        e.Name,
			Source:      e.Source,
			Version:     e.Version,
			Status:      e.Status,
			InstalledAt: e.InstalledAt,
			Targets:     skillTargetsJSON(e.Targets),
			Hints:       e.Hints,
		})
	}
	for _, c := range corrupt {
		out.CorruptRecords = append(out.CorruptRecords, corruptSkillRecord{Path: c.Path, Error: c.Error})
	}
	for _, c := range corruptStd {
		out.CorruptStandardRecords = append(out.CorruptStandardRecords, corruptSkillRecord{Path: c.Path, Error: c.Error})
	}
	return out
}

// renderSkillList prints the human-readable listing: a Core Skills
// section, a Standard Skills section, a notices section (stale hints,
// unavailable-skill and deprecation notices), and a note for unreadable
// records.
func renderSkillList(cmd *cobra.Command, entries []skillListEntry, corrupt []registry.CorruptSkillRecord, corruptStd []registry.CorruptRecord) {
	if len(entries) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No skills available.")
		fmt.Fprintln(cmd.OutOrStdout(), "Core skills ship inside the Anvil binary; standard skills come from installed standards.")
		return
	}

	w := cmd.OutOrStdout()
	var coreRows, standardRows [][]string
	var notices []string
	for _, e := range entries {
		if e.Source == registry.SkillSourceCore {
			coreRows = append(coreRows, []string{e.Name, e.Version, e.Status, skillTargetsCell(e)})
		} else {
			standardRows = append(standardRows, []string{e.Name, e.Source, e.Version, e.Status, skillTargetsCell(e)})
		}
		for _, h := range e.Hints {
			notices = append(notices, fmt.Sprintf("%s (%s): %s", e.Name, e.Source, h))
		}
	}

	// Sections are separated by a blank line so later group headers do not
	// stick to the output above them; the first section starts the listing.
	first := true
	section := func(title string) {
		if !first {
			fmt.Fprintln(w, "")
		}
		PrintSection(cmd, title)
		first = false
	}

	if len(coreRows) > 0 {
		section("Core Skills")
		PrintTable(cmd, []string{"Name", "Version", "Status", "Targets"}, coreRows)
	}
	if len(standardRows) > 0 {
		section("Standard Skills")
		PrintTable(cmd, []string{"Name", "Source", "Version", "Status", "Targets"}, standardRows)
	}
	if len(notices) > 0 {
		section("Notices")
		for _, n := range notices {
			fmt.Fprintf(w, "  %s\n", n)
		}
	}
	if len(corrupt) > 0 || len(corruptStd) > 0 {
		section("Unreadable Records")
		for _, c := range corrupt {
			fmt.Fprintf(w, "  %s: %s\n", c.Path, c.Error)
		}
		for _, c := range corruptStd {
			fmt.Fprintf(w, "  %s: %s\n", c.Path, c.Error)
		}
		fmt.Fprintln(w, "  Re-install the skill to recover, or delete the record file.")
	}
}

// skillTargetsCell renders the targets of one entry as a compact
// "agent@scope:path" list.
func skillTargetsCell(e skillListEntry) string {
	paths := make([]string, 0, len(e.Targets))
	for _, t := range e.Targets {
		paths = append(paths, fmt.Sprintf("%s@%s:%s", t.Agent, t.Scope, t.Path))
	}
	return strings.Join(paths, ", ")
}
