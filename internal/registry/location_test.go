package registry

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureEntry loads a corpus positive fixture as an index Entry, so
// resolution tests run against canonical corpus material (TS-014-02-03:
// fixture-based entries). The fixture document is decoded structurally,
// exactly as the index client resolves entries (index.go loadDocument);
// the parse layer is intentionally not involved, which also exercises the
// defensive path — entries reaching ResolveLocation carry no guarantee
// that they passed Parse.
func fixtureEntry(t *testing.T, name string) Entry {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixturesDir, "positive", name+".json"))
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("fixture not present (EKA mode) — %v", err)
		}
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var md Metadata
	if err := json.Unmarshal(raw, &md); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
	return Entry{Metadata: md}
}

// TestResolveLocationSuccess asserts a standard version resolves to its
// release-channel distribution location: for every positive corpus
// fixture, ResolveLocation returns the declared https location and the
// github-releases channel type (TS-014-02-03 DoD: a standard version
// resolves to its release-channel distribution location; ADR-030 §3).
func TestResolveLocationSuccess(t *testing.T) {
	names := []string{
		"published-full",
		"published-minimal",
		"deprecated-with-removal-date",
		"deprecated-without-removal-date",
		"retired-metadata",
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			entry := fixtureEntry(t, name)

			loc, err := ResolveLocation(entry)
			if err != nil {
				t.Fatalf("ResolveLocation(%q %q): %v", entry.ID, entry.Version, err)
			}
			if loc.Type != DistributionTypeGitHubReleases {
				t.Errorf("loc.Type = %q, want %q", loc.Type, DistributionTypeGitHubReleases)
			}
			if loc.Location != entry.Distribution.Location {
				t.Errorf("loc.Location = %q, want %q", loc.Location, entry.Distribution.Location)
			}
		})
	}
}

// TestResolveLocationMissingLocation asserts an entry whose distribution
// declaration is absent — no location URL, no channel type, or neither —
// fails with an actionable error wrapped around ErrLocationMissing: the
// release content cannot be resolved for install before install completes
// (TS-014-02-03 DoD).
func TestResolveLocationMissingLocation(t *testing.T) {
	for _, tc := range []struct {
		name        string
		entry       Entry
		wantFixPath string
	}{
		{
			name:        "zero entry",
			entry:       Entry{Metadata: Metadata{ID: "anvil-standard-laravel", Version: "1.2.3"}},
			wantFixPath: "distribution.location",
		},
		{
			name: "type only",
			entry: Entry{
				Metadata: Metadata{
					ID:      "anvil-standard-laravel",
					Version: "1.2.3",
					Distribution: Distribution{
						Type: DistributionTypeGitHubReleases,
					},
				},
			},
			wantFixPath: "distribution.location",
		},
		{
			name: "location only",
			entry: Entry{
				Metadata: Metadata{
					ID:      "anvil-standard-laravel",
					Version: "1.2.3",
					Distribution: Distribution{
						Location: "https://github.com/maleolabs/anvil-standard-laravel/releases/download/v1.2.3/anvil-standard-laravel.tar.gz",
					},
				},
			},
			wantFixPath: "distribution.type",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ResolveLocation(tc.entry)
			if !errors.Is(err, ErrLocationMissing) {
				t.Fatalf("ResolveLocation error = %v, want wrapped %v", err, ErrLocationMissing)
			}
			// Actionable: the error names the standard, the version, what
			// failed, and how to fix it.
			for _, want := range []string{tc.entry.ID, tc.entry.Version, tc.wantFixPath, "Fix the metadata document"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}

// TestResolveLocationUnsupportedType asserts an entry declaring a channel
// type outside the supported set fails with an actionable error wrapped
// around ErrLocationUnsupportedType. Defensive: the parse layer rejects
// unsupported types at the document level (parse.go enum), but entries can
// reach resolution without passing through Parse, and per ADR-030 §3 a new
// channel pattern is a schema evolution, not a metadata value.
func TestResolveLocationUnsupportedType(t *testing.T) {
	for _, typeValue := range []string{"s3", "ftp", "raw-github", "github-release"} {
		t.Run(typeValue, func(t *testing.T) {
			entry := fixtureEntry(t, "published-full")
			entry.Distribution.Type = typeValue

			_, err := ResolveLocation(entry)
			if !errors.Is(err, ErrLocationUnsupportedType) {
				t.Fatalf("ResolveLocation error = %v, want wrapped %v", err, ErrLocationUnsupportedType)
			}
			for _, want := range []string{entry.ID, entry.Version, typeValue, DistributionTypeGitHubReleases, "Fix the metadata document"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}

// TestResolveLocationInvalidLocation asserts a declared location that is
// not usable for install — non-https scheme, malformed URL, missing host,
// whitespace — fails with an actionable error wrapped around
// ErrLocationInvalid. Defensive re-validation: these entries carry
// locations the parse layer would have rejected (parse.go), but resolution
// must not trust an entry that skipped Parse (TS-014-02-03: validate the
// location is usable for install, https and well-formed).
func TestResolveLocationInvalidLocation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		location string
	}{
		{name: "non-https scheme", location: "http://github.com/maleolabs/anvil-standard-laravel/releases/download/v1.2.3/anvil-standard-laravel.tar.gz"},
		{name: "malformed url", location: "https://"},
		{name: "no host", location: "https:///releases/download/v1.2.3/anvil-standard-laravel.tar.gz"},
		{name: "whitespace in url", location: "https://github.com/maleolabs/anvil-standard-laravel/releases/download/v1.2.3/a b.tar.gz"},
		{name: "not a url", location: "not a url"},
		{name: "userinfo username only", location: "https://alice@github.com/maleolabs/anvil-standard-laravel/releases/download/v1.2.3/a.tar.gz"},
		{name: "userinfo username and password", location: "https://alice:secret@github.com/maleolabs/anvil-standard-laravel/releases/download/v1.2.3/a.tar.gz"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := fixtureEntry(t, "published-full")
			entry.Distribution.Location = tc.location

			_, err := ResolveLocation(entry)
			if !errors.Is(err, ErrLocationInvalid) {
				t.Fatalf("ResolveLocation error = %v, want wrapped %v", err, ErrLocationInvalid)
			}
			for _, want := range []string{entry.ID, entry.Version, "distribution.location", "Fix the metadata document"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}

// TestResolveLocationDefensiveZeroEntry asserts the zero Entry value — the
// defensive failure case for a caller that forgot to resolve before asking
// for a location — fails with the missing-location sentinel instead of
// returning an empty, unusable location.
func TestResolveLocationDefensiveZeroEntry(t *testing.T) {
	_, err := ResolveLocation(Entry{})
	if !errors.Is(err, ErrLocationMissing) {
		t.Fatalf("ResolveLocation(Entry{}) error = %v, want wrapped %v", err, ErrLocationMissing)
	}
}

// TestResolveLocationDoesNotVerifyTrustOrLifecycle pins the component
// boundary: resolution is a pure mapping over the distribution declaration
// and deliberately ignores the trust fields and the lifecycle state — it
// is not a trust check and not an adoptability decision (ADR-022 §3:
// integrity is enforced at install by trust validation, never by the
// channel; TS-014-02-03 DoD: resolution feeds the validation-at-adoption
// flow without bypass — the flow performs the trust and lifecycle
// validation, this component cannot be used to skip it).
func TestResolveLocationDoesNotVerifyTrustOrLifecycle(t *testing.T) {
	entry := fixtureEntry(t, "published-full")
	entry.Trust = Trust{}
	entry.Lifecycle.State = LifecycleStateRetired

	loc, err := ResolveLocation(entry)
	if err != nil {
		t.Fatalf("ResolveLocation: %v", err)
	}
	if loc.Location != entry.Distribution.Location {
		t.Errorf("loc.Location = %q, want %q", loc.Location, entry.Distribution.Location)
	}
}
