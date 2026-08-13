// Package cmd implements the Anvil CLI commands.
//
// ── Standard Inspect (TS-014-02-02) ──────────────────────────────────
//
// "anvil standard inspect <id> [version]" displays the versions of a
// delivery lifecycle standard from the static registry index: with one
// argument, every release of the standard in the index; with a version
// argument, the full detail of that release — declared contract version,
// capability, distribution location, lifecycle state, and trust
// presence.
//
// Inspection is not adoption: retired releases are shown with their
// retired state (they are excluded from "anvil standard list", which
// offers only fresh adoption, ADR-027 §3). A pinned release that fails
// strict registry validation (TS-014-01-02) returns an actionable error
// identifying the entry and the validation problem; in the multi-version
// overview such releases stay visible, marked invalid with their
// problem — never silently dropped.
//
// Reference: TS-014-02-02, TS-014-01-02, TS-014-01-03, ADR-023, ADR-030
package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/registry"
)

// standardInspectCmd represents the "anvil standard inspect" command
// that displays a standard's versions and lifecycle state.
//
// Reference: TS-014-02-02
var standardInspectCmd = &cobra.Command{
	Use:   "inspect <id> [version]",
	Short: "Inspect a standard's versions and lifecycle state",
	Long: `Display the versions and lifecycle state of a delivery lifecycle
standard from the static registry index.

With one argument the command lists every release of the standard in
the index (versions ordered semantically, 1.2.3 before 1.10.0) with its
version and lifecycle status. With a version argument it displays the
full detail of that release: declared contract version, capability,
distribution location, lifecycle state, and trust presence.

Inspection is not adoption: retired releases are shown with their
retired state — they are not offered for fresh adoption (ADR-027 §3).
A pinned release that fails registry validation is reported with the
validation problem (TS-014-01-02); in the overview such releases stay
visible, marked invalid — they are data-quality signals and do not fail
the command.

Output formats:
  Default      sectioned detail for a pinned release; a rounded table
               (Version, Status) plus Warnings and Invalid entries
               sections for the overview
  --json       standard TS-P8-05 envelope on stdout, data:
               pinned — {id, version, contract_version, capability,
               lifecycle, distribution, trust_presence, warnings,
               source}; overview — {id, versions: [list entries]}

Index resolution order:
  1. --index <path>
  2. the ANVIL_REGISTRY_INDEX environment variable
  3. the default <user config dir>/anvil/registry
     (e.g. ~/.config/anvil/registry on Linux)

Exit codes: 0 on success (including overviews that surface invalid
entries); 3 when the index directory, the standard, or the version is
not found; 1 for other errors (including an invalid pinned release).

This is a read-only command — it does not modify any state.

Examples:
  anvil standard inspect anvil-standard-laravel
  anvil standard inspect anvil-standard-laravel 1.2.3
  anvil standard inspect anvil-standard-laravel 1.2.3 --json`,
	Args:         RangeArgsWithUsage(1, 2, "anvil standard inspect anvil-standard-laravel 1.2.3", "id", "version"),
	SilenceUsage: true,
	RunE:         runStandardInspect,
}

func init() {
	AddJSONFlag(standardInspectCmd)
	standardInspectCmd.Flags().String("index", "", "path to the static registry index directory (default: $ANVIL_REGISTRY_INDEX, else <user config dir>/anvil/registry)")
}

// standardInspectJSON is the machine-readable pinned-inspect output:
// the full detail of one release.
type standardInspectJSON struct {
	ID              string                    `json:"id"`
	Version         string                    `json:"version"`
	ContractVersion string                    `json:"contract_version"`
	Capability      []string                  `json:"capability"`
	Lifecycle       standardLifecycleJSON     `json:"lifecycle"`
	Distribution    standardDistributionJSON  `json:"distribution"`
	TrustPresence   standardTrustPresenceJSON `json:"trust_presence"`
	Warnings        []string                  `json:"warnings,omitempty"`
	Source          string                    `json:"source"`
}

// standardOverviewJSON is the machine-readable overview-inspect output:
// the standard id and every version in the index (list rows, so retired
// releases appear here and invalid releases carry their marker).
type standardOverviewJSON struct {
	ID       string              `json:"id"`
	Versions []standardListEntry `json:"versions"`
}

// runStandardInspect executes the inspect command.
//
// Reference: TS-014-02-02
func runStandardInspect(cmd *cobra.Command, args []string) error {
	id := args[0]
	jsonOutput, _ := cmd.Flags().GetBool("json")

	indexPath, err := standardIndexPath(cmd)
	if err != nil {
		return reportStandardIndexError(cmd, err)
	}
	ix, err := registry.LoadIndex(indexPath)
	if err != nil {
		return reportStandardIndexError(cmd, err)
	}

	if len(ix.Versions(id)) == 0 {
		// A standard not in the index is a not-found failure (exit 3,
		// ExitCodeRuntime — "resource not found", TS-P8-07 / ADR-010 §8.1).
		return ReportErrorWithCode(cmd, &output.AppError{
			Message:    fmt.Sprintf("standard %q not found in the registry index", id),
			Reason:     fmt.Sprintf("the index at %s has no documents declaring the standard id %q", indexPath, id),
			Resolution: "Run 'anvil standard list' to see the standards available in the index",
		}, output.ExitCodeRuntime)
	}

	if len(args) == 2 {
		return runStandardInspectPinned(cmd, ix, id, args[1], jsonOutput)
	}
	return runStandardInspectOverview(cmd, ix, id, jsonOutput)
}

// runStandardInspectPinned inspects one exact release of the standard.
// A release that fails strict registry validation returns an actionable
// error identifying the entry and the validation problem; a version
// missing from the index is a not-found failure (exit 3).
func runStandardInspectPinned(cmd *cobra.Command, ix *registry.Index, id, version string, jsonOutput bool) error {
	entry, err := ix.Resolve(id, version)
	if err != nil {
		return ReportErrorWithCode(cmd, &output.AppError{
			Message:    fmt.Sprintf("standard %q version %q not found", id, version),
			Reason:     err.Error(),
			Resolution: "Run 'anvil standard inspect " + id + "' to see the versions in the index",
			Err:        err,
		}, output.ExitCodeRuntime)
	}

	view := standardEntryViewFromIndex(entry)
	if view.Invalid {
		// The underlying parse error is carried in Err so the error
		// chain (errors.As/Is) and the --json error envelope include the
		// full validation problem, not just the message.
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("standard %q version %q is invalid", id, version),
			Reason:     view.ParseError,
			Resolution: "Fix or remove the metadata document in the index, then run the command again",
			Err:        errors.New(view.ParseError),
		})
	}

	if jsonOutput {
		return WriteJSON(cmd, standardInspectJSONFromView(view))
	}
	renderStandardInspectPinned(cmd, view)
	return nil
}

// runStandardInspectOverview lists every release of the standard in the
// index (versions ordered semantically, 1.2.3 before 1.10.0). Inspection
// is not adoption: retired releases are shown with their state, and
// releases that failed strict validation stay visible, marked invalid —
// invalid entries are data-quality signals and do not fail the command
// (exit 0); the pinned form is where a malformed entry fails.
func runStandardInspectOverview(cmd *cobra.Command, ix *registry.Index, id string, jsonOutput bool) error {
	views := make([]standardEntryView, 0, len(ix.Versions(id)))
	for _, version := range sortStandardVersions(ix.Versions(id)) {
		entry, err := ix.Resolve(id, version)
		if err != nil {
			// Enumerating a loaded index cannot fail; guard against
			// future lazy enumeration sources.
			return ReportPlainErrorf(cmd, err, "could not resolve %s %s: %v", id, version, err)
		}
		views = append(views, standardEntryViewFromIndex(entry))
	}

	if jsonOutput {
		return WriteJSON(cmd, standardOverviewJSON{ID: id, Versions: standardListJSON(views)})
	}
	renderStandardInspectOverview(cmd, id, views)
	return nil
}

// standardInspectJSONFromView converts one presentation view into its
// machine-readable pinned-inspect shape.
func standardInspectJSONFromView(view standardEntryView) standardInspectJSON {
	return standardInspectJSON{
		ID:              view.ID,
		Version:         view.Version,
		ContractVersion: view.ContractVersion,
		Capability:      view.Capability,
		Lifecycle: standardLifecycleJSON{
			State:       view.LifecycleState,
			RemovalDate: view.RemovalDate,
		},
		Distribution: standardDistributionJSON{
			Type:     view.DistributionType,
			Location: view.DistributionLocation,
		},
		TrustPresence: standardTrustPresenceJSON{
			ContentDigests: view.TrustContentDigests,
			Attestation:    view.TrustAttestation,
		},
		Warnings: view.Warnings,
		Source:   view.Source,
	}
}

// renderStandardInspectPinned writes the human-readable pinned inspect
// output: sectioned detail of one release. Deprecated releases surface
// their warning and removal date; retired releases state that they are
// not offered for fresh adoption.
func renderStandardInspectPinned(cmd *cobra.Command, view standardEntryView) {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "Standard: %s %s\n", view.ID, view.Version)
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Contract Version:")
	fmt.Fprintf(w, "  %s\n", view.ContractVersion)
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Capability:")
	fmt.Fprintf(w, "  %s\n", standardCapabilityCell(view))
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Lifecycle:")
	fmt.Fprintf(w, "  %s\n", standardStatusCell(view))
	fmt.Fprintln(w)

	if view.RemovalDate != "" {
		fmt.Fprintln(w, "Removal Date:")
		fmt.Fprintf(w, "  %s\n", view.RemovalDate)
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, "Distribution:")
	fmt.Fprintf(w, "  %s\n", view.DistributionType)
	fmt.Fprintf(w, "  %s\n", view.DistributionLocation)
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Trust:")
	fmt.Fprintf(w, "  content digests: %s\n", yesNo(view.TrustContentDigests))
	fmt.Fprintf(w, "  attestation: %s\n", yesNo(view.TrustAttestation))
	fmt.Fprintln(w)

	renderStandardWarnings(w, []standardEntryView{view})

	if view.LifecycleState == registry.LifecycleStateRetired {
		fmt.Fprintln(w, "Note: retired standards are not offered for fresh adoption (ADR-027 §3); this inspection is informational only.")
	}
}

// renderStandardInspectOverview writes the human-readable overview
// output: every version of the standard in a table, then the warnings
// and invalid-entries sections.
func renderStandardInspectOverview(cmd *cobra.Command, id string, views []standardEntryView) {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "Standard: %s\n", id)
	fmt.Fprintln(w)

	fmt.Fprintf(w, "Versions (%d):\n", len(views))
	rows := make([][]string, 0, len(views))
	for _, view := range views {
		rows = append(rows, []string{
			view.Version,
			standardStatusCell(view),
		})
	}
	PrintRoundedTable(cmd, []string{"Version", "Status"}, rows)
	fmt.Fprintln(w)

	renderStandardWarnings(w, views)
	renderStandardProblems(w, views)
}

// yesNo renders a boolean as "yes"/"no" for human-readable sections.
func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
