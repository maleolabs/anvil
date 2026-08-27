// Package project provides tests for the project discovery mechanism.
//
// Reference: TS-P1-05
package project

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// saveCWD saves the current working directory and registers a cleanup
// function to restore it. Tests using os.Chdir must call this at the
// start and must NOT use t.Parallel().
func saveCWD(t *testing.T) string {
	t.Helper()

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("saveCWD: failed to get current working directory: %v", err)
	}

	t.Cleanup(func() {
		if err := os.Chdir(orig); err != nil {
			t.Errorf("saveCWD: failed to restore original working directory %q: %v", orig, err)
		}
	})

	return orig
}

// --- TS-P1-05 Tests: Project Discovery ---

// TestDiscover_AtProjectRoot verifies that discovery finds the project
// when the current working directory is the project root directory that
// contains anvil.yaml.
//
// Acceptance Criteria: TS-P1-05 AC-1
func TestDiscover_AtProjectRoot(t *testing.T) {
	saveCWD(t)

	root := t.TempDir()
	marker := filepath.Join(root, ConfigFileName)

	if err := os.WriteFile(marker, []byte("project: test\n"), 0644); err != nil {
		t.Fatalf("failed to create marker file %q: %v", marker, err)
	}

	if err := os.Chdir(root); err != nil {
		t.Fatalf("failed to change to project root %q: %v", root, err)
	}

	got, err := Discover()
	if err != nil {
		t.Fatalf("Discover() returned unexpected error: %v", err)
	}
	if got != root {
		t.Errorf("Discover() = %q, want %q", got, root)
	}
}

// TestDiscover_FromSubdirectory verifies that discovery finds the project
// when the current working directory is a subdirectory nested below the
// project root.
//
// Acceptance Criteria: TS-P1-05 AC-2
func TestDiscover_FromSubdirectory(t *testing.T) {
	saveCWD(t)

	root := t.TempDir()
	marker := filepath.Join(root, ConfigFileName)

	if err := os.WriteFile(marker, []byte("project: test\n"), 0644); err != nil {
		t.Fatalf("failed to create marker file %q: %v", marker, err)
	}

	subdir := filepath.Join(root, "nested", "deeply")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("failed to create nested subdirectory %q: %v", subdir, err)
	}

	if err := os.Chdir(subdir); err != nil {
		t.Fatalf("failed to change to subdirectory %q: %v", subdir, err)
	}

	got, err := Discover()
	if err != nil {
		t.Fatalf("Discover() returned unexpected error: %v", err)
	}
	if got != root {
		t.Errorf("Discover() = %q, want %q", got, root)
	}
}

// TestDiscover_NoProjectFound verifies that discovery returns
// ErrNoProjectFound when no anvil.yaml exists in the current working
// directory or any parent directory.
//
// Acceptance Criteria: TS-P1-05 AC-3
func TestDiscover_NoProjectFound(t *testing.T) {
	saveCWD(t)

	dir := t.TempDir()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change to temp directory %q: %v", dir, err)
	}

	got, err := Discover()
	if err == nil {
		t.Fatal("Discover() expected ErrNoProjectFound, got nil")
	}
	if !errors.Is(err, ErrNoProjectFound) {
		t.Errorf("Discover() error = %v, want ErrNoProjectFound", err)
	}
	if got != "" {
		t.Errorf("Discover() = %q, want empty string", got)
	}
}

// TestDiscover_StopsAtFilesystemRoot verifies that the discovery
// traversal stops at the filesystem root and does not loop infinitely.
//
// This is implicitly tested by TestDiscover_NoProjectFound — the
// traversal must reach root and stop. This test makes it explicit.
//
// Acceptance Criteria: TS-P1-05 AC-4 (implicit)
func TestDiscover_StopsAtFilesystemRoot(t *testing.T) {
	saveCWD(t)

	dir := t.TempDir()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change to temp directory %q: %v", dir, err)
	}

	// Traverse up to verify we reach the root. We walk up manually
	// and check that no directory in the chain has anvil.yaml.
	current := dir
	for {
		configPath := filepath.Join(current, ConfigFileName)
		_, err := os.Stat(configPath)
		if err == nil {
			t.Fatalf("unexpected marker file found at %q", configPath)
		}

		parent := filepath.Dir(current)
		if parent == current {
			// Reached root without finding the marker — correct.
			break
		}
		current = parent
	}

	// Now run Discover and verify it returns the expected error.
	got, err := Discover()
	if err == nil {
		t.Fatal("Discover() expected ErrNoProjectFound, got nil")
	}
	if !errors.Is(err, ErrNoProjectFound) {
		t.Errorf("Discover() error = %v, want ErrNoProjectFound", err)
	}
	if got != "" {
		t.Errorf("Discover() = %q, want empty string", got)
	}
}

// TestDiscover_ReadOnly verifies that discovery does not create, modify,
// or delete any files or directories in the project root.
//
// Acceptance Criteria: TS-P1-05 AC-5
func TestDiscover_ReadOnly(t *testing.T) {
	saveCWD(t)

	root := t.TempDir()
	marker := filepath.Join(root, ConfigFileName)

	if err := os.WriteFile(marker, []byte("project: test\n"), 0644); err != nil {
		t.Fatalf("failed to create marker file %q: %v", marker, err)
	}

	// Capture directory state before discovery.
	entriesBefore, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("failed to list directory %q before discovery: %v", root, err)
	}

	if err := os.Chdir(root); err != nil {
		t.Fatalf("failed to change to project root %q: %v", root, err)
	}

	if _, err := Discover(); err != nil {
		t.Fatalf("Discover() returned unexpected error: %v", err)
	}

	// Capture directory state after discovery.
	entriesAfter, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("failed to list directory %q after discovery: %v", root, err)
	}

	if len(entriesBefore) != len(entriesAfter) {
		t.Errorf("directory entry count changed: before=%d, after=%d",
			len(entriesBefore), len(entriesAfter))
	}

	// Verify each entry is identical (same name and type).
	for i := range entriesBefore {
		if entriesBefore[i].Name() != entriesAfter[i].Name() {
			t.Errorf("entry %d name changed: before=%q, after=%q",
				i, entriesBefore[i].Name(), entriesAfter[i].Name())
		}
		if entriesBefore[i].IsDir() != entriesAfter[i].IsDir() {
			t.Errorf("entry %d type changed: before.IsDir=%v, after.IsDir=%v",
				i, entriesBefore[i].IsDir(), entriesAfter[i].IsDir())
		}
	}
}

// TestDiscover_Performance verifies that a single discovery call
// completes within 100 milliseconds.
//
// Acceptance Criteria: TS-P1-05 AC-6
func TestDiscover_Performance(t *testing.T) {
	saveCWD(t)

	root := t.TempDir()
	marker := filepath.Join(root, ConfigFileName)

	if err := os.WriteFile(marker, []byte("project: test\n"), 0644); err != nil {
		t.Fatalf("failed to create marker file %q: %v", marker, err)
	}

	if err := os.Chdir(root); err != nil {
		t.Fatalf("failed to change to project root %q: %v", root, err)
	}

	start := time.Now()
	got, err := Discover()
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Discover() returned unexpected error: %v", err)
	}
	if got != root {
		t.Errorf("Discover() = %q, want %q", got, root)
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("Discover() took %v, want <= 100ms", elapsed)
	}
}

// --- ST-P1-06 Tests: Searched Directory Tracking ---

// TestDiscoverSearched_TracksDirectoriesOnSuccess verifies that
// DiscoverSearched returns the searched directories including the project
// root when a project is found.
//
// Acceptance Criteria: ST-P1-06 AC-1 (partial)
func TestDiscoverSearched_TracksDirectoriesOnSuccess(t *testing.T) {
	saveCWD(t)

	root := t.TempDir()
	marker := filepath.Join(root, ConfigFileName)

	if err := os.WriteFile(marker, []byte("project: test\n"), 0644); err != nil {
		t.Fatalf("failed to create marker file %q: %v", marker, err)
	}

	subdir := filepath.Join(root, "nested")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("failed to create subdirectory %q: %v", subdir, err)
	}

	if err := os.Chdir(subdir); err != nil {
		t.Fatalf("failed to change to subdirectory %q: %v", subdir, err)
	}

	gotRoot, searched, err := DiscoverSearched()
	if err != nil {
		t.Fatalf("DiscoverSearched() returned unexpected error: %v", err)
	}
	if gotRoot != root {
		t.Errorf("DiscoverSearched() root = %q, want %q", gotRoot, root)
	}

	if len(searched) == 0 {
		t.Fatal("DiscoverSearched() returned empty searched slice")
	}

	// The last searched directory should be the project root.
	lastIdx := len(searched) - 1
	if searched[lastIdx] != root {
		t.Errorf("last searched directory = %q, want project root %q", searched[lastIdx], root)
	}

	// The first searched directory should be the CWD (subdir).
	if searched[0] != subdir {
		t.Errorf("first searched directory = %q, want CWD %q", searched[0], subdir)
	}
}

// TestDiscoverSearched_TracksDirectoriesOnFailure verifies that
// DiscoverSearched returns all directories searched up to the filesystem
// root when no project is found.
//
// Acceptance Criteria: ST-P1-06 AC-1 (partial)
func TestDiscoverSearched_TracksDirectoriesOnFailure(t *testing.T) {
	saveCWD(t)

	dir := t.TempDir()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change to temp directory %q: %v", dir, err)
	}

	gotRoot, searched, err := DiscoverSearched()
	if err == nil {
		t.Fatal("DiscoverSearched() expected ErrNoProjectFound, got nil")
	}
	if !errors.Is(err, ErrNoProjectFound) {
		t.Errorf("DiscoverSearched() error = %v, want ErrNoProjectFound", err)
	}
	if gotRoot != "" {
		t.Errorf("DiscoverSearched() root = %q, want empty string", gotRoot)
	}

	if len(searched) == 0 {
		t.Fatal("DiscoverSearched() returned empty searched slice on failure")
	}

	// The first searched directory should be the CWD.
	if searched[0] != dir {
		t.Errorf("first searched directory = %q, want CWD %q", searched[0], dir)
	}

	// Each entry should be an absolute path.
	for i, d := range searched {
		if !filepath.IsAbs(d) {
			t.Errorf("searched[%d] = %q is not an absolute path", i, d)
		}
	}

	// The final entry should be the filesystem root.
	lastIdx := len(searched) - 1
	parent := filepath.Dir(searched[lastIdx])
	if parent == searched[lastIdx] {
		// Reached root — correct.
	} else {
		t.Errorf("last searched directory %q should be the filesystem root", searched[lastIdx])
	}
}

// TestDiscoverSearched_ReadOnly verifies that DiscoverSearched does not
// create, modify, or delete any files or directories.
//
// Acceptance Criteria: ST-P1-06 AC-6
func TestDiscoverSearched_ReadOnly(t *testing.T) {
	saveCWD(t)

	root := t.TempDir()
	marker := filepath.Join(root, ConfigFileName)

	if err := os.WriteFile(marker, []byte("project: test\n"), 0644); err != nil {
		t.Fatalf("failed to create marker file %q: %v", marker, err)
	}

	entriesBefore, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("failed to list directory %q before discovery: %v", root, err)
	}

	if err := os.Chdir(root); err != nil {
		t.Fatalf("failed to change to project root %q: %v", root, err)
	}

	if _, _, err := DiscoverSearched(); err != nil {
		t.Fatalf("DiscoverSearched() returned unexpected error: %v", err)
	}

	entriesAfter, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("failed to list directory %q after discovery: %v", root, err)
	}

	if len(entriesBefore) != len(entriesAfter) {
		t.Errorf("directory entry count changed: before=%d, after=%d",
			len(entriesBefore), len(entriesAfter))
	}

	for i := range entriesBefore {
		if entriesBefore[i].Name() != entriesAfter[i].Name() {
			t.Errorf("entry %d name changed: before=%q, after=%q",
				i, entriesBefore[i].Name(), entriesAfter[i].Name())
		}
		if entriesBefore[i].IsDir() != entriesAfter[i].IsDir() {
			t.Errorf("entry %d type changed: before.IsDir=%v, after.IsDir=%v",
				i, entriesBefore[i].IsDir(), entriesAfter[i].IsDir())
		}
	}
}
