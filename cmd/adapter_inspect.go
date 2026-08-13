// Package cmd implements the Anvil CLI commands.
//
// ── Adapter Inspect (TS-007-032, TS-017-02-02) ───────────────────────
//
// "anvil adapter inspect <name>" displays one adapter's declared
// capabilities (TS-P7-07): deployment model (ADR-016), build phases
// (TS-P7-14), activation phases (server model), verification checks, and
// configuration keys (TS-P7-03). The data is fetched through the command
// contract — the adapter executable answers the capabilities and
// extension commands with JSON documents (005-adapter-command-contract).
//
// Post-gate resolution (TS-017-02-02, ADR-028 §3, §7): the adapter's
// identity is established through the registry — its standard
// (anvil-standard-<name>) must be RECORDED (installed through the
// registry with the ADR-022 trust validation); the executable then
// resolves through the executable resolution contract
// (anvil-adapter-<name> on PATH, ADR-025 decision 4). The closed-set
// binary scan is removed: a bare binary that was never adopted through
// the registry is not discoverable.
//
// Reference: TS-007-032, TS-P7-32, TS-017-02-02,
// 005-adapter-command-contract §10, ADR-022, ADR-025 decision 4,
// ADR-028
package cmd

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/contracts"
	"maleolabs.com/anvil/internal/output"
)

// adapterInspectCmd represents the "anvil adapter inspect" command that
// displays an adapter's declared capabilities and configuration keys.
//
// Reference: TS-007-032
var adapterInspectCmd = &cobra.Command{
	Use:   "inspect <name>",
	Short: "Inspect an adapter's capabilities",
	Long: `Display the capabilities of a framework adapter.

The command resolves the adapter executable (anvil-adapter-<name> on
PATH, 005-adapter-command-contract §10) and asks it for its declared
capabilities and configuration keys.

Sections:
  Deployment Model    server, hybrid, or package (ADR-016)
  Build Phases        build pipeline phases
  Activation Phases   activation phases (server model)
  Verification Checks verification checks with descriptions
  Config Keys         framework-specific configuration keys with
                      description, default, and required flag

Hybrid-model adapters (e.g. flutter) declare no activation phases;
the section renders "(none)".

Examples:
  anvil adapter inspect laravel
  anvil adapter inspect flutter --json`,
	Args:         ExactArgsWithUsage(1, "anvil adapter inspect laravel", "name"),
	SilenceUsage: true,
	RunE:         runAdapterInspect,
	Deprecated:   adapterInspectDeprecationNotice,
}

func init() {
	AddJSONFlag(adapterInspectCmd)
}

// adapterInspectJSON is the machine-readable inspect output: the full
// capability declaration and configuration extension, wrapped in the
// standard envelope (TS-P8-05).
type adapterInspectJSON struct {
	Framework       string                          `json:"framework"`
	Executable      string                          `json:"executable"`
	Capabilities    contracts.CapabilityDeclaration `json:"capabilities"`
	ConfigExtension contracts.ConfigExtension       `json:"config_extension"`
}

// runAdapterInspect executes the inspect command.
//
// Reference: TS-007-032, TS-017-02-02
func runAdapterInspect(cmd *cobra.Command, args []string) error {
	name := args[0]
	jsonOutput, _ := cmd.Flags().GetBool("json")

	// The adapter's identity is established through the registry: its
	// standard must be RECORDED (installed through the registry with
	// the ADR-022 trust validation). The Core performs no binary scan
	// (TS-017-02-02) and carries no known-framework catalog (ADR-026) —
	// the hint resolves installed delivery lifecycle standards through
	// the registry client. A store that cannot be read is a distinct
	// error, never silently "unknown adapter" (team review F5).
	installed, err := installedAdapterVersions()
	if err != nil {
		return ReportError(cmd, &output.AppError{
			Message:    "could not read the installed-standard store",
			Reason:     err.Error(),
			Resolution: "Fix or remove the corrupt record file(s) in the installed-standard store, or re-adopt the standard with 'anvil adapter install <name> --force' (registry-based, trust-validated)",
			Err:        err,
		})
	}
	if _, ok := installed[name]; !ok {
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("unknown adapter %q", name),
			Reason:     adapterResolutionHint(),
			Resolution: "Run 'anvil adapter list' to see the installed adapters (registry records), or 'anvil adapter list --available' to see the adapters offered in the registry index",
		})
	}

	// The executable resolves through the executable resolution
	// contract (anvil-adapter-<name> on PATH, ADR-025 decision 4): the
	// standard is recorded, but the binary may not be installed — the
	// error names the adoption path.
	executable, err := resolveAdapterExecutable(name)
	if err != nil {
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("no adapter binary found for %q", name),
			Reason:     fmt.Sprintf("the standard %q is installed, but the adapter executable %q was not found on PATH", adapterStandardIDForName(name), "anvil-adapter-"+name),
			Resolution: "Run 'anvil adapter install <name>' to install the adapter binary from the standard's release (registry-based, trust-validated)",
			Err:        err,
		})
	}

	capResult, err := invokeAdapterCapabilities(cmd.Context(), name, executable)
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not inspect adapter %q: %v", name, err)
	}

	extResult, err := invokeAdapterConfigExtension(cmd.Context(), name, executable)
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not inspect adapter %q: %v", name, err)
	}

	if jsonOutput {
		return WriteJSON(cmd, adapterInspectJSON{
			Framework:       name,
			Executable:      executable,
			Capabilities:    capResult.Declaration,
			ConfigExtension: extResult.Extension,
		})
	}

	renderAdapterInspect(cmd, name, capResult.Declaration, extResult.Extension)
	return nil
}

// renderAdapterInspect writes the human-readable inspect output. Sections
// render in a fixed order and empty lists render as "(none)" so the
// output stays deterministic across adapters (hybrid-model adapters
// declare no activation phases).
func renderAdapterInspect(cmd *cobra.Command, name string, decl contracts.CapabilityDeclaration, ext contracts.ConfigExtension) {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "Adapter: %s\n", name)
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Deployment Model:")
	model := decl.DeploymentModel
	if model == "" {
		model = "(none)"
	}
	fmt.Fprintf(w, "  %s\n", model)
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Build Phases:")
	renderAdapterNameList(w, decl.BuildPhases)
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Activation Phases:")
	renderAdapterNameList(w, decl.ActivationPhases)
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Verification Checks:")
	if len(decl.VerificationChecks) == 0 {
		fmt.Fprintln(w, "  (none)")
	} else {
		for _, check := range decl.VerificationChecks {
			fmt.Fprintf(w, "  %s — %s\n", check.Name, check.Description)
		}
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Config Keys:")
	if len(ext.Keys) == 0 {
		fmt.Fprintln(w, "  (none)")
	} else {
		for _, key := range ext.Keys {
			fmt.Fprintf(w, "  %s\n", key.Name)
			if key.Description != "" {
				fmt.Fprintf(w, "    %s\n", key.Description)
			}
			defaultText := key.Default
			if defaultText == "" {
				defaultText = "none"
			}
			fmt.Fprintf(w, "    default: %s, required: %t\n", defaultText, key.Required)
		}
	}
}

// renderAdapterNameList writes an indented list of names, or "(none)"
// when the list is empty.
func renderAdapterNameList(w io.Writer, items []string) {
	if len(items) == 0 {
		fmt.Fprintln(w, "  (none)")
		return
	}
	for _, item := range items {
		fmt.Fprintf(w, "  %s\n", item)
	}
}
