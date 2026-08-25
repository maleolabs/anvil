// Package inspection provides read-only diagnostic inspection capabilities
// for Anvil Runtime environments and configuration.
//
// Reference: ST-P9-02, EPIC-003, EPIC-004
package inspection

import (
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/release"
)

// testRelease builds a Release record for eligibility checks.
func testRelease(stage release.Stage) *release.Release {
	return &release.Release{
		ID:         release.ReleaseID("rel-test"),
		ArtifactID: "art-test",
		Version:    "1.0.0",
		Stage:      stage,
	}
}

// TestCheckArtifactVerification_Registered verifies that a registered
// artifact (EPIC-003 verification status consumption) passes the check.
//
// Reference: ST-P9-02, ST-009-002 AC3/AC7
func TestCheckArtifactVerification_Registered(t *testing.T) {
	rel := testRelease(release.StageReady)
	check := CheckArtifactVerification(rel, func(id string) bool { return id == "art-test" })

	if !check.Passed {
		t.Errorf("CheckArtifactVerification() = failed, want passed: %s", check.Details)
	}
	if check.Details == "" {
		t.Error("CheckArtifactVerification() details should describe the verified artifact")
	}
}

// TestCheckArtifactVerification_Unregistered verifies that an artifact
// absent from the registration store fails the check with guidance to run
// artifact verification.
//
// Reference: ST-P9-02, ST-009-002 AC3
func TestCheckArtifactVerification_Unregistered(t *testing.T) {
	rel := testRelease(release.StageReady)

	check := CheckArtifactVerification(rel, func(id string) bool { return false })

	if check.Passed {
		t.Error("CheckArtifactVerification() should fail for an unregistered artifact")
	}
	if !strings.Contains(check.Details, "has not been verified") {
		t.Errorf("details should report the artifact as unverified, got: %s", check.Details)
	}
	if !strings.Contains(check.Details, "anvil artifact verify") {
		t.Errorf("details should reference artifact verification, got: %s", check.Details)
	}
}

// TestCheckArtifactVerification_NilCallback verifies that a nil callback
// is treated as "not registered" (the artifact is reported unverified)
// rather than crashing.
//
// Reference: ST-P9-02
func TestCheckArtifactVerification_NilCallback(t *testing.T) {
	rel := testRelease(release.StageReady)

	check := CheckArtifactVerification(rel, nil)

	if check.Passed {
		t.Error("CheckArtifactVerification() with nil callback should report unverified")
	}
}

// TestCheckArtifactVerification_NilRelease verifies that a nil Release
// fails the check without panicking.
//
// Reference: ST-P9-02
func TestCheckArtifactVerification_NilRelease(t *testing.T) {
	check := CheckArtifactVerification(nil, func(string) bool { return true })

	if check.Passed {
		t.Error("CheckArtifactVerification(nil release) should fail")
	}
}

// TestCheckReleaseEligibility_Ready verifies that a Ready-stage Release is
// eligible for activation.
//
// Reference: ST-P9-02, ST-009-002 AC4
func TestCheckReleaseEligibility_Ready(t *testing.T) {
	check := CheckReleaseEligibility(testRelease(release.StageReady))

	if !check.Passed {
		t.Errorf("CheckReleaseEligibility() = failed, want passed: %s", check.Details)
	}
}

// TestCheckReleaseEligibility_NotReady verifies that a Release not in the
// Ready stage fails the check with the current stage and the requirement
// to reach Ready.
//
// Reference: ST-P9-02, ST-009-002 AC4
func TestCheckReleaseEligibility_NotReady(t *testing.T) {
	for _, stage := range []release.Stage{
		release.StageActive,
		release.StageArchived,
		release.StageFailed,
		release.StageRolledBack,
		release.StageRemoved,
	} {
		t.Run(stage.String(), func(t *testing.T) {
			check := CheckReleaseEligibility(testRelease(stage))

			if check.Passed {
				t.Errorf("CheckReleaseEligibility(%s) should fail", stage)
			}
			if !strings.Contains(check.Details, "is in stage "+stage.String()) {
				t.Errorf("details should report the current stage %q, got: %s", stage, check.Details)
			}
			if !strings.Contains(check.Details, "requires stage ready") {
				t.Errorf("details should report the required stage, got: %s", check.Details)
			}
		})
	}
}

// TestCheckReleaseEligibility_NilRelease verifies that a nil Release fails
// the check without panicking.
//
// Reference: ST-P9-02
func TestCheckReleaseEligibility_NilRelease(t *testing.T) {
	check := CheckReleaseEligibility(nil)

	if check.Passed {
		t.Error("CheckReleaseEligibility(nil release) should fail")
	}
}

// TestBuildReleaseEligibilityComponent verifies that the component
// assembles both identity-based checks in order with the component-level
// verdict.
//
// Reference: ST-P9-02
func TestBuildReleaseEligibilityComponent(t *testing.T) {
	tests := []struct {
		name       string
		rel        *release.Release
		registered bool
		wantPassed bool
		wantChecks []string
	}{
		{
			name:       "verified artifact and Ready release → passed",
			rel:        testRelease(release.StageReady),
			registered: true,
			wantPassed: true,
			wantChecks: []string{"artifact_verification", "release_stage"},
		},
		{
			name:       "unverified artifact → failed",
			rel:        testRelease(release.StageReady),
			registered: false,
			wantPassed: false,
			wantChecks: []string{"artifact_verification", "release_stage"},
		},
		{
			name:       "release not Ready → failed",
			rel:        testRelease(release.StageActive),
			registered: true,
			wantPassed: false,
			wantChecks: []string{"artifact_verification", "release_stage"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			component := BuildReleaseEligibilityComponent(tt.rel, func(string) bool { return tt.registered })

			if component.Component != "release_eligibility" {
				t.Errorf("component name = %q, want %q", component.Component, "release_eligibility")
			}
			if component.Passed != tt.wantPassed {
				t.Errorf("component passed = %v, want %v", component.Passed, tt.wantPassed)
			}
			if len(component.Checks) != len(tt.wantChecks) {
				t.Fatalf("component checks = %d, want %d", len(component.Checks), len(tt.wantChecks))
			}
			for i, name := range tt.wantChecks {
				if component.Checks[i].Name != name {
					t.Errorf("check %d = %q, want %q", i, component.Checks[i].Name, name)
				}
			}
		})
	}
}
