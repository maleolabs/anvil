package registry

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// ── Test Helpers ─────────────────────────────────────────────────────

// testEd25519Keypair returns a fresh real Ed25519 key pair for tests.
func testEd25519Keypair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key: %v", err)
	}
	return pub, priv
}

// testAnchors builds a file-free trust anchor allowlist anchoring the
// publisher id "anvil-standard-laravel" to the given public key.
func testAnchors(t *testing.T, pub ed25519.PublicKey) *TrustAnchors {
	t.Helper()
	anchors, err := TrustAnchorsFromKeys(map[string]string{
		"anvil-standard-laravel": base64.StdEncoding.EncodeToString(pub),
	})
	if err != nil {
		t.Fatalf("build trust anchors: %v", err)
	}
	return anchors
}

// testContent returns deterministic release content bytes.
func testContent() []byte {
	return []byte("anvil-standard-laravel release content for TS-014-04-02 trust validation tests")
}

// testRelease builds a fully attested release document: real content,
// its real SHA-256 digest in the requested encodings (default base16),
// and a real Ed25519 signature over the canonical attestation payload
// made with the given key. Tests mutate the returned document to
// exercise failure paths.
func testRelease(t *testing.T, content []byte, pub ed25519.PublicKey, priv ed25519.PrivateKey, encodings ...string) Metadata {
	t.Helper()
	sum := sha256.Sum256(content)

	md := Metadata{
		ID:              "anvil-standard-laravel",
		Version:         "1.2.3",
		ContractVersion: "1.0.0",
		Capability: Capability{
			FrameworkVersion: []string{"5.1.0"},
		},
		Distribution: Distribution{
			Type:     DistributionTypeGitHubReleases,
			Location: "https://github.com/maleolabs/anvil-standard-laravel/releases/download/v1.2.3/anvil-standard-laravel.tar.gz",
		},
		Lifecycle: Lifecycle{State: LifecycleStatePublished},
	}
	if len(encodings) == 0 {
		encodings = []string{DigestEncodingBase16}
	}
	for _, enc := range encodings {
		md.Trust.ContentDigests = append(md.Trust.ContentDigests, ContentDigest{
			Algorithm: DigestAlgorithmSHA256,
			Encoding:  enc,
			Digest:    encodeDigestBytes(sum[:], enc),
		})
	}

	payload, err := attestationPayload(md)
	if err != nil {
		t.Fatalf("build canonical attestation payload: %v", err)
	}
	md.Trust.Attestation = Attestation{
		Algorithm: AttestationAlgorithmEd25519,
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload)),
		PublicKey: base64.StdEncoding.EncodeToString(pub),
	}
	return md
}

// encodeDigestBytes renders digest bytes in the given encoding.
func encodeDigestBytes(digest []byte, encoding string) string {
	switch encoding {
	case DigestEncodingBase16:
		return hex.EncodeToString(digest)
	case DigestEncodingBase32:
		return base32.StdEncoding.EncodeToString(digest)
	case DigestEncodingBase64:
		return base64.StdEncoding.EncodeToString(digest)
	}
	return ""
}

// ── Positive Verification ────────────────────────────────────────────

// TestVerifyTrustValid asserts a fully attested release — real content,
// its real SHA-256 digest, a real Ed25519 signature over the canonical
// payload, and a matching trust anchor — verifies on every dimension and
// produces no failure reasons (TS-014-04-02 DoD: integrity verified,
// attestation validated, origin established).
func TestVerifyTrustValid(t *testing.T) {
	content := testContent()
	pub, priv := testEd25519Keypair(t)
	md := testRelease(t, content, pub, priv)

	result := VerifyTrust(md, content, testAnchors(t, pub))

	if !result.Valid {
		t.Errorf("Valid = false, want true; errors: %v", result.Errors)
	}
	if !result.IntegrityVerified {
		t.Error("IntegrityVerified = false, want true")
	}
	if !result.AttestationVerified {
		t.Error("AttestationVerified = false, want true")
	}
	if !result.AnchorMatched {
		t.Error("AnchorMatched = false, want true")
	}
	if len(result.Errors) != 0 {
		t.Errorf("Errors = %v, want none", result.Errors)
	}
}

// TestVerifyTrustMultiEncodingDigestsAllMatch asserts the all-match
// semantics hold across encodings: the same digest declared in base16,
// base32, and base64 all match the recomputed SHA-256, and the signature
// over the concatenated decoded digest bytes (96 bytes) verifies
// (registry-metadata.md §4.7: entries may be different encodings of the
// same digest value — all-match, not any-match).
func TestVerifyTrustMultiEncodingDigestsAllMatch(t *testing.T) {
	content := testContent()
	pub, priv := testEd25519Keypair(t)
	md := testRelease(t, content, pub, priv,
		DigestEncodingBase16, DigestEncodingBase32, DigestEncodingBase64)

	result := VerifyTrust(md, content, testAnchors(t, pub))

	if !result.Valid {
		t.Errorf("Valid = false, want true; errors: %v", result.Errors)
	}
	if !result.IntegrityVerified {
		t.Error("IntegrityVerified = false, want true")
	}
	if !result.AttestationVerified {
		t.Error("AttestationVerified = false, want true")
	}
	if len(result.DeclaredDigests) != 3 {
		t.Errorf("DeclaredDigests = %d entries, want 3", len(result.DeclaredDigests))
	}
}

// TestVerifyTrustResultRecordStableAfterInputMutation asserts the audit
// record owns its slices: mutating the metadata document after
// verification cannot rewrite the recorded declared values
// (CompatibilityResult precedent).
func TestVerifyTrustResultRecordStableAfterInputMutation(t *testing.T) {
	content := testContent()
	pub, priv := testEd25519Keypair(t)
	md := testRelease(t, content, pub, priv)

	result := VerifyTrust(md, content, testAnchors(t, pub))

	md.Trust.ContentDigests[0].Digest = strings.Repeat("00", 32)
	md.Trust.ContentDigests = append(md.Trust.ContentDigests, ContentDigest{
		Algorithm: DigestAlgorithmSHA256,
		Encoding:  DigestEncodingBase16,
		Digest:    strings.Repeat("11", 32),
	})
	md.Trust.Attestation.Signature = "tampered"

	if len(result.DeclaredDigests) != 1 {
		t.Errorf("DeclaredDigests mutated after validation: %v", result.DeclaredDigests)
	}
	if result.DeclaredSignature == "tampered" {
		t.Error("DeclaredSignature mutated after validation")
	}
	if !result.Valid {
		t.Errorf("Valid = false after input mutation; errors: %v", result.Errors)
	}
}

// ── Integrity Failures ───────────────────────────────────────────────

// TestVerifyTrustTamperedContent asserts content that does not match the
// declared digest fails verification with an actionable message naming
// the failing entry, the declared value, and the resolution (TS-014-04-02
// DoD: integrity is verified at every install; failure aborts the
// install; a claim is not evidence).
func TestVerifyTrustTamperedContent(t *testing.T) {
	content := testContent()
	pub, priv := testEd25519Keypair(t)
	md := testRelease(t, content, pub, priv)

	tampered := []byte(strings.Clone(string(content)) + "!")
	result := VerifyTrust(md, tampered, testAnchors(t, pub))

	if result.Valid {
		t.Error("Valid = true for tampered content, want false")
	}
	if result.IntegrityVerified {
		t.Error("IntegrityVerified = true for tampered content, want false")
	}
	if !result.AttestationVerified {
		t.Error("AttestationVerified = false — the attestation binds the declared digests and must still verify; want true")
	}
	if !result.AnchorMatched {
		t.Error("AnchorMatched = false, want true")
	}
	if !hasMessage(result.Errors, "content digest mismatch") {
		t.Errorf("Errors = %v, want a content-digest-mismatch message", result.Errors)
	}
	if !hasMessage(result.Errors, "[0]") {
		t.Errorf("Errors = %v, want the failing entry index", result.Errors)
	}
	if !hasMessage(result.Errors, "re-fetch the release content") {
		t.Errorf("Errors = %v, want a resolution hint", result.Errors)
	}
	// The recomputed SHA-256 of the fetched content must NOT be echoed
	// (security: a digest oracle leak when pointed at content that should
	// not be fetched, e.g. internal hosts); the declared digest and the
	// mismatch fact are the actionable parts.
	recomputed := sha256.Sum256(tampered)
	if hasMessage(result.Errors, hex.EncodeToString(recomputed[:])) {
		t.Errorf("Errors = %v, must not echo the recomputed digest of the fetched content", result.Errors)
	}
}

// TestVerifyTrustOneMismatchingEntryFailsAllMatch asserts all-match
// semantics: with two valid digest entries and one mismatching entry,
// verification fails on the single mismatch (registry-metadata.md §4.7:
// if any entry does not match, verification fails).
func TestVerifyTrustOneMismatchingEntryFailsAllMatch(t *testing.T) {
	content := testContent()
	pub, priv := testEd25519Keypair(t)
	md := testRelease(t, content, pub, priv,
		DigestEncodingBase16, DigestEncodingBase32, DigestEncodingBase64)

	// Corrupt the base32 entry only; the payload must be rebuilt with
	// the corrupted declared value so the signature still covers it.
	md.Trust.ContentDigests[1].Digest = encodeDigestBytes(make([]byte, 32), DigestEncodingBase32)
	payload, err := attestationPayload(md)
	if err != nil {
		t.Fatalf("rebuild payload: %v", err)
	}
	md.Trust.Attestation.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload))

	result := VerifyTrust(md, content, testAnchors(t, pub))

	if result.Valid {
		t.Error("Valid = true with one mismatching digest entry, want false")
	}
	if result.IntegrityVerified {
		t.Error("IntegrityVerified = true, want false — all-match semantics")
	}
	if !hasMessage(result.Errors, "[1]") {
		t.Errorf("Errors = %v, want a message naming the mismatching entry [1]", result.Errors)
	}
	if hasMessage(result.Errors, "[0]") || hasMessage(result.Errors, "[2]") {
		t.Errorf("Errors = %v, want no message for the matching entries", result.Errors)
	}
}

// TestVerifyTrustDefensiveDigestLength asserts the engine re-enforces the
// 32-byte digest length defensively: an entry that is canonically encoded
// but decodes to a different length fails integrity AND makes the
// canonical payload unconstructible, failing attestation — even though
// the document bypassed parse (structural decode path; PM decision 2).
// A canonical base32 value of 35 bytes (56 data characters, no padding)
// passes the per-encoding pattern yet decodes to 35 bytes.
func TestVerifyTrustDefensiveDigestLength(t *testing.T) {
	content := testContent()
	pub, priv := testEd25519Keypair(t)
	md := testRelease(t, content, pub, priv)

	md.Trust.ContentDigests[0] = ContentDigest{
		Algorithm: DigestAlgorithmSHA256,
		Encoding:  DigestEncodingBase32,
		Digest:    base32.StdEncoding.EncodeToString(make([]byte, 35)),
	}

	result := VerifyTrust(md, content, testAnchors(t, pub))

	if result.Valid {
		t.Error("Valid = true for a 35-byte digest entry, want false")
	}
	if result.IntegrityVerified {
		t.Error("IntegrityVerified = true, want false")
	}
	if result.AttestationVerified {
		t.Error("AttestationVerified = true, want false — the payload cannot be constructed from a non-32-byte digest")
	}
	if !hasMessage(result.Errors, "want exactly 32 bytes") {
		t.Errorf("Errors = %v, want a 32-byte message", result.Errors)
	}
}

// TestVerifyTrustRejectsNonCanonicalDigestEncodings asserts the engine is
// symmetric with parse.go's strict digest checks (reviewer finding 1;
// security finding 1): a digest that is not the canonical RFC-4648
// encoding of its declared encoding — non-zero base64 pad bits, non-
// canonical base32 padding, uppercase hex — is not verification material
// and fails both integrity and attestation, even though the document
// bypassed parse.
func TestVerifyTrustRejectsNonCanonicalDigestEncodings(t *testing.T) {
	content := testContent()
	pub, priv := testEd25519Keypair(t)
	sum := sha256.Sum256(content)

	t.Run("base64-nonzero-pad-bits", func(t *testing.T) {
		md := testRelease(t, content, pub, priv, DigestEncodingBase64)
		// Flip the low two bits of the final data character: the
		// significant bits are unchanged, so lenient decoders produce
		// the same bytes, but the pad bits are non-zero — the strict
		// decoder and the canonicality re-check must reject it.
		canonical := base64.StdEncoding.EncodeToString(sum[:])
		idx := strings.IndexByte(base64Alphabet, canonical[42])
		mutated := canonical[:42] + string(base64Alphabet[idx^0b11]) + "="
		md.Trust.ContentDigests[0].Digest = mutated

		result := VerifyTrust(md, content, testAnchors(t, pub))

		if result.IntegrityVerified {
			t.Error("IntegrityVerified = true for non-zero base64 pad bits, want false")
		}
		if result.AttestationVerified {
			t.Error("AttestationVerified = true for non-zero base64 pad bits, want false")
		}
		if !hasMessage(result.Errors, "base64") {
			t.Errorf("Errors = %v, want a base64 message", result.Errors)
		}
	})

	t.Run("base32-nonzero-pad-bits", func(t *testing.T) {
		md := testRelease(t, content, pub, priv, DigestEncodingBase32)
		canonical := base32.StdEncoding.EncodeToString(sum[:])
		// The 52nd data character carries 4 pad bits; flip the lowest
		// two (XOR always changes them).
		idx := strings.IndexByte(base32Alphabet, canonical[51])
		mutated := canonical[:51] + string(base32Alphabet[idx^0b11]) + "===="
		md.Trust.ContentDigests[0].Digest = mutated

		result := VerifyTrust(md, content, testAnchors(t, pub))

		if result.IntegrityVerified {
			t.Error("IntegrityVerified = true for non-zero base32 pad bits, want false")
		}
		if !hasMessage(result.Errors, "base32") {
			t.Errorf("Errors = %v, want a base32 message", result.Errors)
		}
	})

	t.Run("base32-excess-padding", func(t *testing.T) {
		md := testRelease(t, content, pub, priv, DigestEncodingBase32)
		// Five '=' padding characters instead of the canonical four.
		canonical := base32.StdEncoding.EncodeToString(sum[:])
		md.Trust.ContentDigests[0].Digest = canonical + "="

		result := VerifyTrust(md, content, testAnchors(t, pub))

		if result.IntegrityVerified {
			t.Error("IntegrityVerified = true for excess base32 padding, want false")
		}
		if !hasMessage(result.Errors, "base32") {
			t.Errorf("Errors = %v, want a base32 message", result.Errors)
		}
	})

	t.Run("uppercase-hex", func(t *testing.T) {
		md := testRelease(t, content, pub, priv, DigestEncodingBase16)
		md.Trust.ContentDigests[0].Digest = strings.ToUpper(hex.EncodeToString(sum[:]))

		result := VerifyTrust(md, content, testAnchors(t, pub))

		if result.IntegrityVerified {
			t.Error("IntegrityVerified = true for uppercase hex, want false")
		}
		if result.AttestationVerified {
			t.Error("AttestationVerified = true for uppercase hex, want false")
		}
		if !hasMessage(result.Errors, "lowercase hex") {
			t.Errorf("Errors = %v, want a lowercase-hex message", result.Errors)
		}
	})
}

// base64Alphabet and base32Alphabet are the RFC-4648 alphabets, used to
// build non-canonical encodings in tests.
const (
	base64Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	base32Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
)

// TestVerifyTrustUnsupportedDigestAlgorithm asserts a digest entry
// declaring an unsupported algorithm is not verification material and
// fails both the integrity check and the payload construction
// (PM decision D-01: sha-256 only).
func TestVerifyTrustUnsupportedDigestAlgorithm(t *testing.T) {
	content := testContent()
	pub, priv := testEd25519Keypair(t)
	md := testRelease(t, content, pub, priv)
	md.Trust.ContentDigests[0].Algorithm = "md5"

	result := VerifyTrust(md, content, testAnchors(t, pub))

	if result.Valid {
		t.Error("Valid = true for an unsupported digest algorithm, want false")
	}
	if result.IntegrityVerified {
		t.Error("IntegrityVerified = true, want false")
	}
	if result.AttestationVerified {
		t.Error("AttestationVerified = true, want false")
	}
	if !hasMessage(result.Errors, `"md5"`) || !hasMessage(result.Errors, `"sha-256"`) {
		t.Errorf("Errors = %v, want a message naming the unsupported and the supported algorithm", result.Errors)
	}
}

// ── Attestation Failures ─────────────────────────────────────────────

// TestVerifyTrustTamperedSignature asserts a signature that does not
// verify over the canonical payload fails the attestation check with an
// actionable message (TS-014-04-02 DoD: publisher attestation is
// validated).
func TestVerifyTrustTamperedSignature(t *testing.T) {
	content := testContent()
	pub, priv := testEd25519Keypair(t)
	md := testRelease(t, content, pub, priv)

	sig, err := base64.StdEncoding.DecodeString(md.Trust.Attestation.Signature)
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	sig[0] ^= 0x01
	md.Trust.Attestation.Signature = base64.StdEncoding.EncodeToString(sig)

	result := VerifyTrust(md, content, testAnchors(t, pub))

	if result.Valid {
		t.Error("Valid = true for a tampered signature, want false")
	}
	if !result.IntegrityVerified {
		t.Error("IntegrityVerified = false, want true")
	}
	if result.AttestationVerified {
		t.Error("AttestationVerified = true, want false")
	}
	if !result.AnchorMatched {
		t.Error("AnchorMatched = false, want true")
	}
	if !hasMessage(result.Errors, "does not verify") {
		t.Errorf("Errors = %v, want a does-not-verify message", result.Errors)
	}
}

// TestVerifyTrustWrongKey asserts an attestation signed by key A but
// carrying key B (the anchored key) fails verification: the signature
// does not verify with the declared key (PM decision D-01: the publicKey
// in the document is the verification key).
func TestVerifyTrustWrongKey(t *testing.T) {
	content := testContent()
	signerPub, signerPriv := testEd25519Keypair(t)
	declaredPub, _ := testEd25519Keypair(t)

	md := testRelease(t, content, signerPub, signerPriv)
	md.Trust.Attestation.PublicKey = base64.StdEncoding.EncodeToString(declaredPub)

	result := VerifyTrust(md, content, testAnchors(t, declaredPub))

	if result.Valid {
		t.Error("Valid = true for an attestation with the wrong key, want false")
	}
	if result.AttestationVerified {
		t.Error("AttestationVerified = true, want false — the signature does not verify with the declared key")
	}
	if !result.AnchorMatched {
		t.Error("AnchorMatched = false — the declared key IS the anchored key; want true")
	}
}

// TestVerifyTrustUnattestedMaterial asserts releases lacking or
// malforming verification material are rejected on the affected
// dimension with actionable messages (ADR-022 §3; ADR-023 §3: adoption-
// time validation checks trust attestation and integrity).
func TestVerifyTrustUnattestedMaterial(t *testing.T) {
	content := testContent()

	t.Run("missing-signature", func(t *testing.T) {
		pub, priv := testEd25519Keypair(t)
		md := testRelease(t, content, pub, priv)
		md.Trust.Attestation.Signature = ""

		result := VerifyTrust(md, content, testAnchors(t, pub))

		if result.Valid {
			t.Error("Valid = true without a signature, want false")
		}
		if result.AttestationVerified {
			t.Error("AttestationVerified = true without a signature, want false")
		}
		if !hasMessage(result.Errors, "carries no signature") {
			t.Errorf("Errors = %v, want a missing-signature message", result.Errors)
		}
	})

	t.Run("missing-public-key", func(t *testing.T) {
		pub, priv := testEd25519Keypair(t)
		md := testRelease(t, content, pub, priv)
		md.Trust.Attestation.PublicKey = ""

		result := VerifyTrust(md, content, testAnchors(t, pub))

		if result.Valid {
			t.Error("Valid = true without a public key, want false")
		}
		if result.AttestationVerified {
			t.Error("AttestationVerified = true without a public key, want false")
		}
		if !hasMessage(result.Errors, "carries no verification public key") {
			t.Errorf("Errors = %v, want a missing-public-key message", result.Errors)
		}
	})

	t.Run("missing-digests", func(t *testing.T) {
		pub, priv := testEd25519Keypair(t)
		md := testRelease(t, content, pub, priv)
		md.Trust.ContentDigests = nil

		result := VerifyTrust(md, content, testAnchors(t, pub))

		if result.Valid {
			t.Error("Valid = true without content digests, want false")
		}
		if result.IntegrityVerified {
			t.Error("IntegrityVerified = true without content digests, want false")
		}
		if !hasMessage(result.Errors, "declares no content digest") {
			t.Errorf("Errors = %v, want a missing-digests message", result.Errors)
		}
	})

	t.Run("malformed-signature-shape", func(t *testing.T) {
		pub, priv := testEd25519Keypair(t)
		md := testRelease(t, content, pub, priv)
		md.Trust.Attestation.Signature = "not-base64!!"

		result := VerifyTrust(md, content, testAnchors(t, pub))

		if result.AttestationVerified {
			t.Error("AttestationVerified = true for a non-base64 signature, want false")
		}
		if !hasMessage(result.Errors, "not strict RFC-4648 base64") {
			t.Errorf("Errors = %v, want a base64-shape message", result.Errors)
		}
	})

	t.Run("short-signature", func(t *testing.T) {
		pub, priv := testEd25519Keypair(t)
		md := testRelease(t, content, pub, priv)
		md.Trust.Attestation.Signature = base64.StdEncoding.EncodeToString(make([]byte, 16))

		result := VerifyTrust(md, content, testAnchors(t, pub))

		if result.AttestationVerified {
			t.Error("AttestationVerified = true for a 16-byte signature, want false")
		}
		if !hasMessage(result.Errors, "want exactly 64 bytes") {
			t.Errorf("Errors = %v, want a 64-byte message", result.Errors)
		}
	})

	t.Run("short-public-key", func(t *testing.T) {
		pub, priv := testEd25519Keypair(t)
		md := testRelease(t, content, pub, priv)
		md.Trust.Attestation.PublicKey = base64.StdEncoding.EncodeToString(make([]byte, 16))

		result := VerifyTrust(md, content, testAnchors(t, pub))

		if result.AttestationVerified {
			t.Error("AttestationVerified = true for a 16-byte public key, want false")
		}
		if !hasMessage(result.Errors, "want exactly 32 bytes") {
			t.Errorf("Errors = %v, want a 32-byte message", result.Errors)
		}
	})

	t.Run("unsupported-attestation-algorithm", func(t *testing.T) {
		pub, priv := testEd25519Keypair(t)
		md := testRelease(t, content, pub, priv)
		md.Trust.Attestation.Algorithm = "rsa"

		result := VerifyTrust(md, content, testAnchors(t, pub))

		if result.AttestationVerified {
			t.Error("AttestationVerified = true for an unsupported algorithm, want false")
		}
		if !hasMessage(result.Errors, `"rsa"`) || !hasMessage(result.Errors, `"ed25519"`) {
			t.Errorf("Errors = %v, want a message naming both algorithms", result.Errors)
		}
	})
}

// ── Trust Anchor Failures ────────────────────────────────────────────

// TestVerifyTrustNoAnchorsConfigured asserts the default state fails
// closed: no anchors (nil store, empty allowlist) → verification fails
// with an actionable message naming the publisher, the anchor file, and
// the resolution surfaces (PM decision D-07: no TOFU, no privileged
// path).
func TestVerifyTrustNoAnchorsConfigured(t *testing.T) {
	content := testContent()

	t.Run("nil-anchors", func(t *testing.T) {
		pub, priv := testEd25519Keypair(t)
		md := testRelease(t, content, pub, priv)

		result := VerifyTrust(md, content, nil)

		if result.Valid {
			t.Error("Valid = true with no anchors, want false")
		}
		if result.AnchorMatched {
			t.Error("AnchorMatched = true with no anchors, want false")
		}
		if !hasMessage(result.Errors, "no trust anchor configured") {
			t.Errorf("Errors = %v, want a no-anchor message", result.Errors)
		}
		if !hasMessage(result.Errors, `"anvil-standard-laravel"`) {
			t.Errorf("Errors = %v, want the publisher id in the message", result.Errors)
		}
		if !hasMessage(result.Errors, "--trust-anchors") {
			t.Errorf("Errors = %v, want the --trust-anchors resolution hint", result.Errors)
		}
		if !hasMessage(result.Errors, EnvTrustAnchors) {
			t.Errorf("Errors = %v, want the %s environment variable hint", result.Errors, EnvTrustAnchors)
		}
	})

	t.Run("empty-allowlist", func(t *testing.T) {
		pub, priv := testEd25519Keypair(t)
		md := testRelease(t, content, pub, priv)
		anchors, err := TrustAnchorsFromKeys(map[string]string{})
		if err != nil {
			t.Fatalf("build empty anchors: %v", err)
		}

		result := VerifyTrust(md, content, anchors)

		if result.Valid {
			t.Error("Valid = true with an empty allowlist, want false")
		}
		if !hasMessage(result.Errors, "no trust anchor configured") {
			t.Errorf("Errors = %v, want a no-anchor message", result.Errors)
		}
	})

	t.Run("empty-allowlist-from-file", func(t *testing.T) {
		pub, priv := testEd25519Keypair(t)
		md := testRelease(t, content, pub, priv)
		anchors := writeTestAnchorsFile(t, `{"publishers": {}}`)

		result := VerifyTrust(md, content, anchors)

		if result.Valid {
			t.Error("Valid = true with an empty allowlist file, want false")
		}
		if !hasMessage(result.Errors, anchors.Path()) {
			t.Errorf("Errors = %v, want the anchor file path in the message", result.Errors)
		}
	})
}

// TestVerifyTrustUnknownPublisher asserts a publisher without an anchor
// is rejected with an actionable unknown-publisher message naming the
// publisher and the allowlist (PM decision D-07: anchor matching by
// publisher identity — the metadata id).
func TestVerifyTrustUnknownPublisher(t *testing.T) {
	content := testContent()
	pub, priv := testEd25519Keypair(t)
	md := testRelease(t, content, pub, priv)

	otherPub, _ := testEd25519Keypair(t)
	anchors, err := TrustAnchorsFromKeys(map[string]string{
		"anvil-standard-flutter": base64.StdEncoding.EncodeToString(otherPub),
	})
	if err != nil {
		t.Fatalf("build anchors: %v", err)
	}

	result := VerifyTrust(md, content, anchors)

	if result.Valid {
		t.Error("Valid = true for an unknown publisher, want false")
	}
	if result.AnchorMatched {
		t.Error("AnchorMatched = true for an unknown publisher, want false")
	}
	if !hasMessage(result.Errors, "unknown publisher") {
		t.Errorf("Errors = %v, want an unknown-publisher message", result.Errors)
	}
	if !hasMessage(result.Errors, `"anvil-standard-laravel"`) {
		t.Errorf("Errors = %v, want the publisher id in the message", result.Errors)
	}
}

// TestVerifyTrustAnchorKeyMismatch asserts origin is NOT established when
// the declared key differs from the anchored key, even though the
// attestation cryptographically verifies: the in-band key proves key
// ownership only; identity comes from the out-of-band anchor (PM
// decision D-07).
func TestVerifyTrustAnchorKeyMismatch(t *testing.T) {
	content := testContent()
	pub, priv := testEd25519Keypair(t)
	md := testRelease(t, content, pub, priv)

	otherPub, _ := testEd25519Keypair(t)

	result := VerifyTrust(md, content, testAnchors(t, otherPub))

	if result.Valid {
		t.Error("Valid = true when the declared key differs from the anchor, want false")
	}
	if !result.AttestationVerified {
		t.Error("AttestationVerified = false — the signature verifies with the declared key; want true")
	}
	if result.AnchorMatched {
		t.Error("AnchorMatched = true for a key differing from the anchor, want false")
	}
	if !hasMessage(result.Errors, "public key mismatch") {
		t.Errorf("Errors = %v, want a public-key-mismatch message", result.Errors)
	}
	if !hasMessage(result.Errors, "does not match the trusted anchor") {
		t.Errorf("Errors = %v, want a trusted-anchor reference", result.Errors)
	}
}

// ── Canonical Payload Exactness ──────────────────────────────────────

// TestVerifyTrustPayloadConstructionExact asserts the attestation payload
// is composed byte-for-byte as utf8(id) || 0x00 || utf8(version) || 0x00
// || concat(decoded digest bytes in contentDigests array order): every
// deviation from the canonical composition — reordering, extra
// separators, swapped claims, string-level construction — must fail
// verification even though the signature in the document is a valid
// Ed25519 signature (over the canonical payload).
func TestVerifyTrustPayloadConstructionExact(t *testing.T) {
	content := testContent()
	pub, priv := testEd25519Keypair(t)

	t.Run("digest-order-reversed", func(t *testing.T) {
		// Two DISTINCT digest values: the signature is made over the
		// canonical array order [D1, D2]. Reversing the array must
		// change the composed payload bytes — and thus fail
		// attestation — proving the composition respects
		// contentDigests array order (byte-for-byte, not any
		// convention).
		md := testRelease(t, content, pub, priv, DigestEncodingBase16)
		other := sha256.Sum256([]byte("a different release payload"))
		md.Trust.ContentDigests = append(md.Trust.ContentDigests, ContentDigest{
			Algorithm: DigestAlgorithmSHA256,
			Encoding:  DigestEncodingBase16,
			Digest:    hex.EncodeToString(other[:]),
		})
		payload, err := attestationPayload(md) // canonical order [D1, D2]
		if err != nil {
			t.Fatalf("build payload: %v", err)
		}
		md.Trust.Attestation.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload))

		md.Trust.ContentDigests[0], md.Trust.ContentDigests[1] = md.Trust.ContentDigests[1], md.Trust.ContentDigests[0]

		result := VerifyTrust(md, content, testAnchors(t, pub))

		if result.AttestationVerified {
			t.Error("AttestationVerified = true with reversed digest order, want false")
		}
		if result.Valid {
			t.Error("Valid = true with reversed digest order, want false")
		}
	})

	t.Run("extra-separator", func(t *testing.T) {
		md := testRelease(t, content, pub, priv)
		// A signature made over a payload with an extra NUL separator
		// between version and the digest bytes must not verify.
		sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv,
			append([]byte(md.ID+"\x00"+md.Version+"\x00\x00"), mustDecodeDigest(t, md.Trust.ContentDigests[0])...)))
		md.Trust.Attestation.Signature = sig

		result := VerifyTrust(md, content, testAnchors(t, pub))

		if result.AttestationVerified {
			t.Error("AttestationVerified = true for a payload with an extra separator, want false")
		}
	})

	t.Run("swapped-id-version", func(t *testing.T) {
		md := testRelease(t, content, pub, priv)
		sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv,
			append([]byte(md.Version+"\x00"+md.ID+"\x00"), mustDecodeDigest(t, md.Trust.ContentDigests[0])...)))
		md.Trust.Attestation.Signature = sig

		result := VerifyTrust(md, content, testAnchors(t, pub))

		if result.AttestationVerified {
			t.Error("AttestationVerified = true with id/version swapped in the payload, want false")
		}
	})

	t.Run("string-level-digest-construction", func(t *testing.T) {
		// The PM binding forbids string-level construction: the payload
		// must carry the DECODED digest bytes, not the encoded text.
		md := testRelease(t, content, pub, priv)
		hexDigest := md.Trust.ContentDigests[0].Digest // the base16 text
		sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv,
			[]byte(md.ID+"\x00"+md.Version+"\x00"+hexDigest)))
		md.Trust.Attestation.Signature = sig

		result := VerifyTrust(md, content, testAnchors(t, pub))

		if result.AttestationVerified {
			t.Error("AttestationVerified = true for a string-level payload construction, want false")
		}
	})

	t.Run("claims-not-bound", func(t *testing.T) {
		// The signature binds id and version: a signature over the same
		// digests but a different id must not verify against this
		// document (replay protection, PM decision D-01).
		md := testRelease(t, content, pub, priv)
		other := md
		other.ID = "anvil-standard-flutter"
		payload, err := attestationPayload(other)
		if err != nil {
			t.Fatalf("build payload for other id: %v", err)
		}
		md.Trust.Attestation.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload))

		result := VerifyTrust(md, content, testAnchors(t, pub))

		if result.AttestationVerified {
			t.Error("AttestationVerified = true for a signature over different claims, want false")
		}
		if result.Valid {
			t.Error("Valid = true for a signature over different claims, want false")
		}
	})

	t.Run("version-relabel", func(t *testing.T) {
		// The signature was made over version "1.2.3"; relabeling the
		// release to "9.9.9" must not survive attestation — the
		// version is bound into the canonical payload (security
		// finding 3).
		md := testRelease(t, content, pub, priv)
		md.Version = "9.9.9"

		result := VerifyTrust(md, content, testAnchors(t, pub))

		if result.AttestationVerified {
			t.Error("AttestationVerified = true for a version relabel, want false")
		}
		if result.Valid {
			t.Error("Valid = true for a version relabel, want false")
		}
	})

	t.Run("id-and-version-relabel", func(t *testing.T) {
		// Relabeling BOTH identity claims must also fail: the payload
		// binds the exact (id, version) pair (security finding 3).
		md := testRelease(t, content, pub, priv)
		md.ID = "anvil-standard-flutter"
		md.Version = "9.9.9"

		result := VerifyTrust(md, content, testAnchors(t, pub))

		if result.AttestationVerified {
			t.Error("AttestationVerified = true for an id+version relabel, want false")
		}
		if result.Valid {
			t.Error("Valid = true for an id+version relabel, want false")
		}
	})
}

// TestVerifyTrustRejectsNULInClaims asserts the canonical payload
// composition rejects id or version containing a NUL byte defensively:
// the schema patterns exclude NUL, but structural decode paths bypass
// parse, and a NUL inside a claim would make the 0x00-separated
// composition ambiguous (reviewer finding 2).
func TestVerifyTrustRejectsNULInClaims(t *testing.T) {
	content := testContent()
	pub, priv := testEd25519Keypair(t)

	t.Run("nul-in-id", func(t *testing.T) {
		md := testRelease(t, content, pub, priv)
		md.ID = "anvil-standard-laravel\x00injected"

		result := VerifyTrust(md, content, testAnchors(t, pub))

		if result.AttestationVerified {
			t.Error("AttestationVerified = true for a NUL-containing id, want false")
		}
		if !hasMessage(result.Errors, "NUL byte") {
			t.Errorf("Errors = %v, want a NUL-byte message", result.Errors)
		}
	})

	t.Run("nul-in-version", func(t *testing.T) {
		md := testRelease(t, content, pub, priv)
		md.Version = "1.2.3\x00injected"

		result := VerifyTrust(md, content, testAnchors(t, pub))

		if result.AttestationVerified {
			t.Error("AttestationVerified = true for a NUL-containing version, want false")
		}
		if !hasMessage(result.Errors, "NUL byte") {
			t.Errorf("Errors = %v, want a NUL-byte message", result.Errors)
		}
	})
}

// mustDecodeDigest decodes a declared digest entry to its bytes.
func mustDecodeDigest(t *testing.T, d ContentDigest) []byte {
	t.Helper()
	decoded, err := decodeDigestValue(d)
	if err != nil {
		t.Fatalf("decode digest: %v", err)
	}
	return decoded
}

// ── No Privileged Path / No Dispatch ─────────────────────────────────

// TestVerifyTrustNoDispatchOnSchemaField asserts the $schema field is an
// annotation only: verification never dispatches on it, and a document's
// self-declared target changes nothing (TS-014-04-02 security notes).
func TestVerifyTrustNoDispatchOnSchemaField(t *testing.T) {
	content := testContent()
	pub, priv := testEd25519Keypair(t)
	anchors := testAnchors(t, pub)

	base := testRelease(t, content, pub, priv)
	baseResult := VerifyTrust(base, content, anchors)

	for _, schema := range []string{
		"",
		SchemaID,
		"urn:anvil:spec:registry-metadata:2.0.0",
		"garbage-not-a-schema",
	} {
		md := testRelease(t, content, pub, priv)
		md.Schema = schema

		result := VerifyTrust(md, content, anchors)

		if !reflect.DeepEqual(result, baseResult) {
			t.Errorf("result differs for $schema %q:\n got %+v\nwant %+v", schema, result, baseResult)
		}
	}
}

// TestVerifyTrustSameVerificationForEveryStandard asserts the engine has
// no standard-specific branches: two different standards with identical
// trust material verify identically — there is no first-party bypass or
// privileged path (ADR-022 §3).
func TestVerifyTrustSameVerificationForEveryStandard(t *testing.T) {
	content := testContent()
	pub, priv := testEd25519Keypair(t)
	anchors := testAnchors(t, pub)

	md1 := testRelease(t, content, pub, priv)
	md2 := testRelease(t, content, pub, priv)
	md2.ID = "anvil-standard-flutter"
	md2.Trust.Attestation.PublicKey = base64.StdEncoding.EncodeToString(pub)
	anchors2, err := TrustAnchorsFromKeys(map[string]string{
		"anvil-standard-flutter": base64.StdEncoding.EncodeToString(pub),
	})
	if err != nil {
		t.Fatalf("build anchors2: %v", err)
	}

	// Re-sign md2 so the payload binds the flutter id.
	payload, err := attestationPayload(md2)
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}
	md2.Trust.Attestation.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload))

	r1 := VerifyTrust(md1, content, anchors)
	r2 := VerifyTrust(md2, content, anchors2)

	if !r1.Valid || !r2.Valid {
		t.Errorf("both standards must verify identically: r1.Valid=%v r2.Valid=%v (r1: %v, r2: %v)", r1.Valid, r2.Valid, r1.Errors, r2.Errors)
	}
}

// ── Auditability ─────────────────────────────────────────────────────

// TestVerifyTrustRecordDeclaredValues asserts the result carries the
// declared values for auditability and round-trips through JSON, so the
// state-recording flow (T-009) persists it without loss — for both the
// accepted and the rejected (most commonly persisted) outcome
// (CompatibilityResult precedent).
func TestVerifyTrustRecordDeclaredValues(t *testing.T) {
	content := testContent()
	pub, priv := testEd25519Keypair(t)
	md := testRelease(t, content, pub, priv, DigestEncodingBase16, DigestEncodingBase32)
	anchors := testAnchors(t, pub)

	result := VerifyTrust(md, content, anchors)

	if result.Publisher != "anvil-standard-laravel" {
		t.Errorf("Publisher = %q, want %q", result.Publisher, "anvil-standard-laravel")
	}
	if len(result.DeclaredDigests) != 2 {
		t.Errorf("DeclaredDigests = %d entries, want 2", len(result.DeclaredDigests))
	}
	if result.DeclaredDigests[0].Encoding != DigestEncodingBase16 || result.DeclaredDigests[1].Encoding != DigestEncodingBase32 {
		t.Errorf("DeclaredDigests encodings = %v, want [base16 base32]", result.DeclaredDigests)
	}
	if result.DeclaredSignature != md.Trust.Attestation.Signature {
		t.Errorf("DeclaredSignature = %q, want the declared signature", result.DeclaredSignature)
	}
	if result.DeclaredPublicKey != md.Trust.Attestation.PublicKey {
		t.Errorf("DeclaredPublicKey = %q, want the declared public key", result.DeclaredPublicKey)
	}

	assertTrustResultJSONRoundTrip(t, result)

	// Rejected result — the common persisted case — round-trips with
	// Errors populated.
	rejected := VerifyTrust(md, []byte("tampered content"), nil)
	if rejected.Valid {
		t.Fatal("test fixture must produce a rejected result")
	}
	assertTrustResultJSONRoundTrip(t, rejected)
}

func assertTrustResultJSONRoundTrip(t *testing.T, result TrustResult) {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var decoded TrustResult
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if !reflect.DeepEqual(decoded, result) {
		t.Errorf("JSON round trip mismatch:\n got %+v\nwant %+v", decoded, result)
	}
}

// TestVerifyTrustEmptyMetadata asserts a zero-value metadata document
// fails closed on every dimension without panicking: no digests, no
// attestation, no anchors.
func TestVerifyTrustEmptyMetadata(t *testing.T) {
	result := VerifyTrust(Metadata{}, testContent(), nil)

	if result.Valid {
		t.Error("Valid = true for an empty metadata document, want false")
	}
	if result.IntegrityVerified {
		t.Error("IntegrityVerified = true for an empty metadata document, want false")
	}
	if result.AttestationVerified {
		t.Error("AttestationVerified = true for an empty metadata document, want false")
	}
	if result.AnchorMatched {
		t.Error("AnchorMatched = true for an empty metadata document, want false")
	}
	if !hasMessage(result.Errors, "declares no content digest") {
		t.Errorf("Errors = %v, want a missing-digests message", result.Errors)
	}
	if !hasMessage(result.Errors, "no trust anchor configured") {
		t.Errorf("Errors = %v, want a no-anchor message", result.Errors)
	}
}

// ── Trust Anchor Store ───────────────────────────────────────────────

// writeTestAnchorsFile writes content to a temp file and loads it as a
// trust anchor store.
func writeTestAnchorsFile(t *testing.T, content string) *TrustAnchors {
	t.Helper()
	path := filepath.Join(t.TempDir(), "trust-anchors.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write anchors file: %v", err)
	}
	anchors, err := LoadTrustAnchors(path)
	if err != nil {
		t.Fatalf("load anchors file: %v", err)
	}
	return anchors
}

// TestLoadTrustAnchors exercises the store's load validation: format
// discipline (exactly "publishers"), strict key shape (RFC-4648 base64,
// canonical pad bits, 32 bytes), size cap, and not-found handling.
func TestLoadTrustAnchors(t *testing.T) {
	pub, _ := testEd25519Keypair(t)
	encoded := base64.StdEncoding.EncodeToString(pub)

	t.Run("valid", func(t *testing.T) {
		anchors := writeTestAnchorsFile(t, `{"publishers": {"anvil-standard-laravel": "`+encoded+`"}}`)

		if anchors.Len() != 1 {
			t.Errorf("Len = %d, want 1", anchors.Len())
		}
		key, ok := anchors.PublicKey("anvil-standard-laravel")
		if !ok {
			t.Fatal("PublicKey = not found, want the anchored key")
		}
		if !reflect.DeepEqual(key, pub) {
			t.Errorf("PublicKey = %x, want %x", key, pub)
		}
		if _, ok := anchors.PublicKey("anvil-standard-flutter"); ok {
			t.Error("PublicKey(anvil-standard-flutter) = found, want not found")
		}
	})

	t.Run("not-found", func(t *testing.T) {
		_, err := LoadTrustAnchors(filepath.Join(t.TempDir(), "missing.json"))
		if !errors.Is(err, ErrTrustAnchorsNotFound) {
			t.Errorf("err = %v, want wrapped %v", err, ErrTrustAnchorsNotFound)
		}
	})

	t.Run("not-json", func(t *testing.T) {
		_, err := LoadTrustAnchors(writeRawFile(t, "not json"))
		if err == nil || !strings.Contains(err.Error(), "not decodable JSON") {
			t.Errorf("err = %v, want a not-decodable-JSON error", err)
		}
	})

	t.Run("unknown-top-level-field", func(t *testing.T) {
		_, err := LoadTrustAnchors(writeRawFile(t, `{"publishers": {}, "keyRotation": true}`))
		if err == nil || !strings.Contains(err.Error(), "unknown field") {
			t.Errorf("err = %v, want an unknown-field error", err)
		}
	})

	t.Run("trailing-content", func(t *testing.T) {
		_, err := LoadTrustAnchors(writeRawFile(t, `{"publishers": {}} {}`))
		if err == nil || !strings.Contains(err.Error(), "unexpected content") {
			t.Errorf("err = %v, want a trailing-content error", err)
		}
	})

	t.Run("bad-base64-key", func(t *testing.T) {
		_, err := LoadTrustAnchors(writeRawFile(t, `{"publishers": {"anvil-standard-laravel": "not-base64!!"}}`))
		if err == nil || !strings.Contains(err.Error(), "not strict RFC-4648 base64") {
			t.Errorf("err = %v, want a base64-shape error", err)
		}
		if err != nil && !strings.Contains(err.Error(), "anvil-standard-laravel") {
			t.Errorf("err = %v, want the publisher named", err)
		}
	})

	t.Run("wrong-key-length", func(t *testing.T) {
		short := base64.StdEncoding.EncodeToString(make([]byte, 16))
		_, err := LoadTrustAnchors(writeRawFile(t, `{"publishers": {"anvil-standard-laravel": "`+short+`"}}`))
		if err == nil || !strings.Contains(err.Error(), "want exactly 32 bytes") {
			t.Errorf("err = %v, want a 32-byte error", err)
		}
	})

	t.Run("non-canonical-pad-bits", func(t *testing.T) {
		_, err := LoadTrustAnchors(writeRawFile(t, `{"publishers": {"anvil-standard-laravel": "ab=="}}`))
		if err == nil || !strings.Contains(err.Error(), "pad bits must be zero") {
			t.Errorf("err = %v, want a pad-bits error", err)
		}
	})

	t.Run("empty-publishers", func(t *testing.T) {
		anchors := writeTestAnchorsFile(t, `{"publishers": {}}`)
		if anchors.Len() != 0 {
			t.Errorf("Len = %d, want 0", anchors.Len())
		}
	})

	t.Run("empty-key", func(t *testing.T) {
		_, err := LoadTrustAnchors(writeRawFile(t, `{"publishers": {"anvil-standard-laravel": ""}}`))
		if err == nil || !strings.Contains(err.Error(), "empty public key") {
			t.Errorf("err = %v, want an empty-key error", err)
		}
	})

	t.Run("missing-publishers-key", func(t *testing.T) {
		_, err := LoadTrustAnchors(writeRawFile(t, `{}`))
		if err == nil || !strings.Contains(err.Error(), `missing required field "publishers"`) {
			t.Errorf("err = %v, want a missing-publishers error", err)
		}
	})

	t.Run("null-publishers", func(t *testing.T) {
		_, err := LoadTrustAnchors(writeRawFile(t, `{"publishers": null}`))
		if err == nil || !strings.Contains(err.Error(), `missing required field "publishers"`) {
			t.Errorf("err = %v, want a missing-publishers error", err)
		}
	})

	t.Run("oversize", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "big.json")
		if err := os.WriteFile(path, []byte(strings.Repeat("a", MaxTrustAnchorsSize+1)), 0o600); err != nil {
			t.Fatalf("write oversize file: %v", err)
		}
		_, err := LoadTrustAnchors(path)
		if err == nil || !strings.Contains(err.Error(), "exceeds the") {
			t.Errorf("err = %v, want a size-cap error", err)
		}
	})
}

// TestTrustAnchorsFromKeysInvalid asserts the in-memory constructor
// rejects empty publisher ids.
func TestTrustAnchorsFromKeysInvalid(t *testing.T) {
	pub, _ := testEd25519Keypair(t)
	_, err := TrustAnchorsFromKeys(map[string]string{
		"": base64.StdEncoding.EncodeToString(pub),
	})
	if err == nil || !strings.Contains(err.Error(), "must not be empty") {
		t.Errorf("err = %v, want an empty-id error", err)
	}
}

// TestTrustAnchorsFromKeysDeterministicErrors asserts the constructor's
// error is deterministic across runs for a store with multiple invalid
// entries: publisher ids are validated in sorted order, so the first
// error always names the lexicographically first publisher (reviewer
// finding 5).
func TestTrustAnchorsFromKeysDeterministicErrors(t *testing.T) {
	keys := map[string]string{
		"anvil-standard-zeta":  "not-base64!!",
		"anvil-standard-alpha": "also-not-base64!!",
	}

	_, err1 := TrustAnchorsFromKeys(keys)
	_, err2 := TrustAnchorsFromKeys(keys)

	if err1 == nil || err2 == nil {
		t.Fatalf("both runs must reject the store: err1=%v err2=%v", err1, err2)
	}
	if err1.Error() != err2.Error() {
		t.Errorf("errors differ across runs:\n run 1: %v\n run 2: %v", err1, err2)
	}
	if !strings.Contains(err1.Error(), "anvil-standard-alpha") {
		t.Errorf("err = %v, want the sorted-first publisher named", err1)
	}
	if strings.Contains(err1.Error(), "anvil-standard-zeta") {
		t.Errorf("err = %v, want only the sorted-first publisher named", err1)
	}
}

// writeRawFile writes raw content to a temp file and returns its path.
func writeRawFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "trust-anchors.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	return path
}

// ── Path Resolution ──────────────────────────────────────────────────

// TestResolveTrustAnchorsPath asserts the resolution order: explicit
// path wins, then the ANVIL_TRUST_ANCHORS environment variable, then the
// documented default under the Anvil global config directory (mirrors
// the static index convention, TS-014-02-02).
func TestResolveTrustAnchorsPath(t *testing.T) {
	t.Run("explicit-wins", func(t *testing.T) {
		path, err := ResolveTrustAnchorsPath("/custom/anchors.json", func(string) string {
			return "/env/anchors.json"
		})
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if path != "/custom/anchors.json" {
			t.Errorf("path = %q, want the explicit path", path)
		}
	})

	t.Run("env-wins", func(t *testing.T) {
		path, err := ResolveTrustAnchorsPath("", func(string) string {
			return "/env/anchors.json"
		})
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if path != "/env/anchors.json" {
			t.Errorf("path = %q, want the env path", path)
		}
	})

	t.Run("default", func(t *testing.T) {
		path, err := ResolveTrustAnchorsPath("", func(string) string { return "" })
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		want, err := DefaultTrustAnchorsPath()
		if err != nil {
			t.Fatalf("default path: %v", err)
		}
		if path != want {
			t.Errorf("path = %q, want default %q", path, want)
		}
		if filepath.Base(path) != DefaultTrustAnchorsFileName {
			t.Errorf("path = %q, want base %q", path, DefaultTrustAnchorsFileName)
		}
	})
}

// TestDefaultTrustAnchorsPath asserts the default path lives under the
// Anvil global config directory (ADR-005 §7.1) with the documented file
// name.
func TestDefaultTrustAnchorsPath(t *testing.T) {
	path, err := DefaultTrustAnchorsPath()
	if err != nil {
		t.Fatalf("default path: %v", err)
	}
	if filepath.Base(path) != DefaultTrustAnchorsFileName {
		t.Errorf("base = %q, want %q", filepath.Base(path), DefaultTrustAnchorsFileName)
	}
	if !strings.Contains(filepath.ToSlash(path), "/anvil/") {
		t.Errorf("path = %q, want it under the anvil config directory", path)
	}
}

// TestTrustAnchorsNilSafety asserts nil stores behave as "no anchors"
// on every store method.
func TestTrustAnchorsNilSafety(t *testing.T) {
	var anchors *TrustAnchors

	if anchors.Len() != 0 {
		t.Errorf("nil Len = %d, want 0", anchors.Len())
	}
	if anchors.Path() != "" {
		t.Errorf("nil Path = %q, want empty", anchors.Path())
	}
	if _, ok := anchors.PublicKey("anvil-standard-laravel"); ok {
		t.Error("nil PublicKey = found, want not found")
	}
}

// ── Named asset digests (TS-014-04-04) ──────────────────────────────

// resign signs the canonical attestation payload of md over its FULL
// declared digest set with priv (used after a test mutates the digest
// array, mirroring what a publisher does when producing a release).
func resign(t *testing.T, md *Metadata, priv ed25519.PrivateKey, pub ed25519.PublicKey) {
	t.Helper()
	payload, err := attestationPayload(*md)
	if err != nil {
		t.Fatalf("build canonical attestation payload: %v", err)
	}
	md.Trust.Attestation.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload))
	md.Trust.Attestation.PublicKey = base64.StdEncoding.EncodeToString(pub)
}

// testNamedBinaryDigest builds a base16 content digest entry for a
// deterministic adapter binary payload.
func testNamedBinaryDigest(name string) (ContentDigest, []byte) {
	bin := []byte("adapter binary payload for " + name + " (TS-014-04-04)")
	sum := sha256.Sum256(bin)
	return ContentDigest{
		Algorithm: DigestAlgorithmSHA256,
		Encoding:  DigestEncodingBase16,
		Digest:    hex.EncodeToString(sum[:]),
		Name:      name,
	}, bin
}

// TestVerifyTrustWithNamedAssetDigests asserts a release whose
// contentDigests carries the release-content digest PLUS attestation-
// bound digests of named binary assets verifies as valid: the named
// entries are asset-bound (verified against their downloaded assets at
// install, not against the release content), and the publisher
// attestation covers every entry — the canonical payload concatenates
// all decoded digest bytes in array order.
func TestVerifyTrustWithNamedAssetDigests(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	content := testContent()
	md := testRelease(t, content, pub, priv)

	binDigest, bin := testNamedBinaryDigest("anvil-adapter-laravel-linux-amd64")
	md.Trust.ContentDigests = append(md.Trust.ContentDigests, binDigest)
	resign(t, &md, priv, pub)

	result := VerifyTrust(md, content, testAnchors(t, pub))
	if !result.Valid {
		t.Fatalf("Valid = false with named asset digests present, Errors = %v", result.Errors)
	}
	if !result.IntegrityVerified || !result.AttestationVerified || !result.AnchorMatched {
		t.Fatalf("per-dimension flags: %+v", result)
	}
	if len(result.DeclaredDigests) != 2 {
		t.Errorf("DeclaredDigests = %d entries, want 2 (content + named binary)", len(result.DeclaredDigests))
	}

	// The named entry is verification material for its OWN asset: the
	// downloaded binary matches the attestation-bound digest.
	attested, err := VerifyAssetDigest(md, "anvil-adapter-laravel-linux-amd64", hex.EncodeToString(sha256Sum(bin)))
	if err != nil || !attested {
		t.Fatalf("VerifyAssetDigest = (%v, %v), want (true, nil)", attested, err)
	}
}

// sha256Sum returns the raw SHA-256 digest of data.
func sha256Sum(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

// TestVerifyTrustNamedAssetDigestIsNotContentDigest asserts the named
// entries are NOT compared against the release content: an entry bound
// to a binary asset whose digest differs from the content hash does not
// fail the release-content integrity check (it fails, instead, the
// asset verification of its own asset — TestVerifyAssetDigest*).
func TestVerifyTrustNamedAssetDigestIsNotContentDigest(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	content := testContent()
	md := testRelease(t, content, pub, priv)

	// A named entry whose digest is deliberately NOT the content hash
	// (it is the digest of the binary payload).
	binDigest, _ := testNamedBinaryDigest("anvil-adapter-laravel-linux-amd64")
	md.Trust.ContentDigests = append(md.Trust.ContentDigests, binDigest)
	resign(t, &md, priv, pub)

	result := VerifyTrust(md, content, testAnchors(t, pub))
	if !result.Valid {
		t.Fatalf("Valid = false, Errors = %v (named asset digests must not be compared with the release content)", result.Errors)
	}
}

// TestVerifyTrustNoReleaseContentDigestAllNamed asserts a release whose
// contentDigests entries are ALL named asset digests (no unnamed
// release-content digest) is rejected: the release content resolved from
// distribution.location would be unverifiable (ADR-022 §3).
func TestVerifyTrustNoReleaseContentDigestAllNamed(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	content := testContent()
	md := testRelease(t, content, pub, priv)

	binDigest, _ := testNamedBinaryDigest("anvil-adapter-laravel-linux-amd64")
	md.Trust.ContentDigests = []ContentDigest{binDigest}
	resign(t, &md, priv, pub)

	result := VerifyTrust(md, content, testAnchors(t, pub))
	if result.Valid {
		t.Fatal("Valid = true with no release-content digest, want false")
	}
	if result.IntegrityVerified {
		t.Error("IntegrityVerified = true with no release-content digest, want false")
	}
	if !hasMessage(result.Errors, "declares no content digest for the release content") {
		t.Errorf("Errors = %v, want a no-release-content-digest message", result.Errors)
	}
}

// TestVerifyTrustNamedDigestTamperBreaksAttestation asserts the
// attestation binds the named entries: silently editing a named digest
// (the same-channel attacker's move) invalidates the signature — the
// entry cannot be swapped without the signing key.
func TestVerifyTrustNamedDigestTamperBreaksAttestation(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	content := testContent()
	md := testRelease(t, content, pub, priv)

	binDigest, _ := testNamedBinaryDigest("anvil-adapter-laravel-linux-amd64")
	md.Trust.ContentDigests = append(md.Trust.ContentDigests, binDigest)
	resign(t, &md, priv, pub)

	// Tamper: flip one hex character of the named digest AFTER signing.
	digest := []byte(md.Trust.ContentDigests[1].Digest)
	if digest[0] == '0' {
		digest[0] = '1'
	} else {
		digest[0] = '0'
	}
	md.Trust.ContentDigests[1].Digest = string(digest)

	result := VerifyTrust(md, content, testAnchors(t, pub))
	if result.Valid {
		t.Fatal("Valid = true with a tampered named digest, want false")
	}
	if result.AttestationVerified {
		t.Error("AttestationVerified = true with a tampered named digest, want false — the signature covers the named entries")
	}
	if !hasMessage(result.Errors, "attestation signature does not verify") {
		t.Errorf("Errors = %v, want a signature failure", result.Errors)
	}
}

// TestVerifyAssetDigestMissingMaterial asserts the no-material path: a
// release without a named entry for the asset (e.g. already-published
// v1.0.0, or a release published before binary attestation) yields
// (false, nil) — the caller degrades to the same-channel checksum with
// an explicit notice; it never fails closed silently.
func TestVerifyAssetDigestMissingMaterial(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	md := testRelease(t, testContent(), pub, priv) // v1.0.0-shaped: no named entries

	attested, err := VerifyAssetDigest(md, "anvil-adapter-laravel-linux-amd64", strings.Repeat("0", 64))
	if err != nil {
		t.Fatalf("VerifyAssetDigest error on missing material: %v", err)
	}
	if attested {
		t.Fatal("attested = true for a release without named entries, want false")
	}
}

// TestVerifyAssetDigestMismatch asserts the tampered-binary path: the
// downloaded bytes do not match the attestation-bound digest — the
// install must abort with an actionable error.
func TestVerifyAssetDigestMismatch(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	content := testContent()
	md := testRelease(t, content, pub, priv)

	binDigest, _ := testNamedBinaryDigest("anvil-adapter-laravel-linux-amd64")
	md.Trust.ContentDigests = append(md.Trust.ContentDigests, binDigest)
	resign(t, &md, priv, pub)

	// The downloaded bytes are TAMPERED: a different binary payload.
	tampered := []byte("tampered binary payload (TS-014-04-04)")
	attested, err := VerifyAssetDigest(md, "anvil-adapter-laravel-linux-amd64", hex.EncodeToString(sha256Sum(tampered)))
	if err == nil {
		t.Fatal("VerifyAssetDigest = nil error for a tampered binary, want an aborting error")
	}
	if !attested {
		t.Fatal("attested = false for a tampered binary — the material exists and must be enforced")
	}
	if !strings.Contains(err.Error(), "attestation-bound digest mismatch") {
		t.Errorf("error = %v, want an attestation-bound digest mismatch message", err)
	}
	if !strings.Contains(err.Error(), "tampered") {
		t.Errorf("error = %v, want an actionable tamper message", err)
	}
}

// TestVerifyAssetDigestUndecodableEntry asserts a named entry that is
// not verification material fails the asset verification with an
// actionable message.
func TestVerifyAssetDigestUndecodableEntry(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	content := testContent()
	md := testRelease(t, content, pub, priv)

	md.Trust.ContentDigests = append(md.Trust.ContentDigests, ContentDigest{
		Algorithm: DigestAlgorithmSHA256,
		Encoding:  DigestEncodingBase16,
		Digest:    strings.ToUpper(strings.Repeat("a", 64)), // non-canonical
		Name:      "anvil-adapter-laravel-linux-amd64",
	})
	// Deliberately NOT resigned: a document carrying an undecodable entry
	// cannot be signed (attestationPayload rejects it), which is exactly
	// why the verifier re-enforces the decode defensively — structural
	// decode paths (e.g. index.go LoadIndex) bypass parse.go.
	md.Trust.Attestation = Attestation{
		Algorithm: AttestationAlgorithmEd25519,
		Signature: strings.Repeat("A", 86) + "==",
		PublicKey: base64.StdEncoding.EncodeToString(pub),
	}

	attested, err := VerifyAssetDigest(md, "anvil-adapter-laravel-linux-amd64", strings.Repeat("0", 64))
	if err == nil {
		t.Fatal("VerifyAssetDigest = nil error for an undecodable declared entry, want an error")
	}
	if !attested {
		t.Fatal("attested = false for an undecodable entry — the material exists and must be enforced")
	}
}

// ── Asset names are signed material (security review F-2) ───────────

// TestAttestationPayload_NamedEntryComposition asserts the canonical
// payload prefixes each NAMED entry's digest bytes with
// utf8(name) || 0x00 — the byte-exact composition fixed by
// registry-metadata.md §4.7 and mirrored byte-for-byte by the standard
// repositories' release pipelines.
func TestAttestationPayload_NamedEntryComposition(t *testing.T) {
	md := Metadata{
		ID:      "anvil-standard-laravel",
		Version: "1.2.3",
	}
	content := testContent()
	bin := []byte("adapter binary payload for the F-2 composition test")
	md.Trust.ContentDigests = []ContentDigest{
		{Algorithm: DigestAlgorithmSHA256, Encoding: DigestEncodingBase16, Digest: hex.EncodeToString(sha256Sum(content))},
		{
			Algorithm: DigestAlgorithmSHA256,
			Encoding:  DigestEncodingBase16,
			Digest:    hex.EncodeToString(sha256Sum(bin)),
			Name:      "anvil-adapter-laravel-linux-amd64",
		},
	}

	payload, err := attestationPayload(md)
	if err != nil {
		t.Fatalf("attestationPayload: %v", err)
	}

	want := []byte("anvil-standard-laravel")
	want = append(want, 0x00)
	want = append(want, "1.2.3"...)
	want = append(want, 0x00)
	want = append(want, sha256Sum(content)...)
	want = append(want, "anvil-adapter-laravel-linux-amd64"...)
	want = append(want, 0x00)
	want = append(want, sha256Sum(bin)...)
	if !bytes.Equal(payload, want) {
		t.Fatalf("payload composition mismatch:\n got %x\nwant %x", payload, want)
	}
}

// TestVerifyTrust_NameStripInvalidatesAttestation asserts the F-2 fix:
// stripping the name from a signed named entry (the attack that forced
// an adoption into the same-channel checksum fallback) changes the
// canonical payload, so the signature no longer verifies — the release
// is rejected, never silently downgraded.
func TestVerifyTrust_NameStripInvalidatesAttestation(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	content := testContent()
	md := testRelease(t, content, pub, priv)

	binDigest, _ := testNamedBinaryDigest("anvil-adapter-laravel-linux-amd64")
	md.Trust.ContentDigests = append(md.Trust.ContentDigests, binDigest)
	resign(t, &md, priv, pub)

	// Attacker strips the name AFTER signing: the digest bytes stay in
	// the payload, but the name prefix disappears — the signature must
	// break (and the attacker cannot re-sign without the key).
	md.Trust.ContentDigests[1].Name = ""

	result := VerifyTrust(md, content, testAnchors(t, pub))
	if result.Valid {
		t.Fatal("Valid = true with a stripped asset name, want false (F-2: the name is signed material)")
	}
	if result.AttestationVerified {
		t.Error("AttestationVerified = true with a stripped asset name, want false")
	}
	if !hasMessage(result.Errors, "attestation signature does not verify") {
		t.Errorf("Errors = %v, want a signature failure", result.Errors)
	}
	// The asset digest must also be unreachable for the stripped asset:
	// without the signed name there is no attestation-bound material.
	if attested, _ := VerifyAssetDigest(md, "anvil-adapter-laravel-linux-amd64", hex.EncodeToString(sha256Sum(binDigestBytes()))); attested {
		t.Error("VerifyAssetDigest = attested for an entry whose name was stripped")
	}
}

// binDigestBytes returns the deterministic binary payload used by
// testNamedBinaryDigest for the given name (mirrors its construction).
func binDigestBytes() []byte {
	return []byte("adapter binary payload for anvil-adapter-laravel-linux-amd64 (TS-014-04-04)")
}

// TestVerifyTrust_NameRenameInvalidatesAttestation asserts the F-2 fix
// on the cross-asset rename: renaming a signed entry (e.g. installing
// the laravel binary as flutter) changes the signed name bytes, so the
// attestation fails — identity confusion is bound out.
func TestVerifyTrust_NameRenameInvalidatesAttestation(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	content := testContent()
	md := testRelease(t, content, pub, priv)

	binDigest, _ := testNamedBinaryDigest("anvil-adapter-laravel-linux-amd64")
	md.Trust.ContentDigests = append(md.Trust.ContentDigests, binDigest)
	resign(t, &md, priv, pub)

	// Attacker renames the entry AFTER signing.
	md.Trust.ContentDigests[1].Name = "anvil-adapter-flutter-linux-amd64"

	result := VerifyTrust(md, content, testAnchors(t, pub))
	if result.Valid {
		t.Fatal("Valid = true with a renamed asset entry, want false (F-2: the name is signed material)")
	}
	if !hasMessage(result.Errors, "attestation signature does not verify") {
		t.Errorf("Errors = %v, want a signature failure", result.Errors)
	}
}

// TestVerifyTrust_NamedPayloadBackwardCompatible asserts releases
// predating binary attestation (no named entries) still verify: their
// payload composes byte-identically to the pre-F-2 composition.
func TestVerifyTrust_NamedPayloadBackwardCompatible(t *testing.T) {
	pub, priv := testEd25519Keypair(t)
	content := testContent()
	// testRelease builds the classic single unnamed digest + signature —
	// exactly the already-published v1.0.0 shape.
	md := testRelease(t, content, pub, priv)

	result := VerifyTrust(md, content, testAnchors(t, pub))
	if !result.Valid {
		t.Fatalf("Valid = false for a pre-binary-attestation release, Errors = %v", result.Errors)
	}
}
