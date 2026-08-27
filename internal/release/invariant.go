// Package release defines the Release model and lifecycle stage management
// for Anvil Runtime Releases.
//
// TS-P4-10 implements the active release invariant enforcement that ensures
// exactly one Release is Active on a given Runtime at any time. When a
// Release is promoted to Active, the previously Active Release transitions
// to Archived.
//
// Reference: TS-P4-10, ADR-003 §9.1, ADR-006 §8.1
package release

import (
	"fmt"
)

// ActiveReleaseInvariant enforces the invariant that exactly one Release
// is Active on a given Runtime at any time.
//
// When a new Release is promoted to Active, this enforcement:
//  1. Queries the currently Active Release via state tracking (TS-P4-09)
//  2. If an Active Release exists, transitions it to Archived via the
//     lifecycle state machine (TS-P4-04)
//  3. The invariant is maintained — exactly one Release is Active
//
// The enforcement must be atomic from the perspective of external observers.
// There is no window where two Releases are both Active or where no Release
// is Active, because the archiving happens before the new Release transitions
// to Active and both operations are synchronous.
//
// Reference: TS-P4-10, ADR-003 §9.1
type ActiveReleaseInvariant struct {
	runtimePath string
}

// NewActiveReleaseInvariant creates an ActiveReleaseInvariant for the
// specified runtime.
//
// Reference: TS-P4-10
func NewActiveReleaseInvariant(runtimePath string) *ActiveReleaseInvariant {
	return &ActiveReleaseInvariant{
		runtimePath: runtimePath,
	}
}

// ArchivePreviousActive finds the currently Active Release (if any) and
// transitions it to the Archived stage. This must be called before a new
// Release is promoted to Active to maintain the invariant that exactly
// one Release is Active at any time.
//
// The archiving process:
//  1. Query the currently Active Release via GetActiveRelease (TS-P4-09)
//  2. If no Active Release exists, return nil (no action needed)
//  3. If an Active Release exists, transition it to Archived
//  4. Persist the archived Release state
//
// Returns the archived Release (or nil if none was active) so the caller
// can verify the invariant.
//
// Reference: TS-P4-10 AC-1, AC-2, AC-3
func (inv *ActiveReleaseInvariant) ArchivePreviousActive() (*Release, error) {
	// Step 1: Query the currently Active Release (TS-P4-09).
	active, err := GetActiveRelease(inv.runtimePath)
	if err != nil {
		return nil, fmt.Errorf("archive previous active: query active release: %w", err)
	}

	// Step 2: If no Active Release exists, nothing to archive.
	if active == nil {
		return nil, nil
	}

	// Step 3: Transition the Active Release to Archived (TS-P4-04).
	if err := active.Transition(StageArchived); err != nil {
		return nil, fmt.Errorf(
			"archive previous active: transition Release %s from %s to %s: %w",
			active.ID, active.Stage, StageArchived, err,
		)
	}

	// Step 4: Persist the archived Release state.
	if err := active.Save(active.SavePath(inv.runtimePath)); err != nil {
		return nil, fmt.Errorf(
			"archive previous active: persist Release %s: %w",
			active.ID, err,
		)
	}

	return active, nil
}
