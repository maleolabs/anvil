// Package artifact defines the Anvil artifact directory layout and
// packaging primitives.
//
// Reference: TS-P3-06, ADR-004 §8.9, §3.4, EPIC-003
package artifact

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ChecksumAlgorithmSHA256 is the identifier for the SHA-256 checksum
// algorithm used by ComputeChecksum.
const ChecksumAlgorithmSHA256 = "sha-256"

// ComputeChecksum computes a SHA-256 checksum over the artifact's deployable
// content. The checksum covers the same ordered file content as the identity,
// ensuring that integrity evidence is tied to the artifact's content-derived
// identity.
//
// Files are processed in sorted (by relative path) order for deterministic
// output. The relative path is included in the hash input alongside the file
// content to prevent different file arrangements from producing the same
// checksum.
//
// Non-regular files (directories, symlinks, sockets, etc.) are skipped
// silently to match the packaging behavior in createArchive.
//
// Returns a hex-encoded checksum string, or an empty string with no error
// when files is empty.
//
// Reference: TS-P3-06, ADR-004 §8.9, §3.4
func ComputeChecksum(sourceDir string, files []string) (string, error) {
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

		info, err := os.Lstat(fullPath)
		if err != nil {
			return "", fmt.Errorf("stat %s for checksum: %w", relPath, err)
		}

		// Skip non-regular files (directories, symlinks, etc.) to match
		// the packaging behavior in createArchive.
		if !info.Mode().IsRegular() {
			continue
		}

		data, err := os.ReadFile(fullPath)
		if err != nil {
			return "", fmt.Errorf("read %s for checksum: %w", relPath, err)
		}

		// Include the relative path to prevent two different file
		// arrangements from producing an identical checksum.
		if _, err := hasher.Write([]byte(relPath)); err != nil {
			return "", fmt.Errorf("hash path %s: %w", relPath, err)
		}
		if _, err := hasher.Write(data); err != nil {
			return "", fmt.Errorf("hash content %s: %w", relPath, err)
		}
	}

	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}
