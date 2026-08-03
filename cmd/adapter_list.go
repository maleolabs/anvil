// Package cmd implements the Anvil CLI commands.
//
// ── Adapter List (TS-007-031) ─────────────────────────────────────────
//
// "anvil adapter list" enumerates the framework adapters available to
// the current Anvil installation.
//
// Availability model: an adapter is available when its executable
// "anvil-adapter-<framework>" resolves on PATH (005-adapter-command-
// contract §10). All known frameworks (engine.KnownFrameworks — the
// frameworks with a registered build pipeline template: laravel,
// flutter) are listed; frameworks whose executable is not installed are
// shown with a "not installed" marker so users can discover what is
// available to install. This satisfies "displays all registered
// adapters" — every resolvable adapter appears with its real
// capabilities — while keeping the list deterministic. When an installed
// adapter's capabilities request fails, the row shows "unknown" instead
// of failing the whole list.
//
// Version semantics: the adapter command contract carries no version
// (CapabilityResult exposes only the capability declaration) and the
// Core keeps no static AdapterInfo registry the CLI could consult
// (adapter descriptors are registered by consumers/installers, ADR-009
// §9.1). The VERSION column therefore shows "-" (not declared); no
// version is fabricated.
//
// Reference: TS-007-031, TS-P7-31, 005-adapter-command-contract §10
package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

// adapterListCmd represents the "anvil adapter list" command that
// displays all available adapters.
//
// Reference: TS-007-031
var adapterListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available adapters",
	Long: `Display all available framework adapters.

An adapter is available when its executable (anvil-adapter-<framework>)
is on PATH. Known frameworks whose executable is not installed are
listed with a "not installed" marker.

Output columns:
  Name             Framework name (e.g. laravel)
  Deployment Model Deployment model (server, hybrid, package)
  Version          Framework version range; not declared by the adapter
                   command contract, so shown as "-"

This is a read-only command — it does not modify any state.

Examples:
  anvil adapter list
  anvil adapter list --json`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runAdapterList,
}

func init() {
	AddJSONFlag(adapterListCmd)
}

// adapterListEntry is one row of "anvil adapter list" output. It is also
// the machine-readable shape used for --json (wrapped in the standard
// envelope, TS-P8-05).
type adapterListEntry struct {
	Name            string `json:"name"`
	DeploymentModel string `json:"deployment_model"`
	Version         string `json:"version"`
}

// runAdapterList executes the list command.
//
// Reference: TS-007-031
func runAdapterList(cmd *cobra.Command, args []string) error {
	frameworks := adapterKnownFrameworks()
	jsonOutput, _ := cmd.Flags().GetBool("json")

	if len(frameworks) == 0 {
		if jsonOutput {
			return WriteJSON(cmd, []adapterListEntry{})
		}
		fmt.Fprintln(cmd.OutOrStdout(), "No adapters available.")
		return nil
	}

	entries := make([]adapterListEntry, 0, len(frameworks))
	for _, framework := range frameworks {
		entries = append(entries, collectAdapterListEntry(cmd.Context(), framework))
	}

	if jsonOutput {
		return WriteJSON(cmd, entries)
	}

	rows := make([][]string, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, []string{entry.Name, entry.DeploymentModel, entry.Version})
	}
	PrintTable(cmd, []string{"Name", "Deployment Model", "Version"}, rows)
	return nil
}

// collectAdapterListEntry gathers the display data for one framework. The
// deployment model comes from the adapter's declared capabilities; a
// missing executable yields a "not installed" marker, and a failed
// capabilities request yields an "unknown" state — a broken adapter never
// fails the whole list.
func collectAdapterListEntry(ctx context.Context, framework string) adapterListEntry {
	entry := adapterListEntry{Name: framework, Version: "-"}

	executable, err := adapterExecutableLookup("anvil-adapter-" + framework)
	if err != nil {
		entry.DeploymentModel = "not installed"
		return entry
	}

	result, err := invokeAdapterCapabilities(ctx, framework, executable)
	if err != nil {
		entry.DeploymentModel = "unknown"
		return entry
	}

	entry.DeploymentModel = result.Declaration.DeploymentModel
	if entry.DeploymentModel == "" {
		entry.DeploymentModel = "-"
	}
	return entry
}
