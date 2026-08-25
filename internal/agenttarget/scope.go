package agenttarget

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"maleolabs.com/anvil/internal/project"
)

// Scope selects where skills are installed (ADR-037 D5).
//
//   - ScopeRepo installs into the current Anvil project's repo root (git
//     root, not cwd) — it REQUIRES an Anvil project context.
//   - ScopeGlobal installs into the user's home-level agent directories —
//     it does NOT require an Anvil project.
type Scope string

const (
	// ScopeRepo installs into the git root of the current Anvil project.
	ScopeRepo Scope = "repo"

	// ScopeGlobal installs into the user's home-level directories.
	ScopeGlobal Scope = "global"
)

// ParseScope validates an `--scope` value. An empty value defaults to
// ScopeRepo (the versionable, team-shared default per ADR-037 §4).
func ParseScope(value string) (Scope, error) {
	if value == "" {
		return ScopeRepo, nil
	}
	switch Scope(value) {
	case ScopeRepo:
		return ScopeRepo, nil
	case ScopeGlobal:
		return ScopeGlobal, nil
	default:
		return "", fmt.Errorf("invalid scope %q — supported values: repo | global", value)
	}
}

// ScopeBase resolves the filesystem base for a scope:
//
//   - ScopeRepo: the git root of the current Anvil project (anvil.yaml
//     must be discoverable, and the enclosing git root is used — not cwd).
//   - ScopeGlobal: the user's home directory. homeOverride, when non-empty,
//     wins over os.UserHomeDir() (used by the installer so the caller can
//     inject a home for tests or redirected environments; the default is
//     HOME/%USERPROFILE%).
//
// Errors are actionable: repo scope failures explain what is missing and
// how to fix it (run `anvil init` / move into a git repository).
func ScopeBase(scope Scope, homeOverride string) (string, error) {
	switch scope {
	case ScopeGlobal:
		home := homeOverride
		if home == "" {
			var err error
			home, err = os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("cannot resolve the home directory for --scope global: %w", err)
			}
		}
		return home, nil
	case ScopeRepo:
		return requireAnvilProjectRepoRoot()
	default:
		return "", fmt.Errorf("invalid scope %q", scope)
	}
}

// requireAnvilProjectRepoRoot resolves the git root of the current Anvil
// project. Both requirements are enforced (ADR-037 D5):
//
//  1. An Anvil project must be discoverable (anvil.yaml in cwd or a parent).
//  2. The project must live inside a git repository — the master copy goes
//     to the git root, not cwd.
func requireAnvilProjectRepoRoot() (string, error) {
	if _, err := project.Discover(); err != nil {
		if errors.Is(err, project.ErrNoProjectFound) {
			return "", fmt.Errorf("--scope repo requires an Anvil project: no anvil.yaml found in this directory or any parent. Run 'anvil init' to create a project, or use --scope global to install into your home directory")
		}
		return "", fmt.Errorf("--scope repo: cannot locate the Anvil project: %w", err)
	}

	root, err := findGitRoot()
	if err != nil {
		return "", fmt.Errorf("--scope repo requires the Anvil project to be inside a git repository: %w. Run 'git init' in the project root, or use --scope global", err)
	}
	return root, nil
}

// findGitRoot locates the git root by walking up from the current working
// directory until a `.git` entry (directory, or file for worktrees) is
// found. It returns the absolute path of the directory containing `.git`.
//
// The walk is limited to the filesystem root; permission errors are
// propagated. Not finding `.git` anywhere yields an actionable error.
func findGitRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot resolve the current directory: %w", err)
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("cannot resolve the current directory: %w", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("cannot inspect %s: %w", dir, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("not inside a git repository (no .git found in this directory or any parent)")
		}
		dir = parent
	}
}

// homeDir resolves the user's home directory (Linux/macOS $HOME, Windows
// %USERPROFILE%).
func homeDir() (string, error) {
	return os.UserHomeDir()
}

// scopeBaseLabel is a short human label for a scope used in messages.
func scopeBaseLabel(scope Scope) string {
	if scope == ScopeGlobal {
		return "global"
	}
	return "repo"
}
