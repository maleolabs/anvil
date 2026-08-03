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
	"encoding/json"
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
// "anvil-adapter-" prefix) is an installed adapter identifier: non-empty,
// without a release asset platform suffix ("-<os>-<arch>" for any
// platform the release publishes), and made only of identifier
// characters (letters, digits, '-', '_'). Installed names are exactly
// "anvil-adapter-<name>" (TS-007-036 §3); anything else — including
// platform-suffixed release artifacts and prefixed non-binary files
// (e.g. "anvil-adapter-foo.txt") sitting in the scanned directory — is
// not an installed adapter and must not be detected or refreshed.
func isInstalledAdapterName(rest string) bool {
	if rest == "" {
		return false
	}
	for _, suffix := range adapterReleasePlatforms {
		if strings.HasSuffix(rest, suffix) {
			return false
		}
	}
	for _, r := range rest {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// adapterReleaseFetch returns the latest release tag and the adapter names
// published as assets in that release. It is a package-level seam: the
// production value hits the GitHub API (updateAPITemplate); tests stub it
// so no test touches the network. It powers "anvil adapter list
// --available" (TS-007-031).
var adapterReleaseFetch = func() (string, []string, error) {
	return fetchLatestReleaseAdapterNames()
}

// fetchLatestReleaseAdapterNames queries the latest GitHub release and
// extracts the sorted, de-duplicated names of adapters published as
// release assets (anvil-adapter-<name>-<os>-<arch> for any published
// platform). The release tag is returned alongside so the CLI can show
// which release the list comes from.
//
// Reference: TS-007-034, TS-007-031
func fetchLatestReleaseAdapterNames() (string, []string, error) {
	apiURL := fmt.Sprintf(updateAPITemplate, updateRepo)

	//nolint:gosec
	resp, err := http.Get(apiURL)
	if err != nil {
		return "", nil, fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", nil, fmt.Errorf("GitHub API returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var release ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", nil, fmt.Errorf("parse GitHub API response: %w", err)
	}
	if release.TagName == "" {
		return "", nil, fmt.Errorf("no releases found in %s", updateRepo)
	}

	return release.TagName, adapterNamesFromAssets(release.Assets), nil
}

// adapterNamesFromAssets extracts the sorted, de-duplicated adapter names
// from release asset names matching "anvil-adapter-<name>-<os>-<arch>"
// for any published platform (adapterReleasePlatforms). Unrelated assets
// (the CLI binaries, SHA256SUMS.txt, etc.) are ignored.
//
// Reference: TS-007-034
func adapterNamesFromAssets(assets []ghReleaseAsset) []string {
	seen := make(map[string]bool)
	names := make([]string, 0, len(assets))
	for _, asset := range assets {
		rest, ok := strings.CutPrefix(asset.Name, adapterBinaryPrefix)
		if !ok {
			continue
		}
		name, ok := adapterNameFromAssetRest(rest)
		if !ok || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// adapterNameFromAssetRest extracts the adapter name from the part of an
// asset name after the "anvil-adapter-" prefix, which must end with one
// of the published platform suffixes ("-<os>-<arch>"). A name that does
// not carry a platform suffix is not a release adapter asset.
func adapterNameFromAssetRest(rest string) (string, bool) {
	for _, suffix := range adapterReleasePlatforms {
		if name := strings.TrimSuffix(rest, suffix); name != "" && name != rest {
			return name, true
		}
	}
	return "", false
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
// When reporter is non-nil, each phase is reported as its own step
// (Download / Verify checksum / Install to <target>) so interactive
// installs show live progress like "anvil update". A nil reporter keeps
// the flow silent — used by the update adapter sync (syncAdapterBinary),
// which reports one coarse step per adapter.
//
// Reference: TS-007-036 §7, TS-007-037 §3
func installBinaryFromRelease(ops binaryOps, assetName, targetPath string, reporter output.StepReporter) error {
	tmpFile, err := os.CreateTemp("", "anvil-download-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	var downloadedHash string

	step := func(name string, fn func() error) error {
		start := time.Now()
		if reporter != nil {
			reporter.StepStart(name)
		}
		err := fn()
		if reporter != nil {
			if err != nil {
				reporter.StepFailed(name, time.Since(start), err)
			} else {
				reporter.StepComplete(name, time.Since(start))
			}
		}
		return err
	}

	if err := step(fmt.Sprintf("Download %s", assetName), func() error {
		hash, err := ops.download(assetName, tmpPath)
		if err != nil {
			return err
		}
		downloadedHash = hash
		return nil
	}); err != nil {
		return err
	}
	if err := step("Verify checksum", func() error {
		if err := ops.verify(assetName, downloadedHash); err != nil {
			return fmt.Errorf("checksum verification failed: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := step(fmt.Sprintf("Install to %s", targetPath), func() error {
		if err := os.Chmod(tmpPath, 0755); err != nil {
			return fmt.Errorf("set executable permission: %w", err)
		}
		if err := ops.replace(tmpPath, targetPath); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

// syncAdapterBinary refreshes one installed adapter binary to the latest
// release version. Progress and failures are reported through reporter
// with a per-adapter label; the failure is not returned — callers treat
// adapter sync as best-effort (TS-007-036 §3). The reporter is not passed
// down to installBinaryFromRelease: the sync reports one coarse step per
// adapter, so the download/verify/install phases stay silent (nil).
func syncAdapterBinary(ops binaryOps, reporter output.StepReporter, name, targetPath string) {
	label := fmt.Sprintf("Update adapter %s", name)
	reporter.StepStart(label)
	start := time.Now()

	if err := installBinaryFromRelease(ops, adapterAssetName(name), targetPath, nil); err != nil {
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
