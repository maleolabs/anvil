// Package cmd implements the Anvil CLI commands.
//
// ── Adapter Uninstall (TS-007-037, TS-017-02-02) ─────────────────────
//
// "anvil adapter uninstall <name>" removes an installed adapter binary
// from the directory where the CLI lives AND the installed-standard
// record of its standard (post-gate, TS-017-02-02, "installed" is the
// registry record — uninstall removes both halves of the installed
// state):
//
//   - the name must be a safe identifier (letters, digits, '-' and '_'
//     only) — the name becomes the removed file name, so anything else
//     is rejected before any filesystem activity so an accidental
//     typo can never remove an unrelated "anvil-adapter-*" file
//     (TS-007-037 §3, §7). The Core no longer whitelists a
//     known-framework set (ADR-026): any identifier may be named, and
//     the command only ever removes the exact "anvil-adapter-<name>"
//     file next to the CLI;
//   - the binary removed is exactly "anvil-adapter-<name>" next to the
//     current executable (os.Executable()) — the same directory used by
//     "anvil adapter install" and "anvil update" sync (TS-007-036);
//   - the installed-standard record of anvil-standard-<name> is deleted
//     from the installed-standard store; a missing record is not an
//     error (binary-only installs keep their pre-existing behavior),
//     but a record that EXISTS and cannot be deleted fails the
//     uninstall with an actionable error — uninstall never leaves a
//     stale "installed" record behind silently;
//   - when the adapter is not installed (no binary AND no record), the
//     command reports it and exits 0 (graceful, TS-007-037 AC-5).
//
// --json wraps an {adapter, status, path, message} object in the
// standard envelope (TS-P8-05), identical to the install command shape.
//
// Reference: TS-007-037, TS-017-02-02, ADR-026
package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/registry"
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
binary lives. The adapter name must be a safe identifier (letters,
digits, '-' and '_' only) — anything else is rejected to avoid
accidental file removal. The Core no longer ships a known-framework
catalog (ADR-026), so any installed adapter can be removed by name;
a name without an installed binary reports it and exits
successfully.

Examples:
  anvil adapter uninstall laravel
  anvil adapter uninstall flutter --json`,
	Args:         ExactArgsWithUsage(1, "anvil adapter uninstall laravel", "name"),
	SilenceUsage: true,
	RunE:         runAdapterUninstall,
	Deprecated:   adapterUninstallDeprecationNotice,
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

	// Unsafe adapter names are rejected before any filesystem activity
	// so only "anvil-adapter-*" identifiers can be removed — the name
	// becomes the removed file name, so it must be a plain identifier
	// that cannot escape the install directory (TS-007-037 §7). The
	// former known-framework whitelist (laravel, flutter) was removed
	// with the catalog (ADR-026): the command removes the exact named
	// binary when it exists, and reports "not installed" otherwise.
	if !isInstalledAdapterName(name) {
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("invalid adapter name %q", name),
			Reason:     "adapter names are identifiers (letters, digits, '-' and '_' only) — the name becomes the removed binary file name",
			Resolution: "Run 'anvil adapter list' to see the installed adapters",
		})
	}

	dir, err := adapterInstallDir()
	if err != nil {
		return ReportPlainErrorf(cmd, err, "could not resolve install directory: %v", err)
	}

	targetPath := filepath.Join(dir, adapterBinaryName(name))

	// Post-gate (TS-017-02-02) "installed" is the registry record:
	// uninstall removes BOTH the binary (v1.x surface) and the
	// installed-standard record (the registry-driven installed view).
	binaryExists := true
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		binaryExists = false
	} else if err != nil {
		return ReportPlainErrorf(cmd, err, "could not check for adapter %q: %v", name, err)
	}

	// The record deletion runs before the graceful not-installed return:
	// a record that EXISTS but cannot be deleted must fail the uninstall
	// (never silently leave a stale "installed" record), while a missing
	// record is a normal state for binary-only installs.
	recordRemoved, err := removeAdapterStandardRecord(name)
	if err != nil {
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("could not remove the installed-standard record of %q", name),
			Reason:     err.Error(),
			Resolution: "Fix the installed-standard store, then re-run 'anvil adapter uninstall <name>' to complete the removal",
			Err:        err,
		})
	}

	// Not installed: no binary and no record — graceful info, exit 0
	// (TS-007-037 AC-5).
	if !binaryExists && !recordRemoved {
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
	}

	if binaryExists {
		if err := os.Remove(targetPath); err != nil {
			return ReportPlainErrorf(cmd, err, "could not uninstall adapter %q: %v", name, err)
		}
	}

	msg := fmt.Sprintf("Adapter %s uninstalled from %s.", name, targetPath)
	if recordRemoved {
		msg = fmt.Sprintf("Adapter %s uninstalled from %s (standard %s record removed).", name, targetPath, adapterStandardIDForName(name))
	}
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

// removeAdapterStandardRecord deletes the installed-standard record of
// the adapter's standard (anvil-standard-<name>). It reports whether a
// record was removed; a missing record is NOT an error (binary-only
// installs keep their pre-existing uninstall behavior), but any other
// store failure — including a corrupt record that cannot be removed —
// is surfaced so the uninstall never silently leaves a stale
// "installed" record behind (TS-017-02-02, team review F4).
func removeAdapterStandardRecord(name string) (bool, error) {
	dir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		return false, fmt.Errorf("could not resolve the installed-standard store: %w", err)
	}
	store := registry.NewInstalledStandardStore(dir)
	err = store.Delete(adapterStandardIDForName(name))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, registry.ErrRecordNotFound) {
		return false, nil
	}
	return false, fmt.Errorf("the installed-standard record of %q could not be deleted: %w", adapterStandardIDForName(name), err)
}
