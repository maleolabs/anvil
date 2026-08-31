//go:build windows

package filelock

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func open(path string, flags int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(path, flags, perm)
}

func lock(file *os.File, exclusive, nonBlocking bool) error {
	var flags uint32
	if exclusive {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	if nonBlocking {
		flags |= windows.LOCKFILE_FAIL_IMMEDIATELY
	}
	if err := windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, 1, 0, &windows.Overlapped{}); err != nil {
		if err == windows.ERROR_LOCK_VIOLATION {
			return fmt.Errorf("%w: %v", ErrWouldBlock, err)
		}
		return err
	}
	return nil
}

func unlock(file *os.File) error {
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &windows.Overlapped{})
}
