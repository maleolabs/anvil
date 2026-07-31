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
// The produced archive is an immutable, self-describing, distributable
// artifact that downstream capabilities (verification, release, deployment)
// consume.
func Package(opts PackageOptions) (*PackageResult, error) {
	// --- Step 1: Validate inputs ---

	sourceInfo, err := os.Stat(opts.SourceDir)
	if err != nil {
		return nil, fmt.Errorf("access source directory: %w", err)
	}
	if !sourceInfo.IsDir() {
		return nil, fmt.Errorf("source path %q is not a directory", opts.SourceDir)
	}

	// Verify the output directory is creatable by attempting to create it.
	if err := os.MkdirAll(opts.OutputDir, 0755); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}

	// --- Step 2: Filter files ---

	filterResult, err := FilterFiles(FilterOptions{
		SourceDir: opts.SourceDir,
		Include:   opts.Include,
		Exclude:   opts.Exclude,
	})
	if err != nil {
		return nil, fmt.Errorf("filter source files: %w", err)
	}

	// --- Step 3: Compute content-derived identity (TS-P3-04) ---

	identity, err := GenerateIdentity(opts.SourceDir, filterResult.Files)
	if err != nil {
		return nil, fmt.Errorf("generate identity: %w", err)
	}

	// --- Step 4: Compute integrity checksum (TS-P3-06) ---

	checksum, err := ComputeChecksum(opts.SourceDir, filterResult.Files)
	if err != nil {
		return nil, fmt.Errorf("compute checksum: %w", err)
	}

	// --- Step 5: Generate manifest (TS-P3-05) ---

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

	manifestBytes, err := MarshalManifest(manifest)
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}

	// --- Step 6: Determine formats ---

	formats := opts.Formats
	if len(formats) == 0 {
		formats = DefaultFormats
	}

	// --- Step 7: Generate shared timestamp ---

	timestamp := time.Now().Format(timestampFormat)

	// --- Step 8: Create archives ---

	var primaryPath, secondaryPath string

	for _, format := range formats {
		switch format {
		case "tar.gz":
			archiveName := fmt.Sprintf(artifactFilenamePattern, timestamp)
			archivePath := filepath.Join(opts.OutputDir, archiveName)
			if err := createTarGz(archivePath, opts.SourceDir, filterResult.Files, manifestBytes); err != nil {
				return nil, fmt.Errorf("create tar.gz archive: %w", err)
			}
			primaryPath = archivePath

		case "zip":
			archiveName := fmt.Sprintf(artifactZipFilenamePattern, timestamp)
			archivePath := filepath.Join(opts.OutputDir, archiveName)
			if err := createZip(archivePath, opts.SourceDir, filterResult.Files, manifestBytes); err != nil {
				return nil, fmt.Errorf("create zip archive: %w", err)
			}
			secondaryPath = archivePath

		default:
			return nil, fmt.Errorf("unsupported archive format: %s", format)
		}
	}

	// If only one format was produced and it's not tar.gz, it becomes the primary.
	if primaryPath == "" && secondaryPath != "" {
		primaryPath = secondaryPath
		secondaryPath = ""
	}

	return &PackageResult{
		ArtifactPath:   primaryPath,
		SecondaryPath:  secondaryPath,
		FileCount:      len(filterResult.Files),
		Manifest:       &manifest,
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
