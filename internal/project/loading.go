// Package project provides the Anvil project loading engine.
//
// Load orchestrates the complete project loading sequence: discovery,
// reading, YAML parsing, validation, and enforcement. It is the primary
// entry point for commands that need to operate on a fully loaded and
// validated Anvil project configuration.
//
// Reference: TS-P1-06, EPIC-001
package project

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Load discovers, reads, parses, validates, and returns the project
// configuration for the current working directory.
//
// Loading sequence:
//  1. Call project.Discover() to find the project root
//  2. If no project found, return ErrNoProjectFound
//  3. Read anvil.yaml from the project root
//  4. Parse the YAML into a ProjectConfig struct
//  5. Call ValidateProject() to validate the parsed configuration
//  6. If validation fails, return ValidationBlockedError
//  7. If validation passes, return the validated *ProjectConfig
//
// The returned ProjectConfig is read-only — consumers must not modify it.
// Each invocation loads fresh configuration; no caching is performed.
//
// Performance target: full sequence within 1 second.
//
// Reference: TS-P1-06
func Load() (*ProjectConfig, error) {
	// Step 1: Discover project root.
	root, err := Discover()
	if err != nil {
		return nil, err
	}

	// Step 3: Read anvil.yaml from the project root.
	configPath := filepath.Join(root, ConfigFileName)
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", configPath, err)
	}

	// Step 4: Parse the YAML into a ProjectConfig struct.
	var cfg ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", configPath, err)
	}

	// Step 5: Validate the parsed configuration.
	result := ValidateProject(&cfg)
	if !result.Valid {
		return nil, &ValidationBlockedError{Errors: result.Errors}
	}

	// Step 6: Enforce project name immutability (ST-P1-03).
	// This detects configuration changes to project.name after initialization.
	if cfg.Project != nil {
		if err := ValidateIdentityImmutability(root, cfg.Project.Name); err != nil {
			return nil, &ValidationBlockedError{Errors: []string{err.Error()}}
		}
	}

	// Step 7: Return validated config.
	return &cfg, nil
}

// LoadSearched discovers, reads, parses, validates, and returns the project
// configuration for the current working directory. It behaves identically to
// Load but additionally returns the complete list of directories that were
// searched during project discovery.
//
// The searched slice is ordered from the current working directory upward
// to the project root (when found) or to the filesystem root (when not found).
//
// When no project is found, cfg is nil and err is ErrNoProjectFound. The
// searched slice is still returned so callers can display which directories
// were checked.
//
// Performance target: full sequence within 1 second.
//
// Reference: ST-P1-06
func LoadSearched() (cfg *ProjectConfig, searched []string, err error) {
	// Step 1: Discover project root with search path tracking.
	root, searched, err := DiscoverSearched()
	if err != nil {
		return nil, searched, err
	}

	// Step 3: Read anvil.yaml from the project root.
	configPath := filepath.Join(root, ConfigFileName)
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, searched, fmt.Errorf("reading %s: %w", configPath, err)
	}

	// Step 4: Parse the YAML into a ProjectConfig struct.
	var loaded ProjectConfig
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		return nil, searched, fmt.Errorf("parsing %s: %w", configPath, err)
	}

	// Step 5: Validate the parsed configuration.
	result := ValidateProject(&loaded)
	if !result.Valid {
		return nil, searched, &ValidationBlockedError{Errors: result.Errors}
	}

	// Step 6: Enforce project name immutability (ST-P1-03).
	if loaded.Project != nil {
		if err := ValidateIdentityImmutability(root, loaded.Project.Name); err != nil {
			return nil, searched, &ValidationBlockedError{Errors: []string{err.Error()}}
		}
	}

	// Step 7: Return validated config and searched directories.
	return &loaded, searched, nil
}
