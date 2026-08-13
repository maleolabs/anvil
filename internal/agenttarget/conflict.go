package agenttarget

import (
	"fmt"
	"os"
	"path/filepath"
)

// Conflict/shadow detection (ADR-037 D7).
//
// Two classes of problem, both errors with a `--force` escape:
//
//   - Conflict: a same-name skill already exists AT a target location
//     (master or native). Installing over it would silently replace user
//     content — aborted unless --force.
//   - Shadow: the skill being installed would be shadowed by a
//     higher-precedence copy the user already has (Claude Code personal
//     > project; Cline global > project; most others project > global).
//     The install would never be read — reported as an error unless
//     --force.
//
// Ownership (M-1): a target occupied by OUR OWN previous install is not a
// conflict — re-run/update is idempotent. Ownership is recognized via the
// `.anvil-install` marker (written by this package into every master,
// copy, and lone-native directory) and via a native symlink that points at
// our master.

// CheckOptions controls conflict/shadow detection.
type CheckOptions struct {
	// Force bypasses both conflict and shadow errors (ADR-037 D7
	// `--force` escape hatch).
	Force bool

	// RepoRoot is the git root used to check project-level shadowing for
	// global-scope installs (project > global agents). Empty triggers a
	// best-effort git-root discovery; when neither is available the
	// project-shadow check is skipped (no project = no project copy).
	RepoRoot string

	// Home is the home directory used to resolve global-level shadow
	// locations (Claude Code personal copy, Cline global master). Empty
	// uses os.UserHomeDir() — callers that redirect HOME (tests,
	// installers with an injected home) must pass it so the check and the
	// install agree on where "global" lives.
	Home string
}

// ConflictError reports a same-name skill already present at a target
// location.
type ConflictError struct {
	// Path is the occupied target location.
	Path string

	// What occupies the path (a directory, a file, a symlink).
	Existing string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("a skill already exists at %s (%s). Refusing to overwrite — run with --force to replace it", e.Path, e.Existing)
}

// ShadowError reports that the install would be shadowed by a user's
// higher-precedence copy.
type ShadowError struct {
	// Path is the higher-precedence copy that would shadow the install.
	Path string

	// Precedence describes the rule (for example "Claude Code personal
	// (global) shadows project").
	Precedence string
}

func (e *ShadowError) Error() string {
	return fmt.Sprintf("this install would be shadowed by your existing skill at %s (%s). The installed copy would never be read — run with --force to install anyway", e.Path, e.Precedence)
}

// CheckConflicts inspects a resolved set against the filesystem and the
// per-agent precedence rules. It returns every problem found (conflict and
// shadow) so the caller can present them all at once. With Force set it
// returns nothing — the caller is expected to have already acknowledged.
//
// Ownership is marker-based: our own previous install (master with our
// marker, native symlink to our master, copy/lone dir with our marker) is
// idempotent, not a conflict (M-1).
func CheckConflicts(set *ResolvedSet, opts CheckOptions) []error {
	if set == nil {
		return []error{fmt.Errorf("conflict check: no resolved set")}
	}

	var problems []error

	// 1. Conflicts at every target location.
	for _, t := range set.Targets {
		if t.Kind == TargetKindMaster {
			continue // the master path is checked once below
		}
		occupied, ours := targetOccupation(t, set.Master, set.SkillName)
		if occupied && !ours {
			problems = append(problems, &ConflictError{Path: t.Path, Existing: describeExisting(t.Path)})
		}
	}
	if set.Master != "" && !masterOwned(set) {
		if existing := describeExisting(set.Master); existing != "" {
			problems = append(problems, &ConflictError{Path: set.Master, Existing: existing})
		}
	}

	// 2. Shadows per precedence — one check per agent in the set, not per
	// target: reader agents (opencode, cline, ...) resolve to the master,
	// so their shadow must still be evaluated.
	for _, a := range agentsInSet(set) {
		if shadow := checkShadow(a, set, opts.RepoRoot, opts.Home); shadow != nil {
			problems = append(problems, shadow)
		}
	}

	if opts.Force {
		return nil
	}
	return problems
}

// agentsInSet returns the unique agents referenced by a resolved set's
// targets, in table order (the shared "all" master target is skipped).
func agentsInSet(set *ResolvedSet) []Agent {
	seen := map[string]bool{}
	var out []Agent
	for _, t := range set.Targets {
		if t.Agent == "all" || seen[t.Agent] {
			continue
		}
		if a, ok := agentsByID[t.Agent]; ok {
			seen[t.Agent] = true
			out = append(out, a)
		}
	}
	return out
}

// checkShadow reports a ShadowError when the install for an agent would sit
// below a higher-precedence copy the user already has.
func checkShadow(a Agent, set *ResolvedSet, repoRoot, home string) *ShadowError {
	higher := shadowHigherLocation(a, set, repoRoot, home)
	if higher == "" {
		return nil
	}
	if describeExisting(higher) == "" {
		return nil
	}
	return &ShadowError{
		Path:       higher,
		Precedence: shadowPrecedenceLabel(a),
	}
}

// shadowHigherLocation returns the higher-precedence location that would
// shadow this install, or "" when the current scope IS the higher one.
//
// For agents whose global copy wins (Claude Code personal > project; Cline
// global > project), a repo-scope install is shadowed by the agent's
// global copy — resolved against the home directory (opts.Home, falling
// back to os.UserHomeDir), not the repo base.
func shadowHigherLocation(a Agent, set *ResolvedSet, repoRoot, homeOverride string) string {
	home := homeOverride
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	hasHome := home != ""

	switch {
	case a.PrecedenceGlobalWins && set.Scope == ScopeRepo && hasHome:
		// Claude Code personal > project; Cline global > project: the
		// global copy shadows a repo install.
		if rel := a.NativeGlobalRel; rel != "" {
			return filepath.Join(home, rel, set.SkillName)
		}
		return globalMasterShadow(home, set.SkillName)
	case !a.PrecedenceGlobalWins && set.Scope == ScopeGlobal:
		// Most agents: project > global. The repo copy shadows a global
		// install — requires a git root to exist.
		root := repoRoot
		if root == "" {
			root, _ = findGitRoot()
		}
		if root == "" {
			return ""
		}
		return filepath.Join(root, ".agents", "skills", set.SkillName)
	default:
		return ""
	}
}

// globalMasterShadow returns the global master copy path for an agent that
// reads `.agents/skills` natively and wins over project (Cline).
func globalMasterShadow(home, skillName string) string {
	return filepath.Join(home, ".agents", "skills", skillName)
}

// shadowPrecedenceLabel is the human precedence rule for a shadow message.
func shadowPrecedenceLabel(a Agent) string {
	if a.PrecedenceGlobalWins {
		return fmt.Sprintf("%s global shadows project", a.DisplayName)
	}
	return fmt.Sprintf("project shadows %s global", a.DisplayName)
}

// describeExisting returns what occupies a path ("a directory", "a file"),
// or "" when the path does not exist.
func describeExisting(path string) string {
	info, err := os.Lstat(path)
	if err != nil {
		return ""
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "a symlink"
	}
	if info.IsDir() {
		return "a directory"
	}
	return "a file"
}
