// Package cmd implements the Anvil CLI commands.
//
// Tests for replaceBinary / replaceBinaryFallback (cmd/update.go), the
// atomic binary replacement shared by "anvil update" and "anvil adapter
// install". The regression focus: installing a brand-new binary must not
// fail when the rename crosses filesystems (EXDEV) and the target does
// not exist yet — the fallback previously errored with "remove old
// binary: no such file or directory".
//
// Reference: TS-007-037 §3, TS-007-036 §7
package cmd

import (
	"os"
	"path/filepath"
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
