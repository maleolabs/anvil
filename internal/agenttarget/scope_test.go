package agenttarget

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Scope semantics (ADR-037 D5; TS-021-02 DoD):
//   - `--scope repo` requires an Anvil project (anvil.yaml discoverable)
//     AND a git root; the master copy lands at the GIT ROOT, not cwd.
//   - `--scope global` requires neither; base is the home directory.
//   - Error messages are actionable (they say how to fix).

func TestParseScope(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want Scope
		ok   bool
	}{
		{"", ScopeRepo, true},
		{"repo", ScopeRepo, true},
		{"global", ScopeGlobal, true},
		{"system", "", false},
		{"REPO", "", false},
	} {
		got, err := ParseScope(tt.in)
		if tt.ok && err != nil {
			t.Errorf("ParseScope(%q): %v", tt.in, err)
		}
		if tt.ok && got != tt.want {
			t.Errorf("ParseScope(%q) = %s, want %s", tt.in, got, tt.want)
		}
		if !tt.ok && err == nil {
			t.Errorf("ParseScope(%q): expected error", tt.in)
		}
	}
}

// chdir changes the working directory for the test and restores it after.
func chdir(t *testing.T, dir string) {
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

// writeAnvilProject creates an anvil.yaml project marker in dir.
func writeAnvilProject(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "anvil.yaml"), []byte("project:\n  name: test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeGitRoot creates a .git entry in dir so it reads as a git root.
func writeGitRoot(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestScopeBase_RepoRequiresAnvilProject(t *testing.T) {
	work := t.TempDir()
	chdir(t, work)

	// No anvil.yaml anywhere: actionable error mentioning anvil init.
	_, err := ScopeBase(ScopeRepo, "")
	if err == nil {
		t.Fatal("repo scope without anvil project: expected error")
	}
	if !strings.Contains(err.Error(), "--scope repo") || !strings.Contains(err.Error(), "anvil init") {
		t.Errorf("repo error not actionable: %v", err)
	}
}

func TestScopeBase_RepoRequiresGitRoot(t *testing.T) {
	work := t.TempDir()
	writeAnvilProject(t, work)
	chdir(t, work)

	// anvil.yaml present but no .git: actionable error mentioning git init.
	_, err := ScopeBase(ScopeRepo, "")
	if err == nil {
		t.Fatal("repo scope without git root: expected error")
	}
	if !strings.Contains(err.Error(), "git") || !strings.Contains(err.Error(), "git init") {
		t.Errorf("repo error not actionable: %v", err)
	}
}

func TestScopeBase_RepoUsesGitRootNotCwd(t *testing.T) {
	// The master copy must land at the git root, not at cwd — even when
	// cwd is a subdirectory of the repo (ADR-037 D5 "git root, not cwd").
	repo := t.TempDir()
	writeGitRoot(t, repo)
	writeAnvilProject(t, repo)
	sub := filepath.Join(repo, "packages", "app")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, sub)

	got, err := ScopeBase(ScopeRepo, "")
	if err != nil {
		t.Fatalf("repo scope from subdirectory: %v", err)
	}
	if filepath.Clean(got) != filepath.Clean(repo) {
		t.Errorf("repo base = %s, want git root %s", got, repo)
	}
}

func TestScopeBase_RepoFindsGitRootFromSubdir(t *testing.T) {
	repo := t.TempDir()
	writeGitRoot(t, repo)
	writeAnvilProject(t, repo) // anvil.yaml at repo root
	sub := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, sub)

	got, err := ScopeBase(ScopeRepo, "")
	if err != nil {
		t.Fatalf("repo scope: %v", err)
	}
	if filepath.Clean(got) != filepath.Clean(repo) {
		t.Errorf("repo base = %s, want %s", got, repo)
	}
}

func TestScopeBase_GlobalNoProjectNeeded(t *testing.T) {
	work := t.TempDir()
	chdir(t, work)

	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := ScopeBase(ScopeGlobal, "")
	if err != nil {
		t.Fatalf("global scope: %v", err)
	}
	want := home
	if !strings.HasSuffix(got, want) && got != want {
		t.Errorf("global base = %s, want %s", got, want)
	}
}

func TestScopeBase_InvalidScope(t *testing.T) {
	if _, err := ScopeBase(Scope("nope"), ""); err == nil {
		t.Error("invalid scope: expected error")
	}
}
