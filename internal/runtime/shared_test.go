package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSharedResourceManager_PathAccessors verifies that all path accessors
// return correct paths given a RuntimeConfig.
//
// Reference: TS-P5-09
func TestSharedResourceManager_PathAccessors(t *testing.T) {
	cfg := DefaultRuntimeConfig()
	m := NewSharedResourceManager(cfg)

	tests := []struct {
		name string
		got  string
		want string
	}{
		{
			name: "SharedConfigDirPath",
			got:  m.SharedConfigDirPath(),
			want: cfg.SharedConfigDirPath(),
		},
		{
			name: "SharedStorageDirPath",
			got:  m.SharedStorageDirPath(),
			want: cfg.SharedStorageDirPath(),
		},
		{
			name: "SharedLogsDirPath",
			got:  m.SharedLogsDirPath(),
			want: cfg.LogsDirPath(),
		},
		{
			name: "SharedTempDirPath",
			got:  m.SharedTempDirPath(),
			want: cfg.TempDirPath(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %q, want %q", tt.got, tt.want)
			}
		})
	}
}

// TestSharedResourceManager_AllSharedDirPaths verifies that AllSharedDirPaths
// returns exactly 4 shared directories and they match the config values.
//
// Reference: TS-P5-09
func TestSharedResourceManager_AllSharedDirPaths(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	m := NewSharedResourceManager(cfg)
	paths := m.AllSharedDirPaths()

	if len(paths) != 4 {
		t.Fatalf("AllSharedDirPaths() returned %d entries, want 4", len(paths))
	}

	expected := []string{
		cfg.SharedConfigDirPath(),
		cfg.SharedStorageDirPath(),
		cfg.LogsDirPath(),
		cfg.TempDirPath(),
	}

	for i, p := range paths {
		if p != expected[i] {
			t.Errorf("AllSharedDirPaths()[%d] = %q, want %q", i, p, expected[i])
		}
	}
}

// TestSharedResourceManager_IsSharedResource verifies that shared resource
// paths return true, while release paths, InstallRoot, and unrelated paths
// return false.
//
// Reference: TS-P5-09
func TestSharedResourceManager_IsSharedResource(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	m := NewSharedResourceManager(cfg)

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "shared config directory itself",
			path:     cfg.SharedConfigDirPath(),
			expected: true,
		},
		{
			name:     "file inside shared config",
			path:     filepath.Join(cfg.SharedConfigDirPath(), "app.yaml"),
			expected: true,
		},
		{
			name:     "subdirectory inside shared config",
			path:     filepath.Join(cfg.SharedConfigDirPath(), "sub", "nested"),
			expected: true,
		},
		{
			name:     "shared storage directory itself",
			path:     cfg.SharedStorageDirPath(),
			expected: true,
		},
		{
			name:     "file inside shared storage",
			path:     filepath.Join(cfg.SharedStorageDirPath(), "data.db"),
			expected: true,
		},
		{
			name:     "shared logs directory itself",
			path:     cfg.LogsDirPath(),
			expected: true,
		},
		{
			name:     "file inside shared logs",
			path:     filepath.Join(cfg.LogsDirPath(), "runtime.log"),
			expected: true,
		},
		{
			name:     "temp directory itself",
			path:     cfg.TempDirPath(),
			expected: true,
		},
		{
			name:     "file inside temp",
			path:     filepath.Join(cfg.TempDirPath(), "extract.tar.gz"),
			expected: true,
		},
		{
			name:     "releases directory (not shared)",
			path:     cfg.ReleasesDirPath(),
			expected: false,
		},
		{
			name:     "release subdirectory (not shared)",
			path:     filepath.Join(cfg.ReleasesDirPath(), "rel-abc123"),
			expected: false,
		},
		{
			name:     "install root (not shared)",
			path:     cfg.InstallRoot,
			expected: false,
		},
		{
			name:     "unrelated path",
			path:     "/tmp/some-other-path",
			expected: false,
		},
		{
			name:     "empty string",
			path:     "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.IsSharedResource(tt.path)
			if got != tt.expected {
				t.Errorf("IsSharedResource(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}

// TestSharedResourceManager_ValidateIsolation_Passes verifies that
// ValidateIsolation returns nil when shared directories are NOT nested
// under the releases directory.
//
// Reference: TS-P5-09 AC-3
func TestSharedResourceManager_ValidateIsolation_Passes(t *testing.T) {
	dir := t.TempDir()

	// Shared dirs are siblings of releases dir, not nested within it.
	cfg := DefaultRuntimeConfig()
	cfg.InstallRoot = dir
	// shared/* and releases/ are siblings under InstallRoot.

	m := NewSharedResourceManager(cfg)

	if err := m.ValidateIsolation(); err != nil {
		t.Errorf("ValidateIsolation() should pass for properly isolated directories: %v", err)
	}
}

// TestSharedResourceManager_ValidateIsolation_Fails verifies that
// ValidateIsolation returns an error when a shared directory is nested
// under the releases directory.
//
// Reference: TS-P5-09 AC-3
func TestSharedResourceManager_ValidateIsolation_Fails(t *testing.T) {
	dir := t.TempDir()

	// Configure a shared directory to be nested inside releases.
	cfg := DefaultRuntimeConfig()
	cfg.InstallRoot = dir
	cfg.SharedConfigDir = filepath.Join("releases", "shared-config") // deliberate conflict

	m := NewSharedResourceManager(cfg)

	err := m.ValidateIsolation()
	if err == nil {
		t.Fatal("ValidateIsolation() should return an error when shared dir is inside releases")
	}

	// Verify the error mentions the conflicting paths.
	errMsg := err.Error()
	if !strings.Contains(errMsg, "shared resource directory") {
		t.Errorf("error should mention 'shared resource directory', got: %s", errMsg)
	}
}

// TestSharedResourceManager_CleanTemp verifies that CleanTemp removes all
// contents of the temp directory while preserving the temp directory itself.
// It also verifies that CleanTemp is a no-op when the temp directory does
// not exist.
//
// Reference: TS-P5-09
func TestSharedResourceManager_CleanTemp(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	m := NewSharedResourceManager(cfg)

	// Create the temp directory with some content.
	tmpDir := cfg.TempDirPath()
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	// Create a file inside temp.
	filePath := filepath.Join(tmpDir, "test-file.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Create a subdirectory with a file inside temp.
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdirectory: %v", err)
	}
	subFile := filepath.Join(subDir, "nested.txt")
	if err := os.WriteFile(subFile, []byte("nested"), 0644); err != nil {
		t.Fatalf("failed to create nested file: %v", err)
	}

	// Verify content exists before cleaning.
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatal("test file should exist before CleanTemp")
	}
	if _, err := os.Stat(subDir); os.IsNotExist(err) {
		t.Fatal("subdirectory should exist before CleanTemp")
	}

	// Clean temp directory.
	if err := m.CleanTemp(); err != nil {
		t.Fatalf("CleanTemp() returned unexpected error: %v", err)
	}

	// Verify temp directory still exists.
	if _, err := os.Stat(tmpDir); os.IsNotExist(err) {
		t.Error("temp directory should still exist after CleanTemp")
	}

	// Verify contents are removed.
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("test file should be removed after CleanTemp")
	}
	if _, err := os.Stat(subDir); !os.IsNotExist(err) {
		t.Error("subdirectory should be removed after CleanTemp")
	}

	// Verify temp directory is empty.
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("failed to read temp dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("temp directory should be empty after CleanTemp, got %d entries", len(entries))
	}
}

// TestSharedResourceManager_CleanTemp_NoDir verifies that CleanTemp returns
// nil when the temp directory does not exist (nothing to clean).
//
// Reference: TS-P5-09
func TestSharedResourceManager_CleanTemp_NoDir(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	cfg.InstallRoot = dir
	// Do NOT create the temp directory.

	m := NewSharedResourceManager(cfg)

	if err := m.CleanTemp(); err != nil {
		t.Errorf("CleanTemp() should return nil when temp dir does not exist: %v", err)
	}
}

// TestSharedResourceManager_EnsureDirectoriesExist verifies that
// EnsureDirectoriesExist returns nil when all shared directories exist
// (created via DirProvisioner), and returns an error when they do not.
//
// Reference: TS-P5-09
func TestSharedResourceManager_EnsureDirectoriesExist(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	m := NewSharedResourceManager(cfg)

	// Before provisioning, directories don't exist.
	if err := m.EnsureDirectoriesExist(); err == nil {
		t.Error("EnsureDirectoriesExist() should return error before provisioning")
	}

	// Provision all directories.
	p := NewDirProvisioner(cfg)
	if err := p.Provision(nil); err != nil {
		t.Fatalf("Provision() returned unexpected error: %v", err)
	}

	// After provisioning, all shared directories should exist.
	if err := m.EnsureDirectoriesExist(); err != nil {
		t.Errorf("EnsureDirectoriesExist() should return nil after provisioning: %v", err)
	}
}
