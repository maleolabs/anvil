// Package inspection provides read-only diagnostic inspection capabilities
// for Anvil Runtime environments and configuration.
//
// Reference: TS-P9-10, ADR-003 §8.5, ADR-013
package inspection

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"maleolabs.com/anvil/internal/server"
)

// ServerReadinessInspector performs read-only diagnostic inspections on
// Anvil Server Runtime configuration. It examines server config store,
// registry store, and project registries without modifying any resources.
//
// Reference: TS-P9-10, ADR-013, ADR-006 §5.2
type ServerReadinessInspector struct {
	serverRoot string
}

// NewServerReadinessInspector creates a ServerReadinessInspector that
// inspects the server configuration at the given root path.
//
// Reference: TS-P9-10
func NewServerReadinessInspector(serverRoot string) *ServerReadinessInspector {
	return &ServerReadinessInspector{serverRoot: serverRoot}
}

// InspectServerConfig validates that the server ConfigStore exists and
// contains a valid ServerConfig. Uses server.ConfigStore for reading
// and server.ValidateServerConfig for validation.
//
// Reference: TS-P9-10, ADR-013
func (si *ServerReadinessInspector) InspectServerConfig() InspectionCheck {
	store := server.NewConfigStore(si.serverRoot)

	if !store.Exists() {
		return InspectionCheck{
			Name:    "server_config",
			Passed:  false,
			Details: fmt.Sprintf("server config not found at %s", store.ConfigPath()),
		}
	}

	cfg, err := store.Load()
	if err != nil {
		return InspectionCheck{
			Name:    "server_config",
			Passed:  false,
			Details: fmt.Sprintf("cannot load server config: %v", err),
		}
	}

	if err := server.ValidateServerConfig(*cfg); err != nil {
		return InspectionCheck{
			Name:    "server_config",
			Passed:  false,
			Details: fmt.Sprintf("server config invalid: %v", err),
		}
	}

	return InspectionCheck{
		Name:    "server_config",
		Passed:  true,
		Details: fmt.Sprintf("server config valid at %s (id=%q)", store.ConfigPath(), cfg.Runtime.ID),
	}
}

// InspectRegistryStore validates that the RegistryStore projects directory
// exists on the filesystem.
//
// Reference: TS-P9-10, ADR-013
func (si *ServerReadinessInspector) InspectRegistryStore() InspectionCheck {
	store := server.NewRegistryStore(si.serverRoot)
	projectsDir := store.ProjectsDir()

	info, err := os.Stat(projectsDir)
	if err != nil {
		return InspectionCheck{
			Name:    "registry_store",
			Passed:  false,
			Details: fmt.Sprintf("registry store directory %s: %v", projectsDir, err),
		}
	}
	if !info.IsDir() {
		return InspectionCheck{
			Name:    "registry_store",
			Passed:  false,
			Details: fmt.Sprintf("registry store path %s exists but is not a directory", projectsDir),
		}
	}

	return InspectionCheck{
		Name:    "registry_store",
		Passed:  true,
		Details: fmt.Sprintf("registry store directory exists at %s", projectsDir),
	}
}

// InspectProjectRegistries loads all project registries and validates each
// one. Uses server.RegistryStore.List and Load for reading, and
// server.ValidateProjectRegistry for validation.
//
// Reference: TS-P9-10, ADR-013
func (si *ServerReadinessInspector) InspectProjectRegistries() InspectionCheck {
	store := server.NewRegistryStore(si.serverRoot)

	ids, err := store.List()
	if err != nil {
		return InspectionCheck{
			Name:    "project_registries",
			Passed:  false,
			Details: fmt.Sprintf("cannot list project registries: %v", err),
		}
	}

	// If no projects are registered, this check passes vacuously.
	if len(ids) == 0 {
		return InspectionCheck{
			Name:    "project_registries",
			Passed:  true,
			Details: "no projects registered; registry validation not applicable",
		}
	}

	var invalid []string
	for _, id := range ids {
		registry, err := store.Load(id)
		if err != nil {
			invalid = append(invalid, fmt.Sprintf("%s (load error: %v)", id, err))
			continue
		}

		if err := registry.Validate(); err != nil {
			invalid = append(invalid, fmt.Sprintf("%s (validation error: %v)", id, err))
		}
	}

	if len(invalid) > 0 {
		return InspectionCheck{
			Name:    "project_registries",
			Passed:  false,
			Details: fmt.Sprintf("invalid project registries: %s", strings.Join(invalid, "; ")),
		}
	}

	return InspectionCheck{
		Name:    "project_registries",
		Passed:  true,
		Details: fmt.Sprintf("all %d project registries are valid", len(ids)),
	}
}

// InspectInstallRoots validates that all registered project install_root
// paths exist on the filesystem.
//
// Reference: TS-P9-10, ADR-013
func (si *ServerReadinessInspector) InspectInstallRoots() InspectionCheck {
	store := server.NewRegistryStore(si.serverRoot)

	ids, err := store.List()
	if err != nil {
		return InspectionCheck{
			Name:    "install_roots",
			Passed:  false,
			Details: fmt.Sprintf("cannot list project registries: %v", err),
		}
	}

	if len(ids) == 0 {
		return InspectionCheck{
			Name:    "install_roots",
			Passed:  true,
			Details: "no projects registered; install root check not applicable",
		}
	}

	var missing []string
	for _, id := range ids {
		registry, err := store.Load(id)
		if err != nil {
			missing = append(missing, fmt.Sprintf("%s (load error: %v)", id, err))
			continue
		}

		installRoot := registry.Project.InstallRoot
		if installRoot == "" {
			missing = append(missing, fmt.Sprintf("%s (install_root empty)", id))
			continue
		}

		if !filepath.IsAbs(installRoot) {
			missing = append(missing, fmt.Sprintf("%s (install_root not absolute: %s)", id, installRoot))
			continue
		}

		info, err := os.Stat(installRoot)
		if err != nil {
			missing = append(missing, fmt.Sprintf("%s (install_root %s: %v)", id, installRoot, err))
			continue
		}
		if !info.IsDir() {
			missing = append(missing, fmt.Sprintf("%s (install_root %s is not a directory)", id, installRoot))
		}
	}

	if len(missing) > 0 {
		return InspectionCheck{
			Name:    "install_roots",
			Passed:  false,
			Details: fmt.Sprintf("invalid install roots: %s", strings.Join(missing, "; ")),
		}
	}

	return InspectionCheck{
		Name:    "install_roots",
		Passed:  true,
		Details: fmt.Sprintf("all %d project install roots are valid", len(ids)),
	}
}

// Inspect runs all server readiness inspection checks and returns a
// consolidated result. All checks are read-only — no config files are
// created or modified.
//
// Reference: TS-P9-10, ADR-013, ADR-006 §5.2
func (si *ServerReadinessInspector) Inspect() InspectionResult {
	result := NewInspectionResult("server_readiness")

	checks := []InspectionCheck{
		si.InspectServerConfig(),
		si.InspectRegistryStore(),
		si.InspectProjectRegistries(),
		si.InspectInstallRoots(),
	}

	for _, c := range checks {
		result.AddCheck(c.Name, c.Passed, c.Details)
	}

	return *result
}
