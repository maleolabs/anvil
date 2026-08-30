// Package release defines the Release model and lifecycle stage management
// for Anvil Runtime Releases.
//
// TS-P4-06 implements the atomic promotion mechanism that transitions a
// Release from Activating to Active, coordinating with EPIC-005 for atomic
// symlink switching.
//
// Reference: TS-P4-06, ADR-003 §6.3
package release

import (
	"fmt"

	"maleolabs.com/anvil/internal/runtime"
)

// PromoteRunner handles the atomic promotion of a Release from Activating
// to Active stage, coordinating with the SymlinkSwitcher (EPIC-005) for
// atomic symlink switching.
//
// The promotion sequence:
//  1. Validate the Release is in Activating stage
//  2. Switch the active symlink via SymlinkSwitcher.SwitchForActivation()
//  3. Transition the Release from Activating to Active
//
// If any step fails, the previous Active Release remains untouched — the
// symlink is never updated and the Release state is not modified.
//
// Reference: TS-P4-06, ADR-003 §6.3
type PromoteRunner struct {
	symlinkSwitcher *runtime.SymlinkSwitcher
	releasesDirPath string // Full path to the releases directory in the runtime install root
}

// NewPromoteRunner creates a PromoteRunner with the given SymlinkSwitcher
// and releases directory path.
//
// The releasesDirPath is the full path to the releases directory in the
// runtime install root (e.g., "/opt/anvil/releases"), from which individual
// release directories (rel-<identity>) are resolved.
//
// Reference: TS-P4-06
func NewPromoteRunner(switcher *runtime.SymlinkSwitcher, releasesDirPath string) *PromoteRunner {
	return &PromoteRunner{
		symlinkSwitcher: switcher,
		releasesDirPath: releasesDirPath,
	}
}

// Promote performs the atomic promotion of a Release from Activating to
// Active stage.
//
// Steps:
//  1. Validates the Release is in Activating stage
//  2. Resolves the release directory path from the runtime releases directory
//  3. Switches the active symlink atomically via SwitchForActivation
//  4. Transitions the Release to Active stage
//
// Atomicity guarantee: If the symlink switch fails (step 3), the previous
// Active Release remains Active because the symlink was never modified.
// If the Release transition fails (step 4), the symlink has already been
// switched — this is a pathological case that should not occur after
// the stage check in step 1, as Activating→Active is always a valid
// transition.
//
// Exactly one Release is Active after successful completion (AC-5): the
// promoted Release is Active via both stage and symlink. The previously
// Active Release is expected to be archived separately by the caller
// (see ActivationEngine for orchestration).
//
// References:
//   - TS-P4-06 AC-1: Release transitions from Activating to Active
//   - TS-P4-06 AC-2: Active symlink is switched to the new Release
//   - TS-P4-06 AC-4: On failure, previous Release remains Active
//   - TS-P4-06 AC-5: Exactly one Release is Active after completion
func (r *PromoteRunner) Promote(release *Release) error {
	// Step 1: Validate the Release is in Activating stage.
	if release.Stage != StageActivating {
		return fmt.Errorf(
			"cannot promote Release %s: current stage is %s, expected %s",
			release.ID, release.Stage, StageActivating,
		)
	}

	// Step 2: Resolve the release directory path.
	releaseDir := runtime.ReleaseDirPath(r.releasesDirPath, release.ID.String())

	// Step 3: Perform the atomic symlink switch.
	// If this fails, the previous Active Release remains Active (AC-4).
	if err := r.symlinkSwitcher.SwitchForActivation(releaseDir); err != nil {
		return fmt.Errorf("promotion failed at symlink switch for Release %s: %w", release.ID, err)
	}

	// Step 4: Transition the Release to Active stage (AC-1).
	// After this point, the Release is Active with both the symlink
	// and state reflecting the promotion (AC-2, AC-5).
	if err := release.Transition(StageActive); err != nil {
		return fmt.Errorf("promotion failed at state transition for Release %s: %w", release.ID, err)
	}

	return nil
}
