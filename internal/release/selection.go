// Package release defines the Release model and lifecycle stage management
// for Anvil Runtime Releases.
//
// ST-P4-12 implements the release selection logic for activation, selecting
// the appropriate Release based on the configured strategy:
//   - Specific identity (if provided)
//   - Most recently created Release in Ready stage (if no identity provided)
//
// Reference: ST-P4-12
package release

import "fmt"

// SelectReleaseForActivation selects the appropriate Release for activation
// based on the following strategy:
//   - If a specific identity is provided, that Release is used (subject to eligibility)
//   - If no identity is provided, the most recently created Release in Ready stage is selected
//
// The selected Release must be in Ready stage — otherwise an error is returned.
// If no Release is in Ready stage, an error is returned.
//
// Reference: ST-P4-12
func SelectReleaseForActivation(runtimePath string, releaseID ReleaseID) (*Release, error) {
	if releaseID != "" {
		// Specific identity provided — look up and validate eligibility.
		rel, err := LookupByID(runtimePath, releaseID)
		if err != nil {
			return nil, fmt.Errorf("select release for activation: %w", err)
		}

		if rel.Stage != StageReady {
			return nil, fmt.Errorf(
				"select release for activation: release %s is in stage %s, expected Ready",
				rel.ID, rel.Stage,
			)
		}

		return rel, nil
	}

	// No specific identity — select the most recently created Ready Release.
	readyReleases, err := ListReleasesByStage(runtimePath, StageReady)
	if err != nil {
		return nil, fmt.Errorf("select release for activation: %w", err)
	}

	if len(readyReleases) == 0 {
		return nil, fmt.Errorf("select release for activation: no releases in Ready stage")
	}

	// Find the latest (most recently created) Release by CreatedAt timestamp.
	latest := readyReleases[0]
	for _, rel := range readyReleases[1:] {
		if rel.CreatedAt > latest.CreatedAt {
			latest = rel
		}
	}

	return latest, nil
}
