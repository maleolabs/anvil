// Package cmd implements the Anvil CLI commands.
//
// ── Adapter Install (TS-007-037) ─────────────────────────────────────
//
// "anvil adapter install <name> [--force]" downloads the adapter binary
// from the Anvil GitHub release and installs it next to the CLI:
//
//   - the name must be a known adapter (laravel, flutter — the adapters
//     that ship in releases, TS-007-034); anything else is rejected
//     before any network or filesystem activity;
//   - the install directory is the directory of the current executable
//     (os.Executable()), NOT a PATH scan — the same directory where
//     "anvil update" looks for installed adapters (TS-007-036);
//   - when the adapter is already installed and --force is absent, the
//     command reports it and exits 0 without touching the file (mirrors
//     the "already active" UX of "anvil adapter use", TS-007-033);
//   - the release asset "anvil-adapter-<name>-<goos>-<goarch>" is
//     downloaded to a temp file, its sha256 is verified against
//     SHA256SUMS.txt (verifyChecksum), the file is chmod 0755, and it is
//     atomically placed as "anvil-adapter-<name>" (replaceBinary) — the
//     same flow as "anvil update" (cmd/update.go) and the update adapter
//     sync (cmd/adapter_binary.go, TS-007-036);
//   - non-JSON runs report the download/verify/install phases through
//     the StepReporter (TS-008-009) exactly like "anvil update", so
//     interactive users see live progress instead of a silent hang;
//     --json output is never polluted by progress lines.
//
// --json wraps an {adapter, status, path, message} object in the
// standard envelope (TS-P8-05), consistent with "anvil adapter
// list"/"inspect" conventions.
//
// Reference: TS-007-037, TS-007-034, TS-007-036
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/output"
)

// adapterInstallCmd represents the "anvil adapter install" command that
// installs an adapter binary from the release.
//
// Reference: TS-007-037
var adapterInstallCmd = &cobra.Command{
	Use:   "install <name>",
	Short: "Install an adapter binary from the release",
	Long: `Install a framework adapter binary from the Anvil release.

Downloads anvil-adapter-<name>-<os>-<arch> from the latest GitHub
release, verifies its checksum against SHA256SUMS.txt, and installs
it as anvil-adapter-<name> next to the anvil CLI binary.

Only known adapters can be installed:
  laravel, flutter

When the adapter is already installed, the command reports it and
does nothing; use --force to reinstall (redownload, reverify,
replace).

Examples:
  anvil adapter install laravel
  anvil adapter install flutter --force
  anvil adapter install laravel --json`,
	Args:         ExactArgsWithUsage(1, "anvil adapter install laravel", "name"),
	SilenceUsage: true,
	RunE:         runAdapterInstall,
}

func init() {
	AddJSONFlag(adapterInstallCmd)
	adapterInstallCmd.Flags().Bool("force", false, "Reinstall even if the adapter is already installed")
}

// adapterBinaryResult is the machine-readable result of adapter install
// and uninstall: the adapter name, the operation status, the binary
// path, and a human-readable message — wrapped in the standard envelope
// (TS-P8-05).
type adapterBinaryResult struct {
	Adapter string `json:"adapter"`
	Status  string `json:"status"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

// runAdapterInstall executes the install command.
//
// Reference: TS-007-037
func runAdapterInstall(cmd *cobra.Command, args []string) error {
	name := args[0]
	jsonOutput, _ := cmd.Flags().GetBool("json")
	force, _ := cmd.Flags().GetBool("force")

	// Unknown adapter names are rejected before any download so the error
	// names the adapter itself and the supported set (TS-007-037 §3).
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

	binaryName := adapterBinaryName(name)
	targetPath := filepath.Join(dir, binaryName)

	// Already installed without --force: informative, non-fatal (exit 0) —
	// mirrors the "already active" reporting of "anvil adapter use".
	if !force {
		if _, err := os.Stat(targetPath); err == nil {
			msg := fmt.Sprintf("Adapter %s is already installed at %s. Use --force to reinstall.", name, targetPath)
			if jsonOutput {
				return WriteJSON(cmd, adapterBinaryResult{
					Adapter: name,
					Status:  "already installed",
					Path:    targetPath,
					Message: msg,
				})
			}
			PrintSuccessf(cmd, "%s", msg)
			return nil
		} else if !os.IsNotExist(err) {
			return ReportPlainErrorf(cmd, err, "could not check for existing adapter %q: %v", name, err)
		}
	}

	// ── Install with live progress (mirrors "anvil update") ──
	// Non-JSON runs report each phase (download → verify → install) so
	// users see the process is alive; --json stays machine-readable and
	// emits only the envelope.
	var reporter output.StepReporter
	overallStart := time.Now()
	if !jsonOutput {
		reporter = output.NewStepReporter(cmd.OutOrStdout())
		reporter.Start(fmt.Sprintf("Install adapter %s", name))
		// 3 fixed phases: download, verify checksum, install.
		reporter.SetTotal(3)
	}

	if err := installBinaryFromRelease(adapterBinaryOps, adapterAssetName(name), targetPath, reporter); err != nil {
		if reporter != nil {
			reporter.Failed(fmt.Sprintf("Install adapter %s", name), time.Since(overallStart))
		}
		return ReportPlainErrorf(cmd, err, "could not install adapter %q: %v", name, err)
	}

	msg := fmt.Sprintf("Adapter %s installed to %s.", name, targetPath)
	if jsonOutput {
		return WriteJSON(cmd, adapterBinaryResult{
			Adapter: name,
			Status:  "installed",
			Path:    targetPath,
			Message: msg,
		})
	}
	reporter.Complete(msg, time.Since(overallStart))
	return nil
}
