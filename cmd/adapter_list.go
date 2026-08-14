// Package cmd implements the Anvil CLI commands.
//
// ── Adapter List (TS-007-031, TS-016-04-01, TS-017-02-02) ────────────
//
// "anvil adapter list" enumerates the framework adapters on the system.
//
// Default mode (no flag): the installed adapters — REGISTRY-DRIVEN
// since the switch-over gate (TS-017-02-02, ADR-028 §3, §7): an adapter
// is installed when its delivery lifecycle standard
// (anvil-standard-<name>, ADR-021 §3.1) is RECORDED in the
// installed-standard store (adopted through the registry with the
// ADR-022 trust validation). The closed-set binary scan is removed; the
// listed adapters' deployment model is probed from the resolved
// executable (the executable resolution contract, ADR-025 decision 4)
// when the binary is present, else shown as "-". When nothing is
// recorded, the command says so and hints at "--available". The
// registry trust validation is the only path into the trusted surface.
//
// --available mode: the adapters offered for adoption in the static
// registry index (TS-016-04-01): standards named anvil-standard-<name>
// (ADR-021 §3.1) are mapped to adapter names, each with the highest
// adoptable version and a Status column telling whether the adapter is
// already installed on this system — "installed" is the registry-driven
// definition (recorded standard), the same one the default mode
// surfaces. This is the REGISTRY-BASED discovery path — the registry is
// the source of what can be installed; the Core GitHub release is no
// longer a distribution channel for adapter binaries after the
// repository split (ADR-025 §3.5). It is the adapter-view counterpart
// of "anvil standard list".
//
// Version semantics: in default mode the VERSION column shows the
// recorded version of the installed standard (registry record). In
// --available mode it shows the highest adoptable registry version of
// the standard.
//
// Reference: TS-007-031, TS-P7-31, TS-016-04-01, TS-017-02-02,
// 005-adapter-command-contract §10, ADR-021 §3.1, ADR-022, ADR-025,
// ADR-028, ADR-030
package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"maleolabs.com/anvil/internal/registry"
)

// adapterListCmd represents the "anvil adapter list" command that
// displays installed adapters (default) or adapters available in the
// registry index (--available).
//
// Reference: TS-007-031, TS-016-04-01
var adapterListCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed adapters",
	Long: `Display the framework adapters on this system.

By default, every installed adapter is listed — the registry-driven
installed view (post-gate, TS-017-02-02): an adapter is installed when
its delivery lifecycle standard anvil-standard-<name> is RECORDED
(adopted through the registry with the ADR-022 trust validation). The
version shown is the recorded standard version. The deployment model
comes from the adapter executable's own capability declaration when the
binary resolves (the executable resolution contract — next to the CLI
or on PATH); a binary that is absent or not resolvable is shown as "-".
Binaries that were never adopted through the registry are not
discovered — there is no binary scan.

Use --available to list the adapters offered for adoption in the
static registry index instead: standards named anvil-standard-<name>
mapped to their adapter names, with the highest adoptable version and
their install status (the same registry-driven installed definition).

Index resolution order (--available):
  1. --index <path>
  2. the ANVIL_REGISTRY_INDEX environment variable
  3. the default <user config dir>/anvil/registry
     (e.g. ~/.config/anvil/registry on Linux)

Output columns (default):
  Name             Adapter name (e.g. laravel)
  Deployment Model Deployment model (server, hybrid, package); "-" when
                   the binary is not resolvable
  Version          Recorded version of the installed standard

Output columns (--available):
  Name             Adapter name
  Deployment Model Deployment model of an installed adapter; "-" when
                   not installed or not resolvable
  Version          Highest adoptable registry version
  Status           installed | available

This is a read-only command — it does not modify any state.

Examples:
  anvil adapter list
  anvil adapter list --available
  anvil adapter list --json
  anvil adapter list --available --json
  anvil adapter list --available --index ./registry`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runAdapterList,
	Deprecated:   adapterListDeprecationNotice,
}

func init() {
	AddJSONFlag(adapterListCmd)
	adapterListCmd.Flags().Bool("available", false, "List adapters offered for adoption in the registry index")
	adapterListCmd.Flags().String("index", "", "path to the static registry index directory (default: $ANVIL_REGISTRY_INDEX, else <user config dir>/anvil/registry)")
}

// adapterListEntry is one row of "anvil adapter list" output. It is also
// the machine-readable shape used for --json (wrapped in the standard
// envelope, TS-P8-05). Status is set only in --available mode.
type adapterListEntry struct {
	Name            string `json:"name"`
	DeploymentModel string `json:"deployment_model"`
	Version         string `json:"version"`
	Status          string `json:"status,omitempty"`
}

// runAdapterList executes the list command: installed adapters by
// default, release adapters with --available.
//
// Reference: TS-007-031
func runAdapterList(cmd *cobra.Command, args []string) error {
	available, _ := cmd.Flags().GetBool("available")
	jsonOutput, _ := cmd.Flags().GetBool("json")

	if available {
		return runAdapterListAvailable(cmd, jsonOutput)
	}
	return runAdapterListInstalled(cmd, jsonOutput)
}

// runAdapterListInstalled lists the adapters whose delivery lifecycle
// standards are installed — the REGISTRY-DRIVEN installed view since
// the switch-over gate (TS-017-02-02): the installed-standard records
// (adopted through the registry with the ADR-022 trust validation), not
// a binary scan. The deployment model is probed from the resolved
// executable (executable resolution contract, ADR-025 decision 4) when
// the binary is present, else rendered as "-".
//
// Reference: TS-007-031, TS-016-04-01, TS-017-02-02
func runAdapterListInstalled(cmd *cobra.Command, jsonOutput bool) error {
	// A store that cannot be read is surfaced, never silently read as
	// "nothing installed" (team review F5): the listing warns on stderr
	// and continues with the empty view.
	installed, err := installedAdapterVersions()
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), FmtWarning("could not read the installed-standard store: %v; showing the empty installed view"), err)
		installed = nil
	}

	if len(installed) == 0 {
		if jsonOutput {
			return WriteJSON(cmd, []adapterListEntry{})
		}
		fmt.Fprintln(cmd.OutOrStdout(), "No adapters installed.")
		fmt.Fprintln(cmd.OutOrStdout(), "Run 'anvil adapter list --available' to see the adapters offered in the registry index.")
		return nil
	}

	names := make([]string, 0, len(installed))
	for name := range installed {
		names = append(names, name)
	}
	sort.Strings(names)

	entries := make([]adapterListEntry, 0, len(names))
	for _, name := range names {
		entries = append(entries, collectAdapterListEntry(cmd.Context(), name, installed[name]))
	}

	return renderAdapterList(cmd, entries, jsonOutput, false)
}

// runAdapterListAvailable lists the adapters offered for adoption in the
// static registry index (TS-016-04-01): first-party standards
// (anvil-standard-<name>, ADR-021 §3.1) are mapped to adapter names by
// the identity convention, each shown with the highest ADOPTABLE version
// and marked installed or available against the REGISTRY-DRIVEN
// installed definition (installedAdapterVersions — the recorded
// installed-standard records, the same "installed" definition the
// default mode surfaces post-gate, TS-017-02-02): an adapter is
// installed when its standard was adopted through the registry, never
// because a bare binary sits on PATH. Entries that fail strict registry
// validation and retired releases are not offered for adoption and are
// skipped (the same "not offered" semantics as "anvil standard list"
// and "anvil adapter install").
//
// Reference: TS-007-031, TS-016-04-01, TS-017-02-01, TS-017-02-02,
// TS-014-01-03, ADR-021 §3.1, ADR-027 §3, ADR-028 §3
func runAdapterListAvailable(cmd *cobra.Command, jsonOutput bool) error {
	ix, err := loadStandardIndex(cmd)
	if err != nil {
		return reportStandardIndexError(cmd, err)
	}

	// name → highest adoptable version, from the standards offered in
	// the index (sorted by id; versions iterated highest-first).
	byAdapter := make(map[string]string)
	standards, err := ix.Standards()
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not enumerate the registry index: %v", err)
	}
	for _, id := range standards {
		name, ok := strings.CutPrefix(id, registry.StandardIDPrefix)
		if !ok || name == "" {
			continue // only anvil-standard-* standards map to adapter names
		}
		// The highest adoptable version — the shared selection rule of
		// the registry-based discovery surfaces (highestAdoptableVersion,
		// cmd/adapter_shared.go): valid, adoptable releases only, ordered
		// semantically.
		if version := highestAdoptableVersion(ix, id); version != "" {
			byAdapter[name] = version
		}
	}

	if len(byAdapter) == 0 {
		if jsonOutput {
			return WriteJSON(cmd, []adapterListEntry{})
		}
		fmt.Fprintln(cmd.OutOrStdout(), "No adapters available in the registry index.")
		fmt.Fprintln(cmd.OutOrStdout(), "Set the index directory with --index <path> or the "+envStandardIndex+" environment variable.")
		return nil
	}

	// The registry-driven installed set (installedAdapterVersions — the
	// recorded installed-standard records) is the installed marking of
	// the registry view: the same "installed" definition the default
	// mode surfaces post-gate (TS-017-02-02), so both views of the
	// adapter surface agree on what "installed" means. A store that
	// cannot be read is surfaced as a warning, never silently read as
	// "nothing installed" (team review F5).
	installed, err := installedAdapterVersions()
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), FmtWarning("could not read the installed-standard store: %v; no adapter is marked installed"), err)
		installed = nil
	}

	names := make([]string, 0, len(byAdapter))
	for name := range byAdapter {
		names = append(names, name)
	}
	sort.Strings(names)

	entries := make([]adapterListEntry, 0, len(names))
	for _, name := range names {
		entry := adapterListEntry{Name: name, DeploymentModel: "-", Version: byAdapter[name], Status: "available"}
		if _, ok := installed[name]; ok {
			entry = collectAdapterListEntry(cmd.Context(), name, byAdapter[name])
			entry.Version = byAdapter[name]
			entry.Status = "installed"
		}
		entries = append(entries, entry)
	}

	return renderAdapterList(cmd, entries, jsonOutput, true)
}

// renderAdapterList prints the entries as a modern rounded-corner table
// or JSON envelope. withStatus switches the table to the --available
// column set.
func renderAdapterList(cmd *cobra.Command, entries []adapterListEntry, jsonOutput, withStatus bool) error {
	if jsonOutput {
		return WriteJSON(cmd, entries)
	}

	rows := make([][]string, 0, len(entries))
	for _, entry := range entries {
		if withStatus {
			rows = append(rows, []string{entry.Name, entry.DeploymentModel, entry.Version, entry.Status})
		} else {
			rows = append(rows, []string{entry.Name, entry.DeploymentModel, entry.Version})
		}
	}
	if withStatus {
		PrintRoundedTable(cmd, []string{"Name", "Deployment Model", "Version", "Status"}, rows)
	} else {
		PrintRoundedTable(cmd, []string{"Name", "Deployment Model", "Version"}, rows)
	}
	return nil
}

// collectAdapterListEntry gathers the display data for one installed
// adapter: the recorded standard version and the deployment model
// declared by the resolved executable (executable resolution contract,
// ADR-025 decision 4). Post-gate (TS-017-02-02) the installed truth is
// the registry record, so a binary that is absent or does not answer
// the capabilities command never fails the entry — the model renders as
// "-".
func collectAdapterListEntry(ctx context.Context, framework, version string) adapterListEntry {
	entry := adapterListEntry{Name: framework, Version: version}

	model := probeAdapterDeploymentModel(ctx, framework)
	if model == "" {
		entry.DeploymentModel = "-"
		return entry
	}
	entry.DeploymentModel = model
	return entry
}
