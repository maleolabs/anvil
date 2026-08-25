package agenttarget

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Conflict/shadow detection tests (ADR-037 D7; TS-021-02 DoD).
//
// Precedence rules pinned here:
//   - Claude Code: personal (global) > project → a repo install is
//     shadowed by an existing `~/.claude/skills/<name>`.
//   - Cline: global > project → a repo install is shadowed by an existing
//     `~/.agents/skills/<name>`.
//   - Most others (opencode, codex, gemini, cursor, zed, windsurf):
//     project > global → a global install is shadowed by an existing
//     `<repo>/.agents/skills/<name>`.
//
// Shadow never silently overwrites: it is reported as an error, and
// --force is the only escape.

// setHomeInEnv points HOME and XDG_CONFIG_HOME at temp dirs so
// os.UserHomeDir and os.UserConfigDir resolve to them.
func setHomeInEnv(t *testing.T) string {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if homeDir, err := os.UserHomeDir(); err != nil || homeDir != home {
		t.Fatalf("cannot redirect UserHomeDir to %s (got %s, err %v)", home, homeDir, err)
	}
	return home
}

func TestCheckConflicts_NoProblems(t *testing.T) {
	base := t.TempDir()
	home := setHomeInEnv(t)
	_ = home
	set, err := Resolve([]Agent{AgentOpenCode}, ScopeGlobal, base, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}
	if problems := CheckConflicts(set, CheckOptions{RepoRoot: ""}); len(problems) != 0 {
		t.Fatalf("clean install reported problems: %v", problems)
	}
}

func TestCheckConflicts_ConflictAtMaster(t *testing.T) {
	base := t.TempDir()
	set, err := Resolve([]Agent{AgentOpenCode}, ScopeRepo, base, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}
	master := filepath.Join(base, ".agents", "skills", "anvil-overview")
	if err := os.MkdirAll(filepath.Join(master, "SKILL.md"), 0o755); err != nil {
		t.Fatal(err)
	}

	problems := CheckConflicts(set, CheckOptions{})
	if len(problems) == 0 {
		t.Fatal("existing master skill not reported as conflict")
	}
	var conflict *ConflictError
	if !errors.As(problems[0], &conflict) {
		t.Fatalf("problem type = %T, want *ConflictError", problems[0])
	}

	// --force escapes.
	if got := CheckConflicts(set, CheckOptions{Force: true}); len(got) != 0 {
		t.Fatalf("--force did not escape conflict: %v", got)
	}
}

func TestCheckConflicts_ConflictAtNativeLocation(t *testing.T) {
	base := t.TempDir()
	allAgents, _ := ParseAgentFlag("all")
	set, err := Resolve(allAgents, ScopeRepo, base, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}
	// User already has a same-name skill at the native claude location.
	existing := filepath.Join(base, ".claude", "skills", "anvil-overview")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatal(err)
	}

	problems := CheckConflicts(set, CheckOptions{})
	if len(problems) == 0 {
		t.Fatal("existing native skill not reported as conflict")
	}
	var conflict *ConflictError
	if !errors.As(problems[0], &conflict) {
		t.Fatalf("problem type = %T, want *ConflictError", problems[0])
	}
	if conflict.Path != existing {
		t.Errorf("conflict path = %s, want %s", conflict.Path, existing)
	}
	if got := CheckConflicts(set, CheckOptions{Force: true}); len(got) != 0 {
		t.Fatalf("--force did not escape conflict: %v", got)
	}
}

func TestCheckConflicts_OwnSymlinkIsNotConflict(t *testing.T) {
	base := t.TempDir()
	allAgents, _ := ParseAgentFlag("all")
	set, err := Resolve(allAgents, ScopeGlobal, base, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}
	// Write once: the native symlink now points at OUR master.
	if err := WriteMaterializes(set, skillFiles(), WriterOptions{}); err != nil {
		t.Fatal(err)
	}
	// Re-running (idempotent update) must not be a conflict.
	if problems := CheckConflicts(set, CheckOptions{}); len(problems) != 0 {
		t.Fatalf("re-run on own symlink reported problems: %v", problems)
	}
}

func TestCheckConflicts_Shadow_ClaudePersonalOverProject(t *testing.T) {
	home := setHomeInEnv(t)
	base := t.TempDir() // repo git root
	set, err := Resolve([]Agent{AgentClaudeCode}, ScopeRepo, base, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}
	// User's personal copy: ~/.claude/skills/anvil-overview (higher
	// precedence than the project copy we are about to install).
	personal := filepath.Join(home, ".claude", "skills", "anvil-overview")
	if err := os.MkdirAll(personal, 0o755); err != nil {
		t.Fatal(err)
	}

	problems := CheckConflicts(set, CheckOptions{})
	if len(problems) == 0 {
		t.Fatal("claude personal shadow not reported")
	}
	var shadow *ShadowError
	if !errors.As(problems[0], &shadow) {
		t.Fatalf("problem type = %T, want *ShadowError", problems[0])
	}
	if shadow.Path != personal {
		t.Errorf("shadow path = %s, want %s", shadow.Path, personal)
	}

	// Force escapes — but the user has been told.
	if got := CheckConflicts(set, CheckOptions{Force: true}); len(got) != 0 {
		t.Fatalf("--force did not escape claude shadow: %v", got)
	}
}

func TestCheckConflicts_Shadow_ClineGlobalOverProject(t *testing.T) {
	home := setHomeInEnv(t)
	base := t.TempDir()
	set, err := Resolve([]Agent{AgentCline}, ScopeRepo, base, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}
	// Cline reads .agents/skills natively; its global copy shadows project.
	global := filepath.Join(home, ".agents", "skills", "anvil-overview")
	if err := os.MkdirAll(global, 0o755); err != nil {
		t.Fatal(err)
	}

	problems := CheckConflicts(set, CheckOptions{})
	if len(problems) == 0 {
		t.Fatal("cline global shadow not reported")
	}
	var shadow *ShadowError
	if !errors.As(problems[0], &shadow) {
		t.Fatalf("problem type = %T, want *ShadowError", problems[0])
	}
	if shadow.Path != global {
		t.Errorf("shadow path = %s, want %s", shadow.Path, global)
	}
	if got := CheckConflicts(set, CheckOptions{Force: true}); len(got) != 0 {
		t.Fatalf("--force did not escape cline shadow: %v", got)
	}
}

func TestCheckConflicts_Shadow_ProjectOverGlobal(t *testing.T) {
	base := t.TempDir()
	set, err := Resolve([]Agent{AgentOpenCode}, ScopeGlobal, base, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}
	// opencode: project > global. The repo copy (in a git root) shadows
	// the global install we are about to make.
	repoRoot := t.TempDir()
	project := filepath.Join(repoRoot, ".agents", "skills", "anvil-overview")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}

	problems := CheckConflicts(set, CheckOptions{RepoRoot: repoRoot})
	if len(problems) == 0 {
		t.Fatal("project shadow not reported")
	}
	var shadow *ShadowError
	if !errors.As(problems[0], &shadow) {
		t.Fatalf("problem type = %T, want *ShadowError", problems[0])
	}
	if shadow.Path != project {
		t.Errorf("shadow path = %s, want %s", shadow.Path, project)
	}
	if got := CheckConflicts(set, CheckOptions{Force: true}); len(got) != 0 {
		t.Fatalf("--force did not escape project shadow: %v", got)
	}
}

func TestCheckConflicts_NoShadowWhenScopeIsHigherPrecedence(t *testing.T) {
	home := setHomeInEnv(t)
	base := t.TempDir()

	// claude-code GLOBAL install: personal IS the higher precedence — the
	// repo project copy does not shadow it.
	globalSet, err := Resolve([]Agent{AgentClaudeCode}, ScopeGlobal, base, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}
	if problems := CheckConflicts(globalSet, CheckOptions{}); len(problems) != 0 {
		t.Fatalf("claude global install reported problems: %v", problems)
	}

	// opencode REPO install: project IS the higher precedence.
	repoSet, err := Resolve([]Agent{AgentOpenCode}, ScopeRepo, base, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}
	// A global copy exists but must NOT shadow the repo install.
	global := filepath.Join(home, ".agents", "skills", "anvil-overview")
	if err := os.MkdirAll(global, 0o755); err != nil {
		t.Fatal(err)
	}
	if problems := CheckConflicts(repoSet, CheckOptions{}); len(problems) != 0 {
		t.Fatalf("opencode repo install shadowed by global copy: %v", problems)
	}
}

func TestCheckConflicts_Shadow_ClaudeGlobalScopeInstallNoShadow(t *testing.T) {
	home := setHomeInEnv(t)
	base := t.TempDir()
	// Personal (global) install: the project copy is lower precedence and
	// does not shadow it.
	set, err := Resolve([]Agent{AgentClaudeCode}, ScopeGlobal, base, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}
	_ = home
	if problems := CheckConflicts(set, CheckOptions{}); len(problems) != 0 {
		t.Fatalf("claude personal install reported problems: %v", problems)
	}
}

func TestCheckConflicts_MultipleProblemsAggregated(t *testing.T) {
	home := setHomeInEnv(t)
	base := t.TempDir()
	set, err := Resolve([]Agent{AgentClaudeCode}, ScopeRepo, base, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}
	// Both a conflict at the native target AND a personal shadow.
	native := filepath.Join(base, ".claude", "skills", "anvil-overview")
	if err := os.MkdirAll(native, 0o755); err != nil {
		t.Fatal(err)
	}
	personal := filepath.Join(home, ".claude", "skills", "anvil-overview")
	if err := os.MkdirAll(personal, 0o755); err != nil {
		t.Fatal(err)
	}

	problems := CheckConflicts(set, CheckOptions{})
	if len(problems) < 2 {
		t.Fatalf("expected conflict + shadow, got %d problems: %v", len(problems), problems)
	}
	hasConflict, hasShadow := false, false
	for _, p := range problems {
		var c *ConflictError
		var s *ShadowError
		switch {
		case errors.As(p, &c):
			hasConflict = true
		case errors.As(p, &s):
			hasShadow = true
		}
	}
	if !hasConflict || !hasShadow {
		t.Errorf("want both conflict and shadow, got conflict=%v shadow=%v", hasConflict, hasShadow)
	}
}
