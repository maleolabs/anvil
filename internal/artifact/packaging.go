// Package artifact defines the Anvil artifact directory layout and
// packaging primitives.
//
// Reference: TS-P3-01, TS-P3-02, TS-P3-03, ADR-004, EPIC-003
package artifact

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// DefaultFormats is the default set of archive formats to produce.
var DefaultFormats = []string{"tar.gz", "zip"}

// timestampFormat is used to generate unique artifact filenames.
// Nanosecond precision ensures uniqueness within concurrent operations.
const timestampFormat = "20060102-150405.000000000"

// artifactFilenamePattern is the format for artifact archive filenames.
// {timestamp} is formatted using timestampFormat.
const artifactFilenamePattern = "artifact-%s.tar.gz"

// artifactZipFilenamePattern is the format for secondary zip artifact filenames.
const artifactZipFilenamePattern = "artifact-%s.zip"

// PackagingReporter reports progress during artifact packaging.
// Implementations can provide interactive feedback (spinner, colors)
// or silent operation for machine-readable output.
//
// The reporter is optional — nil means no progress reporting.
type PackagingReporter interface {
	// StepStart is called before a packaging step begins.
	StepStart(name string)

	// StepComplete is called when a step finishes successfully.
	StepComplete(name string, duration time.Duration)

	// StepFailed is called when a step fails.
	StepFailed(name string, duration time.Duration, err error)
}

// PackageOptions configures the artifact packaging process.
type PackageOptions struct {
	// SourceDir is the project root directory to package.
	SourceDir string

	// OutputDir is the directory where the artifact archive will be written.
	OutputDir string

	// Formats specifies which archive formats to produce.
	// Supported values: "tar.gz", "zip".
	// When nil or empty, DefaultFormats is used (tar.gz + zip).
	Formats []string

	// Include specifies glob patterns for files to include.
	// When nil or empty, all non-excluded files are included.
	Include []string

	// Exclude specifies glob patterns for files to exclude.
	// When nil, default exclusion rules are used.
	Exclude []string

	// Version is the project version identifier for the manifest.
	// When empty, "0.0.0" is used as a fallback.
	Version string

	// Source is the project name or reference for the manifest.
	Source string

	// ProjectID is the repository project identity for Runtime validation.
	// Distinct from artifact_id (content-derived identity) per ADR-004.
	ProjectID string

	// ActivationCommands are the framework activation commands to store
	// in the manifest (ADR-017), in execution order. The values are
	// supplied by the caller — the CLI wiring layer — from the selected
	// framework's standard executable through the manifest command
	// (e.g. the anvil-standard-laravel repository's ActivationCommands);
	// Core stays framework-agnostic and never imports framework packages
	// (ADR-009 §8.1). When nil or empty, the manifest omits the field.
	//
	// Reference: TS-P7-15, ADR-017
	ActivationCommands []string

	// RollbackCommands are the framework rollback commands to store in
	// the manifest (ADR-017), in execution order. Supplied by the caller
	// from the selected framework adapter, as with ActivationCommands.
	// When nil or empty, the manifest omits the field.
	//
	// Reference: TS-P7-16, ADR-017
	RollbackCommands []string

	// Reporter is an optional progress reporter for packaging steps.
	// When nil, no progress is reported (suitable for machine-readable output).
	Reporter PackagingReporter
}

// PackageResult describes the outcome of a packaging operation.
type PackageResult struct {
	// ArtifactPath is the absolute path to the primary artifact archive (tar.gz).
	ArtifactPath string

	// SecondaryPath is the absolute path to the secondary artifact archive (zip),
	// empty if only one format was produced.
	SecondaryPath string

	// FileCount is the number of files included in the artifact.
	FileCount int

	// Manifest is the manifest embedded in the artifact.
	// Populated when packaging completes successfully.
	Manifest *Manifest
}

// Package creates a distributable artifact from project source files.
//
// The packaging sequence:
//  1. Validate inputs (source must exist, output dir must be creatable).
//  2. Filter project files using the file filtering engine (TS-P3-03).
//  3. Create the output directory if it does not exist.
//  4. Compute content-derived identity (TS-P3-04) from the filtered files.
//  5. Compute integrity checksum (TS-P3-06) from the filtered files.
//  6. Generate the artifact manifest (TS-P3-05) with identity, version,
//     timestamp, source reference, and checksum.
//  7. Generate a unique artifact timestamp.
//  8. Create archives for each requested format (tar.gz, zip, etc.) using
//     the same timestamp so they can be paired.
//  9. Return the result with primary path, optional secondary path,
//     file count, and manifest.
//
// When opts.Reporter is non-nil, progress events are emitted for each step.
// This enables interactive feedback in human-readable mode while keeping
// machine-readable mode (--json) silent.
//
// The produced archive is an immutable, self-describing, distributable
// artifact that downstream capabilities (verification, release, deployment)
// consume.
func Package(opts PackageOptions) (*PackageResult, error) {
	reporter := opts.Reporter

	// --- Step 1: Validate inputs ---
	reportStepStart(reporter, "Validate project")
	sourceInfo, err := os.Stat(opts.SourceDir)
	if err != nil {
		reportStepFailed(reporter, "Validate project", err)
		return nil, fmt.Errorf("access source directory: %w", err)
	}
	if !sourceInfo.IsDir() {
		reportStepFailed(reporter, "Validate project", fmt.Errorf("not a directory"))
		return nil, fmt.Errorf("source path %q is not a directory", opts.SourceDir)
	}

	// Verify the output directory is creatable by attempting to create it.
	if err := os.MkdirAll(opts.OutputDir, 0755); err != nil {
		reportStepFailed(reporter, "Validate project", err)
		return nil, fmt.Errorf("create output directory: %w", err)
	}
	reportStepComplete(reporter, "Validate project")

	// --- Step 2: Filter files ---
	reportStepStart(reporter, "Filter source files")
	filterResult, err := FilterFiles(FilterOptions{
		SourceDir: opts.SourceDir,
		Include:   opts.Include,
		Exclude:   opts.Exclude,
	})
	if err != nil {
		reportStepFailed(reporter, "Filter source files", err)
		return nil, fmt.Errorf("filter source files: %w", err)
	}
	reportStepComplete(reporter, "Filter source files")

	// --- Step 3: Compute content-derived identity (TS-P3-04) ---
	reportStepStart(reporter, "Compute identity")
	identity, err := GenerateIdentity(opts.SourceDir, filterResult.Files)
	if err != nil {
		reportStepFailed(reporter, "Compute identity", err)
		return nil, fmt.Errorf("generate identity: %w", err)
	}
	reportStepComplete(reporter, "Compute identity")

	// --- Step 4: Compute integrity checksum (TS-P3-06) ---
	reportStepStart(reporter, "Compute checksum")
	checksum, err := ComputeChecksum(opts.SourceDir, filterResult.Files)
	if err != nil {
		reportStepFailed(reporter, "Compute checksum", err)
		return nil, fmt.Errorf("compute checksum: %w", err)
	}
	reportStepComplete(reporter, "Compute checksum")

	// --- Step 5: Generate manifest (TS-P3-05) ---
	reportStepStart(reporter, "Generate manifest")
	version := opts.Version
	if version == "" {
		version = "0.0.0"
	}

	manifest := GenerateManifest(
		identity,
		version,
		opts.Source,
		checksum,
		ChecksumAlgorithmSHA256,
		opts.ProjectID,
	)

	// Populate deployment command metadata (ADR-017): the orchestrator
	// reads and executes these commands during release activation and
	// rollback. The values come from the caller (the CLI wiring layer)
	// via the selected framework adapter; Core stays framework-agnostic
	// (ADR-009 §8.1). Nil or empty slices are dropped by omitempty,
	// keeping the manifest backward compatible with old artifacts.
	//
	// Reference: TS-P7-15, TS-P7-16, ADR-017
	manifest.ActivationCommands = opts.ActivationCommands
	manifest.RollbackCommands = opts.RollbackCommands

	manifestBytes, err := MarshalManifest(manifest)
	if err != nil {
		reportStepFailed(reporter, "Generate manifest", err)
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}
	reportStepComplete(reporter, "Generate manifest")

	// --- Step 6: Determine formats ---

	formats := opts.Formats
	if len(formats) == 0 {
		formats = DefaultFormats
	}

	// --- Step 7: Generate shared timestamp ---

	timestamp := time.Now().Format(timestampFormat)

	// --- Step 8: Create archives ---
	reportStepStart(reporter, "Create archives")
	var primaryPath, secondaryPath string

	for _, format := range formats {
		switch format {
		case "tar.gz":
			archiveName := fmt.Sprintf(artifactFilenamePattern, timestamp)
			archivePath := filepath.Join(opts.OutputDir, archiveName)
			if err := createTarGz(archivePath, opts.SourceDir, filterResult.Files, manifestBytes); err != nil {
				reportStepFailed(reporter, "Create archives", err)
				return nil, fmt.Errorf("create tar.gz archive: %w", err)
			}
			primaryPath = archivePath

		case "zip":
			archiveName := fmt.Sprintf(artifactZipFilenamePattern, timestamp)
			archivePath := filepath.Join(opts.OutputDir, archiveName)
			if err := createZip(archivePath, opts.SourceDir, filterResult.Files, manifestBytes); err != nil {
				reportStepFailed(reporter, "Create archives", err)
				return nil, fmt.Errorf("create zip archive: %w", err)
			}
			secondaryPath = archivePath

		default:
			reportStepFailed(reporter, "Create archives", fmt.Errorf("unsupported format: %s", format))
			return nil, fmt.Errorf("unsupported archive format: %s", format)
		}
	}
	reportStepComplete(reporter, "Create archives")

	// If only one format was produced and it's not tar.gz, it becomes the primary.
	if primaryPath == "" && secondaryPath != "" {
		primaryPath = secondaryPath
		secondaryPath = ""
	}

	return &PackageResult{
		ArtifactPath:  primaryPath,
		SecondaryPath: secondaryPath,
		FileCount:     len(filterResult.Files),
		Manifest:      &manifest,
	}, nil
}

// createTarGz builds a tar.gz file at archivePath containing the listed
// deployable files and the optional manifest.
//
// Deployable files are placed under the DeployableContentDir ("app/") prefix.
// The manifest (when non-nil) is written at the artifact root.
func createTarGz(archivePath, sourceDir string, files []string, manifestData []byte) error {
	archiveFile, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("create archive file: %w", err)
	}
	defer archiveFile.Close()

	gzipWriter := gzip.NewWriter(archiveFile)
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	for _, relPath := range files {
		srcPath := filepath.Join(sourceDir, relPath)

		info, err := os.Lstat(srcPath)
		if err != nil {
			return fmt.Errorf("stat %s: %w", relPath, err)
		}

		if !info.Mode().IsRegular() {
			continue
		}

		archiveEntry := filepath.Join(DeployableContentDir, relPath)

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("create tar header for %s: %w", relPath, err)
		}
		header.Name = archiveEntry
		header.Uid = 0
		header.Gid = 0
		header.Uname = ""
		header.Gname = ""
		header.ModTime = time.Time{}

		if err := tarWriter.WriteHeader(header); err != nil {
			return fmt.Errorf("write tar header for %s: %w", relPath, err)
		}

		file, err := os.Open(srcPath)
		if err != nil {
			return fmt.Errorf("open %s: %w", relPath, err)
		}
		if _, err := io.Copy(tarWriter, file); err != nil {
			file.Close()
			return fmt.Errorf("write %s to archive: %w", relPath, err)
		}
		file.Close()
	}

	if len(manifestData) > 0 {
		header := &tar.Header{
			Name:     ManifestFile,
			Size:     int64(len(manifestData)),
			Mode:     0644,
			Uid:      0,
			Gid:      0,
			Uname:    "",
			Gname:    "",
			ModTime:  time.Time{},
			Typeflag: tar.TypeReg,
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return fmt.Errorf("write manifest header: %w", err)
		}
		if _, err := tarWriter.Write(manifestData); err != nil {
			return fmt.Errorf("write manifest content: %w", err)
		}
	}

	return nil
}

// createZip builds a zip file at archivePath containing the same deployable
// files and manifest as the tar.gz variant.
//
// Deployable files are placed under the DeployableContentDir ("app/") prefix.
// The manifest is written at the artifact root.
func createZip(archivePath, sourceDir string, files []string, manifestData []byte) error {
	archiveFile, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("create zip file: %w", err)
	}
	defer archiveFile.Close()

	zipWriter := zip.NewWriter(archiveFile)
	defer zipWriter.Close()

	for _, relPath := range files {
		srcPath := filepath.Join(sourceDir, relPath)

		info, err := os.Lstat(srcPath)
		if err != nil {
			return fmt.Errorf("stat %s: %w", relPath, err)
		}

		if !info.Mode().IsRegular() {
			continue
		}

		archiveEntry := filepath.Join(DeployableContentDir, relPath)

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return fmt.Errorf("create zip header for %s: %w", relPath, err)
		}
		header.Name = archiveEntry
		header.Method = zip.Deflate

		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			return fmt.Errorf("create zip entry for %s: %w", relPath, err)
		}

		file, err := os.Open(srcPath)
		if err != nil {
			return fmt.Errorf("open %s: %w", relPath, err)
		}
		if _, err := io.Copy(writer, file); err != nil {
			file.Close()
			return fmt.Errorf("write %s to zip: %w", relPath, err)
		}
		file.Close()
	}

	if len(manifestData) > 0 {
		header := &zip.FileHeader{
			Name:   ManifestFile,
			Method: zip.Deflate,
		}
		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			return fmt.Errorf("create zip entry for manifest: %w", err)
		}
		if _, err := writer.Write(manifestData); err != nil {
			return fmt.Errorf("write manifest to zip: %w", err)
		}
	}

	return nil
}

// reportStepStart is a helper that calls reporter.StepStart if reporter is non-nil.
func reportStepStart(reporter PackagingReporter, name string) {
	if reporter != nil {
		reporter.StepStart(name)
	}
}

// reportStepComplete is a helper that calls reporter.StepComplete if reporter is non-nil.
func reportStepComplete(reporter PackagingReporter, name string) {
	if reporter != nil {
		reporter.StepComplete(name, 0) // Duration tracked externally
	}
}

// reportStepFailed is a helper that calls reporter.StepFailed if reporter is non-nil.
func reportStepFailed(reporter PackagingReporter, name string, err error) {
	if reporter != nil {
		reporter.StepFailed(name, 0, err) // Duration tracked externally
	}
}
