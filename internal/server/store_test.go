package server

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNewConfigStore verifies that NewConfigStore creates a store with the
// expected root and config paths.
func TestNewConfigStore(t *testing.T) {
	root := t.TempDir()
	store := NewConfigStore(root)

	if store.RootPath() != root {
		t.Errorf("RootPath() = %q, want %q", store.RootPath(), root)
	}

	expectedConfigPath := filepath.Join(root, "config.yaml")
	if store.ConfigPath() != expectedConfigPath {
		t.Errorf("ConfigPath() = %q, want %q", store.ConfigPath(), expectedConfigPath)
	}
}

// TestConfigStore_Exists verifies that Exists correctly detects an existing
// and non-existing config file.
func TestConfigStore_Exists(t *testing.T) {
	root := t.TempDir()
	store := NewConfigStore(root)

	// Config should not exist in an empty directory.
	if store.Exists() {
		t.Error("Exists() should be false for empty directory")
	}

	// Create the config file.
	cfg := DefaultServerConfig()
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save() returned unexpected error: %v", err)
	}

	// Config should now exist.
	if !store.Exists() {
		t.Error("Exists() should be true after Save()")
	}
}

// TestConfigStore_SaveLoad_RoundTrip verifies that a config saved to disk
// can be loaded back with identical values.
func TestConfigStore_SaveLoad_RoundTrip(t *testing.T) {
	root := t.TempDir()
	store := NewConfigStore(root)

	original := ServerConfig{
		Runtime: RuntimeSection{
			SchemaVersion: 1,
			ID:            "roundtrip-server",
			DisplayName:   "Round Trip Server",
		},
	}

	if err := store.Save(original); err != nil {
		t.Fatalf("Save() returned unexpected error: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}

	if loaded.Runtime.SchemaVersion != original.Runtime.SchemaVersion {
		t.Errorf("SchemaVersion after round-trip = %d, want %d",
			loaded.Runtime.SchemaVersion, original.Runtime.SchemaVersion)
	}
	if loaded.Runtime.ID != original.Runtime.ID {
		t.Errorf("ID after round-trip = %q, want %q",
			loaded.Runtime.ID, original.Runtime.ID)
	}
	if loaded.Runtime.DisplayName != original.Runtime.DisplayName {
		t.Errorf("DisplayName after round-trip = %q, want %q",
			loaded.Runtime.DisplayName, original.Runtime.DisplayName)
	}
}

// TestConfigStore_Load_NonExistentFile verifies that Load returns an error
// when the config file does not exist.
func TestConfigStore_Load_NonExistentFile(t *testing.T) {
	root := t.TempDir()
	store := NewConfigStore(root)

	_, err := store.Load()
	if err == nil {
		t.Fatal("Load() expected error for non-existent file, got nil")
	}
}

// TestConfigStore_Init_CreatesDirectory verifies that Init creates the
// config root directory if it does not exist.
func TestConfigStore_Init_CreatesDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nested", "anvil")
	store := NewConfigStore(root)

	if err := store.Init(); err != nil {
		t.Fatalf("Init() returned unexpected error: %v", err)
	}

	if _, err := os.Stat(root); os.IsNotExist(err) {
		t.Errorf("config directory %s was not created", root)
	}
}

// TestConfigStore_Init_CreatesDefaultConfig verifies that Init writes the
// default ServerConfig to config.yaml.
func TestConfigStore_Init_CreatesDefaultConfig(t *testing.T) {
	root := t.TempDir()
	store := NewConfigStore(root)

	if err := store.Init(); err != nil {
		t.Fatalf("Init() returned unexpected error: %v", err)
	}

	if !store.Exists() {
		t.Fatal("Init() should create config.yaml")
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() after Init() returned unexpected error: %v", err)
	}

	if loaded.Runtime.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", loaded.Runtime.SchemaVersion)
	}
	if loaded.Runtime.ID != "" {
		t.Errorf("ID = %q, want empty string", loaded.Runtime.ID)
	}
	if loaded.Runtime.DisplayName != "" {
		t.Errorf("DisplayName = %q, want empty string", loaded.Runtime.DisplayName)
	}
}

// TestConfigStore_Init_Idempotent verifies that calling Init multiple times
// is safe and does not modify an existing config.
func TestConfigStore_Init_Idempotent(t *testing.T) {
	root := t.TempDir()
	store := NewConfigStore(root)

	// First Init should create the default config.
	if err := store.Init(); err != nil {
		t.Fatalf("first Init() returned unexpected error: %v", err)
	}

	// Modify the config to something non-default.
	customCfg := ServerConfig{
		Runtime: RuntimeSection{
			SchemaVersion: 1,
			ID:            "custom-server",
			DisplayName:   "Custom Server",
		},
	}
	if err := store.Save(customCfg); err != nil {
		t.Fatalf("Save() returned unexpected error: %v", err)
	}

	// Second Init should NOT overwrite the custom config.
	if err := store.Init(); err != nil {
		t.Fatalf("second Init() returned unexpected error: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() after second Init() returned unexpected error: %v", err)
	}

	if loaded.Runtime.ID != "custom-server" {
		t.Errorf("after idempotent Init, ID = %q, want %q (should not have been overwritten)",
			loaded.Runtime.ID, "custom-server")
	}
}

// TestConfigStore_RootOverride verifies that a ConfigStore with a custom
// root path works correctly.
func TestConfigStore_RootOverride(t *testing.T) {
	root := filepath.Join(t.TempDir(), "custom", "anvil")
	store := NewConfigStore(root)

	cfg := ServerConfig{
		Runtime: RuntimeSection{
			SchemaVersion: 1,
			ID:            "override-server",
		},
	}

	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save() with custom root returned unexpected error: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() with custom root returned unexpected error: %v", err)
	}

	if loaded.Runtime.ID != "override-server" {
		t.Errorf("ID = %q, want %q", loaded.Runtime.ID, "override-server")
	}
}

// TestConfigStore_DefaultRoot verifies that the DefaultConfigRoot constant
// is /etc/anvil.
func TestConfigStore_DefaultRoot(t *testing.T) {
	if DefaultConfigRoot != "/etc/anvil" {
		t.Errorf("DefaultConfigRoot = %q, want %q", DefaultConfigRoot, "/etc/anvil")
	}
}

// TestConfigStore_Save_FilePermissions verifies that the saved config file
// has the expected 0644 permissions.
func TestConfigStore_Save_FilePermissions(t *testing.T) {
	root := t.TempDir()
	store := NewConfigStore(root)

	cfg := DefaultServerConfig()
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save() returned unexpected error: %v", err)
	}

	info, err := os.Stat(store.ConfigPath())
	if err != nil {
		t.Fatalf("Stat config file returned unexpected error: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0644 {
		t.Errorf("config file permissions = %o, want 0644", perm)
	}
}

// TestConfigStore_Save_PreservesExistingFileMode verifies that Save preserves
// an operator-hardened mode on the existing config file — a 0600 config.yaml
// stays 0600 after Save instead of being silently widened to 0644 (TD-002
// review).
func TestConfigStore_Save_PreservesExistingFileMode(t *testing.T) {
	root := t.TempDir()
	store := NewConfigStore(root)

	if err := os.WriteFile(store.ConfigPath(), []byte("runtime:\n  id: old\n"), 0o600); err != nil {
		t.Fatalf("failed to create 0600 config file: %v", err)
	}

	if err := store.Save(DefaultServerConfig()); err != nil {
		t.Fatalf("Save() returned unexpected error: %v", err)
	}

	info, err := os.Stat(store.ConfigPath())
	if err != nil {
		t.Fatalf("Stat config file returned unexpected error: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config file permissions = %o, want 0600 (operator-hardened mode must be preserved)", perm)
	}
}

// TestConfigStore_Save_CrashWindowAtomic verifies the TD-002 crash-window
// property for ConfigStore.Save: a crash mid-save (simulated by a partial
// temp file that never got renamed) leaves the complete previous config file
// at the final path, so Load never observes a truncated config. A subsequent
// Save recovers and leaves no temp files behind.
//
// Reference: TD-002, ADR-013
func TestConfigStore_Save_CrashWindowAtomic(t *testing.T) {
	root := t.TempDir()
	store := NewConfigStore(root)

	// Persist the previous complete config.
	v1 := ServerConfig{
		Runtime: RuntimeSection{
			SchemaVersion: 1,
			ID:            "crash-v1",
			DisplayName:   "Crash V1",
		},
	}
	if err := store.Save(v1); err != nil {
		t.Fatalf("Save() returned unexpected error: %v", err)
	}

	// Simulate a crash mid-write: partial temp file, rename never happened.
	crashTemp := filepath.Join(root, "config.yaml.tmp-crashed")
	if err := os.WriteFile(crashTemp, []byte("runtime:\n  id: crash-"), 0644); err != nil {
		t.Fatalf("failed to simulate crashed temp file: %v", err)
	}

	// The final path must still hold the complete previous config.
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() after simulated crash returned unexpected error: %v", err)
	}
	if loaded.Runtime.ID != "crash-v1" {
		t.Errorf("config ID after simulated crash = %q, want %q (previous complete state)",
			loaded.Runtime.ID, "crash-v1")
	}

	// A subsequent Save must succeed and persist the new complete config.
	v2 := ServerConfig{
		Runtime: RuntimeSection{
			SchemaVersion: 1,
			ID:            "crash-v2",
			DisplayName:   "Crash V2",
		},
	}
	if err := store.Save(v2); err != nil {
		t.Fatalf("Save() after crash returned unexpected error: %v", err)
	}

	loaded, err = store.Load()
	if err != nil {
		t.Fatalf("Load() after recovery returned unexpected error: %v", err)
	}
	if loaded.Runtime.ID != "crash-v2" {
		t.Errorf("config ID after recovery = %q, want %q", loaded.Runtime.ID, "crash-v2")
	}
}

// TestConfigStore_Save_ReplacesCorruptFile verifies that ConfigStore.Save
// atomically replaces a corrupt config file at the final path (the artifact
// of the pre-TD-002 non-atomic writer) with a complete, loadable config.
//
// Reference: TD-002
func TestConfigStore_Save_ReplacesCorruptFile(t *testing.T) {
	root := t.TempDir()
	store := NewConfigStore(root)

	if err := os.WriteFile(store.ConfigPath(), []byte("runtime:\n  id: trunc"), 0644); err != nil {
		t.Fatalf("failed to write corrupt config file: %v", err)
	}

	cfg := ServerConfig{
		Runtime: RuntimeSection{
			SchemaVersion: 1,
			ID:            "recovery-v1",
			DisplayName:   "Recovery V1",
		},
	}
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save() over corrupt file returned unexpected error: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() after Save over corrupt file returned unexpected error: %v", err)
	}
	if loaded.Runtime.ID != "recovery-v1" {
		t.Errorf("config ID = %q, want %q", loaded.Runtime.ID, "recovery-v1")
	}
}
