// Verification checks of the Flutter adapter (TS-P7-25).
//
// Each check validates a Flutter-specific file or structure inside the
// artifact under verification and returns a contracts.VerificationOutcome
// (pass/fail + details), aligned with artifact.CheckResult so outcomes
// merge into the Core's verification report without transformation.
//
// The artifact path may be either a directory (the extracted artifact,
// the common case in tests) or an Anvil artifact archive (tar.gz). For
// archives, entries are scanned directly — no full extraction is
// performed (docs/sessions/impl-TS-P7-09-TS-P7-10-TS-P7-11-TS-P7-12-
// 20260801/CONTEXT.md §Known Risks).
package flutter

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
// Reference: TS-P7-25 AC-1..AC-3
const (
	// CheckPubspecYaml validates that pubspec.yaml exists in the
	// project root — the Flutter project manifest.
	CheckPubspecYaml = "pubspec_yaml"

	// CheckLibDirectory validates that the lib/ directory exists in
	// the project root — the Flutter application source directory.
	CheckLibDirectory = "lib_directory"
)

// RunVerification executes one verification check against the artifact
// path and returns the pass/fail outcome.
//
// Reference: TS-P7-25 AC-1..AC-3
func RunVerification(req contracts.VerificationRequest) contracts.VerificationOutcome {
	switch req.Check {
	case CheckPubspecYaml:
		return checkFiles(req.ArtifactPath, CheckPubspecYaml, "pubspec.yaml")
	case CheckLibDirectory:
		return checkDirectory(req.ArtifactPath, CheckLibDirectory, "lib")
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

// checkDirectory verifies that a required directory exists inside the
// artifact (directory or archive) and reports a single outcome for the
// check.
func checkDirectory(artifactPath, checkName, requiredDir string) contracts.VerificationOutcome {
	found, err := artifactContainsDir(artifactPath, requiredDir)
	if err != nil {
		return contracts.VerificationOutcome{
			Name:    checkName,
			Passed:  false,
			Details: fmt.Sprintf("cannot inspect artifact %q: %v", artifactPath, err),
		}
	}
	if !found {
		return contracts.VerificationOutcome{
			Name:    checkName,
			Passed:  false,
			Details: fmt.Sprintf("missing required directory: %s", requiredDir),
		}
	}
	return contracts.VerificationOutcome{
		Name:    checkName,
		Passed:  true,
		Details: fmt.Sprintf("required directory found: %s", requiredDir),
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

// artifactContainsDir reports whether the directory relPath exists inside
// the artifact at artifactPath (directory or tar.gz archive). Unlike
// artifactContains, a matching entry must be a directory, not a file.
func artifactContainsDir(artifactPath, relPath string) (bool, error) {
	info, err := os.Stat(artifactPath)
	if err != nil {
		return false, err
	}
	if info.IsDir() {
		dirInfo, err := os.Stat(filepath.Join(artifactPath, relPath))
		if err != nil {
			if os.IsNotExist(err) {
				return false, nil
			}
			return false, err
		}
		return dirInfo.IsDir(), nil
	}
	return archiveContainsDir(artifactPath, relPath)
}

// scanArchive opens the tar.gz archive and calls match for each entry —
// with the optional "app/" deployable-content prefix stripped from the
// entry name — until match returns true. The returned bool reports
// whether any entry matched.
func scanArchive(archivePath string, match func(name string, hdr *tar.Header) bool) (bool, error) {
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
		if match(strings.TrimPrefix(hdr.Name, "app/"), hdr) {
			return true, nil
		}
	}
	return false, nil
}

// archiveContains scans the entries of a tar.gz archive for relPath,
// accepting an optional "app/" prefix on entry names. Only regular file
// entries (tar.TypeReg) count.
func archiveContains(archivePath, relPath string) (bool, error) {
	return scanArchive(archivePath, func(name string, hdr *tar.Header) bool {
		return name == relPath && hdr.Typeflag == tar.TypeReg
	})
}

// archiveContainsDir scans the entries of a tar.gz archive for evidence
// of the directory relPath. Two forms count as evidence:
//
//  1. An explicit directory entry: tar.TypeDir entries, or regular file
//     entries whose name ends with "/" (some tar writers store
//     directories that way).
//  2. A regular entry living beneath the directory — Anvil artifact
//     archives never contain directory entries (packaging only stores
//     regular files), so a directory is present whenever an entry is
//     stored under its path.
//
// The optional "app/" deployable-content prefix is stripped before
// matching, so prefixed and unprefixed archives behave consistently.
func archiveContainsDir(archivePath, relPath string) (bool, error) {
	return scanArchive(archivePath, func(name string, hdr *tar.Header) bool {
		isDirMarker := hdr.Typeflag == tar.TypeDir ||
			(hdr.Typeflag == tar.TypeReg && strings.HasSuffix(name, "/"))
		if isDirMarker && strings.TrimSuffix(name, "/") == relPath {
			return true
		}
		return strings.HasPrefix(name, relPath+"/")
	})
}
