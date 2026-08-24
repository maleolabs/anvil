// Package cmd implements the Anvil CLI commands.
//
// ── Standard Shared (TS-014-02-02, TS-014-03-01, TS-014-03-02) ──────
//
// Shared plumbing for the "anvil standard" commands: static index path
// resolution (--index flag → ANVIL_REGISTRY_INDEX → default), the
// strict-parse wiring that every surfaced metadata document must pass
// (TS-014-01-02), the presentation view both discovery commands render
// from, and the adoption plumbing shared by the install flow
// (TS-014-03-01) and the update flow (TS-014-03-02): the supported
// contract majors reader (compatibility matrix), the project framework
// version reader, the trust anchors path resolver, and the release
// content fetch boundary with its https-only / userinfo-rejected /
// bounded-redirect / size-capped / timed policy (ADR-030 §3).
//
// Reference: TS-014-02-02, TS-014-03-01, TS-014-03-02, TS-014-01-02,
// TS-014-01-03, ADR-023, ADR-026, ADR-030
package cmd

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	"maleolabs.com/anvil/internal/config"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/project"
	"maleolabs.com/anvil/internal/registry"
)

// ── Index Path Resolution ────────────────────────────────────────────

// envStandardIndex names the environment variable that overrides the
// default static registry index directory.
//
// Reference: TS-014-02-02 (PM decision: ANVIL_REGISTRY_INDEX)
const envStandardIndex = "ANVIL_REGISTRY_INDEX"

// standardIndexSource identifies where a resolved index path came from.
type standardIndexSource string

const (
	// standardIndexFlag: the --index flag was passed explicitly.
	standardIndexFlag standardIndexSource = "flag"

	// standardIndexEnv: the ANVIL_REGISTRY_INDEX environment variable.
	standardIndexEnv standardIndexSource = "environment"

	// standardIndexDefault: the documented default directory.
	standardIndexDefault standardIndexSource = "default"
)

// defaultStandardIndex returns the default static index directory: the
// Anvil global config directory (os.UserConfigDir()/anvil, the ADR-005
// §7.1 convention implemented by config.GlobalConfigDir) plus the
// "registry" subdirectory. On Linux this resolves to
// ~/.config/anvil/registry (XDG_CONFIG_HOME aware); on macOS to
// ~/Library/Application Support/anvil/registry; on Windows to
// %AppData%/anvil/registry.
//
// Reference: TS-014-02-02, ADR-005 §7.1
func defaultStandardIndex() (string, error) {
	dir, err := config.GlobalConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve default registry index directory: %w", err)
	}
	return filepath.Join(dir, "registry"), nil
}

// resolveStandardIndex resolves the static index directory for the
// standard commands, in order:
//
//  1. the explicit --index flag value (non-empty);
//  2. the ANVIL_REGISTRY_INDEX environment variable (non-empty);
//  3. the documented default (defaultStandardIndex).
//
// getenv is injected for testability. The source of the resolution is
// returned alongside the path.
func resolveStandardIndex(flagValue string, flagSet bool, getenv func(string) string) (string, standardIndexSource, error) {
	if flagSet && flagValue != "" {
		return flagValue, standardIndexFlag, nil
	}
	if value := getenv(envStandardIndex); value != "" {
		return value, standardIndexEnv, nil
	}
	path, err := defaultStandardIndex()
	if err != nil {
		return "", standardIndexDefault, err
	}
	return path, standardIndexDefault, nil
}

// standardIndexPath resolves the index path for a standard command from
// its own flags and the process environment.
func standardIndexPath(cmd *cobra.Command) (string, error) {
	flagSet := FlagIsSet(cmd, "index")
	flagValue, _ := cmd.Flags().GetString("index")
	path, _, err := resolveStandardIndex(flagValue, flagSet, os.Getenv)
	return path, err
}

// reportStandardIndexError renders a registry index load/resolution
// failure with actionable first-run guidance about how the index path is
// chosen. A missing index directory is a not-found failure (exit 3,
// ExitCodeRuntime — "resource not found", TS-P8-07 / ADR-010 §8.1); any
// other index failure (unreadable, malformed, duplicate documents) is a
// general error (exit 1). The resolution is a concrete first-run hint:
// the registry is a decentralized, static index with no bundled or
// canonical hosted directory (ADR-030), so the message names the two
// supported mechanisms — the --index flag and the ANVIL_REGISTRY_INDEX
// environment variable — and the resolved default (ST-021-05).
func reportStandardIndexError(cmd *cobra.Command, err error) error {
	exitCode := output.ExitCodeGeneral
	if errors.Is(err, registry.ErrIndexNotFound) {
		exitCode = output.ExitCodeRuntime
	}
	return ReportErrorWithCode(cmd, &output.AppError{
		Message:    "could not load the registry index",
		Reason:     err.Error(),
		Resolution: "Point the command at a static registry index directory with --index <path> or the " + envStandardIndex + " environment variable, then retry — there is no bundled index; the default is " + defaultStandardIndexDescription(),
		Err:        err,
	}, exitCode)
}

// standardIndexSetupHint renders the single actionable first-run hint
// for a list command whose index is unavailable (ST-021-05): how to point
// the standard commands at a static index directory. There is no bundled
// index and no canonical hosted one (ADR-030), so the hint names the two
// supported mechanisms — the --index flag and the ANVIL_REGISTRY_INDEX
// environment variable — in one line.
func standardIndexSetupHint() string {
	return "Tip: point at a static registry index directory with --index <path> or " + envStandardIndex + "=<path> to see the standards offered for adoption."
}

// defaultStandardIndexDescription renders the default index path for
// error messages; on resolution failure the path itself is described in
// prose.
func defaultStandardIndexDescription() string {
	path, err := defaultStandardIndex()
	if err != nil {
		return "<user config dir>/anvil/registry"
	}
	return path
}

// ── Parse Wiring (TS-014-01-02) ──────────────────────────────────────

// parseStandardEntry enforces registry.Parse on one index entry before
// it is surfaced by discovery: the raw document is re-read from the
// entry's source path — the index client performs structural decode only
// (TS-014-02-01), so strict validation lands here, at the discovery
// surface — and validated against the registry metadata schema. It
// returns the parsed metadata and any advisory warnings, or an error
// identifying the entry and the validation problem.
//
// Reference: TS-014-02-02 (product hand-off T-002), TS-014-01-02
func parseStandardEntry(entry registry.Entry) (*registry.Metadata, []registry.Warning, error) {
	raw, err := os.ReadFile(entry.Source)
	if err != nil {
		return nil, nil, fmt.Errorf("standard %q version %q: read index document %s: %w", entry.ID, entry.Version, entry.Source, err)
	}
	result, err := registry.Parse(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("standard %q version %q (index document %s) failed registry validation: %w", entry.ID, entry.Version, entry.Source, err)
	}
	return result.Metadata, result.Warnings, nil
}

// ── Presentation View ────────────────────────────────────────────────

// standardEntryView is the discovery surface's presentation view of one
// index entry after strict parsing: the parsed metadata plus the
// lifecycle-derived presentation data (warning text, adoption
// eligibility) and the parse-failure marker. Lifecycle decisions use
// LifecycleAdoptable and Lifecycle.State/RemovalDate directly — never an
// inference from the presence of a warning (TS-014-01-03).
type standardEntryView struct {
	ID                   string
	Version              string
	ContractVersion      string
	Capability           []string
	LifecycleState       string
	RemovalDate          string
	DistributionType     string
	DistributionLocation string
	TrustContentDigests  bool
	TrustAttestation     bool
	Warnings             []string
	Adoptable            bool
	Invalid              bool
	ParseError           string
	Source               string
}

// standardEntryViewFromIndex builds the view for one index entry. The
// document must pass registry.Parse first; a document that fails
// validation yields an invalid view carrying the actionable problem —
// it is never silently dropped.
func standardEntryViewFromIndex(entry registry.Entry) standardEntryView {
	view := standardEntryView{
		ID:      entry.ID,
		Version: entry.Version,
		Source:  entry.Source,
	}
	md, warnings, err := parseStandardEntry(entry)
	if err != nil {
		view.Invalid = true
		view.ParseError = err.Error()
		return view
	}
	view.ContractVersion = md.ContractVersion
	view.Capability = md.Capability.FrameworkVersion
	view.LifecycleState = md.Lifecycle.State
	view.RemovalDate = md.Lifecycle.RemovalDate
	view.DistributionType = md.Distribution.Type
	view.DistributionLocation = md.Distribution.Location
	view.TrustContentDigests = len(md.Trust.ContentDigests) > 0
	view.TrustAttestation = md.Trust.Attestation.Signature != "" && md.Trust.Attestation.PublicKey != ""
	for _, w := range warnings {
		view.Warnings = append(view.Warnings, w.Message)
	}
	if warning, ok := registry.LifecycleWarning(md.Lifecycle); ok {
		view.Warnings = append(view.Warnings, warning)
	}
	view.Adoptable = registry.LifecycleAdoptable(md.Lifecycle.State)
	return view
}

// ── Install Record Persistence (shared by install flows) ─────────────

// persistStandardInstallRecord persists the installed-standard record of
// one adoption (TS-014-03-03). It is the single record-persistence path
// shared by the online install flow (T-007 / recordStandardInstall) and
// the offline/bundled install flow (TS-014-05-02 /
// recordStandardBundleInstall): the resolution-specific record — already
// carrying its explicit resolution, whose kind and source differ per
// flow (distribution URL vs bundle path) — is passed in WITHOUT install
// timestamps, and the helper stamps them per the path taken. Semantics
// (ADR-023 §3):
//
//   - no record: a fresh install — Record writes installedAt = updatedAt;
//   - same identity and version already recorded: idempotent success —
//     the full validation already ran (every adoption validates, in the
//     caller), the record's validation results AND its resolution source
//     are refreshed via Update (installedAt preserved, updatedAt
//     re-stamped — last adoption wins), and the result carries
//     AlreadyInstalled so the command reports "already installed at
//     <version> (re-validated)";
//   - a different version already recorded: rejected with an actionable
//     error — version change is an update, an explicit adoption event of
//     the update flow (TS-014-03-02); this flow never updates;
//   - a corrupt record: replaced by the explicit install (recovery by
//     re-adoption).
//
// The compatibility and trust results embedded in the record and
// surfaced on the result come from the combined adoption record
// (TS-014-04-03); the caller must pass a COMPLETED adoption result
// (Trust attached — the post-fetch/verify phase; see
// registry.CompleteAdoption). The helper guards this itself: a
// pre-fetch-only result (Trust nil) is rejected with an actionable
// error BEFORE any store work — recording it would persist a
// half-validated adoption, and the helper is the single persistence
// path, so no caller (current install flows or future ones, e.g. the
// update flow) can panic on the nil dereference. Advisory warnings: the
// strict-parse warnings (TS-014-01-02) and the lifecycle deprecation
// warning (TS-014-01-03) are collected onto the result; a deprecated
// release installs WITH a warning and keeps its lifecycle state in the
// record.
//
// The returned result is rendered by the caller (human-readable report
// or the --json envelope) — output conventions are the caller's
// surface. Store/record errors are rendered and returned as-is.
//
// Reference: TS-014-03-03, TS-014-03-01, TS-014-05-02, ADR-022 §3,
// ADR-023 §3
func persistStandardInstallRecord(cmd *cobra.Command, rec registry.InstalledStandardRecord, adoption registry.AdoptionResult, parseWarnings []registry.Warning) (standardInstallResult, error) {
	// Self-protecting guard: the combined adoption record must carry the
	// trust result — the post-fetch/verification phase (TS-014-04-03)
	// attaches it before any record is written. A pre-fetch-only result
	// (Trust nil) means the adoption never completed; recording it would
	// persist a half-validated adoption, and assembling the result would
	// dereference a nil pointer. This guard fails fast with an actionable
	// error instead of panicking, and protects every caller — including
	// future ones (e.g. the update flow, T-008) that might pass a
	// pre-fetch result by mistake.
	if adoption.Trust == nil {
		return standardInstallResult{}, ReportError(cmd, &output.AppError{
			Message:    "the adoption record carries no trust result",
			Reason:     "trust validation (the post-fetch adoption phase, TS-014-04-03) must run before the install is recorded; a pre-fetch-only adoption result was passed to the record step",
			Resolution: "Run the install again — every adoption runs both validation phases; if this persists, report it as a defect",
		})
	}

	storeDir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		return standardInstallResult{}, ReportPlainErrorf(cmd, err, "could not resolve the installed-standards directory: %v", err)
	}

	now := time.Now().UTC()
	store := registry.NewInstalledStandardStore(storeDir)
	existing, err := store.Get(rec.ID)
	alreadyInstalled := false
	switch {
	case err == nil:
		if existing.Version != rec.Version {
			// Conflicting state: a different version is already installed.
			// Per the truthful exit-code mapping (TS-019-03-02, D-06) a
			// version conflict is a configuration conflict → exit 2.
			return standardInstallResult{}, ReportErrorWithCode(cmd, &output.AppError{
				Message:    fmt.Sprintf("standard %q is already installed at version %q, not %q", rec.ID, existing.Version, rec.Version),
				Reason:     "installing a different version is an update, an explicit adoption event handled by the update flow (TS-014-03-02); this install flow never updates",
				Resolution: "Run the update flow to change the installed version, or install the already-recorded version " + existing.Version + " (idempotent re-install)",
			}, output.ExitCodeConfig)
		}
		// Idempotent success: the same identity plus version is already
		// installed. Every adoption validates — the full validation ran
		// in the caller — so the record's validation results are
		// refreshed and updatedAt re-stamped; installedAt (the original
		// install time) is preserved.
		alreadyInstalled = true
		rec.InstalledAt = existing.InstalledAt
		rec.UpdatedAt = now
		if _, err := store.Update(rec.ID, rec); err != nil {
			return standardInstallResult{}, ReportPlainErrorf(cmd, err, "could not re-validate the installed-standard record: %v", err)
		}
	case errors.Is(err, registry.ErrRecordCorrupt):
		// A corrupt record cannot be compared for idempotency; the
		// explicit install replaces it (recovery by re-adoption,
		// TS-014-03-03).
		rec.InstalledAt = now
		rec.UpdatedAt = now
		if _, _, err := store.Record(rec.ID, rec); err != nil {
			return standardInstallResult{}, ReportPlainErrorf(cmd, err, "could not record the installed standard: %v", err)
		}
	case errors.Is(err, registry.ErrRecordNotFound):
		// Fresh install: the first adoption event, so updatedAt equals
		// installedAt at creation.
		rec.InstalledAt = now
		rec.UpdatedAt = now
		if _, _, err := store.Record(rec.ID, rec); err != nil {
			return standardInstallResult{}, ReportPlainErrorf(cmd, err, "could not record the installed standard: %v", err)
		}
	default:
		return standardInstallResult{}, ReportPlainErrorf(cmd, err, "could not read the installed-standard record: %v", err)
	}

	result := standardInstallResult{
		ID:               rec.ID,
		Version:          rec.Version,
		ContractVersion:  rec.ContractVersion,
		Resolution:       rec.Resolution,
		Lifecycle:        rec.Lifecycle,
		InstalledAt:      rec.InstalledAt,
		UpdatedAt:        rec.UpdatedAt,
		AlreadyInstalled: alreadyInstalled,
		Compatibility:    adoption.Compatibility,
		Trust:            *adoption.Trust,
		RecordPath:       filepath.Join(storeDir, rec.ID+".json"),
	}
	for _, w := range parseWarnings {
		result.Warnings = append(result.Warnings, w.Message)
	}
	if w, ok := registry.LifecycleWarning(rec.Lifecycle); ok {
		result.Warnings = append(result.Warnings, w)
	}
	return result, nil
}

// ── Semver Ordering ──────────────────────────────────────────────────

// sortStandardVersions orders the release versions of a standard
// semantically (ascending by major.minor.patch), so discovery surfaces
// show 1.2.3 before 1.10.0 — the index client's lexical order is a
// documented index-client scope (TS-014-02-01), ordering for display is
// the discovery surface's responsibility. A version that does not parse
// as semver sorts lexically relative to the other versions, keeping the
// ordering total and deterministic.
func sortStandardVersions(versions []string) []string {
	sorted := append([]string(nil), versions...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return standardVersionLess(sorted[i], sorted[j])
	})
	return sorted
}

// standardVersionLess reports whether version a sorts before version b:
// numerically when both parse as semver triplets, lexically otherwise.
func standardVersionLess(a, b string) bool {
	ma, aok := parseSemverTriplet(a)
	mb, bok := parseSemverTriplet(b)
	switch {
	case aok && bok:
		for i := 0; i < 3; i++ {
			if ma[i] != mb[i] {
				return ma[i] < mb[i]
			}
		}
		return false // equal versions — stable sort preserves order
	case aok:
		return true // valid semvers sort before non-semver strings
	case bok:
		return false
	default:
		return a < b
	}
}

// parseSemverTriplet parses major.minor.patch as three non-negative
// integers. Leading zeros are rejected like the registry schema pattern,
// so only well-formed registry versions compare numerically.
func parseSemverTriplet(version string) ([3]int, bool) {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return [3]int{}, false
	}
	var out [3]int
	for i, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return [3]int{}, false
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}

// ── Adoption-Target Resolution (ST-021-05) ───────────────────────────

// standardLatestRelease is the outcome of resolving the latest published
// release of a standard from the index: the resolved entry plus its
// strict-parse result. The parse already ran during resolution — the
// lifecycle filter needs the strict-parsed lifecycle state — so the
// adoption flow reuses the result instead of re-parsing the document.
type standardLatestRelease struct {
	Entry    registry.Entry
	Metadata registry.Metadata
	Warnings []registry.Warning
}

// resolveStandardLatest resolves the latest published release of the
// standard id from the index (ST-021-05): candidates are scanned in
// descending semver order and the first strict-parse-valid release
// offered for fresh adoption wins (LifecycleAdoptable — published or
// deprecated; retired releases are never resolvable, ADR-027 §3). The
// resolved release is then pinned exactly like an explicit version —
// ADR-022 §3 pinning is unchanged, only the version choice is automated.
//
// A candidate at the top of the version ladder that fails strict registry
// validation (TS-014-01-02) fails the resolution with the actionable
// validation problem: the invalid release is never silently skipped in
// favor of an older version, mirroring the explicit-version path. When
// every release is retired, the error says so explicitly.
func resolveStandardLatest(ix *registry.Index, id string) (standardLatestRelease, error) {
	sorted := sortStandardVersions(ix.Versions(id))
	if len(sorted) == 0 {
		return standardLatestRelease{}, fmt.Errorf(
			"%w: standard %q is not in the index", registry.ErrEntryNotFound, id)
	}

	var retired []string
	for i := len(sorted) - 1; i >= 0; i-- {
		version := sorted[i]
		entry, err := ix.Resolve(id, version)
		if err != nil {
			// Enumerating a loaded index cannot fail; guard against
			// future lazy enumeration sources.
			return standardLatestRelease{}, fmt.Errorf("resolve %s %s: %w", id, version, err)
		}
		md, warnings, err := parseStandardEntry(entry)
		if err != nil {
			// The newest resolvable release is invalid (any newer
			// candidates were already skipped as retired): surface the
			// problem instead of silently adopting an older version.
			return standardLatestRelease{}, fmt.Errorf(
				"the newest resolvable release of standard %q (version %q) is invalid: %w", id, version, err)
		}
		if !registry.LifecycleAdoptable(md.Lifecycle.State) {
			retired = append(retired, version)
			continue
		}
		return standardLatestRelease{Entry: entry, Metadata: *md, Warnings: warnings}, nil
	}
	return standardLatestRelease{}, fmt.Errorf(
		"standard %q has no release offered for adoption: every version in the index is retired (%s) — retired releases are never resolvable",
		id, strings.Join(retired, ", "))
}

// standardTarget is the resolved adoption target of an install or update
// flow: the resolved entry, its strict-parse result, the resolved
// version, and the latest-resolution marker.
type standardTarget struct {
	Entry          registry.Entry
	Metadata       registry.Metadata
	ParseWarnings  []registry.Warning
	Version        string
	ResolvedLatest bool
}

// resolveStandardTarget resolves the adoption target of an install or
// update flow from the loaded index (ST-021-05): the explicit version
// when version is non-empty, otherwise the latest published release
// (resolveStandardLatest). The strict parse runs inside resolution — the
// latest path's lifecycle filter needs the strict-parsed lifecycle state
// — and the caller reuses the result instead of re-parsing. Failure
// rendering is centralized here so both flows share one error mapping
// (not-found → exit 3; unresolvable latest — invalid newest release or
// every release retired — → exit 1). verb names the flow's pin command
// in the actionable hint ("install" or "update").
func resolveStandardTarget(cmd *cobra.Command, ix *registry.Index, id, version, verb string) (standardTarget, error) {
	if version == "" {
		latest, err := resolveStandardLatest(ix, id)
		if err != nil {
			if errors.Is(err, registry.ErrEntryNotFound) {
				return standardTarget{}, ReportErrorWithCode(cmd, &output.AppError{
					Message:    fmt.Sprintf("standard %q not found in the registry index", id),
					Reason:     err.Error(),
					Resolution: "Run 'anvil standard list' to see the standards offered for adoption in the index",
					Err:        err,
				}, output.ExitCodeRuntime)
			}
			// The index holds the standard but no release can be
			// resolved: the newest release is invalid, or every release
			// is retired.
			return standardTarget{}, ReportError(cmd, &output.AppError{
				Message:    fmt.Sprintf("standard %q cannot be resolved to a release", id),
				Reason:     err.Error(),
				Resolution: "Pin an explicit version ('anvil standard " + verb + " " + id + " <version>') after inspecting the standard ('anvil standard inspect " + id + "'), or report the broken index to its publisher",
				Err:        err,
			})
		}
		return standardTarget{
			Entry:          latest.Entry,
			Metadata:       latest.Metadata,
			ParseWarnings:  latest.Warnings,
			Version:        latest.Metadata.Version,
			ResolvedLatest: true,
		}, nil
	}

	entry, err := ix.Resolve(id, version)
	if err != nil {
		return standardTarget{}, ReportErrorWithCode(cmd, &output.AppError{
			Message:    fmt.Sprintf("standard %q version %q not found", id, version),
			Reason:     err.Error(),
			Resolution: "Run 'anvil standard list' to see the standards offered for adoption in the index",
			Err:        err,
		}, output.ExitCodeRuntime)
	}
	md, parseWarnings, err := parseStandardEntry(entry)
	if err != nil {
		return standardTarget{}, ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("standard %q version %q is invalid", id, version),
			Reason:     err.Error(),
			Resolution: "Fix or remove the metadata document in the index, then run the " + verb + " again",
			Err:        err,
		})
	}
	return standardTarget{
		Entry:         entry,
		Metadata:      *md,
		ParseWarnings: parseWarnings,
		Version:       version,
	}, nil
}

// standardCapabilityCell renders the capability column value: the
// supported framework versions joined, or "-" when none are declared
// (invalid entries carry no trustworthy capability).
func standardCapabilityCell(view standardEntryView) string {
	if len(view.Capability) == 0 {
		return "-"
	}
	return strings.Join(view.Capability, ", ")
}

// standardStatusCell renders the status column value. Deprecated entries
// carry their announced removal date in the status; invalid entries are
// marked explicitly.
func standardStatusCell(view standardEntryView) string {
	if view.Invalid {
		return "invalid"
	}
	if view.LifecycleState == registry.LifecycleStateDeprecated {
		if view.RemovalDate != "" {
			return "deprecated (removal " + view.RemovalDate + ")"
		}
		return "deprecated"
	}
	return view.LifecycleState
}

// renderStandardWarnings writes the deprecation/advisory warnings section
// for the views that carry warnings; nothing is written when there are
// none.
func renderStandardWarnings(w io.Writer, views []standardEntryView) {
	var warned []standardEntryView
	for _, view := range views {
		if len(view.Warnings) > 0 {
			warned = append(warned, view)
		}
	}
	if len(warned) == 0 {
		return
	}
	fmt.Fprintln(w, "Warnings:")
	for _, view := range warned {
		for _, message := range view.Warnings {
			fmt.Fprintf(w, "  %s %s: %s\n", view.ID, view.Version, message)
		}
	}
	fmt.Fprintln(w)
}

// renderStandardProblems writes the invalid-entries section: every
// surfaced entry that failed strict registry validation, with its
// actionable problem. The section is omitted when every entry parsed.
func renderStandardProblems(w io.Writer, views []standardEntryView) {
	var broken []standardEntryView
	for _, view := range views {
		if view.Invalid {
			broken = append(broken, view)
		}
	}
	if len(broken) == 0 {
		return
	}
	fmt.Fprintln(w, "Invalid entries (not offered for adoption):")
	for _, view := range broken {
		fmt.Fprintf(w, "  %s %s (%s):\n", view.ID, view.Version, view.Source)
		for _, line := range strings.Split(strings.TrimSpace(view.ParseError), "\n") {
			fmt.Fprintf(w, "    %s\n", line)
		}
	}
	fmt.Fprintln(w)
}

// ── Machine-Readable Shapes ──────────────────────────────────────────

// standardLifecycleJSON is the structured lifecycle state of one release
// in machine-readable output.
type standardLifecycleJSON struct {
	State       string `json:"state"`
	RemovalDate string `json:"removal_date,omitempty"`
}

// standardDistributionJSON is the structured distribution location of one
// release in machine-readable output.
type standardDistributionJSON struct {
	Type     string `json:"type"`
	Location string `json:"location"`
}

// standardTrustPresenceJSON reports the trust-material presence of one
// release for transparency (ADR-022): content digests and publisher
// attestation.
type standardTrustPresenceJSON struct {
	ContentDigests bool `json:"content_digests"`
	Attestation    bool `json:"attestation"`
}

// standardListEntry is one row of "anvil standard list" machine-readable
// output (also reused by the inspect overview): id, version, declared
// contract version, capability, structured lifecycle state and removal
// date, distribution location, trust presence, warnings, and — for
// entries that fail strict validation — the explicit invalid marker and
// the validation problem.
type standardListEntry struct {
	ID              string                     `json:"id"`
	Version         string                     `json:"version"`
	ContractVersion string                     `json:"contract_version,omitempty"`
	Capability      []string                   `json:"capability,omitempty"`
	Lifecycle       *standardLifecycleJSON     `json:"lifecycle,omitempty"`
	Distribution    *standardDistributionJSON  `json:"distribution,omitempty"`
	TrustPresence   *standardTrustPresenceJSON `json:"trust_presence,omitempty"`
	Warnings        []string                   `json:"warnings,omitempty"`
	Invalid         bool                       `json:"invalid,omitempty"`
	ValidationError string                     `json:"validation_error,omitempty"`
	Source          string                     `json:"source"`
}

// standardListJSON converts presentation views into machine-readable
// list entries.
func standardListJSON(views []standardEntryView) []standardListEntry {
	entries := make([]standardListEntry, 0, len(views))
	for _, view := range views {
		entries = append(entries, standardListEntryFromView(view))
	}
	return entries
}

// standardListEntryFromView converts one presentation view into its
// machine-readable shape.
func standardListEntryFromView(view standardEntryView) standardListEntry {
	entry := standardListEntry{
		ID:       view.ID,
		Version:  view.Version,
		Source:   view.Source,
		Invalid:  view.Invalid,
		Warnings: view.Warnings,
	}
	if view.Invalid {
		entry.ValidationError = view.ParseError
		return entry
	}
	entry.ContractVersion = view.ContractVersion
	entry.Capability = view.Capability
	entry.Lifecycle = &standardLifecycleJSON{
		State:       view.LifecycleState,
		RemovalDate: view.RemovalDate,
	}
	entry.Distribution = &standardDistributionJSON{
		Type:     view.DistributionType,
		Location: view.DistributionLocation,
	}
	entry.TrustPresence = &standardTrustPresenceJSON{
		ContentDigests: view.TrustContentDigests,
		Attestation:    view.TrustAttestation,
	}
	return entry
}

// ── Adoption Shared (TS-014-03-01 / TS-014-03-02) ────────────────────

// The install flow (TS-014-03-01) and the update flow (TS-014-03-02)
// validate adoptions EXACTLY the same way (ADR-022 §3: trust and
// compatibility are validated together at every install and update).
// The adoption gates below — supported contract majors, project
// framework version, trust anchors path, and the content fetch boundary
// — are shared verbatim between the two flows so the re-validation an
// update performs is byte-for-byte the validation the install performs.
//
// Naming note (PM decision, reviewer F3): the helper names below keep
// their original install-flow names (standardInstallHTTPClient,
// projectFrameworkVersionForInstall) for continuity with the install
// flow's history and tests; they serve BOTH flows — the update flow
// (TS-014-03-02) reuses them verbatim, and renaming would churn both
// flows' tests for cosmetic gain. The one exception is
// supportedContractMajors (formerly ...ForInstall): it is renamed
// because the adoption-time recognition hook (TS-017-01-03, T-007)
// consumes it for contract-version validation at migration — the
// install-only name would be misleading for the shared consumer; the
// rename is mechanical (no behavior change) and no test references the
// old name.

// standardContentMaxBytes caps a single release content download at 1 GiB,
// consistent with the bundle content cap (MaxBundleContentSize). The cap
// is enforced DURING download via a limit reader: content is never
// buffered unbounded. It is a variable so tests can shrink the cap; the
// production value is fixed to the bundle cap.
var standardContentMaxBytes = int64(registry.MaxBundleContentSize)

// standardInstallHTTPClient is the HTTP client for standard release
// content fetches (shared by the install and update flows): the bounded
// download transport (connection-phase timeouts, no total deadline that
// would cut off a slow-but-progressing content download — the body read
// is bounded separately by the idle-timeout reader wrapped in
// standardContentDownload) plus a redirect policy that never follows a
// redirect away from https. It is a package-level seam: tests swap it
// with a client that trusts the local test server.
var standardInstallHTTPClient = newStandardInstallHTTPClient()

// newStandardInstallHTTPClient builds the standard content fetch client.
// The redirect policy is part of the production client: redirects to a
// non-https target are refused outright (release content is resolved
// over TLS only, ADR-030 §3), and the redirect chain is bounded. The
// refusal message renders the target WITHOUT its credentials (QA F-2):
// a redirect to a non-https target carrying userinfo
// (http://alice:secret@host/...) must never echo the credentials — the
// rendered target goes through standardScrubLocation, and Go wraps the
// refusal in a url.Error that keeps the full URL.
//
// Timeout model (TD-008): like downloadClient, this client carries no
// total per-request timeout — release content can legitimately exceed
// 60s to download on a slow link, and a total deadline would cancel the
// request mid-body. The transport bounds the connection phase (dial,
// TLS handshake, response headers via newBoundedTransport) and the body
// read is bounded per-read by the idle timeout (idleTimeoutBody), so a
// stalled fetch still surfaces a clear timeout error.
func newStandardInstallHTTPClient() *http.Client {
	return &http.Client{
		Transport: newBoundedTransport(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" {
				return fmt.Errorf(
					"redirect to %s refused: release content is fetched over TLS only, no plaintext or other scheme (ADR-030 §3)",
					standardScrubLocation(req.URL.String()))
			}
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			return nil
		},
	}
}

// supportedContractMajors loads the runtime's supported contract major
// set for compatibility validation — the adoption-time validation
// (install/update, TS-014-04-01) and the contract-version validation at
// migration (TS-017-01-03, T-007): READ from the compatibility matrix
// record at runtime (docs/specification-corpus/compatibility-matrix.json
// — the corpus reference the declared contract versions are checked
// against; T-010 reviewer finding G4 — must-fix; ADR-029 §3).
// Resolution order (ResolveCompatibilityMatrixPath): the
// ANVIL_COMPATIBILITY_MATRIX environment variable, then the corpus file
// relative to the working directory. A matrix that cannot be read —
// missing, corrupt, or structurally invalid — is an actionable error:
// supported majors are never silently defaulted (PM binding decision 3).
func supportedContractMajors() ([]int, error) {
	path, err := registry.ResolveCompatibilityMatrixPath("", os.Getenv)
	if err != nil {
		return nil, fmt.Errorf("resolve compatibility matrix path: %w", err)
	}
	matrix, err := registry.LoadCompatibilityMatrix(path)
	if err != nil {
		return nil, err
	}
	return matrix.SupportedContractMajors, nil
}

// projectFrameworkVersionForInstall returns the adopting project's
// framework version for capability validation (shared by install and
// update — an update re-validates compatibility against the same project
// state an install would):
//
//   - "" when the project is framework-free — no project found, or the
//     project declares no framework. Capability validation then runs
//     shape-only with FrameworkVersionChecked=false, and the not-checked
//     fact is recorded explicitly (ADR-026; PM binding decision 3);
//   - the declared version when the project declares a framework and
//     anvil.yaml declares its version (framework.<name>.version, the
//     framework config extension convention);
//   - an error when the project declares a framework whose version
//     cannot be determined — the adoption is rejected with an actionable
//     error; compatibility is never assumed (Transition Plan A2).
func projectFrameworkVersionForInstall() (string, error) {
	root, err := project.Discover()
	if err != nil {
		if errors.Is(err, project.ErrNoProjectFound) {
			// Framework-free: no project, no framework declaration.
			return "", nil
		}
		return "", err
	}
	configPath := filepath.Join(root, project.ConfigFileName)

	framework, err := readProjectFramework(configPath)
	if err != nil {
		return "", fmt.Errorf("could not read %s: %w", configPath, err)
	}
	if framework == "" {
		// Framework-free project: shape-only capability validation.
		return "", nil
	}

	version, err := readProjectFrameworkVersion(configPath, framework)
	if err != nil {
		return "", err
	}
	if version == "" {
		return "", fmt.Errorf(
			"the project declares framework %q but its version cannot be determined; the declared capability support scope cannot be checked, and compatibility is never assumed. Declare the framework version in %s under framework.%s.version (e.g. framework.laravel.version: 11.0.0), then re-run the install or update command",
			framework, configPath, framework)
	}
	return version, nil
}

// readProjectFrameworkVersion returns the declared framework version from
// the project config file: the framework config extension convention
// framework.<name>.version (e.g. framework.laravel.version — the Laravel
// adapter's KeyVersion). The key is not part of the canonical config
// schema (it is an adapter-extension key), so it is read from the raw
// YAML document — the same pattern as readProjectFramework. A missing
// key or section yields "".
func readProjectFrameworkVersion(configPath, framework string) (string, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", err
	}
	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("could not parse %s: %w", configPath, err)
	}
	frameworks, ok := doc["framework"].(map[string]interface{})
	if !ok {
		return "", nil
	}
	section, ok := frameworks[framework].(map[string]interface{})
	if !ok {
		return "", nil
	}
	version, _ := section["version"].(string)
	return version, nil
}

// standardTrustAnchorsPath resolves the trust anchors file path for the
// standard adoption commands from their --trust-anchors flag and the
// process environment (ResolveTrustAnchorsPath: flag →
// ANVIL_TRUST_ANCHORS → default <user config dir>/anvil/
// trust-anchors.json).
func standardTrustAnchorsPath(cmd *cobra.Command) (string, error) {
	flagValue, _ := cmd.Flags().GetString("trust-anchors")
	return registry.ResolveTrustAnchorsPath(flagValue, os.Getenv)
}

// fetchStandardContent downloads the release content from the resolved
// https distribution location under the fetch policy (ADR-030 §3):
// https-only (defensively re-checked at the fetch boundary and after
// redirects), userinfo rejected (credentials must never be sent or
// recorded), bounded redirects, the 1 GiB size cap enforced during the
// download via a limit reader, and the download timeout model — the
// connection phase bounded by the transport, the body read bounded by
// the idle window (TD-008). It returns the content and the ACTUAL
// endpoint used — the final response URL after any allowed redirects —
// which the caller records as the explicit resolution (ADR-022 §3). A
// failure — 404, 5xx, DNS, timeout, TLS — returns an actionable error
// naming what failed; the caller aborts the adoption, so no content is
// adopted on failure.
func fetchStandardContent(location string) ([]byte, string, error) {
	// Defensive https re-check: parse and ResolveLocation already enforce
	// https-only and reject userinfo, but the fetch is the security
	// boundary — never issue a plaintext request or send embedded
	// credentials even if a future index source bypasses both.
	parsed, err := url.Parse(location)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, "", fmt.Errorf(
			"the distribution location %s is not a well-formed https URL; release content is resolved over TLS only (ADR-030 §3)",
			standardScrubLocation(location))
	}
	if parsed.User != nil {
		return nil, "", fmt.Errorf(
			"the distribution location %s carries userinfo (username or password); credentials would be sent as Basic auth and recorded in the installed-standard record — publish the release content at a location without userinfo (ADR-030 §3)",
			standardURLWithoutUserinfo(parsed))
	}

	req, err := http.NewRequest(http.MethodGet, location, nil)
	if err != nil {
		return nil, "", fmt.Errorf("could not build the request for %s: %w", location, err)
	}
	resp, err := standardInstallHTTPClient.Do(req)
	if err != nil {
		// httpError distinguishes a timeout from other network failures
		// with a clear message (TD-008 §9); the guidance names what failed
		// and how to resolve it for both audiences. The url.Error is
		// scrubbed FIRST: a redirect to a userinfo-bearing target whose
		// network layer fails would otherwise leak the username (Go's
		// url.Error masks the password but keeps the user) — the
		// "credentials never echoed" contract (ADR-030 §3; QA F-1) must
		// hold on every failure path.
		return nil, "", fmt.Errorf(
			"the release content at %s could not be reached: %v. If you are the publisher, fix the release asset at the declared distribution location; otherwise verify the version exists, choose another version, or report the broken release",
			location, httpErrorWithTimeout(downloadResponseHeaderTimeout, standardScrubURLError(err)))
	}
	defer resp.Body.Close()

	// Bound the body read by ACTIVITY, not by a total deadline: release
	// content can legitimately take longer than any fixed window on a
	// slow link, so a download that keeps delivering data runs as long
	// as it needs, while a stalled read fails within the idle window
	// (ANVIL_DOWNLOAD_IDLE_TIMEOUT, 30s default) with a clear timeout
	// error (TD-008 — no request can hang indefinitely).
	resp.Body = newIdleTimeoutBody(resp.Body, downloadIdleTimeout())

	// The redirect policy refuses non-https targets; the final response
	// URL is re-checked defensively (a custom transport could bypass
	// CheckRedirect). The final URL is also re-checked for userinfo — a
	// redirect must not smuggle credentials into the recorded resolution.
	if resp.Request == nil || resp.Request.URL == nil || resp.Request.URL.Scheme != "https" {
		return nil, "", fmt.Errorf(
			"the release content at %s resolved to a non-https response; release content is fetched over TLS only (ADR-030 §3)",
			location)
	}
	if resp.Request.URL.User != nil {
		return nil, "", fmt.Errorf(
			"the release content at %s resolved to %s, which carries userinfo; credentials must never be sent or recorded — publish the release content at a location without userinfo (ADR-030 §3)",
			location, standardURLWithoutUserinfo(resp.Request.URL))
	}
	contentSource := resp.Request.URL.String()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fetchStatusError(location, resp.StatusCode)
	}

	// The size cap is enforced DURING the download: at most cap+1 bytes
	// are read, so content exceeding the cap is reported precisely
	// instead of being buffered unbounded.
	body, err := io.ReadAll(io.LimitReader(resp.Body, standardContentMaxBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf(
			"the release content at %s could not be downloaded: %v. If you are the publisher, fix the release asset; otherwise choose another version or report the broken release",
			location, httpErrorWithTimeout(downloadIdleTimeout(), err))
	}
	if int64(len(body)) > standardContentMaxBytes {
		return nil, "", fmt.Errorf(
			"the release content at %s exceeds the %d-byte size cap; content is never buffered unbounded. If you are the publisher, republish the release content under the cap; otherwise report the broken release",
			location, standardContentMaxBytes)
	}
	return body, contentSource, nil
}

// standardURLWithoutUserinfo renders a URL string with any userinfo
// (username or password) stripped. Credentials must never be echoed in
// errors — the "credentials never sent, echoed, or persisted" contract
// (ADR-030 §3; security finding 1) — so userinfo rejection messages
// render the offending location without its credentials while keeping
// the host and path visible.
func standardURLWithoutUserinfo(u *url.URL) string {
	redacted := *u
	redacted.User = nil
	return redacted.String()
}

// standardUserinfoPrefixPattern matches the scheme://userinfo@ prefix of
// a raw location string, for best-effort credential scrubbing when the
// location does not parse as a URL (e.g. a malformed escape in the
// userinfo): everything between the scheme and the first @ is dropped,
// keeping the host and path visible.
var standardUserinfoPrefixPattern = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9+.-]*://)[^/@]+@`)

// standardUserinfoFragmentPattern matches ANY scheme://userinfo@
// fragment inside arbitrary error text, for defense-in-depth scrubbing
// of wrapped error messages: a wrapped error can carry a
// credential-bearing URL in its own rendered text (e.g. a
// redirect-refusal message built from a raw request URL — QA F-2).
var standardUserinfoFragmentPattern = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/\s"'\x60@]+@`)

// standardScrubLocation renders a raw location string without any
// userinfo credentials, for error messages: credentials must never be
// echoed (ADR-030 §3; QA O-1 — the malformed-location error path can
// carry a raw userinfo-bearing location). A parseable URL is rendered
// via standardURLWithoutUserinfo; an unparseable location gets the
// best-effort prefix scrub.
func standardScrubLocation(location string) string {
	if parsed, err := url.Parse(location); err == nil {
		return standardURLWithoutUserinfo(parsed)
	}
	return standardUserinfoPrefixPattern.ReplaceAllString(location, "$1")
}

// standardScrubCredentialsInMessage removes every scheme://userinfo@
// fragment from arbitrary error text (defense in depth): a wrapped
// error can carry a credential-bearing URL in its own message.
func standardScrubCredentialsInMessage(msg string) string {
	return standardUserinfoFragmentPattern.ReplaceAllString(msg, "$1")
}

// standardScrubURLError removes any userinfo (username or password) from
// a client.Do network failure before it is surfaced (QA F-1, QA F-2).
// Go's url.Error renders the failed request URL with the password masked
// but the USERNAME still visible (e.g. Get "https://alice:***@host/x":
// dial tcp ... connection refused) — and for a CheckRedirect refusal it
// keeps the FULL credentials in both the URL field and the inner error
// text. A redirect to a credential-bearing target whose refusal or
// network layer fails would otherwise leak the username or the full
// credentials, violating the "credentials never echoed" contract
// (ADR-030 §3). The url.Error rendering is rebuilt with the redacted
// URL and — when the inner error's own text carries a
// credential-bearing URL — with the scrubbed inner text. The original
// error is preserved via %w so timeout detection (httpError / isTimeout)
// keeps working; that wrap is only dropped when the inner text itself
// had to be scrubbed (its rendered text would re-leak), which never
// happens for timeout inner errors (dial/TLS errors carry no URL).
func standardScrubURLError(err error) error {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) || urlErr.URL == "" {
		return err
	}
	redactedURL := standardScrubLocation(urlErr.URL)
	innerText := standardScrubCredentialsInMessage(urlErr.Err.Error())
	if redactedURL == urlErr.URL && innerText == urlErr.Err.Error() {
		return err
	}
	if innerText != urlErr.Err.Error() {
		return fmt.Errorf("%s %q: %s", urlErr.Op, redactedURL, innerText)
	}
	return fmt.Errorf("%s %q: %w", urlErr.Op, redactedURL, urlErr.Err)
}

// fetchStatusError renders an actionable error for a non-200 response.
// A 404 names the missing asset and both resolution paths: the publisher
// fixes the release asset, the adopter chooses another version or
// reports the broken release.
func fetchStatusError(location string, statusCode int) error {
	if statusCode == http.StatusNotFound {
		return fmt.Errorf(
			"the release content was not found at %s (HTTP 404). If you are the publisher, publish the release asset at the declared distribution location; otherwise verify the version exists, choose another version, or report the broken release",
			location)
	}
	return fmt.Errorf(
		"the release content could not be fetched from %s (HTTP %d). If you are the publisher, fix the release asset at the declared distribution location; otherwise choose another version or report the broken release",
		location, statusCode)
}

// standardResolutionJSON is the explicit resolution of an adopted
// release (kind + exact source used), shared by the install and update
// machine-readable outputs.
type standardResolutionJSON struct {
	Kind   string `json:"kind"`
	Source string `json:"source"`
}

// standardStatusCellForLifecycle renders the lifecycle status line with
// the announced removal date for deprecated releases, shared by the
// install and update human-readable reports.
func standardStatusCellForLifecycle(l registry.Lifecycle) string {
	if l.State == registry.LifecycleStateDeprecated {
		if l.RemovalDate != "" {
			return "deprecated (removal " + l.RemovalDate + ")"
		}
		return "deprecated"
	}
	return l.State
}
