// Package cmd implements the Anvil CLI commands.
//
// Reference: ADR-010, ADR-012
package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"maleolabs.com/anvil/internal/output"
)

const (
	// updateRepo is the public repository where releases are published.
	updateRepo = "maleolabs/anvil"

	// updateAPITemplate is the GitHub API URL for the latest release.
	updateAPITemplate = "https://api.github.com/repos/%s/releases/latest"

	// updateDownloadTemplate is the download URL for a release binary.
	updateDownloadTemplate = "https://github.com/%s/releases/latest/download/%s"
)

// updateCmd represents the 'anvil update' command.
var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update Anvil CLI to the latest version",
	Long: `Check for the latest Anvil release and update the CLI binary.

Downloads the latest version from GitHub Releases, verifies the
binary checksum, and replaces the current executable. After the CLI
is updated, any adapter binaries already installed next to it
(anvil-adapter-<name>) are refreshed to the release version of their
RECORDED delivery lifecycle standard (TS-016-04-01): the sync
resolves each standard's release channel from its installed-standard
record — the post-split distribution path (ADR-025 §3.5, ADR-030) —
and never fetches adapter binaries from the Core release. Adapters
without a recorded standard are skipped with a hint pointing at the
full-gate registry adoption ('anvil adapter install <name> --force');
adapters that are not installed are never created.

Uses the public mirror repository (maleolabs/anvil) as the CLI update
source. Requires write permission to the installation directory.

Examples:
  anvil update                          # check and apply update
  anvil update --check                  # only check, don't apply
  anvil update --force                  # update even if same version`,
	Args: cobra.NoArgs,
	RunE: runUpdate,
}

// ghRelease is a partial GitHub Release API response.
type ghRelease struct {
	TagName string           `json:"tag_name"`
	Assets  []ghReleaseAsset `json:"assets"`
}

// ghReleaseAsset is one asset entry of the GitHub Release API response.
type ghReleaseAsset struct {
	Name string `json:"name"`
}

func init() {
	rootCmd.AddCommand(updateCmd)

	updateCmd.Flags().Bool("check", false, "Only check for updates, don't apply")
	updateCmd.Flags().Bool("force", false, "Update even if already at the latest version")
}

func runUpdate(cmd *cobra.Command, args []string) error {
	checkOnly, _ := cmd.Flags().GetBool("check")
	force, _ := cmd.Flags().GetBool("force")

	currentVersion := strings.TrimPrefix(CliVersion, "v")

	// Create step reporter for progress feedback
	reporter := output.NewStepReporter(cmd.OutOrStdout())
	overallStart := time.Now()
	reporter.Start("Update Anvil CLI")

	// ── Step 1: Fetch latest release from GitHub API ──
	reporter.StepStart("Check latest version")
	stepStart := time.Now()
	latestVersion, err := fetchLatestVersion()
	if err != nil {
		reporter.StepFailed("Check latest version", time.Since(stepStart), err)
		reporter.Failed("Update Anvil CLI", time.Since(overallStart))
		return fmt.Errorf("check for updates: %w", err)
	}
	latestVersion = strings.TrimPrefix(latestVersion, "v")
	reporter.StepComplete("Check latest version", time.Since(stepStart))

	// ── Compare versions ──
	if currentVersion == latestVersion && !force {
		reporter.Complete(fmt.Sprintf("Already up to date (v%s)", currentVersion), time.Since(overallStart))
		return nil
	}

	if checkOnly {
		reporter.Complete(fmt.Sprintf("Update available: v%s → v%s", currentVersion, latestVersion), time.Since(overallStart))
		fmt.Fprintln(cmd.OutOrStdout(), "Run 'anvil update' to apply.")
		return nil
	}

	// ── Apply update ──
	// Determine binary name for current platform
	binaryName := fmt.Sprintf("anvil-%s-%s", runtime.GOOS, runtime.GOARCH)
	downloadURL := fmt.Sprintf(updateDownloadTemplate, updateRepo, binaryName)

	// Get current executable path
	execPath, err := os.Executable()
	if err != nil {
		reporter.Failed("Update Anvil CLI", time.Since(overallStart))
		return fmt.Errorf("get executable path: %w", err)
	}

	// Declare the expected step count so the tree renders "└─" on the
	// last step: 4 fixed steps (check/download/verify/install) plus one
	// per installed adapter synced afterwards. The scan is best-effort —
	// if it fails, the total degrades to 4 and the sync failure step is
	// the only step rendered after install.
	installedAdapters, _ := listInstalledAdapters(filepath.Dir(execPath))
	reporter.SetTotal(4 + len(installedAdapters))

	// Create temp file for download
	tmpFile, err := os.CreateTemp("", "anvil-update-*")
	if err != nil {
		reporter.Failed("Update Anvil CLI", time.Since(overallStart))
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// ── Step 2: Download binary ──
	reporter.StepStart(fmt.Sprintf("Download v%s", latestVersion))
	stepStart = time.Now()

	resp, err := httpGet(downloadURL)
	if err != nil {
		reporter.StepFailed(fmt.Sprintf("Download v%s", latestVersion), time.Since(stepStart), err)
		reporter.Failed("Update Anvil CLI", time.Since(overallStart))
		return fmt.Errorf("download %s: %w", binaryName, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		reporter.StepFailed(fmt.Sprintf("Download v%s", latestVersion), time.Since(stepStart),
			fmt.Errorf("HTTP %d", resp.StatusCode))
		reporter.Failed("Update Anvil CLI", time.Since(overallStart))
		return fmt.Errorf("download %s: server returned HTTP %d", binaryName, resp.StatusCode)
	}

	// Write to temp file while computing sha256
	hash := sha256.New()
	writer := io.MultiWriter(tmpFile, hash)
	if _, err := io.Copy(writer, resp.Body); err != nil {
		tmpFile.Close()
		reporter.StepFailed(fmt.Sprintf("Download v%s", latestVersion), time.Since(stepStart), err)
		reporter.Failed("Update Anvil CLI", time.Since(overallStart))
		return fmt.Errorf("download %s: %w", binaryName, err)
	}
	tmpFile.Close()
	reporter.StepComplete(fmt.Sprintf("Download v%s", latestVersion), time.Since(stepStart))

	downloadedHash := hex.EncodeToString(hash.Sum(nil))

	// ── Step 3: Verify checksum ──
	reporter.StepStart("Verify checksum")
	stepStart = time.Now()
	if err := verifyChecksum(binaryName, downloadedHash); err != nil {
		reporter.StepFailed("Verify checksum", time.Since(stepStart), err)
		reporter.Failed("Update Anvil CLI", time.Since(overallStart))
		return fmt.Errorf("checksum verification failed: %w", err)
	}
	reporter.StepComplete("Verify checksum", time.Since(stepStart))

	// Make temp file executable
	if err := os.Chmod(tmpPath, 0755); err != nil {
		reporter.Failed("Update Anvil CLI", time.Since(overallStart))
		return fmt.Errorf("set executable permission: %w", err)
	}

	// ── Step 4: Install ──
	installLabel := fmt.Sprintf("Install to %s", execPath)
	reporter.StepStart(installLabel)
	stepStart = time.Now()
	if err := replaceBinary(tmpPath, execPath); err != nil {
		reporter.StepFailed(installLabel, time.Since(stepStart), err)
		reporter.Failed("Update Anvil CLI", time.Since(overallStart))
		return fmt.Errorf("install update: %w", err)
	}
	reporter.StepComplete(installLabel, time.Since(stepStart))

	// ── Adapter sync (TS-007-036, TS-016-04-01) ──
	// Best-effort: refresh adapter binaries already installed next to the
	// CLI to the release version of their RECORDED standard (the sync
	// resolves the standard repositories' release channels from the
	// installed-standard records — never the Core release — and honors
	// the recorded version pins; unrecorded adapters are skipped with a
	// hint pointing at 'anvil adapter install <name> --force', the
	// full-gate registry adoption). Nothing new is installed, failures
	// are reported per adapter, and the CLI update result remains the
	// primary outcome — adapter sync never fails the overall update.
	syncInstalledAdapters(reporter, execPath)

	// ── Complete ──
	reporter.Complete(fmt.Sprintf("Updated to v%s", latestVersion), time.Since(overallStart))
	return nil
}

// fetchLatestVersion gets the latest release tag from the GitHub API.
func fetchLatestVersion() (string, error) {
	apiURL := fmt.Sprintf(updateAPITemplate, updateRepo)

	resp, err := httpGet(apiURL)
	if err != nil {
		return "", fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub API returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var release ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("parse GitHub API response: %w", err)
	}

	if release.TagName == "" {
		return "", fmt.Errorf("no releases found in %s", updateRepo)
	}

	return release.TagName, nil
}

// verifyChecksum downloads SHA256SUMS.txt and verifies the binary hash.
func verifyChecksum(binaryName, downloadedHash string) error {
	checksumsURL := fmt.Sprintf("https://github.com/%s/releases/latest/download/SHA256SUMS.txt", updateRepo)
	return verifyChecksumFromURLClient(httpGet, checksumsURL, binaryName, downloadedHash)
}

// replaceBinary atomically replaces the current binary with the downloaded one.
//
// On Linux, os.Rename works within the same filesystem. When cross-device
// rename fails (e.g., /tmp on tmpfs vs /usr/local/bin on ext4), execution
// falls back to replaceBinaryFallback, which restores the same atomicity by
// staging the new binary in the target directory before renaming it over
// the old one (TD-003). The running binary stays in memory via its inode —
// this is the standard Linux update pattern.
func replaceBinary(tmpPath, targetPath string) error {
	// Try direct rename (works on same filesystem)
	if err := os.Rename(tmpPath, targetPath); err == nil {
		return nil
	}

	return replaceBinaryFallback(tmpPath, targetPath)
}

// replaceBinaryFallbackAfterCopy and replaceBinaryFallbackAfterRename are
// test-only failure-injection seams inside replaceBinaryFallback (TD-003).
// The production values are nil; tests set them to simulate a crash at each
// step boundary and assert the atomic-replacement invariant — the target
// path always contains either the old binary or the complete new binary.
var (
	replaceBinaryFallbackAfterCopy   func() error
	replaceBinaryFallbackAfterRename func() error
)

// replaceBinaryFallback installs tmpPath at targetPath by staging the new
// binary into a temp file in the target directory and atomically renaming
// it over the target path (cross-device rename fallback).
//
// The primary path (replaceBinary) only works on the same filesystem. When
// the download temp file lives on a different filesystem than the install
// directory, os.Rename fails and this fallback runs. It recreates the
// atomicity of the primary path: the new binary is fully copied and flushed
// into a temp file in the target directory BEFORE any rename, so the final
// rename is a single atomic step that replaces the old binary. The target
// path therefore always contains either the old binary or the complete new
// binary — never nothing, and never a truncated file (TD-003).
//
// A missing target is not an error: this path is reached when installing a
// brand-new binary (e.g. "anvil adapter install" for an adapter that is not
// yet present) and the rename failed only because /tmp lives on a different
// filesystem than the install directory. The staging temp file is created
// independently of the target's existence, so a fresh install proceeds
// without an explicit "old binary" to replace.
//
// Error semantics: an error before the rename leaves the previous binary
// untouched at the target path and removes the staging temp file; an error
// after the rename (the afterRename seam) means the new binary IS already
// in place at the target path.
func replaceBinaryFallback(tmpPath, targetPath string) (err error) {
	dir := filepath.Dir(targetPath)

	// Stage the new binary in a temp file in the target directory: the
	// rename below then stays on the same filesystem and is atomic.
	tmp, err := os.CreateTemp(dir, filepath.Base(targetPath)+".tmp-*")
	if err != nil {
		if os.IsPermission(err) || !isWritable(dir) {
			return fmt.Errorf("cannot write to %s (try running with sudo)", targetPath)
		}
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()

	// On any failure before the rename, remove the staging temp file so a
	// failed install leaves no artifacts behind. The target path is never
	// modified until the rename succeeds.
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	src, err := os.Open(tmpPath)
	if err != nil {
		return fmt.Errorf("open temp file: %w", err)
	}
	defer src.Close()

	if _, err := io.Copy(tmp, src); err != nil {
		return fmt.Errorf("copy binary: %w", err)
	}

	// Flush the content to disk before it becomes visible at the target
	// path (TD-003 §11).
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	// Preserve executable permission semantics: the installed binary is
	// always 0755, matching the primary path.
	if err := os.Chmod(tmpName, 0755); err != nil {
		return fmt.Errorf("set permissions: %w", err)
	}

	// Test-only failure injection: simulate a crash after the copy and
	// before the rename — the target path must still hold the old binary.
	if hook := replaceBinaryFallbackAfterCopy; hook != nil {
		if err := hook(); err != nil {
			return err
		}
	}

	// Atomically replace the old binary: the target path now holds the
	// complete new binary, never a partial one.
	if err := os.Rename(tmpName, targetPath); err != nil {
		return fmt.Errorf("rename temp file to %s: %w", targetPath, err)
	}

	// Test-only failure injection: simulate a crash after the rename and
	// before completion — the target path must hold the complete new
	// binary.
	if hook := replaceBinaryFallbackAfterRename; hook != nil {
		if err := hook(); err != nil {
			return err
		}
	}

	return nil
}

// isWritable checks if a directory is writable by opening a test file.
func isWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".anvil-write-test-*")
	if err != nil {
		return false
	}
	os.Remove(f.Name())
	f.Close()
	return true
}
