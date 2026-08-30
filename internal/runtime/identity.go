// Package runtime provides models and utilities for managing Anvil Runtime
// instances — their configuration, lifecycle state machines, readiness
// assessment, and runtime identity.
//
// Reference: CH-P5-01, TS-P5-01, TS-P5-02, TS-P5-03, EPIC-005, ADR-003 §8.5
package runtime

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
)

// RuntimeID is a typed string wrapper representing a unique runtime identity.
// It is generated as a UUID v4 using crypto/rand (no external dependencies).
//
// Reference: TS-P5-02 AC-1
type RuntimeID string

// String returns the string representation of the RuntimeID, satisfying the
// fmt.Stringer interface for convenient display.
func (id RuntimeID) String() string {
	return string(id)
}

// EnvironmentType represents the deployment environment for a Runtime instance.
//
// Reference: TS-P5-02 AC-3
type EnvironmentType string

const (
	// EnvDevelopment represents a development environment.
	EnvDevelopment EnvironmentType = "development"
	// EnvStaging represents a staging environment.
	EnvStaging EnvironmentType = "staging"
	// EnvProduction represents a production environment.
	EnvProduction EnvironmentType = "production"
)

// ValidEnvironmentTypes returns all valid environment types.
func ValidEnvironmentTypes() []EnvironmentType {
	return []EnvironmentType{EnvDevelopment, EnvStaging, EnvProduction}
}

// IsValidEnvironmentType checks whether the given environment type is valid.
//
// Reference: TS-P5-02 AC-3
func IsValidEnvironmentType(env EnvironmentType) bool {
	for _, valid := range ValidEnvironmentTypes() {
		if env == valid {
			return true
		}
	}
	return false
}

// OperationalStatus represents the operational status of a Runtime instance.
// Statuses are derived from lifecycle stages.
//
// Reference: TS-P5-02 AC-2
type OperationalStatus string

const (
	// StatusProvisioned indicates the Runtime has been initialized but not
	// yet configured.
	StatusProvisioned OperationalStatus = "provisioned"
	// StatusReady indicates the Runtime is fully configured and ready to
	// accept Releases.
	StatusReady OperationalStatus = "ready"
	// StatusActive indicates the Runtime is actively hosting Releases.
	StatusActive OperationalStatus = "active"
	// StatusRetired indicates the Runtime has been taken out of service (terminal).
	StatusRetired OperationalStatus = "retired"
)

// FromStage converts a lifecycle Stage to the corresponding OperationalStatus.
//
// Reference: TS-P5-02 AC-2
func FromStage(stage Stage) OperationalStatus {
	switch stage {
	case StageProvisioned:
		return StatusProvisioned
	case StageReady:
		return StatusReady
	case StageActive:
		return StatusActive
	case StageRetired:
		return StatusRetired
	default:
		return OperationalStatus("unknown")
	}
}

// RuntimeMetadata represents the identity and descriptive metadata for a
// Runtime instance. It is JSON-serializable for persistence and exchange.
//
// Reference: TS-P5-02 AC-4
type RuntimeMetadata struct {
	ID          RuntimeID         `json:"id"`
	Name        string            `json:"name"`
	Environment EnvironmentType   `json:"environment"`
	InstallPath string            `json:"install_path"`
	Status      OperationalStatus `json:"status"`
}

// NewRuntimeID generates a new UUID v4 using crypto/rand.
//
// The format is: xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
// where x is random hex and y is (random & 0x3) | 0x8.
//
// Returns an error if rand.Read fails.
//
// Reference: TS-P5-02 AC-1
func NewRuntimeID() (RuntimeID, error) {
	var buf [16]byte
	if _, err := io.ReadFull(rand.Reader, buf[:]); err != nil {
		return "", fmt.Errorf("generate runtime ID: %w", err)
	}

	// Set version 4 (UUID version 4).
	buf[6] = (buf[6] & 0x0f) | 0x40
	// Set variant bits (RFC 4122).
	buf[8] = (buf[8] & 0x3f) | 0x80

	hex := fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16],
	)

	return RuntimeID(hex), nil
}

// ValidateRuntimeID validates whether the given string is a valid UUID v4.
//
// Returns nil if valid, or an error describing the failure.
//
// Reference: TS-P5-02 AC-1
func ValidateRuntimeID(id string) error {
	if id == "" {
		return fmt.Errorf("runtime ID is required")
	}

	if len(id) != 36 {
		return fmt.Errorf("runtime ID %q has invalid length %d, expected 36", id, len(id))
	}

	// Manual validation to avoid regexp dependency — check UUID v4 format.
	// Format: xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
	if id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return fmt.Errorf("runtime ID %q has invalid format: missing hyphens at expected positions", id)
	}

	// Check version nibble (position 14, 0-indexed: 14).
	if id[14] != '4' {
		return fmt.Errorf("runtime ID %q is not version 4: expected '4' at position 14", id)
	}

	// Check variant nibble (position 19, 0-indexed: 19).
	variant := id[19]
	if variant != '8' && variant != '9' && variant != 'a' && variant != 'b' &&
		variant != 'A' && variant != 'B' {
		return fmt.Errorf("runtime ID %q has invalid variant: expected 8/9/a/b at position 19", id)
	}

	// Validate hex characters in all segments.
	hexSegments := []struct {
		start, end int
	}{
		{0, 8},   // first segment
		{9, 13},  // second segment
		{14, 18}, // third segment
		{19, 23}, // fourth segment
		{24, 36}, // fifth segment
	}

	for _, seg := range hexSegments {
		for i := seg.start; i < seg.end; i++ {
			if !isHexChar(id[i]) {
				return fmt.Errorf("runtime ID %q contains non-hex character %q at position %d", id, id[i], i)
			}
		}
	}

	return nil
}

// isHexChar reports whether c is a valid hexadecimal character (0-9, a-f, A-F).
func isHexChar(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// NewRuntimeMetadata creates a new RuntimeMetadata with a generated RuntimeID
// and the initial StatusProvisioned.
//
// Reference: TS-P5-02 AC-4
func NewRuntimeMetadata(name string, env EnvironmentType, installPath string) RuntimeMetadata {
	id, err := NewRuntimeID()
	if err != nil {
		// This should not happen under normal circumstances (rand.Read failure
		// is extremely unlikely). Panic to make the caller aware of the
		// unexpected system-level failure.
		panic(fmt.Sprintf("new runtime metadata: %v", err))
	}

	return RuntimeMetadata{
		ID:          id,
		Name:        name,
		Environment: env,
		InstallPath: installPath,
		Status:      StatusProvisioned,
	}
}

// MarshalJSON implements json.Marshaler for RuntimeMetadata.
func (m RuntimeMetadata) MarshalJSON() ([]byte, error) {
	type alias RuntimeMetadata
	return json.Marshal(alias(m))
}

// UnmarshalJSON implements json.Unmarshaler for RuntimeMetadata.
func (m *RuntimeMetadata) UnmarshalJSON(data []byte) error {
	type alias RuntimeMetadata
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("unmarshal runtime metadata: %w", err)
	}
	*m = RuntimeMetadata(a)
	return nil
}
