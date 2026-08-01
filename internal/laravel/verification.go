// Verification checks of the Laravel adapter (TS-P7-11).
//
// Each check validates a Laravel-specific file or structure inside the
// artifact under verification and returns a contracts.VerificationOutcome
// (pass/fail + details), aligned with artifact.CheckResult so outcomes
// merge into the Core's verification report without transformation.
//
// The artifact path may be either a directory (the extracted artifact,
// the common case in tests) or an Anvil artifact archive (tar.gz). For
// archives, entries are scanned directly — no full extraction is
// performed (docs/sessions/impl-TS-P7-09-TS-P7-10-TS-P7-11-TS-P7-12-
// 20260801/CONTEXT.md §Known Risks).
package laravel

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"maleolabs.com/anvil/internal/contracts"
)

// Verification check names declared in the capability declaration
// (Capabilities). Check names are part of the adapter's contract surface:
// the Core invokes only declared checks (TS-P7-08 AC-3).
//
// Reference: TS-P7-11
const (
	// CheckVendorPresent validates that vendor/autoload.php exists —
	// Composer dependencies are installed and autoloadable.
	CheckVendorPresent = "vendor_present"

	// CheckBootstrapStructure validates that bootstrap/app.php exists —
	// the application bootstrap is present and structured.
	CheckBootstrapStructure = "bootstrap_structure"

	// CheckConfigFiles validates that required configuration files exist:
	// config/app.php and .env.example.
	CheckConfigFiles = "config_files"
)

// RunVerification executes one verification check against the artifact
// path and returns the pass/fail outcome.
//
// Reference: TS-P7-11 AC-1..AC-5
func RunVerification(req contracts.VerificationRequest) contracts.VerificationOutcome {
	switch req.Check {
	case CheckVendorPresent:
		return checkFiles(req.ArtifactPath, CheckVendorPresent,
			"vendor/autoload.php")
	case CheckBootstrapStructure:
		return checkFiles(req.ArtifactPath, CheckBootstrapStructure,
			"bootstrap/app.php")
	case CheckConfigFiles:
		return checkFiles(req.ArtifactPath, CheckConfigFiles,
			"config/app.php", ".env.example")
	default:
		return contracts.VerificationOutcome{
			Name:    req.Check,
			Passed:  false,
			Details: fmt.Sprintf("unknown verification check %q", req.Check),
		}
	}
}

// checkFiles verifies that every required relative path exists in the
// artifact (directory or archive) and reports a single outcome for the
// check.
func checkFiles(artifactPath, checkName string, required ...string) contracts.VerificationOutcome {
	var missing []string
	for _, rel := range required {
		found, err := artifactContains(artifactPath, rel)
		if err != nil {
			return contracts.VerificationOutcome{
				Name:    checkName,
				Passed:  false,
				Details: fmt.Sprintf("cannot inspect artifact %q: %v", artifactPath, err),
			}
		}
		if !found {
			missing = append(missing, rel)
		}
	}

	if len(missing) > 0 {
		return contracts.VerificationOutcome{
			Name:    checkName,
			Passed:  false,
			Details: fmt.Sprintf("missing required file(s): %s", strings.Join(missing, ", ")),
		}
	}
	return contracts.VerificationOutcome{
		Name:    checkName,
		Passed:  true,
		Details: fmt.Sprintf("required file(s) found: %s", strings.Join(required, ", ")),
	}
}

// artifactContains reports whether relPath exists inside the artifact at
// artifactPath. When artifactPath is a directory, the path is resolved
// directly; otherwise the path is treated as a tar.gz archive and its
// entries are scanned. Anvil artifact archives store deployable content
// under the "app/" prefix (artifact.DeployableContentDir); both prefixed
// and unprefixed entries are accepted so plain directories and archives
// behave consistently.
func artifactContains(artifactPath, relPath string) (bool, error) {
	info, err := os.Stat(artifactPath)
	if err != nil {
		return false, err
	}
	if info.IsDir() {
		if _, err := os.Stat(filepath.Join(artifactPath, relPath)); err != nil {
			if os.IsNotExist(err) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}
	return archiveContains(artifactPath, relPath)
}

// archiveContains scans the entries of a tar.gz archive for relPath,
// accepting an optional "app/" prefix on entry names.
func archiveContains(archivePath, relPath string) (bool, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return false, err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return false, fmt.Errorf("not a gzip archive: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return false, err
		}
		name := strings.TrimPrefix(hdr.Name, "app/")
		if name == relPath && hdr.Typeflag == tar.TypeReg {
			return true, nil
		}
	}
	return false, nil
}
