// Package cmd implements the Anvil CLI commands.
//
// ── Adapter Uninstall (TS-007-037) ───────────────────────────────────
//
// "anvil adapter uninstall <name>" removes an installed adapter binary
// from the directory where the CLI lives:
//
//   - the name must be a known adapter (laravel, flutter); anything else
//     is rejected before any filesystem activity so an accidental
//     typo can never remove an unrelated "anvil-adapter-*" file
//     (TS-007-037 §3, §7);
//   - the binary removed is exactly "anvil-adapter-<name>" next to the
//     current executable (os.Executable()) — the same directory used by
//     "anvil adapter install" and "anvil update" sync (TS-007-036);
//   - when the adapter is not installed, the command reports it and
//     exits 0 (graceful, TS-007-037 AC-5).
//
// --json wraps an {adapter, status, path, message} object in the
// standard envelope (TS-P8-05), identical to the install command shape.
//
// Reference: TS-007-037
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/output"
)

// adapterUninstallCmd represents the "anvil adapter uninstall" command
// that removes an installed adapter binary.
//
// Reference: TS-007-037
var adapterUninstallCmd = &cobra.Command{
	Use:   "uninstall <name>",
	Short: "Uninstall an installed adapter binary",
	Long: `Remove a framework adapter binary installed by the CLI.

Removes anvil-adapter-<name> from the directory where the anvil CLI
binary lives. Only known adapters can be uninstalled:
  laravel, flutter
Anything else is rejected to avoid accidental file removal.

When the adapter is not installed, the command reports it and exits
successfully.

Examples:
  anvil adapter uninstall laravel
  anvil adapter uninstall flutter --json`,
	Args:         ExactArgsWithUsage(1, "anvil adapter uninstall laravel", "name"),
	SilenceUsage: true,
	RunE:         runAdapterUninstall,
}

func init() {
	AddJSONFlag(adapterUninstallCmd)
}

// runAdapterUninstall executes the uninstall command.
//
// Reference: TS-007-037
func runAdapterUninstall(cmd *cobra.Command, args []string) error {
	name := args[0]
	jsonOutput, _ := cmd.Flags().GetBool("json")

	// Unknown adapter names are rejected before any filesystem activity
	// so only whitelisted "anvil-adapter-*" identifiers can be removed
	// (TS-007-037 §7).
	if !isKnownAdapterFramework(name) {
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("unknown adapter %q", name),
			Reason:     fmt.Sprintf("known adapters: %s", strings.Join(adapterKnownFrameworks(), ", ")),
			Resolution: "Run 'anvil adapter list' to see available adapters",
		})
	}

	dir, err := adapterInstallDir()
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not resolve install directory: %v", err)
	}

	targetPath := filepath.Join(dir, adapterBinaryName(name))

	// Not installed: graceful info, exit 0 (TS-007-037 AC-5).
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		msg := fmt.Sprintf("Adapter %s is not installed at %s.", name, targetPath)
		if jsonOutput {
			return WriteJSON(cmd, adapterBinaryResult{
				Adapter: name,
				Status:  "not installed",
				Path:    targetPath,
				Message: msg,
			})
		}
		PrintSuccessf(cmd, "%s", msg)
		return nil
	} else if err != nil {
		return ReportPlainErrorf(cmd, err, "could not check for adapter %q: %v", name, err)
	}

	if err := os.Remove(targetPath); err != nil {
		return ReportPlainErrorf(cmd, err, "could not uninstall adapter %q: %v", name, err)
	}

	msg := fmt.Sprintf("Adapter %s uninstalled from %s.", name, targetPath)
	if jsonOutput {
		return WriteJSON(cmd, adapterBinaryResult{
			Adapter: name,
			Status:  "uninstalled",
			Path:    targetPath,
			Message: msg,
		})
	}
	PrintSuccessf(cmd, "%s", msg)
	return nil
}
