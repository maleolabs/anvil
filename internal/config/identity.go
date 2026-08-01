// Package config provides configuration schema, defaults, validation,
// and identity management for Anvil projects.
//
// Reference: TS-P1-02, ADR-005, EPIC-001
package config

import (
	"fmt"
	"regexp"
)

// validProjectName matches names containing only alphanumeric characters,
// hyphens, and underscores — safe for filesystem paths and common naming
// conventions across platforms.
var validProjectName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Identity represents the immutable project identity.
//
// The identity is established during project initialization and must not
// change for the lifetime of the project. Downstream capabilities
// (EPIC-003 artifact metadata, EPIC-004 release records, EPIC-005 runtime
// state) reference this identity for traceability.
//
// Reference: ADR-004 §3.5, ADR-005 §3.1, 002-core-domain-model §4.1
type Identity struct {
	name string
}

// NewIdentity creates a new Identity after validating the project name.
//
// name must be non-empty and contain only alphanumeric characters, hyphens,
// and underscores. Returns an error with a descriptive message when the
// name is invalid.
func NewIdentity(name string) (Identity, error) {
	if err := ValidateProjectName(name); err != nil {
		return Identity{}, err
	}
	return Identity{name: name}, nil
}

// Name returns the project name.
func (id Identity) Name() string {
	return id.name
}

// String returns the project name as a string, satisfying the fmt.Stringer
// interface for convenient display.
func (id Identity) String() string {
	return id.name
}

// ValidateProjectName validates whether the given string is a valid project
// name. A valid name is non-empty and contains only alphanumeric characters,
// hyphens (-), and underscores (_).
//
// Returns nil if the name is valid, or an error describing the first
// validation failure encountered.
func ValidateProjectName(name string) error {
	if name == "" {
		return fmt.Errorf("project name is required")
	}
	if !validProjectName.MatchString(name) {
		return fmt.Errorf(
			"invalid project name %q: use only letters, numbers, hyphens (-), and underscores (_)",
			name,
		)
	}
	return nil
}

// Identity returns the project identity extracted from the configuration.
//
// The identity is derived from the project name stored in the configuration
// file during initialization. This method provides a stable access point for
// downstream consumers (EPIC-003, EPIC-004, EPIC-005) that need to reference
// the project identity without depending on the config struct layout.
func (cfg ProjectConfig) Identity() Identity {
	return Identity{name: cfg.Project.Name}
}
