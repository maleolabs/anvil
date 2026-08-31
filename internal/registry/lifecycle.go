package registry

import "strings"

// This file implements the client-side lifecycle state behavior of the
// registry (TS-014-01-03): helpers that translate the governed lifecycle
// states of the metadata format (published, deprecated, retired — ADR-023
// §3; ADR-027 §3) into availability decisions for discovery (T-005),
// install (T-007), and update (T-008) flows.
//
// Scope boundary: these helpers consume already-parsed metadata structs
// (Metadata.Lifecycle); parsing and validation of metadata documents is
// the registry client's responsibility (TS-014-01-02). The helpers are
// total — they never fail and never return errors — and guard unknown
// state values defensively via the lifecycle constants: a state string
// that is not one of the three machine values is treated as not
// adoptable, not updatable, and not deprecated. Rejection of unknown
// states at the document level is the parse layer's job (TS-014-01-02);
// the constant-based guards here are defense in depth, not validation.
//
// Reference: TS-014-01-03, ADR-023 §3, ADR-027 §3, PM decision D-03

// LifecycleAdoptable reports whether a release in the given lifecycle
// state is resolvable for fresh adoption (discovery and install).
//
// Published standards are discoverable and installable (ADR-027 §3).
// Deprecated standards remain installable — but with a warning surfaced
// via LifecycleWarning (ADR-023 §3; ADR-027 §3). Retired standards are
// removed from the registry and are not resolvable for fresh adoption
// (ADR-027 §3); existing adoptions follow the documented migration path
// and are out of this helper's scope. Any state string other than the
// three machine values is not adoptable (defensive guard; the parse
// layer rejects unknown states).
//
// The decision ignores calendar dates: an already-passed removal date
// does not change the outcome for a deprecated state — enforcement of
// dates (removing a release from the registry once its announced removal
// date passes) is the registry's retirement responsibility, not this
// helper's.
//
// Consumed by discovery (TS-014-02-02) and install (TS-014-03-01).
func LifecycleAdoptable(state string) bool {
	return state == LifecycleStatePublished || state == LifecycleStateDeprecated
}

// LifecycleUpdateAllowed reports whether a release in the given lifecycle
// state may receive updates.
//
// Only published standards receive updates: deprecated standards receive
// no updates and carry an announced removal date (ADR-023 §3; ADR-027
// §3), and retired standards are removed from the registry. Any state
// string other than the three machine values is not updatable (defensive
// guard; the parse layer rejects unknown states).
//
// Consumed by the update flow (TS-014-03-02); this helper defines the
// reusable rule, it does not implement the update flow itself.
func LifecycleUpdateAllowed(state string) bool {
	return state == LifecycleStatePublished
}

// LifecycleWarning returns the advisory warning to surface when a
// deprecated release is adopted, and whether the release is deprecated.
// The warning is a rendered string ready to surface to the user;
// structured data (Lifecycle.State and Lifecycle.RemovalDate) remains
// available on the struct for callers that need the raw values.
//
// A deprecated standard installs with warning (ADR-023 §3; ADR-027 §3):
// the warning states the deprecation, notes that the release receives no
// updates, and carries the announced removal date. The removal date is
// optional per PM decision D-03 (SHOULD be present once announced): when
// it is absent — or whitespace-only — the warning still surfaces, with a
// "no removal date announced" note. A release that is not deprecated
// yields no warning (ok = false); this includes published, retired, and
// unknown state strings (defensive guard; the parse layer rejects unknown
// states).
//
// Consumed by discovery (TS-014-02-02) and install (TS-014-03-01) to
// surface deprecation warnings at discovery and install.
func LifecycleWarning(l Lifecycle) (warning string, ok bool) {
	if l.State != LifecycleStateDeprecated {
		return "", false
	}
	removalDate := strings.TrimSpace(l.RemovalDate)
	if removalDate == "" {
		return "this standard release is deprecated: no removal date announced; it will receive no updates", true
	}
	return "this standard release is deprecated: removal announced for " + removalDate + "; it will receive no updates", true
}
