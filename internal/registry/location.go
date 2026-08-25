// Content location resolution (TS-014-02-03).
//
// Per ADR-030 the registry is distribution metadata, not content hosting:
// a resolved standard version maps to the release content's distribution
// location on the standard's own release channel, and installation fetches
// content from there. This file implements that mapping: ResolveLocation
// turns a resolved index entry (TS-014-02-01) into a Location — the
// channel type and the https URL the install flow fetches content from.
//
// Scope boundaries. Resolution is a pure mapping over metadata: it does
// not fetch content (fetching belongs to the install flow, TS-014-03-01 /
// T-007) and it does not verify trust (integrity verification and
// attestation are the trust work item TS-014-01-04 / T-011; ADR-022 §3).
// It likewise does not decide adoptability — lifecycle state semantics are
// the lifecycle helpers' job (TS-014-01-03 / lifecycle.go). The install
// flow consumes this component inside the validated adoption path
// (TS-014-04-01 / T-012): resolution feeds validation-at-adoption, it
// never bypasses it.
//
// Resolution defensively re-validates the declared location with the same
// strict rules the parse layer enforces (TS-014-01-02 / parse.go:
// https-only scheme, well-formed absolute URL with a host). Entries that
// reach ResolveLocation without passing through Parse — programmatically
// constructed entries, future index sources — therefore cannot produce an
// unusable location for install. Unresolvable locations fail with an
// actionable error naming the standard, the version, what failed, and how
// to fix it, so the failure surfaces at adoption, not in production
// (TS-014-02-03 DoD).
//
// Reference: TS-014-02-03, ADR-030 §3, ADR-022 §3, ADR-023 §3
package registry

import (
	"errors"
	"fmt"
)

// Location is a resolved content distribution location of one standard
// release (TS-014-02-03): the channel pattern (ADR-030) and the https URL
// the install flow fetches the release content from. Resolving surfaces
// both so consumers never read a location without knowing its channel.
// The type mirrors the metadata's distribution declaration
// (Distribution.Type / Distribution.Location).
type Location struct {
	// Type is the distribution channel pattern of the resolved location;
	// the only supported value is "github-releases" (ADR-030 §3, §5).
	Type string

	// Location is the https URL of the release content on the declared
	// channel — the address the install flow resolves content from. It is
	// a distribution address, not an identity (Manifesto §3.4; ADR-030
	// §3): identity comes from the standard id and the trust fields.
	Location string
}

// Sentinel errors for location resolution. Consumers match them with
// errors.Is on the wrapped errors returned by ResolveLocation.
var (
	// ErrLocationMissing reports that the entry declares no distribution
	// location: the channel type or the location URL is absent, so the
	// release content cannot be resolved for install.
	ErrLocationMissing = errors.New("registry distribution location missing")

	// ErrLocationInvalid reports that the declared location is not usable
	// for install: the scheme is not https, or the URL is not well-formed
	// absolute with a host (the strict https-only rules of the parse
	// layer).
	ErrLocationInvalid = errors.New("registry distribution location invalid")

	// ErrLocationUnsupportedType reports that the declared channel type is
	// not a supported channel pattern; the only supported pattern is
	// "github-releases" (ADR-030 §3).
	ErrLocationUnsupportedType = errors.New("registry distribution location type unsupported")
)

// ResolveLocation resolves the content distribution location of a standard
// version from its index entry (TS-014-02-03): it returns the channel type
// and the https URL of the release content, ready for the install flow to
// fetch — or an actionable error when the entry's distribution declaration
// cannot be resolved.
//
// Failure cases, each producing an actionable error wrapped around a
// sentinel:
//
//   - the entry declares no distribution location — an empty location URL
//     or an empty channel type: wrapped ErrLocationMissing. The metadata
//     document is missing the distribution declaration the release
//     content resolves from;
//   - the entry declares an unsupported channel type: wrapped
//     ErrLocationUnsupportedType. Defensive: the parse layer rejects
//     unsupported types (parse.go), but entries can reach resolution
//     without passing through Parse. Only "github-releases" is a
//     supported pattern (ADR-030 §3, §5);
//   - the declared location is not usable for install — a scheme other
//     than https, or not a well-formed absolute https URL with a host:
//     wrapped ErrLocationInvalid. Defensive re-validation with the parse
//     layer's strict rules (parse.go reHTTPSLocation, checkHTTPSURL), so
//     an entry that skipped Parse cannot carry an unusable location.
//
// Resolution is metadata-only: no content is fetched (fetching belongs to
// the install flow, T-007) and no trust is verified (T-011). The install
// flow consumes this component inside the validated adoption path (T-012);
// this component never bypasses validation-at-adoption.
func ResolveLocation(entry Entry) (Location, error) {
	if entry.Distribution.Location == "" {
		return Location{}, fmt.Errorf(
			"%w: standard %q version %q declares no distribution location — the release content cannot be resolved for install. Fix the metadata document's distribution.location (the https URL of the release content on the standard's release channel; ADR-030 §3).",
			ErrLocationMissing, entry.ID, entry.Version)
	}
	if entry.Distribution.Type == "" {
		return Location{}, fmt.Errorf(
			"%w: standard %q version %q declares no distribution channel type — the release content cannot be resolved for install. Fix the metadata document's distribution.type (the only supported channel pattern is %q; ADR-030 §3).",
			ErrLocationMissing, entry.ID, entry.Version, DistributionTypeGitHubReleases)
	}
	if entry.Distribution.Type != DistributionTypeGitHubReleases {
		return Location{}, fmt.Errorf(
			"%w: standard %q version %q declares distribution channel type %q, which is not a supported channel pattern — the only supported pattern is %q (ADR-030 §3, §5). Fix the metadata document's distribution.type, or evolve the schema for the new channel pattern.",
			ErrLocationUnsupportedType, entry.ID, entry.Version, entry.Distribution.Type, DistributionTypeGitHubReleases)
	}

	location := entry.Distribution.Location
	if !reHTTPSLocation.MatchString(location) {
		return Location{}, fmt.Errorf(
			"%w: standard %q version %q declares distribution location %q whose scheme is not https — release content is resolved over TLS only, no plaintext or other scheme (ADR-030 §3). Fix the metadata document's distribution.location.",
			ErrLocationInvalid, entry.ID, entry.Version, location)
	}
	// Re-validate the URL shape with the parse layer's strict format check
	// (parse.go checkHTTPSURL): a location that is not a well-formed
	// absolute https URL with a host is unusable for install. The check
	// reports every problem into errs; the first one is the most precise
	// reason to surface.
	var errs []ValidationError
	checkHTTPSURL(location, "distribution.location", &errs)
	if len(errs) > 0 {
		return Location{}, fmt.Errorf(
			"%w: standard %q version %q declares distribution location %q: %s. Fix the metadata document's distribution.location.",
			ErrLocationInvalid, entry.ID, entry.Version, location, errs[0].Message)
	}

	return Location{
		Type:     entry.Distribution.Type,
		Location: entry.Distribution.Location,
	}, nil
}
