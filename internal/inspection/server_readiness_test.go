// Package inspection provides read-only diagnostic inspection capabilities
// for Anvil Runtime environments and configuration.
//
// Reference: TS-P9-10
package inspection

import (
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/server"
)

// helperSetupServerConfig creates a valid server config at the given root.
func helperSetupServerConfig(t *testing.T, root string) {
	t.Helper()
	store := server.NewConfigStore(root)
	cfg := server.DefaultServerConfig()
	cfg.Runtime.ID = "test-runtime"
	if err := store.Save(cfg); err != nil {
		t.Fatalf("save server config: %v", err)
	}
}

// helperRegisterProject registers a test project in the given server root.
func helperRegisterProject(t *testing.T, root, id, installRoot string) {
	t.Helper()
	store := server.NewRegistryStore(root)
	registry := server.ProjectRegistry{
		Project: server.ProjectSection{
			ID:          id,
			InstallRoot: installRoot,
		},
	}
	if err := store.Register(registry); err != nil {
		t.Fatalf("register project %s: %v", id, err)
	}
}

// TestNewServerReadinessInspector verifies that NewServerReadinessInspector
// creates a non-nil inspector.
//
// Reference: TS-P9-10
func TestNewServerReadinessInspector(t *testing.T) {
	dir := t.TempDir()
	inspector := NewServerReadinessInspector(dir)
	if inspector == nil {
		t.Fatal("NewServerReadinessInspector() returned nil")
	}
}

// TestServerReadinessInspector_InspectServerConfig_ExistsAndValid verifies
// that the server config check passes when a valid config exists.
//
// Reference: TS-P9-10
func TestServerReadinessInspector_InspectServerConfig_ExistsAndValid(t *testing.T) {
	dir := t.TempDir()
	helperSetupServerConfig(t, dir)

	inspector := NewServerReadinessInspector(dir)
	check := inspector.InspectServerConfig()

	if !check.Passed {
		t.Errorf("InspectServerConfig().Passed = false, want true; details: %s", check.Details)
	}
	if check.Name != "server_config" {
		t.Errorf("check.Name = %q, want %q", check.Name, "server_config")
	}
}

// TestServerReadinessInspector_InspectServerConfig_Missing verifies that
// the server config check fails when the config file does not exist.
//
// Reference: TS-P9-10
func TestServerReadinessInspector_InspectServerConfig_Missing(t *testing.T) {
	dir := t.TempDir()

	inspector := NewServerReadinessInspector(dir)
	check := inspector.InspectServerConfig()

	if check.Passed {
		t.Errorf("InspectServerConfig().Passed = true, want false (config missing)")
	}
}

// TestServerReadinessInspector_InspectServerConfig_Invalid verifies that
// the server config check fails when the config has missing required fields.
//
// Reference: TS-P9-10
func TestServerReadinessInspector_InspectServerConfig_Invalid(t *testing.T) {
	dir := t.TempDir()

	// Create a config with empty ID (invalid).
	store := server.NewConfigStore(dir)
	cfg := server.DefaultServerConfig()
	// ID is empty by default — invalid.
	if err := store.Save(cfg); err != nil {
		t.Fatalf("save server config: %v", err)
	}

	inspector := NewServerReadinessInspector(dir)
	check := inspector.InspectServerConfig()

	if check.Passed {
		t.Errorf("InspectServerConfig().Passed = true, want false (invalid config)")
	}
}

// TestServerReadinessInspector_InspectServerConfig_InvalidYAML verifies
// that the server config check fails when the YAML is malformed.
//
// Reference: TS-P9-10
func TestServerReadinessInspector_InspectServerConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()

	// Write invalid YAML.
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("{{invalid yaml"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	inspector := NewServerReadinessInspector(dir)
	check := inspector.InspectServerConfig()

	if check.Passed {
		t.Errorf("InspectServerConfig().Passed = true, want false (invalid YAML)")
	}
}

// TestServerReadinessInspector_InspectRegistryStore_Exists verifies that
// the registry store check passes when the projects directory exists.
//
// Reference: TS-P9-10
func TestServerReadinessInspector_InspectRegistryStore_Exists(t *testing.T) {
	dir := t.TempDir()

	// Create the projects directory.
	projectsDir := filepath.Join(dir, "projects")
	if err := os.MkdirAll(projectsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	inspector := NewServerReadinessInspector(dir)
	check := inspector.InspectRegistryStore()

	if !check.Passed {
		t.Errorf("InspectRegistryStore().Passed = false, want true; details: %s", check.Details)
	}
}

// TestServerReadinessInspector_InspectRegistryStore_Missing verifies that
// the registry store check fails when the projects directory does not exist.
//
// Reference: TS-P9-10
func TestServerReadinessInspector_InspectRegistryStore_Missing(t *testing.T) {
	dir := t.TempDir()

	inspector := NewServerReadinessInspector(dir)
	check := inspector.InspectRegistryStore()

	if check.Passed {
		t.Errorf("InspectRegistryStore().Passed = true, want false (dir missing)")
	}
}

// TestServerReadinessInspector_InspectRegistryStore_NotADir verifies that
// the check fails when the projects path exists but is not a directory.
//
// Reference: TS-P9-10
func TestServerReadinessInspector_InspectRegistryStore_NotADir(t *testing.T) {
	dir := t.TempDir()

	// Create "projects" as a file.
	projectsPath := filepath.Join(dir, "projects")
	if err := os.WriteFile(projectsPath, []byte("not a dir"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	inspector := NewServerReadinessInspector(dir)
	check := inspector.InspectRegistryStore()

	if check.Passed {
		t.Errorf("InspectRegistryStore().Passed = true, want false (not a dir)")
	}
}

// TestServerReadinessInspector_InspectProjectRegistries_Valid verifies
// that the project registries check passes when all registries are valid.
//
// Reference: TS-P9-10
func TestServerReadinessInspector_InspectProjectRegistries_Valid(t *testing.T) {
	dir := t.TempDir()

	installRoot := filepath.Join(dir, "apps", "my-app")
	if err := os.MkdirAll(installRoot, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	helperRegisterProject(t, dir, "my-app", installRoot)

	inspector := NewServerReadinessInspector(dir)
	check := inspector.InspectProjectRegistries()

	if !check.Passed {
		t.Errorf("InspectProjectRegistries().Passed = false, want true; details: %s", check.Details)
	}
}

// TestServerReadinessInspector_InspectProjectRegistries_NoProjects
// verifies that the check passes vacuously when no projects are registered.
//
// Reference: TS-P9-10
func TestServerReadinessInspector_InspectProjectRegistries_NoProjects(t *testing.T) {
	dir := t.TempDir()

	// Create projects directory but leave it empty.
	projectsDir := filepath.Join(dir, "projects")
	if err := os.MkdirAll(projectsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	inspector := NewServerReadinessInspector(dir)
	check := inspector.InspectProjectRegistries()

	if !check.Passed {
		t.Errorf("InspectProjectRegistries().Passed = false, want true (vacuous); details: %s", check.Details)
	}
}

// TestServerReadinessInspector_InspectProjectRegistries_InvalidYAML
// verifies that the check fails when a registry file contains invalid YAML.
//
// Reference: TS-P9-10
func TestServerReadinessInspector_InspectProjectRegistries_InvalidYAML(t *testing.T) {
	dir := t.TempDir()

	// Create projects directory with an invalid YAML file.
	projectsDir := filepath.Join(dir, "projects")
	if err := os.MkdirAll(projectsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectsDir, "bad.yaml"), []byte("{{invalid"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	inspector := NewServerReadinessInspector(dir)
	check := inspector.InspectProjectRegistries()

	if check.Passed {
		t.Errorf("InspectProjectRegistries().Passed = true, want false (invalid YAML)")
	}
}

// TestServerReadinessInspector_InspectProjectRegistries_MissingInstallRoot
// verifies that the check fails when a project registry is missing
// install_root.
//
// Reference: TS-P9-10
func TestServerReadinessInspector_InspectProjectRegistries_MissingInstallRoot(t *testing.T) {
	dir := t.TempDir()

	// Register a project with empty install_root (invalid).
	store := server.NewRegistryStore(dir)
	// Bypass validation to create an invalid registry file.
	if err := os.MkdirAll(store.ProjectsDir(), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data := []byte("project:\n  id: my-app\n  install_root: \"\"\n")
	if err := os.WriteFile(store.ProjectPath("my-app"), data, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	inspector := NewServerReadinessInspector(dir)
	check := inspector.InspectProjectRegistries()

	if check.Passed {
		t.Errorf("InspectProjectRegistries().Passed = true, want false (missing install_root)")
	}
}

// TestServerReadinessInspector_InspectInstallRoots_AllExist verifies that
// the install roots check passes when all project install roots exist.
//
// Reference: TS-P9-10
func TestServerReadinessInspector_InspectInstallRoots_AllExist(t *testing.T) {
	dir := t.TempDir()

	installRoot := filepath.Join(dir, "apps", "my-app")
	if err := os.MkdirAll(installRoot, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	helperRegisterProject(t, dir, "my-app", installRoot)

	inspector := NewServerReadinessInspector(dir)
	check := inspector.InspectInstallRoots()

	if !check.Passed {
		t.Errorf("InspectInstallRoots().Passed = false, want true; details: %s", check.Details)
	}
}

// TestServerReadinessInspector_InspectInstallRoots_Missing verifies that
// the check fails when a project install root does not exist.
//
// Reference: TS-P9-10
func TestServerReadinessInspector_InspectInstallRoots_Missing(t *testing.T) {
	dir := t.TempDir()

	// Register a project with a non-existent install root.
	installRoot := filepath.Join(dir, "nonexistent", "app")
	helperRegisterProject(t, dir, "my-app", installRoot)

	inspector := NewServerReadinessInspector(dir)
	check := inspector.InspectInstallRoots()

	if check.Passed {
		t.Errorf("InspectInstallRoots().Passed = true, want false (install root missing)")
	}
}

// TestServerReadinessInspector_InspectInstallRoots_RelativePath verifies
// that the check fails when install_root is a relative path.
//
// Reference: TS-P9-10
func TestServerReadinessInspector_InspectInstallRoots_RelativePath(t *testing.T) {
	dir := t.TempDir()

	// Register a project with a relative install root.
	store := server.NewRegistryStore(dir)
	if err := os.MkdirAll(store.ProjectsDir(), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data := []byte("project:\n  id: my-app\n  install_root: relative/path\n")
	if err := os.WriteFile(store.ProjectPath("my-app"), data, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	inspector := NewServerReadinessInspector(dir)
	check := inspector.InspectInstallRoots()

	if check.Passed {
		t.Errorf("InspectInstallRoots().Passed = true, want false (relative path)")
	}
}

// TestServerReadinessInspector_InspectInstallRoots_NoProjects verifies
// that the check passes vacuously when no projects are registered.
//
// Reference: TS-P9-10
func TestServerReadinessInspector_InspectInstallRoots_NoProjects(t *testing.T) {
	dir := t.TempDir()

	inspector := NewServerReadinessInspector(dir)
	check := inspector.InspectInstallRoots()

	if !check.Passed {
		t.Errorf("InspectInstallRoots().Passed = false, want true (vacuous); details: %s", check.Details)
	}
}

// TestServerReadinessInspector_Inspect_AllPassing verifies that Inspect
// returns a passing result when all checks pass.
//
// Reference: TS-P9-10
func TestServerReadinessInspector_Inspect_AllPassing(t *testing.T) {
	dir := t.TempDir()
	helperSetupServerConfig(t, dir)

	installRoot := filepath.Join(dir, "apps", "my-app")
	if err := os.MkdirAll(installRoot, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	helperRegisterProject(t, dir, "my-app", installRoot)

	inspector := NewServerReadinessInspector(dir)
	result := inspector.Inspect()

	if !result.Passed {
		t.Errorf("Inspect().Passed = false, want true")
		for _, c := range result.Checks {
			if !c.Passed {
				t.Logf("  failed check: %s — %s", c.Name, c.Details)
			}
		}
	}
	if result.Component != "server_readiness" {
		t.Errorf("Component = %q, want %q", result.Component, "server_readiness")
	}
	if len(result.Checks) != 4 {
		t.Errorf("len(Checks) = %d, want 4", len(result.Checks))
	}
}

// TestServerReadinessInspector_Inspect_AllFailing verifies that Inspect
// returns a failing result when server config is missing.
//
// Reference: TS-P9-10
func TestServerReadinessInspector_Inspect_AllFailing(t *testing.T) {
	dir := t.TempDir()

	// Create a registry store with an invalid project to ensure all checks fail.
	projectsDir := filepath.Join(dir, "projects")
	if err := os.MkdirAll(projectsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Write invalid YAML to ensure project registries check fails.
	if err := os.WriteFile(filepath.Join(projectsDir, "bad.yaml"), []byte("{{invalid"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	inspector := NewServerReadinessInspector(dir)
	result := inspector.Inspect()

	if result.Passed {
		t.Errorf("Inspect().Passed = true, want false (config missing, invalid registries)")
	}

	checkMap := make(map[string]bool)
	for _, c := range result.Checks {
		checkMap[c.Name] = c.Passed
	}

	// server_config should fail (missing).
	if checkMap["server_config"] {
		t.Error("server_config check should fail (config missing)")
	}
	// registry_store should pass (directory exists).
	if !checkMap["registry_store"] {
		t.Error("registry_store check should pass (directory exists)")
	}
	// project_registries should fail (invalid YAML).
	if checkMap["project_registries"] {
		t.Error("project_registries check should fail (invalid YAML)")
	}
}

// TestServerReadinessInspector_Inspect_PartialSetup verifies behavior
// with a partial server setup (config exists but no registry).
//
// Reference: TS-P9-10
func TestServerReadinessInspector_Inspect_PartialSetup(t *testing.T) {
	dir := t.TempDir()
	helperSetupServerConfig(t, dir)

	inspector := NewServerReadinessInspector(dir)
	result := inspector.Inspect()

	// Should fail because registry store is missing.
	if result.Passed {
		t.Errorf("Inspect().Passed = true, want false (partial setup)")
	}

	checkMap := make(map[string]bool)
	for _, c := range result.Checks {
		checkMap[c.Name] = c.Passed
	}

	if !checkMap["server_config"] {
		t.Error("server_config check should pass")
	}
	if checkMap["registry_store"] {
		t.Error("registry_store check should fail (no projects dir)")
	}
}

// TestServerReadinessInspector_SecureExecution verifies that running the
// inspector does not create or modify any files.
//
// Reference: TS-P9-10
func TestServerReadinessInspector_SecureExecution(t *testing.T) {
	dir := t.TempDir()

	entriesBefore, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read directory before: %v", err)
	}

	inspector := NewServerReadinessInspector(dir)
	_ = inspector.Inspect()

	entriesAfter, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read directory after: %v", err)
	}

	if len(entriesBefore) != len(entriesAfter) {
		t.Errorf("directory contents changed: before=%d entries, after=%d entries",
			len(entriesBefore), len(entriesAfter))
	}
}
