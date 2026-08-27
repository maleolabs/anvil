package agenttarget

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestParseAgentFlag_All(t *testing.T) {
	agents, err := ParseAgentFlag("all")
	if err != nil {
		t.Fatalf("ParseAgentFlag(all): %v", err)
	}
	want := SelectableAgents()
	if len(agents) != len(want) {
		t.Fatalf("all resolved %d agents, want %d", len(agents), len(want))
	}
	for i := range want {
		if agents[i].ID != want[i].ID {
			t.Errorf("all[%d] = %s, want %s", i, agents[i].ID, want[i].ID)
		}
	}
	// all must never include Roo (not selectable in v1).
	for _, a := range agents {
		if a.ID == "roo" {
			t.Error("all includes roo, which is not a selectable --agent value")
		}
	}
}

func TestParseAgentFlag_SelectableValues(t *testing.T) {
	valid := []string{"claude-code", "opencode", "codex", "gemini", "cursor", "zed", "windsurf", "cline"}
	for _, v := range valid {
		agents, err := ParseAgentFlag(v)
		if err != nil {
			t.Fatalf("ParseAgentFlag(%s): %v", v, err)
		}
		if len(agents) != 1 || agents[0].ID != v {
			t.Errorf("ParseAgentFlag(%s) = %+v, want single %s", v, agents, v)
		}
	}
}

func TestParseAgentFlag_Unknown(t *testing.T) {
	_, err := ParseAgentFlag("vim")
	if err == nil {
		t.Fatal("ParseAgentFlag(vim): expected error")
	}
	var unknown *UnknownAgentError
	if !errors.As(err, &unknown) {
		t.Fatalf("error type = %T, want *UnknownAgentError", err)
	}
	if unknown.Value != "vim" {
		t.Errorf("unknown.Value = %s, want vim", unknown.Value)
	}
	if len(unknown.Valid) != len(SelectableIDs()) {
		t.Errorf("unknown.Valid lists %d values, want %d", len(unknown.Valid), len(SelectableIDs()))
	}
}

func TestParseAgentFlag_UnsupportedNotice(t *testing.T) {
	for _, v := range []string{"continue", "aider", "copilot", "roo"} {
		_, err := ParseAgentFlag(v)
		if err == nil {
			t.Fatalf("ParseAgentFlag(%s): expected error", v)
		}
		var unsupported *UnsupportedAgentError
		if !errors.As(err, &unsupported) {
			t.Fatalf("ParseAgentFlag(%s) error type = %T, want *UnsupportedAgentError", v, err)
		}
		if unsupported.Agent != v {
			t.Errorf("unsupported.Agent = %s, want %s", unsupported.Agent, v)
		}
		if unsupported.Notice == "" {
			t.Errorf("ParseAgentFlag(%s): notice is empty — the notice must reach the user", v)
		}
	}
}

func TestParseAgentFlag_Auto(t *testing.T) {
	agents, err := ParseAgentFlag("auto")
	if err != nil {
		t.Fatalf("ParseAgentFlag(auto): %v", err)
	}
	if agents != nil {
		t.Fatalf("auto should return nil for caller-side detection, got %+v", agents)
	}
}

// isolatedDirs returns an isolated home + config dir pair: both point at
// temp dirs so detection never sees the host machine's real folders.
func isolatedDirs(t *testing.T) (home, configDir string) {
	t.Helper()
	home = t.TempDir()
	configDir = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", configDir)
	if got, err := os.UserConfigDir(); err != nil || got != configDir {
		t.Fatalf("cannot redirect UserConfigDir to %s (got %s, err %v)", configDir, got, err)
	}
	return home, configDir
}

func TestDetectAgents_FoldersPresent(t *testing.T) {
	home, configDir := isolatedDirs(t)
	// Present folders: ~/.claude, XDG/opencode, ~/.roo
	dirs := []string{
		filepath.Join(home, ".claude"),
		filepath.Join(configDir, "opencode"),
		filepath.Join(home, ".roo"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	detected := DetectAgents(home)
	ids := map[string]bool{}
	for _, a := range detected {
		ids[a.ID] = true
	}
	if !ids["claude-code"] {
		t.Errorf("~/.claude present but claude-code not detected: %v", ids)
	}
	if !ids["opencode"] {
		t.Errorf("XDG/opencode present but opencode not detected: %v", ids)
	}
	if !ids["roo"] {
		t.Errorf("~/.roo present but roo not detected: %v", ids)
	}
	// Absent folders must not be detected.
	if ids["codex"] || ids["gemini"] || ids["cursor"] || ids["cline"] || ids["zed"] {
		t.Errorf("absent config folders detected as installed: %v", ids)
	}
}

func TestDetectAgents_EmptyHome(t *testing.T) {
	home, _ := isolatedDirs(t)
	if got := DetectAgents(home); len(got) != 0 {
		t.Fatalf("empty home detected %d agents, want 0: %+v", len(got), got)
	}
	if got := DetectAgentsSelectable(home); len(got) != 0 {
		t.Fatalf("empty home detected %d selectable agents, want 0", len(got))
	}
}

func TestDetectAgents_FileNotDir(t *testing.T) {
	home, _ := isolatedDirs(t)
	// A file named .claude must NOT count as the agent being installed.
	if err := os.WriteFile(filepath.Join(home, ".claude"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := DetectAgents(home); len(got) != 0 {
		t.Fatalf("a .claude file counted as installed: %+v", got)
	}
}

func TestDetectAgents_XDGConfigHonored(t *testing.T) {
	home, configDir := isolatedDirs(t)
	// opencode lives under XDG_CONFIG_HOME/opencode (m-2): detection must
	// find it there even though it is not under home.
	if err := os.MkdirAll(filepath.Join(configDir, "opencode"), 0o755); err != nil {
		t.Fatal(err)
	}
	detected := DetectAgents(home)
	ids := map[string]bool{}
	for _, a := range detected {
		ids[a.ID] = true
	}
	if !ids["opencode"] {
		t.Errorf("opencode under XDG_CONFIG_HOME not detected: %v", ids)
	}
	// A folder under home/.config must NOT count when XDG is redirected.
	if err := os.MkdirAll(filepath.Join(home, ".config", "opencode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".config", "zed"), 0o755); err != nil {
		t.Fatal(err)
	}
	detected = DetectAgents(home)
	ids = map[string]bool{}
	for _, a := range detected {
		ids[a.ID] = true
	}
	if ids["zed"] {
		t.Error("zed detected under home/.config despite XDG_CONFIG_HOME redirect")
	}
}

func TestDetectAgents_OnlyRoo(t *testing.T) {
	home, _ := isolatedDirs(t)
	if err := os.MkdirAll(filepath.Join(home, ".roo"), 0o755); err != nil {
		t.Fatal(err)
	}
	all := DetectAgents(home)
	if len(all) != 1 || all[0].ID != "roo" {
		t.Fatalf("only ~/.roo present: DetectAgents = %+v, want [roo]", all)
	}
	// But no selectable agent → an install cannot be triggered.
	if got := DetectAgentsSelectable(home); len(got) != 0 {
		t.Fatalf("roo-only home yielded selectable agents: %+v", got)
	}
}

func TestSelectableIDs_MatchesADR(t *testing.T) {
	want := []string{
		"all", "claude-code", "opencode", "codex", "gemini",
		"cursor", "zed", "windsurf", "cline",
	}
	got := SelectableIDs()
	if len(got) != len(want) {
		t.Fatalf("SelectableIDs length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("SelectableIDs[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestAllAgents_HasExpectedTable(t *testing.T) {
	all := AllAgents()
	if len(all) != 9 {
		t.Fatalf("AllAgents length = %d, want 9 (8 selectable + roo)", len(all))
	}
	seen := map[string]bool{}
	for _, a := range all {
		if seen[a.ID] {
			t.Errorf("duplicate agent ID %s", a.ID)
		}
		seen[a.ID] = true
		if a.DisplayName == "" {
			t.Errorf("agent %s has no display name", a.ID)
		}
	}
}
