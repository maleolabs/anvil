// Package deprecation implements the governed deprecation-window state and
// removal gate for the v1.x command surface (ADR-028 §3, §12.5; ADR-032 §7;
// ANVIL_V2_DEPRECATION_SCHEDULE §4.1).
//
// Removal is an explicit, announced event — never silent (Transition Plan
// §12.5, ADR-028 §3): it fires only when BOTH the deprecation window has
// closed (the dual-run switch-over gate, T-011, GATE-REVIEW PASS) AND the
// migration path has been exercised (GATE-REVIEW criterion C2, evidence
// delivered by T-021). Until both hold, every scheduled surface stays
// alias-active and warns on every use.
//
// The removal condition is a compile-time decision, not a runtime toggle:
// executing the removal is a deliberate code change (flipping
// MigrationPathExercised and, per candidate, deleting the surface), announced
// through the deprecation schedule and the migration guide — never silent.
//
// Reference: ADR-028 §3, §12.5, ADR-032 §7, ANVIL_V2_TRANSITION_PLAN §12.4,
// §12.5, GATE-REVIEW-TS-017-02-02, ANVIL_V2_DEPRECATION_SCHEDULE §4.1
package deprecation

// WindowClosed reports whether the dual-run switch-over gate has closed
// (TS-017-02-02 / T-011, GATE-REVIEW PASS 2026-08-07): the CLI deprecation
// window is event-bounded and closes at this gate (ADR-028 §3, §7;
// Transition Plan §12.4). This is a historical fact — it never flips back.
const WindowClosed = true

// MigrationPathExercised reports whether the migration-path-exercised
// evidence exists (GATE-REVIEW criterion C2; Transition Plan §10 Phase 6
// gate): the documented per-user-type paths (fresh installs, existing
// projects, CI workflows, adapter authors) have been executed and validated.
// The evidence is delivered by T-021 (TS-017-05-02, Wave 4) against the
// post-gate registry-only state.
//
// REMOVAL EVENT: when the evidence lands, this constant flips to true and
// the removal event is armed — surfaces scheduled at window end
// (ANVIL_V2_DEPRECATION_SCHEDULE §4.1) stop being registered/resolved.
// Flipping is an explicit code change announced with the removal (schedule,
// migration guide, release notes) — never a runtime toggle, never silent.
// Until then the aliases keep working and keep warning.
const MigrationPathExercised = false

// RemovalConditionMet reports whether the governed removal condition holds:
// the deprecation window has closed AND the migration path has been
// exercised. Removal fires only when both hold — never early, never silent
// (ADR-028 §3, §12.5). The command layer consults this gate before
// registering the deprecated surfaces.
func RemovalConditionMet() bool {
	return removalConditionMet(WindowClosed, MigrationPathExercised)
}

// removalConditionMet is the single removal decision rule: removal requires
// the window closed AND the migration path exercised. It is a pure function
// of its two inputs so the gate is testable in both branches without global
// state.
func removalConditionMet(windowClosed, migrationExercised bool) bool {
	return windowClosed && migrationExercised
}

// Candidate is one scheduled removal candidate of the v1.x command surface
// (ANVIL_V2_DEPRECATION_SCHEDULE §4.1): a deprecated surface that carries
// the four governed elements of Transition Plan §12.5 / ADR-028 §3 —
// documented replacement, warning on use, removal event, migration path —
// and whose removal fires only after the gate evidence (GATE-REVIEW C2)
// exists.
type Candidate struct {
	// Surface is the user-facing deprecated surface name.
	Surface string

	// Replacement is the canonical v2 surface that replaces it.
	Replacement string

	// RemovalEvent is the event-bounded removal trigger. The CLI
	// deprecation window is event-bounded, not version-bounded (ADR-028
	// §3): no version number is invented for these removals.
	RemovalEvent string

	// MigrationPath points at the documented migration path for the
	// surface (migration-guide-v2.md section).
	MigrationPath string

	// Evidence is the gate evidence that must exist before the removal
	// fires: the migration paths exercised against the post-gate state
	// (GATE-REVIEW C2, T-021).
	Evidence string
}

// RemovalCandidates is the authoritative removal inventory: every surface
// whose removal is scheduled at window end (ANVIL_V2_DEPRECATION_SCHEDULE
// §4.1). It is the machine-checkable announcement — a removal PR must keep
// this inventory complete and update every candidate it executes, so no
// scheduled surface can be dropped silently. Warning texts are pinned
// verbatim by the command-layer tests (cmd/deprecation_removal_test.go);
// the schedule document owns the human-readable form.
//
// IMMUTABILITY CONTRACT: this slice is a compile-time announcement, not
// runtime state — no package may append to, reorder, or mutate its
// entries after init. Changes to the inventory are deliberate governed
// edits (a removal PR), reviewed as such; tests pin its contents
// (TestRemovalCandidates_CoverSchedule).
//
// The removal of a candidate fires only when RemovalConditionMet() holds:
// the window closed at the switch-over gate (T-011) and the migration-path
// evidence (C2, T-021) has landed. Until then every candidate keeps working
// and keeps warning.
var RemovalCandidates = []Candidate{
	{
		Surface:       "adapter group (anvil adapter)",
		Replacement:   "standard group (anvil standard)",
		RemovalEvent:  "end of the deprecation window (switch-over gate — event-bounded, no version number)",
		MigrationPath: "migration-guide-v2.md §5.3, §6; adapter → standard mapping",
		Evidence:      "migration path exercised (GATE-REVIEW C2, T-021)",
	},
	{
		Surface:       "adapter list",
		Replacement:   "standard list",
		RemovalEvent:  "end of the deprecation window (switch-over gate — event-bounded, no version number)",
		MigrationPath: "migration-guide-v2.md §5.3",
		Evidence:      "migration path exercised (GATE-REVIEW C2, T-021)",
	},
	{
		Surface:       "adapter inspect",
		Replacement:   "standard inspect",
		RemovalEvent:  "end of the deprecation window (switch-over gate — event-bounded, no version number)",
		MigrationPath: "migration-guide-v2.md §5.3",
		Evidence:      "migration path exercised (GATE-REVIEW C2, T-021)",
	},
	{
		Surface:       "adapter install",
		Replacement:   "standard install",
		RemovalEvent:  "end of the deprecation window (switch-over gate — event-bounded, no version number)",
		MigrationPath: "migration-guide-v2.md §5.3, §5.4 (fresh installs)",
		Evidence:      "migration path exercised (GATE-REVIEW C2, T-021)",
	},
	{
		Surface:       "adapter use",
		Replacement:   "init --framework <name>",
		RemovalEvent:  "end of the deprecation window (switch-over gate — event-bounded, no version number)",
		MigrationPath: "migration-guide-v2.md §5.4.2 (existing projects)",
		Evidence:      "migration path exercised (GATE-REVIEW C2, T-021)",
	},
	{
		Surface:       "adapter uninstall",
		Replacement:   "no standard-named replacement (documented exception, T-003)",
		RemovalEvent:  "end of the deprecation window (switch-over gate — event-bounded, no version number)",
		MigrationPath: "migration-guide-v2.md §6 (deprecation plan — adapter group row)",
		Evidence:      "migration path exercised (GATE-REVIEW C2, T-021)",
	},
	{
		Surface:       "project.adapter config key",
		Replacement:   "project.standard",
		RemovalEvent:  "end of the deprecation window (switch-over gate — event-bounded, no version number)",
		MigrationPath: "migration-guide-v2.md §5.4.2 (existing projects), §6",
		Evidence:      "migration path exercised (GATE-REVIEW C2, T-021)",
	},
}
