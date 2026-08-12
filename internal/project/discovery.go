// Package project provides the Anvil project discovery mechanism.
//
// Discovery locates an Anvil project by searching the current working
// directory and its parent directories for the project marker file
// (anvil.yaml). It is the primary entry point for commands that need
// to operate within an Anvil project context.
//
// Reference: TS-P1-05, EPIC-001
package project

import (
	"errors"
	"os"
	"path/filepath"
)

// ErrNoProjectFound is returned by Discover when no anvil.yaml exists in
// the current working directory or any parent directory up to the
// filesystem root.
//
// Reference: TS-P1-05
var ErrNoProjectFound = errors.New("no anvil project found in current or parent directories")

// Discover locates an Anvil project by searching the current working
// directory and all parent directories for the project marker file
// (anvil.yaml).
//
// It returns the absolute path of the directory containing anvil.yaml
// as the project root. If no project is found, it returns an empty
// string and ErrNoProjectFound.
//
// Only actual filesystem errors (e.g., permission denied) are returned.
// "File not found" is handled silently by continuing the parent
// traversal.
//
// The function is read-only: it does not create, modify, or delete any
// files or directories.
//
// Edge cases:
//   - If the current working directory has been deleted, os.Getwd
//     returns an error which is propagated.
//   - If a directory in the traversal chain is inaccessible (e.g.,
//     permission denied), os.Stat returns a non-IsNotExist error
//     which is propagated.
//   - Traversal stops at the filesystem root (when
//     filepath.Dir(dir) == dir) to prevent infinite loops.
//
// Reference: TS-P1-05, ADR-002 §3.3
func Discover() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	// Ensure we have an absolute path for consistent parent traversal.
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	for {
		configPath := filepath.Join(dir, ConfigFileName)
		_, err := os.Stat(configPath)
		if err == nil {
			// Found the marker file; return the containing directory.
			return dir, nil
		}

		if !os.IsNotExist(err) {
			// Actual filesystem error (e.g., permission denied).
			return "", err
		}

		// Move to the parent directory.
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached the filesystem root without finding the marker file.
			return "", ErrNoProjectFound
		}
		dir = parent
	}
}

// DiscoverSearched locates an Anvil project by searching the current working
// directory and all parent directories for the project marker file
// (anvil.yaml). It behaves identically to Discover but additionally returns
// the complete list of directories that were searched during traversal.
//
// The searched slice is ordered from the current working directory upward
// to the filesystem root (when no project is found) or to the project root
// (when a project is found).
//
// When a project is found, the returned root is the last entry in searched.
// When no project is found, root is empty and err is ErrNoProjectFound.
//
// The function is read-only: it does not create, modify, or delete any
// files or directories.
//
// Reference: ST-P1-06
func DiscoverSearched() (root string, searched []string, err error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", nil, err
	}

	// Ensure we have an absolute path for consistent parent traversal.
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", nil, err
	}

	for {
		searched = append(searched, dir)

		configPath := filepath.Join(dir, ConfigFileName)
		_, err := os.Stat(configPath)
		if err == nil {
			// Found the marker file; return the containing directory.
			return dir, searched, nil
		}

		if !os.IsNotExist(err) {
			// Actual filesystem error (e.g., permission denied).
			return "", searched, err
		}

		// Move to the parent directory.
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached the filesystem root without finding the marker file.
			return "", searched, ErrNoProjectFound
		}
		dir = parent
	}
}
