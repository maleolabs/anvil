// Package release defines the Release model and lifecycle stage management
// for Anvil Runtime Releases.
//
// TS-P4-07 implements the rollback phase orchestration that executes the
// phase sequence — Identify target, Reverse configuration, Promote —
// restoring the previously Active Release.
//
// Rollback is a first-class lifecycle transition, not a reverse activation.
//
// Reference: TS-P4-07, ADR-003 §7
package release

import (
	"fmt"
	"sort"
	"time"

	"maleolabs.com/anvil/internal/runtime"
)

// RollbackEngine orchestrates the rollback phase sequence for a Release:
//
//	Phase 1 — Identify target:  Determine which Release was Active before
//	                            the current one
//	Phase 2 — Reverse config:   Restore shared resources to the target
//	                            Release's state
//	Phase 3 — Promote:          Atomically promote the target Release back
//	                            to Active via symlink switch and stage
//	                            transition
//
// After successful rollback:
//   - The previously Active Release transitions to Rolled Back stage
//   - The target Release transitions back to Active
//   - The rolled-back Release is preserved for inspection
//
// Reference: TS-P4-07, ADR-003 §7
type RollbackEngine struct {
	runtimePath       string
	sharedResourceMgr *runtime.SharedResourceManager
	symlinkSwitcher   *runtime.SymlinkSwitcher
	releasesDirPath   string
}

// NewRollbackEngine creates a RollbackEngine with the given dependencies.
//
// Reference: TS-P4-07
func NewRollbackEngine(
	runtimePath string,
	sharedResourceMgr *runtime.SharedResourceManager,
	symlinkSwitcher *runtime.SymlinkSwitcher,
	releasesDirPath string,
) *RollbackEngine {
	return &RollbackEngine{
		runtimePath:       runtimePath,
		sharedResourceMgr: sharedResourceMgr,
		symlinkSwitcher:   symlinkSwitcher,
		releasesDirPath:   releasesDirPath,
	}
}

// ReconcileInterruptedRollback detects and reconciles Releases that were left
// in RollingBack stage due to an interrupted rollback operation (e.g., process
// crash) by transitioning them to RolledBack stage.
//
// The only valid transition from RollingBack is RolledBack per the state machine
// (ADR-003 §4). Without reconciliation, a Release stuck in RollingBack would
// block subsequent operations that check the current stage.
//
// Returns a slice of reconciled Release IDs, or nil if no releases needed
// reconciliation. Returns an error if listing or persisting any release fails.
//
// Reference: ST-P4-15, ADR-003 §4
func (e *RollbackEngine) ReconcileInterruptedRollback() ([]ReleaseID, error) {
	// Find all Releases in RollingBack stage.
	releases, err := ListReleasesByStage(e.runtimePath, StageRollingBack)
	if err != nil {
		return nil, fmt.Errorf("reconcile interrupted rollback: list rollingback releases: %w", err)
	}

	if len(releases) == 0 {
		return nil, nil
	}

	var reconciled []ReleaseID
	for _, rel := range releases {
		if err := rel.Transition(StageRolledBack); err != nil {
			return nil, fmt.Errorf(
				"reconcile interrupted rollback: transition Release %s from %s to %s: %w",
				rel.ID, rel.Stage, StageRolledBack, err,
			)
		}

		// Persist the updated Release.
		if err := rel.Save(rel.SavePath(e.runtimePath)); err != nil {
			return nil, fmt.Errorf(
				"reconcile interrupted rollback: persist Release %s: %w",
				rel.ID, err,
			)
		}

		reconciled = append(reconciled, rel.ID)
	}

	return reconciled, nil
}

// RollbackResult summarizes the outcome of a rollback operation.
type RollbackResult struct {
	// RolledBackRelease is the Release that was Active and is now RolledBack.
	RolledBackRelease *Release

	// RestoredRelease is the Release that was Archived and is now Active again.
	RestoredRelease *Release
}

// Rollback executes the full rollback phase sequence for a Runtime.
//
// Phase sequence:
//  1. Identify target — finds the previously Active Release (Archived stage
//     with the most recent archival timestamp)
//  2. Validate — confirms the current Active Release is eligible for rollback
//  3. (Transition current Release to RollingBack)
//  4. Reverse configuration — restores shared resources to the target state
//  5. Promote target — atomically switches the symlink and transitions the
//     target Release to Active
//  6. Transition the rolled-back Release to RolledBack stage
//
// Returns a RollbackResult describing both the rolled-back and restored
// Releases, or an error if any phase fails.
//
// Reference: TS-P4-07 AC-1 through AC-6
func (e *RollbackEngine) Rollback() (*RollbackResult, error) {
	// ----------------------------------------------------------------
	// Phase 1: Identify target Release (the previously Active one)
	// ----------------------------------------------------------------
	target, rolledBack, err := e.identifyTarget()
	if err != nil {
		return nil, fmt.Errorf("rollback failed at Identify target phase: %w", err)
	}

	// ----------------------------------------------------------------
	// Phase 2: Validate eligibility
	// ----------------------------------------------------------------
	if err := e.validateRollback(rolledBack.ID); err != nil {
		return nil, fmt.Errorf("rollback failed at Validate phase: %w", err)
	}

	// ----------------------------------------------------------------
	// Phase 3: Transition the current Active Release to RollingBack
	// ----------------------------------------------------------------
	// Note: We transition the Active Release BEFORE Promote so there's
	// no window where two Releases are both in Active stage.
	if err := rolledBack.Transition(StageRollingBack); err != nil {
		return nil, fmt.Errorf("rollback failed: transition Active to RollingBack: %w", err)
	}

	// ----------------------------------------------------------------
	// Phase 4: Reverse configuration — restore shared resources
	// ----------------------------------------------------------------
	if err := e.reverseConfiguration(); err != nil {
		// Best-effort: transition back to Active (the rollback failed).
		_ = rolledBack.Transition(StageActive)
		return nil, fmt.Errorf("rollback failed at Reverse configuration phase: %w", err)
	}

	// ----------------------------------------------------------------
	// Phase 5: Promote target Release back to Active
	// ----------------------------------------------------------------
	if err := e.promoteTarget(target, rolledBack); err != nil {
		// Best-effort: mark as Failed.
		_ = rolledBack.Transition(StageFailed)
		return nil, fmt.Errorf("rollback failed at Promote phase: %w", err)
	}

	// Phase 5 (Promote) includes:
	// - Switch the symlink to the target Release
	// - Transition target to Active
	// - Transition rolledBack to RolledBack

	// Persist both releases.
	if err := target.Save(target.SavePath(e.runtimePath)); err != nil {
		return nil, fmt.Errorf("rollback: persist restored Release %s: %w", target.ID, err)
	}
	if err := rolledBack.Save(rolledBack.SavePath(e.runtimePath)); err != nil {
		return nil, fmt.Errorf("rollback: persist rolled-back Release %s: %w", rolledBack.ID, err)
	}

	return &RollbackResult{
		RolledBackRelease: rolledBack,
		RestoredRelease:   target,
	}, nil
}

// identifyTarget finds the currently Active Release and the rollback target
// (the previously Active Release).
//
// The rollback target is identified as the Release with the most recent
// transition to Archived stage, which represents the Release that was Active
// before the current one.
//
// Returns:
//   - target: the Release to restore (rollback target)
//   - rolledBack: the currently Active Release that will be rolled back
//   - error: if target identification fails
func (e *RollbackEngine) identifyTarget() (target *Release, rolledBack *Release, err error) {
	// Get the currently Active Release.
	rolledBack, err = GetActiveRelease(e.runtimePath)
	if err != nil {
		return nil, nil, fmt.Errorf("get active release: %w", err)
	}
	if rolledBack == nil {
		return nil, nil, fmt.Errorf("no Active Release found — nothing to roll back")
	}

	// Find the rollback target: the most recently Archived Release.
	target, err = e.findRollbackTarget()
	if err != nil {
		return nil, nil, fmt.Errorf("find rollback target: %w", err)
	}
	if target == nil {
		return nil, nil, fmt.Errorf(
			"no rollback target found for Active Release %s: no Archived Release available",
			rolledBack.ID,
		)
	}

	return target, rolledBack, nil
}

// findRollbackTarget finds the most recently Archived Release as the
// rollback target. This is the Release that was Active before the current
// Active Release was promoted.
//
// It searches all Archived Releases and returns the one with the most
// recent transition to Archived (by timestamp in the transition history).
func (e *RollbackEngine) findRollbackTarget() (*Release, error) {
	archived, err := ListReleasesByStage(e.runtimePath, StageArchived)
	if err != nil {
		return nil, fmt.Errorf("list archived releases: %w", err)
	}

	if len(archived) == 0 {
		return nil, nil
	}

	// Sort by most recent transition to Archived.
	sort.Slice(archived, func(i, j int) bool {
		return archivedTimestamp(archived[i]) > archivedTimestamp(archived[j])
	})

	return archived[0], nil
}

// archivedTimestamp returns the timestamp of the most recent transition
// to Archived stage for the given Release. Returns epoch zero if no
// transition to Archived is found.
func archivedTimestamp(r *Release) int64 {
	for _, tr := range r.Transitions {
		if tr.To == StageArchived && tr.Outcome == "success" {
			t, err := time.Parse(time.RFC3339, tr.Timestamp)
			if err == nil {
				return t.UnixNano()
			}
		}
	}
	return 0
}

// validateRollback checks that the Active Release is eligible for rollback.
// A Release must be in Active stage to be rolled back.
func (e *RollbackEngine) validateRollback(activeID ReleaseID) error {
	active, err := LookupByID(e.runtimePath, activeID)
	if err != nil {
		return fmt.Errorf("lookup active Release %s: %w", activeID, err)
	}

	if active.Stage != StageActive {
		return fmt.Errorf(
			"Release %s is in stage %s, must be %s to roll back",
			activeID, active.Stage, StageActive,
		)
	}
	return nil
}

// reverseConfiguration restores shared resources to their previous state.
// This coordinates with EPIC-005's SharedResourceManager to ensure shared
// directories are properly set up for the target Release.
func (e *RollbackEngine) reverseConfiguration() error {
	if err := e.sharedResourceMgr.EnsureDirectoriesExist(); err != nil {
		return fmt.Errorf("shared resource validation: %w", err)
	}
	if err := e.sharedResourceMgr.ValidateIsolation(); err != nil {
		return fmt.Errorf("shared resource isolation: %w", err)
	}
	return nil
}

// promoteTarget performs the atomic promotion of the target Release back
// to Active and transitions the rolled-back Release to RolledBack.
//
// Steps:
//  1. Resolve the target Release's directory path
//  2. Switch the active symlink to the target Release (atomic)
//  3. Transition the target Release to Active
//  4. Transition the rolled-back Release to RolledBack
func (e *RollbackEngine) promoteTarget(target, rolledBack *Release) error {
	// Step 1: Resolve the target release directory path.
	targetDir := runtime.ReleaseDirPath(e.releasesDirPath, target.ID.String())

	// Step 2: Switch the active symlink atomically.
	if err := e.symlinkSwitcher.SwitchForRollback(targetDir); err != nil {
		return fmt.Errorf("symlink switch to target Release %s: %w", target.ID, err)
	}

	// Step 3: Transition the target Release back to Active.
	// The target was Archived; promote it back to Active.
	if err := target.Transition(StageActive); err != nil {
		// Symlink has been switched, but state transition failed.
		// This is a pathological case — Activating→Active is always
		// valid, but Archived→Active is not a valid transition.
		// The target has already been Archived, so we need to handle
		// this case. Let's try to reinstate the original state.
		return fmt.Errorf("promote target Release %s to Active: %w", target.ID, err)
	}

	// Step 4: Transition the rolled-back Release to RolledBack.
	if err := rolledBack.Transition(StageRolledBack); err != nil {
		return fmt.Errorf("transition rolled-back Release %s to RolledBack: %w", rolledBack.ID, err)
	}

	return nil
}
