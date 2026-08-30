package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"maleolabs.com/anvil/internal/filelock"
)

// LockFileName is the name of the cross-process operation lock file. It
// lives next to the runtime state file it guards
// (<installRoot>/runtime-state.lock) so the lock and the state it
// serializes are colocated; registry files never carry operational state
// (ADR-031 §6), and the lock is operational state (ADR-014: Runtime State
// contains locks), so it stays out of the registry by construction.
//
// The lock file is intentionally never removed: unlinking a locked file
// would let a new process lock a fresh inode while the previous holder
// still holds the original, silently breaking mutual exclusion. The file
// is created once (0600 — see OperationLock.Acquire for the security
// rationale) and reused for the life of the server root.
const LockFileName = "runtime-state.lock"

// stateFileName is the runtime state file the operation lock record is
// persisted in. It must match the path the coordinator uses for
// runtime-state.json; both are derived from the install root so they can
// never drift apart.
const stateFileName = "runtime-state.json"

// OperationLock serializes lifecycle operations on one server across
// processes (TS-015-04-03, ADR-031 §3 keep list: locking; ADR-014
// baseline safety: reject concurrent activation/rollback operations for
// the same project).
//
// The Server Runtime is invoked per command: every `anvil server release
// install/activate/rollback` runs in a fresh process, so in-process
// mutexes (e.g. Lifecycle.mu, StateStore.mu) cannot serialize operations
// — two concurrent commands on the same server would interleave their
// read-modify-write of runtime-state.json. OperationLock closes that gap
// with a kernel flock on a lock file colocated with the state file:
//
//   - Cross-process: flock is enforced by the kernel across processes.
//   - Crash-safe: the kernel releases the lock when the holding process
//     dies (including SIGKILL), so an interrupted lifecycle operation
//     never wedges the server — no stale-lock cleanup is needed (the
//     fatal flaw of O_EXCL lock files for per-command processes).
//   - No unlink races: the lock file is never removed (see LockFileName).
//   - Non-blocking: a contended acquire fails fast (LOCK_NB) and is
//     reported as a descriptive rejection — the caller retries after the
//     in-flight operation completes (ADR-014: "unless a future locking
//     design explicitly permits them").
//
// Lock state is part of lifecycle state handling: Acquire records the
// holder (operation, pid, acquired-at) in runtime-state.json and Release
// clears it, so the persisted lifecycle state observes in-flight
// operations. The flock is the authority; the record is the observation.
// Locking never depends on diagnostics (ADR-036 §3: lifecycle operations
// never depend on diagnostics) — acquisition is a pure state-file
// concern.
//
// Unix-only by design (flock(2)): the Server Runtime targets Linux
// deployments (default /etc/anvil); CI builds run on Linux.
type OperationLock struct {
	lockPath  string
	statePath string
	file      *os.File
	held      bool
}

// NewOperationLock creates an operation lock guarding the server rooted
// at installRoot. The lock file (<installRoot>/runtime-state.lock) and
// the state file it serializes (<installRoot>/runtime-state.json) are
// derived from the same root.
func NewOperationLock(installRoot string) *OperationLock {
	return &OperationLock{
		lockPath:  filepath.Join(installRoot, LockFileName),
		statePath: filepath.Join(installRoot, stateFileName),
	}
}

// Acquire takes the exclusive operation lock for a lifecycle operation.
//
// A concurrent lifecycle operation holding the lock is safely rejected
// with a descriptive error naming the in-flight operation (read from the
// persisted lock record) — the caller is expected to fail the command and
// let the operator retry. On success the lock is held until Release; if
// the process dies first, the kernel releases the flock and the next
// acquire succeeds (crash recovery needs no cleanup).
//
// The lock record is written into runtime-state.json as part of lifecycle
// state handling (ADR-014: Runtime State contains locks). An unreadable
// state file is a hard error — the lock is released again and the
// operation fails, matching the coordinator's rule that lifecycle state
// is never overwritten when it cannot be read.
//
// Security hardening (verified finding, TS-015-04-03 security review):
//
//   - Mode 0600: flock(2) does NOT check the fd's access mode — a
//     non-owner could open a world-readable lock file O_RDONLY and take
//     an exclusive flock, permanently wedging lifecycle operations (the
//     file is never unlinked). Mode 0600 makes the server user (root) the
//     only principal that can open the file at all, closing that vector.
//   - Self-healing chmod: the file is never unlinked, so a lock file
//     created before this hardening (0644) would stay world-readable.
//     After opening, the fd is chmod'ed to 0600 so legacy lock files are
//     healed on the next acquire.
//   - O_NOFOLLOW + regular-file check: a symlinked lock file could
//     redirect the flock to an attacker-held inode (defense-in-depth —
//     symlink creation already requires write access to the install
//     root); non-regular inodes (FIFO, device) are refused.
func (l *OperationLock) Acquire(operation string) error {
	if err := os.MkdirAll(filepath.Dir(l.lockPath), 0755); err != nil {
		return fmt.Errorf("create lock directory: %w", err)
	}

	f, err := filelock.Open(l.lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		if filelock.IsSymlink(err) {
			return fmt.Errorf("operation lock %s is a symbolic link; refusing to lock", l.lockPath)
		}
		return fmt.Errorf("open operation lock %s: %w", l.lockPath, err)
	}

	// Regular-file check (O_NOFOLLOW companion): flock on a non-regular
	// inode is meaningless and could redirect exclusion to an
	// attacker-influenced object. fstat after open closes the
	// check-vs-open race.
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("stat operation lock %s: %w", l.lockPath, err)
	}
	if !fi.Mode().IsRegular() {
		f.Close()
		return fmt.Errorf("operation lock %s is not a regular file (mode %s); refusing to lock", l.lockPath, fi.Mode())
	}

	// Self-healing chmod to 0600 (see Acquire doc): heals lock files
	// created before the security hardening. The permission boundary is
	// enforced on the fd, not the path, so a path swap between open and
	// chmod cannot bypass it.
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return fmt.Errorf("chmod operation lock %s: %w", l.lockPath, err)
	}

	if err := filelock.Lock(f, true, true); err != nil {
		f.Close()
		if filelock.IsWouldBlock(err) {
			return fmt.Errorf(
				"another lifecycle operation is in progress for this server (lock file %s); %s",
				l.lockPath, l.holderSummary(),
			)
		}
		return fmt.Errorf("acquire operation lock %s: %w", l.lockPath, err)
	}

	l.file = f
	l.held = true

	if err := l.record(operation); err != nil {
		_ = l.releaseFlock()
		return err
	}
	return nil
}

// Release drops the operation lock and clears its persisted record.
//
// Clearing the record is best-effort: the flock is the authority and a
// leftover record self-heals on the next acquire; the flock release must
// always succeed for the lock's mutual-exclusion guarantee to hold.
// Release is safe to defer.
func (l *OperationLock) Release() error {
	if !l.held {
		return nil
	}
	l.clearRecord()
	return l.releaseFlock()
}

// releaseFlock drops the kernel lock and closes the lock file.
func (l *OperationLock) releaseFlock() error {
	var unlockErr error
	if l.file != nil {
		unlockErr = filelock.Unlock(l.file)
		closeErr := l.file.Close()
		if unlockErr == nil {
			unlockErr = closeErr
		}
	}
	l.file = nil
	l.held = false
	if unlockErr != nil {
		return fmt.Errorf("release operation lock %s: %w", l.lockPath, unlockErr)
	}
	return nil
}

// record persists the lock holder in runtime-state.json (load-preserve,
// never clobber — the same rule the coordinator applies to its own state
// handling: state survives crashes and restarts, ADR-031 §3, §6).
func (l *OperationLock) record(operation string) error {
	store := NewStateStore(l.statePath)
	if _, err := os.Stat(l.statePath); err == nil {
		if err := store.Load(); err != nil {
			return fmt.Errorf("load existing runtime state for operation lock record: %w", err)
		}
	}
	store.SetOperationLock(operation)
	if err := store.Save(); err != nil {
		return fmt.Errorf("save runtime state operation lock record: %w", err)
	}
	return nil
}

// clearRecord removes the persisted lock record (best-effort; see
// Release).
func (l *OperationLock) clearRecord() {
	store := NewStateStore(l.statePath)
	if _, err := os.Stat(l.statePath); err != nil {
		return // no state file — nothing to clear
	}
	if err := store.Load(); err != nil {
		return // unreadable state stays untouched; the next acquire overwrites it
	}
	store.ClearOperationLock()
	_ = store.Save()
}

// holderSummary describes the operation currently recorded as holding the
// lock, for the rejection error. It reads the persisted lock record — the
// observation half of "lock state is part of lifecycle state handling".
func (l *OperationLock) holderSummary() string {
	data, err := os.ReadFile(l.statePath)
	if err != nil {
		return "retry after it completes"
	}
	var file struct {
		OperationLock *OperationLockRecord `json:"operation_lock"`
	}
	if err := json.Unmarshal(data, &file); err != nil || file.OperationLock == nil {
		return "retry after it completes"
	}
	r := file.OperationLock
	when := ""
	if !r.AcquiredAt.IsZero() {
		when = " since " + r.AcquiredAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	return fmt.Sprintf("operation %q in progress%s by pid %d; retry after it completes", r.Operation, when, r.PID)
}
