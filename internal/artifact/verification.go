// Package artifact defines the Anvil artifact directory layout and
// packaging primitives.
//
// Reference: TS-P3-07, ST-P3-06, ADR-004 §8.9, EPIC-003
package artifact

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"maleolabs.com/anvil/internal/config"
)

// CheckResult represents the result of a single verification check.
type CheckResult struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Details string `json:"details,omitempty"`
}

// VerificationResult represents the consolidated verification outcome.
type VerificationResult struct {
	Passed bool          `json:"passed"`
	Checks []CheckResult `json:"checks"`
}

// requiredManifestFields lists all manifest fields that must be non-empty
// for the manifest content check to pass.
var requiredManifestFields = []string{
	"artifact_id",
	"version",
	"created_at",
	"source",
	"checksum",
	"checksum_type",
	"project_id",
}

// VerifyArtifact validates the integrity of an artifact archive by running
// six sequential checks:
//
//  1. Archive validity      — confirms the tar.gz file is readable, valid gzip.
//  2. Manifest presence     — confirms manifest.json exists at the artifact root.
//  3. Manifest content      — validates all required manifest fields are present.
//  4. Project identity      — validates the project identity format.
//  5. Checksum match        — recomputes checksum over deployable content and
//     compares to the manifest value.
//  6. Artifact immutability — verifies the deployable content has not been
//     altered since the manifest was created.
//
// All checks run regardless of intermediate failures. The result indicates
// pass only when every check passes.
//
// Reference: TS-P3-07, TS-P3-08, TS-P3-10, ADR-004 §8.9, §8.1/§8.3/§8.6/§8.7
func VerifyArtifact(artifactPath string) (*VerificationResult, error) {
	// Verify the file exists before doing anything else.
	if _, err := os.Stat(artifactPath); err != nil {
		return nil, fmt.Errorf("access artifact: %w", err)
	}

	result := &VerificationResult{
		Checks: make([]CheckResult, 0, 6),
	}

	// Check 1: Archive validity
	checkArchive := checkArchiveValidity(artifactPath)
	result.Checks = append(result.Checks, checkArchive)

	// Check 2: Manifest presence
	checkPresence, manifest := checkManifestPresence(artifactPath)
	result.Checks = append(result.Checks, checkPresence)

	// Check 3: Manifest content
	checkContent := checkManifestContent(manifest)
	result.Checks = append(result.Checks, checkContent)

	// Check 4: Project identity
	checkIdentity := checkProjectIdentity(manifest)
	result.Checks = append(result.Checks, checkIdentity)

	// Check 5: Checksum match
	checkChecksum := checkChecksumMatch(artifactPath, manifest)
	result.Checks = append(result.Checks, checkChecksum)

	// Check 6: Artifact immutability
	checkImmutability := checkArtifactImmutability(artifactPath)
	result.Checks = append(result.Checks, checkImmutability)

	// Consolidate: pass if ALL checks pass.
	result.Passed = true
	for _, c := range result.Checks {
		if !c.Passed {
			result.Passed = false
			break
		}
	}

	return result, nil
}

// checkArchiveValidity verifies the archive is a valid gzip-compressed tar file.
func checkArchiveValidity(artifactPath string) CheckResult {
	f, err := os.Open(artifactPath)
	if err != nil {
		return CheckResult{
			Name:    "Archive validity",
			Passed:  false,
			Details: fmt.Sprintf("cannot open file: %v", err),
		}
	}
	defer f.Close()

	// Check gzip magic bytes: 0x1f 0x8b.
	magic := make([]byte, 2)
	if _, err := io.ReadFull(f, magic); err != nil {
		return CheckResult{
			Name:    "Archive validity",
			Passed:  false,
			Details: fmt.Sprintf("cannot read magic bytes: %v", err),
		}
	}

	if magic[0] != 0x1f || magic[1] != 0x8b {
		return CheckResult{
			Name:    "Archive validity",
			Passed:  false,
			Details: fmt.Sprintf("invalid gzip magic: got %02x %02x, expected 1f 8b", magic[0], magic[1]),
		}
	}

	// Seek back to the beginning so the gzip reader can parse the header.
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return CheckResult{
			Name:    "Archive validity",
			Passed:  false,
			Details: fmt.Sprintf("cannot seek: %v", err),
		}
	}

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return CheckResult{
			Name:    "Archive validity",
			Passed:  false,
			Details: fmt.Sprintf("invalid gzip format: %v", err),
		}
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	// Read at least one entry to confirm the tar is readable.
	_, err = tr.Next()
	if err != nil && err != io.EOF {
		return CheckResult{
			Name:    "Archive validity",
			Passed:  false,
			Details: fmt.Sprintf("invalid tar archive: %v", err),
		}
	}

	return CheckResult{
		Name:    "Archive validity",
		Passed:  true,
		Details: "valid",
	}
}

// checkManifestPresence verifies that the manifest exists in the archive.
// It returns the check result and, on success, the parsed Manifest.
func checkManifestPresence(artifactPath string) (CheckResult, *Manifest) {
	manifest, err := ReadManifest(artifactPath)
	if err != nil {
		return CheckResult{
			Name:    "Manifest presence",
			Passed:  false,
			Details: fmt.Sprintf("not found: %v", err),
		}, nil
	}

	return CheckResult{
		Name:    "Manifest presence",
		Passed:  true,
		Details: "found",
	}, manifest
}

// checkManifestContent validates that all required manifest fields are
// non-empty. Returns a passing check when manifest is nil to allow other
// checks to continue independently.
func checkManifestContent(manifest *Manifest) CheckResult {
	if manifest == nil {
		return CheckResult{
			Name:    "Manifest content",
			Passed:  false,
			Details: "manifest not available",
		}
	}

	// Map field names to their values for validation.
	fields := map[string]string{
		"artifact_id":   manifest.ArtifactID,
		"version":       manifest.Version,
		"created_at":    manifest.CreatedAt,
		"source":        manifest.Source,
		"checksum":      manifest.Checksum,
		"checksum_type": manifest.ChecksumType,
		"project_id":    manifest.ProjectID,
	}

	var missing []string
	for _, name := range requiredManifestFields {
		if fields[name] == "" {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		return CheckResult{
			Name:    "Manifest content",
			Passed:  false,
			Details: fmt.Sprintf("missing required field(s): %s", strings.Join(missing, ", ")),
		}
	}

	return CheckResult{
		Name:    "Manifest content",
		Passed:  true,
		Details: "complete",
	}
}

// checkProjectIdentity validates that the manifest's project identity is
// properly formatted. This is the 4th verification check.
//
// The check is independent of checksum and immutability verification but
// requires the manifest to be available.
//
// Reference: ST-P3-10, ADR-004 §3.5
func checkProjectIdentity(manifest *Manifest) CheckResult {
	if manifest == nil {
		return CheckResult{
			Name:    "Project identity",
			Passed:  false,
			Details: "manifest not available",
		}
	}

	if err := config.ValidateProjectName(manifest.ProjectID); err != nil {
		return CheckResult{
			Name:    "Project identity",
			Passed:  false,
			Details: err.Error(),
		}
	}

	return CheckResult{
		Name:    "Project identity",
		Passed:  true,
		Details: "valid",
	}
}

// checkChecksumMatch recomputes the checksum over the deployable content
// of the archive and compares it to the manifest value.
func checkChecksumMatch(artifactPath string, manifest *Manifest) CheckResult {
	if manifest == nil {
		return CheckResult{
			Name:    "Checksum match",
			Passed:  false,
			Details: "manifest not available for checksum comparison",
		}
	}

	// Extract deployable files to a temporary directory.
	tmpDir, err := os.MkdirTemp("", "anvil-verify-*")
	if err != nil {
		return CheckResult{
			Name:    "Checksum match",
			Passed:  false,
			Details: fmt.Sprintf("cannot create temp directory: %v", err),
		}
	}
	defer os.RemoveAll(tmpDir)

	files, err := extractDeployableContent(artifactPath, tmpDir)
	if err != nil {
		return CheckResult{
			Name:    "Checksum match",
			Passed:  false,
			Details: fmt.Sprintf("cannot extract deployable content: %v", err),
		}
	}

	if len(files) == 0 {
		// No deployable files — checksum should be empty.
		if manifest.Checksum == "" {
			return CheckResult{
				Name:    "Checksum match",
				Passed:  true,
				Details: "verified (empty content)",
			}
		}

		return CheckResult{
			Name:    "Checksum match",
			Passed:  false,
			Details: fmt.Sprintf("no deployable files, but manifest checksum is %q", manifest.Checksum),
		}
	}

	computedChecksum, err := ComputeChecksum(tmpDir, files)
	if err != nil {
		return CheckResult{
			Name:    "Checksum match",
			Passed:  false,
			Details: fmt.Sprintf("checksum computation failed: %v", err),
		}
	}

	if computedChecksum != manifest.Checksum {
		return CheckResult{
			Name:    "Checksum match",
			Passed:  false,
			Details: fmt.Sprintf("expected %s, got %s", manifest.Checksum, computedChecksum),
		}
	}

	return CheckResult{
		Name:    "Checksum match",
		Passed:  true,
		Details: "verified",
	}
}

// safeExtractPath validates that an archive entry's resolved extraction path
// stays within the intended destination directory. It returns the resolved
// absolute target path or an error if the entry would escape the extraction
// root or uses an unsafe path.
//
// The validation rejects:
//   - empty entry names
//   - absolute paths (e.g., "/etc/passwd")
//   - parent-directory traversal (e.g., "../../etc/passwd")
//   - any resolved path outside destDir
//
// Reference: TD-001, ADR-004 §8.9, ADR-014
func safeExtractPath(destDir, entryName string) (string, error) {
	if entryName == "" {
		return "", fmt.Errorf("empty entry name")
	}

	// Reject absolute paths in entry names.
	if filepath.IsAbs(entryName) {
		return "", fmt.Errorf("absolute path not allowed: %s", entryName)
	}

	// Clean the entry name to resolve any ".." components before joining.
	cleanName := filepath.Clean(entryName)
	if cleanName == "." {
		return "", fmt.Errorf("entry name resolves to extraction root")
	}

	// Join with destDir to get the intended target path.
	targetPath := filepath.Join(destDir, cleanName)

	// Resolve to absolute paths for comparison.
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return "", fmt.Errorf("resolve target path: %w", err)
	}

	absDest, err := filepath.Abs(destDir)
	if err != nil {
		return "", fmt.Errorf("resolve destination: %w", err)
	}

	// If the resolved target does not start with the resolved destDir,
	// the entry would escape the extraction root.
	//
	// The separator suffix prevents a destDir like "/tmp/foo" from matching
	// a target like "/tmp/foobar".
	prefix := absDest + string(filepath.Separator)
	if !strings.HasPrefix(absTarget, prefix) && absTarget != absDest {
		return "", fmt.Errorf("path traversal detected: %q escapes extraction root %q", entryName, destDir)
	}

	return absTarget, nil
}

// validatedEntry represents an archive entry that has passed path-boundary
// validation and is ready for extraction.
type validatedEntry struct {
	relPath  string
	typeflag byte
	linkname string
}

// extractDeployableContent extracts all files under DeployableContentDir from
// the archive into destDir, stripping the DeployableContentDir prefix.
// Returns the list of relative paths (without the prefix) of extracted files.
//
// The function uses a two-phase approach:
//  1. Phase 1 — Validate: iterate through all archive entries, validate each
//     path against the extraction root boundary. Collect safe entries.
//  2. Phase 2 — Extract: re-open the archive and extract only the validated
//     entries. No filesystem writes occur before Phase 2.
//
// This ensures that a single unsafe entry rejects the entire extraction
// without leaving partial output in destDir.
//
// Reference: TS-P3-07, TD-001, ADR-004 §8.9
func extractDeployableContent(artifactPath, destDir string) ([]string, error) {
	// --- Phase 1: Validate all entries ---
	entries, err := validateArchiveEntries(artifactPath, destDir)
	if err != nil {
		return nil, err
	}

	// --- Phase 2: Extract validated entries ---
	return extractValidatedEntries(artifactPath, destDir, entries)
}

// validateArchiveEntries reads all archive entries under DeployableContentDir,
// validates their paths against the extraction root boundary, and returns the
// list of validated entries. No filesystem writes occur during validation.
//
// Returns an error if any entry fails path validation, ensuring no unsafe
// entry reaches the extraction phase.
func validateArchiveEntries(artifactPath, destDir string) ([]validatedEntry, error) {
	f, err := os.Open(artifactPath)
	if err != nil {
		return nil, fmt.Errorf("open archive for validation: %w", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("create gzip reader for validation: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	var entries []validatedEntry
	prefix := DeployableContentDir + "/"

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar entry during validation: %w", err)
		}

		// Skip entries outside the deployable content prefix.
		if !strings.HasPrefix(hdr.Name, prefix) {
			continue
		}

		relPath := strings.TrimPrefix(hdr.Name, prefix)

		// Validate path-boundary for the entry name itself.
		if _, err := safeExtractPath(destDir, relPath); err != nil {
			return nil, fmt.Errorf("unsafe entry %q: %w", hdr.Name, err)
		}

		switch hdr.Typeflag {
		case tar.TypeReg:
			entries = append(entries, validatedEntry{
				relPath:  relPath,
				typeflag: hdr.Typeflag,
			})

		case tar.TypeSymlink, tar.TypeLink:
			// Validate link target against the extraction root.
			if _, err := safeExtractPath(destDir, hdr.Linkname); err != nil {
				return nil, fmt.Errorf("unsafe link target in entry %q: %w", hdr.Name, err)
			}
			// Safe link entries are tracked so the extraction phase
			// can skip them appropriately. Currently they are not
			// extracted but are validated for security.
			entries = append(entries, validatedEntry{
				relPath:  relPath,
				typeflag: hdr.Typeflag,
				linkname: hdr.Linkname,
			})

		default:
			// Directories and other non-regular, non-link entries
			// are silently skipped.
		}
	}

	return entries, nil
}

// extractValidatedEntries re-opens the archive and extracts only the
// pre-validated entries into destDir. No path validation is performed
// during extraction — all entries have already passed validation.
func extractValidatedEntries(artifactPath, destDir string, entries []validatedEntry) ([]string, error) {
	f, err := os.Open(artifactPath)
	if err != nil {
		return nil, fmt.Errorf("open archive for extraction: %w", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("create gzip reader for extraction: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	var files []string

	// Build a set of entries to extract for efficient lookup.
	type entryKey struct {
		relPath  string
		typeflag byte
	}
	toExtract := make(map[entryKey]bool)
	for _, e := range entries {
		toExtract[entryKey{relPath: e.relPath, typeflag: e.typeflag}] = true
	}

	prefix := DeployableContentDir + "/"

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar entry during extraction: %w", err)
		}

		if !strings.HasPrefix(hdr.Name, prefix) {
			continue
		}

		relPath := strings.TrimPrefix(hdr.Name, prefix)

		switch hdr.Typeflag {
		case tar.TypeReg:
			if !toExtract[entryKey{relPath: relPath, typeflag: hdr.Typeflag}] {
				continue
			}

			targetPath := filepath.Join(destDir, relPath)
			targetDir := filepath.Dir(targetPath)

			if err := os.MkdirAll(targetDir, 0755); err != nil {
				return nil, fmt.Errorf("create directory %s: %w", targetDir, err)
			}

			outFile, err := os.Create(targetPath)
			if err != nil {
				return nil, fmt.Errorf("create file %s: %w", relPath, err)
			}

			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return nil, fmt.Errorf("write file %s: %w", relPath, err)
			}
			outFile.Close()

			files = append(files, relPath)

		default:
			// Non-regular entries are skipped during extraction;
			// they were only validated in the first pass.
		}
	}

	return files, nil
}

// checkArtifactImmutability verifies that the artifact's deployable content
// matches the checksum recorded in its manifest. This is the 6th verification
// check and calls VerifyImmutability for the actual comparison logic.
//
// Reference: TS-P3-08, ADR-004 §8.1/§8.3/§8.6/§8.7
func checkArtifactImmutability(artifactPath string) CheckResult {
	result, err := VerifyImmutability(artifactPath)
	if err != nil {
		return CheckResult{
			Name:    "Artifact immutability",
			Passed:  false,
			Details: fmt.Sprintf("verification error: %v", err),
		}
	}

	if !result.Passed {
		return CheckResult{
			Name:    "Artifact immutability",
			Passed:  false,
			Details: result.Details,
		}
	}

	return CheckResult{
		Name:    "Artifact immutability",
		Passed:  true,
		Details: "verified",
	}
}

// ExtractArtifact extracts all deployable content from a verified artifact
// (.tar.gz) into the specified destination directory.
//
// Only files under the DeployableContentDir ("app/") prefix are extracted;
// the prefix is stripped from the destination paths. The manifest and other
// metadata files are not extracted into the destination.
//
// The extraction uses a two-phase secure approach:
//  1. Validate — iterates all entries and validates path boundaries
//  2. Extract — writes only pre-validated entries to disk
//
// If any entry fails path validation, the entire extraction is aborted
// and no partial output is left in destDir.
//
// Reference: ST-P4-13, ADR-004 §8.9
func ExtractArtifact(artifactPath, destDir string) error {
	if _, err := os.Stat(artifactPath); err != nil {
		return fmt.Errorf("artifact not found: %w", err)
	}

	if _, err := extractDeployableContent(artifactPath, destDir); err != nil {
		return fmt.Errorf("extract artifact: %w", err)
	}

	return nil
}

// RequireVerified checks whether an artifact has passed verification.
// It runs verification and returns an error if verification fails.
// Returns nil if the artifact passes all verification checks.
//
// Reference: ST-P3-06, ADR-004 §8.9
func RequireVerified(artifactPath string) error {
	// Check that the artifact file exists before launching verification.
	if _, err := os.Stat(artifactPath); err != nil {
		return fmt.Errorf("artifact not found: %w", err)
	}

	result, err := VerifyArtifact(artifactPath)
	if err != nil {
		return fmt.Errorf("artifact verification failed: %w", err)
	}

	if result.Passed {
		return nil
	}

	// Collect failure details for a descriptive error message.
	var failures []string
	for _, check := range result.Checks {
		if !check.Passed {
			msg := check.Name
			if check.Details != "" {
				msg += ": " + check.Details
			}
			failures = append(failures, msg)
		}
	}

	return fmt.Errorf("artifact verification failed: %s", strings.Join(failures, "; "))
}
