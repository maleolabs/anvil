package release

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/project"
)

// CreateRelease creates a new Release by verifying the given artifact
// and associating it with the specified Runtime path.
//
// The release lifecycle:
//  1. Validates the artifact file exists on disk.
//  2. Runs full artifact verification (RequireVerified).
//  3. Reads the artifact manifest to extract the ArtifactID.
//  4. Generates a unique ReleaseID using GenerateReleaseID().
//  5. Populates the Release struct with metadata and stage="ready".
//  6. Persists the Release to .anvil/state/releases/{id}.json.
//
// Returns the created Release or an error describing the failure.
//
// Reference: TS-P4-01, TS-P4-02, EPIC-004
func CreateRelease(artifactPath, runtimePath string) (*Release, error) {
	// Step 1: Validate artifact exists.
	if _, err := os.Stat(artifactPath); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("artifact not found: %s", artifactPath)
		}
		return nil, fmt.Errorf("access artifact: %w", err)
	}

	// Step 2: Verify artifact integrity.
	if err := artifact.RequireVerified(artifactPath); err != nil {
		return nil, fmt.Errorf("artifact must be verified first: %w", err)
	}

	// Step 3: Read manifest to get ArtifactID.
	manifest, err := artifact.ReadManifest(artifactPath)
	if err != nil {
		return nil, fmt.Errorf("read artifact manifest: %w", err)
	}

	// Step 4: Generate Release identity.
	id, err := GenerateReleaseID()
	if err != nil {
		return nil, fmt.Errorf("generate release id: %w", err)
	}

	// Step 5: Create Release struct.
	now := time.Now().UTC()
	release := &Release{
		ID:           id,
		ArtifactID:   manifest.ArtifactID,
		Version:      manifest.Version,
		ArtifactPath: artifactPath,
		RuntimePath:  runtimePath,
		Stage:        StageReady,
		CreatedAt:    now.Format(time.RFC3339),
		Transitions:  []TransitionRecord{},
	}

	// Step 6: Save Release to project state directory.
	s := project.NewStructure(runtimePath)
	releasesDir := filepath.Join(s.StateDir, "releases")

	if err := os.MkdirAll(releasesDir, 0755); err != nil {
		return nil, fmt.Errorf("create releases directory: %w", err)
	}

	releasePath := filepath.Join(releasesDir, id.String()+".json")
	if err := release.Save(releasePath); err != nil {
		return nil, fmt.Errorf("save release: %w", err)
	}

	return release, nil
}
