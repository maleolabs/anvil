// Package project defines the Anvil project directory structure.
//
// Reference: TS-001-008
package project

import (
	"path/filepath"
	"testing"
)

// TestNewStructure_ResolvesAllPaths verifies that NewStructure resolves all
// project paths relative to the given root directory.
func TestNewStructure_ResolvesAllPaths(t *testing.T) {
	root := "/tmp/anvil-test-project"
	s := NewStructure(root)

	if s.Root != root {
		t.Errorf("Root = %q, want %q", s.Root, root)
	}
	if s.ConfigFile != filepath.Join(root, ConfigFileName) {
		t.Errorf("ConfigFile = %q, want %q", s.ConfigFile, filepath.Join(root, ConfigFileName))
	}
	if s.AnvilDir != filepath.Join(root, AnvilDirName) {
		t.Errorf("AnvilDir = %q, want %q", s.AnvilDir, filepath.Join(root, AnvilDirName))
	}
	if s.StateDir != filepath.Join(root, AnvilDirName, StateDirName) {
		t.Errorf("StateDir = %q, want %q", s.StateDir, filepath.Join(root, AnvilDirName, StateDirName))
	}
	if s.ReleasesDir != filepath.Join(root, AnvilDirName, ReleasesDirName) {
		t.Errorf("ReleasesDir = %q, want %q", s.ReleasesDir, filepath.Join(root, AnvilDirName, ReleasesDirName))
	}
	if s.SharedDir != filepath.Join(root, AnvilDirName, SharedDirName) {
		t.Errorf("SharedDir = %q, want %q", s.SharedDir, filepath.Join(root, AnvilDirName, SharedDirName))
	}
	if s.SharedConfigDir != filepath.Join(root, AnvilDirName, SharedDirName, SharedConfigDirName) {
		t.Errorf("SharedConfigDir = %q, want %q", s.SharedConfigDir, filepath.Join(root, AnvilDirName, SharedDirName, SharedConfigDirName))
	}
	if s.SharedStorageDir != filepath.Join(root, AnvilDirName, SharedDirName, SharedStorageDirName) {
		t.Errorf("SharedStorageDir = %q, want %q", s.SharedStorageDir, filepath.Join(root, AnvilDirName, SharedDirName, SharedStorageDirName))
	}
}

// TestNewStructure_WithRootPreservesInput verifies that NewStructure stores
// the root as provided (cleaned by filepath.Join in field construction).
func TestNewStructure_WithRootPreservesInput(t *testing.T) {
	tests := []struct {
		name string
		root string
	}{
		{"simple path", "/home/user/my-project"},
		{"root filesystem", "/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStructure(tt.root)
			expectedConfig := filepath.Join(tt.root, ConfigFileName)
			if s.ConfigFile != expectedConfig {
				t.Errorf("ConfigFile = %q, want %q", s.ConfigFile, expectedConfig)
			}
		})
	}
}

// TestNewStructure_FieldsAreNotEmpty verifies that no field in Structure is
// left empty after construction.
func TestNewStructure_FieldsAreNotEmpty(t *testing.T) {
	s := NewStructure("/tmp/test")

	if s.Root == "" {
		t.Error("Root must not be empty")
	}
	if s.ConfigFile == "" {
		t.Error("ConfigFile must not be empty")
	}
	if s.AnvilDir == "" {
		t.Error("AnvilDir must not be empty")
	}
	if s.StateDir == "" {
		t.Error("StateDir must not be empty")
	}
	if s.ReleasesDir == "" {
		t.Error("ReleasesDir must not be empty")
	}
	if s.SharedDir == "" {
		t.Error("SharedDir must not be empty")
	}
	if s.SharedConfigDir == "" {
		t.Error("SharedConfigDir must not be empty")
	}
	if s.SharedStorageDir == "" {
		t.Error("SharedStorageDir must not be empty")
	}
}

// TestDirs_ReturnsAllDirectories verifies that Dirs() returns all expected
// directories and no unexpected ones.
func TestDirs_ReturnsAllDirectories(t *testing.T) {
	root := "/tmp/anvil-project"
	s := NewStructure(root)
	dirs := s.Dirs()

	expected := []string{
		s.AnvilDir,
		s.StateDir,
		s.ReleasesDir,
		s.SharedDir,
		s.SharedConfigDir,
		s.SharedStorageDir,
	}

	if len(dirs) != len(expected) {
		t.Errorf("Dirs() returned %d entries, want %d", len(dirs), len(expected))
	}

	// Verify every expected directory is present.
	for i, expectedDir := range expected {
		if dirs[i] != expectedDir {
			t.Errorf("Dirs()[%d] = %q, want %q", i, dirs[i], expectedDir)
		}
	}
}

// TestDirs_OrderedParentToChild verifies that directories are ordered from
// parent to child: every entry is either directly under .anvil/ or under a
// directory that appears earlier in the list.
func TestDirs_OrderedParentToChild(t *testing.T) {
	root := "/tmp/test"
	s := NewStructure(root)
	dirs := s.Dirs()

	for i := 1; i < len(dirs); i++ {
		parent := filepath.Dir(dirs[i])

		// Direct children of .anvil/ are always valid.
		if parent == s.AnvilDir {
			continue
		}

		// Otherwise, the parent must appear earlier in the list.
		found := false
		for j := 0; j < i; j++ {
			if dirs[j] == parent {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"Dirs()[%d] = %q has parent %q, but parent was not found in Dirs()[0..%d]",
				i, dirs[i], parent, i-1,
			)
		}
	}
}

// TestDirs_ConfigFileNotInDirs verifies that the config file is NOT included
// in Dirs() — Dirs() returns only directories, not files.
func TestDirs_ConfigFileNotInDirs(t *testing.T) {
	root := "/tmp/test"
	s := NewStructure(root)
	dirs := s.Dirs()

	for _, d := range dirs {
		if d == s.ConfigFile {
			t.Error("Dirs() must not contain ConfigFile (it is a file, not a directory)")
		}
	}
}

// TestDirs_Unique verifies that Dirs() contains no duplicate entries.
func TestDirs_Unique(t *testing.T) {
	root := "/tmp/test"
	s := NewStructure(root)
	dirs := s.Dirs()

	seen := make(map[string]bool, len(dirs))
	for _, d := range dirs {
		if seen[d] {
			t.Errorf("Dirs() contains duplicate: %q", d)
		}
		seen[d] = true
	}
}
