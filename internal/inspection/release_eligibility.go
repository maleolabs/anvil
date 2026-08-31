// Package inspection provides read-only diagnostic inspection capabilities
// for Anvil Runtime environments and configuration.
//
// ── Release Eligibility Checks (ST-P9-02, ST-009-002) ──────────────────
//
// Pre-activation readiness evaluates two identity-based prerequisites for
// an existing Runtime Release (identified by project and release identity):
//
//   - Artifact verification status, consumed from the Artifact contract
//     (EPIC-003): the artifact registration store only accepts artifacts
//     that passed verification ("passed" result), so registration status IS
//     the verification status. This check never re-verifies the artifact.
//   - Release stage eligibility, consumed from EPIC-004: a Release must be
//     in the Ready stage to be activated (ST-P4-06 semantics).
//
// Both checks are read-only and follow the four-domain architecture
// (ADR-015): they consume state from the owning domain and never modify it.
//
// Reference: ST-P9-02, ST-009-002, ADR-015, EPIC-003, EPIC-004
package inspection

import (
	"errors"
	"fmt"

	"maleolabs.com/anvil/internal/release"
)

// CheckArtifactVerification inspects whether the artifact referenced by a
// Release has been verified, by consuming the artifact registration status
// from the Artifact contract (EPIC-003).
//
// RegistrationStore only records artifacts that passed verification, so
// "registered" is equivalent to "verified". The isRegistered callback
// decouples this check from the artifact store (following the
// CheckArtifactRegistered pattern in internal/release/registration.go) and
// is expected to be RegistrationStore.IsRegistered.
//
// The check never re-verifies the artifact — it only consumes the recorded
// status. A nil callback is treated as "not registered" (the artifact is
// then reported as unverified).
//
// Reference: ST-P9-02, ST-009-002 AC3/AC7, EPIC-003
func CheckArtifactVerification(rel *release.Release, isRegistered func(string) bool) InspectionCheck {
	check := InspectionCheck{
		Name:   "artifact_verification",
		Passed: true,
	}

	if rel == nil {
		check.Passed = false
		check.Details = "release record is nil; cannot determine artifact verification status"
		return check
	}

	if isRegistered != nil && isRegistered(rel.ArtifactID) {
		check.Details = fmt.Sprintf("artifact %s is registered (verified)", rel.ArtifactID)
		return check
	}

	check.Passed = false
	check.Details = fmt.Sprintf(
		"the artifact %s has not been verified. Run `anvil artifact verify` before activating",
		rel.ArtifactID,
	)
	return check
}

// CheckReleaseEligibility inspects whether a Release is in the Ready stage
// and therefore eligible for activation. It consumes the Release stage
// state from EPIC-004 (via release.CheckActivationReady, ST-P4-06) and
// never modifies it.
//
// When the Release is not Ready, the check reports the current stage and
// the requirement to reach Ready.
//
// Reference: ST-P9-02, ST-009-002 AC4, EPIC-004, ST-P4-06
func CheckReleaseEligibility(rel *release.Release) InspectionCheck {
	check := InspectionCheck{
		Name:   "release_stage",
		Passed: true,
	}

	if rel == nil {
		check.Passed = false
		check.Details = "release record is nil; cannot determine release stage"
		return check
	}

	if err := release.CheckActivationReady(rel); err != nil {
		check.Passed = false
		check.Details = releaseEligibilityDetails(err, rel)
		return check
	}

	check.Details = fmt.Sprintf("release %s is in stage %s and eligible for activation", rel.ID, rel.Stage)
	return check
}

// BuildReleaseEligibilityComponent assembles the identity-based release
// eligibility checks (artifact verification + release stage) into a single
// inspection component for the pre-activation readiness assessment.
//
// The isRegistered callback is the artifact registration lookup (expected
// to be RegistrationStore.IsRegistered); see CheckArtifactVerification.
//
// Reference: ST-P9-02, ST-009-002 §4
func BuildReleaseEligibilityComponent(rel *release.Release, isRegistered func(string) bool) InspectionResult {
	result := NewInspectionResult("release_eligibility")

	for _, check := range []InspectionCheck{
		CheckArtifactVerification(rel, isRegistered),
		CheckReleaseEligibility(rel),
	} {
		result.AddCheck(check.Name, check.Passed, check.Details)
	}

	return *result
}

// releaseEligibilityDetails renders the stage eligibility failure with the
// current stage and the requirement to reach Ready, plus the next step for
// the operator.
func releaseEligibilityDetails(err error, rel *release.Release) string {
	var prereq *release.ActivationPrerequisiteError
	if errors.As(err, &prereq) {
		return fmt.Sprintf(
			"release %s is in stage %s, but activation requires stage %s. "+
				"The release must reach stage ready before it can be activated",
			prereq.ReleaseID, prereq.CurrentStage, prereq.RequiredStage,
		)
	}
	return fmt.Sprintf("release %s stage could not be validated: %v", rel.ID, err)
}
