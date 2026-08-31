// Package runtime provides models and utilities for managing Anvil Runtime
// instances — their configuration, lifecycle state machines, and readiness
// assessment.
//
// Reference: CH-P5-01, TS-P5-01, TS-P5-03, EPIC-005, ADR-003 §8.5
package runtime

import "path/filepath"

// RuntimeConfig defines the default Runtime configuration values that ship
// with Anvil. These provide sensible defaults so that a Runtime can be
// provisioned and ready for Release activation without manual configuration.
//
// Reference: CH-P5-01, EPIC-005, ADR-003 §8.5
type RuntimeConfig struct {
	// InstallRoot is the root directory for all Runtime data.
	// Default: "/opt/anvil"
	InstallRoot string `yaml:"install_root"`

	// ReleasesDir is the parent directory for versioned release directories.
	// Relative to InstallRoot. Default: "releases"
	ReleasesDir string `yaml:"releases_dir"`

	// ActiveSymlink is the name of the symlink pointing to the current release.
	// Relative to InstallRoot. Default: "current"
	ActiveSymlink string `yaml:"active_symlink"`

	// SharedConfigDir is the directory for shared configuration files.
	// Relative to InstallRoot. Default: "shared/config"
	SharedConfigDir string `yaml:"shared_config_dir"`

	// SharedStorageDir is the directory for persistent storage.
	// Relative to InstallRoot. Default: "shared/storage"
	SharedStorageDir string `yaml:"shared_storage_dir"`

	// LogsDir is the directory for runtime logs.
	// Relative to InstallRoot. Default: "shared/logs"
	LogsDir string `yaml:"logs_dir"`

	// TempDir is the temporary directory used during activation.
	// Relative to InstallRoot. Default: "tmp"
	TempDir string `yaml:"temp_dir"`

	// EnvironmentName is the deployment environment name.
	// Default: "production"
	EnvironmentName string `yaml:"environment_name"`

	// DirNamingPattern is the pattern for release directory naming.
	// Valid values: "identity", "version", "timestamp"
	// Default: "identity"
	DirNamingPattern string `yaml:"dir_naming_pattern"`
}

const (
	// DefaultInstallRoot is the compiled default root directory for all
	// Runtime data.
	DefaultInstallRoot = "/opt/anvil"

	// DefaultReleasesDir is the compiled default parent directory for
	// versioned release directories.
	DefaultReleasesDir = "releases"

	// DefaultActiveSymlink is the compiled default name of the symlink
	// pointing to the current release.
	DefaultActiveSymlink = "current"

	// DefaultSharedConfigDir is the compiled default directory for shared
	// configuration files.
	DefaultSharedConfigDir = "shared/config"

	// DefaultSharedStorageDir is the compiled default directory for
	// persistent storage.
	DefaultSharedStorageDir = "shared/storage"

	// DefaultLogsDir is the compiled default directory for runtime logs.
	DefaultLogsDir = "shared/logs"

	// DefaultTempDir is the compiled default temporary directory used
	// during activation.
	DefaultTempDir = "tmp"

	// DefaultEnvironmentName is the compiled default deployment environment
	// name.
	DefaultEnvironmentName = "production"

	// DefaultDirNamingPattern is the compiled default pattern for release
	// directory naming.
	DefaultDirNamingPattern = "identity"
)

// DefaultRuntimeConfig returns a RuntimeConfig populated with sensible
// defaults so that a Runtime can be provisioned without manual configuration.
//
// Reference: CH-P5-01
func DefaultRuntimeConfig() RuntimeConfig {
	return RuntimeConfig{
		InstallRoot:      DefaultInstallRoot,
		ReleasesDir:      DefaultReleasesDir,
		ActiveSymlink:    DefaultActiveSymlink,
		SharedConfigDir:  DefaultSharedConfigDir,
		SharedStorageDir: DefaultSharedStorageDir,
		LogsDir:          DefaultLogsDir,
		TempDir:          DefaultTempDir,
		EnvironmentName:  DefaultEnvironmentName,
		DirNamingPattern: DefaultDirNamingPattern,
	}
}

// ActiveSymlinkPath returns the full path to the active release symlink:
// InstallRoot / ActiveSymlink.
func (c RuntimeConfig) ActiveSymlinkPath() string {
	return filepath.Join(c.InstallRoot, c.ActiveSymlink)
}

// ReleasesDirPath returns the full path to the releases directory:
// InstallRoot / ReleasesDir.
func (c RuntimeConfig) ReleasesDirPath() string {
	return filepath.Join(c.InstallRoot, c.ReleasesDir)
}

// SharedConfigDirPath returns the full path to the shared config directory:
// InstallRoot / SharedConfigDir.
func (c RuntimeConfig) SharedConfigDirPath() string {
	return filepath.Join(c.InstallRoot, c.SharedConfigDir)
}

// SharedStorageDirPath returns the full path to the shared storage directory:
// InstallRoot / SharedStorageDir.
func (c RuntimeConfig) SharedStorageDirPath() string {
	return filepath.Join(c.InstallRoot, c.SharedStorageDir)
}

// LogsDirPath returns the full path to the logs directory:
// InstallRoot / LogsDir.
func (c RuntimeConfig) LogsDirPath() string {
	return filepath.Join(c.InstallRoot, c.LogsDir)
}

// TempDirPath returns the full path to the temporary directory:
// InstallRoot / TempDir.
func (c RuntimeConfig) TempDirPath() string {
	return filepath.Join(c.InstallRoot, c.TempDir)
}

// AllDirs returns all directory paths as a slice. The returned paths are
// the resolved full paths for InstallRoot, ReleasesDir, SharedConfigDir,
// SharedStorageDir, LogsDir, and TempDir.
func (c RuntimeConfig) AllDirs() []string {
	return []string{
		c.InstallRoot,
		c.ReleasesDirPath(),
		c.SharedConfigDirPath(),
		c.SharedStorageDirPath(),
		c.LogsDirPath(),
		c.TempDirPath(),
	}
}
