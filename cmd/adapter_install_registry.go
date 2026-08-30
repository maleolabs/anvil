// Package cmd implements the Anvil CLI commands.
//
// ── Registry-Side Adapter Install (TS-016-04-01) ─────────────────────
//
// "anvil adapter install <name>" resolves the adapter through the
// registry (ADR-025 §3.5, §4.7): the standard identity follows the
// identity convention "anvil-standard-<name>" (ADR-021 §3.1), the
// release is resolved from the static registry index, and the adoption
// runs through the EXACT gates of "anvil standard install"
// (TS-014-03-01) — strict parse, lifecycle + compatibility, trust
// anchors BEFORE the fetch, release content fetched under the ADR-030
// policy, and VerifyTrust (integrity + attestation + anchor, ADR-022)
// after it. Only then is the adapter binary installed from the SAME
// release channel (the standard repository's release, version-pinned),
// checksum-verified against that release's SHA256SUMS.txt.
//
// This file holds the registry-side resolution, validation, and
// recording helpers; the command wiring lives in cmd/adapter_install.go.
//
// Reference: TS-016-04-01, TS-014-03-01, TS-014-04-03, ADR-022 §3,
// ADR-023 §3, ADR-025 §3.5, ADR-030
package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/registry"
)

// adapterStandardIDForName returns the delivery lifecycle standard id an
// adapter name resolves to, following the identity convention (ADR-021
// §3.1): "laravel" → "anvil-standard-laravel".
func adapterStandardIDForName(name string) string {
	return registry.StandardIDForFramework(name)
}

// adapterRegistryAdoption is the outcome of the registry-side resolution
// and validation of one adapter install: the adopted standard identity
// and version (pinned), the explicit resolution source (the actual
// endpoint the release content was fetched from), the pre-fetch and
// combined adoption records (TS-014-04-03 — persisted for auditability),
// the parse warnings to surface, and the record path.
type adapterRegistryAdoption struct {
	id              string
	version         string
	contentSource   string
	parseWarnings   []registry.Warning
	adoption        registry.AdoptionResult
	alreadyRecorded bool
	recordPath      string
}

// resolveAdapterStandardForInstall resolves the standard release an
// "anvil adapter install" adopts, running the pre-fetch adoption gates:
//
//  1. standard id by the identity convention ("anvil-standard-<name>");
//  2. the static registry index (--index → ANVIL_REGISTRY_INDEX →
//     default);
//  3. the version: the RECORDED version when the standard is already
//     installed (the adapter binary is pinned to the installed
//     standard — changing the version is an update, an explicit
//     adoption event of TS-014-03-02), else the highest adoptable
//     version offered in the index;
//  4. the strict registry parse of the resolved document (TS-014-01-02)
//     and the pre-fetch adoption validation (lifecycle gate →
//     compatibility — TS-014-04-03) against the runtime's supported
//     contract majors (compatibility matrix) and the project framework
//     version — the same gates "anvil standard install" runs, so the
//     adapter path can never adopt what the standard path would
//     reject.
//
// The content is NOT fetched here: trust anchors resolution and the
// content fetch/verification are the caller's next gate
// (verifyAdapterStandardAdoption).
//
// Reference: TS-016-04-01, TS-014-03-01, TS-014-04-03
func resolveAdapterStandardForInstall(cmd *cobra.Command, name string) (registry.Entry, *registry.Metadata, []registry.Warning, registry.AdoptionResult, error) {
	id := adapterStandardIDForName(name)

	// 1. The static index (the same resolution order as the standard
	// commands: --index → ANVIL_REGISTRY_INDEX → default).
	ix, err := loadStandardIndex(cmd)
	if err != nil {
		return registry.Entry{}, nil, nil, registry.AdoptionResult{}, err
	}
	if err != nil {
		return registry.Entry{}, nil, nil, registry.AdoptionResult{}, err
	}

	// 2. Version resolution: the recorded version pins the adapter
	// binary to the installed standard; without a record, the highest
	// adoptable version offered in the index is adopted.
	version, err := adapterStandardVersionForInstall(ix, id)
	if err != nil {
		return registry.Entry{}, nil, nil, registry.AdoptionResult{}, err
	}

	// 3. The resolved index entry: exact id + version pin (ADR-022 §3).
	entry, err := ix.Resolve(id, version)
	if err != nil {
		return registry.Entry{}, nil, nil, registry.AdoptionResult{}, err
	}

	// 4. Strict parse (TS-014-01-02): the raw document is re-read from
	// the entry's source and validated against the registry metadata
	// schema — the structural decode alone is never trusted.
	md, warnings, err := parseStandardEntry(entry)
	if err != nil {
		return registry.Entry{}, nil, nil, registry.AdoptionResult{}, err
	}

	// 5. Pre-fetch adoption validation (TS-014-04-03): the lifecycle
	// gate runs BEFORE compatibility; a failure aborts before any
	// content is fetched.
	supportedContractMajors, err := supportedContractMajors()
	if err != nil {
		return registry.Entry{}, nil, nil, registry.AdoptionResult{}, err
	}
	projectFrameworkVersion, err := projectFrameworkVersionForInstall()
	if err != nil {
		return registry.Entry{}, nil, nil, registry.AdoptionResult{}, err
	}
	before := registry.ValidateAdoptionBeforeFetch(*md, supportedContractMajors, projectFrameworkVersion)
	if !before.Valid {
		return registry.Entry{}, nil, nil, registry.AdoptionResult{}, adoptionBeforeFetchFailure(id, version, before)
	}

	return entry, md, warnings, before, nil
}

// adapterStandardVersionForInstall resolves the standard version an
// adapter install adopts:
//
//   - the RECORDED version when the standard is already installed — the
//     adapter binary is pinned to the installed standard (ADR-022 §3:
//     adoptions pin explicit versions; changing the version is an
//     update, an explicit adoption event of TS-014-03-02);
//   - otherwise the highest ADOPTABLE version offered in the index
//     (published or deprecated per LifecycleAdoptable — TS-014-01-03);
//     entries that fail strict registry validation are not offered for
//     adoption and are skipped, mirroring "anvil standard list".
//
// A corrupt installed-standard record is an actionable error: the
// standard must be re-adopted explicitly before its adapter can be
// installed (recovery by re-adoption, TS-014-03-03).
func adapterStandardVersionForInstall(ix *registry.Index, id string) (string, error) {
	storeDir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		return "", fmt.Errorf("resolve the installed-standards directory: %w", err)
	}
	store := registry.NewInstalledStandardStore(storeDir)
	rec, err := store.Get(id)
	switch {
	case err == nil:
		return rec.Version, nil
	case errors.Is(err, registry.ErrRecordNotFound):
		// No record: fall through to the index resolution.
	case errors.Is(err, registry.ErrRecordCorrupt):
		return "", fmt.Errorf(
			"the installed-standard record for %q is corrupt: %v — re-adopt the standard with 'anvil standard install %s <version>' before installing its adapter",
			id, err, id)
	default:
		return "", fmt.Errorf("could not read the installed-standard record for %q: %w", id, err)
	}

	versions := ix.Versions(id)
	if len(versions) == 0 {
		return "", fmt.Errorf(
			"adapter %q is not offered for adoption: standard %q is not in the registry index — run 'anvil standard list' to see the standards offered for adoption",
			strings.TrimPrefix(id, registry.StandardIDPrefix), id)
	}
	// The highest adoptable version — the shared selection rule of the
	// registry-based discovery surfaces (highestAdoptableVersion): valid,
	// adoptable releases only, ordered semantically.
	if version := highestAdoptableVersion(ix, id); version != "" {
		return version, nil
	}
	return "", fmt.Errorf(
		"adapter %q is not offered for adoption: no adoptable release of standard %q is in the index (available versions: %s)",
		strings.TrimPrefix(id, registry.StandardIDPrefix), id, strings.Join(versions, ", "))
}

// adoptionBeforeFetchFailure renders the lifecycle/compatibility
// rejection of the pre-fetch adoption phase as an actionable error,
// distinguishing the lifecycle gate (retired / unknown state — not
// offered for adoption) from a compatibility failure.
func adoptionBeforeFetchFailure(id, version string, before registry.AdoptionResult) error {
	if !before.Adoptable {
		return fmt.Errorf(
			"standard %q version %q is not offered for adoption: %s — run 'anvil standard list' to see the standards offered for adoption, or choose another standard",
			id, version, strings.Join(before.Errors, "; "))
	}
	return fmt.Errorf(
		"standard %q version %q is not compatible: %s — if you are the publisher, resolve the compatibility problems listed above; otherwise choose another version or report the standard to its publisher",
		id, version, strings.Join(before.Errors, "; "))
}

// verifyAdapterStandardAdoption completes the adoption validation of an
// adapter install: trust anchors are resolved and loaded BEFORE the
// fetch (missing anchors fail fast — no download is wasted), the release
// content is fetched from the resolved distribution location under the
// ADR-030 policy (https-only, bounded redirects, size cap, shared
// timeout), and the post-fetch adoption phase completes the pre-fetch
// result with VerifyTrust — integrity, publisher attestation, and the
// out-of-band trust anchor allowlist (ADR-022 §3). It returns the
// combined adoption record, the ACTUAL endpoint used (the final response
// URL after any allowed redirects — the explicit resolution, ADR-022
// §3), and the loaded anchors (recorded for auditability).
//
// Reference: TS-016-04-01, TS-014-03-01, TS-014-04-03
func verifyAdapterStandardAdoption(cmd *cobra.Command, md registry.Metadata, before registry.AdoptionResult) (registry.AdoptionResult, string, *registry.TrustAnchors, error) {
	// Trust anchors BEFORE the fetch (reviewer finding 2; the same
	// order as "anvil standard install"): resolving and loading the
	// allowlist is a local operation, so a missing or corrupt anchors
	// file fails fast without wasting a download.
	anchorsPath, err := standardTrustAnchorsPath(cmd)
	if err != nil {
		return registry.AdoptionResult{}, "", nil, fmt.Errorf("could not resolve the trust anchors path: %w", err)
	}
	anchors, err := loadTrustAnchorsConfigured(anchorsPath)
	if err != nil {
		if errors.Is(err, registry.ErrTrustAnchorsNotFound) {
			return registry.AdoptionResult{}, "", nil, fmt.Errorf(
				"no trust anchors file found at %s: the publisher %q must be anchored in the operator's out-of-band allowlist before any standard is adopted (ADR-022 §3 — no first-use acceptance). Configure the trust anchors file, or point the install at one with --trust-anchors <path> or the %s environment variable",
				anchorsPath, md.ID, registry.EnvTrustAnchors)
		}
		return registry.AdoptionResult{}, "", nil, fmt.Errorf("could not load the trust anchors file at %s: %w", anchorsPath, err)
	}

	// The content location resolves from the metadata (TS-014-02-03)
	// with defensive https re-validation.
	location, err := registry.ResolveLocation(registry.Entry{Metadata: md})
	if err != nil {
		return registry.AdoptionResult{}, "", nil, fmt.Errorf("the release content of standard %q version %q cannot be resolved: %w", md.ID, md.Version, err)
	}

	// Fetch the release content (ADR-030): https-only, bounded
	// redirects, size cap during download, shared timeout.
	content, contentSource, err := fetchStandardContent(location.Location)
	if err != nil {
		return registry.AdoptionResult{}, "", nil, err
	}

	// Post-fetch adoption phase: trust verification (TS-014-04-02 /
	// ADR-022) over the fetched content — the ONLY gate; no
	// skip/insecure/no-verify flag exists.
	adoption := registry.ValidateAdoptionAfterFetch(md, content, anchors, before)
	if !adoption.Valid {
		return registry.AdoptionResult{}, "", nil, fmt.Errorf(
			"trust verification failed for standard %q version %q: %s — do not adopt content that fails verification; if you are the publisher, resolve the trust problems listed above; otherwise choose another version or report the standard to its publisher",
			md.ID, md.Version, strings.Join(adoption.Errors, "; "))
	}
	return adoption, contentSource, anchors, nil
}

// recordAdapterStandardAdoption persists the installed-standard record
// of the adapter install's adoption — the single record-persistence path
// shared with "anvil standard install" (persistStandardInstallRecord,
// TS-014-03-03): same identity-plus-version idempotency, same
// version-change rejection, same corrupt-record recovery. The record
// carries the ACTUAL endpoint the release content was fetched from as
// the explicit resolution, and the embedded compatibility and trust
// results from the combined adoption record. The result is returned so
// the adapter install report can surface the recorded standard.
func recordAdapterStandardAdoption(cmd *cobra.Command, md registry.Metadata, contentSource string, adoption registry.AdoptionResult, parseWarnings []registry.Warning) (standardInstallResult, error) {
	rec := registry.InstalledStandardRecord{
		FormatVersion:   registry.RecordFormatVersion,
		ID:              md.ID,
		Version:         md.Version,
		ContractVersion: md.ContractVersion,
		Resolution: registry.Resolution{
			Kind:   registry.ResolutionKindDistribution,
			Source: contentSource,
		},
		Lifecycle:     md.Lifecycle,
		Compatibility: adoption.CompatibilityRecord(),
		Trust:         adoption.TrustRecord(),
	}
	return persistStandardInstallRecord(cmd, rec, adoption, parseWarnings)
}
