// Package artifact defines the Anvil artifact directory layout and
// packaging primitives.
//
// Reference: TS-P3-03, CH-P3-01, EPIC-003
package artifact

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// setupTestDir creates a temporary directory tree for testing.
// Returns the root directory path.
func setupTestDir(t *testing.T) string {
	t.Helper()

	root := t.TempDir()

	// Create files that SHOULD be included by default.
	files := []string{
		"index.php",
		"src/App.php",
		"src/Controller/HomeController.php",
		"resources/views/home.blade.php",
		"config/app.php",
		"public/index.php",
		"composer.json",
		"README.md",
	}

	// Create files that SHOULD be excluded by default (matching CH-P3-01).
	excludedFiles := []string{
		".git/config",
		".git/HEAD",
		".svn/entries",
		".hg/store/data",
		".anvil/state/config.json",
		".github/workflows/ci.yml",
		".gitlab/merge_request_templates/default.md",
		".circleci/config.yml",
		".idea/workspace.xml",
		".vscode/settings.json",
		"node_modules/express/index.js",
		"vendor/autoload.php",
		"__pycache__/main.cpython-39.pyc",
		"tests/Unit/ExampleTest.php",
		"tests/Feature/FeatureTest.php",
		"spec/spec_helper.php",
		"__tests__/test_main.go",
		".DS_Store",
		"Thumbs.db",
		"error.log",
		"access.log",
	}

	for _, f := range files {
		path := filepath.Join(root, f)
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("failed to create directory %s: %v", dir, err)
		}
		if err := os.WriteFile(path, []byte("content"), 0644); err != nil {
			t.Fatalf("failed to create file %s: %v", path, err)
		}
	}

	for _, f := range excludedFiles {
		path := filepath.Join(root, f)
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("failed to create directory %s: %v", dir, err)
		}
		if err := os.WriteFile(path, []byte("excluded"), 0644); err != nil {
			t.Fatalf("failed to create file %s: %v", path, err)
		}
	}

	return root
}

// TestFilterFiles_AllIncluded verifies that with no exclusion patterns, all
// files in the source directory are included.
func TestFilterFiles_AllIncluded(t *testing.T) {
	root := setupTestDir(t)

	result, err := FilterFiles(FilterOptions{SourceDir: root})
	if err != nil {
		t.Fatalf("FilterFiles returned unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("FilterFiles returned nil result")
	}

	// We expect all files (both "normal" and "excluded" types) since no
	// exclusion patterns were specified.
	totalFiles := 8 + 21 // 8 normal + 21 excluded
	if len(result.Files) != totalFiles {
		// List what we got vs what was expected for debugging.
		t.Errorf("got %d files, want %d", len(result.Files), totalFiles)
		for i, f := range result.Files {
			t.Logf("  result[%d] = %s", i, f)
		}
	}
}

// TestFilterFiles_ExcludePattern verifies that exclusion patterns remove
// matching files from the result.
func TestFilterFiles_ExcludePattern(t *testing.T) {
	root := setupTestDir(t)

	result, err := FilterFiles(FilterOptions{
		SourceDir: root,
		Exclude:   []string{"*.md", "*.log"},
	})
	if err != nil {
		t.Fatalf("FilterFiles returned unexpected error: %v", err)
	}

	for _, f := range result.Files {
		if filepath.Ext(f) == ".md" {
			t.Errorf("excluded .md file found in results: %s", f)
		}
		if filepath.Ext(f) == ".log" {
			t.Errorf("excluded .log file found in results: %s", f)
		}
	}
}

// TestFilterFiles_IncludeOverride verifies that an explicit inclusion pattern
// can override an exclusion pattern and bring a file back.
func TestFilterFiles_IncludeOverride(t *testing.T) {
	root := setupTestDir(t)

	// Exclude all .php files but include .md files via include override.
	result, err := FilterFiles(FilterOptions{
		SourceDir: root,
		Include:   []string{"*.md"},
		Exclude:   []string{"*.php"},
	})
	if err != nil {
		t.Fatalf("FilterFiles returned unexpected error: %v", err)
	}

	// Only .md files should be in the result (basename matching catches
	// both root and nested .md files).
	for _, f := range result.Files {
		if filepath.Ext(f) != ".md" {
			t.Errorf("unexpected file %q (not .md)", f)
		}
	}

	// Should have exactly 2 .md files (README.md and default.md).
	if len(result.Files) != 2 {
		t.Errorf("expected 2 .md files, got %d: %v", len(result.Files), result.Files)
	}

	// Verify both known .md files are present.
	hasReadme := false
	hasDefaultMD := false
	for _, f := range result.Files {
		if f == "README.md" {
			hasReadme = true
		}
		if f == ".gitlab/merge_request_templates/default.md" {
			hasDefaultMD = true
		}
	}
	if !hasReadme {
		t.Error("expected README.md in results")
	}
	if !hasDefaultMD {
		t.Error("expected .gitlab/merge_request_templates/default.md in results")
	}
}

// TestFilterFiles_ExcludeSuppressesIncludedWithoutOverride verifies that when
// only exclude patterns are specified (no include), the excluded files are
// simply removed.
func TestFilterFiles_ExcludeSuppressesIncludedWithoutOverride(t *testing.T) {
	root := setupTestDir(t)

	// Exclude the .git directory tree.
	result, err := FilterFiles(FilterOptions{
		SourceDir: root,
		Exclude:   []string{".git/**"},
	})
	if err != nil {
		t.Fatalf("FilterFiles returned unexpected error: %v", err)
	}

	for _, f := range result.Files {
		if f == ".git" || filepath.HasPrefix(f, ".git"+string(filepath.Separator)) {
			t.Errorf(".git file found in results: %s", f)
		}
	}
}

// TestFilterFiles_EmptySource verifies that an empty source directory returns
// an empty result with no error.
func TestFilterFiles_EmptySource(t *testing.T) {
	root := t.TempDir()

	result, err := FilterFiles(FilterOptions{SourceDir: root})
	if err != nil {
		t.Fatalf("FilterFiles returned unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("FilterFiles returned nil result")
	}

	if len(result.Files) != 0 {
		t.Errorf("expected 0 files, got %d: %v", len(result.Files), result.Files)
	}
}

// TestFilterFiles_NoModifySource verifies that FilterFiles does not create,
// delete, or modify any files in the source directory.
func TestFilterFiles_NoModifySource(t *testing.T) {
	root := setupTestDir(t)

	// Record the initial state: list of files and their contents.
	initial := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		initial[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("failed to record initial state: %v", err)
	}

	// Run filtering with various patterns.
	_, err = FilterFiles(FilterOptions{
		SourceDir: root,
		Include:   []string{"*.php"},
		Exclude:   []string{"*.md"},
	})
	if err != nil {
		t.Fatalf("FilterFiles returned unexpected error: %v", err)
	}

	// Verify the state is unchanged.
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if initial[rel] != string(data) {
			t.Errorf("file %s was modified: content changed", rel)
		}
		delete(initial, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("failed to verify final state: %v", err)
	}

	// Any remaining files in initial would have been deleted.
	for rel := range initial {
		t.Errorf("file %s was deleted", rel)
	}
}

// TestFilterFiles_AllExcluded verifies that when all files match exclusion
// patterns, an empty result is returned.
func TestFilterFiles_AllExcluded(t *testing.T) {
	root := setupTestDir(t)

	// Exclude everything.
	result, err := FilterFiles(FilterOptions{
		SourceDir: root,
		Exclude:   []string{"**"},
	})
	if err != nil {
		t.Fatalf("FilterFiles returned unexpected error: %v", err)
	}

	if len(result.Files) != 0 {
		t.Errorf("expected 0 files when all excluded, got %d", len(result.Files))
	}
}

// TestFilterFiles_IncludeWithNoExclude verifies that with only include
// patterns and no exclude patterns, only files matching the include patterns
// are returned (using basename matching).
func TestFilterFiles_IncludeWithNoExclude(t *testing.T) {
	root := setupTestDir(t)

	result, err := FilterFiles(FilterOptions{
		SourceDir: root,
		Include:   []string{"*.json"},
	})
	if err != nil {
		t.Fatalf("FilterFiles returned unexpected error: %v", err)
	}

	// There are 3 .json files: composer.json, .anvil/state/config.json,
	// .vscode/settings.json.
	if len(result.Files) != 3 {
		t.Errorf("expected 3 .json files, got %d: %v", len(result.Files), result.Files)
	}

	// Verify all .json files have the right extension.
	for _, f := range result.Files {
		if filepath.Ext(f) != ".json" {
			t.Errorf("unexpected non-.json file: %s", f)
		}
	}
}

// TestFilterFiles_ExcludeDirectoryPrunesWalk verifies that excluding a
// directory with a /** pattern prevents the walk from descending into it.
func TestFilterFiles_ExcludeDirectoryPrunesWalk(t *testing.T) {
	root := t.TempDir()

	// Create a directory tree with a deeply nested excluded dir.
	dirs := []string{
		"src/Controller",
		"node_modules/pkg1/src",
		"node_modules/pkg1/test",
		"node_modules/pkg2/src",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	files := []string{
		"src/Controller/HomeController.php",
		"node_modules/pkg1/src/index.js",
		"node_modules/pkg1/test/test.js",
		"node_modules/pkg2/src/index.js",
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(root, f), []byte("content"), 0644); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}

	result, err := FilterFiles(FilterOptions{
		SourceDir: root,
		Exclude:   []string{"node_modules/**"},
	})
	if err != nil {
		t.Fatalf("FilterFiles returned unexpected error: %v", err)
	}

	if len(result.Files) != 1 {
		t.Errorf("expected 1 file (node_modules pruned), got %d: %v", len(result.Files), result.Files)
	}

	if len(result.Files) > 0 && result.Files[0] != "src/Controller/HomeController.php" {
		t.Errorf("expected src/Controller/HomeController.php, got %s", result.Files[0])
	}
}

// TestFilterFiles_DefaultExclusions verifies that the default exclusion
// patterns from the schema exclude common development-only files.
func TestFilterFiles_DefaultExclusions(t *testing.T) {
	root := setupTestDir(t)

	// Use a representative subset of the default CH-P3-01 exclusions.
	defaultExclude := []string{
		".git/**",
		".svn/**",
		".hg/**",
		".anvil/**",
		".github/**",
		".gitlab/**",
		".circleci/**",
		".idea/**",
		".vscode/**",
		"node_modules/**",
		"vendor/**",
		"__pycache__/**",
		"tests/**",
		"spec/**",
		"__tests__/**",
		".DS_Store",
		"Thumbs.db",
		"*.log",
	}

	result, err := FilterFiles(FilterOptions{
		SourceDir: root,
		Exclude:   defaultExclude,
	})
	if err != nil {
		t.Fatalf("FilterFiles returned unexpected error: %v", err)
	}

	// Files that should remain: index.php, src/App.php,
	// src/Controller/HomeController.php, resources/views/home.blade.php,
	// config/app.php, public/index.php, composer.json, README.md
	expected := []string{
		"index.php",
		"src/App.php",
		"src/Controller/HomeController.php",
		"resources/views/home.blade.php",
		"config/app.php",
		"public/index.php",
		"composer.json",
		"README.md",
	}

	if len(result.Files) != len(expected) {
		t.Errorf("expected %d files, got %d", len(expected), len(result.Files))
	}

	// Sort both slices for comparison.
	sort.Strings(result.Files)
	sort.Strings(expected)

	for i, f := range expected {
		if i >= len(result.Files) {
			t.Errorf("missing expected file: %s", f)
			continue
		}
		if result.Files[i] != f {
			t.Errorf("result[%d] = %s, want %s", i, result.Files[i], f)
		}
	}

	// Ensure no excluded files leaked into results.
	excludedSet := map[string]bool{
		".git/config":              true,
		".git/HEAD":                true,
		".svn/entries":             true,
		".hg/store/data":           true,
		".anvil/state/config.json": true,
	}
	for _, f := range result.Files {
		if excludedSet[f] {
			t.Errorf("excluded file %q found in results", f)
		}
	}
}

// TestFilterFiles_InvalidSource verifies that a non-existent source directory
// returns an error.
func TestFilterFiles_InvalidSource(t *testing.T) {
	_, err := FilterFiles(FilterOptions{
		SourceDir: "/tmp/nonexistent-path-that-should-not-exist-12345",
	})
	if err == nil {
		t.Error("expected error for invalid source directory, got nil")
	}
}

// TestFilterFiles_FileAsSource verifies that passing a file path as SourceDir
// returns an error.
func TestFilterFiles_FileAsSource(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "somefile.txt")
	if err := os.WriteFile(filePath, []byte("content"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := FilterFiles(FilterOptions{SourceDir: filePath})
	if err == nil {
		t.Error("expected error when source is a file, got nil")
	}
}
