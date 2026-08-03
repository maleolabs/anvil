// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P4-01, ST-P4-02, ST-P4-11, ST-P4-13, EPIC-004
package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/project"
	"maleolabs.com/anvil/internal/release"
	"maleolabs.com/anvil/internal/server"
)

// setupInstallEnvironment creates a minimal server environment with a
// registered project and a packaged artifact, and returns the server root,
// the artifact path, and the project ID.
//
// The server root is a temp directory managed by the test.
func setupInstallEnvironment(t *testing.T) (serverRoot, artifactPath, projectID string) {
	t.Helper()

	serverRoot = t.TempDir()
	projectID = "test-project"
	installRoot := filepath.Join(serverRoot, "projects", projectID)

	// Initialize server config.
	configStore := server.NewConfigStore(serverRoot)
	cfg := server.DefaultServerConfig()
	cfg.Runtime.ID = "test-runtime"
	if err := configStore.Save(cfg); err != nil {
		t.Fatalf("save server config: %v", err)
	}

	// Register the project.
	reg := server.DefaultProjectRegistry()
	reg.Project.ID = projectID
	reg.Project.InstallRoot = installRoot
	reg.Project.DisplayName = "Test Project"

	registryStore := server.NewRegistryStore(serverRoot)
	if err := registryStore.Register(reg); err != nil {
		t.Fatalf("register project: %v", err)
	}

	// Create a source directory for packaging.
	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "index.php"), []byte("<?php\n"), 0644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	// Package a valid artifact with the matching project ID.
	outputDir := t.TempDir()
	result, err := artifact.Package(artifact.PackageOptions{
		SourceDir: sourceDir,
		OutputDir: outputDir,
		Version:   "1.0.0",
		Source:    projectID,
		ProjectID: projectID,
	})
	if err != nil {
		t.Fatalf("artifact.Package failed: %v", err)
	}

	return serverRoot, result.ArtifactPath, projectID
}

// TestInstallCommand_CreatesRelease verifies that:
//
//	anvil server release install <project-id> <artifact-path> --server-root <root>
//
// creates a Runtime Release.
//
// AC 1: Release created when artifact is verified.
// AC 2: Created Release has unique identity.
// AC 3: Release references the stored artifact.
// AC 4: Release lifecycle stage is Ready after installation.
func TestInstallCommand_CreatesRelease(t *testing.T) {
	serverRoot, artifactPath, projectID := setupInstallEnvironment(t)

	_, stdout, stderr, err := executeCommand("server", "release", "install",
		projectID, artifactPath, "--server-root", serverRoot)
	if err != nil {
		t.Fatalf("install command returned unexpected error: %v\nstderr: %s", err, stderr)
	}

	// Verify success output.
	if !contains(stdout, "Release created") {
		t.Errorf("expected 'Release created' in stdout, got: %s", stdout)
	}
	if !contains(stdout, "Release ID:") {
		t.Errorf("expected 'Release ID:' in stdout, got: %s", stdout)
	}
	if !contains(stdout, "ready") {
		t.Errorf("expected stage 'ready' in stdout, got: %s", stdout)
	}
}

// TestInstallCommand_NonexistentArtifact verifies that running:
//
//	anvil server release install <project-id> <nonexistent-path> --server-root <root>
//
// fails with a clear error.
//
// AC: Invalid or incompatible artifacts fail before extraction.
func TestInstallCommand_NonexistentArtifact(t *testing.T) {
	serverRoot := t.TempDir()

	// No need to set up a full server — the artifact-not-found check happens
	// before the coordinator is called.
	_, _, stderr, err := executeCommand("server", "release", "install",
		"test-project", "/nonexistent/artifact.tar.gz", "--server-root", serverRoot)
	if err == nil {
		t.Fatal("expected error for non-existent artifact, got nil")
	}
	if !contains(stderr, "artifact not found") {
		t.Errorf("expected 'artifact not found' error, got: %s", stderr)
	}
}

// TestInstallCommand_UninitializedRuntime verifies that running:
//
//	anvil server release install <project-id> <artifact-path> --server-root <dir>
//
// reports error when the Runtime has not been initialized.
func TestInstallCommand_UninitializedRuntime(t *testing.T) {
	dir := t.TempDir()

	// Create a valid artifact file (non-empty) but do NOT initialize the server.
	artifactPath := filepath.Join(dir, "artifact.tar.gz")
	if err := os.WriteFile(artifactPath, []byte("artifact content"), 0644); err != nil {
		t.Fatalf("failed to create artifact: %v", err)
	}

	_, _, stderr, err := executeCommand("server", "release", "install",
		"test-project", artifactPath, "--server-root", dir)
	if err == nil {
		t.Fatal("expected error for uninitialized runtime, got nil")
	}

	if !contains(stderr, "not initialized") {
		t.Errorf("stderr should report 'not initialized', got: %s", stderr)
	}
}

// TestInstallCommand_ArtifactCheckBeforeInit verifies that:
//
//	anvil server release install --server-root <dir>
//
// validates the artifact path BEFORE checking server initialization.
// The artifact-not-found error should surface, not the init error.
func TestInstallCommand_ArtifactCheckBeforeInit(t *testing.T) {
	dir := t.TempDir()

	// Do NOT initialize the server.
	// Trying to install a non-existent artifact should fail with artifact error,
	// not server initialization error — because artifact validation happens first.
	_, _, stderr, err := executeCommand("server", "release", "install",
		"test-project", "/nonexistent/artifact.tar.gz", "--server-root", dir)
	if err == nil {
		t.Fatal("expected error for non-existent artifact, got nil")
	}

	if !contains(stderr, "artifact not found") {
		t.Errorf("stderr should report artifact not found (not init error), got: %s", stderr)
	}
}

// TestInstallCommand_UnverifiedArtifact verifies that an invalid artifact
// fails with an appropriate error.
//
// AC: Invalid artifacts fail and do not create a Release.
func TestInstallCommand_UnverifiedArtifact(t *testing.T) {
	serverRoot, _, projectID := setupInstallEnvironment(t)

	// Create an invalid artifact file (not a valid tar.gz).
	invalidPath := filepath.Join(t.TempDir(), "invalid-artifact.tar.gz")
	if err := os.WriteFile(invalidPath, []byte("not-a-valid-archive"), 0644); err != nil {
		t.Fatalf("write invalid artifact: %v", err)
	}

	_, _, stderr, err := executeCommand("server", "release", "install",
		projectID, invalidPath, "--server-root", serverRoot)
	if err == nil {
		t.Fatal("expected error for unverified artifact, got nil")
	}
	if !contains(stderr, "artifact must be verified first") {
		t.Errorf("expected 'artifact must be verified first' error, got: %s", stderr)
	}
}

// TestInstallCommand_ProjectIDMismatch verifies that running:
//
//	anvil server release install <project-id> <artifact-path> --server-root <root>
//
// fails with a clear error when the artifact's project ID does not match
// the registered project.
//
// AC: An error is returned when the artifact project ID does not match
// the registered project.
func TestInstallCommand_ProjectIDMismatch(t *testing.T) {
	serverRoot := t.TempDir()
	projectID := "test-project"
	installRoot := filepath.Join(serverRoot, "projects", projectID)

	// Initialize server config and register the project.
	configStore := server.NewConfigStore(serverRoot)
	cfg := server.DefaultServerConfig()
	cfg.Runtime.ID = "test-runtime"
	if err := configStore.Save(cfg); err != nil {
		t.Fatalf("save server config: %v", err)
	}

	reg := server.DefaultProjectRegistry()
	reg.Project.ID = projectID
	reg.Project.InstallRoot = installRoot
	reg.Project.DisplayName = "Test Project"

	registryStore := server.NewRegistryStore(serverRoot)
	if err := registryStore.Register(reg); err != nil {
		t.Fatalf("register project: %v", err)
	}

	// Create an artifact with a DIFFERENT project ID than the registered one.
	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "index.php"), []byte("<?php\n"), 0644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	outputDir := t.TempDir()
	result, err := artifact.Package(artifact.PackageOptions{
		SourceDir: sourceDir,
		OutputDir: outputDir,
		Version:   "1.0.0",
		Source:    "other-project",
		ProjectID: "other-project",
	})
	if err != nil {
		t.Fatalf("artifact.Package failed: %v", err)
	}

	// Run install with the registered project ID but the artifact belongs to
	// a different project.
	_, _, stderr, err := executeCommand("server", "release", "install",
		projectID, result.ArtifactPath, "--server-root", serverRoot)
	if err == nil {
		t.Fatal("expected error for project ID mismatch, got nil")
	}
	if !contains(stderr, "does not match") {
		t.Errorf("expected project ID mismatch error, got: %s", stderr)
	}
}

// TestInstallCommand_OutputFormat verifies that the command output follows
// the project's output conventions.
func TestInstallCommand_OutputFormat(t *testing.T) {
	serverRoot, artifactPath, projectID := setupInstallEnvironment(t)

	_, stdout, stderr, err := executeCommand("server", "release", "install",
		projectID, artifactPath, "--server-root", serverRoot)
	if err != nil {
		t.Fatalf("install command failed: %v\nstderr: %s", err, stderr)
	}

	// Verify the output contains standard sections.
	if !contains(stdout, "Release created") {
		t.Error("output should start with 'Release created'")
	}
	if !contains(stdout, "Release ID:") {
		t.Error("output should contain Release ID")
	}
	if !contains(stdout, "Artifact ID:") {
		t.Error("output should contain Artifact ID")
	}
	if !contains(stdout, "Stage:") {
		t.Error("output should contain Stage")
	}
	if !contains(stdout, "Created:") {
		t.Error("output should contain Created")
	}

	// Extract the Release ID from the output.
	var releaseID string
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "  Release ID: ") {
			releaseID = strings.TrimPrefix(line, "  Release ID: ")
			break
		}
	}
	if releaseID == "" {
		t.Fatal("could not extract Release ID from output")
	}

	// Verify the Release ID format (32 hex chars).
	if len(releaseID) != 32 {
		t.Errorf("Release ID length = %d, want 32", len(releaseID))
	}
	for _, c := range releaseID {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("invalid Release ID character: %c", c)
		}
	}
}

// TestInstallCommand_MissingArgs verifies that running without arguments
// produces an error.
func TestInstallCommand_MissingArgs(t *testing.T) {
	// No args.
	_, _, stderr, err := executeCommand("server", "release", "install")
	if err == nil {
		t.Fatal("expected error for missing args, got nil")
	}
	if !contains(stderr, "requires") && !contains(stderr, "accepts") {
		t.Errorf("expected arg validation error, got: %s", stderr)
	}

	// Only one arg.
	_, _, stderr, err = executeCommand("server", "release", "install", "only-project")
	if err == nil {
		t.Fatal("expected error for missing artifact arg, got nil")
	}
}

// ── Test Helpers ──────────────────────────────────────────────────────

// setupReadOnlyEnvironment creates a minimal server environment with a
// registered project and a saved Release file, and returns:
//   - serverRoot: the server root temp directory
//   - projectID: the registered project ID
//   - releaseID: the release identity
//   - releaseDir: the releases state directory
func setupReadOnlyEnvironment(t *testing.T) (serverRoot, projectID, releaseID, releaseDir string) {
	t.Helper()

	serverRoot = t.TempDir()
	projectID = "test-project"
	releaseID = "test-rel-history-001"
	installRoot := filepath.Join(serverRoot, "projects", projectID)

	// Initialize server config.
	configStore := server.NewConfigStore(serverRoot)
	cfg := server.DefaultServerConfig()
	cfg.Runtime.ID = "test-runtime"
	if err := configStore.Save(cfg); err != nil {
		t.Fatalf("save server config: %v", err)
	}

	// Register the project.
	reg := server.DefaultProjectRegistry()
	reg.Project.ID = projectID
	reg.Project.InstallRoot = installRoot
	reg.Project.DisplayName = "Test Project"

	registryStore := server.NewRegistryStore(serverRoot)
	if err := registryStore.Register(reg); err != nil {
		t.Fatalf("register project: %v", err)
	}

	// Create the releases state directory.
	s := project.NewStructure(installRoot)
	releaseDir = filepath.Join(s.StateDir, "releases")
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir releases dir: %v", err)
	}

	return serverRoot, projectID, releaseID, releaseDir
}

// ── History Command Tests (ST-P4-10) ──────────────────────────────────

// TestHistoryCommand_DisplaysTransitions verifies that:
//
//	anvil server release history <project-id> <release-id> --server-root <root>
//
// displays all lifecycle transitions for a Release.
//
// AC: Running the history command with a valid Release identity displays
// all lifecycle transitions.
// AC: Each transition is displayed with a timestamp.
// AC: The current stage is indicated in the display.
//
// Reference: ST-P4-10 AC-1, AC-2, AC-4
func TestHistoryCommand_DisplaysTransitions(t *testing.T) {
	serverRoot, projectID, releaseID, releaseDir := setupReadOnlyEnvironment(t)

	// Create a Release with transition history.
	rel := &release.Release{
		ID:         release.ReleaseID(releaseID),
		ArtifactID: "test-artifact",
		Version:    "1.0.0",
		Stage:      release.StageActive,
		CreatedAt:  "2024-01-15T10:00:00Z",
		Transitions: []release.TransitionRecord{
			{Timestamp: "2024-01-15T10:00:00Z", From: release.StageReady, To: release.StageActivating, Outcome: "success"},
			{Timestamp: "2024-01-15T10:00:05Z", From: release.StageActivating, To: release.StageActive, Outcome: "success"},
		},
	}
	relPath := filepath.Join(releaseDir, releaseID+".json")
	if err := rel.Save(relPath); err != nil {
		t.Fatalf("save release: %v", err)
	}

	_, stdout, stderr, err := executeCommand("server", "release", "history",
		projectID, releaseID, "--server-root", serverRoot)
	if err != nil {
		t.Fatalf("history command returned unexpected error: %v\nstderr: %s", err, stderr)
	}

	// Verify the output contains expected sections.
	if !contains(stdout, "Release History for") {
		t.Errorf("expected 'Release History' in stdout, got: %s", stdout)
	}
	if !contains(stdout, "Current Stage: active") {
		t.Errorf("expected 'Current Stage: active' in stdout, got: %s", stdout)
	}
	if !contains(stdout, "Transitions:") {
		t.Errorf("expected 'Transitions:' in stdout, got: %s", stdout)
	}
	if !contains(stdout, "ready → activating  success") {
		t.Errorf("expected 'ready → activating  success' in stdout, got: %s", stdout)
	}
	if !contains(stdout, "activating → active  success") {
		t.Errorf("expected 'activating → active  success' in stdout, got: %s", stdout)
	}
	if !contains(stdout, "2024-01-15T10:00:00Z") {
		t.Errorf("expected timestamp '2024-01-15T10:00:00Z' in stdout, got: %s", stdout)
	}
}

// TestHistoryCommand_NonExistentRelease verifies that running:
//
//	anvil server release history <project-id> <nonexistent-release> --server-root <root>
//
// reports that no Release was found.
//
// AC: Running the history command with a nonexistent Release identity reports
// that no Release was found.
//
// Reference: ST-P4-10 AC-5
func TestHistoryCommand_NonExistentRelease(t *testing.T) {
	serverRoot, projectID, _, _ := setupReadOnlyEnvironment(t)

	_, _, stderr, err := executeCommand("server", "release", "history",
		projectID, "nonexistent-release", "--server-root", serverRoot)
	if err == nil {
		t.Fatal("history command should return error for nonexistent release")
	}

	if !contains(stderr, "Release") || !contains(stderr, "not found") {
		t.Errorf("expected 'Release not found' in stderr, got: %s", stderr)
	}
}

// TestHistoryCommand_NoStateModification verifies that the history command
// does not modify any files.
//
// AC: History inspection does not modify any files or state.
//
// Reference: ST-P4-10 AC-6
func TestHistoryCommand_NoStateModification(t *testing.T) {
	serverRoot, projectID, releaseID, _ := setupReadOnlyEnvironment(t)

	// Read all files before the command.
	var beforeFiles []string
	filepath.Walk(serverRoot, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			beforeFiles = append(beforeFiles, path)
		}
		return nil
	})

	// Run the command (even without a release file, the command should not
	// modify state — it will just error out without side effects).
	executeCommand("server", "release", "history",
		projectID, releaseID, "--server-root", serverRoot)

	// Read all files after the command.
	var afterFiles []string
	filepath.Walk(serverRoot, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			afterFiles = append(afterFiles, path)
		}
		return nil
	})

	if len(beforeFiles) != len(afterFiles) {
		t.Errorf("file count changed: before=%d, after=%d", len(beforeFiles), len(afterFiles))
	}

	// Compare file contents.
	for i, path := range beforeFiles {
		beforeContent, _ := os.ReadFile(path)
		afterContent, _ := os.ReadFile(afterFiles[i])
		if string(beforeContent) != string(afterContent) {
			t.Errorf("file content changed: %s", path)
		}
	}
}

// TestHistoryCommand_JSONOutput verifies the --json flag produces valid
// machine-readable output.
func TestHistoryCommand_JSONOutput(t *testing.T) {
	serverRoot, projectID, releaseID, releaseDir := setupReadOnlyEnvironment(t)

	rel := &release.Release{
		ID:         release.ReleaseID(releaseID),
		ArtifactID: "test-artifact",
		Version:    "1.0.0",
		Stage:      release.StageActive,
		CreatedAt:  "2024-01-15T10:00:00Z",
		Transitions: []release.TransitionRecord{
			{Timestamp: "2024-01-15T10:00:00Z", From: release.StageReady, To: release.StageActivating, Outcome: "success"},
			{Timestamp: "2024-01-15T10:00:05Z", From: release.StageActivating, To: release.StageActive, Outcome: "success"},
		},
	}
	relPath := filepath.Join(releaseDir, releaseID+".json")
	if err := rel.Save(relPath); err != nil {
		t.Fatalf("save release: %v", err)
	}

	_, stdout, stderr, err := executeCommand("server", "release", "history",
		projectID, releaseID, "--server-root", serverRoot, "--json")
	if err != nil {
		t.Fatalf("history command with --json returned unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, "\"release_id\"") {
		t.Errorf("expected JSON field 'release_id' in stdout, got: %s", stdout)
	}
	if !contains(stdout, "\"current_stage\": \"active\"") {
		t.Errorf("expected 'current_stage' in stdout, got: %s", stdout)
	}
	if !contains(stdout, "\"transitions\"") {
		t.Errorf("expected 'transitions' array in stdout, got: %s", stdout)
	}
}

// ── Active Command Tests (ST-P4-11) ───────────────────────────────────

// TestActiveCommand_ShowsActiveRelease verifies that:
//
//	anvil server release active <project-id> --server-root <root>
//
// displays the currently Active Release with its identity, version,
// artifact reference, and stage.
//
// AC: Running the active release query when a Release is Active displays
// the Release identity and version.
// AC: The query output includes the artifact reference and activation
// timestamp.
//
// Reference: ST-P4-11 AC-1, AC-2
func TestActiveCommand_ShowsActiveRelease(t *testing.T) {
	serverRoot, projectID, releaseID, releaseDir := setupReadOnlyEnvironment(t)

	// Create an Active Release with a transition history that includes
	// an activation timestamp.
	rel := &release.Release{
		ID:         release.ReleaseID(releaseID),
		ArtifactID: "test-artifact-xyz",
		Version:    "2.0.0",
		Stage:      release.StageActive,
		CreatedAt:  "2024-06-01T10:00:00Z",
		Transitions: []release.TransitionRecord{
			{Timestamp: "2024-06-01T10:00:00Z", From: release.StageReady, To: release.StageActivating, Outcome: "success"},
			{Timestamp: "2024-06-01T10:00:05Z", From: release.StageActivating, To: release.StageActive, Outcome: "success"},
		},
	}
	relPath := filepath.Join(releaseDir, releaseID+".json")
	if err := rel.Save(relPath); err != nil {
		t.Fatalf("save release: %v", err)
	}

	_, stdout, stderr, err := executeCommand("server", "release", "active",
		projectID, "--server-root", serverRoot)
	if err != nil {
		t.Fatalf("active command returned unexpected error: %v\nstderr: %s", err, stderr)
	}

	// Verify the output contains expected sections.
	if !contains(stdout, "Active Release") {
		t.Errorf("expected 'Active Release' in stdout, got: %s", stdout)
	}
	if !contains(stdout, releaseID) {
		t.Errorf("expected release ID in stdout, got: %s", stdout)
	}
	if !contains(stdout, "Version: 2.0.0") {
		t.Errorf("expected 'Version: 2.0.0' in stdout, got: %s", stdout)
	}
	if !contains(stdout, "Artifact Reference: test-artifact-xyz") {
		t.Errorf("expected artifact reference in stdout, got: %s", stdout)
	}
	if !contains(stdout, "Stage: active") {
		t.Errorf("expected 'Stage: active' in stdout, got: %s", stdout)
	}

	// Verify the activation timestamp is displayed.
	if !contains(stdout, "2024-06-01T10:00:05Z") {
		t.Errorf("expected activation timestamp in stdout, got: %s", stdout)
	}
}

// TestActiveCommand_NoActiveRelease verifies that running:
//
//	anvil server release active <project-id> --server-root <root>
//
// when no Release is Active reports that no Release is Active.
//
// AC: Running the query when no Release is Active reports that no Release
// is Active.
//
// Reference: ST-P4-11 AC-3
func TestActiveCommand_NoActiveRelease(t *testing.T) {
	serverRoot, projectID, _, releaseDir := setupReadOnlyEnvironment(t)

	// Create a Release in Ready stage (not Active).
	rel := &release.Release{
		ID:          release.ReleaseID("test-ready-release"),
		ArtifactID:  "test-artifact",
		Version:     "1.0.0",
		Stage:       release.StageReady,
		CreatedAt:   "2024-01-15T10:00:00Z",
		Transitions: []release.TransitionRecord{},
	}
	relPath := filepath.Join(releaseDir, "test-ready-release.json")
	if err := rel.Save(relPath); err != nil {
		t.Fatalf("save release: %v", err)
	}

	_, stdout, stderr, err := executeCommand("server", "release", "active",
		projectID, "--server-root", serverRoot)
	if err != nil {
		t.Fatalf("active command returned unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, "No Active Release") {
		t.Errorf("expected 'No Active Release' in stdout, got: %s", stdout)
	}
	if contains(stdout, "test-ready-release") {
		t.Errorf("should not mention the ready release, got: %s", stdout)
	}
}

// TestActiveCommand_NoStateModification verifies that the active command
// does not modify any files.
//
// AC: The query does not modify any files or state.
//
// Reference: ST-P4-11 AC-6
func TestActiveCommand_NoStateModification(t *testing.T) {
	serverRoot, projectID, releaseID, releaseDir := setupReadOnlyEnvironment(t)

	// Create an Active Release.
	rel := &release.Release{
		ID:         release.ReleaseID(releaseID),
		ArtifactID: "test-artifact",
		Version:    "1.0.0",
		Stage:      release.StageActive,
		CreatedAt:  "2024-01-15T10:00:00Z",
		Transitions: []release.TransitionRecord{
			{Timestamp: "2024-01-15T10:00:05Z", From: release.StageActivating, To: release.StageActive, Outcome: "success"},
		},
	}
	relPath := filepath.Join(releaseDir, releaseID+".json")
	if err := rel.Save(relPath); err != nil {
		t.Fatalf("save release: %v", err)
	}

	// Read the release file content before the query.
	before, err := os.ReadFile(relPath)
	if err != nil {
		t.Fatalf("read release file before query: %v", err)
	}

	_, _, stderr, err := executeCommand("server", "release", "active",
		projectID, "--server-root", serverRoot)
	if err != nil {
		t.Fatalf("active command returned unexpected error: %v\nstderr: %s", err, stderr)
	}

	// Read the release file content after the query.
	after, err := os.ReadFile(relPath)
	if err != nil {
		t.Fatalf("read release file after query: %v", err)
	}

	if string(before) != string(after) {
		t.Error("release file content changed after read-only query")
	}
}

// TestActiveCommand_JSONOutput verifies the --json flag produces valid
// machine-readable output.
func TestActiveCommand_JSONOutput(t *testing.T) {
	serverRoot, projectID, releaseID, releaseDir := setupReadOnlyEnvironment(t)

	rel := &release.Release{
		ID:         release.ReleaseID(releaseID),
		ArtifactID: "test-artifact-xyz",
		Version:    "2.0.0",
		Stage:      release.StageActive,
		CreatedAt:  "2024-06-01T10:00:00Z",
		Transitions: []release.TransitionRecord{
			{Timestamp: "2024-06-01T10:00:00Z", From: release.StageReady, To: release.StageActivating, Outcome: "success"},
			{Timestamp: "2024-06-01T10:00:05Z", From: release.StageActivating, To: release.StageActive, Outcome: "success"},
		},
	}
	relPath := filepath.Join(releaseDir, releaseID+".json")
	if err := rel.Save(relPath); err != nil {
		t.Fatalf("save release: %v", err)
	}

	_, stdout, stderr, err := executeCommand("server", "release", "active",
		projectID, "--server-root", serverRoot, "--json")
	if err != nil {
		t.Fatalf("active command with --json returned unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, "\"active\": true") {
		t.Errorf("expected 'active: true' in JSON output, got: %s", stdout)
	}
	if !contains(stdout, "\"release_id\"") {
		t.Errorf("expected 'release_id' in JSON output, got: %s", stdout)
	}
	if !contains(stdout, "\"version\": \"2.0.0\"") {
		t.Errorf("expected version in JSON output, got: %s", stdout)
	}
	if !contains(stdout, "\"artifact_reference\": \"test-artifact-xyz\"") {
		t.Errorf("expected artifact_reference in JSON output, got: %s", stdout)
	}
	if !contains(stdout, "\"activation_timestamp\": \"2024-06-01T10:00:05Z\"") {
		t.Errorf("expected activation_timestamp in JSON output, got: %s", stdout)
	}
}

// TestActiveCommand_NoActiveJSONOutput verifies that the --json flag when
// no Release is Active produces the correct JSON structure.
func TestActiveCommand_NoActiveJSONOutput(t *testing.T) {
	serverRoot, projectID, _, _ := setupReadOnlyEnvironment(t)

	_, stdout, stderr, err := executeCommand("server", "release", "active",
		projectID, "--server-root", serverRoot, "--json")
	if err != nil {
		t.Fatalf("active command with --json returned unexpected error: %v\nstderr: %s", err, stderr)
	}

	if !contains(stdout, "\"active\": false") {
		t.Errorf("expected 'active: false' in JSON output, got: %s", stdout)
	}
}
