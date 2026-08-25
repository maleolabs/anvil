// Package runtime provides models and utilities for managing Anvil Runtime
// instances — their configuration, lifecycle state machines, readiness
// assessment, runtime identity, directory structure provisioning, and shared
// resource management.
//
// Reference: CH-P5-01, TS-P5-01, TS-P5-02, TS-P5-03, TS-P5-05, TS-P5-07,
// TS-P5-09, EPIC-005, ADR-003 §8.5
package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SharedResourceManager manages configuration files, persistent storage,
// logs, and temporary files that exist outside Release directories and
// persist across Release transitions.
//
// Reference: TS-P5-09, EPIC-005, ADR-003 §8.5
type SharedResourceManager struct {
	config RuntimeConfig
}

// NewSharedResourceManager creates a new SharedResourceManager configured with
// the given RuntimeConfig.
//
// Reference: TS-P5-09
func NewSharedResourceManager(cfg RuntimeConfig) *SharedResourceManager {
	return &SharedResourceManager{config: cfg}
}

// SharedConfigDirPath returns the full path to the shared config directory.
//
// Reference: TS-P5-09
func (m *SharedResourceManager) SharedConfigDirPath() string {
	return m.config.SharedConfigDirPath()
}

// SharedStorageDirPath returns the full path to the shared storage directory.
//
// Reference: TS-P5-09
func (m *SharedResourceManager) SharedStorageDirPath() string {
	return m.config.SharedStorageDirPath()
}

// SharedLogsDirPath returns the full path to the shared logs directory.
//
// Reference: TS-P5-09
func (m *SharedResourceManager) SharedLogsDirPath() string {
	return m.config.LogsDirPath()
}

// SharedTempDirPath returns the full path to the temporary directory.
//
// Reference: TS-P5-09
func (m *SharedResourceManager) SharedTempDirPath() string {
	return m.config.TempDirPath()
}

// AllSharedDirPaths returns all shared resource directory paths.
// The returned slice contains shared/config, shared/storage, shared/logs,
// and temp. It does NOT include InstallRoot or ReleasesDir.
//
// Reference: TS-P5-09
func (m *SharedResourceManager) AllSharedDirPaths() []string {
	return []string{
		m.config.SharedConfigDirPath(),
		m.config.SharedStorageDirPath(),
		m.config.LogsDirPath(),
		m.config.TempDirPath(),
	}
}

// EnsureDirectoriesExist verifies that all shared resource directories exist
// on the filesystem. Returns nil if all directories exist, or an error
// describing the first missing or invalid entry.
//
// Reference: TS-P5-09
func (m *SharedResourceManager) EnsureDirectoriesExist() error {
	for _, dir := range m.AllSharedDirPaths() {
		info, err := os.Stat(dir)
		if err != nil {
			return fmt.Errorf("shared directory %s: %w", dir, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s exists but is not a directory", dir)
		}
	}
	return nil
}

// CleanTemp removes all contents (files and subdirectories) inside the
// temporary directory without removing the temporary directory itself.
// Returns nil if the temporary directory does not exist (nothing to clean).
//
// Reference: TS-P5-09
func (m *SharedResourceManager) CleanTemp() error {
	tmpDir := m.config.TempDirPath()

	// If temp dir doesn't exist, nothing to clean.
	if _, err := os.Stat(tmpDir); os.IsNotExist(err) {
		return nil
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return fmt.Errorf("read temp directory %s: %w", tmpDir, err)
	}

	for _, entry := range entries {
		entryPath := filepath.Join(tmpDir, entry.Name())
		if err := os.RemoveAll(entryPath); err != nil {
			return fmt.Errorf("remove %s: %w", entryPath, err)
		}
	}

	return nil
}

// IsSharedResource returns true if the given path falls within any shared
// resource directory (config, storage, logs, or temp). The check uses a
// cleaned prefix-based comparison: the path must either equal the shared
// directory path or have the shared directory path as a proper prefix
// (followed by a filepath separator).
//
// Reference: TS-P5-09
func (m *SharedResourceManager) IsSharedResource(path string) bool {
	cleanPath := filepath.Clean(path)

	for _, sharedDir := range m.AllSharedDirPaths() {
		cleanShared := filepath.Clean(sharedDir)
		if cleanPath == cleanShared {
			return true
		}
		// Check if path is a subdirectory or file within the shared dir.
		prefix := cleanShared + string(filepath.Separator)
		if strings.HasPrefix(cleanPath, prefix) {
			return true
		}
	}

	return false
}

// ValidateIsolation verifies that no shared resource directory is a
// subdirectory of the releases directory. If any shared directory path
// starts with the releases directory path, an error describing the
// conflict is returned.
//
// Reference: TS-P5-09, ADR-003 §8.5
func (m *SharedResourceManager) ValidateIsolation() error {
	releasesDir := m.config.ReleasesDirPath()
	releasesClean := filepath.Clean(releasesDir)
	releasesPrefix := releasesClean + string(filepath.Separator)

	for _, sharedDir := range m.AllSharedDirPaths() {
		sharedClean := filepath.Clean(sharedDir)

		// A shared directory conflicts if it is a subdirectory of releasesDir.
		if strings.HasPrefix(sharedClean, releasesPrefix) {
			return fmt.Errorf(
				"shared resource directory %q is a subdirectory of releases directory %q: shared resources must not be nested under releases",
				sharedClean, releasesClean,
			)
		}

		// Also check if shared dir equals releases dir (edge case).
		if sharedClean == releasesClean {
			return fmt.Errorf(
				"shared resource directory %q conflicts with releases directory %q: shared resources must be distinct from releases",
				sharedClean, releasesClean,
			)
		}
	}

	return nil
}
