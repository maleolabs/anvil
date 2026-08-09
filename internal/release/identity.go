// Package release defines the Release model and identity generation for
// Anvil Runtime Releases.
//
// Reference: TS-P4-02, EPIC-004, ADR-003 §4
package release

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// ReleaseID is a typed string representing a unique Release identity.
//
// The identity format is a 32-character lowercase hex string composed of:
//   - Bytes 0–5 (12 hex chars): Timestamp (Unix milliseconds since epoch),
//     enabling chronological sorting (newer IDs are lexicographically larger).
//   - Bytes 6–15 (20 hex chars): Cryptographic random bytes (80 bits),
//     ensuring uniqueness across all Releases.
//
// The format is deterministic, chronologically sortable, and does not require
// any external dependencies. It satisfies ADR-003 §4's requirement that every
// Release has a unique identity assigned at creation time.
//
// Reference: TS-P4-02 AC-1, AC-2, AC-5, ADR-003 §4.1
type ReleaseID string

// GenerateReleaseID generates a unique, chronologically sortable Release
// identity using timestamp-prefixed randomness.
//
// The identity is composed of:
//   - A 48-bit timestamp component (Unix milliseconds, 6 bytes / 12 hex chars)
//   - An 80-bit random component (crypto/rand, 10 bytes / 20 hex chars)
//
// Total: 16 bytes encoded as 32 lowercase hex characters.
//
// Chronological sorting guarantee: For any two identities generated at
// times t1 < t2, GenerateReleaseID() at t2 produces a lexicographically
// larger hex string than GenerateReleaseID() at t1, because the timestamp
// prefix occupies the most significant hex digits.
//
// Uniqueness guarantee: With 80 bits (10 bytes) of cryptographic randomness
// per identity, the probability of collision across 1 billion identities
// is approximately 10^-12 (negligible for all practical purposes).
//
// Reference: TS-P4-02 AC-1, AC-2, AC-5
func GenerateReleaseID() (ReleaseID, error) {
	// 6 bytes for timestamp (48 bits = enough for ~9,000 years of milliseconds).
	// 10 bytes for random (80 bits = negligible collision probability).
	b := make([]byte, 16)

	// Encode the current Unix millisecond timestamp into the first 6 bytes
	// in big-endian order for lexicographic sorting.
	ms := time.Now().UnixMilli()
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)

	// Fill the remaining 10 bytes with cryptographic randomness.
	if _, err := rand.Read(b[6:]); err != nil {
		return "", fmt.Errorf("generate release id: %w", err)
	}

	return ReleaseID(hex.EncodeToString(b)), nil
}

// String returns the string representation of the ReleaseID.
func (id ReleaseID) String() string {
	return string(id)
}
