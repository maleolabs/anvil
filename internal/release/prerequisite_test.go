package release

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Activation Prerequisite Enforcement Tests — ST-P4-06
// ---------------------------------------------------------------------------

// TestCheckActivationReady_ReadyStage verifies that a Release in Ready
// stage passes the prerequisite check.
//
// AC-1: Activating a Release in Ready stage proceeds to phase execution.
//
// Reference: ST-P4-06 AC-1
func TestCheckActivationReady_ReadyStage(t *testing.T) {
	rel := &Release{
		ID:          ReleaseID("test-ready-check"),
		Stage:       StageReady,
		Transitions: []TransitionRecord{},
	}

	err := CheckActivationReady(rel)
	if err != nil {
		t.Fatalf("CheckActivationReady returned unexpected error: %v", err)
	}
}

// TestCheckActivationReady_NotReady verifies that a Release not in Ready
// stage is rejected before any activation phase executes.
//
// AC-2: Activating a Release not in Ready is rejected before any phase
// executes.
//
// Reference: ST-P4-06 AC-2
func TestCheckActivationReady_NotReady(t *testing.T) {
	tests := []struct {
		name  string
		stage Stage
	}{
		{"Activating", StageActivating},
		{"Active", StageActive},
		{"RollingBack", StageRollingBack},
		{"RolledBack", StageRolledBack},
		{"Archived", StageArchived},
		{"Removed", StageRemoved},
		{"Failed", StageFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rel := &Release{
				ID:          ReleaseID("test-not-ready-" + strings.ToLower(tt.name)),
				Stage:       tt.stage,
				Transitions: []TransitionRecord{},
			}

			err := CheckActivationReady(rel)
			if err == nil {
				t.Fatal("CheckActivationReady should return error for non-Ready stage")
			}

			// Verify the error is an ActivationPrerequisiteError.
			prereqErr, ok := err.(*ActivationPrerequisiteError)
			if !ok {
				t.Fatalf("expected *ActivationPrerequisiteError, got %T", err)
			}

			if prereqErr.ReleaseID != rel.ID {
				t.Errorf("error ReleaseID = %s, want %s", prereqErr.ReleaseID, rel.ID)
			}
			if prereqErr.CurrentStage != tt.stage {
				t.Errorf("error CurrentStage = %s, want %s", prereqErr.CurrentStage, tt.stage)
			}
			if prereqErr.RequiredStage != StageReady {
				t.Errorf("error RequiredStage = %s, want %s", prereqErr.RequiredStage, StageReady)
			}
		})
	}
}

// TestCheckActivationReady_AlreadyActive verifies that activating a Release
// already in Active stage is rejected before any phase executes.
//
// AC-3: Activating a Release already in Active stage is rejected before
// any phase executes.
//
// Reference: ST-P4-06 AC-3
func TestCheckActivationReady_AlreadyActive(t *testing.T) {
	rel := &Release{
		ID:          ReleaseID("test-already-active"),
		Stage:       StageActive,
		Transitions: []TransitionRecord{},
	}

	err := CheckActivationReady(rel)
	if err == nil {
		t.Fatal("CheckActivationReady should return error for Active stage")
	}

	if !strings.Contains(err.Error(), "already active") && !strings.Contains(err.Error(), "active") {
		// The error must mention the current stage.
		if !strings.Contains(err.Error(), "active") {
			t.Errorf("error should mention 'active' stage, got: %v", err)
		}
	}
}

// TestCheckActivationReady_RolledBack verifies that activating a Release
// in Rolled Back stage is rejected before any phase executes.
//
// AC-4: Activating a Release in Rolled Back stage is rejected before any
// phase executes.
//
// Reference: ST-P4-06 AC-4
func TestCheckActivationReady_RolledBack(t *testing.T) {
	rel := &Release{
		ID:          ReleaseID("test-rolledback"),
		Stage:       StageRolledBack,
		Transitions: []TransitionRecord{},
	}

	err := CheckActivationReady(rel)
	if err == nil {
		t.Fatal("CheckActivationReady should return error for RolledBack stage")
	}

	if !strings.Contains(err.Error(), "rolled back") {
		t.Errorf("error should mention 'rolled back' stage, got: %v", err)
	}
}

// TestCheckActivationReady_ErrorMessage verifies that the rejection message
// identifies the current stage and the requirement for Ready stage.
//
// AC-5: The rejection message identifies the current stage and the
// requirement for Ready stage.
//
// Reference: ST-P4-06 AC-5
func TestCheckActivationReady_ErrorMessage(t *testing.T) {
	rel := &Release{
		ID:          ReleaseID("test-error-msg"),
		Stage:       StageArchived,
		Transitions: []TransitionRecord{},
	}

	err := CheckActivationReady(rel)
	if err == nil {
		t.Fatal("CheckActivationReady should return error")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "cannot be activated") {
		t.Errorf("error should indicate 'cannot be activated', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "archived") {
		t.Errorf("error should mention current stage 'archived', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "ready") {
		t.Errorf("error should mention required stage 'ready', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, string(rel.ID)) {
		t.Errorf("error should mention Release ID %s, got: %s", rel.ID, errMsg)
	}
}

// TestCheckActivationReady_NoPhaseExecution verifies that when the
// prerequisite check fails, no activation phase can execute — the Release
// stage and state remain unchanged.
//
// AC-6: No activation phase executes when the Release is not in Ready stage.
//
// Reference: ST-P4-06 AC-6
func TestCheckActivationReady_NoPhaseExecution(t *testing.T) {
	rel := &Release{
		ID:          ReleaseID("test-no-phase"),
		Stage:       StageArchived,
		Transitions: []TransitionRecord{},
	}

	// Capture state before check.
	beforeStage := rel.Stage
	beforeTransitions := len(rel.Transitions)

	// The check must fail.
	err := CheckActivationReady(rel)
	if err == nil {
		t.Fatal("CheckActivationReady should return error")
	}

	// Verify Release state is unchanged (no phase executed).
	if rel.Stage != beforeStage {
		t.Errorf("Release Stage changed from %s to %s after failed check", beforeStage, rel.Stage)
	}

	if len(rel.Transitions) != beforeTransitions {
		t.Errorf("Release Transitions changed after failed check: %d -> %d", beforeTransitions, len(rel.Transitions))
	}

	// Verify the ActivationEngine also rejects this.
	// This test ensures the engine-level enforcement also prevents phase execution.
	_, _, _, _, engine := setupActivationTest(t)
	err = engine.Activate(rel)
	if err == nil {
		t.Fatal("ActivationEngine.Activate should return error for non-Ready stage")
	}

	// Verify stage still unchanged after engine attempt.
	if rel.Stage != beforeStage {
		t.Errorf("Release Stage changed after failed ActivationEngine.Activate: %s -> %s", beforeStage, rel.Stage)
	}
}

// ---------------------------------------------------------------------------
// Rollback Eligibility Enforcement Tests — ST-P4-08
// ---------------------------------------------------------------------------

// TestCheckRollbackEligible_ActiveStage verifies that a Release in Active
// stage passes the rollback eligibility check.
//
// AC-1: Rolling back a Release in Active stage proceeds to phase execution.
//
// Reference: ST-P4-08 AC-1
func TestCheckRollbackEligible_ActiveStage(t *testing.T) {
	rel := &Release{
		ID:          ReleaseID("test-active-check"),
		Stage:       StageActive,
		Transitions: []TransitionRecord{},
	}

	err := CheckRollbackEligible(rel)
	if err != nil {
		t.Fatalf("CheckRollbackEligible returned unexpected error: %v", err)
	}
}

// TestCheckRollbackEligible_NotActive verifies that a Release not in Active
// stage is rejected before any rollback phase executes.
//
// AC-2: Rolling back a Release not in Active is rejected before any phase
// executes.
//
// Reference: ST-P4-08 AC-2
func TestCheckRollbackEligible_NotActive(t *testing.T) {
	tests := []struct {
		name  string
		stage Stage
	}{
		{"Ready", StageReady},
		{"Activating", StageActivating},
		{"RollingBack", StageRollingBack},
		{"RolledBack", StageRolledBack},
		{"Archived", StageArchived},
		{"Removed", StageRemoved},
		{"Failed", StageFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rel := &Release{
				ID:          ReleaseID("test-not-active-" + strings.ToLower(tt.name)),
				Stage:       tt.stage,
				Transitions: []TransitionRecord{},
			}

			err := CheckRollbackEligible(rel)
			if err == nil {
				t.Fatal("CheckRollbackEligible should return error for non-Active stage")
			}

			// Verify the error is a RollbackPrerequisiteError.
			prereqErr, ok := err.(*RollbackPrerequisiteError)
			if !ok {
				t.Fatalf("expected *RollbackPrerequisiteError, got %T", err)
			}

			if prereqErr.ReleaseID != rel.ID {
				t.Errorf("error ReleaseID = %s, want %s", prereqErr.ReleaseID, rel.ID)
			}
			if prereqErr.CurrentStage != tt.stage {
				t.Errorf("error CurrentStage = %s, want %s", prereqErr.CurrentStage, tt.stage)
			}
			if prereqErr.RequiredStage != StageActive {
				t.Errorf("error RequiredStage = %s, want %s", prereqErr.RequiredStage, StageActive)
			}
		})
	}
}

// TestCheckRollbackEligible_ReadyStage verifies that rolling back a Release
// in Ready stage is rejected before any phase executes.
//
// AC-3: Rolling back a Release in Ready stage is rejected before any phase
// executes.
//
// Reference: ST-P4-08 AC-3
func TestCheckRollbackEligible_ReadyStage(t *testing.T) {
	rel := &Release{
		ID:          ReleaseID("test-ready-rollback"),
		Stage:       StageReady,
		Transitions: []TransitionRecord{},
	}

	err := CheckRollbackEligible(rel)
	if err == nil {
		t.Fatal("CheckRollbackEligible should return error for Ready stage")
	}

	if !strings.Contains(err.Error(), "ready") {
		t.Errorf("error should mention 'ready' stage, got: %v", err)
	}
}

// TestCheckRollbackEligible_RolledBackStage verifies that rolling back a
// Release already in RolledBack stage is rejected before any phase executes.
//
// AC-4: Rolling back a Release in RolledBack stage is rejected before any
// phase executes.
//
// Reference: ST-P4-08 AC-4
func TestCheckRollbackEligible_RolledBackStage(t *testing.T) {
	rel := &Release{
		ID:          ReleaseID("test-rolledback-rollback"),
		Stage:       StageRolledBack,
		Transitions: []TransitionRecord{},
	}

	err := CheckRollbackEligible(rel)
	if err == nil {
		t.Fatal("CheckRollbackEligible should return error for RolledBack stage")
	}

	if !strings.Contains(err.Error(), "rolled back") {
		t.Errorf("error should mention 'rolled back' stage, got: %v", err)
	}
}

// TestCheckRollbackEligible_FailedStage verifies that rolling back a Release
// in Failed stage is rejected before any phase executes.
//
// Reference: ST-P4-08 AC-5
func TestCheckRollbackEligible_FailedStage(t *testing.T) {
	rel := &Release{
		ID:          ReleaseID("test-failed-rollback"),
		Stage:       StageFailed,
		Transitions: []TransitionRecord{},
	}

	err := CheckRollbackEligible(rel)
	if err == nil {
		t.Fatal("CheckRollbackEligible should return error for Failed stage")
	}

	if !strings.Contains(err.Error(), "failed") {
		t.Errorf("error should mention 'failed' stage, got: %v", err)
	}
}

// TestCheckRollbackEligible_ErrorMessage verifies that the rejection message
// identifies the current stage and the requirement for Active stage.
//
// AC-6: The rejection message identifies the current stage and the
// requirement for Active stage.
//
// Reference: ST-P4-08 AC-6
func TestCheckRollbackEligible_ErrorMessage(t *testing.T) {
	rel := &Release{
		ID:          ReleaseID("test-rollback-error-msg"),
		Stage:       StageArchived,
		Transitions: []TransitionRecord{},
	}

	err := CheckRollbackEligible(rel)
	if err == nil {
		t.Fatal("CheckRollbackEligible should return error")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "cannot be rolled back") {
		t.Errorf("error should indicate 'cannot be rolled back', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "archived") {
		t.Errorf("error should mention current stage 'archived', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "active") {
		t.Errorf("error should mention required stage 'active', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, string(rel.ID)) {
		t.Errorf("error should mention Release ID %s, got: %s", rel.ID, errMsg)
	}
}

// TestCheckRollbackEligible_NoPhaseExecution verifies that when the
// prerequisite check fails, no rollback phase can execute — the Release
// stage and state remain unchanged.
//
// AC-7: No rollback phase executes when the Release is not in Active stage.
//
// Reference: ST-P4-08 AC-7
func TestCheckRollbackEligible_NoPhaseExecution(t *testing.T) {
	rel := &Release{
		ID:          ReleaseID("test-no-rollback-phase"),
		Stage:       StageArchived,
		Transitions: []TransitionRecord{},
	}

	// Capture state before check.
	beforeStage := rel.Stage
	beforeTransitions := len(rel.Transitions)

	// The check must fail.
	err := CheckRollbackEligible(rel)
	if err == nil {
		t.Fatal("CheckRollbackEligible should return error")
	}

	// Verify Release state is unchanged (no phase executed).
	if rel.Stage != beforeStage {
		t.Errorf("Release Stage changed from %s to %s after failed check", beforeStage, rel.Stage)
	}

	if len(rel.Transitions) != beforeTransitions {
		t.Errorf("Release Transitions changed after failed check: %d -> %d", beforeTransitions, len(rel.Transitions))
	}
}

// TestCheckRollbackEligible_PrerequisiteErrorType verifies that the error
// returned by CheckRollbackEligible is typed so callers can inspect the
// current and required stages programmatically.
func TestCheckRollbackEligible_PrerequisiteErrorType(t *testing.T) {
	rel := &Release{
		ID:          ReleaseID("test-rollback-error-type"),
		Stage:       StageReady,
		Transitions: []TransitionRecord{},
	}

	err := CheckRollbackEligible(rel)

	prereqErr, ok := err.(*RollbackPrerequisiteError)
	if !ok {
		t.Fatalf("expected *RollbackPrerequisiteError, got %T (value: %v)", err, err)
	}

	if prereqErr.ReleaseID != rel.ID {
		t.Errorf("ReleaseID = %s, want %s", prereqErr.ReleaseID, rel.ID)
	}
	if prereqErr.CurrentStage != StageReady {
		t.Errorf("CurrentStage = %s, want %s", prereqErr.CurrentStage, StageReady)
	}
	if prereqErr.RequiredStage != StageActive {
		t.Errorf("RequiredStage = %s, want %s", prereqErr.RequiredStage, StageActive)
	}
}

// TestCheckActivationReady_PrerequisiteErrorType verifies that the error
// returned by CheckActivationReady is typed so callers can inspect the
// current and required stages programmatically.
func TestCheckActivationReady_PrerequisiteErrorType(t *testing.T) {
	rel := &Release{
		ID:          ReleaseID("test-error-type"),
		Stage:       StageFailed,
		Transitions: []TransitionRecord{},
	}

	err := CheckActivationReady(rel)

	prereqErr, ok := err.(*ActivationPrerequisiteError)
	if !ok {
		t.Fatalf("expected *ActivationPrerequisiteError, got %T (value: %v)", err, err)
	}

	if prereqErr.ReleaseID != rel.ID {
		t.Errorf("ReleaseID = %s, want %s", prereqErr.ReleaseID, rel.ID)
	}
	if prereqErr.CurrentStage != StageFailed {
		t.Errorf("CurrentStage = %s, want %s", prereqErr.CurrentStage, StageFailed)
	}
	if prereqErr.RequiredStage != StageReady {
		t.Errorf("RequiredStage = %s, want %s", prereqErr.RequiredStage, StageReady)
	}
}
