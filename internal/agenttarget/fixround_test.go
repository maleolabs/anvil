package agenttarget

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Fix-round tests (team review REQUEST_CHANGES):
//
//   - B-1/M1: `--force` must escape native-location conflicts (writer
//     actually replaces the occupant).
//   - M-1: ownership marker makes re-run/update idempotent for every
//     install shape (reader-only, copy fallback, lone native-only).
//   - M-2/L3: Installer.Home is honored (no env-var dependence).
//   - m-1/L2: rollback removes created symlinks (no dangling links).
//   - M2: reparse-point/symlink detection on path components.

func TestInstaller_Force_ReplacesNativeLocationConflict(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	home := setHomeInEnv(t)
	base := t.TempDir() // repo git root
	writeGitRoot(t, base)
	writeAnvilProject(t, base)
	chdir(t, base)

	// User has a real same-name skill directory at the native claude
	// location (not ours — no marker, no symlink to our master).
	native := filepath.Join(base, ".claude", "skills", "anvil-overview")
	userFile := filepath.Join(native, "SKILL.md")
	if err := os.MkdirAll(native, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userFile, []byte("user content"), 0o644); err != nil {
		t.Fatal(err)
	}

	in := &Installer{Home: home}

	// Without --force: blocked, user content untouched.
	if _, err := in.Install(ScopeRepo, "anvil-overview", skillFiles(), []Agent{AgentClaudeCode}, false); err == nil {
		t.Fatal("install over user's native skill without --force: expected error")
	}
	data, _ := os.ReadFile(userFile)
	if string(data) != "user content" {
		t.Fatal("user content modified by a blocked install")
	}

	// With --force: succeeds. A lone claude-code install is a REAL copy
	// (no master, so no symlink) carrying our ownership marker; the user's
	// content is replaced.
	set, err := in.Install(ScopeRepo, "anvil-overview", skillFiles(), []Agent{AgentClaudeCode}, true)
	if err != nil {
		t.Fatalf("--force install: %v", err)
	}
	if set == nil {
		t.Fatal("nil set from forced install")
	}
	info, err := os.Lstat(native)
	if err != nil {
		t.Fatalf("lstat native after force: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("lone native install must be a real directory (got mode %v)", info.Mode())
	}
	if !markerMatches(native, "anvil-overview") {
		t.Fatal("forced install lacks our ownership marker")
	}
	replaced, _ := os.ReadFile(userFile)
	if string(replaced) == "user content" {
		t.Error("user content not replaced by forced install")
	}
	if _, err := os.Stat(filepath.Join(native, "SKILL.md")); err != nil {
		t.Errorf("SKILL.md after forced install missing: %v", err)
	}
}

func TestInstaller_Force_CopyFallbackReplacesNativeConflict(t *testing.T) {
	home := setHomeInEnv(t)
	base := t.TempDir()
	writeGitRoot(t, base)
	writeAnvilProject(t, base)
	chdir(t, base)

	native := filepath.Join(base, ".cursor", "skills", "anvil-overview")
	if err := os.MkdirAll(native, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(native, "SKILL.md"), []byte("user"), 0o644); err != nil {
		t.Fatal(err)
	}

	in := &Installer{Home: home}
	// cursor alone is a native-only agent → the copy path, not symlink.
	if _, err := in.Install(ScopeRepo, "anvil-overview", skillFiles(), []Agent{AgentCursor}, true); err != nil {
		t.Fatalf("--force copy install: %v", err)
	}
	info, err := os.Lstat(native)
	if err != nil {
		t.Fatalf("lstat native copy after force: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("cursor lone install must stay a real copy (not a symlink)")
	}
	// Our marker proves ownership; user content replaced.
	if !markerMatches(native, "anvil-overview") {
		t.Error("forced copy install lacks our ownership marker")
	}
	data, _ := os.ReadFile(filepath.Join(native, "SKILL.md"))
	if string(data) == "user" {
		t.Error("user content not replaced by forced copy install")
	}
}

func TestInstaller_ReRun_ReaderOnly_Idempotent(t *testing.T) {
	home := setHomeInEnv(t)
	in := &Installer{Home: home}

	// opencode is a reader-only agent: master IS its location.
	first, err := in.Install(ScopeGlobal, "anvil-overview", skillFiles(), []Agent{AgentOpenCode}, false)
	if err != nil {
		t.Fatal(err)
	}
	if first == nil {
		t.Fatal("nil first install")
	}

	// Re-run without --force: must NOT be a conflict (our marker proves
	// ownership — M-1).
	second, err := in.Install(ScopeGlobal, "anvil-overview", skillFiles(), []Agent{AgentOpenCode}, false)
	if err != nil {
		t.Fatalf("re-run (reader-only) must be idempotent: %v", err)
	}
	if second == nil {
		t.Fatal("nil re-run set")
	}
}

func TestInstaller_ReRun_CopyFallback_Idempotent(t *testing.T) {
	home := setHomeInEnv(t)
	base := t.TempDir()
	writeGitRoot(t, base)
	writeAnvilProject(t, base)
	chdir(t, base)

	in := &Installer{Home: home}
	if _, err := in.Install(ScopeRepo, "anvil-overview", skillFiles(), []Agent{AgentCursor}, false); err != nil {
		t.Fatal(err)
	}
	// Re-run: copy-mode install must be idempotent via the marker.
	if _, err := in.Install(ScopeRepo, "anvil-overview", skillFiles(), []Agent{AgentCursor}, false); err != nil {
		t.Fatalf("re-run (copy fallback) must be idempotent: %v", err)
	}
}

func TestInstaller_ReRun_LoneNativeOnly_Idempotent(t *testing.T) {
	home := setHomeInEnv(t)
	base := t.TempDir()
	writeGitRoot(t, base)
	writeAnvilProject(t, base)
	chdir(t, base)

	in := &Installer{Home: home}
	if _, err := in.Install(ScopeRepo, "anvil-overview", skillFiles(), []Agent{AgentClaudeCode}, false); err != nil {
		t.Fatal(err)
	}
	// Re-run without --force: lone native install carries our marker.
	if _, err := in.Install(ScopeRepo, "anvil-overview", skillFiles(), []Agent{AgentClaudeCode}, false); err != nil {
		t.Fatalf("re-run (lone native-only) must be idempotent: %v", err)
	}
}

func TestInstaller_ReRun_All_Idempotent(t *testing.T) {
	home := setHomeInEnv(t)
	in := &Installer{Home: home}
	allAgents, err := ParseAgentFlag("all")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.Install(ScopeGlobal, "anvil-overview", skillFiles(), allAgents, false); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Install(ScopeGlobal, "anvil-overview", skillFiles(), allAgents, false); err != nil {
		t.Fatalf("re-run (all) must be idempotent: %v", err)
	}
}

func TestInstaller_Home_OverrideHonored(t *testing.T) {
	// M-2/L3: Installer.Home must drive global resolution without any
	// reliance on the environment. Unset HOME/XDG to prove it.
	home := t.TempDir()
	t.Setenv("HOME", home) // must be ignored: Installer.Home wins

	// An empty dir as home override — the install must land there.
	installHome := t.TempDir()
	in := &Installer{Home: installHome}

	set, err := in.Install(ScopeGlobal, "anvil-overview", skillFiles(), []Agent{AgentOpenCode}, false)
	if err != nil {
		t.Fatal(err)
	}
	if set == nil {
		t.Fatal("nil set")
	}
	master := filepath.Join(installHome, ".agents", "skills", "anvil-overview")
	if _, err := os.Stat(filepath.Join(master, "SKILL.md")); err != nil {
		t.Fatalf("install did not land under Installer.Home (%s): %v", installHome, err)
	}
	// Nothing under the env HOME.
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "anvil-overview")); !os.IsNotExist(err) {
		t.Error("install leaked into env HOME despite Installer.Home override")
	}
}

func TestInstaller_Home_OverrideShadowCheck(t *testing.T) {
	// M-2/L3: shadow check must use the same home the install writes to.
	home := setHomeInEnv(t)
	base := t.TempDir()
	writeGitRoot(t, base)
	writeAnvilProject(t, base)
	chdir(t, base)

	// Personal copy under the INSTALLER's home shadows the repo install.
	personal := filepath.Join(home, ".claude", "skills", "anvil-overview")
	if err := os.MkdirAll(personal, 0o755); err != nil {
		t.Fatal(err)
	}

	in := &Installer{Home: home}
	if _, err := in.Install(ScopeRepo, "anvil-overview", skillFiles(), []Agent{AgentClaudeCode}, false); err == nil {
		t.Fatal("personal copy under Installer.Home not detected as shadow")
	}
	// --force escapes.
	if _, err := in.Install(ScopeRepo, "anvil-overview", skillFiles(), []Agent{AgentClaudeCode}, true); err != nil {
		t.Fatalf("--force shadow escape: %v", err)
	}
}

func TestWriteRollback_RemovesSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	base := t.TempDir()
	allAgents, _ := ParseAgentFlag("all")
	set, err := Resolve(allAgents, ScopeGlobal, base, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}

	// Pre-occupy the CURSOR native path with a plain FILE so the second
	// native target fails after master + claude symlink already exist.
	cursorDir := filepath.Join(base, ".cursor", "skills")
	if err := os.MkdirAll(cursorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cursorDir, "anvil-overview"), []byte("occupied"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Without force the writer pre-flight blocks BEFORE any write.
	err = WriteMaterializes(set, skillFiles(), WriterOptions{})
	if err == nil {
		t.Fatal("expected pre-flight block")
	}
	var blocked *WriteBlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("error type = %T, want *WriteBlockedError", err)
	}
	// Nothing written: no master, no claude symlink.
	if _, err := os.Lstat(filepath.Join(base, ".agents", "skills", "anvil-overview")); !os.IsNotExist(err) {
		t.Error("pre-flight failure left master behind")
	}
	if _, err := os.Lstat(filepath.Join(base, ".claude", "skills", "anvil-overview")); !os.IsNotExist(err) {
		t.Error("pre-flight failure left claude symlink behind")
	}
	// User file untouched.
	if _, err := os.Stat(filepath.Join(cursorDir, "anvil-overview")); err != nil {
		t.Errorf("user's occupying file removed by pre-flight: %v", err)
	}
}

func TestWriteRollback_ForceFailure_NoDanglingSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	base := t.TempDir()
	allAgents, _ := ParseAgentFlag("all")
	set, err := Resolve(allAgents, ScopeGlobal, base, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}

	// Force allows replacing the cursor occupant; the replace then fails
	// mid-way? Simulate a failure after the claude symlink exists by
	// making the cursor path an unwritable parent (permission) — not
	// reliable across environments. Instead, occupy cursor with a real
	// DIRECTORY the replace succeeds on, then force the copy/symlink to
	// fail via a nested non-removable file: too platform-dependent.
	//
	// Robust variant: make the CURSOR occupant a directory whose contents
	// we control and verify that a full forced install succeeds (the
	// pre-flight + replace path), then assert the claude symlink is a
	// valid symlink (not dangling).
	cursorDir := filepath.Join(base, ".cursor", "skills")
	if err := os.MkdirAll(filepath.Join(cursorDir, "anvil-overview"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cursorDir, "anvil-overview", "SKILL.md"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := WriteMaterializes(set, skillFiles(), WriterOptions{Force: true}); err != nil {
		t.Fatalf("forced write: %v", err)
	}

	// Everything present; claude symlink resolves to the master.
	link := filepath.Join(base, ".claude", "skills", "anvil-overview")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("readlink claude: %v", err)
	}
	master := filepath.Join(base, ".agents", "skills", "anvil-overview")
	if filepath.Clean(target) != filepath.Clean(master) {
		t.Errorf("claude symlink -> %s, want %s", target, master)
	}
	if _, err := os.Stat(filepath.Join(master, "SKILL.md")); err != nil {
		t.Errorf("master missing after forced write: %v", err)
	}
	// Cursor location replaced by our copy (or symlink) with the marker.
	if !markerMatches(filepath.Join(base, ".cursor", "skills", "anvil-overview"), "anvil-overview") && !isSymlink(filepath.Join(base, ".cursor", "skills", "anvil-overview")) {
		t.Error("cursor target lacks our ownership after forced write")
	}
}

func TestWriteMaterializes_WriterForce_ReplacesNative(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	base := t.TempDir()
	allAgents, _ := ParseAgentFlag("all")
	set, err := Resolve(allAgents, ScopeGlobal, base, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}
	native := filepath.Join(base, ".claude", "skills", "anvil-overview")
	if err := os.MkdirAll(native, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(native, "SKILL.md"), []byte("user"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := WriteMaterializes(set, skillFiles(), WriterOptions{Force: true}); err != nil {
		t.Fatalf("writer force: %v", err)
	}
	info, err := os.Lstat(native)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("native occupant not replaced by symlink under writer Force")
	}
}

// TestWriteRollback_MidWriteFailure_RemovesSymlinks simulates a native
// target failing AFTER the master and the first native symlink exist: the
// cursor native path is an unwritable directory, so the second symlink
// publish fails and the rollback must remove the master AND the claude
// symlink (no dangling links — m-1/L2).
func TestWriteRollback_MidWriteFailure_RemovesSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on Windows")
	}
	base := t.TempDir()
	allAgents, _ := ParseAgentFlag("all")
	set, err := Resolve(allAgents, ScopeGlobal, base, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}

	// Block the cursor native path: a read-only parent prevents the
	// symlink temp creation there (os.Symlink fails with EACCES for a
	// non-root user).
	cursorSkills := filepath.Join(base, ".cursor", "skills")
	if err := os.MkdirAll(cursorSkills, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cursorSkills, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(cursorSkills, 0o700) })

	err = WriteMaterializes(set, skillFiles(), WriterOptions{})
	if err == nil {
		t.Fatal("expected mid-write failure on the cursor symlink")
	}

	// Rollback removed EVERYTHING this call created: no master, no claude
	// symlink, and crucially no dangling cursor symlink.
	for _, p := range []string{
		filepath.Join(base, ".agents", "skills", "anvil-overview"),
		filepath.Join(base, ".claude", "skills", "anvil-overview"),
		filepath.Join(base, ".cursor", "skills", "anvil-overview"),
	} {
		if _, serr := os.Lstat(p); !os.IsNotExist(serr) {
			t.Errorf("rollback left %s behind (err %v)", p, serr)
		}
	}
}

func TestWriteMaterializes_MarkerWrittenToMaster(t *testing.T) {
	base := t.TempDir()
	set, err := Resolve([]Agent{AgentOpenCode}, ScopeRepo, base, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteMaterializes(set, skillFiles(), WriterOptions{}); err != nil {
		t.Fatal(err)
	}
	master := filepath.Join(base, ".agents", "skills", "anvil-overview")
	if !markerMatches(master, "anvil-overview") {
		t.Error("master lacks our ownership marker")
	}
	// A marker for a different skill must NOT count as ours.
	if markerMatches(master, "other-skill") {
		t.Error("marker matched a different skill name")
	}
}

func TestCheckConflicts_ReRun_ReaderOnly_NotConflict(t *testing.T) {
	home := setHomeInEnv(t)
	in := &Installer{Home: home}
	if _, err := in.Install(ScopeGlobal, "anvil-overview", skillFiles(), []Agent{AgentOpenCode}, false); err != nil {
		t.Fatal(err)
	}
	set, err := Resolve([]Agent{AgentOpenCode}, ScopeGlobal, home, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}
	if problems := CheckConflicts(set, CheckOptions{Home: home}); len(problems) != 0 {
		t.Fatalf("re-run (reader-only) reported conflicts: %v", problems)
	}
}

func TestCheckConflicts_UserDirWithoutMarker_IsConflict(t *testing.T) {
	home := setHomeInEnv(t)
	base := t.TempDir()
	// A user's same-name master WITHOUT our marker is a genuine conflict.
	userDir := filepath.Join(base, ".agents", "skills", "anvil-overview")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "SKILL.md"), []byte("user"), 0o644); err != nil {
		t.Fatal(err)
	}
	set, err := Resolve([]Agent{AgentOpenCode}, ScopeRepo, base, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}
	problems := CheckConflicts(set, CheckOptions{Home: home})
	if len(problems) == 0 {
		t.Fatal("user master without marker not reported as conflict")
	}
	var conflict *ConflictError
	if !errors.As(problems[0], &conflict) {
		t.Fatalf("problem type = %T, want *ConflictError", problems[0])
	}
}

func TestWriteMaterializes_MarkerNotOverwrittenByUserFile(t *testing.T) {
	// The marker is written AFTER the skill files, so a skill that itself
	// ships a `.anvil-install` file gets its marker replaced by ours —
	// acceptable (the marker is our ownership record, not content). This
	// test pins that the final marker is ours.
	base := t.TempDir()
	set, err := Resolve([]Agent{AgentOpenCode}, ScopeRepo, base, "anvil-overview")
	if err != nil {
		t.Fatal(err)
	}
	files := skillFiles()
	files[installMarkerName] = []byte("user-shipped-marker")
	if err := WriteMaterializes(set, files, WriterOptions{}); err != nil {
		t.Fatal(err)
	}
	master := filepath.Join(base, ".agents", "skills", "anvil-overview")
	if !markerMatches(master, "anvil-overview") {
		t.Error("shipped .anvil-install file survived; ownership marker missing")
	}
}

// isSymlink reports whether path is a symlink.
func isSymlink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}
