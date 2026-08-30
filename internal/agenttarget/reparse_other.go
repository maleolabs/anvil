//go:build !windows

package agenttarget

import "os"

// isWindowsReparsePoint reports whether a file info carries the Windows
// FILE_ATTRIBUTE_REPARSE_POINT attribute. On non-Windows platforms there
// are no reparse points; symlinks are detected by the ModeSymlink check
// and the portable EvalSymlinks backstop in writer.go.
func isWindowsReparsePoint(_ os.FileInfo) bool {
	return false
}
