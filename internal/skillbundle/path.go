package skillbundle

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Entry-path safety rules (TS-021-01; skill-bundle-format.md §6.1).
//
// Every path that names an archive entry or a manifest files[] entry must
// satisfy these rules before it may touch the filesystem:
//
//   - relative: no leading '/', no drive-letter prefix ("C:..."), no
//     backslash (a Windows-style separator smuggled into a POSIX archive —
//     on POSIX a backslash is a legal filename character, so rejecting it
//     unconditionally is the only portable-safe rule);
//   - no empty, ".", or ".." components (no traversal, no redundant
//     components);
//   - no control characters in any component.
//
// The rules are character-level and platform-independent; the final
// containment guarantee is the resolved-path prefix check in
// extract.go (pathWithinRoot), which holds even if this character set
// ever needs to grow.

// validateEntryPath checks one entry path against the rooted extraction
// rules. A rejected path yields an error describing the exact violation.
func validateEntryPath(name string) error {
	if name == "" {
		return fmt.Errorf("the entry path is empty")
	}
	if strings.HasPrefix(name, "/") {
		return fmt.Errorf("entry path %q is absolute — a skill bundle entry must be relative to the extraction root", name)
	}
	if filepath.IsAbs(name) {
		return fmt.Errorf("entry path %q is absolute — a skill bundle entry must be relative to the extraction root", name)
	}
	if len(name) >= 2 && isDriveLetter(name[0]) && name[1] == ':' {
		return fmt.Errorf("entry path %q carries a drive-letter prefix — a skill bundle entry must be relative to the extraction root", name)
	}
	if strings.Contains(name, "\\") {
		return fmt.Errorf("entry path %q contains a backslash — Windows-style path separators are not allowed in a skill bundle; use '/'", name)
	}
	for _, comp := range strings.Split(name, "/") {
		if comp == "" {
			return fmt.Errorf("entry path %q contains an empty path component — leading, trailing, or consecutive '/' is not allowed", name)
		}
		if comp == "." {
			return fmt.Errorf("entry path %q contains a '.' component — redundant components are not allowed in a skill bundle", name)
		}
		if comp == ".." {
			return fmt.Errorf("entry path %q contains a '..' component — path traversal is not allowed in a skill bundle", name)
		}
		if hasControlChar(comp) {
			return fmt.Errorf("entry path %q contains control characters — not allowed in a skill bundle", name)
		}
	}
	return nil
}

// pathDepth returns the number of path components of name (with '/'
// separators), 0 for an empty name.
func pathDepth(name string) int {
	if name == "" {
		return 0
	}
	return strings.Count(name, "/") + 1
}

// pathWithinRoot reports whether target (an already-cleaned absolute
// path) lies strictly inside root (the resolved extraction root). It is
// the final containment check: even if a path passed the character-level
// rules, no write may resolve outside root.
func pathWithinRoot(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// isDriveLetter reports whether b is an ASCII letter.
func isDriveLetter(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// hasControlChar reports whether s contains a control character (C0
// range or DEL).
func hasControlChar(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}
