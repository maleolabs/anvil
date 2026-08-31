package server

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"maleolabs.com/anvil/internal/fsutil"
)

const (
	// DefaultConfigRoot is the production default path for Server Runtime
	// configuration. Override only for tests, containers, isolated Runtimes,
	// and integration testing.
	//
	// Reference: ADR-013, Decision 005 §4
	DefaultConfigRoot = "/etc/anvil"

	// configFileName is the name of the global Runtime configuration file
	// within the config root.
	configFileName = "config.yaml"

	// EnvServerRoot is the environment variable that overrides the config
	// root path for non-production use.
	//
	// Reference: Decision 005 §4
	EnvServerRoot = "ANVIL_SERVER_ROOT"
)

// ConfigStore manages persistence of ServerConfig to a YAML file on disk.
//
// It provides Exists, Load, Save, and Init operations for the global Runtime
// configuration file at <rootPath>/config.yaml.
//
// Reference: TS-P5-11, ADR-013
type ConfigStore struct {
	// rootPath is the config root directory (default: /etc/anvil).
	rootPath string

	// configPath is the full path to config.yaml.
	configPath string
}

// NewConfigStore creates a ConfigStore that persists config to the given
// root directory. The config file is stored at <rootPath>/config.yaml.
//
// The root path is used as-is; callers should resolve environment variable
// overrides before calling NewConfigStore.
func NewConfigStore(rootPath string) *ConfigStore {
	return &ConfigStore{
		rootPath:   rootPath,
		configPath: filepath.Join(rootPath, configFileName),
	}
}

// RootPath returns the config root directory path.
func (s *ConfigStore) RootPath() string {
	return s.rootPath
}

// ConfigPath returns the full path to the config.yaml file.
func (s *ConfigStore) ConfigPath() string {
	return s.configPath
}

// Exists checks whether the config.yaml file already exists on disk.
func (s *ConfigStore) Exists() bool {
	_, err := os.Stat(s.configPath)
	return err == nil
}

// Load reads the config.yaml file and unmarshals it into a ServerConfig.
//
// Returns an error if the file does not exist, cannot be read, or contains
// invalid YAML.
func (s *ConfigStore) Load() (*ServerConfig, error) {
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("server config not found at %s", s.configPath)
		}
		return nil, fmt.Errorf("read server config from %s: %w", s.configPath, err)
	}

	var cfg ServerConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal server config from %s: %w", s.configPath, err)
	}

	return &cfg, nil
}

// Save marshals the ServerConfig to YAML and writes it to config.yaml.
//
// The parent directory is created if it does not exist. The file is written
// with 0644 permissions. The write is atomic (temp file + fsync + rename,
// see fsutil.WriteFileAtomic): a crash mid-save leaves either the complete
// previous config or the complete new one at config.yaml — never a truncated
// or partially-written configuration file (TD-002).
func (s *ConfigStore) Save(cfg ServerConfig) error {
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("marshal server config to YAML: %w", err)
	}

	if err := os.MkdirAll(s.rootPath, 0755); err != nil {
		return fmt.Errorf("create config directory %s: %w", s.rootPath, err)
	}

	if err := fsutil.WriteFileAtomic(s.configPath, data, 0644); err != nil {
		return fmt.Errorf("write server config to %s: %w", s.configPath, err)
	}

	return nil
}

// Init ensures the config directory and default config file exist.
//
// If config.yaml already exists, Init returns without modification
// (idempotent). If it does not exist, Init creates the config directory
// (0755) and writes the default ServerConfig (0644).
//
// Init is safe to retry.
func (s *ConfigStore) Init() error {
	if s.Exists() {
		return nil
	}

	if err := os.MkdirAll(s.rootPath, 0755); err != nil {
		return fmt.Errorf("create config directory %s: %w", s.rootPath, err)
	}

	cfg := DefaultServerConfig()
	if err := s.Save(cfg); err != nil {
		return fmt.Errorf("initialize server config: %w", err)
	}

	return nil
}

// RootPath resolves the effective config root path.
//
// It checks the ANVIL_SERVER_ROOT environment variable first. If set and
// non-empty, that value is used. Otherwise, the default /etc/anvil is
// returned.
//
// Reference: Decision 005 §4
func RootPath() string {
	if root := os.Getenv(EnvServerRoot); root != "" {
		return root
	}
	return DefaultConfigRoot
}
