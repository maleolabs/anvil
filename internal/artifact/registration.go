// Package artifact defines the Anvil artifact directory layout and
// packaging primitives.
//
// Reference: TS-P3-10, ADR-004 §7, EPIC-003
package artifact

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// RegistrationRecord records a single artifact registration.
//
// Registration makes a verified artifact discoverable by identity in the
// artifact registry. Only artifacts that pass all verification checks
// can be registered. The record captures the artifact's identity, version,
// checksum, verification outcome, and registration timestamp.
//
// Reference: TS-P3-10, ADR-004 §7
type RegistrationRecord struct {
	ArtifactID         string    `json:"artifact_id"`
	Version            string    `json:"version"`
	ProjectID          string    `json:"project_id"`
	Checksum           string    `json:"checksum"`
	ChecksumType       string    `json:"checksum_type"`
	VerificationResult string    `json:"verification_result"` // "passed" or error
	RegisteredAt       string    `json:"registered_at"`       // RFC 3339
	ArtifactStorePath  string    `json:"artifact_store_path,omitempty"`
	ManifestContent    *Manifest `json:"manifest_content,omitempty"`
}

// registrationIndex is a serializable representation of the RegistrationStore
// index for persistence via Save/Load.
type registrationIndex struct {
	Records map[string]*RegistrationRecord `json:"records"`
}

// RegistrationStore manages a JSON-based index of registered artifacts.
//
// The store provides thread-safe registration, lookup, and persistence.
// Registration is idempotent by artifact identity — re-registering the same
// artifact returns the existing record. Only verified artifacts (those with
// a "passed" verification result) can be registered.
//
// Reference: TS-P3-10, ADR-004 §7
type RegistrationStore struct {
	mu    sync.Mutex
	path  string
	index map[string]*RegistrationRecord // keyed by ArtifactID
}

// NewRegistrationStore creates a RegistrationStore backed by a JSON file
// at the given path. The store is initialized with an empty index. Call
// Load() to restore an existing index from disk.
//
// Reference: TS-P3-10
func NewRegistrationStore(path string) *RegistrationStore {
	return &RegistrationStore{
		path:  path,
		index: make(map[string]*RegistrationRecord),
	}
}

// Register records a verified artifact in the registration store.
//
// Returns an error if:
//   - manifest is nil
//   - verificationResult is not "passed"
//   - the current timestamp cannot be generated
//
// Registration is idempotent by artifact identity — if the artifact is
// already registered, the existing record is returned with no error.
// Registration does NOT modify the artifact file; it only records metadata.
//
// Reference: TS-P3-10, ADR-004 §7
func (rs *RegistrationStore) Register(manifest *Manifest, verificationResult string) (*RegistrationRecord, error) {
	if manifest == nil {
		return nil, fmt.Errorf("cannot register nil manifest")
	}

	if verificationResult != "passed" {
		return nil, fmt.Errorf(
			"cannot register artifact %q: verification result is %q, must be \"passed\"",
			manifest.ArtifactID, verificationResult,
		)
	}

	rs.mu.Lock()
	defer rs.mu.Unlock()

	// Idempotent: return existing record if already registered.
	if existing, ok := rs.index[manifest.ArtifactID]; ok {
		return existing, nil
	}

	record := &RegistrationRecord{
		ArtifactID:         manifest.ArtifactID,
		Version:            manifest.Version,
		ProjectID:          manifest.ProjectID,
		Checksum:           manifest.Checksum,
		ChecksumType:       manifest.ChecksumType,
		VerificationResult: verificationResult,
		RegisteredAt:       time.Now().UTC().Format(time.RFC3339),
		ManifestContent:    manifest,
	}

	rs.index[manifest.ArtifactID] = record
	return record, nil
}

// Lookup retrieves a registration record by artifact identity.
// Returns the record and true if found, or nil and false if not found.
//
// Reference: TS-P3-10
func (rs *RegistrationStore) Lookup(artifactID string) (*RegistrationRecord, bool) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	record, ok := rs.index[artifactID]
	return record, ok
}

// IsRegistered reports whether an artifact with the given identity
// has been registered.
//
// Reference: TS-P3-10
func (rs *RegistrationStore) IsRegistered(artifactID string) bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	_, ok := rs.index[artifactID]
	return ok
}

// Save persists the registration index as JSON to the store's configured
// path. The directory containing the path must already exist.
//
// Reference: TS-P3-10
func (rs *RegistrationStore) Save() error {
	rs.mu.Lock()
	idx := registrationIndex{
		Records: rs.index,
	}
	rs.mu.Unlock()

	data, err := json.Marshal(idx)
	if err != nil {
		return fmt.Errorf("marshal registration index: %w", err)
	}

	if err := os.WriteFile(rs.path, data, 0644); err != nil {
		return fmt.Errorf("write registration index to %s: %w", rs.path, err)
	}

	return nil
}

// Load restores the registration index from the JSON file at the store's
// configured path. Returns an error if the file does not exist or cannot
// be decoded.
//
// Reference: TS-P3-10
func (rs *RegistrationStore) Load() error {
	data, err := os.ReadFile(rs.path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("registration index file not found: %s", rs.path)
		}
		return fmt.Errorf("read registration index from %s: %w", rs.path, err)
	}

	var idx registrationIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return fmt.Errorf("unmarshal registration index: %w", err)
	}

	rs.mu.Lock()
	if idx.Records == nil {
		rs.index = make(map[string]*RegistrationRecord)
	} else {
		rs.index = idx.Records
	}
	rs.mu.Unlock()

	return nil
}

// List returns all registered artifact records. The returned slice is a
// copy and safe for concurrent use.
//
// Reference: TS-P3-10
func (rs *RegistrationStore) List() []*RegistrationRecord {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	records := make([]*RegistrationRecord, 0, len(rs.index))
	for _, record := range rs.index {
		records = append(records, record)
	}
	return records
}
