package registry

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

// sampleConfigExtension returns configuration extension content for the
// laravel namespace exercising every declared-key field of the EPIC-013
// config extension contract: a defaulted key, a required key with a
// default, and a required key without a default.
func sampleConfigExtension() *ConfigExtensionContent {
	return &ConfigExtensionContent{
		Namespace: "laravel",
		Keys: []ConfigExtensionKey{
			{Name: "version", Description: "Laravel version.", Default: "11.0.0"},
			{Name: "cache.store", Description: "Cache store.", Default: "redis", Required: true},
			{Name: "build_args", Description: "Extra build args.", Required: true},
		},
	}
}

// TestInstalledStandardRecord_ConfigExtensionRoundTrip verifies that the
// configuration extension content is part of the installed standard
// (TS-015-03-01): a record written with embedded content reads back with
// the content byte-for-byte — the store persists the standard's content
// and never drops or interprets it (mirroring the embedded
// Compatibility/Trust results pattern).
func TestInstalledStandardRecord_ConfigExtensionRoundTrip(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	rec.ConfigExtension = sampleConfigExtension()
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	got, err := store.Get(rec.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(got.ConfigExtension, sampleConfigExtension()) {
		t.Errorf("Get returned ConfigExtension %+v, want %+v", got.ConfigExtension, sampleConfigExtension())
	}
}

// TestInstalledStandardRecord_ConfigExtensionAbsent verifies that a
// record written without config extension content reads back without it
// (null): a standard that declares nothing in the config extension
// category (command-contract §4.1) leaves the embedded section absent —
// the resolution hands off to ErrConfigExtensionMissing.
func TestInstalledStandardRecord_ConfigExtensionAbsent(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	got, err := store.Get(rec.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ConfigExtension != nil {
		t.Errorf("Get returned ConfigExtension %+v, want nil", got.ConfigExtension)
	}
}

// TestConfigExtensionContent_Resolved verifies the record-level
// resolution success case (TS-015-03-01 DoD: framework config keys
// resolve from the installed standard): a record carrying content whose
// namespace matches the declared framework resolves to exactly that
// content — keys and defaults come from the installed standard, never
// from runtime knowledge.
func TestConfigExtensionContent_Resolved(t *testing.T) {
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	rec.ConfigExtension = sampleConfigExtension()

	got, err := rec.ConfigExtensionContent("laravel")
	if err != nil {
		t.Fatalf("ConfigExtensionContent: %v", err)
	}
	if !reflect.DeepEqual(got, *sampleConfigExtension()) {
		t.Errorf("ConfigExtensionContent returned %+v, want %+v", got, *sampleConfigExtension())
	}
}

// TestConfigExtensionContent_Missing verifies the no-content outcome
// (TS-015-03-01): a resolved standard whose record carries no config
// extension content yields the wrapped ErrConfigExtensionMissing hand-off
// signal — a distinguishable outcome, never a store failure and never an
// invented resolution. The caller decides how it is surfaced (warning
// pattern, mirroring T-004's ErrStandardNotInstalled hand-off; the
// hard-fail of TS-015-02-02 is not implemented here).
func TestConfigExtensionContent_Missing(t *testing.T) {
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")

	_, err := rec.ConfigExtensionContent("laravel")
	if !errors.Is(err, ErrConfigExtensionMissing) {
		t.Fatalf("ConfigExtensionContent error = %v, want wrapped %v", err, ErrConfigExtensionMissing)
	}
	if !strings.Contains(err.Error(), rec.ID) {
		t.Errorf("missing-content error should name the standard, got: %v", err)
	}
}

// TestConfigExtensionContent_NamespaceMismatch verifies the namespace
// isolation guard (TS-015-03-01, C6 / command-contract §4.5): content
// declaring a namespace different from the declared framework is a
// violation — the record is inconsistent with the standard it belongs to,
// an actionable error with the reinstall remediation, never a silent
// pass-through of foreign content.
func TestConfigExtensionContent_NamespaceMismatch(t *testing.T) {
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	rec.ConfigExtension = &ConfigExtensionContent{
		Namespace: "rails",
		Keys: []ConfigExtensionKey{
			{Name: "version", Description: "Version.", Default: "7.1.0"},
		},
	}

	_, err := rec.ConfigExtensionContent("laravel")
	if err == nil {
		t.Fatal("ConfigExtensionContent expected a namespace violation error, got nil")
	}
	for _, want := range []string{"rails", "laravel", "re-install"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("namespace violation error should mention %q, got: %v", want, err)
		}
	}
}

// TestResolveConfigExtension_Installed verifies the store-level
// resolution success case (TS-015-03-01): with the standard's
// installed-standard record present and carrying content, a declared
// framework resolves through the installed-standard records to exactly
// that content — the resolution is explicit and recorded, never runtime
// knowledge.
func TestResolveConfigExtension_Installed(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	rec.ConfigExtension = sampleConfigExtension()
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	got, err := store.ResolveConfigExtension("laravel")
	if err != nil {
		t.Fatalf("ResolveConfigExtension: %v", err)
	}
	if !reflect.DeepEqual(got, *sampleConfigExtension()) {
		t.Errorf("ResolveConfigExtension returned %+v, want %+v", got, *sampleConfigExtension())
	}
}

// TestResolveConfigExtension_InstalledWithoutContent verifies the
// store-level no-content outcome: an installed standard whose record
// carries no config extension content resolves to the wrapped
// ErrConfigExtensionMissing hand-off signal — the standard IS installed
// (not ErrStandardNotInstalled), it simply declares nothing in the config
// extension category.
func TestResolveConfigExtension_InstalledWithoutContent(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	_, err := store.ResolveConfigExtension("laravel")
	if !errors.Is(err, ErrConfigExtensionMissing) {
		t.Fatalf("ResolveConfigExtension error = %v, want wrapped %v", err, ErrConfigExtensionMissing)
	}
	if errors.Is(err, ErrStandardNotInstalled) {
		t.Error("an installed standard without content must not report not-installed")
	}
}

// TestResolveConfigExtension_NotInstalled verifies the no-match hand-off
// of the store-level resolution: a declared framework with no installed
// standard yields the wrapped ErrStandardNotInstalled signal (TS-015-02-01
// / TS-015-02-02) — content resolution never invents a standard.
func TestResolveConfigExtension_NotInstalled(t *testing.T) {
	store := newTestStore(t)

	_, err := store.ResolveConfigExtension("rails")
	if !errors.Is(err, ErrStandardNotInstalled) {
		t.Fatalf("ResolveConfigExtension error = %v, want wrapped %v", err, ErrStandardNotInstalled)
	}
}

// TestResolveConfigExtension_CorruptRecord verifies that a corrupt
// installed-standard record fails the content resolution with the
// store's corrupt-record outcome: the store cannot answer, so the
// resolution fails as a real failure — never a silent no-content or
// no-match.
func TestResolveConfigExtension_CorruptRecord(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := os.WriteFile(recordFilePath(store, rec.ID), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("corrupt record: %v", err)
	}

	_, err := store.ResolveConfigExtension("laravel")
	if !errors.Is(err, ErrRecordCorrupt) {
		t.Fatalf("ResolveConfigExtension error = %v, want wrapped %v", err, ErrRecordCorrupt)
	}
}
