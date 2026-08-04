// Package server provides models and utilities for managing Anvil Server
// Runtime configuration — global Runtime metadata persistence, YAML schema
// definition, defaults, and validation, as well as per-project Registry
// metadata.
//
// This file covers the coordinator critical paths that had no test
// references anywhere: ProvisionProjectDir, ApplyFileOwnership, and
// applySharedLinks (REV-009 F20, TD-010), plus an end-to-end coordinator
// lifecycle test (install → activate → active → rollback) that runs through
// the real production state paths — not fixture-divergent ones.
//
// Reference: TD-010, ADR-014, MVP-001 AC 9.5
package server

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/contracts"
	"maleolabs.com/anvil/internal/project"
	"maleolabs.com/anvil/internal/release"
	"maleolabs.com/anvil/internal/runtime"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// captureStderr runs fn while redirecting os.Stderr to a capture file and
// returns everything written to stderr during the call. Used to assert the
// ADR-014 non-root ownership warning-and-continue behavior.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	tmp, err := os.CreateTemp("", "anvil-stderr-*.log")
	if err != nil {
		t.Fatalf("create stderr capture file: %v", err)
	}
	defer os.Remove(tmp.Name())

	old := os.Stderr
	os.Stderr = tmp
	defer func() { os.Stderr = old }()

	fn()

	if err := tmp.Close(); err != nil {
		t.Fatalf("close stderr capture file: %v", err)
	}
	data, err := os.ReadFile(tmp.Name())
	if err != nil {
		t.Fatalf("read stderr capture file: %v", err)
	}
	return string(data)
}

// currentOwnerGroup resolves the current process user and group names so
// ownership tests always use names that exist on the system. Returns empty
// strings when resolution fails (the caller then skips ownership checks).
func currentOwnerGroup(t *testing.T) (owner, group string) {
	t.Helper()

	u, err := user.Current()
	if err != nil {
		return "", ""
	}
	g, err := user.LookupGroupId(u.Gid)
	if err != nil {
		return u.Username, ""
	}
	return u.Username, g.Name
}

// isRoot reports whether the test process runs with root privileges, which
// changes the ownership semantics of Lchown (root applies, non-root warns).
func isRoot() bool {
	return os.Geteuid() == 0
}

// createTestArtifactWithFiles packages an artifact whose deployable content
// contains the given files (relative path → content), so the packaged app
// can include nested paths (e.g., config/app.env) that the shared-link
// logic replaces during activation.
func createTestArtifactWithFiles(t *testing.T, projectID, version string, files map[string]string) string {
	t.Helper()

	sourceDir := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(sourceDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatalf("mkdir artifact source %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatalf("write artifact source file %s: %v", rel, err)
		}
	}

	outputDir := t.TempDir()
	result, err := artifact.Package(artifact.PackageOptions{
		SourceDir: sourceDir,
		OutputDir: outputDir,
		Version:   version,
		Source:    projectID,
		ProjectID: projectID,
	})
	if err != nil {
		t.Fatalf("package artifact: %v", err)
	}
	return result.ArtifactPath
}

// setupSharedResources creates the shared resource pool under installRoot —
// the counterpart of the shared/config and shared/storage dirs that
// shared links point into.
func setupSharedResources(t *testing.T, installRoot string) {
	t.Helper()

	runtimeCfg := runtime.DefaultRuntimeConfig()
	runtimeCfg.InstallRoot = installRoot
	for _, d := range []string{runtimeCfg.SharedConfigDirPath(), runtimeCfg.SharedStorageDirPath()} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir shared dir %s: %v", d, err)
		}
	}
	if err := os.WriteFile(filepath.Join(runtimeCfg.SharedConfigDirPath(), "app.env"), []byte("APP_ENV=shared\n"), 0644); err != nil {
		t.Fatalf("write shared app.env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runtimeCfg.SharedStorageDirPath(), "data.txt"), []byte("shared-storage-data\n"), 0644); err != nil {
		t.Fatalf("write shared storage data: %v", err)
	}
}

// registerE2EProject registers a project whose registry carries shared
// links and ownership metadata — the full configuration surface the
// coordinator consumes during activation. It complements setupServerEnv,
// which registers a bare project.
func registerE2EProject(t *testing.T, serverRoot string, links []SharedLink, owner, group string) (projectID, installRoot string) {
	t.Helper()

	projectID = "e2e-project"
	installRoot = filepath.Join(serverRoot, "projects", projectID)

	configStore := NewConfigStore(serverRoot)
	cfg := DefaultServerConfig()
	cfg.Runtime.ID = "test-runtime"
	if err := configStore.Save(cfg); err != nil {
		t.Fatalf("save server config: %v", err)
	}

	reg := DefaultProjectRegistry()
	reg.Project.ID = projectID
	reg.Project.InstallRoot = installRoot
	reg.Project.DisplayName = "E2E Project"
	reg.Project.SharedLinks = links
	reg.Project.Owner = owner
	reg.Project.Group = group

	registryStore := NewRegistryStore(serverRoot)
	if err := registryStore.Register(reg); err != nil {
		t.Fatalf("register project: %v", err)
	}
	return projectID, installRoot
}

// assertRelativeSymlink asserts that targetPath is a symlink whose target is
// relative (not absolute) and resolves to sourcePath — the portable,
// relative-link behavior applySharedLinks must produce.
func assertRelativeSymlink(t *testing.T, targetPath, sourcePath string) {
	t.Helper()

	fi, err := os.Lstat(targetPath)
	if err != nil {
		t.Fatalf("lstat %s: %v", targetPath, err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is not a symlink (mode %s)", targetPath, fi.Mode())
	}

	link, err := os.Readlink(targetPath)
	if err != nil {
		t.Fatalf("readlink %s: %v", targetPath, err)
	}
	if filepath.IsAbs(link) {
		t.Errorf("symlink %s -> %s is absolute; want a relative link target", targetPath, link)
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(targetPath), link))
	if resolved != filepath.Clean(sourcePath) {
		t.Errorf("symlink %s resolves to %s, want %s", targetPath, resolved, sourcePath)
	}
}

// ---------------------------------------------------------------------------
// applySharedLinks unit tests (TD-010 §4, §9)
//
// Implements MVP-001 AC 9.5: activation configures shared resources as
// symlinks from shared resources into the release directory.
// ---------------------------------------------------------------------------

// TestApplySharedLinks_ReplacesRegularFileWithSymlink verifies that an
// existing regular file inside the release directory (artifact content) is
// removed and replaced with a symlink to the shared resource.
func TestApplySharedLinks_ReplacesRegularFileWithSymlink(t *testing.T) {
	installRoot := t.TempDir()
	releaseDir := filepath.Join(installRoot, "releases", "rel-001")
	sharedConfig := filepath.Join(installRoot, "shared", "config")
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir release dir: %v", err)
	}
	if err := os.MkdirAll(sharedConfig, 0755); err != nil {
		t.Fatalf("mkdir shared config: %v", err)
	}

	// Artifact content at the target path.
	targetPath := filepath.Join(releaseDir, ".env")
	if err := os.WriteFile(targetPath, []byte("APP_ENV=artifact\n"), 0644); err != nil {
		t.Fatalf("write artifact .env: %v", err)
	}
	sourcePath := filepath.Join(sharedConfig, "app.env")
	if err := os.WriteFile(sourcePath, []byte("APP_ENV=shared\n"), 0644); err != nil {
		t.Fatalf("write shared app.env: %v", err)
	}

	links := []SharedLink{{From: "shared/config/app.env", To: ".env"}}
	if err := applySharedLinks(installRoot, releaseDir, links); err != nil {
		t.Fatalf("applySharedLinks returned unexpected error: %v", err)
	}

	// The regular file must be gone and replaced by a relative symlink.
	assertRelativeSymlink(t, targetPath, sourcePath)

	// The shared content must be readable through the link.
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read linked .env: %v", err)
	}
	if string(data) != "APP_ENV=shared\n" {
		t.Errorf("linked .env content = %q, want %q", data, "APP_ENV=shared\n")
	}
}

// TestApplySharedLinks_Idempotent_ExistingSymlinkSkipped verifies the
// idempotency rule: when the target already exists as a symlink, it is left
// untouched — including on a second invocation.
func TestApplySharedLinks_Idempotent_ExistingSymlinkSkipped(t *testing.T) {
	installRoot := t.TempDir()
	releaseDir := filepath.Join(installRoot, "releases", "rel-001")
	sharedConfig := filepath.Join(installRoot, "shared", "config")
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir release dir: %v", err)
	}
	if err := os.MkdirAll(sharedConfig, 0755); err != nil {
		t.Fatalf("mkdir shared config: %v", err)
	}

	// The target already links somewhere else — e.g., a previous activation.
	targetPath := filepath.Join(releaseDir, ".env")
	if err := os.Symlink("some-other-target", targetPath); err != nil {
		t.Fatalf("create existing symlink: %v", err)
	}

	links := []SharedLink{{From: "shared/config/app.env", To: ".env"}}
	if err := applySharedLinks(installRoot, releaseDir, links); err != nil {
		t.Fatalf("applySharedLinks returned unexpected error: %v", err)
	}

	// The pre-existing symlink must not have been replaced.
	link, err := os.Readlink(targetPath)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if link != "some-other-target" {
		t.Errorf("existing symlink target = %q, want %q (must be skipped)", link, "some-other-target")
	}

	// Idempotency: a second invocation must be a no-op as well.
	if err := applySharedLinks(installRoot, releaseDir, links); err != nil {
		t.Fatalf("second applySharedLinks returned unexpected error: %v", err)
	}
	link, err = os.Readlink(targetPath)
	if err != nil {
		t.Fatalf("readlink after second call: %v", err)
	}
	if link != "some-other-target" {
		t.Errorf("existing symlink target after second call = %q, want %q", link, "some-other-target")
	}
}

// TestApplySharedLinks_RemoveAndLink_ExistingDirectoryReplaced verifies the
// remove-and-link semantics for directory targets: an artifact directory
// (e.g., storage/) is removed wholesale and replaced with a symlink to the
// shared directory, so the shared content — not the artifact content — is
// served.
func TestApplySharedLinks_RemoveAndLink_ExistingDirectoryReplaced(t *testing.T) {
	installRoot := t.TempDir()
	releaseDir := filepath.Join(installRoot, "releases", "rel-001")
	sharedStorage := filepath.Join(installRoot, "shared", "storage")
	targetPath := filepath.Join(releaseDir, "storage")

	// Artifact content at the target: a directory with a file inside.
	if err := os.MkdirAll(filepath.Join(targetPath), 0755); err != nil {
		t.Fatalf("mkdir artifact storage: %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetPath, "artifact.txt"), []byte("artifact-storage\n"), 0644); err != nil {
		t.Fatalf("write artifact storage file: %v", err)
	}
	// Shared content.
	if err := os.MkdirAll(sharedStorage, 0755); err != nil {
		t.Fatalf("mkdir shared storage: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sharedStorage, "data.txt"), []byte("shared-storage-data\n"), 0644); err != nil {
		t.Fatalf("write shared storage file: %v", err)
	}

	links := []SharedLink{{From: "shared/storage", To: "storage"}}
	if err := applySharedLinks(installRoot, releaseDir, links); err != nil {
		t.Fatalf("applySharedLinks returned unexpected error: %v", err)
	}

	assertRelativeSymlink(t, targetPath, sharedStorage)

	// The artifact directory content must be gone; the shared content must be
	// readable through the link.
	if _, err := os.Stat(filepath.Join(targetPath, "artifact.txt")); !os.IsNotExist(err) {
		t.Errorf("artifact storage file should be gone after remove-and-link, stat err = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(targetPath, "data.txt"))
	if err != nil {
		t.Fatalf("read linked storage data: %v", err)
	}
	if string(data) != "shared-storage-data\n" {
		t.Errorf("linked storage data = %q, want %q", data, "shared-storage-data\n")
	}
}

// TestApplySharedLinks_RelativeLinkTarget verifies that created symlinks use
// relative targets (portable across machines and mounts), including for
// nested target paths.
func TestApplySharedLinks_RelativeLinkTarget(t *testing.T) {
	installRoot := t.TempDir()
	releaseDir := filepath.Join(installRoot, "releases", "rel-001")
	sourcePath := filepath.Join(installRoot, "shared", "config", "app.env")
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir release dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0755); err != nil {
		t.Fatalf("mkdir shared config: %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte("x"), 0644); err != nil {
		t.Fatalf("write shared file: %v", err)
	}

	links := []SharedLink{{From: "shared/config/app.env", To: "config/app.env"}}
	if err := applySharedLinks(installRoot, releaseDir, links); err != nil {
		t.Fatalf("applySharedLinks returned unexpected error: %v", err)
	}

	assertRelativeSymlink(t, filepath.Join(releaseDir, "config", "app.env"), sourcePath)
}

// TestApplySharedLinks_SkipsEmptyLinkConfig verifies that a link entry with
// an empty From or To is skipped without error — the defensive guard for
// partially configured registries.
func TestApplySharedLinks_SkipsEmptyLinkConfig(t *testing.T) {
	installRoot := t.TempDir()
	releaseDir := filepath.Join(installRoot, "releases", "rel-001")
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir release dir: %v", err)
	}

	links := []SharedLink{
		{From: "", To: ".env"},
		{From: "shared/config/app.env", To: ""},
	}
	if err := applySharedLinks(installRoot, releaseDir, links); err != nil {
		t.Fatalf("applySharedLinks returned unexpected error: %v", err)
	}

	if _, err := os.Lstat(filepath.Join(releaseDir, ".env")); !os.IsNotExist(err) {
		t.Errorf("no symlink may be created for empty link config, stat err = %v", err)
	}
}

// TestApplySharedLinks_SourceNotRequired verifies documented behavior: the
// source path is not validated before symlink creation — a missing shared
// resource yields a dangling symlink rather than an error (the shared
// resource pool may be provisioned after activation).
func TestApplySharedLinks_SourceNotRequired(t *testing.T) {
	installRoot := t.TempDir()
	releaseDir := filepath.Join(installRoot, "releases", "rel-001")
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir release dir: %v", err)
	}

	links := []SharedLink{{From: "shared/config/not-created.env", To: ".env"}}
	if err := applySharedLinks(installRoot, releaseDir, links); err != nil {
		t.Fatalf("applySharedLinks returned unexpected error: %v", err)
	}

	targetPath := filepath.Join(releaseDir, ".env")
	fi, err := os.Lstat(targetPath)
	if err != nil {
		t.Fatalf("lstat target: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is not a symlink", targetPath)
	}
}

// ---------------------------------------------------------------------------
// ApplyFileOwnership unit tests (TD-010 §4, §9)
//
// Implements ADR-014 ownership safety rules: chown semantics and the
// non-root warning-and-continue behavior.
// ---------------------------------------------------------------------------

// TestApplyFileOwnership_EmptyOwnerAndGroup_Noop verifies the no-op rule:
// with both owner and group empty, nothing is changed and nothing is
// reported.
func TestApplyFileOwnership_EmptyOwnerAndGroup_Noop(t *testing.T) {
	rootPath := t.TempDir()
	filePath := filepath.Join(rootPath, "file.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	before, err := os.Lstat(filePath)
	if err != nil {
		t.Fatalf("lstat before: %v", err)
	}

	warning := captureStderr(t, func() {
		if err := ApplyFileOwnership(rootPath, "", ""); err != nil {
			t.Fatalf("ApplyFileOwnership returned unexpected error: %v", err)
		}
	})
	if warning != "" {
		t.Errorf("no-op ownership call wrote to stderr: %q", warning)
	}

	after, err := os.Lstat(filePath)
	if err != nil {
		t.Fatalf("lstat after: %v", err)
	}
	if after.Mode() != before.Mode() {
		t.Errorf("file mode changed by no-op: %s -> %s", before.Mode(), after.Mode())
	}
}

// TestApplyFileOwnership_UnknownUser_Error verifies that an unresolvable
// owner name fails the operation with a descriptive error.
func TestApplyFileOwnership_UnknownUser_Error(t *testing.T) {
	err := ApplyFileOwnership(t.TempDir(), "anvil-nonexistent-user-xyz", "")
	if err == nil {
		t.Fatal("expected error for unknown user, got nil")
	}
	if !contains(err.Error(), "lookup user") {
		t.Errorf("expected error to mention 'lookup user', got: %v", err)
	}
}

// TestApplyFileOwnership_UnknownGroup_Error verifies that an unresolvable
// group name fails the operation with a descriptive error quoting the
// requested name (regression: the lookup error previously formatted the
// nil lookup result — `%!q(*user.Group=<nil>)` — instead of the group
// name).
func TestApplyFileOwnership_UnknownGroup_Error(t *testing.T) {
	groupName := "anvil-nonexistent-group-xyz"
	err := ApplyFileOwnership(t.TempDir(), "", groupName)
	if err == nil {
		t.Fatal("expected error for unknown group, got nil")
	}
	// The name must be quoted immediately after "lookup group"; a stray
	// match inside the wrapped user error would not catch the regression.
	if want := fmt.Sprintf("lookup group %q", groupName); !contains(err.Error(), want) {
		t.Errorf("expected error to contain %q, got: %v", want, err)
	}
}

// nobodyOwnerGroup resolves a valid user whose uid/gid differ from the
// current process — the deterministic trigger for the non-root chown
// warning path (chown to the same uid/gid is a no-op that succeeds without
// privileges, so the current user cannot trigger EPERM).
func nobodyOwnerGroup(t *testing.T) (owner, group string, ok bool) {
	t.Helper()

	u, err := user.Lookup("nobody")
	if err != nil {
		return "", "", false
	}
	g, err := user.LookupGroupId(u.Gid)
	if err != nil {
		return "", "", false
	}
	return u.Username, g.Name, true
}

// numericOwnerGroup resolves owner/group names to their numeric uid/gid,
// independently of resolveOwnerGroup, so tests can assert the expected
// values without reusing the code under test. An empty name yields -1, the
// os.Lchown "do not change" sentinel. Returns ok=false when a name cannot
// be resolved.
func numericOwnerGroup(owner, group string) (uid, gid int, ok bool) {
	uid, gid = -1, -1

	if owner != "" {
		u, err := user.Lookup(owner)
		if err != nil {
			return 0, 0, false
		}
		uid, err = strconv.Atoi(u.Uid)
		if err != nil {
			return 0, 0, false
		}
	}
	if group != "" {
		g, err := user.LookupGroup(group)
		if err != nil {
			return 0, 0, false
		}
		gid, err = strconv.Atoi(g.Gid)
		if err != nil {
			return 0, 0, false
		}
	}

	return uid, gid, true
}

// TestApplyFileOwnership_OwnerOnly_NonRootWarnAndContinue verifies the
// ADR-014 non-root safety rule for the owner-only combination: when chown is
// not permitted, the operation logs a warning and continues instead of
// failing — the common development case.
func TestApplyFileOwnership_OwnerOnly_NonRootWarnAndContinue(t *testing.T) {
	if isRoot() {
		t.Skip("ownership warning path only applies to non-root processes")
	}

	owner, _, ok := nobodyOwnerGroup(t)
	if !ok {
		t.Skip("could not resolve the 'nobody' user; ownership lookup requires a valid name")
	}

	rootPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootPath, "file.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	warning := captureStderr(t, func() {
		if err := ApplyFileOwnership(rootPath, owner, ""); err != nil {
			t.Fatalf("ApplyFileOwnership returned unexpected error: %v", err)
		}
	})
	if !contains(warning, "Warning: could not chown") {
		t.Errorf("expected non-root chown warning on stderr, got: %q", warning)
	}
}

// TestApplyFileOwnership_GroupOnly_NonRootWarnAndContinue verifies the
// non-root warning path for the group-only combination.
//
// NOTE (BUG-007): this test asserts only the warning-and-continue behavior
// for non-root processes. The BUG-007 fix — group-only must not force the
// owner to root — is verified by
// TestResolveOwnerGroup_GroupOnly_KeepsOwnerUnchanged and
// TestApplyFileOwnership_Root_PartialOwnership_UnchangedComponent.
func TestApplyFileOwnership_GroupOnly_NonRootWarnAndContinue(t *testing.T) {
	if isRoot() {
		t.Skip("ownership warning path only applies to non-root processes")
	}

	_, group, ok := nobodyOwnerGroup(t)
	if !ok {
		t.Skip("could not resolve the 'nobody' group; ownership lookup requires a valid name")
	}

	rootPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootPath, "file.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	warning := captureStderr(t, func() {
		if err := ApplyFileOwnership(rootPath, "", group); err != nil {
			t.Fatalf("ApplyFileOwnership returned unexpected error: %v", err)
		}
	})
	if !contains(warning, "Warning: could not chown") {
		t.Errorf("expected non-root chown warning on stderr, got: %q", warning)
	}
}

// TestApplyFileOwnership_OwnerAndGroup_NonRootWarnAndContinue verifies the
// non-root warning path for the owner+group combination.
func TestApplyFileOwnership_OwnerAndGroup_NonRootWarnAndContinue(t *testing.T) {
	if isRoot() {
		t.Skip("ownership warning path only applies to non-root processes")
	}

	owner, group, ok := nobodyOwnerGroup(t)
	if !ok {
		t.Skip("could not resolve the 'nobody' user/group; ownership lookup requires valid names")
	}

	rootPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootPath, "file.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	warning := captureStderr(t, func() {
		if err := ApplyFileOwnership(rootPath, owner, group); err != nil {
			t.Fatalf("ApplyFileOwnership returned unexpected error: %v", err)
		}
	})
	if !contains(warning, "Warning: could not chown") {
		t.Errorf("expected non-root chown warning on stderr, got: %q", warning)
	}
}

// TestApplyFileOwnership_Root_AppliesOwnerAndGroup verifies that a
// privileged process actually changes file ownership to the resolved
// uid:gid — the root counterpart of the warning path tests above. Only runs
// when the test process is root (e.g., privileged CI).
func TestApplyFileOwnership_Root_AppliesOwnerAndGroup(t *testing.T) {
	if !isRoot() {
		t.Skip("ownership application requires root; running non-root")
	}

	u, err := user.Current()
	if err != nil {
		t.Fatalf("lookup current user: %v", err)
	}
	owner, group := u.Username, ""
	if g, err := user.LookupGroupId(u.Gid); err == nil {
		group = g.Name
	}

	rootPath := t.TempDir()
	filePath := filepath.Join(rootPath, "file.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := ApplyFileOwnership(rootPath, owner, group); err != nil {
		t.Fatalf("ApplyFileOwnership returned unexpected error: %v", err)
	}

	fi, err := os.Lstat(filePath)
	if err != nil {
		t.Fatalf("lstat after chown: %v", err)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("unexpected file info type %T", fi.Sys())
	}
	wantUID, err := strconv.Atoi(u.Uid)
	if err != nil {
		t.Fatalf("parse uid %q: %v", u.Uid, err)
	}
	if int(st.Uid) != wantUID {
		t.Errorf("file uid = %d, want %d", st.Uid, wantUID)
	}
	wantGID, err := strconv.Atoi(u.Gid)
	if err != nil {
		t.Fatalf("parse gid %q: %v", u.Gid, err)
	}
	if int(st.Gid) != wantGID {
		t.Errorf("file gid = %d, want %d", st.Gid, wantGID)
	}
}

// ---------------------------------------------------------------------------
// resolveOwnerGroup tests (BUG-007)
//
// Verifies the fix invariant: an unspecified owner/group resolves to -1,
// the os.Lchown "do not change" sentinel, never 0 (root). These tests run
// unprivileged — no chown privileges required.
// ---------------------------------------------------------------------------

// TestResolveOwnerGroup_Neither_UsesNoChangeSentinels verifies that with
// neither owner nor group specified, both components resolve to -1.
func TestResolveOwnerGroup_Neither_UsesNoChangeSentinels(t *testing.T) {
	uid, gid, err := resolveOwnerGroup("", "")
	if err != nil {
		t.Fatalf("resolveOwnerGroup returned unexpected error: %v", err)
	}
	if uid != -1 {
		t.Errorf("uid = %d, want -1 (do not change sentinel)", uid)
	}
	if gid != -1 {
		t.Errorf("gid = %d, want -1 (do not change sentinel)", gid)
	}
}

// TestResolveOwnerGroup_OwnerOnly_KeepsGroupUnchanged verifies the
// owner-only case: the group resolves to -1, so chown never forces the
// group to root.
func TestResolveOwnerGroup_OwnerOnly_KeepsGroupUnchanged(t *testing.T) {
	owner, _, ok := nobodyOwnerGroup(t)
	if !ok {
		t.Skip("could not resolve the 'nobody' user; ownership lookup requires a valid name")
	}

	wantUID, _, ok := numericOwnerGroup(owner, "")
	if !ok {
		t.Skip("could not resolve numeric uid for the 'nobody' user")
	}

	uid, gid, err := resolveOwnerGroup(owner, "")
	if err != nil {
		t.Fatalf("resolveOwnerGroup returned unexpected error: %v", err)
	}
	if uid != wantUID {
		t.Errorf("uid = %d, want %d", uid, wantUID)
	}
	if gid != -1 {
		t.Errorf("gid = %d, want -1 (do not change sentinel)", gid)
	}
}

// TestResolveOwnerGroup_GroupOnly_KeepsOwnerUnchanged verifies the reported
// BUG-007 defect: with only a group specified, the owner resolves to -1
// (unchanged) instead of 0 (root).
func TestResolveOwnerGroup_GroupOnly_KeepsOwnerUnchanged(t *testing.T) {
	_, group, ok := nobodyOwnerGroup(t)
	if !ok {
		t.Skip("could not resolve the 'nobody' group; ownership lookup requires a valid name")
	}

	_, wantGID, ok := numericOwnerGroup("", group)
	if !ok {
		t.Skip("could not resolve numeric gid for the 'nobody' group")
	}

	uid, gid, err := resolveOwnerGroup("", group)
	if err != nil {
		t.Fatalf("resolveOwnerGroup returned unexpected error: %v", err)
	}
	if uid != -1 {
		t.Errorf("uid = %d, want -1 (do not change sentinel)", uid)
	}
	if gid != wantGID {
		t.Errorf("gid = %d, want %d", gid, wantGID)
	}
}

// TestResolveOwnerGroup_Both_ResolvesBoth verifies that specifying both
// owner and group resolves both components to their numeric values.
func TestResolveOwnerGroup_Both_ResolvesBoth(t *testing.T) {
	owner, group := currentOwnerGroup(t)
	if owner == "" || group == "" {
		t.Skip("could not resolve the current user/group names")
	}

	wantUID, wantGID, ok := numericOwnerGroup(owner, group)
	if !ok {
		t.Skip("could not resolve numeric uid/gid for the current user/group")
	}

	uid, gid, err := resolveOwnerGroup(owner, group)
	if err != nil {
		t.Fatalf("resolveOwnerGroup returned unexpected error: %v", err)
	}
	if uid != wantUID {
		t.Errorf("uid = %d, want %d", uid, wantUID)
	}
	if gid != wantGID {
		t.Errorf("gid = %d, want %d", gid, wantGID)
	}
}

// TestApplyFileOwnership_Root_PartialOwnership_UnchangedComponent verifies
// the BUG-007 fix end to end on a privileged process: when only one of
// owner/group is specified, the unspecified component stays unchanged —
// never forced to root (uid/gid 0). Files are seeded to nobody:nobody so an
// unchanged component is distinguishable from root. Only runs when the test
// process is root; unprivileged runs rely on TestResolveOwnerGroup_*.
func TestApplyFileOwnership_Root_PartialOwnership_UnchangedComponent(t *testing.T) {
	if !isRoot() {
		t.Skip("ownership application requires root; running non-root")
	}

	owner, group, ok := nobodyOwnerGroup(t)
	if !ok {
		t.Skip("could not resolve the 'nobody' user/group; ownership lookup requires valid names")
	}

	wantUID, wantGID, ok := numericOwnerGroup(owner, group)
	if !ok {
		t.Skip("could not resolve numeric uid/gid for the 'nobody' user/group")
	}

	testCases := []struct {
		name         string
		owner, group string
		// wantUID/wantGID = -1 means "must stay unchanged".
		wantUID, wantGID int
	}{
		{"owner-only", owner, "", wantUID, -1},
		{"group-only", "", group, -1, wantGID},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rootPath := t.TempDir()
			filePath := filepath.Join(rootPath, "file.txt")
			if err := os.WriteFile(filePath, []byte("x"), 0644); err != nil {
				t.Fatalf("write file: %v", err)
			}
			// Seed ownership away from root so a regression (forcing the
			// unspecified component to 0) is observable.
			if err := os.Chown(filePath, wantUID, wantGID); err != nil {
				t.Fatalf("seed chown to nobody:nobody: %v", err)
			}

			if err := ApplyFileOwnership(rootPath, tc.owner, tc.group); err != nil {
				t.Fatalf("ApplyFileOwnership returned unexpected error: %v", err)
			}

			fi, err := os.Lstat(filePath)
			if err != nil {
				t.Fatalf("lstat after chown: %v", err)
			}
			st, ok := fi.Sys().(*syscall.Stat_t)
			if !ok {
				t.Fatalf("unexpected file info type %T", fi.Sys())
			}

			if tc.wantUID == -1 && int(st.Uid) != wantUID {
				t.Errorf("uid = %d, want unchanged %d (owner must not be forced to root)", st.Uid, wantUID)
			}
			if tc.wantUID != -1 && int(st.Uid) != tc.wantUID {
				t.Errorf("uid = %d, want %d", st.Uid, tc.wantUID)
			}
			if tc.wantGID == -1 && int(st.Gid) != wantGID {
				t.Errorf("gid = %d, want unchanged %d (group must not be forced to root)", st.Gid, wantGID)
			}
			if tc.wantGID != -1 && int(st.Gid) != tc.wantGID {
				t.Errorf("gid = %d, want %d", st.Gid, tc.wantGID)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ProvisionProjectDir tests (TD-010 Validation Checklist)
// ---------------------------------------------------------------------------

// TestProvisionProjectDir_CreatesRuntimeStructure verifies the full runtime
// directory structure is created under installRoot, including the canonical
// .anvil/state/releases release state directory (BUG-002 layout) — and that
// the legacy <installRoot>/state/releases layout is never created.
func TestProvisionProjectDir_CreatesRuntimeStructure(t *testing.T) {
	installRoot := filepath.Join(t.TempDir(), "install-root")

	if err := ProvisionProjectDir(installRoot, "", ""); err != nil {
		t.Fatalf("ProvisionProjectDir returned unexpected error: %v", err)
	}

	runtimeCfg := runtime.DefaultRuntimeConfig()
	runtimeCfg.InstallRoot = installRoot
	wantDirs := map[string]string{
		"artifacts":                    filepath.Join(installRoot, "artifacts"),
		"releases":                     runtimeCfg.ReleasesDirPath(),
		"release state (.anvil/state)": filepath.Join(project.NewStructure(installRoot).StateDir, "releases"),
		"shared config":                runtimeCfg.SharedConfigDirPath(),
		"shared storage":               runtimeCfg.SharedStorageDirPath(),
		"shared logs":                  runtimeCfg.LogsDirPath(),
		"tmp":                          runtimeCfg.TempDirPath(),
	}
	for name, dir := range wantDirs {
		fi, err := os.Stat(dir)
		if err != nil {
			t.Errorf("provisioned directory %s missing at %s: %v", name, dir, err)
			continue
		}
		if !fi.IsDir() {
			t.Errorf("provisioned path %s (%s) is not a directory", name, dir)
		}
	}

	// The legacy pre-BUG-002 layout must never be created.
	legacyDir := filepath.Join(installRoot, "state", "releases")
	if _, err := os.Stat(legacyDir); err == nil {
		t.Errorf("legacy state dir %s must not be created by ProvisionProjectDir", legacyDir)
	} else if !os.IsNotExist(err) {
		t.Errorf("stat legacy state dir %s: %v", legacyDir, err)
	}
}

// TestProvisionProjectDir_OwnerSpecified_NonRootWarnAndContinue verifies
// that provisioning with an owner continues with a warning when chown is
// not permitted (non-root) — provisioning must not fail for unprivileged
// users (ADR-014).
//
// The owner must differ from the current user: chown to the current uid
// with an unchanged group is a no-op that succeeds without privileges
// (post-BUG-007 the unset group resolves to -1, not 0), so only an
// owner change to another user triggers the permission warning.
func TestProvisionProjectDir_OwnerSpecified_NonRootWarnAndContinue(t *testing.T) {
	if isRoot() {
		t.Skip("ownership warning path only applies to non-root processes")
	}

	owner, _, ok := nobodyOwnerGroup(t)
	if !ok {
		t.Skip("could not resolve the 'nobody' user; ownership lookup requires a valid name")
	}

	installRoot := filepath.Join(t.TempDir(), "install-root")
	warning := captureStderr(t, func() {
		if err := ProvisionProjectDir(installRoot, owner, ""); err != nil {
			t.Fatalf("ProvisionProjectDir returned unexpected error: %v", err)
		}
	})
	if !contains(warning, "Warning: could not chown") {
		t.Errorf("expected non-root chown warning on stderr, got: %q", warning)
	}

	// Provisioning must have completed regardless.
	if _, err := os.Stat(filepath.Join(installRoot, "artifacts")); err != nil {
		t.Errorf("provisioned structure incomplete after warning path: %v", err)
	}
}

// TestProvisionProjectDir_InvalidOwner_Fails verifies that an unresolvable
// owner fails provisioning with a descriptive error.
func TestProvisionProjectDir_InvalidOwner_Fails(t *testing.T) {
	installRoot := filepath.Join(t.TempDir(), "install-root")
	err := ProvisionProjectDir(installRoot, "anvil-nonexistent-user-xyz", "")
	if err == nil {
		t.Fatal("expected error for unknown owner, got nil")
	}
	if !contains(err.Error(), "lookup user") {
		t.Errorf("expected error to mention 'lookup user', got: %v", err)
	}
}

// TestProvisionProjectDir_InstallRootUnderFile_Fails verifies that an
// install root whose parent component is a regular file fails directory
// creation with a descriptive error.
func TestProvisionProjectDir_InstallRootUnderFile_Fails(t *testing.T) {
	parent := t.TempDir()
	blocker := filepath.Join(parent, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}

	err := ProvisionProjectDir(filepath.Join(blocker, "child"), "", "")
	if err == nil {
		t.Fatal("expected error for install root under a file, got nil")
	}
}

// ---------------------------------------------------------------------------
// End-to-end coordinator lifecycle test (TD-010 §4, §9)
//
// Runs the real production state paths — the same class of defect that
// escaped the fixture-based unit tests (BUG-002, BUG-003) cannot escape
// here: nothing writes state except the coordinator itself.
// ---------------------------------------------------------------------------

// TestCoordinatorLifecycle_InstallActivateActiveRollback executes the full
// release lifecycle through ServerReleaseCoordinator on a fresh server root:
// install (A) → activate (A) → active reports A → install (B) → activate (B,
// archiving A) → active reports B → rollback → active reports A again.
//
// The project registry carries shared links and ownership metadata, so the
// test exercises applySharedLinks and ApplyFileOwnership through the
// production activation path (MVP-001 AC 9.5, ADR-014), and every release
// state assertion goes through the internal/release read paths used by
// observability and rollback.
func TestCoordinatorLifecycle_InstallActivateActiveRollback(t *testing.T) {
	serverRoot := t.TempDir()
	links := []SharedLink{
		{From: "shared/config/app.env", To: "config/app.env"},
		{From: "shared/storage", To: "storage"},
	}
	owner, group := currentOwnerGroup(t)
	projectID, installRoot := registerE2EProject(t, serverRoot, links, owner, group)

	// Pre-provision the shared resource pool (the counterpart of the
	// files the shared links point into).
	setupSharedResources(t, installRoot)

	coordinator := NewServerReleaseCoordinator(serverRoot)

	// Two distinct artifacts for the same project — ArtifactID is
	// content-derived (TS-P3-04), so different content yields different IDs
	// (required to bypass the install idempotency check).
	artifactA := createTestArtifactWithFiles(t, projectID, "1.0.0", map[string]string{
		"index.php":            "<?php // release A\n",
		"config/app.env":       "APP_ENV=artifact-a\n",
		"storage/artifact.txt": "artifact-a-storage\n",
	})
	artifactB := createTestArtifactWithFiles(t, projectID, "1.1.0", map[string]string{
		"index.php":            "<?php // release B\n",
		"config/app.env":       "APP_ENV=artifact-b\n",
		"storage/artifact.txt": "artifact-b-storage\n",
	})

	// ------------------------------------------------------------------
	// Install A
	// ------------------------------------------------------------------
	relA, err := coordinator.Install(projectID, artifactA)
	if err != nil {
		t.Fatalf("Install A returned unexpected error: %v", err)
	}
	if relA.Stage != release.StageReady {
		t.Errorf("release A stage = %s after install, want %s", relA.Stage, release.StageReady)
	}
	// The Release must be readable at the canonical state path.
	canonicalA := filepath.Join(project.NewStructure(installRoot).StateDir, "releases", relA.ID.String()+".json")
	if _, err := os.Stat(canonicalA); err != nil {
		t.Errorf("release A JSON not found at canonical path %s: %v", canonicalA, err)
	}

	// ------------------------------------------------------------------
	// Activate A → active reports A
	// ------------------------------------------------------------------
	if err := coordinator.Activate(projectID, relA.ID.String()); err != nil {
		t.Fatalf("Activate A returned unexpected error: %v", err)
	}
	active, err := release.GetActiveRelease(installRoot)
	if err != nil {
		t.Fatalf("GetActiveRelease returned unexpected error: %v", err)
	}
	if active == nil || active.ID != relA.ID || active.Stage != release.StageActive {
		t.Errorf("active release after activating A = %v, want A (%s) in Active stage", active, relA.ID)
	}
	assertRuntimeActiveReleaseID(t, installRoot, relA.ID.String())

	// Shared resources must be linked into the release directory: the
	// artifact's config/app.env and storage/ must have been replaced by
	// relative symlinks to the shared pool (MVP-001 AC 9.5).
	runtimeCfg := runtime.DefaultRuntimeConfig()
	runtimeCfg.InstallRoot = installRoot
	releaseDirA := runtime.ReleaseDirPath(runtimeCfg.ReleasesDirPath(), relA.ID.String())
	assertRelativeSymlink(t, filepath.Join(releaseDirA, "config", "app.env"), filepath.Join(installRoot, "shared", "config", "app.env"))
	assertRelativeSymlink(t, filepath.Join(releaseDirA, "storage"), filepath.Join(installRoot, "shared", "storage"))
	data, err := os.ReadFile(filepath.Join(releaseDirA, "config", "app.env"))
	if err != nil {
		t.Fatalf("read linked app.env: %v", err)
	}
	if string(data) != "APP_ENV=shared\n" {
		t.Errorf("linked app.env content = %q, want shared content %q", data, "APP_ENV=shared\n")
	}
	if _, err := os.Stat(filepath.Join(releaseDirA, "storage", "artifact.txt")); !os.IsNotExist(err) {
		t.Errorf("artifact storage file should be gone after remove-and-link, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(releaseDirA, "storage", "data.txt")); err != nil {
		t.Errorf("shared storage file should be readable through the link: %v", err)
	}

	// ------------------------------------------------------------------
	// Install B + Activate B → A archived, active reports B
	// ------------------------------------------------------------------
	relB, err := coordinator.Install(projectID, artifactB)
	if err != nil {
		t.Fatalf("Install B returned unexpected error: %v", err)
	}
	if err := coordinator.Activate(projectID, relB.ID.String()); err != nil {
		t.Fatalf("Activate B returned unexpected error: %v", err)
	}

	// A must be Archived (ActiveReleaseInvariant wired — BUG-003) and
	// persisted on disk.
	archivedA, err := release.LookupByID(installRoot, relA.ID)
	if err != nil {
		t.Fatalf("LookupByID A returned unexpected error: %v", err)
	}
	if archivedA.Stage != release.StageArchived {
		t.Errorf("release A stage = %s after activating B, want %s", archivedA.Stage, release.StageArchived)
	}
	active, err = release.GetActiveRelease(installRoot)
	if err != nil {
		t.Fatalf("GetActiveRelease returned unexpected error: %v", err)
	}
	if active == nil || active.ID != relB.ID || active.Stage != release.StageActive {
		t.Errorf("active release after activating B = %v, want B (%s) in Active stage", active, relB.ID)
	}
	assertRuntimeActiveReleaseID(t, installRoot, relB.ID.String())

	// B's release directory must also carry the shared links.
	releaseDirB := runtime.ReleaseDirPath(runtimeCfg.ReleasesDirPath(), relB.ID.String())
	assertRelativeSymlink(t, filepath.Join(releaseDirB, "config", "app.env"), filepath.Join(installRoot, "shared", "config", "app.env"))

	// ------------------------------------------------------------------
	// Rollback → active reports A again
	// ------------------------------------------------------------------
	result, err := coordinator.Rollback(projectID)
	if err != nil {
		t.Fatalf("Rollback returned unexpected error: %v", err)
	}
	if result.RolledBackRelease == nil || result.RolledBackRelease.ID != relB.ID {
		t.Errorf("rolled-back release = %v, want B (%s)", result.RolledBackRelease, relB.ID)
	}
	if result.RestoredRelease == nil || result.RestoredRelease.ID != relA.ID {
		t.Errorf("restored release = %v, want A (%s)", result.RestoredRelease, relA.ID)
	}

	// The restored release is Active again; the rolled-back one is RolledBack
	// (BUG-004 transitions); runtime state tracks A.
	active, err = release.GetActiveRelease(installRoot)
	if err != nil {
		t.Fatalf("GetActiveRelease after rollback returned unexpected error: %v", err)
	}
	if active == nil || active.ID != relA.ID || active.Stage != release.StageActive {
		t.Errorf("active release after rollback = %v, want A (%s) in Active stage", active, relA.ID)
	}
	rolledBackB, err := release.LookupByID(installRoot, relB.ID)
	if err != nil {
		t.Fatalf("LookupByID B after rollback returned unexpected error: %v", err)
	}
	if rolledBackB.Stage != release.StageRolledBack {
		t.Errorf("release B stage after rollback = %s, want %s", rolledBackB.Stage, release.StageRolledBack)
	}
	assertRuntimeActiveReleaseID(t, installRoot, relA.ID.String())

	// Rollback is a full lifecycle round trip: the symlink state must also
	// point back at A's directory. Verify through the switcher's read path.
	link, err := os.Readlink(runtimeCfg.ActiveSymlinkPath())
	if err != nil {
		t.Fatalf("readlink active symlink: %v", err)
	}
	resolvedLink := filepath.Clean(link)
	if !filepath.IsAbs(link) {
		resolvedLink = filepath.Clean(filepath.Join(filepath.Dir(runtimeCfg.ActiveSymlinkPath()), link))
	}
	if resolvedLink != releaseDirA {
		t.Errorf("active symlink resolves to %s, want %s (release A)", resolvedLink, releaseDirA)
	}
}

// TestCoordinatorLifecycle_RollbackWithoutArchivedTarget_Fails verifies the
// rollback failure path: a project whose only release is Active has no
// Archived target, so rollback must fail with a descriptive error.
func TestCoordinatorLifecycle_RollbackWithoutArchivedTarget_Fails(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, installRoot := setupServerEnv(t, serverRoot)
	artifactPath := createTestArtifact(t, projectID)

	coordinator := NewServerReleaseCoordinator(serverRoot)
	rel, err := coordinator.Install(projectID, artifactPath)
	if err != nil {
		t.Fatalf("Install returned unexpected error: %v", err)
	}
	if err := coordinator.Activate(projectID, rel.ID.String()); err != nil {
		t.Fatalf("Activate returned unexpected error: %v", err)
	}

	_, err = coordinator.Rollback(projectID)
	if err == nil {
		t.Fatal("expected rollback error for missing Archived target, got nil")
	}
	if !contains(err.Error(), "no Archived Release") {
		t.Errorf("expected error to mention 'no Archived Release', got: %v", err)
	}

	// The failed rollback must not corrupt state: A is still Active.
	active, err := release.GetActiveRelease(installRoot)
	if err != nil {
		t.Fatalf("GetActiveRelease returned unexpected error: %v", err)
	}
	if active == nil || active.ID != rel.ID || active.Stage != release.StageActive {
		t.Errorf("active release after failed rollback = %v, want A (%s) Active", active, rel.ID)
	}
}

// TestCoordinatorLifecycle_RollbackWithoutActiveRelease_Fails verifies the
// rollback failure path for a project with no Active Release: rollback must
// fail with a descriptive error instead of mutating anything.
func TestCoordinatorLifecycle_RollbackWithoutActiveRelease_Fails(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, _ := setupServerEnv(t, serverRoot)

	coordinator := NewServerReleaseCoordinator(serverRoot)
	_, err := coordinator.Rollback(projectID)
	if err == nil {
		t.Fatal("expected rollback error for missing Active release, got nil")
	}
	if !contains(err.Error(), "no Active Release") {
		t.Errorf("expected error to mention 'no Active Release', got: %v", err)
	}
}

// TestInstall_SameArtifactRejected verifies the install idempotency failure
// path: installing the same artifact twice must be rejected with a
// descriptive error — the check scans both the canonical and the legacy
// (read-only, BUG-002) release state directories.
func TestInstall_SameArtifactRejected(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, _ := setupServerEnv(t, serverRoot)
	artifactPath := createTestArtifact(t, projectID)

	coordinator := NewServerReleaseCoordinator(serverRoot)
	rel, err := coordinator.Install(projectID, artifactPath)
	if err != nil {
		t.Fatalf("first Install returned unexpected error: %v", err)
	}

	// A duplicate install is rejected by the canonical state scan.
	_, err = coordinator.Install(projectID, artifactPath)
	if err == nil {
		t.Fatal("expected duplicate-install error, got nil")
	}
	if !contains(err.Error(), "already installed") {
		t.Errorf("expected error to mention 'already installed', got: %v", err)
	}

	// Legacy-only duplicate (BUG-002 back-compat): remove the canonical
	// Release JSON and plant only a legacy <installRoot>/state/releases
	// copy. The idempotency scan must still find it and reject the install
	// — the legacy directory is read-only back-compat, never a second
	// source of truth.
	installRoot := filepath.Join(serverRoot, "projects", projectID)
	canonicalDir := filepath.Join(project.NewStructure(installRoot).StateDir, "releases")
	data, err := os.ReadFile(filepath.Join(canonicalDir, rel.ID.String()+".json"))
	if err != nil {
		t.Fatalf("read canonical release JSON: %v", err)
	}
	if err := os.Remove(filepath.Join(canonicalDir, rel.ID.String()+".json")); err != nil {
		t.Fatalf("remove canonical release JSON: %v", err)
	}
	legacyDir := filepath.Join(installRoot, "state", "releases")
	if err := os.MkdirAll(legacyDir, 0755); err != nil {
		t.Fatalf("mkdir legacy releases dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, rel.ID.String()+".json"), data, 0644); err != nil {
		t.Fatalf("write legacy release JSON: %v", err)
	}

	_, err = coordinator.Install(projectID, artifactPath)
	if err == nil {
		t.Fatal("expected duplicate-install error from the legacy scan, got nil")
	}
	if !contains(err.Error(), "already installed") {
		t.Errorf("expected error to mention 'already installed', got: %v", err)
	}
}

// assertRuntimeActiveReleaseID asserts the persisted runtime state records
// the given active release ID — the state the `server release active`
// observability path reads.
func assertRuntimeActiveReleaseID(t *testing.T, installRoot, wantID string) {
	t.Helper()

	statePath := filepath.Join(installRoot, "runtime-state.json")
	stateStore := runtime.NewStateStore(statePath)
	if err := stateStore.Load(); err != nil {
		t.Fatalf("load runtime state: %v", err)
	}
	if got := stateStore.State().ActiveReleaseID; got != wantID {
		t.Errorf("runtime ActiveReleaseID = %q, want %q", got, wantID)
	}
}

// ---------------------------------------------------------------------------
// Install failure paths (TD-010 §9 — coordinator critical paths)
// ---------------------------------------------------------------------------

// TestInstall_UnverifiedArtifactRejected verifies that an artifact that
// fails verification is rejected before any state is written.
func TestInstall_UnverifiedArtifactRejected(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, _ := setupServerEnv(t, serverRoot)

	// A non-archive file fails artifact verification (gzip tolerates
	// trailing garbage, so appending bytes would not be enough).
	corruptPath := filepath.Join(t.TempDir(), "corrupt.tar.gz")
	if err := os.WriteFile(corruptPath, []byte("not a gzip archive at all"), 0644); err != nil {
		t.Fatalf("write corrupt artifact: %v", err)
	}

	coordinator := NewServerReleaseCoordinator(serverRoot)
	_, err := coordinator.Install(projectID, corruptPath)
	if err == nil {
		t.Fatal("expected error for unverified artifact, got nil")
	}
	if !contains(err.Error(), "must be verified first") {
		t.Errorf("expected error to mention 'must be verified first', got: %v", err)
	}
}

// TestInstall_AccessArtifactError verifies the failure path for an artifact
// path that cannot even be statted (a non-ENOENT stat error, e.g. a path
// component that is a regular file).
func TestInstall_AccessArtifactError(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, _ := setupServerEnv(t, serverRoot)

	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}

	coordinator := NewServerReleaseCoordinator(serverRoot)
	_, err := coordinator.Install(projectID, filepath.Join(blocker, "artifact.tar.gz"))
	if err == nil {
		t.Fatal("expected access error for artifact under a file, got nil")
	}
	if !contains(err.Error(), "access artifact") {
		t.Errorf("expected error to mention 'access artifact', got: %v", err)
	}
}

// TestInstall_CreateDirectoryFailure verifies the failure path when the
// runtime directory structure cannot be created (a path component exists as
// a regular file).
func TestInstall_CreateDirectoryFailure(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, _ := setupServerEnv(t, serverRoot)
	installRoot := filepath.Join(serverRoot, "projects", projectID)

	// artifacts/ must be created by Install, but it exists as a file.
	if err := os.MkdirAll(installRoot, 0755); err != nil {
		t.Fatalf("mkdir install root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installRoot, "artifacts"), []byte("x"), 0644); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}

	artifactPath := createTestArtifact(t, projectID)
	coordinator := NewServerReleaseCoordinator(serverRoot)
	_, err := coordinator.Install(projectID, artifactPath)
	if err == nil {
		t.Fatal("expected directory creation error, got nil")
	}
	if !contains(err.Error(), "create directory") {
		t.Errorf("expected error to mention 'create directory', got: %v", err)
	}
}

// TestInstall_CopyArtifactFailure verifies the failure path when the
// artifact cannot be copied into the artifact store (non-root process
// against a read-only store directory).
func TestInstall_CopyArtifactFailure(t *testing.T) {
	if isRoot() {
		t.Skip("permission-denied copy requires a non-root process")
	}

	serverRoot := t.TempDir()
	projectID, _ := setupServerEnv(t, serverRoot)
	installRoot := filepath.Join(serverRoot, "projects", projectID)

	// Pre-create the artifact store as read-only so Install's MkdirAll
	// succeeds (dir exists) but the copy fails with EACCES. The install
	// root is created with normal permissions first — MkdirAll would
	// otherwise propagate 0555 to every parent it creates.
	if err := os.MkdirAll(installRoot, 0755); err != nil {
		t.Fatalf("mkdir install root: %v", err)
	}
	artifactStoreDir := filepath.Join(installRoot, "artifacts")
	if err := os.Mkdir(artifactStoreDir, 0555); err != nil {
		t.Fatalf("mkdir read-only artifact store: %v", err)
	}
	defer func() { _ = os.Chmod(artifactStoreDir, 0755) }()

	artifactPath := createTestArtifact(t, projectID)
	coordinator := NewServerReleaseCoordinator(serverRoot)
	_, err := coordinator.Install(projectID, artifactPath)
	if err == nil {
		t.Fatal("expected copy error, got nil")
	}
	if !contains(err.Error(), "copy artifact to store") {
		t.Errorf("expected error to mention 'copy artifact to store', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Stored-copy verification (TD-009 — artifact verification TOCTOU)
// ---------------------------------------------------------------------------

// TestVerifyStoredArtifact_ValidCopy verifies the TD-009 helper on the happy
// path: a byte-perfect copy of a verified artifact passes the stored-copy
// verification.
func TestVerifyStoredArtifact_ValidCopy(t *testing.T) {
	projectID := "test-project"
	artifactPath := createTestArtifact(t, projectID)
	storePath := filepath.Join(t.TempDir(), "store.tar.gz")
	if err := copyFile(artifactPath, storePath); err != nil {
		t.Fatalf("copy artifact to store: %v", err)
	}

	manifest, err := artifact.ReadManifest(artifactPath)
	if err != nil {
		t.Fatalf("read source manifest: %v", err)
	}

	if err := verifyStoredArtifact(storePath, manifest); err != nil {
		t.Fatalf("verifyStoredArtifact returned unexpected error: %v", err)
	}
}

// TestVerifyStoredArtifact_CorruptCopyFails verifies that a corrupt stored
// copy — e.g. an interrupted write that truncates the archive — fails the
// stored-copy verification, so it cannot be promoted to a release.
//
// DoD (TD-009): "A corrupt copy cannot produce a release."
func TestVerifyStoredArtifact_CorruptCopyFails(t *testing.T) {
	projectID := "test-project"
	artifactPath := createTestArtifact(t, projectID)
	storePath := filepath.Join(t.TempDir(), "store.tar.gz")
	if err := copyFile(artifactPath, storePath); err != nil {
		t.Fatalf("copy artifact to store: %v", err)
	}

	// Truncate the stored copy like an interrupted write. The manifest is
	// the last tar entry, so the truncated gzip stream cannot yield it and
	// verification must fail.
	info, err := os.Stat(storePath)
	if err != nil {
		t.Fatalf("stat stored copy: %v", err)
	}
	if err := os.Truncate(storePath, info.Size()/2); err != nil {
		t.Fatalf("truncate stored copy: %v", err)
	}

	manifest, err := artifact.ReadManifest(artifactPath)
	if err != nil {
		t.Fatalf("read source manifest: %v", err)
	}

	err = verifyStoredArtifact(storePath, manifest)
	if err == nil {
		t.Fatal("expected error for corrupt stored copy, got nil")
	}
	if !contains(err.Error(), "stored artifact failed verification") {
		t.Errorf("expected error to mention 'stored artifact failed verification', got: %v", err)
	}
}

// TestVerifyStoredArtifact_ManifestMismatchFails verifies that a stored copy
// containing a different — but equally valid — artifact fails the stored-copy
// verification: the source was swapped for another valid artifact between
// verification and copy, and the release record must never be built from a
// manifest that does not describe the stored bytes.
//
// DoD (TD-009): "A source change between verification and copy is detected."
func TestVerifyStoredArtifact_ManifestMismatchFails(t *testing.T) {
	projectID := "test-project"

	// The artifact that passed verification.
	verifiedArtifact := createTestArtifact(t, projectID)
	manifest, err := artifact.ReadManifest(verifiedArtifact)
	if err != nil {
		t.Fatalf("read verified source manifest: %v", err)
	}

	// The swapped source: a different, fully valid artifact (different
	// content yields a different content-derived identity, TS-P3-04).
	swappedArtifact := createTestArtifactVariant(t, projectID, "1.0.0", "<?php // swapped after verification\n")
	storePath := filepath.Join(t.TempDir(), "store.tar.gz")
	if err := copyFile(swappedArtifact, storePath); err != nil {
		t.Fatalf("copy swapped artifact to store: %v", err)
	}

	err = verifyStoredArtifact(storePath, manifest)
	if err == nil {
		t.Fatal("expected manifest mismatch error for swapped artifact, got nil")
	}
	if !contains(err.Error(), "does not match") {
		t.Errorf("expected error to mention 'does not match', got: %v", err)
	}
}

// TestInstall_StoredArtifactVerified verifies the TD-009 invariant from the
// positive side: after a successful install, the stored artifact passes full
// verification and its manifest matches the release record — the release
// references a proven-intact payload.
//
// DoD (TD-009): "The stored copy is verified (or checksum-compared) before
// release creation."
func TestInstall_StoredArtifactVerified(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, _ := setupServerEnv(t, serverRoot)
	artifactPath := createTestArtifact(t, projectID)

	coordinator := NewServerReleaseCoordinator(serverRoot)
	rel, err := coordinator.Install(projectID, artifactPath)
	if err != nil {
		t.Fatalf("Install returned unexpected error: %v", err)
	}

	if err := artifact.RequireVerified(rel.ArtifactPath); err != nil {
		t.Errorf("stored artifact failed verification after install: %v", err)
	}

	storedManifest, err := artifact.ReadManifest(rel.ArtifactPath)
	if err != nil {
		t.Fatalf("read stored manifest: %v", err)
	}
	if storedManifest.ArtifactID != rel.ArtifactID {
		t.Errorf("stored ArtifactID = %q, want %q", storedManifest.ArtifactID, rel.ArtifactID)
	}
}

// TestInstall_SourceChangeBetweenVerifyAndCopy_Fails is the TD-009 TOCTOU
// regression test: it simulates the source artifact changing between the
// verification step and the copy step, so the bytes stored in the artifact
// store differ from the bytes that were verified. Install must fail the
// stored-copy verification, must not create a release, and must not leave
// the unverified copy in the artifact store.
//
// The injection mirrors the existing adapterRunner/adapterExecutable test
// seam convention: the coordinator's storedArtifactVerifier is replaced with
// a hook that corrupts the stored copy before delegating to the default
// implementation — deterministically producing the exact effect a TOCTOU
// source change has on the stored bytes.
//
// DoD (TD-009): "A source change between verification and copy is detected."
// / "A corrupt copy cannot produce a release."
func TestInstall_SourceChangeBetweenVerifyAndCopy_Fails(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, installRoot := setupServerEnv(t, serverRoot)
	artifactPath := createTestArtifact(t, projectID)

	coordinator := NewServerReleaseCoordinator(serverRoot)
	defaultVerify := verifyStoredArtifact
	coordinator.storedArtifactVerifier = func(storePath string, expected *artifact.Manifest) error {
		// The verified source bytes were copied, but the stored bytes are
		// replaced — exactly what a source swap between verification and
		// copy produces.
		if err := os.WriteFile(storePath, []byte("replaced between verify and copy"), 0644); err != nil {
			t.Fatalf("replace stored copy: %v", err)
		}
		return defaultVerify(storePath, expected)
	}

	_, err := coordinator.Install(projectID, artifactPath)
	if err == nil {
		t.Fatal("expected install failure for TOCTOU source change, got nil")
	}
	if !contains(err.Error(), "verify stored artifact") {
		t.Errorf("expected error to mention 'verify stored artifact', got: %v", err)
	}

	// No release may be created from the unverified payload.
	releasesStateDir := filepath.Join(project.NewStructure(installRoot).StateDir, "releases")
	entries, err := os.ReadDir(releasesStateDir)
	if err != nil {
		t.Fatalf("read releases state dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("release JSON created despite stored-copy verification failure: %d entries", len(entries))
	}

	// The unverified copy must not remain in the artifact store (ADR-017
	// store contract: verified artifacts only).
	storeDir := filepath.Join(installRoot, "artifacts")
	entries, err = os.ReadDir(storeDir)
	if err != nil {
		t.Fatalf("read artifact store dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("unverified artifact left in store: %d entries", len(entries))
	}
}

// TestInstall_SaveReleaseFailure verifies the failure path when the Release
// JSON cannot be persisted (non-root process against a read-only release
// state directory).
func TestInstall_SaveReleaseFailure(t *testing.T) {
	if isRoot() {
		t.Skip("permission-denied save requires a non-root process")
	}

	serverRoot := t.TempDir()
	projectID, _ := setupServerEnv(t, serverRoot)
	installRoot := filepath.Join(serverRoot, "projects", projectID)

	// Pre-create the canonical state dir as read-only: Install's MkdirAll
	// succeeds (dir exists), but Release.Save cannot create its temp file.
	// The install root is created with normal permissions first — MkdirAll
	// would otherwise propagate 0555 to every parent it creates.
	if err := os.MkdirAll(installRoot, 0755); err != nil {
		t.Fatalf("mkdir install root: %v", err)
	}
	releasesStateDir := filepath.Join(project.NewStructure(installRoot).StateDir, "releases")
	if err := os.MkdirAll(filepath.Dir(releasesStateDir), 0755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	if err := os.Mkdir(releasesStateDir, 0555); err != nil {
		t.Fatalf("mkdir read-only release state dir: %v", err)
	}
	defer func() { _ = os.Chmod(releasesStateDir, 0755) }()

	artifactPath := createTestArtifact(t, projectID)
	coordinator := NewServerReleaseCoordinator(serverRoot)
	_, err := coordinator.Install(projectID, artifactPath)
	if err == nil {
		t.Fatal("expected save error, got nil")
	}
	if !contains(err.Error(), "save release") {
		t.Errorf("expected error to mention 'save release', got: %v", err)
	}
}

// TestInstall_SaveRuntimeStateFailure verifies the failure path when the
// runtime state cannot be persisted (runtime-state.json exists as a
// directory, so the atomic rename fails).
func TestInstall_SaveRuntimeStateFailure(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, _ := setupServerEnv(t, serverRoot)
	installRoot := filepath.Join(serverRoot, "projects", projectID)

	if err := os.MkdirAll(installRoot, 0755); err != nil {
		t.Fatalf("mkdir install root: %v", err)
	}
	if err := os.Mkdir(filepath.Join(installRoot, "runtime-state.json"), 0755); err != nil {
		t.Fatalf("mkdir runtime-state.json as directory: %v", err)
	}

	artifactPath := createTestArtifact(t, projectID)
	coordinator := NewServerReleaseCoordinator(serverRoot)
	_, err := coordinator.Install(projectID, artifactPath)
	if err == nil {
		t.Fatal("expected runtime state save error, got nil")
	}
	if !contains(err.Error(), "save runtime state") {
		t.Errorf("expected error to mention 'save runtime state', got: %v", err)
	}
}

// TestInstall_OwnershipFailure verifies the failure path when the project
// registry's owner cannot be resolved: Install must fail after persisting
// the release rather than silently skipping ownership.
func TestInstall_OwnershipFailure(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, _ := setupServerEnv(t, serverRoot)

	// Set an unresolvable owner on the registry so Install's final
	// ownership step fails. Rewrite the YAML directly (the store has no
	// update path).
	installRoot := filepath.Join(serverRoot, "projects", projectID)
	registryPath := filepath.Join(serverRoot, "projects", projectID+".yaml")
	registryYAML := "project:\n" +
		"  id: " + projectID + "\n" +
		"  install_root: " + installRoot + "\n" +
		"  display_name: Test Project\n" +
		"  owner: anvil-nonexistent-user-xyz\n"
	if err := os.WriteFile(registryPath, []byte(registryYAML), 0644); err != nil {
		t.Fatalf("rewrite registry with invalid owner: %v", err)
	}

	artifactPath := createTestArtifact(t, projectID)
	coordinator := NewServerReleaseCoordinator(serverRoot)
	_, err := coordinator.Install(projectID, artifactPath)
	if err == nil {
		t.Fatal("expected ownership error, got nil")
	}
	if !contains(err.Error(), "apply file ownership") {
		t.Errorf("expected error to mention 'apply file ownership', got: %v", err)
	}
}

// TestInstall_SkipsCorruptReleaseJSON verifies that the install idempotency
// scan tolerates unreadable entries in the releases state directory
// (corrupt JSON and non-file entries are skipped, not fatal).
func TestInstall_SkipsCorruptReleaseJSON(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, _ := setupServerEnv(t, serverRoot)
	installRoot := filepath.Join(serverRoot, "projects", projectID)

	// Plant corrupt entries in the canonical state dir before Install.
	releasesStateDir := filepath.Join(project.NewStructure(installRoot).StateDir, "releases")
	if err := os.MkdirAll(releasesStateDir, 0755); err != nil {
		t.Fatalf("mkdir release state dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(releasesStateDir, "corrupt.json"), []byte("{not json"), 0644); err != nil {
		t.Fatalf("write corrupt release JSON: %v", err)
	}
	if err := os.Mkdir(filepath.Join(releasesStateDir, "dir.json"), 0755); err != nil {
		t.Fatalf("mkdir directory entry: %v", err)
	}

	artifactPath := createTestArtifact(t, projectID)
	coordinator := NewServerReleaseCoordinator(serverRoot)
	if _, err := coordinator.Install(projectID, artifactPath); err != nil {
		t.Fatalf("Install returned unexpected error with corrupt state entries: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Activate failure paths (TD-010 §9 — coordinator critical paths)
// ---------------------------------------------------------------------------

// TestActivate_ExtractFailure verifies the failure path when the stored
// artifact cannot be extracted into the release directory (corrupt store
// content).
func TestActivate_ExtractFailure(t *testing.T) {
	serverRoot := t.TempDir()
	releaseID := "rel-extract-fail"

	projectID, _ := setupActivateEnvironment(t, serverRoot, releaseID)
	installRoot := filepath.Join(serverRoot, "projects", projectID)

	// Corrupt the stored artifact so extraction fails during Activate.
	storeArtifactPath := filepath.Join(installRoot, "artifacts", releaseID+".tar.gz")
	if err := os.WriteFile(storeArtifactPath, []byte("not a real archive"), 0644); err != nil {
		t.Fatalf("corrupt stored artifact: %v", err)
	}

	coordinator := NewServerReleaseCoordinator(serverRoot)
	err := coordinator.Activate(projectID, releaseID)
	if err == nil {
		t.Fatal("expected extraction error, got nil")
	}
	if !contains(err.Error(), "extract artifact for activation") {
		t.Errorf("expected error to mention 'extract artifact for activation', got: %v", err)
	}
}

// TestActivate_OwnershipFailure verifies the failure path when the release
// directory ownership cannot be applied (unresolvable owner in the
// registry).
func TestActivate_OwnershipFailure(t *testing.T) {
	serverRoot := t.TempDir()
	releaseID := "rel-ownership-fail"

	// Install against a registry without owner, then flip the registry to
	// an unresolvable owner before Activate (the store has no update path,
	// so the YAML is rewritten directly).
	projectID, _ := setupServerEnv(t, serverRoot)
	installRoot := filepath.Join(serverRoot, "projects", projectID)
	setupActivateRelease(t, projectID, installRoot, releaseID)

	registryPath := filepath.Join(serverRoot, "projects", projectID+".yaml")
	registryYAML := "project:\n" +
		"  id: " + projectID + "\n" +
		"  install_root: " + installRoot + "\n" +
		"  display_name: Test Project\n" +
		"  owner: anvil-nonexistent-user-xyz\n"
	if err := os.WriteFile(registryPath, []byte(registryYAML), 0644); err != nil {
		t.Fatalf("rewrite registry with invalid owner: %v", err)
	}

	coordinator := NewServerReleaseCoordinator(serverRoot)
	err := coordinator.Activate(projectID, releaseID)
	if err == nil {
		t.Fatal("expected ownership error, got nil")
	}
	if !contains(err.Error(), "apply release directory ownership") {
		t.Errorf("expected error to mention 'apply release directory ownership', got: %v", err)
	}
}

// TestActivate_SharedLinkFailure verifies the failure path when a shared
// link's parent directory cannot be created inside the release directory
// (an artifact file occupies the target's parent path).
func TestActivate_SharedLinkFailure(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, _ := registerE2EProject(t, serverRoot, []SharedLink{
		{From: "shared/config/app.env", To: "config/app.env"},
	}, "", "")

	// The artifact ships a regular file named "config", so the link target
	// parent (releaseDir/config) cannot be created as a directory.
	artifactPath := createTestArtifactWithFiles(t, projectID, "1.0.0", map[string]string{
		"config": "artifact content",
	})

	coordinator := NewServerReleaseCoordinator(serverRoot)
	rel, err := coordinator.Install(projectID, artifactPath)
	if err != nil {
		t.Fatalf("Install returned unexpected error: %v", err)
	}

	err = coordinator.Activate(projectID, rel.ID.String())
	if err == nil {
		t.Fatal("expected shared link error, got nil")
	}
	if !contains(err.Error(), "apply shared links") {
		t.Errorf("expected error to mention 'apply shared links', got: %v", err)
	}
}

// TestActivate_SaveReleaseFailure verifies the failure path when the
// post-activation Release save fails (non-root process against a read-only
// release state directory).
func TestActivate_SaveReleaseFailure(t *testing.T) {
	if isRoot() {
		t.Skip("permission-denied save requires a non-root process")
	}

	serverRoot := t.TempDir()
	releaseID := "rel-save-fail"

	projectID, _ := setupActivateEnvironment(t, serverRoot, releaseID)
	installRoot := filepath.Join(serverRoot, "projects", projectID)

	// Make the canonical state dir read-only after the fixture wrote the
	// Ready Release JSON: Load still works, the post-activation Save fails.
	releasesStateDir := filepath.Join(project.NewStructure(installRoot).StateDir, "releases")
	if err := os.Chmod(releasesStateDir, 0555); err != nil {
		t.Fatalf("chmod release state dir read-only: %v", err)
	}
	defer func() { _ = os.Chmod(releasesStateDir, 0755) }()

	coordinator := NewServerReleaseCoordinator(serverRoot)
	err := coordinator.Activate(projectID, releaseID)
	if err == nil {
		t.Fatal("expected release save error, got nil")
	}
	if !contains(err.Error(), "save release state") {
		t.Errorf("expected error to mention 'save release state', got: %v", err)
	}
}

// TestActivate_SaveRuntimeStateFailure verifies the failure path when the
// runtime state cannot be persisted after a successful activation
// (runtime-state.json exists as a directory, so the atomic rename fails).
func TestActivate_SaveRuntimeStateFailure(t *testing.T) {
	serverRoot := t.TempDir()
	releaseID := "rel-state-fail"

	projectID, _ := setupActivateEnvironment(t, serverRoot, releaseID)
	installRoot := filepath.Join(serverRoot, "projects", projectID)

	if err := os.Mkdir(filepath.Join(installRoot, "runtime-state.json"), 0755); err != nil {
		t.Fatalf("mkdir runtime-state.json as directory: %v", err)
	}

	coordinator := NewServerReleaseCoordinator(serverRoot)
	err := coordinator.Activate(projectID, releaseID)
	if err == nil {
		t.Fatal("expected runtime state save error, got nil")
	}
	if !contains(err.Error(), "save runtime state") {
		t.Errorf("expected error to mention 'save runtime state', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Rollback failure paths (TD-010 §9 — coordinator critical paths)
// ---------------------------------------------------------------------------

// TestRollback_UnregisteredProject verifies the failure path for rollback
// against an unknown project.
func TestRollback_UnregisteredProject(t *testing.T) {
	serverRoot := t.TempDir()

	coordinator := NewServerReleaseCoordinator(serverRoot)
	_, err := coordinator.Rollback("nonexistent-project")
	if err == nil {
		t.Fatal("expected error for unregistered project, got nil")
	}
	if !contains(err.Error(), "project registry not found") {
		t.Errorf("expected error to mention 'project registry not found', got: %v", err)
	}
}

// TestRollback_SaveRuntimeStateFailure verifies the failure path when the
// runtime state cannot be persisted after a successful rollback engine run
// (runtime-state.json exists as a directory, so the atomic rename fails).
func TestRollback_SaveRuntimeStateFailure(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, installRoot := setupServerEnv(t, serverRoot)

	coordinator := NewServerReleaseCoordinator(serverRoot)

	// Full two-release setup so the rollback engine has a target.
	artifactA := createTestArtifactVariant(t, projectID, "1.0.0", "<?php // release A\n")
	artifactB := createTestArtifactVariant(t, projectID, "1.1.0", "<?php // release B\n")
	relA, err := coordinator.Install(projectID, artifactA)
	if err != nil {
		t.Fatalf("Install A returned unexpected error: %v", err)
	}
	relB, err := coordinator.Install(projectID, artifactB)
	if err != nil {
		t.Fatalf("Install B returned unexpected error: %v", err)
	}
	if err := coordinator.Activate(projectID, relA.ID.String()); err != nil {
		t.Fatalf("Activate A returned unexpected error: %v", err)
	}
	if err := coordinator.Activate(projectID, relB.ID.String()); err != nil {
		t.Fatalf("Activate B returned unexpected error: %v", err)
	}

	// Block the final runtime-state persistence: Install/Activate already
	// created runtime-state.json, so replace it with a directory before
	// rollback.
	runtimeStatePath := filepath.Join(installRoot, "runtime-state.json")
	if err := os.Remove(runtimeStatePath); err != nil {
		t.Fatalf("remove runtime-state.json: %v", err)
	}
	if err := os.Mkdir(runtimeStatePath, 0755); err != nil {
		t.Fatalf("mkdir runtime-state.json as directory: %v", err)
	}

	_, err = coordinator.Rollback(projectID)
	if err == nil {
		t.Fatal("expected runtime state save error, got nil")
	}
	if !contains(err.Error(), "save runtime state") {
		t.Errorf("expected error to mention 'save runtime state', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Adapter executable resolution (TS-P7-11 wiring)
// ---------------------------------------------------------------------------

// TestInstall_MissingAdapterExecutable_Fails verifies the failure path when
// a project selects a framework adapter whose executable is not on PATH:
// the coordinator must fail with a descriptive error instead of silently
// skipping verification.
func TestInstall_MissingAdapterExecutable_Fails(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, installRoot := setupServerEnv(t, serverRoot)

	registryPath := filepath.Join(serverRoot, "projects", projectID+".yaml")
	registryYAML := "project:\n" +
		"  id: " + projectID + "\n" +
		"  install_root: " + installRoot + "\n" +
		"  display_name: Test Project\n" +
		"  adapter: anvil-totally-missing-adapter\n"
	if err := os.WriteFile(registryPath, []byte(registryYAML), 0644); err != nil {
		t.Fatalf("rewrite registry with missing adapter: %v", err)
	}

	artifactPath := createTestArtifact(t, projectID)
	coordinator := NewServerReleaseCoordinator(serverRoot)
	_, err := coordinator.Install(projectID, artifactPath)
	if err == nil {
		t.Fatal("expected adapter executable error, got nil")
	}
	if !contains(err.Error(), "not found on PATH") {
		t.Errorf("expected error to mention 'not found on PATH', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Build wiring helpers (TS-007-040, TS-P7-14)
// ---------------------------------------------------------------------------

// TestFirstBuildFailure verifies the failure-detail selection of the build
// report: structured phase error first, output excerpt as fallback, and a
// descriptive message when no failing phase is reported.
func TestFirstBuildFailure(t *testing.T) {
	cases := []struct {
		name   string
		phases []contracts.BuildPhaseResult
		want   string
	}{
		{
			name:   "structured error wins",
			phases: []contracts.BuildPhaseResult{{Phase: "composer", Success: false, Error: "exit status 1"}},
			want:   `phase "composer": exit status 1`,
		},
		{
			name:   "output excerpt fallback",
			phases: []contracts.BuildPhaseResult{{Phase: "composer", Success: false, Output: "composer install failed"}},
			want:   `phase "composer": composer install failed`,
		},
		{
			name:   "first failing phase wins",
			phases: []contracts.BuildPhaseResult{{Phase: "a", Success: true}, {Phase: "b", Success: false, Error: "boom"}, {Phase: "c", Success: false, Error: "ignored"}},
			want:   `phase "b": boom`,
		},
		{
			name:   "all succeeded",
			phases: []contracts.BuildPhaseResult{{Phase: "a", Success: true}, {Phase: "b", Success: true}},
			want:   "no failing phase reported",
		},
		{
			name:   "no phases",
			phases: nil,
			want:   "no failing phase reported",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstBuildFailure(tc.phases); got != tc.want {
				t.Errorf("firstBuildFailure() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestBuildOutputExcerpt verifies the bounded, trimmed output excerpt used
// in build failure details.
func TestBuildOutputExcerpt(t *testing.T) {
	longOutput := strings.Repeat("x", 2000)

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"short output passthrough", "composer install failed", "composer install failed"},
		{"whitespace trimmed", "  composer failed  ", "composer failed"},
		{"long output truncated", longOutput, strings.Repeat("x", 1000) + "..."},
		{"empty output", "", "no output or error reported for the phase"},
		{"whitespace only", "   \n\t", "no output or error reported for the phase"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildOutputExcerpt(tc.in); got != tc.want {
				t.Errorf("buildOutputExcerpt() = %q, want %q", got, tc.want)
			}
		})
	}
}
