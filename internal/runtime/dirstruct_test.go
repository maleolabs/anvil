package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Provisioner Tests — TS-P5-05
// ---------------------------------------------------------------------------

// TestProvisioner_CreatesAllDirectories verifies that Provision creates all 6
// directories defined in the RuntimeConfig.
//
// Reference: TS-P5-05 AC-1
func TestProvisioner_CreatesAllDirectories(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	p := NewDirProvisioner(cfg)
	if err := p.Provision(context.Background()); err != nil {
		t.Fatalf("Provision() returned unexpected error: %v", err)
	}

	for _, d := range cfg.AllDirs() {
		info, err := os.Stat(d)
		if err != nil {
			t.Errorf("expected directory %q to exist: %v", d, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%q exists but is not a directory", d)
		}
	}
}

// TestProvisioner_Idempotent verifies that calling Provision multiple times
// succeeds (idempotency).
//
// Reference: TS-P5-05 AC-2
func TestProvisioner_Idempotent(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	p := NewDirProvisioner(cfg)

	// First call.
	if err := p.Provision(context.Background()); err != nil {
		t.Fatalf("first Provision() returned unexpected error: %v", err)
	}

	// Second call — must succeed for idempotency.
	if err := p.Provision(context.Background()); err != nil {
		t.Fatalf("second Provision() should succeed (idempotent): %v", err)
	}
}

// TestProvisioner_EnsureDirectoriesExist_Success verifies that
// EnsureDirectoriesExist returns nil when all directories exist.
//
// Reference: TS-P5-05 AC-3
func TestProvisioner_EnsureDirectoriesExist_Success(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	p := NewDirProvisioner(cfg)
	if err := p.Provision(context.Background()); err != nil {
		t.Fatalf("Provision() returned unexpected error: %v", err)
	}

	if err := p.EnsureDirectoriesExist(); err != nil {
		t.Errorf("EnsureDirectoriesExist() should return nil when all dirs exist: %v", err)
	}
}

// TestProvisioner_EnsureDirectoriesExist_Missing verifies that
// EnsureDirectoriesExist returns an error when a directory is missing.
//
// Reference: TS-P5-05 AC-3
func TestProvisioner_EnsureDirectoriesExist_Missing(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	p := NewDirProvisioner(cfg)
	// Intentionally skip Provision — directories do not exist.

	if err := p.EnsureDirectoriesExist(); err == nil {
		t.Error("EnsureDirectoriesExist() should return an error when directories are missing")
	}
}

// TestProvisioner_CustomConfig verifies that Provision works correctly with a
// non-default RuntimeConfig using custom directory names.
func TestProvisioner_CustomConfig(t *testing.T) {
	dir := t.TempDir()

	cfg := RuntimeConfig{
		InstallRoot:      dir,
		ReleasesDir:      "custom-releases",
		ActiveSymlink:    "custom-current",
		SharedConfigDir:  "custom-config",
		SharedStorageDir: "custom-storage",
		LogsDir:          "custom-logs",
		TempDir:          "custom-tmp",
		EnvironmentName:  "staging",
		DirNamingPattern: "identity",
	}

	p := NewDirProvisioner(cfg)
	if err := p.Provision(context.Background()); err != nil {
		t.Fatalf("Provision() with custom config returned unexpected error: %v", err)
	}

	for _, d := range cfg.AllDirs() {
		info, err := os.Stat(d)
		if err != nil {
			t.Errorf("expected custom directory %q to exist: %v", d, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%q exists but is not a directory", d)
		}
	}
}

// ---------------------------------------------------------------------------
// Release Directory Tests — TS-P5-07
// ---------------------------------------------------------------------------

// TestCreateReleaseDir_CreatesDirectory verifies that CreateReleaseDir
// creates a directory at the expected path.
//
// Reference: TS-P5-07 AC-1
func TestCreateReleaseDir_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	releasesPath := filepath.Join(dir, "releases")
	identity := "abc123"

	created, err := CreateReleaseDir(releasesPath, identity)
	if err != nil {
		t.Fatalf("CreateReleaseDir() returned unexpected error: %v", err)
	}

	expected := ReleaseDirPath(releasesPath, identity)
	if created != expected {
		t.Errorf("CreateReleaseDir() = %q, want %q", created, expected)
	}

	info, err := os.Stat(created)
	if err != nil {
		t.Fatalf("expected directory %q to exist: %v", created, err)
	}
	if !info.IsDir() {
		t.Errorf("%q exists but is not a directory", created)
	}
}

// TestCreateReleaseDir_ReturnsCorrectPath verifies that CreateReleaseDir
// returns the correct full path to the created directory.
//
// Reference: TS-P5-07 AC-1
func TestCreateReleaseDir_ReturnsCorrectPath(t *testing.T) {
	dir := t.TempDir()
	releasesPath := filepath.Join(dir, "releases")
	identity := "release-42"

	created, err := CreateReleaseDir(releasesPath, identity)
	if err != nil {
		t.Fatalf("CreateReleaseDir() returned unexpected error: %v", err)
	}

	want := filepath.Join(releasesPath, "rel-"+identity)
	if created != want {
		t.Errorf("CreateReleaseDir() = %q, want %q", created, want)
	}
}

// TestCreateReleaseDir_ErrorWhenExists verifies that CreateReleaseDir returns
// an error when the release directory already exists.
//
// Reference: TS-P5-07 AC-2
func TestCreateReleaseDir_ErrorWhenExists(t *testing.T) {
	dir := t.TempDir()
	releasesPath := filepath.Join(dir, "releases")
	identity := "abc123"

	// First call — should succeed.
	_, err := CreateReleaseDir(releasesPath, identity)
	if err != nil {
		t.Fatalf("first CreateReleaseDir() returned unexpected error: %v", err)
	}

	// Second call — should fail because directory already exists.
	_, err = CreateReleaseDir(releasesPath, identity)
	if err == nil {
		t.Fatal("CreateReleaseDir() should return an error when directory already exists")
	}
}

// TestReleaseDirPath verifies that ReleaseDirPath returns the correct
// formatted path without creating the directory.
//
// Reference: TS-P5-07
func TestReleaseDirPath(t *testing.T) {
	got := ReleaseDirPath("/opt/anvil/releases", "abc123")
	want := "/opt/anvil/releases/rel-abc123"
	if got != want {
		t.Errorf("ReleaseDirPath() = %q, want %q", got, want)
	}
}

// TestReleaseIdentityFromPath_Valid verifies that ReleaseIdentityFromPath
// extracts the identity from valid release directory paths.
//
// Reference: TS-P5-07
func TestReleaseIdentityFromPath_Valid(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/opt/anvil/releases/rel-abc123/", "abc123"},
		{"/opt/anvil/releases/rel-my-release", "my-release"},
		{"rel-some-id", "some-id"},
		{"/tmp/releases/rel-42/", "42"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got, err := ReleaseIdentityFromPath(tt.path)
			if err != nil {
				t.Errorf("ReleaseIdentityFromPath(%q) returned error: %v", tt.path, err)
				return
			}
			if got != tt.expected {
				t.Errorf("ReleaseIdentityFromPath(%q) = %q, want %q", tt.path, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// RemoveReleaseDir Tests — ST-P5-04
// ---------------------------------------------------------------------------

// TestRemoveReleaseDir_RemovesDirectory verifies that RemoveReleaseDir
// removes the release directory and returns its size.
//
// Reference: ST-P5-04 AC-1
func TestRemoveReleaseDir_RemovesDirectory(t *testing.T) {
	dir := t.TempDir()
	releasesPath := filepath.Join(dir, "releases")
	identity := "abc123"

	// Create a release directory with some files.
	created, err := CreateReleaseDir(releasesPath, identity)
	if err != nil {
		t.Fatalf("CreateReleaseDir() returned unexpected error: %v", err)
	}

	// Add a file to the release directory so we can verify size reporting.
	if err := os.WriteFile(filepath.Join(created, "index.php"), []byte("<?php\n"), 0644); err != nil {
		t.Fatalf("write file in release dir: %v", err)
	}

	// Remove the release directory.
	size, err := RemoveReleaseDir(releasesPath, identity)
	if err != nil {
		t.Fatalf("RemoveReleaseDir() returned unexpected error: %v", err)
	}

	// Verify the directory no longer exists.
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Errorf("expected directory %q to be removed, got Stat error: %v", created, err)
	}

	// Verify size is greater than zero (the index.php file was counted).
	if size <= 0 {
		t.Errorf("expected positive size, got %d", size)
	}
}

// TestRemoveReleaseDir_OtherDirectoriesUnaffected verifies that removing one
// release directory does not affect other release directories.
//
// Reference: ST-P5-04 AC-2
func TestRemoveReleaseDir_OtherDirectoriesUnaffected(t *testing.T) {
	dir := t.TempDir()
	releasesPath := filepath.Join(dir, "releases")
	identity1 := "release-001"
	identity2 := "release-002"

	// Create two release directories.
	dir1, err := CreateReleaseDir(releasesPath, identity1)
	if err != nil {
		t.Fatalf("CreateReleaseDir(%q) returned error: %v", identity1, err)
	}

	dir2, err := CreateReleaseDir(releasesPath, identity2)
	if err != nil {
		t.Fatalf("CreateReleaseDir(%q) returned error: %v", identity2, err)
	}

	// Remove the first directory.
	_, err = RemoveReleaseDir(releasesPath, identity1)
	if err != nil {
		t.Fatalf("RemoveReleaseDir() returned unexpected error: %v", err)
	}

	// Verify first directory is removed.
	if _, err := os.Stat(dir1); !os.IsNotExist(err) {
		t.Errorf("expected directory %q to be removed", dir1)
	}

	// Verify second directory still exists.
	info, err := os.Stat(dir2)
	if err != nil {
		t.Errorf("expected directory %q to still exist: %v", dir2, err)
	} else if !info.IsDir() {
		t.Errorf("%q exists but is not a directory", dir2)
	}
}

// TestRemoveReleaseDir_NonexistentRelease verifies that removing a
// nonexistent release directory returns a clear error.
//
// Reference: ST-P5-04 AC-6
func TestRemoveReleaseDir_NonexistentRelease(t *testing.T) {
	dir := t.TempDir()
	releasesPath := filepath.Join(dir, "releases")

	_, err := RemoveReleaseDir(releasesPath, "nonexistent-rel")
	if err == nil {
		t.Fatal("RemoveReleaseDir() should return an error for nonexistent release")
	}
	if !containsStr(err.Error(), "not found") {
		t.Errorf("expected error message containing 'not found', got: %v", err)
	}
}

// TestRemoveReleaseDir_IdempotentOnSecondAttempt verifies that after a
// successful removal, a second attempt returns an appropriate error.
func TestRemoveReleaseDir_IdempotentOnSecondAttempt(t *testing.T) {
	dir := t.TempDir()
	releasesPath := filepath.Join(dir, "releases")
	identity := "abc123"

	_, err := CreateReleaseDir(releasesPath, identity)
	if err != nil {
		t.Fatalf("CreateReleaseDir() returned unexpected error: %v", err)
	}

	// First removal — should succeed.
	_, err = RemoveReleaseDir(releasesPath, identity)
	if err != nil {
		t.Fatalf("first RemoveReleaseDir() returned unexpected error: %v", err)
	}

	// Second removal — should fail because directory no longer exists.
	_, err = RemoveReleaseDir(releasesPath, identity)
	if err == nil {
		t.Fatal("second RemoveReleaseDir() should return an error")
	}
}

// TestRemoveReleaseDir_ErrorSentinelValues verifies that the sentinel error
// values for Active Release and rollback candidate are defined.
//
// Reference: ST-P5-04
func TestRemoveReleaseDir_ErrorSentinelValues(t *testing.T) {
	if ErrActiveReleaseRemoval == nil {
		t.Error("ErrActiveReleaseRemoval should be defined")
	}
	if ErrRollbackCandidateRemoval == nil {
		t.Error("ErrRollbackCandidateRemoval should be defined")
	}
	if !containsStr(ErrActiveReleaseRemoval.Error(), "Active Release") {
		t.Errorf("ErrActiveReleaseRemoval message should mention 'Active Release', got: %v", ErrActiveReleaseRemoval)
	}
	if !containsStr(ErrRollbackCandidateRemoval.Error(), "rollback candidate") {
		t.Errorf("ErrRollbackCandidateRemoval message should mention 'rollback candidate', got: %v", ErrRollbackCandidateRemoval)
	}
}

// TestRemoveReleaseDir_ReleasesParentPreserved verifies that removing a
// release directory does not remove the parent releases directory.
func TestRemoveReleaseDir_ReleasesParentPreserved(t *testing.T) {
	dir := t.TempDir()
	releasesPath := filepath.Join(dir, "releases")
	identity := "abc123"

	_, err := CreateReleaseDir(releasesPath, identity)
	if err != nil {
		t.Fatalf("CreateReleaseDir() returned unexpected error: %v", err)
	}

	// Remove the release directory.
	_, err = RemoveReleaseDir(releasesPath, identity)
	if err != nil {
		t.Fatalf("RemoveReleaseDir() returned unexpected error: %v", err)
	}

	// Verify the parent releases directory still exists.
	info, err := os.Stat(releasesPath)
	if err != nil {
		t.Errorf("expected parent releases directory %q to exist: %v", releasesPath, err)
	} else if !info.IsDir() {
		t.Errorf("%q exists but is not a directory", releasesPath)
	}
}

// containsStr is a helper to check if a string contains a substring.
func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestReleaseIdentityFromPath_Invalid verifies that ReleaseIdentityFromPath
// returns an error for paths that do not match the rel-<identity> pattern.
//
// Reference: TS-P5-07
func TestReleaseIdentityFromPath_Invalid(t *testing.T) {
	tests := []string{
		"/opt/anvil/releases/",
		"/opt/anvil/releases/abc123",
		"rel-",
		"random-path",
		"",
	}

	for _, tt := range tests {
		t.Run(filepath.Base(tt), func(t *testing.T) {
			_, err := ReleaseIdentityFromPath(tt)
			if err == nil {
				t.Errorf("ReleaseIdentityFromPath(%q) should have returned an error", tt)
			}
		})
	}
}
