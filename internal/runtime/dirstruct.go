// Package runtime provides models and utilities for managing Anvil Runtime
// instances — their configuration, lifecycle state machines, readiness
// assessment, runtime identity, and directory structure provisioning.
//
// Reference: CH-P5-01, TS-P5-01, TS-P5-02, TS-P5-03, TS-P5-05, TS-P5-07,
// EPIC-005, ADR-003 §8.5
package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrActiveReleaseRemoval is returned when attempting to remove a release
// directory that is currently the Active Release.
//
// Reference: ST-P5-04
var ErrActiveReleaseRemoval = fmt.Errorf("cannot remove the Active Release directory")

// ErrRollbackCandidateRemoval is returned when attempting to remove a release
// directory that is the rollback candidate (previously Active Release).
//
// Reference: ST-P5-04
var ErrRollbackCandidateRemoval = fmt.Errorf("cannot remove the rollback candidate release directory")

// DirProvisioner handles the creation and verification of the runtime directory
// structure required for Anvil Runtime operation.
//
// Reference: TS-P5-05, ADR-003 §8.5
type DirProvisioner struct {
	config RuntimeConfig
}

// NewDirProvisioner creates a new DirProvisioner configured with the given
// RuntimeConfig.
//
// Reference: TS-P5-05
func NewDirProvisioner(cfg RuntimeConfig) *DirProvisioner {
	return &DirProvisioner{config: cfg}
}

// Provision creates all directories defined in the RuntimeConfig using
// os.MkdirAll with 0755 permissions. It is idempotent — calling Provision
// multiple times will succeed without error.
//
// Reference: TS-P5-05 AC-1, AC-2
func (p *DirProvisioner) Provision(ctx context.Context) error {
	for _, dir := range p.config.AllDirs() {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}
	return nil
}

// EnsureDirectoriesExist verifies that every directory defined in the
// RuntimeConfig exists on the filesystem. Returns nil if all directories
// exist, or an error describing the first missing or invalid entry.
//
// Reference: TS-P5-05 AC-3
func (p *DirProvisioner) EnsureDirectoriesExist() error {
	for _, dir := range p.config.AllDirs() {
		info, err := os.Stat(dir)
		if err != nil {
			return fmt.Errorf("directory %s: %w", dir, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s exists but is not a directory", dir)
		}
	}
	return nil
}

// CreateReleaseDir creates a versioned release directory at
// <releasesPath>/rel-<identity>/ using os.Mkdir with 0755 permissions.
// The releasesPath parent directory is created first if it does not exist.
// Returns an error if the release directory already exists (to prevent
// accidental overwrite).
//
// Reference: TS-P5-07
func CreateReleaseDir(releasesPath string, identity string) (string, error) {
	// Ensure the releases parent directory exists (idempotent).
	if err := os.MkdirAll(releasesPath, 0755); err != nil {
		return "", fmt.Errorf("create releases directory %s: %w", releasesPath, err)
	}

	dir := ReleaseDirPath(releasesPath, identity)

	if err := os.Mkdir(dir, 0755); err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("release directory %s already exists", dir)
		}
		return "", fmt.Errorf("create release directory %s: %w", dir, err)
	}

	return dir, nil
}

// ReleaseDirPath returns the full path to a versioned release directory
// without creating it: <releasesPath>/rel-<identity>.
//
// Reference: TS-P5-07
func ReleaseDirPath(releasesPath string, identity string) string {
	return filepath.Join(releasesPath, "rel-"+identity)
}

// ReleaseIdentityFromPath extracts the release identity from a directory
// path. Given a path like "/opt/anvil/releases/rel-abc123/" it returns
// "abc123". Returns an error if the path does not match the rel-<identity>
// pattern in its final path component.
//
// Reference: TS-P5-07
func ReleaseIdentityFromPath(dirPath string) (string, error) {
	base := filepath.Base(dirPath)

	if !strings.HasPrefix(base, "rel-") || len(base) <= 4 {
		return "", fmt.Errorf("path %q does not match rel-<identity> pattern", dirPath)
	}

	identity := base[4:]
	if identity == "" {
		return "", fmt.Errorf("path %q does not match rel-<identity> pattern", dirPath)
	}

	return identity, nil
}

// RemoveReleaseDir removes the versioned release directory for the given
// release identity. It returns the number of bytes removed and any error.
// The releases parent directory is not removed.
//
// Reference: ST-P5-04
func RemoveReleaseDir(releasesPath string, identity string) (int64, error) {
	dir := ReleaseDirPath(releasesPath, identity)

	// Verify the directory exists and is a directory.
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, fmt.Errorf("release directory %s not found", dir)
		}
		return 0, fmt.Errorf("check release directory %s: %w", dir, err)
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("%s is not a directory", dir)
	}

	// Calculate total size before removal.
	size, err := dirSize(dir)
	if err != nil {
		return 0, fmt.Errorf("calculate size of %s: %w", dir, err)
	}

	// Remove the entire directory tree.
	if err := os.RemoveAll(dir); err != nil {
		return 0, fmt.Errorf("remove release directory %s: %w", dir, err)
	}

	return size, nil
}

// dirSize recursively calculates the total size of a directory tree in bytes.
func dirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !fi.IsDir() {
			size += fi.Size()
		}
		return nil
	})
	return size, err
}
