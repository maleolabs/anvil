package spklocaldeploye2e

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"maleolabs.com/anvil/internal/artifact"
)

// VerifyArtifact runs the 6-check verification-contract (TS-P3-07) via artifact.VerifyArtifact.
// Returns verification result and error. Caller checks result.Passed for gate.
func VerifyArtifact(artifactPath string, logger io.Writer) (*artifact.VerificationResult, error) {
	res, err := artifact.VerifyArtifact(artifactPath)
	if err != nil {
		if logger != nil {
			fmt.Fprintf(logger, "[verify] %s FAIL: %v\n", SanitizeLogLine(artifactPath), err)
		}
		return nil, err
	}
	if logger != nil {
		status := "PASS"
		if !res.Passed {
			status = "FAIL"
		}
		fmt.Fprintf(logger, "[verify] %s %s (%d checks)\n", SanitizeLogLine(artifactPath), status, len(res.Checks))
		for _, c := range res.Checks {
			mark := "✔"
			if !c.Passed {
				mark = "✘"
			}
			fmt.Fprintf(logger, "  %s %-22s passed=%-5t %s\n", mark, c.Name, c.Passed, SanitizeLogLine(c.Details))
		}
	}
	return res, nil
}

// VerifyBeforeTrust is the verification-before-trust gate (spec:verification-contract).
// It MUST be called before Activate. Returns error if verification fails, preventing Activate.
func VerifyBeforeTrust(artifactPath string, logger io.Writer) error {
	res, err := VerifyArtifact(artifactPath, logger)
	if err != nil {
		return fmt.Errorf("verification-before-trust: %w", err)
	}
	if !res.Passed {
		var failed string
		for _, c := range res.Checks {
			if !c.Passed {
				failed = fmt.Sprintf("%s: %s", c.Name, c.Details)
				break
			}
		}
		return fmt.Errorf("verification-before-trust: artifact FAILED verification (%s) — Activate rejected", failed)
	}
	return nil
}

// ValidateManifestSchema validates manifest.json fields against spec:artifact-manifest requirements:
// identity from content (artifact_id), version, checksum, checksum_type, project_id, created_at, source.
func ValidateManifestSchema(m *artifact.Manifest) error {
	if m == nil {
		return fmt.Errorf("manifest nil")
	}
	if m.ArtifactID == "" {
		return fmt.Errorf("manifest missing artifact_id (identity-from-content)")
	}
	if m.Version == "" {
		return fmt.Errorf("manifest missing version")
	}
	if m.Checksum == "" {
		return fmt.Errorf("manifest missing checksum")
	}
	if m.ChecksumType == "" {
		return fmt.Errorf("manifest missing checksum_type")
	}
	if m.ProjectID == "" {
		return fmt.Errorf("manifest missing project_id")
	}
	if m.CreatedAt == "" {
		return fmt.Errorf("manifest missing created_at")
	}
	if m.Source == "" {
		return fmt.Errorf("manifest missing source")
	}
	return nil
}

// ExtractManifest reads manifest.json from artifact tar.gz and returns manifest + raw bytes.
func ExtractManifestBytes(artifactPath string) (*artifact.Manifest, []byte, error) {
	m, err := artifact.ReadManifest(artifactPath)
	if err != nil {
		return nil, nil, err
	}
	// Also read raw manifest bytes via artifact's internal helper: re-read via ExtractManifest-like logic
	// Use artifact manifest marshaling for payload; raw content is json-marshaled manifest for Negotiate payload.
	raw, err := artifact.MarshalManifest(*m)
	if err != nil {
		return m, nil, err
	}
	// Verify raw round-trips
	_ = raw
	// Return manifest and raw JSON (manifestContent for deployment.ArtifactPayload)
	data, err := os.Open(artifactPath)
	if err != nil {
		return m, raw, nil
	}
	defer data.Close()
	// For payload we already have raw; just return it
	return m, raw, nil
}

// TamperArtifact creates a corrupted copy of the artifact at dstPath by appending garbage
// after the valid archive bytes, causing checksum/archive checks to fail. Used for AC3 negative test.
func TamperArtifact(srcPath, dstPath string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	// Corrupt by flipping a byte in the middle (if size > 100)
	if len(data) > 100 {
		mid := len(data) / 2
		data[mid] ^= 0xFF
	} else {
		data = append(data, []byte("CORRUPT")...)
	}
	if err := os.WriteFile(dstPath, data, 0644); err != nil {
		return err
	}
	return nil
}

// TruncateArtifact creates a truncated copy (partial write) for negative test.
func TruncateArtifact(srcPath, dstPath string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	if len(data) > 50 {
		data = data[:len(data)/2]
	}
	return os.WriteFile(dstPath, data, 0644)
}

// EnsureManifestContentForPayload reads manifest raw bytes suitable for deployment.ArtifactPayload.ManifestContent.
// It uses the embedded manifest bytes: try to extract via artifact.ReadManifest then marshal.
func ManifestContentForPayload(artifactPath string) ([]byte, error) {
	m, err := artifact.ReadManifest(artifactPath)
	if err != nil {
		return nil, err
	}
	return artifact.MarshalManifest(*m)
}

// WriteVerifyLog writes detailed verification output to w for evidence.
func WriteVerifyLog(w io.Writer, artifactPath string, result *artifact.VerificationResult) {
	if result == nil {
		fmt.Fprintf(w, "[verify] no result for %s\n", SanitizeLogLine(artifactPath))
		return
	}
	fmt.Fprintf(w, "=== Verify %s ===\n", SanitizeLogLine(artifactPath))
	fmt.Fprintf(w, "Passed: %v\n", result.Passed)
	for _, c := range result.Checks {
		fmt.Fprintf(w, "  %-22s passed=%-5t %s\n", c.Name, c.Passed, c.Details)
	}
	fmt.Fprintf(w, "\n")
}

// LogBuffer helpers
func bytesContains(b []byte, sub string) bool { return bytes.Contains(b, []byte(sub)) }
