package agenttarget

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// skillFiles returns a small representative skill tree.
func skillFiles() map[string][]byte {
	return map[string][]byte{
		"SKILL.md":                []byte("---\nname: anvil-overview\ndescription: x\n---\n# Overview\n"),
		"references/REFERENCE.md": []byte("# Reference\n"),
		"assets/template.txt":     []byte("template"),
	}
}

func TestWriteMaster_AtomicAndComplete(t *testing.T) {
	base := t.TempDir()
	set, err := Resolve([]Agent{AgentOpenCode}, ScopeRepo, base, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteMaterializes(set, skillFiles(), WriterOptions{}); err != nil {
		t.Fatal(err)
	}

	master := filepath.Join(base, ".agents", "skills", "anvil-overview")
	for _, rel := range []string{"SKILL.md", "references/REFERENCE.md", "assets/template.txt"} {
		p := filepath.Join(master, rel)
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("master file %s missing: %v", rel, err)
		}
		if info.Mode().Perm() != 0o644 {
			t.Errorf("%s mode = %v, want 0644", rel, info.Mode().Perm())
		}
	}
}

func TestWriteMaterializes_SymlinkNativeToMaster(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	base := t.TempDir()
	allAgents, err := ParseAgentFlag("all")
	if err != nil {
		t.Fatal(err)
	}
	allSet, err := Resolve(allAgents, ScopeGlobal, base, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteMaterializes(allSet, skillFiles(), WriterOptions{}); err != nil {
		t.Fatal(err)
	}

	// Real .claude/skills layout: the native dir is a symlink to master.
	link := filepath.Join(base, ".claude", "skills", "anvil-overview")
	master := filepath.Join(base, ".agents", "skills", "anvil-overview")

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat %s: %v", link, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is not a symlink (mode %v)", link, info.Mode())
	}
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(target) != filepath.Clean(master) {
		t.Errorf("symlink target = %s, want %s", target, master)
	}

	// The skill is readable THROUGH the symlink.
	viaLink := filepath.Join(link, "SKILL.md")
	data, err := os.ReadFile(viaLink)
	if err != nil {
		t.Fatalf("read via symlink: %v", err)
	}
	if len(data) == 0 {
		t.Error("SKILL.md via symlink is empty")
	}

	// Master still holds the real content.
	if _, err := os.Stat(filepath.Join(master, "SKILL.md")); err != nil {
		t.Errorf("master SKILL.md missing: %v", err)
	}
}

func TestWriteMaterializes_CopyFallback_ProducesFullTree(t *testing.T) {
	base := t.TempDir()
	allAgents, err := ParseAgentFlag("all")
	if err != nil {
		t.Fatal(err)
	}
	set, err := Resolve(allAgents, ScopeRepo, base, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}

	// Force the Windows/no-privilege path: copy fallback.
	if err := WriteMaterializes(set, skillFiles(), WriterOptions{ForceCopy: true}); err != nil {
		t.Fatal(err)
	}

	master := filepath.Join(base, ".agents", "skills", "anvil-overview")
	// Native locations are now full copies, not symlinks.
	for _, rel := range []string{".claude/skills/anvil-overview", ".cursor/skills/anvil-overview"} {
		dir := filepath.Join(base, rel)
		info, err := os.Lstat(dir)
		if err != nil {
			t.Fatalf("copy target %s missing: %v", dir, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Errorf("%s is a symlink under copy fallback", dir)
		}
		if !info.IsDir() {
			t.Errorf("%s is not a directory", dir)
		}
		for _, f := range []string{"SKILL.md", "references/REFERENCE.md", "assets/template.txt"} {
			if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
				t.Errorf("copy fallback: %s/%s missing: %v", rel, f, err)
			}
		}
	}

	// Copies must be byte-identical to the master.
	masterData, _ := os.ReadFile(filepath.Join(master, "SKILL.md"))
	copyData, _ := os.ReadFile(filepath.Join(base, ".claude", "skills", "anvil-overview", "SKILL.md"))
	if string(masterData) != string(copyData) {
		t.Error("copy fallback content differs from master")
	}
}

func TestWriteMaterializes_LoneNativeCopy(t *testing.T) {
	base := t.TempDir()
	set, err := Resolve([]Agent{AgentClaudeCode}, ScopeRepo, base, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}
	if set.Master != "" {
		t.Fatalf("lone claude-code must not create master, got %s", set.Master)
	}
	if err := WriteMaterializes(set, skillFiles(), WriterOptions{}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, ".claude", "skills", "anvil-overview")
	info, err := os.Lstat(dir)
	if err != nil {
		t.Fatalf("lone native copy missing: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("lone native must be a real copy, not a symlink")
	}
	// No master anywhere.
	if _, err := os.Stat(filepath.Join(base, ".agents", "skills", "anvil-overview")); !os.IsNotExist(err) {
		t.Error("lone claude-code created a master copy")
	}
}

func TestWriteMaterializes_RejectsUnsafeRelPaths(t *testing.T) {
	base := t.TempDir()
	set, err := Resolve([]Agent{AgentOpenCode}, ScopeRepo, base, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../escape", "/abs", "a/../../x", "a\\b", "", "."} {
		files := map[string][]byte{bad: []byte("x")}
		if err := WriteMaterializes(set, files, WriterOptions{}); err == nil {
			t.Errorf("unsafe rel path %q accepted", bad)
		}
	}
}

func TestWriteMaterializes_EmptyFilesRejected(t *testing.T) {
	base := t.TempDir()
	set, err := Resolve([]Agent{AgentOpenCode}, ScopeRepo, base, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteMaterializes(set, map[string][]byte{}, WriterOptions{}); err == nil {
		t.Error("empty files accepted")
	}
}

func TestWriteMaterializes_FailureRollsBack(t *testing.T) {
	base := t.TempDir()
	set, err := Resolve([]Agent{AgentOpenCode}, ScopeRepo, base, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}
	// One bad path poisons the whole write: nothing may be left behind.
	files := map[string][]byte{
		"SKILL.md": []byte("ok"),
		"../bad":   []byte("boom"),
	}
	if err := WriteMaterializes(set, files, WriterOptions{}); err == nil {
		t.Fatal("expected error for unsafe path")
	}
	if _, err := os.Stat(filepath.Join(base, ".agents", "skills", "anvil-overview")); !os.IsNotExist(err) {
		t.Error("failed install left master behind")
	}
}

func TestWriteMaterializes_MidWriteFailureRollsBackMaster(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink failure semantics differ on Windows")
	}
	base := t.TempDir()
	allAgents, _ := ParseAgentFlag("all")
	set, err := Resolve(allAgents, ScopeRepo, base, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}

	// Occupy the native claude location with a plain file, bypassing the
	// conflict gate (which would block earlier). The symlink publish must
	// fail → the already-written master must be rolled back.
	nativeDir := filepath.Join(base, ".claude", "skills")
	if err := os.MkdirAll(nativeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nativeDir, "anvil-overview"), []byte("occupied"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := WriteMaterializes(set, skillFiles(), WriterOptions{}); err == nil {
		t.Fatal("expected symlink publish failure")
	}
	if _, err := os.Stat(filepath.Join(base, ".agents", "skills", "anvil-overview")); !os.IsNotExist(err) {
		t.Error("failed install left master behind after native-target failure")
	}
	// The user's occupying file must survive.
	if _, err := os.Stat(filepath.Join(nativeDir, "anvil-overview")); err != nil {
		t.Errorf("user's occupying file was removed: %v", err)
	}
}

func TestReadAllTargets_Deduplicates(t *testing.T) {
	base := t.TempDir()
	allAgents, _ := ParseAgentFlag("all")
	set, err := Resolve(allAgents, ScopeRepo, base, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}
	paths := ReadAllTargets(set)
	if len(paths) != 3 { // master + claude + cursor
		t.Fatalf("ReadAllTargets = %d paths, want 3: %v", len(paths), paths)
	}
	seen := map[string]bool{}
	for _, p := range paths {
		if seen[p] {
			t.Errorf("duplicate path %s", p)
		}
		seen[p] = true
	}
}

func TestWriteMaterializes_RefusesSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	base := t.TempDir()
	// A malicious `.agents` symlink must not redirect the install.
	escape := t.TempDir()
	if err := os.Symlink(escape, filepath.Join(base, ".agents")); err != nil {
		t.Fatal(err)
	}

	set, err := Resolve([]Agent{AgentOpenCode}, ScopeRepo, base, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}
	err = WriteMaterializes(set, skillFiles(), WriterOptions{})
	if err == nil {
		t.Fatal("install through a symlinked .agents must fail")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("error should mention the symlink: %v", err)
	}
	// Nothing written through the symlink.
	if _, err := os.Stat(filepath.Join(escape, "skills", "anvil-overview", "SKILL.md")); !os.IsNotExist(err) {
		t.Error("content escaped through the .agents symlink")
	}
	// The escape dir itself must not be removed.
	if _, err := os.Stat(escape); err != nil {
		t.Errorf("escape target was removed: %v", err)
	}
}

func TestWriteMaterializes_IntermediateSymlinkRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	base := t.TempDir()
	// Deeper escape: `.agents/skills` exists but `.agents` is a symlink.
	escape := t.TempDir()
	if err := os.MkdirAll(filepath.Join(escape, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(escape, filepath.Join(base, ".agents")); err != nil {
		t.Fatal(err)
	}

	set, err := Resolve([]Agent{AgentOpenCode}, ScopeRepo, base, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteMaterializes(set, skillFiles(), WriterOptions{}); err == nil {
		t.Fatal("install through symlinked intermediate must fail")
	}
	if _, err := os.Stat(filepath.Join(escape, "skills", "anvil-overview")); !os.IsNotExist(err) {
		t.Error("content escaped through intermediate symlink")
	}
}

// TestIsSymlinkOrReparse_PortableBackstop pins the M2 fix: detection must
// work via the portable EvalSymlinks backstop even when the platform
// reports the entry as a plain directory (Windows junctions behave exactly
// like this — os.Lstat reports them as directories).
func TestIsSymlinkOrReparse_PortableBackstop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	base := t.TempDir()
	realDir := filepath.Join(base, "real")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// A symlink to a directory: Lstat shows ModeSymlink, but the portable
	// backstop must ALSO flag it (defense in depth).
	link := filepath.Join(base, "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if !isSymlinkOrReparse(info, link) {
		t.Error("symlink not detected by isSymlinkOrReparse")
	}

	// A real directory: neither Lstat nor the backstop flags it.
	dirInfo, err := os.Lstat(realDir)
	if err != nil {
		t.Fatal(err)
	}
	if isSymlinkOrReparse(dirInfo, realDir) {
		t.Error("real directory flagged as symlink/reparse")
	}

	// The path through a symlinked parent resolves differently: the
	// backstop catches a junction-style redirect even when the FINAL
	// component is a plain directory.
	viaLink := filepath.Join(link, "sub")
	if err := os.MkdirAll(viaLink, 0o755); err != nil {
		t.Fatal(err)
	}
	subInfo, err := os.Lstat(viaLink)
	if err != nil {
		t.Fatal(err)
	}
	if !isSymlinkOrReparse(subInfo, viaLink) {
		t.Error("path through symlinked parent not flagged by portable backstop")
	}
}
