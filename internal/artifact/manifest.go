// Package artifact defines the Anvil artifact directory layout and
// packaging primitives.
//
// Reference: TS-P3-05, ADR-004 §4.1, §3.5, EPIC-003
package artifact

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// Manifest represents the artifact manifest content.
//
// The manifest makes artifacts self-describing and traceable by embedding
// identity, version, provenance, and integrity evidence within the artifact.
//
// Reference: TS-P3-05, ADR-004 §4.1
type Manifest struct {
	// ArtifactID is the content-derived identity from TS-P3-04.
	ArtifactID string `json:"artifact_id"`

	// Version is the project version identifier (SemVer).
	Version string `json:"version"`

	// CreatedAt is the ISO 8601 timestamp of manifest creation.
	CreatedAt string `json:"created_at"`

	// Source is the project name or reference (from project config).
	Source string `json:"source"`

	// Checksum is the integrity checksum from TS-P3-06.
	Checksum string `json:"checksum"`

	// ChecksumType identifies the checksum algorithm used.
	ChecksumType string `json:"checksum_type"`

	// ProjectID is the repository project identity for Runtime validation.
	// It is distinct from ArtifactID (content-derived) and is immutable
	// after packaging per ADR-004.
	ProjectID string `json:"project_id"`
}

// GenerateManifest creates a Manifest struct with the given values.
// The CreatedAt field is set to the current time in ISO 8601 format.
//
// Reference: TS-P3-005, TS-P3-011, ADR-004 §4.1
func GenerateManifest(artifactID, version, source, checksum, checksumType, projectID string) Manifest {
	now := time.Now().UTC()

	return Manifest{
		ArtifactID:   artifactID,
		Version:      version,
		CreatedAt:    now.Format(time.RFC3339),
		Source:       source,
		Checksum:     checksum,
		ChecksumType: checksumType,
		ProjectID:    projectID,
	}
}

// MarshalManifest serializes the manifest to indented JSON bytes.
// The output uses 2-space indentation for readability.
//
// Reference: TS-P3-005
func MarshalManifest(m Manifest) ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// ReadManifest reads and parses the manifest from an artifact archive.
// The manifest is expected at the well-known location defined by ManifestFile.
//
// Returns a parsed Manifest or an error if the manifest is missing, unreadable,
// or contains invalid JSON.
//
// Reference: ST-P3-03, ST-P3-04
func ReadManifest(artifactPath string) (*Manifest, error) {
	f, err := os.Open(artifactPath)
	if err != nil {
		return nil, fmt.Errorf("open artifact: %w", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("create gzip reader: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar entry: %w", err)
		}

		if hdr.Name == ManifestFile {
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("read manifest content: %w", err)
			}

			var manifest Manifest
			if err := json.Unmarshal(data, &manifest); err != nil {
				return nil, fmt.Errorf("parse manifest JSON: %w", err)
			}

			return &manifest, nil
		}
	}

	return nil, fmt.Errorf("manifest not found in artifact")
}

// ReadMetadata is an alias for ReadManifest provided for clarity in the
// metadata propagation context (ST-P3-04). Both functions serve the same
// purpose: extracting artifact metadata from the manifest.
//
// Reference: ST-P3-04
func ReadMetadata(artifactPath string) (*Manifest, error) {
	return ReadManifest(artifactPath)
}
