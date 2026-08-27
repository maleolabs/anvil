// ── project.adapter alias warning on coordinator reads (TS-019-02-02) ─
//
// During the deprecation window the coordinator resolves a project's
// framework through ProjectSection.StandardName() at four read sites —
// activation (runAdapterActivation), rollback (runAdapterRollback),
// verification (runAdapterVerification, on install), and the release
// build (BuildRelease). Every read of a project.adapter declaration
// emits the StandardAdapterAliasWarning through the coordinator's
// warning writer (WithWarningWriter; default os.Stderr) — never stdout,
// so machine-readable output stays unpolluted (T-003/T-005 precedent).
// The alias value itself keeps mapping to project.standard semantics
// during the window: the stub adapter is still invoked exactly as with
// the canonical key.
//
// REMOVAL (end of the deprecation window, ADR-032 §7): these tests pin
// the window behavior — the alias works AND warns. The removal is a
// governed, explicit change (never silent): flip these tests to
// post-removal expectations (alias rejected / no longer read) following
// the phantom-target-id removal precedent (cmd/phantom_target_id_removal_test.go)
// and the checklist on StandardAdapterAliasWarning. Projects declaring
// only the canonical project.standard key must keep passing unchanged —
// see TestCoordinator_CanonicalStandard_NoWarning.
package server

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestCoordinator_Install_WarnsOnLegacyAdapterRead verifies that
// installing a release for a project declaring the legacy
// project.adapter key emits the deprecation warning (verification read
// site) while the alias value stays honored.
func TestCoordinator_Install_WarnsOnLegacyAdapterRead(t *testing.T) {
	serverRoot := t.TempDir()
	releaseID := "rel-warn-verify"
	logPath := filepath.Join(t.TempDir(), "stub.log")
	executable := writeServerStubAdapter(t, logPath, "", "")

	projectID, _ := setupAdapterActivateEnvironment(t, serverRoot, releaseID, "laravel")
	artifactPath := createTestArtifact(t, projectID)

	var warnBuf bytes.Buffer
	coord := NewServerReleaseCoordinator(serverRoot, WithWarningWriter(&warnBuf))
	adapterCoordinatorSeams(coord, executable)

	if _, err := coord.Install(projectID, artifactPath); err != nil {
		t.Fatalf("Install returned unexpected error: %v", err)
	}

	assertAliasWarning(t, warnBuf.String())
	if lines := logLines(t, logPath); len(lines) == 0 {
		t.Fatal("stub adapter was never invoked: the alias value must stay honored during the window")
	}
}

// TestCoordinator_Activate_WarnsOnLegacyAdapterRead verifies that
// activating a release warns on the legacy project.adapter read
// (activation read site).
func TestCoordinator_Activate_WarnsOnLegacyAdapterRead(t *testing.T) {
	serverRoot := t.TempDir()
	releaseID := "rel-warn-activate"
	logPath := filepath.Join(t.TempDir(), "stub.log")
	executable := writeServerStubAdapter(t, logPath, "", "")

	projectID, _ := setupAdapterActivateEnvironment(t, serverRoot, releaseID, "laravel")

	var warnBuf bytes.Buffer
	coord := NewServerReleaseCoordinator(serverRoot, WithWarningWriter(&warnBuf))
	adapterCoordinatorSeams(coord, executable)

	if err := coord.Activate(projectID, releaseID); err != nil {
		t.Fatalf("Activate returned unexpected error: %v", err)
	}

	assertAliasWarning(t, warnBuf.String())
	if lines := logLines(t, logPath); len(lines) == 0 {
		t.Fatal("stub adapter was never invoked: the alias value must stay honored during the window")
	}
}

// TestCoordinator_Rollback_WarnsOnLegacyAdapterRead verifies that
// rolling back a release warns on the legacy project.adapter read
// (rollback read site).
func TestCoordinator_Rollback_WarnsOnLegacyAdapterRead(t *testing.T) {
	serverRoot := t.TempDir()
	targetReleaseID := "rel-warn-target"
	activeReleaseID := "rel-warn-active"
	logPath := filepath.Join(t.TempDir(), "stub.log")
	executable := writeServerStubAdapter(t, logPath, "", "")

	projectID := setupAdapterRollbackEnvironment(t, serverRoot, targetReleaseID, activeReleaseID, "laravel")

	var warnBuf bytes.Buffer
	coord := NewServerReleaseCoordinator(serverRoot, WithWarningWriter(&warnBuf))
	adapterCoordinatorSeams(coord, executable)

	if _, err := coord.Rollback(projectID); err != nil {
		t.Fatalf("Rollback returned unexpected error: %v", err)
	}

	assertAliasWarning(t, warnBuf.String())
	if lines := logLines(t, logPath); len(lines) == 0 {
		t.Fatal("stub adapter was never invoked: the alias value must stay honored during the window")
	}
}

// TestCoordinator_BuildRelease_WarnsOnLegacyAdapterRead verifies that
// building a release for a project declaring the legacy project.adapter
// key emits the deprecation warning (build read site) while the alias
// value stays honored.
func TestCoordinator_BuildRelease_WarnsOnLegacyAdapterRead(t *testing.T) {
	serverRoot := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "stub.log")
	executable := writeServerBuildStubAdapter(t, logPath, "", "")

	projectID, _ := setupAdapterBuildEnvironment(t, serverRoot, "laravel")

	var warnBuf bytes.Buffer
	coord := NewServerReleaseCoordinator(serverRoot, WithWarningWriter(&warnBuf))
	adapterCoordinatorSeams(coord, executable)

	result, err := coord.BuildRelease(context.Background(), projectID, BuildReleaseOptions{})
	if err != nil {
		t.Fatalf("BuildRelease returned unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("BuildRelease.Success = false, want true")
	}

	assertAliasWarning(t, warnBuf.String())
	if payload := buildInvocation(t, logPath); payload == "" {
		t.Fatal("stub adapter build was never invoked: the alias value must stay honored during the window")
	}
}

// TestCoordinator_CanonicalStandard_NoWarning verifies that a project
// declaring only the canonical project.standard key never warns on any
// read site — the canonical path is unaffected by the deprecation and by
// the window-end removal (build read site stands for all four, which
// share the WarnIfLegacyAdapter gate).
func TestCoordinator_CanonicalStandard_NoWarning(t *testing.T) {
	serverRoot := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "stub.log")
	executable := writeServerBuildStubAdapter(t, logPath, "", "")

	projectID, _ := setupStandardBuildEnvironment(t, serverRoot, "laravel")

	var warnBuf bytes.Buffer
	coord := NewServerReleaseCoordinator(serverRoot, WithWarningWriter(&warnBuf))
	adapterCoordinatorSeams(coord, executable)

	result, err := coord.BuildRelease(context.Background(), projectID, BuildReleaseOptions{})
	if err != nil {
		t.Fatalf("BuildRelease returned unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("BuildRelease.Success = false, want true")
	}

	if warnBuf.String() != "" {
		t.Errorf("canonical project.standard read wrote warning %q, want no warning", warnBuf.String())
	}
}

// assertAliasWarning fails the test unless got carries the
// project.adapter deprecation warning naming project.standard.
func assertAliasWarning(t *testing.T, got string) {
	t.Helper()
	if !strings.Contains(got, "project.adapter is deprecated") {
		t.Errorf("warning output must announce the project.adapter deprecation, got: %q", got)
	}
	if !strings.Contains(got, "project.standard") {
		t.Errorf("warning output must name the replacement project.standard, got: %q", got)
	}
}
