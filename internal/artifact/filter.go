// Package artifact defines the Anvil artifact directory layout and
// packaging primitives.
//
// Reference: TS-P3-03, CH-P3-01, ADR-004, EPIC-003
package artifact

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// FilterOptions configures the file filtering process.
type FilterOptions struct {
	// SourceDir is the root directory to scan for files.
	SourceDir string

	// Include specifies glob patterns for files to include.
	// If empty, all non-excluded files are included.
	Include []string

	// Exclude specifies glob patterns for files to exclude.
	Exclude []string
}

// FilterResult contains the result of file filtering.
type FilterResult struct {
	// Files is a list of file paths relative to SourceDir.
	Files []string
}

// FilterFiles selects files from SourceDir based on include/exclude rules.
//
// The filtering algorithm:
//  1. Walk the source directory using filepath.WalkDir.
//  2. For each file, check against exclude patterns first.
//  3. If excluded, check against include patterns (include overrides exclude).
//  4. If no include patterns are specified (or only catch-all patterns), all non-excluded files are included.
//  5. Directories that match exclusion patterns are pruned early.
//
// Glob patterns support:
//   - Simple patterns are matched with filepath.Match (*, ?, [a-z]).
//   - The ** sequence matches any number of path segments (e.g. "dir/**"
//     matches "dir", "dir/file", "dir/sub/file").
//   - Patterns ending with "/**" are treated as recursive directory matches.
//
// The engine never modifies source files.
func FilterFiles(opts FilterOptions) (*FilterResult, error) {
	// Validate that SourceDir exists and is a directory.
	info, err := os.Stat(opts.SourceDir)
	if err != nil {
		return nil, fmt.Errorf("access source directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("source path %q is not a directory", opts.SourceDir)
	}

	// Normalize include patterns: treat catch-all patterns as "no include filter"
	// since they match everything and would otherwise make exclude patterns ineffective.
	includePatterns := normalizeIncludePatterns(opts.Include)

	var files []string

	err = filepath.WalkDir(opts.SourceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Compute the path relative to SourceDir.
		relPath, err := filepath.Rel(opts.SourceDir, path)
		if err != nil {
			return err
		}

		// Skip the root itself.
		if relPath == "." {
			return nil
		}

		if d.IsDir() {
			// Prune directories that are excluded and not overridden by an
			// include pattern. This avoids walking large excluded trees.
			if isGlobExcluded(relPath, opts.Exclude) && !isGlobIncluded(relPath, includePatterns) {
				return filepath.SkipDir
			}
			return nil
		}

		// For regular files: exclude by default, include overrides.
		if isGlobExcluded(relPath, opts.Exclude) && !isGlobIncluded(relPath, includePatterns) {
			return nil
		}

		// When include patterns are explicitly provided (non-catch-all), a file must
		// match at least one include pattern regardless of exclusion status.
		if len(includePatterns) > 0 && !isGlobIncluded(relPath, includePatterns) {
			return nil
		}

		files = append(files, relPath)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk source directory: %w", err)
	}

	return &FilterResult{Files: files}, nil
}

// normalizeIncludePatterns returns the include patterns with catch-all patterns
// (["**/*"] or ["**"]) removed, since they match everything and would otherwise
// prevent exclude patterns from working. Returns nil if no effective include
// patterns remain.
func normalizeIncludePatterns(patterns []string) []string {
	if len(patterns) == 1 {
		p := patterns[0]
		if p == "**/*" || p == "**" {
			return nil
		}
	}
	return patterns
}

// isGlobExcluded reports whether path matches any exclusion pattern.
func isGlobExcluded(path string, patterns []string) bool {
	for _, p := range patterns {
		if globMatch(p, path) {
			return true
		}
	}
	return false
}

// isGlobIncluded reports whether path matches any inclusion pattern.
func isGlobIncluded(path string, patterns []string) bool {
	for _, p := range patterns {
		if globMatch(p, path) {
			return true
		}
	}
	return false
}

// globMatch reports whether path matches the given glob pattern.
//
// It extends filepath.Match with support for:
//   - ** sequences: "dir/**" matches "dir" and everything under "dir/";
//     "**" and "**/*" match any path.
//   - Basename matching: patterns without a path separator are also matched
//     against the base name of the path (e.g. "*.log" matches "a/b/error.log").
//
// For standard patterns, it delegates to filepath.Match.
func globMatch(pattern, path string) bool {
	// Handle the catch-all pattern.
	if pattern == "**/*" || pattern == "**" {
		return true
	}

	// Handle patterns ending with "/**" as recursive directory matches.
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		// Match the directory itself.
		if path == prefix {
			return true
		}
		// Match anything under the directory (OS-agnostic separator).
		if strings.HasPrefix(path, prefix+string(filepath.Separator)) {
			return true
		}
		if strings.HasPrefix(path, prefix+"/") {
			return true
		}
		return false
	}

	// For patterns without a path separator, also match against the base
	// name. This makes "*.log" match "subdir/error.log" as expected.
	if !strings.Contains(pattern, string(filepath.Separator)) && !strings.Contains(pattern, "/") {
		base := filepath.Base(path)
		matched, err := filepath.Match(pattern, base)
		if err == nil && matched {
			return true
		}
	}

	// Use filepath.Match for standard glob patterns.
	matched, err := filepath.Match(pattern, path)
	if err != nil {
		return false
	}
	return matched
}
