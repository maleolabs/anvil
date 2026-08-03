// Package cmd implements the Anvil CLI commands.
//
// ── Adapter List (TS-007-031) ─────────────────────────────────────────
//
// "anvil adapter list" enumerates the framework adapters on the system.
//
// Default mode (no flag): the installed adapters. The check is not
// static — every binary named exactly "anvil-adapter-<name>" (no
// platform suffix) that exists in the CLI install directory or anywhere
// on PATH is detected, so adapters placed manually or by a package
// manager show up too. Each installed adapter's capabilities are invoked
// to display its real deployment model; a broken adapter shows "unknown"
// instead of failing the whole list. When nothing is installed, the
// command says so and hints at "--available".
//
// --available mode: the adapters published in the latest GitHub release
// (anvil-adapter-<name>-<os>-<arch> assets, TS-007-034), with a Status
// column telling whether each one is already installed on this system.
// This is the discovery path for "what can I install" — the opposite of
// the old static "known frameworks" enumeration, which is gone: the
// default list is purely system-derived.
//
// Version semantics: the adapter command contract carries no version
// (CapabilityResult exposes only the capability declaration), so the
// VERSION column shows "-" in default mode. In --available mode it shows
// the release tag the list was fetched from.
//
// Reference: TS-007-031, TS-P7-31, 005-adapter-command-contract §10
package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/output"
)

// adapterListCmd represents the "anvil adapter list" command that
// displays installed adapters (default) or adapters available in the
// latest release (--available).
//
// Reference: TS-007-031
var adapterListCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed adapters",
	Long: `Display the framework adapters on this system.

By default, every installed adapter is listed: any executable named
exactly anvil-adapter-<name> found next to the anvil CLI or anywhere
on PATH. Deployment models come from each adapter's own capability
declaration.

Use --available to list the adapters published in the latest GitHub
release instead, with their install status.

Output columns (default):
  Name             Adapter name (e.g. laravel)
  Deployment Model Deployment model (server, hybrid, package)
  Version          Framework version range; not declared by the adapter
                   command contract, so shown as "-"

Output columns (--available):
  Name             Adapter name
  Deployment Model Deployment model of an installed adapter; "-" when
                   not installed
  Version          Latest release tag
  Status           installed | available

This is a read-only command — it does not modify any state.

Examples:
  anvil adapter list
  anvil adapter list --available
  anvil adapter list --json
  anvil adapter list --available --json`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runAdapterList,
}

func init() {
	AddJSONFlag(adapterListCmd)
	adapterListCmd.Flags().Bool("available", false, "List adapters available in the latest GitHub release")
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

// runAdapterListInstalled lists the adapters whose binaries are present
// on this system (CLI install directory first, then PATH).
//
// Reference: TS-007-031
func runAdapterListInstalled(cmd *cobra.Command, jsonOutput bool) error {
	installed := installedAdaptersFromSystem()

	if len(installed) == 0 {
		if jsonOutput {
			return WriteJSON(cmd, []adapterListEntry{})
		}
		fmt.Fprintln(cmd.OutOrStdout(), "No adapters installed.")
		fmt.Fprintln(cmd.OutOrStdout(), "Run 'anvil adapter list --available' to see adapters available in the latest release.")
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

// runAdapterListAvailable lists the adapters published in the latest
// GitHub release, marking each as installed or available on this system.
// While the release fetch is in flight, a loading spinner shows progress
// (non-JSON runs only) so the user never perceives a stuck command.
//
// Reference: TS-007-031, TS-007-034
func runAdapterListAvailable(cmd *cobra.Command, jsonOutput bool) error {
	// ── Fetch with live feedback ──
	// Terminal: animated spinner; non-terminal: silent until the final
	// status line is printed on Stop. --json runs emit no progress.
	var spinner *output.Spinner
	fetchStart := time.Now()
	if !jsonOutput {
		spinner = output.NewSpinner(cmd.OutOrStdout(), "", "Fetching adapters from latest release")
		spinner.Start()
	}
	releaseTag, names, err := adapterReleaseFetch()
	if spinner != nil {
		if err != nil {
			spinner.Stop(fmt.Sprintf("%s Fetching adapters from latest release", output.Red(cmd.OutOrStdout(), "✗")))
		} else {
			spinner.Stop(fmt.Sprintf("%s Fetching adapters from latest release (%s)",
				output.Green(cmd.OutOrStdout(), "✓"), output.FormatDuration(time.Since(fetchStart))))
		}
	}
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not fetch release adapters: %v", err)
	}

	if len(names) == 0 {
		if jsonOutput {
			return WriteJSON(cmd, []adapterListEntry{})
		}
		fmt.Fprintln(cmd.OutOrStdout(), "No adapters available in the latest release.")
		return nil
	}

	installed := installedAdaptersFromSystem()

	entries := make([]adapterListEntry, 0, len(names))
	for _, name := range names {
		entry := adapterListEntry{Name: name, DeploymentModel: "-", Version: releaseTag, Status: "available"}
		if executable, ok := installed[name]; ok {
			entry = collectAdapterListEntry(cmd.Context(), name, executable)
			entry.Version = releaseTag
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

// installedAdaptersFromSystem detects installed adapters on this system:
// every binary named exactly "anvil-adapter-<name>" in the CLI install
// directory (where "anvil adapter install" and install.sh place binaries,
// TS-007-035/037) or anywhere on PATH (the command contract resolution,
// 005 §10). The CLI directory wins when the same adapter exists in both
// locations. Release asset artifacts (platform-suffixed names) and
// unrelated files are skipped. Returns name → executable path.
//
// Reference: TS-007-031, 005-adapter-command-contract §10
func installedAdaptersFromSystem() map[string]string {
	found := make(map[string]string)

	if dir, err := adapterInstallDir(); err == nil {
		collectInstalledAdapterDir(found, dir)
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		collectInstalledAdapterDir(found, dir)
	}
	return found
}

// collectInstalledAdapterDir scans one directory for installed adapter
// binaries and records any new name → executable path into found.
func collectInstalledAdapterDir(found map[string]string, dir string) {
	names, err := listInstalledAdapters(dir)
	if err != nil {
		return // unreadable or missing directory — nothing to detect there
	}
	for _, name := range names {
		if _, ok := found[name]; !ok {
			found[name] = filepath.Join(dir, adapterBinaryName(name))
		}
	}
}

// collectAdapterListEntry gathers the display data for one installed
// adapter. The deployment model comes from the adapter's declared
// capabilities; a failed capabilities request yields an "unknown" state —
// a broken adapter never fails the whole list.
func collectAdapterListEntry(ctx context.Context, framework, executable string) adapterListEntry {
	entry := adapterListEntry{Name: framework, Version: "-"}

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
