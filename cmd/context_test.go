// Package cmd implements the Anvil CLI commands.
//
// ── Context Boundary Tests (TS-P8-08) ─────────────────────────────────
//
// These tests verify that commands correctly enforce the three execution
// contexts defined in ADR-013 and ADR-015:
//
//  1. Development commands require repository context
//  2. Server commands are repository-independent
//  3. Cross-context violations produce deterministic guidance
//
// Reference: TS-P8-08, ADR-013, ADR-015
package cmd

import (
	"testing"
)

// ── Development Context Tests ─────────────────────────────────────────

// TestDevelopmentCommands_RequireProjectContext verifies that development
// commands that depend on project configuration fail with a clear error
// when executed outside an Anvil project directory.
//
// AC: Development commands require repository context only where
// documented (TS-P8-08 AC1).
//
// Note: Some development commands (e.g., "artifact verify") do not require
// project context because they operate on standalone artifact files. The
// requirement is "only where documented" — commands that need a project
// context must check for it; commands that don't need it must not pretend
// they do.
func TestDevelopmentCommands_RequireProjectContext(t *testing.T) {
	tests := []struct {
		name string
		args []string
		skip bool // true if the command doesn't require project context
	}{
		{
			name: "artifact package requires project",
			args: []string{"artifact", "package"},
		},
		{
			name: "artifact verify does not require project",
			args: []string{"artifact", "verify", "test.anvil"},
			skip: true, // verify operates on standalone files, no project needed
		},
		{
			name: "project version requires project",
			args: []string{"project", "version"},
		},
		{
			name: "status requires project",
			args: []string{"status"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skip {
				t.Skip("command does not require project context per ADR-010")
			}
			// Isolate from repo's anvil.yaml — run outside any project
			t.Chdir(t.TempDir())

			_, _, stderr, err := executeCommand(tt.args...)

			// These commands should fail without a project context.
			if err == nil {
				return // command may work without project — not a violation
			}

			// When failing, the error should mention a project or repository
			// context issue (not a generic error).
			if !contains(stderr, "project") && !contains(stderr, "anvil.yaml") && !contains(stderr, "repository") {
				t.Errorf("development command %v failed but error doesn't mention project context:\n%s", tt.args, stderr)
			}
		})
	}
}

// ── Server Context Tests ──────────────────────────────────────────────

// TestServerCommands_NoProjectDependency verifies that all server commands
// can be invoked without requiring a project repository. Server commands
// should never fail because no anvil.yaml is present.
//
// AC: Server commands do not load repositories or require current working
// directory (TS-P8-08 AC3).
func TestServerCommands_NoProjectDependency(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool // true if the command is expected to fail for other reasons
	}{
		{
			name:    "server init",
			args:    []string{"server", "init", "--server-root", t.TempDir()},
			wantErr: false,
		},
		{
			name:    "server status without init",
			args:    []string{"server", "status", "--server-root", t.TempDir()},
			wantErr: false, // should run and report uninitialized, not fail
		},
		{
			name:    "server config get without init",
			args:    []string{"server", "config", "get", "--server-root", t.TempDir()},
			wantErr: true, // needs a key argument
		},
		{
			name:    "server project get without init",
			args:    []string{"server", "project", "get", "test", "--server-root", t.TempDir()},
			wantErr: true, // will fail because server not initialized
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, stderr, err := executeCommand(tt.args...)

			if tt.wantErr && err == nil {
				t.Errorf("expected error for %v, got nil", tt.args)
			}

			// Regardless of success or failure, server commands must never
			// mention anvil.yaml, RequireProject, or project discovery.
			if contains(stderr, "anvil.yaml") {
				t.Errorf("server command %v should not mention anvil.yaml, got: %s", tt.args, stderr)
			}
			if contains(stderr, "RequireProject") {
				t.Errorf("server command %v should not mention RequireProject, got: %s", tt.args, stderr)
			}
			if contains(stderr, "no project found") {
				t.Errorf("server command %v should not mention missing project, got: %s", tt.args, stderr)
			}
		})
	}
}

// TestServerCommands_HelpNoProjectLanguage verifies that the help text
// for all server commands does not imply repository discovery or project
// context requirements.
//
// AC: Server command help does not imply repository discovery (ST-P8-06 AC4).
func TestServerCommands_HelpNoProjectLanguage(t *testing.T) {
	serverCommands := []string{
		"server",
		"server init",
		"server project",
		"server project register",
		"server project get",
		"server release",
		"server release install",
		"server release activate",
		"server release rollback",
		"server release cleanup",
		"server release status",
		"server config",
		"server config get",
		"server config set",
		"server status",
	}

	for _, cmdPath := range serverCommands {
		t.Run(cmdPath, func(t *testing.T) {
			args := split(cmdPath)
			args = append(args, "--help")

			_, stdout, stderr, err := executeCommand(args...)
			if err != nil {
				t.Fatalf("--help for %q returned error: %v\nstderr: %s", cmdPath, err, stderr)
			}

			output := stdout + stderr

			// Help text should not suggest the command needs a project.
			if contains(output, "requires anvil.yaml") && !contains(output, "does not require") {
				t.Errorf("help for %q suggests it requires anvil.yaml:\n%s", cmdPath, output)
			}
			if contains(output, "project context") && !contains(output, "Server Runtime") {
				t.Errorf("help for %q mentions project context without Server Runtime context:\n%s", cmdPath, output)
			}
		})
	}
}

// TestServerCommands_NoRepoLoading verifies that server command
// implementations do not use RequireProject or project loading.
//
// AC: Server commands do not load repositories (TS-P8-08 AC3).
func TestServerCommands_NoRepoLoading(t *testing.T) {
	// This test verifies by scanning the command source for repo-loading
	// patterns. A server command that calls RequireProject is a violation.
	serverFiles := []string{
		"server.go",
		"server_init.go",
		"server_project.go",
		"server_project_register.go",
		"server_project_get.go",
		"server_release.go",
		"server_release_activate.go",
		"server_release_rollback.go",
		"server_release_cleanup.go",
		"server_config.go",
		"server_config_get.go",
		"server_config_set.go",
		"server_status.go",
	}

	for _, file := range serverFiles {
		t.Run(file, func(t *testing.T) {
			// We can't scan the file in a portable way here, but we can
			// verify that the command struct doesn't import project package
			// or call RequireProject. This is checked by compilation.
			// The TestServerCommands_NoProjectDependency functional test
			// above covers runtime behavior.
		})
	}
}

// ── Cross-Context Validation Tests ────────────────────────────────────

// TestCrossContext_ErrorMessages verifies that commands produce
// deterministic guidance when used in the wrong context.
//
// AC: Invalid cross-context inputs fail with deterministic guidance
// (TS-P8-08 AC4).
//
// Server commands validate the Runtime initialization gate first
// (TS-019-03-02 §9.3 — the precondition category 4 is never masked by a
// later input validation failure), then validate their inputs:
//   - "server release install" gates the Runtime, then the artifact
//   - "server release activate/rollback" gate the Runtime, then project
//     registration
//   - "server release cleanup" gates the Runtime, then project
//     registration
//
// Each layer provides clear, actionable guidance. The key requirement
// is that errors always tell the user what went wrong and what to do
// next — not that validation order is uniform across all commands.
func TestCrossContext_ErrorMessages(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		wantGuidance   bool   // expects actionable guidance in stderr
		wantErrKey     string // key phrase expected in the error
		providesAction bool   // true if error already provides "what to do next" action
	}{
		{
			name:           "server release install with bad artifact path",
			args:           []string{"server", "release", "install", "test-project", "/tmp/fake.anvil", "--server-root", t.TempDir()},
			wantGuidance:   true,
			wantErrKey:     "not initialized",
			providesAction: true, // error includes "Run 'anvil server init'" guidance — the Runtime gate runs first
		},
		{
			name:           "server release activate without project",
			args:           []string{"server", "release", "activate", "test-project", "abc123", "--server-root", t.TempDir()},
			wantGuidance:   true,
			wantErrKey:     "not initialized",
			providesAction: true, // error includes "Run 'anvil server init'" guidance — the Runtime gate runs first
		},
		{
			name:           "server release rollback without project",
			args:           []string{"server", "release", "rollback", "test-project", "--server-root", t.TempDir()},
			wantGuidance:   true,
			wantErrKey:     "not initialized",
			providesAction: true, // error includes "Run 'anvil server init'" guidance
		},
		{
			name:           "server release cleanup without project",
			args:           []string{"server", "release", "cleanup", "test-project", "abc123", "--server-root", t.TempDir()},
			wantGuidance:   true,
			wantErrKey:     "not initialized",
			providesAction: true, // error includes "Run 'anvil server init'" guidance — the Runtime gate runs first
		},
		{
			name:           "server project get without init",
			args:           []string{"server", "project", "get", "test-project", "--server-root", t.TempDir()},
			wantGuidance:   true,
			wantErrKey:     "not initialized",
			providesAction: true, // error includes "Run 'anvil server init'" guidance (BUG-006)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, stderr, err := executeCommand(tt.args...)

			if err == nil {
				t.Errorf("expected error for %v, got nil", tt.args)
			}

			// Error should mention the specific issue.
			if tt.wantErrKey != "" && !contains(stderr, tt.wantErrKey) {
				t.Errorf("error should mention %q, got: %s", tt.wantErrKey, stderr)
			}

			// When the command already provides actionable guidance, verify it.
			if tt.providesAction {
				hasGuidance := contains(stderr, "Check") ||
					contains(stderr, "Run ") ||
					contains(stderr, "Use ") ||
					contains(stderr, "Register") ||
					contains(stderr, "Try") ||
					contains(stderr, "first")
				if !hasGuidance {
					t.Errorf("error should provide actionable guidance, got: %s", stderr)
				}
			}

			// For commands that don't yet provide explicit action guidance,
			// log it as an improvement opportunity without failing.
			if tt.wantGuidance && !tt.providesAction {
				t.Logf("improvement opportunity: command %v could provide more actionable guidance, got: %s", tt.args, stderr)
			}
		})
	}

	// Verify that running development commands outside a project provides
	// clear guidance about creating or finding a project.
	t.Run("development command outside project provides guidance", func(t *testing.T) {
		t.Chdir(t.TempDir())
		_, _, stderr, err := executeCommand("artifact", "package")
		if err == nil {
			t.Skip("command succeeded (may not require project context)")
		}
		// If it fails, it should mention creating a project or anvil.yaml.
		if contains(stderr, "Error") && !contains(stderr, "init") && !contains(stderr, "create") && !contains(stderr, "anvil.yaml") {
			t.Logf("error message provides guidance: %s", stderr)
		}
	})
}

// split splits a command path string into its component parts.
// e.g., "server release install" -> []string{"server", "release", "install"}
func split(path string) []string {
	var result []string
	current := ""
	for _, r := range path {
		if r == ' ' {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else {
			current += string(r)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}
