package registry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

// ── Test Helpers ─────────────────────────────────────────────────────

// strPtr returns a pointer to s, for the optional checksum parameter of
// buildBundleArchive.
func strPtr(s string) *string { return &s }

// bundleEntry is one archive entry for buildBundleArchive.
type bundleEntry struct {
	name     string
	typ      byte
	data     []byte
	linkname string
	format   tar.Format
	atime    time.Time
}

// writeTarEntry writes one entry with pinned headers (zeroed ownership
// and timestamps), mirroring the production writer. A non-zero format
// forces the tar encoding (used to build PAX/GNU extended-header
// bundles for rejection tests); a non-zero atime gives the PAX writer a
// record to emit.
func writeTarEntry(tw *tar.Writer, e bundleEntry) error {
	typ := e.typ
	if typ == 0 {
		typ = tar.TypeReg
	}
	hdr := &tar.Header{
		Name:       e.name,
		Typeflag:   typ,
		Linkname:   e.linkname,
		Mode:       0644,
		Size:       int64(len(e.data)),
		ModTime:    time.Time{},
		AccessTime: e.atime,
		Format:     e.format,
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

// buildBundleArchive builds a gzip-compressed tar bundle from the given
// entries, followed by a bundle.sha256 entry when checksum is non-nil.
// checksum == "" computes and writes the correct stream hash (a valid
// bundle); any other value is written verbatim (tampering tests); nil
// omits the checksum entry entirely. The checksum scope mirrors
// CreateBundle: the checksum entry's tar header is hashed, its data is
// not.
func buildBundleArchive(t *testing.T, checksum *string, entries ...bundleEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	sw := &switchableHashWriter{w: gz, h: sha256.New(), on: true}
	tw := tar.NewWriter(sw)
	for _, e := range entries {
		if err := writeTarEntry(tw, e); err != nil {
			t.Fatalf("write entry %q: %v", e.name, err)
		}
	}
	if checksum != nil {
		if err := tw.WriteHeader(&tar.Header{
			Name:     BundleChecksumFileName,
			Mode:     0644,
			Size:     int64(MaxBundleChecksumSize),
			Typeflag: tar.TypeReg,
			ModTime:  time.Time{},
		}); err != nil {
			t.Fatalf("write checksum header: %v", err)
		}
		value := *checksum
		if value == "" {
			value = hex.EncodeToString(sw.h.Sum(nil))
		}
		// The checksum entry is exactly MaxBundleChecksumSize bytes; a
		// shorter value is padded so the archive itself stays well-formed
		// (the read side rejects the value shape).
		data := make([]byte, MaxBundleChecksumSize)
		copy(data, value)
		for i := len(value); i < MaxBundleChecksumSize; i++ {
			data[i] = 'x'
		}
		sw.on = false
		if _, err := tw.Write(data); err != nil {
			t.Fatalf("write checksum value: %v", err)
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

// rawUSTARHeader builds a 512-byte ustar header block for a regular file
// with the given name and size, with a valid header checksum — used to
// craft archives whose entries claim sizes beyond practical write sizes.
func rawUSTARHeader(name string, size int64) []byte {
	block := make([]byte, 512)
	copy(block[0:], name)                         // name (100)
	copy(block[100:], "0000644\x00")              // mode (8)
	copy(block[108:], "0000000\x00")              // uid (8)
	copy(block[116:], "0000000\x00")              // gid (8)
	copy(block[124:], fmt.Sprintf("%011o", size)) // size (12)
	copy(block[136:], "00000000000\x00")          // mtime (12)
	copy(block[148:], "        ")                 // checksum placeholder (8)
	block[156] = '0'                              // typeflag: regular
	copy(block[257:], "ustar\x00")                // magic
	copy(block[263:], "00")                       // version
	var sum int
	for _, b := range block {
		sum += int(b)
	}
	copy(block[148:], fmt.Sprintf("%06o\x00 ", sum))
	return block
}

// testBundleMaterial returns release content and the JSON document of a
// fully attested release for that content (testRelease helper,
// trust_test.go).
func testBundleMaterial(t *testing.T, content []byte, pub ed25519.PublicKey, priv ed25519.PrivateKey) ([]byte, []byte) {
	t.Helper()
	md := testRelease(t, content, pub, priv)
	raw, err := json.Marshal(md)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	return content, raw
}

// validBundle builds a complete, valid bundle for the standard release.
func validBundle(t *testing.T, pub ed25519.PublicKey, priv ed25519.PrivateKey) []byte {
	t.Helper()
	content, metadata := testBundleMaterial(t, testContent(), pub, priv)
	return buildBundleArchive(t, strPtr(""),
		bundleEntry{name: BundleContentFileName, data: content},
		bundleEntry{name: BundleMetadataFileName, data: metadata})
}

// metadataJSONWithout re-marshals a metadata JSON document after fn
// mutates the decoded object — for tampering tests.
func metadataJSONWithout(t *testing.T, raw []byte, fn func(map[string]any)) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	fn(doc)
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("re-marshal metadata: %v", err)
	}
	return out
}

// assertBundleError asserts err is a *BundleError of the given kind
// whose message contains the given substrings, and returns it.
func assertBundleError(t *testing.T, err error, kind BundleErrorKind, substrings ...string) *BundleError {
	t.Helper()
	if err == nil {
		t.Fatal("err = nil, want a *BundleError")
	}
	var be *BundleError
	if !errors.As(err, &be) {
		t.Fatalf("err = %v, want a *BundleError", err)
	}
	if be.Kind != kind {
		t.Errorf("Kind = %q, want %q (err: %v)", be.Kind, kind, err)
	}
	for _, s := range substrings {
		if !strings.Contains(be.Error(), s) {
			t.Errorf("error %q does not contain %q", be.Error(), s)
		}
	}
	return be
}

// ── Positive: Complete Bundle ────────────────────────────────────────

// TestCreateOpenBundleRoundTrip asserts a bundle produced by CreateBundle
// opens cleanly and yields exactly the packed content, the parsed
// metadata (the same document), no warnings, and a Bundle whose Verify
// passes on every dimension with the operator anchors — the end-to-end
// consumption path of TS-014-05-01.
func TestCreateOpenBundleRoundTrip(t *testing.T) {
	content := testContent()
	pub, priv := testEd25519Keypair(t)
	_, metadataRaw := testBundleMaterial(t, content, pub, priv)
	expected := testRelease(t, content, pub, priv)

	bundle, err := CreateBundle(content, metadataRaw)
	if err != nil {
		t.Fatalf("create bundle: %v", err)
	}

	got, err := OpenBundle(bundle)
	if err != nil {
		t.Fatalf("open bundle: %v", err)
	}

	if !bytes.Equal(got.Content, content) {
		t.Errorf("Content = %q, want the packed content", got.Content)
	}
	if !jsonEqual(t, got.Metadata, expected) {
		t.Errorf("Metadata = %+v, want the packed metadata document", got.Metadata)
	}
	if len(got.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none for a published release", got.Warnings)
	}

	result := got.Verify(testAnchors(t, pub))
	if !result.Valid {
		t.Errorf("Verify = invalid, want valid; errors: %v", result.Errors)
	}
	if !result.IntegrityVerified || !result.AttestationVerified || !result.AnchorMatched {
		t.Errorf("Verify = %+v, want all three checks verified", result)
	}
}

// jsonEqual compares two Metadata values through their JSON encoding:
// the parsed document must re-encode to the same document.
func jsonEqual(t *testing.T, got, want Metadata) bool {
	t.Helper()
	g, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal got: %v", err)
	}
	w, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal want: %v", err)
	}
	return bytes.Equal(g, w)
}

// TestCreateBundleDeterministic asserts CreateBundle is byte-deterministic
// for equal inputs: the same content and metadata always produce the
// identical bundle bytes (pinned headers, no timestamps), so bundles are
// reproducible and comparable.
func TestCreateBundleDeterministic(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	content, metadata := testBundleMaterial(t, testContent(), pub, priv)

	b1, err := CreateBundle(content, metadata)
	if err != nil {
		t.Fatalf("create bundle 1: %v", err)
	}
	b2, err := CreateBundle(content, metadata)
	if err != nil {
		t.Fatalf("create bundle 2: %v", err)
	}
	if !bytes.Equal(b1, b2) {
		t.Error("CreateBundle output differs across runs for equal inputs — bundles must be deterministic")
	}
}

// TestCreateBundleRejectsEmptyMetadata asserts the packer refuses to
// produce a bundle without the release's metadata document — such a
// bundle would be rejected at consumption (ADR-022 §3; ADR-023 §3).
func TestCreateBundleRejectsEmptyMetadata(t *testing.T) {
	_, err := CreateBundle(testContent(), nil)
	assertBundleError(t, err, BundleErrorKindMetadata, "must not be empty")

	if _, err := CreateBundle(testContent(), []byte{}); err == nil {
		t.Error("CreateBundle with empty metadata succeeded, want an error")
	}
}

// TestOpenBundleEmptyContentRoundTrip asserts an empty content entry is
// allowed: the metadata's digests decide whether the (empty) release
// verifies — the bundle layer does not second-guess the declared content.
func TestOpenBundleEmptyContentRoundTrip(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	content, metadata := testBundleMaterial(t, []byte{}, pub, priv)
	if len(content) != 0 {
		t.Fatal("test fixture must use empty content")
	}

	bundle, err := CreateBundle(content, metadata)
	if err != nil {
		t.Fatalf("create bundle: %v", err)
	}
	got, err := OpenBundle(bundle)
	if err != nil {
		t.Fatalf("open bundle: %v", err)
	}
	if len(got.Content) != 0 {
		t.Errorf("Content = %q, want empty", got.Content)
	}
	if !got.Verify(testAnchors(t, pub)).Valid {
		t.Error("Verify = invalid for the empty-content release, want valid")
	}
}

// ── Structural Failures ──────────────────────────────────────────────

// TestOpenBundleRejectsNotABundle asserts inputs that are not bundle
// archives are rejected with an actionable structure error.
func TestOpenBundleRejectsNotABundle(t *testing.T) {
	t.Run("random-bytes", func(t *testing.T) {
		_, err := OpenBundle([]byte("this is not a gzip archive at all"))
		assertBundleError(t, err, BundleErrorKindStructure, "not a gzip-compressed tar archive")
	})

	t.Run("empty", func(t *testing.T) {
		_, err := OpenBundle(nil)
		assertBundleError(t, err, BundleErrorKindStructure)
	})

	t.Run("gzip-not-tar", func(t *testing.T) {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		if _, err := gz.Write([]byte("compressed but not a tar stream")); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := gz.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		_, err := OpenBundle(buf.Bytes())
		assertBundleError(t, err, BundleErrorKindStructure, "corrupt or truncated")
	})
}

// TestOpenBundleRejectsTruncatedArchive asserts a bundle cut short is
// rejected: truncation is a corrupt/partial bundle, not a valid one.
func TestOpenBundleRejectsTruncatedArchive(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	bundle := validBundle(t, pub, priv)

	for _, cut := range []int{len(bundle) / 2, len(bundle) - 10} {
		_, err := OpenBundle(bundle[:cut])
		assertBundleError(t, err, BundleErrorKindStructure, "corrupt")
	}
}

// TestOpenBundleRejectsTrailingData asserts the byte-pinned layout:
// trailing input of ANY length after the bundle's gzip stream — one
// byte, many zero bytes, or garbage — is rejected (security F2: the
// bundle ends exactly at its end-of-archive markers; there is no room
// for trailing bytes).
func TestOpenBundleRejectsTrailingData(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	bundle := validBundle(t, pub, priv)

	t.Run("three-bytes", func(t *testing.T) {
		_, err := OpenBundle(append(append([]byte{}, bundle...), []byte("abc")...))
		assertBundleError(t, err, BundleErrorKindStructure, "trailing input", "one gzip member")
	})

	t.Run("trailing-zeros", func(t *testing.T) {
		_, err := OpenBundle(append(append([]byte{}, bundle...), make([]byte, 4096)...))
		assertBundleError(t, err, BundleErrorKindStructure, "trailing input")
	})

	t.Run("garbage", func(t *testing.T) {
		_, err := OpenBundle(append(append([]byte{}, bundle...), []byte("trailing garbage")...))
		assertBundleError(t, err, BundleErrorKindStructure, "trailing input")
	})
}

// TestOpenBundleRejectsSecondGzipMember asserts a second valid gzip
// member appended after the bundle is rejected: the bundle is exactly
// one gzip member, and the second member is never decompressed
// (Multistream disabled) — it is rejected by the exact input-consumption
// check (security F1: multi-member gzip no longer accepted).
func TestOpenBundleRejectsSecondGzipMember(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	bundle := validBundle(t, pub, priv)

	var second bytes.Buffer
	gz := gzip.NewWriter(&second)
	if _, err := gz.Write([]byte("a second gzip member that must not be accepted")); err != nil {
		t.Fatalf("write second member: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close second member: %v", err)
	}

	_, err := OpenBundle(append(append([]byte{}, bundle...), second.Bytes()...))
	assertBundleError(t, err, BundleErrorKindStructure, "trailing input", "one gzip member")
}

// TestOpenBundleRejectsInStreamTrailingData asserts decompressed bytes
// after the tar end-of-archive markers INSIDE the gzip member are
// rejected by the bounded drain: small trailing payloads are rejected as
// trailing data, and a payload beyond the drain budget is rejected
// without unbounded decompression work (security F1).
func TestOpenBundleRejectsInStreamTrailingData(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	content, metadata := testBundleMaterial(t, testContent(), pub, priv)

	// buildWithTrailer writes a valid bundle tar stream and then appends
	// extra decompressed bytes after the end-of-archive markers, inside
	// the same gzip member.
	buildWithTrailer := func(extra []byte) []byte {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		sw := &switchableHashWriter{w: gz, h: sha256.New(), on: true}
		tw := tar.NewWriter(sw)
		if err := writeTarEntry(tw, bundleEntry{name: BundleContentFileName, data: content}); err != nil {
			t.Fatalf("write content: %v", err)
		}
		if err := writeTarEntry(tw, bundleEntry{name: BundleMetadataFileName, data: metadata}); err != nil {
			t.Fatalf("write metadata: %v", err)
		}
		if err := tw.WriteHeader(&tar.Header{
			Name:     BundleChecksumFileName,
			Mode:     0644,
			Size:     int64(MaxBundleChecksumSize),
			Typeflag: tar.TypeReg,
			ModTime:  time.Time{},
		}); err != nil {
			t.Fatalf("write checksum header: %v", err)
		}
		checksum := hex.EncodeToString(sw.h.Sum(nil))
		sw.on = false
		if _, err := tw.Write([]byte(checksum)); err != nil {
			t.Fatalf("write checksum: %v", err)
		}
		if err := tw.Close(); err != nil { // writes the end-of-archive markers
			t.Fatalf("close tar: %v", err)
		}
		if _, err := gz.Write(extra); err != nil { // trailing decompressed bytes
			t.Fatalf("write trailer: %v", err)
		}
		if err := gz.Close(); err != nil {
			t.Fatalf("close gzip: %v", err)
		}
		return buf.Bytes()
	}

	t.Run("small-trailing-payload", func(t *testing.T) {
		_, err := OpenBundle(buildWithTrailer([]byte("trailing decompressed bytes")))
		assertBundleError(t, err, BundleErrorKindStructure, "trailing data", "end-of-archive")
	})

	t.Run("beyond-drain-budget", func(t *testing.T) {
		// 2 MiB of trailing zeros: more than the 1 MiB drain budget. The
		// drain decompresses at most budget+1 bytes and rejects — no
		// unbounded decompression work.
		_, err := OpenBundle(buildWithTrailer(make([]byte, 2*drainBudget)))
		assertBundleError(t, err, BundleErrorKindStructure, "trailing data", "drain budget")
	})
}

// TestOpenBundleRejectsGzipBombTrailingMember asserts a trailing gzip
// member that decompresses to a huge payload (a decompression bomb) is
// rejected WITHOUT decompressing it: Multistream is disabled, so the
// second member is never inflated, and the exact input-consumption check
// rejects it — the test would otherwise take seconds of CPU time
// (security F1: bounded decompression work).
func TestOpenBundleRejectsGzipBombTrailingMember(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	bundle := validBundle(t, pub, priv)

	var bomb bytes.Buffer
	gz := gzip.NewWriter(&bomb)
	chunk := make([]byte, 1<<20) // zeros compress to almost nothing
	for i := 0; i < 64; i++ {    // 64 MiB decompressed payload
		if _, err := gz.Write(chunk); err != nil {
			t.Fatalf("write bomb: %v", err)
		}
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close bomb: %v", err)
	}

	_, err := OpenBundle(append(append([]byte{}, bundle...), bomb.Bytes()...))
	assertBundleError(t, err, BundleErrorKindStructure, "trailing input", "one gzip member")
}

// TestOpenBundleRejectsMissingEntries asserts a bundle missing any of its
// three entries is rejected with an actionable structure error naming the
// pinned layout (PM decision: verification material required — a bundle
// without the metadata document or its checksum is rejected).
func TestOpenBundleRejectsMissingEntries(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	content, metadata := testBundleMaterial(t, testContent(), pub, priv)

	t.Run("no-checksum", func(t *testing.T) {
		raw := buildBundleArchive(t, nil,
			bundleEntry{name: BundleContentFileName, data: content},
			bundleEntry{name: BundleMetadataFileName, data: metadata})
		_, err := OpenBundle(raw)
		assertBundleError(t, err, BundleErrorKindStructure, "corrupt or truncated")
	})

	t.Run("no-metadata", func(t *testing.T) {
		raw := buildBundleArchive(t, strPtr(""),
			bundleEntry{name: BundleContentFileName, data: content},
			bundleEntry{name: BundleChecksumFileName, data: []byte(hex.EncodeToString(make([]byte, 32)))})
		_, err := OpenBundle(raw)
		assertBundleError(t, err, BundleErrorKindStructure, BundleMetadataFileName)
	})

	t.Run("no-content", func(t *testing.T) {
		raw := buildBundleArchive(t, strPtr(""),
			bundleEntry{name: BundleMetadataFileName, data: metadata},
			bundleEntry{name: BundleChecksumFileName, data: []byte(hex.EncodeToString(make([]byte, 32)))})
		_, err := OpenBundle(raw)
		assertBundleError(t, err, BundleErrorKindStructure, BundleContentFileName)
	})
}

// TestOpenBundleRejectsExtraEntry asserts an archive carrying more than
// the pinned three entries is rejected: the layout is exact, not
// extensible. The extra entry is placed after the checksum entry, so the
// rejection names the extra entry explicitly.
func TestOpenBundleRejectsExtraEntry(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	content, metadata := testBundleMaterial(t, testContent(), pub, priv)

	raw := buildBundleArchive(t, nil,
		bundleEntry{name: BundleContentFileName, data: content},
		bundleEntry{name: BundleMetadataFileName, data: metadata},
		bundleEntry{name: BundleChecksumFileName, data: []byte(strings.Repeat("0", MaxBundleChecksumSize))},
		bundleEntry{name: "sneaky", data: []byte("extra")})
	_, err := OpenBundle(raw)
	assertBundleError(t, err, BundleErrorKindStructure, "unexpected entry", "sneaky")
}

// TestOpenBundleRejectsWrongOrder asserts the entry order is pinned:
// metadata first is a structural violation even when every entry is
// present.
func TestOpenBundleRejectsWrongOrder(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	content, metadata := testBundleMaterial(t, testContent(), pub, priv)

	raw := buildBundleArchive(t, strPtr(""),
		bundleEntry{name: BundleMetadataFileName, data: metadata},
		bundleEntry{name: BundleContentFileName, data: content})
	_, err := OpenBundle(raw)
	assertBundleError(t, err, BundleErrorKindStructure, "not the expected bundle entry", "position 1")
}

// TestOpenBundleRejectsNonRegularEntries asserts directory and link
// entries are structural violations: the bundle carries exactly three
// regular files.
func TestOpenBundleRejectsNonRegularEntries(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	content, metadata := testBundleMaterial(t, testContent(), pub, priv)

	t.Run("directory", func(t *testing.T) {
		raw := buildBundleArchive(t, strPtr(""),
			bundleEntry{name: BundleContentFileName, typ: tar.TypeDir},
			bundleEntry{name: BundleMetadataFileName, data: metadata})
		_, err := OpenBundle(raw)
		assertBundleError(t, err, BundleErrorKindStructure, "not the expected bundle entry")
	})

	t.Run("symlink", func(t *testing.T) {
		raw := buildBundleArchive(t, strPtr(""),
			bundleEntry{name: BundleContentFileName, typ: tar.TypeSymlink, linkname: "/etc/passwd"},
			bundleEntry{name: BundleMetadataFileName, data: metadata})
		_, err := OpenBundle(raw)
		assertBundleError(t, err, BundleErrorKindStructure, "not the expected bundle entry")
	})

	t.Run("hardlink", func(t *testing.T) {
		raw := buildBundleArchive(t, strPtr(""),
			bundleEntry{name: BundleContentFileName, typ: tar.TypeLink, linkname: "content"},
			bundleEntry{name: BundleMetadataFileName, data: metadata})
		_, err := OpenBundle(raw)
		assertBundleError(t, err, BundleErrorKindStructure, "not the expected bundle entry")
	})

	t.Run("content-as-symlink-with-valid-checksum", func(t *testing.T) {
		// A symlink entry whose checksum was recomputed by the attacker
		// still fails: the layout check runs before the checksum and is
		// independent of it.
		raw := buildBundleArchive(t, strPtr(""),
			bundleEntry{name: BundleContentFileName, typ: tar.TypeSymlink, linkname: "/etc/passwd"},
			bundleEntry{name: BundleMetadataFileName, data: content})
		_, err := OpenBundle(raw)
		assertBundleError(t, err, BundleErrorKindStructure, "not the expected bundle entry")
	})
}

// TestOpenBundleRejectsExtendedHeaders asserts PAX and GNU extended
// headers are structural violations: the bundle format is pinned to
// plain tar headers, and an extended header could alias entry names or
// smuggle per-entry metadata into the pinned layout (security F4; the
// checksum does not help here — a hostile bundle can recompute it, so
// the extended headers are rejected structurally, before verification).
func TestOpenBundleRejectsExtendedHeaders(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	content, metadata := testBundleMaterial(t, testContent(), pub, priv)

	t.Run("pax-extended-header", func(t *testing.T) {
		// A forced-PAX entry: the writer emits a TypeXHeader record
		// carrying the access time before the entry header, which the
		// reader reports as FormatPAX.
		raw := buildBundleArchive(t, strPtr(""),
			bundleEntry{name: BundleContentFileName, data: content, format: tar.FormatPAX, atime: time.Unix(1700000000, 0)},
			bundleEntry{name: BundleMetadataFileName, data: metadata})
		_, err := OpenBundle(raw)
		assertBundleError(t, err, BundleErrorKindStructure, "PAX", "extended header")
	})

	t.Run("gnu-format-header", func(t *testing.T) {
		// A forced-GNU entry: the writer emits a GNU-magic header, which
		// the reader reports as FormatGNU.
		raw := buildBundleArchive(t, strPtr(""),
			bundleEntry{name: BundleContentFileName, data: content, format: tar.FormatGNU},
			bundleEntry{name: BundleMetadataFileName, data: metadata})
		_, err := OpenBundle(raw)
		assertBundleError(t, err, BundleErrorKindStructure, "GNU", "extended header")
	})

	t.Run("pax-on-checksum-entry", func(t *testing.T) {
		// The last entry is covered too: every entry must use plain tar
		// headers.
		raw := buildBundleArchive(t, strPtr(""),
			bundleEntry{name: BundleContentFileName, data: content},
			bundleEntry{name: BundleMetadataFileName, data: metadata, format: tar.FormatPAX, atime: time.Unix(1700000000, 0)})
		_, err := OpenBundle(raw)
		assertBundleError(t, err, BundleErrorKindStructure, "PAX", "extended header")
	})
}

// TestOpenBundleRejectsOversizeMetadata asserts the metadata entry is
// bounded by MaxBundleMetadataSize: an oversized document fails load
// precisely, naming the entry and the cap (the index convention,
// MaxIndexDocumentSize).
func TestOpenBundleRejectsOversizeMetadata(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	content, _ := testBundleMaterial(t, testContent(), pub, priv)

	raw := buildBundleArchive(t, strPtr(""),
		bundleEntry{name: BundleContentFileName, data: content},
		bundleEntry{name: BundleMetadataFileName, data: make([]byte, MaxBundleMetadataSize+1)})
	_, err := OpenBundle(raw)
	assertBundleError(t, err, BundleErrorKindStructure, BundleMetadataFileName, "exceeding the")
}

// TestOpenBundleRejectsOversizeContent asserts the content entry is
// bounded by MaxBundleContentSize and that the size check happens at the
// header, before any payload is read.
func TestOpenBundleRejectsOversizeContent(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(rawUSTARHeader(BundleContentFileName, MaxBundleContentSize+1)); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := gz.Write(make([]byte, 512)); err != nil { // one data block; the reader rejects before reading it
		t.Fatalf("write data: %v", err)
	}
	if _, err := gz.Write(make([]byte, 1024)); err != nil { // end-of-archive markers
		t.Fatalf("write EOA: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	_, err := OpenBundle(buf.Bytes())
	assertBundleError(t, err, BundleErrorKindStructure, BundleContentFileName, "exceeding the")
}

// TestOpenBundleRejectsMalformedChecksumValue asserts the checksum entry
// must be exactly the canonical 64 lowercase hex characters; any other
// shape is a structural violation.
func TestOpenBundleRejectsMalformedChecksumValue(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	content, metadata := testBundleMaterial(t, testContent(), pub, priv)
	entries := []bundleEntry{
		{name: BundleContentFileName, data: content},
		{name: BundleMetadataFileName, data: metadata},
	}

	t.Run("not-hex", func(t *testing.T) {
		raw := buildBundleArchive(t, strPtr("garbage-not-a-checksum"), entries...)
		_, err := OpenBundle(raw)
		assertBundleError(t, err, BundleErrorKindStructure, BundleChecksumFileName, "64 lowercase hex")
	})

	t.Run("short", func(t *testing.T) {
		raw := buildBundleArchive(t, strPtr(hex.EncodeToString(make([]byte, 31))), entries...)
		_, err := OpenBundle(raw)
		assertBundleError(t, err, BundleErrorKindStructure, BundleChecksumFileName, "64 lowercase hex")
	})

	t.Run("uppercase", func(t *testing.T) {
		// The correct checksum rendered in uppercase: the canonical form
		// is lowercase-only, so the shape check must reject it.
		correct := extractChecksum(t, buildBundleArchive(t, strPtr(""), entries...))
		raw := buildBundleArchive(t, strPtr(strings.ToUpper(correct)), entries...)
		_, err := OpenBundle(raw)
		assertBundleError(t, err, BundleErrorKindStructure, BundleChecksumFileName, "64 lowercase hex")
	})
}

// ── Integrity Failures (attribution) ─────────────────────────────────

// TestOpenBundleChecksumMismatch asserts a bundle whose archive bytes do
// not match the embedded checksum is rejected at open with the "bundle
// corrupt" attribution (kind integrity): content modified inside the
// archive by someone who did not re-checksum the bundle is caught here,
// before verification.
func TestOpenBundleChecksumMismatch(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	content, metadata := testBundleMaterial(t, testContent(), pub, priv)

	// The content entry is tampered; the bundle.sha256 entry keeps the
	// ORIGINAL value (the checksum of the untampered stream) — the
	// attacker did not update it.
	original := validBundle(t, pub, priv)
	originalChecksum := extractChecksum(t, original)

	tamperedContent := append(append([]byte{}, content...), '!')
	raw := buildBundleArchive(t, strPtr(originalChecksum),
		bundleEntry{name: BundleContentFileName, data: tamperedContent},
		bundleEntry{name: BundleMetadataFileName, data: metadata})

	_, err := OpenBundle(raw)
	assertBundleError(t, err, BundleErrorKindIntegrity, "bundle checksum mismatch", "bundle is corrupt or was modified")
}

// extractChecksum reads the bundle.sha256 value out of a bundle archive
// (for tampering tests that must keep the original value).
func extractChecksum(t *testing.T, bundle []byte) string {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(bundle))
	if err != nil {
		t.Fatalf("decompress bundle: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err != nil {
			t.Fatalf("read bundle entries: %v", err)
		}
		if hdr.Name == BundleChecksumFileName {
			raw := make([]byte, MaxBundleChecksumSize)
			if _, err := io.ReadFull(tr, raw); err != nil {
				t.Fatalf("read checksum: %v", err)
			}
			return string(raw)
		}
	}
}

// TestOpenBundleTamperedContentAttributable asserts the two failure
// attributions for content modified inside the archive (PM decision: the
// bundle layer must surface the metadata digests so the failure is
// attributable — "bundle corrupt" vs "content digest mismatch"):
//
//   - when the attacker did not re-checksum the bundle, OpenBundle
//     rejects with kind integrity ("bundle corrupt");
//   - when the attacker re-checksummed the bundle, OpenBundle succeeds
//     and the tampering is caught at verification time by the content
//     digest check ("content digest mismatch"), with the digests
//     surfaced on the returned Bundle.
func TestOpenBundleTamperedContentAttributable(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	content, metadata := testBundleMaterial(t, testContent(), pub, priv)
	tamperedContent := append(append([]byte{}, content...), '!')

	t.Run("checksum-not-updated-bundle-corrupt", func(t *testing.T) {
		original := validBundle(t, pub, priv)
		originalChecksum := extractChecksum(t, original)
		raw := buildBundleArchive(t, strPtr(originalChecksum),
			bundleEntry{name: BundleContentFileName, data: tamperedContent},
			bundleEntry{name: BundleMetadataFileName, data: metadata})

		_, err := OpenBundle(raw)
		assertBundleError(t, err, BundleErrorKindIntegrity, "bundle checksum mismatch")
	})

	t.Run("checksum-updated-content-digest-mismatch", func(t *testing.T) {
		// The attacker re-checksums the modified bundle; the archive is
		// internally consistent, so OpenBundle accepts it — and the
		// bundled material must then fail the same content-digest
		// verification an online install runs, with the failure
		// attributable to the content, not the bundle.
		raw := buildBundleArchive(t, strPtr(""),
			bundleEntry{name: BundleContentFileName, data: tamperedContent},
			bundleEntry{name: BundleMetadataFileName, data: metadata})

		got, err := OpenBundle(raw)
		if err != nil {
			t.Fatalf("open re-checksummed bundle: %v", err)
		}
		if bytes.Equal(got.Content, content) {
			t.Fatal("test fixture must carry tampered content")
		}

		// The digests are surfaced on the bundle, so the failure is
		// attributable to the content at verification time.
		if len(got.Metadata.Trust.ContentDigests) == 0 {
			t.Fatal("the bundle must surface the metadata content digests")
		}

		result := got.Verify(testAnchors(t, pub))
		if result.Valid {
			t.Error("Verify = valid for tampered content, want invalid")
		}
		if result.IntegrityVerified {
			t.Error("IntegrityVerified = true for tampered content, want false")
		}
		if !result.AttestationVerified {
			t.Error("AttestationVerified = false — the attestation binds the declared digests and must still verify; want true")
		}
		if !hasMessage(result.Errors, "content digest mismatch") {
			t.Errorf("Verify errors = %v, want a content-digest-mismatch message (the failure must be attributable to the content, not the bundle)", result.Errors)
		}
	})
}

// TestOpenBundleTamperedMetadataAttributable asserts metadata modified
// inside the archive cannot bypass validation at adoption:
//
//   - a re-checksummed bundle whose metadata no longer passes the strict
//     parse is rejected at open (same parse as online);
//   - a re-checksummed bundle whose metadata is still schema-valid but
//     relabeled opens — and then fails attestation at verification time,
//     because the signature binds id and version into the canonical
//     payload (ADR-022 §3; no bypass).
func TestOpenBundleTamperedMetadataAttributable(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	content, metadata := testBundleMaterial(t, testContent(), pub, priv)

	t.Run("schema-invalid-metadata-rejected-at-open", func(t *testing.T) {
		// Remove a required section: the document no longer passes the
		// strict parse.
		badMetadata := metadataJSONWithout(t, metadata, func(doc map[string]any) {
			delete(doc, "distribution")
		})
		raw := buildBundleArchive(t, strPtr(""),
			bundleEntry{name: BundleContentFileName, data: content},
			bundleEntry{name: BundleMetadataFileName, data: badMetadata})
		_, err := OpenBundle(raw)
		assertBundleError(t, err, BundleErrorKindMetadata, "same strict parse")
	})

	t.Run("relabeled-metadata-fails-attestation", func(t *testing.T) {
		// Version relabel: still schema-valid, but the signature binds
		// the version into the canonical payload, so verification must
		// fail — the offline path verifies exactly like the online path.
		relabeled := metadataJSONWithout(t, metadata, func(doc map[string]any) {
			doc["version"] = "9.9.9"
		})
		raw := buildBundleArchive(t, strPtr(""),
			bundleEntry{name: BundleContentFileName, data: content},
			bundleEntry{name: BundleMetadataFileName, data: relabeled})

		got, err := OpenBundle(raw)
		if err != nil {
			t.Fatalf("open relabeled bundle: %v", err)
		}
		result := got.Verify(testAnchors(t, pub))
		if result.Valid {
			t.Error("Verify = valid for a relabeled release, want invalid")
		}
		if result.AttestationVerified {
			t.Error("AttestationVerified = true for a version relabel, want false")
		}
		if !hasMessage(result.Errors, "does not verify") {
			t.Errorf("Verify errors = %v, want an attestation failure message", result.Errors)
		}
	})
}

// ── Metadata Failures ────────────────────────────────────────────────

// TestOpenBundleRejectsMetadataNotJSON asserts a bundled metadata
// document that is not decodable JSON is rejected with the strict-parse
// attribution: the BundleError wraps the document-level ParseError so
// callers can inspect every problem (errors.As).
func TestOpenBundleRejectsMetadataNotJSON(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	content, _ := testBundleMaterial(t, testContent(), pub, priv)

	raw := buildBundleArchive(t, strPtr(""),
		bundleEntry{name: BundleContentFileName, data: content},
		bundleEntry{name: BundleMetadataFileName, data: []byte("this is not JSON")})

	_, err := OpenBundle(raw)
	be := assertBundleError(t, err, BundleErrorKindMetadata, "same strict parse")

	var pe *ParseError
	if !errors.As(be, &pe) {
		t.Errorf("BundleError = %v, want it to wrap a *ParseError (errors.As)", be)
	}
}

// TestOpenBundleRejectsMetadataWithoutTrust asserts a bundled metadata
// document without trust fields is rejected at consumption: verification
// material is required for every bundle, online or offline (ADR-022 §3;
// PM decision: verification material required, no privileged path). The
// document-level parse errors are wrapped for inspection.
func TestOpenBundleRejectsMetadataWithoutTrust(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	content, metadata := testBundleMaterial(t, testContent(), pub, priv)

	t.Run("trust-section-missing", func(t *testing.T) {
		noTrust := metadataJSONWithout(t, metadata, func(doc map[string]any) {
			delete(doc, "trust")
		})
		raw := buildBundleArchive(t, strPtr(""),
			bundleEntry{name: BundleContentFileName, data: content},
			bundleEntry{name: BundleMetadataFileName, data: noTrust})

		be := assertBundleError(t, openBundle(raw), BundleErrorKindMetadata, "same strict parse")
		assertParseErrorNamesTrust(t, be)
	})

	t.Run("trust-present-but-empty", func(t *testing.T) {
		// A document whose trust section is present but empty (null
		// digests, empty attestation) also fails the strict parse — the
		// schema requires contentDigests (at least one), signature, and
		// publicKey, so the bundle is rejected before verification.
		md := testRelease(t, content, pub, priv)
		md.Trust = Trust{}
		rawDoc, err := json.Marshal(md)
		if err != nil {
			t.Fatalf("marshal trustless document: %v", err)
		}
		raw := buildBundleArchive(t, strPtr(""),
			bundleEntry{name: BundleContentFileName, data: content},
			bundleEntry{name: BundleMetadataFileName, data: rawDoc})

		be := assertBundleError(t, openBundle(raw), BundleErrorKindMetadata, "same strict parse")
		assertParseErrorNamesTrust(t, be)
	})
}

// openBundle opens a bundle, returning only the error (for assertBundleError).
func openBundle(raw []byte) error {
	_, err := OpenBundle(raw)
	return err
}

// assertParseErrorNamesTrust asserts the wrapped ParseError reports a
// problem on a trust field.
func assertParseErrorNamesTrust(t *testing.T, be *BundleError) {
	t.Helper()
	var pe *ParseError
	if !errors.As(be, &pe) {
		t.Errorf("BundleError = %v, want it to wrap a *ParseError (errors.As)", be)
		return
	}
	for _, ve := range pe.Errors {
		if strings.Contains(ve.Field, "trust") {
			return
		}
	}
	t.Errorf("ParseError = %v, want a problem naming a trust field", pe.Errors)
}

// ── Verification Contract (no bypass) ────────────────────────────────

// TestBundleVerifyRequiresOperatorAnchors asserts the verification
// contract of the bundled material: the SAME VerifyTrust path as online
// installs, with anchors from the operator — the bundle never carries
// anchors, and verification fails closed without them (PM decision D-07;
// T-011 security note).
func TestBundleVerifyRequiresOperatorAnchors(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	bundle, err := OpenBundle(validBundle(t, pub, priv))
	if err != nil {
		t.Fatalf("open bundle: %v", err)
	}

	t.Run("no-anchors-fails-closed", func(t *testing.T) {
		result := bundle.Verify(nil)
		if result.Valid {
			t.Error("Verify = valid with no anchors, want invalid (no TOFU)")
		}
		if !hasMessage(result.Errors, "no trust anchor configured") {
			t.Errorf("Verify errors = %v, want a no-anchor message", result.Errors)
		}
	})

	t.Run("unknown-publisher-fails", func(t *testing.T) {
		otherPub, _ := testEd25519Keypair(t)
		anchors, err := TrustAnchorsFromKeys(map[string]string{
			"anvil-standard-flutter": base64.StdEncoding.EncodeToString(otherPub),
		})
		if err != nil {
			t.Fatalf("build anchors: %v", err)
		}
		result := bundle.Verify(anchors)
		if result.Valid {
			t.Error("Verify = valid for an unknown publisher, want invalid")
		}
		if !hasMessage(result.Errors, "unknown publisher") {
			t.Errorf("Verify errors = %v, want an unknown-publisher message", result.Errors)
		}
	})

	t.Run("wrong-key-fails", func(t *testing.T) {
		otherPub, _ := testEd25519Keypair(t)
		result := bundle.Verify(testAnchors(t, otherPub))
		if result.Valid {
			t.Error("Verify = valid for a mismatching anchor key, want invalid")
		}
		if !hasMessage(result.Errors, "public key mismatch") {
			t.Errorf("Verify errors = %v, want a key-mismatch message", result.Errors)
		}
	})

	t.Run("operator-anchors-valid", func(t *testing.T) {
		result := bundle.Verify(testAnchors(t, pub))
		if !result.Valid {
			t.Errorf("Verify = invalid with the operator anchors, want valid; errors: %v", result.Errors)
		}
		if result.Publisher != "anvil-standard-laravel" {
			t.Errorf("Publisher = %q, want %q", result.Publisher, "anvil-standard-laravel")
		}
		if len(result.DeclaredDigests) != 1 {
			t.Errorf("DeclaredDigests = %d, want 1 (the digests surfaced from the bundle)", len(result.DeclaredDigests))
		}
	})
}

// TestBundleSurfacesMetadataDigests asserts the returned Bundle surfaces
// the declared content digests, so a verification failure is attributable
// to the content ("content digest mismatch") rather than the bundle.
func TestBundleSurfacesMetadataDigests(t *testing.T) {
	content := testContent()
	pub, priv := testEd25519Keypair(t)
	md := testRelease(t, content, pub, priv, DigestEncodingBase16, DigestEncodingBase32, DigestEncodingBase64)
	rawDoc, err := json.Marshal(md)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	bundle, err := CreateBundle(content, rawDoc)
	if err != nil {
		t.Fatalf("create bundle: %v", err)
	}
	got, err := OpenBundle(bundle)
	if err != nil {
		t.Fatalf("open bundle: %v", err)
	}

	if len(got.Metadata.Trust.ContentDigests) != 3 {
		t.Fatalf("ContentDigests = %d, want 3", len(got.Metadata.Trust.ContentDigests))
	}
	for i, d := range got.Metadata.Trust.ContentDigests {
		if d.Digest != md.Trust.ContentDigests[i].Digest {
			t.Errorf("digest [%d] = %q, want %q", i, d.Digest, md.Trust.ContentDigests[i].Digest)
		}
	}
	result := got.Verify(testAnchors(t, pub))
	if !result.Valid {
		t.Errorf("Verify = invalid, want valid; errors: %v", result.Errors)
	}
}
