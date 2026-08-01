// Package config provides configuration schema defaults and generation
// for Anvil project initialization.
//
// Reference: CH-P1-01, ADR-005, EPIC-001
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ProjectConfig represents the minimal configuration required for a valid
// Anvil project immediately after initialization.
//
// This schema is the minimal set defined by CH-P1-01. The full canonical
// schema (owned by EPIC-002) extends this with keys across all capabilities.
type ProjectConfig struct {
	Project  ProjectConfigSection  `yaml:"project"`
	Artifact ArtifactConfigSection `yaml:"artifact"`
}

// ProjectConfigSection holds project identity and metadata keys.
type ProjectConfigSection struct {
	// Name is the unique project identity (required, user-provided).
	Name string `yaml:"name"`

	// Version is the project version identifier (optional, default "1.0.0").
	Version string `yaml:"version"`

	// Description is an optional human-readable description of the project.
	Description string `yaml:"description"`
}

// ArtifactConfigSection holds artifact packaging file filter configuration.
// Include and Exclude are initialized as empty slices so users can directly
// edit them in anvil.yaml without needing to know the schema keys.
type ArtifactConfigSection struct {
	// Include specifies glob patterns for files to include in the artifact.
	// Empty by default — all non-excluded files are included.
	Include []string `yaml:"include"`

	// Exclude specifies glob patterns for files to exclude from the artifact.
	// Empty by default — no files are excluded beyond the compiled defaults.
	Exclude []string `yaml:"exclude"`
}

// DefaultVersion is the compiled default version assigned to a new project
// when the user does not specify one.
const DefaultVersion = "1.0.0"

// DefaultDescription is the compiled default description for a new project.
const DefaultDescription = ""

// NewProjectConfig creates a ProjectConfig with the given project name and
// sensible defaults for all optional keys.
func NewProjectConfig(name string) ProjectConfig {
	return ProjectConfig{
		Project: ProjectConfigSection{
			Name:        name,
			Version:     DefaultVersion,
			Description: DefaultDescription,
		},
		Artifact: ArtifactConfigSection{
			Include: []string{},
			Exclude: []string{},
		},
	}
}

// WriteConfig writes the project configuration as YAML to the specified path.
// The directory containing the path must already exist.
func WriteConfig(cfg ProjectConfig, path string) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config to YAML: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create config directory %s: %w", dir, err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write config file %s: %w", path, err)
	}

	return nil
}
