// Package release defines the Release model and lifecycle stage management
// for Anvil Runtime Releases.
//
// TS-P4-05 implements the activation engine that orchestrates the phase
// sequence — Prepare, Configure, Promote — coordinating with Runtime
// services (EPIC-005) and invoking TS-P4-06 for the final atomic promotion.
//
// Reference: TS-P4-05, ADR-003 §6
package release

import (
	"fmt"

	"maleolabs.com/anvil/internal/runtime"
)

// ActivationEngine orchestrates the activation phase sequence for a Release:
//
//	Phase 1 — Prepare:  Validate the Release is eligible (Ready stage)
//	Phase 2 — Configure: Set up shared resources via SharedResourceManager
//	Phase 3 — Promote:   Atomically transition to Active via PromoteRunner
//
// Before promotion, the ActiveReleaseInvariant enforces that exactly one
// Release is Active by archiving the previously Active Release (TS-P4-10).
//
// Each phase is executed in order. If any phase fails, activation is
// considered failed and the Release transitions to the Failed stage
// (best-effort). Phases after the failure point are not executed.
//
// Reference: TS-P4-05, TS-P4-10, ADR-003 §6, ADR-003 §9.1
type ActivationEngine struct {
	sharedResourceMgr *runtime.SharedResourceManager
	promoteRunner     *PromoteRunner
	activeInvariant   *ActiveReleaseInvariant
}

// NewActivationEngine creates an ActivationEngine with the given
// dependencies: a SharedResourceManager for the Configure phase and a
// PromoteRunner for the Promote phase.
//
// The activeInvariant enforces the active release invariant during
// promotion (TS-P4-10). Pass nil if the runtime path is unknown or
// invariant enforcement is not yet configured.
//
// Reference: TS-P4-05, TS-P4-10
func NewActivationEngine(
	sharedResourceMgr *runtime.SharedResourceManager,
	promoteRunner *PromoteRunner,
	activeInvariant *ActiveReleaseInvariant,
) *ActivationEngine {
	return &ActivationEngine{
		sharedResourceMgr: sharedResourceMgr,
		promoteRunner:     promoteRunner,
		activeInvariant:   activeInvariant,
	}
}

// Activate executes the full activation phase sequence for a Release.
//
// Phase sequence:
//  1. Prepare  — validates the Release is in Ready stage (ST-P4-06)
//  2. (Transition to Activating)
//  3. Configure — ensures shared resources are ready
//  4. Invariant — archive previous Active Release if any (TS-P4-10)
//  5. Promote  — atomically promotes to Active via PromoteRunner (TS-P4-06)
//
// If all phases succeed, the Release transitions to Active via the
// PromoteRunner (TS-P4-06). The Release's Stage field is updated in-place;
// the caller is responsible for persisting the Release.
//
// If any phase fails, the error is returned and the Release transitions
// to Failed (best-effort). The caller can inspect the error to determine
// which phase failed.
//
// A Release not in Ready stage cannot be activated (ST-P4-06 AC-1, TS-P4-05 AC-5).
//
// References:
//   - ST-P4-06: Activation prerequisite enforcement
//   - TS-P4-05 AC-2: Phases execute in order: Prepare → Configure → Promote
//   - TS-P4-05 AC-3: All phases succeed → Release transitions to Active
//   - TS-P4-05 AC-4: Any phase fails → activation failed, Release → Failed
//   - TS-P4-10: Active release invariant enforcement
func (e *ActivationEngine) Activate(release *Release) error {
	// ----------------------------------------------------------------
	// Phase 1: Prepare — validate Release eligibility (AC-1, AC-5)
	// ----------------------------------------------------------------
	if err := e.prepare(release); err != nil {
		return fmt.Errorf("activation failed at Prepare phase: %w", err)
	}

	// Transition to Activating stage — marks the start of activation.
	if err := release.Transition(StageActivating); err != nil {
		return fmt.Errorf("activation failed: cannot transition to Activating: %w", err)
	}

	// ----------------------------------------------------------------
	// Phase 2: Configure — ensure shared resources are ready
	// ----------------------------------------------------------------
	if err := e.configure(); err != nil {
		// Best-effort: mark the Release as Failed (AC-4).
		_ = release.Transition(StageFailed)
		return fmt.Errorf("activation failed at Configure phase: %w", err)
	}

	// ----------------------------------------------------------------
	// Phase 3: Invariant enforcement — archive previous Active (TS-P4-10)
	// Before promoting the new Release, archive the currently Active
	// Release to maintain the invariant that exactly one Release is
	// Active at any time (ADR-003 §9.1).
	// ----------------------------------------------------------------
	if e.activeInvariant != nil {
		if _, err := e.activeInvariant.ArchivePreviousActive(); err != nil {
			// Best-effort: mark the Release as Failed (AC-4).
			_ = release.Transition(StageFailed)
			return fmt.Errorf("activation failed at invariant enforcement: %w", err)
		}
	}

	// ----------------------------------------------------------------
	// Phase 4: Promote — atomic promotion to Active via TS-P4-06 (AC-3)
	// ----------------------------------------------------------------
	if err := e.promoteRunner.Promote(release); err != nil {
		// Best-effort: mark the Release as Failed (AC-4).
		_ = release.Transition(StageFailed)
		return fmt.Errorf("activation failed at Promote phase: %w", err)
	}

	return nil
}

// prepare validates that the Release is in Ready stage and is eligible
// for activation. Delegates to CheckActivationReady (ST-P4-06).
//
// Reference: TS-P4-05 AC-1, AC-5, ST-P4-06
func (e *ActivationEngine) prepare(release *Release) error {
	return CheckActivationReady(release)
}

// configure validates that all shared resource directories exist and
// that shared resources are properly isolated from release directories.
//
// Reference: TS-P4-05 AC-2 (Configure phase)
func (e *ActivationEngine) configure() error {
	if err := e.sharedResourceMgr.EnsureDirectoriesExist(); err != nil {
		return fmt.Errorf("shared resource validation: %w", err)
	}
	if err := e.sharedResourceMgr.ValidateIsolation(); err != nil {
		return fmt.Errorf("shared resource isolation: %w", err)
	}
	return nil
}
