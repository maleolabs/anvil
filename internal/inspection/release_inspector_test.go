// Package inspection provides read-only diagnostic inspection capabilities
// for Anvil Runtime environments and configuration.
//
// Reference: TS-P9-07
package inspection

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/runtime"
	"maleolabs.com/anvil/internal/server"
)

// TestNewReleaseInspector verifies that NewReleaseInspector creates a
// non-nil inspector with the given configuration.
//
// Reference: TS-P9-07
func TestNewReleaseInspector(t *testing.T) {
	cfg := runtime.DefaultRuntimeConfig()
	inspector := NewReleaseInspector(cfg)
	if inspector == nil {
		t.Fatal("NewReleaseInspector() returned nil")
	}
}

// TestReleaseInspector_InspectReleaseDirectory_Exists verifies that the
// release directory check passes when the releases directory exists.
//
// Reference: TS-P9-07
func TestReleaseInspector_InspectReleaseDirectory_Exists(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	if err := os.MkdirAll(cfg.ReleasesDirPath(), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	inspector := NewReleaseInspector(cfg)
	check := inspector.InspectReleaseDirectory()

	if !check.Passed {
		t.Errorf("InspectReleaseDirectory().Passed = false, want true; details: %s", check.Details)
	}
	if check.Name != "release_directory" {
		t.Errorf("check.Name = %q, want %q", check.Name, "release_directory")
	}
}

// TestReleaseInspector_InspectReleaseDirectory_Missing verifies that the
// release directory check fails when the releases directory does not exist.
//
// Reference: TS-P9-07
func TestReleaseInspector_InspectReleaseDirectory_Missing(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	inspector := NewReleaseInspector(cfg)
	check := inspector.InspectReleaseDirectory()

	if check.Passed {
		t.Errorf("InspectReleaseDirectory().Passed = true, want false (dir missing)")
	}
}

// TestReleaseInspector_InspectReleaseDirectory_NotADir verifies that the
// check fails when the releases path exists but is not a directory.
//
// Reference: TS-P9-07
func TestReleaseInspector_InspectReleaseDirectory_NotADir(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	// Create the releases path as a file, not a directory.
	releasesPath := cfg.ReleasesDirPath()
	if err := os.MkdirAll(filepath.Dir(releasesPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(releasesPath, []byte("not a dir"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	inspector := NewReleaseInspector(cfg)
	check := inspector.InspectReleaseDirectory()

	if check.Passed {
		t.Errorf("InspectReleaseDirectory().Passed = true, want false (not a dir)")
	}
}

// TestReleaseInspector_InspectArtifactPresence_WithArtifacts verifies that
// the artifact check passes when release directories contain files.
//
// Reference: TS-P9-07
func TestReleaseInspector_InspectArtifactPresence_WithArtifacts(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	// Create a release directory with an artifact file.
	releaseDir := filepath.Join(cfg.ReleasesDirPath(), "release-1")
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(releaseDir, "app.tar.gz"), []byte("data"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	inspector := NewReleaseInspector(cfg)
	check := inspector.InspectArtifactPresence()

	if !check.Passed {
		t.Errorf("InspectArtifactPresence().Passed = false, want true; details: %s", check.Details)
	}
}

// TestReleaseInspector_InspectArtifactPresence_EmptyReleases verifies that
// the artifact check passes vacuously when no release directories exist.
//
// Reference: TS-P9-07
func TestReleaseInspector_InspectArtifactPresence_EmptyReleases(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	// Create empty releases directory.
	if err := os.MkdirAll(cfg.ReleasesDirPath(), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	inspector := NewReleaseInspector(cfg)
	check := inspector.InspectArtifactPresence()

	if !check.Passed {
		t.Errorf("InspectArtifactPresence().Passed = false, want true (vacuous); details: %s", check.Details)
	}
}

// TestReleaseInspector_InspectArtifactPresence_Missing verifies that the
// artifact check fails when a release directory has no artifact files.
//
// Reference: TS-P9-07
func TestReleaseInspector_InspectArtifactPresence_Missing(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	// Create a release directory without any files (only subdirs).
	releaseDir := filepath.Join(cfg.ReleasesDirPath(), "release-1")
	if err := os.MkdirAll(filepath.Join(releaseDir, "subdir"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	inspector := NewReleaseInspector(cfg)
	check := inspector.InspectArtifactPresence()

	if check.Passed {
		t.Errorf("InspectArtifactPresence().Passed = true, want false (no artifacts)")
	}
}

// TestReleaseInspector_InspectArtifactPresence_ReleasesDirMissing verifies
// that the artifact check fails when the releases directory does not exist.
//
// Reference: TS-P9-07
func TestReleaseInspector_InspectArtifactPresence_ReleasesDirMissing(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	inspector := NewReleaseInspector(cfg)
	check := inspector.InspectArtifactPresence()

	if check.Passed {
		t.Errorf("InspectArtifactPresence().Passed = true, want false (releases dir missing)")
	}
}

// TestReleaseInspector_InspectSharedLinks_NoProjects verifies that the
// shared links check passes vacuously when no projects are registered.
//
// Reference: TS-P9-07
func TestReleaseInspector_InspectSharedLinks_NoProjects(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()

	inspector := NewReleaseInspector(cfg)
	check := inspector.InspectSharedLinks(dir)

	if !check.Passed {
		t.Errorf("InspectSharedLinks().Passed = false, want true (vacuous); details: %s", check.Details)
	}
}

// TestReleaseInspector_InspectSharedLinks_ValidLinks verifies that the
// shared links check passes when all shared link targets exist.
//
// Reference: TS-P9-07
func TestReleaseInspector_InspectSharedLinks_ValidLinks(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()

	// Set up a project with a valid shared link.
	installRoot := filepath.Join(dir, "projects", "my-app")
	sharedDir := filepath.Join(installRoot, "shared", "config")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	store := server.NewRegistryStore(dir)
	registry := server.ProjectRegistry{
		Project: server.ProjectSection{
			ID:          "my-app",
			InstallRoot: installRoot,
			SharedLinks: []server.SharedLink{
				{From: "shared/config", To: "config"},
			},
		},
	}
	if err := store.Register(registry); err != nil {
		t.Fatalf("register: %v", err)
	}

	inspector := NewReleaseInspector(cfg)
	check := inspector.InspectSharedLinks(dir)

	if !check.Passed {
		t.Errorf("InspectSharedLinks().Passed = false, want true; details: %s", check.Details)
	}
}

// TestReleaseInspector_InspectSharedLinks_BrokenLinks verifies that the
// shared links check fails when a shared link target does not exist.
//
// Reference: TS-P9-07
func TestReleaseInspector_InspectSharedLinks_BrokenLinks(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()

	// Set up a project with a broken shared link.
	installRoot := filepath.Join(dir, "projects", "my-app")
	if err := os.MkdirAll(installRoot, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	store := server.NewRegistryStore(dir)
	registry := server.ProjectRegistry{
		Project: server.ProjectSection{
			ID:          "my-app",
			InstallRoot: installRoot,
			SharedLinks: []server.SharedLink{
				{From: "shared/config", To: "config"},
			},
		},
	}
	if err := store.Register(registry); err != nil {
		t.Fatalf("register: %v", err)
	}

	inspector := NewReleaseInspector(cfg)
	check := inspector.InspectSharedLinks(dir)

	if check.Passed {
		t.Errorf("InspectSharedLinks().Passed = true, want false (broken link)")
	}
}

// TestReleaseInspector_Inspect_AllPassing verifies that Inspect returns a
// passing result when all checks pass.
//
// Reference: TS-P9-07
func TestReleaseInspector_Inspect_AllPassing(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	// Create releases directory with an artifact.
	releaseDir := filepath.Join(cfg.ReleasesDirPath(), "release-1")
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(releaseDir, "app.tar.gz"), []byte("data"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	inspector := NewReleaseInspector(cfg)
	result := inspector.Inspect("")

	if !result.Passed {
		t.Errorf("Inspect().Passed = false, want true")
		for _, c := range result.Checks {
			if !c.Passed {
				t.Logf("  failed check: %s — %s", c.Name, c.Details)
			}
		}
	}
	if result.Component != "release" {
		t.Errorf("Component = %q, want %q", result.Component, "release")
	}
}

// TestReleaseInspector_Inspect_WithServerRoot verifies that Inspect
// includes shared link checks when serverRoot is provided.
//
// Reference: TS-P9-07
func TestReleaseInspector_Inspect_WithServerRoot(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	// Create releases directory with an artifact.
	releaseDir := filepath.Join(cfg.ReleasesDirPath(), "release-1")
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(releaseDir, "app.tar.gz"), []byte("data"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	inspector := NewReleaseInspector(cfg)
	result := inspector.Inspect(dir)

	if !result.Passed {
		t.Errorf("Inspect().Passed = false, want true")
		for _, c := range result.Checks {
			if !c.Passed {
				t.Logf("  failed check: %s — %s", c.Name, c.Details)
			}
		}
	}

	// Should include shared_links check when serverRoot is provided.
	checkNames := make(map[string]bool)
	for _, c := range result.Checks {
		checkNames[c.Name] = true
	}
	if !checkNames["shared_links"] {
		t.Error("Inspect() with serverRoot should include shared_links check")
	}
}

// TestReleaseInspector_Inspect_AllFailing verifies that Inspect returns a
// failing result when no infrastructure exists.
//
// Reference: TS-P9-07
func TestReleaseInspector_Inspect_AllFailing(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = filepath.Join(dir, "nonexistent")

	inspector := NewReleaseInspector(cfg)
	result := inspector.Inspect("")

	if result.Passed {
		t.Errorf("Inspect().Passed = true, want false (nothing exists)")
	}
}

// TestReleaseInspector_SecureExecution verifies that running the inspector
// does not create or modify any files.
//
// Reference: TS-P9-07
func TestReleaseInspector_SecureExecution(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	entriesBefore, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read directory before: %v", err)
	}

	inspector := NewReleaseInspector(cfg)
	_ = inspector.Inspect("")

	entriesAfter, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read directory after: %v", err)
	}

	if len(entriesBefore) != len(entriesAfter) {
		t.Errorf("directory contents changed: before=%d entries, after=%d entries",
			len(entriesBefore), len(entriesAfter))
	}
}

// TestReleaseInspector_InspectExternalTools_AllFound verifies that
// InspectExternalTools reports all tools as found.
//
// Reference: TS-P9-07
func TestReleaseInspector_InspectExternalTools_AllFound(t *testing.T) {
	cfg := runtime.DefaultRuntimeConfig()
	inspector := NewReleaseInspector(cfg)

	// Override lookPath to simulate all tools found.
	inspector.lookPath = func(name string) (string, error) {
		return "/usr/bin/" + name, nil
	}

	checks := inspector.InspectExternalTools()

	if len(checks) != len(externalTools) {
		t.Fatalf("len(checks) = %d, want %d", len(checks), len(externalTools))
	}

	for _, check := range checks {
		if !check.Passed {
			t.Errorf("check %q.Passed = false, want true (informational checks always pass)", check.Name)
		}
		if check.Details == "" {
			t.Errorf("check %q.Details should not be empty", check.Name)
		}
	}
}

// TestReleaseInspector_InspectExternalTools_NoneFound verifies that
// InspectExternalTools reports all tools as missing (but still passes).
//
// Reference: TS-P9-07
func TestReleaseInspector_InspectExternalTools_NoneFound(t *testing.T) {
	cfg := runtime.DefaultRuntimeConfig()
	inspector := NewReleaseInspector(cfg)

	// Override lookPath to simulate all tools missing.
	inspector.lookPath = func(name string) (string, error) {
		return "", fmt.Errorf("%s: not found", name)
	}

	checks := inspector.InspectExternalTools()

	if len(checks) != len(externalTools) {
		t.Fatalf("len(checks) = %d, want %d", len(checks), len(externalTools))
	}

	// Informational checks always pass even when tools are missing.
	for _, check := range checks {
		if !check.Passed {
			t.Errorf("check %q.Passed = false, want true (informational checks always pass)", check.Name)
		}
		// Details should indicate tool is not found.
		if check.Details == "" {
			t.Errorf("check %q.Details should not be empty", check.Name)
		}
	}
}

// TestReleaseInspector_InspectExternalTools_PartialFound verifies that
// InspectExternalTools correctly reports mixed availability.
//
// Reference: TS-P9-07
func TestReleaseInspector_InspectExternalTools_PartialFound(t *testing.T) {
	cfg := runtime.DefaultRuntimeConfig()
	inspector := NewReleaseInspector(cfg)

	// Override lookPath to simulate partial availability.
	inspector.lookPath = func(name string) (string, error) {
		if name == "php" || name == "git" {
			return "/usr/bin/" + name, nil
		}
		return "", fmt.Errorf("%s: not found", name)
	}

	checks := inspector.InspectExternalTools()

	if len(checks) != len(externalTools) {
		t.Fatalf("len(checks) = %d, want %d", len(checks), len(externalTools))
	}

	// All checks pass (informational), but details differ.
	foundCount := 0
	for _, check := range checks {
		if !check.Passed {
			t.Errorf("check %q.Passed = false, want true", check.Name)
		}
		if check.Details != "" && check.Details[:len("found")] == "found" {
			foundCount++
		}
	}

	if foundCount != 2 {
		t.Errorf("foundCount = %d, want 2 (php and git)", foundCount)
	}
}

// TestReleaseInspector_InspectExternalTools_CheckNames verifies that
// each tool check has the expected name format.
//
// Reference: TS-P9-07
func TestReleaseInspector_InspectExternalTools_CheckNames(t *testing.T) {
	cfg := runtime.DefaultRuntimeConfig()
	inspector := NewReleaseInspector(cfg)

	inspector.lookPath = func(name string) (string, error) {
		return "/usr/bin/" + name, nil
	}

	checks := inspector.InspectExternalTools()

	for i, tool := range externalTools {
		expectedName := fmt.Sprintf("tool_%s", tool)
		if checks[i].Name != expectedName {
			t.Errorf("checks[%d].Name = %q, want %q", i, checks[i].Name, expectedName)
		}
	}
}

// TestReleaseInspector_Inspect_IncludesExternalTools verifies that the
// Inspect method includes external tool checks in the result.
//
// Reference: TS-P9-07
func TestReleaseInspector_Inspect_IncludesExternalTools(t *testing.T) {
	dir := t.TempDir()
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	// Create releases directory with an artifact.
	releaseDir := filepath.Join(cfg.ReleasesDirPath(), "release-1")
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(releaseDir, "app.tar.gz"), []byte("data"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	inspector := NewReleaseInspector(cfg)

	// Override lookPath to simulate all tools found.
	inspector.lookPath = func(name string) (string, error) {
		return "/usr/bin/" + name, nil
	}

	result := inspector.Inspect("")

	// Verify external tool checks are present.
	checkNames := make(map[string]bool)
	for _, c := range result.Checks {
		checkNames[c.Name] = true
	}

	for _, tool := range externalTools {
		expectedName := fmt.Sprintf("tool_%s", tool)
		if !checkNames[expectedName] {
			t.Errorf("Inspect() missing external tool check %q", expectedName)
		}
	}
}
