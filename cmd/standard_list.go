// Package cmd implements the Anvil CLI commands.
//
// ── Standard List (TS-014-02-02) ─────────────────────────────────────
//
// "anvil standard list" lists the installed standards (from the local
// record store) and the delivery lifecycle standards offered for fresh
// adoption from the static registry index (ADR-030): one row per standard
// release, showing the standard name (id), version, and lifecycle status.
//
// Index degradation (ST-021-05): the registry is a decentralized, static
// index with no bundled or canonical hosted directory (ADR-030), so a
// missing or unreadable index does NOT fail the read-only listing. The
// command still shows the installed standards from records and prints one
// actionable setup hint (how to point --index / ANVIL_REGISTRY_INDEX at a
// static index directory), exiting 0. The "available from the registry
// index" section renders only when the index resolves.
//
// Lifecycle handling (TS-014-01-03): published and deprecated releases
// are offered for adoption; deprecated releases carry their warning and
// announced removal date; retired releases are not offered for fresh
// adoption and are excluded from the listing (ADR-027 §3) — inspection
// of a retired release remains possible via "anvil standard inspect".
// Entries that fail strict registry validation (TS-014-01-02) are
// surfaced with an explicit invalid marker and their validation problem,
// never silently dropped.
//
// Index resolution order (documented in the Long help): --index flag,
// ANVIL_REGISTRY_INDEX environment variable, then the default
// <os.UserConfigDir>/anvil/registry.
//
// Reference: TS-014-02-02, TS-014-01-02, TS-014-01-03, ADR-023, ADR-030,
// ST-021-05
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/registry"
)

// standardListCmd represents the "anvil standard list" command that
// displays the standards offered for adoption from the static registry
// index.
//
// Reference: TS-014-02-02
var standardListCmd = &cobra.Command{
	Use:   "list",
	Short: "List standards offered for adoption from the index",
	Long: `Display the installed standards and the delivery lifecycle standards
offered for adoption from the static registry index (ADR-030).

The listing shows the installed standards from the local record store,
then — when the registry index resolves — the releases offered for fresh
adoption. Each row shows the standard name (id), version, and lifecycle
status. Versions are ordered semantically (1.2.3 before 1.10.0).
Deprecated releases show their announced removal date and are listed
with a warning; retired releases are not offered for fresh adoption and
are excluded from the listing (ADR-027 §3). Releases that fail registry
validation are marked invalid and listed with their validation problem —
they are never silently dropped (TS-014-01-02); invalid entries are
data-quality signals and do not fail the command.

There is no bundled or canonical hosted index (ADR-030): when the index
directory is missing or unreadable the listing degrades instead of
failing — the installed standards still appear, followed by one setup
hint (how to point --index / ANVIL_REGISTRY_INDEX at a static index
directory), and the command exits 0.

Output formats:
  Default      "Installed standards" table and an "Available from the
               registry index" table (Standard, Version, Status) plus a
               Warnings section for deprecated releases and an Invalid
               entries section
  --json       standard TS-P8-05 envelope on stdout, data: array of
               {id, version, contract_version, capability, lifecycle,
               distribution, trust_presence, warnings, invalid,
               validation_error, source} — the array of releases offered
               from the index; an unavailable index yields an empty array

Index resolution order:
  1. --index <path>
  2. the ANVIL_REGISTRY_INDEX environment variable
  3. the default <user config dir>/anvil/registry
     (e.g. ~/.config/anvil/registry on Linux)

Exit codes: 0 on success — including listings that surface invalid
entries AND listings with an unavailable index (degraded, installed
standards + setup hint); 1 for other errors.

This is a read-only command — it does not modify any state.

Examples:
  anvil standard list
  anvil standard list --json
  anvil standard list --index ./registry`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runStandardList,
}

func init() {
	AddJSONFlag(standardListCmd)
	standardListCmd.Flags().String("index", "", "path to the static registry index directory (default: $ANVIL_REGISTRY_INDEX, else <user config dir>/anvil/registry)")
	standardListCmd.Flags().Bool("yes", false, "auto-accept registry sync prompts")
	standardListCmd.Flags().Bool("no-sync", false, "disable auto-offer sync (airgap)")
}

// runStandardList executes the list command: the installed standards from
// the local record store, plus — when the registry index resolves — every
// index release offered for fresh adoption (published or deprecated per
// LifecycleAdoptable) and every entry that failed strict validation
// (marked invalid), sorted by standard id and version.
//
// Index degradation (ST-021-05): a missing or unreadable index does NOT
// fail the read-only listing. The command still shows the installed
// standards from records and prints one actionable setup hint (how to
// point --index / ANVIL_REGISTRY_INDEX at a static index directory),
// exiting 0. The "available from the registry index" section renders only
// when the index resolves.
//
// Reference: TS-014-02-02, ST-021-05
func runStandardList(cmd *cobra.Command, args []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	// Auto-offer provisioning when default index empty (ADR-040) — must route chatter to stderr when --json (QA M-1).
	// offerStandardIndexSync handles opt-out, non-TTY auto-decline, and json-aware routing.
	offerStandardIndexSync(cmd)

	// The installed-standards section comes from the local record store
	// (TS-014-03-03), never from the index — it is available even on a
	// fresh setup with no index.
	storeDir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not resolve the installed-standards directory: %v", err)
	}
	records, corrupt, err := registry.NewInstalledStandardStore(storeDir).ListRecords()
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not read installed standards: %v", err)
	}

	// Try the index. A missing or unreadable index degrades the listing
	// (installed + setup hint, exit 0) instead of failing the read-only
	// command — degradation is the strategy for a decentralized/static
	// index with no canonical hosted directory (ADR-030, ST-021-05).
	ix, err := loadStandardIndex(cmd)
	if err != nil {
		return renderStandardListOutput(cmd, jsonOutput, records, corrupt, nil, false)
	}
	if err != nil {
		return renderStandardListOutput(cmd, jsonOutput, records, corrupt, nil, false)
	}
	views, err := collectStandardListViews(ix)
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not enumerate the registry index: %v", err)
	}
	return renderStandardListOutput(cmd, jsonOutput, records, corrupt, views, true)
}

// renderStandardListOutput dispatches the list output surface. The
// machine-readable contract of "anvil standard list" stays the array of
// releases offered for adoption from the index (unchanged); a degraded —
// unavailable — index yields an empty array (nothing is offered), while
// the human surface carries the installed section and the setup hint.
// indexResolved distinguishes a degraded index (load failed) from a
// resolved-but-empty index (an empty index still lists as a resolved
// surface, without the setup hint).
func renderStandardListOutput(cmd *cobra.Command, jsonOutput bool, records []registry.InstalledStandardRecord, corrupt []registry.CorruptRecord, views []standardEntryView, indexResolved bool) error {
	if jsonOutput {
		if !indexResolved {
			return WriteJSON(cmd, []standardListEntry{})
		}
		return WriteJSON(cmd, standardListJSON(views))
	}
	renderStandardList(cmd, records, corrupt, views, indexResolved)
	return nil
}

// collectStandardListViews enumerates the index into list views: every
// release offered for fresh adoption (LifecycleAdoptable: published or
// deprecated — TS-014-01-03) plus every entry that failed strict parsing
// (marked invalid, never silently dropped). Retired releases are
// excluded: they are not resolvable for fresh adoption (ADR-027 §3).
//
// Versions of each standard are ordered semantically (1.2.3 before
// 1.10.0); the index client's lexical order is a documented index-client
// scope, display ordering is the discovery surface's.
func collectStandardListViews(ix *registry.Index) ([]standardEntryView, error) {
	standards, err := ix.Standards()
	if err != nil {
		return nil, err
	}
	var views []standardEntryView
	for _, id := range standards {
		for _, version := range sortStandardVersions(ix.Versions(id)) {
			entry, err := ix.Resolve(id, version)
			if err != nil {
				// Enumerating a loaded index cannot fail; guard against
				// future lazy enumeration sources.
				return nil, fmt.Errorf("resolve %s %s: %w", id, version, err)
			}
			view := standardEntryViewFromIndex(entry)
			if !view.Invalid && !view.Adoptable {
				continue // retired — not offered for fresh adoption
			}
			views = append(views, view)
		}
	}
	return views, nil
}

// renderStandardList prints the listing: an "Installed standards" section
// from the local records (always available), an "Available from the
// registry index" section (only when the index resolves), the warnings
// and invalid-entries sections for the index views, and — when the index
// is unavailable — one actionable setup hint instead of the available
// section (ST-021-05). The columns are trimmed to what users act on:
// standard, version, and lifecycle status. An index with nothing offered
// for adoption says so explicitly. Invalid entries are data-quality
// signals surfaced in the listing — they do not fail the command (exit
// 0); the pinned "standard inspect" form is where a malformed entry
// fails.
func renderStandardList(cmd *cobra.Command, records []registry.InstalledStandardRecord, corrupt []registry.CorruptRecord, views []standardEntryView, indexResolved bool) {
	s := styleFor(cmd)
	w := s.W

	// Installed standards (from the local record store — TS-014-03-03):
	// available even when no index is configured.
	if len(records) > 0 {
		fmt.Fprintln(w, "Installed standards:")
		rows := make([][]string, 0, len(records))
		for _, rec := range records {
			rows = append(rows, []string{
				rec.ID,
				rec.Version,
				standardStatusCellForLifecycle(rec.Lifecycle),
			})
		}
		PrintRoundedTable(cmd, []string{"Standard", "Version", "Status"}, rows)
		fmt.Fprintln(w)
	}

	// Corrupt installed records are surfaced, never silently dropped.
	for _, c := range corrupt {
		fmt.Fprintf(w, "Warning: the installed-standard record %s could not be read (%s) — re-install the standard to recover.\n", c.Path, c.Error)
	}
	if len(corrupt) > 0 {
		fmt.Fprintln(w)
	}

	// Available section only when the index resolves; otherwise the
	// single actionable setup hint (exit 0 — the read-only listing
	// degrades, it does not fail).
	if !indexResolved {
		fmt.Fprintln(w, standardIndexSetupHint())
		return
	}
	if len(views) == 0 {
		fmt.Fprintln(w, "No standards available from the registry index.")
		return
	}

	fmt.Fprintln(w, "Available from the registry index:")
	rows := make([][]string, 0, len(views))
	for _, view := range views {
		rows = append(rows, []string{
			view.ID,
			view.Version,
			standardStatusCell(view),
		})
	}
	PrintRoundedTable(cmd, []string{"Standard", "Version", "Status"}, rows)
	fmt.Fprintln(w)

	renderStandardWarnings(w, views)
	renderStandardProblems(w, views)
}
