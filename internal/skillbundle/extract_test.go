package skillbundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── Archive-building test helpers ────────────────────────────────────

// tarEntry is one archive entry for the test archive builders.
type tarEntry struct {
	name     string
	typ      byte
	data     []byte
	mode     int64
	linkname string
	format   tar.Format
}

// writeTestEntry writes one entry. A zero typ defaults to a regular file;
// a zero mode defaults to 0644; a non-zero format forces the tar encoding.
func writeTestEntry(tw *tar.Writer, e tarEntry) error {
	typ := e.typ
	if typ == 0 {
		typ = tar.TypeReg
	}
	mode := e.mode
	if mode == 0 {
		mode = 0o644
	}
	hdr := &tar.Header{
		Name:     e.name,
		Typeflag: typ,
		Linkname: e.linkname,
		Mode:     mode,
		Size:     int64(len(e.data)),
		ModTime:  time.Time{},
		Format:   e.format,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if len(e.data) > 0 {
		if _, err := tw.Write(e.data); err != nil {
			return err
		}
	}
	return nil
}

// buildArchive builds a single-member gzip tar from the entries.
func buildArchive(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		if err := writeTestEntry(tw, e); err != nil {
			t.Fatalf("write entry %q: %v", e.name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

// buildBundleArchive builds a skill bundle archive from a manifest
// document plus content entries (manifest.json is written first).
func buildBundleArchive(t *testing.T, manifest []byte, entries ...tarEntry) []byte {
	t.Helper()
	all := append([]tarEntry{{name: ManifestFileName, data: manifest}}, entries...)
	return buildArchive(t, all...)
}

// ── Fixtures ─────────────────────────────────────────────────────────

// testName is the skill name used across the fixtures.
const testName = "anvil-cli-usage"

// testManifestJSON builds a valid manifest document for the given content
// files (which must live under the skill root).
func testManifestJSON(t *testing.T, files ...string) []byte {
	t.Helper()
	m := map[string]any{
		"name":            testName,
		"version":         "1.0.0",
		"source":          "anvil",
		"contractVersion": "1.0.0",
		"description":     "How to use the Anvil CLI",
		"files":           files,
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// testSkillMD is a valid SKILL.md for the test skill.
func testSkillMD() []byte {
	return []byte("---\nname: anvil-cli-usage\ndescription: How to use the Anvil CLI\n---\n# Anvil CLI Usage\n")
}

// validBundleBytes builds a well-formed skill bundle archive.
func validBundleBytes(t *testing.T) []byte {
	t.Helper()
	manifest := testManifestJSON(t,
		"anvil-cli-usage/SKILL.md",
		"anvil-cli-usage/lifecycle.md",
	)
	return buildBundleArchive(t, manifest,
		tarEntry{name: "anvil-cli-usage/SKILL.md", data: testSkillMD()},
		tarEntry{name: "anvil-cli-usage/lifecycle.md", data: []byte("# Lifecycle\n")},
	)
}

// extractToTemp extracts data into a fresh temp directory, returning the
// result and a cleanup function.
func extractToTemp(t *testing.T, data []byte) (*Extraction, string, func()) {
	t.Helper()
	dest := t.TempDir()
	ex, err := Extract(data, dest)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	return ex, dest, func() {}
}

// wantExtractError asserts that Extract rejects the archive with a
// BundleError of the given kind on the given field, with an actionable
// message containing substr.
func wantExtractError(t *testing.T, data []byte, kind ErrorKind, field, substr string) {
	t.Helper()
	dest := t.TempDir()
	_, err := Extract(data, dest)
	if err == nil {
		t.Fatalf("Extract accepted the archive; want kind %q on %q", kind, field)
	}
	var be *BundleError
	if !errors.As(err, &be) {
		t.Fatalf("errors.As(*BundleError) failed; got %T: %v", err, err)
	}
	if be.Kind != kind {
		t.Fatalf("Kind = %q, want %q (err: %v)", be.Kind, kind, err)
	}
	if field != "" && be.Field != field {
		t.Fatalf("Field = %q, want %q (err: %v)", be.Field, field, err)
	}
	if substr != "" && !strings.Contains(err.Error(), substr) {
		t.Fatalf("error %q does not contain %q", err.Error(), substr)
	}
}

// ── Positive path ────────────────────────────────────────────────────

func TestExtract_ValidBundle(t *testing.T) {
	ex, dest, _ := extractToTemp(t, validBundleBytes(t))

	if ex.Manifest.Name != testName || ex.Manifest.Version != "1.0.0" || ex.Manifest.Source != "anvil" {
		t.Fatalf("manifest mismatch: %+v", ex.Manifest)
	}
	if ex.Frontmatter.Name != testName {
		t.Fatalf("frontmatter name = %q", ex.Frontmatter.Name)
	}
	if len(ex.Files) != 2 {
		t.Fatalf("Files = %v", ex.Files)
	}

	// The tree exists with mode 0644 files and 0755 directories.
	for _, rel := range ex.Files {
		info, err := os.Stat(filepath.Join(dest, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("stat %s: %v", rel, err)
		}
		if info.Mode().Perm() != 0o644 {
			t.Errorf("%s mode = %v, want 0644 (exec bit stripped)", rel, info.Mode().Perm())
		}
	}
	dirInfo, err := os.Stat(filepath.Join(dest, testName))
	if err != nil {
		t.Fatalf("stat skill root: %v", err)
	}
	if dirInfo.Mode().Perm() != 0o755 {
		t.Errorf("skill root mode = %v, want 0755", dirInfo.Mode().Perm())
	}

	// The installed SKILL.md carries the provenance header (ADR-037 D10).
	skill, err := os.ReadFile(filepath.Join(dest, testName, SkillMarkdownFileName))
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	if !bytes.Contains(skill, []byte("# source: anvil 1.0.0")) {
		t.Fatalf("provenance header missing in installed SKILL.md:\n%s", skill)
	}
	// The body is preserved.
	if !bytes.Contains(skill, []byte("# Anvil CLI Usage")) {
		t.Fatalf("SKILL.md body not preserved:\n%s", skill)
	}
	// The installed copy still validates as portable frontmatter.
	if _, err := ParseFrontmatter(skill); err != nil {
		t.Fatalf("installed SKILL.md fails portable validation: %v", err)
	}
}

func TestExtract_DirectoryEntriesAllowed(t *testing.T) {
	// tar czf-style archives carry directory entries with trailing
	// slashes; they are allowed (and created 0755).
	manifest := testManifestJSON(t, "anvil-cli-usage/SKILL.md", "anvil-cli-usage/notes/cheat.md")
	data := buildBundleArchive(t, manifest,
		tarEntry{name: "anvil-cli-usage/", typ: tar.TypeDir, mode: 0o700},
		tarEntry{name: "anvil-cli-usage/SKILL.md", data: testSkillMD()},
		tarEntry{name: "anvil-cli-usage/notes/", typ: tar.TypeDir, mode: 0o700},
		tarEntry{name: "anvil-cli-usage/notes/cheat.md", data: []byte("# Cheat\n")},
	)
	ex, dest, _ := extractToTemp(t, data)
	if len(ex.Files) != 2 {
		t.Fatalf("Files = %v", ex.Files)
	}
	info, err := os.Stat(filepath.Join(dest, "anvil-cli-usage", "notes"))
	if err != nil {
		t.Fatalf("stat notes dir: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("dir mode = %v, want 0755 (exec bit stripped)", info.Mode().Perm())
	}
}

// ── Security matrix: traversal ───────────────────────────────────────

func TestExtract_TraversalRejected(t *testing.T) {
	cases := []struct {
		name  string
		entry string
		want  string // message substring; empty = any actionable message
	}{
		{"parent-component", "../evil", "traversal"},
		{"nested-parent", "anvil-cli-usage/../../evil", "traversal"},
		{"dot-component", "./anvil-cli-usage/SKILL.md", "'."},
		{"trailing-dotdot", "anvil-cli-usage/..", "traversal"},
		{"empty-component", "anvil-cli-usage//SKILL.md", "empty path component"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifest := testManifestJSON(t, "anvil-cli-usage/SKILL.md")
			data := buildBundleArchive(t, manifest,
				tarEntry{name: tc.entry, data: []byte("evil")},
				tarEntry{name: "anvil-cli-usage/SKILL.md", data: testSkillMD()},
			)
			wantExtractError(t, data, ErrorKindStructure, tc.entry, tc.want)
		})
	}
}

// ── Security matrix: absolute paths ──────────────────────────────────

func TestExtract_AbsolutePathRejected(t *testing.T) {
	cases := []struct {
		name  string
		entry string
		want  string
	}{
		{"posix-absolute", "/etc/passwd", "absolute"},
		{"posix-absolute-nested", "/anvil-cli-usage/SKILL.md", "absolute"},
		{"drive-letter", "C:\\evil", "drive-letter"},
		{"drive-letter-no-backslash", "C:evil", "drive-letter"},
		{"backslash-separator", `anvil-cli-usage\SKILL.md`, "backslash"},
		{"backslash-traversal", `anvil-cli-usage\..\..\evil`, "backslash"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifest := testManifestJSON(t, "anvil-cli-usage/SKILL.md")
			data := buildBundleArchive(t, manifest,
				tarEntry{name: tc.entry, data: []byte("evil")},
				tarEntry{name: "anvil-cli-usage/SKILL.md", data: testSkillMD()},
			)
			wantExtractError(t, data, ErrorKindStructure, tc.entry, tc.want)
		})
	}
}

// ── Security matrix: symlink escape ──────────────────────────────────

func TestExtract_SymlinkAndHardlinkRejected(t *testing.T) {
	cases := []struct {
		name     string
		typ      byte
		linkname string
	}{
		{"file-symlink", tar.TypeSymlink, "/etc"},
		{"file-symlink-relative", tar.TypeSymlink, "../../../../etc"},
		{"dir-symlink", tar.TypeSymlink, "/etc"},
		{"hardlink", tar.TypeLink, "anvil-cli-usage/SKILL.md"},
	}
	entries := []struct {
		name     string
		typ      byte
		linkname string
	}{}
	_ = entries
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifest := testManifestJSON(t, "anvil-cli-usage/SKILL.md")
			typ := tc.typ
			name := "anvil-cli-usage/link"
			if typ == tar.TypeDir {
				name = "anvil-cli-usage/linkdir/"
			}
			data := buildBundleArchive(t, manifest,
				tarEntry{name: name, typ: typ, linkname: tc.linkname},
				tarEntry{name: "anvil-cli-usage/SKILL.md", data: testSkillMD()},
			)
			wantExtractError(t, data, ErrorKindStructure, name, "not allowed in a skill bundle")
		})
	}
}

func TestExtract_SymlinkRootResolved(t *testing.T) {
	// Defense in depth: the extraction root itself may be reached through
	// a symlink; the extractor resolves it so writes land inside the real
	// directory and containment holds against the resolved root.
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	ex, err := Extract(validBundleBytes(t), link)
	if err != nil {
		t.Fatalf("Extract via symlinked root failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(real, testName, SkillMarkdownFileName)); err != nil {
		t.Fatalf("content not written to the resolved root: %v", err)
	}
	if ex.Dest != real {
		t.Fatalf("Dest = %q, want resolved root %q", ex.Dest, real)
	}
}

// ── Security matrix: exec bit stripping ──────────────────────────────

func TestExtract_ExecBitStripped(t *testing.T) {
	manifest := testManifestJSON(t,
		"anvil-cli-usage/SKILL.md",
		"anvil-cli-usage/run.sh",
		"anvil-cli-usage/tool.bin",
	)
	data := buildBundleArchive(t, manifest,
		tarEntry{name: "anvil-cli-usage/SKILL.md", data: testSkillMD(), mode: 0o755},
		tarEntry{name: "anvil-cli-usage/run.sh", data: []byte("#!/bin/sh\n"), mode: 0o777},
		tarEntry{name: "anvil-cli-usage/tool.bin", data: []byte("binary"), mode: 0o700},
	)
	_, dest, _ := extractToTemp(t, data)
	for _, rel := range []string{"anvil-cli-usage/SKILL.md", "anvil-cli-usage/run.sh", "anvil-cli-usage/tool.bin"} {
		info, err := os.Stat(filepath.Join(dest, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("stat %s: %v", rel, err)
		}
		if got := info.Mode().Perm(); got != 0o644 {
			t.Errorf("%s mode = %v, want 0644 (exec bit stripped; ADR-037 D4)", rel, got)
		}
	}
}

// ── Security matrix: caps ────────────────────────────────────────────

func TestExtract_PerAssetCap(t *testing.T) {
	// One file beyond the per-asset cap is rejected before any write.
	big := bytes.Repeat([]byte{0}, MaxAssetSize+1)
	manifest := testManifestJSON(t, "anvil-cli-usage/SKILL.md", "anvil-cli-usage/big.bin")
	data := buildBundleArchive(t, manifest,
		tarEntry{name: "anvil-cli-usage/SKILL.md", data: testSkillMD()},
		tarEntry{name: "anvil-cli-usage/big.bin", data: big},
	)
	wantExtractError(t, data, ErrorKindLimits, "anvil-cli-usage/big.bin", "per-asset cap")

	// A file exactly at the cap is accepted (when the total stays within
	// the total cap).
	exact := bytes.Repeat([]byte{0}, MaxAssetSize)
	manifest = testManifestJSON(t, "anvil-cli-usage/SKILL.md", "anvil-cli-usage/exact.bin")
	data = buildBundleArchive(t, manifest,
		tarEntry{name: "anvil-cli-usage/SKILL.md", data: testSkillMD()},
		tarEntry{name: "anvil-cli-usage/exact.bin", data: exact},
	)
	ex, dest, _ := extractToTemp(t, data)
	info, err := os.Stat(filepath.Join(dest, "anvil-cli-usage", "exact.bin"))
	if err != nil {
		t.Fatalf("stat exact.bin: %v", err)
	}
	if info.Size() != MaxAssetSize {
		t.Fatalf("exact.bin size = %d, want %d", info.Size(), MaxAssetSize)
	}
	if len(ex.Files) != 2 {
		t.Fatalf("Files = %v", ex.Files)
	}
}

func TestExtract_TotalCap(t *testing.T) {
	// The total cap is enforced during extraction: enough per-asset-sized
	// files to exceed MaxTotalSize are rejected mid-extraction.
	chunk := bytes.Repeat([]byte{0}, MaxAssetSize)
	manifestFiles := []string{"anvil-cli-usage/SKILL.md"}
	entries := []tarEntry{{name: "anvil-cli-usage/SKILL.md", data: testSkillMD()}}
	// (MaxTotalSize / MaxAssetSize) + 2 chunks = 8 chunks = 80 MiB > 64 MiB.
	for i := 0; i < (MaxTotalSize/MaxAssetSize)+2; i++ {
		name := fmt.Sprintf("anvil-cli-usage/chunk-%02d.bin", i)
		manifestFiles = append(manifestFiles, name)
		entries = append(entries, tarEntry{name: name, data: chunk})
	}
	manifest := testManifestJSON(t, manifestFiles...)
	data := buildBundleArchive(t, manifest, entries...)
	wantExtractError(t, data, ErrorKindLimits, "anvil-cli-usage/chunk-06.bin", "total cap")
}

func TestExtract_FileCountCap(t *testing.T) {
	manifestFiles := []string{"anvil-cli-usage/SKILL.md"}
	entries := []tarEntry{{name: "anvil-cli-usage/SKILL.md", data: testSkillMD()}}
	// MaxFileCount more content files: the SKILL.md is the first, so the
	// entry that would be file number MaxFileCount+1 is rejected.
	for i := 0; i < MaxFileCount; i++ {
		name := fmt.Sprintf("anvil-cli-usage/f%03d.txt", i)
		manifestFiles = append(manifestFiles, name)
		entries = append(entries, tarEntry{name: name, data: []byte("x")})
	}
	manifest := testManifestJSON(t, manifestFiles...)
	data := buildBundleArchive(t, manifest, entries...)
	rejected := fmt.Sprintf("anvil-cli-usage/f%03d.txt", MaxFileCount-1)
	wantExtractError(t, data, ErrorKindLimits, rejected, "file cap")
}

// ── Archive structure matrix ─────────────────────────────────────────

func TestExtract_ExtendedHeadersRejected(t *testing.T) {
	longName := "anvil-cli-usage/" + strings.Repeat("x", 150) + ".md"
	manifest := testManifestJSON(t, "anvil-cli-usage/SKILL.md")
	data := buildBundleArchive(t, manifest,
		tarEntry{name: longName, data: []byte("x"), format: tar.FormatPAX},
		tarEntry{name: "anvil-cli-usage/SKILL.md", data: testSkillMD()},
	)
	wantExtractError(t, data, ErrorKindStructure, longName, "extended header")
}

func TestExtract_NotTarGz(t *testing.T) {
	wantExtractError(t, []byte("not a gzip archive at all"), ErrorKindStructure, "bundle", "not a skill bundle")
}

func TestExtract_TruncatedArchive(t *testing.T) {
	data := validBundleBytes(t)
	// Cut the gzip stream mid-way; the failure is an integrity rejection
	// (the exact field depends on where the stream broke).
	truncated := data[:len(data)/2]
	wantExtractError(t, truncated, ErrorKindIntegrity, "", "")
}

func TestExtract_TrailingDataRejected(t *testing.T) {
	data := validBundleBytes(t)
	// Garbage after the gzip member.
	withTrailing := append(append([]byte{}, data...), []byte("garbage")...)
	wantExtractError(t, withTrailing, ErrorKindStructure, "bundle", "trailing input")

	// A second gzip member is trailing input too.
	var second bytes.Buffer
	gz := gzip.NewWriter(&second)
	_, _ = gz.Write([]byte("second member"))
	_ = gz.Close()
	twoMembers := append(append([]byte{}, data...), second.Bytes()...)
	wantExtractError(t, twoMembers, ErrorKindStructure, "bundle", "trailing input")
}

func TestExtract_DecompressedTrailingDataRejected(t *testing.T) {
	// Decompressed bytes after the tar end-of-archive markers are
	// rejected by the bounded drain.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = writeTestEntry(tw, tarEntry{name: ManifestFileName, data: testManifestJSON(t, "anvil-cli-usage/SKILL.md")})
	_ = writeTestEntry(tw, tarEntry{name: "anvil-cli-usage/SKILL.md", data: testSkillMD()})
	_ = tw.Close()
	_, _ = gz.Write([]byte("decompressed trailing data after the tar stream"))
	_ = gz.Close()
	wantExtractError(t, buf.Bytes(), ErrorKindStructure, "bundle", "trailing data")
}

func TestExtract_ManifestNotFirst(t *testing.T) {
	data := buildArchive(t,
		tarEntry{name: "anvil-cli-usage/SKILL.md", data: testSkillMD()},
		tarEntry{name: ManifestFileName, data: testManifestJSON(t, "anvil-cli-usage/SKILL.md")},
	)
	wantExtractError(t, data, ErrorKindStructure, "anvil-cli-usage/SKILL.md", "manifest.json first")
}

func TestExtract_ManifestSizeCap(t *testing.T) {
	huge := bytes.Repeat([]byte{' '}, MaxManifestSize+1)
	// A manifest entry beyond the cap is rejected before reading.
	data := buildArchive(t, tarEntry{name: ManifestFileName, data: huge})
	wantExtractError(t, data, ErrorKindLimits, ManifestFileName, "manifest cap")
}

func TestExtract_ManifestInvalid(t *testing.T) {
	data := buildArchive(t,
		tarEntry{name: ManifestFileName, data: []byte(`{"name": "Bad_Name"}`)},
	)
	dest := t.TempDir()
	_, err := Extract(data, dest)
	var be *BundleError
	if !errors.As(err, &be) {
		t.Fatalf("errors.As(*BundleError) failed: %v", err)
	}
	if be.Kind != ErrorKindManifest {
		t.Fatalf("Kind = %q, want %q", be.Kind, ErrorKindManifest)
	}
	var me *ManifestError
	if !errors.As(err, &me) {
		t.Fatalf("errors.As(*ManifestError) through BundleError failed: %v", err)
	}
}

func TestExtract_EntryOutsideContentRoot(t *testing.T) {
	manifest := testManifestJSON(t, "anvil-cli-usage/SKILL.md")
	data := buildBundleArchive(t, manifest,
		tarEntry{name: "other/SKILL.md", data: []byte("stray")},
		tarEntry{name: "anvil-cli-usage/SKILL.md", data: testSkillMD()},
	)
	wantExtractError(t, data, ErrorKindStructure, "other/SKILL.md", "content root")
}

func TestExtract_EntryNotInManifest(t *testing.T) {
	manifest := testManifestJSON(t, "anvil-cli-usage/SKILL.md")
	data := buildBundleArchive(t, manifest,
		tarEntry{name: "anvil-cli-usage/SKILL.md", data: testSkillMD()},
		tarEntry{name: "anvil-cli-usage/extra.md", data: []byte("undeclared")},
	)
	wantExtractError(t, data, ErrorKindStructure, "anvil-cli-usage/extra.md", "not declared in the manifest's files[]")
}

func TestExtract_ManifestDeclaresMissingFile(t *testing.T) {
	manifest := testManifestJSON(t, "anvil-cli-usage/SKILL.md", "anvil-cli-usage/missing.md")
	data := buildBundleArchive(t, manifest,
		tarEntry{name: "anvil-cli-usage/SKILL.md", data: testSkillMD()},
	)
	wantExtractError(t, data, ErrorKindStructure, "anvil-cli-usage/missing.md", "missing from the archive")
}

func TestExtract_DuplicateEntryRejected(t *testing.T) {
	manifest := testManifestJSON(t, "anvil-cli-usage/SKILL.md")
	data := buildBundleArchive(t, manifest,
		tarEntry{name: "anvil-cli-usage/SKILL.md", data: testSkillMD()},
		tarEntry{name: "anvil-cli-usage/SKILL.md", data: []byte("duplicate")},
	)
	wantExtractError(t, data, ErrorKindStructure, "anvil-cli-usage/SKILL.md", "duplicate")
}

func TestExtract_DirFileConflict(t *testing.T) {
	// A directory entry at a path already extracted as a file.
	manifest := testManifestJSON(t, "anvil-cli-usage/SKILL.md", "anvil-cli-usage/conflict.md")
	data := buildBundleArchive(t, manifest,
		tarEntry{name: "anvil-cli-usage/SKILL.md", data: testSkillMD()},
		tarEntry{name: "anvil-cli-usage/conflict.md", data: []byte("x")},
		tarEntry{name: "anvil-cli-usage/conflict.md/", typ: tar.TypeDir},
	)
	wantExtractError(t, data, ErrorKindStructure, "anvil-cli-usage/conflict.md", "dir/file conflict")

	// A file whose parent was extracted as a file (the child is declared
	// in the manifest so the conflict check is what fires).
	manifest = testManifestJSON(t, "anvil-cli-usage/SKILL.md", "anvil-cli-usage/conflict.md", "anvil-cli-usage/conflict.md/child.txt")
	data = buildBundleArchive(t, manifest,
		tarEntry{name: "anvil-cli-usage/SKILL.md", data: testSkillMD()},
		tarEntry{name: "anvil-cli-usage/conflict.md", data: []byte("x")},
		tarEntry{name: "anvil-cli-usage/conflict.md/child.txt", data: []byte("y")},
	)
	wantExtractError(t, data, ErrorKindStructure, "anvil-cli-usage/conflict.md/child.txt", "conflicts with")
}

// ── Frontmatter failures in the bundle flow ──────────────────────────

func TestExtract_FrontmatterRejected(t *testing.T) {
	// Agent-specific frontmatter field in the bundled SKILL.md.
	badSkill := []byte("---\nname: anvil-cli-usage\ndescription: x\ncontext:\n  fork: true\n---\nbody\n")
	manifest := testManifestJSON(t, "anvil-cli-usage/SKILL.md")
	data := buildBundleArchive(t, manifest,
		tarEntry{name: "anvil-cli-usage/SKILL.md", data: badSkill},
	)
	dest := t.TempDir()
	_, err := Extract(data, dest)
	var be *BundleError
	if !errors.As(err, &be) {
		t.Fatalf("errors.As(*BundleError) failed: %v", err)
	}
	if be.Kind != ErrorKindFrontmatter {
		t.Fatalf("Kind = %q, want %q", be.Kind, ErrorKindFrontmatter)
	}
	var fe *FrontmatterError
	if !errors.As(err, &fe) {
		t.Fatalf("errors.As(*FrontmatterError) through BundleError failed: %v", err)
	}
}

func TestExtract_FrontmatterNameMismatch(t *testing.T) {
	mismatched := []byte("---\nname: another-skill\ndescription: x\n---\nbody\n")
	manifest := testManifestJSON(t, "anvil-cli-usage/SKILL.md")
	data := buildBundleArchive(t, manifest,
		tarEntry{name: "anvil-cli-usage/SKILL.md", data: mismatched},
	)
	wantExtractError(t, data, ErrorKindFrontmatter, "anvil-cli-usage/SKILL.md", "does not match the manifest name")
}

func TestExtract_SymlinkParentInStagingRefused(t *testing.T) {
	// Defense in depth: a pre-existing symlink at a directory path inside
	// the staging tree must never redirect writes outside the root.
	base := t.TempDir()
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(base, "staging")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dest, testName)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Extract(validBundleBytes(t), dest); err == nil {
		t.Fatal("Extract accepted a staging tree containing a symlink parent")
	}
	// Nothing was written through the symlink.
	if _, err := os.Stat(filepath.Join(outside, SkillMarkdownFileName)); !os.IsNotExist(err) {
		t.Fatalf("content escaped through the symlink parent: %v", err)
	}
}

// buildTruncatedEntryArchive builds a bundle whose last content entry
// declares more bytes than it carries (a mid-entry truncation): the tar
// trailer is never written, so the reader hits the stream end inside the
// entry.
func buildTruncatedEntryArchive(t *testing.T, manifest []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := writeTestEntry(tw, tarEntry{name: ManifestFileName, data: manifest}); err != nil {
		t.Fatal(err)
	}
	if err := writeTestEntry(tw, tarEntry{name: "anvil-cli-usage/SKILL.md", data: testSkillMD()}); err != nil {
		t.Fatal(err)
	}
	if err := tw.WriteHeader(&tar.Header{Name: "anvil-cli-usage/trunc.bin", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1000, ModTime: time.Time{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("0123456789")); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close() // error expected: entry incomplete — the stream stops here
	_ = gz.Close()
	return buf.Bytes()
}

// ── Failure cleanliness ──────────────────────────────────────────────

func TestExtract_TruncatedEntryRollsBackPartialFile(t *testing.T) {
	// An entry truncated mid-stream must not leave a partial file behind:
	// rollback removes it (and the rest of the partial tree), so a retry
	// is never blocked by an O_EXCL collision with a leftover file.
	manifest := testManifestJSON(t, "anvil-cli-usage/SKILL.md", "anvil-cli-usage/trunc.bin")
	data := buildTruncatedEntryArchive(t, manifest)
	dest := t.TempDir()
	if _, err := Extract(data, dest); err == nil {
		t.Fatal("Extract accepted a truncated entry")
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("destination not rolled back after a truncated entry; found %d entries: %v", len(entries), entries)
	}
}

func TestExtract_DirEntryBombRejected(t *testing.T) {
	// A hostile archive padded with directory entries is bounded by the
	// total-entry cap (files AND directories) and rejected quickly, not
	// after unbounded MkdirAll/Lstat work.
	manifest := testManifestJSON(t, "anvil-cli-usage/SKILL.md")
	entries := []tarEntry{
		{name: ManifestFileName, data: manifest},
		{name: "anvil-cli-usage/SKILL.md", data: testSkillMD()},
	}
	for i := 0; i < MaxTotalEntries+50; i++ {
		entries = append(entries, tarEntry{name: fmt.Sprintf("anvil-cli-usage/d%05d/", i), typ: tar.TypeDir})
	}
	data := buildArchive(t, entries...)
	start := time.Now()
	wantExtractError(t, data, ErrorKindLimits, "", "entry cap")
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("dir-entry bomb not bounded: extraction took %v", elapsed)
	}
}

func TestExtract_PreexistingDirSurvivesRollback(t *testing.T) {
	// A pre-existing directory is never recorded for rollback: a failed
	// extraction leaves it exactly as it was.
	dest := t.TempDir()
	keep := filepath.Join(dest, "keepme")
	if err := os.MkdirAll(keep, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := testManifestJSON(t, "anvil-cli-usage/SKILL.md", "anvil-cli-usage/ok.md")
	data := buildBundleArchive(t, manifest,
		tarEntry{name: "anvil-cli-usage/SKILL.md", data: testSkillMD()},
		tarEntry{name: "anvil-cli-usage/ok.md", data: []byte("x")},
		tarEntry{name: "../evil", data: []byte("evil")},
	)
	if _, err := Extract(data, dest); err == nil {
		t.Fatal("Extract accepted the hostile archive")
	}
	if fi, err := os.Stat(keep); err != nil {
		t.Fatalf("pre-existing directory removed by rollback: %v", err)
	} else if !fi.IsDir() {
		t.Fatalf("keepme is not a directory")
	}
	// Only the pre-existing directory remains — no partial tree.
	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "keepme" {
		t.Fatalf("destination contents = %v, want only keepme", entries)
	}
}

func TestExtract_PreexistingDirInTreeSurvivesRollback(t *testing.T) {
	// A pre-existing directory ON the extraction path (created by the
	// caller's staging setup) survives rollback; only files this
	// extraction created are removed.
	dest := t.TempDir()
	notes := filepath.Join(dest, "anvil-cli-usage", "notes")
	if err := os.MkdirAll(notes, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := testManifestJSON(t, "anvil-cli-usage/SKILL.md", "anvil-cli-usage/notes/cheat.md")
	data := buildBundleArchive(t, manifest,
		tarEntry{name: "anvil-cli-usage/SKILL.md", data: testSkillMD()},
		tarEntry{name: "anvil-cli-usage/notes/cheat.md", data: []byte("# c\n")},
		tarEntry{name: "../evil", data: []byte("evil")},
	)
	if _, err := Extract(data, dest); err == nil {
		t.Fatal("Extract accepted the hostile archive")
	}
	if fi, err := os.Stat(notes); err != nil {
		t.Fatalf("pre-existing tree directory removed by rollback: %v", err)
	} else if !fi.IsDir() {
		t.Fatalf("notes is not a directory")
	}
	if _, err := os.Stat(filepath.Join(notes, "cheat.md")); !os.IsNotExist(err) {
		t.Fatalf("file created by the extraction was not rolled back: %v", err)
	}
}

func TestExtract_RollbackOnFailure(t *testing.T) {
	// A hostile archive that fails mid-extraction must leave the
	// destination empty — no partial skill tree a caller could misuse.
	manifest := testManifestJSON(t, "anvil-cli-usage/SKILL.md", "anvil-cli-usage/ok.md")
	data := buildBundleArchive(t, manifest,
		tarEntry{name: "anvil-cli-usage/SKILL.md", data: testSkillMD()},
		tarEntry{name: "anvil-cli-usage/ok.md", data: []byte("x")},
		tarEntry{name: "../evil", data: []byte("evil")},
	)
	dest := t.TempDir()
	if _, err := Extract(data, dest); err == nil {
		t.Fatal("Extract accepted the hostile archive")
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("destination not rolled back on failure; found %d entries: %v", len(entries), entries)
	}
}

func TestExtract_PreexistingFileRefused(t *testing.T) {
	// O_EXCL: a pre-existing file at a target path is a hard error, never
	// a silent overwrite.
	dest := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dest, testName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, testName, SkillMarkdownFileName), []byte("pre-existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Extract(validBundleBytes(t), dest)
	var be *BundleError
	if !errors.As(err, &be) {
		t.Fatalf("errors.As(*BundleError) failed: %v", err)
	}
	if be.Kind != ErrorKindStructure || !strings.Contains(be.Message, "O_EXCL") {
		t.Fatalf("want an O_EXCL refusal, got %v", err)
	}
	// The pre-existing file is untouched.
	got, err := os.ReadFile(filepath.Join(dest, testName, SkillMarkdownFileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "pre-existing" {
		t.Fatalf("pre-existing file clobbered: %q", got)
	}
}

func TestExtract_FileModeAtBoundaryKept(t *testing.T) {
	// Files written by the extractor are never executable, whatever the
	// archive claims (covered above), and existing 0644 modes stay 0644.
	ex, dest, _ := extractToTemp(t, validBundleBytes(t))
	for _, rel := range ex.Files {
		fi, err := os.Lstat(filepath.Join(dest, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode()&0o111 != 0 {
			t.Errorf("%s is executable: %v", rel, fi.Mode())
		}
	}
}
