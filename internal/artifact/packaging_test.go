// Package artifact defines the Anvil artifact directory layout and
// packaging primitives.
//
// Reference: TS-P3-01, TS-P3-02, TS-P3-03, EPIC-003
package artifact

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// setupPackagingSource creates a minimal project source tree in a temp
// directory and returns its root path.
func setupPackagingSource(t *testing.T) string {
	t.Helper()

	root := t.TempDir()

	files := map[string]string{
		"index.php":               "<?php\n",
		"src/App.php":             "<?php namespace App;\n",
		"src/Controller/Home.php": "<?php namespace App\\Controller;\n",
		"config/app.php":          "<?php\nreturn [];\n",
		"public/index.php":        "<?php\n// entry point\n",
		"composer.json":           `{"name": "test/app"}`,
		".gitignore":              "/vendor\n/node_modules\n",
		"README.md":               "# Test Project\n",
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

// readArchiveEntries returns the list of entry names in a tar.gz archive.
func readArchiveEntries(t *testing.T, archivePath string) []string {
	t.Helper()

	f, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("create gzip reader: %v", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	var entries []string

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar entry: %v", err)
		}
		entries = append(entries, hdr.Name)
	}

	return entries
}

// readManifestData returns the raw manifest JSON bytes embedded in a
// tar.gz artifact archive.
func readManifestData(t *testing.T, archivePath string) []byte {
	t.Helper()

	f, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("create gzip reader: %v", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar entry: %v", err)
		}
		if hdr.Name == ManifestFile {
			data, err := io.ReadAll(tr)
			if err != nil {
				t.Fatalf("read manifest content: %v", err)
			}
			return data
		}
	}

	t.Fatalf("manifest %q not found in archive", ManifestFile)
	return nil
}

// TestPackage_CreatesArchive verifies that Package produces a valid tar.gz
// file at the expected output location.
func TestPackage_CreatesArchive(t *testing.T) {
	sourceDir := setupPackagingSource(t)
	outputDir := t.TempDir()

	result, err := Package(PackageOptions{
		SourceDir: sourceDir,
		OutputDir: outputDir,
	})
	if err != nil {
		t.Fatalf("Package returned unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("Package returned nil result")
	}

	if result.ArtifactPath == "" {
		t.Error("ArtifactPath must not be empty")
	}

	// Verify the archive file exists.
	info, err := os.Stat(result.ArtifactPath)
	if err != nil {
		t.Fatalf("stat archive: %v", err)
	}

	if info.Size() == 0 {
		t.Error("archive must not be empty")
	}

	// Verify it's a regular file.
	if !info.Mode().IsRegular() {
		t.Error("archive must be a regular file")
	}

	// Verify the filename follows the expected pattern.
	base := filepath.Base(result.ArtifactPath)
	if !strings.HasPrefix(base, "artifact-") || !strings.HasSuffix(base, ".tar.gz") {
		t.Errorf("unexpected archive filename: %s", base)
	}
}

// TestPackage_IncludesFilteredFiles verifies that the archive contains the
// expected files after filtering.
func TestPackage_IncludesFilteredFiles(t *testing.T) {
	sourceDir := setupPackagingSource(t)
	outputDir := t.TempDir()

	// Exclude .md files.
	result, err := Package(PackageOptions{
		SourceDir: sourceDir,
		OutputDir: outputDir,
		Exclude:   []string{"*.md"},
	})
	if err != nil {
		t.Fatalf("Package returned unexpected error: %v", err)
	}

	entries := readArchiveEntries(t, result.ArtifactPath)

	// The archive should contain all files except README.md.
	for _, entry := range entries {
		if strings.HasSuffix(entry, ".md") {
			t.Errorf("excluded .md file found in archive: %s", entry)
		}
	}

	// Verify README.md is NOT present.
	for _, entry := range entries {
		if entry == filepath.Join(DeployableContentDir, "README.md") {
			t.Error("README.md should not be in archive (excluded via *.md)")
		}
	}

	// Verify a known included file IS present.
	seenApp := false
	for _, entry := range entries {
		if entry == filepath.Join(DeployableContentDir, "src/App.php") {
			seenApp = true
			break
		}
	}
	if !seenApp {
		t.Errorf("expected src/App.php in archive entries, got: %v", entries)
	}
}

// TestPackage_FollowsStructure verifies that the archive follows the defined
// artifact structure (files under DeployableContentDir).
func TestPackage_FollowsStructure(t *testing.T) {
	sourceDir := setupPackagingSource(t)
	outputDir := t.TempDir()

	result, err := Package(PackageOptions{
		SourceDir: sourceDir,
		OutputDir: outputDir,
	})
	if err != nil {
		t.Fatalf("Package returned unexpected error: %v", err)
	}

	entries := readArchiveEntries(t, result.ArtifactPath)

	if len(entries) == 0 {
		t.Fatal("archive must contain at least one entry")
	}

	// Every entry must be under the DeployableContentDir prefix,
	// except for the manifest file which lives at the artifact root.
	for _, entry := range entries {
		if entry == ManifestFile {
			continue
		}
		if !strings.HasPrefix(entry, DeployableContentDir+"/") && entry != DeployableContentDir {
			t.Errorf("entry %q is not under %s/", entry, DeployableContentDir)
		}
	}
}

// TestPackage_FileCountMatches verifies that the reported FileCount matches
// the actual number of entries in the archive.
func TestPackage_FileCountMatches(t *testing.T) {
	sourceDir := setupPackagingSource(t)
	outputDir := t.TempDir()

	result, err := Package(PackageOptions{
		SourceDir: sourceDir,
		OutputDir: outputDir,
	})
	if err != nil {
		t.Fatalf("Package returned unexpected error: %v", err)
	}

	entries := readArchiveEntries(t, result.ArtifactPath)

	// FileCount should match the number of archive entries excluding the
	// manifest (FileCount counts deployable files only).
	entryCount := len(entries)
	for _, e := range entries {
		if e == ManifestFile {
			entryCount--
		}
	}
	if result.FileCount != entryCount {
		t.Errorf("FileCount = %d, but archive has %d deployable entries", result.FileCount, entryCount)
	}
}

// TestPackage_ArchiveIsGzip verifies that the produced file is a valid
// gzip-compressed tar archive by reading the gzip header.
func TestPackage_ArchiveIsGzip(t *testing.T) {
	sourceDir := setupPackagingSource(t)
	outputDir := t.TempDir()

	result, err := Package(PackageOptions{
		SourceDir: sourceDir,
		OutputDir: outputDir,
	})
	if err != nil {
		t.Fatalf("Package returned unexpected error: %v", err)
	}

	// Read the first two bytes to verify gzip magic number.
	f, err := os.Open(result.ArtifactPath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()

	magic := make([]byte, 2)
	if _, err := io.ReadFull(f, magic); err != nil {
		t.Fatalf("read magic bytes: %v", err)
	}

	if magic[0] != 0x1f || magic[1] != 0x8b {
		t.Errorf("invalid gzip magic: %x %x (expected 1f 8b)", magic[0], magic[1])
	}
}

// TestPackage_InvalidSource verifies that a non-existent source directory
// returns an error.
func TestPackage_InvalidSource(t *testing.T) {
	outputDir := t.TempDir()

	_, err := Package(PackageOptions{
		SourceDir: "/tmp/nonexistent-path-12345-test",
		OutputDir: outputDir,
	})
	if err == nil {
		t.Error("expected error for invalid source directory, got nil")
	}
}

// TestPackage_FileAsSource verifies that passing a file as SourceDir returns
// an error.
func TestPackage_FileAsSource(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "somefile.txt")
	if err := os.WriteFile(filePath, []byte("content"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	outputDir := t.TempDir()

	_, err := Package(PackageOptions{
		SourceDir: filePath,
		OutputDir: outputDir,
	})
	if err == nil {
		t.Error("expected error when source is a file, got nil")
	}
}

// TestPackage_OutputCreated verifies that the output directory is created if
// it does not already exist.
func TestPackage_OutputCreated(t *testing.T) {
	sourceDir := setupPackagingSource(t)

	// Use a non-existent output directory.
	base := t.TempDir()
	outputDir := filepath.Join(base, "new", "nested", "output")

	result, err := Package(PackageOptions{
		SourceDir: sourceDir,
		OutputDir: outputDir,
	})
	if err != nil {
		t.Fatalf("Package returned unexpected error: %v", err)
	}

	// Verify the output directory was created.
	info, err := os.Stat(outputDir)
	if err != nil {
		t.Fatalf("stat output dir: %v", err)
	}
	if !info.IsDir() {
		t.Error("output path must be a directory")
	}

	// Verify the archive was written inside it.
	if filepath.Dir(result.ArtifactPath) != outputDir {
		t.Errorf("artifact path %q is not in output dir %q", result.ArtifactPath, outputDir)
	}
}

// TestPackage_ArchiveContentsPreserved verifies that file contents are
// correctly preserved in the archive.
func TestPackage_ArchiveContentsPreserved(t *testing.T) {
	sourceDir := setupPackagingSource(t)
	outputDir := t.TempDir()

	result, err := Package(PackageOptions{
		SourceDir: sourceDir,
		OutputDir: outputDir,
	})
	if err != nil {
		t.Fatalf("Package returned unexpected error: %v", err)
	}

	// Open the archive and verify contents of a known file.
	f, err := os.Open(result.ArtifactPath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	found := false

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar header: %v", err)
		}

		if hdr.Name == filepath.Join(DeployableContentDir, "index.php") {
			found = true
			data, err := io.ReadAll(tr)
			if err != nil {
				t.Fatalf("read file content: %v", err)
			}
			expected := "<?php\n"
			if string(data) != expected {
				t.Errorf("index.php content = %q, want %q", string(data), expected)
			}
		}
	}

	if !found {
		t.Errorf("expected %s/index.php in archive", DeployableContentDir)
	}
}

// TestPackage_ExcludeAll verifies that packaging with a catch-all exclusion
// still produces a valid (but empty) archive.
func TestPackage_ExcludeAll(t *testing.T) {
	sourceDir := setupPackagingSource(t)
	outputDir := t.TempDir()

	result, err := Package(PackageOptions{
		SourceDir: sourceDir,
		OutputDir: outputDir,
		Exclude:   []string{"**"},
	})
	if err != nil {
		t.Fatalf("Package returned unexpected error: %v", err)
	}

	if result.FileCount != 0 {
		t.Errorf("expected FileCount=0 when all excluded, got %d", result.FileCount)
	}

	entries := readArchiveEntries(t, result.ArtifactPath)
	// The manifest is always included, even when all deployable files are
	// excluded. This ensures the artifact still carries identity and metadata.
	if len(entries) != 1 || entries[0] != ManifestFile {
		t.Errorf("expected only manifest in archive, got %d entries: %v", len(entries), entries)
	}
}

// TestPackage_OutputLocation verifies that the archive is written to the
// configured output directory.
func TestPackage_OutputLocation(t *testing.T) {
	sourceDir := setupPackagingSource(t)
	outputDir := t.TempDir()

	result, err := Package(PackageOptions{
		SourceDir: sourceDir,
		OutputDir: outputDir,
	})
	if err != nil {
		t.Fatalf("Package returned unexpected error: %v", err)
	}

	// The artifact path should be inside outputDir.
	artifactDir := filepath.Dir(result.ArtifactPath)
	if artifactDir != outputDir {
		t.Errorf("artifact is in %q, want %q", artifactDir, outputDir)
	}
}

// TestPackage_IncludesManifest verifies that the archive contains a manifest
// file at the expected location.
func TestPackage_IncludesManifest(t *testing.T) {
	sourceDir := setupPackagingSource(t)
	outputDir := t.TempDir()

	result, err := Package(PackageOptions{
		SourceDir: sourceDir,
		OutputDir: outputDir,
		Version:   "1.0.0",
		Source:    "test-project",
	})
	if err != nil {
		t.Fatalf("Package returned unexpected error: %v", err)
	}

	entries := readArchiveEntries(t, result.ArtifactPath)

	manifestFound := false
	for _, entry := range entries {
		if entry == ManifestFile {
			manifestFound = true
			break
		}
	}

	if !manifestFound {
		t.Errorf("manifest %q not found in archive entries: %v", ManifestFile, entries)
	}
}

// TestPackage_ManifestContent verifies that the manifest embedded in the
// artifact contains the expected fields and values.
func TestPackage_ManifestContent(t *testing.T) {
	sourceDir := setupPackagingSource(t)
	outputDir := t.TempDir()

	result, err := Package(PackageOptions{
		SourceDir: sourceDir,
		OutputDir: outputDir,
		Version:   "2.0.0",
		Source:    "my-app",
	})
	if err != nil {
		t.Fatalf("Package returned unexpected error: %v", err)
	}

	// Verify the result includes the manifest.
	if result.Manifest == nil {
		t.Fatal("PackageResult.Manifest is nil")
	}

	if result.Manifest.Version != "2.0.0" {
		t.Errorf("Manifest.Version = %q, want %q", result.Manifest.Version, "2.0.0")
	}

	if result.Manifest.Source != "my-app" {
		t.Errorf("Manifest.Source = %q, want %q", result.Manifest.Source, "my-app")
	}

	if result.Manifest.ArtifactID == "" {
		t.Error("Manifest.ArtifactID is empty")
	}

	if result.Manifest.Checksum == "" {
		t.Error("Manifest.Checksum is empty")
	}

	if result.Manifest.ChecksumType != ChecksumAlgorithmSHA256 {
		t.Errorf("Manifest.ChecksumType = %q, want %q", result.Manifest.ChecksumType, ChecksumAlgorithmSHA256)
	}

	// Verify the manifest from the result matches the one in the archive.
	readManifest, err := ReadManifest(result.ArtifactPath)
	if err != nil {
		t.Fatalf("ReadManifest error: %v", err)
	}

	if readManifest.ArtifactID != result.Manifest.ArtifactID {
		t.Errorf("manifest identity mismatch: %q vs %q", readManifest.ArtifactID, result.Manifest.ArtifactID)
	}

	if readManifest.Checksum != result.Manifest.Checksum {
		t.Errorf("manifest checksum mismatch: %q vs %q", readManifest.Checksum, result.Manifest.Checksum)
	}
}

// TestPackage_DefaultVersion verifies that when no Version is provided, the
// manifest uses "0.0.0" as the default version.
func TestPackage_DefaultVersion(t *testing.T) {
	sourceDir := setupPackagingSource(t)
	outputDir := t.TempDir()

	result, err := Package(PackageOptions{
		SourceDir: sourceDir,
		OutputDir: outputDir,
	})
	if err != nil {
		t.Fatalf("Package returned unexpected error: %v", err)
	}

	if result.Manifest == nil {
		t.Fatal("PackageResult.Manifest is nil")
	}

	if result.Manifest.Version != "0.0.0" {
		t.Errorf("default Manifest.Version = %q, want %q", result.Manifest.Version, "0.0.0")
	}
}

// TestPackage_StoresManifestCommands verifies that activation and
// rollback commands passed via PackageOptions are stored in the manifest
// embedded in the artifact, in the exact order provided (TS-P7-15 AC-2,
// AC-4; TS-P7-16 AC-2).
func TestPackage_StoresManifestCommands(t *testing.T) {
	sourceDir := setupPackagingSource(t)
	outputDir := t.TempDir()

	activationCommands := []string{
		"php artisan migrate --force",
		"php artisan config:cache",
		"php artisan route:cache",
		"php artisan view:cache",
	}
	rollbackCommands := []string{
		"php artisan migrate:rollback",
	}

	result, err := Package(PackageOptions{
		SourceDir:          sourceDir,
		OutputDir:          outputDir,
		Version:            "1.0.0",
		Source:             "test-project",
		ActivationCommands: activationCommands,
		RollbackCommands:   rollbackCommands,
	})
	if err != nil {
		t.Fatalf("Package returned unexpected error: %v", err)
	}

	// The result manifest carries the commands.
	if result.Manifest == nil {
		t.Fatal("PackageResult.Manifest is nil")
	}
	if !reflect.DeepEqual(result.Manifest.ActivationCommands, activationCommands) {
		t.Errorf("result Manifest.ActivationCommands = %v, want %v", result.Manifest.ActivationCommands, activationCommands)
	}
	if !reflect.DeepEqual(result.Manifest.RollbackCommands, rollbackCommands) {
		t.Errorf("result Manifest.RollbackCommands = %v, want %v", result.Manifest.RollbackCommands, rollbackCommands)
	}

	// The manifest embedded in the archive carries the commands in order.
	readManifest, err := ReadManifest(result.ArtifactPath)
	if err != nil {
		t.Fatalf("ReadManifest error: %v", err)
	}
	if !reflect.DeepEqual(readManifest.ActivationCommands, activationCommands) {
		t.Errorf("archive Manifest.ActivationCommands = %v, want %v", readManifest.ActivationCommands, activationCommands)
	}
	if !reflect.DeepEqual(readManifest.RollbackCommands, rollbackCommands) {
		t.Errorf("archive Manifest.RollbackCommands = %v, want %v", readManifest.RollbackCommands, rollbackCommands)
	}
}

// TestPackage_OmitsCommandsWhenNoneProvided verifies that packaging
// without activation/rollback commands produces a manifest WITHOUT the
// command keys (omitempty), preserving backward compatibility with the
// pre-ADR-017 manifest shape (TS-P7-15, TS-P7-16).
func TestPackage_OmitsCommandsWhenNoneProvided(t *testing.T) {
	sourceDir := setupPackagingSource(t)
	outputDir := t.TempDir()

	result, err := Package(PackageOptions{
		SourceDir: sourceDir,
		OutputDir: outputDir,
	})
	if err != nil {
		t.Fatalf("Package returned unexpected error: %v", err)
	}

	// The parsed manifest has nil/empty command fields.
	readManifest, err := ReadManifest(result.ArtifactPath)
	if err != nil {
		t.Fatalf("ReadManifest error: %v", err)
	}
	if len(readManifest.ActivationCommands) != 0 {
		t.Errorf("archive Manifest.ActivationCommands = %v, want empty", readManifest.ActivationCommands)
	}
	if len(readManifest.RollbackCommands) != 0 {
		t.Errorf("archive Manifest.RollbackCommands = %v, want empty", readManifest.RollbackCommands)
	}

	// The raw manifest JSON omits the command keys entirely.
	data := readManifestData(t, result.ArtifactPath)
	if strings.Contains(string(data), "activation_commands") {
		t.Error(`archive manifest contains "activation_commands" when none were provided`)
	}
	if strings.Contains(string(data), "rollback_commands") {
		t.Error(`archive manifest contains "rollback_commands" when none were provided`)
	}
}

// TestPackage_DeterministicContent verifies that the same source and
// configuration produce archives with identical content (for the same files).
// Note: timestamps in tar headers may differ since filenames include a
// timestamp, but the internal file entries should be identical.
func TestPackage_DeterministicContent(t *testing.T) {
	sourceDir := setupPackagingSource(t)
	outputDir := t.TempDir()

	opts := PackageOptions{
		SourceDir: sourceDir,
		OutputDir: outputDir,
		Exclude:   []string{"*.md"},
	}

	result1, err := Package(opts)
	if err != nil {
		t.Fatalf("first Package call failed: %v", err)
	}

	result2, err := Package(opts)
	if err != nil {
		t.Fatalf("second Package call failed: %v", err)
	}

	// The archives should be different files (different timestamps).
	if result1.ArtifactPath == result2.ArtifactPath {
		t.Error("two Package calls produced the same archive path")
	}

	// But the entry lists should be identical (same file set).
	entries1 := readArchiveEntries(t, result1.ArtifactPath)
	entries2 := readArchiveEntries(t, result2.ArtifactPath)

	if len(entries1) != len(entries2) {
		t.Fatalf("entry count differs: %d vs %d", len(entries1), len(entries2))
	}

	for i := range entries1 {
		if entries1[i] != entries2[i] {
			t.Errorf("entry[%d] differs: %q vs %q", i, entries1[i], entries2[i])
		}
	}
}
