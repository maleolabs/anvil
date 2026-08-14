package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// ── TS-019-01-01: adapter → standard rename and alias registration ───
//
// The command surface moves from the "adapter" vocabulary to the
// "standard" vocabulary (ADR-032): "anvil standard" is the canonical
// surface (EPIC-014); the legacy "adapter"-named commands are registered
// as aliases during the dual-run window (ADR-028 §3) — each emitting a
// deprecation warning that names the replacement on use. Following the
// legacy "runtime" group precedent (TD-007), deprecation only adds a
// warning and a migration note in help text: command behavior and exit
// codes are unchanged, so v1.x users and CI workflows keep working
// through the window (EPIC-017).
//
// NOTE on test buffers: cobra prints the deprecation warning through
// Command.Printf → OutOrStderr(), which prefers the writer set via
// SetOut. The executeCommand harness wires stdout via SetOut, so in these
// tests the warning appears at the start of the stdout buffer. When the
// real binary runs (no SetOut/SetErr), OutOrStderr falls back to
// os.Stderr and the warning goes to stderr as expected — machine-readable
// --json stdout stays unpolluted.

// adapterSubcommands lists the legacy adapter subcommands that must carry
// a cobra Deprecated notice naming the replacement (ADR-032,
// TS-019-01-01 DoD: every alias emits a deprecation warning that names
// the replacement).
var adapterSubcommands = []string{"list", "inspect", "use", "install", "uninstall"}

// adapterReplacementNames maps each legacy adapter subcommand to the
// replacement command its deprecation notice must name.
var adapterReplacementNames = map[string]string{
	"list":      `use "anvil standard list" instead`,
	"inspect":   `use "anvil standard inspect" instead`,
	"install":   `use "anvil standard install" instead`,
	"use":       `use "anvil init --framework <name>" instead`,
	"uninstall": "no standard-named replacement",
}

// TestAdapterGroup_Deprecated verifies that the legacy "adapter" command
// group and every subcommand carry a cobra deprecation notice
// (TS-019-01-01): the group remains registered as an alias — deprecated,
// not removed — and help text documents the migration path to the
// canonical "standard" surface.
func TestAdapterGroup_Deprecated(t *testing.T) {
	group, _, err := rootCmd.Find([]string{"adapter"})
	if err != nil {
		t.Fatalf("adapter group must remain registered (deprecated, not removed): %v", err)
	}

	if group.Deprecated == "" {
		t.Error("adapter group must have a cobra Deprecated notice")
	}
	if !strings.Contains(group.Long, "DEPRECATED") {
		t.Error("adapter group help (Long) must announce the deprecation")
	}
	for _, want := range []string{
		"anvil standard list",
		"anvil standard inspect",
		"anvil standard install",
		"anvil init --framework <name>",
		"no standard-named replacement exists",
	} {
		if !strings.Contains(group.Long, want) {
			t.Errorf("adapter group help (Long) must document the migration path %q", want)
		}
	}

	for _, name := range adapterSubcommands {
		sub, _, err := rootCmd.Find([]string{"adapter", name})
		if err != nil {
			t.Fatalf("adapter subcommand %q must remain registered (deprecated, not removed): %v", name, err)
		}
		if sub.Deprecated == "" {
			t.Errorf("adapter subcommand %q must have a cobra Deprecated notice", name)
		}
	}
}

// TestAdapterAliases_WarningNamesReplacement verifies that each legacy
// subcommand's deprecation notice names its replacement command
// (TS-019-01-01 DoD: each alias emits a deprecation warning naming the
// replacement).
func TestAdapterAliases_WarningNamesReplacement(t *testing.T) {
	for name, want := range adapterReplacementNames {
		sub, _, err := rootCmd.Find([]string{"adapter", name})
		if err != nil {
			t.Fatalf("adapter subcommand %q must remain registered (deprecated, not removed): %v", name, err)
		}
		if !strings.Contains(sub.Deprecated, want) {
			t.Errorf("adapter subcommand %q notice must name the replacement (%q), got: %q", name, want, sub.Deprecated)
		}
	}
}

// TestAdapterDeprecationNotices_StayClean verifies the product-review
// cleanup (TS-019-01-02 note a): user-facing deprecation notices follow
// the legacy "runtime" group precedent — replacement name and migration
// guide pointer only. Internal governance references (ADR-028, ADR-032,
// EPIC-015) and dual-run-window jargon must not surface in the notices
// or in the deprecated group's help text.
func TestAdapterDeprecationNotices_StayClean(t *testing.T) {
	group, _, err := rootCmd.Find([]string{"adapter"})
	if err != nil {
		t.Fatalf("adapter group must remain registered (deprecated, not removed): %v", err)
	}

	all := []*cobra.Command{group}
	for _, name := range adapterSubcommands {
		sub, _, err := rootCmd.Find([]string{"adapter", name})
		if err != nil {
			t.Fatalf("adapter subcommand %q must remain registered (deprecated, not removed): %v", name, err)
		}
		all = append(all, sub)
	}

	for _, c := range all {
		notice := c.Deprecated
		if notice == "" {
			continue
		}
		for _, jargon := range []string{"ADR-", "EPIC-", "dual-run window", "when the window closes"} {
			if strings.Contains(notice, jargon) {
				t.Errorf("%q deprecation notice must not carry governance jargon %q, got: %q", c.Name(), jargon, notice)
			}
		}
		if !strings.Contains(notice, "docs/migration-guide-v2.md") {
			t.Errorf("%q deprecation notice must keep the migration guide pointer, got: %q", c.Name(), notice)
		}
	}

	// The group help's DEPRECATED paragraph is user-facing too: it must
	// announce the deprecation and the canonical surface without
	// governance references or window jargon.
	for _, jargon := range []string{"ADR-", "EPIC-", "dual-run", "window closes"} {
		if strings.Contains(group.Long, jargon) {
			t.Errorf("adapter group help must not carry governance jargon %q, got: %s", jargon, group.Long)
		}
	}
}

// TestAdapterGroup_InvocationWarningAndExitCodes verifies that invoking a
// legacy adapter command still works identically (exit code, output, and
// error behavior unchanged — ADR-028 §3) while printing the deprecation
// warning (TS-019-01-01, EPIC-017 mechanics).
func TestAdapterGroup_InvocationWarningAndExitCodes(t *testing.T) {
	// Bare group invocation still prints group help and the warning.
	_, stdoutHelp, stderrBare, err := executeCommand("adapter")
	if err != nil {
		t.Fatalf("bare 'adapter' must still print group help (exit 0): %v", err)
	}
	if stderrBare != "" {
		t.Errorf("expected empty stderr for bare 'adapter', got: %q", stderrBare)
	}
	if !strings.Contains(stdoutHelp, "is deprecated") {
		t.Errorf("bare 'adapter' must print the deprecation warning, got stdout: %s", stdoutHelp)
	}

	// Help via --help also announces the deprecation and the migration path.
	_, stdoutHelpFlag, stderrHelpFlag, err := executeCommand("adapter", "--help")
	if err != nil {
		t.Fatalf("'adapter --help' must succeed: %v", err)
	}
	if stderrHelpFlag != "" {
		t.Errorf("expected empty stderr for 'adapter --help', got: %q", stderrHelpFlag)
	}
	if !strings.Contains(stdoutHelpFlag, "is deprecated") {
		t.Errorf("'adapter --help' must print the deprecation warning, got stdout: %s", stdoutHelpFlag)
	}
	if !strings.Contains(stdoutHelpFlag, "DEPRECATED") {
		t.Errorf("'adapter --help' must announce the deprecation in help text, got: %s", stdoutHelpFlag)
	}

	// A subcommand invocation keeps working (empty state, exit 0) while
	// printing its own warning (deterministic: no adapters on the system —
	// the registry-driven installed view reads the isolated config).
	isolateGlobalConfigDir(t)
	stubAdapterInstallDirAt(t, t.TempDir())
	t.Setenv("PATH", "")
	_, stdoutList, stderrList, err := executeCommand("adapter", "list")
	if err != nil {
		t.Fatalf("'adapter list' must still succeed (exit 0): %v (stderr: %s)", err, stderrList)
	}
	if !strings.Contains(stdoutList, `Command "list" is deprecated`) {
		t.Errorf("'adapter list' must print its deprecation warning, got stdout: %s", stdoutList)
	}
	if !strings.Contains(stdoutList, "No adapters installed.") {
		t.Errorf("'adapter list' output must be unchanged, got stdout: %s", stdoutList)
	}
}

// jsonEnvelopeFromStdout returns the JSON envelope portion of a captured
// stdout buffer: the cobra deprecation warning (harness-routed to stdout
// — see the file header note) precedes the envelope, so the envelope is
// the substring starting at the first '{'. In the real binary the warning
// goes to stderr and stdout carries only the envelope.
func jsonEnvelopeFromStdout(t *testing.T, stdout string) []byte {
	t.Helper()
	idx := strings.Index(stdout, "{")
	if idx < 0 {
		t.Fatalf("expected a JSON envelope in stdout, got: %s", stdout)
	}
	return []byte(stdout[idx:])
}

// TestAdapterAlias_JSONEnvelopeStaysMachineReadable verifies that the
// --json alias path keeps producing the standard TS-P8-05 envelope: the
// deprecation warning is a separate stream (stderr in the real binary;
// the harness routes it to stdout before the envelope), so the envelope
// itself stays valid JSON with the expected shape (exit 0).
func TestAdapterAlias_JSONEnvelopeStaysMachineReadable(t *testing.T) {
	isolateGlobalConfigDir(t)
	stubAdapterInstallDirAt(t, t.TempDir())
	t.Setenv("PATH", "")

	_, stdout, stderr, err := executeCommand("adapter", "list", "--json")
	if err != nil {
		t.Fatalf("'adapter list --json' must still succeed (exit 0): %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, `Command "list" is deprecated`) {
		t.Errorf("'adapter list --json' must print the deprecation warning, got stdout: %s", stdout)
	}

	var envelope struct {
		Version string          `json:"version"`
		Status  string          `json:"status"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(jsonEnvelopeFromStdout(t, stdout), &envelope); err != nil {
		t.Fatalf("stdout envelope must be valid JSON: %v", err)
	}
	if envelope.Version != "1" || envelope.Status != "success" {
		t.Errorf("envelope must be the standard TS-P8-05 shape, got: %s", stdout)
	}
}

// TestAdapterAlias_BehaviorUnchanged verifies one representative mutating
// alias path ("anvil adapter uninstall" on an empty install directory):
// the graceful not-installed outcome (exit 0) is unchanged by the
// deprecation (ADR-028 §3, TS-019-01-01). The config home is isolated
// so the registry-driven uninstall (record removal, TS-017-02-02) never
// touches the developer machine's real installed-standard store.
func TestAdapterAlias_BehaviorUnchanged(t *testing.T) {
	stubAdapterInstallDirAt(t, t.TempDir())
	isolateGlobalConfigDir(t)
	t.Setenv("PATH", "")
	t.Setenv("PATH", "")

	_, stdout, stderr, err := executeCommand("adapter", "uninstall", "laravel")
	if err != nil {
		t.Fatalf("'adapter uninstall' must still succeed (exit 0): %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, `Command "uninstall" is deprecated`) {
		t.Errorf("'adapter uninstall' must print its deprecation warning, got stdout: %s", stdout)
	}
	if !strings.Contains(stdout, "is not installed") {
		t.Errorf("'adapter uninstall' output must be unchanged, got stdout: %s", stdout)
	}
}

// TestStandardSurface_CanonicalNoWarning verifies the canonical side of
// the rename: the standard-named commands are NOT deprecated — invoking
// "anvil standard" surfaces must not print any deprecation warning
// (TS-019-01-01: standard-named commands are the canonical surface).
func TestStandardSurface_CanonicalNoWarning(t *testing.T) {
	for _, args := range [][]string{
		{"standard", "list"},
		{"standard", "inspect", "anvil-standard-laravel"},
		{"standard", "--help"},
	} {
		_, stdout, stderr, err := executeCommand(args...)
		if err != nil {
			// list/inspect need an index; with none configured they fail
			// with the documented index error — the assertion is only that
			// no DEPRECATION warning is emitted on either stream.
			if strings.Contains(stdout, "is deprecated") || strings.Contains(stderr, "is deprecated") {
				t.Errorf("%v must not print a deprecation warning, stdout: %s, stderr: %s", args, stdout, stderr)
			}
			continue
		}
		if strings.Contains(stdout, "is deprecated") || strings.Contains(stderr, "is deprecated") {
			t.Errorf("%v must not print a deprecation warning, stdout: %s, stderr: %s", args, stdout, stderr)
		}
	}

	// The canonical group itself carries no Deprecated notice.
	group, _, err := rootCmd.Find([]string{"standard"})
	if err != nil {
		t.Fatalf("standard group must be registered: %v", err)
	}
	if group.Deprecated != "" {
		t.Errorf("standard group must not be deprecated, got notice: %q", group.Deprecated)
	}
}

// ── TD-007: adapter domain regrouping (decision 021) ──────────────────
//
// "anvil adapter use" is a repository-aware action (writes
// project.framework into anvil.yaml), so the adapter group belongs under
// the Development domain, not System (TS-008-008, ADR-015 CLI contexts).
//
// These tests were originally colocated with the legacy "runtime" group
// deprecation tests (cmd/runtime_deprecation_test.go); they moved here
// when the runtime group was removed (TS-019-04-01).

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
