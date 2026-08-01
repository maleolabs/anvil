package runtime

import (
	"path/filepath"
	"testing"
)

// TestDefaultRuntimeConfig verifies that DefaultRuntimeConfig() returns
// all expected default values.
//
// Reference: CH-P5-01 AC-1
func TestDefaultRuntimeConfig(t *testing.T) {
	cfg := DefaultRuntimeConfig()

	if cfg.InstallRoot != DefaultInstallRoot {
		t.Errorf("InstallRoot = %q, want %q", cfg.InstallRoot, DefaultInstallRoot)
	}
	if cfg.ReleasesDir != DefaultReleasesDir {
		t.Errorf("ReleasesDir = %q, want %q", cfg.ReleasesDir, DefaultReleasesDir)
	}
	if cfg.ActiveSymlink != DefaultActiveSymlink {
		t.Errorf("ActiveSymlink = %q, want %q", cfg.ActiveSymlink, DefaultActiveSymlink)
	}
	if cfg.SharedConfigDir != DefaultSharedConfigDir {
		t.Errorf("SharedConfigDir = %q, want %q", cfg.SharedConfigDir, DefaultSharedConfigDir)
	}
	if cfg.SharedStorageDir != DefaultSharedStorageDir {
		t.Errorf("SharedStorageDir = %q, want %q", cfg.SharedStorageDir, DefaultSharedStorageDir)
	}
	if cfg.LogsDir != DefaultLogsDir {
		t.Errorf("LogsDir = %q, want %q", cfg.LogsDir, DefaultLogsDir)
	}
	if cfg.TempDir != DefaultTempDir {
		t.Errorf("TempDir = %q, want %q", cfg.TempDir, DefaultTempDir)
	}
	if cfg.EnvironmentName != DefaultEnvironmentName {
		t.Errorf("EnvironmentName = %q, want %q", cfg.EnvironmentName, DefaultEnvironmentName)
	}
	if cfg.DirNamingPattern != DefaultDirNamingPattern {
		t.Errorf("DirNamingPattern = %q, want %q", cfg.DirNamingPattern, DefaultDirNamingPattern)
	}
}

// TestPathMethods verifies that all path methods return correctly
// concatenated paths using filepath.Join.
//
// Reference: CH-P5-01 AC-2
func TestPathMethods(t *testing.T) {
	cfg := DefaultRuntimeConfig()

	wantInstallRoot := "/opt/anvil"
	wantReleasesDir := filepath.Join(wantInstallRoot, "releases")
	wantActiveSymlink := filepath.Join(wantInstallRoot, "current")
	wantSharedConfig := filepath.Join(wantInstallRoot, "shared/config")
	wantSharedStorage := filepath.Join(wantInstallRoot, "shared/storage")
	wantLogsDir := filepath.Join(wantInstallRoot, "shared/logs")
	wantTempDir := filepath.Join(wantInstallRoot, "tmp")

	if got := cfg.ReleasesDirPath(); got != wantReleasesDir {
		t.Errorf("ReleasesDirPath() = %q, want %q", got, wantReleasesDir)
	}
	if got := cfg.ActiveSymlinkPath(); got != wantActiveSymlink {
		t.Errorf("ActiveSymlinkPath() = %q, want %q", got, wantActiveSymlink)
	}
	if got := cfg.SharedConfigDirPath(); got != wantSharedConfig {
		t.Errorf("SharedConfigDirPath() = %q, want %q", got, wantSharedConfig)
	}
	if got := cfg.SharedStorageDirPath(); got != wantSharedStorage {
		t.Errorf("SharedStorageDirPath() = %q, want %q", got, wantSharedStorage)
	}
	if got := cfg.LogsDirPath(); got != wantLogsDir {
		t.Errorf("LogsDirPath() = %q, want %q", got, wantLogsDir)
	}
	if got := cfg.TempDirPath(); got != wantTempDir {
		t.Errorf("TempDirPath() = %q, want %q", got, wantTempDir)
	}
}

// TestAllDirs verifies that AllDirs() returns all 6 directories with
// correct full paths.
//
// Reference: CH-P5-01 AC-3
func TestAllDirs(t *testing.T) {
	cfg := DefaultRuntimeConfig()
	dirs := cfg.AllDirs()

	if len(dirs) != 6 {
		t.Fatalf("AllDirs() returned %d entries, want 6", len(dirs))
	}

	expected := []string{
		cfg.InstallRoot,
		cfg.ReleasesDirPath(),
		cfg.SharedConfigDirPath(),
		cfg.SharedStorageDirPath(),
		cfg.LogsDirPath(),
		cfg.TempDirPath(),
	}

	for i, dir := range dirs {
		if dir != expected[i] {
			t.Errorf("AllDirs()[%d] = %q, want %q", i, dir, expected[i])
		}
	}
}
