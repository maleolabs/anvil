package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"maleolabs.com/anvil/internal/runtime"
	"maleolabs.com/anvil/internal/server"
)

// setupHealthyServerRoot creates a minimal healthy server root for testing.
//
// The helper was moved here from cmd/system_health_test.go, which was
// removed with the platform-ops breadth demotion (ADR-036 §3,
// TS-015-05-02). It is shared by the demoted-but-present diagnostics
// tests: server doctor, server readiness, and system inspect.
func setupHealthyServerRoot(t *testing.T, dir string) {
	t.Helper()

	// Create runtime directories.
	cfg := runtime.DefaultRuntimeConfig()
	cfg.InstallRoot = dir
	for _, d := range cfg.AllDirs() {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	// Create a release directory with an artifact.
	releaseDir := filepath.Join(dir, "releases", "release-1")
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(releaseDir, "app.tar.gz"), []byte("data"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Create active symlink.
	if err := os.Symlink(releaseDir, cfg.ActiveSymlinkPath()); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Create config file.
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("test: true"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Create server config.
	store := server.NewConfigStore(dir)
	serverCfg := server.DefaultServerConfig()
	serverCfg.Runtime.ID = "test-runtime"
	if err := store.Save(serverCfg); err != nil {
		t.Fatalf("save server config: %v", err)
	}

	// Create projects directory.
	projectsDir := filepath.Join(dir, "projects")
	if err := os.MkdirAll(projectsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
}
