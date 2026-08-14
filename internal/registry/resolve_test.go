package registry

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestStandardIDForFramework verifies the explicit framework-to-standard
// id mapping rule of framework-declared initialization: first-party
// delivery lifecycle standards are named anvil-standard-<framework>
// (ADR-021 §3.1), so a declared framework name resolves to
// "anvil-standard-<framework>".
func TestStandardIDForFramework(t *testing.T) {
	tests := []struct {
		framework string
		want      string
	}{
		{"laravel", "anvil-standard-laravel"},
		{"flutter", "anvil-standard-flutter"},
		{"symfony", "anvil-standard-symfony"},
		{"rails", "anvil-standard-rails"},
	}
	for _, tt := range tests {
		if got := StandardIDForFramework(tt.framework); got != tt.want {
			t.Errorf("StandardIDForFramework(%q) = %q, want %q", tt.framework, got, tt.want)
		}
	}
}

// TestResolveFrameworkStandard_Installed verifies the resolution success
// case (TS-015-02-01 DoD: a declared framework resolves to the installed
// standard): with the standard's installed-standard record present, the
// declared framework resolves to exactly that record — identity, pinned
// version, declared contract version, and the explicit resolution
// recorded at install (ADR-022 §3). The resolution comes from the record
// store; no runtime knowledge participates.
func TestResolveFrameworkStandard_Installed(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	got, err := store.ResolveFrameworkStandard("laravel")
	if err != nil {
		t.Fatalf("ResolveFrameworkStandard: %v", err)
	}
	if !reflect.DeepEqual(got, rec) {
		t.Errorf("ResolveFrameworkStandard returned %+v, want %+v", got, rec)
	}
}

// TestResolveFrameworkStandard_InstalledOtherStandards verifies that
// resolution is exact by standard id: other installed standards never
// satisfy a declared framework — only the standard named
// anvil-standard-<framework> resolves (never runtime knowledge, no fuzzy
// matching).
func TestResolveFrameworkStandard_InstalledOtherStandards(t *testing.T) {
	store := newTestStore(t)
	for _, id := range []string{"anvil-standard-flutter", "anvil-standard-symfony"} {
		rec := sampleRecord(id, "1.0.0")
		if _, _, err := store.Record(rec.ID, rec); err != nil {
			t.Fatalf("Record %s: %v", id, err)
		}
	}

	_, err := store.ResolveFrameworkStandard("laravel")
	if !errors.Is(err, ErrStandardNotInstalled) {
		t.Fatalf("ResolveFrameworkStandard error = %v, want wrapped %v (no-match hand-off, TS-015-02-02)", err, ErrStandardNotInstalled)
	}
}

// TestResolveFrameworkStandard_NoMatch verifies the no-match case
// (TS-015-02-01 DoD: no-match cases hand off to standard-missing
// semantics): a declared framework with no installed-standard record
// yields the distinguishable ErrStandardNotInstalled signal — the hand-off
// for TS-015-02-02 — never a silent success and never a store-level
// error. A missing store directory means no records and is also a
// no-match, not a failure.
func TestResolveFrameworkStandard_NoMatch(t *testing.T) {
	store := newTestStore(t) // empty store, no records

	_, err := store.ResolveFrameworkStandard("laravel")
	if !errors.Is(err, ErrStandardNotInstalled) {
		t.Fatalf("ResolveFrameworkStandard error = %v, want wrapped %v", err, ErrStandardNotInstalled)
	}
	if err == nil {
		t.Fatal("expected an error for a framework with no installed standard")
	}
	msg := err.Error()
	if !strings.Contains(msg, "anvil-standard-laravel") {
		t.Errorf("no-match error should name the missing standard id, got: %s", msg)
	}
	if !strings.Contains(msg, "anvil standard install") {
		t.Errorf("no-match error should carry the install remediation, got: %s", msg)
	}
}

// TestResolveFrameworkStandard_MissingStore verifies the missing-store
// directory case: no store directory exists — resolution reports
// no-match (nothing installed), never a store failure. This is the fresh
// machine case: the first framework-declared init without any adoption
// records must hand off to the standard-missing semantics, not fail on
// the store.
func TestResolveFrameworkStandard_MissingStore(t *testing.T) {
	store := NewInstalledStandardStore(filepath.Join(t.TempDir(), "does-not-exist"))

	_, err := store.ResolveFrameworkStandard("flutter")
	if !errors.Is(err, ErrStandardNotInstalled) {
		t.Fatalf("ResolveFrameworkStandard error = %v, want wrapped %v", err, ErrStandardNotInstalled)
	}
}

// TestResolveFrameworkStandard_CorruptRecord verifies that a corrupt
// record file is a real store failure, never a silent no-match: the
// record store cannot answer whether the standard is installed, so
// resolution surfaces the wrapped ErrRecordCorrupt with the file name.
func TestResolveFrameworkStandard_CorruptRecord(t *testing.T) {
	store := newTestStore(t)
	path := recordFilePath(store, "anvil-standard-laravel")
	if err := os.WriteFile(path, []byte("{not json"), 0644); err != nil {
		t.Fatalf("write corrupt record: %v", err)
	}

	_, err := store.ResolveFrameworkStandard("laravel")
	if !errors.Is(err, ErrRecordCorrupt) {
		t.Fatalf("ResolveFrameworkStandard error = %v, want wrapped %v", err, ErrRecordCorrupt)
	}
	if errors.Is(err, ErrStandardNotInstalled) {
		t.Error("a corrupt record must not be reported as no-match — the store cannot answer")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("corrupt-record error should name the record file, got: %v", err)
	}
}

// TestResolveFrameworkStandard_UnsafeFrameworkName verifies that a
// framework name that cannot form a safe standard id (the id is the
// record file name) is an invalid-id failure, not a no-match: the derived
// standard id is rejected by the store before any record read. The
// framework declaration itself is not whitelisted (TS-015-01-03); this is
// the resolution boundary reporting the unsafe id.
func TestResolveFrameworkStandard_UnsafeFrameworkName(t *testing.T) {
	store := newTestStore(t)

	_, err := store.ResolveFrameworkStandard("foo.bar")
	if !errors.Is(err, ErrRecordInvalid) {
		t.Fatalf("ResolveFrameworkStandard error = %v, want wrapped %v", err, ErrRecordInvalid)
	}
}
