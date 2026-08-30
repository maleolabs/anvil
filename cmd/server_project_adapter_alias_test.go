// ── project.adapter alias warning on CLI reads (TS-019-02-02) ────────
//
// Every CLI read of a project's standard — "anvil server project get",
// "anvil server status <id>" (detail view), and "anvil server project
// register" (--adapter flag and the interactive summary) — emits the
// StandardAdapterAliasWarning when the project declares the legacy
// project.adapter key. The alias value stays honored during the
// deprecation window: the resolved standard still displays, and the
// registration still succeeds. Warnings go to stderr only — stdout stays
// machine-readable (T-003/T-005 precedent) — which this suite asserts
// explicitly.
//
// The registration-automation contract (docs/operations/
// registration-automation-contract.md) documents "--adapter ... with a
// warning"; TestServerProjectRegister_AdapterFlagWarning makes that
// claim true.
//
// REMOVAL (end of the deprecation window, ADR-032 §7): flip these tests
// to post-removal expectations (the alias rejected / no longer read)
// following the phantom-target-id removal precedent
// (phantom_target_id_removal_test.go) and the checklist on
// server.StandardAdapterAliasWarning.
package cmd

import (
	"strings"
	"testing"
)

// registerProject registers a project with the given extra args and
// returns stdout/stderr of the registration call.
func registerProject(t *testing.T, dir string, extraArgs ...string) (string, string) {
	t.Helper()

	if _, _, _, err := executeCommand("server", "init", "--server-root", dir); err != nil {
		t.Fatalf("server init failed: %v", err)
	}

	args := []string{
		"server", "project", "register",
		"--server-root", dir,
		"--project-id", "alias-test",
		"--install-root", "/srv/alias-test",
		"--non-interactive",
	}
	args = append(args, extraArgs...)

	_, stdout, stderr, err := executeCommand(args...)
	if err != nil {
		t.Fatalf("registration failed: %v\nstderr: %s", err, stderr)
	}
	return stdout, stderr
}

// TestServerProjectGet_AdapterAliasWarning verifies that "anvil server
// project get" on a project declaring the legacy project.adapter key
// emits the deprecation warning on stderr while stdout stays
// machine-readable and the alias value keeps displaying (TS-019-02-02:
// alias-with-warning on read).
func TestServerProjectGet_AdapterAliasWarning(t *testing.T) {
	dir := t.TempDir()
	registerProject(t, dir, "--adapter", "node")

	_, stdout, stderr, err := executeCommand(
		"server", "project", "get",
		"alias-test",
		"--server-root", dir,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	assertAliasWarningOnStderr(t, stderr)
	assertStdoutClean(t, stdout)
	if !contains(stdout, "node") {
		t.Errorf("stdout should still show the resolved standard (alias honored), got: %s", stdout)
	}
}

// TestServerStatus_AdapterAliasWarning verifies that "anvil server
// status <id>" (detail read) emits the deprecation warning on stderr for
// a project declaring the legacy project.adapter key while stdout stays
// machine-readable.
func TestServerStatus_AdapterAliasWarning(t *testing.T) {
	dir := t.TempDir()
	registerProject(t, dir, "--adapter", "node")

	_, stdout, stderr, err := executeCommand(
		"server", "status",
		"alias-test",
		"--server-root", dir,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	assertAliasWarningOnStderr(t, stderr)
	assertStdoutClean(t, stdout)
	if !contains(stdout, "node") {
		t.Errorf("stdout should still show the resolved standard (alias honored), got: %s", stdout)
	}
}

// TestServerProjectRegister_AdapterFlagWarning verifies that using the
// legacy --adapter flag on "anvil server project register" emits the
// deprecation warning on stderr while the registration still succeeds —
// the registration automation contract documents "--adapter ... with a
// warning" (TS-019-02-02).
func TestServerProjectRegister_AdapterFlagWarning(t *testing.T) {
	dir := t.TempDir()
	_, stderr := registerProject(t, dir, "--adapter", "node")

	assertAliasWarningOnStderr(t, stderr)
}

// TestServerProjectGet_NoWarningForCanonicalStandard verifies that a
// project declaring only the canonical project.standard key never warns
// on read — the canonical path is unaffected by the deprecation and by
// the window-end removal.
func TestServerProjectGet_NoWarningForCanonicalStandard(t *testing.T) {
	dir := t.TempDir()
	registerProject(t, dir, "--standard", "laravel")

	_, stdout, stderr, err := executeCommand(
		"server", "project", "get",
		"alias-test",
		"--server-root", dir,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	if contains(stderr, "project.adapter is deprecated") {
		t.Errorf("canonical-only project read must not warn, stderr: %s", stderr)
	}
	if !contains(stdout, "laravel") {
		t.Errorf("stdout should show the canonical standard, got: %s", stdout)
	}
}

// assertAliasWarningOnStderr fails the test unless stderr carries the
// project.adapter deprecation warning naming project.standard.
func assertAliasWarningOnStderr(t *testing.T, stderr string) {
	t.Helper()
	if !strings.Contains(stderr, "project.adapter is deprecated") {
		t.Errorf("stderr must carry the project.adapter deprecation warning, got: %q", stderr)
	}
	if !strings.Contains(stderr, "project.standard") {
		t.Errorf("stderr warning must name the replacement project.standard, got: %q", stderr)
	}
}

// assertStdoutClean fails the test unless stdout carries no deprecation
// warning — machine-readable output integrity (T-003/T-005 precedent).
func assertStdoutClean(t *testing.T, stdout string) {
	t.Helper()
	if strings.Contains(stdout, "project.adapter is deprecated") {
		t.Errorf("stdout must not carry the deprecation warning, got: %s", stdout)
	}
	if strings.Contains(stdout, "Warning:") {
		t.Errorf("stdout must not carry warnings (stderr-only channel), got: %s", stdout)
	}
}
