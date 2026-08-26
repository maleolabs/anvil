// Trust anchor store for adoption-time trust validation (TS-014-04-02;
// PM decision D-07).
//
// The in-band attestation publicKey proves key ownership — the holder of
// the matching private key signed the release — but not publisher
// identity: anyone can generate a key pair and sign a claim. Origin is
// established only against an out-of-band trust anchor: a local allowlist
// of publisher public keys that the adopting operator explicitly trusts.
//
// Default state: NO anchors are configured. Verification with no anchors
// (nil store, or a store with an empty allowlist) always fails with an
// actionable message — there is no first-use acceptance, no TOFU, and no
// privileged path for any standard (ADR-022 §3: the model applies equally
// to first-party and community standards). Every standard, first-party
// included, is verified against the same anchor allowlist.
//
// Anchor format. The store is a local JSON file, one entry per publisher:
//
//	{
//	  "publishers": {
//	    "anvil-standard-laravel": "<base64 Ed25519 public key>",
//	    "anvil-standard-flutter": "<base64 Ed25519 public key>"
//	  }
//	}
//
// The "publishers" field is REQUIRED: a file without it is rejected at
// load with an actionable error. An explicit empty allowlist
// ({"publishers": {}}) is valid and means no anchors are configured —
// verification then always fails. Unknown top-level fields are rejected:
// the format is pinned, not extensible.
//
// The publisher identity is the metadata document's id field — the
// registry metadata schema carries no separate publisher field
// (registry-metadata.schema.json: trust declares only contentDigests and
// attestation; the root declares no publisher), so the standard id is the
// publisher identity for anchor matching. Each key value is the strict
// RFC-4648 base64 (standard alphabet with padding, canonical pad bits)
// encoding of the publisher's 32-byte Ed25519 verification public key —
// the same shape parse.go enforces for trust.attestation.publicKey. A
// key that does not decode, or decodes to a length other than 32 bytes,
// rejects the whole file at load with an actionable error naming the
// publisher — a broken allowlist is never partially accepted.
//
// Path resolution mirrors the static registry index convention
// (cmd/standard_shared.go, TS-014-02-02): explicit path →
// ANVIL_TRUST_ANCHORS environment variable → default
// <user config dir>/anvil/trust-anchors.json (the ADR-005 §7.1 global
// config directory). The CLI --trust-anchors flag is wired at the
// install command surface (T-007); the resolution order is fixed here so
// the flag, the environment, and the default all resolve to the same
// store.
//
// Loading is purely local: the store is read from disk at adoption time
// — no network, no fetching. Verification material requirements for
// offline/bundled installs (T-013): the bundled release must carry the
// same trust fields (contentDigests and attestation with signature +
// publicKey), and the adopting host must carry the same operator-
// configured anchor allowlist — the anchor is local operator trust, never
// bundled inside the release being verified.
//
// Reference: TS-014-04-02, ADR-022 §3, ADR-023 §3, ADR-030, ADR-005 §7.1
package registry

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"maleolabs.com/anvil/internal/config"
)

// EnvTrustAnchors names the environment variable that overrides the
// default trust anchors file path.
//
// Reference: TS-014-04-02 (PM decision D-07; index convention TS-014-02-02)
const EnvTrustAnchors = "ANVIL_TRUST_ANCHORS"

// DefaultTrustAnchorsFileName is the trust anchors file name under the
// Anvil global config directory (ADR-005 §7.1).
const DefaultTrustAnchorsFileName = "trust-anchors.json"

// MaxTrustAnchorsSize caps the size of the trust anchors file (1 MiB).
// The allowlist holds one short base64 key per trusted publisher — a
// file beyond the cap is a misconfiguration or a broken artifact and
// fails load with a precise, actionable error instead of unbounded
// memory use (mirrors MaxIndexDocumentSize).
const MaxTrustAnchorsSize = 1 << 20

// ErrTrustAnchorsNotFound reports that the trust anchors file does not
// exist. Consumers match it with errors.Is on the error returned by
// LoadTrustAnchors.
var ErrTrustAnchorsNotFound = errors.New("trust anchors file not found")

// TrustAnchors is the out-of-band trust anchor allowlist: the set of
// publisher public keys the adopting operator explicitly trusts
// (PM decision D-07). Publisher identity is the metadata document id
// (the schema declares no separate publisher field); each id maps to the
// publisher's Ed25519 verification public key.
//
// A nil TrustAnchors, or one whose allowlist is empty, is the default
// state: no anchors configured. Verification against such a store always
// fails — origin cannot be established without an anchor (no TOFU, no
// privileged path; ADR-022 §3).
type TrustAnchors struct {
	// keys maps publisher id -> decoded verification public key.
	keys map[string]ed25519.PublicKey

	// path is the file the store was loaded from, for actionable
	// messages and auditability. Empty when the store was constructed
	// in memory.
	path string
}

// TrustAnchorsFromKeys builds an in-memory trust anchor allowlist from a
// publisher id -> base64-encoded Ed25519 public key map. Every key must
// be strict RFC-4648 base64 (standard alphabet with padding, canonical
// pad bits) decoding to exactly 32 bytes; a malformed key rejects the
// whole store with an error naming the publisher. Publisher ids must be
// non-empty. The returned store carries no source path.
//
// Publisher ids are validated in sorted order, so the error for a store
// with multiple invalid entries is deterministic across runs (reviewer
// finding 5).
//
// The primary entry point is LoadTrustAnchors (file-backed); this
// constructor exists for tests and for callers that resolve anchors from
// a non-file source.
func TrustAnchorsFromKeys(keys map[string]string) (*TrustAnchors, error) {
	anchors := &TrustAnchors{keys: make(map[string]ed25519.PublicKey, len(keys))}
	ids := make([]string, 0, len(keys))
	for id := range keys {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if id == "" {
			return nil, fmt.Errorf("trust anchors: publisher id must not be empty")
		}
		decoded, err := decodeAnchorKey(keys[id], id)
		if err != nil {
			return nil, err
		}
		anchors.keys[id] = decoded
	}
	return anchors, nil
}

// LoadTrustAnchors reads the trust anchors file at path and validates
// it:
//
//   - the file must exist (wrapped ErrTrustAnchorsNotFound) and be
//     readable;
//   - the file must not exceed MaxTrustAnchorsSize;
//   - the file must be decodable JSON declaring exactly the
//     "publishers" object; the field is REQUIRED (a file without it is
//     rejected with an actionable error — the format is pinned, not
//     extensible, and an anchor file that declares no allowlist is a
//     broken artifact, not an empty one); unknown top-level fields and
//     trailing content are rejected;
//   - every publisher id must be non-empty and every key must be strict
//     RFC-4648 base64 (standard alphabet with padding, canonical pad
//     bits — the same shape parse.go enforces for
//     trust.attestation.publicKey) decoding to exactly 32 bytes; an
//     invalid key rejects the whole file with an actionable error
//     naming the publisher.
//
// An allowlist with no publishers is valid and means: no anchors
// configured — verification against it always fails. Loading is purely
// local: the store is read from disk, no network.
func LoadTrustAnchors(path string) (*TrustAnchors, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrTrustAnchorsNotFound, path)
		}
		return nil, fmt.Errorf("trust anchors: open %s: %w", path, err)
	}
	defer f.Close()

	raw, err := io.ReadAll(io.LimitReader(f, MaxTrustAnchorsSize+1))
	if err != nil {
		return nil, fmt.Errorf("trust anchors: read %s: %w", path, err)
	}
	if len(raw) > MaxTrustAnchorsSize {
		return nil, fmt.Errorf(
			"trust anchors: %s: file exceeds the %d-byte size cap",
			path, MaxTrustAnchorsSize,
		)
	}

	var doc struct {
		// Pointer so a missing (or null) "publishers" key is
		// distinguishable from an explicitly empty allowlist.
		Publishers *map[string]string `json:"publishers"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("trust anchors: %s: not decodable JSON: %w", path, err)
	}
	if dec.More() {
		return nil, fmt.Errorf("trust anchors: %s: unexpected content after the document", path)
	}
	if doc.Publishers == nil {
		return nil, fmt.Errorf(
			"trust anchors: %s: missing required field \"publishers\" — the trust anchors file must declare the publisher allowlist as {\"publishers\": {\"<standard-id>\": \"<base64 Ed25519 public key>\"}}",
			path)
	}

	anchors, err := TrustAnchorsFromKeys(*doc.Publishers)
	if err != nil {
		return nil, fmt.Errorf("trust anchors: %s: %w", path, err)
	}
	anchors.path = path
	return anchors, nil
}

// Path returns the file the store was loaded from, or "" for an
// in-memory store. A nil store returns "".
func (a *TrustAnchors) Path() string {
	if a == nil {
		return ""
	}
	return a.path
}

// Len reports how many publishers are anchored. A nil store reports 0.
func (a *TrustAnchors) Len() int {
	if a == nil {
		return 0
	}
	return len(a.keys)
}

// PublicKey returns the anchored public key for the publisher id and
// whether the publisher has an anchor in the allowlist. A nil store
// yields ok == false.
func (a *TrustAnchors) PublicKey(id string) (ed25519.PublicKey, bool) {
	if a == nil {
		return nil, false
	}
	key, ok := a.keys[id]
	return key, ok
}

// DefaultTrustAnchorsPath returns the default trust anchors file path:
// the Anvil global config directory (ADR-005 §7.1, implemented by
// config.GlobalConfigDir — os.UserConfigDir()/anvil) plus
// trust-anchors.json. On Linux this resolves to
// ~/.config/anvil/trust-anchors.json (XDG_CONFIG_HOME aware); on macOS
// to ~/Library/Application Support/anvil/trust-anchors.json; on Windows
// to %AppData%/anvil/trust-anchors.json.
func DefaultTrustAnchorsPath() (string, error) {
	dir, err := config.GlobalConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve default trust anchors path: %w", err)
	}
	return filepath.Join(dir, DefaultTrustAnchorsFileName), nil
}

// ResolveTrustAnchorsPath resolves the trust anchors file path, in
// order:
//
//  1. the explicit path argument (non-empty);
//  2. the ANVIL_TRUST_ANCHORS environment variable (non-empty);
//  3. the documented default (DefaultTrustAnchorsPath).
//
// getenv is injected for testability. This mirrors the static index path
// convention (cmd/standard_shared.go, TS-014-02-02); the CLI
// --trust-anchors flag is the explicit path at the install command
// surface (T-007).
func ResolveTrustAnchorsPath(explicit string, getenv func(string) string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if value := getenv(EnvTrustAnchors); value != "" {
		return value, nil
	}
	return DefaultTrustAnchorsPath()
}

// decodeAnchorKey decodes and validates one anchor key: strict RFC-4648
// base64 (standard alphabet with padding) decoding to exactly 32 bytes
// (an Ed25519 public key). The pad bits must be canonical — the decoded
// bytes must re-encode to the value up to letter case — mirroring the
// parse-level check for trust.attestation.publicKey (parse.go):
// non-zero pad bits are rejected even though Go's decoder tolerates
// them.
func decodeAnchorKey(encoded, id string) (ed25519.PublicKey, error) {
	if encoded == "" {
		return nil, fmt.Errorf("publisher %q: empty public key — every anchored publisher must carry its base64 Ed25519 public key", id)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("publisher %q: public key %q is not strict RFC-4648 base64 (standard alphabet with padding): %v", id, encoded, err)
	}
	if !strings.EqualFold(base64.StdEncoding.EncodeToString(decoded), encoded) {
		return nil, fmt.Errorf("publisher %q: public key %q is not the canonical RFC-4648 base64 encoding — the pad bits must be zero", id, encoded)
	}
	if len(decoded) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("publisher %q: public key decodes to %d bytes, want exactly %d bytes (Ed25519)", id, len(decoded), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(decoded), nil
}
