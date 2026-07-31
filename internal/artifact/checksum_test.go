// Package artifact defines the Anvil artifact directory layout and
// packaging primitives.
//
// Reference: TS-P3-06, EPIC-003
package artifact

import (
	"os"
	"path/filepath"
	"testing"
)

// TestComputeChecksum_Deterministic verifies that the same set of files always
// produces the same checksum.
func TestComputeChecksum_Deterministic(t *testing.T) {
	sourceDir, files := setupIdentitySource(t)

	cs1, err := ComputeChecksum(sourceDir, files)
	if err != nil {
		t.Fatalf("first call returned error: %v", err)
	}

	cs2, err := ComputeChecksum(sourceDir, files)
	if err != nil {
		t.Fatalf("second call returned error: %v", err)
	}

	if cs1 != cs2 {
		t.Errorf("checksum differs between calls:\n  first:  %s\n  second: %s", cs1, cs2)
	}

	if cs1 == "" {
		t.Error("checksum must not be empty for non-empty file set")
	}
}

// TestComputeChecksum_ContentSensitivity verifies that different file content
// produces a different checksum.
func TestComputeChecksum_ContentSensitivity(t *testing.T) {
	sourceDir, files := setupIdentitySource(t)

	originalCS, err := ComputeChecksum(sourceDir, files)
	if err != nil {
		t.Fatalf("original checksum: %v", err)
	}

	// Modify one file.
	changedPath := filepath.Join(sourceDir, "index.php")
	if err := os.WriteFile(changedPath, []byte("<?php\n// modified\n"), 0644); err != nil {
		t.Fatalf("write modified file: %v", err)
	}

	modifiedCS, err := ComputeChecksum(sourceDir, files)
	if err != nil {
		t.Fatalf("modified checksum: %v", err)
	}

	if originalCS == modifiedCS {
		t.Error("checksum must change when file content changes")
	}
}

// TestComputeChecksum_EmptyFiles verifies that an empty file list produces an
// empty checksum with no error.
func TestComputeChecksum_EmptyFiles(t *testing.T) {
	sourceDir := t.TempDir()

	cs, err := ComputeChecksum(sourceDir, []string{})
	if err != nil {
		t.Fatalf("unexpected error for empty files: %v", err)
	}

	if cs != "" {
		t.Errorf("expected empty checksum, got %q", cs)
	}
}

// TestComputeChecksum_OrderInvariance verifies that checksum is the same
// regardless of the order files are passed in.
func TestComputeChecksum_OrderInvariance(t *testing.T) {
	sourceDir, files := setupIdentitySource(t)

	cs1, err := ComputeChecksum(sourceDir, files)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Reverse the file order.
	reversed := make([]string, len(files))
	for i, f := range files {
		reversed[len(files)-1-i] = f
	}

	cs2, err := ComputeChecksum(sourceDir, reversed)
	if err != nil {
		t.Fatalf("reversed call: %v", err)
	}

	if cs1 != cs2 {
		t.Error("checksum must be independent of file order")
	}
}

// TestComputeChecksum_MissingSourceFile verifies that a missing source file
// returns an error.
func TestComputeChecksum_MissingSourceFile(t *testing.T) {
	sourceDir := t.TempDir()

	_, err := ComputeChecksum(sourceDir, []string{"nonexistent.php"})
	if err == nil {
		t.Error("expected error for missing source file, got nil")
	}
}

// TestComputeChecksum_AlgorithmConstant verifies the algorithm constant is
// correctly defined.
func TestComputeChecksum_AlgorithmConstant(t *testing.T) {
	if ChecksumAlgorithmSHA256 != "sha-256" {
		t.Errorf("expected \"sha-256\", got %q", ChecksumAlgorithmSHA256)
	}
}

// TestComputeChecksum_SkipsDirectories verifies that directories in the file
// list are skipped silently (defensive behavior). This guards against edge
// cases where the file filter might return a directory path.
func TestComputeChecksum_SkipsDirectories(t *testing.T) {
	sourceDir := t.TempDir()

	// Create a regular file.
	filePath := filepath.Join(sourceDir, "regular.txt")
	if err := os.WriteFile(filePath, []byte("content"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Create a directory with the same name pattern that might be returned
	// by a buggy filter.
	dirPath := filepath.Join(sourceDir, "somedir")
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Pass both the file and the directory to ComputeChecksum.
	// The directory should be skipped, only the file should contribute.
	cs, err := ComputeChecksum(sourceDir, []string{"regular.txt", "somedir"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Compute expected checksum from just the regular file.
	expectedCS, err := ComputeChecksum(sourceDir, []string{"regular.txt"})
	if err != nil {
		t.Fatalf("expected checksum: %v", err)
	}

	if cs != expectedCS {
		t.Errorf("checksum with directory in list differs from file-only:\n  got:      %s\n  expected: %s", cs, expectedCS)
	}
}
