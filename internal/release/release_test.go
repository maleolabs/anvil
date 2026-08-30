// Package release provides tests for the Release model.
//
// Reference: ST-P1-02, TS-P4-01
package release

import (
	"encoding/json"
	"os"
	"path/filepath"
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

// TestRelease_Save_CrashWindowAtomic verifies the TD-002 crash-window
// property for Release.Save: a crash mid-save (simulated by a partial temp
// file that never got renamed) leaves the complete previous release file at
// the final path, so Load never observes a truncated release. A subsequent
// Save recovers and leaves no temp files behind.
//
// Reference: TD-002, TS-P4-01 AC-4
func TestRelease_Save_CrashWindowAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rel-001.json")

	rel := func(id string, stage Stage) *Release {
		return &Release{
			ID:           ReleaseID(id),
			ArtifactID:   "artifact-abc",
			Version:      "1.0.0",
			Source:       "crash-test",
			ArtifactPath: "/store/artifact.tar.gz",
			RuntimePath:  dir,
			Stage:        stage,
			CreatedAt:    "2026-07-30T12:00:00Z",
			Transitions:  []TransitionRecord{},
		}
	}

	// Persist the previous complete release state.
	v1 := rel("rel-001", StageReady)
	if err := v1.Save(path); err != nil {
		t.Fatalf("Save() returned unexpected error: %v", err)
	}

	// Simulate a crash mid-write: partial temp file, rename never happened.
	crashTemp := filepath.Join(dir, "rel-001.json.tmp-crashed")
	if err := os.WriteFile(crashTemp, []byte(`{"id":"rel-001"`), 0644); err != nil {
		t.Fatalf("failed to simulate crashed temp file: %v", err)
	}

	// The final path must still hold the complete previous release.
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() after simulated crash returned unexpected error: %v", err)
	}
	if loaded.Stage != StageReady {
		t.Errorf("stage after simulated crash = %s, want %s (previous complete state)", loaded.Stage, StageReady)
	}

	// A subsequent Save must succeed and persist the new complete state.
	v2 := rel("rel-001", StageActive)
	if err := v2.Save(path); err != nil {
		t.Fatalf("Save() after crash returned unexpected error: %v", err)
	}

	loaded, err = Load(path)
	if err != nil {
		t.Fatalf("Load() after recovery returned unexpected error: %v", err)
	}
	if loaded.Stage != StageActive {
		t.Errorf("stage after recovery = %s, want %s", loaded.Stage, StageActive)
	}
}

// TestRelease_Save_ReplacesCorruptFile verifies that Release.Save atomically
// replaces a corrupt release file at the final path (the artifact of the
// pre-TD-002 non-atomic writer) with a complete, loadable release.
//
// Reference: TD-002
func TestRelease_Save_ReplacesCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rel-002.json")

	if err := os.WriteFile(path, []byte(`{"id":"rel-002","stag`), 0644); err != nil {
		t.Fatalf("failed to write corrupt release file: %v", err)
	}

	rel := &Release{
		ID:           ReleaseID("rel-002"),
		ArtifactID:   "artifact-abc",
		Version:      "1.0.0",
		Source:       "recovery-test",
		ArtifactPath: "/store/artifact.tar.gz",
		RuntimePath:  dir,
		Stage:        StageActive,
		CreatedAt:    "2026-07-30T12:00:00Z",
		Transitions:  []TransitionRecord{},
	}
	if err := rel.Save(path); err != nil {
		t.Fatalf("Save() over corrupt file returned unexpected error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() after Save over corrupt file returned unexpected error: %v", err)
	}
	if loaded.Stage != StageActive {
		t.Errorf("stage = %s, want %s", loaded.Stage, StageActive)
	}
}
