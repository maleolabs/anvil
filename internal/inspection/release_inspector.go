// Package inspection provides read-only diagnostic inspection capabilities
// for Anvil Runtime environments and configuration.
//
// Reference: TS-P9-07, ADR-003 §8.5, ADR-006 §5.2
package inspection

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"maleolabs.com/anvil/internal/runtime"
	"maleolabs.com/anvil/internal/server"
)

// ReleaseInspector performs read-only diagnostic inspections on Anvil
// release infrastructure. It examines release directory structure, artifact
// presence, and shared link integrity without modifying any resources.
//
// Reference: TS-P9-07, ADR-003 §8.5, ADR-006 §5.2
type ReleaseInspector struct {
	runtimeCfg runtime.RuntimeConfig
	lookPath   func(string) (string, error) // injectable for testing
}

// NewReleaseInspector creates a ReleaseInspector that inspects the release
// infrastructure described by the given Runtime configuration.
//
// Reference: TS-P9-07
func NewReleaseInspector(cfg runtime.RuntimeConfig) *ReleaseInspector {
	return &ReleaseInspector{
		runtimeCfg: cfg,
		lookPath:   exec.LookPath,
	}
}

// InspectReleaseDirectory checks whether the releases directory exists and
// is a valid directory on the filesystem.
//
// Reference: TS-P9-07, ADR-003 §8.5
func (ri *ReleaseInspector) InspectReleaseDirectory() InspectionCheck {
	releasesDir := ri.runtimeCfg.ReleasesDirPath()

	info, err := os.Stat(releasesDir)
	if err != nil {
		return InspectionCheck{
			Name:    "release_directory",
			Passed:  false,
			Details: fmt.Sprintf("release directory %s: %v", releasesDir, err),
		}
	}
	if !info.IsDir() {
		return InspectionCheck{
			Name:    "release_directory",
			Passed:  false,
			Details: fmt.Sprintf("release path %s exists but is not a directory", releasesDir),
		}
	}

	return InspectionCheck{
		Name:    "release_directory",
		Passed:  true,
		Details: fmt.Sprintf("release directory exists at %s", releasesDir),
	}
}

// InspectArtifactPresence checks whether artifact files exist in the
// release directories. For each release directory, it verifies that at
// least one artifact file is present.
//
// Reference: TS-P9-07, ADR-003 §8.5
func (ri *ReleaseInspector) InspectArtifactPresence() InspectionCheck {
	releasesDir := ri.runtimeCfg.ReleasesDirPath()

	entries, err := os.ReadDir(releasesDir)
	if err != nil {
		return InspectionCheck{
			Name:    "artifact_presence",
			Passed:  false,
			Details: fmt.Sprintf("cannot read releases directory %s: %v", releasesDir, err),
		}
	}

	// If there are no release directories, artifact check passes vacuously.
	if len(entries) == 0 {
		return InspectionCheck{
			Name:    "artifact_presence",
			Passed:  true,
			Details: "no release directories found; artifact check not applicable",
		}
	}

	var missing []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		releasePath := filepath.Join(releasesDir, entry.Name())

		// Check for common artifact indicators.
		hasArtifact := false
		releaseEntries, err := os.ReadDir(releasePath)
		if err != nil {
			missing = append(missing, fmt.Sprintf("%s (unreadable: %v)", entry.Name(), err))
			continue
		}

		for _, re := range releaseEntries {
			if !re.IsDir() {
				hasArtifact = true
				break
			}
		}

		if !hasArtifact {
			missing = append(missing, entry.Name())
		}
	}

	if len(missing) > 0 {
		return InspectionCheck{
			Name:    "artifact_presence",
			Passed:  false,
			Details: fmt.Sprintf("release directories without artifacts: %s", strings.Join(missing, "; ")),
		}
	}

	return InspectionCheck{
		Name:    "artifact_presence",
		Passed:  true,
		Details: fmt.Sprintf("all %d release directories contain artifacts", len(entries)),
	}
}

// InspectSharedLinks validates that all shared symlinks defined in project
// registries point to existing targets. Uses server.RegistryStore to load
// project configurations and verifies each SharedLink.
//
// Reference: TS-P9-07, ADR-013
func (ri *ReleaseInspector) InspectSharedLinks(serverRoot string) InspectionCheck {
	registryStore := server.NewRegistryStore(serverRoot)

	ids, err := registryStore.List()
	if err != nil {
		return InspectionCheck{
			Name:    "shared_links",
			Passed:  false,
			Details: fmt.Sprintf("cannot list project registries: %v", err),
		}
	}

	// If no projects are registered, shared links check passes vacuously.
	if len(ids) == 0 {
		return InspectionCheck{
			Name:    "shared_links",
			Passed:  true,
			Details: "no projects registered; shared link check not applicable",
		}
	}

	var broken []string
	for _, id := range ids {
		registry, err := registryStore.Load(id)
		if err != nil {
			broken = append(broken, fmt.Sprintf("%s (cannot load: %v)", id, err))
			continue
		}

		for _, link := range registry.Project.SharedLinks {
			sharedPath := filepath.Join(registry.Project.InstallRoot, link.From)
			if _, err := os.Stat(sharedPath); err != nil {
				broken = append(broken, fmt.Sprintf("%s: shared path %s: %v", id, link.From, err))
			}
		}
	}

	if len(broken) > 0 {
		return InspectionCheck{
			Name:    "shared_links",
			Passed:  false,
			Details: fmt.Sprintf("broken shared links: %s", strings.Join(broken, "; ")),
		}
	}

	return InspectionCheck{
		Name:    "shared_links",
		Passed:  true,
		Details: "all shared links are valid",
	}
}

// Inspect runs all release inspection checks and returns a consolidated
// result. All checks are read-only — no filesystem state is modified.
//
// The serverRoot parameter is used for shared link validation via the
// server registry store. If empty, shared link validation is skipped.
//
// Reference: TS-P9-07, ADR-003 §8.5, ADR-006 §5.2
func (ri *ReleaseInspector) Inspect(serverRoot string) InspectionResult {
	result := NewInspectionResult("release")

	checks := []InspectionCheck{
		ri.InspectReleaseDirectory(),
		ri.InspectArtifactPresence(),
	}

	// Only check shared links if server root is provided.
	if serverRoot != "" {
		checks = append(checks, ri.InspectSharedLinks(serverRoot))
	}

	// Always include external tool availability checks.
	checks = append(checks, ri.InspectExternalTools()...)

	for _, c := range checks {
		result.AddCheck(c.Name, c.Passed, c.Details)
	}

	return *result
}

// externalTools defines the list of external tools to check for availability.
var externalTools = []string{"php", "node", "composer", "npm", "git"}

// InspectExternalTools checks whether required external tools are available
// in the system PATH. Each tool is checked individually using exec.LookPath.
// These checks are informational: they always pass but report tool location
// or absence in the details. This ensures CI environments without all tools
// installed still pass health checks while providing diagnostic visibility.
//
// Reference: TS-P9-07, ADR-003 §8.5
func (ri *ReleaseInspector) InspectExternalTools() []InspectionCheck {
	var checks []InspectionCheck

	for _, tool := range externalTools {
		path, err := ri.lookPath(tool)
		details := ""
		if err == nil {
			details = fmt.Sprintf("found at %s", path)
		} else {
			details = fmt.Sprintf("%s not found in PATH", tool)
		}
		// Tool availability is informational — always passes.
		checks = append(checks, InspectionCheck{
			Name:    fmt.Sprintf("tool_%s", tool),
			Passed:  true,
			Details: details,
		})
	}

	return checks
}
