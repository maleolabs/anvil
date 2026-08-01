// Tests for the Laravel adapter verification checks (TS-P7-11). Checks
// run against temp artifact-like directories and against real tar.gz
// archive fixtures, so the directory and archive access paths are both
// covered.
package laravel

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/contracts"
)

// writeArtifactDir creates a temp directory artifact containing the given
// relative files (empty contents) and returns its path.
func writeArtifactDir(t *testing.T, files ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, rel := range files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte("fixture"), 0644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return dir
}

// writeArtifactArchive creates a tar.gz archive containing the given
// relative files under the "app/" deployable-content prefix (the Anvil
// artifact convention) and returns its path.
func writeArtifactArchive(t *testing.T, files ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "artifact.tar.gz")

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer f.Close()

	gzr := gzip.NewWriter(f)
	tr := tar.NewWriter(gzr)
	for _, rel := range files {
		content := []byte("fixture")
		if err := tr.WriteHeader(&tar.Header{
			Name: filepath.ToSlash(filepath.Join("app", rel)),
			Mode: 0644,
			Size: int64(len(content)),
		}); err != nil {
			t.Fatalf("write header for %s: %v", rel, err)
		}
		if _, err := tr.Write(content); err != nil {
			t.Fatalf("write content for %s: %v", rel, err)
		}
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gzr.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return path
}

// TestRunVerification_Pass verifies that each check passes when the
// required files exist in the artifact directory (TS-P7-11 AC-1..AC-4).
func TestRunVerification_Pass(t *testing.T) {
	tests := []struct {
		check string
		files []string
	}{
		{check: CheckVendorPresent, files: []string{"vendor/autoload.php"}},
		{check: CheckBootstrapStructure, files: []string{"bootstrap/app.php"}},
		{check: CheckConfigFiles, files: []string{"config/app.php", ".env.example"}},
	}
	for _, tt := range tests {
		t.Run(tt.check, func(t *testing.T) {
			artifactPath := writeArtifactDir(t, tt.files...)
			outcome := RunVerification(contracts.VerificationRequest{Check: tt.check, ArtifactPath: artifactPath})

			if !outcome.Passed {
				t.Errorf("Passed = false, want true (outcome: %#v)", outcome)
			}
			if outcome.Name != tt.check {
				t.Errorf("Name = %q, want %q", outcome.Name, tt.check)
			}
			if outcome.Details == "" {
				t.Error("Details = empty, want a description of what was validated")
			}
		})
	}
}

// TestRunVerification_Fail verifies that each check fails with details
// naming the missing file when a required file is absent (TS-P7-11 AC-4).
func TestRunVerification_Fail(t *testing.T) {
	tests := []struct {
		check       string
		present     []string
		missingFile string
	}{
		{check: CheckVendorPresent, present: nil, missingFile: "vendor/autoload.php"},
		{check: CheckBootstrapStructure, present: []string{"vendor/autoload.php"}, missingFile: "bootstrap/app.php"},
		{check: CheckConfigFiles, present: []string{"config/app.php"}, missingFile: ".env.example"},
	}
	for _, tt := range tests {
		t.Run(tt.check, func(t *testing.T) {
			artifactPath := writeArtifactDir(t, tt.present...)
			outcome := RunVerification(contracts.VerificationRequest{Check: tt.check, ArtifactPath: artifactPath})

			if outcome.Passed {
				t.Errorf("Passed = true, want false (outcome: %#v)", outcome)
			}
			if !strings.Contains(outcome.Details, tt.missingFile) {
				t.Errorf("Details = %q, want mention of missing file %q", outcome.Details, tt.missingFile)
			}
		})
	}
}

// TestRunVerification_Archive verifies that checks also pass against a
// real tar.gz artifact archive with the "app/" deployable-content prefix,
// and fail when the entry is absent — no full extraction is performed.
func TestRunVerification_Archive(t *testing.T) {
	t.Run("pass", func(t *testing.T) {
		artifactPath := writeArtifactArchive(t, "vendor/autoload.php", "bootstrap/app.php", "config/app.php", ".env.example")
		for _, check := range []string{CheckVendorPresent, CheckBootstrapStructure, CheckConfigFiles} {
			outcome := RunVerification(contracts.VerificationRequest{Check: check, ArtifactPath: artifactPath})
			if !outcome.Passed {
				t.Errorf("%s: Passed = false, want true (outcome: %#v)", check, outcome)
			}
		}
	})

	t.Run("fail", func(t *testing.T) {
		artifactPath := writeArtifactArchive(t, "bootstrap/app.php")
		outcome := RunVerification(contracts.VerificationRequest{Check: CheckVendorPresent, ArtifactPath: artifactPath})
		if outcome.Passed {
			t.Error("Passed = true, want false")
		}
		if !strings.Contains(outcome.Details, "vendor/autoload.php") {
			t.Errorf("Details = %q, want mention of vendor/autoload.php", outcome.Details)
		}
	})
}

// TestRunVerification_UnknownCheck verifies that an undeclared check
// yields a failing outcome with details, not a panic.
func TestRunVerification_UnknownCheck(t *testing.T) {
	outcome := RunVerification(contracts.VerificationRequest{Check: "unknown_check", ArtifactPath: t.TempDir()})
	if outcome.Passed {
		t.Error("Passed = true, want false")
	}
	if !strings.Contains(outcome.Details, `unknown verification check "unknown_check"`) {
		t.Errorf("Details = %q, want mention of the unknown check", outcome.Details)
	}
}

// TestRunVerification_MissingArtifact verifies that an unreadable artifact
// path yields a failing outcome with an actionable detail.
func TestRunVerification_MissingArtifact(t *testing.T) {
	outcome := RunVerification(contracts.VerificationRequest{
		Check:        CheckVendorPresent,
		ArtifactPath: filepath.Join(t.TempDir(), "does-not-exist"),
	})
	if outcome.Passed {
		t.Error("Passed = true, want false")
	}
	if !strings.Contains(outcome.Details, "cannot inspect artifact") {
		t.Errorf("Details = %q, want mention of the inspection failure", outcome.Details)
	}
}

// TestCapabilities_DeclaresChecks verifies that the capability declaration
// lists exactly the three verification checks (TS-P7-11 DoD).
func TestCapabilities_DeclaresChecks(t *testing.T) {
	result := Capabilities()
	checks := result.Declaration.VerificationChecks
	if len(checks) != 3 {
		t.Fatalf("VerificationChecks length = %d, want 3", len(checks))
	}
	want := []string{CheckVendorPresent, CheckBootstrapStructure, CheckConfigFiles}
	for i, name := range want {
		if checks[i].Name != name {
			t.Errorf("VerificationChecks[%d].Name = %q, want %q", i, checks[i].Name, name)
		}
		if checks[i].Description == "" {
			t.Errorf("VerificationChecks[%d].Description = empty, want a description", i)
		}
	}
}
