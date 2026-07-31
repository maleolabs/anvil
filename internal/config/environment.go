// Package config provides environment-specific configuration loading for
// Anvil projects. Environment configuration files live in a well-known
// location within the project root and are loaded by the active environment
// name (e.g., staging, production).
//
// Reference: TS-P2-08, ST-P2-07, ADR-005 §7.5
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// envConfigDir is the relative path (from the project root) to the directory
// containing environment-specific configuration files.
const envConfigDir = "config/environments"

// EnvironmentConfigDir returns the absolute path to the environment
// configuration directory for the given project root.
//
// The directory is: <root>/config/environments
//
// Reference: TS-P2-08, ST-P2-07
func EnvironmentConfigDir(root string) string {
	return filepath.Join(root, envConfigDir)
}

// LoadEnvironmentConfig loads environment-specific configuration for the
// given environment name from the project root. It reads the YAML file at:
//
//	<root>/config/environments/<envName>.yaml
//
// If the file does not exist, it returns an empty map and no error — missing
// environment configuration is not a fatal error. This enables graceful
// fallback: when no environment file exists for the specified environment,
// project-level configuration is used without error.
//
// Only the exact <envName>.yaml file is checked — <envName>.yml is not
// tried. This keeps the lookup deterministic and avoids ambiguity.
//
// The returned map contains flat dot-notation keys, produced by the same
// flattenYAML function used by the primary config loader.
//
// Reference: TS-P2-08, ST-P2-07, ADR-005 §7.5
func LoadEnvironmentConfig(root string, envName string) (map[string]interface{}, error) {
	envDir := EnvironmentConfigDir(root)
	envPath := filepath.Join(envDir, envName+".yaml")

	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		// Graceful fallback: missing environment file is not an error.
		return make(map[string]interface{}), nil
	}

	cfg, err := loadYAMLFile(envPath)
	if err != nil {
		return nil, fmt.Errorf("read environment config %s: %w", envPath, err)
	}

	return cfg, nil
}
