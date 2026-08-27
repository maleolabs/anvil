// Registry bundle format for offline/bundled installs (TS-014-05-01;
// ADR-022 §3; ADR-023 §3).
//
// A bundle is the offline distribution unit of one standard release: the
// standard release content and its verification material travel together
// in a single archive, so an offline install consumes exactly what an
// online install fetches — content plus the registry metadata document
// with its trust fields — and validates it with the same machinery, with
// no network access at any point (ADR-023 §3: offline/bundled installs
// follow the same validation path; ADR-022 §3: every install requires
// verification material).
//
// Format. A bundle is a single gzip-compressed tar archive (tar.gz) — the
// primary archive format of the Core's artifact packaging
// (internal/artifact/packaging.go: DefaultFormats leads with "tar.gz") —
// with exactly three regular-file entries in a pinned, deterministic
// order:
//
//  1. "content"       — the release content bytes: exactly the bytes the
//     metadata document's trust.contentDigests are
//     computed over (the content an online install
//     resolves from distribution.location).
//  2. "metadata.json" — the registry metadata document for the release
//     (registry-metadata.schema.json), exactly the
//     document an online install fetches. It carries
//     the verification material: trust.contentDigests
//     and trust.attestation (signature + publicKey).
//  3. "bundle.sha256" — the bundle checksum: the lowercase-hex SHA-256
//     of the uncompressed tar stream bytes that precede
//     the entry's data — covering the content and
//     metadata entries (headers, data, padding) and the
//     checksum entry's own tar header, excluding the
//     checksum value itself and the end-of-archive
//     markers. It is a corruption detector for
//     transport/storage damage and the basis for
//     failure attribution ("bundle corrupt" vs
//     "content digest mismatch") — not a security
//     boundary: it is recomputable by anyone who can
//     modify the bundle, and the security boundary is
//     VerifyTrust with operator-supplied anchors.
//
// Exactly these three regular files, exactly in this order, encoded with
// plain tar headers (no PAX or GNU extended headers); any other entry,
// any other order, any directory, link, or device entry, any extended
// header, or a checksum entry that is not last is a structural violation
// and the bundle is rejected (the format is pinned, not extensible). The
// bundle is a single gzip member: trailing data of any length — bytes
// after the end-of-archive markers inside the stream, or any input after
// the gzip member (including a second gzip member) — is rejected. The
// reference layout is documented in
// docs/operations/registry-bundle-format.md.
//
// Consumption contract (no bypass; ADR-022 §3). OpenBundle is the only
// consumption entry point and it always, for every bundle:
//
//  1. validates the archive structure (pinned layout, no extended
//     headers, single gzip member) and the bundle checksum — the
//     checksum is checked before the bounded remainder drain, so a
//     corrupt bundle is rejected without further decompression work —
//     and rejects trailing data of any length within a fixed work
//     budget;
//  2. runs the strict metadata parse (Parse) on the bundled document —
//     the same parse used online, not a structural decode;
//  3. through that parse, requires verification material: the schema
//     makes trust.contentDigests (at least one), trust.attestation.
//     signature, and trust.attestation.publicKey required, so a document
//     without verification material cannot pass the parse and the bundle
//     is rejected with the document-level problems (ADR-022 §3:
//     verification material required, no privileged path).
//
// It then hands the caller the content bytes and the parsed metadata.
// Trust verification of the material runs through the same VerifyTrust
// engine used online (Bundle.Verify) — there is no offline-specific
// verification path and no privileged path; the trust anchors come from
// the operator on the host side, never from inside the bundle
// (anchors.go; PM decision D-07; T-011 security note: anchors are never
// bundled inside the release being verified).
//
// Consumption is purely local: the bundle is read from bytes already in
// memory; no network access is used at any point.
//
// Reference: TS-014-05-01, ADR-022 §3, ADR-023 §3,
// docs/operations/registry-bundle-format.md
package registry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"time"
)

// Bundle archive layout constants (TS-014-05-01). The layout is pinned:
// exactly three regular files with exactly these names, in exactly this
// order.
const (
	// BundleContentFileName is the archive entry carrying the release
	// content: exactly the bytes the metadata document's
	// trust.contentDigests are computed over.
	BundleContentFileName = "content"

	// BundleMetadataFileName is the archive entry carrying the registry
	// metadata document for the release (registry-metadata.schema.json),
	// exactly the document an online install would fetch.
	BundleMetadataFileName = "metadata.json"

	// BundleChecksumFileName is the archive entry carrying the bundle
	// checksum (lowercase-hex SHA-256 of the preceding bundle stream).
	// It must be the last entry.
	BundleChecksumFileName = "bundle.sha256"

	// MaxBundleMetadataSize caps the metadata entry at 1 MiB, mirroring
	// MaxIndexDocumentSize: registry metadata documents are small
	// (kilobytes at most), and an entry beyond the cap is a broken or
	// hostile artifact, not a valid bundle.
	MaxBundleMetadataSize = 1 << 20

	// MaxBundleContentSize caps the content entry at 1 GiB: release
	// content is a standard's distribution archive, which can reach tens
	// of megabytes; the cap keeps extraction memory-bounded while
	// allowing any realistic release payload.
	MaxBundleContentSize = 1 << 30

	// MaxBundleChecksumSize is the exact size of the checksum entry:
	// the lowercase hex encoding of a SHA-256 digest, exactly 64
	// characters, no trailing newline.
	MaxBundleChecksumSize = 64
)

// BundleErrorKind classifies a rejected bundle into the failure classes
// of TS-014-05-01: structure (the archive is not a valid bundle layout),
// integrity (the archive bytes do not match the bundle checksum), and
// metadata (the bundled metadata document is missing, unparseable, or
// carries no verification material).
type BundleErrorKind string

const (
	// BundleErrorKindStructure marks a bundle whose archive is not a
	// valid bundle: not a tar.gz, truncated, corrupt, or violating the
	// pinned layout (wrong or extra entries, wrong order, non-regular
	// entries, entries beyond the size caps, malformed checksum entry).
	BundleErrorKindStructure BundleErrorKind = "structure"

	// BundleErrorKindIntegrity marks a bundle whose archive bytes do not
	// match the bundle.sha256 value embedded in the bundle: the bundle
	// is corrupt or was modified after creation ("bundle corrupt").
	BundleErrorKindIntegrity BundleErrorKind = "integrity"

	// BundleErrorKindMetadata marks a bundle whose metadata document is
	// missing, fails the strict metadata parse, or lacks the trust
	// fields (verification material) the schema requires (ADR-022 §3:
	// verification material is required; a bundle without it is
	// rejected).
	BundleErrorKindMetadata BundleErrorKind = "metadata"
)

// BundleError reports that a bundle was rejected at consumption. The
// Kind classifies the failure (BundleErrorKind), Field names the archive
// entry or component the failure concerns (the entry names for entry
// problems, "bundle" for archive-level problems), and Message is
// human-readable and actionable: what failed and how to resolve it.
//
// When the bundled metadata document fails the strict parse, Cause wraps
// the *ParseError so callers can inspect the document-level problems
// with errors.As.
type BundleError struct {
	// Kind classifies the failure as structure, integrity, or metadata.
	Kind BundleErrorKind

	// Field is the archive entry or component the failure concerns.
	Field string

	// Message is a human-readable, actionable explanation.
	Message string

	// Cause is the underlying error, when one exists (for example the
	// *ParseError of the bundled metadata document).
	Cause error
}

// Error implements the error interface.
func (e *BundleError) Error() string {
	field := e.Field
	if field == "" {
		field = "bundle"
	}
	if e.Cause != nil {
		return fmt.Sprintf("bundle %s: %s: %v (kind %s)", field, e.Message, e.Cause, e.Kind)
	}
	return fmt.Sprintf("bundle %s: %s (kind %s)", field, e.Message, e.Kind)
}

// Unwrap exposes the underlying error for errors.As matching.
func (e *BundleError) Unwrap() error {
	return e.Cause
}

// bundleError builds a BundleError without a cause.
func bundleError(kind BundleErrorKind, field, message string) *BundleError {
	return &BundleError{Kind: kind, Field: field, Message: message}
}

// bundleErrorCause builds a BundleError wrapping an underlying error.
func bundleErrorCause(kind BundleErrorKind, field, message string, cause error) *BundleError {
	return &BundleError{Kind: kind, Field: field, Message: message, Cause: cause}
}

// Bundle is the validated, extracted material of one bundle archive
// (TS-014-05-01): the release content and the parsed registry metadata
// document that carries the verification material, ready for the same
// trust validation used online.
//
// Verification contract: the bundled material must be verified with the
// same VerifyTrust path used for online installs, with the trust anchors
// supplied by the operator (host side) — the bundle never carries trust
// anchors, and there is no bundle-specific verification path (ADR-022
// §3; PM decision D-07). Bundle.Verify runs exactly that path; the
// offline install flow (TS-014-05-02) calls it and aborts the install on
// a result with Valid == false.
type Bundle struct {
	// Content is the release content bytes, exactly as carried in the
	// bundle: the bytes the metadata digests are computed over. It is
	// ready to hand to VerifyTrust alongside the metadata.
	Content []byte

	// Metadata is the bundled registry metadata document after the
	// strict parse (Parse) — the same parse used online. Its trust
	// fields (Trust.ContentDigests, Trust.Attestation) are the
	// verification material the content must be validated against.
	Metadata Metadata

	// Warnings lists the advisory notes of the strict parse (for
	// example a deprecated release without an announced removal date,
	// PM decision D-03).
	Warnings []Warning
}

// Verify performs adoption-time trust validation of the bundled material:
// integrity of the content against every digest declared in the bundled
// metadata, the publisher attestation, and the out-of-band trust anchor
// match. It is the exact same engine online installs use (VerifyTrust);
// anchors must come from the operator — nil or empty means no anchors
// configured and verification fails closed (no TOFU, no privileged path;
// PM decision D-07).
func (b *Bundle) Verify(anchors *TrustAnchors) TrustResult {
	return VerifyTrust(b.Metadata, b.Content, anchors)
}

// CreateBundle packs release content and its metadata document into a
// bundle archive (TS-014-05-01): a deterministic tar.gz carrying exactly
// content, metadata.json, and bundle.sha256, in that order.
//
// CreateBundle exists for tests and for future publishing tooling; it is
// a packer, not a validation surface — the metadata document is packed as
// given, and consumption-side validation (OpenBundle: structure, checksum,
// strict parse) is the only gate that applies at adoption. Publishing is
// out of scope for this work item; no path through this function bypasses
// consumption-side validation because OpenBundle is the only consumption
// entry point.
//
// The metadata document must not be empty (a bundle without the release's
// metadata document is rejected at consumption, ADR-022 §3). Content may
// be empty: the digests declared in the metadata decide whether an empty
// release verifies. Both inputs are bounded by the same caps enforced at
// consumption (MaxBundleMetadataSize, MaxBundleContentSize), so a bundle
// produced here is never rejected for size at consumption.
//
// The output is byte-deterministic for equal inputs: the tar headers are
// pinned (zeroed ownership fields, zeroed timestamps) and the gzip stream
// is produced without timestamps, so the same content and metadata always
// produce the identical bundle bytes.
func CreateBundle(content []byte, metadata []byte) ([]byte, error) {
	if len(metadata) == 0 {
		return nil, bundleError(BundleErrorKindMetadata, BundleMetadataFileName,
			"the metadata document must not be empty — a bundle without the release's metadata document is rejected at consumption (ADR-022 §3; ADR-023 §3)")
	}
	if len(metadata) > MaxBundleMetadataSize {
		return nil, bundleError(BundleErrorKindStructure, BundleMetadataFileName,
			fmt.Sprintf("the metadata document is %d bytes, exceeding the %d-byte cap", len(metadata), MaxBundleMetadataSize))
	}
	if len(content) > MaxBundleContentSize {
		return nil, bundleError(BundleErrorKindStructure, BundleContentFileName,
			fmt.Sprintf("the content is %d bytes, exceeding the %d-byte cap", len(content), MaxBundleContentSize))
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	sw := &switchableHashWriter{w: gz, h: sha256.New(), on: true}
	tw := tar.NewWriter(sw)

	if err := writeBundleEntry(tw, BundleContentFileName, content); err != nil {
		return nil, bundleErrorCause(BundleErrorKindStructure, BundleContentFileName, "write content entry", err)
	}
	if err := writeBundleEntry(tw, BundleMetadataFileName, metadata); err != nil {
		return nil, bundleErrorCause(BundleErrorKindStructure, BundleMetadataFileName, "write metadata entry", err)
	}

	// The checksum entry: its tar header completes the hashed scope, its
	// data carries the value and is excluded from it (see the format
	// contract above) — the header is written while hashing is on, then
	// hashing is switched off for the value.
	if err := tw.WriteHeader(&tar.Header{
		Name:     BundleChecksumFileName,
		Mode:     0644,
		Size:     int64(MaxBundleChecksumSize),
		Typeflag: tar.TypeReg,
		ModTime:  time.Time{},
	}); err != nil {
		return nil, bundleErrorCause(BundleErrorKindStructure, BundleChecksumFileName, "write checksum header", err)
	}
	checksum := hex.EncodeToString(sw.h.Sum(nil))
	sw.on = false
	if _, err := tw.Write([]byte(checksum)); err != nil {
		return nil, bundleErrorCause(BundleErrorKindStructure, BundleChecksumFileName, "write checksum value", err)
	}

	if err := tw.Close(); err != nil {
		return nil, bundleErrorCause(BundleErrorKindStructure, "bundle", "finalize archive", err)
	}
	if err := gz.Close(); err != nil {
		return nil, bundleErrorCause(BundleErrorKindStructure, "bundle", "finalize compression", err)
	}
	return buf.Bytes(), nil
}

// writeBundleEntry writes one regular-file entry with pinned header
// fields (zeroed ownership and timestamps), so bundles are deterministic.
func writeBundleEntry(tw *tar.Writer, name string, data []byte) error {
	if err := tw.WriteHeader(&tar.Header{
		Name:     name,
		Mode:     0644,
		Size:     int64(len(data)),
		Typeflag: tar.TypeReg,
		ModTime:  time.Time{},
	}); err != nil {
		return err
	}
	if len(data) > 0 {
		if _, err := tw.Write(data); err != nil {
			return err
		}
	}
	return nil
}

// OpenBundle reads and validates a bundle archive from bytes and extracts
// the bundled material (TS-014-05-01). It is the only consumption entry
// point for bundles; every bundle passes the same validation, in order:
//
//  1. the input must be a single-member gzip-compressed tar archive whose
//     stream is intact: truncation, a corrupt stream, trailing data of
//     any length (decompressed bytes after the end-of-archive markers, or
//     uncompressed input after the gzip member, including a second gzip
//     member) are rejected — the gzip stream is drained within a bounded
//     budget, so a hostile bundle with a huge trailing payload is
//     rejected without unbounded decompression work;
//  2. the archive must carry exactly the pinned layout — content,
//     metadata.json, bundle.sha256 (last), all plain regular files
//     without PAX or GNU extended headers, within the size caps;
//  3. the archive bytes must match the bundle.sha256 value embedded in
//     the bundle — checked before any further decompression work, so a
//     corrupt bundle is rejected without draining its stream — a mismatch
//     means the bundle is corrupt or was modified after creation ("bundle
//     corrupt"), attributed separately from content tampering, which is
//     detectable only at verification time ("content digest mismatch",
//     via the digests this function surfaces);
//  4. the bundled metadata document must pass the strict metadata parse
//     (Parse) — the same parse used online, with the same validation
//     surface. The schema requires the trust fields (trust.contentDigests
//     with at least one entry, trust.attestation.signature, and
//     trust.attestation.publicKey), so a document without verification
//     material cannot pass and the bundle is rejected with the
//     document-level problems wrapped (errors.As to *ParseError);
//     verification material is required for every bundle, online or
//     offline (ADR-022 §3).
//
// The returned Bundle carries the content bytes, the parsed metadata (and
// its digests, so verification failures are attributable), and the parse
// warnings. Trust validation itself runs later, through Bundle.Verify
// with operator-supplied anchors — OpenBundle never skips or weakens the
// verification path; a bundle carries no anchors and provides no bypass
// (ADR-022 §3; PM decision D-07).
//
// Consumption is purely local: the bundle is read from the bytes given,
// no network access is used.
func OpenBundle(data []byte) (*Bundle, error) {
	// The counting reader tracks exactly how many input bytes the gzip
	// reader consumes (it implements flate.Reader, so compress/gzip uses
	// it unbuffered — gunzip.go Reset keeps flate.Reader inputs as-is),
	// letting the final check reject ANY trailing input — a second gzip
	// member, or garbage of any length — against the pinned single-member
	// format.
	cr := &countingReader{r: bytes.NewReader(data)}
	gz, err := gzip.NewReader(cr)
	if err != nil {
		return nil, bundleError(BundleErrorKindStructure, "bundle",
			fmt.Sprintf("not a bundle: the input is not a gzip-compressed tar archive (%v). A bundle is a single .tar.gz archive carrying exactly %s, %s, and %s in that order (TS-014-05-01).", err, BundleContentFileName, BundleMetadataFileName, BundleChecksumFileName))
	}
	defer gz.Close()
	// Single-member gzip only: a second gzip member is never decompressed
	// (no decompression work on it) and is rejected by the exact
	// consumption check at the end.
	gz.Multistream(false)

	// The hashing reader observes the uncompressed tar stream as the tar
	// reader consumes it (headers, data, and padding), so the bundle
	// checksum is verified against exactly the bytes CreateBundle
	// hashed.
	sw := &switchableHashReader{r: gz, h: sha256.New(), on: true}
	tr := tar.NewReader(sw)

	// Entry 1: content.
	hdr, err := tr.Next()
	if err != nil {
		return nil, nextEntryError(err)
	}
	if hdr.Name != BundleContentFileName || !isRegularFile(hdr) {
		return nil, entryShapeError(1, hdr)
	}
	if be := rejectExtendedHeader(hdr); be != nil {
		return nil, be
	}
	content, err := readBundleEntry(tr, hdr, MaxBundleContentSize, BundleContentFileName)
	if err != nil {
		return nil, err
	}

	// Entry 2: metadata document.
	hdr, err = tr.Next()
	if err != nil {
		return nil, nextEntryError(err)
	}
	if hdr.Name != BundleMetadataFileName || !isRegularFile(hdr) {
		return nil, entryShapeError(2, hdr)
	}
	if be := rejectExtendedHeader(hdr); be != nil {
		return nil, be
	}
	metadata, err := readBundleEntry(tr, hdr, MaxBundleMetadataSize, BundleMetadataFileName)
	if err != nil {
		return nil, err
	}

	// Entry 3: bundle checksum — the last entry. Its tar header completes
	// the hashed scope; its data carries the declared value and is
	// excluded, so hashing stops once the header is consumed.
	hdr, err = tr.Next()
	if err != nil {
		return nil, nextEntryError(err)
	}
	if hdr.Name != BundleChecksumFileName || !isRegularFile(hdr) {
		return nil, entryShapeError(3, hdr)
	}
	if be := rejectExtendedHeader(hdr); be != nil {
		return nil, be
	}
	sw.on = false
	declaredChecksum, err := readChecksumEntry(tr, hdr)
	if err != nil {
		return nil, err
	}

	// No further entries are allowed: the layout is pinned.
	if extra, err := tr.Next(); err != io.EOF {
		if err == nil {
			return nil, bundleError(BundleErrorKindStructure, extra.Name,
				fmt.Sprintf("unexpected entry after %s — a bundle carries exactly %s, %s, and %s in that order; the format is pinned (TS-014-05-01). Obtain a fresh copy of the bundle.", BundleChecksumFileName, BundleContentFileName, BundleMetadataFileName, BundleChecksumFileName))
		}
		return nil, nextEntryError(err)
	}

	// Bundle checksum: the archive-level integrity check ("bundle
	// corrupt" attribution), run BEFORE the bounded drain so a corrupt
	// bundle is rejected without further decompression work. Content
	// tampering that an attacker re-checksums is NOT caught here — it is
	// caught by the content digest verification at validation time, and
	// the digests are surfaced on the returned Bundle so that failure is
	// attributable as "content digest mismatch".
	computed := hex.EncodeToString(sw.h.Sum(nil))
	if declaredChecksum != computed {
		return nil, bundleError(BundleErrorKindIntegrity, BundleChecksumFileName,
			fmt.Sprintf("bundle checksum mismatch: the archive stream does not match the declared %s value (declared %s, computed %s) — the bundle is corrupt or was modified after it was created; obtain a fresh copy of the bundle.", BundleChecksumFileName, declaredChecksum, computed))
	}

	// Bounded drain: rejects decompressed trailing data after the tar
	// stream (end-of-archive markers) within a fixed work budget, and
	// validates the gzip trailer (CRC and size) when the stream is
	// intact.
	if err := drainRemainder(gz); err != nil {
		return nil, err
	}

	// Exact consumption check: the single gzip member must end exactly at
	// the end of the input. Any unconsumed input — a second gzip member,
	// or trailing bytes of any length — is rejected (the pinned format
	// has no room for trailing input).
	if remaining := int64(len(data)) - cr.n; remaining != 0 {
		return nil, bundleError(BundleErrorKindStructure, "bundle",
			fmt.Sprintf("trailing input after the bundle's gzip stream (%d bytes) — a bundle is exactly one gzip member that ends at its end-of-archive markers; the format is pinned (TS-014-05-01). Obtain a fresh copy of the bundle.", remaining))
	}

	// Strict metadata parse — the same parse used online, not a
	// structural decode (no bypass; ADR-022 §3). The schema requires the
	// trust fields — trust.contentDigests (at least one entry),
	// trust.attestation.signature, and trust.attestation.publicKey — so
	// a bundled document without verification material is rejected here,
	// with the document-level problems wrapped for inspection.
	parsed, err := Parse(metadata)
	if err != nil {
		return nil, bundleErrorCause(BundleErrorKindMetadata, BundleMetadataFileName,
			"the bundled metadata document is rejected by the same strict parse used for online installs (registry.Parse) — a bundle without a valid metadata document cannot be verified and is rejected (ADR-022 §3); fix the document or obtain a fresh bundle from the publisher", err)
	}

	return &Bundle{
		Content:  content,
		Metadata: *parsed.Metadata,
		Warnings: parsed.Warnings,
	}, nil
}

// isRegularFile reports whether the header describes a regular file
// (TypeReg '0' or the legacy TypeRegA '\x00'); directories, links, and
// device entries are rejected by the pinned bundle layout.
func isRegularFile(hdr *tar.Header) bool {
	return hdr.Typeflag == tar.TypeReg || hdr.Typeflag == tar.TypeRegA
}

// nextEntryError renders an error from tar.Next: unexpected EOF is a
// truncated archive, any other error is a corrupt stream.
func nextEntryError(err error) *BundleError {
	return bundleError(BundleErrorKindStructure, "bundle",
		fmt.Sprintf("the archive is corrupt or truncated (%v) — the bundle is damaged or incomplete; obtain a fresh copy of the bundle.", err))
}

// entryShapeError renders the pinned-layout violation for an unexpected
// entry at the given position.
func entryShapeError(position int, hdr *tar.Header) *BundleError {
	return bundleError(BundleErrorKindStructure, hdr.Name,
		fmt.Sprintf("is not the expected bundle entry at position %d: a bundle carries exactly three regular files in order — %s, %s, and %s; the format is pinned (TS-014-05-01). Obtain a fresh copy of the bundle.", position, BundleContentFileName, BundleMetadataFileName, BundleChecksumFileName))
}

// rejectExtendedHeader rejects tar entries encoded with PAX or GNU
// extended headers (hdr.Format reports the encoding the reader had to
// parse — FormatPAX for TypeXHeader records, FormatGNU for GNU long
// name/link records and GNU-magic headers). The bundle format is pinned
// to plain tar headers: CreateBundle never emits extended headers, and a
// PAX or GNU record could attempt to alias entry names or smuggle
// per-entry metadata into a pinned layout — the layout is exact, not
// extensible, so any extended header is a structural violation
// (security hardening, TS-014-05-01).
func rejectExtendedHeader(hdr *tar.Header) *BundleError {
	if hdr.Format != tar.FormatPAX && hdr.Format != tar.FormatGNU {
		return nil
	}
	return bundleError(BundleErrorKindStructure, hdr.Name,
		fmt.Sprintf("is encoded with a %s extended header — the bundle format is pinned to plain tar headers and does not support extended headers; the layout is exact, not extensible (TS-014-05-01). Obtain a fresh copy of the bundle.", hdr.Format))
}

// drainBudget bounds the decompression work spent looking for trailing
// data after the bundle's tar stream. Any decompressed bytes beyond the
// end-of-archive markers are trailing data (rejected); the budget turns a
// hostile bundle with a huge trailing payload into a bounded rejection
// instead of unbounded decompression. 1 MiB is far beyond any legitimate
// remainder (tar block alignment is 512 bytes) and small enough to bound
// hostile work.
const drainBudget = 1 << 20

// drainRemainder reads the rest of the gzip stream within the bounded
// drainBudget and rejects the bundle if any decompressed bytes remain
// after the tar end-of-archive markers. Reading the remainder also
// validates the gzip trailer (CRC and size) when the stream is intact,
// so a corrupt compressed layer is rejected here. Returns a BundleError
// for corrupt streams, trailing decompressed data, and trailing data
// beyond the budget.
func drainRemainder(gz *gzip.Reader) error {
	n, err := io.Copy(io.Discard, io.LimitReader(gz, drainBudget+1))
	if err != nil {
		return bundleError(BundleErrorKindStructure, "bundle",
			fmt.Sprintf("the archive is corrupt: the gzip stream fails validation (%v). The bundle is corrupt or was modified; obtain a fresh copy of the bundle.", err))
	}
	if n > drainBudget {
		return bundleError(BundleErrorKindStructure, "bundle",
			fmt.Sprintf("the bundle carries more than %d bytes of trailing data after its stream, exceeding the %d-byte drain budget — a bundle ends at its end-of-archive markers; the format is pinned (TS-014-05-01). Obtain a fresh copy of the bundle.", drainBudget, drainBudget))
	}
	if n > 0 {
		return bundleError(BundleErrorKindStructure, "bundle",
			fmt.Sprintf("trailing data after the bundle stream (%d bytes) — a bundle ends at its end-of-archive markers; the format is pinned (TS-014-05-01). Obtain a fresh copy of the bundle.", n))
	}
	return nil
}

// readBundleEntry reads one regular-file entry, enforcing its size cap
// before any data is read: an entry beyond the cap is rejected precisely,
// without unbounded memory use.
func readBundleEntry(tr *tar.Reader, hdr *tar.Header, max int64, field string) ([]byte, error) {
	if hdr.Size > max {
		return nil, bundleError(BundleErrorKindStructure, field,
			fmt.Sprintf("is %d bytes, exceeding the %d-byte cap — an entry beyond the cap is not a valid bundle entry; obtain a fresh copy of the bundle.", hdr.Size, max))
	}
	raw, err := io.ReadAll(io.LimitReader(tr, max+1))
	if err != nil {
		return nil, bundleError(BundleErrorKindStructure, field,
			fmt.Sprintf("the entry data is unreadable: %v — the bundle is corrupt; obtain a fresh copy of the bundle.", err))
	}
	if int64(len(raw)) > max { // defensive: hdr.Size was already checked
		return nil, bundleError(BundleErrorKindStructure, field,
			fmt.Sprintf("is %d bytes, exceeding the %d-byte cap — an entry beyond the cap is not a valid bundle entry; obtain a fresh copy of the bundle.", len(raw), max))
	}
	return raw, nil
}

// readChecksumEntry reads the bundle.sha256 value: exactly 64 lowercase
// hex characters, no trailing newline. Any other shape is a structural
// violation of the pinned format.
func readChecksumEntry(tr *tar.Reader, hdr *tar.Header) (string, error) {
	if hdr.Size != MaxBundleChecksumSize {
		return "", bundleError(BundleErrorKindStructure, BundleChecksumFileName,
			fmt.Sprintf("must be exactly %d bytes (the lowercase-hex SHA-256 of the bundle stream), found %d bytes — the format is pinned (TS-014-05-01); obtain a fresh copy of the bundle.", MaxBundleChecksumSize, hdr.Size))
	}
	raw := make([]byte, MaxBundleChecksumSize)
	if _, err := io.ReadFull(tr, raw); err != nil {
		return "", bundleError(BundleErrorKindStructure, BundleChecksumFileName,
			fmt.Sprintf("the checksum value is unreadable: %v — the bundle is corrupt; obtain a fresh copy of the bundle.", err))
	}
	value := string(raw)
	if !isLowercaseHexDigest(value) {
		return "", bundleError(BundleErrorKindStructure, BundleChecksumFileName,
			fmt.Sprintf("must be exactly 64 lowercase hex characters (the SHA-256 of the bundle stream), found %q — the format is pinned (TS-014-05-01); obtain a fresh copy of the bundle.", value))
	}
	return value, nil
}

// isLowercaseHexDigest reports whether s is exactly 64 lowercase hex
// characters (the canonical encoding of a SHA-256 digest).
func isLowercaseHexDigest(s string) bool {
	if len(s) != sha256.Size*2 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// switchableHashWriter writes through to w while hashing every byte when
// on — used on the bundle creation side to compute the bundle checksum
// over the uncompressed tar stream, with the scope toggled around the
// checksum value.
type switchableHashWriter struct {
	w  io.Writer
	h  hash.Hash
	on bool
}

func (s *switchableHashWriter) Write(p []byte) (int, error) {
	n, err := s.w.Write(p)
	if s.on {
		s.h.Write(p[:n])
	}
	return n, err
}

// switchableHashReader mirrors switchableHashWriter on the read side:
// every byte consumed from the gzip stream is hashed while on.
type switchableHashReader struct {
	r  io.Reader
	h  hash.Hash
	on bool
}

func (s *switchableHashReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if s.on {
		s.h.Write(p[:n])
	}
	return n, err
}

// countingReader tracks exactly how many input bytes the gzip reader
// consumes. It implements flate.Reader (io.Reader + io.ByteReader), so
// compress/gzip uses it directly instead of wrapping it in a buffered
// reader (gunzip.go: Reset keeps an input that implements flate.Reader
// unbuffered) — the count is therefore exact, and any input bytes left
// after the bundle's gzip stream has ended are trailing input (a second
// gzip member, or garbage of any length), which the pinned single-member
// format rejects.
type countingReader struct {
	r *bytes.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func (c *countingReader) ReadByte() (byte, error) {
	b, err := c.r.ReadByte()
	if err == nil {
		c.n++
	}
	return b, err
}
