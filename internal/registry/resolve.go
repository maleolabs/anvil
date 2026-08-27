// Framework-standard resolution for initialization (TS-015-02-01).
//
// Per ADR-026 decision 2, framework-declared initialization resolves the
// delivery lifecycle standard through the registry (A3, A4): the declared
// framework name is matched against installed delivery lifecycle standards
// recorded by EPIC-014 (TS-014-03-03) — never through runtime knowledge.
// There is no runtime-side framework catalog to fall back on (ST-015-01);
// the installed-standard record is the authoritative local record of what
// is installed (identity, pinned version, declared contract version, the
// explicit resolution used — ADR-022 §3).
//
// The matching rule is the standard identity convention of ADR-021 §3.1
// (first-party standards are named anvil-standard-*): a declared framework
// "laravel" resolves to the installed standard record "anvil-standard-
// laravel". The rule is explicit and documented — it is contract
// knowledge, not runtime framework knowledge.
//
// No-match semantics (TS-015-02-02): a declared framework with no
// installed standard record is a distinguishable no-match outcome
// (ErrStandardNotInstalled) that hands off to the standard-missing
// failure semantics — the hard-fail behavior and its actionable
// remediation are implemented by TS-015-02-02, not here. This component
// only makes the no-match explicit; it never degrades silently and never
// invents a resolution.
//
// Reference: TS-015-02-01, ADR-026 decision 2, ADR-021 §3.1, ADR-022 §3,
// ADR-023 §3
package registry

import (
	"errors"
	"fmt"
)

// StandardIDPrefix is the identity prefix of first-party delivery
// lifecycle standards (ADR-021 §3.1): standards are named
// anvil-standard-<framework>, one per framework. The declared framework
// name maps to the standard id by this convention — the explicit,
// documented matching rule of framework-declared initialization
// (TS-015-02-01).
const StandardIDPrefix = "anvil-standard-"

// ErrStandardNotInstalled reports that the declared framework's delivery
// lifecycle standard has no installed-standard record: a no-match
// resolution. It is the hand-off signal for the standard-missing failure
// semantics (TS-015-02-02) — the caller decides how the no-match is
// surfaced; this component only makes the outcome explicit and
// distinguishable from store failures.
var ErrStandardNotInstalled = errors.New("delivery lifecycle standard not installed")

// StandardIDForFramework returns the delivery lifecycle standard id for a
// declared framework name, following the standard identity convention
// (ADR-021 §3.1): "laravel" → "anvil-standard-laravel". The mapping is
// explicit and deterministic — framework-declared initialization never
// consults runtime knowledge (ADR-026 decision 2).
func StandardIDForFramework(framework string) string {
	return StandardIDPrefix + framework
}

// ResolveFrameworkStandard resolves a declared framework name against the
// installed-standard record store: the standard id follows the identity
// convention (StandardIDForFramework) and is looked up in the recorded
// installed state (TS-014-03-03). Resolution is explicit and recorded:
// the returned record is the authoritative installed-standard record —
// identity, pinned version, declared contract version, and the explicit
// resolution recorded at install (ADR-022 §3) — and the outcome is fully
// determined by the store, never by runtime framework knowledge.
//
// Outcomes:
//
//   - a record exists: the installed record is returned — the declared
//     framework resolves to the installed standard;
//   - no record exists: no-match — wrapped ErrStandardNotInstalled, the
//     hand-off to the standard-missing semantics of TS-015-02-02;
//   - the store cannot answer: store failures pass through unwrapped in
//     intent — a corrupt record file (wrapped ErrRecordCorrupt), an
//     unreadable store (wrapped ErrStoreUnreadable), or an unsafe
//     standard id derived from the framework name (wrapped
//     ErrRecordInvalid — the framework name is not a valid standard
//     name, e.g. contains dots or slashes).
func (s *InstalledStandardStore) ResolveFrameworkStandard(framework string) (InstalledStandardRecord, error) {
	id := StandardIDForFramework(framework)
	rec, err := s.Get(id)
	if err != nil {
		if errors.Is(err, ErrRecordNotFound) {
			return InstalledStandardRecord{}, fmt.Errorf(
				"%w: %s: no delivery lifecycle standard is installed for framework %q — install the standard with 'anvil standard install %s <version>' (standard-missing handling: TS-015-02-02)",
				ErrStandardNotInstalled, id, framework, id)
		}
		return InstalledStandardRecord{}, err
	}
	return rec, nil
}
