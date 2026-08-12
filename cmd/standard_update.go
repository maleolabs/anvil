// Package cmd implements the Anvil CLI commands.
//
// ── Standard Update (TS-014-03-02) ───────────────────────────────────
//
// "anvil standard update <id> <version>" is the explicit update flow of
// an installed delivery lifecycle standard release (ADR-022 §3, ADR-023
// §3): the user invokes it deliberately — updates never happen
// implicitly and never as a side effect of another command; there is no
// auto-update path for server-side standards (DoD TS-014-03-02). The
// target version is explicit: there is no "latest" resolution — versions
// are pinned per ADR-022 §3.
//
// Downgrade semantics (PM binding decision 1; reviewer F5): the target
// version is explicit and ANY version — including an OLDER one — is a
// valid explicit adoption event. A downgrade is fully re-validated
// (compatibility and trust run against the target exactly as at
// installation), records the older pinned version, and never triggers
// an automatic rollback: the user invoked the downgrade, and only the
// invoked version is adopted.
//
// Update requires an installed standard (PM binding decision 2): the
// command reads the current installed-standard record FIRST — a standard
// that is not installed is rejected with an actionable error (install
// first), and a corrupt record is rejected with recovery guidance
// (re-install re-establishes the record state; the update flow cannot
// evaluate the installed lifecycle of an unreadable record).
//
// Adoption order (PM binding decision 5 — the core requirement: an
// update re-validates EXACTLY as at installation). The flow runs the
// SAME orchestration phases as the install flow (TS-014-03-01), reusing
// its helpers verbatim (standard_shared.go) — compatibility and trust
// are re-checked against the TARGET version before it replaces the
// installed one (ADR-022 §3: "trust and compatibility are validated
// together at every install and update"):
//
//  1. Read the current installed-standard record and run the update
//     lifecycle gate on the INSTALLED standard (LifecycleUpdateAllowed,
//     TS-014-01-03): deprecated installed standards receive no updates
//     (ADR-023 §3) — the update attempt fails with an actionable error
//     explaining the no-updates rule. Only a published installed
//     standard is updatable; the gate runs BEFORE anything is resolved
//     or fetched;
//  2. Resolve the index entry (structural decode, index.go) and re-read
//     the raw document through the strict registry parse (parse.go,
//     TS-014-01-02);
//  3. Read the runtime's supported contract majors from the
//     compatibility matrix (LoadCompatibilityMatrix, TS-014-04-03) — an
//     unreadable matrix aborts the update with an actionable error;
//  4. Project framework version gate: declared-but-undeterminable
//     REJECTS the update; framework-free projects proceed shape-only,
//     recorded explicitly (ADR-026);
//  5. Adoption validation, pre-fetch phase
//     (ValidateAdoptionBeforeFetch, TS-014-04-03) on the TARGET entry:
//     the lifecycle gate — a retired target is not offered for adoption
//     and fails with an actionable error distinguishing retired from
//     not-found; a deprecated TARGET is adoptable and the update
//     proceeds WITH the deprecation warning (PM binding decision 4: the
//     update is itself the explicit adoption event; the updated record
//     keeps the deprecated lifecycle state and will receive no further
//     updates) — followed by compatibility validation. A failure aborts
//     the update BEFORE any content is fetched (a compatibility failure
//     means zero fetches);
//  6. Content location resolution (ResolveLocation, TS-014-02-03) with
//     defensive https re-validation;
//  7. Trust anchors BEFORE the fetch (same as install: a missing or
//     corrupt anchors file fails fast without wasting a download);
//  8. Content fetch (ADR-030): https-only, userinfo rejected, redirects
//     never followed to non-https, the 1 GiB cap enforced during the
//     download, the download timeout model — connection phase bounded by
//     the transport, body read bounded by the idle window (TD-008) — the
//     SAME fetch boundary as the install (reused
//     fetchStandardContent; no --skip-verify and no downgrade of
//     trust); the ACTUAL endpoint used — the final response URL after
//     any allowed redirects — is recorded as the new explicit
//     resolution; a fetch failure aborts the update with an actionable
//     error and the old record stays intact;
//  9. Adoption validation, post-fetch phase (ValidateAdoptionAfterFetch,
//     TS-014-04-03): trust verification over the fetched content with
//     the operator's trust anchor allowlist — the ONLY gate; both
//     validation phases always run in a full adoption (TS-014-04-03
//     DoD);
//  10. State recording (InstalledStandardStore.Update, TS-014-03-03 —
//     atomic replace): the new pinned version, the NEW declared contract
//     version, the new explicit resolution, the target's lifecycle
//     state, and the freshly embedded compatibility and trust results.
//     installedAt (the original install time) is PRESERVED; updatedAt is
//     refreshed with this adoption event. A failed update leaves the old
//     record intact (atomic).
//
// Idempotency (PM binding decision 7): for a published installed
// standard, updating to the version already installed still runs the
// full validation (every adoption validates; trust is non-negotiable)
// and refreshes the record via Update, and the command reports "already
// at version X (re-validated)".
//
// No auto-update (DoD): the update flow exists ONLY as this explicit
// command; nothing in the engine invokes it implicitly, and server-side
// standards are never updated without an explicit adoption event.
//
// Record semantics (PM binding decision 6; TS-014-03-03): Update
// atomically replaces the record file (temp file + rename + fsync). The
// new record carries the TARGET version and the target's lifecycle
// state: a deprecated target is recorded as deprecated (the explicit
// adoption event), so the updated record immediately receives no
// further updates — the no-updates rule is self-enforcing on the record
// state, never bypassable by re-running the update.
//
// Exit codes (truthful mapping, TS-019-03-02 D-02): 0 on success; 3 when
// the index directory, the standard, or the version is not found; 4 when
// the standard is not installed (precondition — the installed standard is
// a required prerequisite of the update); 1 for other errors (corrupt
// record, installed-deprecated no-updates rejection, retired target,
// invalid release, compatibility or trust failure, fetch failure,
// framework version undeterminable).
//
// Reference: TS-014-03-02, TS-014-03-01, TS-014-03-03, TS-014-04,
// ADR-022 §3, ADR-023 §3, ADR-027 §3, ADR-030 §3, Transition Plan A2
package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/registry"
)

// standardUpdateCmd represents the "anvil standard update" command that
// explicitly updates an installed standard release to a pinned target
// version from the static registry index, re-validating exactly as at
// installation.
//
// Reference: TS-014-03-02, ADR-022 §3, ADR-023 §3
var standardUpdateCmd = &cobra.Command{
	Use:   "update <id> <version>",
	Short: "Update an installed standard release (explicit adoption)",
	Long: `Update one installed delivery lifecycle standard release to an explicit
target version (TS-014-03-02, ADR-022 §3, ADR-023 §3).

Updates are explicit: they happen only when you run this command — never
implicitly and never as a side effect of another command; there is no
auto-update path for server-side standards. The target version is
explicit: there is no "latest" resolution — versions are pinned
(ADR-022 §3). ANY version is a valid explicit adoption event, including
an OLDER one: a downgrade is fully re-validated (compatibility and
trust run against the target exactly as at installation), records the
older pinned version, and never triggers an automatic rollback — the
update only ever adopts the version you invoked.

Update requires an installed standard: the command reads the current
installed-standard record first, and a standard that is not installed
is rejected with an actionable error (install it first).

Every update is validated exactly like an install: compatibility
(declared contract version and capability) and trust (integrity,
attestation, trust anchor allowlist — ADR-022; there is no skip or
insecure path) are re-checked against the TARGET version before it
replaces the installed one. The adoption order is the install's: read
record → update lifecycle gate → resolve → strict parse → compatibility
matrix → project framework version → lifecycle gate → compatibility →
location → trust anchors → fetch (https only) → trust → record. A
compatibility failure means zero fetches; a trust or fetch failure
leaves the record unchanged.

Lifecycle rules (ADR-023 §3, ADR-027 §3):
  - the INSTALLED standard's lifecycle gates the update: a deprecated
    installed standard receives no updates, and the update is rejected
    with an actionable error explaining the rule — only published
    installed standards are updatable;
  - the TARGET version's lifecycle runs through the standard adoption
    gate: a retired target is not offered for adoption and the update
    is rejected;
  - a deprecated TARGET version can be adopted explicitly: the update
    proceeds WITH the deprecation warning — the update is itself the
    explicit adoption event — and the updated record keeps the
    deprecated lifecycle state, so it receives no further updates.

For a published installed standard, updating to the version already
installed is idempotent: the full validation still runs and the record
is re-validated (the command reports "already at version X").

Record semantics: the update atomically replaces the installed-standard
record with the new pinned version, the new explicit resolution (the
final https URL the content was actually fetched from), the target's
lifecycle state, and the freshly embedded compatibility and trust
results. installedAt (the original install time) is preserved; updatedAt
is refreshed. A failed update leaves the old record intact.

Output formats:
  Default      sectioned update report (identity, resolution, lifecycle,
               original install time, update time, record path,
               validation summary, warnings)
  --json       standard TS-P8-05 envelope on stdout, data:
               {id, version, contract_version, resolution, lifecycle,
               installed_at, updated_at, already_at_version, warnings,
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
     update with an actionable error — supported majors are never
     silently defaulted.

Exit codes (truthful mapping, TS-019-03-02 D-02): 0 on success; 3 when
the index directory, the standard, or the version is not found; 4 when
the standard is not installed (the installed standard is a required
prerequisite of the update); 1 for other errors (corrupt record,
installed-deprecated no-updates rejection, retired target, invalid
release, compatibility or trust failure, fetch failure, framework
version undeterminable).

Examples:
  anvil standard update anvil-standard-laravel 1.3.0
  anvil standard update anvil-standard-laravel 1.3.0 --index ./registry
  anvil standard update anvil-standard-laravel 1.3.0 --trust-anchors ./anchors.json
  anvil standard update anvil-standard-laravel 1.3.0 --json`,
	Args:         RangeArgsWithUsage(2, 2, "anvil standard update anvil-standard-laravel 1.3.0", "id", "version"),
	SilenceUsage: true,
	RunE:         runStandardUpdate,
}

func init() {
	AddJSONFlag(standardUpdateCmd)
	standardUpdateCmd.Flags().String("index", "", "path to the static registry index directory (default: $ANVIL_REGISTRY_INDEX, else <user config dir>/anvil/registry)")
	standardUpdateCmd.Flags().String("trust-anchors", "", "path to the trust anchors allowlist file (default: $ANVIL_TRUST_ANCHORS, else <user config dir>/anvil/trust-anchors.json)")
}

// ── Run ──────────────────────────────────────────────────────────────

// standardUpdateResult is the outcome of one update run, rendered by
// the human-readable and machine-readable surfaces.
type standardUpdateResult struct {
	ID               string
	Version          string
	ContractVersion  string
	Resolution       registry.Resolution
	Lifecycle        registry.Lifecycle
	InstalledAt      time.Time
	UpdatedAt        time.Time
	AlreadyAtVersion bool
	Warnings         []string
	Compatibility    registry.CompatibilityResult
	Trust            registry.TrustResult
	RecordPath       string
}

// runStandardUpdate executes the update command through the documented
// adoption order (file header): read record → update lifecycle gate →
// resolve → strict parse → supported contract majors (compatibility
// matrix) → project framework version → adoption validation pre-fetch
// phase (lifecycle gate → compatibility) → location → trust anchors →
// fetch → adoption validation post-fetch phase (trust + combined
// record) → record update. Both validation phases always run in a full
// adoption, and a failure at any gate aborts the update with an
// actionable error and no state is written.
//
// Reference: TS-014-03-02, TS-014-03-01, TS-014-04, ADR-022 §3,
// ADR-023 §3
func runStandardUpdate(cmd *cobra.Command, args []string) error {
	id, version := args[0], args[1]

	// 1. Read the current installed-standard record FIRST (PM binding
	// decision 2): update requires an installed standard, and the
	// installed lifecycle state gates the update (step 2). Nothing is
	// resolved or fetched before these local gates pass.
	storeDir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not resolve the installed-standards directory: %v", err)
	}
	store := registry.NewInstalledStandardStore(storeDir)
	existing, err := store.Get(id)
	switch {
	case errors.Is(err, registry.ErrRecordNotFound):
		// Not installed: the update has nothing to update — the
		// actionable error says so and points at the install flow. Per
		// the truthful exit-code mapping (TS-019-03-02, D-02) the
		// installed standard is a required prerequisite of the update →
		// precondition category (exit 4).
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("standard %q is not installed", id),
			Reason:     "update requires an installed standard: the current installed-standard record is read first, and no record exists for this standard",
			Resolution: "Run 'anvil standard install " + id + " <version>' to install a release first, then update it to another version",
			Err:        err,
			// Precondition category (D-02): the installed standard is a
			// required prerequisite of the update.
			ExitCodeValue: output.ExitCodePrecondition,
		})
	case errors.Is(err, registry.ErrRecordCorrupt):
		// A corrupt record cannot be read: the installed lifecycle
		// state cannot be evaluated, so the update refuses rather than
		// bypass the update lifecycle gate. Recovery is a plain
		// re-adoption: the explicit install replaces the corrupt record
		// (TS-014-03-03).
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("the installed-standard record for %q cannot be read", id),
			Reason:     err.Error(),
			Resolution: "Re-install the standard explicitly ('anvil standard install " + id + " <version>') — the install flow replaces a corrupt record by re-adoption (TS-014-03-03); the update flow cannot update a standard whose installed state is unreadable",
			Err:        err,
		})
	case err != nil:
		return ReportPlainErrorf(cmd, err, "could not read the installed-standard record: %v", err)
	}

	// 2. Update lifecycle gate on the INSTALLED standard (PM binding
	// decision 4; TS-014-01-03): only published installed standards
	// receive updates — deprecated installed standards receive no
	// updates (ADR-023 §3, ADR-027 §3). The gate runs BEFORE anything
	// is resolved or fetched, and it can never be bypassed: there is no
	// flag and no path that skips it. LifecycleUpdateAllowed is the
	// reusable rule (lifecycle.go); retired or unknown installed states
	// are rejected defensively with the same gate.
	if !registry.LifecycleUpdateAllowed(existing.Lifecycle.State) {
		return ReportError(cmd, &output.AppError{
			Message: fmt.Sprintf("standard %q receives no updates", id),
			Reason: fmt.Sprintf("the installed standard is in %q state (installed version %s) and only published installed standards receive updates: deprecated standards receive no updates (ADR-023 §3, ADR-027 §3) — the no-updates rule applies to the INSTALLED standard",
				existing.Lifecycle.State, existing.Version),
			Resolution: "Keep the installed version, or adopt a DIFFERENT standard explicitly with 'anvil standard install <other-id> <version>' — this standard receives no updates, and the update flow never bypasses the no-updates rule (ADR-023 §3); contact the publisher for the standard's migration path",
		})
	}

	// 3. Load the index and resolve the entry. This is structural decode
	// only (TS-014-02-01); the strict parse follows in step 4.
	indexPath, err := standardIndexPath(cmd)
	if err != nil {
		return reportStandardIndexError(cmd, err)
	}
	ix, err := registry.LoadIndex(indexPath)
	if err != nil {
		return reportStandardIndexError(cmd, err)
	}
	entry, err := ix.Resolve(id, version)
	if err != nil {
		return ReportErrorWithCode(cmd, &output.AppError{
			Message:    fmt.Sprintf("standard %q version %q not found", id, version),
			Reason:     err.Error(),
			Resolution: "Run 'anvil standard list' to see the standards offered for adoption in the index",
			Err:        err,
		}, output.ExitCodeRuntime)
	}

	// 4. Strict parse (TS-014-01-02): the raw document is re-read from
	// the entry's source and validated against the registry metadata
	// schema — the structural decode is never trusted alone.
	md, parseWarnings, err := parseStandardEntry(entry)
	if err != nil {
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("standard %q version %q is invalid", id, version),
			Reason:     err.Error(),
			Resolution: "Fix or remove the metadata document in the index, then run the update again",
			Err:        err,
		})
	}

	// 5. Supported contract majors from the compatibility matrix
	// (T-010 reviewer finding G4 — must-fix): the runtime's supported
	// contract major set is READ from the corpus matrix record at
	// runtime (docs/specification-corpus/compatibility-matrix.json;
	// ADR-029 §3) — never hardcoded, so the engine and the corpus
	// cannot drift. The matrix is a local read; an unreadable matrix
	// aborts the update with an actionable error: supported majors are
	// never silently defaulted (PM binding decision 3).
	supportedContractMajors, err := supportedContractMajors()
	if err != nil {
		return ReportError(cmd, &output.AppError{
			Message:    "could not load the compatibility matrix",
			Reason:     err.Error(),
			Resolution: "Set the " + registry.EnvCompatibilityMatrix + " environment variable to the corpus matrix file (docs/specification-corpus/compatibility-matrix.json), or run the update from the repository root",
			Err:        err,
		})
	}

	// 6. Project framework version gate (PM binding decision 3): the
	// project's framework version is read from the project config.
	// Declared-but-undeterminable rejects the update (never assumed,
	// Transition Plan A2); framework-free projects proceed shape-only,
	// recorded explicitly (ADR-026).
	projectFrameworkVersion, err := projectFrameworkVersionForInstall()
	if err != nil {
		return ReportError(cmd, &output.AppError{
			Message:    "could not determine the project's framework version",
			Reason:     err.Error(),
			Resolution: "Declare the framework version in anvil.yaml (framework.<name>.version), or update in a framework-free project (shape-only validation)",
			Err:        err,
		})
	}

	// 7. Adoption validation, pre-fetch phase (TS-014-04-03) on the
	// TARGET entry: inside the component the lifecycle gate runs BEFORE
	// the compatibility validation. A retired target — not offered for
	// adoption (ADR-027 §3) — fails here with an actionable error
	// distinguishing retired from not-found; a deprecated target is
	// adoptable and the update proceeds (the deprecation warning is
	// surfaced at the record step). A failure aborts the update before
	// any content is fetched (the pinned adoption order — a
	// compatibility failure means zero fetches).
	before := registry.ValidateAdoptionBeforeFetch(*md, supportedContractMajors, projectFrameworkVersion)
	if !before.Valid {
		if !before.Adoptable {
			// The lifecycle gate failed (retired, or an unknown state):
			// the release is not offered for adoption. The component's
			// message distinguishes retired from not-found.
			return ReportError(cmd, &output.AppError{
				Message:    fmt.Sprintf("standard %q version %q is not offered for adoption", id, version),
				Reason:     strings.Join(before.Errors, "\n"),
				Resolution: "Run 'anvil standard list' to see the standards offered for adoption, or choose another version",
			})
		}
		// Compatibility failed: the update aborts BEFORE any content is
		// fetched (the pinned adoption order — a compatibility failure
		// means zero fetches). The installed record is unchanged.
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("standard %q version %q is not compatible", id, version),
			Reason:     strings.Join(before.Errors, "\n"),
			Resolution: "If you are the publisher, resolve the compatibility problems listed above; otherwise choose another version or report the standard to its publisher",
		})
	}

	// 8. Content location resolution (TS-014-02-03). Resolution
	// defensively re-validates the https-only rules; the fetch boundary
	// re-checks them again.
	location, err := registry.ResolveLocation(entry)
	if err != nil {
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("the release content of standard %q version %q cannot be resolved", id, version),
			Reason:     err.Error(),
			Resolution: "Fix the metadata document's distribution declaration, then run the update again",
			Err:        err,
		})
	}

	// 9. Trust anchors BEFORE the fetch (same order as install): a
	// missing or corrupt anchors file fails fast without wasting up to
	// 1 GiB / 60s of download. VerifyTrust still runs over the fetched
	// content AFTER the fetch — the adoption order is unchanged.
	anchorsPath, err := standardTrustAnchorsPath(cmd)
	if err != nil {
		return ReportError(cmd, &output.AppError{
			Message:    "could not resolve the trust anchors path",
			Reason:     err.Error(),
			Resolution: "Configure the trust anchors file, or point the update at one with --trust-anchors <path> or the " + registry.EnvTrustAnchors + " environment variable",
			Err:        err,
		})
	}
	anchors, err := registry.LoadTrustAnchors(anchorsPath)
	if err != nil {
		if errors.Is(err, registry.ErrTrustAnchorsNotFound) {
			return ReportError(cmd, &output.AppError{
				Message:    "no trust anchors file found",
				Reason:     err.Error(),
				Resolution: "Configure the publisher's public key at " + anchorsPath + ", or point the update at a different allowlist with --trust-anchors <path> or the " + registry.EnvTrustAnchors + " environment variable",
				Err:        err,
			})
		}
		return ReportError(cmd, &output.AppError{
			Message:    "could not load the trust anchors file",
			Reason:     err.Error(),
			Resolution: "Fix the trust anchors file, or point the update at a different allowlist with --trust-anchors <path> or the " + registry.EnvTrustAnchors + " environment variable",
			Err:        err,
		})
	}

	// 10. Fetch the release content (ADR-030): the SAME fetch boundary
	// as the install (reused fetchStandardContent + the shared client):
	// https-only, userinfo rejected, bounded redirects, size cap during
	// download, shared timeout. The final response URL (after any
	// allowed redirects) is the actual endpoint used and is recorded as
	// the new explicit resolution. A fetch failure aborts the update —
	// the old record stays intact.
	content, contentSource, err := fetchStandardContent(location.Location)
	if err != nil {
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("could not fetch the release content of standard %q version %q", id, version),
			Reason:     err.Error(),
			Resolution: "If you are the publisher, fix the release asset at the declared distribution location; otherwise choose another version or report the broken release",
			Err:        err,
		})
	}

	// 11. Adoption validation, post-fetch phase (TS-014-04-03): trust
	// verification (TS-014-04-02 / ADR-022) over the fetched content —
	// the ONLY gate: no skip/insecure/no-verify flag and no privileged
	// path. A failure aborts the update and the record is unchanged.
	adoption := registry.ValidateAdoptionAfterFetch(*md, content, anchors, before)
	if !adoption.Valid {
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("trust verification failed for standard %q version %q", id, version),
			Reason:     strings.Join(adoption.Errors, "\n"),
			Resolution: "Do not adopt content that fails verification; if you are the publisher, resolve the trust problems listed above; otherwise choose another version or report the standard to its publisher",
		})
	}

	// 12. Record the update (TS-014-03-03): atomic replace with the new
	// pinned version, the new explicit resolution, the target's
	// lifecycle state, and the freshly embedded validation results.
	// installedAt preserved, updatedAt refreshed; the old record stays
	// intact on failure.
	return recordStandardUpdate(cmd, *md, existing, contentSource, adoption, parseWarnings)
}

// recordStandardUpdate persists the updated installed-standard record.
// Semantics (PM binding decision 6; TS-014-03-03):
//
//   - the record is replaced ATOMICALLY via InstalledStandardStore.Update
//     (temp file + rename + fsync): a failed update leaves the old
//     record intact;
//   - the new record carries the TARGET version, the target's declared
//     contract version, the new explicit resolution (the ACTUAL endpoint
//     the content was fetched from — the final response URL after any
//     allowed redirects, ADR-022 §3), the target's lifecycle state, and
//     the freshly embedded CompatibilityResult and TrustResult from the
//     combined adoption record (TS-014-04-03);
//   - installedAt is PRESERVED from the existing record (the original
//     install time is never rewritten); updatedAt is refreshed with this
//     adoption event;
//   - updating to the version already installed is idempotent (PM
//     binding decision 7): the full validation already ran above (every
//     adoption validates), the record's validation results are refreshed
//     via Update, and the command reports "already at version X
//     (re-validated)";
//   - a deprecated target keeps its deprecated lifecycle state in the
//     record (the update is the explicit adoption event), so the updated
//     record immediately receives no further updates (self-enforcing).
func recordStandardUpdate(cmd *cobra.Command, md registry.Metadata, existing registry.InstalledStandardRecord, contentSource string, adoption registry.AdoptionResult, parseWarnings []registry.Warning) error {
	// The combined adoption record must carry the trust result: the
	// post-fetch phase (ValidateAdoptionAfterFetch, TS-014-04-03) runs
	// trust validation and attaches it before any record is written. A
	// pre-fetch-only result (Trust nil) means the adoption never
	// completed — recording it would persist a half-validated adoption.
	// This guard fails fast with an actionable error instead of
	// panicking.
	if adoption.Trust == nil {
		return ReportError(cmd, &output.AppError{
			Message:    "the adoption record carries no trust result",
			Reason:     "trust validation (the post-fetch adoption phase, TS-014-04-03) must run before the update is recorded; a pre-fetch-only adoption result was passed to the record step",
			Resolution: "Run the update again — every adoption runs both validation phases; if this persists, report it as a defect",
		})
	}

	storeDir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not resolve the installed-standards directory: %v", err)
	}

	now := time.Now().UTC()
	rec := registry.InstalledStandardRecord{
		FormatVersion: registry.RecordFormatVersion,
		ID:            md.ID,
		Version:       md.Version,
		// The declared contract version of the TARGET release — the
		// compatibility target recorded at adoption (ADR-024 §3.1).
		ContractVersion: md.ContractVersion,
		Resolution: registry.Resolution{
			// The updated content came from the distribution location on
			// the standard's release channel; the recorded source is the
			// ACTUAL endpoint used — the final https URL after any
			// allowed redirects (ADR-022 §3).
			Kind:   registry.ResolutionKindDistribution,
			Source: contentSource,
		},
		// The lifecycle state of the TARGET release (ADR-023 §3,
		// ADR-027 §3): a deprecated target is recorded as deprecated —
		// the explicit adoption event — and receives no further updates.
		Lifecycle: md.Lifecycle,
		// installedAt is PRESERVED: the original install time is never
		// rewritten by an update (TS-014-03-03).
		InstalledAt: existing.InstalledAt,
		// updatedAt is refreshed with this adoption event.
		UpdatedAt:     now,
		Compatibility: adoption.CompatibilityRecord(),
		Trust:         adoption.TrustRecord(),
	}

	store := registry.NewInstalledStandardStore(storeDir)
	if _, err := store.Update(md.ID, rec); err != nil {
		return ReportPlainErrorf(cmd, err, "could not update the installed-standard record: %v", err)
	}

	alreadyAtVersion := existing.Version == md.Version
	result := standardUpdateResult{
		ID:               md.ID,
		Version:          md.Version,
		ContractVersion:  md.ContractVersion,
		Resolution:       rec.Resolution,
		Lifecycle:        md.Lifecycle,
		InstalledAt:      rec.InstalledAt,
		UpdatedAt:        rec.UpdatedAt,
		AlreadyAtVersion: alreadyAtVersion,
		Compatibility:    adoption.Compatibility,
		Trust:            *adoption.Trust,
		RecordPath:       filepath.Join(storeDir, md.ID+".json"),
	}
	// Advisory warnings: the strict-parse warnings (TS-014-01-02) and the
	// lifecycle deprecation warning (TS-014-01-03). A deprecated TARGET
	// version updates WITH a warning — removal date and no-updates note —
	// and keeps its lifecycle state in the record (PM binding decision 4).
	for _, w := range parseWarnings {
		result.Warnings = append(result.Warnings, w.Message)
	}
	if w, ok := registry.LifecycleWarning(md.Lifecycle); ok {
		result.Warnings = append(result.Warnings, w)
	}

	if jsonFlag, _ := cmd.Flags().GetBool("json"); jsonFlag {
		return WriteJSON(cmd, standardUpdateJSONFromResult(result))
	}
	renderStandardUpdate(cmd, result)
	return nil
}

// ── Output ───────────────────────────────────────────────────────────

// standardUpdateJSON is the machine-readable update output: identity,
// pinned target version, declared contract version, explicit resolution,
// lifecycle, the preserved install time and refreshed update time, the
// already-at-version marker, warnings, and the embedded validation
// results (recorded exactly as persisted).
type standardUpdateJSON struct {
	ID               string                       `json:"id"`
	Version          string                       `json:"version"`
	ContractVersion  string                       `json:"contract_version"`
	Resolution       standardResolutionJSON       `json:"resolution"`
	Lifecycle        standardLifecycleJSON        `json:"lifecycle"`
	InstalledAt      string                       `json:"installed_at"`
	UpdatedAt        string                       `json:"updated_at"`
	AlreadyAtVersion bool                         `json:"already_at_version"`
	Warnings         []string                     `json:"warnings,omitempty"`
	Compatibility    registry.CompatibilityResult `json:"compatibility"`
	Trust            registry.TrustResult         `json:"trust"`
	RecordPath       string                       `json:"record_path"`
}

// standardUpdateJSONFromResult converts the update result into its
// machine-readable shape. Timestamps are RFC 3339 (the record
// persistence shape).
func standardUpdateJSONFromResult(result standardUpdateResult) standardUpdateJSON {
	return standardUpdateJSON{
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
		AlreadyAtVersion: result.AlreadyAtVersion,
		Warnings:         result.Warnings,
		Compatibility:    result.Compatibility,
		Trust:            result.Trust,
		RecordPath:       result.RecordPath,
	}
}

// renderStandardUpdate writes the human-readable update report. The
// report surfaces the record semantics explicitly — the original install
// time (preserved) and the update time (refreshed) — plus the validation
// summary (including the framework-not-checked state of a framework-free
// project, never hidden) and the deprecation warning section for
// deprecated targets.
func renderStandardUpdate(cmd *cobra.Command, result standardUpdateResult) {
	w := cmd.OutOrStdout()
	if result.AlreadyAtVersion {
		fmt.Fprintf(w, "Standard %s is already at version %s (re-validated).\n", result.ID, result.Version)
	} else {
		fmt.Fprintf(w, "Updated standard: %s %s\n", result.ID, result.Version)
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

	fmt.Fprintln(w, "Installed At (original install):")
	fmt.Fprintf(w, "  %s\n", result.InstalledAt.UTC().Format(time.RFC3339))
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Updated At (this update):")
	fmt.Fprintf(w, "  %s\n", result.UpdatedAt.UTC().Format(time.RFC3339))
	fmt.Fprintln(w)

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
