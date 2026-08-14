package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"maleolabs.com/anvil/internal/artifact"
)

// setupPackagingSource creates a minimal project source tree in a temp
// directory and returns its root path. Mirrors the helper from
// internal/artifact/packaging_test.go so we don't depend on test internals.
func setupPackagingSource(t *testing.T) string {
	t.Helper()

	root := t.TempDir()

	files := map[string]string{
		"index.php":        "<?php\n",
		"src/App.php":      "<?php namespace App;\n",
		"config/app.php":   "<?php\nreturn [];\n",
		"public/index.php": "<?php\n// entry point\n",
		"composer.json":    `{"name": "test/app"}`,
		"README.md":        "# Test Project\n",
	}

	for path, content := range files {
		fullPath := filepath.Join(root, path)
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	return root
}

// packageValidArtifact creates a valid artifact from a temp source directory
// and returns its path. Uses artifact.Package with default options.
func packageValidArtifact(t *testing.T, sourceDir, outputDir string) string {
	t.Helper()

	result, err := artifact.Package(artifact.PackageOptions{
		SourceDir: sourceDir,
		OutputDir: outputDir,
		Version:   "1.0.0",
		Source:    "test-project",
		ProjectID: "test-project",
	})
	if err != nil {
		t.Fatalf("Package failed: %v", err)
	}

	return result.ArtifactPath
}

// TestCreateRelease_ValidArtifact_CreatesRelease verifies that a Release is
// created when the artifact is valid and passes verification.
//
// AC 1: Release created when artifact is verified.
func TestCreateRelease_ValidArtifact_CreatesRelease(t *testing.T) {
	// Arrange: create a valid artifact.
	sourceDir := setupPackagingSource(t)
	outputDir := t.TempDir()
	runtimeDir := t.TempDir()

	artifactPath := packageValidArtifact(t, sourceDir, outputDir)

	// Act: create the release.
	rel, err := CreateRelease(artifactPath, runtimeDir)
	if err != nil {
		t.Fatalf("CreateRelease returned unexpected error: %v", err)
	}

	// Assert: release is not nil, has non-empty ID, stage is "ready".
	if rel == nil {
		t.Fatal("CreateRelease returned nil release")
	}

	if rel.ID == "" {
		t.Error("Release ID must not be empty")
	}

	if rel.Stage != StageReady {
		t.Errorf("Release Stage = %s, want %s", rel.Stage, StageReady)
	}

	// Verify the release file was written to disk.
	s := filepath.Join(runtimeDir, ".anvil", "state", "releases", rel.ID.String()+".json")
	if _, err := os.Stat(s); err != nil {
		t.Errorf("release file not found at %s: %v", s, err)
	}
}

// TestCreateRelease_NonExistentArtifact_ReturnsError verifies that calling
// CreateRelease with a non-existent artifact path returns an error.
//
// AC 2: Fail if artifact doesn't exist.
func TestCreateRelease_NonExistentArtifact_ReturnsError(t *testing.T) {
	runtimeDir := t.TempDir()

	_, err := CreateRelease("/tmp/nonexistent-artifact-99999.tar.gz", runtimeDir)
	if err == nil {
		t.Fatal("CreateRelease should return error for non-existent artifact")
	}

	if !strings.Contains(err.Error(), "artifact not found") {
		t.Errorf("error message should indicate 'artifact not found', got: %v", err)
	}
}

// TestCreateRelease_UnverifiedArtifact_ReturnsError verifies that calling
// CreateRelease with an artifact that fails verification returns an error.
//
// AC 3: Fail if artifact not verified.
func TestCreateRelease_UnverifiedArtifact_ReturnsError(t *testing.T) {
	runtimeDir := t.TempDir()

	// Create an invalid artifact file (empty file — not a valid gzip archive).
	artifactDir := t.TempDir()
	artifactPath := filepath.Join(artifactDir, "invalid-artifact.tar.gz")
	if err := os.WriteFile(artifactPath, []byte("not-a-valid-archive"), 0644); err != nil {
		t.Fatalf("write invalid artifact: %v", err)
	}

	_, err := CreateRelease(artifactPath, runtimeDir)
	if err == nil {
		t.Fatal("CreateRelease should return error for unverified artifact")
	}

	if !strings.Contains(err.Error(), "artifact must be verified first") {
		t.Errorf("error message should contain 'artifact must be verified first', got: %v", err)
	}
}

// TestCreateRelease_StageIsReady verifies that a newly created Release starts
// in the Ready stage.
//
// AC 4: Release initialized in Ready stage.
func TestCreateRelease_StageIsReady(t *testing.T) {
	// Arrange: create a valid artifact.
	sourceDir := setupPackagingSource(t)
	outputDir := t.TempDir()
	runtimeDir := t.TempDir()

	artifactPath := packageValidArtifact(t, sourceDir, outputDir)

	// Act: create the release.
	rel, err := CreateRelease(artifactPath, runtimeDir)
	if err != nil {
		t.Fatalf("CreateRelease returned unexpected error: %v", err)
	}

	// Assert: stage is exactly StageReady.
	if rel.Stage != StageReady {
		t.Errorf("Release Stage = %s, want %s", rel.Stage, StageReady)
	}

	// Also verify via Load to ensure it's persisted correctly.
	s := filepath.Join(runtimeDir, ".anvil", "state", "releases", rel.ID.String()+".json")
	loaded, err := Load(s)
	if err != nil {
		t.Fatalf("Load release from %s: %v", s, err)
	}

	if loaded.Stage != StageReady {
		t.Errorf("Loaded Release Stage = %s, want %s", loaded.Stage, StageReady)
	}
}

// TestCreateRelease_ReferencesArtifactAndRuntime verifies that the created
// Release correctly references the artifact and target Runtime.
//
// AC 5: Release references artifact and target Runtime.
func TestCreateRelease_ReferencesArtifactAndRuntime(t *testing.T) {
	// Arrange: create a valid artifact with known properties.
	sourceDir := setupPackagingSource(t)
	outputDir := t.TempDir()
	runtimeDir := t.TempDir()

	artifactPath := packageValidArtifact(t, sourceDir, outputDir)

	// Read the manifest to verify against the release's ArtifactID.
	manifest, err := artifact.ReadManifest(artifactPath)
	if err != nil {
		t.Fatalf("ReadManifest failed: %v", err)
	}

	// Act: create the release.
	rel, err := CreateRelease(artifactPath, runtimeDir)
	if err != nil {
		t.Fatalf("CreateRelease returned unexpected error: %v", err)
	}

	// Assert: all reference fields are populated correctly.
	if rel.ArtifactID == "" {
		t.Error("Release ArtifactID must not be empty")
	}

	if rel.ArtifactID != manifest.ArtifactID {
		t.Errorf("Release ArtifactID = %q, want %q (from manifest)", rel.ArtifactID, manifest.ArtifactID)
	}

	if rel.Version == "" {
		t.Error("Release Version must not be empty")
	}

	if rel.Version != manifest.Version {
		t.Errorf("Release Version = %q, want %q (from manifest)", rel.Version, manifest.Version)
	}

	if rel.ArtifactPath != artifactPath {
		t.Errorf("Release ArtifactPath = %q, want %q", rel.ArtifactPath, artifactPath)
	}

	if rel.RuntimePath != runtimeDir {
		t.Errorf("Release RuntimePath = %q, want %q", rel.RuntimePath, runtimeDir)
	}

	if rel.CreatedAt == "" {
		t.Error("Release CreatedAt must not be empty")
	}

	if rel.ID == "" {
		t.Error("Release ID must not be empty")
	}
}

// TestCreateRelease_CreatesStateDirectory verifies that the releases state
// directory is created when it does not already exist.
func TestCreateRelease_CreatesStateDirectory(t *testing.T) {
	sourceDir := setupPackagingSource(t)
	outputDir := t.TempDir()
	runtimeDir := t.TempDir()

	artifactPath := packageValidArtifact(t, sourceDir, outputDir)

	// Ensure the .anvil directory does not exist yet.
	anvilDir := filepath.Join(runtimeDir, ".anvil")
	if _, err := os.Stat(anvilDir); !os.IsNotExist(err) {
		t.Fatal("expected .anvil directory to not exist before CreateRelease")
	}

	rel, err := CreateRelease(artifactPath, runtimeDir)
	if err != nil {
		t.Fatalf("CreateRelease returned unexpected error: %v", err)
	}

	// Verify the releases directory was created.
	releasesDir := filepath.Join(runtimeDir, ".anvil", "state", "releases")
	info, err := os.Stat(releasesDir)
	if err != nil {
		t.Fatalf("stat releases directory: %v", err)
	}
	if !info.IsDir() {
		t.Error("releases path must be a directory")
	}

	// Verify the release file exists inside.
	releasePath := filepath.Join(releasesDir, rel.ID.String()+".json")
	if _, err := os.Stat(releasePath); err != nil {
		t.Errorf("release file not found at %s: %v", releasePath, err)
	}
}

// TestSaveAndLoad_PersistenceRoundTrip verifies that a Release can be saved
// to disk and loaded back with identical field values.
func TestSaveAndLoad_PersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-release.json")

	original := &Release{
		ID:           ReleaseID("abc123"),
		ArtifactID:   "def456",
		Version:      "1.0.0",
		ArtifactPath: "/tmp/artifact.tar.gz",
		RuntimePath:  "/tmp/runtime",
		Stage:        StageReady,
		CreatedAt:    "2026-07-25T12:00:00Z",
		Transitions:  []TransitionRecord{},
	}

	if err := original.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.ID != original.ID {
		t.Errorf("ID = %q, want %q", loaded.ID, original.ID)
	}
	if loaded.ArtifactID != original.ArtifactID {
		t.Errorf("ArtifactID = %q, want %q", loaded.ArtifactID, original.ArtifactID)
	}
	if loaded.Version != original.Version {
		t.Errorf("Version = %q, want %q", loaded.Version, original.Version)
	}
	if loaded.ArtifactPath != original.ArtifactPath {
		t.Errorf("ArtifactPath = %q, want %q", loaded.ArtifactPath, original.ArtifactPath)
	}
	if loaded.RuntimePath != original.RuntimePath {
		t.Errorf("RuntimePath = %q, want %q", loaded.RuntimePath, original.RuntimePath)
	}
	if loaded.Stage != original.Stage {
		t.Errorf("Stage = %s, want %s", loaded.Stage, original.Stage)
	}
	if loaded.CreatedAt != original.CreatedAt {
		t.Errorf("CreatedAt = %q, want %q", loaded.CreatedAt, original.CreatedAt)
	}
}

// TestLoad_NonExistentFile_ReturnsError verifies that Load returns an error
// when the file does not exist.
func TestLoad_NonExistentFile_ReturnsError(t *testing.T) {
	_, err := Load("/tmp/nonexistent-release-99999.json")
	if err == nil {
		t.Fatal("Load should return error for non-existent file")
	}

	if !strings.Contains(err.Error(), "release file not found") {
		t.Errorf("error should indicate file not found, got: %v", err)
	}
}

// TestReleaseID_Uniqueness verifies that multiple GenerateReleaseID calls
// produce distinct identities.
//
// Reference: TS-P4-02 AC-1, AC-2
func TestReleaseID_Uniqueness(t *testing.T) {
	ids := make(map[ReleaseID]bool)

	for i := 0; i < 100; i++ {
		id, err := GenerateReleaseID()
		if err != nil {
			t.Fatalf("GenerateReleaseID failed: %v", err)
		}

		if ids[id] {
			t.Errorf("duplicate ReleaseID generated: %s", id)
		}
		ids[id] = true
	}
}

// TestReleaseID_Format verifies that GenerateReleaseID produces a 32-character
// lowercase hex string.
//
// Reference: TS-P4-02 AC-1
func TestReleaseID_Format(t *testing.T) {
	id, err := GenerateReleaseID()
	if err != nil {
		t.Fatalf("GenerateReleaseID failed: %v", err)
	}

	if len(id) != 32 {
		t.Errorf("ReleaseID length = %d, want 32", len(id))
	}

	// Verify only hex characters.
	s := string(id)
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("invalid ReleaseID character: %c", c)
		}
	}
}

// TestReleaseID_ChronologicalSorting verifies that identities generated at
// different times sort lexicographically in chronological order.
//
// The identity uses millisecond timestamp precision. IDs generated in the
// same millisecond may have the same timestamp prefix — the random suffix
// determines ordering in that case, which is acceptable because the
// identities are still unique and the chronological ordering holds across
// different millisecond boundaries.
//
// Reference: TS-P4-02 AC-5
func TestReleaseID_ChronologicalSorting(t *testing.T) {
	id1, err := GenerateReleaseID()
	if err != nil {
		t.Fatalf("GenerateReleaseID failed: %v", err)
	}

	// Sleep to ensure a different millisecond timestamp.
	time.Sleep(2 * time.Millisecond)

	id2, err := GenerateReleaseID()
	if err != nil {
		t.Fatalf("GenerateReleaseID failed: %v", err)
	}

	if string(id2) < string(id1) {
		t.Errorf("id2 (%s) must be >= id1 (%s) for chronological ordering", id2, id1)
	}
}

// TestCreateRelease_PopulatesMetadataFromManifest verifies that CreateRelease
// populates all metadata fields from the artifact manifest.
//
// Reference: TS-P4-03 AC-1, AC-2, ST-P4-03 AC-1, AC-2, AC-3
func TestCreateRelease_PopulatesMetadataFromManifest(t *testing.T) {
	// Arrange: create a valid artifact.
	sourceDir := setupPackagingSource(t)
	outputDir := t.TempDir()
	runtimeDir := t.TempDir()

	artifactPath := packageValidArtifact(t, sourceDir, outputDir)

	// Read the manifest for expected values.
	manifest, err := artifact.ReadManifest(artifactPath)
	if err != nil {
		t.Fatalf("ReadManifest failed: %v", err)
	}

	// Act: create the release.
	rel, err := CreateRelease(artifactPath, runtimeDir)
	if err != nil {
		t.Fatalf("CreateRelease returned unexpected error: %v", err)
	}

	// Assert: all metadata fields are populated correctly.
	if rel.Version == "" {
		t.Error("Release Version must not be empty")
	}
	if rel.Version != manifest.Version {
		t.Errorf("Release Version = %q, want %q", rel.Version, manifest.Version)
	}

	if rel.ArtifactID == "" {
		t.Error("Release ArtifactID must not be empty")
	}
	if rel.ArtifactID != manifest.ArtifactID {
		t.Errorf("Release ArtifactID = %q, want %q", rel.ArtifactID, manifest.ArtifactID)
	}

	if rel.CreatedAt == "" {
		t.Error("Release CreatedAt must not be empty")
	}

	if rel.RuntimePath != runtimeDir {
		t.Errorf("Release RuntimePath = %q, want %q", rel.RuntimePath, runtimeDir)
	}
}

// TestLookupByID_ExistingRelease_ReturnsRelease verifies that LookupByID
// retrieves an existing Release by its identity.
//
// Reference: TS-P4-03 AC-3
func TestLookupByID_ExistingRelease_ReturnsRelease(t *testing.T) {
	// Arrange: create a valid artifact and release.
	sourceDir := setupPackagingSource(t)
	outputDir := t.TempDir()
	runtimeDir := t.TempDir()

	artifactPath := packageValidArtifact(t, sourceDir, outputDir)
	rel, err := CreateRelease(artifactPath, runtimeDir)
	if err != nil {
		t.Fatalf("CreateRelease failed: %v", err)
	}

	// Act: look up the release by its identity.
	found, err := LookupByID(runtimeDir, rel.ID)
	if err != nil {
		t.Fatalf("LookupByID returned unexpected error: %v", err)
	}

	// Assert: the retrieved release matches the original.
	if found.ID != rel.ID {
		t.Errorf("found.ID = %q, want %q", found.ID, rel.ID)
	}
	if found.ArtifactID != rel.ArtifactID {
		t.Errorf("found.ArtifactID = %q, want %q", found.ArtifactID, rel.ArtifactID)
	}
	if found.Version != rel.Version {
		t.Errorf("found.Version = %q, want %q", found.Version, rel.Version)
	}
	if found.Stage != rel.Stage {
		t.Errorf("found.Stage = %q, want %q", found.Stage, rel.Stage)
	}
	if found.CreatedAt != rel.CreatedAt {
		t.Errorf("found.CreatedAt = %q, want %q", found.CreatedAt, rel.CreatedAt)
	}
}

// TestLookupByID_NonExistentRelease_ReturnsError verifies that LookupByID
// returns an error when the Release does not exist.
//
// Reference: TS-P4-03 AC-3
func TestLookupByID_NonExistentRelease_ReturnsError(t *testing.T) {
	runtimeDir := t.TempDir()
	nonExistentID := ReleaseID("00000000000000000000000000000000")

	_, err := LookupByID(runtimeDir, nonExistentID)
	if err == nil {
		t.Fatal("LookupByID should return error for non-existent release")
	}

	if !strings.Contains(err.Error(), "release file not found") {
		t.Errorf("error should indicate 'release file not found', got: %v", err)
	}
}
