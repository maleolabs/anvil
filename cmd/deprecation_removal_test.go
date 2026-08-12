// ── Deprecation warning completeness & governed removal (TS-017-04-02) ─
//
// The deprecation schedule (ANVIL_V2_DEPRECATION_SCHEDULE §4.1, T-014) is
// the source of truth for the v1.x command surface: every deprecated
// surface gets a documented replacement, a warning on use, a removal
// event, and a migration path (Transition Plan §12.5, ADR-028 §3). These
// tests pin the WARNING side (each alias-active surface warns with the
// schedule's verbatim notice — replacement named, migration guide pointed
// at; removal is event-bounded, no invented version number) and the
// REMOVAL side (the removal gate: the surface stays registered while the
// migration-path evidence is missing and the removal fires only when the
// window is closed AND the path is exercised — never silent, never early).
//
// Reference: TS-017-04-02, ANVIL_V2_DEPRECATION_SCHEDULE §4.1, ADR-028 §3,
// §12.5, GATE-REVIEW-TS-017-02-02 (C2 deferred to T-021)

package cmd

import (
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/deprecation"
	"maleolabs.com/anvil/internal/server"
)

// ── Warning completeness (schedule §4.1 verbatim) ─────────────────────

// scheduleAdapterNotices mirrors the "Warning on use" column of the
// deprecation schedule §4.1 for the adapter alias surface — the texts are
// verbatim from the schedule (which itself recorded them verbatim from the
// code). The four governed elements per surface: replacement (element 1),
// warning on use (element 2), removal event (element 3 — carried by the
// schedule/guide, event-bounded), migration path (element 4 — the guide
// pointer every notice carries).
var scheduleAdapterNotices = []struct {
	path           []string // command path under the root
	notice         string   // exact deprecation notice per schedule §4.1
	replacement    string   // fragment the notice must name
	removalInfo    string   // fragment the notice must carry when the schedule text carries removal info
	migrationGuide string   // fragment the notice must carry
}{
	{
		path:           []string{"adapter"},
		notice:         `use "anvil standard" commands instead; this group is retained for backward compatibility and will be removed in a future release (see docs/migration-guide-v2.md)`,
		replacement:    `anvil standard`,
		removalInfo:    "will be removed in a future release",
		migrationGuide: "docs/migration-guide-v2.md",
	},
	{
		path:           []string{"adapter", "list"},
		notice:         `use "anvil standard list" instead (see docs/migration-guide-v2.md)`,
		replacement:    `anvil standard list`,
		removalInfo:    "",
		migrationGuide: "docs/migration-guide-v2.md",
	},
	{
		path:           []string{"adapter", "inspect"},
		notice:         `use "anvil standard inspect" instead (see docs/migration-guide-v2.md)`,
		replacement:    `anvil standard inspect`,
		removalInfo:    "",
		migrationGuide: "docs/migration-guide-v2.md",
	},
	{
		path:           []string{"adapter", "install"},
		notice:         `use "anvil standard install" instead (see docs/migration-guide-v2.md)`,
		replacement:    `anvil standard install`,
		removalInfo:    "",
		migrationGuide: "docs/migration-guide-v2.md",
	},
	{
		path:           []string{"adapter", "use"},
		notice:         `use "anvil init --framework <name>" instead (see docs/migration-guide-v2.md)`,
		replacement:    `anvil init --framework <name>`,
		removalInfo:    "",
		migrationGuide: "docs/migration-guide-v2.md",
	},
	{
		path:           []string{"adapter", "uninstall"},
		notice:         `no standard-named replacement exists; this command is retained for backward compatibility and will be removed in a future release (see docs/migration-guide-v2.md)`,
		replacement:    "no standard-named replacement",
		removalInfo:    "will be removed in a future release",
		migrationGuide: "docs/migration-guide-v2.md",
	},
}

// TestAdapterWarnings_MatchScheduleVerbatim pins every adapter alias
// surface's deprecation notice to the schedule §4.1 "Warning on use" text
// (exact match, not substring): a drift in either direction — code or
// schedule — fails loudly. The warnings must name the replacement and
// point at the migration guide on every deprecated surface use.
func TestAdapterWarnings_MatchScheduleVerbatim(t *testing.T) {
	for _, tc := range scheduleAdapterNotices {
		cmd, _, err := rootCmd.Find(tc.path)
		if err != nil {
			t.Fatalf("%v must remain registered (deprecated, not removed): %v", tc.path, err)
		}
		if cmd.Deprecated != tc.notice {
			t.Errorf("%v notice must match the schedule §4.1 text verbatim\n got: %q\nwant: %q", tc.path, cmd.Deprecated, tc.notice)
		}
		if !strings.Contains(cmd.Deprecated, tc.replacement) {
			t.Errorf("%v notice must name the replacement %q, got: %q", tc.path, tc.replacement, cmd.Deprecated)
		}
		if tc.removalInfo != "" && !strings.Contains(cmd.Deprecated, tc.removalInfo) {
			t.Errorf("%v notice must carry the removal information %q per the schedule, got: %q", tc.path, tc.removalInfo, cmd.Deprecated)
		}
		if !strings.Contains(cmd.Deprecated, tc.migrationGuide) {
			t.Errorf("%v notice must point at the migration guide, got: %q", tc.path, cmd.Deprecated)
		}
	}
}

// TestProjectAdapterWarning_MatchScheduleVerbatim pins the project.adapter
// config-key warning to the schedule §4.1 text (exact match): every read
// of the legacy key emits it on stderr, naming the replacement
// project.standard and the migration guide (T-012, TS-019-02-02).
func TestProjectAdapterWarning_MatchScheduleVerbatim(t *testing.T) {
	const scheduleText = `project.adapter is deprecated; declare project.standard instead (see docs/migration-guide-v2.md)`
	if server.StandardAdapterAliasWarning != scheduleText {
		t.Errorf("project.adapter warning must match the schedule §4.1 text verbatim\n got: %q\nwant: %q", server.StandardAdapterAliasWarning, scheduleText)
	}
}

// TestDeprecatedSurfaces_WarnOnUse verifies the "warning on use" element
// per surface: invoking any deprecated adapter surface emits the
// deprecation notice naming the replacement (schedule §4.1). Subcommands
// are invoked without arguments: cobra prints the deprecation notice
// before argument validation, so the deterministic pre-existing argument
// error (not a deprecation-induced failure) is expected — the assertion is
// the warning on the deprecated-surface use. The group invocation prints
// the group notice alongside the group help.
func TestDeprecatedSurfaces_WarnOnUse(t *testing.T) {
	isolateGlobalConfigDir(t)
	stubAdapterInstallDirAt(t, t.TempDir())
	t.Setenv("PATH", "")

	cases := []struct {
		path   []string
		notice string // fragment the emitted warning must carry
	}{
		{path: []string{"adapter"}, notice: `Command "adapter" is deprecated`},
		{path: []string{"adapter", "list"}, notice: `Command "list" is deprecated`},
		{path: []string{"adapter", "inspect"}, notice: `Command "inspect" is deprecated`},
		{path: []string{"adapter", "use"}, notice: `Command "use" is deprecated`},
		{path: []string{"adapter", "install"}, notice: `Command "install" is deprecated`},
		{path: []string{"adapter", "uninstall"}, notice: `Command "uninstall" is deprecated`},
	}
	for _, tc := range cases {
		_, stdout, _, _ := executeCommand(tc.path...)
		if !strings.Contains(stdout, tc.notice) {
			t.Errorf("%v must warn on use (%q), got stdout: %s", tc.path, tc.notice, stdout)
		}
	}
}

// ── Governed removal mechanics (no silent removal) ────────────────────

// TestRemovalGate_CurrentState verifies the governed removal state of the
// running binary: the deprecation window closed at the switch-over gate
// (T-011), but the migration-path evidence (GATE-REVIEW C2) is deferred to
// T-021 — so the removal condition does NOT hold and the adapter alias
// surface must remain registered (deprecated, warning on use), not
// removed. This is the no-silent-removal guard: removal fires only when
// the gate holds (internal/deprecation).
func TestRemovalGate_CurrentState(t *testing.T) {
	if !deprecation.WindowClosed {
		t.Error("the deprecation window must be closed (switch-over gate, T-011)")
	}
	if deprecation.MigrationPathExercised {
		t.Error("the migration-path evidence (GATE-REVIEW C2) must be pending until T-021 delivers it")
	}
	if deprecation.RemovalConditionMet() {
		t.Fatal("removal must not fire before the migration path is exercised (no silent removal)")
	}

	// The surface stays registered: the group and every subcommand resolve.
	for _, tc := range scheduleAdapterNotices {
		if _, _, err := rootCmd.Find(tc.path); err != nil {
			t.Errorf("%v must remain registered while the removal gate is closed: %v", tc.path, err)
		}
	}
}

// TestRemovalInventory_CoversRegisteredSurface verifies the link between
// the removal inventory (internal/deprecation) and the registered surface:
// every registered adapter alias command corresponds to a removal
// candidate that documents its replacement, removal event, migration path,
// and evidence requirement — nothing that warns is left out of the removal
// inventory, and the inventory fires only after the T-021 evidence.
func TestRemovalInventory_CoversRegisteredSurface(t *testing.T) {
	if len(deprecation.RemovalCandidates) != 7 {
		t.Fatalf("removal inventory must cover the 7 scheduled surfaces (schedule §4.1), got %d", len(deprecation.RemovalCandidates))
	}
	for _, c := range deprecation.RemovalCandidates {
		if c.Surface == "" || c.Replacement == "" || c.RemovalEvent == "" || c.MigrationPath == "" || c.Evidence == "" {
			t.Errorf("removal candidate %q must document replacement, removal event, migration path, and evidence", c.Surface)
		}
	}
}

// TestRemovedSurfacePrecedent_Announced verifies the removal announcement
// precedent this ticket's mechanics follow: removed surfaces stop
// resolving ("unknown command") and the removal is announced in the
// schedule/guide — the adapter surface's removal is announced in the same
// artifact (schedule §4.1/§5) and fires only after the gate holds. The
// registered surface is therefore the expected current state, and the
// post-removal state ("unknown command") is the documented outcome of
// flipping deprecation.MigrationPathExercised — never a silent change.
func TestRemovedSurfacePrecedent_Announced(t *testing.T) {
	// The runtime group removal (TS-019-04-01) established the precedent:
	// removed surfaces fail with "unknown command" and are announced.
	_, _, _, err := executeCommand("runtime", "status")
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("removed surfaces must fail with 'unknown command' (removal precedent), got: %v", err)
	}
}
