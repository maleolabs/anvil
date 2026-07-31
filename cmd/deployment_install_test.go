// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P10-03, ADR-015, EPIC-010
package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/artifact"
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
	if sub.Use != "install <target-id> <artifact-path>" {
		t.Errorf("command Use = %q, want %q", sub.Use, "install <target-id> <artifact-path>")
	}

	// Verify it's nested under deployment (parent is deploymentCmd).
	if sub.Parent() == nil || sub.Parent().Use != "deployment" {
		t.Errorf("install command parent = %v, want deployment subcommand", sub.Parent())
	}
}

// TestDeploymentInstallCommand_ValidatesArtifactPath verifies that:
//
//	anvil deployment install <target-id> <artifact-path> --server-root <dir>
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
	_, _, stderr, err := executeCommand("deployment", "install", "my-target", "/nonexistent/artifact.tar.gz", "--server-root", dir)
	if err == nil {
		t.Fatal("expected error for non-existent artifact, got nil")
	}

	if !contains(stderr, "not found") {
		t.Errorf("stderr should report artifact not found, got: %s", stderr)
	}
}

// TestDeploymentInstallCommand_ValidatesEmptyArtifact verifies that:
//
//	anvil deployment install <target-id> <artifact-path> --server-root <dir>
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

	_, _, stderr, err := executeCommand("deployment", "install", "my-target", emptyArtifact, "--server-root", dir)
	if err == nil {
		t.Fatal("expected error for empty artifact, got nil")
	}

	if !contains(stderr, "empty") {
		t.Errorf("stderr should report artifact is empty, got: %s", stderr)
	}
}

// TestDeploymentInstallCommand_UninitializedRuntime verifies that:
//
//	anvil deployment install <target-id> <artifact-path> --server-root <dir>
//
// reports error when the Runtime has not been initialized.
func TestDeploymentInstallCommand_UninitializedRuntime(t *testing.T) {
	dir := t.TempDir()

	// Create a valid artifact file (non-empty).
	artifactPath := filepath.Join(dir, "artifact.tar.gz")
	if err := os.WriteFile(artifactPath, []byte("artifact content"), 0644); err != nil {
		t.Fatalf("failed to create artifact: %v", err)
	}

	_, stdout, stderr, err := executeCommand("deployment", "install", "my-target", artifactPath, "--server-root", dir)
	if err == nil {
		t.Fatal("expected error for uninitialized runtime, got nil")
	}

	if !contains(stderr, "not initialized") {
		t.Errorf("stderr should report 'not initialized', got: %s", stderr)
	}
	_ = stdout
}

// TestDeploymentInstallCommand_ArtifactCheckBeforeInit verifies that:
//
//	anvil deployment install --server-root <dir>
//
// validates the artifact path BEFORE checking server initialization.
// This matches the deployment upload pattern: artifact validation first,
// then server context.
func TestDeploymentInstallCommand_ArtifactCheckBeforeInit(t *testing.T) {
	dir := t.TempDir()

	// Do NOT initialize the server.
	// Trying to install a non-existent artifact should fail with artifact error,
	// not server initialization error — because artifact validation happens first.
	_, _, stderr, err := executeCommand("deployment", "install", "my-target", "/nonexistent/artifact.tar.gz", "--server-root", dir)
	if err == nil {
		t.Fatal("expected error for non-existent artifact, got nil")
	}

	if !contains(stderr, "not found") {
		t.Errorf("stderr should report artifact not found (not init error), got: %s", stderr)
	}
}

// TestDeploymentInstallCommand_Success verifies that:
//
//	anvil deployment install <target-id> <artifact-path> --server-root <dir>
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
	_, stdout, stderr, err := executeCommand("deployment", "install", "my-target", artifactPath, "--server-root", dir)
	if err != nil {
		t.Fatalf("install command returned unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, "Installation completed") {
		t.Errorf("stdout should contain 'Installation completed', got: %s", stdout)
	}
	if !contains(stdout, "my-target") {
		t.Errorf("stdout should contain target ID 'my-target', got: %s", stdout)
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
//	anvil deployment install <target-id> <artifact-path> --server-root <dir> --json
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

	_, stdout, stderr, err := executeCommand("deployment", "install", "my-target", pkgResult.ArtifactPath, "--server-root", dir, "--json")
	if err != nil {
		t.Fatalf("install command returned unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, `"release_id"`) {
		t.Errorf("stdout should contain JSON field 'release_id', got: %s", stdout)
	}
	if !contains(stdout, `"stage"`) {
		t.Errorf("stdout should contain JSON field 'stage', got: %s", stdout)
	}
	if !contains(stdout, `"artifact_id"`) {
		t.Errorf("stdout should contain JSON field 'artifact_id', got: %s", stdout)
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
// requires exactly two positional arguments.
func TestDeploymentInstallCommand_ExactArgs(t *testing.T) {
	// Test with no args.
	_, _, stderr, err := executeCommand("deployment", "install")
	if err == nil {
		t.Fatal("expected error for missing args, got nil")
	}
	if !contains(stderr, "requires 2 argument") {
		t.Errorf("stderr should require target-id and artifact-path, got: %s", stderr)
	}

	// Test with one arg.
	_, _, stderr2, err2 := executeCommand("deployment", "install", "target-1")
	if err2 == nil {
		t.Fatal("expected error for missing artifact-path, got nil")
	}
	if !contains(stderr2, "requires 2 argument") {
		t.Errorf("stderr should require exactly 2 args, got: %s", stderr2)
	}

	// Test with three args.
	_, _, stderr3, err3 := executeCommand("deployment", "install", "target-1", "path", "extra-arg")
	if err3 == nil {
		t.Fatal("expected error for extra args, got nil")
	}
	if !contains(stderr3, "requires 2 argument") {
		t.Errorf("stderr should require exactly 2 args, got: %s", stderr3)
	}
}
