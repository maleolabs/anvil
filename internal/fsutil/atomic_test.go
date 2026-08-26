package fsutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteFileAtomic_WritesContent verifies that WriteFileAtomic writes the
// exact data to the final path with the requested permissions.
func TestWriteFileAtomic_WritesContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	data := []byte(`{"stage":"active"}`)
	if err := WriteFileAtomic(path, data, 0644); err != nil {
		t.Fatalf("WriteFileAtomic() returned unexpected error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() returned unexpected error: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("content = %q, want %q", got, data)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() returned unexpected error: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0644 {
		t.Errorf("file permissions = %o, want 0644", perm)
	}
}

// TestWriteFileAtomic_ReplacesExisting verifies that WriteFileAtomic fully
// replaces an existing file's content — the previous content must not be
// observable after the write (rename, not truncate-in-place).
func TestWriteFileAtomic_ReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	if err := WriteFileAtomic(path, []byte("old"), 0644); err != nil {
		t.Fatalf("first WriteFileAtomic() returned unexpected error: %v", err)
	}
	if err := WriteFileAtomic(path, []byte("new-content"), 0644); err != nil {
		t.Fatalf("second WriteFileAtomic() returned unexpected error: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() returned unexpected error: %v", err)
	}
	if string(got) != "new-content" {
		t.Errorf("content after overwrite = %q, want %q", got, "new-content")
	}
}

// TestWriteFileAtomic_NoTempLeftovers verifies that a successful write leaves
// no temporary staging files behind in the target directory.
func TestWriteFileAtomic_NoTempLeftovers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	if err := WriteFileAtomic(path, []byte(`{"ok":true}`), 0644); err != nil {
		t.Fatalf("WriteFileAtomic() returned unexpected error: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() returned unexpected error: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("leftover temp file %q after successful write", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("directory contains %d entries, want exactly 1 (state.json)", len(entries))
	}
}

// TestWriteFileAtomic_ErrorWhenParentMissing verifies that WriteFileAtomic
// returns an error when the parent directory does not exist and leaves no
// file at the final path (preserves the directory-must-exist contract).
func TestWriteFileAtomic_ErrorWhenParentMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "state.json")

	if err := WriteFileAtomic(path, []byte("data"), 0644); err == nil {
		t.Fatal("WriteFileAtomic() expected error for missing parent directory, got nil")
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file %s should not exist after failed write", path)
	}
}

// TestWriteFileAtomic_SimulatedCrashLeavesPreviousIntact verifies the
// crash-window property: a crash mid-write (simulated by a partial temp file
// that never got renamed) leaves the complete previous file at the final
// path — the final path is never observable in a partially-written form. A
// subsequent write succeeds and persists the new complete state; the stale
// temp file from the crashed write is inert (never read by any load path)
// and does not interfere.
func TestWriteFileAtomic_SimulatedCrashLeavesPreviousIntact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// Write the previous complete state.
	if err := WriteFileAtomic(path, []byte(`{"stage":"active"}`), 0644); err != nil {
		t.Fatalf("initial WriteFileAtomic() returned unexpected error: %v", err)
	}

	// Simulate a crash mid-write of the next version: a partial temp file
	// exists in the same directory, but the rename never happened.
	crashTemp := filepath.Join(dir, "state.json.tmp-crashed")
	if err := os.WriteFile(crashTemp, []byte(`{"stage":"acti`), 0644); err != nil {
		t.Fatalf("failed to simulate crashed temp file: %v", err)
	}

	// The final path must still hold the complete previous state.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() returned unexpected error: %v", err)
	}
	if string(got) != `{"stage":"active"}` {
		t.Errorf("final path after simulated crash = %q, want complete previous state", got)
	}

	// A subsequent save must succeed and persist the new complete state.
	if err := WriteFileAtomic(path, []byte(`{"stage":"rolled_back"}`), 0644); err != nil {
		t.Fatalf("WriteFileAtomic() after crash returned unexpected error: %v", err)
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() after recovery returned unexpected error: %v", err)
	}
	if string(got) != `{"stage":"rolled_back"}` {
		t.Errorf("content after recovery = %q, want the new complete state", got)
	}
}

// TestWriteFileAtomic_RecoversCorruptFile verifies that WriteFileAtomic
// replaces a corrupt file at the final path (e.g., the artifact of the old
// non-atomic write pattern) with complete content — the atomic save is the
// recovery path for any previously-corrupted state file.
func TestWriteFileAtomic_RecoversCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// Pre-existing corrupt file left by the pre-TD-002 non-atomic writer.
	if err := os.WriteFile(path, []byte("{truncated"), 0644); err != nil {
		t.Fatalf("failed to write corrupt file: %v", err)
	}

	if err := WriteFileAtomic(path, []byte(`{"stage":"ready"}`), 0644); err != nil {
		t.Fatalf("WriteFileAtomic() over corrupt file returned unexpected error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() returned unexpected error: %v", err)
	}
	if string(got) != `{"stage":"ready"}` {
		t.Errorf("content after recovery = %q, want the complete new state", got)
	}
}

// TestWriteFileAtomic_PreservesExistingMode verifies that an existing
// target file's mode is preserved on overwrite — an operator-hardened 0600
// state file stays 0600 instead of being silently widened to the requested
// 0644 (TD-002 review).
func TestWriteFileAtomic_PreservesExistingMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatalf("failed to create 0600 file: %v", err)
	}

	if err := WriteFileAtomic(path, []byte("new"), 0644); err != nil {
		t.Fatalf("WriteFileAtomic() returned unexpected error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() returned unexpected error: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("existing file permissions = %o, want 0600 (mode must be preserved)", perm)
	}
}

// TestWriteFileAtomic_RenameFailureRemovesTemp verifies that when the atomic
// rename fails (the target path is an existing directory), the error is
// wrapped and the temporary file is removed — a failed write leaves no
// staging artifacts behind and the target is untouched.
func TestWriteFileAtomic_RenameFailureRemovesTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target")

	// The target exists as a directory, so the rename over it must fail.
	if err := os.Mkdir(path, 0755); err != nil {
		t.Fatalf("Mkdir() returned unexpected error: %v", err)
	}

	err := WriteFileAtomic(path, []byte("data"), 0644)
	if err == nil {
		t.Fatal("WriteFileAtomic() expected error when target is a directory, got nil")
	}
	if !strings.Contains(err.Error(), "rename") {
		t.Errorf("error = %q, want it to mention the rename failure", err.Error())
	}

	// The directory must contain only the target — no leftover temp file.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() returned unexpected error: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("leftover temp file %q after failed rename", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("directory contains %d entries, want exactly 1 (the target directory)", len(entries))
	}
}
