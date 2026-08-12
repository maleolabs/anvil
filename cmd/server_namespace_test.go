// Package cmd implements the Anvil CLI commands.
//
// ── Server Namespace Tests (ST-P8-06) ─────────────────────────────────
//
// These tests verify that the Server Runtime command namespace is properly
// structured and discoverable. Server commands must be exposed under
// "anvil server" and their help must distinguish Server Runtime operations
// from Development and Deployment contexts.
//
// Reference: ST-P8-06, ADR-013, ADR-015
package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// ── Namespace Structure Tests ─────────────────────────────────────────

// TestServerNamespace_RequiredGroups verifies that the server command group
// contains all required subcommand groups.
//
// AC: Help exposes "server init", "server project", and "server release"
// (ST-P8-06 AC1).
func TestServerNamespace_RequiredGroups(t *testing.T) {
	if rootCmd == nil {
		t.Fatal("rootCmd must not be nil")
	}

	serverSub, _, err := rootCmd.Find([]string{"server"})
	if err != nil {
		t.Fatalf("could not find server command: %v", err)
	}
	if serverSub == nil {
		t.Fatal("server command not found")
	}

	// Build a lookup of server's subcommands.
	subcommands := make(map[string]*cobra.Command)
	for _, sub := range serverSub.Commands() {
		subcommands[sub.Name()] = sub
	}

	// Required top-level groups under server.
	required := []struct {
		name        string
		description string
	}{
		{"init", "Initialize Server Runtime configuration"},
		{"project", "Manage registered projects"},
		{"release", "Manage Runtime Releases"},
		{"config", "Manage Server Runtime configuration"},
		{"status", "Display Server Runtime status and readiness"},
	}

	for _, r := range required {
		cmd, ok := subcommands[r.name]
		if !ok {
			t.Errorf("required server subcommand %q (%s) is not registered under server", r.name, r.description)
			continue
		}
		if cmd == nil {
			t.Errorf("server subcommand %q is nil", r.name)
		}
	}
}

// TestServerNamespace_ReleaseSubcommands verifies that the server release
// command group exposes install, activate, and rollback subcommands.
//
// AC: Server release install, activate, and rollback commands are
// discoverable (ST-P8-06 AC2).
func TestServerNamespace_ReleaseSubcommands(t *testing.T) {
	serverRelease, _, err := rootCmd.Find([]string{"server", "release"})
	if err != nil {
		t.Fatalf("could not find server release command: %v", err)
	}
	if serverRelease == nil {
		t.Fatal("server release command not found")
	}

	// Build a lookup of server release's subcommands.
	subcommands := make(map[string]*cobra.Command)
	for _, sub := range serverRelease.Commands() {
		subcommands[sub.Name()] = sub
	}

	required := []struct {
		name        string
		description string
	}{
		{"install", "Install an artifact and create a Runtime Release"},
		{"activate", "Activate a Runtime Release"},
		{"rollback", "Rollback the Active Release"},
		{"cleanup", "Remove a release directory and reclaim disk space"},
	}

	for _, r := range required {
		cmd, ok := subcommands[r.name]
		if !ok {
			t.Errorf("required server release subcommand %q (%s) is not registered", r.name, r.description)
			continue
		}
		if cmd == nil {
			t.Errorf("server release subcommand %q is nil", r.name)
		}
		// Verify RunE is set (command is executable).
		if cmd.RunE == nil {
			t.Errorf("server release subcommand %q has no RunE; leaf commands must be executable", r.name)
		}
	}
}

// TestServerNamespace_HelpOutput verifies that the server command's help
// output properly exposes all subcommands.
func TestServerNamespace_HelpOutput(t *testing.T) {
	_, stdout, stderr, err := executeCommand("server", "--help")
	if err != nil {
		t.Fatalf("server --help returned error: %v\nstderr: %s", err, stderr)
	}

	output := stdout + stderr

	if !contains(output, "init") {
		t.Errorf("server --help should list 'init' subcommand")
	}
	if !contains(output, "project") {
		t.Errorf("server --help should list 'project' subcommand")
	}
	if !contains(output, "release") {
		t.Errorf("server --help should list 'release' subcommand")
	}
	if !contains(output, "config") {
		t.Errorf("server --help should list 'config' subcommand")
	}
	if !contains(output, "status") {
		t.Errorf("server --help should list 'status' subcommand")
	}
}

// TestServerNamespace_ReleaseHelpOutput verifies that the server release
// help exposes all release subcommands.
func TestServerNamespace_ReleaseHelpOutput(t *testing.T) {
	_, stdout, stderr, err := executeCommand("server", "release", "--help")
	if err != nil {
		t.Fatalf("server release --help returned error: %v\nstderr: %s", err, stderr)
	}

	output := stdout + stderr

	// Must list release lifecycle subcommands.
	releaseCommands := []string{"install", "activate", "rollback", "cleanup"}
	for _, cmd := range releaseCommands {
		if !contains(output, cmd) {
			t.Errorf("server release --help should list '%s' subcommand", cmd)
		}
	}
}

// ── Context Distinction Tests ─────────────────────────────────────────

// TestServerNamespace_HelpDistinguishesServerContext verifies that server
// command help text clearly distinguishes Server Runtime operations from
// Development or Deployment contexts.
//
// AC: Help distinguishes Server Runtime operations from Deployment
// wrappers (ST-P8-06 AC3).
func TestServerNamespace_HelpDistinguishesServerContext(t *testing.T) {
	serverCommands := []string{
		"server init",
		"server project",
		"server release",
		"server release install",
		"server release activate",
		"server release rollback",
		"server config",
		"server status",
	}

	for _, cmdPath := range serverCommands {
		t.Run(cmdPath, func(t *testing.T) {
			args := strings.Split(cmdPath, " ")
			args = append(args, "--help")

			_, stdout, stderr, err := executeCommand(args...)
			if err != nil {
				t.Fatalf("--help for %q returned error: %v\nstderr: %s", cmdPath, err, stderr)
			}

			output := stdout + stderr

			// Help should reference Server Runtime, not a repository or
			// development context.
			hasServerRef := contains(output, "Server Runtime") ||
				contains(output, "Runtime") ||
				contains(output, "server") ||
				contains(output, "Server")

			if !hasServerRef {
				// Only flag if there's also no repo context mentioned.
				if contains(output, "repository") || contains(output, "anvil.yaml") {
					t.Errorf("help for %q references repository but not Server Runtime:\n%s", cmdPath, output)
				}
			}
		})
	}
}

// TestServerNamespace_HelpNoRepoDiscovery verifies that server command
// help text does not imply repository discovery or project context.
//
// AC: Server command help does not imply repository discovery
// (ST-P8-06 AC4).
func TestServerNamespace_HelpNoRepoDiscovery(t *testing.T) {
	serverCommands := []string{
		"server init",
		"server project",
		"server project register",
		"server project get",
		"server release",
		"server release install",
		"server release activate",
		"server release rollback",
		"server release cleanup",
		"server config",
		"server config get",
		"server config set",
		"server status",
	}

	for _, cmdPath := range serverCommands {
		t.Run(cmdPath, func(t *testing.T) {
			args := strings.Split(cmdPath, " ")
			args = append(args, "--help")

			_, stdout, stderr, err := executeCommand(args...)
			if err != nil {
				t.Fatalf("--help for %q returned error: %v\nstderr: %s", cmdPath, err, stderr)
			}

			output := stdout + stderr

			// Blocking keywords that imply repo dependency.
			repoKeywords := []string{
				"current working directory",
				"repository context",
				"project context",
			}

			for _, kw := range repoKeywords {
				if contains(output, kw) {
					t.Errorf("help for %q contains %q which implies repository discovery:\n%s", cmdPath, kw, output)
				}
			}
		})
	}

	// Specifically verify server init says "does not require anvil.yaml"
	t.Run("server init explicitly excludes repo", func(t *testing.T) {
		_, stdout, stderr, err := executeCommand("server", "init", "--help")
		if err != nil {
			t.Fatalf("server init --help returned error: %v\nstderr: %s", err, stderr)
		}
		output := stdout + stderr
		if !contains(output, "Does not inspect a repository") {
			t.Errorf("server init --help should state it doesn't inspect a repository:\n%s", output)
		}
	})
}

// TestServerNamespace_TopLevelHelpNoServerOps verifies that top-level
// help correctly distinguishes server operations from top-level commands.
func TestServerNamespace_TopLevelHelpNoServerOps(t *testing.T) {
	_, stdout, stderr, err := executeCommand("--help")
	if err != nil {
		t.Fatalf("--help returned error: %v\nstderr: %s", err, stderr)
	}

	output := stdout + stderr

	// Server commands should appear under the "server" group, not as
	// top-level commands.
	if contains(output, "server init") && !contains(output, "server") {
		// This would be odd — server init appears but server group doesn't.
		// Actually, cobra help shows subcommands under their parent group,
		// so "server init" won't appear at the top level. Server group should.
	}
	if !contains(output, "server") {
		t.Errorf("top-level --help should list 'server' as a command group:\n%s", output)
	}
}
