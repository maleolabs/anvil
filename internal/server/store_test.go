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
