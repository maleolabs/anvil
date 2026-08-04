// Package artifact defines the Anvil artifact directory layout and
// packaging primitives.
//
// Reference: TS-P3-07, ST-P3-06, EPIC-003
package artifact

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// createTestArtifact creates a minimal valid artifact archive at the given
// path with the specified manifest and deployable files. Returns the path.
//
// deployableFiles is a map of relative paths (within DeployableContentDir)
// to file content.
func createTestArtifact(t *testing.T, path string, manifest Manifest, deployableFiles map[string]string) {
	t.Helper()

	var buf bytes.Buffer
	gzW := gzip.NewWriter(&buf)
	tarW := tar.NewWriter(gzW)

	// Write deployable files under DeployableContentDir.
	for relPath, content := range deployableFiles {
		entryName := filepath.Join(DeployableContentDir, relPath)
		hdr := &tar.Header{
			Name:     entryName,
			Size:     int64(len(content)),
			Mode:     0644,
			Typeflag: tar.TypeReg,
		}
		if err := tarW.WriteHeader(hdr); err != nil {
			t.Fatalf("write tar header for %s: %v", relPath, err)
		}
		if _, err := tarW.Write([]byte(content)); err != nil {
			t.Fatalf("write tar content for %s: %v", relPath, err)
		}
	}

	// Write the manifest at the artifact root.
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}

	hdr := &tar.Header{
		Name:     ManifestFile,
		Size:     int64(len(manifestData)),
		Mode:     0644,
		Typeflag: tar.TypeReg,
	}
	if err := tarW.WriteHeader(hdr); err != nil {
		t.Fatalf("write manifest tar header: %v", err)
	}
	if _, err := tarW.Write(manifestData); err != nil {
		t.Fatalf("write manifest content: %v", err)
	}

	if err := tarW.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gzW.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}

	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write artifact file: %v", err)
	}
}

// completeManifest returns a fully populated Manifest for testing.
func completeManifest() Manifest {
	return Manifest{
		ArtifactID:   "abc123def456",
		Version:      "1.0.0",
		CreatedAt:    "2026-07-25T12:00:00Z",
		Source:       "test-project",
		Checksum:     "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		ChecksumType: ChecksumAlgorithmSHA256,
		ProjectID:    "test-project-id",
	}
}

// TestVerifyArtifact_AllPass verifies that a valid, properly constructed
// artifact passes all six verification checks.
func TestVerifyArtifact_AllPass(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "valid-artifact.tar.gz")

	// Create an artifact with one deployable file.
	deployable := map[string]string{
		"index.php": "<?php\n",
	}
	manifest := completeManifest()

	createTestArtifact(t, artifactPath, manifest, deployable)

	// Compute the actual checksum to make the checksum check pass.
	// First extract to temp dir and compute.
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

	result, err := VerifyArtifact(artifactPath)
	if err != nil {
		t.Fatalf("VerifyArtifact returned unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("VerifyArtifact returned nil result")
	}

	if !result.Passed {
		t.Errorf("expected Passed=true, got false. Checks: %+v", result.Checks)
	}

	if len(result.Checks) != 6 {
		t.Errorf("expected 6 checks, got %d", len(result.Checks))
	}

	for _, c := range result.Checks {
		if !c.Passed {
			t.Errorf("check %q failed: %s", c.Name, c.Details)
		}
	}
}

// TestVerifyArtifact_CorruptedArchive verifies that a corrupted archive
// fails the archive validity check.
func TestVerifyArtifact_CorruptedArchive(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "corrupted.tar.gz")

	// Write invalid gzip data.
	if err := os.WriteFile(artifactPath, []byte("not-a-gzip-file"), 0644); err != nil {
		t.Fatalf("write corrupted file: %v", err)
	}

	result, err := VerifyArtifact(artifactPath)
	if err != nil {
		t.Fatalf("VerifyArtifact returned unexpected error: %v", err)
	}

	if result.Passed {
		t.Error("expected Passed=false for corrupted archive")
	}

	found := false
	for _, c := range result.Checks {
		if c.Name == "Archive validity" && !c.Passed {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected Archive validity check to fail")
	}
}

// TestVerifyArtifact_MissingManifest verifies that an archive without a
// manifest fails the manifest presence check.
func TestVerifyArtifact_MissingManifest(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "no-manifest.tar.gz")

	// Create a valid tar.gz with no manifest.
	var buf bytes.Buffer
	gzW := gzip.NewWriter(&buf)
	tarW := tar.NewWriter(gzW)

	// Just one deployable file, no manifest.
	content := "<?php\n"
	hdr := &tar.Header{
		Name:     filepath.Join(DeployableContentDir, "index.php"),
		Size:     int64(len(content)),
		Mode:     0644,
		Typeflag: tar.TypeReg,
	}
	if err := tarW.WriteHeader(hdr); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tarW.Write([]byte(content)); err != nil {
		t.Fatalf("write tar content: %v", err)
	}

	if err := tarW.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gzW.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	if err := os.WriteFile(artifactPath, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	result, err := VerifyArtifact(artifactPath)
	if err != nil {
		t.Fatalf("VerifyArtifact returned unexpected error: %v", err)
	}

	if result.Passed {
		t.Error("expected Passed=false for missing manifest")
	}

	found := false
	for _, c := range result.Checks {
		if c.Name == "Manifest presence" && !c.Passed {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected Manifest presence check to fail")
	}
}

// TestVerifyArtifact_MissingManifestFields verifies that a manifest with
// empty required fields fails the content check.
func TestVerifyArtifact_MissingManifestFields(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "bad-manifest.tar.gz")

	// Manifest missing several fields.
	manifest := Manifest{
		ArtifactID:   "abc123",
		Version:      "",
		CreatedAt:    "",
		Source:       "test",
		Checksum:     "",
		ChecksumType: "",
	}

	createTestArtifact(t, artifactPath, manifest, map[string]string{
		"index.php": "<?php\n",
	})

	result, err := VerifyArtifact(artifactPath)
	if err != nil {
		t.Fatalf("VerifyArtifact returned unexpected error: %v", err)
	}

	if result.Passed {
		t.Error("expected Passed=false for manifest with missing fields")
	}

	found := false
	for _, c := range result.Checks {
		if c.Name == "Manifest content" && !c.Passed {
			found = true
			// Should mention the missing fields.
			if !strings.Contains(c.Details, "version") ||
				!strings.Contains(c.Details, "created_at") ||
				!strings.Contains(c.Details, "checksum") ||
				!strings.Contains(c.Details, "checksum_type") ||
				!strings.Contains(c.Details, "project_id") {
				t.Errorf("details should mention all missing fields, got: %s", c.Details)
			}
			break
		}
	}
	if !found {
		t.Error("expected Manifest content check to fail")
	}
}

// TestVerifyArtifact_MissingProjectIdentity verifies that an artifact with
// an empty ProjectID fails with a clear diagnostic.
func TestVerifyArtifact_MissingProjectIdentity(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "missing-project-id.tar.gz")

	manifest := completeManifest()
	manifest.ProjectID = ""

	// Compute correct checksum so only the identity check fails.
	createTestArtifact(t, artifactPath, manifest, map[string]string{"index.php": "<?php\n"})
	tmpExtract := t.TempDir()
	files, err := extractDeployableContent(artifactPath, tmpExtract)
	if err != nil {
		t.Fatalf("extract deployable content: %v", err)
	}
	actualChecksum, err := ComputeChecksum(tmpExtract, files)
	if err != nil {
		t.Fatalf("compute checksum: %v", err)
	}
	manifest.Checksum = actualChecksum
	createTestArtifact(t, artifactPath, manifest, map[string]string{"index.php": "<?php\n"})

	result, err := VerifyArtifact(artifactPath)
	if err != nil {
		t.Fatalf("VerifyArtifact returned unexpected error: %v", err)
	}

	if result.Passed {
		t.Error("expected Passed=false for missing project_id")
	}

	// Should fail with a diagnostic mentioning project_id.
	found := false
	for _, c := range result.Checks {
		if !c.Passed && strings.Contains(c.Details, "project_id") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no failing check mentioned project_id. Checks: %+v", result.Checks)
	}
}

// TestVerifyArtifact_MalformedProjectIdentity verifies that a manifest with
// an invalid ProjectID format fails with a clear diagnostic.
func TestVerifyArtifact_MalformedProjectIdentity(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "malformed-project-id.tar.gz")

	manifest := completeManifest()
	manifest.ProjectID = "invalid project!"

	// Compute correct checksum so only the identity check fails.
	createTestArtifact(t, artifactPath, manifest, map[string]string{"index.php": "<?php\n"})
	tmpExtract := t.TempDir()
	files, err := extractDeployableContent(artifactPath, tmpExtract)
	if err != nil {
		t.Fatalf("extract deployable content: %v", err)
	}
	actualChecksum, err := ComputeChecksum(tmpExtract, files)
	if err != nil {
		t.Fatalf("compute checksum: %v", err)
	}
	manifest.Checksum = actualChecksum
	createTestArtifact(t, artifactPath, manifest, map[string]string{"index.php": "<?php\n"})

	result, err := VerifyArtifact(artifactPath)
	if err != nil {
		t.Fatalf("VerifyArtifact returned unexpected error: %v", err)
	}

	if result.Passed {
		t.Error("expected Passed=false for malformed project_id")
	}

	// Should fail the Project identity check.
	found := false
	for _, c := range result.Checks {
		if c.Name == "Project identity" && !c.Passed {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected Project identity check to fail for malformed project_id")
	}
}

// TestVerifyArtifact_ValidProjectIdentity verifies that an artifact with
// a valid project identity passes all checks including Project identity.
func TestVerifyArtifact_ValidProjectIdentity(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "valid-project-id.tar.gz")

	manifest := completeManifest()

	// Compute correct checksum.
	createTestArtifact(t, artifactPath, manifest, map[string]string{"index.php": "<?php\n"})
	tmpExtract := t.TempDir()
	files, err := extractDeployableContent(artifactPath, tmpExtract)
	if err != nil {
		t.Fatalf("extract deployable content: %v", err)
	}
	actualChecksum, err := ComputeChecksum(tmpExtract, files)
	if err != nil {
		t.Fatalf("compute checksum: %v", err)
	}
	manifest.Checksum = actualChecksum
	createTestArtifact(t, artifactPath, manifest, map[string]string{"index.php": "<?php\n"})

	result, err := VerifyArtifact(artifactPath)
	if err != nil {
		t.Fatalf("VerifyArtifact returned unexpected error: %v", err)
	}

	if !result.Passed {
		t.Errorf("expected Passed=true for valid artifact, got false. Checks: %+v", result.Checks)
	}

	// Verify Project identity check passed.
	found := false
	for _, c := range result.Checks {
		if c.Name == "Project identity" {
			if !c.Passed {
				t.Errorf("Project identity check should pass, got: %s", c.Details)
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("expected Project identity check to be present")
	}
}

// TestVerifyArtifact_ChecksumMismatch verifies that a checksum mismatch
// causes the checksum match check to fail.
func TestVerifyArtifact_ChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "bad-checksum.tar.gz")

	manifest := completeManifest()
	// Set a deliberately wrong checksum.
	manifest.Checksum = "0000000000000000000000000000000000000000000000000000000000000000"

	createTestArtifact(t, artifactPath, manifest, map[string]string{
		"index.php": "<?php\n",
	})

	result, err := VerifyArtifact(artifactPath)
	if err != nil {
		t.Fatalf("VerifyArtifact returned unexpected error: %v", err)
	}

	if result.Passed {
		t.Error("expected Passed=false for checksum mismatch")
	}

	found := false
	for _, c := range result.Checks {
		if c.Name == "Checksum match" && !c.Passed {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected Checksum match check to fail")
	}
}

// TestVerifyArtifact_AllChecksReported verifies that all six checks run
// and report even when some fail.
func TestVerifyArtifact_AllChecksReported(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "multi-fail.tar.gz")

	// Write data that is not even valid gzip.
	if err := os.WriteFile(artifactPath, []byte("junk-data"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	result, err := VerifyArtifact(artifactPath)
	if err != nil {
		t.Fatalf("VerifyArtifact returned unexpected error: %v", err)
	}

	if len(result.Checks) != 6 {
		t.Errorf("expected 6 checks, got %d", len(result.Checks))
	}

	// Even though archive is invalid, all 6 checks should still be present.
	checkNames := make(map[string]bool)
	for _, c := range result.Checks {
		checkNames[c.Name] = c.Passed
	}

	expectedChecks := []string{"Archive validity", "Manifest presence", "Manifest content", "Project identity", "Checksum match", "Artifact immutability"}
	for _, name := range expectedChecks {
		if _, ok := checkNames[name]; !ok {
			t.Errorf("missing check: %s", name)
		}
	}
}

// TestVerifyArtifact_EmptyArchive verifies handling of an empty archive
// (valid gzip/tar with no entries).
func TestVerifyArtifact_EmptyArchive(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "empty.tar.gz")

	// Create an empty tar.gz (valid gzip with empty tar).
	var buf bytes.Buffer
	gzW := gzip.NewWriter(&buf)
	tarW := tar.NewWriter(gzW)
	if err := tarW.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gzW.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	if err := os.WriteFile(artifactPath, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	result, err := VerifyArtifact(artifactPath)
	if err != nil {
		t.Fatalf("VerifyArtifact returned unexpected error: %v", err)
	}

	if result.Passed {
		t.Error("expected Passed=false for empty archive (no manifest)")
	}

	// Archive validity should pass (empty tar is valid).
	// Manifest presence should fail.
	archiveOK := false
	manifestMissing := false
	for _, c := range result.Checks {
		if c.Name == "Archive validity" && c.Passed {
			archiveOK = true
		}
		if c.Name == "Manifest presence" && !c.Passed {
			manifestMissing = true
		}
	}
	if !archiveOK {
		t.Error("expected Archive validity to pass for empty tar.gz")
	}
	if !manifestMissing {
		t.Error("expected Manifest presence to fail for empty archive")
	}
}

// TestVerifyArtifact_IntegrationWithPackage creates a real artifact using
// Package(), then verifies it passes all checks.
func TestVerifyArtifact_IntegrationWithPackage(t *testing.T) {
	sourceDir := setupPackagingSource(t)
	outputDir := t.TempDir()

	// Package with known version, source, and project identity.
	result, err := Package(PackageOptions{
		SourceDir: sourceDir,
		OutputDir: outputDir,
		Version:   "1.0.0",
		Source:    "integration-test",
		ProjectID: "integration-test",
	})
	if err != nil {
		t.Fatalf("Package failed: %v", err)
	}

	// Verify the packaged artifact.
	vr, err := VerifyArtifact(result.ArtifactPath)
	if err != nil {
		t.Fatalf("VerifyArtifact failed: %v", err)
	}

	if !vr.Passed {
		t.Errorf("expected verification to pass for freshly packaged artifact. Checks: %+v", vr.Checks)
	}

	if len(vr.Checks) != 6 {
		t.Errorf("expected 6 checks, got %d", len(vr.Checks))
	}

	for _, c := range vr.Checks {
		if !c.Passed {
			t.Errorf("check %q should pass: %s", c.Name, c.Details)
		}
	}
}

// TestVerifyArtifact_NonExistentFile verifies that a non-existent artifact
// path returns an error.
func TestVerifyArtifact_NonExistentFile(t *testing.T) {
	_, err := VerifyArtifact("/tmp/nonexistent-artifact-12345.tar.gz")
	if err == nil {
		t.Error("expected error for non-existent artifact, got nil")
	}
}

// TestRequireVerified_PassesForValidArtifact verifies that RequireVerified
// returns nil for a passing artifact.
func TestRequireVerified_PassesForValidArtifact(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "valid.tar.gz")

	deployable := map[string]string{"index.php": "<?php\n"}
	manifest := completeManifest()

	// Compute the correct checksum.
	createTestArtifact(t, artifactPath, manifest, deployable)
	tmpExtract := t.TempDir()
	files, err := extractDeployableContent(artifactPath, tmpExtract)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	actualChecksum, err := ComputeChecksum(tmpExtract, files)
	if err != nil {
		t.Fatalf("compute checksum: %v", err)
	}
	manifest.Checksum = actualChecksum
	createTestArtifact(t, artifactPath, manifest, deployable)

	err = RequireVerified(artifactPath)
	if err != nil {
		t.Errorf("RequireVerified should return nil, got: %v", err)
	}
}

// TestRequireVerified_FailsForInvalidArtifact verifies that RequireVerified
// returns an error when verification fails.
func TestRequireVerified_FailsForInvalidArtifact(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "invalid.tar.gz")

	// Create an artifact with a bad checksum.
	manifest := completeManifest()
	manifest.Checksum = "badchecksum"
	createTestArtifact(t, artifactPath, manifest, map[string]string{"index.php": "<?php\n"})

	err := RequireVerified(artifactPath)
	if err == nil {
		t.Fatal("RequireVerified should return error for invalid artifact")
	}

	if !strings.Contains(err.Error(), "artifact verification failed") {
		t.Errorf("error message should contain 'artifact verification failed', got: %v", err)
	}
}

// TestRequireVerified_FailsForNonExistentFile verifies that RequireVerified
// returns an error when the artifact does not exist.
func TestRequireVerified_FailsForNonExistentFile(t *testing.T) {
	err := RequireVerified("/tmp/nonexistent-artifact-99999.tar.gz")
	if err == nil {
		t.Fatal("RequireVerified should return error for non-existent artifact")
	}

	if !strings.Contains(err.Error(), "artifact not found") {
		t.Errorf("error message should indicate artifact not found, got: %v", err)
	}
}

// --- safeExtractPath tests ---

// TestSafeExtractPath_ValidPaths verifies that safe, nested paths are
// accepted and return the correct resolved target.
func TestSafeExtractPath_ValidPaths(t *testing.T) {
	destDir := t.TempDir()

	tests := []struct {
		name      string
		entryName string
	}{
		{"simple file", "index.php"},
		{"nested path", "sub/dir/file.txt"},
		{"deep nesting", "a/b/c/d/e/f/g.php"},
		{"file with dots", "file.name.with.dots.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, err := safeExtractPath(destDir, tt.entryName)
			if err != nil {
				t.Fatalf("safeExtractPath(%q, %q) returned unexpected error: %v", destDir, tt.entryName, err)
			}
			expected := filepath.Join(destDir, tt.entryName)
			absExpected, _ := filepath.Abs(expected)
			if target != absExpected {
				t.Errorf("expected %q, got %q", absExpected, target)
			}
		})
	}
}

// TestSafeExtractPath_ParentTraversal verifies that paths attempting
// parent-directory traversal are rejected.
func TestSafeExtractPath_ParentTraversal(t *testing.T) {
	destDir := t.TempDir()

	tests := []struct {
		name      string
		entryName string
	}{
		{"direct parent", "../etc/passwd"},
		{"deep traversal", "../../../../etc/passwd"},
		{"nested traversal", "sub/../../../etc/passwd"},
		{"mixed safe then traversal", "app/../../etc/passwd"},
		{"leading traversal", "../outside"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := safeExtractPath(destDir, tt.entryName)
			if err == nil {
				t.Errorf("safeExtractPath(%q, %q) should have returned error for path traversal", destDir, tt.entryName)
			}
		})
	}
}

// TestSafeExtractPath_AbsolutePath verifies that entry names with absolute
// paths are rejected.
func TestSafeExtractPath_AbsolutePath(t *testing.T) {
	destDir := t.TempDir()

	tests := []struct {
		name      string
		entryName string
	}{
		{"unix absolute", "/etc/passwd"},
		{"unix absolute nested", "/etc/../etc/shadow"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := safeExtractPath(destDir, tt.entryName)
			if err == nil {
				t.Errorf("safeExtractPath(%q, %q) should have returned error for absolute path", destDir, tt.entryName)
			}
		})
	}
}

// TestSafeExtractPath_EmptyEntry verifies that an empty entry name is rejected.
func TestSafeExtractPath_EmptyEntry(t *testing.T) {
	destDir := t.TempDir()
	_, err := safeExtractPath(destDir, "")
	if err == nil {
		t.Error("safeExtractPath should reject empty entry name")
	}
}

// TestSafeExtractPath_EntryResolvesToRoot verifies that an entry name that
// resolves to the extraction root itself is rejected.
func TestSafeExtractPath_EntryResolvesToRoot(t *testing.T) {
	destDir := t.TempDir()

	tests := []struct {
		name      string
		entryName string
	}{
		{"dot", "."},
		{"cleaned dot", "foo/.."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := safeExtractPath(destDir, tt.entryName)
			if err == nil {
				t.Errorf("safeExtractPath(%q, %q) should have returned error for root-resolving entry", destDir, tt.entryName)
			}
		})
	}
}

// --- extractDeployableContent security tests ---

// createMaliciousArtifact creates a tar.gz archive with a single entry
// having the given entryName (under DeployableContentDir) and content.
// This helper allows creating archives with unsafe entries for testing.
func createMaliciousArtifact(t *testing.T, path string, entryName string, content string) {
	t.Helper()

	var buf bytes.Buffer
	gzW := gzip.NewWriter(&buf)
	tarW := tar.NewWriter(gzW)

	hdr := &tar.Header{
		Name:     entryName,
		Size:     int64(len(content)),
		Mode:     0644,
		Typeflag: tar.TypeReg,
	}
	if err := tarW.WriteHeader(hdr); err != nil {
		t.Fatalf("write tar header for %s: %v", entryName, err)
	}
	if _, err := tarW.Write([]byte(content)); err != nil {
		t.Fatalf("write tar content: %v", err)
	}

	if err := tarW.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gzW.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write artifact file: %v", err)
	}
}

// TestExtractDeployableContent_AbsolutePathEntry verifies that an archive
// entry with an absolute path is rejected during extraction.
func TestExtractDeployableContent_AbsolutePathEntry(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "absolute-path.tar.gz")

	// Create an archive with an entry outside DeployableContentDir
	// using an absolute path. The entry name does not have the "app/"
	// prefix, so it should be skipped by the prefix check, not rejected.
	// This test verifies the entry does not cause an error.
	createMaliciousArtifact(t, artifactPath, "/etc/passwd", "malicious")

	destDir := t.TempDir()
	_, err := extractDeployableContent(artifactPath, destDir)
	if err != nil {
		t.Fatalf("absolute path entry outside prefix should be skipped, got error: %v", err)
	}
}

// TestExtractDeployableContent_AbsolutePathUnderPrefix verifies that an
// archive entry with an absolute path under the DeployableContentDir prefix
// is rejected. This entry starts with "app/" but includes an absolute path
// (which is technically impossible since absolute paths start with "/"),
// so this tests the edge case where the prefix plus absolute path could
// confuse the validator.
func TestExtractDeployableContent_AbsolutePathUnderPrefix(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "prefix-absolute.tar.gz")

	// An entry like "app//etc/passwd" has the "app/" prefix but the
	// resolved path would be "/etc/passwd" after path cleaning.
	createMaliciousArtifact(t, artifactPath, DeployableContentDir+"/../etc/passwd", "malicious")

	destDir := t.TempDir()
	_, err := extractDeployableContent(artifactPath, destDir)
	if err == nil {
		t.Error("expected error for parent-traversal entry under DeployableContentDir prefix, got nil")
	} else if !strings.Contains(err.Error(), "unsafe entry") {
		t.Errorf("error should mention unsafe entry, got: %v", err)
	}
}

// TestExtractDeployableContent_ParentTraversal verifies that an archive entry
// with parent-directory traversal is rejected during extraction.
func TestExtractDeployableContent_ParentTraversal(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "traversal.tar.gz")

	// Entry with parent traversal under DeployableContentDir.
	entryName := DeployableContentDir + "/../../etc/passwd"
	createMaliciousArtifact(t, artifactPath, entryName, "malicious")

	destDir := t.TempDir()
	_, err := extractDeployableContent(artifactPath, destDir)
	if err == nil {
		t.Error("expected error for parent-traversal entry, got nil")
	} else if !strings.Contains(err.Error(), "unsafe entry") {
		t.Errorf("error should mention unsafe entry, got: %v", err)
	}
}

// TestExtractDeployableContent_DeepParentTraversal verifies that a deeply
// nested parent-traversal entry is rejected.
func TestExtractDeployableContent_DeepParentTraversal(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "deep-traversal.tar.gz")

	entryName := DeployableContentDir + "/a/b/../../../../etc/shadow"
	createMaliciousArtifact(t, artifactPath, entryName, "malicious")

	destDir := t.TempDir()
	_, err := extractDeployableContent(artifactPath, destDir)
	if err == nil {
		t.Error("expected error for deep parent-traversal entry, got nil")
	}
}

// TestExtractDeployableContent_UnsafeSymlink verifies that an archive entry
// with a symlink pointing outside the extraction root is rejected.
func TestExtractDeployableContent_UnsafeSymlink(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "unsafe-symlink.tar.gz")

	var buf bytes.Buffer
	gzW := gzip.NewWriter(&buf)
	tarW := tar.NewWriter(gzW)

	// Add a regular file first.
	content := "safe content"
	hdr := &tar.Header{
		Name:     filepath.Join(DeployableContentDir, "safe.txt"),
		Size:     int64(len(content)),
		Mode:     0644,
		Typeflag: tar.TypeReg,
	}
	if err := tarW.WriteHeader(hdr); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tarW.Write([]byte(content)); err != nil {
		t.Fatalf("write tar content: %v", err)
	}

	// Add a symlink pointing outside the extraction root.
	symlink := &tar.Header{
		Name:     filepath.Join(DeployableContentDir, "evil.link"),
		Linkname: "/etc/passwd",
		Mode:     0644,
		Typeflag: tar.TypeSymlink,
	}
	if err := tarW.WriteHeader(symlink); err != nil {
		t.Fatalf("write symlink header: %v", err)
	}

	if err := tarW.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gzW.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	if err := os.WriteFile(artifactPath, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	destDir := t.TempDir()
	_, err := extractDeployableContent(artifactPath, destDir)
	if err == nil {
		t.Error("expected error for unsafe symlink, got nil")
	} else if !strings.Contains(err.Error(), "unsafe link target") {
		t.Errorf("error should mention unsafe link target, got: %v", err)
	}
}

// TestExtractDeployableContent_UnsafeHardlink verifies that an archive entry
// with a hardlink pointing outside the extraction root is rejected.
func TestExtractDeployableContent_UnsafeHardlink(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "unsafe-hardlink.tar.gz")

	var buf bytes.Buffer
	gzW := gzip.NewWriter(&buf)
	tarW := tar.NewWriter(gzW)

	// Add a regular file first.
	content := "safe content"
	hdr := &tar.Header{
		Name:     filepath.Join(DeployableContentDir, "safe.txt"),
		Size:     int64(len(content)),
		Mode:     0644,
		Typeflag: tar.TypeReg,
	}
	if err := tarW.WriteHeader(hdr); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tarW.Write([]byte(content)); err != nil {
		t.Fatalf("write tar content: %v", err)
	}

	// Add a hardlink pointing outside the extraction root.
	link := &tar.Header{
		Name:     filepath.Join(DeployableContentDir, "evil.link"),
		Linkname: "/etc/passwd",
		Mode:     0644,
		Typeflag: tar.TypeLink,
	}
	if err := tarW.WriteHeader(link); err != nil {
		t.Fatalf("write hardlink header: %v", err)
	}

	if err := tarW.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gzW.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	if err := os.WriteFile(artifactPath, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	destDir := t.TempDir()
	_, err := extractDeployableContent(artifactPath, destDir)
	if err == nil {
		t.Error("expected error for unsafe hardlink, got nil")
	} else if !strings.Contains(err.Error(), "unsafe link target") {
		t.Errorf("error should mention unsafe link target, got: %v", err)
	}
}

// TestExtractDeployableContent_SafeSymlink verifies that a symlink pointing
// within the extraction root is accepted (validated and skipped).
func TestExtractDeployableContent_SafeSymlink(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "safe-symlink.tar.gz")

	var buf bytes.Buffer
	gzW := gzip.NewWriter(&buf)
	tarW := tar.NewWriter(gzW)

	// Add a regular file.
	content := "safe content"
	hdr := &tar.Header{
		Name:     filepath.Join(DeployableContentDir, "safe.txt"),
		Size:     int64(len(content)),
		Mode:     0644,
		Typeflag: tar.TypeReg,
	}
	if err := tarW.WriteHeader(hdr); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tarW.Write([]byte(content)); err != nil {
		t.Fatalf("write tar content: %v", err)
	}

	// Add a symlink pointing to a file within the extraction root.
	// The link target is a file that would be extracted alongside it.
	symlink := &tar.Header{
		Name:     filepath.Join(DeployableContentDir, "link-to-safe.link"),
		Linkname: "safe.txt",
		Mode:     0644,
		Typeflag: tar.TypeSymlink,
	}
	if err := tarW.WriteHeader(symlink); err != nil {
		t.Fatalf("write symlink header: %v", err)
	}

	if err := tarW.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gzW.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	if err := os.WriteFile(artifactPath, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	destDir := t.TempDir()
	_, err := extractDeployableContent(artifactPath, destDir)
	if err != nil {
		t.Fatalf("safe symlink should not return error, got: %v", err)
	}
}

// TestExtractDeployableContent_SafeNestedPaths verifies that deeply nested
// safe paths are extracted correctly after path-boundary hardening.
func TestExtractDeployableContent_SafeNestedPaths(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "nested-safe.tar.gz")

	// Create an artifact with nested safe paths.
	manifest := completeManifest()
	deployable := map[string]string{
		"index.php":                      "<?php\n",
		"src/App.php":                    "<?php\nclass App {}\n",
		"src/Controller/Foo.php":         "<?php\nclass Foo {}\n",
		"config/app.php":                 "<?php\nreturn [];\n",
		"resources/views/home.blade.php": "<html></html>",
	}

	createTestArtifact(t, artifactPath, manifest, deployable)

	// Compute correct checksum.
	tmpExtract := t.TempDir()
	files, err := extractDeployableContent(artifactPath, tmpExtract)
	if err != nil {
		t.Fatalf("extract deployable content: %v", err)
	}

	if len(files) != len(deployable) {
		t.Errorf("expected %d files, got %d", len(deployable), len(files))
	}

	// Verify each expected file exists.
	for relPath := range deployable {
		fullPath := filepath.Join(tmpExtract, relPath)
		if _, err := os.Stat(fullPath); err != nil {
			t.Errorf("expected file %s to exist: %v", relPath, err)
		}
	}
}

// TestExtractDeployableContent_NoPartialOutputOnFailure verifies that when
// extraction fails due to an unsafe entry, no files from earlier entries
// remain in the output directory.
func TestExtractDeployableContent_NoPartialOutputOnFailure(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "partial-fail.tar.gz")

	var buf bytes.Buffer
	gzW := gzip.NewWriter(&buf)
	tarW := tar.NewWriter(gzW)

	// Add a safe file first.
	content := "safe content"
	hdr := &tar.Header{
		Name:     filepath.Join(DeployableContentDir, "safe.txt"),
		Size:     int64(len(content)),
		Mode:     0644,
		Typeflag: tar.TypeReg,
	}
	if err := tarW.WriteHeader(hdr); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tarW.Write([]byte(content)); err != nil {
		t.Fatalf("write tar content: %v", err)
	}

	// Add an unsafe entry with parent traversal.
	evilContent := "evil"
	evilHdr := &tar.Header{
		Name:     DeployableContentDir + "/../../evil.txt",
		Size:     int64(len(evilContent)),
		Mode:     0644,
		Typeflag: tar.TypeReg,
	}
	if err := tarW.WriteHeader(evilHdr); err != nil {
		t.Fatalf("write evil tar header: %v", err)
	}
	if _, err := tarW.Write([]byte(evilContent)); err != nil {
		t.Fatalf("write evil tar content: %v", err)
	}

	if err := tarW.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gzW.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	if err := os.WriteFile(artifactPath, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	destDir := t.TempDir()
	_, err := extractDeployableContent(artifactPath, destDir)
	if err == nil {
		t.Fatal("expected error for unsafe entry, got nil")
	}

	// Verify no files were extracted (safe.txt should not exist).
	if entries, _ := os.ReadDir(destDir); len(entries) > 0 {
		t.Error("expected no files in destDir after failed extraction, but files were found")
	}
}
