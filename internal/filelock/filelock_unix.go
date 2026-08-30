//go:build !windows

package filelock

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func open(path string, flags int, perm os.FileMode) (*os.File, error) {
	file, err := os.OpenFile(path, flags|syscall.O_NOFOLLOW, perm)
	if errors.Is(err, syscall.ELOOP) {
		return nil, fmt.Errorf("%w: %s", ErrSymlink, path)
	}
	return file, err
}

func lock(file *os.File, exclusive, nonBlocking bool) error {
	flags := syscall.LOCK_SH
	if exclusive {
		flags = syscall.LOCK_EX
	}
	if nonBlocking {
		flags |= syscall.LOCK_NB
	}
	if err := syscall.Flock(int(file.Fd()), flags); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return fmt.Errorf("%w: %v", ErrWouldBlock, err)
		}
		return err
	}
	return nil
}

func unlock(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
