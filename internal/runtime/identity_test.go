package runtime

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// TestNewRuntimeID_GeneratesUniqueID verifies that NewRuntimeID returns a
// non-empty RuntimeID that conforms to the UUID v4 format.
//
// Reference: TS-P5-02 AC-1
func TestNewRuntimeID_GeneratesUniqueID(t *testing.T) {
	id, err := NewRuntimeID()
	if err != nil {
		t.Fatalf("NewRuntimeID() returned unexpected error: %v", err)
	}

	if id.String() == "" {
		t.Error("NewRuntimeID() returned empty ID")
	}

	if err := ValidateRuntimeID(id.String()); err != nil {
		t.Errorf("ValidateRuntimeID(%q) = %v, want nil", id, err)
	}
}

// TestNewRuntimeID_Uniqueness verifies that consecutive calls to NewRuntimeID
// produce different IDs.
//
// Reference: TS-P5-02 AC-1
func TestNewRuntimeID_Uniqueness(t *testing.T) {
	seen := make(map[string]bool)

	for i := 0; i < 100; i++ {
		id, err := NewRuntimeID()
		if err != nil {
			t.Fatalf("NewRuntimeID() returned unexpected error at iteration %d: %v", i, err)
		}

		if seen[id.String()] {
			t.Errorf("duplicate RuntimeID generated at iteration %d: %s", i, id)
		}
		seen[id.String()] = true
	}
}

// TestNewRuntimeID_Error verifies that NewRuntimeID returns an error when
// rand.Reader fails.
//
// Reference: TS-P5-02 AC-1
func TestNewRuntimeID_Error(t *testing.T) {
	// Replace rand.Reader with a reader that always fails.
	originalReader := rand.Reader
	t.Cleanup(func() {
		rand.Reader = originalReader
	})

	rand.Reader = &failReader{}

	_, err := NewRuntimeID()
	if err == nil {
		t.Fatal("NewRuntimeID() should have returned an error with failing reader")
	}
}

// failReader is an io.Reader that always returns an error.
type failReader struct{}

func (r *failReader) Read(p []byte) (int, error) {
	return 0, errors.New("simulated read failure")
}

// TestValidateRuntimeID_Valid verifies that a valid UUID v4 passes validation.
//
// Reference: TS-P5-02 AC-1
func TestValidateRuntimeID_Valid(t *testing.T) {
	valid := []string{
		"f47ac10b-58cc-4372-a567-0e02b2c3d479",
		"f47ac10b-58cc-4372-b567-0e02b2c3d479",
		"f47ac10b-58cc-4372-8567-0e02b2c3d479",
		"f47ac10b-58cc-4372-9567-0e02b2c3d479",
		"00000000-0000-4000-8000-000000000000",
		"ffffffff-ffff-4fff-afff-ffffffffffff",
	}
	for _, id := range valid {
		t.Run(id, func(t *testing.T) {
			if err := ValidateRuntimeID(id); err != nil {
				t.Errorf("ValidateRuntimeID(%q) = %v, want nil", id, err)
			}
		})
	}
}

// TestValidateRuntimeID_Invalid verifies that invalid UUIDs fail validation.
//
// Reference: TS-P5-02 AC-1
func TestValidateRuntimeID_Invalid(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{"empty", ""},
		{"too_short", "f47ac10b"},
		{"wrong_format_no_hyphens", "f47ac10b58cc4372a5670e02b2c3d479"},
		{"wrong_version", "f47ac10b-58cc-1372-a567-0e02b2c3d479"},
		{"wrong_variant", "f47ac10b-58cc-4372-c567-0e02b2c3d479"},
		{"non_hex_chars", "g47ac10b-58cc-4372-a567-0e02b2c3d479"},
		{"all_zeros_wrong_version", "00000000-0000-3000-8000-000000000000"},
		{"hyphens_wrong_positions", "f47ac10b58cc-4372-a567-0e02b2c3d479"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateRuntimeID(tt.id); err == nil {
				t.Errorf("ValidateRuntimeID(%q) should have returned an error", tt.id)
			}
		})
	}
}

// TestNewRuntimeMetadata verifies that NewRuntimeMetadata populates all fields
// and sets the initial status to StatusProvisioned.
//
// Reference: TS-P5-02 AC-4
func TestNewRuntimeMetadata(t *testing.T) {
	meta := NewRuntimeMetadata("test-runtime", EnvStaging, "/opt/anvil")

	if meta.Name != "test-runtime" {
		t.Errorf("Name = %q, want %q", meta.Name, "test-runtime")
	}

	if meta.Environment != EnvStaging {
		t.Errorf("Environment = %q, want %q", meta.Environment, EnvStaging)
	}

	if meta.InstallPath != "/opt/anvil" {
		t.Errorf("InstallPath = %q, want %q", meta.InstallPath, "/opt/anvil")
	}

	if meta.Status != StatusProvisioned {
		t.Errorf("Status = %q, want %q", meta.Status, StatusProvisioned)
	}

	if meta.ID.String() == "" {
		t.Error("ID is empty, expected a generated RuntimeID")
	}

	if err := ValidateRuntimeID(meta.ID.String()); err != nil {
		t.Errorf("ValidateRuntimeID(%q) = %v, want nil", meta.ID, err)
	}
}

// TestRuntimeMetadata_JSONRoundTrip verifies that RuntimeMetadata can be
// marshaled to JSON and unmarshaled back without data loss.
//
// Reference: TS-P5-02 AC-4
func TestRuntimeMetadata_JSONRoundTrip(t *testing.T) {
	original := NewRuntimeMetadata("json-test", EnvProduction, "/opt/anvil")

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal() returned error: %v", err)
	}

	var decoded RuntimeMetadata
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() returned error: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID mismatch: got %q, want %q", decoded.ID, original.ID)
	}
	if decoded.Name != original.Name {
		t.Errorf("Name mismatch: got %q, want %q", decoded.Name, original.Name)
	}
	if decoded.Environment != original.Environment {
		t.Errorf("Environment mismatch: got %q, want %q", decoded.Environment, original.Environment)
	}
	if decoded.InstallPath != original.InstallPath {
		t.Errorf("InstallPath mismatch: got %q, want %q", decoded.InstallPath, original.InstallPath)
	}
	if decoded.Status != original.Status {
		t.Errorf("Status mismatch: got %q, want %q", decoded.Status, original.Status)
	}
}

// TestEnvironmentType_Valid verifies that valid environment types are accepted.
//
// Reference: TS-P5-02 AC-3
func TestEnvironmentType_Valid(t *testing.T) {
	tests := []struct {
		env EnvironmentType
	}{
		{EnvDevelopment},
		{EnvStaging},
		{EnvProduction},
	}
	for _, tt := range tests {
		t.Run(string(tt.env), func(t *testing.T) {
			if !IsValidEnvironmentType(tt.env) {
				t.Errorf("IsValidEnvironmentType(%q) = false, want true", tt.env)
			}
		})
	}
}

// TestEnvironmentType_Invalid verifies that invalid environment types are
// rejected.
//
// Reference: TS-P5-02 AC-3
func TestEnvironmentType_Invalid(t *testing.T) {
	invalid := []EnvironmentType{
		"",
		"invalid",
		"PRODUCTION",
		"prod",
		"dev",
	}
	for _, env := range invalid {
		t.Run(string(env), func(t *testing.T) {
			if IsValidEnvironmentType(env) {
				t.Errorf("IsValidEnvironmentType(%q) = true, want false", env)
			}
		})
	}
}

// TestOperationalStatus_FromStage verifies that each lifecycle Stage maps to
// the correct OperationalStatus.
//
// Reference: TS-P5-02 AC-2
func TestOperationalStatus_FromStage(t *testing.T) {
	tests := []struct {
		stage Stage
		want  OperationalStatus
	}{
		{StageProvisioned, StatusProvisioned},
		{StageReady, StatusReady},
		{StageActive, StatusActive},
		{StageRetired, StatusRetired},
	}
	for _, tt := range tests {
		t.Run(tt.stage.String(), func(t *testing.T) {
			got := FromStage(tt.stage)
			if got != tt.want {
				t.Errorf("FromStage(%s) = %q, want %q", tt.stage, got, tt.want)
			}
		})
	}
}

// TestOperationalStatus_FromStage_Unknown verifies that an unknown stage
// returns "unknown".
func TestOperationalStatus_FromStage_Unknown(t *testing.T) {
	got := FromStage(Stage(99))
	if got != OperationalStatus("unknown") {
		t.Errorf("FromStage(99) = %q, want %q", got, "unknown")
	}
}

// TestRuntimeMetadata_String verifies that String() returns the RuntimeID.
//
// Reference: TS-P5-02 AC-4
func TestRuntimeMetadata_String(t *testing.T) {
	id, err := NewRuntimeID()
	if err != nil {
		t.Fatalf("NewRuntimeID() returned unexpected error: %v", err)
	}

	if id.String() != string(id) {
		t.Errorf("RuntimeID.String() = %q, want %q", id.String(), string(id))
	}
}

// TestNewRuntimeMetadata_PanicsOnBadReader tests the panic path when
// NewRuntimeMetadata hits a rand.Reader failure. This is an edge case
// test that exercises the panic-recover path.
func TestNewRuntimeMetadata_PanicsOnBadReader(t *testing.T) {
	originalReader := rand.Reader
	t.Cleanup(func() {
		rand.Reader = originalReader
	})

	rand.Reader = &failReader{}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewRuntimeMetadata should have panicked with failing reader")
		}
	}()

	NewRuntimeMetadata("panic-test", EnvDevelopment, "/tmp/test")
}

// TestValidateRuntimeID_HandlesBoundaryCases verifies edge cases for
// validation.
func TestValidateRuntimeID_HandlesBoundaryCases(t *testing.T) {
	// Very long string.
	longID := strings.Repeat("a", 100)
	if err := ValidateRuntimeID(longID); err == nil {
		t.Error("ValidateRuntimeID(longID) should have returned an error")
	}

	// Single character.
	if err := ValidateRuntimeID("x"); err == nil {
		t.Error("ValidateRuntimeID('x') should have returned an error")
	}

	// Just hyphens of correct length but no hex.
	h := "------------------------------------"
	if len(h) == 36 {
		if err := ValidateRuntimeID(h); err == nil {
			t.Error("ValidateRuntimeID(hyphen string) should have returned an error")
		}
	}
}
