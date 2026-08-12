// Package artifact defines the Anvil artifact directory layout and
// packaging primitives.
//
// Reference: TS-P3-04, ADR-004 §3.4, §8.8, EPIC-003
package artifact

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// GenerateIdentity computes a content-derived identity from deployable files.
//
// The identity is a SHA-256 hash over the concatenated, ordered content of all
// deployable files. Files are processed in sorted (by relative path) order to
// ensure deterministic output regardless of filesystem enumeration order.
//
// Only deployable file content affects the identity — metadata such as
// timestamps, permissions, or configuration-only changes that do not affect
// file content do not change the identity.
//
// Non-regular files (directories, symlinks, sockets, etc.) are skipped
// silently to handle edge cases where the file filter may return them.
//
// Returns a hex-encoded hash string, or an empty string with no error when
// files is empty (an artifact with no files has a well-defined identity).
//
// Reference: TS-P3-04, ADR-004 §3.4, §8.8
func GenerateIdentity(sourceDir string, files []string) (string, error) {
	if len(files) == 0 {
		return "", nil
	}

	// Sort files by relative path for deterministic ordering.
	sorted := make([]string, len(files))
	copy(sorted, files)
	sort.Strings(sorted)

	hasher := sha256.New()

	for _, relPath := range sorted {
		fullPath := filepath.Join(sourceDir, relPath)

		// Skip non-regular files (directories, symlinks, etc.) defensively.
		// The file filter should only return regular files, but this guards
		// against edge cases (mount points, race conditions, unusual filesystems).
		info, err := os.Lstat(fullPath)
		if err != nil {
			return "", fmt.Errorf("stat %s for identity: %w", relPath, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}

		data, err := os.ReadFile(fullPath)
		if err != nil {
			return "", fmt.Errorf("read %s for identity: %w", relPath, err)
		}

		// Write the relative path and content to the hash.
		// Including the path prevents two different files with identical
		// content from producing the same identity contribution.
		if _, err := hasher.Write([]byte(relPath)); err != nil {
			return "", fmt.Errorf("hash path %s: %w", relPath, err)
		}
		if _, err := hasher.Write(data); err != nil {
			return "", fmt.Errorf("hash content %s: %w", relPath, err)
		}
	}

	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}
