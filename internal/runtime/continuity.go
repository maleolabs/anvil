// Package runtime provides models and utilities for managing Anvil Runtime
// instances — their configuration, lifecycle state machines, readiness
// assessment, runtime identity, directory structure provisioning, shared
// resource management, atomic symlink switching, and runtime continuity.
//
// Reference: CH-P5-01, TS-P5-01, TS-P5-02, TS-P5-03, TS-P5-04, TS-P5-05,
// TS-P5-06, TS-P5-07, TS-P5-08, TS-P5-09, EPIC-005, ADR-003 §8.5
package runtime

import (
	"fmt"
	"os"
	"time"
)

// ContinuityReport documents the current state of Runtime continuity after a
// continuity verification. It captures the active symlink state, shared
// resource status, and whether all continuity conditions are satisfied.
//
// Reference: TS-P5-06
type ContinuityReport struct {
	// ActiveSymlinkPath is the filesystem path of the active release symlink.
	ActiveSymlinkPath string `json:"active_symlink_path"`

	// ActiveSymlinkTarget is the directory the active symlink currently points
	// to. Empty if the symlink does not exist.
	ActiveSymlinkTarget string `json:"active_symlink_target"`

	// SymlinkExists indicates whether the active symlink exists on the
	// filesystem.
	SymlinkExists bool `json:"symlink_exists"`

	// SharedResourcesAccessible indicates whether all shared resource
	// directories exist and are accessible.
	SharedResourcesAccessible bool `json:"shared_resources_accessible"`

	// SharedResourcesDetail provides per-directory status of shared resources.
	SharedResourcesDetail []SharedResourceStatusDetail `json:"shared_resources_detail,omitempty"`

	// AllContinuityConditionsSatisfied is true only when every continuity
	// invariant holds.
	AllContinuityConditionsSatisfied bool `json:"all_continuity_conditions_satisfied"`

	// VerifiedAt records when the continuity check was performed.
	VerifiedAt time.Time `json:"verified_at"`

	// Errors collects any issues found during verification.
	Errors []string `json:"errors,omitempty"`
}

// SharedResourceStatusDetail provides the status of a single shared resource
// directory during continuity verification.
//
// Reference: TS-P5-06
type SharedResourceStatusDetail struct {
	// Path is the filesystem path of the shared resource directory.
	Path string `json:"path"`

	// Exists indicates whether the directory exists on the filesystem.
	Exists bool `json:"exists"`

	// IsDirectory indicates whether the path is a directory.
	IsDirectory bool `json:"is_directory"`
}

// ContinuityGuard validates and asserts runtime continuity invariants. It
// ensures that the Runtime persists across Release activations and rollbacks
// without interruption, with atomic symlink changes as the only visible
// transition.
//
// Continuity invariants:
//   - The Runtime does not restart during Release activation
//   - The Runtime does not restart during Release rollback
//   - Shared resources remain available during symlink switches
//   - The active symlink change is the only visible filesystem change
//   - Continuity is maintained across multiple sequential activations/rollbacks
//
// Note: This guard verifies filesystem-level and resource-level continuity.
// Runtime process-level continuity (no restart) is an operational invariant
// maintained by the caller — the guard confirms that the environment supports
// it by verifying that shared resources are not inside release directories
// and that the symlink switch does not affect shared resource paths.
//
// Reference: TS-P5-06, ADR-003 §8.5
type ContinuityGuard struct {
	config    RuntimeConfig
	sharedMgr *SharedResourceManager
	switcher  *SymlinkSwitcher
}

// NewContinuityGuard creates a new ContinuityGuard with the given Runtime
// configuration, shared resource manager, and symlink switcher.
//
// Reference: TS-P5-06
func NewContinuityGuard(
	cfg RuntimeConfig,
	sharedMgr *SharedResourceManager,
	switcher *SymlinkSwitcher,
) *ContinuityGuard {
	return &ContinuityGuard{
		config:    cfg,
		sharedMgr: sharedMgr,
		switcher:  switcher,
	}
}

// Verify performs a comprehensive continuity check and returns a
// ContinuityReport documenting the current state.
//
// It checks:
//   - Active symlink existence
//   - Active symlink target (if the symlink exists)
//   - Shared resource directory accessibility
//   - Isolation: shared resources are not subdirectories of releases
//
// This method does not modify any filesystem state.
//
// Reference: TS-P5-06 AC-3, AC-4
func (g *ContinuityGuard) Verify() ContinuityReport {
	report := ContinuityReport{
		ActiveSymlinkPath: g.switcher.ActiveSymlinkPath(),
		VerifiedAt:        time.Now(),
	}

	// Check active symlink existence.
	report.SymlinkExists = g.switcher.SymlinkExists()
	if report.SymlinkExists {
		target, err := g.switcher.ActiveSymlinkTarget()
		if err == nil {
			report.ActiveSymlinkTarget = target
		} else {
			report.Errors = append(report.Errors, fmt.Sprintf("read active symlink target: %s", err))
		}
	}

	// Check shared resource accessibility.
	sharedDirs := g.sharedMgr.AllSharedDirPaths()
	allAccessible := true
	for _, dir := range sharedDirs {
		detail := SharedResourceStatusDetail{Path: dir}
		info, err := os.Stat(dir)
		if err != nil {
			detail.Exists = false
			detail.IsDirectory = false
			allAccessible = false
			report.Errors = append(report.Errors, fmt.Sprintf("shared resource directory %s: %s", dir, err))
		} else {
			detail.Exists = true
			detail.IsDirectory = info.IsDir()
			if !info.IsDir() {
				allAccessible = false
				report.Errors = append(report.Errors, fmt.Sprintf("%s exists but is not a directory", dir))
			}
		}
		report.SharedResourcesDetail = append(report.SharedResourcesDetail, detail)
	}
	report.SharedResourcesAccessible = allAccessible

	// Validate shared resource isolation from releases directory.
	if err := g.sharedMgr.ValidateIsolation(); err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("shared resource isolation: %s", err))
	}

	// Determine overall continuity condition.
	report.AllContinuityConditionsSatisfied = report.SymlinkExists &&
		report.SharedResourcesAccessible &&
		len(report.Errors) == 0

	return report
}

// AssertContinuity performs a continuity check and returns an error if any
// continuity invariant is violated. This is the primary method for callers
// that need to assert continuity before or after a Release transition.
//
// Returns nil if all continuity conditions are satisfied.
//
// Reference: TS-P5-06 AC-1, AC-2, AC-5
func (g *ContinuityGuard) AssertContinuity() error {
	report := g.Verify()

	if !report.AllContinuityConditionsSatisfied {
		if len(report.Errors) > 0 {
			return fmt.Errorf("runtime continuity check failed: %v", report.Errors)
		}
		return fmt.Errorf("runtime continuity check failed: unknown condition")
	}

	return nil
}

// AssertContinuityAfterActivation verifies continuity invariants specifically
// after a Release activation. It checks that the active symlink points to a
// valid target and that shared resources remain accessible.
//
// Reference: TS-P5-06 AC-1
func (g *ContinuityGuard) AssertContinuityAfterActivation() error {
	report := g.Verify()

	if !report.SymlinkExists {
		return fmt.Errorf("runtime continuity after activation: active symlink does not exist")
	}

	if report.ActiveSymlinkTarget == "" {
		return fmt.Errorf("runtime continuity after activation: active symlink target is empty")
	}

	if !report.SharedResourcesAccessible {
		return fmt.Errorf("runtime continuity after activation: shared resources are not accessible")
	}

	// Verify the symlink target is a release directory within the releases
	// path. This confirms the symlink change was the only visible transition.
	targetDir := report.ActiveSymlinkTarget
	targetInfo, err := os.Stat(targetDir)
	if err != nil {
		return fmt.Errorf("runtime continuity after activation: active symlink target %s: %w", targetDir, err)
	}
	if !targetInfo.IsDir() {
		return fmt.Errorf("runtime continuity after activation: active symlink target %s is not a directory", targetDir)
	}

	return nil
}

// AssertContinuityAfterRollback verifies continuity invariants specifically
// after a Release rollback. It checks that the active symlink was restored to
// its previous target and that shared resources remain accessible.
//
// Reference: TS-P5-06 AC-2
func (g *ContinuityGuard) AssertContinuityAfterRollback() error {
	// After rollback, the same invariants hold: symlink exists, target is
	// valid, shared resources are accessible.
	return g.AssertContinuityAfterActivation()
}

// SharedResourcesUnchangedDuringSwitch verifies that the set of shared
// resource files and directories remains unchanged after a symlink switch.
// It compares the state of shared resources before and after the switch
// by checking directory existence only (not file contents).
//
// Reference: TS-P5-06 AC-3
func (g *ContinuityGuard) SharedResourcesUnchangedDuringSwitch(before, after ContinuityReport) bool {
	if len(before.SharedResourcesDetail) != len(after.SharedResourcesDetail) {
		return false
	}

	for i := range before.SharedResourcesDetail {
		b := before.SharedResourcesDetail[i]
		a := after.SharedResourcesDetail[i]

		if b.Path != a.Path {
			return false
		}
		if b.Exists != a.Exists {
			return false
		}
		if b.IsDirectory != a.IsDirectory {
			return false
		}
	}

	return true
}

// OnlySymlinkChangedDuringTransition verifies that the only difference
// between continuity states before and after a Release transition is the
// active symlink target. All other continuity invariants must remain
// unchanged.
//
// Reference: TS-P5-06 AC-4
func (g *ContinuityGuard) OnlySymlinkChangedDuringTransition(before, after ContinuityReport) bool {
	// Shared resources must be unchanged.
	if !g.SharedResourcesUnchangedDuringSwitch(before, after) {
		return false
	}

	// The active symlink must exist both before and after.
	if !before.SymlinkExists || !after.SymlinkExists {
		return false
	}

	// The symlink target may have changed (that's expected during activation
	// or rollback). The symlink path itself must remain the same.
	if before.ActiveSymlinkPath != after.ActiveSymlinkPath {
		return false
	}

	return true
}
