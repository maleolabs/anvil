// Package release defines the Release model and lifecycle stage management
// for Anvil Runtime Releases.
//
// ST-P4-06 implements activation prerequisite enforcement — a Release in
// Ready stage can be activated, and a Release not in Ready stage is rejected
// with a clear error before any activation phase executes.
//
// Reference: ST-P4-06, ADR-003 §6, ADR-003 §9.3
package release

import (
	"fmt"
)

// ActivationPrerequisiteError is returned when activation prerequisites
// are not satisfied. It includes the current Release stage and the
// required stage.
//
// Reference: ST-P4-06
type ActivationPrerequisiteError struct {
	ReleaseID     ReleaseID
	CurrentStage  Stage
	RequiredStage Stage
}

// Error returns a human-readable error message describing the prerequisite
// failure, including the current stage and the required stage.
//
// Reference: ST-P4-06 AC
func (e *ActivationPrerequisiteError) Error() string {
	return fmt.Sprintf(
		"Release %s cannot be activated: current stage is %s, must be %s",
		e.ReleaseID, e.CurrentStage, e.RequiredStage,
	)
}

// RollbackPrerequisiteError is returned when rollback eligibility
// requirements are not satisfied. It includes the current Release stage
// and the required stage.
//
// Reference: ST-P4-08
type RollbackPrerequisiteError struct {
	ReleaseID     ReleaseID
	CurrentStage  Stage
	RequiredStage Stage
}

// Error returns a human-readable error message describing the prerequisite
// failure, including the current stage and the required stage.
//
// Reference: ST-P4-08 AC
func (e *RollbackPrerequisiteError) Error() string {
	return fmt.Sprintf(
		"Release %s cannot be rolled back: current stage is %s, must be %s",
		e.ReleaseID, e.CurrentStage, e.RequiredStage,
	)
}

// CheckRollbackEligible verifies that a Release is in the Active stage and
// eligible for rollback. Returns a RollbackPrerequisiteError if the Release
// is not in Active stage.
//
// This check must be called before any rollback phase executes. It prevents
// wasted work by rejecting invalid rollback attempts early, before any phase
// sequence begins.
//
// Business rules:
//   - Rollback requires the Release to be in Active stage
//   - Releases in any stage other than Active must be rejected before any
//     rollback phase executes
//   - The rejection message must include the current stage and the
//     requirement for Active stage
//   - No rollback phase may execute if the eligibility check fails
//
// Reference: ST-P4-08 AC-1 through AC-5
func CheckRollbackEligible(release *Release) error {
	if release.Stage != StageActive {
		return &RollbackPrerequisiteError{
			ReleaseID:     release.ID,
			CurrentStage:  release.Stage,
			RequiredStage: StageActive,
		}
	}
	return nil
}

// CheckActivationReady verifies that a Release is in the Ready stage and
// eligible for activation. Returns an ActivationPrerequisiteError if the
// Release is not in Ready stage.
//
// This check must be called before any activation phase executes. It
// prevents wasted work by rejecting invalid activation attempts early,
// before any phase sequence begins.
//
// Business rules:
//   - Activation requires the Release to be in Ready stage
//   - Releases in any stage other than Ready must be rejected before any
//     activation phase executes
//   - The rejection message must include the current stage and the
//     requirement for Ready stage
//   - No activation phase may execute if the prerequisite check fails
//
// Reference: ST-P4-06 AC-1 through AC-6
func CheckActivationReady(release *Release) error {
	if release.Stage != StageReady {
		return &ActivationPrerequisiteError{
			ReleaseID:     release.ID,
			CurrentStage:  release.Stage,
			RequiredStage: StageReady,
		}
	}
	return nil
}
