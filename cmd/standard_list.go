// Package cmd implements the Anvil CLI commands.
//
// ── Standard List (TS-014-02-02) ─────────────────────────────────────
//
// "anvil standard list" lists the delivery lifecycle standards offered
// for fresh adoption from the static registry index (ADR-030): one row
// per standard release, showing the standard name (id), version,
// declared contract version, capability (supported framework versions),
// and lifecycle status.
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
// Reference: TS-014-02-02, TS-014-01-02, TS-014-01-03, ADR-023, ADR-030
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
	Long: `Display the delivery lifecycle standards offered for adoption from the
static registry index (ADR-030).

Each row is one standard release: the standard name (id), version,
declared contract version, capability (supported framework versions),
and lifecycle status. Versions are ordered semantically (1.2.3 before
1.10.0). Deprecated releases show their announced removal date and are
listed with a warning; retired releases are not offered for fresh
adoption and are excluded from the listing (ADR-027 §3). Releases that
fail registry validation are marked invalid and listed with their
validation problem — they are never silently dropped (TS-014-01-02);
invalid entries are data-quality signals and do not fail the command.

Output formats:
  Default      rounded table (Standard, Version, Contract, Capability,
               Status) plus a Warnings section for deprecated releases
               and an Invalid entries section
  --json       standard TS-P8-05 envelope on stdout, data: array of
               {id, version, contract_version, capability, lifecycle,
               distribution, trust_presence, warnings, invalid,
               validation_error, source}

Index resolution order:
  1. --index <path>
  2. the ANVIL_REGISTRY_INDEX environment variable
  3. the default <user config dir>/anvil/registry
     (e.g. ~/.config/anvil/registry on Linux)

Exit codes: 0 on success (including listings that surface invalid
entries); 3 when the index directory is not found; 1 for other errors.

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
}

// runStandardList executes the list command: every index release that is
// offered for fresh adoption (published or deprecated per
// LifecycleAdoptable) plus every entry that failed strict validation
// (marked invalid), sorted by standard id and version.
//
// Reference: TS-014-02-02
func runStandardList(cmd *cobra.Command, args []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")

	indexPath, err := standardIndexPath(cmd)
	if err != nil {
		return reportStandardIndexError(cmd, err)
	}
	ix, err := registry.LoadIndex(indexPath)
	if err != nil {
		return reportStandardIndexError(cmd, err)
	}

	views, err := collectStandardListViews(ix)
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not enumerate the registry index: %v", err)
	}

	if jsonOutput {
		return WriteJSON(cmd, standardListJSON(views))
	}

	renderStandardList(cmd, views)
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

// renderStandardList prints the listing as a table followed by the
// warnings and invalid-entries sections. An index with nothing offered
// for adoption says so explicitly. Invalid entries are data-quality
// signals surfaced in the listing — they do not fail the command (exit
// 0); the pinned "standard inspect" form is where a malformed entry
// fails.
func renderStandardList(cmd *cobra.Command, views []standardEntryView) {
	if len(views) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No standards available in the registry index.")
		fmt.Fprintln(cmd.OutOrStdout(), "Set the index directory with --index <path> or the "+envStandardIndex+" environment variable.")
		return
	}

	rows := make([][]string, 0, len(views))
	for _, view := range views {
		rows = append(rows, []string{
			view.ID,
			view.Version,
			standardContractCell(view),
			standardCapabilityCell(view),
			standardStatusCell(view),
		})
	}
	PrintRoundedTable(cmd, []string{"Standard", "Version", "Contract", "Capability", "Status"}, rows)
	fmt.Fprintln(cmd.OutOrStdout())

	w := cmd.OutOrStdout()
	renderStandardWarnings(w, views)
	renderStandardProblems(w, views)
}
