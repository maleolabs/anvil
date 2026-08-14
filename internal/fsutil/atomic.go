// Package fsutil provides low-level filesystem utilities shared across the
// Anvil Core.
//
// The central primitive is WriteFileAtomic: crash-safe persistence for state,
// configuration, and release files. It replaces the previous plain
// os.WriteFile pattern so that a process crash or power loss mid-write can
// never leave a truncated or partially-written file at the final path
// (TD-002).
package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteFileAtomic writes data to path atomically.
//
// The write sequence is:
//
//  1. Create a uniquely-named temporary file in the same directory as path.
//     The temp file must live on the same filesystem as the final path for
//     os.Rename to be atomic (TD-002, TS-P5-08).
//  2. Write the data, fsync the temp file so the content is durable before
//     it becomes visible, and close it.
//  3. Apply the target file mode: the requested perm for new files, or the
//     existing file's mode when path already exists — an operator-hardened
//     mode such as 0600 is preserved on overwrite rather than silently
//     widened. (os.CreateTemp creates the temp file with 0600 regardless,
//     so the mode is always set explicitly here.)
//  4. Atomically rename the temp file over path. Observers see either the
//     complete previous file or the complete new file — never a partial one.
//  5. fsync the parent directory so the rename itself is durable across a
//     power loss, not just the file content.
//
// Error semantics: an error before the rename means path is untouched and
// the temporary file has been removed. An error after the rename (the final
// directory fsync) means the content HAS been replaced at path, but the
// durability of the rename is unconfirmed — callers may treat that outcome
// distinctly from a pre-rename failure.
//
// Crash-recovery invariant: a crash at any point leaves either the previous
// complete file or the new complete file at path, plus at most a leftover
// temp file in the same directory (never a partial final file). A subsequent
// call to WriteFileAtomic — or any reader of path — observes only complete
// content. Leftover temp files from a crashed write are inert: no load path
// reads them (they carry a .tmp-* suffix and never match the final path),
// and each write creates a fresh uniquely-named temp, so they never
// interfere with a later write.
//
// The parent directory must already exist; the helper never creates it. This
// preserves the directory contract of the previous os.WriteFile call sites
// (e.g., Release.Save requires the containing directory to exist).
//
// Reference: TD-002, TS-P5-08 (atomic activation crash-safety assumption)
func WriteFileAtomic(path string, data []byte, perm os.FileMode) (err error) {
	dir := filepath.Dir(path)

	// Step 1: Stage the content in a uniquely-named temp file in the same
	// directory as the final path.
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()

	// Clean up the temp file if any step below fails. The final path is
	// never modified until the rename succeeds, so a failed write leaves
	// the previous file intact.
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	// Step 2: Write and fsync the content before it becomes visible.
	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("write temp file %s: %w", tmpName, err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("sync temp file %s: %w", tmpName, err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close temp file %s: %w", tmpName, err)
	}

	// Step 3: Apply the target mode — the existing file's mode when the
	// target already exists (an operator may have hardened it, e.g. 0600),
	// otherwise the requested perm for new files. CreateTemp defaults to
	// 0600, so the mode is always set explicitly here.
	mode := perm
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat target file %s: %w", path, err)
	}
	if err = os.Chmod(tmpName, mode); err != nil {
		return fmt.Errorf("set permissions on temp file %s: %w", tmpName, err)
	}

	// Step 4: Atomically replace the final path.
	if err = os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpName, path, err)
	}

	// Step 5: Make the rename itself durable. Without this, a power loss
	// could persist the temp file's content but not the directory entry
	// that publishes it under the final name.
	return syncDir(dir)
}

// syncDir fsyncs the directory containing the given path so directory-entry
// changes (e.g., a completed rename) are durable across a power loss.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open directory %s for sync: %w", dir, err)
	}
	defer d.Close()

	if err := d.Sync(); err != nil {
		return fmt.Errorf("sync directory %s: %w", dir, err)
	}
	return nil
}
