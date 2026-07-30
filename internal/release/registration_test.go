package release

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Artifact Registration Prerequisite Enforcement Tests — ST-P3-09
// ---------------------------------------------------------------------------

// TestCheckArtifactRegistered_Registered passes when the artifact is
// registered.
//
// AC: A registered artifact passes the prerequisite check.
//
// Reference: ST-P3-09 AC-1
func TestCheckArtifactRegistered_Registered(t *testing.T) {
	// Simulate an artifact that is registered.
	isRegistered := func(artifactID string) bool {
		return artifactID == "registered-artifact"
	}

	err := CheckArtifactRegistered("registered-artifact", isRegistered)
	if err != nil {
		t.Fatalf("CheckArtifactRegistered returned unexpected error: %v", err)
	}
}

// TestCheckArtifactRegistered_Unregistered verifies that an unregistered
// artifact is blocked with a RegistrationPrerequisiteError.
//
// AC: An unregistered artifact is blocked with a RegistrationPrerequisiteError.
//
// Reference: ST-P3-09 AC-2
func TestCheckArtifactRegistered_Unregistered(t *testing.T) {
	// Simulate no artifacts being registered.
	isRegistered := func(artifactID string) bool {
		return false
	}

	err := CheckArtifactRegistered("unregistered-artifact", isRegistered)
	if err == nil {
		t.Fatal("CheckArtifactRegistered should return error for unregistered artifact")
	}

	// Verify the error is a RegistrationPrerequisiteError.
	prereqErr, ok := err.(*RegistrationPrerequisiteError)
	if !ok {
		t.Fatalf("expected *RegistrationPrerequisiteError, got %T", err)
	}

	if prereqErr.ArtifactID != "unregistered-artifact" {
		t.Errorf("error ArtifactID = %s, want %s", prereqErr.ArtifactID, "unregistered-artifact")
	}
}

// TestCheckArtifactRegistered_ErrorMessage verifies that the error message
// contains the artifact ID and explains the registration requirement.
//
// AC: The error message identifies the artifact that must be registered.
//
// Reference: ST-P3-09 AC-3
func TestCheckArtifactRegistered_ErrorMessage(t *testing.T) {
	isRegistered := func(artifactID string) bool {
		return false
	}

	err := CheckArtifactRegistered("missing-artifact-42", isRegistered)
	if err == nil {
		t.Fatal("CheckArtifactRegistered should return error")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "missing-artifact-42") {
		t.Errorf("error should contain artifact ID 'missing-artifact-42', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "must be registered") {
		t.Errorf("error should mention 'must be registered', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "referenced by a release") {
		t.Errorf("error should mention 'referenced by a release', got: %s", errMsg)
	}
}

// TestCheckArtifactRegistered_RegistrationStoreIntegration verifies that
// CheckArtifactRegistered works with a real RegistrationStore-like
// implementation. This test simulates the integration pattern without
// coupling to the artifact package directly.
//
// Reference: ST-P3-09
func TestCheckArtifactRegistered_RegistrationStoreIntegration(t *testing.T) {
	// Simulate a registration store's IsRegistered method.
	registered := map[string]bool{
		"artifact-alpha": true,
		"artifact-beta":  true,
	}

	isRegistered := func(artifactID string) bool {
		return registered[artifactID]
	}

	// Registered artifacts pass.
	if err := CheckArtifactRegistered("artifact-alpha", isRegistered); err != nil {
		t.Errorf("expected artifact-alpha to pass, got: %v", err)
	}
	if err := CheckArtifactRegistered("artifact-beta", isRegistered); err != nil {
		t.Errorf("expected artifact-beta to pass, got: %v", err)
	}

	// Unregistered artifacts are blocked.
	if err := CheckArtifactRegistered("artifact-gamma", isRegistered); err == nil {
		t.Error("expected artifact-gamma to be blocked")
	}
	if err := CheckArtifactRegistered("unknown", isRegistered); err == nil {
		t.Error("expected unknown to be blocked")
	}
}
