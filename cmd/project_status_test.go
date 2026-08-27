package cmd

import (
	"os"
	"testing"
)

// TestProjectStatus_NormalProject verifies that running:
//
//	anvil project status
//
// inside a newly initialized Anvil project displays the project name and
// the "created" lifecycle stage.
//
// Reference: ST-P1-08
func TestProjectStatus_NormalProject(t *testing.T) {
	dir := t.TempDir()

	// Initialize a project.
	_, _, _, err := executeCommand("init", "test-status-app", "--path", dir)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	defer func() {
		_ = os.Chdir(origDir)
	}()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir to project dir: %v", err)
	}

	_, stdout, stderr, err := executeCommand("project", "status")
	if err != nil {
		t.Fatalf("project status command failed: %v\nstderr: %s", err, stderr)
	}

	// Verify project name is displayed.
	if !contains(stdout, "test-status-app") {
		t.Errorf("stdout should contain project name 'test-status-app', got: %s", stdout)
	}

	// Verify lifecycle stage is "created" (newly initialized).
	if !contains(stdout, "created") {
		t.Errorf("stdout should contain lifecycle stage 'created', got: %s", stdout)
	}

	// Verify configuration is valid (modern header + icon).
	if !contains(stdout, "Configuration valid") {
		t.Errorf("stdout should contain 'Configuration valid', got: %s", stdout)
	}

	// Stderr should be empty on success.
	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}
}

// TestProjectStatus_MissingProject verifies that running:
//
//	anvil project status
//
// in a directory without an Anvil project produces a guidance error
// via RequireProject.
//
// Reference: ST-P1-08
func TestProjectStatus_MissingProject(t *testing.T) {
	dir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	defer func() {
		_ = os.Chdir(origDir)
	}()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir to temp dir: %v", err)
	}

	_, _, stderr, err := executeCommand("project", "status")
	if err == nil {
		t.Fatal("expected error for non-existent project, got nil")
	}
	if !contains(stderr, "no anvil project found") {
		t.Errorf("expected guidance about missing project, got: %s", stderr)
	}
}

// TestProjectStatus_RemovedProject verifies that running:
//
//	anvil project status
//
// after a project has been removed reports that the project no longer
// exists (RequireProject fails because anvil.yaml is gone).
//
// Reference: ST-P1-08
func TestProjectStatus_RemovedProject(t *testing.T) {
	dir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	defer func() {
		_ = os.Chdir(origDir)
	}()

	// Initialize a project.
	_, _, _, err = executeCommand("init", "removable-app", "--path", dir)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir to project dir: %v", err)
	}

	// Verify status works and shows "created" stage before removal.
	_, stdout, stderr, err := executeCommand("project", "status")
	if err != nil {
		t.Fatalf("project status before removal failed: %v\nstderr: %s", err, stderr)
	}
	if !contains(stdout, "created") {
		t.Errorf("expected lifecycle stage 'created' before removal, got: %s", stdout)
	}

	// Remove the project with --force.
	_, _, _, err = executeCommand("project", "remove", "--force")
	if err != nil {
		t.Fatalf("remove command failed: %v", err)
	}

	// After removal the project directory is gone. Change to a different
	// temp directory and verify status reports no project found.
	emptyDir := t.TempDir()
	if err := os.Chdir(emptyDir); err != nil {
		t.Fatalf("failed to chdir to empty temp dir: %v", err)
	}

	_, _, stderr, err = executeCommand("project", "status")
	if err == nil {
		t.Fatal("expected error for removed project, got nil")
	}
	if !contains(stderr, "no anvil project found") {
		t.Errorf("expected guidance about missing project after removal, got: %s", stderr)
	}
}
