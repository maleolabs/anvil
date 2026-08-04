// Tests for the Flutter adapter verification checks (TS-P7-25). Checks
// run against temp artifact-like directories and against real tar.gz
// archive fixtures, so the directory and archive access paths are both
// covered.
package flutter

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

// TestRunVerification_PubspecYaml verifies the pubspec_yaml check passes
// when pubspec.yaml exists in the artifact directory and fails with a
// descriptive detail when it is absent (TS-P7-25 AC-1, AC-3).
func TestRunVerification_PubspecYaml(t *testing.T) {
	t.Run("pass", func(t *testing.T) {
		outcome := RunVerification(contracts.VerificationRequest{
			Check:        CheckPubspecYaml,
			ArtifactPath: writeArtifactDir(t, "pubspec.yaml"),
		})
		if !outcome.Passed {
			t.Errorf("Passed = false, want true (outcome: %#v)", outcome)
		}
		if outcome.Name != CheckPubspecYaml {
			t.Errorf("Name = %q, want %q", outcome.Name, CheckPubspecYaml)
		}
		if outcome.Details == "" {
			t.Error("Details = empty, want a description of what was validated")
		}
	})

	t.Run("fail", func(t *testing.T) {
		outcome := RunVerification(contracts.VerificationRequest{
			Check:        CheckPubspecYaml,
			ArtifactPath: writeArtifactDir(t, "lib/main.dart"),
		})
		if outcome.Passed {
			t.Errorf("Passed = true, want false (outcome: %#v)", outcome)
		}
		if !strings.Contains(outcome.Details, "pubspec.yaml") {
			t.Errorf("Details = %q, want mention of the missing pubspec.yaml", outcome.Details)
		}
	})
}

// TestRunVerification_LibDirectory verifies the lib_directory check
// passes when the lib/ directory exists in the artifact directory and
// fails with a descriptive detail when it is absent (TS-P7-25 AC-2,
// AC-3).
func TestRunVerification_LibDirectory(t *testing.T) {
	t.Run("pass", func(t *testing.T) {
		outcome := RunVerification(contracts.VerificationRequest{
			Check:        CheckLibDirectory,
			ArtifactPath: writeArtifactStructure(t, []string{"lib/main.dart"}, []string{"lib"}),
		})
		if !outcome.Passed {
			t.Errorf("Passed = false, want true (outcome: %#v)", outcome)
		}
		if outcome.Name != CheckLibDirectory {
			t.Errorf("Name = %q, want %q", outcome.Name, CheckLibDirectory)
		}
		if outcome.Details == "" {
			t.Error("Details = empty, want a description of what was validated")
		}
	})

	t.Run("fail", func(t *testing.T) {
		outcome := RunVerification(contracts.VerificationRequest{
			Check:        CheckLibDirectory,
			ArtifactPath: writeArtifactDir(t, "pubspec.yaml"),
		})
		if outcome.Passed {
			t.Errorf("Passed = true, want false (outcome: %#v)", outcome)
		}
		if !strings.Contains(outcome.Details, "missing required directory: lib") {
			t.Errorf("Details = %q, want the descriptive missing message", outcome.Details)
		}
	})
}

// TestRunVerification_LibDirectory_FileNotDirectory verifies that a
// regular file named "lib" does not satisfy the lib_directory check —
// only a real directory counts (TS-P7-25 AC-2).
func TestRunVerification_LibDirectory_FileNotDirectory(t *testing.T) {
	t.Run("directory_artifact", func(t *testing.T) {
		outcome := RunVerification(contracts.VerificationRequest{
			Check:        CheckLibDirectory,
			ArtifactPath: writeArtifactDir(t, "lib"),
		})
		if outcome.Passed {
			t.Errorf("Passed = true, want false (outcome: %#v)", outcome)
		}
	})

	t.Run("archive", func(t *testing.T) {
		outcome := RunVerification(contracts.VerificationRequest{
			Check:        CheckLibDirectory,
			ArtifactPath: writeArtifactArchive(t, "lib"),
		})
		if outcome.Passed {
			t.Errorf("Passed = true, want false (outcome: %#v)", outcome)
		}
	})
}

// TestRunVerification_Archive verifies that both checks also pass against
// a real tar.gz artifact archive with the "app/" deployable-content
// prefix, and fail when the entries are absent — no full extraction is
// performed (TS-P7-25 AC-1..AC-3).
func TestRunVerification_Archive(t *testing.T) {
	t.Run("pass_existing", func(t *testing.T) {
		// The archive contains pubspec.yaml and explicit TypeDir
		// entries for lib/, as most tar writers emit them.
		artifactPath := writeArtifactArchiveWithDirs(t,
			[]string{"pubspec.yaml", "lib/main.dart"},
			[]string{"lib"},
		)
		for _, check := range []string{CheckPubspecYaml, CheckLibDirectory} {
			outcome := RunVerification(contracts.VerificationRequest{Check: check, ArtifactPath: artifactPath})
			if !outcome.Passed {
				t.Errorf("%s: Passed = false, want true (outcome: %#v)", check, outcome)
			}
		}
	})

	t.Run("pass_no_dir_entries", func(t *testing.T) {
		// Anvil packaging stores only regular files, never directory
		// entries: a directory still counts when entries live beneath
		// it.
		artifactPath := writeArtifactArchive(t, "pubspec.yaml", "lib/main.dart")
		for _, check := range []string{CheckPubspecYaml, CheckLibDirectory} {
			outcome := RunVerification(contracts.VerificationRequest{Check: check, ArtifactPath: artifactPath})
			if !outcome.Passed {
				t.Errorf("%s: Passed = false, want true (outcome: %#v)", check, outcome)
			}
		}
	})

	t.Run("fail", func(t *testing.T) {
		// The archive has no pubspec.yaml and no lib/ entries.
		artifactPath := writeArtifactArchive(t, "README.md")
		tests := []struct {
			check         string
			missingDetail string
		}{
			{check: CheckPubspecYaml, missingDetail: "pubspec.yaml"},
			{check: CheckLibDirectory, missingDetail: "missing required directory: lib"},
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
// yields a failing outcome with descriptive details, not a panic.
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
		Check:        CheckPubspecYaml,
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
// lists exactly the two Flutter verification checks — pubspec_yaml and
// lib_directory — with descriptions (TS-P7-25 DoD, TS-P7-08 AC-3).
func TestCapabilities_DeclaresChecks(t *testing.T) {
	result := Capabilities()
	checks := result.Declaration.VerificationChecks
	if len(checks) != 2 {
		t.Fatalf("VerificationChecks length = %d, want 2", len(checks))
	}
	want := []string{CheckPubspecYaml, CheckLibDirectory}
	for i, name := range want {
		if checks[i].Name != name {
			t.Errorf("VerificationChecks[%d].Name = %q, want %q", i, checks[i].Name, name)
		}
		if checks[i].Description == "" {
			t.Errorf("VerificationChecks[%d].Description = empty, want a description", i)
		}
	}
}
