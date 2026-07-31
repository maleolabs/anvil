// Package config provides configuration schema, defaults, validation,
// and metadata management for Anvil projects.
//
// Reference: TS-P1-03, ADR-002 §3.3, ADR-005 §7.2
package config

import "fmt"

// Metadata represents the project's descriptive metadata — name and version.
//
// Downstream consumers (EPIC-003 artifact packaging, EPIC-004 release management)
// use this type to access project metadata through a stable interface rather than
// raw dot-notation string keys.
//
// Metadata is immutable: once created via NewMetadata, the values cannot be
// changed. Accessor methods provide read-only access.
//
// Reference: TS-P1-03, ADR-002 §3.3, ADR-005 §7.2
type Metadata struct {
	name    string
	version string
}

// NewMetadata creates a Metadata value with the given name and version.
//
// No validation is performed — validation is the responsibility of the
// configuration system (EPIC-002). The caller guarantees that name and version
// are meaningful according to project conventions.
func NewMetadata(name, version string) Metadata {
	return Metadata{name: name, version: version}
}

// Name returns the project name.
func (m Metadata) Name() string { return m.name }

// Version returns the project version identifier.
func (m Metadata) Version() string { return m.version }

// String returns a human-readable representation, e.g. "my-app v2.0.0".
func (m Metadata) String() string {
	return fmt.Sprintf("%s v%s", m.name, m.version)
}

// Metadata returns the project metadata from this configuration.
//
// The metadata is derived from the project name and version stored in the
// configuration during initialization. This method provides a stable access
// point for downstream consumers (EPIC-003, EPIC-004) that need to reference
// both the project name and version without depending on the config struct
// layout or raw dot-notation keys.
//
// Reference: TS-P1-03, ADR-005 §7.2
func (cfg ProjectConfig) Metadata() Metadata {
	return NewMetadata(cfg.Project.Name, cfg.Project.Version)
}
