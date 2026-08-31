// Installed-skill state recording (TS-021-03).
//
// Per ADR-037 D8, installed skills are recorded under the global config dir
// at installed-skills/<name>.json. This store mirrors the installed-standard
// record store (record.go): one JSON file per skill identity, atomic writes,
// pinned format version, corrupt-record recovery, and idempotency by
// identity+version.
//
// Each record carries identity, version, source ("core" or a standard id),
// explicit resolution, install/update timestamps, and the target list
// targets: [{agent, scope, path}]. The store is a persistence component only:
// it does not materialize skill content, resolve agent paths, or remove
// installed files — those belong to the command surface (ST-021-01) and the
// agent-target mapping (TS-021-02).
//
// Stale detection is provided as a query-time operation (Status / ListStatuses)
// so `skill list` can surface actionable hints without deleting user content:
//   - core skills: stale when the recorded version differs from the current
//     CLI version (core skills are lockstep with the CLI, ADR-037 D2).
//   - standard-sourced skills: stale when the source standard is missing,
//     deprecated, or retired.
//
// Records are kept in all stale cases; the hint tells the user to update or
// uninstall.
//
// Reference: TS-021-03, ADR-037 D8, ADR-005 §7.1
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
	"strings"
	"syscall"
	"time"

	"maleolabs.com/anvil/internal/config"
)

// DefaultInstalledSkillsDirName is the skill record store directory name
// under the Anvil global config directory (ADR-005 §7.1).
const DefaultInstalledSkillsDirName = "installed-skills"

// InstalledSkillRecordFormatVersion is the pinned format version of an
// installed-skill record. Reads accept exactly this version.
const InstalledSkillRecordFormatVersion = 1

// Skill scope values. Scope is explicit per ADR-037 D5.
const (
	// SkillScopeRepo installs the skill into the current Anvil project's
	// repository root.
	SkillScopeRepo = "repo"

	// SkillScopeGlobal installs the skill into the user's agent config
	// directories.
	SkillScopeGlobal = "global"
)

// Skill resolution kinds. Core skills ship inside the Anvil binary;
// standard-skill assets are fetched from the standard's release channel.
const (
	// SkillResolutionKindCore records that the skill came from the
	// embedded core skill set.
	SkillResolutionKindCore = "core"

	// SkillResolutionKindDistribution records that the skill was resolved
	// from a standard's release channel distribution location.
	SkillResolutionKindDistribution = "distribution"
)

// SkillSourceCore is the sentinel source value for core skills (ADR-037
// D2). Standard-sourced skills use the standard id as their source.
const SkillSourceCore = "core"

// Sentinel errors. Consumers match them with errors.Is on the wrapped
// errors returned by the store methods.
var (
	// ErrSkillRecordNotFound reports that no skill record exists for the
	// requested identity.
	ErrSkillRecordNotFound = errors.New("installed skill record not found")

	// ErrSkillRecordVersionConflict reports that a record exists for the
	// skill at a different version: recording a new version is an update.
	ErrSkillRecordVersionConflict = errors.New("installed skill record version conflict")

	// ErrSkillRecordCorrupt reports that a skill record file exists but
	// cannot be read as a record.
	ErrSkillRecordCorrupt = errors.New("installed skill record corrupt")

	// ErrSkillStoreUnreadable reports that the store directory itself cannot
	// be read.
	ErrSkillStoreUnreadable = errors.New("installed skill store unreadable")

	// ErrSkillRecordInvalid reports that the record given to a write
	// operation is not structurally valid.
	ErrSkillRecordInvalid = errors.New("installed skill record invalid")
)

// InstalledSkillTarget records one installed copy of a skill: the agent it
// was written for, the scope (repo/global), and the filesystem path.
type InstalledSkillTarget struct {
	// Agent is the agent name, e.g. "claude-code", "opencode", "codex".
	Agent string `json:"agent"`

	// Scope is either SkillScopeRepo or SkillScopeGlobal.
	Scope string `json:"scope"`

	// Path is the absolute filesystem path where the skill content was
	// materialized for this agent/scope combination.
	Path string `json:"path"`
}

// InstalledSkillRecord is the persisted record of one installed skill
// (TS-021-03). It carries identity, pinned version, source (core or a
// standard id), explicit resolution, install/update timestamps, and the
// list of agent targets that were materialized.
type InstalledSkillRecord struct {
	// FormatVersion is the record format version this record was written
	// with; reads require exactly InstalledSkillRecordFormatVersion.
	FormatVersion int `json:"formatVersion"`

	// ID is the skill identity (the idempotency key, ADR-023 §3).
	ID string `json:"id"`

	// Version is the installed, pinned skill version.
	Version string `json:"version"`

	// Source is SkillSourceCore for core skills or the standard id for
	// skills provided by an installed standard (ADR-037 D8).
	Source string `json:"source"`

	// Resolution is the explicit resolution of the installed skill: where
	// the skill content came from.
	Resolution Resolution `json:"resolution"`

	// InstalledAt is the timestamp of the install that created this record.
	// It is preserved across updates.
	InstalledAt time.Time `json:"installedAt"`

	// UpdatedAt is the timestamp of the last adoption event on this record.
	UpdatedAt time.Time `json:"updatedAt"`

	// Targets lists every agent/scope/path where this skill was installed.
	Targets []InstalledSkillTarget `json:"targets"`
}

// InstalledSkillSummary is the lightweight enumeration of one installed
// skill returned by List.
type InstalledSkillSummary struct {
	ID          string
	Version     string
	Source      string
	InstalledAt time.Time
}

// InstalledSkillStatus is the record plus computed stale hints. Staleness
// is determined at query time; it is never persisted.
type InstalledSkillStatus struct {
	Record InstalledSkillRecord
	Stale  bool
	Hints  []string
}

// StandardLookup resolves an installed-standard record by id. It is the
// only dependency the stale-status query needs from the standard store,
// keeping the skill store decoupled from standard-registration internals.
type StandardLookup interface {
	Get(id string) (InstalledStandardRecord, error)
}

// CorruptSkillRecord describes one skill record file that could not be
// read during List.
type CorruptSkillRecord struct {
	Path  string
	Error string
}

// InstalledSkillStore is a file-backed store of installed-skill records
// (TS-021-03). Records are pure file persistence; they survive restarts.
// The store does not interpret skill content, agent paths, or standard
// lifecycle policy beyond the structural checks needed for stale hints.
type InstalledSkillStore struct {
	dir string
}

// NewInstalledSkillStore creates a skill record store rooted at dir. dir
// is created on the first write if it does not exist.
func NewInstalledSkillStore(dir string) *InstalledSkillStore {
	return &InstalledSkillStore{dir: dir}
}

// DefaultInstalledSkillsDir returns the default skill record store
// directory under the Anvil global config directory (ADR-005 §7.1).
func DefaultInstalledSkillsDir() (string, error) {
	dir, err := config.GlobalConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve default installed-skills directory: %w", err)
	}
	return filepath.Join(dir, DefaultInstalledSkillsDirName), nil
}

// Dir returns the store directory.
func (s *InstalledSkillStore) Dir() string {
	return s.dir
}

// Record records an install. Idempotency by identity plus version:
// re-recording the same id at the same version returns the existing record
// and created=false; the existing record (including its targets) is left
// untouched. Adding or replacing targets for the same version is an update,
// not a Record: callers must branch on created==false and use Update to
// change targets. Recording the same id at a different version is rejected
// with ErrSkillRecordVersionConflict (use Update).
func (s *InstalledSkillStore) Record(id string, rec InstalledSkillRecord) (InstalledSkillRecord, bool, error) {
	if err := validateSkillRecord(id, rec); err != nil {
		return InstalledSkillRecord{}, false, err
	}
	if !rec.UpdatedAt.Equal(rec.InstalledAt) {
		return InstalledSkillRecord{}, false, fmt.Errorf(
			"%w: updatedAt %s does not equal installedAt %s — a fresh install is the first adoption event; refreshing updatedAt is an update",
			ErrSkillRecordInvalid, rec.UpdatedAt.Format(time.RFC3339), rec.InstalledAt.Format(time.RFC3339))
	}

	existing, err := s.read(id)
	switch {
	case err == nil:
		if existing.Version == rec.Version {
			return existing, false, nil
		}
		return InstalledSkillRecord{}, false, fmt.Errorf(
			"%w: skill %q is recorded at version %q, not %q — install a different version via update",
			ErrSkillRecordVersionConflict, id, existing.Version, rec.Version)
	case errors.Is(err, ErrSkillRecordCorrupt):
		// Recover by re-adoption.
	case errors.Is(err, ErrSkillRecordNotFound):
		// Fresh record.
	default:
		return InstalledSkillRecord{}, false, err
	}

	if err := s.write(id, rec); err != nil {
		return InstalledSkillRecord{}, false, err
	}
	return rec, true, nil
}

// Get returns the skill record for id.
func (s *InstalledSkillStore) Get(id string) (InstalledSkillRecord, error) {
	if id == "" || !recordIDPattern.MatchString(id) {
		return InstalledSkillRecord{}, fmt.Errorf(
			"%w: %q is not a safe record key — the skill id must match ^[a-z0-9][a-z0-9-]*$ and be at most 64 characters",
			ErrSkillRecordInvalid, id)
	}
	return s.read(id)
}

// Update replaces the recorded skill atomically. It requires an existing
// record. If the existing record is readable, its installedAt is preserved
// (the original install time); if the existing record is corrupt, recovery
// by re-adoption uses the caller-supplied installedAt. updatedAt reflects
// the new adoption event.
func (s *InstalledSkillStore) Update(id string, rec InstalledSkillRecord) (InstalledSkillRecord, error) {
	if err := validateSkillRecord(id, rec); err != nil {
		return InstalledSkillRecord{}, err
	}

	existing, err := s.read(id)
	switch {
	case err == nil:
		rec.InstalledAt = existing.InstalledAt
	case errors.Is(err, ErrSkillRecordCorrupt):
		// Recovery by re-adoption uses the caller's installedAt.
	case errors.Is(err, ErrSkillRecordNotFound):
		return InstalledSkillRecord{}, fmt.Errorf(
			"%w: %s: no record for skill %q — install the skill first, then update",
			ErrSkillRecordNotFound, s.recordPath(id), id)
	default:
		return InstalledSkillRecord{}, err
	}

	if err := s.write(id, rec); err != nil {
		return InstalledSkillRecord{}, err
	}
	return rec, nil
}

// Delete removes the skill record. Deleting a skill that is not recorded
// fails with wrapped ErrSkillRecordNotFound.
func (s *InstalledSkillStore) Delete(id string) error {
	if id == "" || !recordIDPattern.MatchString(id) {
		return fmt.Errorf(
			"%w: %q is not a safe record key — the skill id must match ^[a-z0-9][a-z0-9-]*$ and be at most 64 characters",
			ErrSkillRecordInvalid, id)
	}
	path := s.recordPath(id)
	if err := os.Remove(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%w: %s", ErrSkillRecordNotFound, path)
		}
		return fmt.Errorf("installed skill store: remove %s: %w", path, err)
	}
	if err := s.syncDir(); err != nil {
		return fmt.Errorf(
			"installed skill store: remove %s completed, but the store directory could not be synced: %w (the record is gone; durability of the removal could not be confirmed)",
			path, err)
	}
	return nil
}

// List returns the id+version summary of every recorded skill, sorted by
// id, plus any corrupt record files that were skipped.
func (s *InstalledSkillStore) List() ([]InstalledSkillSummary, []CorruptSkillRecord, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf(
			"%w: read store directory %s: %v", ErrSkillStoreUnreadable, s.dir, err)
	}

	var summaries []InstalledSkillSummary
	var corrupt []CorruptSkillRecord
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, filepath.Ext(name))
		rec, err := s.read(id)
		if err != nil {
			corrupt = append(corrupt, CorruptSkillRecord{
				Path:  filepath.Join(s.dir, name),
				Error: err.Error(),
			})
			continue
		}
		summaries = append(summaries, InstalledSkillSummary{
			ID:          rec.ID,
			Version:     rec.Version,
			Source:      rec.Source,
			InstalledAt: rec.InstalledAt,
		})
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].ID < summaries[j].ID })
	sort.Slice(corrupt, func(i, j int) bool { return corrupt[i].Path < corrupt[j].Path })
	return summaries, corrupt, nil
}

// Status returns the record plus stale hints for id. Staleness is computed
// at query time and never deletes the record.
func (s *InstalledSkillStore) Status(id string, cliVersion string, lookup StandardLookup) (InstalledSkillStatus, error) {
	rec, err := s.Get(id)
	if err != nil {
		return InstalledSkillStatus{}, err
	}
	status := InstalledSkillStatus{Record: rec}
	if rec.Source == SkillSourceCore {
		if rec.Version != cliVersion {
			status.Stale = true
			status.Hints = append(status.Hints, fmt.Sprintf(
				"core skill materialized by Anvil CLI %s; current CLI is %s — run 'anvil skill update %s' to refresh",
				rec.Version, cliVersion, rec.ID))
		}
		return status, nil
	}

	std, err := lookup.Get(rec.Source)
	switch {
	case err == nil:
		switch std.Lifecycle.State {
		case LifecycleStateDeprecated:
			status.Stale = true
			removalDate := strings.TrimSpace(std.Lifecycle.RemovalDate)
			removalPhrase := "no removal date announced"
			if removalDate != "" {
				removalPhrase = "removal " + removalDate
			}
			status.Hints = append(status.Hints, fmt.Sprintf(
				"source standard %s is deprecated (%s) — refresh or uninstall this skill",
				rec.Source, removalPhrase))
		case LifecycleStateRetired:
			status.Stale = true
			status.Hints = append(status.Hints, fmt.Sprintf(
				"source standard %s is retired — uninstall this skill",
				rec.Source))
		}
	case errors.Is(err, ErrRecordNotFound):
		status.Stale = true
		status.Hints = append(status.Hints, fmt.Sprintf(
			"source standard %s is not installed — install the standard or run 'anvil skill uninstall %s' to remove this skill",
			rec.Source, rec.ID))
	case errors.Is(err, ErrRecordCorrupt):
		status.Stale = true
		status.Hints = append(status.Hints, fmt.Sprintf(
			"record of source standard %s is unreadable — re-adopt the standard or run 'anvil skill uninstall %s' to remove this skill",
			rec.Source, rec.ID))
	default:
		return InstalledSkillStatus{}, fmt.Errorf("installed skill store: resolve source standard %q: %w", rec.Source, err)
	}
	return status, nil
}

// ListStatuses enumerates every healthy record with its stale status.
func (s *InstalledSkillStore) ListStatuses(cliVersion string, lookup StandardLookup) ([]InstalledSkillStatus, []CorruptSkillRecord, error) {
	summaries, corrupt, err := s.List()
	if err != nil {
		return nil, nil, err
	}
	statuses := make([]InstalledSkillStatus, 0, len(summaries))
	for _, summary := range summaries {
		status, err := s.Status(summary.ID, cliVersion, lookup)
		if err != nil {
			return nil, nil, err
		}
		statuses = append(statuses, status)
	}
	return statuses, corrupt, nil
}

// read loads the record file for id.
func (s *InstalledSkillStore) read(id string) (InstalledSkillRecord, error) {
	path := s.recordPath(id)

	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return InstalledSkillRecord{}, fmt.Errorf("%w: %s", ErrSkillRecordNotFound, path)
		}
		return InstalledSkillRecord{}, fmt.Errorf(
			"%w: stat %s: %w", ErrSkillStoreUnreadable, path, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return InstalledSkillRecord{}, fmt.Errorf(
			"%w: %s is a symlink — delete the symlink or re-install the skill to recover",
			ErrSkillRecordCorrupt, path)
	}
	if info.IsDir() {
		return InstalledSkillRecord{}, fmt.Errorf(
			"%w: %s is a directory, not a record file — delete it or re-install the skill to recover",
			ErrSkillRecordCorrupt, path)
	}

	f, err := os.Open(path)
	if err != nil {
		return InstalledSkillRecord{}, fmt.Errorf(
			"%w: open %s: %w", ErrSkillStoreUnreadable, path, err)
	}
	defer f.Close()

	raw, err := io.ReadAll(io.LimitReader(f, MaxRecordSize+1))
	if err != nil {
		return InstalledSkillRecord{}, fmt.Errorf(
			"%w: read %s: %w", ErrSkillStoreUnreadable, path, err)
	}
	if len(raw) > MaxRecordSize {
		return InstalledSkillRecord{}, fmt.Errorf(
			"%w: %s: record exceeds the %d-byte size cap — delete the record or re-install the skill to recover",
			ErrSkillRecordCorrupt, path, MaxRecordSize)
	}

	var rec InstalledSkillRecord
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rec); err != nil {
		return InstalledSkillRecord{}, fmt.Errorf(
			"%w: %s: not decodable as an installed-skill record: %v — delete the record or re-install the skill to recover",
			ErrSkillRecordCorrupt, path, err)
	}
	if dec.More() {
		return InstalledSkillRecord{}, fmt.Errorf(
			"%w: %s: unexpected content after the record document — delete the record or re-install the skill to recover",
			ErrSkillRecordCorrupt, path)
	}

	if err := validateSkillRecord(id, rec); err != nil {
		return InstalledSkillRecord{}, fmt.Errorf(
			"%w: %s: %v — delete the record or re-install the skill to recover",
			ErrSkillRecordCorrupt, path, err)
	}
	return rec, nil
}

// write atomically writes the record for id.
func (s *InstalledSkillStore) write(id string, rec InstalledSkillRecord) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("installed skill store: create store directory %s: %w", s.dir, err)
	}

	raw, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("installed skill store: encode record %q: %w", id, err)
	}
	if len(raw) > MaxRecordSize {
		return fmt.Errorf(
			"%w: record for skill %q is %d bytes, exceeding the %d-byte cap",
			ErrSkillRecordInvalid, id, len(raw), MaxRecordSize)
	}
	raw = append(raw, '\n')

	path := s.recordPath(id)
	tmp, err := os.CreateTemp(s.dir, ".tmp-"+id+"-*.json")
	if err != nil {
		return fmt.Errorf("installed skill store: create temp file in %s: %w", s.dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("installed skill store: write %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("installed skill store: sync %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("installed skill store: close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("installed skill store: replace %s: %w", path, err)
	}
	if err := s.syncDir(); err != nil {
		return fmt.Errorf(
			"installed skill store: replace %s completed, but the store directory could not be synced: %w (the record content is in place; durability of the rename could not be confirmed)",
			path, err)
	}
	return nil
}

// syncDir flushes the store directory's metadata after a rename or remove.
func (s *InstalledSkillStore) syncDir() error {
	d, err := os.Open(s.dir)
	if err != nil {
		return fmt.Errorf("open store directory %s: %w", s.dir, err)
	}
	defer d.Close()

	if err := d.Sync(); err != nil {
		if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) {
			return nil
		}
		return fmt.Errorf("sync store directory %s: %w", s.dir, err)
	}
	return nil
}

// recordPath returns the record file path for id.
func (s *InstalledSkillStore) recordPath(id string) string {
	return filepath.Join(s.dir, id+".json")
}

// validateSkillRecord checks structural shape for writes and reads.
func validateSkillRecord(id string, rec InstalledSkillRecord) error {
	var problems []string
	if id == "" {
		problems = append(problems, "id must not be empty")
	} else if !recordIDPattern.MatchString(id) {
		problems = append(problems, fmt.Sprintf(
			"id %q is not a safe record key — the skill id must match ^[a-z0-9][a-z0-9-]*$ and be at most 64 characters",
			id))
	}
	if rec.ID != id {
		problems = append(problems, fmt.Sprintf("record id %q does not match the record key %q", rec.ID, id))
	}
	if rec.FormatVersion != InstalledSkillRecordFormatVersion {
		problems = append(problems, fmt.Sprintf(
			"formatVersion must be %d, got %d — the record format version is pinned",
			InstalledSkillRecordFormatVersion, rec.FormatVersion))
	}
	if rec.Version == "" {
		problems = append(problems, "version must not be empty")
	}
	if rec.Source == "" {
		problems = append(problems, "source must not be empty — expected '"+SkillSourceCore+"' or a standard id")
	} else if rec.Source != SkillSourceCore && !recordIDPattern.MatchString(rec.Source) {
		problems = append(problems, fmt.Sprintf(
			"source %q is invalid — expected '"+SkillSourceCore+"' or a standard id matching ^[a-z0-9][a-z0-9-]*$",
			rec.Source))
	}
	if rec.Resolution.Kind == "" {
		problems = append(problems, "resolution.kind must not be empty")
	} else if !knownSkillResolutionKind(rec.Resolution.Kind) {
		problems = append(problems, fmt.Sprintf(
			"resolution.kind %q is unknown — supported kinds: %s",
			rec.Resolution.Kind, strings.Join(skillResolutionKinds(), ", ")))
	}
	if rec.Resolution.Source == "" {
		problems = append(problems, "resolution.source must not be empty")
	}
	if rec.InstalledAt.IsZero() {
		problems = append(problems, "installedAt must be set")
	}
	if rec.UpdatedAt.IsZero() {
		problems = append(problems, "updatedAt must be set")
	} else if !rec.InstalledAt.IsZero() && rec.UpdatedAt.Before(rec.InstalledAt) {
		problems = append(problems, "updatedAt must not be before installedAt")
	}
	if len(rec.Targets) == 0 {
		problems = append(problems, "targets must not be empty")
	}
	seenTargets := make(map[string]struct{}, len(rec.Targets))
	for i, t := range rec.Targets {
		if t.Agent == "" {
			problems = append(problems, fmt.Sprintf("targets[%d].agent must not be empty", i))
		}
		if t.Scope != SkillScopeRepo && t.Scope != SkillScopeGlobal {
			problems = append(problems, fmt.Sprintf(
				"targets[%d].scope %q is invalid — expected %q or %q",
				i, t.Scope, SkillScopeRepo, SkillScopeGlobal))
		}
		if t.Path == "" {
			problems = append(problems, fmt.Sprintf("targets[%d].path must not be empty", i))
		} else if !filepath.IsAbs(t.Path) {
			problems = append(problems, fmt.Sprintf(
				"targets[%d].path %q must be an absolute path",
				i, t.Path))
		}
		key := t.Agent + "\x00" + t.Scope + "\x00" + t.Path
		if _, ok := seenTargets[key]; ok {
			problems = append(problems, fmt.Sprintf(
				"targets[%d] is a duplicate of (agent=%q, scope=%q, path=%q)",
				i, t.Agent, t.Scope, t.Path))
		}
		seenTargets[key] = struct{}{}
	}

	if len(problems) > 0 {
		return fmt.Errorf("%w: %s", ErrSkillRecordInvalid, strings.Join(problems, "; "))
	}
	return nil
}

func knownSkillResolutionKind(kind string) bool {
	switch kind {
	case SkillResolutionKindCore, SkillResolutionKindDistribution:
		return true
	}
	return false
}

func skillResolutionKinds() []string {
	return []string{SkillResolutionKindCore, SkillResolutionKindDistribution}
}
