// Package server provides models and utilities for managing Anvil Server
// Runtime configuration and coordinates installation and activation of
// Runtime Releases using registered project metadata.
//
// This file implements the lifecycle observability query — the read-only
// surface that reports what is active, what is installed, what can roll
// back, release status, and runtime state for a registered project
// (ADR-036 §3, TS-015-05-01).
//
// The query observes the same authoritative lifecycle state the Server
// Runtime enforces (ADR-031 §3): it reads through the production read paths
// — release.GetActiveRelease, release.ListReleases, release.GetRollbackEligibility,
// and runtime.StateStore.Load — and never infers state from memory or from
// filesystem symlinks. The query is read-only: it never mutates, repairs, or
// persists any state (structural enforcement is TS-015-05-03).
//
// Reference: TS-015-05-01, ADR-036 §3, ADR-031
package server

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"maleolabs.com/anvil/internal/release"
	"maleolabs.com/anvil/internal/runtime"
)

// ActiveReleaseStatus reports the currently Active Release for a project,
// observed from the authoritative release state (release.GetActiveRelease —
// the stage-based read path, TS-P4-09).
//
// Reference: TS-015-05-01
type ActiveReleaseStatus struct {
	// ReleaseID is the identity of the Active Release.
	ReleaseID string `json:"release_id"`
	// Version is the Active Release's artifact version.
	Version string `json:"version"`
	// Stage is the Active Release's lifecycle stage (always "active" when
	// reported through GetActiveRelease; carried for parity with the
	// release status surface).
	Stage string `json:"stage"`
}

// InstalledReleaseStatus reports one installed Release with its lifecycle
// stage — the "what is installed" + release status observation, read from
// release.ListReleases (TS-P4-09).
//
// Reference: TS-015-05-01
type InstalledReleaseStatus struct {
	// ReleaseID is the identity of the installed Release.
	ReleaseID string `json:"release_id"`
	// Version is the Release's artifact version.
	Version string `json:"version"`
	// Stage is the Release's current lifecycle stage.
	Stage string `json:"stage"`
}

// RollbackStatus reports whether the project can roll back and to which
// Release, observed read-only from the lifecycle state
// (release.GetRollbackEligibility).
//
// Reference: TS-015-05-01
type RollbackStatus struct {
	// Eligible is true when a rollback is possible (an Active Release is
	// present and a previously Active Release exists as target).
	Eligible bool `json:"eligible"`
	// ActiveReleaseID is the Release a rollback would reverse.
	ActiveReleaseID string `json:"active_release_id"`
	// TargetReleaseID is the Release a rollback would restore.
	TargetReleaseID string `json:"target_release_id"`
	// Reason explains why rollback is not possible when Eligible is false.
	Reason string `json:"reason"`
}

// RuntimeStateStatus reports the persisted Runtime state for the project's
// install root, read from runtime.StateStore (runtime-state.json) — the
// authoritative operational record (TS-P5-04, ADR-031 §3).
//
// Reference: TS-015-05-01
type RuntimeStateStatus struct {
	// Recorded is true when a runtime state file exists and was loaded.
	Recorded bool `json:"recorded"`
	// ActiveReleaseID is the Active Release recorded in the runtime state.
	ActiveReleaseID string `json:"active_release_id"`
	// RuntimeCondition is the recorded operational condition.
	RuntimeCondition string `json:"runtime_condition"`
	// SharedResource is the recorded shared resource status.
	SharedResource string `json:"shared_resource"`
	// LastUpdated is the recorded last-updated timestamp (zero when not
	// recorded).
	LastUpdated time.Time `json:"last_updated"`
	// LoadError describes why the runtime state could not be loaded when
	// Recorded is false but a state file exists (e.g. corrupt file). A
	// missing state file is not an error — Recorded stays false with an
	// empty LoadError.
	LoadError string `json:"load_error,omitempty"`
}

// LifecycleStatus is the complete lifecycle observability snapshot for a
// registered project: what is active, what is installed, what can roll
// back, release status, and runtime state — all observed from the
// authoritative lifecycle state.
//
// Reference: TS-015-05-01, ADR-036 §3
type LifecycleStatus struct {
	// ProjectID is the registered project the snapshot describes.
	ProjectID string `json:"project_id"`
	// Active is the currently Active Release, or nil when none is Active.
	Active *ActiveReleaseStatus `json:"active"`
	// Installed lists every installed Release with its lifecycle stage
	// (empty when no Release is installed).
	Installed []InstalledReleaseStatus `json:"installed"`
	// Rollback reports rollback eligibility.
	Rollback RollbackStatus `json:"rollback"`
	// RuntimeState reports the persisted Runtime state.
	RuntimeState RuntimeStateStatus `json:"runtime_state"`
}

// runtimeStatePath returns the canonical runtime state file path for an
// install root. The coordinator persists state here (coordinator.go) and
// the observability query reads the same file (ADR-031: state survives and
// is the single operational record).
func runtimeStatePath(installRoot string) string {
	return filepath.Join(installRoot, "runtime-state.json")
}

// QueryLifecycleStatus builds the lifecycle observability snapshot for a
// registered project. It resolves the project's install root through the
// project registry and reads every observation from the authoritative
// production read paths:
//
//   - what is active → release.GetActiveRelease (stage-based, TS-P4-09)
//   - what is installed / release status → release.ListReleases (TS-P4-09)
//   - what can roll back → release.GetRollbackEligibility (TS-015-05-01)
//   - state queries → runtime.StateStore.Load (TS-P5-04)
//
// The query is read-only: it never mutates, repairs, or persists state. It
// returns an error only when the project is not registered or the
// underlying state cannot be read.
//
// Reference: TS-015-05-01, ADR-036 §3, ADR-031 §3
func QueryLifecycleStatus(serverRoot, projectID string) (*LifecycleStatus, error) {
	// Resolve the install root through the project registry (ADR-013).
	registryStore := NewRegistryStore(serverRoot)
	reg, err := registryStore.Load(projectID)
	if err != nil {
		return nil, fmt.Errorf("load project registry: %w", err)
	}
	installRoot := reg.Project.InstallRoot

	status := &LifecycleStatus{
		ProjectID: projectID,
		Installed: []InstalledReleaseStatus{},
	}

	// What is active: the Active Release from the authoritative release
	// state (stage-based read path — never the symlink).
	active, err := release.GetActiveRelease(installRoot)
	if err != nil {
		return nil, fmt.Errorf("query active release: %w", err)
	}
	if active != nil {
		status.Active = &ActiveReleaseStatus{
			ReleaseID: active.ID.String(),
			Version:   active.Version,
			Stage:     active.Stage.String(),
		}
	}

	// What is installed + release status: every Release with its stage.
	releases, err := release.ListReleases(installRoot)
	if err != nil {
		return nil, fmt.Errorf("query installed releases: %w", err)
	}
	for _, rel := range releases {
		status.Installed = append(status.Installed, InstalledReleaseStatus{
			ReleaseID: rel.ID.String(),
			Version:   rel.Version,
			Stage:     rel.Stage.String(),
		})
	}

	// What can roll back: read-only eligibility over the same state the
	// rollback engine derives its target from.
	eligibility, err := release.GetRollbackEligibility(installRoot)
	if err != nil {
		return nil, fmt.Errorf("query rollback eligibility: %w", err)
	}
	status.Rollback = RollbackStatus{
		Eligible:        eligibility.Eligible,
		ActiveReleaseID: eligibility.ActiveReleaseID.String(),
		TargetReleaseID: eligibility.TargetReleaseID.String(),
		Reason:          eligibility.Reason,
	}

	// State queries: the persisted Runtime state. A missing state file is
	// reported as not recorded (e.g. a registered project with no lifecycle
	// activity yet); an existing but unreadable file is surfaced as a load
	// error so corrupt state is visible instead of silently ignored.
	statePath := runtimeStatePath(installRoot)
	if _, err := os.Stat(statePath); err != nil {
		if os.IsNotExist(err) {
			return status, nil
		}
		status.RuntimeState = RuntimeStateStatus{LoadError: fmt.Sprintf("stat runtime state: %v", err)}
		return status, nil
	}

	stateStore := runtime.NewStateStore(statePath)
	if err := stateStore.Load(); err != nil {
		status.RuntimeState = RuntimeStateStatus{LoadError: err.Error()}
		return status, nil
	}

	state := stateStore.State()
	status.RuntimeState = RuntimeStateStatus{
		Recorded:         true,
		ActiveReleaseID:  state.ActiveReleaseID,
		RuntimeCondition: string(state.RuntimeCondition),
		SharedResource:   string(state.SharedResourceStatus),
		LastUpdated:      state.LastUpdated,
	}

	return status, nil
}
