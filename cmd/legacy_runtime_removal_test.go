// Package cmd implements the Anvil CLI commands.
//
// ── Legacy "runtime" Group Removal (TS-019-04-01) ─────────────────────
//
// ADR-032 D12: the legacy "runtime" command group was deprecated
// throughout v1.x (ADR-021, decision 021) with the canonical CLI surface
// being the "server" command group (ADR-014, ADR-015). At the end of the
// announced deprecation window the group no longer resolves — no ghost
// surface remains — and users are directed to the documented replacement
// surface. The migration path was exercised during the window and was
// documented in the deprecation notices (help text) and
// docs/migration-guide-v1.5.md; nothing is removed silently.
//
// Removal inventory (cmd/ files removed with TS-019-04-01):
//
//	"runtime"            (cmd/runtime.go — parent group)
//	"runtime list"       (cmd/runtime_list.go)
//	"runtime readiness"  (cmd/runtime_readiness.go)
//	"runtime status"     (cmd/runtime_status.go)
//	"runtime verify-shared" (cmd/runtime_verify_shared.go)
//
// Replacement table (from the deprecated group's own migration notes,
// ADR-021 migration table):
//
//	anvil runtime readiness     -> anvil server readiness (signature
//	                               differs: legacy was a zero-argument
//	                               local filesystem check; server
//	                               readiness requires <project-id>
//	                               <release-id>, ST-P9-02)
//	anvil runtime status        -> anvil server status (signature differs:
//	                               legacy took no arguments; server status
//	                               accepts an optional <project-id>)
//	anvil runtime list          -> anvil server status (known gap: the
//	                               legacy multi-runtime registry has no
//	                               server-side equivalent; tracked as
//	                               follow-up work)
//	anvil runtime verify-shared -> anvil server doctor
//
// The internal/runtime package is untouched: it remains the shared
// implementation behind the server surface (state store, readiness
// checker, shared-resource manager) and the system inspect surface.
//
// Reference: TS-019-04-01, ADR-032 (D12), ADR-021, EPIC-017
package cmd

import (
	"regexp"
	"strings"
	"testing"
)

// removedRuntimeInvocations lists the legacy runtime invocation paths
// that must no longer resolve after the removal. Each entry documents
// the replacement surface users are directed to (the removal
// announcement).
var removedRuntimeInvocations = []struct {
	args        []string // invocation that must not resolve
	replacement string   // documented replacement referenced in guidance
}{
	{
		args:        []string{"runtime"},
		replacement: "anvil server",
	},
	{
		args:        []string{"runtime", "readiness"},
		replacement: "anvil server readiness",
	},
	{
		args:        []string{"runtime", "status"},
		replacement: "anvil server status",
	},
	{
		args:        []string{"runtime", "list"},
		replacement: "anvil server status",
	},
	{
		args:        []string{"runtime", "verify-shared"},
		replacement: "anvil server doctor",
	},
}

// TestLegacyRuntimeGroup_Removed verifies that the legacy "runtime"
// command group and its subcommands no longer resolve anywhere in the
// command tree (TS-019-04-01 DoD: the group no longer resolves after
// removal). Invoking them produces an "unknown command" error with a
// non-zero exit instead of printing help with exit 0 — the ghost-surface
// behavior ADR-032 D12 forbids.
func TestLegacyRuntimeGroup_Removed(t *testing.T) {
	for _, tc := range removedRuntimeInvocations {
		// The command path must not fully resolve in the cobra tree:
		// Find must leave the removed command name(s) in the remaining
		// args (the deepest registered ancestor is the root command,
		// never a runtime command).
		cmd, remaining, err := rootCmd.Find(tc.args)
		if err == nil && len(remaining) == 0 {
			t.Errorf("legacy runtime command %q must no longer resolve, found: %s",
				strings.Join(tc.args, " "), cmd.CommandPath())
		}

		// Invoking it must fail as an unknown command (never help + exit 0).
		_, stdout, stderr, err := executeCommand(tc.args...)
		if err == nil {
			t.Errorf("executeCommand(%q) should return an error (unknown command), got nil",
				strings.Join(tc.args, " "))
		}
		if strings.Contains(stdout, "Usage:") {
			t.Errorf("executeCommand(%q) should not print help to stdout, got:\n%s",
				strings.Join(tc.args, " "), stdout)
		}
		if !strings.Contains(stderr, "unknown command") {
			t.Errorf("executeCommand(%q) stderr should indicate an unknown command, got: %q",
				strings.Join(tc.args, " "), stderr)
		}
	}
}

// TestLegacyRuntimeGroup_RootHelpNoLongerListsIt verifies that the
// top-level help (bare invocation and --help) no longer documents the
// removed runtime group anywhere in the domain-grouped help output.
//
// AC: The command surface help reflects the removal; nothing removed
// silently stays discoverable (TS-019-04-01 DoD).
func TestLegacyRuntimeGroup_RootHelpNoLongerListsIt(t *testing.T) {
	// The Server Runtime domain group must list only the server command.
	for i := range rootDomainGroups {
		if rootDomainGroups[i].Name != "Server Runtime" {
			continue
		}
		for _, name := range rootDomainGroups[i].Commands {
			if name == "runtime" {
				t.Errorf("Server Runtime domain group must no longer list the runtime command, got: %v",
					rootDomainGroups[i].Commands)
			}
		}
	}

	for _, args := range [][]string{nil, {"--help"}} {
		_, stdout, _, err := executeCommand(args...)
		if err != nil {
			t.Fatalf("executeCommand(%q) failed: %v", args, err)
		}
		// Restrict scanning to the domain-help block (from the "Product
		// Domains:" header up to the trailing usage hint): the exit-code
		// summary below it legitimately mentions "runtime" in prose
		// ("the runtime environment is unavailable"), which is not the
		// command surface.
		block := stdout
		if start := strings.Index(stdout, "Product Domains:"); start >= 0 {
			block = stdout[start:]
		}
		if end := strings.Index(block, `Use "anvil [command] --help"`); end >= 0 {
			block = block[:end]
		}
		// Word-boundary match: "runtime" must not appear as a command
		// entry (or anywhere) in the domain-grouped help block.
		re := regexp.MustCompile(`\bruntime\b`)
		if re.MatchString(block) {
			t.Errorf("top-level help must no longer document the runtime command, got:\n%s", block)
		}
	}
}

// TestLegacyRuntimeGroup_ReplacementSurfaceFunctional verifies that the
// documented replacement surface — the "server" group and its
// readiness/status/doctor commands — remains registered and functional
// after the removal (TS-019-04-01 DoD: the replacement surface remains
// functional).
func TestLegacyRuntimeGroup_ReplacementSurfaceFunctional(t *testing.T) {
	// The canonical group and the named replacements must still resolve.
	for _, path := range [][]string{
		{"server"},
		{"server", "readiness"},
		{"server", "status"},
		{"server", "doctor"},
	} {
		sub, remaining, err := rootCmd.Find(path)
		if err != nil || len(remaining) > 0 || sub == nil {
			t.Errorf("replacement surface %q must remain registered, found: %v remaining: %v err: %v",
				strings.Join(path, " "), sub, remaining, err)
			continue
		}
		// The parent group is a namespace; the leaf replacements execute.
		if len(path) == 1 {
			if len(sub.Commands()) == 0 {
				t.Errorf("replacement group %q must keep its subcommands", strings.Join(path, " "))
			}
		} else if sub.RunE == nil {
			t.Errorf("replacement command %q must remain executable (RunE set)", strings.Join(path, " "))
		}
	}

	// Help on the replacement surface exits 0.
	for _, args := range [][]string{
		{"server", "--help"},
		{"server", "readiness", "--help"},
		{"server", "status", "--help"},
		{"server", "doctor", "--help"},
	} {
		if _, _, _, err := executeCommand(args...); err != nil {
			t.Errorf("executeCommand(%q) should exit 0 for help on the replacement surface, got: %v",
				strings.Join(args, " "), err)
		}
	}

	// The server group is listed under the Server Runtime domain.
	found := false
	for i := range rootDomainGroups {
		if rootDomainGroups[i].Name != "Server Runtime" {
			continue
		}
		for _, name := range rootDomainGroups[i].Commands {
			if name == "server" {
				found = true
			}
		}
	}
	if !found {
		t.Error("Server Runtime domain group must still list the server command")
	}
}
