// Package config provides configuration discovery for Anvil projects.
// Discovery finds configuration files from well-known locations — the project
// root directory (via project discovery) and the global configuration
// directory (platform-specific). It returns all discovered file paths so
// that the loader (TS-P2-04) can read them.
//
// Reference: TS-P2-03, ADR-005 §7.2, §7.1, ADR-002
package config

import (
	"os"
	"path/filepath"
)

// configExtensions is the list of YAML file extensions to search for when
// discovering configuration files.
var configExtensions = []string{".yaml", ".yml"}

// configFileCandidates returns the list of config file base names to check,
// ordered by preference (canonical extension first).
var configFileCandidates = []string{"anvil.yaml", "anvil.yml"}

// configFileBase is the base name of the Anvil configuration file (without
// extension). The canonical name is "anvil.yaml"; "anvil.yml" is also
// accepted.
const configFileBase = "anvil"

// GlobalConfigDir returns the platform-specific global configuration directory
// for Anvil. It is derived from os.UserConfigDir() with "/anvil" appended.
//
// On Linux this typically resolves to ~/.config/anvil/.
// On macOS it resolves to ~/Library/Application Support/anvil/.
// On Windows it resolves to %AppData%/anvil/.
//
// Reference: TS-P2-03, ADR-005 §7.1
func GlobalConfigDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "anvil"), nil
}

// DiscoverConfigFiles searches for Anvil configuration files in well-known
// locations and returns the discovered file paths.
//
// Search order:
//  1. Project root directory — found by searching the current working
//     directory and its parents for anvil.yaml (matching the project discovery
//     strategy defined by EPIC-001). Both anvil.yaml and anvil.yml are checked.
//  2. Global config directory — os.UserConfigDir() + "/anvil/". Both
//     anvil.yaml and anvil.yml are checked.
//
// Key behaviour:
//   - Returns an empty slice (not an error) when no configuration files are
//     found anywhere. The loader applies compiled defaults in that case.
//   - Project location files are returned before global location files.
//   - The function is read-only: it never creates or modifies files.
//   - Only actual filesystem errors (e.g., permission denied) are propagated
//     by the underlying calls. "Not found" conditions are handled silently.
//
// Reference: TS-P2-03, ADR-005 §7.2, ADR-005 §12.5
func DiscoverConfigFiles() []string {
	paths := make([]string, 0)

	// 1. Check project root directory.
	// Discover the project root by searching from the current working
	// directory upward for the project marker file (anvil.yaml or anvil.yml).
	if root, err := discoverProjectRoot(); err == nil {
		for _, ext := range configExtensions {
			p := filepath.Join(root, configFileBase+ext)
			if fileExists(p) {
				paths = append(paths, p)
			}
		}
	}

	// 2. Check global config directory.
	globalDir, err := GlobalConfigDir()
	if err == nil {
		for _, ext := range configExtensions {
			p := filepath.Join(globalDir, configFileBase+ext)
			if fileExists(p) {
				paths = append(paths, p)
			}
		}
	}

	return paths
}

// discoverProjectRoot locates the Anvil project root by searching the current
// working directory and all parent directories for the project marker file
// (anvil.yaml or anvil.yml). It returns the absolute path of the directory
// containing the marker.
//
// This mirrors the discovery strategy implemented by project.Discover()
// (TS-P1-05) but is defined here to avoid a circular dependency between
// internal/config and internal/project.
//
// The function is read-only: it does not create, modify, or delete any files.
func discoverProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	for {
		for _, candidate := range configFileCandidates {
			configPath := filepath.Join(dir, candidate)
			_, err := os.Stat(configPath)
			if err == nil {
				return dir, nil
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errNoProjectFound
		}
		dir = parent
	}
}

// errNoProjectFound is returned by discoverProjectRoot when no anvil.yaml
// or anvil.yml exists in the current working directory or any parent directory.
var errNoProjectFound = &discoveryError{"no anvil project found in current or parent directories"}

type discoveryError struct{ msg string }

func (e *discoveryError) Error() string { return e.msg }

// fileExists checks whether the given path exists and is a regular file
// (not a directory). It returns false for non-existent paths or when an
// error occurs during stat.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
