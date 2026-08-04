// Tests for the Laravel adapter verification checks (TS-P7-11, TS-P7-17).
// Checks run against temp artifact-like directories and against real
// tar.gz archive fixtures, so the directory and archive access paths are
// both covered.
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

// writeArtifactStructure creates a temp directory artifact containing the
// given relative files (empty contents) and directories, and returns its
// path. Directories are created first, so files nested inside them are
// placed into the real directories.
func writeArtifactStructure(t *testing.T, files, dirs []string) string {
	t.Helper()
	dir := t.TempDir()
	for _, rel := range dirs {
		if err := os.MkdirAll(filepath.Join(dir, rel), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
	}
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

// writeArtifactArchiveWithDirs creates a tar.gz archive containing the
// given relative files and directories under the "app/" deployable-content
// prefix and returns its path. Directories are written as tar.TypeDir
// entries with a trailing slash, as most tar writers emit them.
func writeArtifactArchiveWithDirs(t *testing.T, files, dirs []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "artifact.tar.gz")

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer f.Close()

	gzr := gzip.NewWriter(f)
	tr := tar.NewWriter(gzr)
	for _, rel := range dirs {
		if err := tr.WriteHeader(&tar.Header{
			Name:     filepath.ToSlash(filepath.Join("app", rel)) + "/",
			Mode:     0755,
			Typeflag: tar.TypeDir,
		}); err != nil {
			t.Fatalf("write dir header for %s: %v", rel, err)
		}
	}
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
// required files exist in the artifact directory (TS-P7-11 AC-1..AC-4,
// TS-P7-17 AC-1..AC-3).
func TestRunVerification_Pass(t *testing.T) {
	tests := []struct {
		check string
		files []string
	}{
		{check: CheckVendorPresent, files: []string{"vendor/autoload.php"}},
		{check: CheckBootstrapStructure, files: []string{"bootstrap/app.php"}},
		{check: CheckConfigFiles, files: []string{"config/app.php", ".env.example"}},
		{check: CheckArtisanFile, files: []string{"artisan"}},
		{check: CheckComposerJSON, files: []string{"composer.json"}},
		{check: CheckEnvFile, files: []string{".env"}},
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

// TestRunVerification_DirectoryPass verifies that the directory checks
// pass when the required directories exist in the artifact directory
// (TS-P7-17 AC-1..AC-3).
func TestRunVerification_DirectoryPass(t *testing.T) {
	artifactPath := writeArtifactStructure(t,
		[]string{"app/Http/Controllers/Controller.php", "routes/web.php"},
		[]string{"app", "routes"},
	)

	for _, check := range []string{CheckAppDirectory, CheckRoutesDirectory} {
		outcome := RunVerification(contracts.VerificationRequest{Check: check, ArtifactPath: artifactPath})
		if !outcome.Passed {
			t.Errorf("%s: Passed = false, want true (outcome: %#v)", check, outcome)
		}
		if outcome.Name != check {
			t.Errorf("Name = %q, want %q", outcome.Name, check)
		}
		if outcome.Details == "" {
			t.Errorf("%s: Details = empty, want a description of what was validated", check)
		}
	}
}

// TestRunVerification_Fail verifies that each check fails with details
// naming the missing file when a required file is absent (TS-P7-11 AC-4,
// TS-P7-17 AC-2, AC-3).
func TestRunVerification_Fail(t *testing.T) {
	tests := []struct {
		check       string
		present     []string
		missingFile string
	}{
		{check: CheckVendorPresent, present: nil, missingFile: "vendor/autoload.php"},
		{check: CheckBootstrapStructure, present: []string{"vendor/autoload.php"}, missingFile: "bootstrap/app.php"},
		{check: CheckConfigFiles, present: []string{"config/app.php"}, missingFile: ".env.example"},
		{check: CheckArtisanFile, present: nil, missingFile: "artisan"},
		{check: CheckComposerJSON, present: []string{"artisan"}, missingFile: "composer.json"},
		{check: CheckEnvFile, present: []string{"artisan"}, missingFile: ".env.example"},
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

// TestRunVerification_DirectoryFail verifies that the directory checks
// fail with a descriptive message when the required directory is absent
// from the artifact directory (TS-P7-17 AC-2, AC-3).
func TestRunVerification_DirectoryFail(t *testing.T) {
	tests := []struct {
		check         string
		present       []string
		missingDetail string
	}{
		{check: CheckAppDirectory, present: []string{"routes/web.php"}, missingDetail: "missing required directory: app"},
		{check: CheckRoutesDirectory, present: []string{"app/Http/Controllers/Controller.php"}, missingDetail: "missing required directory: routes"},
	}
	for _, tt := range tests {
		t.Run(tt.check, func(t *testing.T) {
			artifactPath := writeArtifactDir(t, tt.present...)
			outcome := RunVerification(contracts.VerificationRequest{Check: tt.check, ArtifactPath: artifactPath})

			if outcome.Passed {
				t.Errorf("Passed = true, want false (outcome: %#v)", outcome)
			}
			if !strings.Contains(outcome.Details, tt.missingDetail) {
				t.Errorf("Details = %q, want it to contain %q", outcome.Details, tt.missingDetail)
			}
		})
	}
}

// TestRunVerification_EnvFile verifies the env_file alternatives: the
// check passes when either .env or .env.example is present and fails
// with a descriptive message when neither is (TS-P7-17 AC-1..AC-3).
func TestRunVerification_EnvFile(t *testing.T) {
	t.Run("env_only", func(t *testing.T) {
		outcome := RunVerification(contracts.VerificationRequest{
			Check:        CheckEnvFile,
			ArtifactPath: writeArtifactDir(t, ".env"),
		})
		if !outcome.Passed {
			t.Errorf("Passed = false, want true (outcome: %#v)", outcome)
		}
	})

	t.Run("env_example_only", func(t *testing.T) {
		outcome := RunVerification(contracts.VerificationRequest{
			Check:        CheckEnvFile,
			ArtifactPath: writeArtifactDir(t, ".env.example"),
		})
		if !outcome.Passed {
			t.Errorf("Passed = false, want true (outcome: %#v)", outcome)
		}
	})

	t.Run("neither", func(t *testing.T) {
		outcome := RunVerification(contracts.VerificationRequest{
			Check:        CheckEnvFile,
			ArtifactPath: writeArtifactDir(t, "artisan", "composer.json"),
		})
		if outcome.Passed {
			t.Errorf("Passed = true, want false (outcome: %#v)", outcome)
		}
		if !strings.Contains(outcome.Details, "neither .env nor .env.example found") {
			t.Errorf("Details = %q, want the descriptive missing message", outcome.Details)
		}
	})
}

// TestRunVerification_AppDirectory_FileNotDirectory verifies that a
// regular file named "app" does not satisfy the app_directory check —
// only a real directory counts (TS-P7-17 AC-2).
func TestRunVerification_AppDirectory_FileNotDirectory(t *testing.T) {
	t.Run("directory_artifact", func(t *testing.T) {
		outcome := RunVerification(contracts.VerificationRequest{
			Check:        CheckAppDirectory,
			ArtifactPath: writeArtifactDir(t, "app"),
		})
		if outcome.Passed {
			t.Errorf("Passed = true, want false (outcome: %#v)", outcome)
		}
	})

	t.Run("archive", func(t *testing.T) {
		outcome := RunVerification(contracts.VerificationRequest{
			Check:        CheckAppDirectory,
			ArtifactPath: writeArtifactArchive(t, "app"),
		})
		if outcome.Passed {
			t.Errorf("Passed = true, want false (outcome: %#v)", outcome)
		}
	})
}

// TestRunVerification_Archive verifies that checks also pass against a
// real tar.gz artifact archive with the "app/" deployable-content prefix,
// and fail when the entry is absent — no full extraction is performed.
func TestRunVerification_Archive(t *testing.T) {
	t.Run("pass_existing", func(t *testing.T) {
		artifactPath := writeArtifactArchive(t, "vendor/autoload.php", "bootstrap/app.php", "config/app.php", ".env.example")
		for _, check := range []string{CheckVendorPresent, CheckBootstrapStructure, CheckConfigFiles} {
			outcome := RunVerification(contracts.VerificationRequest{Check: check, ArtifactPath: artifactPath})
			if !outcome.Passed {
				t.Errorf("%s: Passed = false, want true (outcome: %#v)", check, outcome)
			}
		}
	})

	t.Run("pass_new_checks", func(t *testing.T) {
		// The archive contains the required files and explicit TypeDir
		// entries for app/ and routes/, as most tar writers emit them.
		artifactPath := writeArtifactArchiveWithDirs(t,
			[]string{
				"vendor/autoload.php",
				"bootstrap/app.php",
				"config/app.php",
				".env.example",
				"artisan",
				"composer.json",
				"routes/web.php",
				"app/Http/Controllers/Controller.php",
			},
			[]string{"app", "routes"},
		)
		for _, check := range []string{
			CheckVendorPresent,
			CheckBootstrapStructure,
			CheckConfigFiles,
			CheckArtisanFile,
			CheckComposerJSON,
			CheckEnvFile,
			CheckAppDirectory,
			CheckRoutesDirectory,
		} {
			outcome := RunVerification(contracts.VerificationRequest{Check: check, ArtifactPath: artifactPath})
			if !outcome.Passed {
				t.Errorf("%s: Passed = false, want true (outcome: %#v)", check, outcome)
			}
		}
	})

	t.Run("pass_new_checks_no_dir_entries", func(t *testing.T) {
		// Anvil packaging stores only regular files, never directory
		// entries: a directory still counts when entries live beneath it.
		artifactPath := writeArtifactArchive(t,
			"artisan", "composer.json", ".env.example",
			"routes/web.php", "app/Http/Controllers/Controller.php",
		)
		for _, check := range []string{CheckAppDirectory, CheckRoutesDirectory} {
			outcome := RunVerification(contracts.VerificationRequest{Check: check, ArtifactPath: artifactPath})
			if !outcome.Passed {
				t.Errorf("%s: Passed = false, want true (outcome: %#v)", check, outcome)
			}
		}
	})

	t.Run("env_alternatives", func(t *testing.T) {
		for _, files := range [][]string{{".env"}, {".env.example"}} {
			artifactPath := writeArtifactArchive(t, files...)
			outcome := RunVerification(contracts.VerificationRequest{Check: CheckEnvFile, ArtifactPath: artifactPath})
			if !outcome.Passed {
				t.Errorf("files %v: Passed = false, want true (outcome: %#v)", files, outcome)
			}
		}
	})

	t.Run("fail_existing", func(t *testing.T) {
		artifactPath := writeArtifactArchive(t, "bootstrap/app.php")
		outcome := RunVerification(contracts.VerificationRequest{Check: CheckVendorPresent, ArtifactPath: artifactPath})
		if outcome.Passed {
			t.Error("Passed = true, want false")
		}
		if !strings.Contains(outcome.Details, "vendor/autoload.php") {
			t.Errorf("Details = %q, want mention of vendor/autoload.php", outcome.Details)
		}
	})

	t.Run("fail_new_checks", func(t *testing.T) {
		// The archive has none of the entries the new checks require:
		// no artisan/composer.json/env files, and neither directory.
		artifactPath := writeArtifactArchive(t, "bootstrap/app.php", "config/app.php")
		tests := []struct {
			check         string
			missingDetail string
		}{
			{check: CheckArtisanFile, missingDetail: "artisan"},
			{check: CheckComposerJSON, missingDetail: "composer.json"},
			{check: CheckEnvFile, missingDetail: "neither .env nor .env.example found"},
			{check: CheckAppDirectory, missingDetail: "missing required directory: app"},
			{check: CheckRoutesDirectory, missingDetail: "missing required directory: routes"},
		}
		for _, tt := range tests {
			outcome := RunVerification(contracts.VerificationRequest{Check: tt.check, ArtifactPath: artifactPath})
			if outcome.Passed {
				t.Errorf("%s: Passed = true, want false (outcome: %#v)", tt.check, outcome)
			}
			if !strings.Contains(outcome.Details, tt.missingDetail) {
				t.Errorf("%s: Details = %q, want it to contain %q", tt.check, outcome.Details, tt.missingDetail)
			}
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
// lists exactly the eight verification checks — the three original
// (TS-P7-11) plus the five additive ones (TS-P7-17) — in declaration
// order (TS-P7-11 DoD, TS-P7-17 DoD).
func TestCapabilities_DeclaresChecks(t *testing.T) {
	result := Capabilities()
	checks := result.Declaration.VerificationChecks
	if len(checks) != 8 {
		t.Fatalf("VerificationChecks length = %d, want 8", len(checks))
	}
	want := []string{
		CheckVendorPresent,
		CheckBootstrapStructure,
		CheckConfigFiles,
		CheckArtisanFile,
		CheckComposerJSON,
		CheckEnvFile,
		CheckAppDirectory,
		CheckRoutesDirectory,
	}
	for i, name := range want {
		if checks[i].Name != name {
			t.Errorf("VerificationChecks[%d].Name = %q, want %q", i, checks[i].Name, name)
		}
		if checks[i].Description == "" {
			t.Errorf("VerificationChecks[%d].Description = empty, want a description", i)
		}
	}
}
