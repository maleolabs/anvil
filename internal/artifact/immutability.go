// Package artifact defines the Anvil artifact directory layout and
// packaging primitives.
//
// Reference: TS-P3-08, ADR-004 §8.1/§8.3/§8.6/§8.7, EPIC-003
package artifact

import (
	"fmt"
	"os"
)

// ImmutabilityResult represents the outcome of an immutability verification.
//
// The result captures both the original (manifest-recorded) and the
// recomputed checksum so that callers can inspect the discrepancy when
// immutability is violated.
//
// Reference: TS-P3-08, ADR-004 §8.1/§8.3/§8.6/§8.7
type ImmutabilityResult struct {
	Passed           bool   `json:"passed"`
	OriginalChecksum string `json:"original_checksum"`
	CurrentChecksum  string `json:"current_checksum"`
	Details          string `json:"details,omitempty"`
}

// AssertImmutability verifies that the deployable content of an artifact
// matches the expected checksum. It opens the artifact in read-only mode,
// extracts deployable content to a temporary directory, recomputes the
// checksum using ComputeChecksum, and compares it against the provided
// original checksum.
//
// Returns nil if the checksums match, or an error describing the mismatch.
// Returns an error if the artifact does not exist or cannot be read.
//
// This is a low-level enforcement function designed for use by downstream
// consumers that already know the expected checksum (e.g. from a deployment
// record or policy document).
//
// Reference: TS-P3-08, ADR-004 §8.1/§8.3/§8.6/§8.7
func AssertImmutability(artifactPath string, originalChecksum string) error {
	// Verify the file exists before attempting extraction.
	if _, err := os.Stat(artifactPath); err != nil {
		return fmt.Errorf("access artifact: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "anvil-immutability-*")
	if err != nil {
		return fmt.Errorf("create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	files, err := extractDeployableContent(artifactPath, tmpDir)
	if err != nil {
		return fmt.Errorf("extract deployable content: %w", err)
	}

	computedChecksum, err := ComputeChecksum(tmpDir, files)
	if err != nil {
		return fmt.Errorf("compute checksum: %w", err)
	}

	if computedChecksum != originalChecksum {
		return fmt.Errorf("immutability check failed: expected %s, got %s", originalChecksum, computedChecksum)
	}

	return nil
}

// VerifyImmutability reads the original checksum from the artifact's
// manifest, extracts and recomputes the checksum over the deployable
// content, and returns a detailed ImmutabilityResult.
//
// Returns an error only for I/O or structural failures (file not found,
// manifest unreadable, extraction failure). The comparison result itself
// is always communicated through the ImmutabilityResult — never as an error.
//
// Reference: TS-P3-08, ADR-004 §8.1/§8.3/§8.6/§8.7
func VerifyImmutability(artifactPath string) (*ImmutabilityResult, error) {
	// Verify the file exists.
	if _, err := os.Stat(artifactPath); err != nil {
		return nil, fmt.Errorf("access artifact: %w", err)
	}

	// Read the manifest to obtain the original checksum.
	manifest, err := ReadManifest(artifactPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	originalChecksum := manifest.Checksum

	// Extract deployable content to a temporary directory for checksum
	// recomputation.
	tmpDir, err := os.MkdirTemp("", "anvil-verify-immutability-*")
	if err != nil {
		return nil, fmt.Errorf("create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	files, err := extractDeployableContent(artifactPath, tmpDir)
	if err != nil {
		return nil, fmt.Errorf("extract deployable content: %w", err)
	}

	computedChecksum, err := ComputeChecksum(tmpDir, files)
	if err != nil {
		return nil, fmt.Errorf("compute checksum: %w", err)
	}

	if computedChecksum != originalChecksum {
		return &ImmutabilityResult{
			Passed:           false,
			OriginalChecksum: originalChecksum,
			CurrentChecksum:  computedChecksum,
			Details:          fmt.Sprintf("checksum mismatch: expected %s, got %s", originalChecksum, computedChecksum),
		}, nil
	}

	return &ImmutabilityResult{
		Passed:           true,
		OriginalChecksum: originalChecksum,
		CurrentChecksum:  computedChecksum,
		Details:          "artifact is immutable",
	}, nil
}
