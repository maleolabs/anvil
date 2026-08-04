// Package server provides models and utilities for managing Anvil Server
// Runtime configuration — global Runtime metadata persistence, YAML schema
// definition, defaults, and validation, as well as per-project Registry
// metadata.
//
// Reference: TS-P5-11, TS-P5-12, ADR-013, Decision 005, EPIC-005
package server

import (
	"errors"
	"fmt"
)

var (
	// ErrProjectIDRequired is returned when project.id is empty.
	//
	// Reference: TS-P5-12, ADR-013
	ErrProjectIDRequired = errors.New("project.id is required and must not be empty")

	// ErrInstallRootRequired is returned when project.install_root is empty.
	//
	// Reference: TS-P5-12, ADR-013
	ErrInstallRootRequired = errors.New("project.install_root is required and must not be empty")
)

// ProjectRegistry represents one registered project's declarative metadata.
//
// Each registered project is stored as a separate YAML file at
// <configRoot>/projects/<project-id>.yaml.
//
// Reference: TS-P5-12, ADR-013
type ProjectRegistry struct {
	Project ProjectSection `yaml:"project"`
}

// SharedLink defines a symlink between a shared resource and a location
// inside the release directory. During activation, the target path inside
// the release directory is replaced with a symlink pointing to the shared
// resource path.
//
// Both paths are relative:
//   - From: relative to installRoot (e.g., "shared/config/.env")
//   - To:   relative to the release directory (e.g., ".env")
//
// Reference: EPIC-005 §11.5, §9.2
type SharedLink struct {
	// From is the shared resource path relative to installRoot.
	From string `yaml:"from"`

	// To is the target path inside the release directory.
	To string `yaml:"to"`
}

// ProjectSection holds the identity and metadata keys for a registered
// project within a Server Runtime.
type ProjectSection struct {
	// ID is the unique project identifier (required, immutable).
	ID string `yaml:"id"`

	// DisplayName is an optional human-readable name for the project.
	DisplayName string `yaml:"display_name,omitempty"`

	// InstallRoot is the absolute filesystem path where the project is
	// installed (required).
	InstallRoot string `yaml:"install_root"`

	// Adapter specifies the deployment adapter to use (optional).
	Adapter string `yaml:"adapter,omitempty"`

	// Owner is the responsible user or team (optional).
	Owner string `yaml:"owner,omitempty"`

	// Group is the system group for file ownership (optional).
	Group string `yaml:"group,omitempty"`

	// SharedLinks defines symlinks from shared resources into the release
	// directory, applied during activation (optional).
	SharedLinks []SharedLink `yaml:"shared_links,omitempty"`
}

// DefaultProjectRegistry returns a ProjectRegistry with compiled-in defaults.
//
// All fields are empty by default; the caller must provide at least ID and
// InstallRoot before the registry is considered valid.
func DefaultProjectRegistry() ProjectRegistry {
	return ProjectRegistry{
		Project: ProjectSection{
			ID:          "",
			DisplayName: "",
			InstallRoot: "",
			Adapter:     "",
			Owner:       "",
			Group:       "",
			SharedLinks: nil,
		},
	}
}

// ValidateProjectRegistry validates the required fields of a ProjectRegistry.
//
// Validation rules:
//   - project.id must be non-empty (required)
//   - project.install_root must be non-empty (required)
//   - project.display_name is optional
//   - other fields are optional
//
// Returns nil if the config is valid, or an error describing the first
// validation failure.
//
// Reference: TS-P5-12, ADR-013
func ValidateProjectRegistry(cfg ProjectRegistry) error {
	if cfg.Project.ID == "" {
		return ErrProjectIDRequired
	}
	if cfg.Project.InstallRoot == "" {
		return ErrInstallRootRequired
	}
	return nil
}

// Validate validates the ProjectRegistry against the canonical schema.
// It is a convenience method wrapping ValidateProjectRegistry.
func (c ProjectRegistry) Validate() error {
	return ValidateProjectRegistry(c)
}

// String returns a human-readable summary of the ProjectRegistry.
func (c ProjectRegistry) String() string {
	display := c.Project.DisplayName
	if display == "" {
		display = c.Project.ID
	}
	links := len(c.Project.SharedLinks)
	return fmt.Sprintf("ProjectRegistry{id=%q, display_name=%q, install_root=%q, adapter=%q, owner=%q, group=%q, shared_links=%d}",
		c.Project.ID, display, c.Project.InstallRoot, c.Project.Adapter, c.Project.Owner, c.Project.Group, links)
}
