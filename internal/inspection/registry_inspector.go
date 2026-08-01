// Package inspection provides read-only diagnostic inspection capabilities
// for Anvil Runtime environments and configuration.
//
// Reference: TS-P9-11, ADR-013, ADR-006 §5.2
package inspection

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"maleolabs.com/anvil/internal/server"
)

// RegistryInspector performs read-only diagnostic inspections on the
// integrity of the server project registry store. It validates file
// existence, YAML validity, schema compliance, and cross-file consistency
// without modifying any registry files.
//
// Reference: TS-P9-11, ADR-013, ADR-006 §5.2
type RegistryInspector struct {
	serverRoot string
}

// NewRegistryInspector creates a RegistryInspector that inspects the
// registry store at the given server root path.
//
// Reference: TS-P9-11
func NewRegistryInspector(serverRoot string) *RegistryInspector {
	return &RegistryInspector{serverRoot: serverRoot}
}

// InspectRegistryDirectory checks whether the registry projects directory
// exists and is a valid directory on the filesystem.
//
// Reference: TS-P9-11, ADR-013
func (ri *RegistryInspector) InspectRegistryDirectory() InspectionCheck {
	store := server.NewRegistryStore(ri.serverRoot)
	projectsDir := store.ProjectsDir()

	info, err := os.Stat(projectsDir)
	if err != nil {
		return InspectionCheck{
			Name:    "registry_directory",
			Passed:  false,
			Details: fmt.Sprintf("registry directory %s: %v", projectsDir, err),
		}
	}
	if !info.IsDir() {
		return InspectionCheck{
			Name:    "registry_directory",
			Passed:  false,
			Details: fmt.Sprintf("registry path %s exists but is not a directory", projectsDir),
		}
	}

	return InspectionCheck{
		Name:    "registry_directory",
		Passed:  true,
		Details: fmt.Sprintf("registry directory exists at %s", projectsDir),
	}
}

// InspectRegistryFiles validates each YAML file in the registry directory.
// For each file, it checks:
//  1. File is readable
//  2. File contains valid YAML
//  3. YAML unmarshals to ProjectRegistry
//  4. ProjectRegistry.Validate() passes
//  5. Project.InstallRoot path exists on filesystem
//
// Reference: TS-P9-11, ADR-013
func (ri *RegistryInspector) InspectRegistryFiles() InspectionCheck {
	store := server.NewRegistryStore(ri.serverRoot)
	projectsDir := store.ProjectsDir()

	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return InspectionCheck{
			Name:    "registry_files",
			Passed:  false,
			Details: fmt.Sprintf("cannot read registry directory %s: %v", projectsDir, err),
		}
	}

	// Filter to .yaml files only.
	var yamlFiles []os.DirEntry
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) == ".yaml" {
			yamlFiles = append(yamlFiles, entry)
		}
	}

	if len(yamlFiles) == 0 {
		return InspectionCheck{
			Name:    "registry_files",
			Passed:  true,
			Details: "no registry files found; file validation not applicable",
		}
	}

	var invalid []string
	for _, entry := range yamlFiles {
		filePath := filepath.Join(projectsDir, entry.Name())
		projectID := entry.Name()[:len(entry.Name())-len(".yaml")]

		// Check 1: File is readable.
		data, err := os.ReadFile(filePath)
		if err != nil {
			invalid = append(invalid, fmt.Sprintf("%s (unreadable: %v)", projectID, err))
			continue
		}

		// Check 2: File contains valid YAML.
		// Check 3: YAML unmarshals to ProjectRegistry.
		var registry server.ProjectRegistry
		if err := yaml.Unmarshal(data, &registry); err != nil {
			invalid = append(invalid, fmt.Sprintf("%s (invalid YAML: %v)", projectID, err))
			continue
		}

		// Check 4: ProjectRegistry.Validate() passes.
		if err := registry.Validate(); err != nil {
			invalid = append(invalid, fmt.Sprintf("%s (validation error: %v)", projectID, err))
			continue
		}

		// Check 5: Project.InstallRoot path exists on filesystem.
		installRoot := registry.Project.InstallRoot
		if installRoot == "" {
			invalid = append(invalid, fmt.Sprintf("%s (install_root empty)", projectID))
			continue
		}
		if !filepath.IsAbs(installRoot) {
			invalid = append(invalid, fmt.Sprintf("%s (install_root not absolute: %s)", projectID, installRoot))
			continue
		}
		if _, err := os.Stat(installRoot); err != nil {
			invalid = append(invalid, fmt.Sprintf("%s (install_root %s: %v)", projectID, installRoot, err))
		}
	}

	if len(invalid) > 0 {
		return InspectionCheck{
			Name:    "registry_files",
			Passed:  false,
			Details: fmt.Sprintf("invalid registry files: %s", strings.Join(invalid, "; ")),
		}
	}

	return InspectionCheck{
		Name:    "registry_files",
		Passed:  true,
		Details: fmt.Sprintf("all %d registry files are valid", len(yamlFiles)),
	}
}

// InspectRegistryConsistency checks cross-file consistency of the registry:
//  1. No duplicate project IDs (by YAML content, not filename)
//  2. All install_root paths are absolute
//  3. No orphaned directories (directories without matching YAML)
//
// Reference: TS-P9-11, ADR-013
func (ri *RegistryInspector) InspectRegistryConsistency() InspectionCheck {
	store := server.NewRegistryStore(ri.serverRoot)
	projectsDir := store.ProjectsDir()

	ids, err := store.List()
	if err != nil {
		return InspectionCheck{
			Name:    "registry_consistency",
			Passed:  false,
			Details: fmt.Sprintf("cannot list project registries: %v", err),
		}
	}

	if len(ids) == 0 {
		return InspectionCheck{
			Name:    "registry_consistency",
			Passed:  true,
			Details: "no projects registered; consistency check not applicable",
		}
	}

	var issues []string

	// Check 1: No duplicate project IDs (by YAML content).
	// Load each file and check the actual project.id field.
	seen := make(map[string]string) // project.id -> filename
	for _, id := range ids {
		registry, err := store.Load(id)
		if err != nil {
			issues = append(issues, fmt.Sprintf("%s (load error: %v)", id, err))
			continue
		}
		projectID := registry.Project.ID
		if projectID == "" {
			continue
		}
		if existingFile, exists := seen[projectID]; exists {
			issues = append(issues, fmt.Sprintf("duplicate project ID %q found in %s.yaml and %s.yaml", projectID, existingFile, id))
		} else {
			seen[projectID] = id
		}
	}

	// Check 2: All install_root paths are absolute.
	for _, id := range ids {
		registry, err := store.Load(id)
		if err != nil {
			// Already reported above.
			continue
		}
		if registry.Project.InstallRoot != "" && !filepath.IsAbs(registry.Project.InstallRoot) {
			issues = append(issues, fmt.Sprintf("%s has relative install_root: %s", id, registry.Project.InstallRoot))
		}
	}

	// Check 3: No orphaned directories (directories in projects/ without matching YAML).
	entries, err := os.ReadDir(projectsDir)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			// A directory exists — check if there's a matching YAML file.
			yamlPath := filepath.Join(projectsDir, entry.Name()+".yaml")
			if _, err := os.Stat(yamlPath); err != nil {
				issues = append(issues, fmt.Sprintf("orphaned directory: %s (no matching %s.yaml)", entry.Name(), entry.Name()))
			}
		}
	}

	if len(issues) > 0 {
		return InspectionCheck{
			Name:    "registry_consistency",
			Passed:  false,
			Details: fmt.Sprintf("consistency issues: %s", strings.Join(issues, "; ")),
		}
	}

	return InspectionCheck{
		Name:    "registry_consistency",
		Passed:  true,
		Details: "registry is consistent",
	}
}

// Inspect runs all registry integrity inspection checks and returns a
// consolidated result. All checks are read-only — no registry files are
// created or modified.
//
// Reference: TS-P9-11, ADR-013, ADR-006 §5.2
func (ri *RegistryInspector) Inspect() InspectionResult {
	result := NewInspectionResult("registry")

	checks := []InspectionCheck{
		ri.InspectRegistryDirectory(),
		ri.InspectRegistryFiles(),
		ri.InspectRegistryConsistency(),
	}

	for _, c := range checks {
		result.AddCheck(c.Name, c.Passed, c.Details)
	}

	return *result
}
