// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P3-05, EPIC-003
package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/contracts"
	"maleolabs.com/anvil/internal/execution"
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
		trimmed := strings.TrimLeft(line, " ")
		if trimmed == "" || !strings.HasPrefix(trimmed, "[PASS]") {
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

// ── Framework Adapter Verification (ST-007-004) ─────────────────────

// verificationStubRunner is a fake execution.Runner that answers the
// adapter commands dispatched by `anvil artifact verify` — the
// capabilities command (registration, TS-P7-07) and the verify command
// (per-check invocation, TS-P7-08) — with canned JSON documents. It
// records every verification invocation (check name and payload) so
// tests can assert the adapter was consulted and with which request.
type verificationStubRunner struct {
	capabilitiesJSON string            // stdout for the capabilities command
	verificationJSON map[string]string // check name -> VerificationOutcome JSON
	invokedChecks    []string          // recorded verification check names
	invokedPayloads  []string          // recorded verification request payloads
}

// Execute dispatches on the command name in req.Args[0], mirroring the
// command contract (005-adapter-command-contract §10). Unknown commands
// fail like a non-conforming adapter.
func (r *verificationStubRunner) Execute(ctx context.Context, req execution.ExecutionRequest) execution.Result {
	if len(req.Args) == 0 {
		return execution.Result{Status: execution.StatusFailure, Stderr: "missing command"}
	}
	switch req.Args[0] {
	case contracts.CommandCapabilities:
		return execution.Result{Status: execution.StatusSuccess, Stdout: r.capabilitiesJSON}
	case contracts.CommandVerification:
		var payload contracts.VerificationRequest
		if len(req.Args) > 1 {
			if err := json.Unmarshal([]byte(req.Args[1]), &payload); err != nil {
				return execution.Result{Status: execution.StatusFailure, Stderr: "invalid verification payload"}
			}
		}
		r.invokedChecks = append(r.invokedChecks, payload.Check)
		r.invokedPayloads = append(r.invokedPayloads, req.Args[1])
		outcome, ok := r.verificationJSON[payload.Check]
		if !ok {
			return execution.Result{Status: execution.StatusFailure, Stderr: "no canned outcome for check"}
		}
		return execution.Result{Status: execution.StatusSuccess, Stdout: outcome}
	default:
		return execution.Result{Status: execution.StatusFailure, Stderr: "unexpected command " + req.Args[0]}
	}
}

// stubVerificationRunner replaces adapterRunnerFactory with runner for
// the duration of the test and registers cleanup.
func stubVerificationRunner(t *testing.T, runner *verificationStubRunner) {
	t.Helper()
	orig := adapterRunnerFactory
	adapterRunnerFactory = func() execution.Runner { return runner }
	t.Cleanup(func() { adapterRunnerFactory = orig })
}

// verificationCapabilitiesFixture returns the CapabilityResult JSON the
// fake adapter returns for the capabilities command, declaring the given
// checks.
func verificationCapabilitiesFixture(checks []string) string {
	decl := contracts.CapabilityDeclaration{}
	for _, name := range checks {
		decl.VerificationChecks = append(decl.VerificationChecks, contracts.VerificationCheck{
			Name:        name,
			Description: "validates " + name,
		})
	}
	data, err := json.Marshal(contracts.CapabilityResult{Declaration: decl})
	if err != nil {
		panic(err)
	}
	return string(data)
}

// verificationOutcomeFixture returns the VerificationOutcome JSON the
// fake adapter returns for one check invocation.
func verificationOutcomeFixture(name string, passed bool, details string) string {
	data, err := json.Marshal(contracts.VerificationOutcome{Name: name, Passed: passed, Details: details})
	if err != nil {
		panic(err)
	}
	return string(data)
}

// TestVerifyCmd_NoFrameworkSkipsAdapter verifies the pre-existing
// behavior (ST-007-004 backward compatibility): a project without
// project.framework runs only the generic checks — the adapter lookup is
// never consulted and no framework section appears in the output.
func TestVerifyCmd_NoFrameworkSkipsAdapter(t *testing.T) {
	dir := setupPackageProject(t, "")
	artifactPath := filepath.Join(dir, "test-artifact.tar.gz")
	createValidArtifact(t, artifactPath)

	// A failing lookup proves the adapter is not consulted at all.
	orig := adapterExecutableLookup
	adapterExecutableLookup = func(name string) (string, error) {
		t.Fatalf("adapter lookup called for %q with no framework", name)
		return "", os.ErrNotExist
	}
	t.Cleanup(func() { adapterExecutableLookup = orig })

	_, stdout, stderr, err := executeCommand("artifact", "verify", artifactPath)
	if err != nil {
		t.Fatalf("execute command returned error: %v", err)
	}
	if !strings.Contains(stdout, "Artifact verification: PASSED") {
		t.Errorf("expected PASSED in output, got:\n%s", stdout)
	}
	if strings.Contains(stdout, "Framework verification") {
		t.Errorf("expected no framework verification section, got:\n%s", stdout)
	}
	if stderr != "" {
		t.Errorf("expected no warnings on stderr, got: %s", stderr)
	}
}

// TestVerifyCmd_FrameworkChecksPass verifies that with a framework and a
// working adapter, every declared verification check runs after the
// generic checks and all-pass output shows one [PASS] line per check
// under a "Framework verification:" section.
func TestVerifyCmd_FrameworkChecksPass(t *testing.T) {
	dir := setupPackageProject(t, "laravel")
	artifactPath := filepath.Join(dir, "test-artifact.tar.gz")
	createValidArtifact(t, artifactPath)

	// The stub adapter executable only needs to exist for the lookup
	// seam; the fake runner below never executes it.
	stubAdapterLookup(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "anvil-adapter-laravel"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write stub adapter placeholder: %v", err)
	}

	runner := &verificationStubRunner{
		capabilitiesJSON: verificationCapabilitiesFixture([]string{"vendor_present", "config_files"}),
		verificationJSON: map[string]string{
			"vendor_present": verificationOutcomeFixture("vendor_present", true, "vendor/autoload.php exists"),
			"config_files":   verificationOutcomeFixture("config_files", true, "config/app.php and .env.example exist"),
		},
	}
	stubVerificationRunner(t, runner)

	_, stdout, stderr, err := executeCommand("artifact", "verify", artifactPath)
	if err != nil {
		t.Fatalf("execute command returned error: %v", err)
	}

	// Generic section first, then the framework section.
	if !strings.Contains(stdout, "Artifact verification: PASSED") {
		t.Errorf("expected PASSED header, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Framework verification:") {
		t.Errorf("expected framework verification section, got:\n%s", stdout)
	}
	for _, want := range []string{
		"[PASS] vendor_present: vendor/autoload.php exists",
		"[PASS] config_files: config/app.php and .env.example exist",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected %q in output, got:\n%s", want, stdout)
		}
	}
	// The framework section must come after the generic checks.
	genericIdx := strings.Index(stdout, "Checksum match")
	frameworkIdx := strings.Index(stdout, "Framework verification:")
	if genericIdx == -1 || frameworkIdx == -1 || genericIdx > frameworkIdx {
		t.Errorf("framework section must appear after generic checks, got:\n%s", stdout)
	}

	// Every declared check was invoked exactly once, carrying the path.
	if len(runner.invokedChecks) != 2 {
		t.Fatalf("invoked verification checks = %v, want [vendor_present config_files]", runner.invokedChecks)
	}
	if runner.invokedChecks[0] != "vendor_present" || runner.invokedChecks[1] != "config_files" {
		t.Errorf("invoked verification checks = %v, want [vendor_present config_files]", runner.invokedChecks)
	}
	for _, payload := range runner.invokedPayloads {
		if !strings.Contains(payload, `"artifact_path":"`+artifactPath+`"`) {
			t.Errorf("verification payload %q does not carry artifact_path %q", payload, artifactPath)
		}
	}

	if stderr != "" {
		t.Errorf("expected no warnings on stderr, got: %s", stderr)
	}
}

// TestVerifyCmd_FrameworkCheckFailure verifies that a failing declared
// check fails the whole verification: the [FAIL] line with details is
// printed and the command returns the "artifact verification failed"
// error (non-zero exit), while passing checks are still reported.
func TestVerifyCmd_FrameworkCheckFailure(t *testing.T) {
	dir := setupPackageProject(t, "laravel")
	artifactPath := filepath.Join(dir, "test-artifact.tar.gz")
	createValidArtifact(t, artifactPath)

	stubAdapterLookup(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "anvil-adapter-laravel"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write stub adapter placeholder: %v", err)
	}

	runner := &verificationStubRunner{
		capabilitiesJSON: verificationCapabilitiesFixture([]string{"vendor_present", "config_files"}),
		verificationJSON: map[string]string{
			"vendor_present": verificationOutcomeFixture("vendor_present", true, "vendor/autoload.php exists"),
			"config_files":   verificationOutcomeFixture("config_files", false, "missing required file(s): config/app.php"),
		},
	}
	stubVerificationRunner(t, runner)

	_, stdout, stderr, err := executeCommand("artifact", "verify", artifactPath)
	if err == nil {
		t.Fatal("expected error for a failed framework check, got nil")
	}
	if !strings.Contains(err.Error(), "artifact verification failed") {
		t.Errorf("error %q does not mention artifact verification failure", err)
	}

	if !strings.Contains(stdout, "[PASS] vendor_present: vendor/autoload.php exists") {
		t.Errorf("expected passing check reported in output, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "[FAIL] config_files: missing required file(s): config/app.php") {
		t.Errorf("expected failing check with details in output, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "Error:") {
		t.Errorf("expected error rendered to stderr, got: %s", stderr)
	}

	// Both checks were still invoked — the failing one is not skipped.
	if len(runner.invokedChecks) != 2 {
		t.Errorf("invoked verification checks = %v, want both checks invoked", runner.invokedChecks)
	}
}

// TestVerifyCmd_MissingAdapterWarnsAndPasses verifies that a project
// with a framework but no installed adapter executable keeps the generic
// PASS result: a warning is printed to stderr and no framework checks
// run (ADR-009 §9.7 — adapters are optional).
func TestVerifyCmd_MissingAdapterWarnsAndPasses(t *testing.T) {
	dir := setupPackageProject(t, "laravel")
	artifactPath := filepath.Join(dir, "test-artifact.tar.gz")
	createValidArtifact(t, artifactPath)

	// No adapter executable resolves anywhere.
	orig := adapterExecutableLookup
	adapterExecutableLookup = func(name string) (string, error) {
		return "", os.ErrNotExist
	}
	t.Cleanup(func() { adapterExecutableLookup = orig })

	// The runner must never be consulted without a resolved executable.
	runner := &verificationStubRunner{
		capabilitiesJSON: verificationCapabilitiesFixture([]string{"vendor_present"}),
		verificationJSON: map[string]string{},
	}
	stubVerificationRunner(t, runner)

	_, stdout, stderr, err := executeCommand("artifact", "verify", artifactPath)
	if err != nil {
		t.Fatalf("execute command returned error: %v", err)
	}

	if !strings.Contains(stdout, "Artifact verification: PASSED") {
		t.Errorf("expected PASSED in output, got:\n%s", stdout)
	}
	if strings.Contains(stdout, "Framework verification") {
		t.Errorf("expected no framework verification section, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "Warning:") {
		t.Errorf("expected a warning about the missing adapter, got: %s", stderr)
	}
	if !strings.Contains(stderr, "anvil-adapter-laravel") {
		t.Errorf("expected warning to name the adapter executable, got: %s", stderr)
	}
	if len(runner.invokedChecks) != 0 {
		t.Errorf("verification checks invoked = %v, want none without an executable", runner.invokedChecks)
	}
}

// ── F1 Security Guard: Slash Framework Name Never Probes (team review F1) ─

// TestVerifyCmd_SlashFrameworkNameNeverProbesAdapter is the
// verification regression test for team review F1 (security blocker): a
// project declaration with a path separator in project.framework must
// be rejected BEFORE any lookup — the CWD-relative trap executable is
// never resolved and never executed; verification degrades to the
// missing-adapter warning and the generic PASS result. The
// adapterExecutableLookup seam stays the REAL exec.LookPath: a
// regression of the identifier guard would resolve and execute the trap
// and fail this test.
func TestVerifyCmd_SlashFrameworkNameNeverProbesAdapter(t *testing.T) {
	dir := setupPackageProject(t, "x/evil")
	artifactPath := filepath.Join(dir, "test-artifact.tar.gz")
	createValidArtifact(t, artifactPath)
	placeLookPathTrap(t, dir, "pwned-verify")

	_, stdout, stderr, err := executeCommand("artifact", "verify", artifactPath)
	if err != nil {
		t.Fatalf("artifact verify returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	assertTrapNotExecuted(t, dir, "pwned-verify")
	if !strings.Contains(stdout, "Artifact verification: PASSED") {
		t.Errorf("expected the generic PASSED result, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "Warning:") {
		t.Errorf("stderr should carry the degrade warning, got: %s", stderr)
	}
}
