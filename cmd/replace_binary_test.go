// Package cmd implements the Anvil CLI commands.
//
// Tests for replaceBinary / replaceBinaryFallback (cmd/update.go), the
// atomic binary replacement shared by "anvil update" and "anvil adapter
// install". The regression focus: installing a brand-new binary must not
// fail when the rename crosses filesystems (EXDEV) and the target does
// not exist yet — the fallback previously errored with "remove old
// binary: no such file or directory".
//
// Reference: TS-007-037 §3, TS-007-036 §7, TD-003
package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReplaceBinary_InstallsWhenTargetMissing verifies the fast path: a
// same-filesystem rename installs a new binary when no old binary exists
// (the everyday fresh-install case).
func TestReplaceBinary_InstallsWhenTargetMissing(t *testing.T) {
	dir := t.TempDir()
	tmpPath := filepath.Join(dir, "anvil-download-tmp")
	targetPath := filepath.Join(dir, "anvil-adapter-laravel")
	if err := os.WriteFile(tmpPath, []byte("fresh binary"), 0644); err != nil {
		t.Fatalf("write temp binary: %v", err)
	}

	if err := replaceBinary(tmpPath, targetPath); err != nil {
		t.Fatalf("replaceBinary: %v", err)
	}
	verifyTestFileContent(t, targetPath, "fresh binary")
}

// TestReplaceBinary_ReplacesExisting verifies the fast path when an old
// binary exists: the rename replaces it in place.
func TestReplaceBinary_ReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	tmpPath := filepath.Join(dir, "anvil-download-tmp")
	targetPath := filepath.Join(dir, "anvil-adapter-laravel")
	writeTestFile(t, dir, "anvil-adapter-laravel", "old binary")
	if err := os.WriteFile(tmpPath, []byte("new binary"), 0644); err != nil {
		t.Fatalf("write temp binary: %v", err)
	}

	if err := replaceBinary(tmpPath, targetPath); err != nil {
		t.Fatalf("replaceBinary: %v", err)
	}
	verifyTestFileContent(t, targetPath, "new binary")
}

// TestReplaceBinaryFallback_InstallsWhenTargetMissing is the regression
// test for the reported bug: on a cross-device rename failure (EXDEV,
// e.g. /tmp on tmpfs vs the install dir on ext4) the fallback removes the
// old entry before creating the new file — when there is no old binary
// (fresh install), removing must be skipped, not error out.
func TestReplaceBinaryFallback_InstallsWhenTargetMissing(t *testing.T) {
	dir := t.TempDir()
	tmpPath := filepath.Join(dir, "anvil-download-tmp")
	targetPath := filepath.Join(dir, "anvil-adapter-laravel")
	if err := os.WriteFile(tmpPath, []byte("fresh binary"), 0644); err != nil {
		t.Fatalf("write temp binary: %v", err)
	}

	if err := replaceBinaryFallback(tmpPath, targetPath); err != nil {
		t.Fatalf("replaceBinaryFallback: %v", err)
	}
	verifyTestFileContent(t, targetPath, "fresh binary")
}

// TestReplaceBinaryFallback_ReplacesExisting verifies that the fallback
// still replaces an existing binary (the "anvil update" cross-device
// case where the running binary lives in the install directory).
func TestReplaceBinaryFallback_ReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	tmpPath := filepath.Join(dir, "anvil-download-tmp")
	targetPath := filepath.Join(dir, "anvil-adapter-laravel")
	writeTestFile(t, dir, "anvil-adapter-laravel", "old binary")
	if err := os.WriteFile(tmpPath, []byte("new binary"), 0644); err != nil {
		t.Fatalf("write temp binary: %v", err)
	}

	if err := replaceBinaryFallback(tmpPath, targetPath); err != nil {
		t.Fatalf("replaceBinaryFallback: %v", err)
	}
	verifyTestFileContent(t, targetPath, "new binary")
}

// TestReplaceBinaryFallback_CrashAfterCopyLeavesOldIntact injects a failure
// between the copy and the rename (TD-003 §9): the target path must still
// hold the complete old binary — never nothing, never a truncated file —
// and a subsequent install must succeed (recovery from the crash window).
func TestReplaceBinaryFallback_CrashAfterCopyLeavesOldIntact(t *testing.T) {
	dir := t.TempDir()
	tmpPath := filepath.Join(dir, "anvil-download-tmp")
	targetPath := filepath.Join(dir, "anvil-adapter-laravel")
	writeTestFile(t, dir, "anvil-adapter-laravel", "old binary")
	if err := os.WriteFile(tmpPath, []byte("new binary"), 0644); err != nil {
		t.Fatalf("write temp binary: %v", err)
	}

	replaceBinaryFallbackAfterCopy = func() error { return errors.New("simulated crash after copy") }
	t.Cleanup(func() { replaceBinaryFallbackAfterCopy = nil })

	if err := replaceBinaryFallback(tmpPath, targetPath); err == nil {
		t.Fatal("replaceBinaryFallback: expected error from injected crash after copy, got nil")
	}
	verifyTestFileContent(t, targetPath, "old binary")

	// Recovery: a subsequent install without the fault succeeds and the
	// staging temp file from the crashed attempt is inert.
	replaceBinaryFallbackAfterCopy = nil
	if err := replaceBinaryFallback(tmpPath, targetPath); err != nil {
		t.Fatalf("replaceBinaryFallback after crash: %v", err)
	}
	verifyTestFileContent(t, targetPath, "new binary")
}

// TestReplaceBinaryFallback_CrashAfterRenameLeavesNewComplete injects a
// failure between the rename and completion (TD-003 §9): the replacement
// already happened atomically, so a crash here must leave the target path
// holding the complete new binary — not absent, not truncated.
func TestReplaceBinaryFallback_CrashAfterRenameLeavesNewComplete(t *testing.T) {
	dir := t.TempDir()
	tmpPath := filepath.Join(dir, "anvil-download-tmp")
	targetPath := filepath.Join(dir, "anvil-adapter-laravel")
	writeTestFile(t, dir, "anvil-adapter-laravel", "old binary")
	if err := os.WriteFile(tmpPath, []byte("new binary"), 0644); err != nil {
		t.Fatalf("write temp binary: %v", err)
	}

	replaceBinaryFallbackAfterRename = func() error { return errors.New("simulated crash after rename") }
	t.Cleanup(func() { replaceBinaryFallbackAfterRename = nil })

	if err := replaceBinaryFallback(tmpPath, targetPath); err == nil {
		t.Fatal("replaceBinaryFallback: expected error from injected crash after rename, got nil")
	}
	verifyTestFileContent(t, targetPath, "new binary")
}

// TestReplaceBinaryFallback_CopyFailureLeavesOldIntactAndCleansTemp
// verifies that a failure while copying (source temp file unreadable)
// leaves the old binary untouched and removes the staging temp file — a
// failed install never leaves a partial binary at the target path and
// leaves no staging artifacts behind.
func TestReplaceBinaryFallback_CopyFailureLeavesOldIntactAndCleansTemp(t *testing.T) {
	dir := t.TempDir()
	tmpPath := filepath.Join(dir, "anvil-download-missing")
	targetPath := filepath.Join(dir, "anvil-adapter-laravel")
	writeTestFile(t, dir, "anvil-adapter-laravel", "old binary")

	if err := replaceBinaryFallback(tmpPath, targetPath); err == nil {
		t.Fatal("replaceBinaryFallback: expected error for missing source, got nil")
	}
	verifyTestFileContent(t, targetPath, "old binary")

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("leftover temp file %q after failed install", e.Name())
		}
	}
}

// TestReplaceBinaryFallback_NoTempLeftovers verifies that a successful
// install leaves no staging temp files behind in the target directory.
func TestReplaceBinaryFallback_NoTempLeftovers(t *testing.T) {
	dir := t.TempDir()
	tmpPath := filepath.Join(dir, "anvil-download-tmp")
	targetPath := filepath.Join(dir, "anvil-adapter-laravel")
	writeTestFile(t, dir, "anvil-adapter-laravel", "old binary")
	if err := os.WriteFile(tmpPath, []byte("new binary"), 0644); err != nil {
		t.Fatalf("write temp binary: %v", err)
	}

	if err := replaceBinaryFallback(tmpPath, targetPath); err != nil {
		t.Fatalf("replaceBinaryFallback: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("leftover temp file %q after successful install", e.Name())
		}
	}
	if len(entries) != 2 {
		t.Errorf("directory contains %d entries, want exactly 2 (target + source)", len(entries))
	}
}

// TestReplaceBinaryFallback_SetsExecutablePermission verifies that the
// fallback preserves executable permission semantics: the installed binary
// is always 0755 (TD-003 §9).
func TestReplaceBinaryFallback_SetsExecutablePermission(t *testing.T) {
	dir := t.TempDir()
	tmpPath := filepath.Join(dir, "anvil-download-tmp")
	targetPath := filepath.Join(dir, "anvil-adapter-laravel")
	if err := os.WriteFile(tmpPath, []byte("new binary"), 0644); err != nil {
		t.Fatalf("write temp binary: %v", err)
	}

	if err := replaceBinaryFallback(tmpPath, targetPath); err != nil {
		t.Fatalf("replaceBinaryFallback: %v", err)
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0755 {
		t.Errorf("installed binary permissions = %o, want 0755", perm)
	}
}
