// Package artifact defines the Anvil artifact directory layout and
// packaging primitives.
//
// Reference: TS-P3-08, ST-P3-07, ADR-004 §8.1/§8.3/§8.6/§8.7, EPIC-003
package artifact

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestAssertImmutability_PassesForValidArtifact verifies that a valid,
// unmodified artifact passes the immutability assertion.
func TestAssertImmutability_PassesForValidArtifact(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "valid.tar.gz")

	deployable := map[string]string{"index.php": "<?php\n"}
	manifest := completeManifest()

	// First pass: create with hardcoded checksum.
	createTestArtifact(t, artifactPath, manifest, deployable)

	// Compute the actual checksum.
	tmpExtract := t.TempDir()
	files, err := extractDeployableContent(artifactPath, tmpExtract)
	if err != nil {
		t.Fatalf("extract deployable content: %v", err)
	}
	actualChecksum, err := ComputeChecksum(tmpExtract, files)
	if err != nil {
		t.Fatalf("compute checksum: %v", err)
	}

	// Recreate with the correct checksum.
	manifest.Checksum = actualChecksum
	createTestArtifact(t, artifactPath, manifest, deployable)

	err = AssertImmutability(artifactPath, actualChecksum)
	if err != nil {
		t.Errorf("AssertImmutability should pass for valid artifact, got: %v", err)
	}
}

// TestAssertImmutability_FailsForTamperedArtifact verifies that a tampered
// artifact fails the immutability assertion.
func TestAssertImmutability_FailsForTamperedArtifact(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "tampered.tar.gz")

	// Create a valid artifact with "original" content.
	originalContent := map[string]string{"index.php": "<?php\n// original\n"}
	manifest := completeManifest()

	createTestArtifact(t, artifactPath, manifest, originalContent)

	// Compute the actual checksum of the original content.
	tmpExtract := t.TempDir()
	files, err := extractDeployableContent(artifactPath, tmpExtract)
	if err != nil {
		t.Fatalf("extract deployable content: %v", err)
	}
	originalChecksum, err := ComputeChecksum(tmpExtract, files)
	if err != nil {
		t.Fatalf("compute checksum: %v", err)
	}

	// Now overwrite the artifact with DIFFERENT content but same filename.
	modifiedContent := map[string]string{"index.php": "<?php\n// modified\n"}
	manifest.Checksum = originalChecksum // keep original checksum in manifest
	createTestArtifact(t, artifactPath, manifest, modifiedContent)

	// The checksum of the artifact should now differ from original.
	err = AssertImmutability(artifactPath, originalChecksum)
	if err == nil {
		t.Fatal("AssertImmutability should fail when artifact content changed")
	}

	if !strings.Contains(err.Error(), "immutability check failed") {
		t.Errorf("error should mention immutability check failed, got: %v", err)
	}
}

// TestAssertImmutability_NonExistentFile verifies that a non-existent artifact
// returns an error.
func TestAssertImmutability_NonExistentFile(t *testing.T) {
	err := AssertImmutability("/tmp/nonexistent-artifact-immut-12345.tar.gz", "somechecksum")
	if err == nil {
		t.Error("expected error for non-existent artifact, got nil")
	}
}

// TestAssertImmutability_EmptyContent verifies immutability for an artifact
// with no deployable files (manifest-only archive).
func TestAssertImmutability_EmptyContent(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "empty.tar.gz")

	// Empty deployable files.
	manifest := completeManifest()
	manifest.Checksum = "" // empty content = empty checksum

	createTestArtifact(t, artifactPath, manifest, nil)

	err := AssertImmutability(artifactPath, "")
	if err != nil {
		t.Errorf("AssertImmutability should pass for empty artifact, got: %v", err)
	}
}

// TestVerifyImmutability_AllPass verifies that a valid artifact returns a
// passing ImmutabilityResult with matching checksums.
func TestVerifyImmutability_AllPass(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "valid-verify.tar.gz")

	deployable := map[string]string{
		"index.php": "<?php\n",
		"app.php":   "<?php\n// test\n",
	}
	manifest := completeManifest()

	createTestArtifact(t, artifactPath, manifest, deployable)

	// Compute actual checksum to make things consistent.
	tmpExtract := t.TempDir()
	files, err := extractDeployableContent(artifactPath, tmpExtract)
	if err != nil {
		t.Fatalf("extract deployable content: %v", err)
	}
	actualChecksum, err := ComputeChecksum(tmpExtract, files)
	if err != nil {
		t.Fatalf("compute checksum: %v", err)
	}

	// Recreate with correct checksum.
	manifest.Checksum = actualChecksum
	createTestArtifact(t, artifactPath, manifest, deployable)

	result, err := VerifyImmutability(artifactPath)
	if err != nil {
		t.Fatalf("VerifyImmutability returned unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("VerifyImmutability returned nil result")
	}

	if !result.Passed {
		t.Errorf("expected Passed=true for valid artifact. Original: %s, Current: %s, Details: %s",
			result.OriginalChecksum, result.CurrentChecksum, result.Details)
	}

	if result.OriginalChecksum != actualChecksum {
		t.Errorf("OriginalChecksum = %q, want %q", result.OriginalChecksum, actualChecksum)
	}

	if result.CurrentChecksum != actualChecksum {
		t.Errorf("CurrentChecksum = %q, want %q", result.CurrentChecksum, actualChecksum)
	}
}

// TestVerifyImmutability_TamperedArtifact verifies that a tampered artifact
// returns a failing ImmutabilityResult.
func TestVerifyImmutability_TamperedArtifact(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "tampered-verify.tar.gz")

	deployable := map[string]string{"index.php": "<?php\n"}
	manifest := completeManifest()

	// Create with a deliberately wrong checksum.
	manifest.Checksum = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	createTestArtifact(t, artifactPath, manifest, deployable)

	result, err := VerifyImmutability(artifactPath)
	if err != nil {
		t.Fatalf("VerifyImmutability returned unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("VerifyImmutability returned nil result")
	}

	if result.Passed {
		t.Error("expected Passed=false for tampered artifact")
	}

	if result.OriginalChecksum != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("OriginalChecksum should match manifest, got %q", result.OriginalChecksum)
	}

	if result.CurrentChecksum == "" {
		t.Error("CurrentChecksum should not be empty")
	}

	if !strings.Contains(result.Details, "checksum mismatch") {
		t.Errorf("details should mention checksum mismatch, got: %s", result.Details)
	}
}

// TestVerifyImmutability_NonExistentFile verifies that a non-existent artifact
// returns an error from VerifyImmutability.
func TestVerifyImmutability_NonExistentFile(t *testing.T) {
	_, err := VerifyImmutability("/tmp/nonexistent-artifact-verify-99999.tar.gz")
	if err == nil {
		t.Error("expected error for non-existent artifact, got nil")
	}
}
