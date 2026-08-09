// Package cmd implements the Anvil CLI commands.
//
// ── Standard Install Bundle (TS-014-05-02) ───────────────────────────
//
// "anvil standard install-bundle <bundle-path>" is the offline/bundled
// installation flow of a delivery lifecycle standard release (ADR-023
// §3): the user invokes it deliberately, pointing at the bundled install
// material (TS-014-05-01) that carries the release content and its
// metadata document — with its verification material — in one archive.
// The command validates the bundle, verifies integrity and attestation
// against the operator's trust anchor allowlist, records the installed
// version, and remains idempotent by standard identity plus version.
//
// No network access at any point. The flow consumes exactly three local
// inputs — the bundle file, the trust anchors file, and the
// installed-standard record store (plus the local compatibility matrix
// and the project config, exactly like the online install) — and never
// constructs an HTTP client or resolves the bundled metadata's
// distribution.location: the bundled content bytes ARE the release
// content, so the fetch phase of the online flow is replaced by bundle
// consumption (ADR-023 §3). The bundled metadata document still declares
// a distribution location (the strict parse requires the field), but the
// offline flow never fetches from it.
//
// Offline validation path — equivalent to the online path (PM binding
// decision 3; ADR-023 §3: offline/bundled installs follow the same
// validation semantics). Nothing the online install enforces is bypassed:
//
//  1. OpenBundle (TS-014-05-01): the archive structure (pinned layout,
//     no extended headers, single gzip member), the bundle checksum
//     (checked before the bounded drain), and the strict metadata parse
//     (registry.Parse — the same parse used online) of the bundled
//     document, which REQUIRES the verification material
//     (trust.contentDigests, trust.attestation.signature,
//     trust.attestation.publicKey — ADR-022 §3). A bundle that fails
//     here is rejected with its failure class attributed distinctly:
//     structure ("not a valid bundle archive"), integrity ("bundle
//     corrupt — obtain a fresh copy"), or metadata ("bundled metadata
//     document invalid"), each with an actionable resolution;
//  2. The runtime's supported contract majors are read from the
//     compatibility matrix (LoadCompatibilityMatrix — the corpus
//     reference, ADR-029 §3); an unreadable matrix aborts with an
//     actionable error — supported majors are never silently defaulted;
//  3. The project framework version gate (PM binding decision 3): the
//     project's framework version is read from the project config
//     (anvil.yaml — framework.<name>.version), reused from the online
//     install; declared-but-undeterminable REJECTS the install, while
//     framework-free projects proceed shape-only, recorded explicitly
//     (ADR-026);
//  4. Adoption validation, pre-fetch phase (ValidateAdoptionBeforeFetch,
//     TS-014-04-03) on the BUNDLED metadata — the "before fetch" phase
//     applies to the bundled metadata; the fetch phase is replaced by
//     bundle consumption. The lifecycle gate runs before compatibility:
//     retired releases are not offered for fresh adoption ("not
//     offered" — ADR-027 §3), deprecated releases remain adoptable with
//     a warning (LifecycleWarning), and compatibility runs
//     (ValidateCompatibility) against the bundled contractVersion and
//     capability declaration;
//  5. Trust anchors BEFORE verification (reviewer finding: resolving and
//     loading the allowlist is a local operation, so a missing or
//     corrupt anchors file fails fast). Anchors resolve via the
//     documented order (--trust-anchors flag → ANVIL_TRUST_ANCHORS →
//     default <user config dir>/anvil/trust-anchors.json) and
//     default-fail when none are configured — the anchors come from the
//     OPERATOR, never from the bundle (PM decision D-07; the bundle
//     never carries anchors);
//  6. Bundle.Verify (TS-014-05-01): the exact same trust engine online
//     installs use (VerifyTrust — integrity of the bundled content
//     against every declared digest, the publisher attestation, and the
//     out-of-band trust anchor match) with the operator-supplied
//     anchors. A result with Valid == false ABORTS the install (the
//     documented contract: no bundle-specific verification path, no
//     TOFU, no privileged path). Failure reasons are attributed per
//     dimension and displayed separately: content digest mismatch,
//     attestation signature failure, or anchor mismatch;
//  7. State recording (InstalledStandardStore, TS-014-03-03): the pinned
//     version, declared contract version, explicit resolution
//     (kind "bundle", source = the bundle path), lifecycle state, and
//     the embedded compatibility and trust results are recorded — the
//     same idempotency semantics as the online install: re-installing
//     the same identity plus version re-validates from the bundle,
//     refreshes the record via Update, and reports "already installed at
//     <version> (re-validated)"; a different version already installed
//     is rejected with an actionable error referencing the update flow
//     (TS-014-03-02) — there is no offline update.
//
// Failure attribution (PM binding decision 4; TS-014-05-01 security
// notes). The three failure classes are surfaced distinctly:
//
//   - OpenBundle structure failure → "not a valid bundle archive"
//     (wrong layout, corrupt stream, trailing data) — obtain a fresh
//     copy of the bundle;
//   - OpenBundle integrity failure → "bundle corrupt or modified after
//     creation" (bundle.sha256 mismatch) — obtain a fresh copy of the
//     bundle;
//   - OpenBundle metadata failure → "bundled metadata document invalid"
//     (missing/unparseable document, or no verification material — the
//     strict parse rejects the document with the field-level problems) —
//     fix the document or obtain a fresh bundle;
//   - Bundle.Verify failure → trust verification failed with the
//     per-dimension reasons (content digest mismatch / attestation /
//     anchor) displayed separately.
//
// The bundle content is extracted in memory (OpenBundle) and never
// written to disk by entry name: nothing is materialized before every
// gate passes, and the only persisted state is the installed-standard
// record (the content materialization flow is downstream scope,
// EPIC-015).
//
// Reference: TS-014-05-02, TS-014-05-01, ADR-022 §3, ADR-023 §3,
// ADR-026, ADR-027 §3, ADR-029 §3, PRD-002 §5.7, §7.2
package cmd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/registry"
)

// standardInstallBundleCmd represents the "anvil standard install-bundle"
// command that explicitly installs one standard release from bundled
// install material, with no network access.
//
// Reference: TS-014-05-02, ADR-023 §3
var standardInstallBundleCmd = &cobra.Command{
	Use:   "install-bundle <bundle-path>",
	Short: "Install a standard release from bundled material (offline)",
	Long: `Install one delivery lifecycle standard release from bundled
install material (TS-014-05-02, ADR-023) — the offline installation
flow: no network access is used at any point.

Installation is explicit: it happens only when you run this command. The
command consumes the bundle archive (a single tar.gz carrying the release
content, the release's metadata document with its verification material,
and the bundle checksum — TS-014-05-01), validates the bundled metadata
with the same strict parse used online, verifies integrity and publisher
attestation against the operator's trust anchor allowlist (ADR-022 —
there is no skip or insecure path), and records the installed version
with its pinned resolution (kind "bundle", source = the bundle path).

The offline validation path is equivalent to the online path:
compatibility (declared contract version and capability) and trust run
before the install completes, and the trust anchors come from the
operator (--trust-anchors, ANVIL_TRUST_ANCHORS, or the default
allowlist) — never from the bundle. Re-installing the same identity and
version is idempotent — the full validation still runs (re-validated
from the bundle) and the record is refreshed, including its recorded
resolution source: re-installing from a different bundle path updates
the record to that path (last adoption wins). Installing a different
version of an already-installed standard is rejected: version change is
an update, an explicit adoption event of the update flow
(TS-014-03-02) — there is no offline update. Deprecated releases install
with a warning; retired releases are not offered for fresh adoption.

A bundle whose verification material is missing or invalid — a corrupt
bundle, an invalid bundled metadata document, or a trust verification
mismatch — fails the install with an actionable error naming the failure
class: the bundle is not a valid bundle archive, the bundle is corrupt
or was modified after creation, the bundled metadata document is
invalid, or the content/attestation/anchor verification failed.

Output formats:
  Default      sectioned install report (identity, resolution, lifecycle,
               record path, validation summary, warnings) — identical to
               "anvil standard install"
  --json       standard TS-P8-05 envelope on stdout, data:
               {id, version, contract_version, resolution, lifecycle,
               installed_at, updated_at, already_installed, warnings,
               compatibility, trust, record_path}

Trust anchors resolution order (anchors from the OPERATOR, never from
the bundle):
  1. --trust-anchors <path>
  2. the ANVIL_TRUST_ANCHORS environment variable
  3. the default <user config dir>/anvil/trust-anchors.json
     (e.g. ~/.config/anvil/trust-anchors.json on Linux)

Compatibility matrix resolution order (the runtime's supported contract
majors are read from the corpus matrix record at runtime — never
hardcoded):
  1. the ANVIL_COMPATIBILITY_MATRIX environment variable
  2. the default docs/specification-corpus/compatibility-matrix.json
     relative to the working directory (running from the repository
     root needs no configuration); an unreadable matrix aborts the
     install with an actionable error — supported majors are never
     silently defaulted.

Exit codes: 0 on success; 3 when the bundle file is not found; 1 for
other errors (invalid bundle archive, corrupt bundle, invalid bundled
metadata, retired release, compatibility or trust failure, missing or
invalid trust anchors, version conflict, project framework version
undeterminable).

Examples:
  anvil standard install-bundle ./anvil-standard-laravel-1.2.3.bundle.tar.gz
  anvil standard install-bundle ./anvil-standard-laravel-1.2.3.bundle.tar.gz --trust-anchors ./anchors.json
  anvil standard install-bundle ./anvil-standard-laravel-1.2.3.bundle.tar.gz --json`,
	Args:         RangeArgsWithUsage(1, 1, "anvil standard install-bundle ./anvil-standard-laravel-1.2.3.bundle.tar.gz", "bundle-path"),
	SilenceUsage: true,
	RunE:         runStandardInstallBundle,
}

func init() {
	AddJSONFlag(standardInstallBundleCmd)
	standardInstallBundleCmd.Flags().String("trust-anchors", "", "path to the trust anchors allowlist file (default: $ANVIL_TRUST_ANCHORS, else <user config dir>/anvil/trust-anchors.json)")
}

// standardBundleMaxFileBytes caps the on-disk size of a bundle archive.
// A valid bundle decompresses to at most MaxBundleContentSize (1 GiB)
// plus the metadata and checksum entries and their tar headers — and
// compression never expands incompressible data by more than a small
// fraction — so an archive beyond this cap is a broken or hostile
// artifact, not a valid bundle. The cap bounds the file read so a
// hostile archive is rejected before OpenBundle's entry caps are
// reached, instead of buffering an unbounded file into memory. It is a
// variable so tests can shrink the cap; the production value is fixed
// well above any legitimate bundle.
var standardBundleMaxFileBytes = int64(registry.MaxBundleContentSize + (4 << 20))

// ── Run ──────────────────────────────────────────────────────────────

// runStandardInstallBundle executes the offline install command through
// the documented adoption sequence (file header): read bundle →
// OpenBundle (structure → checksum → strict parse of the bundled
// metadata) → supported contract majors (compatibility matrix) → project
// framework version → adoption validation pre-fetch phase on the bundled
// metadata (lifecycle gate → compatibility) → trust anchors → Bundle.Verify
// → record. A failure at any gate aborts the install with an actionable
// error and no state is written; nothing is ever fetched over the
// network.
//
// Reference: TS-014-05-02 (PM binding decisions 3, 4), TS-014-04,
// ADR-022 §3, ADR-023 §3
func runStandardInstallBundle(cmd *cobra.Command, args []string) error {
	bundlePath := args[0]

	// 1. Read the bundle file from disk, bounded: the flow's only
	// content input besides the anchors and the record store. A missing
	// bundle file is a not-found failure (exit 3); an unreadable or
	// oversized file is a general error.
	data, err := readStandardBundleFile(bundlePath)
	if err != nil {
		return reportStandardBundleFileError(cmd, bundlePath, err)
	}

	// 2. OpenBundle (TS-014-05-01): structure → checksum → strict parse
	// of the bundled metadata. The verification material is REQUIRED by
	// the parse (ADR-022 §3), so a bundle whose metadata lacks it is
	// rejected here with the metadata failure class. The failure is
	// attributed distinctly: structure / integrity ("bundle corrupt") /
	// metadata ("bundled metadata document invalid").
	bundle, err := registry.OpenBundle(data)
	if err != nil {
		return reportStandardBundleOpenError(cmd, bundlePath, err)
	}

	// 3. Supported contract majors from the compatibility matrix
	// (T-010 reviewer finding G4 — must-fix; reused from the online
	// install): the runtime's supported contract major set is READ from
	// the corpus matrix record at runtime (ADR-029 §3), never
	// hardcoded. An unreadable matrix aborts with an actionable error —
	// supported majors are never silently defaulted (PM binding
	// decision 3).
	supportedContractMajors, err := supportedContractMajors()
	if err != nil {
		return ReportError(cmd, &output.AppError{
			Message:    "could not load the compatibility matrix",
			Reason:     err.Error(),
			Resolution: "Set the " + registry.EnvCompatibilityMatrix + " environment variable to the corpus matrix file (docs/specification-corpus/compatibility-matrix.json), or run the install from the repository root",
			Err:        err,
		})
	}

	// 4. Project framework version gate (PM binding decision 3; reused
	// from the online install): declared-but-undeterminable REJECTS the
	// install (never assumed, Transition Plan A2); framework-free
	// projects proceed shape-only, recorded explicitly (ADR-026).
	projectFrameworkVersion, err := projectFrameworkVersionForInstall()
	if err != nil {
		return ReportError(cmd, &output.AppError{
			Message:    "could not determine the project's framework version",
			Reason:     err.Error(),
			Resolution: "Declare the framework version in anvil.yaml (framework.<name>.version), or install in a framework-free project (shape-only validation)",
			Err:        err,
		})
	}

	// 5. Adoption validation, pre-fetch phase (TS-014-04-03) on the
	// BUNDLED metadata: the "before fetch" phase applies to the bundled
	// metadata, and the fetch phase is replaced by bundle consumption.
	// Inside the component the lifecycle gate runs BEFORE the
	// compatibility validation — a failure aborts before any
	// verification work (the offline analog of "a compatibility failure
	// means zero fetches").
	before := registry.ValidateAdoptionBeforeFetch(bundle.Metadata, supportedContractMajors, projectFrameworkVersion)
	if !before.Valid {
		if !before.Adoptable {
			// The lifecycle gate failed (retired, or an unknown state):
			// the release is not offered for fresh adoption. The
			// component's message distinguishes retired from not-found.
			return ReportError(cmd, &output.AppError{
				Message:    fmt.Sprintf("standard %q version %q is not offered for adoption", bundle.Metadata.ID, bundle.Metadata.Version),
				Reason:     strings.Join(before.Errors, "\n"),
				Resolution: "Run 'anvil standard list' to see the standards offered for adoption, or choose another standard",
			})
		}
		// Compatibility failed: the adoption aborts before the bundle's
		// content is verified (the pinned adoption order).
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("standard %q version %q is not compatible", bundle.Metadata.ID, bundle.Metadata.Version),
			Reason:     strings.Join(before.Errors, "\n"),
			Resolution: "If you are the publisher, resolve the compatibility problems listed above; otherwise choose another version or report the standard to its publisher",
		})
	}

	// 6. Trust anchors BEFORE verification (reviewer finding): resolving
	// and loading the allowlist is a local operation, so a missing or
	// corrupt anchors file fails fast instead of wasting the
	// verification work. Anchors resolve via the documented order (flag
	// → env → default) and default-fail when none are configured — the
	// bundle never carries anchors (PM decision D-07; anchors.go
	// contract).
	anchorsPath, err := standardTrustAnchorsPath(cmd)
	if err != nil {
		return ReportError(cmd, &output.AppError{
			Message:    "could not resolve the trust anchors path",
			Reason:     err.Error(),
			Resolution: "Configure the trust anchors file, or point the install at one with --trust-anchors <path> or the " + registry.EnvTrustAnchors + " environment variable",
			Err:        err,
		})
	}
	anchors, err := registry.LoadTrustAnchors(anchorsPath)
	if err != nil {
		if errors.Is(err, registry.ErrTrustAnchorsNotFound) {
			return ReportError(cmd, &output.AppError{
				Message:    "no trust anchors file found",
				Reason:     err.Error(),
				Resolution: "Configure the publisher's public key at " + anchorsPath + ", or point the install at a different allowlist with --trust-anchors <path> or the " + registry.EnvTrustAnchors + " environment variable",
				Err:        err,
			})
		}
		return ReportError(cmd, &output.AppError{
			Message:    "could not load the trust anchors file",
			Reason:     err.Error(),
			Resolution: "Fix the trust anchors file, or point the install at a different allowlist with --trust-anchors <path> or the " + registry.EnvTrustAnchors + " environment variable",
			Err:        err,
		})
	}

	// 7. Bundle.Verify (TS-014-05-01): the exact same trust engine
	// online installs use (VerifyTrust — content digest integrity,
	// publisher attestation, out-of-band anchor match) with the
	// operator-supplied anchors. The documented contract: the offline
	// install calls Bundle.Verify and ABORTS on Valid == false — there
	// is no bundle-specific verification path, no TOFU, and no
	// privileged path (ADR-022 §3). The per-dimension failure reasons
	// (digest mismatch / attestation / anchor) are displayed separately,
	// with the offline-appropriate guidance for a content digest
	// mismatch (the shared engine's "re-fetch from the distribution
	// location" advice is misleading offline — the bundled content IS
	// the content, and the location is never fetched).
	trust := bundle.Verify(anchors)
	if !trust.Valid {
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("trust verification failed for standard %q version %q", bundle.Metadata.ID, bundle.Metadata.Version),
			Reason:     standardBundleVerifyFailureReason(trust),
			Resolution: "Do not adopt content that fails verification; if you are the publisher, resolve the trust problems listed above; otherwise obtain a fresh bundle from the publisher or report the release",
		})
	}

	// 8. Record the installed version (TS-014-03-03): pinned version,
	// declared contract version, explicit resolution (kind "bundle",
	// source = the bundle path), lifecycle state, and the embedded
	// compatibility and trust results. Idempotent by identity plus
	// version (ADR-023 §3), exactly like the online install — the
	// persistence itself is the shared
	// persistStandardInstallRecord helper.
	return recordStandardBundleInstall(cmd, bundlePath, bundle, before, trust)
}

// standardBundleVerifyFailureReason renders the verify-failure reasons
// of an offline install with the offline-appropriate digest guidance.
// The shared engine's digest-mismatch message (trust.go / VerifyTrust)
// suggests "re-fetch the release content from <distribution.location>"
// — the right guidance online, but misleading offline, where the
// bundled content IS the release content and the declared location is
// never fetched (and is a dead URL by design in tests). When the
// integrity check failed, an offline note redirects the adopter to the
// bundle itself: obtain a fresh copy from the publisher. The shared
// engine text is unchanged (the online path is untouched).
func standardBundleVerifyFailureReason(trust registry.TrustResult) string {
	reason := strings.Join(trust.Errors, "\n")
	if !trust.IntegrityVerified {
		reason += "\nOffline note: the bundled content does not match the content digests declared in the bundled metadata — obtain a fresh copy of the bundle from the publisher"
	}
	return reason
}

// recordStandardBundleInstall persists the installed-standard record of
// an offline install. The persistence semantics (idempotency, version
// conflict, corrupt-record recovery, timestamps, result assembly) are
// the shared persistStandardInstallRecord helper (standard_shared.go) —
// the single record-persistence path also used by the online install
// flow (T-007). This caller contributes the resolution-specific record:
// kind "bundle" with the bundle path as the source (ADR-022 §3:
// resolution is explicit and recorded; TS-014-05-02 PM binding decision
// 6). The compatibility and trust results embedded in the record come
// from the combined adoption record: the pre-fetch result (lifecycle +
// compatibility) completed with the Bundle.Verify trust result through
// registry.CompleteAdoption — the single combination source the online
// flow's ValidateAdoptionAfterFetch uses internally, since Bundle.Verify
// runs the exact same engine (VerifyTrust).
func recordStandardBundleInstall(cmd *cobra.Command, bundlePath string, bundle *registry.Bundle, before registry.AdoptionResult, trust registry.TrustResult) error {
	md := bundle.Metadata

	// Combine the pre-fetch result with the trust result into the
	// adoption record through the single combination source
	// (registry.CompleteAdoption — the same combination
	// ValidateAdoptionAfterFetch applies online): Valid requires BOTH
	// the pre-fetch phase and trust to have passed (TS-014-04-03). No
	// double verification: Bundle.Verify already ran the exact same
	// trust engine, so the trust result is attached directly.
	adoption := registry.CompleteAdoption(before, trust)

	rec := registry.InstalledStandardRecord{
		FormatVersion:   registry.RecordFormatVersion,
		ID:              md.ID,
		Version:         md.Version,
		ContractVersion: md.ContractVersion,
		Resolution: registry.Resolution{
			// The installed content came from the bundled install
			// material: the recorded source is the exact bundle path
			// consumed (ADR-022 §3: resolution is explicit and
			// recorded; TS-014-05-02 PM binding decision 6).
			Kind:   registry.ResolutionKindBundle,
			Source: bundlePath,
		},
		Lifecycle:     md.Lifecycle,
		Compatibility: adoption.CompatibilityRecord(),
		Trust:         adoption.TrustRecord(),
	}

	result, err := persistStandardInstallRecord(cmd, rec, adoption, bundle.Warnings)
	if err != nil {
		return err
	}

	if jsonFlag, _ := cmd.Flags().GetBool("json"); jsonFlag {
		return WriteJSON(cmd, standardInstallJSONFromResult(result))
	}
	renderStandardInstall(cmd, result)
	return nil
}

// ── Bundle File Read ─────────────────────────────────────────────────

// readStandardBundleFile reads the bundle archive from disk, bounded:
// the archive must not exceed standardBundleMaxFileBytes (a valid bundle
// decompresses within the OpenBundle caps and compression expands
// incompressible data by only a small fraction, so an archive beyond the
// cap is a broken or hostile artifact). The read is purely local — the
// flow's only file inputs are the bundle, the anchors file, the
// compatibility matrix, the project config, and the record store.
func readStandardBundleFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, standardBundleMaxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if int64(len(data)) > standardBundleMaxFileBytes {
		return nil, fmt.Errorf(
			"%s exceeds the %d-byte bundle size cap — a valid bundle is a gzip-compressed tar archive carrying at most %d bytes of content; the file is a broken or hostile artifact, not a valid bundle. Obtain a fresh copy of the bundle from the publisher",
			path, standardBundleMaxFileBytes, registry.MaxBundleContentSize)
	}
	return data, nil
}

// reportStandardBundleFileError renders a bundle-file read failure. A
// missing bundle file is a not-found failure (exit 3 — "resource not
// found", TS-P8-07 / ADR-010 §8.1, mirroring the online install's
// not-found convention); any other read failure is a general error
// (exit 1).
func reportStandardBundleFileError(cmd *cobra.Command, bundlePath string, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return ReportErrorWithCode(cmd, &output.AppError{
			Message:    fmt.Sprintf("the bundle file %q was not found", bundlePath),
			Reason:     err.Error(),
			Resolution: "Point the command at the bundled install material you received from the publisher (a single .tar.gz archive, TS-014-05-01)",
			Err:        err,
		}, output.ExitCodeRuntime)
	}
	return ReportError(cmd, &output.AppError{
		Message:    fmt.Sprintf("the bundle file %q could not be read", bundlePath),
		Reason:     err.Error(),
		Resolution: "Fix the file permissions or path, then run the install again",
		Err:        err,
	})
}

// reportStandardBundleOpenError renders a bundle rejection with its
// failure class attributed distinctly (TS-014-05-01 security notes; PM
// binding decision 4):
//
//   - structure: the archive is not a valid bundle (wrong layout,
//     corrupt stream, trailing data) → "not a valid bundle archive";
//   - integrity: the archive bytes do not match the embedded bundle
//     checksum → "corrupt or modified after it was created" — get a new
//     bundle;
//   - metadata: the bundled metadata document is missing, fails the
//     strict parse, or carries no verification material → "bundled
//     metadata document invalid" — fix the document or get a fresh
//     bundle (the wrapped *ParseError carries the field-level problems).
//
// Verification mismatches (content digest / attestation / anchor) are
// NOT rendered here: OpenBundle does not verify trust — that is
// Bundle.Verify's surface, attributed separately in the run flow.
func reportStandardBundleOpenError(cmd *cobra.Command, bundlePath string, err error) error {
	var be *registry.BundleError
	if errors.As(err, &be) {
		switch be.Kind {
		case registry.BundleErrorKindStructure:
			return ReportError(cmd, &output.AppError{
				Message:    fmt.Sprintf("the bundle %q is not a valid bundle archive", bundlePath),
				Reason:     be.Error(),
				Resolution: "Obtain a fresh copy of the bundle from the publisher — the bundle format is pinned: a single tar.gz carrying exactly content, metadata.json, and bundle.sha256, in that order (TS-014-05-01)",
				Err:        err,
			})
		case registry.BundleErrorKindIntegrity:
			return ReportError(cmd, &output.AppError{
				Message:    fmt.Sprintf("the bundle %q is corrupt or was modified after it was created", bundlePath),
				Reason:     be.Error(),
				Resolution: "Obtain a fresh copy of the bundle from the publisher — do not use a bundle whose checksum does not match (TS-014-05-01)",
				Err:        err,
			})
		case registry.BundleErrorKindMetadata:
			return ReportError(cmd, &output.AppError{
				Message:    fmt.Sprintf("the bundled metadata document of %q is invalid", bundlePath),
				Reason:     be.Error(),
				Resolution: "Fix the metadata document or obtain a fresh bundle from the publisher — a bundle without a valid metadata document carrying its verification material (content digests, signature, public key) cannot be verified (ADR-022 §3)",
				Err:        err,
			})
		}
	}
	return ReportError(cmd, &output.AppError{
		Message:    fmt.Sprintf("the bundle %q was rejected", bundlePath),
		Reason:     err.Error(),
		Resolution: "Obtain a fresh copy of the bundle from the publisher",
		Err:        err,
	})
}
