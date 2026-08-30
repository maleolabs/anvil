// Package server provides models and utilities for managing Anvil Server
// Runtime configuration — global Runtime metadata persistence, YAML schema
// definition, defaults, and validation.
//
// Reference: TS-P5-11, ST-P5-07, ADR-013, Decision 005, EPIC-005
package server

import (
	"errors"
	"fmt"
)

var (
	// ErrSchemaVersionRequired is returned when schema_version is not set.
	ErrSchemaVersionRequired = errors.New("runtime.schema_version is required and must be 1")

	// ErrIDRequired is returned when runtime.id is empty.
	ErrIDRequired = errors.New("runtime.id is required and must not be empty")
)

// ServerConfig represents the canonical global Runtime configuration YAML
// schema stored at /etc/anvil/config.yaml.
//
// It contains Runtime-level metadata only. It must not contain project lists,
// current releases, locks, activation state, or rollback history — those
// belong in Runtime State.
//
// Reference: ADR-013, Decision 005
type ServerConfig struct {
	Runtime RuntimeSection `yaml:"runtime"`
}

// RuntimeSection holds the identity and metadata keys for a Server Runtime
// instance.
type RuntimeSection struct {
	// SchemaVersion identifies the configuration schema version (required).
	SchemaVersion int `yaml:"schema_version"`

	// ID is the unique Runtime identity (required, user-provided).
	ID string `yaml:"id"`

	// DisplayName is an optional human-readable name for the Runtime.
	// Defaults to ID when empty.
	DisplayName string `yaml:"display_name,omitempty"`
}

// DefaultServerConfig returns a ServerConfig with compiled-in defaults.
//
// The default config has schema_version=1 and empty ID/DisplayName.
// The ID must be provided before the config is considered valid.
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		Runtime: RuntimeSection{
			SchemaVersion: 1,
			ID:            "",
			DisplayName:   "",
		},
	}
}

// ValidateServerConfig validates the required fields of a ServerConfig.
//
// Validation rules:
//   - schema_version must be 1 (required)
//   - id must be non-empty (required)
//   - display_name is optional
//
// Returns nil if the config is valid, or an error describing the first
// validation failure.
func ValidateServerConfig(cfg ServerConfig) error {
	if cfg.Runtime.SchemaVersion != 1 {
		return ErrSchemaVersionRequired
	}
	if cfg.Runtime.ID == "" {
		return ErrIDRequired
	}
	return nil
}

// Validate validates the ServerConfig against the canonical schema.
// It is a convenience method wrapping ValidateServerConfig.
func (c ServerConfig) Validate() error {
	return ValidateServerConfig(c)
}

// String returns a human-readable summary of the ServerConfig.
func (c ServerConfig) String() string {
	display := c.Runtime.DisplayName
	if display == "" {
		display = c.Runtime.ID
	}
	return fmt.Sprintf("ServerConfig{schema_version=%d, id=%q, display_name=%q}",
		c.Runtime.SchemaVersion, c.Runtime.ID, display)
}
