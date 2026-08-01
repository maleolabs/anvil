// Package inspection provides read-only diagnostic inspection capabilities
// for Anvil Runtime environments and configuration.
//
// Reference: TS-P9-11
package inspection

import (
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/server"
)

// helperSetupRegistryDir creates the projects directory under the given root.
func helperSetupRegistryDir(t *testing.T, root string) {
	t.Helper()
	store := server.NewRegistryStore(root)
	if err := os.MkdirAll(store.ProjectsDir(), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
}

// helperWriteRegistryFile writes a raw YAML file to the registry directory.
func helperWriteRegistryFile(t *testing.T, root, projectID, content string) {
	t.Helper()
	store := server.NewRegistryStore(root)
	if err := os.WriteFile(store.ProjectPath(projectID), []byte(content), 0644); err != nil {
		t.Fatalf("write registry file: %v", err)
	}
}

// TestNewRegistryInspector verifies that NewRegistryInspector creates a
// non-nil inspector.
//
// Reference: TS-P9-11
func TestNewRegistryInspector(t *testing.T) {
	dir := t.TempDir()
	inspector := NewRegistryInspector(dir)
	if inspector == nil {
		t.Fatal("NewRegistryInspector() returned nil")
	}
}

// TestRegistryInspector_InspectRegistryDirectory_Exists verifies that the
// registry directory check passes when the projects directory exists.
//
// Reference: TS-P9-11
func TestRegistryInspector_InspectRegistryDirectory_Exists(t *testing.T) {
	dir := t.TempDir()
	helperSetupRegistryDir(t, dir)

	inspector := NewRegistryInspector(dir)
	check := inspector.InspectRegistryDirectory()

	if !check.Passed {
		t.Errorf("InspectRegistryDirectory().Passed = false, want true; details: %s", check.Details)
	}
	if check.Name != "registry_directory" {
		t.Errorf("check.Name = %q, want %q", check.Name, "registry_directory")
	}
}

// TestRegistryInspector_InspectRegistryDirectory_Missing verifies that the
// check fails when the projects directory does not exist.
//
// Reference: TS-P9-11
func TestRegistryInspector_InspectRegistryDirectory_Missing(t *testing.T) {
	dir := t.TempDir()

	inspector := NewRegistryInspector(dir)
	check := inspector.InspectRegistryDirectory()

	if check.Passed {
		t.Errorf("InspectRegistryDirectory().Passed = true, want false (dir missing)")
	}
}

// TestRegistryInspector_InspectRegistryDirectory_NotADir verifies that the
// check fails when the projects path exists but is not a directory.
//
// Reference: TS-P9-11
func TestRegistryInspector_InspectRegistryDirectory_NotADir(t *testing.T) {
	dir := t.TempDir()

	projectsPath := filepath.Join(dir, "projects")
	if err := os.WriteFile(projectsPath, []byte("not a dir"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	inspector := NewRegistryInspector(dir)
	check := inspector.InspectRegistryDirectory()

	if check.Passed {
		t.Errorf("InspectRegistryDirectory().Passed = true, want false (not a dir)")
	}
}

// TestRegistryInspector_InspectRegistryFiles_ValidFiles verifies that the
// registry files check passes when all YAML files are valid.
//
// Reference: TS-P9-11
func TestRegistryInspector_InspectRegistryFiles_ValidFiles(t *testing.T) {
	dir := t.TempDir()

	installRoot := filepath.Join(dir, "apps", "my-app")
	if err := os.MkdirAll(installRoot, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	store := server.NewRegistryStore(dir)
	registry := server.ProjectRegistry{
		Project: server.ProjectSection{
			ID:          "my-app",
			InstallRoot: installRoot,
		},
	}
	if err := store.Register(registry); err != nil {
		t.Fatalf("register: %v", err)
	}

	inspector := NewRegistryInspector(dir)
	check := inspector.InspectRegistryFiles()

	if !check.Passed {
		t.Errorf("InspectRegistryFiles().Passed = false, want true; details: %s", check.Details)
	}
}

// TestRegistryInspector_InspectRegistryFiles_NoFiles verifies that the
// check passes vacuously when no YAML files exist.
//
// Reference: TS-P9-11
func TestRegistryInspector_InspectRegistryFiles_NoFiles(t *testing.T) {
	dir := t.TempDir()
	helperSetupRegistryDir(t, dir)

	inspector := NewRegistryInspector(dir)
	check := inspector.InspectRegistryFiles()

	if !check.Passed {
		t.Errorf("InspectRegistryFiles().Passed = false, want true (vacuous); details: %s", check.Details)
	}
}

// TestRegistryInspector_InspectRegistryFiles_Unreadable verifies that the
// check fails when a YAML file cannot be read.
//
// Reference: TS-P9-11
func TestRegistryInspector_InspectRegistryFiles_Unreadable(t *testing.T) {
	dir := t.TempDir()
	helperSetupRegistryDir(t, dir)

	// Create a file and remove read permissions.
	store := server.NewRegistryStore(dir)
	filePath := store.ProjectPath("unreadable")
	if err := os.WriteFile(filePath, []byte("project:\n  id: test\n  install_root: /tmp\n"), 0000); err != nil {
		t.Fatalf("write: %v", err)
	}

	inspector := NewRegistryInspector(dir)
	check := inspector.InspectRegistryFiles()

	if check.Passed {
		t.Errorf("InspectRegistryFiles().Passed = true, want false (unreadable file)")
	}
}

// TestRegistryInspector_InspectRegistryFiles_InvalidYAML verifies that the
// check fails when a file contains invalid YAML.
//
// Reference: TS-P9-11
func TestRegistryInspector_InspectRegistryFiles_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	helperSetupRegistryDir(t, dir)
	helperWriteRegistryFile(t, dir, "bad", "{{invalid yaml")

	inspector := NewRegistryInspector(dir)
	check := inspector.InspectRegistryFiles()

	if check.Passed {
		t.Errorf("InspectRegistryFiles().Passed = true, want false (invalid YAML)")
	}
}

// TestRegistryInspector_InspectRegistryFiles_InvalidRegistry verifies that
// the check fails when YAML is valid but ProjectRegistry validation fails.
//
// Reference: TS-P9-11
func TestRegistryInspector_InspectRegistryFiles_InvalidRegistry(t *testing.T) {
	dir := t.TempDir()
	helperSetupRegistryDir(t, dir)
	helperWriteRegistryFile(t, dir, "no-id", "project:\n  install_root: /tmp\n")

	inspector := NewRegistryInspector(dir)
	check := inspector.InspectRegistryFiles()

	if check.Passed {
		t.Errorf("InspectRegistryFiles().Passed = true, want false (missing ID)")
	}
}

// TestRegistryInspector_InspectRegistryFiles_InstallRootMissing verifies
// that the check fails when install_root does not exist on filesystem.
//
// Reference: TS-P9-11
func TestRegistryInspector_InspectRegistryFiles_InstallRootMissing(t *testing.T) {
	dir := t.TempDir()
	helperSetupRegistryDir(t, dir)
	helperWriteRegistryFile(t, dir, "my-app", "project:\n  id: my-app\n  install_root: /nonexistent/path\n")

	inspector := NewRegistryInspector(dir)
	check := inspector.InspectRegistryFiles()

	if check.Passed {
		t.Errorf("InspectRegistryFiles().Passed = true, want false (install_root missing)")
	}
}

// TestRegistryInspector_InspectRegistryFiles_RelativeInstallRoot verifies
// that the check fails when install_root is a relative path.
//
// Reference: TS-P9-11
func TestRegistryInspector_InspectRegistryFiles_RelativeInstallRoot(t *testing.T) {
	dir := t.TempDir()
	helperSetupRegistryDir(t, dir)
	helperWriteRegistryFile(t, dir, "my-app", "project:\n  id: my-app\n  install_root: relative/path\n")

	inspector := NewRegistryInspector(dir)
	check := inspector.InspectRegistryFiles()

	if check.Passed {
		t.Errorf("InspectRegistryFiles().Passed = true, want false (relative install_root)")
	}
}

// TestRegistryInspector_InspectRegistryConsistency_NoDuplicates verifies
// that the consistency check passes when there are no duplicate IDs.
//
// Reference: TS-P9-11
func TestRegistryInspector_InspectRegistryConsistency_NoDuplicates(t *testing.T) {
	dir := t.TempDir()

	installRoot1 := filepath.Join(dir, "apps", "app1")
	installRoot2 := filepath.Join(dir, "apps", "app2")
	if err := os.MkdirAll(installRoot1, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(installRoot2, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	store := server.NewRegistryStore(dir)
	if err := store.Register(server.ProjectRegistry{
		Project: server.ProjectSection{ID: "app1", InstallRoot: installRoot1},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := store.Register(server.ProjectRegistry{
		Project: server.ProjectSection{ID: "app2", InstallRoot: installRoot2},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	inspector := NewRegistryInspector(dir)
	check := inspector.InspectRegistryConsistency()

	if !check.Passed {
		t.Errorf("InspectRegistryConsistency().Passed = false, want true; details: %s", check.Details)
	}
}

// TestRegistryInspector_InspectRegistryConsistency_NoProjects verifies
// that the consistency check passes vacuously when no projects exist.
//
// Reference: TS-P9-11
func TestRegistryInspector_InspectRegistryConsistency_NoProjects(t *testing.T) {
	dir := t.TempDir()

	inspector := NewRegistryInspector(dir)
	check := inspector.InspectRegistryConsistency()

	if !check.Passed {
		t.Errorf("InspectRegistryConsistency().Passed = false, want true (vacuous); details: %s", check.Details)
	}
}

// TestRegistryInspector_InspectRegistryConsistency_DuplicateIDs verifies
// that the consistency check fails when duplicate project IDs exist in
// YAML content (different file names, same project.id).
//
// Reference: TS-P9-11
func TestRegistryInspector_InspectRegistryConsistency_DuplicateIDs(t *testing.T) {
	dir := t.TempDir()
	helperSetupRegistryDir(t, dir)

	// Write two files with different names but the same project.id in YAML.
	helperWriteRegistryFile(t, dir, "app-v1", "project:\n  id: my-app\n  install_root: /tmp/a\n")
	helperWriteRegistryFile(t, dir, "app-v2", "project:\n  id: my-app\n  install_root: /tmp/b\n")

	inspector := NewRegistryInspector(dir)
	check := inspector.InspectRegistryConsistency()

	if check.Passed {
		t.Errorf("InspectRegistryConsistency().Passed = true, want false (duplicate IDs)")
	}
}

// TestRegistryInspector_InspectRegistryConsistency_RelativeInstallRoot
// verifies that the consistency check flags relative install_root paths.
//
// Reference: TS-P9-11
func TestRegistryInspector_InspectRegistryConsistency_RelativeInstallRoot(t *testing.T) {
	dir := t.TempDir()
	helperSetupRegistryDir(t, dir)
	helperWriteRegistryFile(t, dir, "my-app", "project:\n  id: my-app\n  install_root: relative/path\n")

	inspector := NewRegistryInspector(dir)
	check := inspector.InspectRegistryConsistency()

	if check.Passed {
		t.Errorf("InspectRegistryConsistency().Passed = true, want false (relative install_root)")
	}
}

// TestRegistryInspector_InspectRegistryConsistency_OrphanedDirectory
// verifies that the consistency check detects orphaned directories.
//
// Reference: TS-P9-11
func TestRegistryInspector_InspectRegistryConsistency_OrphanedDirectory(t *testing.T) {
	dir := t.TempDir()
	helperSetupRegistryDir(t, dir)

	// Write at least one valid YAML file so the consistency check runs.
	helperWriteRegistryFile(t, dir, "valid-app", "project:\n  id: valid-app\n  install_root: /tmp\n")

	// Create an orphaned directory without a matching YAML file.
	store := server.NewRegistryStore(dir)
	orphanDir := filepath.Join(store.ProjectsDir(), "orphan-project")
	if err := os.MkdirAll(orphanDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	inspector := NewRegistryInspector(dir)
	check := inspector.InspectRegistryConsistency()

	if check.Passed {
		t.Errorf("InspectRegistryConsistency().Passed = true, want false (orphaned directory)")
	}
}

// TestRegistryInspector_Inspect_AllPassing verifies that Inspect returns
// a passing result when all checks pass.
//
// Reference: TS-P9-11
func TestRegistryInspector_Inspect_AllPassing(t *testing.T) {
	dir := t.TempDir()

	installRoot := filepath.Join(dir, "apps", "my-app")
	if err := os.MkdirAll(installRoot, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	store := server.NewRegistryStore(dir)
	if err := store.Register(server.ProjectRegistry{
		Project: server.ProjectSection{ID: "my-app", InstallRoot: installRoot},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	inspector := NewRegistryInspector(dir)
	result := inspector.Inspect()

	if !result.Passed {
		t.Errorf("Inspect().Passed = false, want true")
		for _, c := range result.Checks {
			if !c.Passed {
				t.Logf("  failed check: %s — %s", c.Name, c.Details)
			}
		}
	}
	if result.Component != "registry" {
		t.Errorf("Component = %q, want %q", result.Component, "registry")
	}
	if len(result.Checks) != 3 {
		t.Errorf("len(Checks) = %d, want 3", len(result.Checks))
	}
}

// TestRegistryInspector_Inspect_AllFailing verifies that Inspect returns
// a failing result when the registry directory is missing.
//
// Reference: TS-P9-11
func TestRegistryInspector_Inspect_AllFailing(t *testing.T) {
	dir := t.TempDir()

	inspector := NewRegistryInspector(dir)
	result := inspector.Inspect()

	if result.Passed {
		t.Errorf("Inspect().Passed = true, want false (nothing exists)")
	}
}

// TestRegistryInspector_SecureExecution verifies that running the inspector
// does not create or modify any files.
//
// Reference: TS-P9-11
func TestRegistryInspector_SecureExecution(t *testing.T) {
	dir := t.TempDir()
	helperSetupRegistryDir(t, dir)

	entriesBefore, err := os.ReadDir(filepath.Join(dir, "projects"))
	if err != nil {
		t.Fatalf("failed to read directory before: %v", err)
	}

	inspector := NewRegistryInspector(dir)
	_ = inspector.Inspect()

	entriesAfter, err := os.ReadDir(filepath.Join(dir, "projects"))
	if err != nil {
		t.Fatalf("failed to read directory after: %v", err)
	}

	if len(entriesBefore) != len(entriesAfter) {
		t.Errorf("directory contents changed: before=%d entries, after=%d entries",
			len(entriesBefore), len(entriesAfter))
	}
}
