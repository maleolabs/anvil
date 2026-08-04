// Package runtime provides models and utilities for managing Anvil Runtime
// instances — their configuration, lifecycle state machines, readiness
// assessment, runtime identity, directory structure provisioning, shared
// resource management, atomic symlink switching, and runtime continuity.
//
// Reference: CH-P5-01, TS-P5-01, TS-P5-02, TS-P5-03, TS-P5-04, TS-P5-05,
// TS-P5-07, TS-P5-08, TS-P5-09, EPIC-005, ADR-003 §8.5
package runtime

import (
	"fmt"
	"os"
	"path/filepath"
)

// SymlinkSwitcher handles atomic updates of the active release symlink.
// It uses an atomic rename pattern to ensure zero-downtime switching:
// create a temporary symlink, then atomically rename it to the active path.
//
// Reference: TS-P5-08, ADR-003 §8.5, Architecture Alignment §8.5
type SymlinkSwitcher struct {
	config RuntimeConfig
}

// NewSymlinkSwitcher creates a new SymlinkSwitcher configured with the given
// RuntimeConfig.
//
// Reference: TS-P5-08
func NewSymlinkSwitcher(cfg RuntimeConfig) *SymlinkSwitcher {
	return &SymlinkSwitcher{config: cfg}
}

// SwitchTo atomically changes the active symlink to point to the given target
// release directory. It uses an atomic rename pattern:
//
//  1. Create a temporary symlink pointing to the new release directory
//  2. Atomically rename (os.Rename) the temporary symlink to the active
//     symlink path
//
// On the filesystem, os.Rename is atomic on the same filesystem — observers
// see either the old symlink target or the new one, never an invalid state.
//
// If the operation fails at any point, the existing active symlink is left
// intact. An error is returned describing the failure.
//
// Reference: TS-P5-08 AC-1, AC-2, AC-4
func (s *SymlinkSwitcher) SwitchTo(targetReleaseDir string) error {
	activePath := s.config.ActiveSymlinkPath()
	tmpPath := activePath + ".tmp"

	// Ensure the target directory exists before switching.
	targetInfo, err := os.Stat(targetReleaseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("target release directory %s does not exist", targetReleaseDir)
		}
		return fmt.Errorf("stat target release directory %s: %w", targetReleaseDir, err)
	}
	if !targetInfo.IsDir() {
		return fmt.Errorf("target path %s exists but is not a directory", targetReleaseDir)
	}

	// Step 1: Clean up any stale temporary symlink from a previous failed
	// operation.
	if err := os.Remove(tmpPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale temporary symlink %s: %w", tmpPath, err)
	}

	// Step 2: Create a temporary symlink pointing to the target.
	//
	// The temp symlink is created in the same directory as the active symlink.
	// This guarantees that the subsequent os.Rename stays within the same
	// filesystem mount, which is a requirement for atomic rename.
	if err := os.Symlink(targetReleaseDir, tmpPath); err != nil {
		return fmt.Errorf("create temporary symlink %s -> %s: %w", tmpPath, targetReleaseDir, err)
	}

	// Step 3: Atomically rename the temporary symlink to the active symlink
	// path. On Linux, os.Rename is atomic when both paths are on the same
	// filesystem.
	if err := os.Rename(tmpPath, activePath); err != nil {
		// Clean up the temporary symlink on rename failure.
		removeErr := os.Remove(tmpPath)
		if removeErr != nil && !os.IsNotExist(removeErr) {
			return fmt.Errorf("rename %s to %s failed (%w) and cleanup also failed: %v",
				tmpPath, activePath, err, removeErr)
		}
		return fmt.Errorf("rename temporary symlink %s to active symlink %s: %w",
			tmpPath, activePath, err)
	}

	return nil
}

// SwitchForActivation is a convenience wrapper around SwitchTo for use during
// Release activation. It performs the same atomic symlink switch but with a
// domain-meaningful name for readability at the call site.
//
// Reference: TS-P5-08 AC-3
func (s *SymlinkSwitcher) SwitchForActivation(targetReleaseDir string) error {
	return s.SwitchTo(targetReleaseDir)
}

// SwitchForRollback is a convenience wrapper around SwitchTo for use during
// Release rollback. It performs the same atomic symlink switch but with a
// domain-meaningful name for readability at the call site.
//
// Reference: TS-P5-08 AC-3
func (s *SymlinkSwitcher) SwitchForRollback(targetReleaseDir string) error {
	return s.SwitchTo(targetReleaseDir)
}

// ActiveSymlinkTarget reads the current target of the active symlink using
// os.Readlink. Returns the target path as a string, or an error if the
// symlink does not exist or cannot be read.
//
// Reference: TS-P5-08
func (s *SymlinkSwitcher) ActiveSymlinkTarget() (string, error) {
	activePath := s.config.ActiveSymlinkPath()
	target, err := os.Readlink(activePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("active symlink %s does not exist", activePath)
		}
		return "", fmt.Errorf("read active symlink %s: %w", activePath, err)
	}
	return target, nil
}

// ActiveSymlinkPath returns the full filesystem path to the active release
// symlink.
//
// Reference: TS-P5-08
func (s *SymlinkSwitcher) ActiveSymlinkPath() string {
	return s.config.ActiveSymlinkPath()
}

// SymlinkExists reports whether the active symlink exists on the filesystem.
// Returns false if the symlink does not exist or cannot be stat'd.
//
// Reference: TS-P5-08
func (s *SymlinkSwitcher) SymlinkExists() bool {
	_, err := os.Lstat(s.config.ActiveSymlinkPath())
	return err == nil
}

// ensureActiveSymlinkParentDir ensures the parent directory for the active
// symlink exists. The parent directory is the InstallRoot, which should
// already exist post-provisioning. This is a safety check for edge cases.
//
// Reference: TS-P5-08
func ensureActiveSymlinkParentDir(activePath string) error {
	parent := filepath.Dir(activePath)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return fmt.Errorf("create active symlink parent directory %s: %w", parent, err)
	}
	return nil
}
