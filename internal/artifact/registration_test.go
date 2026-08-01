// Package artifact defines the Anvil artifact directory layout and
// packaging primitives.
//
// Reference: TS-P3-10, ADR-004 §7, EPIC-003
package artifact

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// RegistrationStore Tests — TS-P3-10
// ---------------------------------------------------------------------------

// TestNewRegistrationStore verifies that a new store is initialized with an
// empty index and the given path.
func TestNewRegistrationStore(t *testing.T) {
	store := NewRegistrationStore("/tmp/test-index.json")

	if store.path != "/tmp/test-index.json" {
		t.Errorf("expected path /tmp/test-index.json, got %s", store.path)
	}

	records := store.List()
	if len(records) != 0 {
		t.Errorf("expected empty store, got %d records", len(records))
	}
}

// TestRegistrationStore_Register_Success verifies that a verified artifact
// can be registered successfully, returning a record with the expected fields.
//
// Reference: TS-P3-10
func TestRegistrationStore_Register_Success(t *testing.T) {
	store := NewRegistrationStore("")

	manifest := &Manifest{
		ArtifactID:   "abc123",
		Version:      "1.0.0",
		ProjectID:    "my-project",
		Checksum:     "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		ChecksumType: ChecksumAlgorithmSHA256,
		CreatedAt:    "2026-07-25T12:00:00Z",
		Source:       "my-project",
	}

	record, err := store.Register(manifest, "passed")
	if err != nil {
		t.Fatalf("Register returned unexpected error: %v", err)
	}

	if record == nil {
		t.Fatal("expected non-nil registration record")
	}

	if record.ArtifactID != "abc123" {
		t.Errorf("expected ArtifactID abc123, got %s", record.ArtifactID)
	}
	if record.Version != "1.0.0" {
		t.Errorf("expected Version 1.0.0, got %s", record.Version)
	}
	if record.VerificationResult != "passed" {
		t.Errorf("expected VerificationResult 'passed', got %s", record.VerificationResult)
	}
	if record.RegisteredAt == "" {
		t.Error("expected non-empty RegisteredAt timestamp")
	}
	if record.ManifestContent != manifest {
		t.Error("expected ManifestContent to reference the original manifest")
	}
}

// TestRegistrationStore_Register_NilManifest verifies that registering a nil
// manifest returns an error.
func TestRegistrationStore_Register_NilManifest(t *testing.T) {
	store := NewRegistrationStore("")

	_, err := store.Register(nil, "passed")
	if err == nil {
		t.Fatal("expected error for nil manifest, got nil")
	}
	if !strings.Contains(err.Error(), "nil manifest") {
		t.Errorf("expected error about nil manifest, got: %v", err)
	}
}

// TestRegistrationStore_Register_Unverified verifies that an artifact with
// a non-"passed" verification result is rejected.
//
// Reference: TS-P3-10
func TestRegistrationStore_Register_Unverified(t *testing.T) {
	store := NewRegistrationStore("")

	manifest := &Manifest{
		ArtifactID: "unverified-artifact",
		Version:    "1.0.0",
	}

	_, err := store.Register(manifest, "failed: checksum mismatch")
	if err == nil {
		t.Fatal("expected error for unverified artifact, got nil")
	}
	if !strings.Contains(err.Error(), "must be \"passed\"") {
		t.Errorf("expected error about 'passed' requirement, got: %v", err)
	}

	// Verify the artifact is NOT in the store.
	if store.IsRegistered("unverified-artifact") {
		t.Error("unverified artifact should not be registered")
	}
}

// TestRegistrationStore_Register_Idempotent verifies that re-registering the
// same artifact returns the existing record without error.
//
// Reference: TS-P3-10
func TestRegistrationStore_Register_Idempotent(t *testing.T) {
	store := NewRegistrationStore("")

	manifest := &Manifest{
		ArtifactID: "idempotent-artifact",
		Version:    "1.0.0",
		ProjectID:  "my-project",
		Checksum:   "abc123",
	}

	first, err := store.Register(manifest, "passed")
	if err != nil {
		t.Fatalf("first Register returned error: %v", err)
	}

	second, err := store.Register(manifest, "passed")
	if err != nil {
		t.Fatalf("second Register returned error: %v", err)
	}

	if first != second {
		t.Error("expected same record pointer for idempotent registration")
	}

	if first.RegisteredAt != second.RegisteredAt {
		t.Error("expected same RegisteredAt for idempotent registration")
	}
}

// TestRegistrationStore_LookupByIdentity verifies that a registered artifact
// can be found by its identity.
//
// Reference: TS-P3-10
func TestRegistrationStore_LookupByIdentity(t *testing.T) {
	store := NewRegistrationStore("")

	manifest := &Manifest{
		ArtifactID: "lookup-artifact",
		Version:    "2.0.0",
		ProjectID:  "test-project",
	}

	registered, err := store.Register(manifest, "passed")
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	// Successful lookup.
	found, ok := store.Lookup("lookup-artifact")
	if !ok {
		t.Fatal("Lookup returned false for registered artifact")
	}
	if found != registered {
		t.Error("Lookup returned a different record than the one registered")
	}

	// Lookup non-existent artifact.
	_, ok = store.Lookup("nonexistent")
	if ok {
		t.Error("Lookup returned true for non-existent artifact")
	}
}

// TestRegistrationStore_IsRegistered verifies that IsRegistered correctly
// reports registration status.
//
// Reference: TS-P3-10
func TestRegistrationStore_IsRegistered(t *testing.T) {
	store := NewRegistrationStore("")

	if store.IsRegistered("not-yet-registered") {
		t.Error("expected IsRegistered to return false before registration")
	}

	manifest := &Manifest{ArtifactID: "check-registered"}
	_, err := store.Register(manifest, "passed")
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	if !store.IsRegistered("check-registered") {
		t.Error("expected IsRegistered to return true after registration")
	}

	if store.IsRegistered("other-artifact") {
		t.Error("expected IsRegistered to return false for different artifact")
	}
}

// TestRegistrationStore_SaveAndLoad_Roundtrip verifies that the registration
// index can be persisted to disk and restored correctly.
//
// Reference: TS-P3-10
func TestRegistrationStore_SaveAndLoad_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "registration-index.json")

	store := NewRegistrationStore(indexPath)

	// Register a few artifacts.
	manifest1 := &Manifest{
		ArtifactID:   "artifact-one",
		Version:      "1.0.0",
		ProjectID:    "project-alpha",
		Checksum:     "sum1",
		ChecksumType: ChecksumAlgorithmSHA256,
	}

	manifest2 := &Manifest{
		ArtifactID:   "artifact-two",
		Version:      "2.0.0",
		ProjectID:    "project-beta",
		Checksum:     "sum2",
		ChecksumType: ChecksumAlgorithmSHA256,
	}

	if _, err := store.Register(manifest1, "passed"); err != nil {
		t.Fatalf("Register artifact one: %v", err)
	}
	if _, err := store.Register(manifest2, "passed"); err != nil {
		t.Fatalf("Register artifact two: %v", err)
	}

	// Save to disk.
	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify the file exists and is non-empty.
	info, err := os.Stat(indexPath)
	if err != nil {
		t.Fatalf("Stat saved file: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("saved file is empty")
	}

	// Load into a new store (simulating process restart).
	store2 := NewRegistrationStore(indexPath)
	if err := store2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Verify all registrations are restored.
	if !store2.IsRegistered("artifact-one") {
		t.Error("expected artifact-one to be registered after load")
	}
	if !store2.IsRegistered("artifact-two") {
		t.Error("expected artifact-two to be registered after load")
	}

	record, ok := store2.Lookup("artifact-one")
	if !ok {
		t.Fatal("Lookup artifact-one after load returned false")
	}
	if record.Version != "1.0.0" {
		t.Errorf("expected Version 1.0.0 after load, got %s", record.Version)
	}
	if record.ProjectID != "project-alpha" {
		t.Errorf("expected ProjectID project-alpha after load, got %s", record.ProjectID)
	}
}

// TestRegistrationStore_Load_NonExistent verifies that Load returns an error
// for a non-existent file.
func TestRegistrationStore_Load_NonExistent(t *testing.T) {
	store := NewRegistrationStore("/tmp/nonexistent-registration-index-99999.json")
	err := store.Load()
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

// TestRegistrationStore_List verifies that List returns all registered records.
//
// Reference: TS-P3-10
func TestRegistrationStore_List(t *testing.T) {
	store := NewRegistrationStore("")

	records := store.List()
	if len(records) != 0 {
		t.Errorf("expected empty list, got %d records", len(records))
	}

	manifest1 := &Manifest{ArtifactID: "list-artifact-1", Version: "1.0.0", Checksum: "c1", ChecksumType: ChecksumAlgorithmSHA256}
	manifest2 := &Manifest{ArtifactID: "list-artifact-2", Version: "2.0.0", Checksum: "c2", ChecksumType: ChecksumAlgorithmSHA256}
	manifest3 := &Manifest{ArtifactID: "list-artifact-3", Version: "3.0.0", Checksum: "c3", ChecksumType: ChecksumAlgorithmSHA256}

	_, _ = store.Register(manifest1, "passed")
	_, _ = store.Register(manifest2, "passed")
	_, _ = store.Register(manifest3, "passed")

	records = store.List()
	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}

	// Verify all IDs are present.
	ids := make(map[string]bool)
	for _, r := range records {
		ids[r.ArtifactID] = true
	}
	for _, id := range []string{"list-artifact-1", "list-artifact-2", "list-artifact-3"} {
		if !ids[id] {
			t.Errorf("missing artifact %q in List results", id)
		}
	}
}

// TestRegistrationStore_RegistrationDoesNotModifyArtifact verifies that
// Register only records metadata and does not modify the artifact file.
// It creates a temporary file, registers its manifest, and confirms the
// file content is unchanged by comparing SHA-256 hashes.
//
// Reference: ST-P3-08
func TestRegistrationStore_RegistrationDoesNotModifyArtifact(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "test-artifact.bin")

	// Create a simple file to simulate an artifact. Registration does not
	// read or write the artifact file, so any file works for this test.
	originalContent := []byte("this is simulated artifact content")
	if err := os.WriteFile(artifactPath, originalContent, 0644); err != nil {
		t.Fatalf("Write test artifact: %v", err)
	}
	originalSum := fmt.Sprintf("%x", sha256.Sum256(originalContent))

	// Create a manifest referencing this artifact.
	manifest := &Manifest{
		ArtifactID:   "test-no-modify",
		Version:      "1.0.0",
		ProjectID:    "test-project",
		Checksum:     originalSum,
		ChecksumType: ChecksumAlgorithmSHA256,
	}

	store := NewRegistrationStore(filepath.Join(dir, "reg-index.json"))

	_, err := store.Register(manifest, "passed")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Verify the artifact file is unchanged.
	currentData, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("Read artifact file after registration: %v", err)
	}
	currentSum := fmt.Sprintf("%x", sha256.Sum256(currentData))

	if originalSum != currentSum {
		t.Error("artifact file was modified by registration")
	}
}

// TestRegistrationStore_MultipleRegistrations verifies that the store
// supports registering multiple distinct artifacts, looking them up,
// and re-registering the same identity is idempotent.
func TestRegistrationStore_MultipleRegistrations(t *testing.T) {
	store := NewRegistrationStore("")

	manifestA := &Manifest{ArtifactID: "artifact-A", Version: "1.0.0", Checksum: "ca", ChecksumType: ChecksumAlgorithmSHA256}
	manifestB := &Manifest{ArtifactID: "artifact-B", Version: "2.0.0", Checksum: "cb", ChecksumType: ChecksumAlgorithmSHA256}
	manifestC := &Manifest{ArtifactID: "artifact-A", Version: "1.0.0", Checksum: "ca", ChecksumType: ChecksumAlgorithmSHA256} // same ID

	recordA, err := store.Register(manifestA, "passed")
	if err != nil {
		t.Fatalf("Register artifact-A: %v", err)
	}

	recordB, err := store.Register(manifestB, "passed")
	if err != nil {
		t.Fatalf("Register artifact-B: %v", err)
	}

	// Re-registering artifact-A should return the existing record (idempotent).
	recordA2, err := store.Register(manifestC, "passed")
	if err != nil {
		t.Fatalf("Register artifact-A again: %v", err)
	}
	if recordA != recordA2 {
		t.Error("expected same record for idempotent re-registration of artifact-A")
	}

	if recordA == recordB {
		t.Error("expected different records for different artifacts")
	}

	// List should return 2 unique records.
	records := store.List()
	if len(records) != 2 {
		t.Fatalf("expected 2 records (artifact-A and artifact-B), got %d", len(records))
	}
}

// createTestManifest creates a minimal manifest for testing.
func createTestManifest(artifactID, version string) *Manifest {
	return &Manifest{
		ArtifactID:   artifactID,
		Version:      version,
		ProjectID:    "test-project",
		Checksum:     "test-checksum",
		ChecksumType: ChecksumAlgorithmSHA256,
		CreatedAt:    "2026-07-25T12:00:00Z",
		Source:       "test-project",
	}
}
