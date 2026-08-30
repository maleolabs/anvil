// Installed-standard state recording (TS-014-03-03).
//
// Per ADR-022 §3, adoptions pin standard versions and resolution is
// explicit and recorded: the installed-standard record is the
// authoritative local record of what is installed — identity, pinned
// version, declared contract version, the explicit resolution used, and
// the lifecycle state at install time. It is state about installed
// distribution content, not operational state; downstream flows (EPIC-015)
// resolve against it, and the install/update flows (T-007/T-008) write it.
//
// This file implements the persistence component only. It is a pure,
// local, file-based record store: no network, no lifecycle logic, no
// validation logic. It does not call the compatibility or trust
// validators — it stores their JSON-ready results (CompatibilityResult,
// TrustResult) as embedded, optional sections of the record; T-012/T-007
// populate them. Update semantics beyond the atomic replace mechanism
// (re-validation, resolution changes) belong to the update flow (T-008);
// this component exposes the mechanism Update(id, newRecord) needs.
//
// Store layout. Records live one per standard under a directory following
// the ADR-005 §7.1 global config convention (config.GlobalConfigDir):
//
//	<config dir>/anvil/installed-standards/<standard-id>.json
//
// Per-standard files were chosen over a single store file:
//
//   - Recovery. A corrupt record file must not kill the whole store
//     (TS-014-03-03 deliverable): with per-standard files, one corrupt
//     record is skipped/reported while every other record stays readable;
//     a single-file store would make whole-store corruption the unit of
//     failure.
//   - Concurrency. Installs/updates of different standards touch different
//     files and never contend; a single-file store would need a global
//     lock around every write.
//   - Atomicity. Each record is written atomically (temp file + rename),
//     so per-record atomic replace — the exact mechanism Update needs —
//     is free. A single-file store's main advantage (whole-store atomic
//     commit) is not needed: records are read and replaced one standard
//     at a time.
//
// The trade-off: there is no single-file snapshot of all records. List
// reads the directory instead, which is fine for the store's size (one
// small JSON file per installed standard).
//
// Write path. Every write is atomic: the record is marshaled to a hidden
// temp file in the store directory, fsynced, and renamed over the record
// file; the store directory is then fsynced so the rename itself is
// durable — not just the record content. Filesystems that do not support
// directory fsync are tolerated. A crash cannot leave a torn or
// half-written record file — the record survives restarts (pure file
// persistence, no in-memory-only state).
//
// Concurrency semantics. Different standard ids never contend (different
// files). Concurrent writes of the same id are last-writer-wins, each
// write atomic — no torn files, and the record always reflects one
// complete write. Idempotency by identity plus version (ADR-023 §3):
// re-recording the same id at the same version is an idempotent success
// returning the existing record; recording the same id at a different
// version is rejected with ErrRecordVersionConflict — version change is
// an update, which targets the recorded version and belongs to the update
// flow (T-008). The version-conflict guard is a check-then-act read
// followed by an atomic rename: under same-id concurrency it is
// best-effort, and the serialization boundary is the rename — T-007/T-008
// must serialize same-id adoptions or accept last-writer-wins.
//
// Corruption and recovery. A record file that cannot be decoded — or that
// decodes to a structurally invalid record, or that declares an id
// different from its file name — is corrupt: Get fails with an actionable
// ErrRecordCorrupt naming the file, and List skips it and reports it.
// Corrupt records never kill the store, and they are recoverable:
// Record and Update replace a corrupt record with the new write (an
// explicit install/update re-establishes the record state), so recovery
// is a plain re-adoption — no manual file surgery.
//
// Reference: TS-014-03-03, ADR-022 §3, ADR-023 §3, ADR-005 §7.1
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
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"maleolabs.com/anvil/internal/config"
)

// Resolution kinds: the explicit resolution recorded per ADR-022 §3
// ("resolution is explicit and recorded"). The kind names the source
// family that produced the installed content; the source is the exact
// location used. Exactly one kind is recorded per record.
const (
	// ResolutionKindIndex records that the installed release was
	// resolved from a local static registry index (TS-014-02-01):
	// Source is the index directory path.
	ResolutionKindIndex = "index"

	// ResolutionKindBundle records that the installed release came from
	// an offline/bundled install path (TS-014-05-01): Source is the
	// bundled install material path.
	ResolutionKindBundle = "bundle"

	// ResolutionKindDistribution records that the installed release was
	// resolved from a distribution location on the standard's release
	// channel (ADR-030 §3): Source is the https URL of the release
	// content.
	ResolutionKindDistribution = "distribution"
)

// DefaultInstalledStandardsDirName is the record store directory name
// under the Anvil global config directory (ADR-005 §7.1).
const DefaultInstalledStandardsDirName = "installed-standards"

// MaxRecordSize caps the size of a single record file (1 MiB). Records
// hold one standard's core fields plus embedded validation results — a
// record beyond the cap is a broken artifact. The cap is enforced on both
// sides: writes reject an oversize record before it is persisted, and
// reads fail an oversize file with a precise, actionable error instead of
// unbounded memory use (mirrors MaxIndexDocumentSize /
// MaxTrustAnchorsSize). The store never persists a record its own read
// path would classify as corrupt.
const MaxRecordSize = 1 << 20

// RecordFormatVersion is the version of the installed-standard record
// format (TS-014-03-03). The record format is pinned: every record
// carries the version it was written with, and reads accept exactly the
// current version or the legacy version it was migrated from
// (LegacyRecordFormatVersion). The validation-result section
// (Compatibility, Trust) and the skill-declaration section (Skills,
// ST-021-04 / ADR-037 D3) are part of the pinned format; changing their
// shape is a format change — the migration path is bumping
// RecordFormatVersion and teaching reads to handle the previous
// versions, not silently tolerating unknown shapes.
//
// Format history:
//
//   - 1: the original record format (TS-014-03-03, W1/W2): identity,
//     pinned version, contract version, resolution, timestamps,
//     lifecycle, embedded compatibility and trust results, config
//     extension and template content. Still readable — migration keeps
//     existing records usable (ST-021-04: no data loss).
//   - 2: adds the optional Skills []SkillDeclaration section (ST-021-04,
//     ADR-037 D3): the standard's declared per-skill assets, persisted at
//     standard install/update (explicit re-validated events). Records
//     written in format 1 carry no declarations and decode with Skills
//     nil (default empty); the next explicit install/update rewrites them
//     as format 2.
const RecordFormatVersion = 2

// LegacyRecordFormatVersion is the previous pinned record format version.
// Reads accept it so existing records remain readable after the format
// bump (ST-021-04 DoD: no data loss); the format-2 Skills section is
// simply absent in legacy records and defaults to empty. Writes always
// produce the current RecordFormatVersion.
const LegacyRecordFormatVersion = 1

// Sentinel errors. Consumers match them with errors.Is on the wrapped
// errors returned by the store methods.
var (
	// ErrRecordNotFound reports that no record exists for the requested
	// standard id.
	ErrRecordNotFound = errors.New("installed standard record not found")

	// ErrRecordVersionConflict reports that a record exists for the
	// standard id at a different version: recording a new version is an
	// update (TS-014-03-02), not an idempotent install.
	ErrRecordVersionConflict = errors.New("installed standard record version conflict")

	// ErrRecordCorrupt reports that a record file exists but cannot be
	// read as a record: not decodable JSON, structurally invalid,
	// declaring an id that does not match its file name, an unknown
	// format version, a symlink, or a directory occupying the record
	// path. The wrapped error names the file and the reason.
	ErrRecordCorrupt = errors.New("installed standard record corrupt")

	// ErrStoreUnreadable reports that the store directory itself cannot
	// be read (missing directory is not an error: it means no records).
	ErrStoreUnreadable = errors.New("installed standard store unreadable")

	// ErrRecordInvalid reports that the record given to a write operation
	// is not a valid record: missing or unsafe identity, missing core
	// fields, an unknown resolution kind, an unsupported format version,
	// or a record too large to persist.
	ErrRecordInvalid = errors.New("installed standard record invalid")
)

// recordIDPattern mirrors the registry metadata schema's id pattern
// (registry-metadata.schema.json): lowercase alphanumeric with hyphens,
// at most 64 characters. The record store uses the id as the record file
// name (<id>.json), so the pattern doubles as the filename-safety rule:
// it rejects traversal-style separators (dots, slashes) that could
// escape the store directory, and keeps ids bounded for the store.
var recordIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// Resolution records the explicit resolution of an installed release
// (ADR-022 §3: resolution is explicit and recorded). The kind names the
// source family that produced the installed content; Source is the exact
// source used — the index directory path, the bundled install material
// path, or the distribution location URL.
type Resolution struct {
	// Kind is the resolution kind: one of ResolutionKindIndex,
	// ResolutionKindBundle, or ResolutionKindDistribution.
	Kind string `json:"kind"`

	// Source is the exact source used: the index directory path, the
	// bundled install material path, or the https distribution URL.
	Source string `json:"source"`
}

// SkillDeclaration is one skill declared by an installed standard release
// (ST-021-04; ADR-037 D3), persisted in the installed-standard record at
// standard install/update time — the explicit re-validated adoption
// events (TS-014-03-01/02). The declaration mirrors the release's
// metadata skills[] entry (TS-021-04, registry.Metadata.Skills): the
// per-skill release asset, its version, and its description. The record IS
// the skill registry (ADR-037 D3): `anvil skill list` discovers available
// standard skills by iterating installed-standard records, and the skill
// install path resolves a skill by matching these declarations — no
// separate store, no sync invariant.
//
// The declarations are parser-validated at metadata parse time
// (TS-021-04: name pattern, semver version, digest-bound asset). The
// record store validates their shape structurally (safe identifier,
// non-empty version and asset) so a hand-edited or corrupt record cannot
// smuggle an uninstallable declaration.
type SkillDeclaration struct {
	// Name is the skill name: the install target of anvil skill install
	// and the namespace component of skills/<standard-id>/<name>
	// (ADR-037 §7). Safe identifier only (^[a-z0-9][a-z0-9-]*$), at most
	// 64 characters.
	Name string `json:"name"`

	// Version is the skill version, semver (major.minor.patch).
	Version string `json:"version"`

	// Asset is the safe identifier of the release asset carrying the
	// skill content (e.g. anvil-skill-overview-1-0-0), covered by the
	// attested named digest of the release metadata (TS-014-04-04) — the
	// skill install gate verifies the downloaded asset against it.
	Asset string `json:"asset"`

	// Description is the optional human-readable skill description
	// (advisory annotation, no validation semantics).
	Description string `json:"description,omitempty"`
}

// SkillDeclarations converts the strict-parsed metadata skills[] of a
// release (TS-021-04) into the record's persisted declaration shape. The
// metadata skills are parser-validated (name pattern, semver version,
// digest-bound asset); the conversion is a field-by-field copy.
func SkillDeclarations(skills []Skill) []SkillDeclaration {
	if len(skills) == 0 {
		return nil
	}
	out := make([]SkillDeclaration, 0, len(skills))
	for _, s := range skills {
		out = append(out, SkillDeclaration{
			Name:        s.Name,
			Version:     s.Version,
			Asset:       s.Asset,
			Description: s.Description,
		})
	}
	return out
}

// InstalledStandardRecord is the persisted record of one installed
// standard release (TS-014-03-03). The core is stable: identity, pinned
// version, declared contract version, explicit resolution, and the
// install/update timestamps. The validation-results section carries the
// JSON-ready validation results (T-010/T-011) as optional embedded
// objects that the validation orchestration (T-012) and the install flow
// (T-007) populate; the store stores their JSON — it never runs or
// interprets the validators. The whole shape — core and validation
// results — is pinned by FormatVersion: reads accept exactly the current
// format version, and changing the shape is a format change (migration
// path = bump RecordFormatVersion).
//
// Versions are pinned (ADR-022 §3): the record names the exact installed
// version; there is no "latest" semantics anywhere in the record or the
// store.
type InstalledStandardRecord struct {
	// FormatVersion is the record format version this record was
	// written with; reads accept the current RecordFormatVersion and the
	// legacy version it was migrated from (LegacyRecordFormatVersion —
	// format-1 records predating the ST-021-04 skills extension remain
	// readable, with Skills defaulting empty).
	FormatVersion int `json:"formatVersion"`

	// ID is the standard identity: the stable identifier of the
	// installed standard (the identity half of the installation
	// idempotency key, ADR-023 §3).
	ID string `json:"id"`

	// Version is the installed release version, pinned at adoption
	// (ADR-022 §3; the second half of the idempotency key, ADR-023 §3).
	Version string `json:"version"`

	// ContractVersion is the contract version the installed release
	// declares (ADR-024 §3.1) — the compatibility target recorded at
	// install.
	ContractVersion string `json:"contractVersion"`

	// Resolution is the explicit resolution of the installed release:
	// the source used (ADR-022 §3).
	Resolution Resolution `json:"resolution"`

	// InstalledAt is the timestamp of the install that created this
	// record, RFC 3339. It is the original install time and is
	// preserved across updates: updates never rewrite it.
	InstalledAt time.Time `json:"installedAt"`

	// UpdatedAt is the timestamp of the last adoption event on this
	// record, RFC 3339: set to InstalledAt when the record is created
	// and refreshed on every update (the update flow, T-008, passes the
	// new adoption-event timestamp). installedAt = original install
	// time; updatedAt = last adoption/update event.
	UpdatedAt time.Time `json:"updatedAt"`

	// Lifecycle is the lifecycle state of the installed release at
	// install time (ADR-023 §3, ADR-027 §3), recorded for auditability.
	Lifecycle Lifecycle `json:"lifecycle"`

	// Compatibility is the optional embedded compatibility validation
	// result (T-010; TS-014-04-01), persisted as its JSON shape. Absent
	// (null) when no compatibility validation result was recorded.
	Compatibility *CompatibilityResult `json:"compatibility,omitempty"`

	// Trust is the optional embedded trust validation result
	// (T-011; TS-014-04-02), persisted as its JSON shape. Absent (null)
	// when no trust validation result was recorded.
	Trust *TrustResult `json:"trust,omitempty"`

	// ConfigExtension is the optional embedded configuration extension
	// content of the installed release (EPIC-013 config extension
	// contract; TS-015-03-01), persisted as its JSON shape — the
	// standard's declared framework configuration keys and their defaults
	// under the framework's own namespace. It is part of the installed
	// standard: the resolution of framework config keys and defaults
	// (TS-015-03-01) reads it from the record — never from runtime
	// knowledge (ADR-026 decision 1). Absent (null) when the installed
	// release declares no configuration extension content — a standard
	// may declare nothing in a category (command-contract §4.1), and
	// resolution then hands off to the missing-extension outcome
	// (ErrConfigExtensionMissing). The store stores its JSON; it never
	// interprets the content.
	ConfigExtension *ConfigExtensionContent `json:"configExtension,omitempty"`

	// Templates is the optional embedded template content of the
	// installed release (TS-015-02-03), persisted as its JSON shape —
	// the standard's declared pipeline template files under the
	// framework's own namespace. It is part of the installed standard:
	// initialization template generation (TS-015-02-03) reads it from
	// the record — never from runtime knowledge (ADR-026 decision 1;
	// TS-015-01-02). Absent (null) when the installed release declares
	// no template content — a standard may declare nothing in a
	// category (command-contract §4.1), and generation then hands off
	// to the interim adapter-driven path (ADR-020) via
	// ErrTemplateContentMissing. Like ConfigExtension, the content is
	// today populated by tests and hand-written records only: the
	// install/update flows do not yet extract template content from
	// the standard's release content (supplier side, EPIC-016/EPIC-018
	// scope — the same fixture limitation recorded for TS-015-03-01
	// §6.5). The store stores its JSON; it never interprets the
	// content.
	Templates *TemplateContent `json:"templateContent,omitempty"`

	// Skills is the optional skill-declaration section of the installed
	// release (ST-021-04; ADR-037 D3), persisted at standard
	// install/update — the explicit re-validated adoption events
	// (TS-014-03-01/02). It carries the standard's declared per-skill
	// release assets (name, version, asset, description), parser-validated
	// from the release metadata skills[] (TS-021-04). The record IS the
	// skill registry (ADR-037 D3): `anvil skill list` discovers available
	// standard skills by iterating installed-standard records, and the
	// skill install path resolves a skill against these declarations. No
	// separate store, no sync invariant — standard update replaces the
	// record (new declarations), standard deprecate/retire propagates the
	// no-updates rule to its skills, and standard uninstall removes the
	// record so declared-but-not-installed skills disappear from `list`
	// (installed ones stay, flagged stale by TS-021-03). Absent (null) in
	// legacy format-1 records — a record predating T-006 carries no
	// declarations until the standard is explicitly re-installed or
	// re-updated.
	Skills []SkillDeclaration `json:"skills,omitempty"`
}

// InstalledStandardSummary is the id + version summary of one installed
// standard returned by List, for server-side contexts that enumerate
// installed standards without needing full records.
type InstalledStandardSummary struct {
	// ID is the standard identity.
	ID string `json:"id"`

	// Version is the installed, pinned version.
	Version string `json:"version"`

	// ContractVersion is the contract version of the installed release.
	ContractVersion string `json:"contractVersion"`

	// InstalledAt is the install timestamp of the record.
	InstalledAt time.Time `json:"installedAt"`
}

// CorruptRecord describes one record file that could not be read during
// List: the file path and the reason. Corrupt records are skipped — they
// never fail the whole listing — and reported here for diagnostics.
type CorruptRecord struct {
	// Path is the record file that could not be read.
	Path string

	// Error is the actionable reason: what failed and how to recover.
	Error string
}

// InstalledStandardStore is a file-backed store of installed-standard
// records (TS-014-03-03): one JSON record file per standard id under a
// directory following the ADR-005 §7.1 global config convention. Records
// are pure file persistence — they survive restarts and are readable by
// downstream flows. The store is a persistence component only: it never
// fetches, never validates beyond structural record shape, and never
// interprets embedded validation results.
type InstalledStandardStore struct {
	// dir is the store directory holding the per-standard record files.
	dir string
}

// NewInstalledStandardStore creates a record store rooted at dir. dir
// must be a directory path (typically DefaultInstalledStandardsDir);
// it is created on the first write if it does not exist. A missing dir
// is not an error: it simply means no records exist yet.
func NewInstalledStandardStore(dir string) *InstalledStandardStore {
	return &InstalledStandardStore{dir: dir}
}

// DefaultInstalledStandardsDir returns the default record store
// directory: the Anvil global config directory (ADR-005 §7.1,
// implemented by config.GlobalConfigDir — os.UserConfigDir()/anvil) plus
// installed-standards. On Linux this resolves to
// ~/.config/anvil/installed-standards (XDG_CONFIG_HOME aware); on macOS
// to ~/Library/Application Support/anvil/installed-standards; on Windows
// to %AppData%/anvil/installed-standards.
func DefaultInstalledStandardsDir() (string, error) {
	dir, err := config.GlobalConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve default installed-standards directory: %w", err)
	}
	return filepath.Join(dir, DefaultInstalledStandardsDirName), nil
}

// Dir returns the store directory.
func (s *InstalledStandardStore) Dir() string {
	return s.dir
}

// Record records an install (TS-014-03-03). Idempotency by identity plus
// version (ADR-023 §3): re-recording the same id at the same version is
// an idempotent success — the existing record is returned unchanged and
// created is false; nothing is rewritten. Recording the same id at a
// different version is rejected with ErrRecordVersionConflict: a version
// change is an update (TS-014-03-02), which targets the recorded version
// and replaces it via Update.
//
// A corrupt existing record cannot be compared for idempotency; recording
// over it replaces it with the new write — recovery by re-adoption (an
// explicit install re-establishes the record state).
//
// Timestamps: a fresh install is the first adoption event, so the record
// is written with updatedAt equal to installedAt; a record with a
// different updatedAt is rejected — updates that refresh updatedAt belong
// to the update flow (T-008 / Update).
//
// The record is validated structurally before writing (validateRecord):
// missing or unsafe identity, missing core fields, a mismatch between id
// and the record's ID, an unknown resolution kind, a wrong format
// version, or an oversize record fails with an actionable
// ErrRecordInvalid. Validation results (Compatibility, Trust) are stored
// as-is; no validation logic runs here.
//
// The first write creates the store directory. The write is atomic (temp
// file + rename + directory fsync): a crash never leaves a torn record
// file.
func (s *InstalledStandardStore) Record(id string, rec InstalledStandardRecord) (InstalledStandardRecord, bool, error) {
	if err := validateRecord(id, rec, true); err != nil {
		return InstalledStandardRecord{}, false, err
	}
	// A fresh install is the first adoption event: updatedAt equals
	// installedAt at creation. A record carrying a later updatedAt is an
	// update in disguise — refreshing updatedAt belongs to Update
	// (TS-014-03-02).
	if !rec.UpdatedAt.Equal(rec.InstalledAt) {
		return InstalledStandardRecord{}, false, fmt.Errorf(
			"%w: updatedAt %s does not equal installedAt %s — a fresh install is the first adoption event, so updatedAt equals installedAt at creation; refreshing updatedAt is an update (TS-014-03-02 / Update)",
			ErrRecordInvalid, rec.UpdatedAt.Format(time.RFC3339), rec.InstalledAt.Format(time.RFC3339))
	}

	existing, err := s.read(id)
	switch {
	case err == nil:
		if existing.Version == rec.Version {
			// Idempotent success: the same identity plus version is
			// already recorded; return the existing state unchanged
			// (ADR-023 §3).
			return existing, false, nil
		}
		return InstalledStandardRecord{}, false, fmt.Errorf(
			"%w: standard %q is recorded at version %q, not %q — installing a different version is an update (TS-014-03-02), which targets the recorded version and replaces it atomically via the update flow",
			ErrRecordVersionConflict, id, existing.Version, rec.Version)
	case errors.Is(err, ErrRecordCorrupt):
		// The corrupt record cannot be compared for idempotency; the
		// explicit install replaces it (recovery by re-adoption).
	case errors.Is(err, ErrRecordNotFound):
		// Fresh record.
	default:
		return InstalledStandardRecord{}, false, err
	}

	if err := s.write(id, rec); err != nil {
		return InstalledStandardRecord{}, false, err
	}
	return rec, true, nil
}

// Get returns the record for the standard id, or an actionable error when
// there is none (wrapped ErrRecordNotFound) or when the record file
// cannot be read as a record (wrapped ErrRecordCorrupt naming the file:
// undecodable, oversize, unknown format version, symlink, or directory).
// Downstream flows resolve installed standards against this read.
func (s *InstalledStandardStore) Get(id string) (InstalledStandardRecord, error) {
	if id == "" || !recordIDPattern.MatchString(id) {
		return InstalledStandardRecord{}, fmt.Errorf(
			"%w: %q is not a safe record key — the standard id must match ^[a-z0-9][a-z0-9-]*$ and be at most 64 characters (the id is the record file name)",
			ErrRecordInvalid, id)
	}
	return s.read(id)
}

// Update replaces the recorded record atomically with the new record
// (TS-014-03-03 DoD: update replaces the recorded version atomically with
// the new resolution). This is the replace MECHANISM the update flow
// (T-008) targets: the update-flow semantics — re-validation, resolution
// changes, lifecycle policy — belong to T-008, not here. Timestamps: the
// update flow preserves the record's installedAt (the original install
// time) and refreshes updatedAt with the new adoption-event timestamp;
// the store enforces that updatedAt is set and not before installedAt.
//
// Update requires an existing record: updating a standard that is not
// recorded fails with wrapped ErrRecordNotFound (install first). A
// corrupt existing record is replaced by the explicit update (recovery by
// re-adoption). The write is atomic (temp file + rename + directory
// fsync); the record is validated structurally like Record.
func (s *InstalledStandardStore) Update(id string, rec InstalledStandardRecord) (InstalledStandardRecord, error) {
	if err := validateRecord(id, rec, true); err != nil {
		return InstalledStandardRecord{}, err
	}

	_, err := s.read(id)
	switch {
	case err == nil:
		// Existing record: replace.
	case errors.Is(err, ErrRecordCorrupt):
		// A corrupt record is replaced by the explicit update (recovery
		// by re-adoption).
	case errors.Is(err, ErrRecordNotFound):
		return InstalledStandardRecord{}, fmt.Errorf(
			"%w: %s: no record for standard %q — there is nothing to update; install the standard first (TS-014-03-01), then update",
			ErrRecordNotFound, s.recordPath(id), id)
	default:
		return InstalledStandardRecord{}, err
	}

	if err := s.write(id, rec); err != nil {
		return InstalledStandardRecord{}, err
	}
	return rec, nil
}

// Delete removes the record for the standard id. It exists for
// completeness and rollback: an explicit uninstall/rollback can remove
// the recorded state. The removal is durable: the store directory is
// fsynced after the unlink. Deleting a standard that is not recorded
// fails with wrapped ErrRecordNotFound.
func (s *InstalledStandardStore) Delete(id string) error {
	if id == "" || !recordIDPattern.MatchString(id) {
		return fmt.Errorf(
			"%w: %q is not a safe record key — the standard id must match ^[a-z0-9][a-z0-9-]*$ and be at most 64 characters (the id is the record file name)",
			ErrRecordInvalid, id)
	}
	path := s.recordPath(id)
	if err := os.Remove(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%w: %s", ErrRecordNotFound, path)
		}
		return fmt.Errorf("installed standard store: remove %s: %w", path, err)
	}
	if err := s.syncDir(); err != nil {
		return fmt.Errorf(
			"installed standard store: remove %s completed, but the store directory could not be synced: %w (the record is gone; durability of the removal could not be confirmed)",
			path, err)
	}
	return nil
}

// List returns the id + version summary of every recorded standard,
// sorted ascending by id, plus the corrupt record files that were skipped
// and reported. A missing store directory lists as empty: no records
// exist yet, which is not an error. Corrupt records never fail the
// listing (TS-014-03-03 deliverable: a corrupt single record must not
// kill the whole store); each one is reported in corrupt with its path
// and reason, and Get on its id yields the precise error.
func (s *InstalledStandardStore) List() ([]InstalledStandardSummary, []CorruptRecord, error) {
	records, corrupt, err := s.ListRecords()
	if err != nil {
		return nil, nil, err
	}
	summaries := make([]InstalledStandardSummary, 0, len(records))
	for _, rec := range records {
		summaries = append(summaries, InstalledStandardSummary{
			ID:              rec.ID,
			Version:         rec.Version,
			ContractVersion: rec.ContractVersion,
			InstalledAt:     rec.InstalledAt,
		})
	}
	return summaries, corrupt, nil
}

// ListRecords returns every readable installed-standard record in full,
// sorted ascending by id, plus the corrupt record files that were skipped
// and reported. A missing store directory lists as empty. Like List,
// corrupt records never fail the enumeration — each one is reported with
// its path and reason.
//
// ListRecords is the enumeration surface for record-as-registry consumers
// (ADR-037 D3): `anvil skill list` discovers available standard skills by
// iterating these records and reading their Skills declarations, and the
// standard-skill resolver matches declarations the same way. The summary
// projection (List) cannot serve them — declarations live on the full
// record.
func (s *InstalledStandardStore) ListRecords() ([]InstalledStandardRecord, []CorruptRecord, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf(
			"%w: read store directory %s: %v", ErrStoreUnreadable, s.dir, err)
	}

	var records []InstalledStandardRecord
	var corrupt []CorruptRecord
	for _, entry := range entries {
		name := entry.Name()
		// Only record files participate: hidden entries and non-.json
		// files are not records. Directories and symlinks named
		// *.json are NOT silently skipped — read classifies them as
		// corrupt record paths, consistently with Get.
		if strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, filepath.Ext(name))
		rec, err := s.read(id)
		if err != nil {
			corrupt = append(corrupt, CorruptRecord{
				Path:  filepath.Join(s.dir, name),
				Error: err.Error(),
			})
			continue
		}
		records = append(records, rec)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
	sort.Slice(corrupt, func(i, j int) bool { return corrupt[i].Path < corrupt[j].Path })
	return records, corrupt, nil
}

// read loads the record file for id. A missing file is wrapped
// ErrRecordNotFound; a file that exists but cannot be read as a record —
// a symlink, a directory, oversize, not decodable JSON, unknown fields,
// trailing content, an unsupported format version, structurally invalid,
// or declaring an id that does not match the file name — is wrapped
// ErrRecordCorrupt naming the file and the reason. The record path is
// classified with Lstat BEFORE opening (mirroring the index client's
// plain-tree convention, index.go): a symlink or a directory occupying a
// record path is a corrupt record, not a file to follow or a directory to
// read.
func (s *InstalledStandardStore) read(id string) (InstalledStandardRecord, error) {
	path := s.recordPath(id)

	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return InstalledStandardRecord{}, fmt.Errorf("%w: %s", ErrRecordNotFound, path)
		}
		return InstalledStandardRecord{}, fmt.Errorf(
			"%w: stat %s: %w", ErrStoreUnreadable, path, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return InstalledStandardRecord{}, fmt.Errorf(
			"%w: %s is a symlink — symlinked record files are not supported (the record store is a plain file store, mirroring the index client's symlink rejection); delete the symlink or re-install the standard to recover",
			ErrRecordCorrupt, path)
	}
	if info.IsDir() {
		return InstalledStandardRecord{}, fmt.Errorf(
			"%w: %s is a directory, not a record file — delete it or re-install the standard to recover",
			ErrRecordCorrupt, path)
	}

	f, err := os.Open(path)
	if err != nil {
		return InstalledStandardRecord{}, fmt.Errorf(
			"%w: open %s: %w", ErrStoreUnreadable, path, err)
	}
	defer f.Close()

	raw, err := io.ReadAll(io.LimitReader(f, MaxRecordSize+1))
	if err != nil {
		return InstalledStandardRecord{}, fmt.Errorf(
			"%w: read %s: %w", ErrStoreUnreadable, path, err)
	}
	if len(raw) > MaxRecordSize {
		return InstalledStandardRecord{}, fmt.Errorf(
			"%w: %s: record exceeds the %d-byte size cap — the file is a broken artifact; delete the record or re-install the standard to recover",
			ErrRecordCorrupt, path, MaxRecordSize)
	}

	var rec InstalledStandardRecord
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rec); err != nil {
		return InstalledStandardRecord{}, fmt.Errorf(
			"%w: %s: not decodable as an installed-standard record: %v — delete the record or re-install the standard to recover",
			ErrRecordCorrupt, path, err)
	}
	if dec.More() {
		return InstalledStandardRecord{}, fmt.Errorf(
			"%w: %s: unexpected content after the record document — delete the record or re-install the standard to recover",
			ErrRecordCorrupt, path)
	}

	// The file name is the record key: a file that declares a different
	// id than its name is corrupt, and the record must be structurally
	// valid to be trusted by downstream flows (identity, pinned version,
	// declared contract version, explicit resolution, install timestamp).
	// Read-mode validation accepts the current format version AND the
	// legacy version it was migrated from (LegacyRecordFormatVersion):
	// records written before the ST-021-04 format bump stay readable and
	// their Skills section defaults empty (no data loss).
	if err := validateRecord(id, rec, false); err != nil {
		return InstalledStandardRecord{}, fmt.Errorf(
			"%w: %s: %v — delete the record or re-install the standard to recover",
			ErrRecordCorrupt, path, err)
	}
	return rec, nil
}

// write atomically writes the record for id: marshal to a hidden temp
// file in the store directory, fsync, rename over the record file, and
// fsync the store directory so the rename itself is durable. The store
// directory is created on first write. An oversize record is rejected
// with ErrRecordInvalid before any file is created — the store never
// persists a record its own read path would classify as corrupt. A crash
// never leaves a torn or partial record file.
func (s *InstalledStandardStore) write(id string, rec InstalledStandardRecord) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("installed standard store: create store directory %s: %w", s.dir, err)
	}

	raw, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("installed standard store: encode record %q: %w", id, err)
	}
	if len(raw) > MaxRecordSize {
		return fmt.Errorf(
			"%w: record for standard %q is %d bytes, exceeding the %d-byte cap — the record would be unreadable by the store's own read path; trim the record (e.g. oversized resolution.source or embedded validation results)",
			ErrRecordInvalid, id, len(raw), MaxRecordSize)
	}
	raw = append(raw, '\n')

	path := s.recordPath(id)
	tmp, err := os.CreateTemp(s.dir, ".tmp-"+id+"-*.json")
	if err != nil {
		return fmt.Errorf("installed standard store: create temp file in %s: %w", s.dir, err)
	}
	tmpName := tmp.Name()
	// On success the temp file has been renamed away and Remove is a
	// harmless no-op; on failure it cleans up the partial temp file.
	defer os.Remove(tmpName)

	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("installed standard store: write %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("installed standard store: sync %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("installed standard store: close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("installed standard store: replace %s: %w", path, err)
	}
	if err := s.syncDir(); err != nil {
		return fmt.Errorf(
			"installed standard store: replace %s completed, but the store directory could not be synced: %w (the record content is in place; durability of the rename could not be confirmed)",
			path, err)
	}
	return nil
}

// syncDir flushes the store directory's metadata after a rename or
// remove, so the directory entry itself is durable — not just the record
// content. This closes the last durability gap in the write path: content
// fsync + rename + directory fsync means a crash after a successful
// write cannot lose the rename (the record survives restarts, DoD
// TS-014-03-03). Filesystems that do not support directory fsync (some
// tmpfs/network mounts) report EINVAL or ENOTSUP — tolerated, since the
// rename/remove itself is still atomic. Note: directory fsync via os.Open
// is Unix-oriented; on platforms where the store directory cannot be
// opened for syncing, writes fail rather than silently skip durability.
func (s *InstalledStandardStore) syncDir() error {
	d, err := os.Open(s.dir)
	if err != nil {
		return fmt.Errorf("open store directory %s: %w", s.dir, err)
	}
	defer d.Close()

	if err := d.Sync(); err != nil {
		if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) {
			// The filesystem does not support directory fsync; the
			// rename/remove itself is still atomic, so the operation
			// succeeded — the sync is best-effort durability.
			return nil
		}
		return fmt.Errorf("sync store directory %s: %w", s.dir, err)
	}
	return nil
}

// recordPath returns the record file path for id.
func (s *InstalledStandardStore) recordPath(id string) string {
	return filepath.Join(s.dir, id+".json")
}

// validateRecord checks the structural shape of a record for write and
// read: the key id is a safe file name (mirrors the registry metadata
// schema id pattern — lowercase alphanumeric with hyphens, at most 64
// characters), the record's ID matches the key, the format version is
// valid for the operation, and the core fields are present: pinned
// version, declared contract version, explicit resolution (kind and
// source), lifecycle state, and the install/update timestamps. Resolution
// kind must be one of the three known kinds. Timestamp semantics:
// updatedAt must be set and not before installedAt (installedAt = original
// install time, preserved across updates; updatedAt = last adoption
// event). Validation is structural only — no semver parsing, no lifecycle
// policy, no validation logic (those belong to parse.go and the adoption
// validation orchestration T-012).
//
// The format-version rule depends on the operation. Writes (write=true)
// require exactly the current RecordFormatVersion — the store never
// persists a record in a legacy format. Reads (write=false) accept the
// current version AND the legacy version it was migrated from
// (LegacyRecordFormatVersion): format-1 records predating the ST-021-04
// skills extension remain readable, with Skills defaulting to empty.
// Skill declarations (Skills) are validated structurally when present:
// each name must be a safe identifier (it is the install target and the
// namespace component of skills/<standard-id>/<name>, ADR-037 §7), with a
// non-empty version and asset; descriptions are advisory and unchecked.
func validateRecord(id string, rec InstalledStandardRecord, write bool) error {
	var problems []string
	if id == "" {
		problems = append(problems, "id must not be empty")
	} else if !recordIDPattern.MatchString(id) {
		problems = append(problems, fmt.Sprintf(
			"id %q is not a safe record key — the standard id must match ^[a-z0-9][a-z0-9-]*$ and be at most 64 characters (the id is the record file name)",
			id))
	}
	if rec.ID != id {
		problems = append(problems, fmt.Sprintf(
			"record id %q does not match the record key %q", rec.ID, id))
	}
	if write {
		if rec.FormatVersion != RecordFormatVersion {
			problems = append(problems, fmt.Sprintf(
				"formatVersion must be %d, got %d — the record format version is pinned and every write persists the current format; records written in another format version must be migrated (bump RecordFormatVersion and teach reads to handle both versions)",
				RecordFormatVersion, rec.FormatVersion))
		}
	} else if !recordFormatVersionReadable(rec.FormatVersion) {
		problems = append(problems, fmt.Sprintf(
			"formatVersion %d is not readable — supported versions: %d (legacy, readable with Skills defaulting empty) and %d (current)",
			rec.FormatVersion, LegacyRecordFormatVersion, RecordFormatVersion))
	}
	if rec.Version == "" {
		problems = append(problems, "version must not be empty — the installed version is pinned and recorded (ADR-022 §3)")
	}
	if rec.ContractVersion == "" {
		problems = append(problems, "contractVersion must not be empty — the declared contract version is part of the record (ADR-024 §3.1)")
	}
	if rec.Resolution.Kind == "" {
		problems = append(problems, "resolution.kind must not be empty — the resolution is explicit and recorded (ADR-022 §3)")
	} else if !knownResolutionKind(rec.Resolution.Kind) {
		problems = append(problems, fmt.Sprintf(
			"resolution.kind %q is unknown — supported kinds: %s",
			rec.Resolution.Kind, strings.Join(resolutionKinds(), ", ")))
	}
	if rec.Resolution.Source == "" {
		problems = append(problems, "resolution.source must not be empty — the exact source used is recorded (ADR-022 §3)")
	}
	if rec.InstalledAt.IsZero() {
		problems = append(problems, "installedAt must be set — the original install time is part of the record")
	}
	if rec.UpdatedAt.IsZero() {
		problems = append(problems, "updatedAt must be set — the last adoption-event time is part of the record")
	} else if !rec.InstalledAt.IsZero() && rec.UpdatedAt.Before(rec.InstalledAt) {
		problems = append(problems, "updatedAt must not be before installedAt — installedAt is the original install time and is preserved across updates; updatedAt is refreshed on every update (last adoption event)")
	}
	if rec.Lifecycle.State == "" {
		problems = append(problems, "lifecycle.state must not be empty — the lifecycle state at install time is part of the record (ADR-023 §3, ADR-027 §3)")
	}
	for i, sk := range rec.Skills {
		if !recordIDPattern.MatchString(sk.Name) {
			problems = append(problems, fmt.Sprintf(
				"skills[%d].name %q is not a safe skill identifier — skill names must match ^[a-z0-9][a-z0-9-]*$ and be at most 64 characters (the name is the install target of anvil skill install and the namespace component of skills/<standard-id>/<name>, ADR-037 §7)",
				i, sk.Name))
		}
		if sk.Version == "" {
			problems = append(problems, fmt.Sprintf("skills[%d].version must not be empty — the skill version is part of its identity", i))
		}
		if sk.Asset == "" {
			problems = append(problems, fmt.Sprintf("skills[%d].asset must not be empty — the skill content is a release asset covered by the attested named digest (TS-014-04-04)", i))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%w: %s", ErrRecordInvalid, strings.Join(problems, "; "))
	}
	return nil
}

// recordFormatVersionReadable reports whether a record file carrying
// format version v can be read: the current format version or the legacy
// version it was migrated from (ST-021-04 format bump — existing records
// stay readable, Skills default empty). Anything else is an unknown or
// future format and the file is corrupt.
func recordFormatVersionReadable(v int) bool {
	return v == RecordFormatVersion || v == LegacyRecordFormatVersion
}

// knownResolutionKind reports whether kind is one of the supported
// resolution kinds.
func knownResolutionKind(kind string) bool {
	switch kind {
	case ResolutionKindIndex, ResolutionKindBundle, ResolutionKindDistribution:
		return true
	}
	return false
}

// resolutionKinds returns the supported resolution kinds, sorted
// ascending, for actionable messages.
func resolutionKinds() []string {
	return []string{ResolutionKindBundle, ResolutionKindDistribution, ResolutionKindIndex}
}
