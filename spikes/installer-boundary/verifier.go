package spkinstallerboundary

import (
	"fmt"
	"io"

	"maleolabs.com/anvil/internal/artifact"
)

// ValidateManifestSchema validates manifest.json fields against spec:artifact-manifest.
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

// VerifyArtifact runs 6-check verification and logs.
func VerifyArtifact(artifactPath string, logger io.Writer) (*artifact.VerificationResult, error) {
	res, err := artifact.VerifyArtifact(artifactPath)
	if err != nil {
		if logger != nil {
			fmt.Fprintf(logger, "[verify] %s FAIL: %v\n", artifactPath, err)
		}
		return nil, err
	}
	if logger != nil {
		status := "PASS"
		if !res.Passed {
			status = "FAIL"
		}
		fmt.Fprintf(logger, "[verify] %s %s (%d checks)\n", artifactPath, status, len(res.Checks))
		for _, c := range res.Checks {
			mark := "✔"
			if !c.Passed {
				mark = "✘"
			}
			fmt.Fprintf(logger, "  %s %-22s passed=%-5t %s\n", mark, c.Name, c.Passed, c.Details)
		}
	}
	return res, nil
}

// WriteVerifyLog writes detailed verification to w.
func WriteVerifyLog(w io.Writer, artifactPath string, result *artifact.VerificationResult) {
	if w == nil {
		return
	}
	if result == nil {
		fmt.Fprintf(w, "[verify] no result for %s\n", artifactPath)
		return
	}
	fmt.Fprintf(w, "=== Verify %s ===\n", artifactPath)
	fmt.Fprintf(w, "Passed: %v\n", result.Passed)
	for _, c := range result.Checks {
		fmt.Fprintf(w, "  %-22s passed=%-5t %s\n", c.Name, c.Passed, c.Details)
	}
	fmt.Fprintln(w)
}
