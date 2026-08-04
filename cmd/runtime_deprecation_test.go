package cmd

import (
	"strings"
	"testing"
)

// ── TD-007: Legacy "runtime" group deprecation (decision 021) ─────────
//
// The "server" command group is the canonical Server Runtime surface
// (ADR-014 migration table, ADR-015 CLI contract). The legacy "runtime"
// group is DEPRECATED but retained — commands must keep working
// identically (ST-007-006), so deprecation only adds a warning and a
// migration note in help text. Behavior and exit codes are unchanged.
//
// NOTE on test buffers: cobra prints the deprecation warning through
// Command.Printf → OutOrStderr(), which prefers the writer set via
// SetOut. The executeCommand harness wires stdout via SetOut, so in these
// tests the warning appears at the start of the stdout buffer. When the
// real binary runs (no SetOut/SetErr), OutOrStderr falls back to
// os.Stderr and the warning goes to stderr as expected.

// runtimeSubcommands lists the legacy runtime subcommands that must carry
// a cobra Deprecated notice (decision 021).
var runtimeSubcommands = []string{"provision", "readiness", "status", "list", "verify-shared"}

// TestRuntimeGroup_Deprecated verifies that the legacy "runtime" command
// group and every subcommand carry a cobra deprecation notice (TD-007):
// invoking them must print a deprecation warning, and help text must
// document the migration path to the canonical "server" surface.
func TestRuntimeGroup_Deprecated(t *testing.T) {
	runtimeCmdRef, _, err := rootCmd.Find([]string{"runtime"})
	if err != nil {
		t.Fatalf("runtime group must remain registered (deprecated, not removed): %v", err)
	}

	if runtimeCmdRef.Deprecated == "" {
		t.Error("runtime group must have a cobra Deprecated notice")
	}
	if !strings.Contains(runtimeCmdRef.Long, "DEPRECATED") {
		t.Error("runtime group help (Long) must announce the deprecation")
	}
	for _, want := range []string{"anvil server init", "anvil server readiness", "anvil server status", "anvil server doctor"} {
		if !strings.Contains(runtimeCmdRef.Long, want) {
			t.Errorf("runtime group help (Long) must document the migration path %q", want)
		}
	}

	for _, name := range runtimeSubcommands {
		sub, _, err := rootCmd.Find([]string{"runtime", name})
		if err != nil {
			t.Fatalf("runtime subcommand %q must remain registered (deprecated, not removed): %v", name, err)
		}
		if sub.Deprecated == "" {
			t.Errorf("runtime subcommand %q must have a cobra Deprecated notice", name)
		}
	}
}

// TestRuntimeGroup_InvocationWarningAndExitCodes verifies that invoking a
// legacy runtime command still works identically (exit code, output, and
// error behavior unchanged) while printing the deprecation warning
// (TD-007, ST-007-006).
func TestRuntimeGroup_InvocationWarningAndExitCodes(t *testing.T) {
	dir := t.TempDir()

	// Success path: provision still exits 0, output is unchanged, and the
	// deprecation warning is printed (stdout here — see note above).
	_, stdout, stderr, err := executeCommand("runtime", "provision",
		"--name", "deprecation-test",
		"--environment", "production",
		"--install-path", dir,
	)
	if err != nil {
		t.Fatalf("provision must still succeed (exit 0) despite deprecation: %v", err)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}
	if !strings.Contains(stdout, "is deprecated") {
		t.Errorf("provision must print the deprecation warning, got stdout: %s", stdout)
	}
	if !contains(stdout, "Runtime provisioned successfully") {
		t.Errorf("provision output must be unchanged, got: %s", stdout)
	}

	// Error path: missing --name still exits non-zero with the same error.
	_, _, _, err = executeCommand("runtime", "provision", "--name", "")
	if err == nil {
		t.Fatal("provision without --name must still fail (exit non-zero)")
	}
	if !contains(err.Error(), "runtime name is required") {
		t.Errorf("provision error message must be unchanged, got: %v", err)
	}

	// Bare group invocation still prints group help and the warning.
	_, stdoutHelp, stderrBare, err := executeCommand("runtime")
	if err != nil {
		t.Fatalf("bare 'runtime' must still print group help (exit 0): %v", err)
	}
	if stderrBare != "" {
		t.Errorf("expected empty stderr for bare 'runtime', got: %q", stderrBare)
	}
	if !strings.Contains(stdoutHelp, "is deprecated") {
		t.Errorf("bare 'runtime' must print the deprecation warning, got stdout: %s", stdoutHelp)
	}
	if !contains(stdoutHelp, "anvil server init") {
		t.Errorf("runtime group help must document the migration path, got: %s", stdoutHelp)
	}

	// Help via --help also announces the deprecation and the migration path.
	_, stdoutHelpFlag, stderrHelpFlag, err := executeCommand("runtime", "--help")
	if err != nil {
		t.Fatalf("'runtime --help' must succeed: %v", err)
	}
	if stderrHelpFlag != "" {
		t.Errorf("expected empty stderr for 'runtime --help', got: %q", stderrHelpFlag)
	}
	if !strings.Contains(stdoutHelpFlag, "is deprecated") {
		t.Errorf("'runtime --help' must print the deprecation warning, got stdout: %s", stdoutHelpFlag)
	}
	if !strings.Contains(stdoutHelpFlag, "DEPRECATED") {
		t.Errorf("'runtime --help' must announce the deprecation in help text, got: %s", stdoutHelpFlag)
	}
}

// ── TD-007: adapter domain regrouping (decision 021) ──────────────────
//
// "anvil adapter use" is a repository-aware action (writes
// project.framework into anvil.yaml), so the adapter group belongs under
// the Development domain, not System (TS-008-008, ADR-015 CLI contexts).

// TestAdapterDomainGroup_Development verifies that the adapter group is
// listed under Development and no longer under System in the domain help
// grouping (TD-007).
func TestAdapterDomainGroup_Development(t *testing.T) {
	var development, system *domainGroup
	for i := range rootDomainGroups {
		switch rootDomainGroups[i].Name {
		case "Development":
			development = &rootDomainGroups[i]
		case "System":
			system = &rootDomainGroups[i]
		}
	}
	if development == nil {
		t.Fatal("Development domain group must exist")
	}
	if system == nil {
		t.Fatal("System domain group must exist")
	}

	if !containsString(development.Commands, "adapter") {
		t.Errorf("adapter must be grouped under Development, got: %v", development.Commands)
	}
	if containsString(system.Commands, "adapter") {
		t.Errorf("adapter must no longer be grouped under System, got: %v", system.Commands)
	}
}

// TestAdapterDomainGroup_HelpOutput verifies that the rendered top-level
// help places the adapter command inside the Development section and the
// System section no longer lists it. The help block is scanned line by
// line using the section headers as boundaries (two-space indented lines
// are domain headers, four-space indented lines are command entries), so
// the test is not brittle to section reordering.
func TestAdapterDomainGroup_HelpOutput(t *testing.T) {
	_, stdout, _, err := executeCommand()
	if err != nil {
		t.Fatalf("bare 'anvil' help must succeed: %v", err)
	}

	// Restrict scanning to the domain-help block: from the "Product
	// Domains:" header up to the trailing usage hint.
	block := stdout
	if start := strings.Index(stdout, "Product Domains:"); start >= 0 {
		block = stdout[start:]
	}
	if end := strings.Index(block, `Use "anvil [command] --help"`); end >= 0 {
		block = block[:end]
	}

	var devEntries, sysEntries []string
	section := ""
	for _, line := range strings.Split(block, "\n") {
		switch {
		case strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    "):
			// Two-space indented: a domain section header.
			section = strings.TrimSpace(line)
		case strings.HasPrefix(line, "    "):
			// Four-space indented: a command entry of the current section.
			switch section {
			case "Development":
				devEntries = append(devEntries, strings.TrimSpace(line))
			case "System":
				sysEntries = append(sysEntries, strings.TrimSpace(line))
			}
		}
	}

	if !containsCommandEntry(devEntries, "adapter") {
		t.Errorf("Development section must list the adapter command, got entries: %v", devEntries)
	}
	if containsCommandEntry(sysEntries, "adapter") {
		t.Errorf("System section must not list the adapter command, got entries: %v", sysEntries)
	}
}

// containsCommandEntry reports whether any rendered command entry line
// starts with the given command name.
func containsCommandEntry(entries []string, name string) bool {
	for _, e := range entries {
		if strings.HasPrefix(e, name) {
			return true
		}
	}
	return false
}

// containsString reports whether s contains the given element.
func containsString(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}
