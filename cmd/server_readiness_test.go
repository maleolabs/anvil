// Package cmd implements the Anvil CLI commands.
//
// Reference: ST-P9-02
package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/project"
	"maleolabs.com/anvil/internal/release"
	"maleolabs.com/anvil/internal/server"
)

// readinessSetupOptions controls the state created by
// setupReadinessEnvironment.
type readinessSetupOptions struct {
	// artifactRegistered registers the Release's artifact in the EPIC-003
	// registration store (verified status).
	artifactRegistered bool

	// releaseStage is the lifecycle stage written to the Release record.
	releaseStage release.Stage
}

// setupReadinessEnvironment creates a server root with a healthy runtime
// layout, a registered project, and a persisted Runtime Release identified
// by project and release identity. It returns the server root, project ID,
// and release ID for the readiness and release inspection commands.
//
// The server root gets the full healthy layout (via setupHealthyServerRoot)
// so the platform readiness checks pass; the project install root carries
// the Release record and the optional artifact registration index.
func setupReadinessEnvironment(t *testing.T, opts readinessSetupOptions) (serverRoot, projectID, releaseID string) {
	t.Helper()

	serverRoot = t.TempDir()
	projectID = "test-project"
	releaseID = "rel-1234567890abcdef"

	// Healthy server root layout: runtime directories, release directory
	// with artifact, active symlink, runtime config, server config,
	// and projects directory.
	setupHealthyServerRoot(t, serverRoot)

	// Register the project with an install root inside the server root.
	installRoot := filepath.Join(serverRoot, "projects", projectID)
	reg := server.DefaultProjectRegistry()
	reg.Project.ID = projectID
	reg.Project.InstallRoot = installRoot
	reg.Project.DisplayName = "Test Project"

	registryStore := server.NewRegistryStore(serverRoot)
	if err := registryStore.Register(reg); err != nil {
		t.Fatalf("register project: %v", err)
	}

	// Create the project state directory for the Release record and the
	// artifact registration index.
	stateDir := project.NewStructure(installRoot).StateDir
	if err := os.MkdirAll(filepath.Join(stateDir, "releases"), 0755); err != nil {
		t.Fatalf("mkdir release state dir: %v", err)
	}

	// Persist the Release record (EPIC-004 state).
	rel := &release.Release{
		ID:          release.ReleaseID(releaseID),
		ArtifactID:  "art-1234567890abcdef",
		Version:     "1.0.0",
		Source:      projectID,
		RuntimePath: installRoot,
		Stage:       opts.releaseStage,
		Transitions: []release.TransitionRecord{},
	}
	if err := rel.Save(rel.SavePath(installRoot)); err != nil {
		t.Fatalf("save release JSON: %v", err)
	}

	// Optionally register the artifact (EPIC-003 verification status).
	if opts.artifactRegistered {
		regStore := artifact.NewRegistrationStore(filepath.Join(stateDir, "registration-index.json"))
		manifest := &artifact.Manifest{
			ArtifactID:   rel.ArtifactID,
			Version:      rel.Version,
			Source:       projectID,
			ProjectID:    projectID,
			Checksum:     "test-checksum",
			ChecksumType: "sha256",
			CreatedAt:    "2026-07-31T00:00:00Z",
		}
		if _, err := regStore.Register(manifest, "passed"); err != nil {
			t.Fatalf("register artifact: %v", err)
		}
		if err := regStore.Save(); err != nil {
			t.Fatalf("save registration index: %v", err)
		}
	}

	return serverRoot, projectID, releaseID
}

// TestServerReadinessCommand_RegistersUnderServer verifies that the
// readiness command is registered as a subcommand of the server command.
//
// Reference: ST-P9-02
func TestServerReadinessCommand_RegistersUnderServer(t *testing.T) {
	serverSub, _, err := rootCmd.Find([]string{"server", "readiness"})
	if err != nil {
		t.Fatalf("rootCmd.Find([\"server\", \"readiness\"]) returned error: %v", err)
	}
	if serverSub == nil {
		t.Fatal("rootCmd.Find([\"server\", \"readiness\"]) returned nil command")
	}
	if serverSub.Use != "readiness <project-id> <release-id>" {
		t.Errorf("command Use = %q, want %q", serverSub.Use, "readiness <project-id> <release-id>")
	}

	// Verify it is nested under server (not directly under root).
	_, _, err = rootCmd.Find([]string{"readiness"})
	if err == nil {
		t.Error("rootCmd.Find([\"readiness\"]) should have failed (readiness is not a direct subcommand)")
	}
}

// TestServerReadinessCommand_JsonFlag verifies that the --json flag is
// registered on the readiness command.
//
// Reference: ST-P9-02
func TestServerReadinessCommand_JsonFlag(t *testing.T) {
	readinessCmd, _, err := rootCmd.Find([]string{"server", "readiness"})
	if err != nil {
		t.Fatalf("failed to find readiness command: %v", err)
	}

	flag := readinessCmd.Flags().Lookup("json")
	if flag == nil {
		t.Error("--json flag should be on the readiness subcommand")
	}
}

// TestServerReadinessCommand_ServerRootFlag verifies that the
// --server-root flag is registered on the readiness command.
//
// Reference: ST-P9-02
func TestServerReadinessCommand_ServerRootFlag(t *testing.T) {
	readinessCmd, _, err := rootCmd.Find([]string{"server", "readiness"})
	if err != nil {
		t.Fatalf("failed to find readiness command: %v", err)
	}

	flag := readinessCmd.Flags().Lookup("server-root")
	if flag == nil {
		t.Error("--server-root flag should be on the readiness subcommand")
	}
}

// TestServerReadinessCommand_MissingArgs verifies that running the
// readiness command without project and release identity produces a clear
// usage error.
//
// Reference: ST-P9-02 (ST-009-002 §2)
func TestServerReadinessCommand_MissingArgs(t *testing.T) {
	dir := t.TempDir()

	_, _, stderr, err := executeCommand("server", "readiness", "--server-root", dir)
	if err == nil {
		t.Fatal("expected usage error without project/release identity, got nil")
	}
	if !contains(stderr, "project-id") || !contains(stderr, "release-id") {
		t.Errorf("stderr should explain the required arguments, got: %s", stderr)
	}
}

// TestServerReadinessCommand_ProjectNotFound verifies that an unknown
// project ID produces a registry error.
//
// Reference: ST-P9-02
func TestServerReadinessCommand_ProjectNotFound(t *testing.T) {
	dir := t.TempDir()

	_, _, stderr, err := executeCommand("server", "readiness", "unknown-project", "rel-123", "--server-root", dir)
	if err == nil {
		t.Fatal("expected error for unknown project, got nil")
	}
	if !contains(stderr, "could not load project registry") {
		t.Errorf("stderr should report the registry failure, got: %s", stderr)
	}
}

// TestServerReadinessCommand_ReleaseNotFound verifies that an unknown
// Release identity produces a not-found error.
//
// Reference: ST-P9-02
func TestServerReadinessCommand_ReleaseNotFound(t *testing.T) {
	serverRoot, projectID, _ := setupReadinessEnvironment(t, readinessSetupOptions{
		artifactRegistered: true,
		releaseStage:       release.StageReady,
	})

	_, _, stderr, err := executeCommand("server", "readiness", projectID, "rel-does-not-exist", "--server-root", serverRoot)
	if err == nil {
		t.Fatal("expected error for unknown release, got nil")
	}
	if !contains(stderr, "Release \"rel-does-not-exist\" not found") {
		t.Errorf("stderr should report the missing release, got: %s", stderr)
	}
}

// TestServerReadinessCommand_Ready verifies that when all prerequisites are
// satisfied — verified artifact, Ready-stage release, valid configuration,
// and healthy platform components — the readiness check reports the
// platform is ready and exits with code 0.
//
// Reference: ST-P9-02 (ST-009-002 AC1/AC3/AC4/AC7)
func TestServerReadinessCommand_Ready(t *testing.T) {
	serverRoot, projectID, releaseID := setupReadinessEnvironment(t, readinessSetupOptions{
		artifactRegistered: true,
		releaseStage:       release.StageReady,
	})

	_, stdout, stderr, err := executeCommand("server", "readiness", projectID, releaseID, "--server-root", serverRoot)
	if err != nil {
		t.Fatalf("readiness command returned unexpected error: %v", err)
	}

	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	if !contains(stdout, "Health: HEALTHY") {
		t.Errorf("stdout should contain 'Health: HEALTHY', got: %s", stdout)
	}
	if !contains(stdout, "System is ready for deployment operations") {
		t.Errorf("stdout should contain the ready summary, got: %s", stdout)
	}

	// The identity-based eligibility component is reported individually
	// (per-check pass/fail details are carried in the JSON output; the
	// human-readable view renders the component verdict).
	if !contains(stdout, "release_eligibility") {
		t.Errorf("stdout should contain the release_eligibility component, got: %s", stdout)
	}
	if !contains(stdout, "[PASS] release_eligibility") {
		t.Errorf("stdout should report the eligibility component as passed, got: %s", stdout)
	}
}

// TestServerReadinessCommand_ArtifactUnverified verifies that an artifact
// that is not registered (and therefore not verified) blocks readiness
// with actionable guidance to run artifact verification.
//
// Reference: ST-P9-02 (ST-009-002 AC3)
func TestServerReadinessCommand_ArtifactUnverified(t *testing.T) {
	serverRoot, projectID, releaseID := setupReadinessEnvironment(t, readinessSetupOptions{
		artifactRegistered: false, // no registration index → unverified
		releaseStage:       release.StageReady,
	})

	_, stdout, stderr, err := executeCommand("server", "readiness", projectID, releaseID, "--server-root", serverRoot)
	if err == nil {
		t.Fatal("expected error (exit code 1) when the artifact is unverified, got nil")
	}

	if !contains(stdout, "[FAIL] release_eligibility") {
		t.Errorf("stdout should report the failing eligibility component, got: %s", stdout)
	}
	if !contains(stdout, "artifact_verification") {
		t.Errorf("stdout should report the artifact verification check, got: %s", stdout)
	}

	// Blocker with the exact story guidance: run artifact verification.
	if !contains(stdout, "has not been verified") {
		t.Errorf("stdout blocker should report the artifact as unverified, got: %s", stdout)
	}
	if !contains(stdout, "`anvil artifact verify`") {
		t.Errorf("stdout blocker should reference artifact verification, got: %s", stdout)
	}

	if !contains(stderr, "server is not ready for release activation") {
		t.Errorf("stderr should contain 'server is not ready for release activation', got: %s", stderr)
	}
}

// TestServerReadinessCommand_ReleaseNotReady verifies that a Release not in
// the Ready stage blocks readiness with the current stage and the
// requirement to reach Ready.
//
// Reference: ST-P9-02 (ST-009-002 AC4)
func TestServerReadinessCommand_ReleaseNotReady(t *testing.T) {
	serverRoot, projectID, releaseID := setupReadinessEnvironment(t, readinessSetupOptions{
		artifactRegistered: true,
		releaseStage:       release.StageActive, // already activated — not Ready
	})

	_, stdout, _, err := executeCommand("server", "readiness", projectID, releaseID, "--server-root", serverRoot)
	if err == nil {
		t.Fatal("expected error (exit code 1) when the release is not Ready, got nil")
	}

	// Blocker reports the current stage and the requirement.
	if !contains(stdout, "is in stage active") {
		t.Errorf("stdout blocker should report the current stage, got: %s", stdout)
	}
	if !contains(stdout, "activation requires stage ready") {
		t.Errorf("stdout blocker should report the required stage, got: %s", stdout)
	}
}

// TestServerReadinessCommand_ConfigInvalid verifies that an invalid project
// configuration blocks readiness with a blocker identifying the specific
// key and expected format.
//
// Reference: ST-P9-02 (ST-009-002 AC2)
func TestServerReadinessCommand_ConfigInvalid(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	serverRoot, projectID, releaseID := setupReadinessEnvironment(t, readinessSetupOptions{
		artifactRegistered: true,
		releaseStage:       release.StageReady,
	})

	// Project directory with an invalid anvil.yaml: project.name must be
	// a string, not an integer.
	projectDir := t.TempDir()
	invalidConfig := "project:\n  name: 12345\n"
	if err := os.WriteFile(filepath.Join(projectDir, "anvil.yaml"), []byte(invalidConfig), 0644); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current working directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(orig); err != nil {
			t.Errorf("failed to restore original directory %q: %v", orig, err)
		}
	}()
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("failed to change to project directory: %v", err)
	}

	_, stdout, _, err := executeCommand("server", "readiness", projectID, releaseID, "--server-root", serverRoot)
	if err == nil {
		t.Fatal("expected error (exit code 1) when configuration is invalid, got nil")
	}

	// Blocker identifies the failing config check and the specific key.
	if !contains(stdout, "config_load") {
		t.Errorf("stdout blocker should identify the config_load check, got: %s", stdout)
	}
	if !contains(stdout, "project.name") {
		t.Errorf("stdout blocker should identify the invalid key, got: %s", stdout)
	}
}

// TestServerReadinessCommand_JsonReady verifies that --json produces the
// standard envelope with passed=true and the identity-based eligibility
// checks when the platform is ready.
//
// Reference: ST-P9-02
func TestServerReadinessCommand_JsonReady(t *testing.T) {
	serverRoot, projectID, releaseID := setupReadinessEnvironment(t, readinessSetupOptions{
		artifactRegistered: true,
		releaseStage:       release.StageReady,
	})

	_, stdout, _, err := executeCommand("server", "readiness", projectID, releaseID, "--server-root", serverRoot, "--json")
	if err != nil {
		t.Fatalf("readiness command returned unexpected error: %v", err)
	}

	if !contains(stdout, "\"passed\": true") {
		t.Errorf("JSON output should contain passed=true, got: %s", stdout)
	}
	if !contains(stdout, "\"status\": \"healthy\"") {
		t.Errorf("JSON output should contain data.status healthy, got: %s", stdout)
	}
	if !contains(stdout, "\"component\": \"release_eligibility\"") {
		t.Errorf("JSON output should contain the release_eligibility component, got: %s", stdout)
	}
	if !contains(stdout, "artifact_verification") {
		t.Errorf("JSON output should contain the artifact verification check, got: %s", stdout)
	}
}

// TestServerReadinessCommand_JsonNotReady verifies that --json works when
// the platform is not ready: the report is still written as JSON with the
// blockers and the command exits non-zero.
//
// Reference: ST-P9-02
func TestServerReadinessCommand_JsonNotReady(t *testing.T) {
	serverRoot, projectID, releaseID := setupReadinessEnvironment(t, readinessSetupOptions{
		artifactRegistered: false, // unverified artifact → not ready
		releaseStage:       release.StageReady,
	})

	_, stdout, _, err := executeCommand("server", "readiness", projectID, releaseID, "--server-root", serverRoot, "--json")

	if err == nil {
		t.Fatal("expected error when not ready, got nil")
	}

	if !contains(stdout, "\"passed\": false") {
		t.Errorf("JSON output should contain passed=false, got: %s", stdout)
	}
	if !contains(stdout, "\"blockers\"") {
		t.Errorf("JSON output should contain the blockers field, got: %s", stdout)
	}
}

// TestServerReadinessCommand_SecureExecution verifies that running the
// readiness check does not modify any platform state.
//
// Reference: ST-P9-02 (ST-009-002 AC8)
func TestServerReadinessCommand_SecureExecution(t *testing.T) {
	serverRoot, projectID, releaseID := setupReadinessEnvironment(t, readinessSetupOptions{
		artifactRegistered: true,
		releaseStage:       release.StageReady,
	})

	entriesBefore, err := os.ReadDir(serverRoot)
	if err != nil {
		t.Fatalf("failed to read directory before: %v", err)
	}

	_, _, _, _ = executeCommand("server", "readiness", projectID, releaseID, "--server-root", serverRoot)

	entriesAfter, err := os.ReadDir(serverRoot)
	if err != nil {
		t.Fatalf("failed to read directory after: %v", err)
	}

	if len(entriesBefore) != len(entriesAfter) {
		t.Errorf("directory contents changed: before=%d entries, after=%d entries",
			len(entriesBefore), len(entriesAfter))
	}
}
