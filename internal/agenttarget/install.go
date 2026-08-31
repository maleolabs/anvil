package agenttarget

import (
	"fmt"
)

// Installer orchestrates a skill installation end to end: resolve the
// agent targets for a scope, detect conflicts/shadows, and materialize the
// skill (TS-021-02). It is the single entry point the command surface
// (ST-021-01) and the record store consumer (TS-021-03) use.
//
// Install is deliberately NOT a full pipeline: the caller has already
// extracted the skill content (TS-021-01 extractor), validated the
// manifest, and resolved the record. This type owns the filesystem layout
// decisions only.
type Installer struct {
	// Home overrides the home directory for global-scope resolution and
	// shadow checks. Empty uses os.UserHomeDir.
	Home string

	// RepoRoot overrides the git root used for project-scope shadow
	// checks. Empty falls back to discovery from the working directory.
	RepoRoot string
}

// Install writes the skill for the given agents at the given scope.
//
// It returns the ResolvedSet (with every target path, kind, and master)
// so the caller can persist `targets[]` in the record store. The skill
// files land only when every conflict/shadow check passes (or Force is
// set); a failed install rolls back everything this call created.
//
// agents may be nil (or empty): the agents are then auto-detected from the
// config folders under the installer's Home — the `--agent` default. When
// detection finds no selectable agent, an actionable error is returned.
//
// force maps to the ADR-037 D7 `--force` escape: it bypasses conflict and
// shadow errors AND allows the writer to replace a native-location path
// occupied by a user's same-name skill.
func (in *Installer) Install(scope Scope, skillName string, files map[string][]byte, agents []Agent, force bool) (*ResolvedSet, error) {
	if in == nil {
		return nil, fmt.Errorf("install skill: nil installer")
	}

	// `--agent` default: auto-detect from config folders under Home.
	if len(agents) == 0 {
		detected, err := in.AutoDetect()
		if err != nil {
			return nil, err
		}
		agents = detected
	}

	// Scope base (validates repo requires anvil project + git root; the
	// global scope resolves against the installer's Home, honoring
	// overrides instead of silently using os.UserHomeDir — M-2/L3).
	base, err := ScopeBase(scope, in.Home)
	if err != nil {
		return nil, err
	}

	// Resolve targets.
	set, err := Resolve(agents, scope, base, skillName)
	if err != nil {
		return nil, fmt.Errorf("install skill %s: %w", skillName, err)
	}

	// Conflict/shadow gate (ADR-037 D7): never a silent overwrite. The
	// gate and the writer share the same ownership recognition, so a
	// blocked install writes nothing and a forced install replaces every
	// occupant in one pass.
	problems := CheckConflicts(set, CheckOptions{
		Force:    force,
		RepoRoot: in.RepoRoot,
		Home:     in.Home,
	})
	if len(problems) > 0 {
		return nil, &InstallBlockedError{Problems: problems}
	}

	// Materialize.
	if err := WriteMaterializes(set, files, WriterOptions{Force: force}); err != nil {
		return nil, fmt.Errorf("install skill %s: %w", skillName, err)
	}
	return set, nil
}

// InstallBlockedError aggregates every conflict and shadow problem found
// during the gate, so the caller can present them all at once with the
// `--force` hint.
type InstallBlockedError struct {
	Problems []error
}

func (e *InstallBlockedError) Error() string {
	if len(e.Problems) == 0 {
		return "skill install blocked"
	}
	out := fmt.Sprintf("skill install blocked by %d problem(s):", len(e.Problems))
	for _, p := range e.Problems {
		out += "\n  - " + p.Error()
	}
	out += "\nRun with --force to override."
	return out
}

// AutoDetect returns the selectable agents detected on this machine under
// the installer's Home. It is the `--agent` default resolution.
func (in *Installer) AutoDetect() ([]Agent, error) {
	home := in.Home
	if home == "" {
		var err error
		home, err = homeDir()
		if err != nil {
			return nil, fmt.Errorf("detect agents: %w", err)
		}
	}
	detected := DetectAgentsSelectable(home)
	if len(detected) == 0 {
		return nil, fmt.Errorf("no supported AI agent detected on this machine (checked config folders for claude-code, opencode, codex, gemini, cursor, cline, zed under %s). Install one of those agents first, or pass --agent <agent> explicitly", home)
	}
	return detected, nil
}
