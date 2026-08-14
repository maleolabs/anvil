package skillbundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validManifestValue is a Manifest struct matching the test fixtures.
func validManifestValue() Manifest {
	return Manifest{
		Name:            testName,
		Version:         "1.0.0",
		Source:          "anvil",
		ContractVersion: "1.0.0",
		Description:     "How to use the Anvil CLI",
		Files: []string{
			"anvil-cli-usage/SKILL.md",
			"anvil-cli-usage/lifecycle.md",
		},
	}
}

func TestCreateBundle_ValidRoundTrip(t *testing.T) {
	m := validManifestValue()
	contents := map[string][]byte{
		"anvil-cli-usage/SKILL.md":     testSkillMD(),
		"anvil-cli-usage/lifecycle.md": []byte("# Lifecycle\n"),
	}
	data, err := CreateBundle(m, contents)
	if err != nil {
		t.Fatalf("CreateBundle failed: %v", err)
	}

	// A bundle produced by the packer is accepted by the extractor with
	// provenance injected.
	ex, dest, _ := extractToTemp(t, data)
	if ex.Manifest.Name != testName || ex.Manifest.Source != "anvil" {
		t.Fatalf("manifest mismatch: %+v", ex.Manifest)
	}
	skill, err := os.ReadFile(filepath.Join(dest, testName, SkillMarkdownFileName))
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	if !bytes.Contains(skill, []byte("# source: anvil 1.0.0")) {
		t.Fatalf("provenance header missing:\n%s", skill)
	}
}

func TestCreateBundle_InvalidManifestRejected(t *testing.T) {
	m := validManifestValue()
	m.Name = "Bad_Name"
	if _, err := CreateBundle(m, map[string][]byte{}); err == nil || !strings.Contains(err.Error(), "manifest is invalid") {
		t.Fatalf("CreateBundle accepted an invalid manifest: %v", err)
	}
}

func TestCreateBundle_MissingContentRejected(t *testing.T) {
	m := validManifestValue()
	if _, err := CreateBundle(m, map[string][]byte{"anvil-cli-usage/SKILL.md": testSkillMD()}); err == nil {
		t.Fatal("CreateBundle accepted a manifest without all its declared content")
	}
}

func TestCreateBundle_UndeclaredContentRejected(t *testing.T) {
	m := validManifestValue()
	contents := map[string][]byte{
		"anvil-cli-usage/SKILL.md":     testSkillMD(),
		"anvil-cli-usage/lifecycle.md": []byte("# L\n"),
		"anvil-cli-usage/extra.md":     []byte("undeclared"),
	}
	if _, err := CreateBundle(m, contents); err == nil {
		t.Fatal("CreateBundle accepted content not declared in the manifest")
	}
}

func TestCreateBundle_Deterministic(t *testing.T) {
	m := validManifestValue()
	contents := map[string][]byte{
		"anvil-cli-usage/SKILL.md":     testSkillMD(),
		"anvil-cli-usage/lifecycle.md": []byte("# Lifecycle\n"),
	}
	a, err := CreateBundle(m, contents)
	if err != nil {
		t.Fatal(err)
	}
	b, err := CreateBundle(m, contents)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("CreateBundle is not deterministic for equal inputs")
	}
}

func TestCreateBundle_ArchiveLayout(t *testing.T) {
	// The archive carries manifest.json first, then the content files in
	// manifest order, all plain regular files (no dir entries), all mode
	// 0644 — the layout the extractor pins.
	m := validManifestValue()
	data, err := CreateBundle(m, map[string][]byte{
		"anvil-cli-usage/SKILL.md":     testSkillMD(),
		"anvil-cli-usage/lifecycle.md": []byte("# Lifecycle\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var names []string
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		names = append(names, hdr.Name)
		if hdr.Typeflag != tar.TypeReg {
			t.Fatalf("entry %q is not a regular file (flag %d)", hdr.Name, hdr.Typeflag)
		}
		if hdr.Mode != 0o644 {
			t.Fatalf("entry %q mode = %o, want 0644", hdr.Name, hdr.Mode)
		}
	}
	want := []string{ManifestFileName, "anvil-cli-usage/SKILL.md", "anvil-cli-usage/lifecycle.md"}
	if len(names) != len(want) {
		t.Fatalf("entries = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("entry %d = %q, want %q (pinned order)", i, names[i], want[i])
		}
	}
}

func TestCreateBundle_SizesCapped(t *testing.T) {
	m := validManifestValue()
	// Per-asset cap enforced at pack time.
	m.Files = []string{"anvil-cli-usage/SKILL.md", "anvil-cli-usage/big.bin"}
	big := make([]byte, MaxAssetSize+1)
	if _, err := CreateBundle(m, map[string][]byte{
		"anvil-cli-usage/SKILL.md": testSkillMD(),
		"anvil-cli-usage/big.bin":  big,
	}); err == nil || !strings.Contains(err.Error(), "per-asset cap") {
		t.Fatalf("CreateBundle accepted an over-cap asset: %v", err)
	}
}

func TestCreateBundle_InvalidFrontmatterRejected(t *testing.T) {
	// The packer validates the SKILL.md through the same portable-field
	// parse the extractor applies at install (ADR-037 D1): a bundle with
	// an agent-specific frontmatter field is never emitted.
	m := validManifestValue()
	bad := []byte("---\nname: anvil-cli-usage\ndescription: x\ncontext:\n  fork: true\n---\n")
	_, err := CreateBundle(m, map[string][]byte{
		"anvil-cli-usage/SKILL.md":     bad,
		"anvil-cli-usage/lifecycle.md": []byte("# L\n"),
	})
	if err == nil || !strings.Contains(err.Error(), "frontmatter") {
		t.Fatalf("CreateBundle accepted invalid frontmatter: %v", err)
	}

	// The frontmatter name must match the manifest name (skill-bundle-
	// format.md §5.1).
	mismatch := []byte("---\nname: other-skill\ndescription: x\n---\n")
	_, err = CreateBundle(m, map[string][]byte{
		"anvil-cli-usage/SKILL.md":     mismatch,
		"anvil-cli-usage/lifecycle.md": []byte("# L\n"),
	})
	if err == nil || !strings.Contains(err.Error(), "does not match the manifest name") {
		t.Fatalf("CreateBundle accepted a mismatched frontmatter name: %v", err)
	}
}

func TestCreateBundle_LongPathRoundTrip(t *testing.T) {
	// A content path longer than the 100-byte USTAR name field (but
	// within the 255-byte USTAR total and the format's depth cap) must be
	// encoded with the USTAR prefix split — never a PAX/GNU extended
	// header, which the extractor rejects — so the packer's own bundle
	// extracts cleanly.
	longName := testName + "/" + strings.Repeat("d", 110) + "/file.md" // 133 bytes
	if len(longName) <= 100 || len(longName) > 255 || pathDepth(longName) > MaxPathDepth {
		t.Fatalf("test path length %d / depth %d outside the USTAR split range", len(longName), pathDepth(longName))
	}
	m := validManifestValue()
	m.Files = []string{"anvil-cli-usage/SKILL.md", longName}
	data, err := CreateBundle(m, map[string][]byte{
		"anvil-cli-usage/SKILL.md": testSkillMD(),
		longName:                   []byte("deep content\n"),
	})
	if err != nil {
		t.Fatalf("CreateBundle failed on a USTAR-encodable long path: %v", err)
	}
	_, dest, _ := extractToTemp(t, data)
	got, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(longName)))
	if err != nil {
		t.Fatalf("long-path file not extracted: %v", err)
	}
	if string(got) != "deep content\n" {
		t.Fatalf("long-path file content = %q", got)
	}
}

func TestCreateBundle_OverlongPathRejected(t *testing.T) {
	// A 256-byte path is manifest-valid (≤ MaxFilePathLength) but beyond
	// USTAR's 255-byte total; the packer must fail hard instead of
	// silently upgrading to an extended header the extractor rejects.
	tooLong := testName + "/" + strings.Repeat("x", 241) // 15 + 241 = 256
	m := validManifestValue()
	m.Files = []string{"anvil-cli-usage/SKILL.md", tooLong}
	_, err := CreateBundle(m, map[string][]byte{
		"anvil-cli-usage/SKILL.md": testSkillMD(),
		tooLong:                    []byte("x"),
	})
	if err == nil {
		t.Fatal("CreateBundle accepted a path beyond the USTAR encoding limit")
	}
}

// ── Asset name convention ────────────────────────────────────────────

func TestBundleFileName(t *testing.T) {
	name, err := BundleFileName("anvil-cli-usage", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if name != "anvil-skill-anvil-cli-usage-1.0.0.tar.gz" {
		t.Fatalf("name = %q", name)
	}
	for _, bad := range []struct{ n, v string }{
		{"Bad_Name", "1.0.0"},
		{"-x", "1.0.0"},
		{"anvil", "v1.0.0"},
		{"anvil", "1.0"},
		{"", "1.0.0"},
		{"anvil", ""},
	} {
		if _, err := BundleFileName(bad.n, bad.v); err == nil {
			t.Errorf("BundleFileName(%q, %q) accepted; want rejection", bad.n, bad.v)
		}
	}
}

func TestParseBundleFileName(t *testing.T) {
	cases := []struct {
		filename string
		name     string
		version  string
	}{
		{"anvil-skill-anvil-cli-usage-1.0.0.tar.gz", "anvil-cli-usage", "1.0.0"},
		{"anvil-skill-foo-0.1.0.tar.gz", "foo", "0.1.0"},
		{"anvil-skill-a-10.20.30.tar.gz", "a", "10.20.30"},
	}
	for _, tc := range cases {
		name, version, err := ParseBundleFileName(tc.filename)
		if err != nil {
			t.Errorf("ParseBundleFileName(%q) failed: %v", tc.filename, err)
			continue
		}
		if name != tc.name || version != tc.version {
			t.Errorf("ParseBundleFileName(%q) = (%q, %q), want (%q, %q)",
				tc.filename, name, version, tc.name, tc.version)
		}
	}

	// Round trip: a name that itself contains hyphens splits unambiguously
	// because the version is pinned to semver.
	name, version, err := ParseBundleFileName("anvil-skill-my-skill-name-2.3.4.tar.gz")
	if err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
	if name != "my-skill-name" || version != "2.3.4" {
		t.Fatalf("round trip = (%q, %q)", name, version)
	}

	for _, bad := range []string{
		"",
		"foo.tar.gz",
		"anvil-skill-foo.tar.gz",
		"anvil-skill-foo-1.0.tar.gz",
		"anvil-skill-Bad_Name-1.0.0.tar.gz",
		"anvil-skill-foo-1.0.0.tgz",
		"anvil-skill--1.0.0.tar.gz",
	} {
		if _, _, err := ParseBundleFileName(bad); err == nil {
			t.Errorf("ParseBundleFileName(%q) accepted; want rejection", bad)
		}
	}
}
