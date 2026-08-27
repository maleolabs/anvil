// Installed-adapter migration outcomes (TS-017-01-02, T-004).
//
// Per ADR-028 §3 and Transition Plan §12.3, installed v1.x adapters are
// recognized at adoption time and migrated to the corresponding delivery
// lifecycle standard via the authoritative mapping table
// (docs/planning/ANVIL_V2_ADAPTER_STANDARD_MAPPING.md, TS-017-01-01):
// compatibility is declared, validated, and recorded — not assumed (A2).
// The migration is explicit and its OUTCOME IS RECORDED: this file
// implements the persistence component (the migration outcome record
// store) and the recognition/migration orchestration (the adoption-time
// recognition of an installed v1.x adapter for a declared framework and
// the recording of its migration outcome).
//
// Recognition mechanism (RFC-P7, resolved by TS-017-01-02). The identity
// source is config AND executable — the project's declared framework
// (project.framework, the v1.x declaration; project.standard is the
// canonical v2 key, TS-019-02-01) is matched against the mapping table
// by the adapter_name lookup key, and the installed adapter is confirmed
// through the probe-validated executable identity (closed-set discovery,
// TS-007-039 §7: a binary counts as an adapter only when it answers the
// capabilities command). The mapping table supplies the mapping data
// only (ANVIL_V2_ADAPTER_STANDARD_MAPPING §7: "The recognition mechanism
// is not decided here... This table supplies the mapping data only").
//
// Migration outcome. Two statuses are recorded, explicit and never
// assumed:
//
//   - recognized — the installed v1.x adapter was identified via the
//     mapping table and the migration is NOT complete: either the
//     corresponding standard is not installed (migration pending), or
//     the standard IS installed but its declared contract version is
//     not supported by this runtime (contract-version mismatch,
//     TS-017-01-03 — a mismatch never silently passes, ADR-028 §3).
//     In both cases the v1.x lifecycle keeps working through the
//     recognized adapter during the dual-run window (ADR-028 §12.3).
//     The outcome names the standard that completes the migration.
//   - migrated — the standard corresponding to the recognized adapter
//     IS installed (the installed-standard record, TS-014-03-03) AND
//     its declared contract version is supported by this runtime:
//     resolution switched to the standard (the standard's identity,
//     pinned version, and content are the resolution target; the
//     engine's standard-driven paths — TS-015-02-01, TS-015-02-03 —
//     resolve through the standard). The record pins the version the
//     resolution switched to and the validated declared contract
//     version.
//
// Contract-version validation at migration (TS-017-01-03, T-007). At
// migration time the mapped standard's declared contract version —
// read from the standard's installed-standard record (the declaration
// recorded at install from the standard's registry metadata document,
// registry-metadata §4.3) — is validated against the contract version
// the runtime supports (ValidateMigrationContractVersion): the
// declared version must be well-formed semver and its major must be in
// the runtime's supported contract major set (ADR-024 §3.1, §3.4; the
// set is read from the compatibility matrix by the caller, never
// hardcoded). A valid match completes the migration (status migrated,
// the contract_version seam filled with the validated declared
// version). A mismatch NEVER silently passes: the outcome is recorded
// as recognized — the migration did not complete — with the declared
// contract version recorded in the contract_version seam so the
// mismatch is observable and auditable, and the actionable reasons are
// surfaced in the recognition result. Both outcomes — match and
// mismatch — are recorded (ADR-024 §3.6: declared, validated, and
// recorded, not assumed).
//
// Store layout. Records live one per adapter under a directory
// following the ADR-005 §7.1 global config convention:
//
//	<config dir>/anvil/adapter-migrations/<adapter-name>.json
//
// Per-adapter files mirror the installed-standard record store
// (record.go, TS-014-03-03): recovery (one corrupt record never kills
// the store), concurrency (different adapters never contend), and
// atomicity (temp file + rename + directory fsync) are inherited from
// the same layout decision.
//
// Scope. Recognition covers the first-party v1.x adapters (the rows of
// the mapping table); third-party adapters have no mapping row and are
// out of scope (ANVIL_V2_ADAPTER_STANDARD_MAPPING §7). The store never
// modifies project state: anvil.yaml (project.framework) is preserved
// as-is — the migration is a resolution behavior, not a config rewrite
// (the config key rename is a separate governed decision, ADR-032 /
// TS-019-02-01).
//
// Reference: TS-017-01-02, TS-017-01-01 §7, ADR-028 §3, §12.3,
// ADR-005 §7.1, ADR-022 §3
package registry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"maleolabs.com/anvil/internal/config"
)

// DefaultAdapterMigrationsDirName is the migration outcome record store
// directory name under the Anvil global config directory (ADR-005 §7.1).
const DefaultAdapterMigrationsDirName = "adapter-migrations"

// MaxMigrationRecordSize caps the size of a single migration outcome
// record file (1 MiB). Records hold one adapter's outcome — a record
// beyond the cap is a broken artifact and fails with a precise,
// actionable error instead of unbounded memory use (mirrors
// MaxRecordSize).
const MaxMigrationRecordSize = 1 << 20

// MigrationRecordFormatVersion is the version of the migration outcome
// record format (TS-017-01-02). The record format is pinned: every
// record carries the version it was written with, and reads accept
// exactly MigrationRecordFormatVersion. The contract-version seam
// (ContractVersion, filled by TS-017-01-03) is part of the pinned
// format; changing its shape is a format change.
const MigrationRecordFormatVersion = 1

// Migration outcome statuses. Exactly one status is recorded per
// outcome, explicit and never assumed (ADR-028 A2).
const (
	// MigrationStatusRecognized records that the installed v1.x adapter
	// was identified via the mapping table and the migration is NOT
	// complete: the corresponding standard is not installed (migration
	// pending — nothing to validate yet), or it IS installed but its
	// declared contract version is not supported by this runtime
	// (contract-version mismatch, TS-017-01-03 — a mismatch never
	// silently passes, ADR-028 §3). In both cases the v1.x lifecycle
	// keeps working through the recognized adapter during the dual-run
	// window (ADR-028 §12.3).
	MigrationStatusRecognized = "recognized"

	// MigrationStatusMigrated records that the standard corresponding
	// to the recognized adapter IS installed and its declared contract
	// version is supported by this runtime: resolution switched to
	// the standard (the record pins the version and the validated
	// declared contract version).
	MigrationStatusMigrated = "migrated"
)

// Sentinel errors for the migration outcome store. Consumers match them
// with errors.Is on the wrapped errors returned by the store methods.
var (
	// ErrMigrationRecordNotFound reports that no migration outcome
	// record exists for the requested adapter name.
	ErrMigrationRecordNotFound = errors.New("adapter migration outcome record not found")

	// ErrMigrationRecordCorrupt reports that the record file for the
	// adapter cannot be read as a migration outcome record.
	ErrMigrationRecordCorrupt = errors.New("adapter migration outcome record corrupt")

	// ErrMigrationRecordInvalid reports that a migration outcome record
	// is structurally invalid for writing: unsafe record key, wrong
	// format version, missing core fields, an unknown status, or a
	// record whose state contradicts its status.
	ErrMigrationRecordInvalid = errors.New("adapter migration outcome record invalid")
)

// MigrationOutcome is the recorded outcome of one installed v1.x
// adapter recognition/migration (TS-017-01-02, ADR-028 §12.3): the
// adapter identity (the mapping lookup keys), the standard the
// recognition mapped it to, the recorded status, the contract-version
// validation outcome at migration (TS-017-01-03), and when the outcome
// was recorded.
//
// The contract_version field is the TS-017-01-03 seam: contract-version
// VALIDATION at migration is implemented here — the field records the
// declared contract version of the standard that was checked (match or
// mismatch), never an invented value.
type MigrationOutcome struct {
	// FormatVersion is the pinned record format version
	// (MigrationRecordFormatVersion).
	FormatVersion int `json:"formatVersion"`

	// AdapterName is the recognized adapter identity — the mapping
	// row's adapter_name lookup key (the v1.x adapter identifier).
	AdapterName string `json:"adapterName"`

	// AdapterExecutable is the recognized adapter's executable name —
	// the mapping row's adapter_executable lookup key
	// (anvil-adapter-<name>).
	AdapterExecutable string `json:"adapterExecutable"`

	// StandardID is the delivery lifecycle standard the adapter maps to
	// (the mapping row's standard_id) — the migration target.
	StandardID string `json:"standardId"`

	// Framework is the framework the standard carries (the mapping
	// row's framework — the natural-language anchor of the row).
	Framework string `json:"framework"`

	// Status is the recorded migration outcome status
	// (MigrationStatusRecognized | MigrationStatusMigrated), explicit
	// and never assumed (ADR-028 A2).
	Status string `json:"status"`

	// ResolvedAt is when the outcome state was recorded (the adoption
	// time of the recognition).
	ResolvedAt time.Time `json:"resolvedAt"`

	// StandardVersion is the pinned version of the standard the
	// resolution switched to. Set exactly when Status is migrated; empty
	// for recognized (the standard is not installed — there is no
	// version to pin).
	StandardVersion string `json:"standardVersion,omitempty"`

	// ContractVersion is the declared contract version of the standard
	// checked at migration validation (TS-017-01-03): the contract
	// version recorded in the installed-standard record of the mapped
	// standard (the declaration recorded at install from the standard's
	// registry metadata document, registry-metadata §4.3). Filled when
	// a declared version is available at migration time (the standard
	// is installed): on a valid match the outcome is recorded as
	// migrated with the validated version pinned; on a mismatch the
	// outcome is recorded as recognized (the migration did not
	// complete — never silent acceptance) with the declared version
	// recorded so the mismatch is observable and auditable. Empty when
	// no standard is installed at migration time — nothing is declared
	// yet, there is no version to check.
	ContractVersion string `json:"contractVersion,omitempty"`
}

// AdapterMigrationStore is a file-backed store of migration outcome
// records (TS-017-01-02): one JSON record file per adapter name under
// the Anvil global config directory (ADR-005 §7.1), mirroring the
// installed-standard record store (record.go). Records are pure file
// persistence — they survive restarts and are readable by downstream
// flows. The store is a persistence component only: it never probes
// adapters, never resolves standards, and never interprets the mapping
// table — recognition and orchestration live in
// RecognizeInstalledAdapter, which consumes this store.
type AdapterMigrationStore struct {
	// dir is the store directory holding the per-adapter record files.
	dir string
}

// NewAdapterMigrationStore creates a migration outcome record store
// rooted at dir. dir must be a directory path (typically
// DefaultAdapterMigrationsDir); it is created on the first write. A
// missing dir is not an error: it simply means no outcomes are recorded
// yet.
func NewAdapterMigrationStore(dir string) *AdapterMigrationStore {
	return &AdapterMigrationStore{dir: dir}
}

// DefaultAdapterMigrationsDir returns the default migration outcome
// record store directory: the Anvil global config directory (ADR-005
// §7.1, implemented by config.GlobalConfigDir — os.UserConfigDir()/
// anvil) plus adapter-migrations. On Linux this resolves to
// ~/.config/anvil/adapter-migrations (XDG_CONFIG_HOME aware).
func DefaultAdapterMigrationsDir() (string, error) {
	dir, err := config.GlobalConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve default adapter-migrations directory: %w", err)
	}
	return filepath.Join(dir, DefaultAdapterMigrationsDirName), nil
}

// Dir returns the store directory.
func (s *AdapterMigrationStore) Dir() string {
	return s.dir
}

// Record records a migration outcome (TS-017-01-02). The record key is
// the adapter name (the record file name); the recorded STATE is the
// mapping identity (standard_id, adapter_executable) plus the status,
// the pinned standard version, and the validated declared contract
// version (TS-017-01-03 — the contract-version validation outcome is
// part of the recorded state). Re-recording the same state is an
// idempotent success — the existing record is returned unchanged and
// created is false; nothing is rewritten. Any state change — a status
// transition (e.g. recognized → migrated once the standard is
// installed and its declared contract version validated), a declared
// contract version change (e.g. a mismatch re-validated against a
// corrected standard release), or a mapping-table change that remaps
// the adapter to a different standard or executable
// (ANVIL_V2_ADAPTER_STANDARD_MAPPING §7: the table is the
// authoritative, maintained mapping) — replaces the record atomically
// and reports created, so the recorded outcome never stays silently
// stale. A corrupt existing record cannot be compared; recording over
// it replaces it with the new write (recovery by re-recognition).
//
// The record is validated structurally before writing
// (validateMigrationOutcome): a safe adapter-name key, a matching
// adapter name, the exact format version, the core fields, a known
// status, and the status/state coherence (migrated pins a standard
// version; recognized does not). An invalid record fails with wrapped
// ErrMigrationRecordInvalid.
//
// The first write creates the store directory. The write is atomic
// (temp file + rename + directory fsync): a crash never leaves a torn
// record file.
func (s *AdapterMigrationStore) Record(adapterName string, outcome MigrationOutcome) (MigrationOutcome, bool, error) {
	if err := validateMigrationOutcome(adapterName, outcome); err != nil {
		return MigrationOutcome{}, false, err
	}

	existing, err := s.read(adapterName)
	switch {
	case err == nil:
		if existing.Status == outcome.Status &&
			existing.StandardVersion == outcome.StandardVersion &&
			existing.StandardID == outcome.StandardID &&
			existing.AdapterExecutable == outcome.AdapterExecutable &&
			existing.ContractVersion == outcome.ContractVersion {
			// Idempotent success: the same outcome state — mapping
			// identity (standard_id, adapter_executable), status, pinned
			// version, and the validated declared contract version
			// (TS-017-01-03) — is already recorded; return the existing
			// record unchanged (the outcome is re-confirmed on every
			// adoption, not re-churned). A state change — a status
			// transition, a declared contract version change, or a
			// mapping-table remap — is recorded as a change, never
			// silently stale.
			return existing, false, nil
		}
	case errors.Is(err, ErrMigrationRecordCorrupt):
		// The corrupt record cannot be compared; the explicit
		// recognition replaces it (recovery by re-recognition).
	case errors.Is(err, ErrMigrationRecordNotFound):
		// Fresh record.
	default:
		return MigrationOutcome{}, false, err
	}

	if err := s.write(adapterName, outcome); err != nil {
		return MigrationOutcome{}, false, err
	}
	return outcome, true, nil
}

// Get returns the migration outcome record for the adapter name, or an
// actionable error when there is none (wrapped ErrMigrationRecordNotFound)
// or when the record file cannot be read as an outcome record (wrapped
// ErrMigrationRecordCorrupt naming the file). Downstream flows resolve
// recorded migration outcomes against this read.
func (s *AdapterMigrationStore) Get(adapterName string) (MigrationOutcome, error) {
	if adapterName == "" || !recordIDPattern.MatchString(adapterName) {
		return MigrationOutcome{}, fmt.Errorf(
			"%w: %q is not a safe record key — the adapter name must match ^[a-z0-9][a-z0-9-]*$ and be at most 64 characters (the name is the record file name)",
			ErrMigrationRecordInvalid, adapterName)
	}
	return s.read(adapterName)
}

// List returns every recorded migration outcome sorted by adapter name,
// plus the corrupt record files that were skipped (mirroring
// InstalledStandardStore.List, TS-014-03-03): a corrupt record file is
// reported and skipped — it never fails the whole listing. A missing or
// unreadable store directory yields no records and no error (an empty
// store is a valid state).
func (s *AdapterMigrationStore) List() ([]MigrationOutcome, []CorruptRecord, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, nil // no store directory yet — no outcomes recorded
		}
		return nil, nil, fmt.Errorf("adapter migration store: read store directory %s: %w", s.dir, err)
	}

	var outcomes []MigrationOutcome
	var corrupt []CorruptRecord
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name, ok := strings.CutSuffix(entry.Name(), ".json")
		if !ok {
			continue // not a record file
		}
		rec, err := s.read(name)
		if err != nil {
			if errors.Is(err, ErrMigrationRecordCorrupt) {
				corrupt = append(corrupt, CorruptRecord{
					Path:  filepath.Join(s.dir, entry.Name()),
					Error: err.Error(),
				})
				continue
			}
			return nil, nil, err
		}
		outcomes = append(outcomes, rec)
	}
	sort.Slice(outcomes, func(i, j int) bool {
		return outcomes[i].AdapterName < outcomes[j].AdapterName
	})
	return outcomes, corrupt, nil
}

// read returns the migration outcome record for adapterName, classifying
// the file BEFORE decoding (mirroring InstalledStandardStore.read): a
// symlink, a directory, oversize, not decodable JSON, unknown fields,
// an unsupported format version, or a structurally invalid record is
// wrapped ErrMigrationRecordCorrupt naming the file and the reason.
func (s *AdapterMigrationStore) read(adapterName string) (MigrationOutcome, error) {
	path := filepath.Join(s.dir, adapterName+".json")

	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return MigrationOutcome{}, fmt.Errorf("%w: %s", ErrMigrationRecordNotFound, path)
		}
		return MigrationOutcome{}, fmt.Errorf(
			"adapter migration store: stat %s: %w", path, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return MigrationOutcome{}, fmt.Errorf(
			"%w: %s is a symlink — symlinked record files are not supported (the store is a plain file store, mirroring the installed-standard store); delete the symlink or re-run the recognition to recover",
			ErrMigrationRecordCorrupt, path)
	}
	if info.IsDir() {
		return MigrationOutcome{}, fmt.Errorf(
			"%w: %s is a directory, not a record file — delete it or re-run the recognition to recover",
			ErrMigrationRecordCorrupt, path)
	}

	f, err := os.Open(path)
	if err != nil {
		return MigrationOutcome{}, fmt.Errorf(
			"adapter migration store: open %s: %w", path, err)
	}
	defer f.Close()

	raw, err := io.ReadAll(io.LimitReader(f, MaxMigrationRecordSize+1))
	if err != nil {
		return MigrationOutcome{}, fmt.Errorf(
			"adapter migration store: read %s: %w", path, err)
	}
	if len(raw) > MaxMigrationRecordSize {
		return MigrationOutcome{}, fmt.Errorf(
			"%w: %s: record exceeds the %d-byte size cap — the file is a broken artifact; delete the record or re-run the recognition to recover",
			ErrMigrationRecordCorrupt, path, MaxMigrationRecordSize)
	}

	var outcome MigrationOutcome
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&outcome); err != nil {
		return MigrationOutcome{}, fmt.Errorf(
			"%w: %s: not decodable as a migration outcome record: %v — delete the record or re-run the recognition to recover",
			ErrMigrationRecordCorrupt, path, err)
	}
	if dec.More() {
		return MigrationOutcome{}, fmt.Errorf(
			"%w: %s: unexpected content after the record document — delete the record or re-run the recognition to recover",
			ErrMigrationRecordCorrupt, path)
	}

	if err := validateMigrationOutcome(adapterName, outcome); err != nil {
		return MigrationOutcome{}, fmt.Errorf(
			"%w: %s: %v — delete the record or re-run the recognition to recover",
			ErrMigrationRecordCorrupt, path, err)
	}
	return outcome, nil
}

// write atomically writes the migration outcome record for adapterName:
// marshal to a hidden temp file in the store directory, fsync, rename
// over the record file, and fsync the store directory so the rename
// itself is durable. The store directory is created on first write. A
// crash never leaves a torn or partial record file.
func (s *AdapterMigrationStore) write(adapterName string, outcome MigrationOutcome) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("adapter migration store: create store directory %s: %w", s.dir, err)
	}

	raw, err := json.MarshalIndent(outcome, "", "  ")
	if err != nil {
		return fmt.Errorf("adapter migration store: encode record %q: %w", adapterName, err)
	}
	if len(raw) > MaxMigrationRecordSize {
		return fmt.Errorf(
			"%w: record for adapter %q is %d bytes, exceeding the %d-byte cap — the record would be unreadable by the store's own read path",
			ErrMigrationRecordInvalid, adapterName, len(raw), MaxMigrationRecordSize)
	}
	raw = append(raw, '\n')

	path := filepath.Join(s.dir, adapterName+".json")
	tmp, err := os.CreateTemp(s.dir, ".tmp-"+adapterName+"-*.json")
	if err != nil {
		return fmt.Errorf("adapter migration store: create temp file in %s: %w", s.dir, err)
	}
	tmpName := tmp.Name()
	// On success the temp file has been renamed away and Remove is a
	// harmless no-op; on failure it cleans up the partial temp file.
	defer os.Remove(tmpName)

	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("adapter migration store: write %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("adapter migration store: sync %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("adapter migration store: close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("adapter migration store: replace %s: %w", path, err)
	}
	if err := s.syncDir(); err != nil {
		return fmt.Errorf(
			"adapter migration store: replace %s completed, but the store directory could not be synced: %w (the record content is in place; durability of the rename could not be confirmed)",
			path, err)
	}
	return nil
}

// syncDir flushes the store directory's metadata after a rename, so the
// directory entry itself is durable — not just the record content
// (mirrors InstalledStandardStore.syncDir). Filesystems that do not
// support directory fsync (some tmpfs/network mounts) report EINVAL or
// ENOTSUP — tolerated, since the rename itself is still atomic.
func (s *AdapterMigrationStore) syncDir() error {
	d, err := os.Open(s.dir)
	if err != nil {
		return fmt.Errorf("open store directory %s: %w", s.dir, err)
	}
	defer d.Close()

	if err := d.Sync(); err != nil {
		if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) {
			return nil // the filesystem does not support directory fsync
		}
		return fmt.Errorf("sync store directory %s: %w", s.dir, err)
	}
	return nil
}

// validateMigrationOutcome checks the structural shape of a migration
// outcome record for write and read: the key is a safe file name
// (mirrors recordIDPattern), the record's adapter name matches the key,
// the format version is exactly MigrationRecordFormatVersion, the core
// identity fields are present, the status is one of the two known
// statuses, and the state is coherent with the status (migrated pins a
// standard version; recognized does not). Validation is structural only
// — no mapping lookups, no standard resolution, no policy (those belong
// to RecognizeInstalledAdapter).
func validateMigrationOutcome(adapterName string, outcome MigrationOutcome) error {
	var problems []string
	if adapterName == "" {
		problems = append(problems, "adapter name must not be empty")
	} else if !recordIDPattern.MatchString(adapterName) {
		problems = append(problems, fmt.Sprintf("adapter name %q is not a safe record key (must match ^[a-z0-9][a-z0-9-]*$ and be at most 64 characters)", adapterName))
	}
	if outcome.FormatVersion != MigrationRecordFormatVersion {
		problems = append(problems, fmt.Sprintf("format version %d is not the supported %d", outcome.FormatVersion, MigrationRecordFormatVersion))
	}
	if outcome.AdapterName != adapterName {
		problems = append(problems, fmt.Sprintf("record adapter name %q does not match the record key %q", outcome.AdapterName, adapterName))
	}
	if outcome.AdapterExecutable == "" {
		problems = append(problems, "adapter_executable must not be empty (the mapping lookup key, ANVIL_V2_ADAPTER_STANDARD_MAPPING §7)")
	}
	if outcome.StandardID == "" {
		problems = append(problems, "standard_id must not be empty (the migration target, ANVIL_V2_ADAPTER_STANDARD_MAPPING §7)")
	}
	if outcome.Framework == "" {
		problems = append(problems, "framework must not be empty (the mapping row's framework anchor)")
	}
	switch outcome.Status {
	case MigrationStatusRecognized:
		if outcome.StandardVersion != "" {
			problems = append(problems, "status \"recognized\" must not pin a standard version — the standard is not installed (no version to pin)")
		}
	case MigrationStatusMigrated:
		if outcome.StandardVersion == "" {
			problems = append(problems, "status \"migrated\" must pin the standard version the resolution switched to")
		}
	default:
		problems = append(problems, fmt.Sprintf("status %q is not a known migration outcome status (recognized | migrated)", outcome.Status))
	}
	if outcome.ResolvedAt.IsZero() {
		problems = append(problems, "resolvedAt must be set (when the outcome was recorded)")
	}

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrMigrationRecordInvalid, strings.Join(problems, "; "))
}

// ── Contract-version validation at migration (TS-017-01-03) ──────────

// MigrationContractValidationResult is the outcome of contract-version
// validation at migration (TS-017-01-03; ADR-024 §3.1, §3.4): whether
// the declared contract version of the mapped standard targets a
// contract major the runtime supports, the declared version and the
// supported set the check ran against (recorded for auditability —
// declared, validated, and recorded, never assumed, A2), and the
// actionable reasons when it does not.
type MigrationContractValidationResult struct {
	// DeclaredContractVersion is the declared contract version that was
	// checked (from the mapped standard's installed-standard record).
	DeclaredContractVersion string `json:"declaredContractVersion"`

	// Compatible reports whether the declared contract version is
	// present, well-formed semver, and targets a supported contract
	// major (ADR-024 §3.1, §3.4).
	Compatible bool `json:"compatible"`

	// SupportedContractMajors is the runtime's supported contract major
	// set the declaration was checked against.
	SupportedContractMajors []int `json:"supportedContractMajors"`

	// Errors lists every rejection reason found, each actionable: what
	// failed and how to resolve it. Empty when Compatible is true.
	Errors []string `json:"errors,omitempty"`
}

// ValidateMigrationContractVersion validates a standard's declared
// contract version at migration time (TS-017-01-03): the declared
// version — read from the mapped standard's installed-standard record —
// must be present, well-formed semver, and target a supported contract
// major (ADR-024 §3.1: the contract major version is the unit of
// compatibility; §3.4: the supported set). The supported majors are
// supplied by the caller — read from the compatibility matrix record,
// the corpus reference declared contract versions are checked against
// (ADR-029 §3); the engine never hardcodes them.
//
// A mismatch is never a Go error: it is reported in the result with
// Compatible=false and one actionable message per rejection reason, so
// the caller surfaces it at migration and records the outcome — never
// silent acceptance (ADR-028 §3; ADR-024 §3.6). Malformed declared
// values are rejection reasons, not Go errors — the validation's job is
// exactly to produce the actionable record. The result carries the
// declared value and the checked-against set for auditability.
//
// Reference: TS-017-01-03, ADR-024 §3.1, §3.4, ADR-028 §3, §12.3
func ValidateMigrationContractVersion(declared string, supportedMajors []int, label string) MigrationContractValidationResult {
	result := MigrationContractValidationResult{
		DeclaredContractVersion: declared,
		// The result is surfaced and recorded for auditability; copy the
		// input slice so later caller mutation cannot rewrite what was
		// validated.
		SupportedContractMajors: append([]int(nil), supportedMajors...),
	}

	if declared == "" {
		result.Errors = append(result.Errors, fmt.Sprintf(
			"standard %q is installed but its record declares no contract version; a standard that does not declare compatibility is rejected (PRD-002 §5.8). Update the standard to a release whose registry metadata document declares the target contract version (registry-metadata §4.3).",
			label))
		return result
	}

	if !contractVersionPattern.MatchString(declared) {
		result.Errors = append(result.Errors, fmt.Sprintf(
			"standard %q declares contract version %q, which is not well-formed semver (expected major.minor.patch without leading zeros, e.g. \"1.0.0\"). Update the standard to a release whose declared contract version is well-formed (registry-metadata §4.3).",
			label, declared))
		return result
	}

	major, ok := semverMajor(declared)
	if !ok {
		result.Errors = append(result.Errors, fmt.Sprintf(
			"standard %q declares contract version %q, whose major overflows the supported range; contract majors are compared numerically and this value cannot be represented. Migrate the standard to a supported contract major.",
			label, declared))
		return result
	}

	if len(supportedMajors) == 0 {
		result.Errors = append(result.Errors, fmt.Sprintf(
			"standard %q declares contract version %q targeting contract major %d, but the runtime declares no supported contract majors; the migration cannot be validated. Declare the supported contract major(s) from the version line (ADR-024 §3.4).",
			label, declared, major))
		return result
	}

	if !containsMajor(supportedMajors, major) {
		result.Errors = append(result.Errors, fmt.Sprintf(
			"standard %q declares contract version %q targeting contract major %d, which the runtime does not support (supported contract major(s): %s; ADR-024 §3.4). Update the standard to a release declaring a supported contract version, or upgrade the runtime.",
			label, declared, major, FormatContractMajors(supportedMajors)))
		return result
	}

	result.Compatible = true
	return result
}

// FormatContractMajors renders a supported contract major set for
// actionable messages, e.g. "[1, 2]".
func FormatContractMajors(supported []int) string {
	parts := make([]string, len(supported))
	for i, m := range supported {
		parts[i] = strconv.Itoa(m)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// ── Recognition and migration orchestration (TS-017-01-02) ───────────

// RecognitionResult is the outcome of one adoption-time recognition
// (RecognizeInstalledAdapter): whether an installed v1.x adapter was
// recognized for the declared framework, the mapping row that
// identified it, the contract-version validation outcome at migration
// (TS-017-01-03), the migration outcome recorded, and whether a NEW
// outcome state was persisted.
type RecognitionResult struct {
	// Recognized reports whether an installed v1.x adapter was
	// identified for the declared framework: the framework matched a
	// mapping row (adapter_name) AND a probe-validated adapter binary
	// is installed on the system.
	Recognized bool

	// Row is the mapping row that identified the adapter (zero when
	// not recognized).
	Row AdapterMappingRow

	// ContractVersionValidated reports whether contract-version
	// validation ran at migration (TS-017-01-03): a declared contract
	// version was available — the mapped standard IS installed (the
	// declaration recorded in its installed-standard record). False
	// when the standard is not installed: nothing is declared at
	// migration time, and the outcome is recorded without a contract
	// version.
	ContractVersionValidated bool

	// ContractVersionCompatible reports whether the declared contract
	// version targets a supported contract major (ADR-024 §3.1, §3.4).
	// Meaningful only when ContractVersionValidated is true; when
	// false, ContractVersionErrors carries the actionable reasons and
	// the recorded outcome does NOT complete the migration (never
	// silent acceptance — ADR-028 §3).
	ContractVersionCompatible bool

	// ContractVersionErrors lists the actionable rejection reasons of
	// the migration contract-version validation. Empty on a valid
	// match and when nothing was validated.
	ContractVersionErrors []string

	// Outcome is the migration outcome recorded (zero when not
	// recognized).
	Outcome MigrationOutcome

	// Recorded reports whether a NEW outcome state was persisted (the
	// first record, or a state change such as recognized → migrated or
	// a declared contract version change). False when the same state
	// was already recorded (re-confirmed, not re-churned) or when
	// nothing was recognized.
	Recorded bool
}

// RecognizeInstalledAdapter is the adoption-time recognition and
// migration of an installed v1.x adapter (TS-017-01-02, ADR-028 §3,
// §12.3): when a project with a declared framework and an installed
// v1.x adapter is used, the runtime identifies the installed adapter
// and maps it to the corresponding delivery lifecycle standard via the
// authoritative mapping table (TS-017-01-01), switches resolution to
// the standard while preserving project state and lifecycle behavior,
// and RECORDS the migration outcome (explicit, never assumed — A2).
//
// Inputs:
//
//   - framework: the project's declared framework (project.framework —
//     the v1.x declaration honored during the deprecation window;
//     RFC-P7: recognition keys on the config declaration);
//   - installed: the probe-validated installed adapter set (name →
//     executable path) from closed-set discovery (TS-007-039 §7 — a
//     binary counts as an adapter only when it answers the capabilities
//     command; RFC-P7: recognition confirms through the executable
//     identity);
//   - mapping: the parsed authoritative mapping table (LoadAdapterMapping);
//   - standards: the installed-standard record store (TS-014-03-03) —
//     the resolution target state and the source of the standard's
//     declared contract version at migration;
//   - outcomes: the migration outcome record store (this file);
//   - supportedMajors: the runtime's supported contract major set for
//     migration validation (TS-017-01-03), read from the compatibility
//     matrix record by the caller (ADR-029 §3 — the engine never
//     hardcodes it; a nil or empty set fails validation fail-closed:
//     the engine still RECORDS the outcome — recognized with the
//     mismatch — rather than inventing supported majors; it never
//     silently completes the migration). The cmd layer skips
//     recognition entirely when the matrix is unreadable, so this
//     engine-level case is reached only by direct callers;
//   - now: the clock, injected for testability.
//
// Recognition rule. The declared framework is matched against the
// mapping table by the adapter_name lookup key (the §7 contract of
// ANVIL_V2_ADAPTER_STANDARD_MAPPING); a framework with no mapping row
// is not a first-party adapter identity and is NOT recognized
// (third-party adapters are out of scope, §7). A matching row only
// recognizes when a probe-validated adapter binary for the framework is
// installed — recognition is never assumed from a declaration alone.
// Project state is preserved: anvil.yaml is never modified; the
// migration is a resolution behavior recorded in the outcome store.
//
// Recorded outcome. When recognized, the outcome status is migrated
// when the standard's installed-standard record exists AND its declared
// contract version is supported by this runtime — the resolution
// switched to the standard (the record pins the version and the
// validated declared contract version); or recognized when it does not
// (migration pending; the v1.x lifecycle keeps working through the
// recognized adapter during the dual-run window, ADR-028 §12.3).
//
// Contract-version validation at migration (TS-017-01-03). When the
// mapped standard IS installed, its declared contract version (the
// contractVersion recorded in the installed-standard record at install
// from the standard's registry metadata document, registry-metadata
// §4.3) is validated against supportedMajors
// (ValidateMigrationContractVersion; ADR-024 §3.1, §3.4). A valid
// match completes the migration. A mismatch NEVER silently passes: the
// outcome is recorded as recognized (the migration did not complete)
// with the declared contract version recorded for auditability, and the
// actionable reasons are surfaced in the recognition result.
//
// Failures. A store that cannot answer (corrupt standard record,
// unreadable stores) is a real failure returned as-is — recognition is
// explicit, never silently skipped. The mapping itself is loaded by the
// caller (LoadAdapterMapping), which returns actionable errors for a
// missing or invalid artifact.
func RecognizeInstalledAdapter(framework string, installed map[string]string, mapping *AdapterMapping, standards *InstalledStandardStore, outcomes *AdapterMigrationStore, supportedMajors []int, now func() time.Time) (RecognitionResult, error) {
	// No declaration: nothing to recognize (RFC-P7 — config identity).
	if framework == "" {
		return RecognitionResult{}, nil
	}
	// The declared framework must be a first-party adapter identity in
	// the authoritative mapping (adapter_name lookup key, §7).
	row, ok := mapping.LookupByAdapterName(framework)
	if !ok {
		return RecognitionResult{}, nil
	}
	// A probe-validated adapter binary must actually be installed —
	// recognition is never assumed from the declaration alone
	// (RFC-P7 — executable identity; TS-007-039 §7).
	executable, ok := installed[framework]
	if !ok || executable == "" {
		return RecognitionResult{}, nil
	}
	// The probed executable's file name must be the mapping row's
	// adapter_executable: closed-set discovery only detects binaries
	// named anvil-adapter-<name>, and the row's adapter_executable is
	// that name (§4, §7). A mismatch means the recognized binary is not
	// the executable identity the mapping row describes — not
	// recognized (the mapping is the authoritative identity source).
	if filepath.Base(executable) != row.AdapterExecutable {
		return RecognitionResult{}, nil
	}

	// The migration target state: is the standard the adapter maps to
	// installed? Resolution switched to the standard only when its
	// installed-standard record exists (TS-014-03-03); otherwise the
	// migration is pending and the v1.x lifecycle keeps working through
	// the recognized adapter (ADR-028 §12.3). When the standard IS
	// installed, its declared contract version (the contractVersion
	// recorded in the installed-standard record at install, from the
	// standard's registry metadata document — registry-metadata §4.3)
	// is validated against the runtime's supported contract majors
	// (TS-017-01-03; ADR-024 §3.1, §3.4): a valid match completes the
	// migration; a mismatch NEVER silently passes — the migration does
	// not complete and the mismatch is recorded and surfaced.
	status := MigrationStatusRecognized
	standardVersion := ""
	contractVersion := ""
	validated := false
	compatible := false
	var validationErrors []string
	if rec, err := standards.Get(row.StandardID); err == nil {
		validated = true
		validation := ValidateMigrationContractVersion(rec.ContractVersion, supportedMajors, row.StandardID)
		contractVersion = rec.ContractVersion
		compatible = validation.Compatible
		validationErrors = validation.Errors
		if compatible {
			status = MigrationStatusMigrated
			standardVersion = rec.Version
		}
	} else if !errors.Is(err, ErrRecordNotFound) {
		return RecognitionResult{}, fmt.Errorf(
			"adapter recognition: cannot resolve the installed state of standard %q for adapter %q: %w",
			row.StandardID, row.AdapterName, err)
	}

	outcome := MigrationOutcome{
		FormatVersion:     MigrationRecordFormatVersion,
		AdapterName:       row.AdapterName,
		AdapterExecutable: row.AdapterExecutable,
		StandardID:        row.StandardID,
		Framework:         row.Framework,
		Status:            status,
		ResolvedAt:        now().UTC(),
		StandardVersion:   standardVersion,
		ContractVersion:   contractVersion,
	}

	recorded, created, err := outcomes.Record(row.AdapterName, outcome)
	if err != nil {
		return RecognitionResult{}, fmt.Errorf(
			"adapter recognition: cannot record the migration outcome for adapter %q: %w",
			row.AdapterName, err)
	}
	return RecognitionResult{
		Recognized:                true,
		Row:                       row,
		ContractVersionValidated:  validated,
		ContractVersionCompatible: compatible,
		ContractVersionErrors:     validationErrors,
		Outcome:                   recorded,
		Recorded:                  created,
	}, nil
}
