package filelock

import (
	"errors"
	"os"
)

var (
	ErrSymlink    = errors.New("lock path is a symbolic link")
	ErrWouldBlock = errors.New("lock is already held")
)

func IsWouldBlock(err error) bool { return errors.Is(err, ErrWouldBlock) }

func IsSymlink(err error) bool { return errors.Is(err, ErrSymlink) }

func Open(path string, flags int, perm os.FileMode) (*os.File, error) {
	return open(path, flags, perm)
}

func Lock(file *os.File, exclusive, nonBlocking bool) error {
	return lock(file, exclusive, nonBlocking)
}

func Unlock(file *os.File) error { return unlock(file) }
