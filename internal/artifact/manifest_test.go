// Package artifact defines the Anvil artifact directory layout and
// packaging primitives.
//
// Reference: TS-P3-05, EPIC-003
package artifact

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestGenerateManifest_PopulatesFields verifies that all manifest fields are
// populated correctly.
func TestGenerateManifest_PopulatesFields(t *testing.T) {
	m := GenerateManifest("abc123", "1.0.0", "my-project", "def456", ChecksumAlgorithmSHA256, "my-project-id")

	if m.ArtifactID != "abc123" {
		t.Errorf("ArtifactID = %q, want %q", m.ArtifactID, "abc123")
	}

	if m.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", m.Version, "1.0.0")
	}

	if m.Source != "my-project" {
		t.Errorf("Source = %q, want %q", m.Source, "my-project")
	}

	if m.Checksum != "def456" {
		t.Errorf("Checksum = %q, want %q", m.Checksum, "def456")
	}

	if m.ChecksumType != ChecksumAlgorithmSHA256 {
		t.Errorf("ChecksumType = %q, want %q", m.ChecksumType, ChecksumAlgorithmSHA256)
	}

	if m.ProjectID != "my-project-id" {
		t.Errorf("ProjectID = %q, want %q", m.ProjectID, "my-project-id")
	}
}

// TestGenerateManifest_TimestampFormat verifies that CreatedAt is a valid
// ISO 8601 / RFC 3339 timestamp.
func TestGenerateManifest_TimestampFormat(t *testing.T) {
	m := GenerateManifest("id", "1.0.0", "proj", "cs", "sha-256", "proj-id")

	_, err := time.Parse(time.RFC3339, m.CreatedAt)
	if err != nil {
		t.Errorf("CreatedAt %q is not valid RFC 3339: %v", m.CreatedAt, err)
	}
}

// TestGenerateManifest_TimestampIsUTC verifies that the timestamp is in UTC.
func TestGenerateManifest_TimestampIsUTC(t *testing.T) {
	m := GenerateManifest("id", "1.0.0", "proj", "cs", "sha-256", "proj-id")

	parsed, err := time.Parse(time.RFC3339, m.CreatedAt)
	if err != nil {
		t.Fatalf("parse timestamp: %v", err)
	}

	if parsed.Location().String() != "UTC" {
		t.Errorf("timestamp location = %q, want UTC", parsed.Location().String())
	}
}

// TestMarshalManifest_ValidJSON verifies that the marshalled output is valid
// JSON with the expected field names (snake_case).
func TestMarshalManifest_ValidJSON(t *testing.T) {
	m := GenerateManifest("abc123", "1.0.0", "my-project", "def456", "sha-256", "my-project-id")

	data, err := MarshalManifest(m)
	if err != nil {
		t.Fatalf("MarshalManifest returned error: %v", err)
	}

	// Verify it's valid JSON by unmarshalling.
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, string(data))
	}

	// Verify snake_case field names.
	expectedFields := []string{"artifact_id", "version", "created_at", "source", "checksum", "checksum_type", "project_id"}
	for _, field := range expectedFields {
		if _, ok := result[field]; !ok {
			t.Errorf("missing field %q in JSON output", field)
		}
	}

	// Verify no unexpected fields.
	for key := range result {
		found := false
		for _, field := range expectedFields {
			if key == field {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("unexpected field %q in JSON output", key)
		}
	}
}

// TestMarshalManifest_Indentation verifies that the JSON output uses 2-space
// indentation.
func TestMarshalManifest_Indentation(t *testing.T) {
	m := GenerateManifest("id", "1.0.0", "proj", "cs", "sha-256", "proj-id")

	data, err := MarshalManifest(m)
	if err != nil {
		t.Fatalf("MarshalManifest returned error: %v", err)
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		// Check that indentation uses spaces and is a multiple of 2.
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			indent := 0
			for _, c := range line {
				if c == ' ' {
					indent++
				} else if c == '\t' {
					t.Error("tab character found in JSON output, expected spaces")
					break
				} else {
					break
				}
			}
			if indent%2 != 0 {
				t.Errorf("odd indentation level %d in line: %s", indent, line)
			}
		}
	}
}

// TestMarshalManifest_RoundTrip verifies that marshalling and unmarshalling
// preserves all fields.
func TestMarshalManifest_RoundTrip(t *testing.T) {
	original := GenerateManifest("abc123", "1.0.0", "my-project", "def456", "sha-256", "my-project-id")

	data, err := MarshalManifest(original)
	if err != nil {
		t.Fatalf("MarshalManifest: %v", err)
	}

	var restored Manifest
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if restored.ArtifactID != original.ArtifactID {
		t.Errorf("ArtifactID: %q != %q", restored.ArtifactID, original.ArtifactID)
	}
	if restored.Version != original.Version {
		t.Errorf("Version: %q != %q", restored.Version, original.Version)
	}
	if restored.Source != original.Source {
		t.Errorf("Source: %q != %q", restored.Source, original.Source)
	}
	if restored.Checksum != original.Checksum {
		t.Errorf("Checksum: %q != %q", restored.Checksum, original.Checksum)
	}
	if restored.ChecksumType != original.ChecksumType {
		t.Errorf("ChecksumType: %q != %q", restored.ChecksumType, original.ChecksumType)
	}
	if restored.ProjectID != original.ProjectID {
		t.Errorf("ProjectID: %q != %q", restored.ProjectID, original.ProjectID)
	}
}

// TestMarshalManifest_WithCommands_RoundTrip verifies that a manifest
// carrying activation and rollback commands marshals to JSON with the
// expected keys and round-trips through Unmarshal unchanged (TS-P7-15,
// TS-P7-16).
func TestMarshalManifest_WithCommands_RoundTrip(t *testing.T) {
	original := GenerateManifest("abc123", "1.0.0", "my-project", "def456", "sha-256", "my-project-id")
	original.ActivationCommands = []string{
		"php artisan migrate --force",
		"php artisan config:cache",
		"php artisan route:cache",
		"php artisan view:cache",
	}
	original.RollbackCommands = []string{
		"php artisan migrate:rollback",
	}

	data, err := MarshalManifest(original)
	if err != nil {
		t.Fatalf("MarshalManifest: %v", err)
	}

	// The keys must be present in the JSON output.
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, string(data))
	}
	if _, ok := result["activation_commands"]; !ok {
		t.Error(`missing key "activation_commands" in JSON output`)
	}
	if _, ok := result["rollback_commands"]; !ok {
		t.Error(`missing key "rollback_commands" in JSON output`)
	}

	// Values must round-trip unchanged.
	var restored Manifest
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(restored.ActivationCommands, original.ActivationCommands) {
		t.Errorf("ActivationCommands: %v != %v", restored.ActivationCommands, original.ActivationCommands)
	}
	if !reflect.DeepEqual(restored.RollbackCommands, original.RollbackCommands) {
		t.Errorf("RollbackCommands: %v != %v", restored.RollbackCommands, original.RollbackCommands)
	}
}

// TestMarshalManifest_WithoutCommands_OmitsKeys verifies that a manifest
// without activation or rollback commands serializes WITHOUT the
// command keys (omitempty), keeping new manifests without commands
// backward compatible with the pre-ADR-017 manifest shape (TS-P7-15,
// TS-P7-16).
func TestMarshalManifest_WithoutCommands_OmitsKeys(t *testing.T) {
	m := GenerateManifest("abc123", "1.0.0", "my-project", "def456", "sha-256", "my-project-id")

	data, err := MarshalManifest(m)
	if err != nil {
		t.Fatalf("MarshalManifest: %v", err)
	}

	if strings.Contains(string(data), "activation_commands") {
		t.Error(`output contains "activation_commands" for empty commands`)
	}
	if strings.Contains(string(data), "rollback_commands") {
		t.Error(`output contains "rollback_commands" for empty commands`)
	}
}

// TestReadManifest_OldFormatParses verifies that an artifact carrying an
// old-format manifest (JSON without activation/rollback command keys)
// still parses successfully, with nil/empty command fields (backward
// compatibility, TS-P7-15, TS-P7-16).
func TestReadManifest_OldFormatParses(t *testing.T) {
	// createTestArtifact marshals the Manifest struct: with empty command
	// slices and omitempty tags, the resulting JSON matches the old
	// pre-ADR-017 manifest format exactly (no command keys).
	oldFormat := GenerateManifest("abc123", "1.0.0", "my-project", "def456", "sha-256", "my-project-id")

	artifactPath := filepath.Join(t.TempDir(), "old-format-artifact.tar.gz")
	createTestArtifact(t, artifactPath, oldFormat, map[string]string{"index.php": "<?php\n"})

	manifest, err := ReadManifest(artifactPath)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}

	if manifest.ArtifactID != "abc123" {
		t.Errorf("ArtifactID = %q, want %q", manifest.ArtifactID, "abc123")
	}
	if manifest.ActivationCommands != nil && len(manifest.ActivationCommands) != 0 {
		t.Errorf("ActivationCommands = %v, want empty/nil", manifest.ActivationCommands)
	}
	if manifest.RollbackCommands != nil && len(manifest.RollbackCommands) != 0 {
		t.Errorf("RollbackCommands = %v, want empty/nil", manifest.RollbackCommands)
	}
}
