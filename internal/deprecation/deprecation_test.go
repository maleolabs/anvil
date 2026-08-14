// ── Governed removal gate (TS-017-04-02) ──────────────────────────────
//
// Removal of the v1.x command surface is an explicit, announced event —
// never silent (Transition Plan §12.5, ADR-028 §3, §12.5): it fires only
// after the deprecation window closes AND the migration path is exercised.
// The window closed at the dual-run switch-over gate (T-011, GATE-REVIEW
// PASS 2026-08-07); the migration-path-exercised evidence (GATE-REVIEW
// criterion C2) is delivered by T-021 (Wave 4) and has NOT landed yet.
// These tests pin the gate state and the removal inventory: the removal
// must not fire before both conditions hold, and every scheduled surface
// keeps its four governed elements (replacement, warning, removal event,
// migration path) plus its evidence requirement.
package deprecation

import (
	"regexp"
	"strings"
	"testing"
)

// TestRemovalCondition_WindowClosed verifies the historical half of the
// gate: the dual-run window is closed (T-011, GATE-REVIEW PASS) — the
// removal is not time-bounded, it is event-bounded at this gate (ADR-028
// §3, §7; ANVIL_V2_DEPRECATION_SCHEDULE §2).
func TestRemovalCondition_WindowClosed(t *testing.T) {
	if !WindowClosed {
		t.Error("WindowClosed must be true: the switch-over gate passed on 2026-08-07 (T-011)")
	}
}

// TestRemovalCondition_EvidenceNotYetDelivered verifies the exercised-path
// half of the gate: the migration-path evidence (GATE-REVIEW C2) is
// deferred to T-021 (Wave 4). Removal must NOT fire while the evidence is
// missing — the aliases stay registered and keep warning (no silent
// removal, ADR-028 §12.5).
func TestRemovalCondition_EvidenceNotYetDelivered(t *testing.T) {
	if MigrationPathExercised {
		t.Error("MigrationPathExercised must be false until T-021 delivers the C2 evidence")
	}
	if RemovalConditionMet() {
		t.Error("RemovalConditionMet() must be false before the migration path is exercised: removal must not fire early")
	}
}

// TestRemovalCondition_FiresOnlyOnBoth verifies the single decision rule
// (removalConditionMet): removal fires only when the window closed AND the
// migration path was exercised. Any other combination keeps the surfaces
// registered with warnings.
func TestRemovalCondition_FiresOnlyOnBoth(t *testing.T) {
	cases := []struct {
		name               string
		windowClosed       bool
		migrationExercised bool
		want               bool
	}{
		{name: "window open, path unexercised", windowClosed: false, migrationExercised: false, want: false},
		{name: "window open, path exercised", windowClosed: false, migrationExercised: true, want: false},
		{name: "window closed, path unexercised (current state)", windowClosed: true, migrationExercised: false, want: false},
		{name: "window closed, path exercised (post T-021)", windowClosed: true, migrationExercised: true, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := removalConditionMet(tc.windowClosed, tc.migrationExercised); got != tc.want {
				t.Errorf("removalConditionMet(%v, %v) = %v, want %v", tc.windowClosed, tc.migrationExercised, got, tc.want)
			}
		})
	}
}

// TestRemovalCandidates_CoverSchedule verifies that the removal inventory
// covers every scheduled removal candidate of the deprecation schedule §4.1
// — the adapter alias surface (group + five subcommands) and the
// project.adapter config key — and nothing else. A removal PR must keep
// this inventory in step with the schedule: a surface dropped from the
// inventory would fail this test, so no scheduled removal can go missing
// silently.
func TestRemovalCandidates_CoverSchedule(t *testing.T) {
	want := []string{
		"adapter group (anvil adapter)",
		"adapter list",
		"adapter inspect",
		"adapter install",
		"adapter use",
		"adapter uninstall",
		"project.adapter config key",
	}
	if len(RemovalCandidates) != len(want) {
		t.Fatalf("RemovalCandidates has %d entries, want %d (schedule §4.1)", len(RemovalCandidates), len(want))
	}
	for i, w := range want {
		if got := RemovalCandidates[i].Surface; got != w {
			t.Errorf("RemovalCandidates[%d].Surface = %q, want %q", i, got, w)
		}
	}
}

// TestRemovalCandidates_FourElementsAndEvidence verifies that every
// candidate carries the four governed elements of Transition Plan §12.5 /
// ADR-028 §3 (documented replacement, warning on use, removal event,
// migration path) plus the gate evidence requirement, and that the removal
// events stay event-bounded — the schedule invents no version number for
// these removals (ADR-028 §3; ANVIL_V2_DEPRECATION_SCHEDULE §2).
func TestRemovalCandidates_FourElementsAndEvidence(t *testing.T) {
	versionNumber := regexp.MustCompile(`\bv\d+(\.\d+)*\b`)
	for _, c := range RemovalCandidates {
		if strings.TrimSpace(c.Surface) == "" {
			t.Error("candidate must have a surface name")
		}
		if strings.TrimSpace(c.Replacement) == "" {
			t.Errorf("%s: candidate must document the replacement (element 1 of §12.5)", c.Surface)
		}
		if strings.TrimSpace(c.RemovalEvent) == "" {
			t.Errorf("%s: candidate must document the removal event (element 3 of §12.5)", c.Surface)
		}
		if !strings.Contains(c.RemovalEvent, "event-bounded") {
			t.Errorf("%s: removal event must be event-bounded (no invented version number), got %q", c.Surface, c.RemovalEvent)
		}
		if versionNumber.MatchString(c.RemovalEvent) {
			t.Errorf("%s: removal event must not invent a version number, got %q", c.Surface, c.RemovalEvent)
		}
		if strings.TrimSpace(c.MigrationPath) == "" {
			t.Errorf("%s: candidate must document the migration path (element 4 of §12.5)", c.Surface)
		}
		if !strings.Contains(c.MigrationPath, "migration-guide-v2.md") {
			t.Errorf("%s: migration path must point at the migration guide, got %q", c.Surface, c.MigrationPath)
		}
		if !strings.Contains(c.Evidence, "T-021") {
			t.Errorf("%s: evidence requirement must name the C2 evidence delivery (T-021), got %q", c.Surface, c.Evidence)
		}
	}
}
