// Package cmd implements the Anvil CLI commands.
//
// ── Adapter Install (TS-007-037, TS-016-04-01) ───────────────────────
//
// "anvil adapter install <name> [--force]" installs the adapter binary
// for a framework through REGISTRY-BASED resolution (TS-016-04-01,
// ADR-025 §3.5): since the repository split, adapter binaries are
// published by the standard repositories (maleolabs/anvil-standard-
// <name>), never by Core, so the command resolves the standard through
// the registry — the same resolution, validation, and trust gates as
// "anvil standard install" (TS-014-03-01) — and then downloads the
// binary from the standard's release:
//
//   - the name must be a safe identifier (letters, digits, '-' and '_'
//     only) — the name becomes the installed binary file name, so
//     anything else is rejected before any network or filesystem
//     activity; the adapter name maps to the standard id by the
//     identity convention "anvil-standard-<name>" (ADR-021 §3.1);
//   - the standard release is resolved from the static registry index
//     (--index / ANVIL_REGISTRY_INDEX / default): the RECORDED version
//     when the standard is already installed (the binary is pinned to
//     the installed standard — version change is an update, TS-014-03-02),
//     else the highest adoptable version offered in the index;
//   - the adoption runs the full "anvil standard install" gates: strict
//     parse, lifecycle + compatibility, trust anchors BEFORE the fetch
//     (missing anchors fail with an actionable error — there is no
//     default anchor and no first-use acceptance, ADR-022 §3), release
//     content fetched under the ADR-030 policy, and VerifyTrust
//     (integrity + attestation + anchor) after it. The adoption is
//     recorded (identity-plus-version idempotent) before the binary
//     phase, so the registry path is complete even if the binary
//     download fails;
//   - the adapter binary "anvil-adapter-<name>-<goos>-<goarch>" is then
//     downloaded from the SAME release (the standard repository's
//     release channel, version-pinned); its sha256 is verified against
//     the attestation-bound digest the release's registry metadata
//     document declares for the asset (registry.VerifyAssetDigest,
//     TS-014-04-04) — the digest is covered by the publisher's Ed25519
//     signature, superseding the same-channel, unsigned SHA256SUMS.txt;
//     a release without the material (e.g. already-published v1.0.0)
//     degrades to the SHA256SUMS.txt check with an explicit warning —
//     never a silent trust downgrade. The file is then chmod 0755 and
//     atomically placed as "anvil-adapter-<name>" next to the CLI
//     (replaceBinary — the same flow as "anvil update" (cmd/update.go)
//     and the update adapter sync (cmd/adapter_binary.go, TS-007-036);
//   - the install directory is the directory of the current executable
//     (os.Executable()), NOT a PATH scan — the same directory where
//     "anvil update" looks for installed adapters (TS-007-036);
//   - when the adapter is already installed and --force is absent, the
//     command reports it and exits 0 without touching the file (mirrors
//     the "already active" UX of "anvil adapter use", TS-007-033);
//   - non-JSON runs report the phases through the StepReporter
//     (TS-008-009): resolve from the registry index → verify the
//     release (integrity, attestation, trust anchors) → download →
//     verify binary (attestation-bound, checksum fallback) → install;
//     --json output is never polluted by progress lines.
//
// --json wraps an {adapter, standard, version, status, path, message}
// object in the standard envelope (TS-P8-05), consistent with "anvil
// adapter list"/"inspect" conventions; the standard and version fields
// carry the adopted release.
//
// Reference: TS-007-037, TS-016-04-01, TS-014-03-01, TS-007-034,
// TS-007-036, ADR-021 §3.1, ADR-022 §3, ADR-025 §3.5, ADR-026, ADR-030
package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/registry"
)

// adapterInstallCmd represents the "anvil adapter install" command that
// installs an adapter binary through registry-based resolution.
//
// Reference: TS-007-037, TS-016-04-01
var adapterInstallCmd = &cobra.Command{
	Use:   "install <name>",
	Short: "Install an adapter binary through the registry",
	Long: `Install a framework adapter binary through registry-based resolution.

Resolves the standard anvil-standard-<name> from the static registry
index (the identity convention, ADR-021 §3.1), validates the release
with the full adoption gates — strict metadata parse, lifecycle and
compatibility, release content integrity, publisher attestation, and
the operator's trust anchor allowlist (ADR-022 — there is no skip or
insecure path) — records the adopted standard, and downloads
anvil-adapter-<name>-<os>-<arch> from the standard's release,
verifying it against the attestation-bound digest declared in the
release's registry metadata document (TS-014-04-04); releases without
that material (e.g. already-published v1.0.0) fall back to the
release's SHA256SUMS.txt with an explicit warning. The binary is then
installed as anvil-adapter-<name> next to the anvil CLI binary.

The adapter name must be a safe identifier (letters, digits, '-' and
'_' only) — it becomes the installed binary file name. The version is
the recorded version when the standard is already installed (the
binary is pinned to the installed standard); otherwise the highest
adoptable version offered in the index is adopted. Installations
never happen implicitly — the command is the explicit surface.

Index resolution order:
  1. --index <path>
  2. the ANVIL_REGISTRY_INDEX environment variable
  3. the default <user config dir>/anvil/registry
     (e.g. ~/.config/anvil/registry on Linux)

Trust anchors resolution order:
  1. --trust-anchors <path>
  2. the ANVIL_TRUST_ANCHORS environment variable
  3. the default <user config dir>/anvil/trust-anchors.json
     (e.g. ~/.config/anvil/trust-anchors.json on Linux)

Without an anchored key for the standard's publisher the install
fails: there is no first-use acceptance (ADR-022 §3).

When the adapter is already installed, the command reports it and
does nothing; use --force to reinstall (redownload, reverify,
replace).

Examples:
  anvil adapter install laravel
  anvil adapter install flutter --force
  anvil adapter install laravel --index ./registry
  anvil adapter install laravel --trust-anchors ./anchors.json
  anvil adapter install laravel --json`,
	Args:         ExactArgsWithUsage(1, "anvil adapter install laravel", "name"),
	SilenceUsage: true,
	RunE:         runAdapterInstall,
	Deprecated:   adapterInstallDeprecationNotice,
}

func init() {
	AddJSONFlag(adapterInstallCmd)
	adapterInstallCmd.Flags().Bool("force", false, "Reinstall even if the adapter is already installed")
	adapterInstallCmd.Flags().String("index", "", "path to the static registry index directory (default: $ANVIL_REGISTRY_INDEX, else <user config dir>/anvil/registry)")
	adapterInstallCmd.Flags().String("trust-anchors", "", "path to the trust anchors allowlist file (default: $ANVIL_TRUST_ANCHORS, else <user config dir>/anvil/trust-anchors.json)")
}

// adapterBinaryResult is the machine-readable result of adapter install
// and uninstall: the adapter name, the operation status, the binary
// path, and a human-readable message — wrapped in the standard envelope
// (TS-P8-05). The standard and version fields carry the adopted release
// of a registry-based install (TS-016-04-01).
type adapterBinaryResult struct {
	Adapter  string `json:"adapter"`
	Standard string `json:"standard,omitempty"`
	Version  string `json:"version,omitempty"`
	Status   string `json:"status"`
	Path     string `json:"path"`
	Message  string `json:"message"`
}

// runAdapterInstall executes the install command.
//
// Reference: TS-007-037, TS-016-04-01
func runAdapterInstall(cmd *cobra.Command, args []string) error {
	s := styleFor(cmd)
	name := args[0]
	jsonOutput, _ := cmd.Flags().GetBool("json")
	force, _ := cmd.Flags().GetBool("force")

	// Unsafe adapter names are rejected before any download so the name
	// can never escape the install directory as a path component: the
	// name becomes the installed binary file name ("anvil-adapter-<name>"
	// next to the CLI), so it must be a plain identifier.
	if !isInstalledAdapterName(name) {
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("invalid adapter name %q", name),
			Reason:     "adapter names are identifiers (letters, digits, '-' and '_' only) — the name becomes the installed binary file name",
			Resolution: "Run 'anvil adapter list --available' to see the adapters offered in the registry",
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
	// --force also completes the standard adoption through the registry
	// (trust-validated, ADR-022).
	if !force {
		if _, err := os.Stat(targetPath); err == nil {
			msg := fmt.Sprintf("Adapter %s is already installed at %s. Use --force to reinstall (--force also completes the standard adoption through the registry, trust-validated).", name, targetPath)
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

	// ── Registry-based install with live progress ──
	// Non-JSON runs report each phase (resolve → verify → download →
	// verify binary → install; the adoption is recorded between verify
	// and download, without its own step) so users see the process is
	// alive; --json stays machine-readable and emits only the envelope.
	var reporter output.StepReporter
	overallStart := time.Now()
	if !jsonOutput {
		reporter = output.NewStepReporter(s.W)
		reporter.Start(fmt.Sprintf("Install adapter %s", name))
		// 5 rendered phases: resolve from the registry index, verify the
		// release, download the binary, verify binary (attestation-bound,
		// checksum fallback), install.
		reporter.SetTotal(5)
	}

	fail := func(stepName string, start time.Time) {
		if reporter != nil {
			reporter.Failed(fmt.Sprintf("Install adapter %s", name), time.Since(start))
		}
	}

	// ── 1. Registry resolution + pre-fetch validation ──
	var (
		entry         registry.Entry
		md            *registry.Metadata
		parseWarnings []registry.Warning
		before        registry.AdoptionResult
	)
	err = reportStep(reporter, "Resolve "+adapterStandardIDForName(name)+" from the registry index", func() error {
		var resolveErr error
		entry, md, parseWarnings, before, resolveErr = resolveAdapterStandardForInstall(cmd, name)
		return resolveErr
	})
	if err != nil {
		fail("Resolve standard", overallStart)
		return reportAdapterInstallError(cmd, err, name)
	}

	// ── 2. Verify the release (anchors → fetch → trust) ──
	var (
		adoption      registry.AdoptionResult
		contentSource string
	)
	err = reportStep(reporter, "Verify release (integrity, attestation, trust anchors)", func() error {
		var verifyErr error
		adoption, contentSource, _, verifyErr = verifyAdapterStandardAdoption(cmd, *md, before)
		return verifyErr
	})
	if err != nil {
		fail("Verify release", overallStart)
		return reportAdapterInstallError(cmd, err, name)
	}

	// ── 3. Record the standard adoption (registry path) ──
	// The record is the durable half of the registry path: the adoption
	// is recorded BEFORE the binary download, so a binary-phase failure
	// (e.g. network) leaves a recorded, validated adoption — re-running
	// the install re-validates idempotently.
	recordResult, err := recordAdapterStandardAdoption(cmd, *md, contentSource, adoption, parseWarnings)
	if err != nil {
		fail("Record standard", overallStart)
		return err
	}

	// ── 4. Install the binary from the standard's release channel ──
	// The release base derives from the registry metadata's distribution
	// location: the binary and its checksums live in the same release
	// download directory as the validated release content (ADR-030).
	releaseBase, err := standardReleaseDownloadBase(entry.Distribution.Location)
	if err != nil {
		fail("Resolve release channel", overallStart)
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("could not resolve the release channel of standard %q", md.ID),
			Reason:     err.Error(),
			Resolution: "Fix the metadata document's distribution declaration, then run the install again",
			Err:        err,
		})
	}
	assetName := adapterAssetName(name)
	notice, err := installBinaryFromRelease(standardAdapterBinaryOpsTrusted(releaseBase, md), assetName, targetPath, reporter)
	if err != nil {
		fail("Install binary", overallStart)
		return ReportError(cmd, &output.AppError{
			Message:    fmt.Sprintf("could not install adapter %q", name),
			Reason:     err.Error(),
			Resolution: "Run 'anvil adapter list --available' to see the adapters offered in the registry",
			Err:        err,
		})
	}
	// Explicit degradation notice when the release carried no
	// attestation-bound binary digest and the verification fell back to
	// the same-channel checksum (TS-014-04-04) — never a silent trust
	// downgrade.
	if notice != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", output.Yellow(cmd.ErrOrStderr(), notice))
	}

	msg := fmt.Sprintf("Adapter %s installed to %s (standard %s %s recorded).", name, targetPath, recordResult.ID, recordResult.Version)
	if jsonOutput {
		return WriteJSON(cmd, adapterBinaryResult{
			Adapter:  name,
			Standard: recordResult.ID,
			Version:  recordResult.Version,
			Status:   "installed",
			Path:     targetPath,
			Message:  msg,
		})
	}
	reporter.Complete(msg, time.Since(overallStart))
	return nil
}

// reportAdapterInstallError renders a registry-side adapter install
// failure with the index/anchors-specific actionable guidance: a missing
// index directory is a not-found failure (exit 3, ExitCodeRuntime —
// "resource not found", TS-P8-07 / ADR-010 §8.1), consistent with the
// standard commands; anything else is a general error (exit 1).
func reportAdapterInstallError(cmd *cobra.Command, err error, name string) error {
	if errors.Is(err, registry.ErrIndexNotFound) || errors.Is(err, registry.ErrEntryNotFound) {
		return ReportErrorWithCode(cmd, &output.AppError{
			Message:    fmt.Sprintf("could not resolve adapter %q through the registry", name),
			Reason:     err.Error(),
			Resolution: "Set the index directory with --index <path> or the " + envStandardIndex + " environment variable (default: " + defaultStandardIndexDescription() + "), then run the install again",
			Err:        err,
		}, output.ExitCodeRuntime)
	}
	return ReportError(cmd, &output.AppError{
		Message:    fmt.Sprintf("could not install adapter %q", name),
		Reason:     err.Error(),
		Resolution: "Run 'anvil adapter list --available' to see the adapters offered in the registry",
		Err:        err,
	})
}
