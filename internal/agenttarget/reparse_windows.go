//go:build windows

package agenttarget

import (
	"os"
	"syscall"
)

// isWindowsReparsePoint reports whether a file info carries the Windows
// FILE_ATTRIBUTE_REPARSE_POINT attribute. os.Lstat reports a junction
// (directory symlink) as a plain directory, so the Lstat ModeSymlink check
// alone misses it (M2 security fix).
//
// The attribute is read from the FileInfo's underlying Win32 file
// attribute data (FileInfo.Sys() → syscall.Win32FileAttributeData). When
// the info is not of the expected type the check returns false and the
// caller's portable EvalSymlinks backstop still applies.
func isWindowsReparsePoint(info os.FileInfo) bool {
	attr, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return false
	}
	// FILE_ATTRIBUTE_REPARSE_POINT = 0x400
	const fileAttributeReparsePoint = 0x400
	return attr.FileAttributes&fileAttributeReparsePoint != 0
}
