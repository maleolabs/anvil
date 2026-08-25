// Package release defines the Release model and lifecycle stage management
// for Anvil Runtime Releases.
//
// This file implements the lifecycle observability queries — the read-only
// surface that reports what is active, what is installed, and what can roll
// back from the authoritative lifecycle state (ADR-036 §3).
//
// All queries in this file are read-only: they never transition stages,
// never persist, and never repair state. They observe the same persisted
// lifecycle state the runtime enforces (ADR-031 §3) through the production
// read paths (ListReleasesByStage, GetActiveRelease) — never by inference
// from memory or filesystem symlinks.
//
// Reference: TS-015-05-01, ADR-036 §3, ADR-031
package release

import (
	"fmt"
	"sort"
)

// RollbackEligibility describes whether the Runtime can roll back right now:
// whether a Release is Active (the one a rollback would reverse) and whether
// a previously Active Release exists as the rollback target.
//
// The eligibility mirrors the rollback engine's target identification
// (RollbackEngine.Rollback, TS-P4-07) — the same two conditions, observed
// read-only. When Eligible is false, Reason explains why.
//
// Reference: TS-015-05-01, TS-P4-07
type RollbackEligibility struct {
	// Eligible is true when an Active Release is present and a rollback
	// target (previously Active, Archived Release) is present.
	Eligible bool

	// ActiveReleaseID is the Release a rollback would reverse (the
	// currently Active Release). Empty when no Release is Active.
	ActiveReleaseID ReleaseID

	// TargetReleaseID is the Release a rollback would restore (the most
	// recently Archived Release). Empty when no target is available.
	TargetReleaseID ReleaseID

	// Reason explains why rollback is not possible when Eligible is false.
	Reason string
}

// FindRollbackTarget returns the Release that a rollback would restore: the
// most recently Archived Release, which represents the Release that was
// Active before the current one.
//
// Returns nil and no error when no Archived Release exists. This is the
// read-only form of RollbackEngine.findRollbackTarget — the rollback engine
// delegates to this query so lifecycle enforcement and observability share
// the same target identification (single source of truth).
//
// Read-only: never mutates state.
//
// Reference: TS-015-05-01, TS-P4-07
func FindRollbackTarget(runtimePath string) (*Release, error) {
	archived, err := ListReleasesByStage(runtimePath, StageArchived)
	if err != nil {
		return nil, fmt.Errorf("find rollback target: %w", err)
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

// GetRollbackEligibility reports whether the Runtime can roll back, observed
// read-only from the authoritative lifecycle state.
//
// Rollback is possible when exactly two conditions hold (the same conditions
// the RollbackEngine validates before rolling back, TS-P4-07):
//  1. A Release is currently Active — the Release a rollback would reverse.
//  2. A previously Active Release exists (Archived stage) — the rollback
//     target a rollback would restore.
//
// Returns an error only when the underlying state cannot be read; absence of
// an Active Release or of a target is a non-eligible result, not an error.
//
// Reference: TS-015-05-01, TS-P4-07
func GetRollbackEligibility(runtimePath string) (RollbackEligibility, error) {
	active, err := GetActiveRelease(runtimePath)
	if err != nil {
		return RollbackEligibility{}, fmt.Errorf("rollback eligibility: %w", err)
	}

	if active == nil {
		return RollbackEligibility{
			Reason: "no Active Release — nothing to roll back",
		}, nil
	}

	target, err := FindRollbackTarget(runtimePath)
	if err != nil {
		return RollbackEligibility{}, fmt.Errorf("rollback eligibility: %w", err)
	}

	if target == nil {
		return RollbackEligibility{
			ActiveReleaseID: active.ID,
			Reason:          "no rollback target — no previously Active (Archived) Release",
		}, nil
	}

	return RollbackEligibility{
		Eligible:        true,
		ActiveReleaseID: active.ID,
		TargetReleaseID: target.ID,
	}, nil
}
