package inspection

import (
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/runtime"
)

// helperSetupRuntimeDirs creates all runtime directories under the given root.
func helperSetupRuntimeDirs(t *testing.T, cfg runtime.RuntimeConfig) {
	t.Helper()
	for _, d := range cfg.AllDirs() {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("failed to create directory %s: %v", d, err)
		}
	}
}

// TestNewRuntimeInspector verifies that NewRuntimeInspector creates a
// non-nil inspector.
//
// Reference: TS-009-005
func TestNewRuntimeInspector(t *testing.T) {
	cfg := runtime.DefaultRuntimeConfig()
	inspector := NewRuntimeInspector(cfg)
	if inspector == nil {
		t.Fatal("NewRuntimeInspector() returned nil")
	}
}

// TestRuntimeInspector_InspectActiveSymlink_SymlinkExists verifies that
// the active symlink check passes when a valid symlink points to a directory.
//
// Reference: TS-009-005
func TestRuntimeInspector_InspectActiveSymlink_SymlinkExists(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	// Create a release directory to point the symlink at.
	releaseDir := filepath.Join(dir, "releases", "release-1")
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create the symlink.
	symlinkPath := cfg.ActiveSymlinkPath()
	if err := os.Symlink(releaseDir, symlinkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	inspector := NewRuntimeInspector(cfg)
	check := inspector.InspectActiveSymlink()

	if !check.Passed {
		t.Errorf("InspectActiveSymlink().Passed = false, want true; details: %s", check.Details)
	}
	if check.Name != "active_symlink" {
		t.Errorf("check.Name = %q, want %q", check.Name, "active_symlink")
	}
}

// TestRuntimeInspector_InspectActiveSymlink_NoSymlink verifies that the
// active symlink check fails when no symlink exists.
//
// Reference: TS-009-005
func TestRuntimeInspector_InspectActiveSymlink_NoSymlink(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	inspector := NewRuntimeInspector(cfg)
	check := inspector.InspectActiveSymlink()

	if check.Passed {
		t.Errorf("InspectActiveSymlink().Passed = true, want false (no symlink)")
	}
}

// TestRuntimeInspector_InspectActiveSymlink_BrokenSymlink verifies that
// the check fails when the symlink target does not exist.
//
// Reference: TS-009-005
func TestRuntimeInspector_InspectActiveSymlink_BrokenSymlink(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	// Create a symlink pointing to a non-existent target.
	symlinkPath := cfg.ActiveSymlinkPath()
	if err := os.Symlink("/nonexistent/path", symlinkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	inspector := NewRuntimeInspector(cfg)
	check := inspector.InspectActiveSymlink()

	if check.Passed {
		t.Errorf("InspectActiveSymlink().Passed = true, want false (broken symlink)")
	}
}

// TestRuntimeInspector_InspectReleaseDirectories_Exists verifies that the
// release directories check passes when the releases directory exists.
//
// Reference: TS-009-005
func TestRuntimeInspector_InspectReleaseDirectories_Exists(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	if err := os.MkdirAll(cfg.ReleasesDirPath(), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	inspector := NewRuntimeInspector(cfg)
	check := inspector.InspectReleaseDirectories()

	if !check.Passed {
		t.Errorf("InspectReleaseDirectories().Passed = false, want true; details: %s", check.Details)
	}
}

// TestRuntimeInspector_InspectReleaseDirectories_Missing verifies that the
// check fails when the releases directory does not exist.
//
// Reference: TS-009-005
func TestRuntimeInspector_InspectReleaseDirectories_Missing(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	inspector := NewRuntimeInspector(cfg)
	check := inspector.InspectReleaseDirectories()

	if check.Passed {
		t.Errorf("InspectReleaseDirectories().Passed = true, want false (dir missing)")
	}
}

// TestRuntimeInspector_InspectSharedResources_AllExist verifies that the
// shared resources check passes when all shared directories exist.
//
// Reference: TS-009-005
func TestRuntimeInspector_InspectSharedResources_AllExist(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	helperSetupRuntimeDirs(t, cfg)

	inspector := NewRuntimeInspector(cfg)
	check := inspector.InspectSharedResources()

	if !check.Passed {
		t.Errorf("InspectSharedResources().Passed = false, want true; details: %s", check.Details)
	}
}

// TestRuntimeInspector_InspectSharedResources_SomeMissing verifies that the
// check fails when some shared directories are missing.
//
// Reference: TS-009-005
func TestRuntimeInspector_InspectSharedResources_SomeMissing(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	// Create only install root and releases dir, skip shared dirs.
	if err := os.MkdirAll(cfg.InstallRoot, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(cfg.ReleasesDirPath(), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	inspector := NewRuntimeInspector(cfg)
	check := inspector.InspectSharedResources()

	if check.Passed {
		t.Errorf("InspectSharedResources().Passed = true, want false (shared dirs missing)")
	}
}

// TestRuntimeInspector_InspectRuntimeConfig_WithConfigFile verifies that
// the runtime config check passes when a config file exists.
//
// Reference: TS-009-005
func TestRuntimeInspector_InspectRuntimeConfig_WithConfigFile(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	// Create a config.yaml in the install root.
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("test: true"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	inspector := NewRuntimeInspector(cfg)
	check := inspector.InspectRuntimeConfig()

	if !check.Passed {
		t.Errorf("InspectRuntimeConfig().Passed = false, want true; details: %s", check.Details)
	}
}

// TestRuntimeInspector_InspectRuntimeConfig_NoConfig verifies that the
// check fails when no config file exists anywhere.
//
// Reference: TS-009-005
func TestRuntimeInspector_InspectRuntimeConfig_NoConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	inspector := NewRuntimeInspector(cfg)
	check := inspector.InspectRuntimeConfig()

	if check.Passed {
		t.Errorf("InspectRuntimeConfig().Passed = true, want false (no config)")
	}
}

// TestRuntimeInspector_Inspect_AllPassing verifies that Inspect returns a
// passing result when all checks pass.
//
// Reference: TS-009-005
func TestRuntimeInspector_Inspect_AllPassing(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	helperSetupRuntimeDirs(t, cfg)

	// Create a release directory and symlink to it.
	releaseDir := filepath.Join(dir, "releases", "release-1")
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(releaseDir, cfg.ActiveSymlinkPath()); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Create config file.
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("test: true"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	inspector := NewRuntimeInspector(cfg)
	result := inspector.Inspect()

	if !result.Passed {
		t.Errorf("Inspect().Passed = false, want true")
		for _, c := range result.Checks {
			if !c.Passed {
				t.Logf("  failed check: %s — %s", c.Name, c.Details)
			}
		}
	}
	if result.Component != "runtime" {
		t.Errorf("Component = %q, want %q", result.Component, "runtime")
	}
	if len(result.Checks) != 4 {
		t.Errorf("len(Checks) = %d, want 4", len(result.Checks))
	}
}

// TestRuntimeInspector_Inspect_AllFailing verifies that Inspect returns a
// failing result when no infrastructure exists.
//
// Reference: TS-009-005
func TestRuntimeInspector_Inspect_AllFailing(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = filepath.Join(dir, "nonexistent")

	inspector := NewRuntimeInspector(cfg)
	result := inspector.Inspect()

	if result.Passed {
		t.Errorf("Inspect().Passed = true, want false (nothing exists)")
	}
	if len(result.Checks) != 4 {
		t.Errorf("len(Checks) = %d, want 4", len(result.Checks))
	}

	// All checks should fail.
	for _, c := range result.Checks {
		if c.Passed {
			t.Errorf("check %q passed, want fail", c.Name)
		}
	}
}

// TestRuntimeInspector_Inspect_PartialSetup verifies behavior with a
// partial runtime setup (dirs exist but no symlink or config).
//
// Reference: TS-009-005
func TestRuntimeInspector_Inspect_PartialSetup(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	helperSetupRuntimeDirs(t, cfg)

	inspector := NewRuntimeInspector(cfg)
	result := inspector.Inspect()

	// Should fail because symlink and config are missing.
	if result.Passed {
		t.Errorf("Inspect().Passed = true, want false (partial setup)")
	}

	// Directories and shared resources should pass.
	checkMap := make(map[string]bool)
	for _, c := range result.Checks {
		checkMap[c.Name] = c.Passed
	}

	if !checkMap["release_directories"] {
		t.Error("release_directories check should pass")
	}
	if !checkMap["shared_resources"] {
		t.Error("shared_resources check should pass")
	}
	if checkMap["active_symlink"] {
		t.Error("active_symlink check should fail (no symlink)")
	}
	if checkMap["runtime_config"] {
		t.Error("runtime_config check should fail (no config file)")
	}
}
