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

// cloneSkillRecord returns a shallow copy with a fresh Targets slice so
// mutation of one copy cannot corrupt the original.
func cloneSkillRecord(r InstalledSkillRecord) InstalledSkillRecord {
	r.Targets = append([]InstalledSkillTarget(nil), r.Targets...)
	return r
}

// sampleSkillRecord returns a structurally valid installed-skill record.
func sampleSkillRecord(id, version string) InstalledSkillRecord {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	return InstalledSkillRecord{
		FormatVersion: InstalledSkillRecordFormatVersion,
		ID:            id,
		Version:       version,
		Source:        "core",
		Resolution: Resolution{
			Kind:   SkillResolutionKindCore,
			Source: "embedded",
		},
		InstalledAt: now,
		UpdatedAt:   now,
		Targets: []InstalledSkillTarget{{
			Agent: "opencode",
			Scope: SkillScopeRepo,
			Path:  filepath.Join("/tmp", "project", ".agents", "skills", id),
		}},
	}
}

// sampleStandardRecord returns a structurally valid installed-standard
// record for fake stale-source lookups.
func sampleStandardRecord(id, version, state string) InstalledStandardRecord {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	return InstalledStandardRecord{
		FormatVersion:   RecordFormatVersion,
		ID:              id,
		Version:         version,
		ContractVersion: "1.0.0",
		Resolution: Resolution{
			Kind:   ResolutionKindDistribution,
			Source: "https://example.com/" + id,
		},
		InstalledAt: now,
		UpdatedAt:   now,
		Lifecycle: Lifecycle{
			State:       state,
			RemovalDate: "2027-01-01T00:00:00Z",
		},
	}
}

// fakeStandardLookup is a test double for StandardLookup.
type fakeStandardLookup map[string]InstalledStandardRecord

func (f fakeStandardLookup) Get(id string) (InstalledStandardRecord, error) {
	if r, ok := f[id]; ok {
		return r, nil
	}
	return InstalledStandardRecord{}, ErrRecordNotFound
}

func newSkillTestStore(t *testing.T) *InstalledSkillStore {
	t.Helper()
	return NewInstalledSkillStore(t.TempDir())
}

func skillRecordFilePath(store *InstalledSkillStore, id string) string {
	return filepath.Join(store.Dir(), id+".json")
}

func readSkillRecordFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read record file %s: %v", path, err)
	}
	return raw
}

func TestInstalledSkillStoreRecordPersists(t *testing.T) {
	store := newSkillTestStore(t)
	rec := sampleSkillRecord("anvil-overview", "2.1.0")

	got, created, err := store.Record(rec.ID, rec)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if !created {
		t.Error("created = false, want true")
	}
	if !reflect.DeepEqual(got, rec) {
		t.Errorf("Record returned %+v, want %+v", got, rec)
	}

	path := skillRecordFilePath(store, rec.ID)
	raw := readSkillRecordFile(t, path)

	var decoded InstalledSkillRecord
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("record file not decodable: %v", err)
	}
	if !reflect.DeepEqual(decoded, rec) {
		t.Errorf("record file decodes to %+v, want %+v", decoded, rec)
	}
	if !bytes.HasSuffix(raw, []byte("\n")) {
		t.Error("record file does not end with newline")
	}
}

func TestInstalledSkillStoreRecordIdempotentSameVersion(t *testing.T) {
	store := newSkillTestStore(t)
	rec := sampleSkillRecord("anvil-overview", "2.1.0")

	if _, created, err := store.Record(rec.ID, rec); err != nil || !created {
		t.Fatalf("first Record: created=%v err=%v", created, err)
	}
	path := skillRecordFilePath(store, rec.ID)
	before := readSkillRecordFile(t, path)

	reinstall := sampleSkillRecord(rec.ID, rec.Version)
	reinstall.InstalledAt = reinstall.InstalledAt.Add(24 * time.Hour)
	reinstall.UpdatedAt = reinstall.InstalledAt
	reinstall.Targets = []InstalledSkillTarget{{
		Agent: "codex",
		Scope: SkillScopeGlobal,
		Path:  "/home/user/.agents/skills/anvil-overview",
	}}

	got, created, err := store.Record(rec.ID, reinstall)
	if err != nil {
		t.Fatalf("idempotent Record: %v", err)
	}
	if created {
		t.Error("created = true, want false")
	}
	if !reflect.DeepEqual(got, rec) {
		t.Errorf("idempotent Record returned %+v, want existing %+v", got, rec)
	}
	if after := readSkillRecordFile(t, path); !bytes.Equal(before, after) {
		t.Error("record file changed on idempotent re-record")
	}
}

func TestInstalledSkillStoreRecordRejectsVersionChange(t *testing.T) {
	store := newSkillTestStore(t)
	v1 := sampleSkillRecord("anvil-overview", "2.1.0")

	if _, created, err := store.Record(v1.ID, v1); err != nil || !created {
		t.Fatalf("first Record: created=%v err=%v", created, err)
	}

	v2 := sampleSkillRecord(v1.ID, "2.2.0")
	_, _, err := store.Record(v1.ID, v2)
	if !errors.Is(err, ErrSkillRecordVersionConflict) {
		t.Fatalf("err = %v, want ErrSkillRecordVersionConflict", err)
	}
	for _, want := range []string{"2.1.0", "2.2.0", "update"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}

	got, err := store.Get(v1.ID)
	if err != nil {
		t.Fatalf("Get after rejected change: %v", err)
	}
	if got.Version != v1.Version {
		t.Errorf("version = %q, want %q", got.Version, v1.Version)
	}
}

func TestInstalledSkillStoreRecordRejectsInvalidRecords(t *testing.T) {
	valid := sampleSkillRecord("anvil-overview", "2.1.0")

	cases := []struct {
		name string
		id   string
		rec  InstalledSkillRecord
		want string
	}{
		{"empty id", "", valid, "id must not be empty"},
		{"id with slash", "a/b", valid, "not a safe record key"},
		{"id with dot", "anvil.overview", valid, "not a safe record key"},
		{"id dot dot", "..", valid, "not a safe record key"},
		{"id starts hyphen", "-skill", valid, "not a safe record key"},
		{"id uppercase", "Anvil-Overview", valid, "not a safe record key"},
		{"id too long", strings.Repeat("a", 65), valid, "not a safe record key"},
		{"record id mismatch", valid.ID, func() InstalledSkillRecord { r := cloneSkillRecord(valid); r.ID = "anvil-other"; return r }(), "does not match the record key"},
		{"empty version", valid.ID, func() InstalledSkillRecord { r := cloneSkillRecord(valid); r.Version = ""; return r }(), "version must not be empty"},
		{"empty source", valid.ID, func() InstalledSkillRecord { r := cloneSkillRecord(valid); r.Source = ""; return r }(), "source must not be empty"},
		{"invalid source", valid.ID, func() InstalledSkillRecord { r := cloneSkillRecord(valid); r.Source = "not valid"; return r }(), "source \"not valid\" is invalid"},
		{"empty resolution kind", valid.ID, func() InstalledSkillRecord { r := cloneSkillRecord(valid); r.Resolution.Kind = ""; return r }(), "resolution.kind must not be empty"},
		{"unknown resolution kind", valid.ID, func() InstalledSkillRecord { r := cloneSkillRecord(valid); r.Resolution.Kind = "git"; return r }(), "resolution.kind \"git\" is unknown"},
		{"empty resolution source", valid.ID, func() InstalledSkillRecord { r := cloneSkillRecord(valid); r.Resolution.Source = ""; return r }(), "resolution.source must not be empty"},
		{"zero installed at", valid.ID, func() InstalledSkillRecord { r := cloneSkillRecord(valid); r.InstalledAt = time.Time{}; return r }(), "installedAt must be set"},
		{"zero updated at", valid.ID, func() InstalledSkillRecord { r := cloneSkillRecord(valid); r.UpdatedAt = time.Time{}; return r }(), "updatedAt must be set"},
		{"updated at before installed", valid.ID, func() InstalledSkillRecord {
			r := cloneSkillRecord(valid)
			r.UpdatedAt = r.InstalledAt.Add(-time.Hour)
			return r
		}(), "updatedAt must not be before installedAt"},
		{"updated at differs on record", valid.ID, func() InstalledSkillRecord {
			r := cloneSkillRecord(valid)
			r.UpdatedAt = r.InstalledAt.Add(time.Hour)
			return r
		}(), "does not equal installedAt"},
		{"missing format version", valid.ID, func() InstalledSkillRecord { r := cloneSkillRecord(valid); r.FormatVersion = 0; return r }(), "formatVersion must be 1"},
		{"unknown format version", valid.ID, func() InstalledSkillRecord { r := cloneSkillRecord(valid); r.FormatVersion = 2; return r }(), "formatVersion must be 1"},
		{"empty targets", valid.ID, func() InstalledSkillRecord { r := cloneSkillRecord(valid); r.Targets = nil; return r }(), "targets must not be empty"},
		{"empty target agent", valid.ID, func() InstalledSkillRecord { r := cloneSkillRecord(valid); r.Targets[0].Agent = ""; return r }(), "targets[0].agent must not be empty"},
		{"invalid target scope", valid.ID, func() InstalledSkillRecord { r := cloneSkillRecord(valid); r.Targets[0].Scope = "workspace"; return r }(), "targets[0].scope \"workspace\" is invalid"},
		{"empty target path", valid.ID, func() InstalledSkillRecord { r := cloneSkillRecord(valid); r.Targets[0].Path = ""; return r }(), "targets[0].path must not be empty"},
		{"relative target path", valid.ID, func() InstalledSkillRecord {
			r := cloneSkillRecord(valid)
			r.Targets[0].Path = ".agents/skills/anvil-overview"
			return r
		}(), "must be an absolute path"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newSkillTestStore(t)
			_, _, err := store.Record(tc.id, tc.rec)
			if !errors.Is(err, ErrSkillRecordInvalid) {
				t.Fatalf("err = %v, want ErrSkillRecordInvalid", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err, tc.want)
			}
			if entries, err := os.ReadDir(store.Dir()); err == nil && len(entries) != 0 {
				t.Errorf("store dir has %d entries after rejected record, want 0", len(entries))
			}
		})
	}
}

func TestInstalledSkillStoreRecordRejectsDuplicateTarget(t *testing.T) {
	store := newSkillTestStore(t)
	rec := sampleSkillRecord("anvil-overview", "2.1.0")
	rec.Targets = []InstalledSkillTarget{
		{Agent: "opencode", Scope: SkillScopeRepo, Path: "/tmp/project/.agents/skills/anvil-overview"},
		{Agent: "codex", Scope: SkillScopeGlobal, Path: "/home/user/.agents/skills/anvil-overview"},
		{Agent: "opencode", Scope: SkillScopeRepo, Path: "/tmp/project/.agents/skills/anvil-overview"},
	}

	_, _, err := store.Record(rec.ID, rec)
	if !errors.Is(err, ErrSkillRecordInvalid) {
		t.Fatalf("err = %v, want ErrSkillRecordInvalid", err)
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error %q does not contain 'duplicate'", err)
	}
	if !strings.Contains(err.Error(), "opencode") {
		t.Errorf("error %q does not name the duplicated agent", err)
	}
	if entries, err := os.ReadDir(store.Dir()); err == nil && len(entries) != 0 {
		t.Errorf("store dir has %d entries after rejected record, want 0", len(entries))
	}
}

func TestInstalledSkillStoreGet(t *testing.T) {
	store := newSkillTestStore(t)
	rec := sampleSkillRecord("anvil-overview", "2.1.0")

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

	_, err = store.Get("anvil-unknown")
	if !errors.Is(err, ErrSkillRecordNotFound) {
		t.Fatalf("Get unknown: err = %v, want ErrSkillRecordNotFound", err)
	}
	if !strings.Contains(err.Error(), skillRecordFilePath(store, "anvil-unknown")) {
		t.Errorf("error %q does not name record file", err)
	}

	_, err = store.Get("not/a/key")
	if !errors.Is(err, ErrSkillRecordInvalid) {
		t.Fatalf("Get unsafe key: err = %v, want ErrSkillRecordInvalid", err)
	}
}

func TestInstalledSkillStoreUpdateReplacesRecord(t *testing.T) {
	store := newSkillTestStore(t)
	old := sampleSkillRecord("anvil-overview", "2.1.0")
	old.Resolution = Resolution{Kind: SkillResolutionKindCore, Source: "embedded"}

	if _, _, err := store.Record(old.ID, old); err != nil {
		t.Fatalf("Record: %v", err)
	}

	next := sampleSkillRecord(old.ID, "2.2.0")
	next.InstalledAt = old.InstalledAt
	next.UpdatedAt = old.InstalledAt.Add(30 * 24 * time.Hour)
	next.Resolution = Resolution{Kind: SkillResolutionKindDistribution, Source: "https://example.com/anvil-overview/v2.2.0.tar.gz"}
	next.Targets = []InstalledSkillTarget{
		{Agent: "opencode", Scope: SkillScopeRepo, Path: "/tmp/project/.agents/skills/anvil-overview"},
		{Agent: "claude-code", Scope: SkillScopeGlobal, Path: "/home/user/.claude/skills/anvil-overview"},
	}

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
		t.Errorf("record after Update = %+v, want %+v", loaded, next)
	}
	if !loaded.InstalledAt.Equal(old.InstalledAt) {
		t.Errorf("installedAt changed: %s vs %s", loaded.InstalledAt, old.InstalledAt)
	}
	if !loaded.UpdatedAt.Equal(next.UpdatedAt) {
		t.Errorf("updatedAt = %s, want %s", loaded.UpdatedAt, next.UpdatedAt)
	}

	var fileRec InstalledSkillRecord
	if err := json.Unmarshal(readSkillRecordFile(t, skillRecordFilePath(store, old.ID)), &fileRec); err != nil {
		t.Fatalf("record file not decodable after Update: %v", err)
	}
	if !reflect.DeepEqual(fileRec, next) {
		t.Errorf("record file after Update = %+v, want %+v", fileRec, next)
	}
}

func TestInstalledSkillStoreUpdateRequiresExistingRecord(t *testing.T) {
	store := newSkillTestStore(t)
	_, err := store.Update("anvil-overview", sampleSkillRecord("anvil-overview", "2.1.0"))
	if !errors.Is(err, ErrSkillRecordNotFound) {
		t.Fatalf("err = %v, want ErrSkillRecordNotFound", err)
	}
	if !strings.Contains(err.Error(), "install the skill first") {
		t.Errorf("error %q lacks install-first hint", err)
	}
}

func TestInstalledSkillStoreUpdateValidatesRecord(t *testing.T) {
	store := newSkillTestStore(t)
	rec := sampleSkillRecord("anvil-overview", "2.1.0")
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	bad := rec
	bad.Version = ""
	if _, err := store.Update(rec.ID, bad); !errors.Is(err, ErrSkillRecordInvalid) {
		t.Fatalf("err = %v, want ErrSkillRecordInvalid", err)
	}
}

func TestInstalledSkillStoreUpdatePreservesInstalledAt(t *testing.T) {
	store := newSkillTestStore(t)
	old := sampleSkillRecord("anvil-overview", "2.1.0")
	if _, _, err := store.Record(old.ID, old); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// A second update whose record carries a fresh InstalledAt (e.g. the
	// caller rebuilt the record from the new release) must not reset the
	// original install time.
	next := sampleSkillRecord(old.ID, "2.2.0")
	next.InstalledAt = old.InstalledAt.Add(24 * time.Hour) // would be wrong if kept
	next.UpdatedAt = next.InstalledAt.Add(time.Hour)
	next.Resolution = Resolution{Kind: SkillResolutionKindDistribution, Source: "https://example.com/v2.2.0.tar.gz"}
	next.Targets = []InstalledSkillTarget{
		{Agent: "opencode", Scope: SkillScopeRepo, Path: "/tmp/project/.agents/skills/anvil-overview"},
		{Agent: "claude-code", Scope: SkillScopeGlobal, Path: "/home/user/.claude/skills/anvil-overview"},
	}

	got, err := store.Update(old.ID, next)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !got.InstalledAt.Equal(old.InstalledAt) {
		t.Errorf("Update reset installedAt to %s, want original %s", got.InstalledAt, old.InstalledAt)
	}
	if got.Version != next.Version {
		t.Errorf("version = %q, want %q", got.Version, next.Version)
	}
	if len(got.Targets) != 2 {
		t.Errorf("targets = %d, want 2", len(got.Targets))
	}

	loaded, err := store.Get(old.ID)
	if err != nil {
		t.Fatalf("Get after Update: %v", err)
	}
	if !loaded.InstalledAt.Equal(old.InstalledAt) {
		t.Errorf("persisted installedAt = %s, want original %s", loaded.InstalledAt, old.InstalledAt)
	}
}

func TestInstalledSkillStoreDelete(t *testing.T) {
	store := newSkillTestStore(t)
	rec := sampleSkillRecord("anvil-overview", "2.1.0")

	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := store.Delete(rec.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(skillRecordFilePath(store, rec.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("record file still exists after Delete (stat err=%v)", err)
	}
	if _, err := store.Get(rec.ID); !errors.Is(err, ErrSkillRecordNotFound) {
		t.Fatalf("Get after Delete: err = %v, want ErrSkillRecordNotFound", err)
	}
	if err := store.Delete(rec.ID); !errors.Is(err, ErrSkillRecordNotFound) {
		t.Fatalf("second Delete: err = %v, want ErrSkillRecordNotFound", err)
	}
}

func TestInstalledSkillStoreList(t *testing.T) {
	store := newSkillTestStore(t)
	zeta := sampleSkillRecord("anvil-zeta", "1.0.0")
	overview := sampleSkillRecord("anvil-overview", "2.1.0")
	laravel := sampleSkillRecord("skill-laravel", "3.0.0")

	for _, rec := range []InstalledSkillRecord{zeta, overview, laravel} {
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
	wantIDs := []string{"anvil-overview", "anvil-zeta", "skill-laravel"}
	if len(summaries) != len(wantIDs) {
		t.Fatalf("List returned %d summaries, want %d", len(summaries), len(wantIDs))
	}
	for i, want := range wantIDs {
		if summaries[i].ID != want {
			t.Errorf("summary[%d].ID = %q, want %q", i, summaries[i].ID, want)
		}
	}
	if summaries[0].Version != "2.1.0" || summaries[1].Version != "1.0.0" || summaries[2].Version != "3.0.0" {
		t.Errorf("summary versions = %v, want 2.1.0,1.0.0,3.0.0", []string{summaries[0].Version, summaries[1].Version, summaries[2].Version})
	}
	if summaries[0].Source != "core" || summaries[0].InstalledAt.IsZero() {
		t.Errorf("summary core fields missing: %+v", summaries[0])
	}

	empty := newSkillTestStore(t)
	if summaries, corrupt, err := empty.List(); err != nil || len(summaries) != 0 || len(corrupt) != 0 {
		t.Errorf("empty store List = (%v, %v, %v), want (nil,nil,nil)", summaries, corrupt, err)
	}
}

func TestInstalledSkillStoreListSkipsCorruptRecords(t *testing.T) {
	store := newSkillTestStore(t)
	rec := sampleSkillRecord("anvil-overview", "2.1.0")
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	brokenPath := skillRecordFilePath(store, "anvil-broken")
	if err := os.WriteFile(brokenPath, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	summaries, corrupt, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != rec.ID {
		t.Fatalf("summaries = %+v, want only %s", summaries, rec.ID)
	}
	if len(corrupt) != 1 {
		t.Fatalf("corrupt = %+v, want one", corrupt)
	}
	if corrupt[0].Path != brokenPath {
		t.Errorf("corrupt[0].Path = %q, want %q", corrupt[0].Path, brokenPath)
	}
	if !strings.Contains(corrupt[0].Error, "not decodable") {
		t.Errorf("error %q lacks decode reason", corrupt[0].Error)
	}
	if !strings.Contains(corrupt[0].Error, "re-install") {
		t.Errorf("error %q lacks recovery hint", corrupt[0].Error)
	}
}

func TestInstalledSkillStoreCorruptRecordReportsAndRecovers(t *testing.T) {
	store := newSkillTestStore(t)
	path := skillRecordFilePath(store, "anvil-overview")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	_, err := store.Get("anvil-overview")
	if !errors.Is(err, ErrSkillRecordCorrupt) {
		t.Fatalf("Get corrupt: err = %v, want ErrSkillRecordCorrupt", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not name file", err)
	}

	rec := sampleSkillRecord("anvil-overview", "2.1.0")
	got, created, err := store.Record(rec.ID, rec)
	if err != nil {
		t.Fatalf("Record over corrupt: %v", err)
	}
	if !created {
		t.Error("created = false, want true")
	}
	if !reflect.DeepEqual(got, rec) {
		t.Errorf("recovered record = %+v, want %+v", got, rec)
	}
	if loaded, err := store.Get(rec.ID); err != nil || !reflect.DeepEqual(loaded, rec) {
		t.Errorf("Get after recovery = (%+v, %v), want (%+v, nil)", loaded, err, rec)
	}
}

func TestInstalledSkillStoreCorruptRecordRecoversByUpdate(t *testing.T) {
	store := newSkillTestStore(t)
	if err := os.WriteFile(skillRecordFilePath(store, "anvil-overview"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	rec := sampleSkillRecord("anvil-overview", "2.1.0")
	got, err := store.Update(rec.ID, rec)
	if err != nil {
		t.Fatalf("Update over corrupt: %v", err)
	}
	if !reflect.DeepEqual(got, rec) {
		t.Errorf("recovered record = %+v, want %+v", got, rec)
	}
}

func TestInstalledSkillStoreSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	first := NewInstalledSkillStore(dir)
	rec := sampleSkillRecord("anvil-overview", "2.1.0")
	if _, _, err := first.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	second := NewInstalledSkillStore(dir)
	loaded, err := second.Get(rec.ID)
	if err != nil {
		t.Fatalf("Get via restarted store: %v", err)
	}
	if !reflect.DeepEqual(loaded, rec) {
		t.Errorf("record after restart = %+v, want %+v", loaded, rec)
	}
	summaries, corrupt, err := second.List()
	if err != nil || len(summaries) != 1 || len(corrupt) != 0 {
		t.Errorf("List after restart = (%v, %v, %v)", summaries, corrupt, err)
	}
}

func TestInstalledSkillStoreAtomicWriteLeavesNoTempFiles(t *testing.T) {
	store := newSkillTestStore(t)
	rec := sampleSkillRecord("anvil-overview", "2.1.0")
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if _, err := store.Update(rec.ID, sampleSkillRecord(rec.ID, "2.2.0")); err != nil {
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
		t.Errorf("store dir contains %v, want [%s.json]", names, rec.ID)
	}
	if entries[0].Name() != rec.ID+".json" {
		t.Errorf("store file = %q, want %q", entries[0].Name(), rec.ID+".json")
	}
}

func TestInstalledSkillStoreFileWithMismatchedIDIsCorrupt(t *testing.T) {
	store := newSkillTestStore(t)
	rec := sampleSkillRecord("anvil-other", "1.0.0")
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := skillRecordFilePath(store, "anvil-overview")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err = store.Get("anvil-overview")
	if !errors.Is(err, ErrSkillRecordCorrupt) {
		t.Fatalf("err = %v, want ErrSkillRecordCorrupt", err)
	}
	if !strings.Contains(err.Error(), "does not match the record key") {
		t.Errorf("error %q lacks id mismatch reason", err)
	}
}

func TestInstalledSkillStoreFileWithTrailingContentIsCorrupt(t *testing.T) {
	store := newSkillTestStore(t)
	rec := sampleSkillRecord("anvil-overview", "2.1.0")
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := skillRecordFilePath(store, rec.ID)
	if err := os.WriteFile(path, append(raw, []byte("{}")...), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err = store.Get(rec.ID)
	if !errors.Is(err, ErrSkillRecordCorrupt) {
		t.Fatalf("err = %v, want ErrSkillRecordCorrupt", err)
	}
	if !strings.Contains(err.Error(), "unexpected content after") {
		t.Errorf("error %q lacks trailing content reason", err)
	}
}

func TestInstalledSkillStoreFileWithUnknownFieldIsCorrupt(t *testing.T) {
	store := newSkillTestStore(t)
	rec := sampleSkillRecord("anvil-overview", "2.1.0")
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	raw = bytes.TrimSuffix(raw, []byte("}"))
	raw = append(raw, []byte(",\"sneaky\":true}")...)

	path := skillRecordFilePath(store, rec.ID)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err = store.Get(rec.ID)
	if !errors.Is(err, ErrSkillRecordCorrupt) {
		t.Fatalf("err = %v, want ErrSkillRecordCorrupt", err)
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("error %q lacks unknown field reason", err)
	}
}

func TestInstalledSkillStoreOversizedRecordIsCorrupt(t *testing.T) {
	store := newSkillTestStore(t)
	rec := sampleSkillRecord("anvil-overview", "2.1.0")
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	oversize := append(raw, bytes.Repeat([]byte(" "), MaxRecordSize)...)

	path := skillRecordFilePath(store, rec.ID)
	if err := os.WriteFile(path, oversize, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err = store.Get(rec.ID)
	if !errors.Is(err, ErrSkillRecordCorrupt) {
		t.Fatalf("err = %v, want ErrSkillRecordCorrupt", err)
	}
	if !strings.Contains(err.Error(), "size cap") {
		t.Errorf("error %q lacks size cap reason", err)
	}
}

func TestInstalledSkillStoreUnreadableStoreDir(t *testing.T) {
	dirFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(dirFile, []byte("file"), 0o644); err != nil {
		t.Fatalf("write dir-file: %v", err)
	}
	store := NewInstalledSkillStore(dirFile)
	rec := sampleSkillRecord("anvil-overview", "2.1.0")

	if _, err := store.Get(rec.ID); !errors.Is(err, ErrSkillStoreUnreadable) {
		t.Errorf("Get: err = %v, want ErrSkillStoreUnreadable", err)
	}
	if _, _, err := store.List(); !errors.Is(err, ErrSkillStoreUnreadable) {
		t.Errorf("List: err = %v, want ErrSkillStoreUnreadable", err)
	}
	if _, _, err := store.Record(rec.ID, rec); err == nil {
		t.Error("Record over unreadable dir succeeded, want error")
	}
	if _, err := store.Update(rec.ID, rec); err == nil {
		t.Error("Update over unreadable dir succeeded, want error")
	}
	if err := store.Delete(rec.ID); err == nil {
		t.Error("Delete over unreadable dir succeeded, want error")
	}
}

func TestInstalledSkillStoreAtomicReplaceFailureLeavesNoCorruption(t *testing.T) {
	store := newSkillTestStore(t)
	rec := sampleSkillRecord("anvil-overview", "2.1.0")
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}
	path := skillRecordFilePath(store, rec.ID)

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove record: %v", err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("squat on record path: %v", err)
	}

	next := sampleSkillRecord(rec.ID, "2.2.0")
	if _, err := store.Update(rec.ID, next); err == nil {
		t.Fatal("Update with unreplaceable target succeeded, want error")
	}
	if _, _, err := store.Record(rec.ID, next); err == nil {
		t.Fatal("Record with unreplaceable target succeeded, want error")
	}

	entries, err := os.ReadDir(store.Dir())
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".tmp-") {
			t.Errorf("failed write left temp file %q", entry.Name())
		}
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove obstruction: %v", err)
	}
	if _, err := store.Get(rec.ID); !errors.Is(err, ErrSkillRecordNotFound) {
		t.Fatalf("Get after obstruction removal: err = %v", err)
	}
	got, created, err := store.Record(rec.ID, next)
	if err != nil || !created {
		t.Fatalf("Record after recovery: created=%v err=%v", created, err)
	}
	if loaded, err := store.Get(rec.ID); err != nil || !reflect.DeepEqual(loaded, got) {
		t.Errorf("record after recovery = (%+v, %v), want (%+v, nil)", loaded, err, got)
	}
}

func TestInstalledSkillStoreFormatVersionWrittenAndEnforced(t *testing.T) {
	store := newSkillTestStore(t)
	rec := sampleSkillRecord("anvil-overview", "2.1.0")
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	var fileRec InstalledSkillRecord
	if err := json.Unmarshal(readSkillRecordFile(t, skillRecordFilePath(store, rec.ID)), &fileRec); err != nil {
		t.Fatalf("record file not decodable: %v", err)
	}
	if fileRec.FormatVersion != InstalledSkillRecordFormatVersion {
		t.Errorf("formatVersion = %d, want %d", fileRec.FormatVersion, InstalledSkillRecordFormatVersion)
	}

	for _, version := range []int{0, 2} {
		future := rec
		future.FormatVersion = version
		raw, err := json.Marshal(future)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := os.WriteFile(skillRecordFilePath(store, rec.ID), raw, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		_, err = store.Get(rec.ID)
		if !errors.Is(err, ErrSkillRecordCorrupt) {
			t.Fatalf("formatVersion %d: Get err = %v, want ErrSkillRecordCorrupt", version, err)
		}
		if !strings.Contains(err.Error(), "formatVersion must be 1") {
			t.Errorf("formatVersion %d: error %q lacks version reason", version, err)
		}

		summaries, corrupt, err := store.List()
		if err != nil || len(summaries) != 0 {
			t.Fatalf("formatVersion %d: List = (%v, err %v), want no summaries", version, summaries, err)
		}
		if len(corrupt) != 1 || !strings.Contains(corrupt[0].Error, "formatVersion must be 1") {
			t.Errorf("formatVersion %d: corrupt = %+v", version, corrupt)
		}
	}
}

func TestInstalledSkillStoreWriteSideOversizeRejected(t *testing.T) {
	store := newSkillTestStore(t)
	rec := sampleSkillRecord("anvil-overview", "2.1.0")
	rec.Resolution.Source = strings.Repeat("x", MaxRecordSize)

	_, _, err := store.Record(rec.ID, rec)
	if !errors.Is(err, ErrSkillRecordInvalid) {
		t.Fatalf("Record oversize: err = %v, want ErrSkillRecordInvalid", err)
	}
	if !strings.Contains(err.Error(), "exceeding the 1048576-byte cap") {
		t.Errorf("error %q lacks cap", err)
	}

	seeded := sampleSkillRecord(rec.ID, "2.0.0")
	if _, created, err := store.Record(seeded.ID, seeded); err != nil || !created {
		t.Fatalf("seed Record: created=%v err=%v", created, err)
	}
	oversized := seeded
	oversized.Version = "3.0.0"
	oversized.UpdatedAt = seeded.InstalledAt.Add(time.Hour)
	oversized.Resolution.Source = strings.Repeat("x", MaxRecordSize)
	if _, err := store.Update(seeded.ID, oversized); !errors.Is(err, ErrSkillRecordInvalid) {
		t.Fatalf("Update oversize: err = %v, want ErrSkillRecordInvalid", err)
	}

	loaded, err := store.Get(seeded.ID)
	if err != nil {
		t.Fatalf("Get after rejected update: %v", err)
	}
	if !reflect.DeepEqual(loaded, seeded) {
		t.Errorf("record after rejected update = %+v, want %+v", loaded, seeded)
	}
	entries, err := os.ReadDir(store.Dir())
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != seeded.ID+".json" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("store dir = %v, want [%s.json]", names, seeded.ID)
	}
}

func TestInstalledSkillStoreSymlinkedRecordRejected(t *testing.T) {
	store := newSkillTestStore(t)
	rec := sampleSkillRecord("anvil-overview", "2.1.0")

	target := filepath.Join(t.TempDir(), "record-target.json")
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(target, raw, 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Symlink(target, skillRecordFilePath(store, rec.ID)); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	_, err = store.Get(rec.ID)
	if !errors.Is(err, ErrSkillRecordCorrupt) {
		t.Fatalf("Get symlinked: err = %v, want ErrSkillRecordCorrupt", err)
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("error %q lacks symlink reason", err)
	}

	summaries, corrupt, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 0 {
		t.Errorf("summaries = %+v, want none", summaries)
	}
	if len(corrupt) != 1 || !strings.Contains(corrupt[0].Error, "symlink") {
		t.Errorf("corrupt = %+v, want one symlink report", corrupt)
	}
}

func TestInstalledSkillStoreDirectoryAtRecordPathIsCorrupt(t *testing.T) {
	store := newSkillTestStore(t)
	path := skillRecordFilePath(store, "anvil-overview")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir at record path: %v", err)
	}

	_, err := store.Get("anvil-overview")
	if !errors.Is(err, ErrSkillRecordCorrupt) {
		t.Fatalf("Get: err = %v, want ErrSkillRecordCorrupt", err)
	}
	if !strings.Contains(err.Error(), "is a directory") {
		t.Errorf("error %q lacks directory reason", err)
	}

	summaries, corrupt, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 0 {
		t.Errorf("summaries = %+v, want none", summaries)
	}
	if len(corrupt) != 1 || !strings.Contains(corrupt[0].Error, "is a directory") {
		t.Errorf("corrupt = %+v, want one directory report", corrupt)
	}
}

func TestDefaultInstalledSkillsDir(t *testing.T) {
	dir, err := DefaultInstalledSkillsDir()
	if err != nil {
		t.Fatalf("DefaultInstalledSkillsDir: %v", err)
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("default dir %q is not absolute", dir)
	}
	wantSuffix := filepath.Join("anvil", DefaultInstalledSkillsDirName)
	if !strings.HasSuffix(filepath.Clean(dir), filepath.Clean(wantSuffix)) {
		t.Errorf("default dir %q does not end with %q", dir, wantSuffix)
	}
}

// Stale-status tests.

func TestInstalledSkillStoreStatusCoreStale(t *testing.T) {
	store := newSkillTestStore(t)
	rec := sampleSkillRecord("anvil-overview", "2.1.0")
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	status, err := store.Status(rec.ID, "2.2.0", fakeStandardLookup{})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Stale {
		t.Error("Stale = false, want true")
	}
	if len(status.Hints) != 1 {
		t.Fatalf("hints = %v, want one", status.Hints)
	}
	if !strings.Contains(status.Hints[0], "2.1.0") || !strings.Contains(status.Hints[0], "2.2.0") {
		t.Errorf("hint %q does not name versions", status.Hints[0])
	}
}

func TestInstalledSkillStoreStatusCoreFresh(t *testing.T) {
	store := newSkillTestStore(t)
	rec := sampleSkillRecord("anvil-overview", "2.1.0")
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	status, err := store.Status(rec.ID, "2.1.0", fakeStandardLookup{})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Stale {
		t.Error("Stale = true, want false")
	}
	if len(status.Hints) != 0 {
		t.Errorf("hints = %v, want none", status.Hints)
	}
}

func TestInstalledSkillStoreStatusCoreFlipsWithoutMutatingRecord(t *testing.T) {
	store := newSkillTestStore(t)
	rec := sampleSkillRecord("anvil-overview", "2.1.0")
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}
	path := skillRecordFilePath(store, rec.ID)
	recordBytesBefore := readSkillRecordFile(t, path)

	fresh, err := store.Status(rec.ID, "2.1.0", fakeStandardLookup{})
	if err != nil {
		t.Fatalf("Status fresh: %v", err)
	}
	if fresh.Stale {
		t.Error("fresh Stale = true, want false")
	}

	stale, err := store.Status(rec.ID, "2.2.0", fakeStandardLookup{})
	if err != nil {
		t.Fatalf("Status stale: %v", err)
	}
	if !stale.Stale {
		t.Error("stale Stale = false, want true")
	}

	recordBytesAfter := readSkillRecordFile(t, path)
	if !bytes.Equal(recordBytesBefore, recordBytesAfter) {
		t.Error("record file changed after Status queries, want no mutation")
	}
}

func TestInstalledSkillStoreStatusSourceMissing(t *testing.T) {
	store := newSkillTestStore(t)
	rec := sampleSkillRecord("skill-laravel", "1.0.0")
	rec.Source = "anvil-standard-laravel"
	rec.Resolution = Resolution{Kind: SkillResolutionKindDistribution, Source: "https://example.com/skill.tar.gz"}
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	status, err := store.Status(rec.ID, "2.1.0", fakeStandardLookup{})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Stale {
		t.Error("Stale = false, want true")
	}
	if len(status.Hints) != 1 || !strings.Contains(status.Hints[0], "not installed") {
		t.Errorf("hint %q does not report missing source", status.Hints[0])
	}
}

// corruptStandardLookup always reports a corrupt installed-standard record.
type corruptStandardLookup struct{}

func (corruptStandardLookup) Get(string) (InstalledStandardRecord, error) {
	return InstalledStandardRecord{}, ErrRecordCorrupt
}

func TestInstalledSkillStoreStatusSourceCorrupt(t *testing.T) {
	store := newSkillTestStore(t)
	rec := sampleSkillRecord("skill-laravel", "1.0.0")
	rec.Source = "anvil-standard-laravel"
	rec.Resolution = Resolution{Kind: SkillResolutionKindDistribution, Source: "https://example.com/skill.tar.gz"}
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	status, err := store.Status(rec.ID, "2.1.0", corruptStandardLookup{})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Stale {
		t.Error("Stale = false, want true")
	}
	if len(status.Hints) != 1 || !strings.Contains(status.Hints[0], "unreadable") {
		t.Errorf("hint %q does not report unreadable source record", status.Hints[0])
	}
	if strings.Contains(status.Hints[0], "not installed") {
		t.Errorf("hint %q incorrectly reports 'not installed' for a corrupt source record", status.Hints[0])
	}
}

func TestInstalledSkillStoreStatusSourceDeprecated(t *testing.T) {
	store := newSkillTestStore(t)
	rec := sampleSkillRecord("skill-laravel", "1.0.0")
	rec.Source = "anvil-standard-laravel"
	rec.Resolution = Resolution{Kind: SkillResolutionKindDistribution, Source: "https://example.com/skill.tar.gz"}
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	lookup := fakeStandardLookup{
		"anvil-standard-laravel": sampleStandardRecord("anvil-standard-laravel", "5.0.0", LifecycleStateDeprecated),
	}
	status, err := store.Status(rec.ID, "2.1.0", lookup)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Stale {
		t.Error("Stale = false, want true")
	}
	if len(status.Hints) != 1 || !strings.Contains(status.Hints[0], "deprecated") {
		t.Errorf("hint %q does not report deprecated source", status.Hints[0])
	}
}

func TestInstalledSkillStoreStatusSourceDeprecatedNoRemovalDate(t *testing.T) {
	store := newSkillTestStore(t)
	rec := sampleSkillRecord("skill-laravel", "1.0.0")
	rec.Source = "anvil-standard-laravel"
	rec.Resolution = Resolution{Kind: SkillResolutionKindDistribution, Source: "https://example.com/skill.tar.gz"}
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	std := sampleStandardRecord("anvil-standard-laravel", "5.0.0", LifecycleStateDeprecated)
	std.Lifecycle.RemovalDate = ""
	lookup := fakeStandardLookup{"anvil-standard-laravel": std}

	status, err := store.Status(rec.ID, "2.1.0", lookup)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Stale {
		t.Error("Stale = false, want true")
	}
	if len(status.Hints) != 1 || !strings.Contains(status.Hints[0], "no removal date announced") {
		t.Errorf("hint %q does not use 'no removal date announced' fallback", status.Hints[0])
	}
}

func TestInstalledSkillStoreStatusSourceRetired(t *testing.T) {
	store := newSkillTestStore(t)
	rec := sampleSkillRecord("skill-laravel", "1.0.0")
	rec.Source = "anvil-standard-laravel"
	rec.Resolution = Resolution{Kind: SkillResolutionKindDistribution, Source: "https://example.com/skill.tar.gz"}
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	lookup := fakeStandardLookup{
		"anvil-standard-laravel": sampleStandardRecord("anvil-standard-laravel", "5.0.0", LifecycleStateRetired),
	}
	status, err := store.Status(rec.ID, "2.1.0", lookup)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Stale {
		t.Error("Stale = false, want true")
	}
	if len(status.Hints) != 1 || !strings.Contains(status.Hints[0], "retired") {
		t.Errorf("hint %q does not report retired source", status.Hints[0])
	}
}

func TestInstalledSkillStoreStatusSourcePublished(t *testing.T) {
	store := newSkillTestStore(t)
	rec := sampleSkillRecord("skill-laravel", "1.0.0")
	rec.Source = "anvil-standard-laravel"
	rec.Resolution = Resolution{Kind: SkillResolutionKindDistribution, Source: "https://example.com/skill.tar.gz"}
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	lookup := fakeStandardLookup{
		"anvil-standard-laravel": sampleStandardRecord("anvil-standard-laravel", "5.0.0", LifecycleStatePublished),
	}
	status, err := store.Status(rec.ID, "2.1.0", lookup)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Stale {
		t.Error("Stale = true, want false")
	}
	if len(status.Hints) != 0 {
		t.Errorf("hints = %v, want none", status.Hints)
	}
}

// errLookup always returns a non-standard error.
type errLookup struct{}

func (errLookup) Get(string) (InstalledStandardRecord, error) {
	return InstalledStandardRecord{}, errors.New("lookup exploded")
}

func TestInstalledSkillStoreStatusLookupErrorPropagates(t *testing.T) {
	store := newSkillTestStore(t)
	rec := sampleSkillRecord("skill-laravel", "1.0.0")
	rec.Source = "anvil-standard-laravel"
	rec.Resolution = Resolution{Kind: SkillResolutionKindDistribution, Source: "https://example.com/skill.tar.gz"}
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	_, err := store.Status(rec.ID, "2.1.0", errLookup{})
	if err == nil {
		t.Fatal("Status with failing lookup returned nil, want error")
	}
	if !strings.Contains(err.Error(), "lookup exploded") {
		t.Errorf("error %q does not propagate lookup error", err)
	}
}

func TestInstalledSkillStoreListStatuses(t *testing.T) {
	store := newSkillTestStore(t)
	core := sampleSkillRecord("anvil-overview", "2.1.0")
	stdStale := sampleSkillRecord("skill-laravel", "1.0.0")
	stdStale.Source = "anvil-standard-laravel"
	stdStale.Resolution = Resolution{Kind: SkillResolutionKindDistribution, Source: "https://example.com/skill.tar.gz"}
	stdFresh := sampleSkillRecord("skill-flutter", "1.0.0")
	stdFresh.Source = "anvil-standard-flutter"
	stdFresh.Resolution = Resolution{Kind: SkillResolutionKindDistribution, Source: "https://example.com/skill2.tar.gz"}

	for _, rec := range []InstalledSkillRecord{core, stdStale, stdFresh} {
		if _, _, err := store.Record(rec.ID, rec); err != nil {
			t.Fatalf("Record %s: %v", rec.ID, err)
		}
	}

	lookup := fakeStandardLookup{
		"anvil-standard-laravel": sampleStandardRecord("anvil-standard-laravel", "5.0.0", LifecycleStateDeprecated),
		"anvil-standard-flutter": sampleStandardRecord("anvil-standard-flutter", "5.0.0", LifecycleStatePublished),
	}

	statuses, corrupt, err := store.ListStatuses("2.2.0", lookup)
	if err != nil {
		t.Fatalf("ListStatuses: %v", err)
	}
	if len(corrupt) != 0 {
		t.Errorf("corrupt = %+v, want none", corrupt)
	}
	if len(statuses) != 3 {
		t.Fatalf("statuses = %d, want 3", len(statuses))
	}

	wantStale := map[string]bool{
		"anvil-overview": true,
		"skill-laravel":  true,
		"skill-flutter":  false,
	}
	for _, s := range statuses {
		if wantStale[s.Record.ID] != s.Stale {
			t.Errorf("%s Stale = %v, want %v", s.Record.ID, s.Stale, wantStale[s.Record.ID])
		}
	}
}
