package agenttarget

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// End-to-end installer tests: resolve → gate → write, with the conflict
// and shadow paths and the --force escape.

func TestInstaller_Install_GlobalReaderAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	in := &Installer{Home: home, RepoRoot: ""}
	set, err := in.Install(ScopeGlobal, "anvil-overview", skillFiles(), []Agent{AgentOpenCode}, false)
	if err != nil {
		t.Fatal(err)
	}
	if set == nil {
		t.Fatal("nil resolved set")
	}
	master := filepath.Join(home, ".agents", "skills", "anvil-overview")
	if _, err := os.Stat(filepath.Join(master, "SKILL.md")); err != nil {
		t.Errorf("master SKILL.md missing: %v", err)
	}
}

func TestInstaller_Install_RepoScope_BlockedWithoutProject(t *testing.T) {
	work := t.TempDir()
	chdir(t, work)

	in := &Installer{}
	if _, err := in.Install(ScopeRepo, "anvil-overview", skillFiles(), []Agent{AgentOpenCode}, false); err == nil {
		t.Fatal("repo install without anvil project: expected error")
	}
}

func TestInstaller_Install_RepoScope_Success(t *testing.T) {
	work := t.TempDir()
	writeGitRoot(t, work)
	writeAnvilProject(t, work)
	chdir(t, work)
	t.Setenv("HOME", t.TempDir())

	in := &Installer{}
	set, err := in.Install(ScopeRepo, "anvil-overview", skillFiles(), []Agent{AgentClaudeCode}, false)
	if err != nil {
		t.Fatal(err)
	}
	// Lone claude-code: native copy, no master.
	if set.Master != "" {
		t.Fatalf("lone claude-code created master %s", set.Master)
	}
	native := filepath.Join(work, ".claude", "skills", "anvil-overview")
	if _, err := os.Stat(filepath.Join(native, "SKILL.md")); err != nil {
		t.Errorf("native SKILL.md missing: %v", err)
	}
}

func TestInstaller_Install_ConflictBlocks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Pre-existing master (not ours — no symlink points at it).
	existing := filepath.Join(home, ".agents", "skills", "anvil-overview")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatal(err)
	}

	in := &Installer{Home: home}
	_, err := in.Install(ScopeGlobal, "anvil-overview", skillFiles(), []Agent{AgentOpenCode}, false)
	if err == nil {
		t.Fatal("conflict not blocked")
	}
	var blocked *InstallBlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("error type = %T, want *InstallBlockedError", err)
	}
	if len(blocked.Problems) == 0 {
		t.Fatal("blocked error carries no problems")
	}

	// --force escapes.
	if _, err := in.Install(ScopeGlobal, "anvil-overview", skillFiles(), []Agent{AgentOpenCode}, true); err != nil {
		t.Fatalf("--force did not escape conflict: %v", err)
	}
}

func TestInstaller_Install_ShadowBlocks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Personal claude copy shadows a repo install.
	personal := filepath.Join(home, ".claude", "skills", "anvil-overview")
	if err := os.MkdirAll(personal, 0o755); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	writeGitRoot(t, work)
	writeAnvilProject(t, work)
	chdir(t, work)

	in := &Installer{}
	_, err := in.Install(ScopeRepo, "anvil-overview", skillFiles(), []Agent{AgentClaudeCode}, false)
	if err == nil {
		t.Fatal("shadow not blocked")
	}
	var blocked *InstallBlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("error type = %T, want *InstallBlockedError", err)
	}
	var shadow *ShadowError
	found := false
	for _, p := range blocked.Problems {
		if errors.As(p, &shadow) {
			found = true
		}
	}
	if !found {
		t.Errorf("blocked problems lack a ShadowError: %v", blocked.Problems)
	}

	// --force escapes.
	if _, err := in.Install(ScopeRepo, "anvil-overview", skillFiles(), []Agent{AgentClaudeCode}, true); err != nil {
		t.Fatalf("--force did not escape shadow: %v", err)
	}
}

func TestInstaller_AutoDetect(t *testing.T) {
	home, _ := isolatedDirs(t)
	t.Setenv("HOME", home)

	// Nothing installed → actionable error.
	in := &Installer{Home: home}
	_, err := in.AutoDetect()
	if err == nil {
		t.Fatal("auto-detect on empty machine: expected error")
	}
	if !strings.Contains(err.Error(), "--agent") {
		t.Errorf("auto-detect error not actionable (no --agent hint): %v", err)
	}

	// ~/.claude present → claude-code detected.
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	agents, err := in.AutoDetect()
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || agents[0].ID != "claude-code" {
		t.Fatalf("auto-detect = %+v, want [claude-code]", agents)
	}
}
