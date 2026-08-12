// Package cmd implements the Anvil CLI commands.
//
// ── Non-Gating Guarantee Regression (ADR-036 §3, TS-015-05-03) ─────────
//
// These tests enforce the read-only half of the diagnostics scope
// guarantee: diagnostics and observability commands never mutate lifecycle
// state, and state queries observe but never gate. The structural
// (import-graph) half lives in internal/nongating; this file proves the
// behavioral half — every diagnostics command runs against a populated
// lifecycle tree and the tree is byte-identical afterwards.
package cmd

import (
	"reflect"
	"testing"
)

// TestDiagnosticsSurface_ReadOnly verifies that the full diagnostics and
// observability command surface is read-only against a populated lifecycle
// tree (ADR-036 §3, TS-015-05-03):
//
//   - "anvil server status" — lifecycle observability (TS-015-05-01)
//   - "anvil server doctor" — demoted informational diagnostics (TS-015-05-02)
//   - "anvil server readiness" — demoted informational diagnostics (TS-015-05-02)
//   - "anvil system inspect release" — targeted inspection of lifecycle state
//   - "anvil system inspect runtime" / "environment" — targeted inspection
//     of the server root
//
// The server root (which is also the project install root — the tree
// carrying releases, runtime state, artifact store, and registry) must be
// byte-identical after every command: nothing created, removed, or
// modified. Each command must also succeed — a broken surface would fail
// the test before the read-only assertion can run.
func TestDiagnosticsSurface_ReadOnly(t *testing.T) {
	serverRoot := t.TempDir()
	projectID := "non-gating-test"

	// Populate lifecycle state through the production paths: server init,
	// project registration, and two install + activate cycles so the tree
	// carries releases, stage transitions, runtime state, and an artifact
	// store — everything the diagnostics surface observes.
	_, _, _, err := executeCommand("server", "init", "--server-root", serverRoot)
	if err != nil {
		t.Fatalf("server init failed: %v", err)
	}
	_, _, _, err = executeCommand(
		"server", "project", "register",
		"--server-root", serverRoot,
		"--project-id", projectID,
		"--install-root", serverRoot,
		"--non-interactive",
	)
	if err != nil {
		t.Fatalf("registration failed: %v", err)
	}
	installAndActivateViaCoordinator(t, serverRoot, projectID, "1.0.0", "<?php // non-gating A\n")
	relB := installAndActivateViaCoordinator(t, serverRoot, projectID, "1.1.0", "<?php // non-gating B\n")

	before := snapshotStatusFiles(t, serverRoot)

	diagnostics := [][]string{
		{"server", "status", projectID, "--server-root", serverRoot},
		{"server", "doctor", "--server-root", serverRoot},
		{"server", "readiness", projectID, relB.ID.String(), "--server-root", serverRoot},
		{"system", "inspect", "release", projectID, relB.ID.String(), "--server-root", serverRoot},
		{"system", "inspect", "runtime", "--server-root", serverRoot},
		{"system", "inspect", "environment", "--server-root", serverRoot},
	}
	for _, args := range diagnostics {
		_, _, stderr, err := executeCommand(args...)
		if err != nil {
			t.Fatalf("executeCommand(%q) failed: %v\nstderr: %s", args, err, stderr)
		}
	}

	assertSnapshotUnchanged(t, "server root", before, snapshotStatusFiles(t, serverRoot))
}

// assertSnapshotUnchanged fails the test when a snapshot changed between
// two observations: a file removed, a file created, or a file modified
// (byte comparison).
func assertSnapshotUnchanged(t *testing.T, label string, before, after map[string]string) {
	t.Helper()

	for path, want := range before {
		got, ok := after[path]
		if !ok {
			t.Errorf("%s: file %s removed by diagnostics surface", label, path)
			continue
		}
		if got != want {
			t.Errorf("%s: file %s changed by diagnostics surface", label, path)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			t.Errorf("%s: file %s created by diagnostics surface", label, path)
		}
	}
	if !reflect.DeepEqual(before, after) {
		t.Errorf("%s: snapshot differs (before=%d files, after=%d files)", label, len(before), len(after))
	}
}
