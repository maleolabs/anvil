package registry

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// sampleRecord returns a structurally valid installed-standard record.
// Tests mutate the fields they exercise.
func sampleRecord(id, version string) InstalledStandardRecord {
	return InstalledStandardRecord{
		FormatVersion:   RecordFormatVersion,
		ID:              id,
		Version:         version,
		ContractVersion: "1.0.0",
		Resolution: Resolution{
			Kind:   ResolutionKindIndex,
			Source: "/home/operator/registry-index",
		},
		InstalledAt: time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC),
		Lifecycle: Lifecycle{
			State: LifecycleStatePublished,
		},
	}
}

// newTestStore returns a record store rooted at a fresh temp directory.
func newTestStore(t *testing.T) *InstalledStandardStore {
	t.Helper()
	return NewInstalledStandardStore(t.TempDir())
}

// recordFilePath returns the expected record file path for id under the
// store directory.
func recordFilePath(store *InstalledStandardStore, id string) string {
	return filepath.Join(store.Dir(), id+".json")
}

// readRecordFile reads the raw record file bytes from disk.
func readRecordFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read record file %s: %v", path, err)
	}
	return raw
}

// TestInstalledStandardStoreRecordPersists asserts a recorded install is
// persisted as <dir>/<id>.json with the full record content: identity,
// pinned version, declared contract version, explicit resolution, install
// timestamp, and lifecycle state (TS-014-03-03 DoD: a record is created
// at install with identity, version, contract version, and explicit
// resolution; ADR-022 §3: versions are pinned, resolution explicit and
// recorded).
func TestInstalledStandardStoreRecordPersists(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")

	got, created, err := store.Record(rec.ID, rec)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if !created {
		t.Error("created = false, want true (fresh record)")
	}
	if !reflect.DeepEqual(got, rec) {
		t.Errorf("Record returned %+v, want %+v", got, rec)
	}

	path := recordFilePath(store, rec.ID)
	raw := readRecordFile(t, path)

	var decoded InstalledStandardRecord
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("record file %s is not decodable JSON: %v", path, err)
	}
	if !reflect.DeepEqual(decoded, rec) {
		t.Errorf("record file decodes to %+v, want %+v", decoded, rec)
	}
	if !bytes.HasSuffix(raw, []byte("\n")) {
		t.Error("record file does not end with a newline")
	}
}

// TestInstalledStandardStoreRecordIdempotentSameVersion asserts
// re-recording the same id at the same version is an idempotent success:
// the existing record is returned unchanged, nothing is rewritten
// (ADR-023 §3: installation is idempotent by standard identity plus
// version; TS-014-03-03 DoD).
func TestInstalledStandardStoreRecordIdempotentSameVersion(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")

	if _, created, err := store.Record(rec.ID, rec); err != nil || !created {
		t.Fatalf("first Record: created=%v err=%v", created, err)
	}
	path := recordFilePath(store, rec.ID)
	before := readRecordFile(t, path)

	// A re-install of the same version carries new timestamps and
	// validation results; the store must return the existing state
	// unchanged. A fresh install sets updatedAt == installedAt (first
	// adoption event), so the re-install record carries both shifted by
	// the same amount.
	reinstall := sampleRecord(rec.ID, rec.Version)
	reinstall.InstalledAt = reinstall.InstalledAt.Add(24 * time.Hour)
	reinstall.UpdatedAt = reinstall.InstalledAt
	reinstall.Compatibility = &CompatibilityResult{Valid: true}
	reinstall.Trust = &TrustResult{Valid: true}

	got, created, err := store.Record(rec.ID, reinstall)
	if err != nil {
		t.Fatalf("idempotent Record: %v", err)
	}
	if created {
		t.Error("created = true, want false (idempotent success)")
	}
	if !reflect.DeepEqual(got, rec) {
		t.Errorf("idempotent Record returned %+v, want the existing record %+v", got, rec)
	}
	if after := readRecordFile(t, path); !bytes.Equal(before, after) {
		t.Error("record file changed on idempotent re-record, want it untouched")
	}
}

// TestInstalledStandardStoreRecordRejectsVersionChange asserts recording
// the same id at a different version is rejected with
// ErrRecordVersionConflict: a version change is an update (TS-014-03-02),
// not an idempotent install (ADR-023 §3; TS-014-03-03).
func TestInstalledStandardStoreRecordRejectsVersionChange(t *testing.T) {
	store := newTestStore(t)
	v1 := sampleRecord("anvil-standard-laravel", "1.2.3")

	if _, created, err := store.Record(v1.ID, v1); err != nil || !created {
		t.Fatalf("first Record: created=%v err=%v", created, err)
	}

	v2 := sampleRecord(v1.ID, "1.3.0")
	_, _, err := store.Record(v1.ID, v2)
	if !errors.Is(err, ErrRecordVersionConflict) {
		t.Fatalf("err = %v, want wrapped ErrRecordVersionConflict", err)
	}
	for _, want := range []string{"1.2.3", "1.3.0", "update"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}

	got, err := store.Get(v1.ID)
	if err != nil {
		t.Fatalf("Get after rejected version change: %v", err)
	}
	if got.Version != v1.Version {
		t.Errorf("record version = %q after rejected version change, want %q (record untouched)", got.Version, v1.Version)
	}
}

// TestInstalledStandardStoreRecordRejectsInvalidRecords asserts
// structurally invalid records are rejected with an actionable
// ErrRecordInvalid and nothing is written (TS-014-03-03: the record's
// core — identity, version, contract version, resolution, timestamp —
// is stable).
func TestInstalledStandardStoreRecordRejectsInvalidRecords(t *testing.T) {
	valid := sampleRecord("anvil-standard-laravel", "1.2.3")

	cases := []struct {
		name string
		id   string
		rec  InstalledStandardRecord
		want string // substring the error must carry
	}{
		{
			name: "empty id", id: "",
			rec:  valid,
			want: "id must not be empty",
		},
		{
			name: "id with slash", id: "a/b",
			rec:  valid,
			want: "not a safe record key",
		},
		{
			name: "id with dot", id: "anvil.standard",
			rec:  valid,
			want: "not a safe record key",
		},
		{
			name: "id is dot dot", id: "..",
			rec:  valid,
			want: "not a safe record key",
		},
		{
			name: "id starts with hyphen", id: "-standard",
			rec:  valid,
			want: "not a safe record key",
		},
		{
			name: "id uppercase", id: "Anvil-Standard",
			rec:  valid,
			want: "not a safe record key",
		},
		{
			name: "id too long", id: strings.Repeat("a", 65),
			rec:  valid,
			want: "not a safe record key",
		},
		{
			name: "record id mismatch",
			id:   "anvil-standard-laravel",
			rec:  func() InstalledStandardRecord { r := valid; r.ID = "anvil-standard-flutter"; return r }(),
			want: "does not match the record key",
		},
		{
			name: "empty version",
			id:   valid.ID,
			rec:  func() InstalledStandardRecord { r := valid; r.Version = ""; return r }(),
			want: "version must not be empty",
		},
		{
			name: "empty contract version",
			id:   valid.ID,
			rec:  func() InstalledStandardRecord { r := valid; r.ContractVersion = ""; return r }(),
			want: "contractVersion must not be empty",
		},
		{
			name: "empty resolution kind",
			id:   valid.ID,
			rec:  func() InstalledStandardRecord { r := valid; r.Resolution.Kind = ""; return r }(),
			want: "resolution.kind must not be empty",
		},
		{
			name: "unknown resolution kind",
			id:   valid.ID,
			rec:  func() InstalledStandardRecord { r := valid; r.Resolution.Kind = "git"; return r }(),
			want: "resolution.kind \"git\" is unknown",
		},
		{
			name: "empty resolution source",
			id:   valid.ID,
			rec:  func() InstalledStandardRecord { r := valid; r.Resolution.Source = ""; return r }(),
			want: "resolution.source must not be empty",
		},
		{
			name: "zero installed at",
			id:   valid.ID,
			rec:  func() InstalledStandardRecord { r := valid; r.InstalledAt = time.Time{}; return r }(),
			want: "installedAt must be set",
		},
		{
			name: "zero updated at",
			id:   valid.ID,
			rec:  func() InstalledStandardRecord { r := valid; r.UpdatedAt = time.Time{}; return r }(),
			want: "updatedAt must be set",
		},
		{
			name: "updated at before installed at",
			id:   valid.ID,
			rec: func() InstalledStandardRecord {
				r := valid
				r.UpdatedAt = r.InstalledAt.Add(-time.Hour)
				return r
			}(),
			want: "updatedAt must not be before installedAt",
		},
		{
			name: "updated at differs from installed at on record",
			id:   valid.ID,
			rec: func() InstalledStandardRecord {
				r := valid
				r.UpdatedAt = r.InstalledAt.Add(time.Hour)
				return r
			}(),
			want: "does not equal installedAt",
		},
		{
			name: "missing format version",
			id:   valid.ID,
			rec:  func() InstalledStandardRecord { r := valid; r.FormatVersion = 0; return r }(),
			want: "formatVersion must be 2",
		},
		{
			name: "unknown format version",
			id:   valid.ID,
			rec:  func() InstalledStandardRecord { r := valid; r.FormatVersion = 3; return r }(),
			want: "formatVersion must be 2",
		},
		{
			name: "empty lifecycle state",
			id:   valid.ID,
			rec:  func() InstalledStandardRecord { r := valid; r.Lifecycle.State = ""; return r }(),
			want: "lifecycle.state must not be empty",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newTestStore(t)
			_, _, err := store.Record(tc.id, tc.rec)
			if !errors.Is(err, ErrRecordInvalid) {
				t.Fatalf("err = %v, want wrapped ErrRecordInvalid", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not carry %q", err, tc.want)
			}
			if entries, err := os.ReadDir(store.Dir()); err == nil && len(entries) != 0 {
				t.Errorf("store directory contains %d entries after a rejected record, want none", len(entries))
			}
		})
	}
}

// TestInstalledStandardStoreGet asserts Get returns the recorded standard
// for downstream flows, and fails with wrapped ErrRecordNotFound for
// unrecorded standards (TS-014-03-03 DoD: the record is readable by
// downstream flows).
func TestInstalledStandardStoreGet(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")

	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	got, err := store.Get(rec.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(got, rec) {
		t.Errorf("Get returned %+v, want %+v", got, rec)
	}

	_, err = store.Get("anvil-standard-unknown")
	if !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("Get unknown: err = %v, want wrapped ErrRecordNotFound", err)
	}
	if !strings.Contains(err.Error(), recordFilePath(store, "anvil-standard-unknown")) {
		t.Errorf("error %q does not name the record file", err)
	}

	_, err = store.Get("not/a/safe/key")
	if !errors.Is(err, ErrRecordInvalid) {
		t.Fatalf("Get unsafe key: err = %v, want wrapped ErrRecordInvalid", err)
	}
}

// TestInstalledStandardStoreUpdateReplacesRecord asserts Update replaces
// the recorded version atomically with the new resolution (TS-014-03-03
// DoD): after Update, the record carries the new version and the new
// resolution, and the file content is the new record. Timestamp
// semantics: installedAt (the original install time) is preserved, and
// updatedAt is refreshed with the update's adoption-event timestamp.
func TestInstalledStandardStoreUpdateReplacesRecord(t *testing.T) {
	store := newTestStore(t)
	old := sampleRecord("anvil-standard-laravel", "1.2.3")
	old.Resolution = Resolution{Kind: ResolutionKindIndex, Source: "/old/index"}

	if _, _, err := store.Record(old.ID, old); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// The update flow (T-008) keeps the original install time and
	// refreshes updatedAt with the new adoption-event timestamp.
	next := sampleRecord(old.ID, "2.0.0")
	next.InstalledAt = old.InstalledAt
	next.UpdatedAt = old.InstalledAt.Add(30 * 24 * time.Hour)
	next.ContractVersion = "2.0.0"
	next.Resolution = Resolution{Kind: ResolutionKindDistribution, Source: "https://github.com/maleolabs/anvil-standard-laravel/releases/download/v2.0.0/anvil-standard-laravel.tar.gz"}

	got, err := store.Update(old.ID, next)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !reflect.DeepEqual(got, next) {
		t.Errorf("Update returned %+v, want %+v", got, next)
	}

	loaded, err := store.Get(old.ID)
	if err != nil {
		t.Fatalf("Get after Update: %v", err)
	}
	if !reflect.DeepEqual(loaded, next) {
		t.Errorf("record after Update = %+v, want the new record %+v", loaded, next)
	}
	if !loaded.InstalledAt.Equal(old.InstalledAt) {
		t.Errorf("installedAt after Update = %s, want the original install time %s preserved",
			loaded.InstalledAt, old.InstalledAt)
	}
	if !loaded.UpdatedAt.Equal(next.UpdatedAt) {
		t.Errorf("updatedAt after Update = %s, want the refreshed adoption-event time %s",
			loaded.UpdatedAt, next.UpdatedAt)
	}
	if !loaded.UpdatedAt.After(loaded.InstalledAt) {
		t.Error("updatedAt after Update is not after installedAt, want the update event later than the install")
	}

	var fileRec InstalledStandardRecord
	if err := json.Unmarshal(readRecordFile(t, recordFilePath(store, old.ID)), &fileRec); err != nil {
		t.Fatalf("record file not decodable after Update: %v", err)
	}
	if !reflect.DeepEqual(fileRec, next) {
		t.Errorf("record file after Update = %+v, want the new record %+v", fileRec, next)
	}
}

// TestInstalledStandardStoreUpdateRequiresExistingRecord asserts Update
// of a standard that was never recorded fails with wrapped
// ErrRecordNotFound: update targets an existing recorded state (T-008;
// TS-014-03-03).
func TestInstalledStandardStoreUpdateRequiresExistingRecord(t *testing.T) {
	store := newTestStore(t)
	_, err := store.Update("anvil-standard-laravel", sampleRecord("anvil-standard-laravel", "1.2.3"))
	if !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("err = %v, want wrapped ErrRecordNotFound", err)
	}
	if !strings.Contains(err.Error(), "install the standard first") {
		t.Errorf("error %q does not name the install-first recovery", err)
	}
}

// TestInstalledStandardStoreUpdateValidatesRecord asserts Update rejects
// structurally invalid records with ErrRecordInvalid.
func TestInstalledStandardStoreUpdateValidatesRecord(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	bad := rec
	bad.Version = ""
	if _, err := store.Update(rec.ID, bad); !errors.Is(err, ErrRecordInvalid) {
		t.Fatalf("err = %v, want wrapped ErrRecordInvalid", err)
	}
}

// TestInstalledStandardStoreDelete asserts Delete removes the record
// (rollback support) and fails with wrapped ErrRecordNotFound when there
// is nothing to delete.
func TestInstalledStandardStoreDelete(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")

	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := store.Delete(rec.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(recordFilePath(store, rec.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("record file still exists after Delete (stat err = %v)", err)
	}
	if _, err := store.Get(rec.ID); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("Get after Delete: err = %v, want wrapped ErrRecordNotFound", err)
	}
	if err := store.Delete(rec.ID); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("second Delete: err = %v, want wrapped ErrRecordNotFound", err)
	}
}

// TestInstalledStandardStoreList asserts List returns the id + version
// summary of every recorded standard sorted by id (server-side
// enumeration), with an empty store listing as empty.
func TestInstalledStandardStoreList(t *testing.T) {
	store := newTestStore(t)
	zeta := sampleRecord("anvil-standard-zeta", "3.0.0")
	flutter := sampleRecord("anvil-standard-flutter", "2.0.0")
	laravel := sampleRecord("anvil-standard-laravel", "1.2.3")

	for _, rec := range []InstalledStandardRecord{zeta, flutter, laravel} {
		if _, created, err := store.Record(rec.ID, rec); err != nil || !created {
			t.Fatalf("Record %s: created=%v err=%v", rec.ID, created, err)
		}
	}

	summaries, corrupt, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(corrupt) != 0 {
		t.Errorf("corrupt = %+v, want none", corrupt)
	}
	wantIDs := []string{"anvil-standard-flutter", "anvil-standard-laravel", "anvil-standard-zeta"}
	if len(summaries) != len(wantIDs) {
		t.Fatalf("List returned %d summaries, want %d", len(summaries), len(wantIDs))
	}
	for i, wantID := range wantIDs {
		s := summaries[i]
		if s.ID != wantID {
			t.Errorf("summary[%d].ID = %q, want %q (sorted by id)", i, s.ID, wantID)
		}
	}
	if summaries[0].Version != "2.0.0" || summaries[1].Version != "1.2.3" || summaries[2].Version != "3.0.0" {
		t.Errorf("summary versions = %s, %s, %s; want 2.0.0, 1.2.3, 3.0.0",
			summaries[0].Version, summaries[1].Version, summaries[2].Version)
	}
	if summaries[0].ContractVersion != "1.0.0" || summaries[0].InstalledAt.IsZero() {
		t.Errorf("summary core fields not carried: %+v", summaries[0])
	}

	empty := newTestStore(t)
	if summaries, corrupt, err := empty.List(); err != nil || len(summaries) != 0 || len(corrupt) != 0 {
		t.Errorf("empty store List = (%v, %v, %v), want (nil, nil, nil)", summaries, corrupt, err)
	}
}

// TestInstalledStandardStoreListSkipsCorruptRecords asserts a corrupt
// record file does not kill the listing: it is skipped and reported with
// its path and reason while healthy records still list (TS-014-03-03
// deliverable: corrupt single record must not kill the whole store).
func TestInstalledStandardStoreListSkipsCorruptRecords(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	brokenPath := recordFilePath(store, "anvil-standard-broken")
	if err := os.WriteFile(brokenPath, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write corrupt record file: %v", err)
	}

	summaries, corrupt, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != rec.ID {
		t.Fatalf("summaries = %+v, want only the healthy record %s", summaries, rec.ID)
	}
	if len(corrupt) != 1 {
		t.Fatalf("corrupt = %+v, want exactly the broken file reported", corrupt)
	}
	if corrupt[0].Path != brokenPath {
		t.Errorf("corrupt[0].Path = %q, want %q", corrupt[0].Path, brokenPath)
	}
	if !strings.Contains(corrupt[0].Error, "not decodable") {
		t.Errorf("corrupt[0].Error = %q, want the decode reason", corrupt[0].Error)
	}
	if !strings.Contains(corrupt[0].Error, "re-install") {
		t.Errorf("corrupt[0].Error = %q, want the recovery hint", corrupt[0].Error)
	}
}

// TestInstalledStandardStoreCorruptRecordReportsAndRecovers asserts the
// corruption lifecycle: Get fails with an actionable ErrRecordCorrupt
// naming the file, and a fresh Record over the corrupt file recovers the
// store (recovery by re-adoption, no manual file surgery).
func TestInstalledStandardStoreCorruptRecordReportsAndRecovers(t *testing.T) {
	store := newTestStore(t)
	path := recordFilePath(store, "anvil-standard-laravel")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write corrupt record file: %v", err)
	}

	_, err := store.Get("anvil-standard-laravel")
	if !errors.Is(err, ErrRecordCorrupt) {
		t.Fatalf("Get corrupt: err = %v, want wrapped ErrRecordCorrupt", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not name the corrupt file", err)
	}

	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	got, created, err := store.Record(rec.ID, rec)
	if err != nil {
		t.Fatalf("Record over corrupt file: %v", err)
	}
	if !created {
		t.Error("created = false, want true (corrupt record replaced by the new write)")
	}
	if !reflect.DeepEqual(got, rec) {
		t.Errorf("recovered record = %+v, want %+v", got, rec)
	}
	if loaded, err := store.Get(rec.ID); err != nil || !reflect.DeepEqual(loaded, rec) {
		t.Errorf("Get after recovery = (%+v, %v), want (%+v, nil)", loaded, err, rec)
	}
}

// TestInstalledStandardStoreCorruptRecordRecoversByUpdate asserts Update
// over a corrupt record replaces it (recovery by explicit update).
func TestInstalledStandardStoreCorruptRecordRecoversByUpdate(t *testing.T) {
	store := newTestStore(t)
	if err := os.WriteFile(recordFilePath(store, "anvil-standard-laravel"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write corrupt record file: %v", err)
	}

	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	got, err := store.Update(rec.ID, rec)
	if err != nil {
		t.Fatalf("Update over corrupt file: %v", err)
	}
	if !reflect.DeepEqual(got, rec) {
		t.Errorf("recovered record = %+v, want %+v", got, rec)
	}
}

// TestInstalledStandardStoreSurvivesRestart asserts the record is pure
// file persistence: a fresh store instance over the same directory (a
// process restart) reads the same records (TS-014-03-03 DoD: the record
// survives restarts and is readable by downstream flows).
func TestInstalledStandardStoreSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	first := NewInstalledStandardStore(dir)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	if _, _, err := first.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	second := NewInstalledStandardStore(dir)
	loaded, err := second.Get(rec.ID)
	if err != nil {
		t.Fatalf("Get via restarted store: %v", err)
	}
	if !reflect.DeepEqual(loaded, rec) {
		t.Errorf("record after restart = %+v, want %+v", loaded, rec)
	}
	summaries, corrupt, err := second.List()
	if err != nil || len(summaries) != 1 || len(corrupt) != 0 {
		t.Errorf("List after restart = (%v, %v, %v), want one summary and no corrupt", summaries, corrupt, err)
	}
}

// TestInstalledStandardStoreRoundTripValidationResults asserts embedded
// validation results (CompatibilityResult + TrustResult JSON, T-010/T-011)
// survive the record round trip byte-for-byte: the store stores their
// JSON and never interprets them (TS-014-03-03).
func TestInstalledStandardStoreRoundTripValidationResults(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	rec.Compatibility = &CompatibilityResult{
		Valid:                     true,
		ContractVersionCompatible: true,
		CapabilityCompatible:      true,
		FrameworkVersionChecked:   true,
		DeclaredContractVersion:   "1.0.0",
		DeclaredFrameworkVersions: []string{"5.1.0", "5.2.0"},
		SupportedContractMajors:   []int{1},
		ProjectFrameworkVersion:   "5.2.3",
	}
	rec.Trust = &TrustResult{
		Valid:               true,
		IntegrityVerified:   true,
		AttestationVerified: true,
		AnchorMatched:       true,
		Publisher:           rec.ID,
		DeclaredDigests: []DeclaredDigest{{
			Algorithm: DigestAlgorithmSHA256,
			Encoding:  DigestEncodingBase16,
			Digest:    "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		}},
	}

	if _, created, err := store.Record(rec.ID, rec); err != nil || !created {
		t.Fatalf("Record: created=%v err=%v", created, err)
	}

	restarted := NewInstalledStandardStore(store.Dir())
	loaded, err := restarted.Get(rec.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(loaded, rec) {
		t.Errorf("record round trip = %+v, want %+v", loaded, rec)
	}
	if loaded.Compatibility == nil || loaded.Trust == nil {
		t.Fatal("embedded validation results lost in the round trip")
	}
}

// TestInstalledStandardStoreAtomicWriteLeavesNoTempFiles asserts the
// atomic write (temp file + rename) leaves no temp files behind: the
// store directory holds exactly the record files.
func TestInstalledStandardStoreAtomicWriteLeavesNoTempFiles(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if _, err := store.Update(rec.ID, sampleRecord(rec.ID, "1.3.0")); err != nil {
		t.Fatalf("Update: %v", err)
	}

	entries, err := os.ReadDir(store.Dir())
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("store directory contains %v, want exactly [%s.json]", names, rec.ID)
	}
	if entries[0].Name() != rec.ID+".json" {
		t.Errorf("store file = %q, want %q", entries[0].Name(), rec.ID+".json")
	}
}

// TestInstalledStandardStoreFileWithMismatchedIDIsCorrupt asserts a
// record file that declares an id different from its file name is corrupt
// — the file name is the record key, identity from content must agree
// with it.
func TestInstalledStandardStoreFileWithMismatchedIDIsCorrupt(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-flutter", "2.0.0")
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// The file name says laravel; the content says flutter.
	path := recordFilePath(store, "anvil-standard-laravel")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write record file: %v", err)
	}

	_, err = store.Get("anvil-standard-laravel")
	if !errors.Is(err, ErrRecordCorrupt) {
		t.Fatalf("err = %v, want wrapped ErrRecordCorrupt", err)
	}
	if !strings.Contains(err.Error(), "does not match the record key") {
		t.Errorf("error %q does not name the id mismatch", err)
	}
}

// TestInstalledStandardStoreFileWithTrailingContentIsCorrupt asserts a
// record file with content after the JSON document is corrupt (pinned
// format, mirrors the trust anchors load).
func TestInstalledStandardStoreFileWithTrailingContentIsCorrupt(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := recordFilePath(store, rec.ID)
	if err := os.WriteFile(path, append(raw, []byte("{}")...), 0o644); err != nil {
		t.Fatalf("write record file: %v", err)
	}

	_, err = store.Get(rec.ID)
	if !errors.Is(err, ErrRecordCorrupt) {
		t.Fatalf("err = %v, want wrapped ErrRecordCorrupt", err)
	}
	if !strings.Contains(err.Error(), "unexpected content after the record document") {
		t.Errorf("error %q does not name the trailing content", err)
	}
}

// TestInstalledStandardStoreFileWithUnknownFieldIsCorrupt asserts a
// record file carrying an unknown top-level field is corrupt: the record
// format is pinned, not extensible.
func TestInstalledStandardStoreFileWithUnknownFieldIsCorrupt(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	raw = bytes.TrimSuffix(raw, []byte("}"))
	raw = append(raw, []byte(",\"sneaky\":true}")...)

	path := recordFilePath(store, rec.ID)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write record file: %v", err)
	}

	_, err = store.Get(rec.ID)
	if !errors.Is(err, ErrRecordCorrupt) {
		t.Fatalf("err = %v, want wrapped ErrRecordCorrupt", err)
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("error %q does not name the unknown field", err)
	}
}

// TestInstalledStandardStoreOversizedRecordIsCorrupt asserts a record
// file beyond MaxRecordSize fails with an actionable ErrRecordCorrupt
// naming the cap, not with unbounded memory use.
func TestInstalledStandardStoreOversizedRecordIsCorrupt(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	oversize := append(raw, bytes.Repeat([]byte(" "), MaxRecordSize)...)

	path := recordFilePath(store, rec.ID)
	if err := os.WriteFile(path, oversize, 0o644); err != nil {
		t.Fatalf("write record file: %v", err)
	}

	_, err = store.Get(rec.ID)
	if !errors.Is(err, ErrRecordCorrupt) {
		t.Fatalf("err = %v, want wrapped ErrRecordCorrupt", err)
	}
	if !strings.Contains(err.Error(), "size cap") {
		t.Errorf("error %q does not name the size cap", err)
	}
}

// TestInstalledStandardStoreLifecycleStateRecorded asserts the lifecycle
// state at install time is part of the record and survives the round
// trip (TS-014-03-03: lifecycle state at install time is recorded).
func TestInstalledStandardStoreLifecycleStateRecorded(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	rec.Lifecycle = Lifecycle{
		State:       LifecycleStateDeprecated,
		RemovalDate: "2027-01-15T00:00:00Z",
	}

	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}
	loaded, err := NewInstalledStandardStore(store.Dir()).Get(rec.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(loaded.Lifecycle, rec.Lifecycle) {
		t.Errorf("lifecycle = %+v, want %+v", loaded.Lifecycle, rec.Lifecycle)
	}
}

// TestInstalledStandardStoreUnreadableStoreDir asserts a store directory
// that cannot be read as a directory surfaces as an actionable
// ErrStoreUnreadable on reads and a precise write error on writes — the
// unreadable-store-dir failure semantics of TS-014-03-03.
func TestInstalledStandardStoreUnreadableStoreDir(t *testing.T) {
	// A regular file where the store directory must be: every
	// directory-level operation fails deterministically (ENOTDIR), no
	// root-vs-permission flakiness.
	dirFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(dirFile, []byte("file, not dir"), 0o644); err != nil {
		t.Fatalf("write dir-file: %v", err)
	}
	store := NewInstalledStandardStore(dirFile)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")

	if _, err := store.Get(rec.ID); !errors.Is(err, ErrStoreUnreadable) {
		t.Errorf("Get: err = %v, want wrapped ErrStoreUnreadable", err)
	}
	if _, _, err := store.List(); !errors.Is(err, ErrStoreUnreadable) {
		t.Errorf("List: err = %v, want wrapped ErrStoreUnreadable", err)
	}
	if _, _, err := store.Record(rec.ID, rec); err == nil {
		t.Error("Record over an unreadable store dir succeeded, want error")
	}
	if _, err := store.Update(rec.ID, rec); err == nil {
		t.Error("Update over an unreadable store dir succeeded, want error")
	}
	if err := store.Delete(rec.ID); err == nil {
		t.Error("Delete over an unreadable store dir succeeded, want error")
	}
}

// TestInstalledStandardStoreAtomicReplaceFailureLeavesNoCorruption
// asserts a failed atomic replace (the rename step cannot complete) does
// not corrupt the existing record and leaves no temp file behind.
func TestInstalledStandardStoreAtomicReplaceFailureLeavesNoCorruption(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}
	path := recordFilePath(store, rec.ID)

	// A directory squatting on the record path makes the rename step
	// fail deterministically (a file cannot replace a directory).
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove record file: %v", err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("squat on record path: %v", err)
	}

	next := sampleRecord(rec.ID, "1.3.0")
	if _, err := store.Update(rec.ID, next); err == nil {
		t.Fatal("Update with un-replaceable target succeeded, want error")
	}
	if _, _, err := store.Record(rec.ID, next); err == nil {
		t.Fatal("Record with un-replaceable target succeeded, want error")
	}

	// The failed writes must not leave temp files behind.
	entries, err := os.ReadDir(store.Dir())
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".tmp-") {
			t.Errorf("failed write left temp file %q behind", entry.Name())
		}
	}

	// Recovery: remove the obstruction; the store is fully usable again
	// (the original record file was removed as part of the obstruction
	// setup, so the next write records fresh state).
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove obstruction: %v", err)
	}
	if _, err := store.Get(rec.ID); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("Get after obstruction removal: err = %v, want wrapped ErrRecordNotFound", err)
	}
	if _, err := store.Update(rec.ID, next); err == nil {
		t.Fatal("Update after obstruction removal succeeded, want ErrRecordNotFound (no record to update)")
	}
	if _, created, err := store.Record(rec.ID, next); err != nil || !created {
		t.Fatalf("Record after obstruction removed: created=%v err=%v", created, err)
	}
	if loaded, err := store.Get(rec.ID); err != nil || !reflect.DeepEqual(loaded, next) {
		t.Errorf("record after recovery = (%+v, %v), want (%+v, nil)", loaded, err, next)
	}
}

// TestInstalledStandardStoreFormatVersionWrittenAndEnforced asserts the
// record format version is written with every record and enforced on
// read: a file carrying an unsupported version — the legacy version it
// was migrated from is supported (ST-021-04 migration) — is corrupt: the
// record format is pinned (CR: format version + typed validation results;
// migration path = bump RecordFormatVersion and teach reads to handle the
// previous versions).
func TestInstalledStandardStoreFormatVersionWrittenAndEnforced(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// Written: the persisted file carries the current format version.
	var fileRec InstalledStandardRecord
	if err := json.Unmarshal(readRecordFile(t, recordFilePath(store, rec.ID)), &fileRec); err != nil {
		t.Fatalf("record file not decodable: %v", err)
	}
	if fileRec.FormatVersion != RecordFormatVersion {
		t.Errorf("persisted formatVersion = %d, want %d", fileRec.FormatVersion, RecordFormatVersion)
	}

	// Enforced: an unknown/future-format file is rejected as corrupt, on
	// Get and in List, with an actionable message naming the format
	// version. The legacy format-1 version stays readable (migration,
	// ST-021-04) and is exercised by
	// TestInstalledStandardStoreLegacyRecordReadable.
	for _, version := range []int{0, 3} {
		future := rec
		future.FormatVersion = version
		raw, err := json.Marshal(future)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := os.WriteFile(recordFilePath(store, rec.ID), raw, 0o644); err != nil {
			t.Fatalf("write record file: %v", err)
		}

		_, err = store.Get(rec.ID)
		if !errors.Is(err, ErrRecordCorrupt) {
			t.Fatalf("formatVersion %d: Get err = %v, want wrapped ErrRecordCorrupt", version, err)
		}
		if !strings.Contains(err.Error(), "formatVersion") || !strings.Contains(err.Error(), "not readable") {
			t.Errorf("formatVersion %d: error %q does not name the format version", version, err)
		}

		summaries, corrupt, err := store.List()
		if err != nil || len(summaries) != 0 {
			t.Fatalf("formatVersion %d: List = (%v, err %v), want no summaries", version, summaries, err)
		}
		if len(corrupt) != 1 || !strings.Contains(corrupt[0].Error, "not readable") {
			t.Errorf("formatVersion %d: corrupt = %+v, want one report naming the format version", version, corrupt)
		}
	}
}

// TestInstalledStandardStoreLegacyRecordReadable verifies the ST-021-04
// format-bump migration: a record file written in the legacy format
// version (1) stays fully readable after the bump — Get, List, and
// ListRecords all surface it — and its Skills section defaults to empty
// (nil), so existing records remain usable with no data loss. The legacy
// record is never rewritten in place; the next write upgrades it.
func TestInstalledStandardStoreLegacyRecordReadable(t *testing.T) {
	store := newTestStore(t)

	// A legacy format-1 record: the full pre-T-006 shape, no skills[]
	// field (mirrors a record written by the W2 code, TS-014-03-03).
	legacy := sampleRecord("anvil-standard-legacy", "1.0.0")
	legacy.FormatVersion = LegacyRecordFormatVersion
	raw, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatalf("marshal legacy record: %v", err)
	}
	if strings.Contains(string(raw), "skills") {
		t.Fatal("legacy fixture unexpectedly carries a skills field")
	}
	if err := os.WriteFile(recordFilePath(store, legacy.ID), raw, 0o644); err != nil {
		t.Fatalf("write legacy record file: %v", err)
	}

	// Get reads it back intact: same identity/version, Skills nil.
	got, err := store.Get(legacy.ID)
	if err != nil {
		t.Fatalf("Get legacy record: %v", err)
	}
	if got.ID != legacy.ID || got.Version != legacy.Version || got.FormatVersion != LegacyRecordFormatVersion {
		t.Errorf("Get legacy = %+v, want the legacy record untouched", got)
	}
	if got.Skills != nil {
		t.Errorf("Get legacy Skills = %v, want nil (default empty for pre-T-006 records)", got.Skills)
	}

	// List and ListRecords enumerate it (not corrupt).
	summaries, corrupt, err := store.List()
	if err != nil || len(corrupt) != 0 {
		t.Fatalf("List = (%v, corrupt %v, err %v), want one summary and no corrupt", summaries, corrupt, err)
	}
	if len(summaries) != 1 || summaries[0].ID != legacy.ID {
		t.Errorf("List = %+v, want the legacy standard", summaries)
	}
	records, corrupt, err := store.ListRecords()
	if err != nil || len(corrupt) != 0 {
		t.Fatalf("ListRecords = (%v, corrupt %v, err %v), want one record and no corrupt", records, corrupt, err)
	}
	if len(records) != 1 || records[0].Skills != nil || records[0].Version != legacy.Version {
		t.Errorf("ListRecords = %+v, want the legacy record with empty Skills", records)
	}
}

// TestInstalledStandardStoreSkillsDeclarationRoundTrip verifies the
// ST-021-04 Skills extension: a record written with skill declarations
// persists and reads them back exactly (name, version, asset,
// description), and the record file carries the declarations under
// "skills".
func TestInstalledStandardStoreSkillsDeclarationRoundTrip(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	rec.Skills = []SkillDeclaration{
		{Name: "overview", Version: "1.0.0", Asset: "anvil-skill-overview-1-0-0", Description: "Anvil overview skill"},
		{Name: "lifecycle", Version: "1.0.0", Asset: "anvil-skill-lifecycle-1-0-0"},
	}

	got, created, err := store.Record(rec.ID, rec)
	if err != nil || !created {
		t.Fatalf("Record: created=%v err=%v", created, err)
	}
	if !reflect.DeepEqual(got, rec) {
		t.Errorf("Record returned %+v, want %+v", got, rec)
	}

	// The persisted file carries the declarations.
	raw := readRecordFile(t, recordFilePath(store, rec.ID))
	if !strings.Contains(string(raw), `"skills"`) {
		t.Errorf("record file lacks the skills section:\n%s", raw)
	}
	loaded, err := store.Get(rec.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(loaded.Skills, rec.Skills) {
		t.Errorf("Get Skills = %+v, want %+v", loaded.Skills, rec.Skills)
	}
	records, _, err := store.ListRecords()
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	if len(records) != 1 || !reflect.DeepEqual(records[0].Skills, rec.Skills) {
		t.Errorf("ListRecords Skills = %+v, want %+v", records, rec.Skills)
	}
}

// TestInstalledStandardStoreRecordRejectsInvalidSkillDeclarations asserts
// the structural skill-declaration validation: a record carrying a
// declaration with an unsafe name, an empty version, or an empty asset is
// rejected with ErrRecordInvalid on write, and a hand-edited record file
// carrying one is corrupt on read.
func TestInstalledStandardStoreRecordRejectsInvalidSkillDeclarations(t *testing.T) {
	cases := []struct {
		name string
		sk   SkillDeclaration
		want string
	}{
		{name: "unsafe name", sk: SkillDeclaration{Name: "Bad_Name", Version: "1.0.0", Asset: "anvil-skill-bad-1-0-0"}, want: "not a safe skill identifier"},
		{name: "empty version", sk: SkillDeclaration{Name: "overview", Version: "", Asset: "anvil-skill-overview-1-0-0"}, want: "skills[0].version must not be empty"},
		{name: "empty asset", sk: SkillDeclaration{Name: "overview", Version: "1.0.0", Asset: ""}, want: "skills[0].asset must not be empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newTestStore(t)
			rec := sampleRecord("anvil-standard-laravel", "1.2.3")
			rec.Skills = []SkillDeclaration{tc.sk}

			_, _, err := store.Record(rec.ID, rec)
			if !errors.Is(err, ErrRecordInvalid) {
				t.Fatalf("Record: err = %v, want wrapped ErrRecordInvalid", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not carry %q", err, tc.want)
			}
		})
	}
}

// TestInstalledStandardStoreWriteSideOversizeRejected asserts the write
// path rejects a record larger than MaxRecordSize with ErrRecordInvalid
// naming the cap BEFORE any file is created: the store never persists a
// record its own read path would classify as corrupt (write-side size
// cap).
func TestInstalledStandardStoreWriteSideOversizeRejected(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	// A resolution source larger than the whole cap makes the marshaled
	// record oversize regardless of the rest of the content.
	rec.Resolution.Source = strings.Repeat("x", MaxRecordSize)

	_, _, err := store.Record(rec.ID, rec)
	if !errors.Is(err, ErrRecordInvalid) {
		t.Fatalf("Record oversize: err = %v, want wrapped ErrRecordInvalid", err)
	}
	if !strings.Contains(err.Error(), "exceeding the 1048576-byte cap") {
		t.Errorf("error %q does not name the cap", err)
	}

	// Update is rejected the same way (same write path), provided a
	// record exists to update.
	seeded := sampleRecord(rec.ID, "1.0.0")
	if _, created, err := store.Record(seeded.ID, seeded); err != nil || !created {
		t.Fatalf("seed Record: created=%v err=%v", created, err)
	}
	oversized := seeded
	oversized.Version = "2.0.0"
	oversized.UpdatedAt = seeded.InstalledAt.Add(time.Hour)
	oversized.Resolution.Source = strings.Repeat("x", MaxRecordSize)
	if _, err := store.Update(seeded.ID, oversized); !errors.Is(err, ErrRecordInvalid) {
		t.Fatalf("Update oversize: err = %v, want wrapped ErrRecordInvalid", err)
	}

	// The rejected write must not disturb the seeded record, and no
	// temp file may exist.
	loaded, err := store.Get(seeded.ID)
	if err != nil {
		t.Fatalf("Get after rejected oversize update: %v", err)
	}
	if !reflect.DeepEqual(loaded, seeded) {
		t.Errorf("record after rejected oversize update = %+v, want the seeded record %+v", loaded, seeded)
	}
	entries, err := os.ReadDir(store.Dir())
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("store directory contains %v after rejected oversize writes, want only the seeded record file", names)
	}
	if entries[0].Name() != seeded.ID+".json" {
		t.Errorf("store file = %q, want %q", entries[0].Name(), seeded.ID+".json")
	}
}

// TestInstalledStandardStoreSymlinkedRecordRejected asserts a symlink
// occupying a record path is rejected as corrupt — the record store is a
// plain file store and reads never follow links (mirroring the index
// client's symlink rejection, index.go) — on Get and reported by List.
func TestInstalledStandardStoreSymlinkedRecordRejected(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")

	// A valid record behind the link: reads must refuse to follow it.
	target := filepath.Join(t.TempDir(), "record-target.json")
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(target, raw, 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Symlink(target, recordFilePath(store, rec.ID)); err != nil {
		t.Skipf("symlinks not supported on this platform: %v", err)
	}

	_, err = store.Get(rec.ID)
	if !errors.Is(err, ErrRecordCorrupt) {
		t.Fatalf("Get symlinked record: err = %v, want wrapped ErrRecordCorrupt", err)
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("error %q does not name the symlink", err)
	}

	summaries, corrupt, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 0 {
		t.Errorf("summaries = %+v, want none (symlinked record must not list)", summaries)
	}
	if len(corrupt) != 1 || !strings.Contains(corrupt[0].Error, "symlink") {
		t.Errorf("corrupt = %+v, want one report naming the symlink", corrupt)
	}
}

// TestInstalledStandardStoreDirectoryAtRecordPathIsCorrupt asserts a
// directory occupying a record path is classified as a corrupt record —
// consistently in Get and List (directory classification is done before
// open).
func TestInstalledStandardStoreDirectoryAtRecordPathIsCorrupt(t *testing.T) {
	store := newTestStore(t)
	path := recordFilePath(store, "anvil-standard-laravel")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir at record path: %v", err)
	}

	_, err := store.Get("anvil-standard-laravel")
	if !errors.Is(err, ErrRecordCorrupt) {
		t.Fatalf("Get: err = %v, want wrapped ErrRecordCorrupt", err)
	}
	if !strings.Contains(err.Error(), "is a directory") {
		t.Errorf("error %q does not name the directory", err)
	}

	summaries, corrupt, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 0 {
		t.Errorf("summaries = %+v, want none", summaries)
	}
	if len(corrupt) != 1 || !strings.Contains(corrupt[0].Error, "is a directory") {
		t.Errorf("corrupt = %+v, want one report naming the directory", corrupt)
	}
}

// TestDefaultInstalledStandardsDir asserts the default store directory
// follows the ADR-005 §7.1 global config convention: the Anvil global
// config directory plus installed-standards.
func TestDefaultInstalledStandardsDir(t *testing.T) {
	dir, err := DefaultInstalledStandardsDir()
	if err != nil {
		t.Fatalf("DefaultInstalledStandardsDir: %v", err)
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("default dir %q is not absolute", dir)
	}
	wantSuffix := filepath.Join("anvil", DefaultInstalledStandardsDirName)
	if !strings.HasSuffix(filepath.Clean(dir), filepath.Clean(wantSuffix)) {
		t.Errorf("default dir %q does not end with %q (ADR-005 §7.1 convention)", dir, wantSuffix)
	}
}
