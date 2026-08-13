package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeIndexDoc writes content to relPath under dir (creating parent
// directories), so tests can assemble static index layouts in t.TempDir().
func writeIndexDoc(t *testing.T, dir, relPath, content string) string {
	t.Helper()
	path := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// minimalIndexDoc returns a schema-shaped metadata document for the given
// standard id and version (published state, single digest). The document
// is built by marshaling the Go mirror, so ids and versions are always
// JSON-escaped correctly regardless of content.
func minimalIndexDoc(id, version string) string {
	md := Metadata{
		ID:              id,
		Version:         version,
		ContractVersion: "1.0.0",
		Capability: Capability{
			FrameworkVersion: []string{"5.1.0"},
		},
		Distribution: Distribution{
			Type:     DistributionTypeGitHubReleases,
			Location: "https://github.com/maleolabs/" + id + "/releases/download/v" + version + "/" + id + ".tar.gz",
		},
		Lifecycle: Lifecycle{
			State: LifecycleStatePublished,
		},
		Trust: Trust{
			ContentDigests: []ContentDigest{{
				Algorithm: DigestAlgorithmSHA256,
				Encoding:  DigestEncodingBase16,
				Digest:    "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			}},
			Attestation: Attestation{
				Algorithm: AttestationAlgorithmEd25519,
				Signature: "c2lnbmF0dXJlLXZhbHVlLWJhc2U2NC1lbmNvZGVkLWRlbW8tc2lnbmF0dXJlLTEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5",
				PublicKey: "cHVibGljLWtleS1iYXNlNjQtZW5jb2RlZC1kZW1vLXB1YmxpYy1rZXktMTIzNDU2Nzg5MA==",
			},
		},
	}
	raw, err := json.Marshal(md)
	if err != nil {
		panic(fmt.Sprintf("marshal test metadata: %v", err)) // cannot happen: Metadata marshals losslessly
	}
	return string(raw)
}

// TestLoadIndexResolvesEntryByNameAndVersion asserts entries are resolved
// by standard id and exact version (TS-014-02-01 DoD: entries resolved by
// name and version; ADR-022 §3: explicit pinning).
func TestLoadIndexResolvesEntryByNameAndVersion(t *testing.T) {
	dir := t.TempDir()
	writeIndexDoc(t, dir, "anvil-standard-laravel/1.2.3.json", minimalIndexDoc("anvil-standard-laravel", "1.2.3"))
	writeIndexDoc(t, dir, "anvil-standard-flutter/2.0.0.json", minimalIndexDoc("anvil-standard-flutter", "2.0.0"))

	ix, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	for _, tc := range []struct {
		id      string
		version string
	}{
		{id: "anvil-standard-laravel", version: "1.2.3"},
		{id: "anvil-standard-flutter", version: "2.0.0"},
	} {
		entry, err := ix.Resolve(tc.id, tc.version)
		if err != nil {
			t.Fatalf("Resolve(%q, %q): %v", tc.id, tc.version, err)
		}
		if entry.ID != tc.id {
			t.Errorf("entry.ID = %q, want %q", entry.ID, tc.id)
		}
		if entry.Version != tc.version {
			t.Errorf("entry.Version = %q, want %q", entry.Version, tc.version)
		}
	}
}

// TestResolvedEntryCarriesDoDFields asserts the resolved entry carries the
// full metadata surface the DoD requires: declared contract version,
// capability declaration, distribution location, and lifecycle state
// (TS-014-02-01 DoD), as promoted Metadata fields.
func TestResolvedEntryCarriesDoDFields(t *testing.T) {
	dir := t.TempDir()
	writeIndexDoc(t, dir, "anvil-standard-laravel/1.2.3.json", `{
    "$schema": "urn:anvil:spec:registry-metadata:1.0.0",
    "id": "anvil-standard-laravel",
    "version": "1.2.3",
    "contractVersion": "2.4.0",
    "capability": {
        "frameworkVersion": ["5.1.0", "5.2.0", "5.3.0"]
    },
    "distribution": {
        "type": "github-releases",
        "location": "https://github.com/maleolabs/anvil-standard-laravel/releases/download/v1.2.3/anvil-standard-laravel.tar.gz"
    },
    "lifecycle": {
        "state": "deprecated",
        "removalDate": "2027-01-31T00:00:00Z"
    },
    "trust": {
        "contentDigests": [
            {
                "algorithm": "sha-256",
                "encoding": "base16",
                "digest": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
            }
        ],
        "attestation": {
            "algorithm": "ed25519",
            "signature": "c2lnbmF0dXJlLXZhbHVlLWJhc2U2NC1lbmNvZGVkLWRlbW8tc2lnbmF0dXJlLTEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5",
            "publicKey": "cHVibGljLWtleS1iYXNlNjQtZW5jb2RlZC1kZW1vLXB1YmxpYy1rZXktMTIzNDU2Nzg5MA=="
        }
    }
}`)

	ix, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	entry, err := ix.Resolve("anvil-standard-laravel", "1.2.3")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Declared contract version (ADR-024 §3.1).
	if entry.ContractVersion != "2.4.0" {
		t.Errorf("ContractVersion = %q, want 2.4.0", entry.ContractVersion)
	}
	// Capability declaration (ADR-021 §3.2).
	wantFramework := []string{"5.1.0", "5.2.0", "5.3.0"}
	if !reflect.DeepEqual(entry.Capability.FrameworkVersion, wantFramework) {
		t.Errorf("Capability.FrameworkVersion = %v, want %v", entry.Capability.FrameworkVersion, wantFramework)
	}
	// Distribution location (ADR-030 §3).
	if entry.Distribution.Type != DistributionTypeGitHubReleases {
		t.Errorf("Distribution.Type = %q, want %q", entry.Distribution.Type, DistributionTypeGitHubReleases)
	}
	if !strings.HasPrefix(entry.Distribution.Location, "https://") {
		t.Errorf("Distribution.Location = %q, want https:// location", entry.Distribution.Location)
	}
	// Lifecycle state (ADR-023 §3, ADR-027 §3).
	if entry.Lifecycle.State != LifecycleStateDeprecated {
		t.Errorf("Lifecycle.State = %q, want %q", entry.Lifecycle.State, LifecycleStateDeprecated)
	}
	if entry.Lifecycle.RemovalDate != "2027-01-31T00:00:00Z" {
		t.Errorf("Lifecycle.RemovalDate = %q, want 2027-01-31T00:00:00Z", entry.Lifecycle.RemovalDate)
	}
	// Trust fields are part of the surface too (ADR-022 §3).
	if len(entry.Trust.ContentDigests) == 0 {
		t.Error("Trust.ContentDigests is empty")
	}
	if entry.Trust.Attestation.PublicKey == "" {
		t.Error("Trust.Attestation.PublicKey is empty")
	}
}

// TestResolveMissingVersionListsAvailableVersions asserts a missing
// version produces an actionable ErrEntryNotFound naming the standard and
// listing the versions the index does hold (TS-014-02-01 DoD: missing
// entries produce actionable errors).
func TestResolveMissingVersionListsAvailableVersions(t *testing.T) {
	dir := t.TempDir()
	writeIndexDoc(t, dir, "anvil-standard-laravel/1.0.0.json", minimalIndexDoc("anvil-standard-laravel", "1.0.0"))
	writeIndexDoc(t, dir, "anvil-standard-laravel/1.2.3.json", minimalIndexDoc("anvil-standard-laravel", "1.2.3"))

	ix, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	_, err = ix.Resolve("anvil-standard-laravel", "2.0.0")
	if !errors.Is(err, ErrEntryNotFound) {
		t.Fatalf("Resolve error = %v, want wrapped %v", err, ErrEntryNotFound)
	}
	for _, want := range []string{"anvil-standard-laravel", "2.0.0", "1.0.0", "1.2.3"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// TestResolveUnknownStandard asserts resolving a standard id that is not
// in the index at all returns a wrapped ErrEntryNotFound.
func TestResolveUnknownStandard(t *testing.T) {
	dir := t.TempDir()
	writeIndexDoc(t, dir, "anvil-standard-laravel/1.2.3.json", minimalIndexDoc("anvil-standard-laravel", "1.2.3"))

	ix, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	if _, err := ix.Resolve("anvil-standard-unknown", "1.0.0"); !errors.Is(err, ErrEntryNotFound) {
		t.Fatalf("Resolve error = %v, want wrapped %v", err, ErrEntryNotFound)
	}
}

// TestResolveEmptyArguments asserts empty id or version arguments are
// rejected as a caller error, not treated as a lookup miss.
func TestResolveEmptyArguments(t *testing.T) {
	dir := t.TempDir()
	writeIndexDoc(t, dir, "anvil-standard-laravel/1.2.3.json", minimalIndexDoc("anvil-standard-laravel", "1.2.3"))

	ix, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	for _, tc := range []struct {
		id, version string
	}{
		{id: "", version: "1.2.3"},
		{id: "anvil-standard-laravel", version: ""},
	} {
		if _, err := ix.Resolve(tc.id, tc.version); err == nil {
			t.Errorf("Resolve(%q, %q) succeeded, want an error", tc.id, tc.version)
		}
	}
}

// TestLoadIndexMissingDir asserts a missing index directory produces an
// actionable wrapped ErrIndexNotFound (TS-014-02-01 DoD: missing index
// entries produce actionable errors).
func TestLoadIndexMissingDir(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	_, err := LoadIndex(missing)
	if !errors.Is(err, ErrIndexNotFound) {
		t.Fatalf("LoadIndex error = %v, want wrapped %v", err, ErrIndexNotFound)
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error %q does not mention the missing directory %q", err, missing)
	}
}

// TestLoadIndexPathIsNotADirectory asserts a non-directory index path is
// rejected with an actionable error.
func TestLoadIndexPathIsNotADirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "index.json")
	if err := os.WriteFile(file, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := LoadIndex(file); err == nil {
		t.Fatal("LoadIndex succeeded on a file path, want an error")
	}
}

// TestLoadIndexEmptyDir asserts an empty index directory loads as an empty
// index: every resolution misses with ErrEntryNotFound.
func TestLoadIndexEmptyDir(t *testing.T) {
	ix, err := LoadIndex(t.TempDir())
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	if _, err := ix.Resolve("anvil-standard-laravel", "1.2.3"); !errors.Is(err, ErrEntryNotFound) {
		t.Fatalf("Resolve error = %v, want wrapped %v", err, ErrEntryNotFound)
	}
}

// TestLoadIndexUnreadableDocument asserts an unreadable index document
// fails load with an error naming the file (TS-014-02-01 DoD: unreadable
// index entries produce actionable errors).
func TestLoadIndexUnreadableDocument(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-based unreadability is not reproducible as root")
	}

	dir := t.TempDir()
	path := writeIndexDoc(t, dir, "anvil-standard-laravel/1.2.3.json", minimalIndexDoc("anvil-standard-laravel", "1.2.3"))
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	_, err := LoadIndex(dir)
	if err == nil {
		t.Fatal("LoadIndex succeeded on an unreadable document, want an error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not name the unreadable document %q", err, path)
	}
}

// TestLoadIndexMalformedDocument asserts a document that is not decodable
// JSON fails load with an actionable error naming the file. Schema-level
// parse diagnostics are deferred to TS-014-01-02 (parse.go, planned); the
// index client must still surface the document so the failure is
// actionable.
func TestLoadIndexMalformedDocument(t *testing.T) {
	dir := t.TempDir()
	path := writeIndexDoc(t, dir, "anvil-standard-laravel/1.2.3.json", `{"id": "anvil-standard-laravel",`)

	_, err := LoadIndex(dir)
	if err == nil {
		t.Fatal("LoadIndex succeeded on a malformed document, want an error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not name the malformed document %q", err, path)
	}
}

// TestLoadIndexDocumentWithoutIdentity asserts a decodable document that
// lacks the identity fields fails load: id and version are the index key,
// so a document without them cannot be indexed (structural requirement —
// schema-level validation remains parse's job, TS-014-01-02, parse.go,
// planned).
func TestLoadIndexDocumentWithoutIdentity(t *testing.T) {
	dir := t.TempDir()
	path := writeIndexDoc(t, dir, "anvil-standard-laravel/1.2.3.json", `{
    "contractVersion": "1.0.0",
    "capability": {"frameworkVersion": ["5.1.0"]},
    "distribution": {"type": "github-releases", "location": "https://example.invalid/a.tar.gz"},
    "lifecycle": {"state": "published"},
    "trust": {"contentDigests": [], "attestation": {}}
}`)

	_, err := LoadIndex(dir)
	if err == nil {
		t.Fatal("LoadIndex succeeded on a document without identity, want an error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not name the document %q", err, path)
	}
}

// TestLoadIndexDuplicateEntries asserts two documents declaring the same
// id and version fail index load with an actionable wrapped
// ErrDuplicateEntry naming both documents (TS-014-02-01: duplicates are a
// publishing error — fail fast at load, not first-wins).
func TestLoadIndexDuplicateEntries(t *testing.T) {
	dir := t.TempDir()
	first := writeIndexDoc(t, dir, "anvil-standard-laravel/1.2.3.json", minimalIndexDoc("anvil-standard-laravel", "1.2.3"))

	// Identity comes from document content, not the file path: a second
	// copy under a different path must still be detected as a duplicate.
	second := writeIndexDoc(t, dir, "anvil-standard-laravel/other/1.2.3-copy.json", minimalIndexDoc("anvil-standard-laravel", "1.2.3"))

	_, err := LoadIndex(dir)
	if !errors.Is(err, ErrDuplicateEntry) {
		t.Fatalf("LoadIndex error = %v, want wrapped %v", err, ErrDuplicateEntry)
	}
	if !strings.Contains(err.Error(), first) || !strings.Contains(err.Error(), second) {
		t.Errorf("duplicate error %q does not name both documents (%s, %s)", err, first, second)
	}
}

// TestVersionsSorted asserts Versions lists the available versions of a
// standard in deterministic ascending (lexical) order; semantic version
// ordering is out of scope for the index client.
func TestVersionsSorted(t *testing.T) {
	dir := t.TempDir()
	writeIndexDoc(t, dir, "anvil-standard-laravel/1.10.0.json", minimalIndexDoc("anvil-standard-laravel", "1.10.0"))
	writeIndexDoc(t, dir, "anvil-standard-laravel/1.2.3.json", minimalIndexDoc("anvil-standard-laravel", "1.2.3"))
	writeIndexDoc(t, dir, "anvil-standard-laravel/1.0.0.json", minimalIndexDoc("anvil-standard-laravel", "1.0.0"))

	ix, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	got := ix.Versions("anvil-standard-laravel")
	want := []string{"1.0.0", "1.10.0", "1.2.3"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Versions = %v, want %v", got, want)
	}
	if ix.Versions("anvil-standard-unknown") != nil {
		t.Errorf("Versions(unknown) = %v, want nil", ix.Versions("anvil-standard-unknown"))
	}
}

// TestLoadIndexIgnoresNonJSONAndHiddenEntries asserts the index scan skips
// non-.json files and hidden entries (e.g. .git directories from a git
// checkout), so a fetched/published index tree loads cleanly.
func TestLoadIndexIgnoresNonJSONAndHiddenEntries(t *testing.T) {
	dir := t.TempDir()
	writeIndexDoc(t, dir, "anvil-standard-laravel/1.2.3.json", minimalIndexDoc("anvil-standard-laravel", "1.2.3"))
	writeIndexDoc(t, dir, "README.md", "# static index")
	writeIndexDoc(t, dir, ".git/1.0.0.json", minimalIndexDoc("anvil-standard-laravel", "1.0.0"))
	writeIndexDoc(t, dir, "anvil-standard-flutter/.hidden/2.0.0.json", minimalIndexDoc("anvil-standard-flutter", "2.0.0"))

	ix, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	if _, err := ix.Resolve("anvil-standard-laravel", "1.2.3"); err != nil {
		t.Errorf("Resolve(laravel 1.2.3): %v", err)
	}
	// The hidden copies must not be indexed.
	if got := ix.Versions("anvil-standard-flutter"); got != nil {
		t.Errorf("hidden entry was indexed: Versions(flutter) = %v, want nil", got)
	}
	if got := ix.Versions("anvil-standard-laravel"); len(got) != 1 {
		t.Errorf("Versions(laravel) = %v, want only 1.2.3", got)
	}
}

// TestResolvedEntryCarriesSource asserts every resolved entry carries the
// index document path it was resolved from, for diagnostics.
func TestResolvedEntryCarriesSource(t *testing.T) {
	dir := t.TempDir()
	path := writeIndexDoc(t, dir, "anvil-standard-laravel/1.2.3.json", minimalIndexDoc("anvil-standard-laravel", "1.2.3"))

	ix, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	entry, err := ix.Resolve("anvil-standard-laravel", "1.2.3")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if entry.Source != path {
		t.Errorf("entry.Source = %q, want %q", entry.Source, path)
	}
}

// TestLoadIndexStructuralDecodeOnly asserts the index client performs
// structural decoding only: a document that is decodable but would fail
// strict schema validation (here: empty trust) still indexes when its
// identity is intact. Strict validation is the parse responsibility
// (TS-014-01-02, parse.go, planned) and plugs in at the decode site when
// it lands.
func TestLoadIndexStructuralDecodeOnly(t *testing.T) {
	dir := t.TempDir()
	writeIndexDoc(t, dir, "anvil-standard-laravel/1.2.3.json", `{
    "id": "anvil-standard-laravel",
    "version": "1.2.3",
    "contractVersion": "1.0.0",
    "capability": {"frameworkVersion": ["5.1.0"]},
    "distribution": {"type": "github-releases", "location": "https://example.invalid/a.tar.gz"},
    "lifecycle": {"state": "published"},
    "trust": {"contentDigests": [], "attestation": {}}
}`)

	ix, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	if _, err := ix.Resolve("anvil-standard-laravel", "1.2.3"); err != nil {
		t.Errorf("Resolve: %v", err)
	}
}

// TestResolveAgainstCorpusPositiveFixtures binds the index client to the
// canonical corpus fixtures (read-only): the positive conformance fixtures
// are staged as a static index and resolved by name and version, asserting
// each lifecycle state round-trips through resolution.
func TestResolveAgainstCorpusPositiveFixtures(t *testing.T) {
	names := []string{
		"published-full",
		"deprecated-without-removal-date",
		"deprecated-with-removal-date",
		"retired-metadata",
	}

	dir := t.TempDir()
	wantLifecycle := map[string]string{}
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(fixturesDir, "positive", name+".json"))
		if err != nil {
			t.Fatalf("read fixture %s: %v", name, err)
		}
		var md Metadata
		if err := json.Unmarshal(raw, &md); err != nil {
			t.Fatalf("decode fixture %s: %v", name, err)
		}
		writeIndexDoc(t, dir, filepath.Join(md.ID, md.Version+".json"), string(raw))
		wantLifecycle[md.ID+"@"+md.Version] = md.Lifecycle.State
	}

	ix, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	for key, state := range wantLifecycle {
		id, version, _ := strings.Cut(key, "@")
		entry, err := ix.Resolve(id, version)
		if err != nil {
			t.Fatalf("Resolve(%q, %q): %v", id, version, err)
		}
		if entry.Lifecycle.State != state {
			t.Errorf("Resolve(%q, %q) lifecycle.state = %q, want %q", id, version, entry.Lifecycle.State, state)
		}
	}
}

// TestCorpusFixturesDuplicatePairIsRejected asserts the corpus's own
// fixture pair that declares the same id and version (published-full and
// published-minimal are both anvil-standard-laravel 1.2.3) is rejected as
// a duplicate — the client's duplicate detection is exercised against real
// corpus material, not just synthetic documents.
func TestCorpusFixturesDuplicatePairIsRejected(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"published-full", "published-minimal"} {
		raw, err := os.ReadFile(filepath.Join(fixturesDir, "positive", name+".json"))
		if err != nil {
			t.Fatalf("read fixture %s: %v", name, err)
		}
		var md Metadata
		if err := json.Unmarshal(raw, &md); err != nil {
			t.Fatalf("decode fixture %s: %v", name, err)
		}
		// Distinct paths (the fixture name disambiguates) — the documents
		// themselves declare the same id and version, so the client must
		// still detect the duplicate from content alone.
		writeIndexDoc(t, dir, filepath.Join(md.ID, name+".json"), string(raw))
	}

	if _, err := LoadIndex(dir); !errors.Is(err, ErrDuplicateEntry) {
		t.Fatalf("LoadIndex error = %v, want wrapped %v (fixtures must declare distinct id+version pairs)", err, ErrDuplicateEntry)
	}
}

// TestLoadIndexRejectsSymlinkFile asserts a symlink named *.json that
// points outside the index directory fails load with an actionable error
// naming the path — a broken or hostile index must not be able to read
// arbitrary paths through links (reviewer finding 1).
func TestLoadIndexRejectsSymlinkFile(t *testing.T) {
	dir := t.TempDir()

	// A valid entry document living OUTSIDE the index directory.
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte(minimalIndexDoc("anvil-standard-laravel", "1.2.3")), 0o644); err != nil {
		t.Fatalf("write outside document: %v", err)
	}
	link := filepath.Join(dir, "anvil-standard-laravel", "1.2.3.json")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	_, err := LoadIndex(dir)
	if err == nil {
		t.Fatal("LoadIndex succeeded with a symlinked document, want an error")
	}
	if !strings.Contains(err.Error(), link) {
		t.Errorf("error %q does not name the symlink path %q", err, link)
	}
}

// TestLoadIndexRejectsSymlinkDir asserts a symlink directory named *.json
// fails load with an actionable error naming the path (reviewer finding 1).
func TestLoadIndexRejectsSymlinkDir(t *testing.T) {
	dir := t.TempDir()

	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "1.2.3.json"), []byte(minimalIndexDoc("anvil-standard-laravel", "1.2.3")), 0o644); err != nil {
		t.Fatalf("write outside document: %v", err)
	}
	link := filepath.Join(dir, "anvil-standard-laravel.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	_, err := LoadIndex(dir)
	if err == nil {
		t.Fatal("LoadIndex succeeded with a symlinked directory, want an error")
	}
	if !strings.Contains(err.Error(), link) {
		t.Errorf("error %q does not name the symlink path %q", err, link)
	}
}

// TestLoadIndexOversizedDocument asserts a document exceeding
// MaxIndexDocumentSize fails load with an error naming the file — the
// size cap bounds memory use and reports the cap precisely instead of
// surfacing as a truncated-JSON decode error (reviewer finding 2).
func TestLoadIndexOversizedDocument(t *testing.T) {
	dir := t.TempDir()
	path := writeIndexDoc(t, dir, "anvil-standard-laravel/1.2.3.json", strings.Repeat("x", MaxIndexDocumentSize+1))

	_, err := LoadIndex(dir)
	if err == nil {
		t.Fatal("LoadIndex succeeded on an oversized document, want an error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not name the oversized document %q", err, path)
	}
}

// TestLoadIndexLargeButUnderCapDocument asserts a large but legitimate
// document (well under the cap, with the size driven by real content
// padding) still loads: the cap rejects pathological documents, not big
// metadata (reviewer finding 2).
func TestLoadIndexLargeButUnderCapDocument(t *testing.T) {
	dir := t.TempDir()

	md := Metadata{
		ID:              "anvil-standard-laravel",
		Version:         "1.2.3",
		ContractVersion: "1.0.0",
		Capability:      Capability{FrameworkVersion: []string{"5.1.0"}},
		Distribution: Distribution{
			Type:     DistributionTypeGitHubReleases,
			Location: "https://github.com/maleolabs/anvil-standard-laravel/releases/download/v1.2.3/anvil-standard-laravel.tar.gz",
		},
		Lifecycle: Lifecycle{State: LifecycleStatePublished},
		Trust: Trust{
			ContentDigests: []ContentDigest{{
				Algorithm: DigestAlgorithmSHA256,
				Encoding:  DigestEncodingBase16,
				Digest:    "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			}},
			Attestation: Attestation{
				Algorithm: AttestationAlgorithmEd25519,
				Signature: "c2lnbmF0dXJlLXZhbHVl",
				PublicKey: "cHVibGljLWtleS12YWx1ZQ==",
			},
		},
		Description: strings.Repeat("a", 512<<10), // 512 KiB of description — large, still a metadata document
	}
	raw, err := json.Marshal(md)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(raw) >= MaxIndexDocumentSize {
		t.Fatalf("test document is %d bytes, want under the %d-byte cap", len(raw), MaxIndexDocumentSize)
	}
	writeIndexDoc(t, dir, "anvil-standard-laravel/1.2.3.json", string(raw))

	ix, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}
	if _, err := ix.Resolve("anvil-standard-laravel", "1.2.3"); err != nil {
		t.Errorf("Resolve: %v", err)
	}
}

// TestStandardsListsAvailableStandards asserts Standards returns the
// available standard IDs sorted deterministically — the discovery surface
// T-005 (list available standards) depends on (product GAP-1).
func TestStandardsListsAvailableStandards(t *testing.T) {
	dir := t.TempDir()
	writeIndexDoc(t, dir, "anvil-standard-flutter/2.0.0.json", minimalIndexDoc("anvil-standard-flutter", "2.0.0"))
	writeIndexDoc(t, dir, "anvil-standard-laravel/1.2.3.json", minimalIndexDoc("anvil-standard-laravel", "1.2.3"))
	writeIndexDoc(t, dir, "anvil-standard-laravel/1.0.0.json", minimalIndexDoc("anvil-standard-laravel", "1.0.0"))
	writeIndexDoc(t, dir, "anvil-standard-docs/0.9.0.json", minimalIndexDoc("anvil-standard-docs", "0.9.0"))

	ix, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	got, err := ix.Standards()
	if err != nil {
		t.Fatalf("Standards: %v", err)
	}
	want := []string{"anvil-standard-docs", "anvil-standard-flutter", "anvil-standard-laravel"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Standards = %v, want %v", got, want)
	}
}

// TestStandardsEmptyIndex asserts Standards on an empty index returns an
// empty list, not nil.
func TestStandardsEmptyIndex(t *testing.T) {
	ix, err := LoadIndex(t.TempDir())
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	got, err := ix.Standards()
	if err != nil {
		t.Fatalf("Standards: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Standards = %v, want empty", got)
	}
}

// TestLoadIndexIgnoresHiddenFileAtRoot asserts a hidden file at the index
// root (".index.json") is ignored like every other hidden entry — a
// fetched/published tree may carry dotfiles, and they must not fail load
// or leak into the index.
func TestLoadIndexIgnoresHiddenFileAtRoot(t *testing.T) {
	dir := t.TempDir()
	writeIndexDoc(t, dir, "anvil-standard-laravel/1.2.3.json", minimalIndexDoc("anvil-standard-laravel", "1.2.3"))
	writeIndexDoc(t, dir, ".index.json", minimalIndexDoc("anvil-standard-hidden", "9.9.9"))

	ix, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	if _, err := ix.Resolve("anvil-standard-hidden", "9.9.9"); !errors.Is(err, ErrEntryNotFound) {
		t.Fatalf("hidden file was indexed: Resolve error = %v, want wrapped %v", err, ErrEntryNotFound)
	}
	standards, err := ix.Standards()
	if err != nil {
		t.Fatalf("Standards: %v", err)
	}
	if len(standards) != 1 {
		t.Errorf("Standards = %v, want only anvil-standard-laravel", standards)
	}
}

// TestIndexIDWithPathSeparatorIsSafe asserts a document declaring an id
// containing a path separator is handled as a plain map key: no filesystem
// traversal happens on resolve or enumerate, and the entry round-trips
// under its declared id (reviewer finding 5).
func TestIndexIDWithPathSeparatorIsSafe(t *testing.T) {
	dir := t.TempDir()
	// The canonical layout would be <id>/<version>.json; here the path
	// deliberately does not match the declared id — identity comes from
	// content, and the id is only ever a map key.
	writeIndexDoc(t, dir, "anvil-standard-laravel/1.2.3.json", minimalIndexDoc("a/b", "1.0.0"))

	ix, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	entry, err := ix.Resolve("a/b", "1.0.0")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if entry.ID != "a/b" || entry.Version != "1.0.0" {
		t.Errorf("resolved entry = %q %q, want a/b 1.0.0", entry.ID, entry.Version)
	}
	if got, err := ix.Standards(); err != nil || len(got) != 1 || got[0] != "a/b" {
		t.Errorf("Standards = %v, %v; want [a/b], nil", got, err)
	}
	if !reflect.DeepEqual(ix.Versions("a/b"), []string{"1.0.0"}) {
		t.Errorf("Versions(a/b) = %v, want [1.0.0]", ix.Versions("a/b"))
	}
}

// TestValidEntryAtIndexRoot asserts a standalone entry document at the
// index root loads and resolves — proof that the client does not depend on
// the canonical <id>/<version>.json layout (reviewer finding 5).
func TestValidEntryAtIndexRoot(t *testing.T) {
	dir := t.TempDir()
	writeIndexDoc(t, dir, "standalone.json", minimalIndexDoc("anvil-standard-laravel", "1.2.3"))

	ix, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	entry, err := ix.Resolve("anvil-standard-laravel", "1.2.3")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if entry.ID != "anvil-standard-laravel" || entry.Version != "1.2.3" {
		t.Errorf("resolved entry = %q %q, want anvil-standard-laravel 1.2.3", entry.ID, entry.Version)
	}
}
