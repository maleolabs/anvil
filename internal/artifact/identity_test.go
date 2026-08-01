// Package artifact defines the Anvil artifact directory layout and
// packaging primitives.
//
// Reference: TS-P3-04, EPIC-003
package artifact

import (
	"os"
	"path/filepath"
	"testing"
)

// setupIdentitySource creates a temporary directory with test files for
// identity generation tests. Returns the root directory path and the list
// of relative file paths.
func setupIdentitySource(t *testing.T) (string, []string) {
	t.Helper()

	root := t.TempDir()

	files := map[string]string{
		"index.php":               "<?php\n",
		"src/App.php":             "<?php namespace App;\n",
		"src/Controller/Home.php": "<?php namespace App\\Controller;\n",
		"config/app.php":          "<?php\nreturn [];\n",
		"composer.json":           `{"name": "test/app"}`,
	}

	var paths []string
	for relPath, content := range files {
		fullPath := filepath.Join(root, relPath)
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", relPath, err)
		}
		paths = append(paths, relPath)
	}

	return root, paths
}

// TestGenerateIdentity_Deterministic verifies that the same set of files
// always produces the same identity.
func TestGenerateIdentity_Deterministic(t *testing.T) {
	sourceDir, files := setupIdentitySource(t)

	id1, err := GenerateIdentity(sourceDir, files)
	if err != nil {
		t.Fatalf("first call returned error: %v", err)
	}

	id2, err := GenerateIdentity(sourceDir, files)
	if err != nil {
		t.Fatalf("second call returned error: %v", err)
	}

	if id1 != id2 {
		t.Errorf("identity differs between calls:\n  first:  %s\n  second: %s", id1, id2)
	}

	if id1 == "" {
		t.Error("identity must not be empty for non-empty file set")
	}
}

// TestGenerateIdentity_ContentSensitivity verifies that different file content
// produces a different identity.
func TestGenerateIdentity_ContentSensitivity(t *testing.T) {
	sourceDir, files := setupIdentitySource(t)

	originalID, err := GenerateIdentity(sourceDir, files)
	if err != nil {
		t.Fatalf("original identity: %v", err)
	}

	// Modify one file.
	changedPath := filepath.Join(sourceDir, "index.php")
	if err := os.WriteFile(changedPath, []byte("<?php\n// modified\n"), 0644); err != nil {
		t.Fatalf("write modified file: %v", err)
	}

	modifiedID, err := GenerateIdentity(sourceDir, files)
	if err != nil {
		t.Fatalf("modified identity: %v", err)
	}

	if originalID == modifiedID {
		t.Error("identity must change when file content changes")
	}
}

// TestGenerateIdentity_EmptyFiles verifies that an empty file list produces
// an empty identity with no error.
func TestGenerateIdentity_EmptyFiles(t *testing.T) {
	sourceDir := t.TempDir()

	id, err := GenerateIdentity(sourceDir, []string{})
	if err != nil {
		t.Fatalf("unexpected error for empty files: %v", err)
	}

	if id != "" {
		t.Errorf("expected empty identity, got %q", id)
	}
}

// TestGenerateIdentity_MissingSourceFile verifies that a missing source file
// returns an error.
func TestGenerateIdentity_MissingSourceFile(t *testing.T) {
	sourceDir := t.TempDir()

	_, err := GenerateIdentity(sourceDir, []string{"nonexistent.php"})
	if err == nil {
		t.Error("expected error for missing source file, got nil")
	}
}

// TestGenerateIdentity_OrderInvariance verifies that identity is the same
// regardless of the order files are passed in.
func TestGenerateIdentity_OrderInvariance(t *testing.T) {
	sourceDir, files := setupIdentitySource(t)

	id1, err := GenerateIdentity(sourceDir, files)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Reverse the file order.
	reversed := make([]string, len(files))
	for i, f := range files {
		reversed[len(files)-1-i] = f
	}

	id2, err := GenerateIdentity(sourceDir, reversed)
	if err != nil {
		t.Fatalf("reversed call: %v", err)
	}

	if id1 != id2 {
		t.Error("identity must be independent of file order")
	}
}

// TestGenerateIdentity_SkipsDirectories verifies that directories in the file
// list are skipped silently (defensive behavior). This guards against edge
// cases where the file filter might return a directory path.
func TestGenerateIdentity_SkipsDirectories(t *testing.T) {
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

	// Pass both the file and the directory to GenerateIdentity.
	// The directory should be skipped, only the file should contribute.
	id, err := GenerateIdentity(sourceDir, []string{"regular.txt", "somedir"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Compute expected identity from just the regular file.
	expectedID, err := GenerateIdentity(sourceDir, []string{"regular.txt"})
	if err != nil {
		t.Fatalf("expected identity: %v", err)
	}

	if id != expectedID {
		t.Errorf("identity with directory in list differs from file-only:\n  got:      %s\n  expected: %s", id, expectedID)
	}
}
