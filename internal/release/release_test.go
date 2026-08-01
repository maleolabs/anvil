// Package release provides tests for the Release model.
//
// Reference: ST-P1-02, TS-P4-01
package release

import (
	"encoding/json"
	"testing"
)

// TestRelease_SourceField verifies that the Release struct includes a Source
// field that is serialized to and deserialized from JSON.
//
// AC: Release records reference the project identity (Source field).
//
// Reference: ST-P1-02
func TestRelease_SourceField(t *testing.T) {
	rel := &Release{
		ID:           ReleaseID("test-id-1234567890123456"),
		ArtifactID:   "artifact-abc",
		Version:      "1.0.0",
		Source:       "my-project",
		ArtifactPath: "/tmp/artifact.tar.gz",
		RuntimePath:  "/var/anvil/my-project",
		Stage:        StageReady,
		CreatedAt:    "2026-07-30T12:00:00Z",
		Transitions:  []TransitionRecord{},
	}

	data, err := json.Marshal(rel)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	// Verify Source field is present in JSON output.
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal to map failed: %v", err)
	}

	source, ok := raw["source"]
	if !ok {
		t.Fatal("JSON output missing 'source' field")
	}
	if source != "my-project" {
		t.Errorf("source = %v, want %q", source, "my-project")
	}

	// Verify round-trip deserialization preserves Source.
	var decoded Release
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal to Release failed: %v", err)
	}
	if decoded.Source != "my-project" {
		t.Errorf("decoded.Source = %q, want %q", decoded.Source, "my-project")
	}
}

// TestRelease_SourceFieldOptional verifies that Source can be empty
// (backwards compatibility with releases created before this field existed).
func TestRelease_SourceFieldOptional(t *testing.T) {
	// Simulate older JSON without source field.
	// Stage 0 = StageReady (Stage is an int-based enum).
	input := `{
		"id": "test-id-1234567890123456",
		"artifact_id": "artifact-abc",
		"version": "1.0.0",
		"artifact_path": "/tmp/artifact.tar.gz",
		"runtime_path": "/var/anvil/my-project",
		"stage": 0,
		"created_at": "2026-07-30T12:00:00Z"
	}`

	var rel Release
	if err := json.Unmarshal([]byte(input), &rel); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if rel.Source != "" {
		t.Errorf("Source should be empty for legacy JSON, got %q", rel.Source)
	}
}

// TestRelease_JSONRoundTrip verifies that a fully populated Release
// survives a JSON marshal/unmarshal round trip without data loss.
func TestRelease_JSONRoundTrip(t *testing.T) {
	original := &Release{
		ID:           ReleaseID("rt-001"),
		ArtifactID:   "artifact-xyz",
		Version:      "2.1.0",
		Source:       "round-trip-app",
		ArtifactPath: "/store/rt-001.tar.gz",
		RuntimePath:  "/var/anvil/round-trip-app",
		Stage:        StageActive,
		CreatedAt:    "2026-06-15T08:30:00Z",
		Transitions: []TransitionRecord{
			{From: StageReady, To: StageActive, Timestamp: "2026-06-15T08:30:00Z", Outcome: "success"},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Release
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID mismatch: %q vs %q", decoded.ID, original.ID)
	}
	if decoded.ArtifactID != original.ArtifactID {
		t.Errorf("ArtifactID mismatch: %q vs %q", decoded.ArtifactID, original.ArtifactID)
	}
	if decoded.Version != original.Version {
		t.Errorf("Version mismatch: %q vs %q", decoded.Version, original.Version)
	}
	if decoded.Source != original.Source {
		t.Errorf("Source mismatch: %q vs %q", decoded.Source, original.Source)
	}
	if decoded.Stage != original.Stage {
		t.Errorf("Stage mismatch: %s vs %s", decoded.Stage, original.Stage)
	}
	if len(decoded.Transitions) != len(original.Transitions) {
		t.Errorf("Transitions count mismatch: %d vs %d", len(decoded.Transitions), len(original.Transitions))
	}
}
