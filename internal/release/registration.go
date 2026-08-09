// Package release defines the Release model and lifecycle stage management
// for Anvil Runtime Releases.
//
// ST-P3-09 implements artifact registration enforcement — a Release can only
// reference an artifact that has been registered in the artifact registry.
// This prerequisite check prevents releases from referencing unregistered
// or unknown artifacts.
//
// Reference: ST-P3-09, ADR-003 §6, EPIC-003
package release

import "fmt"

// RegistrationPrerequisiteError is returned when an artifact has not been
// registered before being referenced by a Release.
//
// Reference: ST-P3-09, ADR-003 §6
type RegistrationPrerequisiteError struct {
	ArtifactID string
}

// Error returns a human-readable error message describing the prerequisite
// failure, including the artifact ID that must be registered.
//
// Reference: ST-P3-09 AC
func (e *RegistrationPrerequisiteError) Error() string {
	return fmt.Sprintf(
		"artifact %q must be registered before it can be referenced by a release",
		e.ArtifactID,
	)
}

// CheckArtifactRegistered verifies that the given artifact ID has been
// registered in the artifact registry. Returns nil if registered, or a
// RegistrationPrerequisiteError if not.
//
// The isRegistered parameter is a callback that allows injecting the
// registration lookup without coupling the release package directly to
// the artifact package. Callers should pass artifact.IsRegistered or
// RegistrationStore.IsRegistered as appropriate.
//
// Reference: ST-P3-09, ADR-003 §6
func CheckArtifactRegistered(artifactID string, isRegistered func(string) bool) error {
	if !isRegistered(artifactID) {
		return &RegistrationPrerequisiteError{ArtifactID: artifactID}
	}
	return nil
}
