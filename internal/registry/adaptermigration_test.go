package registry

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sampleOutcome returns a structurally valid migration outcome for
// adapterName/standardID. Tests mutate the fields they exercise.
func sampleOutcome(adapterName, standardID, status string) MigrationOutcome {
	return MigrationOutcome{
		FormatVersion:     MigrationRecordFormatVersion,
		AdapterName:       adapterName,
		AdapterExecutable: "anvil-adapter-" + adapterName,
		StandardID:        standardID,
		Framework:         strings.ToUpper(adapterName[:1]) + adapterName[1:],
		Status:            status,
		ResolvedAt:        time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC),
		StandardVersion:   map[bool]string{true: "1.2.3"}[status == MigrationStatusMigrated],
	}
}

// newOutcomeStore returns a migration outcome store rooted at a fresh
// temp directory.
func newOutcomeStore(t *testing.T) *AdapterMigrationStore {
	t.Helper()
	return NewAdapterMigrationStore(t.TempDir())
}

// authoritativeMapping loads the maintained mapping artifact for the
// recognition tests (the same artifact the runtime consumes).
func authoritativeMapping(t *testing.T) *AdapterMapping {
	t.Helper()
	mapping, err := LoadAdapterMapping(authoritativeMappingPath(t))
	if err != nil {
		t.Fatalf("load authoritative mapping: %v", err)
	}
	return mapping
}

// testNow is the injected clock of the recognition tests.
func testNow() time.Time { return time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC) }

// ── Store: persistence and semantics ─────────────────────────────────

// TestAdapterMigrationStore_RecordPersists asserts a recorded migration
// outcome is persisted as <dir>/<adapter>.json with the full outcome
// content: the adapter identity (lookup keys), the standard target, the
// status, and the recorded-at timestamp (TS-017-01-02: outcomes are
// recorded).
func TestAdapterMigrationStore_RecordPersists(t *testing.T) {
	store := newOutcomeStore(t)
	outcome := sampleOutcome("laravel", "anvil-standard-laravel", MigrationStatusRecognized)

	recorded, created, err := store.Record("laravel", outcome)
	if err != nil {
		t.Fatalf("Record returned error: %v", err)
	}
	if !created {
		t.Error("Record should report created on a fresh record")
	}
	if !recorded.ResolvedAt.Equal(outcome.ResolvedAt) {
		t.Errorf("recorded resolvedAt = %v, want %v", recorded.ResolvedAt, outcome.ResolvedAt)
	}

	got, err := store.Get("laravel")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got.AdapterName != "laravel" || got.StandardID != "anvil-standard-laravel" || got.Status != MigrationStatusRecognized {
		t.Errorf("Get = %+v, want the recorded laravel outcome", got)
	}
	if got.AdapterExecutable != "anvil-adapter-laravel" {
		t.Errorf("adapter executable = %q, want anvil-adapter-laravel (the mapping lookup key)", got.AdapterExecutable)
	}
	if got.ContractVersion != "" {
		t.Errorf("contract_version = %q, want empty — the sample outcome is the recognized-without-standard case: no standard is installed at migration time, so nothing is declared and no contract version is recorded (TS-017-01-03)", got.ContractVersion)
	}
}

// TestAdapterMigrationStore_RecordIdempotent verifies that re-recording
// the same outcome state is an idempotent success: the existing record
// is returned unchanged, nothing is rewritten (the outcome is
// re-confirmed on every adoption, not re-churned).
func TestAdapterMigrationStore_RecordIdempotent(t *testing.T) {
	store := newOutcomeStore(t)
	outcome := sampleOutcome("flutter", "anvil-standard-flutter", MigrationStatusRecognized)

	if _, created, err := store.Record("flutter", outcome); err != nil || !created {
		t.Fatalf("first Record = (created %v, err %v), want created", created, err)
	}

	reRecorded, created, err := store.Record("flutter", outcome)
	if err != nil {
		t.Fatalf("re-Record returned error: %v", err)
	}
	if created {
		t.Error("re-Record of the same state should not report created")
	}
	if !reRecorded.ResolvedAt.Equal(outcome.ResolvedAt) {
		t.Errorf("re-recorded resolvedAt = %v, want the original %v — identical state must not rewrite the record", reRecorded.ResolvedAt, outcome.ResolvedAt)
	}
}

// TestAdapterMigrationStore_RecordStateChange verifies that a state
// change (recognized → migrated once the standard is installed)
// replaces the record atomically and reports created.
func TestAdapterMigrationStore_RecordStateChange(t *testing.T) {
	store := newOutcomeStore(t)
	recognized := sampleOutcome("laravel", "anvil-standard-laravel", MigrationStatusRecognized)
	migrated := sampleOutcome("laravel", "anvil-standard-laravel", MigrationStatusMigrated)

	if _, _, err := store.Record("laravel", recognized); err != nil {
		t.Fatalf("record recognized: %v", err)
	}
	got, created, err := store.Record("laravel", migrated)
	if err != nil {
		t.Fatalf("record migrated: %v", err)
	}
	if !created {
		t.Error("state change should report created")
	}
	if got.Status != MigrationStatusMigrated || got.StandardVersion != "1.2.3" {
		t.Errorf("state change not applied: %+v", got)
	}
}

// TestAdapterMigrationStore_RecordMappingRemap verifies that a
// mapping-table change is a recorded state change, never a silently
// stale record: the same adapter re-recognized against a row with a
// DIFFERENT standard_id or adapter_executable (ANVIL_V2_ADAPTER_
// STANDARD_MAPPING §7 — the maintained table is the authoritative
// mapping) replaces the record and reports created, so downstream
// consumers (e.g. TS-017-01-03 contract-version validation) never read
// a stale mapping identity.
func TestAdapterMigrationStore_RecordMappingRemap(t *testing.T) {
	store := newOutcomeStore(t)
	original := sampleOutcome("laravel", "anvil-standard-laravel", MigrationStatusRecognized)
	if _, _, err := store.Record("laravel", original); err != nil {
		t.Fatalf("record original: %v", err)
	}

	// The mapping row remaps the adapter to a different standard
	// (same status and version state — only the identity changed).
	remapped := original
	remapped.StandardID = "anvil-standard-laravel-next"
	if _, created, err := store.Record("laravel", remapped); err != nil {
		t.Fatalf("record remapped standard: %v", err)
	} else if !created {
		t.Error("a standard_id remap must report created — the recorded identity must never stay stale")
	}

	// The mapping row remaps the executable identity (same standard).
	reexec := original
	reexec.AdapterExecutable = "anvil-adapter-laravel-v2"
	if _, created, err := store.Record("laravel", reexec); err != nil {
		t.Fatalf("record re-executable: %v", err)
	} else if !created {
		t.Error("an adapter_executable remap must report created")
	}

	got, err := store.Get("laravel")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.StandardID != "anvil-standard-laravel" || got.AdapterExecutable != "anvil-adapter-laravel-v2" {
		t.Errorf("recorded identity = %+v, want the latest remap state", got)
	}
}

// TestAdapterMigrationStore_RecordValidation verifies the structural
// validation of outcomes: unsafe keys, wrong format version, unknown
// status, missing identity, and status/state incoherence are all
// rejected with wrapped ErrMigrationRecordInvalid before any write.
func TestAdapterMigrationStore_RecordValidation(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		outcome MigrationOutcome
		want    string
	}{
		{"unsafe key", "bad/name", sampleOutcome("laravel", "anvil-standard-laravel", MigrationStatusRecognized), "not a safe record key"},
		{"wrong format version", "laravel", func() MigrationOutcome {
			o := sampleOutcome("laravel", "anvil-standard-laravel", MigrationStatusRecognized)
			o.FormatVersion = 99
			return o
		}(), "format version 99"},
		{"name mismatch", "laravel", sampleOutcome("flutter", "anvil-standard-flutter", MigrationStatusRecognized), "does not match the record key"},
		{"empty executable", "laravel", func() MigrationOutcome {
			o := sampleOutcome("laravel", "anvil-standard-laravel", MigrationStatusRecognized)
			o.AdapterExecutable = ""
			return o
		}(), "adapter_executable must not be empty"},
		{"empty standard", "laravel", func() MigrationOutcome {
			o := sampleOutcome("laravel", "anvil-standard-laravel", MigrationStatusRecognized)
			o.StandardID = ""
			return o
		}(), "standard_id must not be empty"},
		{"unknown status", "laravel", func() MigrationOutcome {
			o := sampleOutcome("laravel", "anvil-standard-laravel", MigrationStatusRecognized)
			o.Status = "halfway"
			return o
		}(), "not a known migration outcome status"},
		{"recognized with version", "laravel", func() MigrationOutcome {
			o := sampleOutcome("laravel", "anvil-standard-laravel", MigrationStatusRecognized)
			o.StandardVersion = "1.2.3"
			return o
		}(), "must not pin a standard version"},
		{"migrated without version", "laravel", func() MigrationOutcome {
			o := sampleOutcome("laravel", "anvil-standard-laravel", MigrationStatusMigrated)
			o.StandardVersion = ""
			return o
		}(), "must pin the standard version"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newOutcomeStore(t)
			_, _, err := store.Record(tc.key, tc.outcome)
			if !errors.Is(err, ErrMigrationRecordInvalid) {
				t.Fatalf("error = %v, want wrapped ErrMigrationRecordInvalid", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error should mention %q, got: %v", tc.want, err)
			}
		})
	}
}

// TestAdapterMigrationStore_GetNotFoundAndCorrupt verifies the read-path
// classification: a missing record is wrapped ErrMigrationRecordNotFound;
// a corrupt file (undecodable) is wrapped ErrMigrationRecordCorrupt
// naming the file; a record declaring a different adapter than its file
// name is corrupt too.
func TestAdapterMigrationStore_GetNotFoundAndCorrupt(t *testing.T) {
	store := newOutcomeStore(t)

	if _, err := store.Get("laravel"); !errors.Is(err, ErrMigrationRecordNotFound) {
		t.Fatalf("Get(missing) = %v, want wrapped ErrMigrationRecordNotFound", err)
	}

	// Corrupt: undecodable content.
	corruptPath := filepath.Join(store.Dir(), "laravel.json")
	if err := os.MkdirAll(store.Dir(), 0755); err != nil {
		t.Fatalf("create store dir: %v", err)
	}
	if err := os.WriteFile(corruptPath, []byte("{not json"), 0644); err != nil {
		t.Fatalf("write corrupt record: %v", err)
	}
	if _, err := store.Get("laravel"); !errors.Is(err, ErrMigrationRecordCorrupt) {
		t.Fatalf("Get(corrupt) = %v, want wrapped ErrMigrationRecordCorrupt", err)
	}

	// Corrupt: a directory occupying the record path.
	dirPath := filepath.Join(store.Dir(), "flutter.json")
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		t.Fatalf("create dir at record path: %v", err)
	}
	if _, err := store.Get("flutter"); !errors.Is(err, ErrMigrationRecordCorrupt) {
		t.Fatalf("Get(dir at record path) = %v, want wrapped ErrMigrationRecordCorrupt", err)
	}
}

// TestAdapterMigrationStore_CorruptRecovery verifies recovery by
// re-recognition: recording over a corrupt record replaces it with the
// new write (the corrupt record never blocks the store).
func TestAdapterMigrationStore_CorruptRecovery(t *testing.T) {
	store := newOutcomeStore(t)
	path := filepath.Join(store.Dir(), "laravel.json")
	if err := os.MkdirAll(store.Dir(), 0755); err != nil {
		t.Fatalf("create store dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{corrupt"), 0644); err != nil {
		t.Fatalf("write corrupt record: %v", err)
	}

	outcome := sampleOutcome("laravel", "anvil-standard-laravel", MigrationStatusRecognized)
	got, created, err := store.Record("laravel", outcome)
	if err != nil {
		t.Fatalf("Record over corrupt record: %v", err)
	}
	if !created {
		t.Error("Record over a corrupt record should report created (recovery by re-recognition)")
	}
	if got.Status != MigrationStatusRecognized {
		t.Errorf("recovered record = %+v, want the recognized outcome", got)
	}
}

// TestAdapterMigrationStore_List verifies List returns every recorded
// outcome sorted by adapter name, and reports — without failing — the
// corrupt record files it skipped.
func TestAdapterMigrationStore_List(t *testing.T) {
	store := newOutcomeStore(t)
	if err := os.MkdirAll(store.Dir(), 0755); err != nil {
		t.Fatalf("create store dir: %v", err)
	}

	if _, _, err := store.Record("flutter", sampleOutcome("flutter", "anvil-standard-flutter", MigrationStatusRecognized)); err != nil {
		t.Fatalf("record flutter: %v", err)
	}
	if _, _, err := store.Record("laravel", sampleOutcome("laravel", "anvil-standard-laravel", MigrationStatusMigrated)); err != nil {
		t.Fatalf("record laravel: %v", err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir(), "broken.json"), []byte("{broken"), 0644); err != nil {
		t.Fatalf("write corrupt record: %v", err)
	}

	outcomes, corrupt, err := store.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("List = %d outcomes, want 2", len(outcomes))
	}
	if outcomes[0].AdapterName != "flutter" || outcomes[1].AdapterName != "laravel" {
		t.Errorf("List order = %q, %q — want sorted by adapter name", outcomes[0].AdapterName, outcomes[1].AdapterName)
	}
	if len(corrupt) != 1 || !strings.Contains(corrupt[0].Path, "broken.json") {
		t.Errorf("corrupt records = %+v, want the broken.json report", corrupt)
	}

	// A missing store directory is an empty store, not an error.
	if outcomes, corrupt, err := NewAdapterMigrationStore(filepath.Join(t.TempDir(), "missing")).List(); err != nil || len(outcomes) != 0 || len(corrupt) != 0 {
		t.Errorf("List(missing dir) = (%d, %d, %v), want (0, 0, nil)", len(outcomes), len(corrupt), err)
	}
}

// TestDefaultAdapterMigrationsDir verifies the default store directory
// follows the ADR-005 §7.1 global config convention:
// <config dir>/anvil/adapter-migrations.
func TestDefaultAdapterMigrationsDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/anvil-test-xdg")
	dir, err := DefaultAdapterMigrationsDir()
	if err != nil {
		t.Fatalf("DefaultAdapterMigrationsDir: %v", err)
	}
	if !strings.HasSuffix(dir, "/anvil/adapter-migrations") {
		t.Errorf("default dir = %q, want <config dir>/anvil/adapter-migrations", dir)
	}
}

// ── Recognition and migration orchestration ──────────────────────────

// TestRecognizeInstalledAdapter_FirstPartyLaravelRecognized is the
// Laravel first-party recognition case (TS-017-01-02 DoD): a project
// declaring framework "laravel" with a probe-validated installed
// adapter, and NO installed standard, is recognized via the
// authoritative mapping and the outcome is recorded as recognized —
// migration pending, the standard named as the completion step.
func TestRecognizeInstalledAdapter_FirstPartyLaravelRecognized(t *testing.T) {
	mapping := authoritativeMapping(t)
	standards := newTestStore(t)
	outcomes := newOutcomeStore(t)

	result, err := RecognizeInstalledAdapter(
		"laravel",
		map[string]string{"laravel": "/usr/local/bin/anvil-adapter-laravel"},
		mapping, standards, outcomes, []int{1}, testNow)
	if err != nil {
		t.Fatalf("RecognizeInstalledAdapter returned error: %v", err)
	}
	if !result.Recognized {
		t.Fatal("laravel adapter should be recognized")
	}
	if !result.Recorded {
		t.Error("first recognition should record a new outcome state")
	}
	if result.Row.StandardID != "anvil-standard-laravel" {
		t.Errorf("mapped standard = %q, want anvil-standard-laravel (from the authoritative mapping, never hard-coded)", result.Row.StandardID)
	}
	if result.Outcome.Status != MigrationStatusRecognized {
		t.Errorf("status = %q, want recognized — the standard is not installed, migration pending", result.Outcome.Status)
	}
	if result.Outcome.StandardVersion != "" {
		t.Errorf("standard version = %q, want empty for recognized", result.Outcome.StandardVersion)
	}

	// The outcome is persisted and re-readable.
	got, err := outcomes.Get("laravel")
	if err != nil {
		t.Fatalf("Get recorded outcome: %v", err)
	}
	if got.StandardID != "anvil-standard-laravel" || got.Status != MigrationStatusRecognized {
		t.Errorf("recorded outcome = %+v, want the recognized laravel outcome", got)
	}
}

// TestRecognizeInstalledAdapter_FirstPartyFlutterRecognized is the
// Flutter first-party recognition case (TS-017-01-02 DoD): the second
// first-party row of the authoritative mapping recognizes identically —
// proving the row set drives recognition, not any code-side list.
func TestRecognizeInstalledAdapter_FirstPartyFlutterRecognized(t *testing.T) {
	mapping := authoritativeMapping(t)
	standards := newTestStore(t)
	outcomes := newOutcomeStore(t)

	result, err := RecognizeInstalledAdapter(
		"flutter",
		map[string]string{"flutter": "/usr/local/bin/anvil-adapter-flutter"},
		mapping, standards, outcomes, []int{1}, testNow)
	if err != nil {
		t.Fatalf("RecognizeInstalledAdapter returned error: %v", err)
	}
	if !result.Recognized {
		t.Fatal("flutter adapter should be recognized")
	}
	if result.Row.StandardID != "anvil-standard-flutter" || result.Outcome.AdapterExecutable != "anvil-adapter-flutter" {
		t.Errorf("flutter outcome = %+v, want standard anvil-standard-flutter / executable anvil-adapter-flutter", result.Outcome)
	}
	if result.Outcome.Status != MigrationStatusRecognized {
		t.Errorf("status = %q, want recognized", result.Outcome.Status)
	}
}

// TestRecognizeInstalledAdapter_MigratedWhenStandardInstalled is the
// completed-migration case (TS-017-01-02 DoD: "migration switches
// resolution to the standard"; TS-017-01-03: the declared contract
// version is validated at migration): when the mapped standard has an
// installed-standard record whose declared contract version is
// supported by the runtime, the outcome is recorded as migrated, pins
// the version the resolution switched to, and fills the contract-version
// seam with the validated declared version. Project state is untouched —
// the recognition only consults the stores.
func TestRecognizeInstalledAdapter_MigratedWhenStandardInstalled(t *testing.T) {
	mapping := authoritativeMapping(t)
	standards := newTestStore(t)
	outcomes := newOutcomeStore(t)

	// The standard is installed (TS-014-03-03 record) declaring
	// contract version 1.0.0 — supported by this runtime (major 1).
	rec := sampleRecord("anvil-standard-laravel", "2.1.0")
	if _, _, err := standards.Record(rec.ID, rec); err != nil {
		t.Fatalf("record installed standard: %v", err)
	}

	result, err := RecognizeInstalledAdapter(
		"laravel",
		map[string]string{"laravel": "/usr/local/bin/anvil-adapter-laravel"},
		mapping, standards, outcomes, []int{1}, testNow)
	if err != nil {
		t.Fatalf("RecognizeInstalledAdapter returned error: %v", err)
	}
	if !result.Recognized {
		t.Fatal("laravel adapter should be recognized")
	}
	if result.Outcome.Status != MigrationStatusMigrated {
		t.Errorf("status = %q, want migrated — resolution switched to the standard", result.Outcome.Status)
	}
	if result.Outcome.StandardVersion != "2.1.0" {
		t.Errorf("standard version = %q, want 2.1.0 (the pinned version of the installed record)", result.Outcome.StandardVersion)
	}
	// The contract-version seam is filled with the VALIDATED declared
	// version of the installed standard (TS-017-01-03).
	if !result.ContractVersionValidated {
		t.Error("contract-version validation should have run — the standard is installed")
	}
	if !result.ContractVersionCompatible {
		t.Errorf("declared contract version should be compatible, errors: %v", result.ContractVersionErrors)
	}
	if result.Outcome.ContractVersion != "1.0.0" {
		t.Errorf("contract version = %q, want 1.0.0 (the declared contract version of the installed standard)", result.Outcome.ContractVersion)
	}
	if !result.Recorded {
		t.Error("first recognition should record a new outcome state")
	}

	// Re-running with the same state is a re-confirmation, not a
	// re-churn: no rewrite, no new record.
	result, err = RecognizeInstalledAdapter(
		"laravel",
		map[string]string{"laravel": "/usr/local/bin/anvil-adapter-laravel"},
		mapping, standards, outcomes, []int{1}, testNow)
	if err != nil {
		t.Fatalf("second RecognizeInstalledAdapter returned error: %v", err)
	}
	if result.Recorded {
		t.Error("re-recognition of the same state should not record")
	}
}

// TestRecognizeInstalledAdapter_StateTransition verifies the
// recognized → migrated transition: the outcome is re-recorded (a new
// state) once the standard is installed — the migration completing
// without any project-state change.
func TestRecognizeInstalledAdapter_StateTransition(t *testing.T) {
	mapping := authoritativeMapping(t)
	standards := newTestStore(t)
	outcomes := newOutcomeStore(t)

	// First: standard not installed — recognized.
	result, err := RecognizeInstalledAdapter(
		"laravel",
		map[string]string{"laravel": "/usr/local/bin/anvil-adapter-laravel"},
		mapping, standards, outcomes, []int{1}, testNow)
	if err != nil {
		t.Fatalf("recognize (pending): %v", err)
	}
	if result.Outcome.Status != MigrationStatusRecognized {
		t.Fatalf("status = %q, want recognized", result.Outcome.Status)
	}

	// The user installs the standard (TS-014-03-01) — the mapping
	// supplies the target identity.
	rec := sampleRecord("anvil-standard-laravel", "1.0.0")
	if _, _, err := standards.Record(rec.ID, rec); err != nil {
		t.Fatalf("record installed standard: %v", err)
	}

	result, err = RecognizeInstalledAdapter(
		"laravel",
		map[string]string{"laravel": "/usr/local/bin/anvil-adapter-laravel"},
		mapping, standards, outcomes, []int{1}, testNow)
	if err != nil {
		t.Fatalf("recognize (migrated): %v", err)
	}
	if !result.Recorded {
		t.Error("the recognized → migrated transition should record a new outcome state")
	}
	if result.Outcome.Status != MigrationStatusMigrated || result.Outcome.StandardVersion != "1.0.0" {
		t.Errorf("outcome = %+v, want migrated at 1.0.0", result.Outcome)
	}

	got, err := outcomes.Get("laravel")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != MigrationStatusMigrated {
		t.Errorf("persisted outcome status = %q, want migrated", got.Status)
	}
}

// TestRecognizeInstalledAdapter_NoDeclaration verifies that a project
// without a framework declaration triggers no recognition (RFC-P7:
// recognition keys on the config declaration).
func TestRecognizeInstalledAdapter_NoDeclaration(t *testing.T) {
	mapping := authoritativeMapping(t)
	outcomes := newOutcomeStore(t)

	result, err := RecognizeInstalledAdapter(
		"",
		map[string]string{"laravel": "/usr/local/bin/anvil-adapter-laravel"},
		mapping, newTestStore(t), outcomes, []int{1}, testNow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Recognized {
		t.Error("no declaration must not recognize")
	}
	if n, _, err := outcomes.List(); err != nil || len(n) != 0 {
		t.Errorf("no outcome should be recorded, got %d (err %v)", len(n), err)
	}
}

// TestRecognizeInstalledAdapter_NotFirstParty verifies that a declared
// framework with no mapping row is not recognized: third-party adapters
// are out of scope for the first-party mapping (§7), so no outcome is
// recorded.
func TestRecognizeInstalledAdapter_NotFirstParty(t *testing.T) {
	mapping := authoritativeMapping(t)
	outcomes := newOutcomeStore(t)

	result, err := RecognizeInstalledAdapter(
		"rails",
		map[string]string{"rails": "/usr/local/bin/anvil-adapter-rails"},
		mapping, newTestStore(t), outcomes, []int{1}, testNow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Recognized {
		t.Error("rails has no first-party mapping row — must not be recognized")
	}
	if n, _, err := outcomes.List(); err != nil || len(n) != 0 {
		t.Errorf("no outcome should be recorded for a non-first-party adapter, got %d", len(n))
	}
}

// TestRecognizeInstalledAdapter_AdapterNotInstalled verifies that a
// declared framework with a mapping row but NO probe-validated adapter
// binary is not recognized: recognition is never assumed from the
// declaration alone (RFC-P7 — executable identity, TS-007-039 §7).
func TestRecognizeInstalledAdapter_AdapterNotInstalled(t *testing.T) {
	mapping := authoritativeMapping(t)
	outcomes := newOutcomeStore(t)

	result, err := RecognizeInstalledAdapter(
		"laravel",
		map[string]string{}, // no installed adapter on the system
		mapping, newTestStore(t), outcomes, []int{1}, testNow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Recognized {
		t.Error("declaration alone must not recognize — no probe-validated adapter is installed")
	}
	if n, _, err := outcomes.List(); err != nil || len(n) != 0 {
		t.Errorf("no outcome should be recorded, got %d", len(n))
	}
}

// TestRecognizeInstalledAdapter_ExecutableIdentityMismatch verifies
// that a probed executable whose FILE NAME is not the mapping row's
// adapter_executable is not recognized: closed-set discovery only
// detects binaries named anvil-adapter-<name>, and the row's
// adapter_executable is that name (§4, §7) — a mismatch means the
// recognized binary is not the executable identity the mapping row
// describes (the mapping is the authoritative identity source).
func TestRecognizeInstalledAdapter_ExecutableIdentityMismatch(t *testing.T) {
	mapping := authoritativeMapping(t)
	outcomes := newOutcomeStore(t)

	result, err := RecognizeInstalledAdapter(
		"laravel",
		map[string]string{"laravel": "/usr/local/bin/anvil-adapter-laravel-v2"},
		mapping, newTestStore(t), outcomes, []int{1}, testNow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Recognized {
		t.Error("a probed executable name that is not the row's adapter_executable must not be recognized")
	}
	if n, _, err := outcomes.List(); err != nil || len(n) != 0 {
		t.Errorf("no outcome should be recorded on an identity mismatch, got %d", len(n))
	}
}

// TestRecognizeInstalledAdapter_StandardStoreFailure verifies that a
// standard store that cannot answer is a real failure, never a silent
// skip: a corrupt installed-standard record for the mapped standard
// must surface as an error (recognition is explicit).
func TestRecognizeInstalledAdapter_StandardStoreFailure(t *testing.T) {
	mapping := authoritativeMapping(t)
	standards := newTestStore(t)
	outcomes := newOutcomeStore(t)

	// A directory occupying the record path of the mapped standard:
	// the store cannot resolve the standard state.
	dirPath := filepath.Join(standards.Dir(), "anvil-standard-laravel.json")
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		t.Fatalf("create dir at record path: %v", err)
	}

	_, err := RecognizeInstalledAdapter(
		"laravel",
		map[string]string{"laravel": "/usr/local/bin/anvil-adapter-laravel"},
		mapping, standards, outcomes, []int{1}, testNow)
	if err == nil {
		t.Fatal("standard store failure should surface as an error, not a silent skip")
	}
	if !strings.Contains(err.Error(), "anvil-standard-laravel") {
		t.Errorf("error should name the standard, got: %v", err)
	}
}

// ── Contract-version validation at migration (TS-017-01-03) ───────────

// TestValidateMigrationContractVersion_Match asserts a declared contract
// version targeting a supported contract major validates (ADR-024 §3.1:
// the contract major is the unit of compatibility): the result reports
// compatible with no reasons, and the declared version and the checked
// set are recorded for auditability.
func TestValidateMigrationContractVersion_Match(t *testing.T) {
	result := ValidateMigrationContractVersion("1.0.0", []int{1}, "anvil-standard-laravel")

	if !result.Compatible {
		t.Errorf("Compatible = false, want true — declared 1.0.0 targets supported major 1")
	}
	if len(result.Errors) != 0 {
		t.Errorf("Errors = %v, want none on a valid match", result.Errors)
	}
	if result.DeclaredContractVersion != "1.0.0" {
		t.Errorf("DeclaredContractVersion = %q, want 1.0.0", result.DeclaredContractVersion)
	}
	if len(result.SupportedContractMajors) != 1 || result.SupportedContractMajors[0] != 1 {
		t.Errorf("SupportedContractMajors = %v, want [1]", result.SupportedContractMajors)
	}
}

// TestValidateMigrationContractVersion_MismatchMajor asserts a declared
// contract version targeting a contract major the runtime does not
// support fails validation with an actionable reason naming the
// unsupported major, the supported set, and the remediation (ADR-024
// §3.4): the mismatch is a recorded rejection, never a Go error and
// never a silent pass.
func TestValidateMigrationContractVersion_MismatchMajor(t *testing.T) {
	result := ValidateMigrationContractVersion("2.0.0", []int{1}, "anvil-standard-laravel")

	if result.Compatible {
		t.Error("Compatible = true, want false — declared 2.0.0 targets unsupported major 2")
	}
	if len(result.Errors) != 1 {
		t.Fatalf("Errors = %v, want exactly one rejection reason", result.Errors)
	}
	err := result.Errors[0]
	if !strings.Contains(err, "anvil-standard-laravel") {
		t.Errorf("reason should name the standard, got: %s", err)
	}
	if !strings.Contains(err, "2.0.0") || !strings.Contains(err, "major 2") {
		t.Errorf("reason should name the declared version and the unsupported major, got: %s", err)
	}
	if !strings.Contains(err, "[1]") {
		t.Errorf("reason should name the supported contract majors, got: %s", err)
	}
	if !strings.Contains(err, "ADR-024") {
		t.Errorf("reason should cite the policy, got: %s", err)
	}
}

// TestValidateMigrationContractVersion_Malformed asserts a declared
// contract version that is not well-formed semver fails validation with
// an actionable reason (registry-metadata §4.3: semver without leading
// zeros) — the malformed declaration is a recorded rejection, never
// accepted by coercion.
func TestValidateMigrationContractVersion_Malformed(t *testing.T) {
	for _, declared := range []string{"not-a-version", "1", "1.0", "01.0.0", "1.0.0-rc1"} {
		result := ValidateMigrationContractVersion(declared, []int{1}, "anvil-standard-laravel")
		if result.Compatible {
			t.Errorf("Compatible = true for declared %q — malformed semver must be rejected", declared)
		}
		if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "not well-formed semver") {
			t.Errorf("Errors = %v for declared %q, want one well-formedness rejection", result.Errors, declared)
		}
	}
}

// TestValidateMigrationContractVersion_EmptyDeclared asserts an empty
// declared contract version fails validation: a standard that does not
// declare compatibility is rejected (PRD-002 §5.8). The installed-
// standard record requires a declared contract version
// (validateRecord), so this guards the defensive path.
func TestValidateMigrationContractVersion_EmptyDeclared(t *testing.T) {
	result := ValidateMigrationContractVersion("", []int{1}, "anvil-standard-laravel")

	if result.Compatible {
		t.Error("Compatible = true for an empty declaration — a standard that does not declare compatibility must be rejected")
	}
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "declares no contract version") {
		t.Errorf("Errors = %v, want the missing-declaration rejection", result.Errors)
	}
}

// TestValidateMigrationContractVersion_NoSupportedMajors asserts a
// runtime declaring no supported contract majors fails validation
// fail-closed: the migration cannot be validated against an empty set —
// supported majors are never silently defaulted (PM binding decision 3;
// ADR-024 §3.4).
func TestValidateMigrationContractVersion_NoSupportedMajors(t *testing.T) {
	result := ValidateMigrationContractVersion("1.0.0", nil, "anvil-standard-laravel")

	if result.Compatible {
		t.Error("Compatible = true with no supported majors — validation must fail closed")
	}
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "declares no supported contract majors") {
		t.Errorf("Errors = %v, want the no-supported-majors rejection", result.Errors)
	}
}

// ── Contract-version validation in the recognition flow ───────────────

// TestRecognizeInstalledAdapter_MismatchDoesNotCompleteMigration is the
// mismatch case of TS-017-01-03: the mapped standard IS installed but
// its declared contract version targets a contract major the runtime
// does not support. The migration must NOT silently pass — the outcome
// is recorded as recognized (the migration did not complete;the v1.x
// adapter keeps working, ADR-028 §12.3), the declared contract version
// is recorded in the contract-version seam for auditability, and the
// actionable reasons are surfaced in the recognition result.
func TestRecognizeInstalledAdapter_MismatchDoesNotCompleteMigration(t *testing.T) {
	mapping := authoritativeMapping(t)
	standards := newTestStore(t)
	outcomes := newOutcomeStore(t)

	// The standard is installed declaring contract version 2.0.0 —
	// unsupported by this runtime (supported contract majors: {1}).
	rec := sampleRecord("anvil-standard-laravel", "2.1.0")
	rec.ContractVersion = "2.0.0"
	if _, _, err := standards.Record(rec.ID, rec); err != nil {
		t.Fatalf("record installed standard: %v", err)
	}

	result, err := RecognizeInstalledAdapter(
		"laravel",
		map[string]string{"laravel": "/usr/local/bin/anvil-adapter-laravel"},
		mapping, standards, outcomes, []int{1}, testNow)
	if err != nil {
		t.Fatalf("RecognizeInstalledAdapter returned error: %v", err)
	}
	if !result.Recognized {
		t.Fatal("laravel adapter should be recognized")
	}
	if !result.ContractVersionValidated {
		t.Error("contract-version validation should have run — the standard is installed")
	}
	if result.ContractVersionCompatible {
		t.Error("declared contract version 2.0.0 must not be compatible — the runtime supports major 1 only")
	}
	if len(result.ContractVersionErrors) == 0 {
		t.Error("mismatch must carry actionable reasons")
	}
	// The mismatch never silently passes: the outcome is recorded as
	// recognized — the migration did NOT complete.
	if result.Outcome.Status != MigrationStatusRecognized {
		t.Errorf("status = %q, want recognized — a mismatch never silently completes the migration", result.Outcome.Status)
	}
	if result.Outcome.StandardVersion != "" {
		t.Errorf("standard version = %q, want empty — the migration did not complete, no version is pinned", result.Outcome.StandardVersion)
	}
	// The declared contract version is recorded so the mismatch is
	// observable and auditable (ADR-024 §3.6).
	if result.Outcome.ContractVersion != "2.0.0" {
		t.Errorf("contract version = %q, want 2.0.0 (the declared contract version that failed validation)", result.Outcome.ContractVersion)
	}
	if !result.Recorded {
		t.Error("first recognition should record a new outcome state")
	}

	// The mismatch outcome is persisted and re-readable.
	got, err := outcomes.Get("laravel")
	if err != nil {
		t.Fatalf("Get recorded outcome: %v", err)
	}
	if got.Status != MigrationStatusRecognized || got.ContractVersion != "2.0.0" {
		t.Errorf("recorded outcome = %+v, want the recognized mismatch outcome with contract version 2.0.0", got)
	}
}

// TestRecognizeInstalledAdapter_MismatchRecoveredBySupportedStandard
// verifies the recovery path of a recorded mismatch (TS-017-01-03): the
// declared contract version is part of the recorded state — when the
// standard is updated to a release declaring a supported contract
// version, the next recognition records the completed migration (a NEW
// outcome state), so the recorded outcome never stays silently stale.
func TestRecognizeInstalledAdapter_MismatchRecoveredBySupportedStandard(t *testing.T) {
	mapping := authoritativeMapping(t)
	standards := newTestStore(t)
	outcomes := newOutcomeStore(t)

	// First recognition: the installed standard declares 2.0.0 —
	// mismatch, migration does not complete.
	rec := sampleRecord("anvil-standard-laravel", "2.1.0")
	rec.ContractVersion = "2.0.0"
	if _, _, err := standards.Record(rec.ID, rec); err != nil {
		t.Fatalf("record installed standard: %v", err)
	}
	result, err := RecognizeInstalledAdapter(
		"laravel",
		map[string]string{"laravel": "/usr/local/bin/anvil-adapter-laravel"},
		mapping, standards, outcomes, []int{1}, testNow)
	if err != nil {
		t.Fatalf("first RecognizeInstalledAdapter returned error: %v", err)
	}
	if result.Outcome.Status != MigrationStatusRecognized || result.Outcome.ContractVersion != "2.0.0" {
		t.Fatalf("first outcome = %+v, want the recognized mismatch", result.Outcome)
	}

	// The standard is updated (TS-014-03-02) to a release declaring a
	// supported contract version (1.0.0).
	updated := sampleRecord("anvil-standard-laravel", "2.2.0")
	updated.ContractVersion = "1.0.0"
	updated.UpdatedAt = time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	if _, err := standards.Update(updated.ID, updated); err != nil {
		t.Fatalf("update installed standard: %v", err)
	}

	// Re-recognition: the declared version is now supported — the
	// migration completes and the state change is recorded.
	result, err = RecognizeInstalledAdapter(
		"laravel",
		map[string]string{"laravel": "/usr/local/bin/anvil-adapter-laravel"},
		mapping, standards, outcomes, []int{1}, testNow)
	if err != nil {
		t.Fatalf("second RecognizeInstalledAdapter returned error: %v", err)
	}
	if !result.Recorded {
		t.Error("the declared-contract-version change should record a new outcome state")
	}
	if !result.ContractVersionCompatible {
		t.Errorf("declared contract version 1.0.0 should be compatible, errors: %v", result.ContractVersionErrors)
	}
	if result.Outcome.Status != MigrationStatusMigrated {
		t.Errorf("status = %q, want migrated after the mismatch recovery", result.Outcome.Status)
	}
	if result.Outcome.ContractVersion != "1.0.0" {
		t.Errorf("contract version = %q, want 1.0.0 (the validated declared version)", result.Outcome.ContractVersion)
	}
}

// TestRecognizeInstalledAdapter_RecognizedRecordsNoContractVersion
// verifies the not-installed case of TS-017-01-03: when the mapped
// standard is NOT installed, there is no declared contract version at
// migration time (nothing is declared yet) — the outcome is recorded as
// recognized without a contract version, and the recognition result
// reports that nothing was validated (never an assumed validation).
func TestRecognizeInstalledAdapter_RecognizedRecordsNoContractVersion(t *testing.T) {
	mapping := authoritativeMapping(t)
	standards := newTestStore(t)
	outcomes := newOutcomeStore(t)

	result, err := RecognizeInstalledAdapter(
		"laravel",
		map[string]string{"laravel": "/usr/local/bin/anvil-adapter-laravel"},
		mapping, standards, outcomes, []int{1}, testNow)
	if err != nil {
		t.Fatalf("RecognizeInstalledAdapter returned error: %v", err)
	}
	if result.Outcome.Status != MigrationStatusRecognized {
		t.Fatalf("status = %q, want recognized", result.Outcome.Status)
	}
	if result.ContractVersionValidated {
		t.Error("nothing was validated — the standard is not installed")
	}
	if result.ContractVersionCompatible {
		t.Error("Compatible must be false — no declaration was checked")
	}
	if result.Outcome.ContractVersion != "" {
		t.Errorf("contract version = %q, want empty — no standard is installed, nothing is declared at migration time", result.Outcome.ContractVersion)
	}
}

// TestAdapterMigrationStore_RecordContractVersionChange verifies that
// the validated declared contract version is part of the recorded
// outcome state (TS-017-01-03): re-recording the same outcome with a
// different contract version replaces the record and reports created —
// the outcome never stays silently stale (the guard T-004 anticipated
// for TS-017-01-03 consumers).
func TestAdapterMigrationStore_RecordContractVersionChange(t *testing.T) {
	store := newOutcomeStore(t)
	mismatch := sampleOutcome("laravel", "anvil-standard-laravel", MigrationStatusRecognized)
	mismatch.ContractVersion = "2.0.0"

	if _, created, err := store.Record("laravel", mismatch); err != nil || !created {
		t.Fatalf("first Record = (created %v, err %v), want created", created, err)
	}

	// The same outcome state except for the declared contract version
	// (a corrected standard release declares 1.0.0 instead of 2.0.0):
	// the state changed and must be recorded as a change.
	mismatch.ContractVersion = "1.0.0"
	reRecorded, created, err := store.Record("laravel", mismatch)
	if err != nil {
		t.Fatalf("re-Record with a different contract version returned error: %v", err)
	}
	if !created {
		t.Error("a declared contract version change should record a new outcome state")
	}
	if reRecorded.ContractVersion != "1.0.0" {
		t.Errorf("re-recorded contract version = %q, want 1.0.0", reRecorded.ContractVersion)
	}

	got, err := store.Get("laravel")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got.ContractVersion != "1.0.0" {
		t.Errorf("persisted contract version = %q, want 1.0.0 — the recorded state reflects the latest validation outcome", got.ContractVersion)
	}
}
