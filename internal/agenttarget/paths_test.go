package agenttarget

import (
	"path/filepath"
	"testing"
)

// The per-agent path table is pinned here for BOTH scopes (ADR-037 §9.3,
// TS-021-02 DoD): a change in any agent's resolution is a deliberate
// decision that must update this table.
//
// Base convention: repo scope base = git root; global scope base = home.
// Master copy: <base>/.agents/skills/<name>/.
// Native locations (claude-code, cursor): <base>/.claude/skills/<name>,
// <base>/.cursor/skills/<name>.
//
// Lone install semantics (ADR-037 D6 "writes only that agent's native
// location so other agents never see it"):
//   - a lone native-only agent (claude-code, cursor) lands as a real copy
//     at its native location, with NO master;
//   - a lone reader agent (opencode, codex, gemini, zed, windsurf, cline,
//     roo) lands at the master (`.agents/skills` is its native readable
//     location).
//
// `--agent all` semantics: master + native symlinks for claude-code and
// cursor; readers read the master.

// agentTableExpected defines each agent's resolved path for a scope when
// installed within `all` (master present) and when installed alone.
type agentTableExpected struct {
	agentID      string
	allPath      string // target path when part of `all`
	allKind      TargetKind
	lonePath     string // target path when installed alone
	loneKind     TargetKind
	loneNoMaster bool // lone install creates no master
}

func expectedTable(scope Scope, base, name string) []agentTableExpected {
	master := filepath.Join(base, ".agents", "skills", name)
	claude := filepath.Join(base, ".claude", "skills", name)
	cursor := filepath.Join(base, ".cursor", "skills", name)
	return []agentTableExpected{
		{"claude-code", claude, TargetKindSymlink, claude, TargetKindCopy, true},
		{"opencode", master, TargetKindMaster, master, TargetKindMaster, false},
		{"codex", master, TargetKindMaster, master, TargetKindMaster, false},
		{"gemini", master, TargetKindMaster, master, TargetKindMaster, false},
		{"cursor", cursor, TargetKindSymlink, cursor, TargetKindCopy, true},
		{"zed", master, TargetKindMaster, master, TargetKindMaster, false},
		{"windsurf", master, TargetKindMaster, master, TargetKindMaster, false},
		{"cline", master, TargetKindMaster, master, TargetKindMaster, false},
		{"roo", master, TargetKindMaster, master, TargetKindMaster, false},
	}
}

func TestResolve_RepoScope_AllAndLone(t *testing.T) {
	base := "/tmp/anvil-repo" // git root
	name := "anvil-overview"
	scope := ScopeRepo

	for _, exp := range expectedTable(scope, base, name) {
		a, ok := agentsByID[exp.agentID]
		if !ok {
			t.Fatalf("agent %s not in table", exp.agentID)
		}

		// Lone install.
		lone, err := Resolve([]Agent{a}, scope, base, name)
		if err != nil {
			t.Fatalf("Resolve(%s lone): %v", exp.agentID, err)
		}
		if lone.Master == "" != exp.loneNoMaster {
			t.Errorf("%s lone repo: master empty = %v, want %v", exp.agentID, lone.Master == "", exp.loneNoMaster)
		}
		gotLone := findAgentTarget(lone, exp.agentID)
		if gotLone == nil {
			t.Fatalf("%s lone repo: no agent target in %+v", exp.agentID, lone.Targets)
		}
		if gotLone.Path != exp.lonePath {
			t.Errorf("%s lone repo: path = %s, want %s", exp.agentID, gotLone.Path, exp.lonePath)
		}
		if gotLone.Kind != exp.loneKind {
			t.Errorf("%s lone repo: kind = %s, want %s", exp.agentID, gotLone.Kind, exp.loneKind)
		}
	}

	// `all`: master + symlinks.
	allAgents, err := ParseAgentFlag("all")
	if err != nil {
		t.Fatal(err)
	}
	allSet, err := Resolve(allAgents, scope, base, name)
	if err != nil {
		t.Fatal(err)
	}
	if allSet.Master == "" {
		t.Fatal("all repo: master must exist")
	}
	for _, exp := range expectedTable(scope, base, name) {
		if exp.agentID == "roo" {
			continue // roo is not selectable → not in `all`
		}
		got := findAgentTarget(allSet, exp.agentID)
		if got == nil {
			t.Fatalf("all repo: missing %s target", exp.agentID)
		}
		if got.Path != exp.allPath {
			t.Errorf("all repo %s: path = %s, want %s", exp.agentID, got.Path, exp.allPath)
		}
		if got.Kind != exp.allKind {
			t.Errorf("all repo %s: kind = %s, want %s", exp.agentID, got.Kind, exp.allKind)
		}
	}
	if findAgentTarget(allSet, "roo") != nil {
		t.Error("all repo: roo must not be included")
	}
}

func TestResolve_GlobalScope_AllAndLone(t *testing.T) {
	base := "/home/user" // home dir
	name := "anvil-overview"
	scope := ScopeGlobal

	for _, exp := range expectedTable(scope, base, name) {
		a, ok := agentsByID[exp.agentID]
		if !ok {
			t.Fatalf("agent %s not in table", exp.agentID)
		}
		lone, err := Resolve([]Agent{a}, scope, base, name)
		if err != nil {
			t.Fatalf("Resolve(%s lone): %v", exp.agentID, err)
		}
		if lone.Master == "" != exp.loneNoMaster {
			t.Errorf("%s lone global: master empty = %v, want %v", exp.agentID, lone.Master == "", exp.loneNoMaster)
		}
		gotLone := findAgentTarget(lone, exp.agentID)
		if gotLone == nil {
			t.Fatalf("%s lone global: no agent target", exp.agentID)
		}
		if gotLone.Path != exp.lonePath {
			t.Errorf("%s lone global: path = %s, want %s", exp.agentID, gotLone.Path, exp.lonePath)
		}
		if gotLone.Kind != exp.loneKind {
			t.Errorf("%s lone global: kind = %s, want %s", exp.agentID, gotLone.Kind, exp.loneKind)
		}
	}

	allAgents, err := ParseAgentFlag("all")
	if err != nil {
		t.Fatal(err)
	}
	allSet, err := Resolve(allAgents, scope, base, name)
	if err != nil {
		t.Fatal(err)
	}
	if allSet.Master == "" {
		t.Fatal("all global: master must exist")
	}
	for _, exp := range expectedTable(scope, base, name) {
		if exp.agentID == "roo" {
			continue
		}
		got := findAgentTarget(allSet, exp.agentID)
		if got == nil {
			t.Fatalf("all global: missing %s target", exp.agentID)
		}
		if got.Path != exp.allPath {
			t.Errorf("all global %s: path = %s, want %s", exp.agentID, got.Path, exp.allPath)
		}
		if got.Kind != exp.allKind {
			t.Errorf("all global %s: kind = %s, want %s", exp.agentID, got.Kind, exp.allKind)
		}
	}
}

func TestResolve_InvalidInputs(t *testing.T) {
	base := "/tmp/x"
	if _, err := Resolve(nil, ScopeRepo, base, "name"); err == nil {
		t.Error("empty agents: expected error")
	}
	if _, err := Resolve([]Agent{AgentClaudeCode}, ScopeRepo, "relative", "name"); err == nil {
		t.Error("relative base: expected error")
	}
	for _, bad := range []string{"", "..", "a b", "-x", "X", "a..b", "a_b"} {
		if _, err := Resolve([]Agent{AgentClaudeCode}, ScopeRepo, base, bad); err == nil {
			t.Errorf("invalid skill name %q: expected error", bad)
		}
	}
}

func TestResolve_ValidSkillNames(t *testing.T) {
	base := "/tmp/x"
	for _, good := range []string{"a", "anvil-overview", "a1", "x-y-z", "123"} {
		if _, err := Resolve([]Agent{AgentClaudeCode}, ScopeRepo, base, good); err != nil {
			t.Errorf("valid skill name %q rejected: %v", good, err)
		}
	}
}

func TestNativePath_PerAgent(t *testing.T) {
	// Agents reading .agents/skills natively have no native path.
	for _, a := range []Agent{AgentOpenCode, AgentCodex, AgentGemini, AgentZed, AgentWindsurf, AgentCline, AgentRoo} {
		if p := nativePath(a, ScopeRepo, "/base", "n"); p != "" {
			t.Errorf("%s nativePath = %q, want empty", a.ID, p)
		}
	}
	if p := nativePath(AgentClaudeCode, ScopeRepo, "/base", "n"); p != filepath.Join("/base", ".claude", "skills", "n") {
		t.Errorf("claude-code nativePath = %q", p)
	}
	if p := nativePath(AgentCursor, ScopeGlobal, "/base", "n"); p != filepath.Join("/base", ".cursor", "skills", "n") {
		t.Errorf("cursor nativePath = %q", p)
	}
}

// findAgentTarget returns the target for an agent ID within a set.
func findAgentTarget(set *ResolvedSet, agentID string) *Target {
	for i := range set.Targets {
		if set.Targets[i].Agent == agentID {
			return &set.Targets[i]
		}
	}
	return nil
}
