// Package cmd implements the Anvil CLI commands.
//
// ── Undocumented Subcommand Removal (TS-019-04-02) ────────────────────
//
// D12 (ADR-032): undocumented subcommands are ghost surfaces — registered
// in the CLI but absent from the command surface documentation (README,
// wiki, docs/operations, docs/architecture, the command-implementation
// checklist, and the migration guides). At the end of the deprecation
// window they are removed per the announced schedule: the subcommands no
// longer resolve and users are directed to the documented surface.
//
// Removal inventory (registered in cmd/, absent from the command-surface
// documentation — README.md, wiki/, docs/operations/, docs/architecture/,
// docs/migration-guide-*.md and docs/command-implementation-checklist.md;
// cross-referenced 2026-08-07):
//
//   - "project remove"            (cmd/project_remove.go)
//   - "project version"           (cmd/project_version.go — group plus
//     set / bump:patch / bump:minor / bump:major / generate)
//   - "artifact status"           (cmd/artifact_status.go)
//   - "artifact verify-immutability" (cmd/artifact_immutability.go)
//
// Mentions in non-command-surface documents (ADRs, PRDs, the epic and
// planning corpus, and the exit-code audit table in
// docs/operations/exit-codes-audit.md — an internal TS-019-03-01 audit
// deliverable) do not document these commands' usage; the audit is a
// snapshot of origin/develop and is intentionally not modified.
//
// Reference: TS-019-04-02, ADR-032 (D12), EPIC-019
package cmd

import (
	"regexp"
	"strings"
	"testing"
)

// removedUndocumentedSubcommands lists the invocation paths that must no
// longer resolve after the removal. Each entry documents the registered
// command it replaces and the documented surface users are directed to
// (the removal announcement).
var removedUndocumentedSubcommands = []struct {
	args        []string // invocation that must not resolve
	replacement string   // documented replacement referenced in guidance
}{
	{
		args:        []string{"project", "remove"},
		replacement: "anvil project status",
	},
	{
		args:        []string{"project", "version"},
		replacement: "anvil status",
	},
	{
		args:        []string{"project", "version", "set", "1.2.3"},
		replacement: "anvil status",
	},
	{
		args:        []string{"project", "version", "bump:patch"},
		replacement: "anvil status",
	},
	{
		args:        []string{"project", "version", "bump:minor"},
		replacement: "anvil status",
	},
	{
		args:        []string{"project", "version", "bump:major"},
		replacement: "anvil status",
	},
	{
		args:        []string{"project", "version", "generate"},
		replacement: "anvil status",
	},
	{
		args:        []string{"artifact", "status", "abc123def456"},
		replacement: "anvil server release status",
	},
	{
		args:        []string{"artifact", "verify-immutability", "test.anvil"},
		replacement: "anvil artifact verify",
	},
}

// TestUndocumentedSubcommands_Removed verifies that the undocumented
// subcommands removed per the announced schedule (TS-019-04-02) no
// longer resolve anywhere in the command tree, and that invoking them
// produces an "unknown command" error with a non-zero exit instead of
// printing help with exit 0 (the ghost-surface behavior D12 forbids).
//
// AC: No undocumented subcommand resolves after removal (TS-019-04-02
// DoD).
func TestUndocumentedSubcommands_Removed(t *testing.T) {
	for _, tc := range removedUndocumentedSubcommands {
		// The command path must not fully resolve in the cobra tree:
		// Find must leave the removed subcommand name(s) in the
		// remaining args (the deepest registered ancestor is the parent
		// group, never the removed subcommand).
		cmd, remaining, err := rootCmd.Find(tc.args)
		if err == nil && len(remaining) == 0 {
			t.Errorf("undocumented subcommand %q must no longer resolve, found: %s",
				strings.Join(tc.args, " "), cmd.CommandPath())
		}

		// Invoking it must fail as an unknown command (never help + exit 0).
		_, _, stderr, err := executeCommand(tc.args...)
		if err == nil {
			t.Errorf("executeCommand(%q) should return an error (unknown command), got nil",
				strings.Join(tc.args, " "))
		}
		if !strings.Contains(stderr, "unknown command") {
			t.Errorf("executeCommand(%q) stderr should indicate an unknown command, got: %q",
				strings.Join(tc.args, " "), stderr)
		}
	}
}

// TestUndocumentedSubcommands_ParentHelpNoLongerListsThem verifies that
// the parent groups' help (anvil project --help, anvil artifact --help)
// no longer documents the removed subcommands.
//
// AC: The command surface help reflects the removal; nothing removed
// silently stays discoverable (TS-019-04-02 DoD).
func TestUndocumentedSubcommands_ParentHelpNoLongerListsThem(t *testing.T) {
	for _, tc := range []struct {
		parent string
		ghost  string
	}{
		{"project", "remove"},
		{"project", "version"},
		{"artifact", "status"},
		{"artifact", "verify-immutability"},
	} {
		_, stdout, _, err := executeCommand(tc.parent, "--help")
		if err != nil {
			t.Fatalf("executeCommand(%q, '--help') failed: %v", tc.parent, err)
		}
		// Word-boundary match: the removed subcommand name must not appear
		// anywhere in the help output (as a command entry or in prose).
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(tc.ghost) + `\b`)
		if re.MatchString(stdout) {
			t.Errorf("%s --help must no longer document the removed subcommand %q, got:\n%s",
				tc.parent, tc.ghost, stdout)
		}
	}
}
