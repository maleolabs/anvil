package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReadinessChecker_AllDirectoriesExist verifies that when all runtime
// directories exist, the readiness check passes.
//
// Reference: TS-P5-03 AC-1
func TestReadinessChecker_AllDirectoriesExist(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	// Create all required directories.
	for _, d := range cfg.AllDirs() {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("failed to create directory %s: %v", d, err)
		}
	}

	checker := NewReadinessChecker(cfg)
	result := checker.Check()

	if !result.Ready {
		t.Errorf("Check().Ready = false, want true; checks: %+v", result.Checks)
	}
}

// TestReadinessChecker_MissingDirectory verifies that when a runtime
// directory is missing, the readiness check fails with details.
//
// Reference: TS-P5-03 AC-2
func TestReadinessChecker_MissingDirectory(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	// Create all directories except LogsDir.
	for _, d := range cfg.AllDirs() {
		if d == cfg.LogsDirPath() {
			continue
		}
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("failed to create directory %s: %v", d, err)
		}
	}

	checker := NewReadinessChecker(cfg)
	result := checker.Check()

	if result.Ready {
		t.Errorf("Check().Ready = true, want false (LogsDir missing)")
	}

	// The directories check should have failed.
	var dirCheck *ReadinessCheck
	for i, c := range result.Checks {
		if c.Name == "directories" {
			dirCheck = &result.Checks[i]
			break
		}
	}
	if dirCheck == nil {
		t.Fatal("no directories check found in result")
	}
	if dirCheck.Passed {
		t.Errorf("directories check passed, want failed")
	}
	if dirCheck.Details == "" {
		t.Errorf("directories check details is empty, want missing dir info")
	}
}

// TestReadinessChecker_ValidConfig verifies that the config check passes
// with valid default configuration values.
//
// Reference: TS-P5-03 AC-3
func TestReadinessChecker_ValidConfig(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	cfg.InstallRoot = dir

	// Create all directories so the consolidated result passes.
	for _, d := range cfg.AllDirs() {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("failed to create directory %s: %v", d, err)
		}
	}

	checker := NewReadinessChecker(cfg)
	result := checker.Check()

	if !result.Ready {
		t.Errorf("Check().Ready = false, want true with valid config")
	}

	// The config check should have passed.
	var configCheck *ReadinessCheck
	for i, c := range result.Checks {
		if c.Name == "config" {
			configCheck = &result.Checks[i]
			break
		}
	}
	if configCheck == nil {
		t.Fatal("no config check found in result")
	}
	if !configCheck.Passed {
		t.Errorf("config check failed: %s", configCheck.Details)
	}
}

// TestReadinessChecker_ConsolidatedResult verifies that the consolidated
// readiness result reflects all checks.
//
// Reference: TS-P5-03 AC-4
func TestReadinessChecker_ConsolidatedResult(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	cfg.InstallRoot = dir
	cfg.EnvironmentName = "" // invalid config

	// Create all directories.
	for _, d := range cfg.AllDirs() {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("failed to create directory %s: %v", d, err)
		}
	}

	checker := NewReadinessChecker(cfg)
	result := checker.Check()

	// Should be not ready because config is invalid.
	if result.Ready {
		t.Errorf("Check().Ready = true, want false with empty EnvironmentName")
	}

	if len(result.Checks) != 2 {
		t.Errorf("got %d checks, want 2", len(result.Checks))
	}

	// Verify check names are present.
	checkNames := make(map[string]bool)
	for _, c := range result.Checks {
		checkNames[c.Name] = c.Passed
	}

	if _, ok := checkNames["directories"]; !ok {
		t.Error("missing 'directories' check in consolidated result")
	}
	if _, ok := checkNames["config"]; !ok {
		t.Error("missing 'config' check in consolidated result")
	}
}

// TestReadinessChecker_AllDirectoriesMissing verifies the behavior when
// no directories exist.
func TestReadinessChecker_AllDirectoriesMissing(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultRuntimeConfig()
	cfg.InstallRoot = filepath.Join(dir, "nonexistent")

	checker := NewReadinessChecker(cfg)
	result := checker.Check()

	if result.Ready {
		t.Errorf("Check().Ready = true, want false (no directories exist)")
	}

	// The check should report details about missing directories.
	var dirCheck *ReadinessCheck
	for i, c := range result.Checks {
		if c.Name == "directories" {
			dirCheck = &result.Checks[i]
			break
		}
	}
	if dirCheck == nil {
		t.Fatal("no directories check found in result")
	}
	if dirCheck.Passed {
		t.Errorf("directories check passed, want failed")
	}
}

// TestNewReadinessChecker verifies the constructor returns a non-nil checker.
func TestNewReadinessChecker(t *testing.T) {
	cfg := DefaultRuntimeConfig()
	checker := NewReadinessChecker(cfg)
	if checker == nil {
		t.Fatal("NewReadinessChecker() returned nil")
	}
}
