// Package cmd implements the Anvil CLI commands.
//
// ── Skill Uninstall (ST-021-01; ADR-037 D8) ──────────────────────────
//
// "anvil skill uninstall <name>" removes an installed skill: every
// recorded target path (agenttarget.ReadAllTargets over the record's
// targets[]) is removed — absolute-path + containment check BEFORE any
// RemoveAll (T-005 reviewer note) — and the installed-skills record is
// deleted. Removing an uninstalled skill is graceful (exit 0, nothing to
// do) — the desired end state already holds.
//
// --agent / --scope act as a filter: only the recorded targets matching
// the filter are removed, and the record is deleted only when no targets
// remain (otherwise it is Updated with the remaining targets). --force
// is accepted for surface consistency and has no effect: uninstall is
// always explicit removal of the recorded content.
//
// Reference: ST-021-01, ADR-037 D8, TS-021-02/03
package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/agenttarget"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/registry"
	"maleolabs.com/anvil/internal/skillbundle"
)

// skillUninstallCmd represents the "anvil skill uninstall" command that
// removes an installed skill's content and record.
var skillUninstallCmd = &cobra.Command{
	Use:   "uninstall <name>",
	Short: "Uninstall an installed skill (content and record)",
	Long: `Remove an installed skill: every recorded target path (master copy,
native symlinks and copies) is removed and the installed-skills record
is deleted.

Target paths are verified before removal — an absolute-path and
containment check runs against the skill's scope before anything is
removed, so a stale or hand-edited record can never direct the removal
outside the intended skill directories.

--agent / --scope filter which recorded targets are removed (e.g.
'uninstall <name> --scope repo' removes only the repo-scope targets);
the record is deleted when no targets remain. A filesystem path is
removed only when every recorded target referencing it is matched — the
shared master copy survives a partial --agent uninstall, so the other
agents' symlinks never dangle. --force is accepted for surface
consistency and has no effect — uninstall always removes the recorded
content explicitly.

Uninstalling a skill that is not installed is graceful: the command
reports it and exits 0 (the desired end state already holds).

Output formats:
  Default      removal report (removed paths, record status)
  --json       standard TS-P8-05 envelope on stdout, data:
               {name, status, removed: [paths], record_removed, message}

Exit codes: 0 success (including nothing-to-remove); 1 errors (record
unreadable, removal failure, containment rejection, filter matching
only shared targets); 3 the filter matches no recorded target; 4
precondition (the recorded scope base cannot be resolved, e.g. repo
scope outside the project).

Examples:
  anvil skill uninstall anvil-overview
  anvil skill uninstall anvil-overview --scope repo
  anvil skill uninstall anvil-overview --json`,
	Args:         RangeArgsWithUsage(1, 1, "anvil skill uninstall anvil-overview", "name"),
	SilenceUsage: true,
	RunE:         runSkillUninstall,
}

func init() {
	AddJSONFlag(skillUninstallCmd)
	addSkillTargetFlags(skillUninstallCmd)
}

// skillUninstallJSON is the machine-readable uninstall output (TS-P8-05
// data).
type skillUninstallJSON struct {
	Name          string   `json:"name"`
	Status        string   `json:"status"`
	Removed       []string `json:"removed,omitempty"`
	RecordRemoved bool     `json:"record_removed"`
	Message       string   `json:"message,omitempty"`
}

// runSkillUninstall executes the uninstall command: read the record,
// filter the recorded targets, remove each (containment-checked), and
// delete or update the record.
func runSkillUninstall(cmd *cobra.Command, args []string) error {
	s := styleFor(cmd)
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
		return skillReportError(cmd, "could not open the installed-skills store", err.Error(), "", output.ExitCodeGeneral, err)
	}
	rec, err := store.Get(name)
	if err != nil {
		if errors.Is(err, registry.ErrSkillRecordNotFound) {
			// Graceful: nothing recorded to remove — the desired end
			// state already holds (mirrors the adapter uninstall).
			jsonOutput, _ := cmd.Flags().GetBool("json")
			if jsonOutput {
				return WriteJSON(cmd, skillUninstallJSON{
					Name:    name,
					Status:  "not-installed",
					Message: fmt.Sprintf("skill %q is not installed — nothing to remove", name),
				})
			}
			fmt.Fprintf(s.W, "Skill %s is not installed — nothing to remove.\n", name)
			return nil
		}
		return skillReportStoreError(cmd, "uninstall", name, err)
	}

	// Filter by --agent / --scope when given. --agent all means the whole
	// install — the same as no filter.
	agentFilter, _ := cmd.Flags().GetString("agent")
	if agentFilter == "all" {
		agentFilter = ""
	}
	scopeFilter, _ := cmd.Flags().GetString("scope")

	// The filter selects RECORDED TARGETS. A filesystem path is removed
	// only when EVERY target referencing it is selected: the shared
	// master copy must survive a partial uninstall — removing it would
	// orphan the other agents that still reference it (MED-1/F-1).
	selected := make(map[string]bool)
	for _, t := range rec.Targets {
		if agentFilter != "" && t.Agent != agentFilter {
			continue
		}
		if scopeFilter != "" && t.Scope != scopeFilter {
			continue
		}
		selected[skillTargetKey(t)] = true
	}
	if len(selected) == 0 {
		return skillReportError(cmd,
			fmt.Sprintf("skill %q is installed but no recorded target matches the filter", name),
			fmt.Sprintf("recorded targets: %d (agents/scopes do not match --agent %q --scope %q)", len(rec.Targets), agentFilter, scopeFilter),
			"Run 'anvil skill list --json' to inspect the recorded targets, or drop the filter flags",
			output.ExitCodeRuntime, nil)
	}

	// The removable paths: distinct target paths whose referencing
	// targets are all selected. The path set comes from
	// agenttarget.ReadAllTargets over the record's targets (the shared
	// path-set logic of the mapping package).
	refsByPath := make(map[string][]registry.InstalledSkillTarget)
	for _, t := range rec.Targets {
		refsByPath[t.Path] = append(refsByPath[t.Path], t)
	}
	var removable []string
	for _, p := range skillUninstallPaths(rec) {
		all := false
		if refs, ok := refsByPath[p]; ok {
			all = true
			for _, ref := range refs {
				if !selected[skillTargetKey(ref)] {
					all = false
					break
				}
			}
		}
		if all {
			removable = append(removable, p)
		}
	}
	if len(removable) == 0 {
		// Every selected target references a shared path (e.g. the master
		// copy used by all reader agents) — a partial uninstall cannot
		// remove it without orphaning the other agents.
		return skillReportError(cmd,
			fmt.Sprintf("skill %q is shared through the master copy; the filter matches no removable target", name),
			"the matched targets all reference the shared master copy, which other agents still use",
			"Run 'anvil skill uninstall "+name+"' without --agent/--scope to remove the whole skill",
			output.ExitCodeGeneral, nil)
	}

	// Containment base per path derives from its own target scope
	// (MIN-8), resolved with the typed precondition classification.
	baseCache := make(map[string]string)
	removed := make([]string, 0, len(removable))
	for _, p := range removable {
		scopeOf := ""
		if refs, ok := refsByPath[p]; ok && len(refs) > 0 {
			scopeOf = refs[0].Scope
		}
		base, ok := baseCache[scopeOf]
		if !ok {
			b, berr := skillScopeBase(agenttarget.Scope(scopeOf))
			if berr != nil {
				return skillReportScopeError(cmd, fmt.Sprintf("cannot resolve the %s scope base", scopeOf), berr)
			}
			base = b
			baseCache[scopeOf] = base
		}
		if err := skillTargetContainment(p, base, name); err != nil {
			return skillReportError(cmd,
				fmt.Sprintf("refusing to remove skill target %s", p),
				err.Error(),
				"Delete the stale record manually, or re-install the skill to refresh it",
				output.ExitCodeGeneral, err)
		}
		if err := os.RemoveAll(p); err != nil {
			return skillReportError(cmd,
				fmt.Sprintf("could not remove skill target %s", p),
				err.Error(),
				"",
				output.ExitCodeGeneral, err)
		}
		removed = append(removed, p)
	}

	// Record disposition: delete when every target is gone, otherwise
	// update with the remaining targets (path-based: a target whose path
	// survived stays recorded — the skill is still installed there).
	remaining := make([]registry.InstalledSkillTarget, 0, len(rec.Targets))
	removedSet := make(map[string]bool, len(removed))
	for _, p := range removed {
		removedSet[p] = true
	}
	for _, t := range rec.Targets {
		if !removedSet[t.Path] {
			remaining = append(remaining, t)
		}
	}

	recordRemoved := false
	if len(remaining) == 0 {
		if err := store.Delete(name); err != nil {
			return skillReportStoreError(cmd, "uninstall", name, err)
		}
		recordRemoved = true
	} else {
		rec.Targets = remaining
		rec.UpdatedAt = now()
		if _, err := store.Update(name, rec); err != nil {
			return skillReportStoreError(cmd, "uninstall", name, err)
		}
	}

	jsonOutput, _ := cmd.Flags().GetBool("json")
	if jsonOutput {
		msg := fmt.Sprintf("uninstalled skill %q", name)
		if !recordRemoved {
			msg = fmt.Sprintf("removed %d target(s) of skill %q; %d target(s) remain (record kept)", len(removed), name, len(remaining))
		}
		return WriteJSON(cmd, skillUninstallJSON{
			Name:          name,
			Status:        "uninstalled",
			Removed:       removed,
			RecordRemoved: recordRemoved,
			Message:       msg,
		})
	}

	w := s.W
	if recordRemoved {
		fmt.Fprintf(w, "Uninstalled skill %s.\n", name)
		fmt.Fprintln(w, "Removed:")
		for _, p := range removed {
			fmt.Fprintf(w, "  %s\n", p)
		}
		fmt.Fprintf(w, "Record removed: yes\n")
		return nil
	}
	fmt.Fprintf(w, "Removed %d target(s) of skill %s.\n", len(removed), name)
	for _, p := range removed {
		fmt.Fprintf(w, "  %s\n", p)
	}
	fmt.Fprintf(w, "%d target(s) remain (record kept).\n", len(remaining))
	return nil
}
