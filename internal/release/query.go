// Package release defines the Release model and lifecycle stage management
// for Anvil Runtime Releases.
//
// TS-P4-09 implements the state tracking and query interface that records
// and exposes the current lifecycle stage of every Release, supporting
// queries for active release determination.
//
// TS-P4-08 implements the release history recorder that retrieves the
// complete transition history for a Release by identity.
//
// Reference: TS-P4-08, TS-P4-09, ADR-003, ADR-006
package release

import (
	"fmt"
	"os"
	"path/filepath"

	"maleolabs.com/anvil/internal/project"
)

// ---------------------------------------------------------------------------
// State Query Interface — TS-P4-09
//
// The query interface supports three operations:
//  1. GetStateByID  — returns the current lifecycle stage for a Release
//  2. ListByStage   — returns all Releases in a given lifecycle stage
//  3. GetActive     — returns the Release currently in Active stage
//
// All queries are read-only — they never modify state.
// Reference: TS-P4-09 AC-5, ADR-006 §5.1
// ---------------------------------------------------------------------------

// GetReleaseState returns the current lifecycle stage for the Release with
// the given identity in the specified runtime.
//
// Returns an error if the Release does not exist or cannot be loaded.
//
// Reference: TS-P4-09 AC-1
func GetReleaseState(runtimePath string, id ReleaseID) (Stage, error) {
	rel, err := LookupByID(runtimePath, id)
	if err != nil {
		return Stage(0), fmt.Errorf("get release state: %w", err)
	}
	return rel.Stage, nil
}

// ListReleasesByStage returns all Releases in the specified lifecycle stage
// within the given runtime path. Returns an empty slice if no Releases match
// the stage.
//
// Reference: TS-P4-09 AC-2
func ListReleasesByStage(runtimePath string, stage Stage) ([]*Release, error) {
	s := project.NewStructure(runtimePath)
	releasesDir := filepath.Join(s.StateDir, "releases")

	entries, err := os.ReadDir(releasesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*Release{}, nil
		}
		return nil, fmt.Errorf("list releases by stage: read releases dir: %w", err)
	}

	var result []*Release
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		relPath := filepath.Join(releasesDir, entry.Name())
		rel, err := Load(relPath)
		if err != nil {
			continue // skip unreadable releases
		}

		if rel.Stage == stage {
			result = append(result, rel)
		}
	}

	return result, nil
}

// GetActiveRelease returns the Release currently in the Active stage for
// the given runtime. Returns nil and no error if no Release is Active.
//
// The Active Release is determined by reading the stage from every Release's
// persisted state. This is the authoritative mechanism — the state machine
// tracks lifecycle stage, and the Active stage is only set through valid
// lifecycle transitions.
//
// Exactly one Release is expected to be Active at any time (enforced by
// TS-P4-10). This method returns the first Active Release found; if multiple
// exist (which would indicate an invariant violation), the first encountered
// is returned and no error is reported for the duplicate.
//
// Reference: TS-P4-09 AC-3
func GetActiveRelease(runtimePath string) (*Release, error) {
	active, err := ListReleasesByStage(runtimePath, StageActive)
	if err != nil {
		return nil, fmt.Errorf("get active release: %w", err)
	}

	if len(active) == 0 {
		return nil, nil
	}

	// Return the first Active Release found.
	// If multiple exist (invariant violation), log but do not fail.
	return active[0], nil
}

// ---------------------------------------------------------------------------
// Release History Recorder — TS-P4-08
//
// GetReleaseHistory retrieves the complete transition history for a Release
// identified by its identity. The History() method provides access to the
// in-memory transitions of a loaded Release.
//
// Reference: TS-P4-08
// ---------------------------------------------------------------------------

// GetReleaseHistory returns the complete transition history for a Release
// identified by the given identity in the specified runtime.
//
// Returns nil and no error if the Release does not have any recorded
// transitions (empty history).
//
// Reference: TS-P4-08 AC-5
func GetReleaseHistory(runtimePath string, id ReleaseID) ([]TransitionRecord, error) {
	rel, err := LookupByID(runtimePath, id)
	if err != nil {
		return nil, fmt.Errorf("get release history: %w", err)
	}

	if len(rel.Transitions) == 0 {
		return nil, nil
	}

	result := make([]TransitionRecord, len(rel.Transitions))
	copy(result, rel.Transitions)
	return result, nil
}

// History returns the transition history of the Release.
//
// Returns nil if the Release has no recorded transitions.
//
// Reference: TS-P4-08
func (r *Release) History() []TransitionRecord {
	if len(r.Transitions) == 0 {
		return nil
	}

	result := make([]TransitionRecord, len(r.Transitions))
	copy(result, r.Transitions)
	return result
}
