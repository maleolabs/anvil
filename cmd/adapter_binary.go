// Package cmd implements the Anvil CLI commands.
//
// ── Adapter Binary Distribution (TS-007-036, TS-007-037, TS-016-04-01) ─
//
// This file holds the shared machinery for downloading, verifying, and
// replacing adapter binaries from the STANDARD repositories' releases —
// the registry distribution channel after the repository split (ADR-025
// §3.5, ADR-030):
//
//   - name helpers (adapterAssetName, adapterBinaryName) matching the
//     release asset convention "anvil-adapter-<name>-<goos>-<goarch>"
//     and the installed-name convention "anvil-adapter-<name>"
//     (TS-007-034, 004-review-resolutions D1, ADR-009 §8.1);
//   - listInstalledAdapters, the directory scan used by "anvil update"
//     to refresh only adapters the user already installed (TS-007-036);
//   - standardReleaseDownloadBase / standardReleaseAssetURL, deriving
//     the release channel from the registry metadata's distribution
//     location (https-only, userinfo-free — ADR-030 §3);
//   - standardAdapterBinaryOps, the download/verify/replace operation
//     set shared by "anvil adapter install" and the "anvil update"
//     adapter sync. All fetches go through the hardened standard content
//     client (standardInstallHTTPClient: https-only redirects, bounded
//     chain) under the adapterBinaryMaxBytes download cap; tests inject
//     fakes so no test touches the network;
//   - the update adapter sync (syncInstalledAdapters / syncAdapterBinary
//     / adapterRecordReleaseBase): refreshes installed adapters to the
//     release version of their RECORDED standard (TS-016-04-01) — the
//     sync never bumps versions, never adopts implicitly, and never
//     fetches from a Core release; unrecorded adapters are skipped with
//     a hint pointing at 'anvil adapter install <name> --force'.
//
// The install flow mirrors cmd/update.go's runUpdate steps: download to
// a temp file while computing sha256, verify against SHA256SUMS.txt,
// chmod 0755, atomic replace via replaceBinary.
//
// Reference: TS-007-036, TS-007-037, TS-007-034, TS-016-04-01
package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/registry"
)

// adapterBinaryPrefix is the installed executable name prefix for
// framework adapters: "anvil-adapter-<name>" (ADR-009 §8.1,
// 004-review-resolutions D1, 005-adapter-command-contract §10).
const adapterBinaryPrefix = "anvil-adapter-"

// binaryOps bundles the network/filesystem operations needed to install
// one binary from a release. Production values come from
// standardAdapterBinaryOps / standardAdapterBinaryOpsTrusted (the
// standard repositories' release channel, TS-016-04-01); tests inject
// fakes (no network, no real replaces).
type binaryOps struct {
	// download downloads the release asset assetName into tmpPath while
	// computing its sha256, and returns the hex digest.
	download func(assetName, tmpPath string) (string, error)

	// verify verifies downloadedHash for the asset and returns an
	// explicit notice string when the verification DEGRADED to the
	// same-channel checksum (no attestation-bound material in the
	// release — TS-014-04-04: releases predating binary attestation
	// keep verifying, with a warning, never a silent skip). The
	// attestation-bound path (VerifyAssetDigest) returns an empty
	// notice on success: the digest is signed material and supersedes
	// the checksum file.
	verify func(assetName, downloadedHash string) (notice string, err error)

	// replace atomically replaces targetPath with the verified temp file.
	replace func(tmpPath, targetPath string) error
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

// ── Standard Release Channel (TS-016-04-01) ──────────────────────────

// standardReleaseDownloadBase derives the release download base URL of a
// standard repository release from the registry metadata's distribution
// location — the release archive URL (ADR-030: release content resolves
// from the standard's own release channel). The adapter binary assets and
// SHA256SUMS.txt live in the SAME release download directory as the
// archive, so the base is the archive URL with the file name stripped:
//
//	https://github.com/maleolabs/anvil-standard-laravel/releases/download/v1.0.0/anvil-standard-laravel-1.0.0.tar.gz
//	→ https://github.com/maleolabs/anvil-standard-laravel/releases/download/v1.0.0/
//
// https-only and userinfo-free are re-validated here (ADR-030 §3) and
// again at the download boundary: release material is never fetched over
// plaintext, and credentials must never be sent.
func standardReleaseDownloadBase(distributionLocation string) (string, error) {
	parsed, err := url.Parse(distributionLocation)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", fmt.Errorf(
			"the distribution location %s is not a well-formed https URL; adapter binaries are resolved over TLS only (ADR-030 §3)",
			standardScrubLocation(distributionLocation))
	}
	if parsed.User != nil {
		return "", fmt.Errorf(
			"the distribution location %s carries userinfo (username or password); credentials would be sent as Basic auth — publish the release content at a location without userinfo (ADR-030 §3)",
			standardURLWithoutUserinfo(parsed))
	}
	dir := path.Dir(parsed.Path)
	if dir == "/" || dir == "." {
		return "", fmt.Errorf(
			"the distribution location %s does not resolve to a release download directory; the adapter binary and checksums cannot be located alongside the release content",
			standardScrubLocation(distributionLocation))
	}
	return parsed.Scheme + "://" + parsed.Host + dir + "/", nil
}

// standardReleaseAssetURL validates the release base (https-only,
// userinfo-free — ADR-030 §3) and appends the asset name. It is the
// download-boundary re-check of the https rules: the base was derived
// from an https distribution location, but the fetch is the security
// boundary and never issues a plaintext request.
func standardReleaseAssetURL(releaseBase, assetName string) (string, error) {
	parsed, err := url.Parse(releaseBase)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", fmt.Errorf(
			"release base %s is not a well-formed https URL; adapter binaries and checksums are fetched over TLS only (ADR-030 §3)",
			standardScrubLocation(releaseBase))
	}
	if parsed.User != nil {
		return "", fmt.Errorf(
			"release base %s carries userinfo (username or password); credentials would be sent as Basic auth — publish the release content at a location without userinfo (ADR-030 §3)",
			standardURLWithoutUserinfo(parsed))
	}
	return releaseBase + assetName, nil
}

// adapterBinaryMaxBytes caps a single adapter binary download at 512 MiB.
// Adapter executables are small (tens of MB at most); the cap is enforced
// DURING the download via a limit reader, mirroring the release content
// cap (standardContentMaxBytes) — a download beyond the cap is reported
// precisely instead of buffered unbounded.
var adapterBinaryMaxBytes = int64(512 << 20)

// standardAdapterBinaryOps builds the binary operation set for installing
// an adapter binary from a STANDARD repository release — the registry
// distribution channel (ADR-030): the download and the checksum file come
// from the same release (tag) the validated registry metadata describes,
// never from a Core release. replaceBinary is shared with the CLI update
// path.
//
// This variant verifies against the release's SHA256SUMS.txt only — the
// pre-TS-014-04-04 behavior, kept for the "anvil update" adapter sync
// (syncAdapterBinary): the sync resolves the release channel from the
// installed-standard RECORD, which does not carry the registry metadata
// document, so the attestation-bound path (standardAdapterBinaryOpsTrusted)
// is not available there. The full adoption surface ('anvil adapter
// install <name> --force') always verifies against the attestation-bound
// digests.
func standardAdapterBinaryOps(releaseBase string) binaryOps {
	return binaryOps{
		download: func(assetName, tmpPath string) (string, error) {
			return downloadReleaseAssetFrom(releaseBase, assetName, tmpPath)
		},
		verify: func(assetName, downloadedHash string) (string, error) {
			return "", verifyChecksumFrom(releaseBase, assetName, downloadedHash)
		},
		replace: replaceBinary,
	}
}

// standardAdapterBinaryOpsTrusted builds the binary operation set for
// installing an adapter binary from a STANDARD repository release with
// attestation-bound verification (TS-014-04-04): the downloaded binary
// is verified against the digest the release's attested registry metadata
// document declares for the asset (registry.VerifyAssetDigest) BEFORE
// installation; the digest is covered by the publisher's Ed25519
// signature, so a same-channel attacker who swaps the binary (and the
// unsigned SHA256SUMS.txt) cannot also rewrite this declaration.
//
// A release WITHOUT the new material (e.g. already-published v1.0.0)
// degrades to today's same-channel checksum verification WITH an explicit
// notice — no silent trust downgrade, no fail-closed for old releases.
// Verification failure of either path aborts the install with an
// actionable error.
//
// md must be the registry metadata document of the SAME release (version)
// releaseBase points at — the one already parsed and trust-validated by
// the adoption gates (ADR-022 §3).
func standardAdapterBinaryOpsTrusted(releaseBase string, md *registry.Metadata) binaryOps {
	return binaryOps{
		download: func(assetName, tmpPath string) (string, error) {
			return downloadReleaseAssetFrom(releaseBase, assetName, tmpPath)
		},
		verify: func(assetName, downloadedHash string) (string, error) {
			attested, err := registry.VerifyAssetDigest(*md, assetName, downloadedHash)
			if err != nil {
				return "", err
			}
			if attested {
				// Attestation-bound verification passed: the digest is
				// signed material and supersedes the same-channel,
				// unsigned checksum file (TS-014-04-04).
				return "", nil
			}
			// No attestation-bound material for this asset: degrade to
			// today's same-channel checksum with an EXPLICIT notice
			// (TS-014-04-04) — never a silent trust downgrade, never
			// fail-closed for releases published before binary
			// attestation.
			notice := fmt.Sprintf(
				"release %s %s declares no attestation-bound digest for %s; verifying against the release's SHA256SUMS.txt (same-channel checksum — weaker trust; releases published before binary attestation, TS-014-04-04)",
				md.ID, md.Version, assetName)
			if err := verifyChecksumFrom(releaseBase, assetName, downloadedHash); err != nil {
				return notice, err
			}
			return notice, nil
		},
		replace: replaceBinary,
	}
}

// downloadReleaseAssetFrom downloads assetName from releaseBase — the
// release download directory of a standard repository release — into
// tmpPath while computing its sha256. The request goes through the
// hardened standard content client (standardInstallHTTPClient: https-only
// redirects, bounded chain — ADR-030 §3) and the download is capped at
// adapterBinaryMaxBytes.
func downloadReleaseAssetFrom(releaseBase, assetName, tmpPath string) (string, error) {
	downloadURL, err := standardReleaseAssetURL(releaseBase, assetName)
	if err != nil {
		return "", err
	}
	return downloadReleaseAssetFromURLClient(standardInstallHTTPClient.Get, downloadURL, assetName, tmpPath)
}

// downloadReleaseAssetFromURLClient downloads assetName from downloadURL
// through the given client into tmpPath while computing its sha256, under
// the adapterBinaryMaxBytes cap (enforced during the download via a limit
// reader: at most cap+1 bytes are read, so an oversized asset is reported
// precisely instead of buffered unbounded).
func downloadReleaseAssetFromURLClient(get func(string) (*http.Response, error), downloadURL, assetName, tmpPath string) (string, error) {
	resp, err := get(downloadURL)
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
	n, err := io.Copy(io.MultiWriter(f, hash), io.LimitReader(resp.Body, adapterBinaryMaxBytes+1))
	if err != nil {
		return "", fmt.Errorf("download %s: %w", assetName, err)
	}
	if n > adapterBinaryMaxBytes {
		return "", fmt.Errorf(
			"download %s exceeds the %d-byte size cap; downloads are never buffered unbounded. If you are the publisher, republish the binary under the cap; otherwise report the broken release",
			assetName, adapterBinaryMaxBytes)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// verifyChecksumFrom downloads SHA256SUMS.txt from releaseBase — the
// release download directory of a standard repository release — through
// the hardened standard content client (standardInstallHTTPClient:
// https-only redirects, bounded chain — ADR-030 §3), and verifies
// downloadedHash for assetName against it.
func verifyChecksumFrom(releaseBase, assetName, downloadedHash string) error {
	checksumsURL, err := standardReleaseAssetURL(releaseBase, "SHA256SUMS.txt")
	if err != nil {
		return err
	}
	return verifyChecksumFromURLClient(standardInstallHTTPClient.Get, checksumsURL, assetName, downloadedHash)
}

// verifyChecksumFromURLClient is the shared checksum-verification body:
// GET the SHA256SUMS.txt file through the given client, find the asset's
// line (with or without the "binaries/" prefix — the release format
// "sha256sum binaries/*"), and compare the hashes.
func verifyChecksumFromURLClient(get func(string) (*http.Response, error), checksumsURL, binaryName, downloadedHash string) error {
	resp, err := get(checksumsURL)
	if err != nil {
		return fmt.Errorf("fetch checksums: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("checksums not available (HTTP %d)", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read checksums: %w", err)
	}

	// Parse SHA256SUMS format: "<hash>  <filename>"
	// The filename may be "anvil-adapter-laravel-linux-amd64" or
	// "binaries/anvil-adapter-laravel-linux-amd64".
	expectedHash := ""
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// SHA256SUMS uses two-space separator: "<hash>  <filename>"
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 {
			continue
		}

		filename := strings.TrimSpace(parts[1])
		hash := strings.TrimSpace(parts[0])

		// Match with or without "binaries/" prefix
		if filename == binaryName || filename == "binaries/"+binaryName {
			expectedHash = hash
			break
		}
	}

	if expectedHash == "" {
		return fmt.Errorf("checksum for %s not found in SHA256SUMS.txt", binaryName)
	}

	if downloadedHash != expectedHash {
		return fmt.Errorf("hash mismatch for %s: expected %s, got %s",
			binaryName, expectedHash, downloadedHash)
	}

	return nil
}

// installBinaryFromRelease downloads assetName from the release, verifies
// its integrity, makes it executable, and atomically replaces targetPath.
// The steps mirror the CLI update flow in cmd/update.go runUpdate
// (download → verifyChecksum → chmod → replaceBinary).
//
// Verification follows the ops' trust model (TS-014-04-04): the trusted
// ops verify the downloaded binary against the attestation-bound digest
// declared in the release's registry metadata document (superseding the
// same-channel checksum file) and degrade to the checksum — with an
// explicit notice — only when the release carries no material. The
// returned notice is the explicit degradation warning for the caller to
// surface; an empty notice means no downgrade happened.
//
// When reporter is non-nil, each phase is reported as its own step
// (Download / Verify binary / Install to <target>) so interactive
// installs show live progress like "anvil update". A nil reporter keeps
// the flow silent — used by the update adapter sync (syncAdapterBinary),
// which reports one coarse step per adapter.
//
// Reference: TS-007-036 §7, TS-007-037 §3, TS-014-04-04
func installBinaryFromRelease(ops binaryOps, assetName, targetPath string, reporter output.StepReporter) (notice string, err error) {
	tmpFile, err := os.CreateTemp("", "anvil-download-*")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("close temp file: %w", err)
	}

	var downloadedHash string

	if err := reportStep(reporter, fmt.Sprintf("Download %s", assetName), func() error {
		hash, err := ops.download(assetName, tmpPath)
		if err != nil {
			return err
		}
		downloadedHash = hash
		return nil
	}); err != nil {
		return "", err
	}
	if err := reportStep(reporter, "Verify binary", func() error {
		var verifyErr error
		notice, verifyErr = ops.verify(assetName, downloadedHash)
		if verifyErr != nil {
			return fmt.Errorf("binary verification failed: %w", verifyErr)
		}
		return nil
	}); err != nil {
		return "", err
	}
	if err := reportStep(reporter, fmt.Sprintf("Install to %s", targetPath), func() error {
		if err := os.Chmod(tmpPath, 0755); err != nil {
			return fmt.Errorf("set executable permission: %w", err)
		}
		if err := ops.replace(tmpPath, targetPath); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return "", err
	}
	return notice, nil
}

// reportStep runs fn under the step reporter as one named step: the
// shared step runner of linear install workflows (the binary install
// phases above and the registry-side phases of "anvil adapter install",
// TS-016-04-01). A nil reporter keeps the step silent.
func reportStep(reporter output.StepReporter, name string, fn func() error) error {
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

// syncAdapterBinary refreshes one installed adapter binary to the release
// version of its RECORDED standard (TS-016-04-01). Since the repository
// split, adapter binaries come from the standard repositories' releases
// (registry distribution channel, ADR-030) — never from a Core release —
// so the sync resolves the release channel from the installed-standard
// record's explicit resolution and honors the RECORDED version pin: the
// sync never bumps the version and never adopts a standard implicitly. An
// adapter whose standard has no record, or whose record carries no
// distribution resolution (e.g. a bundle install), is NOT refreshed: the
// step fails with a hint pointing at the full-gate 'anvil adapter install
// <name> --force' (registry adoption with trust validation, ADR-022).
//
// Progress and failures are reported through reporter with a per-adapter
// label; the failure is not returned — callers treat adapter sync as
// best-effort (TS-007-036 §3). The reporter is not passed down to
// installBinaryFromRelease: the sync reports one coarse step per adapter,
// so the download/verify/install phases stay silent (nil).
func syncAdapterBinary(reporter output.StepReporter, name, targetPath string) {
	label := fmt.Sprintf("Update adapter %s", name)
	reporter.StepStart(label)
	start := time.Now()

	releaseBase, err := adapterRecordReleaseBase(name)
	if err != nil {
		reporter.StepFailed(label, time.Since(start), err)
		return
	}
	// The sync verifies against the release's SHA256SUMS.txt (today's
	// behavior — standardAdapterBinaryOps): the record does not carry the
	// registry metadata document, so the attestation-bound path is not
	// available here; the full-gate 'anvil adapter install <name> --force'
	// always verifies against the attestation-bound digests
	// (TS-014-04-04).
	if _, err := installBinaryFromRelease(standardAdapterBinaryOps(releaseBase), adapterAssetName(name), targetPath, nil); err != nil {
		reporter.StepFailed(label, time.Since(start), err)
		return
	}
	reporter.StepComplete(label, time.Since(start))
}

// adapterRecordReleaseBase returns the standard release channel base of
// the adapter's RECORDED standard — the mechanism the update adapter sync
// uses to re-point at the standard repositories (TS-016-04-01). The
// release base derives from the installed-standard record's explicit
// resolution source (the actual https endpoint the release content was
// adopted from, ADR-022 §3), so the binary and its checksums come from
// the SAME release the record pins:
//
//   - no record: an actionable error — the adapter is not refreshed by
//     'anvil update' (refresh-only, never adopts); the full-gate adoption
//     is 'anvil adapter install <name> --force';
//   - a record without a distribution resolution (e.g. a bundled/offline
//     install): an actionable error naming the resolution kind;
//   - a corrupt record: the error passes through (recovery by
//     re-adoption, TS-014-03-03).
func adapterRecordReleaseBase(name string) (string, error) {
	id := adapterStandardIDForName(name)
	storeDir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		return "", fmt.Errorf("resolve the installed-standards directory: %w", err)
	}
	store := registry.NewInstalledStandardStore(storeDir)
	rec, err := store.Get(id)
	if err != nil {
		if errors.Is(err, registry.ErrRecordNotFound) {
			return "", fmt.Errorf(
				"standard %q has no installed-standard record — 'anvil update' never adopts implicitly (refresh-only, TS-007-036 §3); run 'anvil adapter install %s --force' to adopt through the registry (trust-validated, ADR-022)",
				id, name)
		}
		return "", fmt.Errorf("could not read the installed-standard record for %q: %w", id, err)
	}
	if rec.Resolution.Kind != registry.ResolutionKindDistribution || rec.Resolution.Source == "" {
		return "", fmt.Errorf(
			"standard %q was not adopted from a distribution channel (recorded resolution kind %q) — its adapter is not refreshed; run 'anvil adapter install %s --force' to re-adopt through the registry (trust-validated, ADR-022)",
			id, rec.Resolution.Kind, name)
	}
	releaseBase, err := standardReleaseDownloadBase(rec.Resolution.Source)
	if err != nil {
		return "", fmt.Errorf("standard %q: %w", id, err)
	}
	return releaseBase, nil
}

// syncInstalledAdapters refreshes every adapter binary installed next to
// the CLI (the directory of execPath) to the release version of its
// RECORDED standard (TS-016-04-01): the sync re-points at the standard
// repositories' release channels and honors each recorded version pin —
// it never bumps versions and never adopts standards implicitly. An
// adapter without a recorded standard is skipped with a hint pointing at
// the full-gate 'anvil adapter install <name> --force'.
//
// Only adapters that already exist on disk are synced — nothing new is
// installed (TS-007-036 §3 design decision). When no adapters are found,
// the sync is silent: update keeps today's behaviour. Failures are
// reported per adapter and never fail the CLI update; the function
// returns nothing and the caller's result remains the primary outcome.
//
// Reference: TS-007-036 §3, §7, TS-016-04-01
func syncInstalledAdapters(reporter output.StepReporter, execPath string) {
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
		syncAdapterBinary(reporter, name, targetPath)
	}
}
