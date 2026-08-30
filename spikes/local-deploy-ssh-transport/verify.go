package spksshtransport

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"maleolabs.com/anvil/internal/artifact"
)

// VerifyChecksum verifies artifact integrity via artifact.VerifyArtifact + manifest checksum compare.
// Returns nil if valid, error otherwise.
func VerifyChecksum(artifactPath string) error {
	result, err := artifact.VerifyArtifact(artifactPath)
	if err != nil {
		return fmt.Errorf("VerifyArtifact: %w", err)
	}
	if !result.Passed {
		for _, c := range result.Checks {
			if !c.Passed {
				return fmt.Errorf("verify failed: %s: %s", c.Name, c.Details)
			}
		}
		return fmt.Errorf("verify failed: unknown")
	}
	return nil
}

// ExtractManifest reads manifest.json from artifact tar.gz (raw bytes).
func ExtractManifest(artifactPath string) (*artifact.Manifest, []byte, error) {
	f, err := os.Open(artifactPath)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	gzr, err := gzip.NewReader(f)
	if err != nil {
		return nil, nil, err
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		if hdr.Name == artifact.ManifestFile {
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, nil, err
			}
			var m artifact.Manifest
			if err := json.Unmarshal(data, &m); err != nil {
				return nil, nil, err
			}
			return &m, data, nil
		}
	}
	return nil, nil, fmt.Errorf("manifest not found")
}

// ChecksumFile computes sha256 hex of file (for evidence compare).
func ChecksumFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifyManifestChecksum compares manifest.Checksum against actual file checksum on disk (if needed).
// For artifacts, the manifest checksum is content-derived via artifact.GenerateChecksum; we verify via VerifyArtifact.
func VerifyManifestChecksum(artifactPath string) error {
	m, _, err := ExtractManifest(artifactPath)
	if err != nil {
		return err
	}
	if m.Checksum == "" || m.ChecksumType == "" {
		return fmt.Errorf("manifest missing checksum")
	}
	if m.ArtifactID == "" {
		return fmt.Errorf("manifest missing artifact_id")
	}
	if m.ProjectID == "" {
		return fmt.Errorf("manifest missing project_id")
	}
	// delegate to full verification
	return VerifyChecksum(artifactPath)
}
