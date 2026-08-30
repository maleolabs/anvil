// Package cmd implements the Anvil CLI commands.
//
// Tests for the registry-side adapter install helpers (TS-016-04-01):
// the release-channel base derivation from the registry metadata's
// distribution location (standardReleaseDownloadBase), and the version
// resolution of an adapter install (adapterStandardVersionForInstall).
//
// Reference: TS-016-04-01, ADR-030
package cmd

import (
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/registry"
)

// ── Release Channel Derivation (TS-016-04-01) ────────────────────────

// TestStandardReleaseDownloadBase derives the release download base from
// a github-releases distribution location: the directory of the archive
// URL, where the adapter binary assets and SHA256SUMS.txt live alongside
// the release content.
//
// Reference: TS-016-04-01, ADR-030
func TestStandardReleaseDownloadBase(t *testing.T) {
	base, err := standardReleaseDownloadBase("https://github.com/maleolabs/anvil-standard-laravel/releases/download/v1.0.0/anvil-standard-laravel-1.0.0.tar.gz")
	if err != nil {
		t.Fatalf("standardReleaseDownloadBase returned an error: %v", err)
	}
	want := "https://github.com/maleolabs/anvil-standard-laravel/releases/download/v1.0.0/"
	if base != want {
		t.Errorf("release base = %q, want %q", base, want)
	}
}

// TestStandardReleaseDownloadBase_RejectsNonHTTPS verifies the https-only
// rule (ADR-030 §3): a plaintext distribution location is rejected — the
// adapter binary is never fetched over an unencrypted channel.
//
// Reference: TS-016-04-01, ADR-030 §3
func TestStandardReleaseDownloadBase_RejectsNonHTTPS(t *testing.T) {
	for _, location := range []string{
		"http://github.com/maleolabs/anvil-standard-laravel/releases/download/v1.0.0/anvil-standard-laravel-1.0.0.tar.gz",
		"ftp://github.com/anvil-standard-laravel-1.0.0.tar.gz",
		"not a url",
	} {
		if _, err := standardReleaseDownloadBase(location); err == nil {
			t.Errorf("standardReleaseDownloadBase(%q) should fail (https-only)", location)
		} else if !strings.Contains(err.Error(), "https") {
			t.Errorf("standardReleaseDownloadBase(%q) error should mention https, got: %v", location, err)
		}
	}
}

// TestStandardReleaseDownloadBase_RejectsUserinfo verifies the
// credentials-never-sent rule (ADR-030 §3): a distribution location
// carrying userinfo is rejected, and the error does not echo the
// credentials.
//
// Reference: TS-016-04-01, ADR-030 §3
func TestStandardReleaseDownloadBase_RejectsUserinfo(t *testing.T) {
	_, err := standardReleaseDownloadBase("https://alice:secret@github.com/maleolabs/anvil-standard-laravel/releases/download/v1.0.0/anvil-standard-laravel-1.0.0.tar.gz")
	if err == nil {
		t.Fatal("expected userinfo rejection, got nil")
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "alice") {
		t.Errorf("error must not echo credentials, got: %v", err)
	}
}

// TestStandardReleaseDownloadBase_RejectsNoDirectory verifies that a
// distribution location whose path has no release directory cannot
// locate the sibling assets.
//
// Reference: TS-016-04-01
func TestStandardReleaseDownloadBase_RejectsNoDirectory(t *testing.T) {
	if _, err := standardReleaseDownloadBase("https://example.com/release.tar.gz"); err == nil {
		t.Error("expected an error for a root-level distribution location, got nil")
	}
}

// ── Version Resolution (TS-016-04-01) ────────────────────────────────

// TestAdapterStandardVersionForInstall_HighestAdoptable resolves the
// highest ADOPTABLE version: published releases qualify, retired releases
// do not, and structurally invalid documents are not offered.
//
// Reference: TS-016-04-01, ADR-027 §3
func TestAdapterStandardVersionForInstall_HighestAdoptable(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // no pre-recorded standard
	indexDir := adapterListTestIndex(t,
		adapterListTestRelease(t, "anvil-standard-laravel", "1.0.0", registry.LifecycleStatePublished),
		adapterListTestRelease(t, "anvil-standard-laravel", "1.1.0", registry.LifecycleStateDeprecated),
		adapterListTestRelease(t, "anvil-standard-laravel", "2.0.0", registry.LifecycleStateRetired),
	)

	ix, err := registry.LoadIndex(indexDir)
	if err != nil {
		t.Fatalf("load index: %v", err)
	}
	version, err := adapterStandardVersionForInstall(ix, "anvil-standard-laravel")
	if err != nil {
		t.Fatalf("adapterStandardVersionForInstall returned an error: %v", err)
	}
	if version != "1.1.0" {
		t.Errorf("resolved version = %q, want 1.1.0 (highest adoptable; retired 2.0.0 excluded)", version)
	}
}

// TestAdapterStandardVersionForInstall_NoAdoptable verifies the
// no-offering outcome: an index whose releases are all retired yields an
// actionable error.
//
// Reference: TS-016-04-01, ADR-027 §3
func TestAdapterStandardVersionForInstall_NoAdoptable(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // no pre-recorded standard
	indexDir := adapterListTestIndex(t,
		adapterListTestRelease(t, "anvil-standard-laravel", "1.0.0", registry.LifecycleStateRetired),
	)
	ix, err := registry.LoadIndex(indexDir)
	if err != nil {
		t.Fatalf("load index: %v", err)
	}
	if _, err := adapterStandardVersionForInstall(ix, "anvil-standard-laravel"); err == nil {
		t.Fatal("expected an error for a retired-only standard, got nil")
	} else if !strings.Contains(err.Error(), "no adoptable release") {
		t.Errorf("error should report that no adoptable release is offered, got: %v", err)
	}
}

// ── Shared Version Selection (TS-016-04-01) ──────────────────────────

// TestHighestAdoptableVersion_SemanticOrdering verifies the shared
// version-selection rule (highestAdoptableVersion — used by both 'anvil
// adapter install' and 'anvil adapter list --available') orders versions
// SEMANTICALLY: 1.10.0 is higher than 1.9.0 although the index client's
// lexical order would place 1.10.0 before 1.9.0.
//
// Reference: TS-016-04-01
func TestHighestAdoptableVersion_SemanticOrdering(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // no pre-recorded standard
	indexDir := adapterListTestIndex(t,
		adapterListTestRelease(t, "anvil-standard-laravel", "1.9.0", registry.LifecycleStatePublished),
		adapterListTestRelease(t, "anvil-standard-laravel", "1.10.0", registry.LifecycleStatePublished),
	)
	ix, err := registry.LoadIndex(indexDir)
	if err != nil {
		t.Fatalf("load index: %v", err)
	}
	if got := highestAdoptableVersion(ix, "anvil-standard-laravel"); got != "1.10.0" {
		t.Errorf("highestAdoptableVersion = %q, want 1.10.0 (semantic order)", got)
	}
}

// TestHighestAdoptableVersion_EmptyWhenNothingOffered verifies the
// empty outcome: an index whose releases are all retired (or invalid)
// yields "" — the callers render their own actionable errors.
//
// Reference: TS-016-04-01, ADR-027 §3
func TestHighestAdoptableVersion_EmptyWhenNothingOffered(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	indexDir := adapterListTestIndex(t,
		adapterListTestRelease(t, "anvil-standard-laravel", "1.0.0", registry.LifecycleStateRetired),
	)
	ix, err := registry.LoadIndex(indexDir)
	if err != nil {
		t.Fatalf("load index: %v", err)
	}
	if got := highestAdoptableVersion(ix, "anvil-standard-laravel"); got != "" {
		t.Errorf("highestAdoptableVersion = %q, want \"\" (nothing offered)", got)
	}
	if got := highestAdoptableVersion(ix, "anvil-standard-nope"); got != "" {
		t.Errorf("highestAdoptableVersion(unknown) = %q, want \"\"", got)
	}
}
