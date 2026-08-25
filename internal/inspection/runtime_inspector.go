// Package inspection provides read-only diagnostic inspection capabilities
// for Anvil Runtime environments and configuration.
//
// Reference: TS-009-005, TS-009-006, ADR-003 §8.5, ADR-005 §7.5/§8.3
package inspection

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"maleolabs.com/anvil/internal/runtime"
	"maleolabs.com/anvil/internal/server"
)

// RuntimeInspector performs read-only diagnostic inspections on an Anvil
// Runtime environment. It examines filesystem state, symlinks, directories,
// and configuration files without modifying anything.
//
// Reference: TS-009-005, ADR-003 §8.5, ADR-006 §5.2
type RuntimeInspector struct {
	cfg runtime.RuntimeConfig
}

// NewRuntimeInspector creates a RuntimeInspector that inspects the Runtime
// environment described by the given configuration.
//
// Reference: TS-009-005
func NewRuntimeInspector(cfg runtime.RuntimeConfig) *RuntimeInspector {
	return &RuntimeInspector{cfg: cfg}
}

// InspectActiveSymlink checks whether the active release symlink exists and
// points to a valid directory. Uses runtime.SymlinkSwitcher to read symlink
// state.
//
// Reference: TS-009-005, ADR-003 §8.5
func (ri *RuntimeInspector) InspectActiveSymlink() InspectionCheck {
	switcher := runtime.NewSymlinkSwitcher(ri.cfg)

	if !switcher.SymlinkExists() {
		return InspectionCheck{
			Name:    "active_symlink",
			Passed:  false,
			Details: fmt.Sprintf("active symlink does not exist at %s", ri.cfg.ActiveSymlinkPath()),
		}
	}

	target, err := switcher.ActiveSymlinkTarget()
	if err != nil {
		return InspectionCheck{
			Name:    "active_symlink",
			Passed:  false,
			Details: fmt.Sprintf("cannot read symlink target: %v", err),
		}
	}

	// Verify the target is a valid directory.
	info, err := os.Stat(target)
	if err != nil {
		return InspectionCheck{
			Name:    "active_symlink",
			Passed:  false,
			Details: fmt.Sprintf("symlink target %s is not accessible: %v", target, err),
		}
	}
	if !info.IsDir() {
		return InspectionCheck{
			Name:    "active_symlink",
			Passed:  false,
			Details: fmt.Sprintf("symlink target %s exists but is not a directory", target),
		}
	}

	return InspectionCheck{
		Name:    "active_symlink",
		Passed:  true,
		Details: fmt.Sprintf("symlink exists at %s, target: %s", ri.cfg.ActiveSymlinkPath(), target),
	}
}

// InspectReleaseDirectories checks whether the releases directory exists and
// is a valid directory on the filesystem.
//
// Reference: TS-009-005, ADR-003 §8.5
func (ri *RuntimeInspector) InspectReleaseDirectories() InspectionCheck {
	releasesDir := ri.cfg.ReleasesDirPath()

	info, err := os.Stat(releasesDir)
	if err != nil {
		return InspectionCheck{
			Name:    "release_directories",
			Passed:  false,
			Details: fmt.Sprintf("releases directory %s: %v", releasesDir, err),
		}
	}
	if !info.IsDir() {
		return InspectionCheck{
			Name:    "release_directories",
			Passed:  false,
			Details: fmt.Sprintf("releases path %s exists but is not a directory", releasesDir),
		}
	}

	return InspectionCheck{
		Name:    "release_directories",
		Passed:  true,
		Details: fmt.Sprintf("releases directory exists at %s", releasesDir),
	}
}

// InspectSharedResources checks whether all shared resource directories
// (config, storage, logs, temp) exist on the filesystem. Uses
// runtime.SharedResourceManager to enumerate shared paths.
//
// Reference: TS-009-005, ADR-003 §8.5
func (ri *RuntimeInspector) InspectSharedResources() InspectionCheck {
	manager := runtime.NewSharedResourceManager(ri.cfg)
	sharedDirs := manager.AllSharedDirPaths()

	var missing []string
	for _, dir := range sharedDirs {
		info, err := os.Stat(dir)
		if err != nil {
			missing = append(missing, dir)
			continue
		}
		if !info.IsDir() {
			missing = append(missing, dir+" (not a directory)")
		}
	}

	if len(missing) > 0 {
		return InspectionCheck{
			Name:    "shared_resources",
			Passed:  false,
			Details: fmt.Sprintf("missing or invalid shared directories: %s", strings.Join(missing, "; ")),
		}
	}

	return InspectionCheck{
		Name:    "shared_resources",
		Passed:  true,
		Details: fmt.Sprintf("all %d shared resource directories exist", len(sharedDirs)),
	}
}

// InspectRuntimeConfig checks whether the runtime configuration file exists.
// It checks both the expected config path derived from InstallRoot and the
// server ConfigStore location as a fallback.
//
// Reference: TS-009-005, ADR-003 §8.5
func (ri *RuntimeInspector) InspectRuntimeConfig() InspectionCheck {
	// Check expected runtime config path: <install_root>/config.yaml
	configPath := filepath.Join(ri.cfg.InstallRoot, "config.yaml")
	_, err := os.Stat(configPath)
	if err == nil {
		return InspectionCheck{
			Name:    "runtime_config",
			Passed:  true,
			Details: fmt.Sprintf("runtime config found at %s", configPath),
		}
	}

	// Fallback: check server ConfigStore location.
	store := server.NewConfigStore(ri.cfg.InstallRoot)
	if store.Exists() {
		return InspectionCheck{
			Name:    "runtime_config",
			Passed:  true,
			Details: fmt.Sprintf("server config found at %s", store.ConfigPath()),
		}
	}

	return InspectionCheck{
		Name:    "runtime_config",
		Passed:  false,
		Details: fmt.Sprintf("no runtime config found at %s or %s", configPath, store.ConfigPath()),
	}
}

// Inspect runs all runtime inspection checks and returns a consolidated
// result. All checks are read-only — no filesystem state is modified.
//
// Reference: TS-009-005, ADR-003 §8.5, ADR-006 §5.2
func (ri *RuntimeInspector) Inspect() InspectionResult {
	result := NewInspectionResult("runtime")

	checks := []InspectionCheck{
		ri.InspectActiveSymlink(),
		ri.InspectReleaseDirectories(),
		ri.InspectSharedResources(),
		ri.InspectRuntimeConfig(),
	}

	for _, c := range checks {
		result.AddCheck(c.Name, c.Passed, c.Details)
	}

	return *result
}
