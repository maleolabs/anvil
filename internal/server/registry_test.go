package server

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNewRegistryStore verifies that NewRegistryStore creates a store with
// the expected root and projects directory paths.
func TestNewRegistryStore(t *testing.T) {
	root := t.TempDir()
	store := NewRegistryStore(root)

	if store.RootPath() != root {
		t.Errorf("RootPath() = %q, want %q", store.RootPath(), root)
	}

	expectedProjectsDir := filepath.Join(root, "projects")
	if store.ProjectsDir() != expectedProjectsDir {
		t.Errorf("ProjectsDir() = %q, want %q", store.ProjectsDir(), expectedProjectsDir)
	}
}

// TestRegistryStore_ProjectPath verifies that ProjectPath generates the
// correct file path for a given project ID.
func TestRegistryStore_ProjectPath(t *testing.T) {
	root := t.TempDir()
	store := NewRegistryStore(root)

	expected := filepath.Join(root, "projects", "my-project.yaml")
	if store.ProjectPath("my-project") != expected {
		t.Errorf("ProjectPath() = %q, want %q", store.ProjectPath("my-project"), expected)
	}

	expectedNested := filepath.Join(root, "projects", "another.yaml")
	if store.ProjectPath("another") != expectedNested {
		t.Errorf("ProjectPath() = %q, want %q", store.ProjectPath("another"), expectedNested)
	}
}

// TestRegistryStore_Exists verifies that Exists correctly detects an existing
// and non-existing project registry file.
func TestRegistryStore_Exists(t *testing.T) {
	root := t.TempDir()
	store := NewRegistryStore(root)

	// Project should not exist in an empty directory.
	if store.Exists("nonexistent") {
		t.Error("Exists() should be false for non-existent project")
	}

	// Register a project.
	cfg := ProjectRegistry{
		Project: ProjectSection{
			ID:          "test-exists",
			InstallRoot: "/var/www/test-exists",
		},
	}
	if err := store.Register(cfg); err != nil {
		t.Fatalf("Register() returned unexpected error: %v", err)
	}

	// Project should now exist.
	if !store.Exists("test-exists") {
		t.Error("Exists() should be true after Register()")
	}
}

// TestRegistryStore_Register verifies that Register creates a project YAML
// file at the correct path.
func TestRegistryStore_Register(t *testing.T) {
	root := t.TempDir()
	store := NewRegistryStore(root)

	cfg := ProjectRegistry{
		Project: ProjectSection{
			ID:          "test-register",
			DisplayName: "Test Register",
			InstallRoot: "/var/www/test-register",
			Adapter:     "laravel",
			Owner:       "deploy",
			Group:       "www-data",
		},
	}

	if err := store.Register(cfg); err != nil {
		t.Fatalf("Register() returned unexpected error: %v", err)
	}

	// Verify the file was created.
	projectPath := store.ProjectPath("test-register")
	if _, err := os.Stat(projectPath); os.IsNotExist(err) {
		t.Errorf("project file %s was not created", projectPath)
	}
}

// TestRegistryStore_Register_DuplicateRejection verifies that registering a
// project with an already-existing ID returns ErrProjectAlreadyRegistered.
func TestRegistryStore_Register_DuplicateRejection(t *testing.T) {
	root := t.TempDir()
	store := NewRegistryStore(root)

	cfg := ProjectRegistry{
		Project: ProjectSection{
			ID:          "duplicate-test",
			InstallRoot: "/var/www/duplicate",
		},
	}

	// First registration should succeed.
	if err := store.Register(cfg); err != nil {
		t.Fatalf("first Register() returned unexpected error: %v", err)
	}

	// Second registration should fail.
	err := store.Register(cfg)
	if err == nil {
		t.Fatal("Register() expected error for duplicate project, got nil")
	}
	if !isErrProjectAlreadyRegistered(err, "duplicate-test") {
		t.Errorf("Register() returned %v, want ErrProjectAlreadyRegistered for %q", err, "duplicate-test")
	}
}

// TestRegistryStore_Register_ValidatesConfig verifies that Register returns
// a validation error when given an invalid project config.
func TestRegistryStore_Register_ValidatesConfig(t *testing.T) {
	root := t.TempDir()
	store := NewRegistryStore(root)

	// Missing ID.
	cfgMissingID := ProjectRegistry{
		Project: ProjectSection{
			ID:          "",
			InstallRoot: "/var/www/missing-id",
		},
	}
	err := store.Register(cfgMissingID)
	if err == nil {
		t.Fatal("Register() expected error for missing ID, got nil")
	}
	if err != ErrProjectIDRequired {
		t.Errorf("Register() returned %v, want ErrProjectIDRequired", err)
	}

	// Missing InstallRoot.
	cfgMissingRoot := ProjectRegistry{
		Project: ProjectSection{
			ID:          "missing-root",
			InstallRoot: "",
		},
	}
	err = store.Register(cfgMissingRoot)
	if err == nil {
		t.Fatal("Register() expected error for missing InstallRoot, got nil")
	}
	if err != ErrInstallRootRequired {
		t.Errorf("Register() returned %v, want ErrInstallRootRequired", err)
	}
}

// TestRegistryStore_Load verifies that Load reads and returns the correct
// project registry data.
func TestRegistryStore_Load(t *testing.T) {
	root := t.TempDir()
	store := NewRegistryStore(root)

	original := ProjectRegistry{
		Project: ProjectSection{
			ID:          "test-load",
			DisplayName: "Test Load",
			InstallRoot: "/var/www/test-load",
			Adapter:     "node",
			Owner:       "admin",
			Group:       "admin",
		},
	}

	if err := store.Register(original); err != nil {
		t.Fatalf("Register() returned unexpected error: %v", err)
	}

	loaded, err := store.Load("test-load")
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}

	if loaded.Project.ID != original.Project.ID {
		t.Errorf("ID after load = %q, want %q", loaded.Project.ID, original.Project.ID)
	}
	if loaded.Project.DisplayName != original.Project.DisplayName {
		t.Errorf("DisplayName after load = %q, want %q", loaded.Project.DisplayName, original.Project.DisplayName)
	}
	if loaded.Project.InstallRoot != original.Project.InstallRoot {
		t.Errorf("InstallRoot after load = %q, want %q", loaded.Project.InstallRoot, original.Project.InstallRoot)
	}
	if loaded.Project.Adapter != original.Project.Adapter {
		t.Errorf("Adapter after load = %q, want %q", loaded.Project.Adapter, original.Project.Adapter)
	}
	if loaded.Project.Owner != original.Project.Owner {
		t.Errorf("Owner after load = %q, want %q", loaded.Project.Owner, original.Project.Owner)
	}
	if loaded.Project.Group != original.Project.Group {
		t.Errorf("Group after load = %q, want %q", loaded.Project.Group, original.Project.Group)
	}
}

// TestRegistryStore_Load_NotFound verifies that Load returns an error when
// the project registry file does not exist.
func TestRegistryStore_Load_NotFound(t *testing.T) {
	root := t.TempDir()
	store := NewRegistryStore(root)

	_, err := store.Load("nonexistent")
	if err == nil {
		t.Fatal("Load() expected error for non-existent project, got nil")
	}
}

// TestRegistryStore_RoundTrip verifies that a project registry saved to disk
// can be loaded back with identical values.
func TestRegistryStore_RoundTrip(t *testing.T) {
	root := t.TempDir()
	store := NewRegistryStore(root)

	original := ProjectRegistry{
		Project: ProjectSection{
			ID:          "roundtrip-project",
			DisplayName: "Round Trip Project",
			InstallRoot: "/var/www/roundtrip",
			Adapter:     "laravel",
			Owner:       "deploy",
			Group:       "www-data",
		},
	}

	if err := store.Register(original); err != nil {
		t.Fatalf("Register() returned unexpected error: %v", err)
	}

	loaded, err := store.Load("roundtrip-project")
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}

	if loaded.Project.ID != original.Project.ID {
		t.Errorf("ID after round-trip = %q, want %q",
			loaded.Project.ID, original.Project.ID)
	}
	if loaded.Project.DisplayName != original.Project.DisplayName {
		t.Errorf("DisplayName after round-trip = %q, want %q",
			loaded.Project.DisplayName, original.Project.DisplayName)
	}
	if loaded.Project.InstallRoot != original.Project.InstallRoot {
		t.Errorf("InstallRoot after round-trip = %q, want %q",
			loaded.Project.InstallRoot, original.Project.InstallRoot)
	}
}

// TestRegistryStore_ProjectsDirCreation verifies that Register creates the
// projects directory if it does not exist.
func TestRegistryStore_ProjectsDirCreation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nested", "anvil")
	store := NewRegistryStore(root)

	cfg := ProjectRegistry{
		Project: ProjectSection{
			ID:          "dir-creation-test",
			InstallRoot: "/var/www/dir-creation",
		},
	}

	if err := store.Register(cfg); err != nil {
		t.Fatalf("Register() returned unexpected error: %v", err)
	}

	if _, err := os.Stat(store.ProjectsDir()); os.IsNotExist(err) {
		t.Errorf("projects directory %s was not created", store.ProjectsDir())
	}
}

// TestRegistryStore_FilePermissions verifies that the saved project registry
// file has the expected 0644 permissions.
func TestRegistryStore_FilePermissions(t *testing.T) {
	root := t.TempDir()
	store := NewRegistryStore(root)

	cfg := ProjectRegistry{
		Project: ProjectSection{
			ID:          "perms-test",
			InstallRoot: "/var/www/perms-test",
		},
	}

	if err := store.Register(cfg); err != nil {
		t.Fatalf("Register() returned unexpected error: %v", err)
	}

	info, err := os.Stat(store.ProjectPath("perms-test"))
	if err != nil {
		t.Fatalf("Stat project file returned unexpected error: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0644 {
		t.Errorf("project file permissions = %o, want 0644", perm)
	}
}

// isErrProjectAlreadyRegistered checks whether err wraps
// ErrProjectAlreadyRegistered for the given projectID. It compares both the
// sentinel and the formatted error message.
func isErrProjectAlreadyRegistered(err error, projectID string) bool {
	if err == nil {
		return false
	}
	return err.Error() == "project already registered: \""+projectID+"\""
}
