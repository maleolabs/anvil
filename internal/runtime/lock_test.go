// Package runtime provides models and utilities for managing Anvil Runtime
// instances — their configuration, lifecycle state machines, readiness
// assessment, runtime identity, and runtime state tracking.
//
// This file tests the cross-process operation lock (TS-015-04-03, ADR-031
// §3 keep list: locking; ADR-014 baseline safety: reject concurrent
// activation/rollback operations for the same project).
//
// Reference: TS-015-04-03, ADR-031, ADR-014
package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// TestOperationLock_AcquireReleaseRecordsLockState verifies that lock state
// is part of lifecycle state handling: Acquire persists a lock record
// (operation, pid, acquired-at) into runtime-state.json and Release clears
// it, and a fresh lock can be re-acquired after release (ADR-014: Runtime
// State contains locks).
func TestOperationLock_AcquireReleaseRecordsLockState(t *testing.T) {
	installRoot := t.TempDir()
	lock := NewOperationLock(installRoot)
	statePath := filepath.Join(installRoot, stateFileName)

	if err := lock.Acquire("install"); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	// The record is observable in the persisted lifecycle state.
	store := NewStateStore(statePath)
	if err := store.Load(); err != nil {
		t.Fatalf("load state: %v", err)
	}
	rec := store.State().OperationLock
	if rec == nil {
		t.Fatal("lock record missing from runtime state while held")
	}
	if rec.Operation != "install" {
		t.Errorf("record operation = %q, want %q", rec.Operation, "install")
	}
	if rec.PID != os.Getpid() {
		t.Errorf("record pid = %d, want %d", rec.PID, os.Getpid())
	}
	if rec.AcquiredAt.IsZero() {
		t.Error("record acquired_at is zero")
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	store = NewStateStore(statePath)
	if err := store.Load(); err != nil {
		t.Fatalf("load state after release: %v", err)
	}
	if rec := store.State().OperationLock; rec != nil {
		t.Errorf("lock record must be cleared on release, got %+v", rec)
	}

	// The lock is reusable after release.
	if err := lock.Acquire("rollback"); err != nil {
		t.Fatalf("re-acquire after release: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("final Release: %v", err)
	}
}

// TestOperationLock_ContentionSafelyRejectedWithHolderInfo verifies that a
// contended acquire is safely rejected (LOCK_NB — the v1.x operational
// contract rejects concurrent operations, ADR-014) and that the rejection
// names the in-flight operation read from the persisted lock record.
func TestOperationLock_ContentionSafelyRejectedWithHolderInfo(t *testing.T) {
	installRoot := t.TempDir()

	holder := NewOperationLock(installRoot)
	if err := holder.Acquire("activate"); err != nil {
		t.Fatalf("holder Acquire: %v", err)
	}

	contender := NewOperationLock(installRoot)
	err := contender.Acquire("install")
	if err == nil {
		t.Fatal("contended acquire must be rejected, not granted")
	}
	for _, want := range []string{"another lifecycle operation is in progress", "activate"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("rejection error must mention %q, got: %v", want, err)
		}
	}

	// After the holder releases, the contender succeeds.
	if err := holder.Release(); err != nil {
		t.Fatalf("holder Release: %v", err)
	}
	if err := contender.Acquire("install"); err != nil {
		t.Fatalf("acquire after holder release: %v", err)
	}
	if err := contender.Release(); err != nil {
		t.Fatalf("contender Release: %v", err)
	}
}

// TestOperationLock_ConcurrentAcquireExactlyOneHolds verifies that
// concurrent acquires on one server are mutually exclusive: exactly one
// contender holds the lock at a time and every other contender is safely
// rejected. Each goroutine uses its own lock instance and file descriptor,
// exercising the same kernel path concurrent processes would (flock is
// associated with the open file description, not the process).
func TestOperationLock_ConcurrentAcquireExactlyOneHolds(t *testing.T) {
	installRoot := t.TempDir()

	const contenders = 8
	start := make(chan struct{})
	var attempted atomic.Int32
	results := make([]error, contenders)
	var wg sync.WaitGroup

	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			lock := NewOperationLock(installRoot)
			<-start
			err := lock.Acquire("install")
			attempted.Add(1)
			if err == nil {
				// Hold until every contender has attempted, so the number
				// of winners is deterministic: the first acquirer must
				// still be holding when the last contender is rejected.
				for attempted.Load() != contenders {
					runtime.Gosched()
				}
				results[i] = lock.Release()
			} else {
				results[i] = err
			}
		}(i)
	}
	close(start)
	wg.Wait()

	successes := 0
	for i, err := range results {
		if err == nil {
			successes++
			continue
		}
		if !strings.Contains(err.Error(), "another lifecycle operation is in progress") {
			t.Errorf("contender %d failed with unexpected error: %v", i, err)
		}
	}
	if successes != 1 {
		t.Errorf("exactly one concurrent acquire must win, got %d successes", successes)
	}
}

// helperEnv and helperRootEnv gate the cross-process test helper: the test
// binary re-executes itself as a child process that acquires the operation
// lock and holds it until the parent kills it — simulating a lifecycle
// operation running in a separate `anvil` command process.
const (
	helperEnv     = "ANVIL_OPLOCK_HELPER"
	helperRootEnv = "ANVIL_OPLOCK_INSTALL_ROOT"
)

// TestOperationLock_CrossProcessExclusionAndCrashRelease verifies the two
// properties that make the flock the correct cross-process primitive for a
// per-command runtime:
//
//  1. Exclusion across processes: a real child process holding the lock
//     rejects the parent's acquire.
//  2. Crash auto-release: when the holder dies without releasing (SIGKILL —
//     no defer runs, no cleanup), the kernel drops the flock and the next
//     acquire succeeds immediately. No stale-lock cleanup is needed.
//
// The in-process mutexes (Lifecycle.mu, StateStore.mu) cannot provide
// either property — this is the cross-process gap TS-015-04-03 closes.
func TestOperationLock_CrossProcessExclusionAndCrashRelease(t *testing.T) {
	if os.Getenv(helperEnv) == "1" {
		// Helper process body: acquire the lock like an in-flight
		// `anvil server release` command would, signal readiness via a
		// file the parent polls, then block until the parent kills us to
		// simulate a crash (SIGKILL — no Release, no defer, no cleanup).
		installRoot := os.Getenv(helperRootEnv)
		lock := NewOperationLock(installRoot)
		if err := lock.Acquire("activate"); err != nil {
			fmt.Fprintln(os.Stderr, "helper acquire:", err)
			os.Exit(2)
		}
		if err := os.WriteFile(filepath.Join(installRoot, "helper-ready"), []byte("ready"), 0644); err != nil {
			fmt.Fprintln(os.Stderr, "helper ready:", err)
			os.Exit(3)
		}
		// Sleep (not select{} — a bare select deadlocks the Go runtime's
		// deadlock detector). The timer keeps the process alive until the
		// parent SIGKILLs it.
		time.Sleep(time.Hour)
		return
	}

	installRoot := t.TempDir()
	readyPath := filepath.Join(installRoot, "helper-ready")

	cmd := exec.Command(os.Args[0], "-test.run=^TestOperationLock_CrossProcessExclusionAndCrashRelease$")
	cmd.Env = append(os.Environ(), helperEnv+"=1", helperRootEnv+"="+installRoot)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper process: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	// Wait until the helper holds the lock (it writes the ready file only
	// after a successful acquire).
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(readyPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper process never acquired the lock")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Cross-process exclusion: the parent is rejected while the child
	// process holds the lock, and the rejection names the child's operation
	// (recorded by the child in runtime-state.json).
	parentLock := NewOperationLock(installRoot)
	err := parentLock.Acquire("install")
	if err == nil {
		t.Fatal("parent acquire must be rejected while the child process holds the lock")
	}
	for _, want := range []string{"another lifecycle operation is in progress", "activate"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("rejection error must mention %q, got: %v", want, err)
		}
	}

	// Crash the holder without release (SIGKILL). The kernel must release
	// the flock, so the parent acquires immediately.
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill helper: %v", err)
	}
	if _, err := cmd.Process.Wait(); err != nil {
		t.Fatalf("wait helper: %v", err)
	}

	if err := parentLock.Acquire("install"); err != nil {
		t.Fatalf("acquire after holder crash must succeed without stale-lock cleanup: %v", err)
	}
	if err := parentLock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
}

// TestOperationLock_NeverGatesOnStateConditionAndPreservesIt verifies
// ADR-036 §3 at the lock level: locking never depends on diagnostics-style
// results. The closest lifecycle-state analogue of diagnostics is the
// runtime condition fields — a degraded/offline condition neither blocks
// acquisition nor is clobbered by the lock record handling (state is
// load-preserved, ADR-031 §3, §6).
func TestOperationLock_NeverGatesOnStateConditionAndPreservesIt(t *testing.T) {
	installRoot := t.TempDir()
	statePath := filepath.Join(installRoot, stateFileName)

	store := NewStateStore(statePath)
	store.SetRuntimeCondition(ConditionDegraded)
	store.SetSharedResourceStatus(ResourceInaccessible)
	if err := store.Save(); err != nil {
		t.Fatalf("save pre-existing state: %v", err)
	}

	lock := NewOperationLock(installRoot)
	if err := lock.Acquire("install"); err != nil {
		t.Fatalf("acquire must not be gated by runtime condition: %v", err)
	}
	store = NewStateStore(statePath)
	if err := store.Load(); err != nil {
		t.Fatalf("load state while held: %v", err)
	}
	state := store.State()
	if state.RuntimeCondition != ConditionDegraded {
		t.Errorf("condition changed by lock acquire: %s", state.RuntimeCondition)
	}
	if state.SharedResourceStatus != ResourceInaccessible {
		t.Errorf("shared resource status changed by lock acquire: %s", state.SharedResourceStatus)
	}
	if state.OperationLock == nil {
		t.Error("lock record missing while held")
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	store = NewStateStore(statePath)
	if err := store.Load(); err != nil {
		t.Fatalf("load state after release: %v", err)
	}
	state = store.State()
	if state.RuntimeCondition != ConditionDegraded {
		t.Errorf("condition changed by lock release: %s", state.RuntimeCondition)
	}
	if state.SharedResourceStatus != ResourceInaccessible {
		t.Errorf("shared resource status changed by lock release: %s", state.SharedResourceStatus)
	}
	if state.OperationLock != nil {
		t.Error("lock record not cleared on release")
	}
}

// TestOperationLock_FirstAcquireCreatesStateWithLockRecord verifies the
// first-install path: when runtime-state.json does not exist yet, Acquire
// initializes it (defaults + lock record) instead of failing — the same
// load-preserve semantics the coordinator applies to its own state
// handling, with the "missing file initializes default state" rule.
func TestOperationLock_FirstAcquireCreatesStateWithLockRecord(t *testing.T) {
	installRoot := t.TempDir()
	statePath := filepath.Join(installRoot, stateFileName)

	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("precondition: state file must not exist yet, stat err = %v", err)
	}

	lock := NewOperationLock(installRoot)
	if err := lock.Acquire("install"); err != nil {
		t.Fatalf("Acquire on fresh root: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	store := NewStateStore(statePath)
	if err := store.Load(); err != nil {
		t.Fatalf("state file must exist after acquire: %v", err)
	}
	if rec := store.State().OperationLock; rec != nil {
		t.Errorf("lock record must be cleared after release, got %+v", rec)
	}
}

// TestOperationLock_UnreadableStateFailsAcquireAndDropsLock verifies the
// never-clobber rule: a state file that cannot be read is a hard error at
// acquire (lifecycle state is never overwritten when it cannot be read),
// and the flock is dropped again so a repaired state file unblocks the
// server.
func TestOperationLock_UnreadableStateFailsAcquireAndDropsLock(t *testing.T) {
	installRoot := t.TempDir()
	statePath := filepath.Join(installRoot, stateFileName)
	if err := os.WriteFile(statePath, []byte("{ not json"), 0644); err != nil {
		t.Fatalf("write corrupt state: %v", err)
	}

	lock := NewOperationLock(installRoot)
	if err := lock.Acquire("install"); err == nil {
		t.Fatal("acquire with unreadable state file must fail")
	}

	// Repair the state file; the lock must be reusable (the failed acquire
	// dropped the flock).
	if err := os.WriteFile(statePath, []byte(`{"active_release_id":"","runtime_condition":"normal","shared_resource_status":"accessible"}`), 0644); err != nil {
		t.Fatalf("repair state: %v", err)
	}
	if err := lock.Acquire("install"); err != nil {
		t.Fatalf("acquire after state repair: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
}

// TestOperationLock_ReleaseWithoutAcquireIsNoop verifies Release is safe to
// defer on every path.
func TestOperationLock_ReleaseWithoutAcquireIsNoop(t *testing.T) {
	lock := NewOperationLock(t.TempDir())
	if err := lock.Release(); err != nil {
		t.Fatalf("Release without Acquire must be a no-op: %v", err)
	}
}

// TestOperationLock_LockFilePersists verifies the lock file is created once
// and never removed: unlinking a locked file would let a second process
// lock a fresh inode, silently breaking mutual exclusion.
func TestOperationLock_LockFilePersists(t *testing.T) {
	installRoot := t.TempDir()
	lockPath := filepath.Join(installRoot, LockFileName)

	lock := NewOperationLock(installRoot)
	if err := lock.Acquire("install"); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file must persist after release: %v", err)
	}
}

// TestStateStore_OperationLockRecordRoundTrip verifies the lock record is
// first-class persisted lifecycle state: it survives Save/Load and is
// cleared by ClearOperationLock (ADR-014: Runtime State contains locks).
func TestStateStore_OperationLockRecordRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), stateFileName)

	store := NewStateStore(path)
	store.SetOperationLock("activate")
	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded := NewStateStore(path)
	if err := reloaded.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	rec := reloaded.State().OperationLock
	if rec == nil {
		t.Fatal("operation lock record missing after reload")
	}
	if rec.Operation != "activate" || rec.PID != os.Getpid() || rec.AcquiredAt.IsZero() {
		t.Errorf("record not persisted faithfully: %+v", rec)
	}

	reloaded.ClearOperationLock()
	if err := reloaded.Save(); err != nil {
		t.Fatalf("Save after clear: %v", err)
	}
	reloaded = NewStateStore(path)
	if err := reloaded.Load(); err != nil {
		t.Fatalf("Load after clear: %v", err)
	}
	if rec := reloaded.State().OperationLock; rec != nil {
		t.Errorf("operation lock record must be nil after ClearOperationLock, got %+v", rec)
	}
}

// ---------------------------------------------------------------------------
// Security hardening tests (TS-015-04-03 security review, MAJOR finding)
//
// Finding: the lock file was created 0644 (world-readable). flock(2) does
// NOT check the fd's access mode — a non-owner could open the lock file
// O_RDONLY and take an exclusive flock, permanently wedging lifecycle
// operations (the file is never unlinked). The fix: mode 0600 + fd-based
// self-healing chmod + O_NOFOLLOW + regular-file check.
//
// The O_RDONLY vector is closed by the permission boundary, not by flock
// semantics: with mode 0600 only the server user (root) can open the file
// at all; any other principal fails at open(2) with EACCES before flock
// is ever reached. The owner (root) is the trusted principal — a
// root-owned exclusive flock is the legitimate operation lock, not an
// attack. TestOperationLock_ReadOnlyOpenRejectedForNonOwner proves the
// EACCES boundary end-to-end when the suite runs as root; the mode
// assertions below pin the boundary in every run.
// ---------------------------------------------------------------------------

// TestOperationLock_LockFileModeIs0600 verifies the security boundary: the
// lock file is created and held at mode 0600 (owner-only), so a non-owner
// cannot open it at all — closing the O_RDONLY + flock(LOCK_EX) wedge
// (flock does not check the fd's access mode).
func TestOperationLock_LockFileModeIs0600(t *testing.T) {
	installRoot := t.TempDir()
	lockPath := filepath.Join(installRoot, LockFileName)

	lock := NewOperationLock(installRoot)
	if err := lock.Acquire("install"); err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	fi, err := os.Stat(lockPath)
	if err != nil {
		t.Fatalf("stat lock file: %v", err)
	}
	if !fi.Mode().IsRegular() {
		t.Fatalf("lock file is not a regular file: %v", fi.Mode())
	}
	if got := fi.Mode().Perm(); got != 0600 {
		t.Errorf("lock file mode = %o, want 0600 (owner-only; a world-readable lock file lets a non-owner take an exclusive flock on a read-only fd)", got)
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	// The mode must survive the operation (the file is never unlinked).
	fi, err = os.Stat(lockPath)
	if err != nil {
		t.Fatalf("stat lock file after release: %v", err)
	}
	if got := fi.Mode().Perm(); got != 0600 {
		t.Errorf("lock file mode after release = %o, want 0600", got)
	}
}

// TestOperationLock_ChmodSelfHealsLegacy0644 verifies that a lock file
// created before the security hardening (0644) is healed to 0600 on the
// next acquire — the file is never unlinked, so healing on acquire is the
// only upgrade path.
func TestOperationLock_ChmodSelfHealsLegacy0644(t *testing.T) {
	installRoot := t.TempDir()
	lockPath := filepath.Join(installRoot, LockFileName)

	// Simulate a pre-hardening lock file: world-readable, empty, present.
	if err := os.WriteFile(lockPath, nil, 0644); err != nil {
		t.Fatalf("pre-create legacy lock file: %v", err)
	}
	if fi, err := os.Stat(lockPath); err != nil || fi.Mode().Perm() != 0644 {
		t.Fatalf("precondition: lock file must start at 0644 (mode %v, err %v)", fi.Mode(), err)
	}

	lock := NewOperationLock(installRoot)
	if err := lock.Acquire("install"); err != nil {
		t.Fatalf("Acquire on legacy lock file: %v", err)
	}
	fi, err := os.Stat(lockPath)
	if err != nil {
		t.Fatalf("stat lock file: %v", err)
	}
	if got := fi.Mode().Perm(); got != 0600 {
		t.Errorf("legacy lock file mode after acquire = %o, want 0600 (self-healing chmod)", got)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
}

// TestOperationLock_SymlinkLockFileRejected verifies O_NOFOLLOW: a
// symlinked lock file is refused instead of redirecting the flock to an
// attacker-held inode, and the symlink target is not locked.
func TestOperationLock_SymlinkLockFileRejected(t *testing.T) {
	installRoot := t.TempDir()
	lockPath := filepath.Join(installRoot, LockFileName)

	// Attacker-held inode: a regular file the symlink would point at.
	targetPath := filepath.Join(installRoot, "attacker-inode")
	if err := os.WriteFile(targetPath, []byte("target"), 0644); err != nil {
		t.Fatalf("create target: %v", err)
	}
	if err := os.Symlink(targetPath, lockPath); err != nil {
		t.Fatalf("create symlink at lock path: %v", err)
	}

	lock := NewOperationLock(installRoot)
	err := lock.Acquire("install")
	if err == nil {
		t.Fatal("acquire must refuse a symlinked lock file")
	}
	if !strings.Contains(err.Error(), "symbolic link") {
		t.Errorf("rejection must explain the symlink, got: %v", err)
	}

	// The symlink must not have been followed: the target inode is not
	// flocked, so a direct exclusive flock on it succeeds.
	target, err := os.OpenFile(targetPath, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open target: %v", err)
	}
	defer target.Close()
	if err := syscall.Flock(int(target.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Errorf("target inode is locked despite the symlink being refused: %v", err)
	}
	_ = syscall.Flock(int(target.Fd()), syscall.LOCK_UN)
}

// TestOperationLock_NonRegularLockFileRejected verifies the regular-file
// check: a non-regular inode at the lock path (here a FIFO) is refused
// instead of being flocked.
func TestOperationLock_NonRegularLockFileRejected(t *testing.T) {
	installRoot := t.TempDir()
	lockPath := filepath.Join(installRoot, LockFileName)

	if err := syscall.Mkfifo(lockPath, 0600); err != nil {
		t.Fatalf("create fifo at lock path: %v", err)
	}

	lock := NewOperationLock(installRoot)
	err := lock.Acquire("install")
	if err == nil {
		t.Fatal("acquire must refuse a non-regular lock file")
	}
	if !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("rejection must explain the non-regular inode, got: %v", err)
	}
}

// lockHelperOpenEnv gates the O_RDONLY-wedge helper process. When run as
// root, the helper drops to an unprivileged uid and attempts to open the
// lock file read-only — the exact attack the 0600 mode closes.
const lockHelperOpenEnv = "ANVIL_OPLOCK_HELPER_RO_OPEN"

// TestOperationLock_ReadOnlyOpenRejectedForNonOwner verifies the security
// boundary end-to-end: with the lock file at 0600, an unprivileged
// principal cannot even open it read-only, so the
// open(O_RDONLY) + flock(LOCK_EX) wedge is impossible — flock is never
// reached by a non-owner.
//
// The test drops privileges in a helper process (setuid to 65534), which
// requires the suite to run as root; it skips otherwise. The in-process
// mode assertions (TestOperationLock_LockFileModeIs0600) pin the boundary
// in every run.
func TestOperationLock_ReadOnlyOpenRejectedForNonOwner(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to drop privileges in the helper process")
	}

	installRoot := t.TempDir()
	lockPath := filepath.Join(installRoot, LockFileName)

	// Create the lock file at 0600, owned by root, exactly as the server
	// would.
	lock := NewOperationLock(installRoot)
	if err := lock.Acquire("install"); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	fi, err := os.Stat(lockPath)
	if err != nil {
		t.Fatalf("stat lock file: %v", err)
	}
	if got := fi.Mode().Perm(); got != 0600 {
		t.Fatalf("precondition: lock file mode = %o, want 0600", got)
	}

	if os.Getenv(lockHelperOpenEnv) == "1" {
		// Helper process body: drop privileges, then try the attack —
		// open(O_RDONLY) on the root-owned 0600 lock file.
		if err := syscall.Setgid(65534); err != nil {
			fmt.Fprintln(os.Stderr, "helper setgid:", err)
			os.Exit(4)
		}
		if err := syscall.Setuid(65534); err != nil {
			fmt.Fprintln(os.Stderr, "helper setuid:", err)
			os.Exit(5)
		}
		f, err := os.OpenFile(os.Getenv(lockHelperOpenEnv+"_PATH"), os.O_RDONLY, 0)
		if err == nil {
			f.Close()
			fmt.Println("OPEN_OK")
			os.Exit(0)
		}
		fmt.Println("OPEN_DENIED")
		os.Exit(0)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestOperationLock_ReadOnlyOpenRejectedForNonOwner$")
	cmd.Env = append(os.Environ(), lockHelperOpenEnv+"=1", lockHelperOpenEnv+"_PATH="+lockPath)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run helper: %v", err)
	}
	if !strings.Contains(string(out), "OPEN_DENIED") {
		t.Fatalf("unprivileged read-only open of the 0600 lock file must be denied; helper output:\n%s", out)
	}
}
