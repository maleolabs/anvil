// Package cmd implements the Anvil CLI commands.
//
// ── Standard Install (TS-014-03-01) ──────────────────────────────────
//
// "anvil standard install <id> [version]" is the explicit installation
// flow of a delivery lifecycle standard release (ADR-023 §3): the user
// invokes it deliberately — installation never happens implicitly or as a
// side effect of another command. The command resolves the version — the
// explicit version when given, otherwise the latest published release
// from the index (ST-021-05) — validates metadata, verifies integrity
// and attestation, records the installed version, and remains idempotent
// by standard identity plus version.
//
// Adoption order (TS-014-04; PM binding decision: must not bypass
// validation). The flow is a documented, enforced sequence; content is
// never installed before every gate passes:
//
//  1. Resolve the index entry (structural decode, index.go) and re-read
//     the raw document through the strict registry parse (parse.go,
//     TS-014-01-02) — the structural decode alone is never trusted;
//  2. Read the runtime's supported contract majors from the
//     compatibility matrix (LoadCompatibilityMatrix, TS-014-04-03) — the
//     corpus reference (docs/specification-corpus/compatibility-matrix.
//     json; ADR-029 §3), read at runtime, never hardcoded (T-010
//     reviewer finding G4); a matrix that cannot be read aborts the
//     install with an actionable error — supported majors are never
//     silently defaulted;
//  3. Project framework version gate (PM binding decision 3): the
//     project's framework version is determined from the project config
//     (anvil.yaml — framework.<name>.version; see the paragraph below);
//     declared-but-undeterminable REJECTS the install — compatibility
//     is never assumed — while framework-free projects proceed
//     shape-only, recorded explicitly (ADR-026);
//  4. Adoption validation, pre-fetch phase (ValidateAdoptionBeforeFetch,
//     TS-014-04-03): inside the component the lifecycle gate runs
//     BEFORE compatibility validation. The lifecycle gate — only
//     published and deprecated releases are adoptable (lifecycle.go);
//     retired releases are not offered for fresh adoption and fail with
//     an actionable error distinguishing retired from not-found; an
//     unknown state (defensive guard) gets its own message — is
//     followed by compatibility validation (ValidateCompatibility,
//     TS-014-04-01) against the supported contract majors from the
//     matrix and the project framework version from step 3 — declared,
//     validated, recorded, never assumed (Transition Plan A2; ADR-024
//     §3.6). A failure aborts the install before any content is
//     fetched;
//  5. Content location resolution (ResolveLocation, TS-014-02-03) with
//     defensive https re-validation;
//  6. Trust anchors BEFORE the fetch: resolving and loading the allowlist
//     is a local operation, so a missing or corrupt anchors file fails
//     fast without wasting a download (reviewer finding; the adoption
//     order — verify trust over the fetched content — is unchanged);
//  7. Content fetch (ADR-030): https-only, userinfo rejected, redirects
//     never followed to non-https, a 1 GiB cap enforced during download,
//     and the download timeout model — connection phase bounded by the
//     transport, body read bounded by the idle window (TD-008); the
//     ACTUAL endpoint used — the final response URL after any allowed
//     redirects — is recorded as the explicit resolution; a fetch
//     failure aborts the install with an actionable error;
//  8. Adoption validation, post-fetch phase (ValidateAdoptionAfterFetch,
//     TS-014-04-03): trust verification (VerifyTrust, TS-014-04-02 /
//     ADR-022) over the fetched content with the operator's out-of-band
//     trust anchor allowlist — the ONLY gate; there is no
//     skip/insecure/no-verify flag and no privileged path. Both
//     validation phases always run in a full adoption, and a failure in
//     either aborts the install (TS-014-04-03 DoD);
//  9. State recording (InstalledStandardStore, TS-014-03-03): the
//     pinned version, declared contract version, explicit resolution,
//     lifecycle state, and the embedded compatibility and trust results
//     are recorded. Recording the same identity plus version again is
//     idempotent: the full validation still runs (every adoption
//     validates; trust is non-negotiable), the record's validation
//     results are refreshed via Update, and the command reports
//     "already installed at <version> (re-validated)". A different
//     version already installed is rejected: version change is an
//     update, an explicit adoption event handled by the update flow
//     (TS-014-03-02); this flow never updates.
//
// Project framework version (PM binding decision 3). The project's
// framework version is read from the project config (anvil.yaml) when
// the project declares a framework: project.framework names the
// framework, and the version comes from the framework config extension
// convention framework.<name>.version (e.g. framework.laravel.version).
// A project that declares a framework whose version cannot be determined
// REJECTS the install with an actionable error — compatibility is never
// assumed. A framework-free project (no project, or no framework
// declared) is valid (ADR-026): capability validation then runs
// shape-only with FrameworkVersionChecked=false, and that not-checked
// fact is recorded in the installed-standard record — never hidden.
//
// Content fetch policy (PM binding decisions 5, 10; security review):
//
//   - https-only: enforced at parse and ResolveLocation, and re-checked
//     defensively at the fetch boundary and after every redirect;
//   - userinfo (https://user:pass@host/...) rejected at parse, at
//     ResolveLocation, and at the fetch boundary — credentials must
//     never be sent as Basic auth, echoed in errors, or persisted in
//     the installed-standard record;
//   - redirects: never followed to a non-https target, chain bounded;
//     the final response URL (after allowed redirects) is recorded as
//     the resolution source — the auditable actual endpoint used;
//   - size cap: 1 GiB (consistent with the bundle content cap,
//     MaxBundleContentSize), enforced DURING download via a limit
//     reader — content is never buffered unbounded;
//   - timeout: the download client bounds the connection phase (dial,
//     TLS handshake, response headers) and the body read by an idle
//     window (30s default, overridable via ANVIL_DOWNLOAD_IDLE_TIMEOUT)
//     — a stalled fetch fails with a clear timeout error while a slow
//     but progressing download is never cut off by a total deadline
//     (TD-008);
//   - failure: 404/5xx/DNS/timeout/TLS abort the install with an
//     actionable error naming what failed and how to resolve it, for
//     two audiences — the publisher (fix the release asset) and the
//     adopter (choose another version or report the broken release).
//
// No content is installed when validation fails: nothing is written
// before every gate passes, and the only persisted state is the
// installed-standard record (the content materialization flow is
// downstream scope, EPIC-015).
//
// Reference: TS-014-03-01, ADR-022 §3, ADR-023 §3, ADR-026, ADR-027 §3,
// ADR-030 §3, Transition Plan A2
package cmd

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/registry"
)

// standardInstallCmd represents the "anvil standard install" command that
// explicitly installs one standard release from the static registry
// index.
//
// Reference: TS-014-03-01, ADR-023 §3
var standardInstallCmd = &cobra.Command{
	Use:   "install <id> [version]",
	Short: "Install a standard release (explicit adoption)",
	Long: `Install one delivery lifecycle standard release from the static
registry index (TS-014-03-01, ADR-023).

Installation is explicit: it happens only when you run this command —
never implicitly and never as a side effect of another command. The
command resolves the version, validates the metadata, fetches
the release content from the standard's release channel (https only),
verifies integrity and publisher attestation against the operator's
trust anchor allowlist (ADR-022 — there is no skip or insecure path),
and records the installed version with its pinned resolution.

When <version> is omitted, the latest published release is resolved
from the index: candidates are ordered semantically and the highest
release offered for fresh adoption wins (retired releases are never
resolved, ADR-027 §3). The resolved version is then pinned and
validated exactly like an explicit one (ADR-022 §3).

Every adoption is validated: compatibility (declared contract version
and capability) and trust run before the install completes. Re-installing
the same identity and version is idempotent — the full validation still
runs and the record is re-validated. Installing a different version of
an already-installed standard is rejected: version change is an update,
an explicit adoption event of the update flow (TS-014-03-02). Deprecated
releases install with a warning; retired releases are not offered for
fresh adoption.

The project's framework version is read from anvil.yaml when the
project declares a framework (framework.<name>.version); a project that
declares a framework without a determinable version is rejected (never
assumed). Framework-free projects install with shape-only capability
validation, recorded explicitly.

Trust anchors file format (one entry per publisher, JSON):
  {"publishers": {"<standard-id>": "<base64 Ed25519 public key>"}}
The standard id is the publisher identity (the metadata document's id);
the key is the strict base64 encoding of the publisher's 32-byte
Ed25519 verification public key. Without an anchored key the install
fails: there is no first-use acceptance.

Install boundary: this command verifies the release (metadata,
compatibility, integrity, attestation) and records the adoption with
its pinned resolution. Materializing the verified content into a usable
standard artifact is the downstream adoption scope (EPIC-015) — it is
not part of this command.

Output formats:
  Default      sectioned install report (identity, resolution, lifecycle,
               record path, validation summary, warnings)
  --json       standard TS-P8-05 envelope on stdout, data:
               {id, version, contract_version, resolution, lifecycle,
               installed_at, updated_at, already_installed, warnings,
               compatibility, trust, record_path}

Index resolution order:
  1. --index <path>
  2. the ANVIL_REGISTRY_INDEX environment variable
  3. the default <user config dir>/anvil/registry
     (e.g. ~/.config/anvil/registry on Linux)

Trust anchors resolution order:
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

Exit codes (truthful mapping, TS-019-03-02 D-06): 0 on success; 3 when
the index directory, the standard, or the version is not found; 2 when a
different version of the standard is already installed (version
conflict); 1 for other errors (invalid release, retired release,
compatibility or trust failure, fetch failure, project framework version
undeterminable).

Examples:
  anvil standard install anvil-standard-laravel
  anvil standard install anvil-standard-laravel 1.2.3
  anvil standard install anvil-standard-laravel 1.2.3 --index ./registry
  anvil standard install anvil-standard-laravel 1.2.3 --trust-anchors ./anchors.json
  anvil standard install anvil-standard-laravel 1.2.3 --json`,
	Args:         RangeArgsWithUsage(1, 2, "anvil standard install anvil-standard-laravel [version]", "id", "version"),
	SilenceUsage: true,
	RunE:         runStandardInstall,
}

func init() {
	AddJSONFlag(standardInstallCmd)
	standardInstallCmd.Flags().String("index", "", "path to the static registry index directory (default: $ANVIL_REGISTRY_INDEX, else <user config dir>/anvil/registry)")
	standardInstallCmd.Flags().String("trust-anchors", "", "path to the trust anchors allowlist file (default: $ANVIL_TRUST_ANCHORS, else <user config dir>/anvil/trust-anchors.json)")
	standardInstallCmd.Flags().Bool("yes", false, "auto-accept registry sync prompts")
	standardInstallCmd.Flags().Bool("no-sync", false, "disable auto-offer sync (airgap)")
}

// ── Run ──────────────────────────────────────────────────────────────

// standardInstallResult is the outcome of one install run, rendered by
// the human-readable and machine-readable surfaces.
type standardInstallResult struct {
	ID               string
	Version          string
	ContractVersion  string
	Resolution       registry.Resolution
	Lifecycle        registry.Lifecycle
	InstalledAt      time.Time
	UpdatedAt        time.Time
	AlreadyInstalled bool
	Warnings         []string
	Compatibility    registry.CompatibilityResult
	Trust            registry.TrustResult
	RecordPath       string
	// ResolvedLatest marks a version resolved as the latest published
	// release (version omitted, ST-021-05) — the human report annotates
	// the choice; the JSON contract is unchanged.
	ResolvedLatest bool
}

// runStandardInstall executes the install command through the documented
// adoption order (file header): resolve (with the strict parse —
// TS-014-01-02) → supported contract majors (compatibility matrix) →
// project framework version → adoption validation pre-fetch phase
// (lifecycle gate → compatibility) → location → fetch → adoption
// validation post-fetch phase (trust + combined record) → record. Both
// validation phases always run in a full adoption and a failure at any
// gate aborts the install with an actionable error and no state is
// written.
//
// Reference: TS-014-03-01, TS-014-04, ADR-022 §3, ADR-023 §3
func runStandardInstall(cmd *cobra.Command, args []string) error {
	offerStandardIndexSync(cmd)
	id := args[0]
	version := ""
	if len(args) == 2 {
		version = args[1]
	}

	// 1. Load the index and resolve the target: the explicit version when
	// given, otherwise the latest published release (ST-021-05). The
	// strict parse runs inside the shared resolution (the latest path's
	// lifecycle filter needs the strict-parsed state), so its result is
	// reused. This is structural decode only (TS-014-02-01).
	ix, err := loadStandardIndex(cmd)
	if err != nil {
		return reportStandardIndexError(cmd, err)
	}
	target, err := resolveStandardTarget(cmd, ix, id, version, "install")
	if err != nil {
		return err
	}
	version = target.Version
	resolvedLatest := target.ResolvedLatest

	// 2. Supported contract majors from the compatibility matrix
	// (T-010 reviewer finding G4 — must-fix): the runtime's supported
	// contract major set is READ from the corpus matrix record at
	// runtime (docs/specification-corpus/compatibility-matrix.json;
	// ADR-029 §3) — never hardcoded, so the engine and the corpus
	// cannot drift. The matrix is a local read; an unreadable matrix
	// aborts the install with an actionable error: supported majors are
	// never silently defaulted (PM binding decision 3).
	supportedContractMajors, err := supportedContractMajors()
	if err != nil {
		return ReportError(cmd, &output.AppError{
			Message:    "could not load the compatibility matrix",
			Reason:     err.Error(),
			Resolution: "Set the " + registry.EnvCompatibilityMatrix + " environment variable to the corpus matrix file (docs/specification-corpus/compatibility-matrix.json), or run the install from the repository root",
			Err:        err,
		})
	}

	// 3. Project framework version gate (PM binding decision 3): the
	// project's framework version is read from the project config.
	// Declared-but-undeterminable rejects the install (never assumed,
	// Transition Plan A2); framework-free projects proceed shape-only,
	// recorded explicitly (ADR-026).
	projectFrameworkVersion, err := projectFrameworkVersionForInstall()
	if err != nil {
		return ReportError(cmd, &output.AppError{
			Message:    "could not determine the project's framework version",
			Reason:     err.Error(),
			Resolution: "Declare the framework version in anvil.yaml (framework.<name>.version), or install in a framework-free project (shape-only validation)",
			Err:        err,
		})
	}

	// 4. Adoption validation, pre-fetch phase (TS-014-04-03): inside the
	// component the lifecycle gate runs BEFORE the compatibility
	// validation. A failure aborts the install before any content is
	// fetched (the pinned adoption order — a compatibility failure
	// means zero fetches).
	before := registry.ValidateAdoptionBeforeFetch(target.Metadata, supportedContractMajors, projectFrameworkVersion)
	if !before.Valid {
		if !before.Adoptable {
			// The lifecycle gate failed (retired, or an unknown state):
			// the release is not offered for fresh adoption. The
			// component's message distinguishes retired from not-found.
			return ReportError(cmd, &output.AppError{
				Message:    fmt.Sprintf("standard %q version %q is not offered for adoption", id, version),
				Reason:     strings.Join(before.Errors, "\n"),
				Resolution: "Run 'anvil standard list' to see the standards offered for adoption, or choose another standard",
			})
		}
		// Compatibility failed: the adoption aborts BEFORE any content
		// is fetched (the pinned adoption order — a compatibility
		// failure means zero fetches).
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("standard %q version %q is not compatible", id, version),
			Reason:     strings.Join(before.Errors, "\n"),
			Resolution: "If you are the publisher, resolve the compatibility problems listed above; otherwise choose another version or report the standard to its publisher",
		})
	}

	// 5. Content location resolution (TS-014-02-03). Resolution
	// defensively re-validates the https-only rules; the fetch boundary
	// re-checks them again.
	location, err := registry.ResolveLocation(target.Entry)
	if err != nil {
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("the release content of standard %q version %q cannot be resolved", id, version),
			Reason:     err.Error(),
			Resolution: "Fix the metadata document's distribution declaration, then run the install again",
			Err:        err,
		})
	}

	// 6. Trust anchors BEFORE the fetch (reviewer finding 2): resolving
	// and loading the allowlist is a local operation, so a missing or
	// corrupt anchors file fails fast without wasting up to 1 GiB / 60s
	// of download. Anchors resolve via the documented order (flag → env →
	// default) and default-fail when none are configured. VerifyTrust
	// still runs over the fetched content AFTER the fetch — the adoption
	// order is unchanged.
	anchorsPath, err := standardTrustAnchorsPath(cmd)
	if err != nil {
		return ReportError(cmd, &output.AppError{
			Message:    "could not resolve the trust anchors path",
			Reason:     err.Error(),
			Resolution: "Configure the trust anchors file, or point the install at one with --trust-anchors <path> or the " + registry.EnvTrustAnchors + " environment variable",
			Err:        err,
		})
	}
	anchors, err := loadTrustAnchorsConfigured(anchorsPath)
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

	// 7. Fetch the release content (ADR-030): https-only, bounded
	// redirects, size cap during download, shared timeout. The final
	// response URL (after any allowed redirects) is the actual endpoint
	// used and is recorded as the explicit resolution. A fetch failure
	// aborts the install — nothing is installed on failure.
	content, contentSource, err := fetchStandardContent(location.Location)
	if err != nil {
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("could not fetch the release content of standard %q version %q", id, version),
			Reason:     err.Error(),
			Resolution: "If you are the publisher, fix the release asset at the declared distribution location; otherwise choose another version or report the broken release",
			Err:        err,
		})
	}

	// 8. Adoption validation, post-fetch phase (TS-014-04-03): trust
	// verification (TS-014-04-02 / ADR-022) over the fetched content —
	// the ONLY gate: no skip/insecure/no-verify flag and no privileged
	// path — and the combined adoption record. A failure in either
	// validation phase aborts the install (TS-014-04-03 DoD).
	adoption := registry.ValidateAdoptionAfterFetch(target.Metadata, content, anchors, before)
	if !adoption.Valid {
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("trust verification failed for standard %q version %q", id, version),
			Reason:     strings.Join(adoption.Errors, "\n"),
			Resolution: "Do not adopt content that fails verification; if you are the publisher, resolve the trust problems listed above; otherwise choose another version or report the standard to its publisher",
		})
	}

	// 9. Record the installed version (TS-014-03-03): pinned version,
	// declared contract version, explicit resolution, lifecycle state,
	// and the embedded compatibility and trust results from the combined
	// adoption record. Idempotent by identity plus version (ADR-023 §3).
	return recordStandardInstall(cmd, target.Metadata, contentSource, adoption, target.ParseWarnings, resolvedLatest)
}

// recordStandardInstall persists the installed-standard record. The
// persistence semantics (idempotency, version conflict, corrupt-record
// recovery, the completed-adoption requirement, timestamps, result
// assembly) are the shared persistStandardInstallRecord helper
// (standard_shared.go) — the single record-persistence path also used
// by the offline/bundled install flow (TS-014-05-02). This caller
// contributes the resolution-specific record: contentSource is the
// ACTUAL endpoint the content was fetched from — the final response URL
// after any allowed redirects, not the declared location — so the
// recorded resolution is the auditable truth (ADR-022 §3: resolution is
// explicit and recorded). The compatibility and trust results embedded
// in the record come from the combined adoption record (TS-014-04-03);
// CompatibilityRecord and TrustRecord expose them in the record's
// persistence shape (T-009). resolvedLatest marks a version resolved as
// the latest published release (version omitted, ST-021-05) — advisory
// context for the human report only, never part of the record.
func recordStandardInstall(cmd *cobra.Command, md registry.Metadata, contentSource string, adoption registry.AdoptionResult, parseWarnings []registry.Warning, resolvedLatest bool) error {
	rec := registry.InstalledStandardRecord{
		FormatVersion:   registry.RecordFormatVersion,
		ID:              md.ID,
		Version:         md.Version,
		ContractVersion: md.ContractVersion,
		Resolution: registry.Resolution{
			// The installed content came from the distribution location on
			// the standard's release channel; the recorded source is the
			// ACTUAL endpoint used — the final https URL after any allowed
			// redirects (ADR-022 §3).
			Kind:   registry.ResolutionKindDistribution,
			Source: contentSource,
		},
		Lifecycle:     md.Lifecycle,
		Compatibility: adoption.CompatibilityRecord(),
		Trust:         adoption.TrustRecord(),
		// The standard's declared per-skill assets, persisted at the
		// explicit re-validated install (ST-021-04 / ADR-037 D3): the
		// record IS the skill registry — anvil skill list and the skill
		// install path read these declarations.
		Skills: registry.SkillDeclarations(md.Skills),
	}

	result, err := persistStandardInstallRecord(cmd, rec, adoption, parseWarnings)
	if err != nil {
		return err
	}
	result.ResolvedLatest = resolvedLatest

	if jsonFlag, _ := cmd.Flags().GetBool("json"); jsonFlag {
		return WriteJSON(cmd, standardInstallJSONFromResult(result))
	}
	renderStandardInstall(cmd, result)
	return nil
}

// ── Output ───────────────────────────────────────────────────────────

// standardInstallJSON is the machine-readable install output: identity,
// pinned version, declared contract version, explicit resolution,
// lifecycle, timestamps, the already-installed marker, warnings, and the
// embedded validation results (recorded exactly as persisted).
type standardInstallJSON struct {
	ID               string                       `json:"id"`
	Version          string                       `json:"version"`
	ContractVersion  string                       `json:"contract_version"`
	Resolution       standardResolutionJSON       `json:"resolution"`
	Lifecycle        standardLifecycleJSON        `json:"lifecycle"`
	InstalledAt      string                       `json:"installed_at"`
	UpdatedAt        string                       `json:"updated_at"`
	AlreadyInstalled bool                         `json:"already_installed"`
	Warnings         []string                     `json:"warnings,omitempty"`
	Compatibility    registry.CompatibilityResult `json:"compatibility"`
	Trust            registry.TrustResult         `json:"trust"`
	RecordPath       string                       `json:"record_path"`
}

// standardInstallJSONFromResult converts the install result into its
// machine-readable shape. Timestamps are RFC 3339 (the record
// persistence shape).
func standardInstallJSONFromResult(result standardInstallResult) standardInstallJSON {
	return standardInstallJSON{
		ID:              result.ID,
		Version:         result.Version,
		ContractVersion: result.ContractVersion,
		Resolution: standardResolutionJSON{
			Kind:   result.Resolution.Kind,
			Source: result.Resolution.Source,
		},
		Lifecycle: standardLifecycleJSON{
			State:       result.Lifecycle.State,
			RemovalDate: result.Lifecycle.RemovalDate,
		},
		InstalledAt:      result.InstalledAt.UTC().Format(time.RFC3339),
		UpdatedAt:        result.UpdatedAt.UTC().Format(time.RFC3339),
		AlreadyInstalled: result.AlreadyInstalled,
		Warnings:         result.Warnings,
		Compatibility:    result.Compatibility,
		Trust:            result.Trust,
		RecordPath:       result.RecordPath,
	}
}

// renderStandardInstall writes the human-readable install report. The
// validation summary surfaces the recorded facts — including the
// framework-not-checked state of a framework-free project, never hidden —
// and the deprecation warning section is rendered for deprecated
// releases.
func renderStandardInstall(cmd *cobra.Command, result standardInstallResult) {
	s := styleFor(cmd)
	w := s.W
	if result.AlreadyInstalled {
		fmt.Fprintf(w, "Standard %s is already installed at version %s (re-validated).\n", result.ID, result.Version)
	} else if result.ResolvedLatest {
		fmt.Fprintf(w, "Installed standard: %s %s (latest published release)\n", result.ID, result.Version)
	} else {
		fmt.Fprintf(w, "Installed standard: %s %s\n", result.ID, result.Version)
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Contract Version:")
	fmt.Fprintf(w, "  %s\n", result.ContractVersion)
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Resolution:")
	fmt.Fprintf(w, "  kind: %s\n", result.Resolution.Kind)
	fmt.Fprintf(w, "  source: %s\n", result.Resolution.Source)
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Lifecycle:")
	fmt.Fprintf(w, "  %s\n", standardStatusCellForLifecycle(result.Lifecycle))
	fmt.Fprintln(w)

	if result.AlreadyInstalled {
		fmt.Fprintln(w, "Updated At:")
		fmt.Fprintf(w, "  %s\n", result.UpdatedAt.UTC().Format(time.RFC3339))
		fmt.Fprintln(w)
	} else {
		fmt.Fprintln(w, "Installed At:")
		fmt.Fprintf(w, "  %s\n", result.InstalledAt.UTC().Format(time.RFC3339))
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, "Record:")
	fmt.Fprintf(w, "  %s\n", result.RecordPath)
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Validation:")
	compatStatus := "ok (framework version " + result.Compatibility.ProjectFrameworkVersion + " checked)"
	if !result.Compatibility.FrameworkVersionChecked {
		compatStatus = "ok (shape-only: project framework version not checked — recorded, not assumed)"
	}
	fmt.Fprintf(w, "  compatibility: %s\n", compatStatus)
	fmt.Fprintf(w, "  trust: ok (integrity verified, attestation verified, anchor matched")
	if result.Trust.AnchorPath != "" {
		fmt.Fprintf(w, " against %s", result.Trust.AnchorPath)
	}
	fmt.Fprintln(w, ")")
	fmt.Fprintln(w)

	if len(result.Warnings) > 0 {
		fmt.Fprintln(w, "Warnings:")
		for _, message := range result.Warnings {
			fmt.Fprintf(w, "  %s\n", message)
		}
		fmt.Fprintln(w)
	}
}
