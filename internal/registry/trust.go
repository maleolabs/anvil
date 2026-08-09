// Trust validation at adoption (TS-014-04-02; ADR-022).
//
// Every standard install verifies integrity and publisher attestation
// before the standard's code runs with production privileges; a failed
// verification aborts the install (ADR-022 §3). This file implements the
// verification engine; wiring it into the install flow is T-007's scope,
// and the offline/bundled install path (T-013) reuses this engine with
// identical semantics through the single entry point VerifyTrust.
//
// Verification is pure and local: it consumes the metadata document, the
// release content bytes, and the out-of-band trust anchor allowlist
// (anchors.go), and produces a TrustResult record — results, not errors,
// following the CompatibilityResult precedent (compatibility.go,
// TS-014-04-01) — that the validation orchestration (T-012) and the
// install/update flows (T-007/T-008) persist for auditability (T-009).
// No content is fetched here: content arrives as bytes; resolution is the
// caller's scope (ADR-030: the registry is distribution metadata, not
// content hosting).
//
// Three checks, all mandatory, all independent, all recorded:
//
//  1. Integrity — every declared CONTENT digest (the entries without a
//     name, TS-014-04-04) must equal the recomputed SHA-256 of the
//     content bytes: all-match semantics, not any-match
//     (registry-metadata.md §4.7; ADR-022 §3). Each entry is decoded
//     from its declared encoding and compared as bytes; entries in
//     different encodings of the same digest all match; a mismatch on
//     ANY entry fails verification. Named entries — digests bound to
//     release assets (e.g. the adapter binaries of the same release,
//     TS-014-04-04) — are not content digests: they are verified against
//     their named assets at asset install (VerifyAssetDigest) and are
//     covered by the same publisher attestation (the payload
//     concatenates every declared digest). An entry that cannot be
//     decoded, does not decode to exactly 32 bytes, or declares an
//     unsupported algorithm is not verification material and fails the
//     check — parse.go enforces these at parse time; verification
//     re-enforces them defensively because structural decode paths (e.g.
//     index.go LoadIndex) bypass parse.
//
//  2. Publisher attestation — an Ed25519 signature over the canonical
//     attestation payload, constructed byte-for-byte as
//
//     utf8(id) || 0x00 || utf8(version) || 0x00 ||
//     concat(entry bytes in contentDigests array order)
//
//     where each entry contributes its decoded digest bytes, prefixed by
//     utf8(name) || 0x00 when the entry carries a name (TS-014-04-04,
//     security review F-2 — the name is SIGNED, so it can be neither
//     stripped nor renamed across assets without invalidating the
//     attestation; registry-metadata.md §4.7; PM decision D-01),
//     verified with the publicKey declared in the document. The payload
//     is composed from the DECLARED digest values in array order — the
//     signature binds the release claims (id, version) to the declared
//     content digests AND their asset bindings, so it cannot be detached
//     and replayed against a different release of the same content — and
//     the composition is exact: any deviation (extra or missing
//     separator, reordering, string-level construction) invalidates the
//     attestation. The signature must be strict RFC-4648 base64 decoding
//     to exactly 64 bytes, and the publicKey to exactly 32 bytes; any
//     decode, length, or verification error fails the check.
//
//  3. Trust anchor — the declared publicKey proves key ownership only,
//     not publisher identity (PM decision D-07). Origin is established
//     only when the declared key equals the anchored public key for the
//     publisher in the out-of-band allowlist (anchors.go). The publisher
//     identity is the metadata document id: the schema declares no
//     separate publisher field. No anchors configured, an unknown
//     publisher, or a declared key that differs from the anchor all fail
//     the check with actionable messages — there is no first-use
//     acceptance (no TOFU) and no privileged path for any standard
//     (ADR-022 §3).
//
// Valid is true only when all three checks pass. Failure reasons are
// accumulated, never short-circuited: every failing check contributes an
// actionable message (what failed and how to resolve it), so the
// installer can fix the whole problem at once (CompatibilityResult
// precedent, TS-014-04-01).
//
// Verification material requirements — what the offline/bundled install
// path (T-013) must preserve: the release must carry trust.contentDigests
// (at least one entry, algorithm sha-256, decodable to 32 bytes) and
// trust.attestation (algorithm ed25519, a base64 signature, a base64
// public key); the adopting host must supply the operator-configured
// trust anchor allowlist (anchors.go). Everything is local: metadata,
// content bytes, and anchors — no network.
//
// The $schema field is an annotation only: verification never dispatches
// on it, and a document's self-declared target does not change the
// checks — the engine verifies against the pinned schema version
// semantics, not document claims (TS-014-04-02 security notes).
//
// Reference: TS-014-04-02, ADR-022 §3, ADR-023 §3, ADR-030
package registry

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// TrustResult records the outcome of adoption-time trust validation:
// whether the adoption may proceed (Valid), the per-dimension check
// flags, and the declared values the checks were performed against. The
// record is persisted by the validation orchestration (T-012) and the
// install/update flows (T-007/T-008) for auditability (TS-014-04-02 DoD:
// adoptions remain auditable via recorded results; ADR-022 §3; ADR-024
// §3.6). The json tags are the persistence shape of the record (T-009);
// the registry metadata format is untouched — this type is runtime-side,
// not part of the format.
type TrustResult struct {
	// Valid reports whether the adoption may proceed: every check
	// passed. When false, Errors carries every rejection reason.
	Valid bool `json:"valid"`

	// IntegrityVerified reports whether EVERY declared content digest
	// matched the recomputed SHA-256 of the content bytes (all-match
	// semantics, registry-metadata.md §4.7).
	IntegrityVerified bool `json:"integrityVerified"`

	// AttestationVerified reports whether the Ed25519 signature
	// verified over the canonical attestation payload with the
	// publicKey declared in the document (PM decision D-01).
	AttestationVerified bool `json:"attestationVerified"`

	// AnchorMatched reports whether the declared public key equals the
	// trusted anchor for the publisher — origin established out of
	// band (PM decision D-07). False when no anchors are configured.
	AnchorMatched bool `json:"anchorMatched"`

	// Publisher is the publisher identity the checks were performed
	// against: the metadata document's id (the schema declares no
	// separate publisher field).
	Publisher string `json:"publisher"`

	// DeclaredDigests is the content digest set the integrity check
	// and the attestation payload were performed against — the
	// auditable record of what was declared.
	DeclaredDigests []DeclaredDigest `json:"declaredDigests"`

	// DeclaredSignature is the base64 signature declared in the
	// document.
	DeclaredSignature string `json:"declaredSignature,omitempty"`

	// DeclaredPublicKey is the base64 verification public key declared
	// in the document.
	DeclaredPublicKey string `json:"declaredPublicKey,omitempty"`

	// AnchorPath is the trust anchors file the allowlist was loaded
	// from, when the verification used a file-backed store. Empty for
	// in-memory stores and when no anchors were configured.
	AnchorPath string `json:"anchorPath,omitempty"`

	// Errors lists every rejection reason found, each actionable:
	// what failed and how to resolve it. Empty when Valid is true.
	Errors []string `json:"errors,omitempty"`
}

// DeclaredDigest is one declared content digest entry of the metadata
// document, recorded as declared for auditability.
type DeclaredDigest struct {
	// Algorithm is the declared digest algorithm.
	Algorithm string `json:"algorithm"`

	// Encoding is the declared digest encoding.
	Encoding string `json:"encoding"`

	// Digest is the declared digest value in the declared encoding.
	Digest string `json:"digest"`
}

// VerifyTrust performs adoption-time trust validation of one release
// (TS-014-04-02): integrity of the content bytes against every declared
// digest, the publisher attestation over the canonical payload, and the
// out-of-band trust anchor match. Incompatibilities are never Go errors:
// the outcome is a TrustResult with Valid=false and one actionable
// message per rejection reason, so the caller surfaces them at install
// and persists the record for auditability (CompatibilityResult
// precedent, TS-014-04-01).
//
// The metadata document is expected to have passed the strict format
// parse (parse.go); the engine still verifies defensively — it never
// assumes the document was parsed (structural decode paths bypass
// parse). anchors is the out-of-band allowlist (anchors.go); nil or an
// empty store means no anchors are configured, and verification then
// always fails with an actionable no-anchor message (no TOFU, no
// privileged path; PM decision D-07). The same entry point serves online
// and offline/bundled installs (T-013): verification consumes only the
// metadata, the content bytes, and the local anchor store — no network.
//
// Reference: TS-014-04-02, ADR-022 §3, ADR-023 §3
func VerifyTrust(md Metadata, content []byte, anchors *TrustAnchors) TrustResult {
	result := TrustResult{
		Publisher:         md.ID,
		DeclaredSignature: md.Trust.Attestation.Signature,
		DeclaredPublicKey: md.Trust.Attestation.PublicKey,
	}
	// The record is persisted for auditability; copy the declared
	// digests so later caller mutation cannot rewrite what was
	// validated and recorded (CompatibilityResult precedent).
	for _, d := range md.Trust.ContentDigests {
		result.DeclaredDigests = append(result.DeclaredDigests, DeclaredDigest{
			Algorithm: d.Algorithm,
			Encoding:  d.Encoding,
			Digest:    d.Digest,
		})
	}
	if anchors != nil {
		result.AnchorPath = anchors.path
	}

	checkIntegrity(&result, md, content)
	checkAttestation(&result, md)
	checkAnchor(&result, md, anchors)

	result.Valid = result.IntegrityVerified && result.AttestationVerified && result.AnchorMatched
	return result
}

// checkIntegrity verifies the content bytes against every declared
// CONTENT digest — the entries without a name (TS-014-04-04): each entry
// is decoded from its declared encoding and compared byte-for-byte with
// the recomputed SHA-256 — all-match, not any-match (registry-metadata.md
// §4.7). Named entries (digests bound to release assets, e.g. adapter
// binaries) are NOT content digests: they are verified against their
// named assets at asset install (VerifyAssetDigest) and are not compared
// with the release content. Every failing entry appends an actionable
// message; IntegrityVerified is set only when every content entry
// matched.
func checkIntegrity(result *TrustResult, md Metadata, content []byte) {
	contentDigests := 0
	for _, d := range md.Trust.ContentDigests {
		if d.Name == "" {
			contentDigests++
		}
	}
	if contentDigests == 0 {
		result.Errors = append(result.Errors,
			"the release declares no content digest for the release content (no unnamed trust.contentDigests entry — every entry, if any, is a named asset digest); a release without release-content integrity material cannot be verified (ADR-022 §3). Publish the release with an unnamed trust.contentDigests entry (at least one sha-256 digest of the release content).")
		return
	}

	sum := sha256.Sum256(content)
	allMatch := true
	for i, d := range md.Trust.ContentDigests {
		if d.Name != "" {
			// Asset-bound entry: verified against the named asset at
			// asset install (VerifyAssetDigest), not against the release
			// content (TS-014-04-04). Decodability is still enforced by
			// the attestation check (attestationPayload decodes every
			// declared entry).
			continue
		}
		decoded, err := decodeDigestValue(d)
		if err != nil {
			allMatch = false
			result.Errors = append(result.Errors, fmt.Sprintf(
				"content digest entry [%d] (%s) is not verification material: %v. Fix the trust.contentDigests declaration.",
				i, d.Encoding, err))
			continue
		}
		if !bytes.Equal(decoded, sum[:]) {
			allMatch = false
			// The recomputed digest is deliberately NOT included in the
			// message: it would leak a hash oracle when the installer is
			// pointed at content it should not fetch (e.g. internal hosts).
			// The declared digest and the mismatch fact are actionable;
			// the recomputed value is not.
			message := fmt.Sprintf(
				"content digest mismatch: entry [%d] (%s, declared %q) does not match the recomputed SHA-256 of the fetched content. The content is not what the release claims — re-fetch the release content",
				i, d.Encoding, d.Digest)
			if md.Distribution.Location != "" {
				message += fmt.Sprintf(" from %s", md.Distribution.Location)
			}
			result.Errors = append(result.Errors, message+", or fix the digest declaration.")
		}
	}
	result.IntegrityVerified = allMatch
}

// checkAttestation verifies the publisher attestation: an Ed25519
// signature over the canonical attestation payload, verified with the
// publicKey declared in the document (PM decision D-01). The payload is
// constructed byte-for-byte from the declared values (attestationPayload);
// the signature and publicKey are strict RFC-4648 base64 (parse.go
// enforces shape at parse time; the decode is re-checked defensively) of
// exactly 64 and 32 bytes respectively. Any decode, length, or
// verification failure appends an actionable message and fails the
// check.
func checkAttestation(result *TrustResult, md Metadata) {
	if md.Trust.Attestation.Algorithm != "" && md.Trust.Attestation.Algorithm != AttestationAlgorithmEd25519 {
		result.Errors = append(result.Errors, fmt.Sprintf(
			"the release declares attestation algorithm %q, only %q is supported (PM decision D-01). Fix trust.attestation.algorithm.",
			md.Trust.Attestation.Algorithm, AttestationAlgorithmEd25519))
		return
	}

	payload, err := attestationPayload(md)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf(
			"the canonical attestation payload cannot be constructed: %v. Fix the trust fields.",
			err))
		return
	}

	if md.Trust.Attestation.Signature == "" {
		result.Errors = append(result.Errors,
			"the release carries no signature; without a signature there is no publisher attestation (ADR-022 §3). Publish the release with trust.attestation.signature populated.")
		return
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(md.Trust.Attestation.Signature)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf(
			"the declared signature is not strict RFC-4648 base64 (standard alphabet with padding): %v. Fix trust.attestation.signature.",
			err))
		return
	}
	if len(signature) != ed25519.SignatureSize {
		result.Errors = append(result.Errors, fmt.Sprintf(
			"the declared signature decodes to %d bytes, want exactly %d bytes (Ed25519). Fix trust.attestation.signature.",
			len(signature), ed25519.SignatureSize))
		return
	}

	if md.Trust.Attestation.PublicKey == "" {
		result.Errors = append(result.Errors,
			"the release carries no verification public key; without a key the attestation is unverifiable (ADR-022 §3). Publish the release with trust.attestation.publicKey populated.")
		return
	}
	publicKey, err := base64.StdEncoding.Strict().DecodeString(md.Trust.Attestation.PublicKey)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf(
			"the declared public key is not strict RFC-4648 base64 (standard alphabet with padding): %v. Fix trust.attestation.publicKey.",
			err))
		return
	}
	if len(publicKey) != ed25519.PublicKeySize {
		result.Errors = append(result.Errors, fmt.Sprintf(
			"the declared public key decodes to %d bytes, want exactly %d bytes (Ed25519). Fix trust.attestation.publicKey.",
			len(publicKey), ed25519.PublicKeySize))
		return
	}

	if !ed25519.Verify(ed25519.PublicKey(publicKey), payload, signature) {
		result.Errors = append(result.Errors,
			"the attestation signature does not verify with the declared public key over the canonical payload (utf8(id) || 0x00 || utf8(version) || 0x00 || concat(decoded digest bytes in contentDigests array order)). The release was not signed by the holder of the declared key — the signature, or the declared claims (id, version, contentDigests), are not what the publisher signed.")
		return
	}
	result.AttestationVerified = true
}

// checkAnchor establishes origin against the out-of-band trust anchor
// allowlist (PM decision D-07): the declared public key must equal the
// anchored key for the publisher (the metadata document id — the schema
// declares no separate publisher field). No anchors configured, an
// unknown publisher, or a declared key that differs from the anchor
// fails the check with an actionable message; there is no first-use
// acceptance and no privileged path (ADR-022 §3).
func checkAnchor(result *TrustResult, md Metadata, anchors *TrustAnchors) {
	label := md.ID
	if label == "" {
		label = "<unknown>"
	}

	if anchors == nil || len(anchors.keys) == 0 {
		result.Errors = append(result.Errors, noAnchorMessage(md.ID, anchorStorePath(anchors)))
		return
	}

	anchored, ok := anchors.keys[md.ID]
	if !ok {
		result.Errors = append(result.Errors, fmt.Sprintf(
			"unknown publisher: no trust anchor configured for publisher %q in %s. Add the publisher's Ed25519 public key to the trust anchors file, or point the install at a different allowlist (--trust-anchors <path>).",
			label, anchorStorePath(anchors)))
		return
	}

	declared, err := base64.StdEncoding.Strict().DecodeString(md.Trust.Attestation.PublicKey)
	if err != nil || len(declared) != ed25519.PublicKeySize {
		result.Errors = append(result.Errors, fmt.Sprintf(
			"the declared public key cannot be checked against the trusted anchor for publisher %q: it is not a valid Ed25519 public key (strict RFC-4648 base64, 32 bytes). Fix trust.attestation.publicKey.",
			label))
		return
	}
	if !bytes.Equal(declared, anchored) {
		result.Errors = append(result.Errors, fmt.Sprintf(
			"public key mismatch: the declared public key does not match the trusted anchor for publisher %q. The release was not signed by the trusted publisher — do not adopt it; update the anchor only if you trust the new key out of band.",
			label))
		return
	}
	result.AnchorMatched = true
}

// noAnchorMessage renders the actionable default-fail message for a
// verification with no anchors configured: what failed, and how to
// resolve it (PM decision D-07: the message names the publisher, the
// anchor file, and the override surfaces).
func noAnchorMessage(publisher, anchorPath string) string {
	label := publisher
	if label == "" {
		label = "<unknown>"
	}
	return fmt.Sprintf(
		"no trust anchor configured for publisher %q; configure the publisher's public key at %s or with --trust-anchors <path> (or the %s environment variable). Verification fails without an out-of-band anchor — no first-use acceptance.",
		label, anchorPath, EnvTrustAnchors)
}

// anchorStorePath renders the path of the anchor store for actionable
// messages: the store's own path when file-backed, the documented
// default otherwise.
func anchorStorePath(anchors *TrustAnchors) string {
	if anchors != nil && anchors.path != "" {
		return anchors.path
	}
	if path, err := DefaultTrustAnchorsPath(); err == nil {
		return path
	}
	return "<user config dir>/anvil/" + DefaultTrustAnchorsFileName
}

// attestationPayload composes the canonical signed payload byte-for-byte
// (registry-metadata.md §4.7; PM decision D-01):
//
//	utf8(id) || 0x00 || utf8(version) || 0x00 ||
//	concat(entry bytes in contentDigests array order)
//
// where 0x00 is a single NUL byte and each entry contributes:
//
//	unnamed entry (release-content digest): decoded digest bytes only
//	named entry (asset digest, TS-014-04-04 / F-2):
//	  utf8(name) || 0x00 || decoded digest bytes
//
// The composition is exact, not a string convention: consumers verify the
// signature over exactly these bytes, and any deviation in the composition
// invalidates the attestation. An entry that is not verification material
// (decodeDigestValue failure) makes the payload unconstructible.
//
// Binding the name into the payload is mandatory (security review F-2): a
// name that stayed outside the signed payload could be stripped from an
// entry without invalidating the signature — the digest bytes would
// remain, VerifyTrust would pass, and the asset verification would see no
// material and degrade to the same-channel checksum (which the same
// attacker controls). Cross-asset renaming (installing one framework's
// binary as another) is equally bound: the name bytes are signed.
//
// Backward compatibility: releases published before binary attestation
// (e.g. v1.0.0) carry no named entries, so their payloads compose
// byte-identically to the pre-F-2 composition and their signatures still
// verify (TS-014-04-04 requirement). All three repositories — this one,
// anvil-standard-laravel, anvil-standard-flutter — compose the payload
// byte-for-byte identically; the composition is fixed by
// registry-metadata.md §4.7.
//
// The separator is a NUL byte, so the claims themselves must not contain
// NUL: a NUL inside id, version, or an asset name would make the
// composition ambiguous and let a signature be attributed to the wrong
// claim pair. The schema patterns exclude NUL; the engine rejects it
// defensively — structural decode paths bypass parse.
func attestationPayload(md Metadata) ([]byte, error) {
	if strings.Contains(md.ID, "\x00") || strings.Contains(md.Version, "\x00") {
		return nil, fmt.Errorf("id or version contains a NUL byte, which the canonical payload composition uses as a separator; a NUL inside a claim makes the composition ambiguous — the schema patterns exclude it")
	}
	buf := make([]byte, 0, len(md.ID)+1+len(md.Version)+1+32*len(md.Trust.ContentDigests))
	buf = append(buf, md.ID...)
	buf = append(buf, 0x00)
	buf = append(buf, md.Version...)
	buf = append(buf, 0x00)
	for i, d := range md.Trust.ContentDigests {
		decoded, err := decodeDigestValue(d)
		if err != nil {
			return nil, fmt.Errorf("content digest entry [%d] (%s) is not verification material: %v", i, d.Encoding, err)
		}
		if d.Name != "" {
			if strings.Contains(d.Name, "\x00") {
				return nil, fmt.Errorf("content digest entry [%d] carries an asset name with a NUL byte, which the canonical payload composition uses as a separator — the schema pattern excludes it", i)
			}
			buf = append(buf, d.Name...)
			buf = append(buf, 0x00)
		}
		buf = append(buf, decoded...)
	}
	return buf, nil
}

// decodeDigestValue decodes one declared digest to its 32 bytes,
// defensively: the entry must declare the supported sha-256 algorithm, a
// known encoding, the canonical encoding shape of that encoding, and a
// decoded length of exactly 32 bytes. Canonicality mirrors parse.go's
// strict digest checks exactly (checkDigestValue): base16 must be exactly
// 64 lowercase hex characters (^[0-9a-f]{64}$); base64 is decoded with
// the Strict RFC-4648 decoder (non-zero pad bits rejected) and must
// re-encode to the declared value (case-sensitive); base32 has no strict
// decoder, so its canonicality is enforced exactly like parse.go — the
// decoded bytes must re-encode to the declared value, which rejects
// non-zero pad bits, excess padding, and missing padding. Parse enforces
// these at parse time; the engine re-enforces them because it must not
// assume the document passed parse (e.g. the structural decode in
// index.go) — a non-canonical digest declaration is not verification
// material.
func decodeDigestValue(d ContentDigest) ([]byte, error) {
	if d.Algorithm != DigestAlgorithmSHA256 {
		return nil, fmt.Errorf("declares digest algorithm %q, only %q is supported (PM decision D-01)", d.Algorithm, DigestAlgorithmSHA256)
	}

	var decoded []byte
	var err error
	switch d.Encoding {
	case DigestEncodingBase16:
		if !reDigestBase16.MatchString(d.Digest) {
			return nil, fmt.Errorf("digest %q is not the canonical base16 encoding of a SHA-256 digest — exactly 64 lowercase hex characters (^[0-9a-f]{64}$)", d.Digest)
		}
		decoded, err = hex.DecodeString(d.Digest)
	case DigestEncodingBase32:
		if !reDigestBase32.MatchString(d.Digest) {
			return nil, fmt.Errorf("digest %q is not the canonical base32 encoding of a SHA-256 digest — 52 data characters plus exactly 4 padding '=' characters (^[A-Z2-7]{52,56}=*$)", d.Digest)
		}
		decoded, err = base32.StdEncoding.DecodeString(d.Digest)
		if err == nil && base32.StdEncoding.EncodeToString(decoded) != d.Digest {
			err = fmt.Errorf("not the canonical RFC-4648 base32 encoding — exactly 4 padding '=' characters with zero pad bits")
		}
	case DigestEncodingBase64:
		if !reDigestBase64.MatchString(d.Digest) {
			return nil, fmt.Errorf("digest %q is not the canonical base64 encoding of a SHA-256 digest — 43 data characters plus one '=' padding (^[A-Za-z0-9+/]{43}=$)", d.Digest)
		}
		decoded, err = base64.StdEncoding.Strict().DecodeString(d.Digest)
		if err == nil && base64.StdEncoding.EncodeToString(decoded) != d.Digest {
			err = fmt.Errorf("not the canonical RFC-4648 base64 encoding — zero pad bits, case-sensitive (a case variant is a different digest value)")
		}
	default:
		return nil, fmt.Errorf("declares unsupported digest encoding %q (supported: base16, base32, base64)", d.Encoding)
	}
	if err != nil {
		return nil, fmt.Errorf("digest %q is not decodable as %s: %v", d.Digest, d.Encoding, err)
	}
	if len(decoded) != 32 {
		return nil, fmt.Errorf("digest %q decodes to %d bytes, want exactly 32 bytes (SHA-256)", d.Digest, len(decoded))
	}
	return decoded, nil
}

// AssetDigest returns the declared content digest entry bound to the
// named release asset (TS-014-04-04): the trust.contentDigests entry
// whose name equals assetName, if any. Named entries are covered by the
// publisher attestation like every other entry — the canonical payload
// concatenates all decoded digest bytes in array order — so a returned
// digest is attestation-bound material; the release's SHA256SUMS.txt is
// same-channel and unsigned and is never a substitute for it.
//
// The caller matches the asset name exactly (the schema restricts names
// to ^[a-z0-9][a-z0-9-]*$, so no path prefixes appear in documents).
func AssetDigest(md Metadata, assetName string) (ContentDigest, bool) {
	for _, d := range md.Trust.ContentDigests {
		if d.Name == assetName {
			return d, true
		}
	}
	return ContentDigest{}, false
}

// VerifyAssetDigest verifies the SHA-256 digest of a downloaded release
// asset against the attestation-bound declaration in the metadata
// document (TS-014-04-04). sha256Hex is the lowercase-hex SHA-256 of the
// downloaded bytes (the shape the download path computes).
//
// It returns:
//
//   - (false, nil) when the document declares no digest for the asset —
//     the release predates binary attestation or was published without
//     it; the caller degrades to the same-channel checksum with an
//     explicit notice (no silent trust downgrade, no fail-closed for old
//     releases);
//   - (true, nil) when the declared digest matches the downloaded bytes —
//     the asset is bound to the attested release;
//   - (true, err) on any mismatch or undecodable declaration — the
//     caller aborts the install with the actionable error.
//
// The digest entry is attestation-bound: it is covered by the publisher's
// Ed25519 signature over the canonical payload, so an attacker who swaps
// the binary (and the same-channel checksum file) cannot also adjust this
// declaration without the signing key.
func VerifyAssetDigest(md Metadata, assetName, sha256Hex string) (bool, error) {
	declared, ok := AssetDigest(md, assetName)
	if !ok {
		return false, nil
	}
	expected, err := decodeDigestValue(declared)
	if err != nil {
		return true, fmt.Errorf(
			"the attestation-bound digest declared for asset %q is not verification material: %v. Fix the trust.contentDigests declaration of release %s %s.",
			assetName, err, md.ID, md.Version)
	}
	actual, err := hex.DecodeString(sha256Hex)
	if err != nil || len(actual) != 32 {
		return true, fmt.Errorf(
			"the downloaded digest of asset %q is not a canonical base16 SHA-256 value: %v. Re-download the asset and retry.",
			assetName, err)
	}
	if !bytes.Equal(actual, expected) {
		return true, fmt.Errorf(
			"attestation-bound digest mismatch for asset %q: the downloaded bytes do not match the digest declared in the release's attested metadata (declared %s %s, downloaded sha-256 %s). The binary was tampered with or the release is broken — the install is aborted (ADR-022 §3; TS-014-04-04). Re-fetch the binary from the release channel %s, or report the broken release to the publisher.",
			assetName, declared.Encoding, declared.Digest, sha256Hex, md.Distribution.Location)
	}
	return true, nil
}
