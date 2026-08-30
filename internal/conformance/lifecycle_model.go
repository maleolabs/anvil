package conformance

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// conformanceProjectID is the project identity the harness fixtures use
// and the Anvil runtime binding registers. It is a fixture constant —
// the checks and the binding agree on it so artifacts install into the
// runtime under test.
const conformanceProjectID = "conformance-project"

// addLifecycleModelChecks registers the lifecycle-model contract checks
// (lifecycle-model.md; lifecycle-model.schema.json; ADR-003 §4, §6–§9).
//
// The checks assert the runtime's observable lifecycle behavior: the
// installation operation (§6.3), graph-validated transitions (R2), the
// one-Active invariant (R3), atomic activation (R4), forward rollback
// with state-defined legality (R5), no-silent-success recovery (R6),
// idempotent installation by content identity (R7), and decisions
// derived from persisted state (R8).
func (h *Harness) addLifecycleModelChecks() {
	const contract = "lifecycle-model"

	// L-01: Installation is an operation, not a state transition: a
	// verified artifact is adopted by manifest identity and a Release is
	// created directly in Ready (lifecycle-model.md §6.3; ADR-003 §4).
	h.add(Check{
		ID:          "L-01",
		Contract:    contract,
		Requirement: "lifecycle-model.md §6.3 (installation operation)",
		Title:       "installation creates a Release directly in Ready",
		Expected:    "Installing a verified artifact creates exactly one Release in the Ready state, and the state is persisted and queryable.",
		Run: func(rt Runtime, ws Workspace) *Result {
			artifact, err := makeArtifact(rt, ws, "1.0.0", "<?php\n// content L-01\n")
			if err != nil {
				return Fail("fixture: " + err.Error())
			}

			rel, err := rt.Install(artifact.Path)
			if err != nil {
				return Fail(fmt.Sprintf("Install returned an error: %v", err))
			}
			if rel.Stage != StageReady {
				return Fail(fmt.Sprintf("installed Release stage = %q, want %q (contract §6.3: createsState Ready)", rel.Stage, StageReady))
			}
			if rel.ArtifactID != artifact.Manifest.ArtifactID {
				return Fail(fmt.Sprintf("installed Release artifact identity = %q, want manifest identity %q (adoption by manifest identity, §6.3)", rel.ArtifactID, artifact.Manifest.ArtifactID))
			}

			stage, err := rt.StageOf(rel.ID)
			if err != nil {
				return Fail(fmt.Sprintf("StageOf(%s) returned an error — the state must be persisted and queryable (R8): %v", rel.ID, err))
			}
			if stage != StageReady {
				return Fail(fmt.Sprintf("persisted stage = %q, want %q (R8: decisions derive from persisted state)", stage, StageReady))
			}
			return Pass()
		},
	})

	// L-02: Installation is idempotent by content identity (R7): the
	// same artifact installed twice is one Release, not two
	// (lifecycle-model.md §6.2 R7; artifact-manifest.md §4.1).
	h.add(Check{
		ID:          "L-02",
		Contract:    contract,
		Requirement: "lifecycle-model.md §6.2 R7",
		Title:       "installation is idempotent by content identity",
		Expected:    "Installing the same artifact a second time must not create a second Release: the runtime either rejects the second install or returns the already-installed Release — in both cases exactly one Release exists (one Release, not two).",
		Run: func(rt Runtime, ws Workspace) *Result {
			artifact, err := makeArtifact(rt, ws, "1.0.0", "<?php\n// content L-02\n")
			if err != nil {
				return Fail("fixture: " + err.Error())
			}

			first, err := rt.Install(artifact.Path)
			if err != nil {
				return Fail(fmt.Sprintf("first Install returned an error: %v", err))
			}

			// A conforming runtime either rejects the duplicate install
			// or returns the same Release — idempotent success. What is
			// never conforming is a second, distinct Release (R7: one
			// Release, not two).
			second, err := rt.Install(artifact.Path)
			if err == nil && second.ID != first.ID {
				return Fail(fmt.Sprintf("second Install of the same artifact returned Release %q, not the first-installed %q — the same artifact installed twice must be one Release, not two (R7)", second.ID, first.ID))
			}

			ready, err := rt.ReleasesIn(StageReady)
			if err != nil {
				return Fail(fmt.Sprintf("ReleasesIn(Ready) returned an error: %v", err))
			}
			if len(ready) != 1 {
				return Fail(fmt.Sprintf("after installing the same artifact twice, %d Release(s) exist in Ready, want exactly 1 (R7: one Release, not two)", len(ready)))
			}
			if ready[0].ID != first.ID {
				return Fail(fmt.Sprintf("the surviving Release is %q, want the first-installed %q", ready[0].ID, first.ID))
			}
			return Pass()
		},
	})

	// L-03: Illegal transitions are rejected, not advised against (R2):
	// a Release that is already Active cannot be activated again — the
	// graph-validated machine rejects Active → Activating
	// (lifecycle-model.md §6.2 R2; §6.4).
	h.add(Check{
		ID:          "L-03",
		Contract:    contract,
		Requirement: "lifecycle-model.md §6.2 R2, §6.4",
		Title:       "illegal transitions are rejected",
		Expected:    "An activation attempt on a Release that is not in Ready (e.g. already Active) is rejected with an error and the Release's stage is left unchanged.",
		Run: func(rt Runtime, ws Workspace) *Result {
			artifact, err := makeArtifact(rt, ws, "1.0.0", "<?php\n// content L-03\n")
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			rel, err := rt.Install(artifact.Path)
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			if err := rt.Activate(rel.ID); err != nil {
				return Fail(fmt.Sprintf("activating a Ready Release returned an error (fixture): %v", err))
			}

			// Re-activation from Active must be rejected (Active →
			// Activating is not an edge of the machine, §6.4).
			err = rt.Activate(rel.ID)
			if err == nil {
				return Fail("activating the already-Active Release succeeded — illegal transitions must be rejected, not advised against (R2)")
			}

			stage, stageErr := rt.StageOf(rel.ID)
			if stageErr != nil {
				return Fail(fmt.Sprintf("StageOf returned an error: %v", stageErr))
			}
			if stage != StageActive {
				return Fail(fmt.Sprintf("after the rejected re-activation the Release stage = %q, want %q (a rejected transition must not change the stage)", stage, StageActive))
			}
			return Pass()
		},
	})

	// L-04: The legal activation path is Ready → Activating → Active,
	// recorded in the transition history (lifecycle-model.md §6.4).
	h.add(Check{
		ID:          "L-04",
		Contract:    contract,
		Requirement: "lifecycle-model.md §6.4 (Ready→Activating, Activating→Active)",
		Title:       "activation advances Ready → Activating → Active",
		Expected:    "Activating a Ready Release succeeds and records the legal transitions Ready → Activating → Active in its history, both as successes.",
		Run: func(rt Runtime, ws Workspace) *Result {
			artifact, err := makeArtifact(rt, ws, "1.0.0", "<?php\n// content L-04\n")
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			rel, err := rt.Install(artifact.Path)
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			if err := rt.Activate(rel.ID); err != nil {
				return Fail(fmt.Sprintf("Activate returned an error: %v", err))
			}

			stage, err := rt.StageOf(rel.ID)
			if err != nil {
				return Fail(fmt.Sprintf("StageOf returned an error: %v", err))
			}
			if stage != StageActive {
				return Fail(fmt.Sprintf("stage after activation = %q, want %q", stage, StageActive))
			}

			history, err := rt.HistoryOf(rel.ID)
			if err != nil {
				return Fail(fmt.Sprintf("HistoryOf returned an error: %v", err))
			}
			if len(history) < 2 {
				return Fail(fmt.Sprintf("transition history has %d record(s), want at least 2 (Ready→Activating, Activating→Active)", len(history)))
			}
			if history[0].From != StageReady || history[0].To != StageActivating || history[0].Outcome != "success" {
				return Fail(fmt.Sprintf("history[0] = %s → %s (%q), want Ready → Activating (success)", history[0].From, history[0].To, history[0].Outcome))
			}
			if history[1].From != StageActivating || history[1].To != StageActive || history[1].Outcome != "success" {
				return Fail(fmt.Sprintf("history[1] = %s → %s (%q), want Activating → Active (success)", history[1].From, history[1].To, history[1].Outcome))
			}
			return Pass()
		},
	})

	// L-05: No silent success (R6): a failed activation is reported as a
	// failure, never as success, and does not disturb the Active
	// Release (atomicity, R4 — the previous Release keeps serving).
	h.add(Check{
		ID:          "L-05",
		Contract:    contract,
		Requirement: "lifecycle-model.md §6.2 R4, R6",
		Title:       "a failed activation is reported and leaves the previous Release active",
		Expected:    "When activation of a Release fails (its stored artifact is no longer consumable), the operation returns an error, the Release is not reported Active, and the previously Active Release remains Active.",
		Run: func(rt Runtime, ws Workspace) *Result {
			a, err := makeArtifact(rt, ws, "1.0.0", "<?php\n// content L-05-A\n")
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			relA, err := rt.Install(a.Path)
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			if err := rt.Activate(relA.ID); err != nil {
				return Fail(fmt.Sprintf("fixture: activating Release A: %v", err))
			}

			b, err := makeArtifact(rt, ws, "2.0.0", "<?php\n// content L-05-B\n")
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			relB, err := rt.Install(b.Path)
			if err != nil {
				return Fail("fixture: " + err.Error())
			}

			// Fixture: make B's stored artifact unreadable so the
			// activation cannot consume it.
			if err := corruptFile(relB.ArtifactPath); err != nil {
				return Fail("fixture: " + err.Error())
			}

			err = rt.Activate(relB.ID)
			if err == nil {
				return Fail("activation of a Release whose artifact is unreadable succeeded — an interrupted/failed operation must never be silently reported as success (R6)")
			}

			active, activeErr := rt.Active()
			if activeErr != nil {
				return Fail(fmt.Sprintf("Active() returned an error: %v", activeErr))
			}
			if active == nil || active.ID != relA.ID {
				return Fail(fmt.Sprintf("after the failed activation, the Active Release = %v, want %q — the previous Release must remain Active (R4 atomicity)", active, relA.ID))
			}
			return Pass()
		},
	})

	// L-06: The one-Active invariant (R3) with automatic archival
	// (ADR-003 §9.1): when a second Release is activated, the previously
	// Active Release automatically becomes Archived, so exactly one
	// Release is Active at any time (lifecycle-model.md §6.2 R3; §6.4
	// Active→Archived).
	h.add(Check{
		ID:          "L-06",
		Contract:    contract,
		Requirement: "lifecycle-model.md §6.2 R3; §6.4 (Active→Archived)",
		Title:       "activating a second Release archives the previous one — exactly one Active",
		Expected:    "When a new Release is activated while another is Active, the previously Active Release automatically transitions to Archived (recorded as a successful transition) and exactly one Release is Active.",
		Run: func(rt Runtime, ws Workspace) *Result {
			a, err := makeArtifact(rt, ws, "1.0.0", "<?php\n// content L-06-A\n")
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			relA, err := rt.Install(a.Path)
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			b, err := makeArtifact(rt, ws, "2.0.0", "<?php\n// content L-06-B\n")
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			relB, err := rt.Install(b.Path)
			if err != nil {
				return Fail("fixture: " + err.Error())
			}

			if err := rt.Activate(relA.ID); err != nil {
				return Fail(fmt.Sprintf("fixture: activating A: %v", err))
			}
			if err := rt.Activate(relB.ID); err != nil {
				return Fail(fmt.Sprintf("activating B while A is Active returned an error: %v", err))
			}

			active, err := rt.Active()
			if err != nil {
				return Fail(fmt.Sprintf("Active() returned an error: %v", err))
			}
			if active == nil || active.ID != relB.ID {
				return Fail(fmt.Sprintf("Active Release = %v, want %q — exactly one Release must be Active (R3)", active, relB.ID))
			}

			actives, err := rt.ReleasesIn(StageActive)
			if err != nil {
				return Fail(fmt.Sprintf("ReleasesIn(Active) returned an error: %v", err))
			}
			if len(actives) != 1 {
				return Fail(fmt.Sprintf("%d Release(s) in Active state, want exactly 1 (R3: at most one Active at any time)", len(actives)))
			}

			stageA, err := rt.StageOf(relA.ID)
			if err != nil {
				return Fail(fmt.Sprintf("StageOf(A) returned an error: %v", err))
			}
			if stageA != StageArchived {
				return Fail(fmt.Sprintf("previously Active Release A stage = %q, want %q (automatic archival of the superseded Release, ADR-003 §9.1)", stageA, StageArchived))
			}

			historyA, err := rt.HistoryOf(relA.ID)
			if err != nil {
				return Fail(fmt.Sprintf("HistoryOf(A) returned an error: %v", err))
			}
			archived := false
			for _, tr := range historyA {
				if tr.From == StageActive && tr.To == StageArchived && tr.Outcome == "success" {
					archived = true
				}
			}
			if !archived {
				return Fail("the automatic archival of the superseded Release A is not recorded as a successful Active → Archived transition in its history (§6.4)")
			}
			return Pass()
		},
	})

	// L-07: Rollback is a first-class forward transition restoring the
	// previously Active Release (R5; lifecycle-model.md §5.2; ADR-003
	// §7.1): the target returns to Active via Archived → Active and the
	// rolled-back Release is preserved in the Rolled Back state.
	h.add(Check{
		ID:          "L-07",
		Contract:    contract,
		Requirement: "lifecycle-model.md §5.2, §6.4 (Archived→Active, Rolling Back→Rolled Back)",
		Title:       "rollback restores the previously Active Release by a forward transition",
		Expected:    "Rollback restores the previously Active Release to Active via the forward Archived → Active transition, records it in the target's history, and preserves the rolled-back Release in the Rolled Back state for inspection.",
		Run: func(rt Runtime, ws Workspace) *Result {
			a, err := makeArtifact(rt, ws, "1.0.0", "<?php\n// content L-07-A\n")
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			relA, err := rt.Install(a.Path)
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			b, err := makeArtifact(rt, ws, "2.0.0", "<?php\n// content L-07-B\n")
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			relB, err := rt.Install(b.Path)
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			if err := rt.Activate(relA.ID); err != nil {
				return Fail(fmt.Sprintf("fixture: activating A: %v", err))
			}
			if err := rt.Activate(relB.ID); err != nil {
				return Fail(fmt.Sprintf("fixture: activating B: %v", err))
			}

			outcome, err := rt.Rollback()
			if err != nil {
				return Fail(fmt.Sprintf("Rollback returned an error: %v", err))
			}
			if outcome.RestoredRelease == nil || outcome.RestoredRelease.ID != relA.ID {
				return Fail(fmt.Sprintf("rollback restored Release = %v, want %q — rollback restores the previously Active Release (§5.2)", outcome.RestoredRelease, relA.ID))
			}
			if outcome.RolledBackRelease == nil || outcome.RolledBackRelease.ID != relB.ID {
				return Fail(fmt.Sprintf("rolled-back Release = %v, want %q", outcome.RolledBackRelease, relB.ID))
			}

			stageA, err := rt.StageOf(relA.ID)
			if err != nil {
				return Fail(fmt.Sprintf("StageOf(A) returned an error: %v", err))
			}
			if stageA != StageActive {
				return Fail(fmt.Sprintf("restored Release A stage = %q, want %q (rollback restore is the forward transition to Active, §6.4)", stageA, StageActive))
			}
			stageB, err := rt.StageOf(relB.ID)
			if err != nil {
				return Fail(fmt.Sprintf("StageOf(B) returned an error: %v", err))
			}
			if stageB != StageRolledBack {
				return Fail(fmt.Sprintf("rolled-back Release B stage = %q, want %q (the rolled-back Release is preserved for inspection, §5.2)", stageB, StageRolledBack))
			}

			active, err := rt.Active()
			if err != nil {
				return Fail(fmt.Sprintf("Active() returned an error: %v", err))
			}
			if active == nil || active.ID != relA.ID {
				return Fail(fmt.Sprintf("Active Release after rollback = %v, want %q (exactly one Active, R3)", active, relA.ID))
			}

			historyA, err := rt.HistoryOf(relA.ID)
			if err != nil {
				return Fail(fmt.Sprintf("HistoryOf(A) returned an error: %v", err))
			}
			restored := false
			for _, tr := range historyA {
				if tr.From == StageArchived && tr.To == StageActive && tr.Outcome == "success" {
					restored = true
				}
			}
			if !restored {
				return Fail("the rollback restore is not recorded as a successful Archived → Active transition in the target's history (forward transition, R5)")
			}
			return Pass()
		},
	})

	// L-08: Rollback legality is defined by state (R5): when no eligible
	// target exists — no Archived Release that was previously Active —
	// the rollback operation is rejected and the Active Release is
	// untouched.
	h.add(Check{
		ID:          "L-08",
		Contract:    contract,
		Requirement: "lifecycle-model.md §5.2, §6.2 R5",
		Title:       "rollback with no eligible target is rejected",
		Expected:    "Rollback with no Archived rollback target is rejected with an error, and the Active Release remains Active and unchanged.",
		Run: func(rt Runtime, ws Workspace) *Result {
			a, err := makeArtifact(rt, ws, "1.0.0", "<?php\n// content L-08-A\n")
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			relA, err := rt.Install(a.Path)
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			if err := rt.Activate(relA.ID); err != nil {
				return Fail(fmt.Sprintf("fixture: activating A: %v", err))
			}

			_, err = rt.Rollback()
			if err == nil {
				return Fail("rollback succeeded although no eligible target exists — rollback legality must be defined by state and the operation rejected otherwise (R5)")
			}

			stage, stageErr := rt.StageOf(relA.ID)
			if stageErr != nil {
				return Fail(fmt.Sprintf("StageOf returned an error: %v", stageErr))
			}
			if stage != StageActive {
				return Fail(fmt.Sprintf("after the rejected rollback the Release stage = %q, want %q — the rejected operation must not change the state", stage, StageActive))
			}
			return Pass()
		},
	})

	// L-09: The rollback target is the Release last superseded by the
	// current Active Release — operationally, the Archived Release with
	// the newest archival timestamp among Releases that were previously
	// Active (lifecycle-model.md §5.2; ADR-003 §7.3).
	//
	// The activations are separated by more than the runtime's recorded
	// timestamp resolution so the "newest archival timestamp" rule is
	// well-defined: consecutive archivals within the same recorded
	// timestamp would leave the newest-archived selection undefined by
	// the contract, and asserting a tie outcome would invent a
	// requirement (TS-013-05-03 §4).
	h.add(Check{
		ID:          "L-09",
		Contract:    contract,
		Requirement: "lifecycle-model.md §5.2 (rollback target selection)",
		Title:       "rollback selects the newest archived previously-Active Release as target",
		Expected:    "With three sequential activations (A, then B, then C), the first rollback restores B — the newest archived previously-Active Release — and the second rollback restores A.",
		Run: func(rt Runtime, ws Workspace) *Result {
			var releases []ReleaseInfo
			for i, content := range []string{"<?php\n// content L-09-A\n", "<?php\n// content L-09-B\n", "<?php\n// content L-09-C\n"} {
				artifact, err := makeArtifact(rt, ws, fmt.Sprintf("%d.0.0", i+1), content)
				if err != nil {
					return Fail("fixture: " + err.Error())
				}
				rel, err := rt.Install(artifact.Path)
				if err != nil {
					return Fail("fixture: " + err.Error())
				}
				if err := rt.Activate(rel.ID); err != nil {
					return Fail(fmt.Sprintf("fixture: activating release %d: %v", i+1, err))
				}
				// Separate the recorded archival timestamps so the
				// selection rule is well-defined (see check note).
				if i < 2 {
					time.Sleep(1100 * time.Millisecond)
				}
				releases = append(releases, rel)
			}

			first, err := rt.Rollback()
			if err != nil {
				return Fail(fmt.Sprintf("first Rollback returned an error: %v", err))
			}
			if first.RestoredRelease == nil || first.RestoredRelease.ID != releases[1].ID {
				return Fail(fmt.Sprintf("first rollback restored %v, want %q — the rollback target is the newest archived previously-Active Release (B)", first.RestoredRelease, releases[1].ID))
			}

			second, err := rt.Rollback()
			if err != nil {
				return Fail(fmt.Sprintf("second Rollback returned an error: %v", err))
			}
			if second.RestoredRelease == nil || second.RestoredRelease.ID != releases[0].ID {
				return Fail(fmt.Sprintf("second rollback restored %v, want %q — the next target is the remaining archived previously-Active Release (A)", second.RestoredRelease, releases[0].ID))
			}
			return Pass()
		},
	})

	// L-10: An interrupted operation is never silently reported as
	// success (R6): a Release stuck in the Rolling Back transitional
	// stage is reconciled to Rolled Back by the recovery rule
	// (lifecycle-model.md §6.5).
	h.add(Check{
		ID:          "L-10",
		Contract:    contract,
		Requirement: "lifecycle-model.md §6.5, §6.2 R6",
		Title:       "interrupted rollback is reconciled, not silently reported",
		Expected:    "A Release left in the Rolling Back stage by an interrupted rollback is reconciled to Rolled Back by the recovery operation, with the transition recorded in its history.",
		Run: func(rt Runtime, ws Workspace) *Result {
			a, err := makeArtifact(rt, ws, "1.0.0", "<?php\n// content L-10-A\n")
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			relA, err := rt.Install(a.Path)
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			b, err := makeArtifact(rt, ws, "2.0.0", "<?php\n// content L-10-B\n")
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			relB, err := rt.Install(b.Path)
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			if err := rt.Activate(relA.ID); err != nil {
				return Fail(fmt.Sprintf("fixture: activating A: %v", err))
			}
			if err := rt.Activate(relB.ID); err != nil {
				return Fail(fmt.Sprintf("fixture: activating B: %v", err))
			}

			// Fixture: persist the crash state a mid-rollback leaves
			// behind — the Active Release recorded in Rolling Back
			// (lifecycle-model.md §6.5).
			if err := rt.PersistInterruptedRollback(relB.ID); err != nil {
				return Fail("fixture: " + err.Error())
			}

			reconciled, err := rt.ReconcileInterruptedRollback()
			if err != nil {
				return Fail(fmt.Sprintf("reconciliation returned an error: %v", err))
			}
			found := false
			for _, id := range reconciled {
				if id == relB.ID {
					found = true
				}
			}
			if !found {
				return Fail(fmt.Sprintf("reconciliation reported %v, want it to include %q — the interrupted Release must be reconciled explicitly (R6)", reconciled, relB.ID))
			}

			stage, err := rt.StageOf(relB.ID)
			if err != nil {
				return Fail(fmt.Sprintf("StageOf(B) returned an error: %v", err))
			}
			if stage != StageRolledBack {
				return Fail(fmt.Sprintf("reconciled Release B stage = %q, want %q (interrupted rollback reconciled to Rolled Back, §6.5)", stage, StageRolledBack))
			}

			history, err := rt.HistoryOf(relB.ID)
			if err != nil {
				return Fail(fmt.Sprintf("HistoryOf(B) returned an error: %v", err))
			}
			recorded := false
			for _, tr := range history {
				if tr.From == StageRollingBack && tr.To == StageRolledBack && tr.Outcome == "success" {
					recorded = true
				}
			}
			if !recorded {
				return Fail("the reconciliation is not recorded as a successful Rolling Back → Rolled Back transition (R6: no silent success)")
			}
			return Pass()
		},
	})

	// L-11: Lifecycle decisions derive from persisted, queryable,
	// authoritative state (R8) — never from process memory: state,
	// active release, and history are queryable after the operations
	// that produced them (lifecycle-model.md §6.2 R8).
	h.add(Check{
		ID:          "L-11",
		Contract:    contract,
		Requirement: "lifecycle-model.md §6.2 R8",
		Title:       "lifecycle facts derive from persisted state",
		Expected:    "After install and activation, the persisted state reports the Release's stage, the Active Release, and its recorded transition history without further operations.",
		Run: func(rt Runtime, ws Workspace) *Result {
			artifact, err := makeArtifact(rt, ws, "1.0.0", "<?php\n// content L-11\n")
			if err != nil {
				return Fail("fixture: " + err.Error())
			}
			rel, err := rt.Install(artifact.Path)
			if err != nil {
				return Fail("fixture: " + err.Error())
			}

			ready, err := rt.ReleasesIn(StageReady)
			if err != nil {
				return Fail(fmt.Sprintf("ReleasesIn(Ready) returned an error: %v", err))
			}
			if len(ready) != 1 || ready[0].ID != rel.ID {
				return Fail(fmt.Sprintf("after install, ReleasesIn(Ready) = %v, want exactly [%s]", ready, rel.ID))
			}

			if err := rt.Activate(rel.ID); err != nil {
				return Fail(fmt.Sprintf("fixture: activating: %v", err))
			}

			active, err := rt.Active()
			if err != nil {
				return Fail(fmt.Sprintf("Active() returned an error: %v", err))
			}
			if active == nil || active.ID != rel.ID {
				return Fail(fmt.Sprintf("Active() = %v, want %q — the Active Release must be determined from state (R8)", active, rel.ID))
			}

			history, err := rt.HistoryOf(rel.ID)
			if err != nil {
				return Fail(fmt.Sprintf("HistoryOf returned an error: %v", err))
			}
			if len(history) == 0 {
				return Fail("transition history is empty after activation — lifecycle facts must be persisted and queryable (R8)")
			}
			return Pass()
		},
	})
}

// makeArtifact creates a source tree with one file and packages it
// through the runtime under test. It is fixture setup, not an
// assertion: the checks use it to drive the runtime's observable
// operations.
func makeArtifact(rt Runtime, ws Workspace, version, content string) (ArtifactInfo, error) {
	src, err := ws.TempDir("src-")
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("create source dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(src, "index.php"), []byte(content), 0o644); err != nil {
		return ArtifactInfo{}, fmt.Errorf("write source file: %w", err)
	}
	out, err := ws.TempDir("out-")
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("create output dir: %w", err)
	}
	return rt.Package(PackageInput{
		SourceDir: src,
		OutputDir: out,
		Version:   version,
		Source:    conformanceProjectID,
		ProjectID: conformanceProjectID,
	})
}

// corruptFile overwrites the file at path with bytes that are no longer
// a consumable artifact. It is a fixture helper the checks use to make
// an artifact unreadable.
func corruptFile(path string) error {
	return os.WriteFile(path, []byte("corrupted-by-conformance-fixture"), 0o644)
}
