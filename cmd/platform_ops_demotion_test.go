// Package cmd implements the Anvil CLI commands.
//
// ── Platform-Ops Demotion Regression (ADR-036, TS-015-05-02) ──────────
//
// These tests lock the diagnostics scope boundary drawn by ADR-036 §3:
// the platform-ops breadth (system health, readiness verdicts,
// recommendations-style diagnostics) is demoted or removed, while
// lifecycle observability (what is active, what can roll back, release
// status, state queries) stays intact.
package cmd

import (
	"strings"
	"testing"
)

// TestPlatformOpsDemotion_RemovedSurfacesUnregistered verifies that the
// removed platform-ops command surfaces are no longer registered anywhere
// in the CLI (ADR-036 §3, TS-015-05-02):
//
//   - "anvil system health" — system-health/readiness verdict surface
//   - "anvil system diagnose" — recommendations-style report surface
//
// Invoking them must produce an unknown-command error.
func TestPlatformOpsDemotion_RemovedSurfacesUnregistered(t *testing.T) {
	removed := [][]string{
		{"system", "health"},
		{"system", "diagnose"},
	}

	for _, args := range removed {
		if cmd, _, _ := rootCmd.Find(args); cmd != nil && cmd.Name() == args[len(args)-1] {
			t.Errorf("command %q must be removed (ADR-036 demotion), still registered as %q", args, cmd.Name())
		}

		_, _, stderr, err := executeCommand(args...)
		if err == nil {
			t.Errorf("executeCommand(%q) should return an error (unknown command), got nil", args)
		}
		if !strings.Contains(stderr, "unknown command") {
			t.Errorf("executeCommand(%q) stderr should indicate an unknown command, got: %q", args, stderr)
		}
	}
}

// TestPlatformOpsDemotion_DemotedSurfacesRemainRegistered verifies that the
// demoted-but-present diagnostics remain registered as optional,
// non-governing surfaces (ADR-036 §3, TS-015-05-02): their reports are
// informational and their exit codes never gate lifecycle operations.
func TestPlatformOpsDemotion_DemotedSurfacesRemainRegistered(t *testing.T) {
	demoted := [][]string{
		{"server", "readiness"},
		{"server", "doctor"},
		{"system", "inspect"},
	}

	for _, args := range demoted {
		cmd, _, err := rootCmd.Find(args)
		if err != nil {
			t.Errorf("rootCmd.Find(%q) returned error: %v", args, err)
			continue
		}
		if cmd == nil {
			t.Errorf("demoted command %q must remain registered, got nil", args)
		}
	}
}

// TestPlatformOpsDemotion_LifecycleObservabilityIntact verifies that the
// lifecycle observability surface (TS-015-05-01) is unaffected by the
// platform-ops demotion: state queries and release status surfaces remain
// registered (ADR-036 §3).
func TestPlatformOpsDemotion_LifecycleObservabilityIntact(t *testing.T) {
	observability := [][]string{
		{"server", "status"},
		{"server", "release", "status"},
		{"server", "release", "active"},
		{"server", "release", "history"},
	}

	for _, args := range observability {
		cmd, _, err := rootCmd.Find(args)
		if err != nil {
			t.Errorf("rootCmd.Find(%q) returned error: %v", args, err)
			continue
		}
		if cmd == nil {
			t.Errorf("lifecycle observability command %q must remain registered (TS-015-05-01), got nil", args)
		}
	}
}
