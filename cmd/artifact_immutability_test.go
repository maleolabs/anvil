// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P3-07, EPIC-003
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

// createTamperedArtifact creates an artifact at the given path where the
// manifest checksum does not match the actual deployable content. This
// simulates an artifact that has been modified since creation.
func createTamperedArtifact(t *testing.T, path string) {
	t.Helper()

	content := "<?php\n"
	entryName := filepath.Join(artifact.DeployableContentDir, "index.php")

	// Build a manifest with a deliberately wrong checksum.
	manifest := artifact.Manifest{
		ArtifactID:   "test-id-456",
		Version:      "1.0.0",
		CreatedAt:    "2026-07-25T12:00:00Z",
		Source:       "test-project",
		Checksum:     "wrong-checksum-that-does-not-match",
		ChecksumType: artifact.ChecksumAlgorithmSHA256,
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

	// Manifest with wrong checksum.
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
}

// TestVerifyImmutabilityCmd_Registered verifies that the verify-immutability
// subcommand is registered under the artifact command.
func TestVerifyImmutabilityCmd_Registered(t *testing.T) {
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
		if c.Use == "verify-immutability <artifact-path>" {
			found = true
			break
		}
	}

	if !found {
		t.Error("verify-immutability subcommand not found under artifact command")
	}
}

// TestVerifyImmutabilityCmd_Usage verifies the verify-immutability command
// has the expected usage.
func TestVerifyImmutabilityCmd_Usage(t *testing.T) {
	if verifyImmutabilityCmd.Short == "" {
		t.Error("verify-immutability command short description is empty")
	}

	if verifyImmutabilityCmd.Long == "" {
		t.Error("verify-immutability command long description is empty")
	}

	if verifyImmutabilityCmd.Use != "verify-immutability <artifact-path>" {
		t.Errorf("verify-immutability command Use = %q, want %q",
			verifyImmutabilityCmd.Use, "verify-immutability <artifact-path>")
	}
}

// TestVerifyImmutabilityCmd_RunE verifies the verify-immutability command
// has a RunE handler set.
func TestVerifyImmutabilityCmd_RunE(t *testing.T) {
	if verifyImmutabilityCmd.RunE == nil {
		t.Error("verify-immutability command RunE handler is nil")
	}
}

// TestVerifyImmutabilityCmd_ExactArgs verifies the verify-immutability command
// requires exactly 1 argument.
func TestVerifyImmutabilityCmd_ExactArgs(t *testing.T) {
	if verifyImmutabilityCmd.Args == nil {
		t.Error("verify-immutability command Args validator is nil, expected cobra.ExactArgs(1)")
		return
	}

	cmd := &cobra.Command{Use: "verify-immutability"}

	// 0 args should fail.
	err := verifyImmutabilityCmd.Args(cmd, []string{})
	if err == nil {
		t.Error("expected error for 0 arguments, got nil")
	}

	// 1 arg should pass.
	err = verifyImmutabilityCmd.Args(cmd, []string{"some-file.tar.gz"})
	if err != nil {
		t.Errorf("expected no error for 1 argument, got: %v", err)
	}

	// 2 args should fail.
	err = verifyImmutabilityCmd.Args(cmd, []string{"a.tar.gz", "b.tar.gz"})
	if err == nil {
		t.Error("expected error for 2 arguments, got nil")
	}
}

// TestVerifyImmutabilityCmd_Success verifies that a valid artifact produces
// the expected pass output.
func TestVerifyImmutabilityCmd_Success(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "test-artifact.tar.gz")
	createValidArtifact(t, artifactPath)

	_, stdout, _, err := executeCommand("artifact", "verify-immutability", artifactPath)
	if err != nil {
		t.Fatalf("execute command returned error: %v", err)
	}

	if !strings.Contains(stdout, "Immutability verification: PASSED") {
		t.Errorf("expected PASSED in output, got:\n%s", stdout)
	}

	if !strings.Contains(stdout, "[PASS] Checksum:") {
		t.Errorf("expected checksum line in output, got:\n%s", stdout)
	}
}

// TestVerifyImmutabilityCmd_Failure verifies that an artifact with a
// mismatched checksum produces the expected failure output.
func TestVerifyImmutabilityCmd_Failure(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "tampered-artifact.tar.gz")
	createTamperedArtifact(t, artifactPath)

	_, stdout, _, err := executeCommand("artifact", "verify-immutability", artifactPath)

	// The command should return an error for failed verification.
	if err == nil {
		t.Error("expected error for failed immutability verification, got nil")
	}

	if !strings.Contains(stdout, "Immutability verification: FAILED") {
		t.Errorf("expected FAILED in output, got:\n%s", stdout)
	}

	if !strings.Contains(stdout, "[FAIL] Original checksum:") {
		t.Errorf("expected original checksum line in output, got:\n%s", stdout)
	}

	if !strings.Contains(stdout, "[FAIL] Current checksum:") {
		t.Errorf("expected current checksum line in output, got:\n%s", stdout)
	}
}

// TestVerifyImmutabilityCmd_NonExistentFile verifies that a non-existent file
// produces an appropriate error.
func TestVerifyImmutabilityCmd_NonExistentFile(t *testing.T) {
	_, _, stderr, err := executeCommand("artifact", "verify-immutability", "/tmp/nonexistent-file-98765.tar.gz")

	if err == nil {
		t.Error("expected error for non-existent file, got nil")
	}

	if !strings.Contains(stderr, "Error:") {
		t.Errorf("expected error message in stderr, got:\n%s", stderr)
	}
}

// TestVerifyImmutabilityCmd_NoArgs verifies that running
// verify-immutability without args shows usage error.
func TestVerifyImmutabilityCmd_NoArgs(t *testing.T) {
	_, _, stderr, err := executeCommand("artifact", "verify-immutability")

	if err == nil {
		t.Error("expected error when no args provided")
	}

	if !strings.Contains(stderr, "Error:") {
		t.Errorf("expected error in stderr, got:\n%s", stderr)
	}
}

// TestVerifyImmutabilityCmd_ExtraArgs verifies that running
// verify-immutability with >1 args fails.
func TestVerifyImmutabilityCmd_ExtraArgs(t *testing.T) {
	_, _, stderr, err := executeCommand("artifact", "verify-immutability", "a.tar.gz", "b.tar.gz")

	if err == nil {
		t.Error("expected error when extra args provided")
	}

	if !strings.Contains(stderr, "Error:") {
		t.Errorf("expected error in stderr, got:\n%s", stderr)
	}
}
