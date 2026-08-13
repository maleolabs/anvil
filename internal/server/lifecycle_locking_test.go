// Package server provides models and utilities for managing Anvil Server
// Runtime configuration and coordinates installation and activation of
// Runtime Releases using registered project metadata.
//
// This file codifies lifecycle locking within the state-only scope
// (TS-015-04-03, ADR-031 §3 keep list: locking; ADR-014 baseline safety:
// reject concurrent activation/rollback operations for the same project):
//
//  1. Concurrent lifecycle operations on one server are serialized or
//     safely rejected — a lifecycle operation holding the cross-process
//     operation lock rejects every concurrent install/activate/rollback
//     with a descriptive error; the coordinator never races state
//     mutations (cross-process atomicity of the runtime-state
//     read-modify-write).
//  2. Locking never gates on diagnostics (ADR-036 §3) — lock acquisition
//     is a pure state-file concern; runtime condition fields neither gate
//     nor are clobbered by it.
//  3. Lock state is part of lifecycle state handling (ADR-014: Runtime
//     State contains locks) — the persisted runtime-state.json records
//     the in-flight operation and clears it on completion.
//
// Reference: TS-015-04-03, ADR-031, ADR-014, ADR-036
package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"maleolabs.com/anvil/internal/release"
	"maleolabs.com/anvil/internal/runtime"
)

// TestConcurrentLifecycleOperations_SafelyRejectedWhileHeld verifies the
// acceptance criterion "concurrent lifecycle operations on one server are
// serialized or safely rejected": while another process holds the
// operation lock (as an in-flight lifecycle operation would), every
// coordinator lifecycle operation fails fast with the descriptive
// in-progress rejection — Install, Activate, and Rollback — and the same
// operations succeed once the holder releases.
func TestConcurrentLifecycleOperations_SafelyRejectedWhileHeld(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, installRoot := setupServerEnv(t, serverRoot)

	// Pre-flight: a release exists so Activate and Rollback have targets.
	coordinator := NewServerReleaseCoordinator(serverRoot)
	relA, err := coordinator.Install(projectID, createTestArtifact(t, projectID))
	if err != nil {
		t.Fatalf("pre-flight Install: %v", err)
	}
	if err := coordinator.Activate(projectID, relA.ID.String()); err != nil {
		t.Fatalf("pre-flight Activate: %v", err)
	}

	// An in-flight lifecycle operation from another process holds the
	// cross-process operation lock (runtime.NewOperationLock is the exact
	// primitive the coordinator acquires).
	holder := runtime.NewOperationLock(installRoot)
	if err := holder.Acquire("rollback"); err != nil {
		t.Fatalf("holder Acquire: %v", err)
	}

	// Every concurrent lifecycle operation is safely rejected, and the
	// rejection names the in-flight operation (lock state read from the
	// persisted runtime state).
	artifact := createTestArtifactVariant(t, projectID, "1.1.0", "<?php // rejected\n")
	if _, err := coordinator.Install(projectID, artifact); !assertLockRejection(t, err, "rollback") {
		t.Fatalf("concurrent Install: %v", err)
	}
	if err := coordinator.Activate(projectID, relA.ID.String()); !assertLockRejection(t, err, "rollback") {
		t.Fatalf("concurrent Activate: %v", err)
	}
	if _, err := coordinator.Rollback(projectID); !assertLockRejection(t, err, "rollback") {
		t.Fatalf("concurrent Rollback: %v", err)
	}

	// Nothing was mutated by the rejected operations: the state file still
	// records relA as active, and the rejected install created no release.
	assertRuntimeActiveReleaseID(t, installRoot, relA.ID.String())
	assertStatusSnapshot(t, installRoot, map[release.ReleaseID]release.Stage{
		relA.ID: release.StageActive,
	})

	// After the in-flight operation completes, the same operations succeed.
	if err := holder.Release(); err != nil {
		t.Fatalf("holder Release: %v", err)
	}
	relB, err := coordinator.Install(projectID, createTestArtifactVariant(t, projectID, "1.1.0", "<?php // after release\n"))
	if err != nil {
		t.Fatalf("Install after lock release: %v", err)
	}
	if err := coordinator.Activate(projectID, relB.ID.String()); err != nil {
		t.Fatalf("Activate after lock release: %v", err)
	}
	if _, err := coordinator.Rollback(projectID); err != nil {
		t.Fatalf("Rollback after lock release: %v", err)
	}
}

// TestConcurrentLifecycleOperations_StressNoCorruption races several
// install+activate operations on one server and verifies the cross-process
// atomicity of the runtime-state read-modify-write: every operation either
// completes or is safely rejected, and the persisted state stays readable
// and consistent — no interleaved state mutations, no torn state files.
func TestConcurrentLifecycleOperations_StressNoCorruption(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, installRoot := setupServerEnv(t, serverRoot)

	// A pre-existing active release gives Activate a stable archive target.
	relA, err := NewServerReleaseCoordinator(serverRoot).Install(projectID, createTestArtifact(t, projectID))
	if err != nil {
		t.Fatalf("seed Install: %v", err)
	}
	if err := NewServerReleaseCoordinator(serverRoot).Activate(projectID, relA.ID.String()); err != nil {
		t.Fatalf("seed Activate: %v", err)
	}

	// Artifacts are prepared before the race (test helpers cannot Fatal
	// from spawned goroutines).
	const contenders = 6
	artifacts := make([]string, contenders)
	for i := range artifacts {
		artifacts[i] = createTestArtifactVariant(t, projectID, fmt.Sprintf("1.%d.0", i), fmt.Sprintf("<?php // concurrent %d\n", i))
	}

	start := make(chan struct{})
	errs := make([]error, contenders)
	var wg sync.WaitGroup
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			coordinator := NewServerReleaseCoordinator(serverRoot) // per-command model
			rel, err := coordinator.Install(projectID, artifacts[i])
			if err != nil {
				errs[i] = err
				return
			}
			errs[i] = coordinator.Activate(projectID, rel.ID.String())
		}(i)
	}
	close(start)
	wg.Wait()

	// Every outcome is either success or the safe lock rejection.
	successes := 0
	for i, err := range errs {
		if err == nil {
			successes++
			continue
		}
		if !strings.Contains(err.Error(), "another lifecycle operation is in progress") {
			t.Errorf("contender %d failed with unexpected error: %v", i, err)
		}
	}

	// The persisted runtime state must be intact: it loads, records a
	// release as active, and carries no leftover lock record.
	statePath := filepath.Join(installRoot, "runtime-state.json")
	store := runtime.NewStateStore(statePath)
	if err := store.Load(); err != nil {
		t.Fatalf("runtime state corrupted by concurrent operations: %v", err)
	}
	if store.State().OperationLock != nil {
		t.Errorf("leftover operation lock record after all operations completed: %+v", store.State().OperationLock)
	}
	assertActiveRelease(t, installRoot, store.State().ActiveReleaseID)

	// Every successful install produced exactly one release with a valid,
	// readable stage (Ready, or Active/Archived once activated); rejected
	// installs created nothing.
	releases, err := release.ListReleases(installRoot)
	if err != nil {
		t.Fatalf("list releases: %v", err)
	}
	if len(releases) != successes+1 { // +1 for the seeded release
		t.Errorf("releases on disk = %d, want %d (1 seed + %d successful installs)", len(releases), successes+1, successes)
	}
	for _, rel := range releases {
		switch rel.Stage {
		case release.StageReady, release.StageActive, release.StageArchived:
		default:
			t.Errorf("release %s in unexpected stage %s after concurrent operations", rel.ID, rel.Stage)
		}
	}
}

// TestLockStatePartOfLifecycleStateHandling verifies the acceptance
// criterion "lock state is part of lifecycle state handling" (ADR-014:
// Runtime State contains locks): the persisted runtime state observes the
// in-flight operation while the lock is held and is cleared once the
// operation completes, and the lock file itself persists across operations
// (it is never unlinked — see runtime.LockFileName).
func TestLockStatePartOfLifecycleStateHandling(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, installRoot := setupServerEnv(t, serverRoot)

	// Run a full lifecycle through the coordinator.
	_, _ = runStateOnlyLifecycleOnEnv(t, serverRoot, projectID)

	// After every operation completed, no lock record remains in the
	// persisted lifecycle state.
	statePath := filepath.Join(installRoot, "runtime-state.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read runtime state: %v", err)
	}
	if strings.Contains(string(data), "operation_lock") {
		t.Errorf("runtime state carries an operation lock record after the lifecycle completed:\n%s", data)
	}

	// While an operation is in flight, the record is observable and names
	// the holder operation and process.
	holder := runtime.NewOperationLock(installRoot)
	if err := holder.Acquire("rollback"); err != nil {
		t.Fatalf("holder Acquire: %v", err)
	}
	store := runtime.NewStateStore(statePath)
	if err := store.Load(); err != nil {
		t.Fatalf("load state while held: %v", err)
	}
	rec := store.State().OperationLock
	if rec == nil {
		t.Fatal("lock record missing from runtime state while an operation is in flight")
	}
	if rec.Operation != "rollback" || rec.PID == 0 || rec.AcquiredAt.IsZero() {
		t.Errorf("lock record incomplete: %+v", rec)
	}
	if err := holder.Release(); err != nil {
		t.Fatalf("holder Release: %v", err)
	}

	// The operation lock file persists (never unlinked), and the state file
	// it guards still loads.
	lockPath := filepath.Join(installRoot, runtime.LockFileName)
	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("operation lock file must persist across operations: %v", err)
	}
	store = runtime.NewStateStore(statePath)
	if err := store.Load(); err != nil {
		t.Fatalf("state file unreadable after lifecycle: %v", err)
	}
	if rec := store.State().OperationLock; rec != nil {
		t.Errorf("lock record must be cleared after release, got %+v", rec)
	}
}

// TestLockingNeverGatesOnDiagnostics verifies the acceptance criterion
// "locking never gates on diagnostics" (ADR-036 §3: lifecycle operations
// never depend on diagnostics results). The closest lifecycle-state
// analogue of diagnostics is the runtime condition record: a condition
// that reports the runtime offline neither blocks lock acquisition nor is
// clobbered by lock record handling — and contention rejection behaves
// identically regardless of the condition.
func TestLockingNeverGatesOnDiagnostics(t *testing.T) {
	serverRoot := t.TempDir()
	projectID, installRoot := setupServerEnv(t, serverRoot)

	// A diagnostics-style observation records the runtime as offline
	// before any lifecycle operation runs. The install root exists (the
	// runtime state lives there); the directory is created here the same
	// way Install's directory setup would.
	statePath := filepath.Join(installRoot, "runtime-state.json")
	if err := os.MkdirAll(installRoot, 0755); err != nil {
		t.Fatalf("create install root: %v", err)
	}
	store := runtime.NewStateStore(statePath)
	store.SetRuntimeCondition(runtime.ConditionOffline)
	if err := store.Save(); err != nil {
		t.Fatalf("save offline condition: %v", err)
	}

	// Lifecycle operations proceed without consulting the condition.
	coordinator := NewServerReleaseCoordinator(serverRoot)
	relA, err := coordinator.Install(projectID, createTestArtifact(t, projectID))
	if err != nil {
		t.Fatalf("Install must not be gated by runtime condition: %v", err)
	}
	if err := coordinator.Activate(projectID, relA.ID.String()); err != nil {
		t.Fatalf("Activate must not be gated by runtime condition: %v", err)
	}

	// Contention rejection is equally independent of diagnostics: the
	// offline condition does not affect the lock's behavior.
	holder := runtime.NewOperationLock(installRoot)
	if err := holder.Acquire("activate"); err != nil {
		t.Fatalf("holder Acquire: %v", err)
	}
	if _, err := coordinator.Rollback(projectID); !assertLockRejection(t, err, "activate") {
		t.Fatalf("Rollback under contention: %v", err)
	}
	if err := holder.Release(); err != nil {
		t.Fatalf("holder Release: %v", err)
	}

	// The condition survives the whole lifecycle (state is load-preserved;
	// lock state handling never clobbers unrelated operational state).
	store = runtime.NewStateStore(statePath)
	if err := store.Load(); err != nil {
		t.Fatalf("load state: %v", err)
	}
	if got := store.State().RuntimeCondition; got != runtime.ConditionOffline {
		t.Errorf("runtime condition after lifecycle = %s, want %s (state must be preserved)", got, runtime.ConditionOffline)
	}
	if rec := store.State().OperationLock; rec != nil {
		t.Errorf("lock record must be cleared, got %+v", rec)
	}
}

// assertLockRejection asserts err is the descriptive safe-rejection of a
// concurrent lifecycle operation naming the in-flight operation.
func assertLockRejection(t *testing.T, err error, inFlightOperation string) bool {
	t.Helper()
	if err == nil {
		t.Error("operation must be rejected while another lifecycle operation holds the lock")
		return false
	}
	for _, want := range []string{"another lifecycle operation is in progress", inFlightOperation} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("rejection error must mention %q, got: %v", want, err)
			return false
		}
	}
	return true
}
