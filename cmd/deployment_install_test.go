// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P10-03, ADR-015, EPIC-010
package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/server"
)

// TestDeploymentInstallCommand_RegistersUnderDeployment verifies that:
//
//	anvil deployment install
//
// is registered as a subcommand of the deployment command.
func TestDeploymentInstallCommand_RegistersUnderDeployment(t *testing.T) {
	sub, _, err := rootCmd.Find([]string{"deployment", "install"})
	if err != nil {
		t.Fatalf("rootCmd.Find([\"deployment\", \"install\"]) returned error: %v", err)
	}
	if sub == nil {
		t.Fatal("rootCmd.Find([\"deployment\", \"install\"]) returned nil command")
	}
	if sub.Use != "install <artifact-path>" {
		t.Errorf("command Use = %q, want %q", sub.Use, "install <artifact-path>")
	}

	// Verify it's nested under deployment (parent is deploymentCmd).
	if sub.Parent() == nil || sub.Parent().Use != "deployment" {
		t.Errorf("install command parent = %v, want deployment subcommand", sub.Parent())
	}
}

// TestDeploymentInstallCommand_ValidatesArtifactPath verifies that:
//
//	anvil deployment install <artifact-path> --server-root <dir>
//
// validates the artifact path exists on disk.
func TestDeploymentInstallCommand_ValidatesArtifactPath(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server first.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// Run install with a non-existent artifact path.
	_, _, stderr, err := executeCommand("deployment", "install", "/nonexistent/artifact.tar.gz", "--server-root", dir)
	if err == nil {
		t.Fatal("expected error for non-existent artifact, got nil")
	}

	if !contains(stderr, "not found") {
		t.Errorf("stderr should report artifact not found, got: %s", stderr)
	}
}

// TestDeploymentInstallCommand_ValidatesEmptyArtifact verifies that:
//
//	anvil deployment install <artifact-path> --server-root <dir>
//
// validates the artifact has content (non-empty).
func TestDeploymentInstallCommand_ValidatesEmptyArtifact(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server first.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// Create an empty artifact file.
	emptyArtifact := filepath.Join(dir, "empty-artifact.tar.gz")
	if err := os.WriteFile(emptyArtifact, []byte{}, 0644); err != nil {
		t.Fatalf("failed to create empty artifact: %v", err)
	}

	_, _, stderr, err := executeCommand("deployment", "install", emptyArtifact, "--server-root", dir)
	if err == nil {
		t.Fatal("expected error for empty artifact, got nil")
	}

	if !contains(stderr, "empty") {
		t.Errorf("stderr should report artifact is empty, got: %s", stderr)
	}
}

// TestDeploymentInstallCommand_UninitializedRuntime verifies that:
//
//	anvil deployment install <artifact-path> --server-root <dir>
//
// reports error when the Runtime has not been initialized.
func TestDeploymentInstallCommand_UninitializedRuntime(t *testing.T) {
	dir := t.TempDir()

	// Create a valid artifact file (non-empty).
	artifactPath := filepath.Join(dir, "artifact.tar.gz")
	if err := os.WriteFile(artifactPath, []byte("artifact content"), 0644); err != nil {
		t.Fatalf("failed to create artifact: %v", err)
	}

	_, stdout, stderr, err := executeCommand("deployment", "install", artifactPath, "--server-root", dir)
	if err == nil {
		t.Fatal("expected error for uninitialized runtime, got nil")
	}

	if !contains(stderr, "not initialized") {
		t.Errorf("stderr should report 'not initialized', got: %s", stderr)
	}
	_ = stdout
}

// TestDeploymentInstallCommand_InitCheckBeforeArtifact verifies that:
//
//	anvil deployment install --server-root <dir>
//
// validates Server Runtime initialization BEFORE the artifact path
// (TS-019-03-02 §9.3): the precondition category (4) is never masked by
// a later input validation failure, so an uninitialized Runtime surfaces
// the init error even when the artifact path is bad.
func TestDeploymentInstallCommand_InitCheckBeforeArtifact(t *testing.T) {
	dir := t.TempDir()

	// Do NOT initialize the server.
	// Trying to install a non-existent artifact against an uninitialized
	// Runtime must fail with the init error (precondition), not the
	// artifact error — the runtime gate is the first check.
	_, _, stderr, err := executeCommand("deployment", "install", "/nonexistent/artifact.tar.gz", "--server-root", dir)
	if err == nil {
		t.Fatal("expected error for uninitialized runtime, got nil")
	}
	requireExitCode(t, err, output.ExitCodePrecondition)

	if !contains(stderr, "not initialized") {
		t.Errorf("stderr should report the Runtime not initialized (gate first), got: %s", stderr)
	}
}

// TestDeploymentInstallCommand_Success verifies that:
//
//	anvil deployment install <artifact-path> --server-root <dir>
//
// succeeds with a valid artifact and initialized runtime.
//
// AC 1: Delegates installation to the Server Runtime command surface.
// AC 2: Reports Ready Release and Already Installed outcomes.
func TestDeploymentInstallCommand_Success(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server first.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// Create and register a project.
	projectID := "test-project"
	installRoot := filepath.Join(dir, "projects", projectID)
	reg := server.DefaultProjectRegistry()
	reg.Project.ID = projectID
	reg.Project.InstallRoot = installRoot
	reg.Project.DisplayName = "Test Project"

	registryStore := server.NewRegistryStore(dir)
	if err := registryStore.Register(reg); err != nil {
		t.Fatalf("register project: %v", err)
	}

	// Create a valid packaged artifact that will pass ReadManifest.
	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "index.php"), []byte("<?php\n"), 0644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	pkgResult, err := artifact.Package(artifact.PackageOptions{
		SourceDir: sourceDir,
		OutputDir: t.TempDir(),
		Version:   "1.0.0",
		Source:    projectID,
		ProjectID: projectID,
	})
	if err != nil {
		t.Fatalf("package artifact: %v", err)
	}

	// Verify the artifact is verified (RequiredVerified check).
	// The artifact.Package also writes verification markers.
	artifactPath := pkgResult.ArtifactPath

	// Run the install command.
	_, stdout, stderr, err := executeCommand("deployment", "install", artifactPath, "--server-root", dir)
	if err != nil {
		t.Fatalf("install command returned unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, "Installation completed") {
		t.Errorf("stdout should contain 'Installation completed', got: %s", stdout)
	}
	if !contains(stdout, projectID) {
		t.Errorf("stdout should contain project ID %q, got: %s", projectID, stdout)
	}
	if !contains(stdout, "Release ID:") {
		t.Errorf("stdout should contain 'Release ID:', got: %s", stdout)
	}
	if !contains(stdout, "ready") {
		t.Errorf("stdout should contain stage 'ready', got: %s", stdout)
	}
}

// TestDeploymentInstallCommand_JSONOutput verifies that:
//
//	anvil deployment install <artifact-path> --server-root <dir> --json
//
// produces valid JSON output.
func TestDeploymentInstallCommand_JSONOutput(t *testing.T) {
	dir := t.TempDir()

	// Initialize the server first.
	_, _, _, err := executeCommand("server", "init", "--server-root", dir)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	// Create and register a project.
	projectID := "test-project"
	installRoot := filepath.Join(dir, "projects", projectID)
	reg := server.DefaultProjectRegistry()
	reg.Project.ID = projectID
	reg.Project.InstallRoot = installRoot

	registryStore := server.NewRegistryStore(dir)
	if err := registryStore.Register(reg); err != nil {
		t.Fatalf("register project: %v", err)
	}

	// Create a valid artifact.
	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "index.php"), []byte("<?php\n"), 0644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	pkgResult, err := artifact.Package(artifact.PackageOptions{
		SourceDir: sourceDir,
		OutputDir: t.TempDir(),
		Version:   "1.0.0",
		Source:    projectID,
		ProjectID: projectID,
	})
	if err != nil {
		t.Fatalf("package artifact: %v", err)
	}

	_, stdout, stderr, err := executeCommand("deployment", "install", pkgResult.ArtifactPath, "--server-root", dir, "--json")
	if err != nil {
		t.Fatalf("install command returned unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, `"release_id"`) {
		t.Errorf("stdout should contain JSON field 'release_id', got: %s", stdout)
	}
	if !contains(stdout, `"stage"`) {
		t.Errorf("stdout should contain JSON field 'stage', got: %s", stdout)
	}
	if !contains(stdout, `"project_id"`) {
		t.Errorf("stdout should contain JSON field 'project_id', got: %s", stdout)
	}
	if contains(stdout, `"target_id"`) {
		t.Errorf("stdout should NOT contain JSON field 'target_id' (phantom argument removed, TS-019-04-03), got: %s", stdout)
	}
	if contains(stdout, `"artifact_id"`) {
		t.Errorf("stdout should NOT contain JSON field 'artifact_id' (deployment format), got: %s", stdout)
	}
	if contains(stdout, `"version"`) {
		t.Errorf("stdout should NOT contain JSON field 'version' (deployment format), got: %s", stdout)
	}
}

// TestDeploymentInstallCommand_ServerRootFlag verifies that the --server-root
// flag is available on the install command.
func TestDeploymentInstallCommand_ServerRootFlag(t *testing.T) {
	installCmd, _, err := rootCmd.Find([]string{"deployment", "install"})
	if err != nil {
		t.Fatalf("failed to find deployment install command: %v", err)
	}
	flag := installCmd.Flags().Lookup("server-root")
	if flag == nil {
		t.Errorf("flag --server-root should be on the deployment install subcommand")
	}

	// Verify --json flag exists.
	jsonFlag := installCmd.Flags().Lookup("json")
	if jsonFlag == nil {
		t.Errorf("flag --json should be on the deployment install subcommand")
	}
}

// TestDeploymentInstallCommand_ExactArgs verifies that:
//
//	anvil deployment install
//
// requires exactly one positional argument.
func TestDeploymentInstallCommand_ExactArgs(t *testing.T) {
	// Test with no args.
	_, _, stderr, err := executeCommand("deployment", "install")
	if err == nil {
		t.Fatal("expected error for missing args, got nil")
	}
	if !contains(stderr, "requires 1 argument") {
		t.Errorf("stderr should require artifact-path, got: %s", stderr)
	}

	// Test with two args (the pre-removal form "<target-id> <artifact-path>"):
	// the phantom target-id argument must be rejected per the announced
	// deprecation schedule (TS-019-04-03, ADR-032 D10).
	_, _, stderr2, err2 := executeCommand("deployment", "install", "target-1", "path")
	if err2 == nil {
		t.Fatal("expected error for extra args, got nil")
	}
	if !contains(stderr2, "requires 1 argument") {
		t.Errorf("stderr should require exactly 1 arg, got: %s", stderr2)
	}
}
