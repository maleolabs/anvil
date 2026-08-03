// Package cmd implements the Anvil CLI commands.
//
// ── Adapter Binary Distribution (TS-007-036, TS-007-037) ────────────
//
// This file holds the shared machinery for downloading, verifying, and
// replacing adapter binaries from the GitHub release:
//
//   - name helpers (adapterAssetName, adapterBinaryName) matching the
//     release asset convention "anvil-adapter-<name>-<goos>-<goarch>"
//     and the installed-name convention "anvil-adapter-<name>"
//     (TS-007-034, 004-review-resolutions D1, ADR-009 §8.1);
//   - listInstalledAdapters, the directory scan used by "anvil update"
//     to refresh only adapters the user already installed (TS-007-036);
//   - binaryOps, the injectable download/verify/replace operation set
//     shared by "anvil update" adapter sync and "anvil adapter install".
//     Production values hit the GitHub release (updateRepo /
//     updateDownloadTemplate from cmd/update.go); tests inject fakes so
//     no test touches the network.
//
// The install flow mirrors cmd/update.go's runUpdate steps: download to
// a temp file while computing sha256, verify against SHA256SUMS.txt,
// chmod 0755, atomic replace via replaceBinary.
//
// Reference: TS-007-036, TS-007-037, TS-007-034
package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"maleolabs.com/anvil/internal/output"
)

// adapterBinaryPrefix is the installed executable name prefix for
// framework adapters: "anvil-adapter-<name>" (ADR-009 §8.1,
// 004-review-resolutions D1, 005-adapter-command-contract §10).
const adapterBinaryPrefix = "anvil-adapter-"

// binaryOps bundles the network/filesystem operations needed to install
// one binary from the release. Production values are set in
// adapterBinaryOps; tests inject fakes (no network, no real replaces).
type binaryOps struct {
	// download downloads the release asset assetName into tmpPath while
	// computing its sha256, and returns the hex digest.
	download func(assetName, tmpPath string) (string, error)

	// verify checks downloadedHash against SHA256SUMS.txt for the asset.
	verify func(assetName, downloadedHash string) error

	// replace atomically replaces targetPath with the verified temp file.
	replace func(tmpPath, targetPath string) error
}

// adapterBinaryOps is the production operation set used by "anvil update"
// adapter sync and "anvil adapter install". It is a package-level seam:
// tests replace it to fake download/verify/replace without touching the
// network.
var adapterBinaryOps = binaryOps{
	download: downloadReleaseAsset,
	verify:   verifyChecksum,
	replace:  replaceBinary,
}

// adapterAssetName returns the release asset name for an adapter on the
// current platform: "anvil-adapter-<name>-<goos>-<goarch>" (TS-007-034),
// consistent with the CLI asset naming in cmd/update.go
// (binaryName := fmt.Sprintf("anvil-%s-%s", runtime.GOOS, runtime.GOARCH)).
func adapterAssetName(name string) string {
	return fmt.Sprintf("%s%s-%s-%s", adapterBinaryPrefix, name, runtime.GOOS, runtime.GOARCH)
}

// adapterBinaryName returns the installed executable name for an adapter:
// "anvil-adapter-<name>" (ADR-009 §8.1).
func adapterBinaryName(name string) string {
	return adapterBinaryPrefix + name
}

// adapterInstallDir returns the directory where adapter binaries are
// installed: the directory of the current executable — the same directory
// where the anvil CLI binary lives (TS-007-036 §5, TS-007-037 §3). It is
// a package-level seam so tests can point installation at a temp dir.
var adapterInstallDir = func() (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("get executable path: %w", err)
	}
	return filepath.Dir(execPath), nil
}

// listInstalledAdapters returns the names of adapter binaries installed
// in dir. A file qualifies when its name is exactly
// "anvil-adapter-<name>" — no platform suffix (any release platform),
// no extra tokens. Release asset artifacts
// ("anvil-adapter-<name>-<os>-<arch>"), the CLI binary itself,
// directories, and unrelated files are skipped. Names are returned
// sorted for deterministic sync order.
//
// Reference: TS-007-036 §3, §7
func listInstalledAdapters(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, adapterBinaryPrefix) {
			continue
		}
		rest := strings.TrimPrefix(name, adapterBinaryPrefix)
		if !isInstalledAdapterName(rest) {
			continue
		}
		names = append(names, rest)
	}
	sort.Strings(names)
	return names, nil
}

// adapterReleasePlatforms lists the platform suffixes used in release
// asset names ("-<os>-<arch>") for every platform the release pipeline
// publishes (TS-007-034: linux/darwin × amd64/arm64). Installed adapter
// names never carry a platform suffix, so any name ending with one of
// these suffixes is a release artifact, not an installed adapter — on
// every host, not only the current platform.
var adapterReleasePlatforms = []string{
	"-linux-amd64", "-linux-arm64",
	"-darwin-amd64", "-darwin-arm64",
}

// isInstalledAdapterName reports whether rest (the part after the
// "anvil-adapter-" prefix) is an installed adapter identifier: non-empty
// and without a release asset platform suffix ("-<os>-<arch>" for any
// platform the release publishes). Installed names are exactly
// "anvil-adapter-<name>" (TS-007-036 §3); anything else — including
// platform-suffixed release artifacts sitting in the CLI directory — is
// not an installed adapter and must not be refreshed.
func isInstalledAdapterName(rest string) bool {
	if rest == "" {
		return false
	}
	for _, suffix := range adapterReleasePlatforms {
		if strings.HasSuffix(rest, suffix) {
			return false
		}
	}
	return true
}

// downloadReleaseAsset downloads the release asset assetName into tmpPath
// while computing its sha256, and returns the hex digest. The asset is
// fetched from the latest release via updateDownloadTemplate (the same
// source update.go uses for the CLI binary).
func downloadReleaseAsset(assetName, tmpPath string) (string, error) {
	downloadURL := fmt.Sprintf(updateDownloadTemplate, updateRepo, assetName)

	//nolint:gosec
	resp, err := http.Get(downloadURL)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", assetName, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: server returned HTTP %d", assetName, resp.StatusCode)
	}

	f, err := os.Create(tmpPath)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer f.Close()

	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, hash), resp.Body); err != nil {
		return "", fmt.Errorf("download %s: %w", assetName, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// installBinaryFromRelease downloads assetName from the release, verifies
// its checksum, makes it executable, and atomically replaces targetPath.
// The steps mirror the CLI update flow in cmd/update.go runUpdate
// (download → verifyChecksum → chmod → replaceBinary).
//
// Reference: TS-007-036 §7, TS-007-037 §3
func installBinaryFromRelease(ops binaryOps, assetName, targetPath string) error {
	tmpFile, err := os.CreateTemp("", "anvil-download-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	downloadedHash, err := ops.download(assetName, tmpPath)
	if err != nil {
		return err
	}
	if err := ops.verify(assetName, downloadedHash); err != nil {
		return fmt.Errorf("checksum verification failed: %w", err)
	}
	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("set executable permission: %w", err)
	}
	if err := ops.replace(tmpPath, targetPath); err != nil {
		return err
	}
	return nil
}

// syncAdapterBinary refreshes one installed adapter binary to the latest
// release version. Progress and failures are reported through reporter
// with a per-adapter label; the failure is not returned — callers treat
// adapter sync as best-effort (TS-007-036 §3).
func syncAdapterBinary(ops binaryOps, reporter output.StepReporter, name, targetPath string) {
	label := fmt.Sprintf("Update adapter %s", name)
	reporter.StepStart(label)
	start := time.Now()

	if err := installBinaryFromRelease(ops, adapterAssetName(name), targetPath); err != nil {
		reporter.StepFailed(label, time.Since(start), err)
		return
	}
	reporter.StepComplete(label, time.Since(start))
}

// syncInstalledAdapters refreshes every adapter binary installed next to
// the CLI (the directory of execPath) to the latest release version.
//
// Only adapters that already exist on disk are synced — nothing new is
// installed (TS-007-036 §3 design decision). When no adapters are found,
// the sync is silent: update keeps today's behaviour. Failures are
// reported per adapter and never fail the CLI update; the function
// returns nothing and the caller's result remains the primary outcome.
//
// Reference: TS-007-036 §3, §7
func syncInstalledAdapters(ops binaryOps, reporter output.StepReporter, execPath string) {
	dir := filepath.Dir(execPath)
	adapters, err := listInstalledAdapters(dir)
	if err != nil {
		label := "Sync adapter binaries"
		reporter.StepStart(label)
		reporter.StepFailed(label, time.Since(time.Now()), fmt.Errorf("list installed adapters in %s: %w", dir, err))
		return
	}

	for _, name := range adapters {
		targetPath := filepath.Join(dir, adapterBinaryName(name))
		syncAdapterBinary(ops, reporter, name, targetPath)
	}
}
