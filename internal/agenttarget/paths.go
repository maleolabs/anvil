package agenttarget

import (
	"fmt"
	"path/filepath"
)

// TargetKind classifies how a target location receives the skill content
// (ADR-037 D6): the master copy is written directly, native-only agents
// receive a symlink to the master (POSIX) or a copy (Windows fallback).
type TargetKind string

const (
	// TargetKindMaster is the `.agents/skills/<name>/` master copy — the
	// single source of truth for agents that read it natively.
	TargetKindMaster TargetKind = "master"

	// TargetKindSymlink is a native-location symlink pointing at the
	// master copy (Claude Code, Cursor on POSIX).
	TargetKindSymlink TargetKind = "symlink"

	// TargetKindCopy is a native-location copy of the master (Windows, or
	// POSIX without symlink privilege). Content is duplicated but the
	// record tracks both paths so update/uninstall stay correct.
	TargetKindCopy TargetKind = "copy"
)

// Target is one resolved installation location for one agent at one scope.
//
// It is the shape consumed by the installed-skills record store
// (TS-021-03 `targets[]: {agent, scope, path}`): Path is the absolute
// directory that must exist for the agent to read the skill. Kind and
// Master tell the writer how to materialize it (write / symlink / copy).
type Target struct {
	// Agent is the target agent's ID (for example "claude-code").
	Agent string

	// Scope is the scope this target belongs to ("repo" or "global").
	Scope Scope

	// Path is the absolute filesystem path of the skill directory for
	// this agent (e.g. <base>/.claude/skills/<name>).
	Path string

	// Kind classifies the write: master / symlink / copy.
	Kind TargetKind

	// Master is the absolute path of the master copy. Empty for
	// TargetKindMaster; set for symlink and copy targets.
	Master string
}

// ResolvedSet is the full resolution for one install: the scope base, the
// master copy path (empty when no agent needs it), and every per-agent
// target. The record store persists Targets; the writer consumes the rest.
type ResolvedSet struct {
	// Scope is the requested scope.
	Scope Scope

	// Base is the scope base directory (git root or home).
	Base string

	// SkillName is the skill being installed.
	SkillName string

	// Master is the absolute master copy path
	// `<base>/.agents/skills/<skillName>/`. Empty when no agent in the
	// set reads `.agents/skills` (a lone claude-code/cursor install).
	Master string

	// Targets lists every per-agent target, in a stable agent-table order.
	Targets []Target
}

// Resolve computes the target paths for an agent set at a scope (ADR-037
// D5/D6):
//
//   - Agents that read `.agents/skills` natively resolve to the master
//     copy path `<base>/.agents/skills/<name>/`.
//   - Claude Code and Cursor resolve to their native location
//     (`<base>/.claude/skills/<name>`, `<base>/.cursor/skills/<name>`).
//
// The master copy is written when at least one agent reads `.agents/skills`
// OR the set spans multiple agents; a lone native-only agent
// (--agent claude-code / --agent cursor) receives a real copy at its native
// location and no master, so other agents never see it (ADR-037 D6).
//
// base must be an absolute path (from ScopeBase). skillName must be a valid
// skill name (the caller validates via the bundle manifest).
func Resolve(agents []Agent, scope Scope, base, skillName string) (*ResolvedSet, error) {
	if len(agents) == 0 {
		return nil, fmt.Errorf("resolve skill targets: no agents selected")
	}
	if !filepath.IsAbs(base) {
		return nil, fmt.Errorf("resolve skill targets: scope base %q is not absolute", base)
	}
	if !validateSkillDirName(skillName) {
		return nil, fmt.Errorf("resolve skill targets: invalid skill name %q (^[a-z0-9][a-z0-9-]*$)", skillName)
	}

	set := &ResolvedSet{
		Scope:     scope,
		Base:      base,
		SkillName: skillName,
	}

	// A lone native-only agent (claude-code / cursor) is installed as a
	// real copy at its native location — no master, so no other agent
	// sees it (ADR-037 D6 "so other agents never see it").
	loneNativeOnly := len(agents) == 1 && agents[0].NativeRepoRel != ""

	if !loneNativeOnly {
		set.Master = masterPath(base, skillName)
		set.Targets = append(set.Targets, Target{
			Agent: "all",
			Scope: scope,
			Path:  set.Master,
			Kind:  TargetKindMaster,
		})
	}

	for _, a := range agents {
		nativeRel := nativeRel(a, scope)
		if nativeRel == "" {
			// Reads `.agents/skills` natively: the master IS the target.
			// Add the master target under this agent's ID too when a
			// master exists, so the record can attribute it per agent.
			if set.Master != "" {
				set.Targets = append(set.Targets, Target{
					Agent: a.ID,
					Scope: scope,
					Path:  set.Master,
					Kind:  TargetKindMaster,
				})
			}
			continue
		}

		// Native-only agent: symlink to master (or copy when no master
		// exists — the lone case handled above) / copy fallback.
		path := filepath.Join(base, nativeRel, skillName)
		kind := TargetKindSymlink
		if set.Master == "" {
			kind = TargetKindCopy
		}
		set.Targets = append(set.Targets, Target{
			Agent:  a.ID,
			Scope:  scope,
			Path:   path,
			Kind:   kind,
			Master: set.Master,
		})
	}

	return set, nil
}

// nativeRel returns the agent's native skill dir relative to the scope
// base, or "" when the agent reads `.agents/skills` natively.
func nativeRel(a Agent, scope Scope) string {
	if scope == ScopeGlobal {
		return a.NativeGlobalRel
	}
	return a.NativeRepoRel
}

// masterPath returns the master copy path for a skill at a scope base:
// `<base>/.agents/skills/<name>/`.
func masterPath(base, skillName string) string {
	return filepath.Join(base, ".agents", "skills", skillName)
}

// nativePath returns the native location of an agent for a skill at a
// scope base, or "" for agents that read `.agents/skills` natively.
func nativePath(a Agent, scope Scope, base, skillName string) string {
	rel := nativeRel(a, scope)
	if rel == "" {
		return ""
	}
	return filepath.Join(base, rel, skillName)
}

// validateSkillDirName mirrors the bundle name rule
// (^[a-z0-9][a-z0-9-]*$, bounded) for directory naming safety: the skill
// name becomes a path segment, so it must be a safe identifier.
func validateSkillDirName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for i, r := range name {
		lower := r >= 'a' && r <= 'z'
		digit := r >= '0' && r <= '9'
		hyphen := r == '-'
		if i == 0 && !lower && !digit {
			return false
		}
		if !lower && !digit && !hyphen {
			return false
		}
	}
	return true
}
