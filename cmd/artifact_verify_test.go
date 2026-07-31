// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P3-05, EPIC-003
package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/artifact"
)

// createValidArtifact creates a valid artifact at the given path for testing.
// Returns the path to the artifact.
func createValidArtifact(t *testing.T, path string) {
	t.Helper()

	// Create deployable content.
	content := "<?php\n"
	entryName := filepath.Join(artifact.DeployableContentDir, "index.php")

	// Build manifest with a pre-computed checksum.
	manifest := artifact.Manifest{
		ArtifactID:   "test-id-123",
		Version:      "1.0.0",
		CreatedAt:    "2026-07-25T12:00:00Z",
		Source:       "test-project",
		Checksum:     "placeholder",
		ChecksumType: artifact.ChecksumAlgorithmSHA256,
		ProjectID:    "test-project",
	}

	// Write the archive.
	var buf bytes.Buffer
	gzW := gzip.NewWriter(&buf)
	tarW := tar.NewWriter(gzW)

	// Deployable file.
	hdr := &tar.Header{
		Name:     entryName,
		Size:     int64(len(content)),
		Mode:     0644,
		Typeflag: tar.TypeReg,
	}
	if err := tarW.WriteHeader(hdr); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tarW.Write([]byte(content)); err != nil {
		t.Fatalf("write content: %v", err)
	}

	// Manifest.
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	mHdr := &tar.Header{
		Name:     artifact.ManifestFile,
		Size:     int64(len(manifestBytes)),
		Mode:     0644,
		Typeflag: tar.TypeReg,
	}
	if err := tarW.WriteHeader(mHdr); err != nil {
		t.Fatalf("write manifest header: %v", err)
	}
	if _, err := tarW.Write(manifestBytes); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if err := tarW.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gzW.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	// Now compute the correct checksum.
	tmpDir := t.TempDir()
	extractFile := filepath.Join(tmpDir, "index.php")
	if err := os.WriteFile(extractFile, []byte(content), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	cs, err := artifact.ComputeChecksum(tmpDir, []string{"index.php"})
	if err != nil {
		t.Fatalf("compute checksum: %v", err)
	}

	// Recreate with the correct checksum.
	manifest.Checksum = cs
	manifestBytes, err = json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}

	var buf2 bytes.Buffer
	gzW2 := gzip.NewWriter(&buf2)
	tarW2 := tar.NewWriter(gzW2)

	hdr2 := &tar.Header{
		Name:     entryName,
		Size:     int64(len(content)),
		Mode:     0644,
		Typeflag: tar.TypeReg,
	}
	if err := tarW2.WriteHeader(hdr2); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tarW2.Write([]byte(content)); err != nil {
		t.Fatalf("write content: %v", err)
	}

	mHdr2 := &tar.Header{
		Name:     artifact.ManifestFile,
		Size:     int64(len(manifestBytes)),
		Mode:     0644,
		Typeflag: tar.TypeReg,
	}
	if err := tarW2.WriteHeader(mHdr2); err != nil {
		t.Fatalf("write manifest header: %v", err)
	}
	if _, err := tarW2.Write(manifestBytes); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if err := tarW2.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gzW2.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := os.WriteFile(path, buf2.Bytes(), 0644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
}

// createCorruptedArtifact creates an invalid file at the given path.
func createCorruptedArtifact(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("not-a-valid-archive"), 0644); err != nil {
		t.Fatalf("write corrupted file: %v", err)
	}
}

// TestVerifyCmd_Registered verifies that the verify subcommand is registered
// under the artifact command.
func TestVerifyCmd_Registered(t *testing.T) {
	var artifactSub *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Use == "artifact" {
			artifactSub = c
			break
		}
	}

	if artifactSub == nil {
		t.Fatal("artifact command not found")
	}

	found := false
	for _, c := range artifactSub.Commands() {
		if c.Use == "verify <artifact-path>" {
			found = true
			break
		}
	}

	if !found {
		t.Error("verify subcommand not found under artifact command")
	}
}

// TestVerifyCmd_Usage verifies the verify command has the expected usage.
func TestVerifyCmd_Usage(t *testing.T) {
	if verifyCmd.Short == "" {
		t.Error("verify command short description is empty")
	}

	if verifyCmd.Long == "" {
		t.Error("verify command long description is empty")
	}

	if verifyCmd.Use != "verify <artifact-path>" {
		t.Errorf("verify command Use = %q, want %q", verifyCmd.Use, "verify <artifact-path>")
	}
}

// TestVerifyCmd_RunE verifies the verify command has a RunE handler set.
func TestVerifyCmd_RunE(t *testing.T) {
	if verifyCmd.RunE == nil {
		t.Error("verify command RunE handler is nil")
	}
}

// TestVerifyCmd_ExactArgs verifies the verify command requires exactly 1 arg.
func TestVerifyCmd_ExactArgs(t *testing.T) {
	if verifyCmd.Args == nil {
		t.Error("verify command Args validator is nil, expected cobra.ExactArgs(1)")
		return
	}

	cmd := &cobra.Command{Use: "verify"}

	// 0 args should fail.
	err := verifyCmd.Args(cmd, []string{})
	if err == nil {
		t.Error("expected error for 0 arguments, got nil")
	}

	// 1 arg should pass.
	err = verifyCmd.Args(cmd, []string{"some-file.tar.gz"})
	if err != nil {
		t.Errorf("expected no error for 1 argument, got: %v", err)
	}

	// 2 args should fail.
	err = verifyCmd.Args(cmd, []string{"a.tar.gz", "b.tar.gz"})
	if err == nil {
		t.Error("expected error for 2 arguments, got nil")
	}
}

// TestVerifyCmd_PassOutput verifies that a valid artifact produces the
// expected pass output.
func TestVerifyCmd_PassOutput(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "test-artifact.tar.gz")
	createValidArtifact(t, artifactPath)

	_, stdout, _, err := executeCommand("artifact", "verify", artifactPath)
	if err != nil {
		t.Fatalf("execute command returned error: %v", err)
	}

	if !strings.Contains(stdout, "Artifact verification: PASSED") {
		t.Errorf("expected PASSED in output, got:\n%s", stdout)
	}

	// Should have check marks for all 6 checks.
	expectedChecks := []string{
		"Archive validity",
		"Manifest presence",
		"Manifest content",
		"Project identity",
		"Checksum match",
	}
	for _, name := range expectedChecks {
		if !strings.Contains(stdout, name) {
			t.Errorf("expected check %q in output, got:\n%s", name, stdout)
		}
	}

	// Each line should start with [PASS] for passing checks.
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	for _, line := range lines[1:] { // Skip the header line
		if !strings.HasPrefix(line, "[PASS]") {
			t.Errorf("expected check line to start with [PASS], got: %s", line)
		}
	}
}

// TestVerifyCmd_FailOutput verifies that a corrupted artifact produces the
// expected failure output.
func TestVerifyCmd_FailOutput(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "corrupted.tar.gz")
	createCorruptedArtifact(t, artifactPath)

	_, stdout, _, err := executeCommand("artifact", "verify", artifactPath)

	// The command should return an error for failed verification.
	if err == nil {
		t.Error("expected error for failed verification, got nil")
	}

	if !strings.Contains(stdout, "Artifact verification: FAILED") {
		t.Errorf("expected FAILED in output, got:\n%s", stdout)
	}

	// Should have failed check with [FAIL].
	if !strings.Contains(stdout, "[FAIL]") {
		t.Errorf("expected [FAIL] for failed check, got:\n%s", stdout)
	}
}

// TestVerifyCmd_NonExistentFile verifies that a non-existent file produces
// an appropriate error.
func TestVerifyCmd_NonExistentFile(t *testing.T) {
	_, _, stderr, err := executeCommand("artifact", "verify", "/tmp/nonexistent-file-98765.tar.gz")

	if err == nil {
		t.Error("expected error for non-existent file, got nil")
	}

	if !strings.Contains(stderr, "Error:") {
		t.Errorf("expected error message in stderr, got:\n%s", stderr)
	}
}

// TestVerifyCmd_NoArgs verifies that running verify without args shows usage.
func TestVerifyCmd_NoArgs(t *testing.T) {
	_, _, stderr, err := executeCommand("artifact", "verify")

	if err == nil {
		t.Error("expected error when no args provided")
	}

	if !strings.Contains(stderr, "Error:") {
		t.Errorf("expected error in stderr, got:\n%s", stderr)
	}
}

// TestVerifyCmd_ExtraArgs verifies that running verify with >1 args fails.
func TestVerifyCmd_ExtraArgs(t *testing.T) {
	_, _, stderr, err := executeCommand("artifact", "verify", "a.tar.gz", "b.tar.gz")

	if err == nil {
		t.Error("expected error when extra args provided")
	}

	if !strings.Contains(stderr, "Error:") {
		t.Errorf("expected error in stderr, got:\n%s", stderr)
	}
}

// TestVerifyCmd_FailOutputFormat verifies that failure output lists each
// failed check with detailed reasons.
func TestVerifyCmd_FailOutputFormat(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "bad-manifest.tar.gz")

	// Create an archive that is valid gzip but has a manifest with missing fields.
	var buf bytes.Buffer
	gzW := gzip.NewWriter(&buf)
	tarW := tar.NewWriter(gzW)

	content := "<?php\n"
	hdr := &tar.Header{
		Name:     filepath.Join(artifact.DeployableContentDir, "index.php"),
		Size:     int64(len(content)),
		Mode:     0644,
		Typeflag: tar.TypeReg,
	}
	if err := tarW.WriteHeader(hdr); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tarW.Write([]byte(content)); err != nil {
		t.Fatalf("write content: %v", err)
	}

	// Manifest with missing fields.
	manifest := artifact.Manifest{
		ArtifactID:   "test-id",
		Version:      "",
		CreatedAt:    "",
		Source:       "test",
		Checksum:     "",
		ChecksumType: "",
	}
	manifestBytes, _ := json.Marshal(manifest)
	mHdr := &tar.Header{
		Name:     artifact.ManifestFile,
		Size:     int64(len(manifestBytes)),
		Mode:     0644,
		Typeflag: tar.TypeReg,
	}
	if err := tarW.WriteHeader(mHdr); err != nil {
		t.Fatalf("write manifest header: %v", err)
	}
	if _, err := tarW.Write(manifestBytes); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if err := tarW.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gzW.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := os.WriteFile(artifactPath, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	_, stdout, _, err := executeCommand("artifact", "verify", artifactPath)
	if err == nil {
		t.Error("expected error for bad manifest")
	}

	if !strings.Contains(stdout, "Artifact verification: FAILED") {
		t.Errorf("expected FAILED header, got:\n%s", stdout)
	}

	// Should contain details about missing fields.
	if !strings.Contains(stdout, "Manifest content") {
		t.Errorf("expected Manifest content check in output, got:\n%s", stdout)
	}

	// Should also fail checksum because manifest checksum is empty.
	if !strings.Contains(stdout, "Checksum match") {
		t.Errorf("expected Checksum match check in output, got:\n%s", stdout)
	}
}
