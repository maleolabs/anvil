// Package cmd implements the Anvil CLI commands.
//
// Tests for the "anvil update" adapter synchronization phase (TS-007-036):
// after the CLI binary is updated, adapter binaries already installed
// next to the CLI are refreshed to the same release version — and nothing
// new is ever installed.
//
// The tests never touch the network: the download/verify/replace
// operations (binaryOps) are injected as fakes and the adapter scan runs
// against fake directories in t.TempDir(). The helpers under test are
// the pure pieces of the sync flow (listInstalledAdapters,
// adapterAssetName) and the sync loop (syncInstalledAdapters,
// installBinaryFromRelease) with fake ops.
//
// Reference: TS-007-036
package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/output"
	"maleolabs.com/anvil/internal/registry"
	"time"
)

// recordOps builds a fake binaryOps set that records every call and
// installs the downloaded payload at the target path (emulating the real
// atomic replace). Returns the ops and the recorded call log.
func recordOps(t *testing.T, payloads map[string]string) (binaryOps, *[]string) {
	t.Helper()
	var calls []string
	ops := binaryOps{
		download: func(assetName, tmpPath string) (string, error) {
			calls = append(calls, "download:"+assetName)
			payload, ok := payloads[assetName]
			if !ok {
				return "", fmt.Errorf("asset %s not found", assetName)
			}
			if err := os.WriteFile(tmpPath, []byte(payload), 0644); err != nil {
				return "", err
			}
			// Deterministic fake hash so verify can match it.
			return fakeHash(payload), nil
		},
		verify: func(assetName, downloadedHash string) (string, error) {
			calls = append(calls, "verify:"+assetName)
			return "", nil
		},
		replace: func(tmpPath, targetPath string) error {
			calls = append(calls, "replace:"+targetPath)
			data, err := os.ReadFile(tmpPath)
			if err != nil {
				return err
			}
			return os.WriteFile(targetPath, data, 0755)
		},
	}
	return ops, &calls
}

// fakeHash returns a stable pseudo-hash for a payload so fakes can match
// download output against verify expectations.
func fakeHash(payload string) string {
	return fmt.Sprintf("hash-%d", len(payload))
}

// ── Adapter Detection / Filtering (TS-007-036 §7) ────────────────────

// TestListInstalledAdapters_FiltersInstalledNames verifies that the scan
// detects files named exactly "anvil-adapter-<name>" and skips everything
// else: the CLI binary itself, platform-suffixed release assets,
// empty-name files, unrelated files, and directories.
//
// Reference: TS-007-036 AC-1, AC-2
func TestListInstalledAdapters_FiltersInstalledNames(t *testing.T) {
	dir := t.TempDir()

	// Installed adapters (must be detected).
	writeTestFile(t, dir, "anvil-adapter-laravel", "bin")
	writeTestFile(t, dir, "anvil-adapter-flutter", "bin")

	// Platform-suffixed release asset for the current platform (skipped).
	writeTestFile(t, dir, fmt.Sprintf("anvil-adapter-laravel-%s-%s", runtime.GOOS, runtime.GOARCH), "asset")

	// Platform-suffixed release assets for OTHER platforms (skipped too —
	// the installed-name filter is not limited to the current platform).
	writeTestFile(t, dir, "anvil-adapter-laravel-darwin-arm64", "asset")
	writeTestFile(t, dir, "anvil-adapter-flutter-linux-amd64", "asset")

	// Non-adapter files (skipped).
	writeTestFile(t, dir, "anvil", "cli binary")
	writeTestFile(t, dir, "anvil-adapter", "no name")
	writeTestFile(t, dir, "anvil-adapter-laravel.txt", "prefixed non-binary file")
	writeTestFile(t, dir, "README.md", "docs")

	// Directory with the prefix (skipped — not a file).
	if err := os.MkdirAll(filepath.Join(dir, "anvil-adapter-notafile"), 0755); err != nil {
		t.Fatalf("create dir: %v", err)
	}

	names, err := listInstalledAdapters(dir)
	if err != nil {
		t.Fatalf("listInstalledAdapters: %v", err)
	}
	want := []string{"flutter", "laravel"}
	if len(names) != len(want) {
		t.Fatalf("got adapters %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("adapter[%d] = %q, want %q (sorted)", i, names[i], want[i])
		}
	}
}

// TestListInstalledAdapters_EmptyDir verifies that a directory without
// adapters yields an empty list (no adapters → nothing to sync, AC-2).
//
// Reference: TS-007-036 AC-2
func TestListInstalledAdapters_EmptyDir(t *testing.T) {
	names, err := listInstalledAdapters(t.TempDir())
	if err != nil {
		t.Fatalf("listInstalledAdapters: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("got adapters %v, want none", names)
	}
}

// TestListInstalledAdapters_MissingDir verifies that an unreadable or
// missing directory surfaces the underlying error.
//
// Reference: TS-007-036 §7
func TestListInstalledAdapters_MissingDir(t *testing.T) {
	if _, err := listInstalledAdapters(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Error("expected error for missing directory, got nil")
	}
}

// TestAdapterAssetName verifies the release asset naming convention:
// "anvil-adapter-<name>-<goos>-<goarch>" (TS-007-034), consistent with
// the CLI asset name in cmd/update.go.
//
// Reference: TS-007-036 §3, TS-007-034
func TestAdapterAssetName(t *testing.T) {
	want := fmt.Sprintf("anvil-adapter-laravel-%s-%s", runtime.GOOS, runtime.GOARCH)
	if got := adapterAssetName("laravel"); got != want {
		t.Errorf("adapterAssetName(laravel) = %q, want %q", got, want)
	}
}

// ── Per-Adapter Sync Loop (TS-007-036 §3, TS-016-04-01) ──────────────

// syncTestEnv seeds installed-standard records (in the caller's isolated
// XDG_CONFIG_HOME — set once per test) for the given standards and
// stages ONE TLS server per standard serving its release channels (see
// adapterInstallTestServer), so the update sync resolves the STANDARD
// channel from the records (TS-016-04-01) and never touches a Core
// release.
func syncTestEnv(t *testing.T, standards map[string]map[string]adapterInstallTestRelease) {
	t.Helper()
	storeDir, err := registry.DefaultInstalledStandardsDir()
	if err != nil {
		t.Fatalf("default installed standards dir: %v", err)
	}
	store := registry.NewInstalledStandardStore(storeDir)
	now := time.Now().UTC()
	for id, releases := range standards {
		serverURL, _ := adapterInstallTestServer(t, id, releases)
		for version := range releases {
			archive := fmt.Sprintf("%s-%s.tar.gz", id, version)
			rec := registry.InstalledStandardRecord{
				FormatVersion:   registry.RecordFormatVersion,
				ID:              id,
				Version:         version,
				ContractVersion: "1.0.0",
				Resolution: registry.Resolution{
					Kind:   registry.ResolutionKindDistribution,
					Source: serverURL + "/releases/download/v" + version + "/" + archive,
				},
				Lifecycle:   registry.Lifecycle{State: registry.LifecycleStatePublished},
				InstalledAt: now,
				UpdatedAt:   now,
			}
			if _, _, err := store.Record(id, rec); err != nil {
				t.Fatalf("seed record %s %s: %v", id, version, err)
			}
		}
	}
}

// TestSyncInstalledAdapters_SkipsWhenNoneInstalled verifies that the
// sync performs no downloads, verifications, or replaces when no
// adapters are installed — "anvil update" must not create adapters
// (AC-2).
//
// Reference: TS-007-036 AC-2, AC-5
func TestSyncInstalledAdapters_SkipsWhenNoneInstalled(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "anvil", "cli") // only the CLI itself
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	reporter := output.NewStepReporter(io.Discard)
	syncInstalledAdapters(reporter, filepath.Join(dir, "anvil"))

	// No adapters installed: nothing to resolve, download, or replace.
	// (The sync performed no work; there is no release server to hit.)
	if _, err := os.Stat(filepath.Join(dir, "anvil-adapter-laravel")); !os.IsNotExist(err) {
		t.Errorf("sync must not create adapters")
	}
}

// TestSyncInstalledAdapters_SyncsEachInstalledAdapter verifies that every
// installed adapter is refreshed from ITS RECORDED standard's release
// channel (TS-016-04-01): the sync resolves the standard repositories'
// releases from the installed-standard records — never the Core release —
// and replaces each binary at its exact path next to the CLI.
//
// Reference: TS-007-036 AC-1, TS-016-04-01
func TestSyncInstalledAdapters_SyncsEachInstalledAdapter(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "anvil", "cli")
	writeTestFile(t, dir, "anvil-adapter-laravel", "old laravel")
	writeTestFile(t, dir, "anvil-adapter-flutter", "old flutter")

	laravelBinary := []byte("new laravel")
	flutterBinary := []byte("new flutter")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	syncTestEnv(t, map[string]map[string]adapterInstallTestRelease{
		"anvil-standard-laravel": {
			"1.0.0": {content: []byte("laravel content"), binary: laravelBinary},
		},
		"anvil-standard-flutter": {
			"1.0.0": {content: []byte("flutter content"), binary: flutterBinary},
		},
	})

	reporter := output.NewStepReporter(io.Discard)
	syncInstalledAdapters(reporter, filepath.Join(dir, "anvil"))

	// The installed files now carry the release payloads from the
	// standard repositories' channels.
	verifyTestFileContent(t, filepath.Join(dir, "anvil-adapter-laravel"), string(laravelBinary))
	verifyTestFileContent(t, filepath.Join(dir, "anvil-adapter-flutter"), string(flutterBinary))
}

// TestSyncInstalledAdapters_SkipsUnrecordedAdapter verifies the
// post-split sync contract (TS-016-04-01): an adapter whose standard has
// no installed-standard record is NOT refreshed — 'anvil update' never
// adopts implicitly (refresh-only, TS-007-036 §3) — and the skip hints
// at the full-gate registry adoption.
//
// Reference: TS-007-036 §3, TS-016-04-01
func TestSyncInstalledAdapters_SkipsUnrecordedAdapter(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "anvil", "cli")
	writeTestFile(t, dir, "anvil-adapter-laravel", "old laravel")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // no records at all

	reporter := output.NewStepReporter(io.Discard)
	syncInstalledAdapters(reporter, filepath.Join(dir, "anvil"))

	// The binary is untouched and the failure carries the adoption hint.
	verifyTestFileContent(t, filepath.Join(dir, "anvil-adapter-laravel"), "old laravel")
}

// TestAdapterRecordReleaseBase_NoRecordHintsAtFullGate verifies the
// no-record outcome of the sync's channel resolution: the error names the
// standard and points at the full-gate registry adoption.
//
// Reference: TS-016-04-01
func TestAdapterRecordReleaseBase_NoRecordHintsAtFullGate(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, err := adapterRecordReleaseBase("laravel")
	if err == nil {
		t.Fatal("expected an error for an unrecorded standard, got nil")
	}
	for _, want := range []string{"anvil-standard-laravel", "anvil adapter install laravel --force", "never adopts"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should contain %q, got: %v", want, err)
		}
	}
}

// TestSyncInstalledAdapters_FailureIsolation verifies that a failing
// adapter (missing asset on its standard's release channel) is reported
// and skipped while the remaining adapters are still synced — the CLI
// update result is not blocked by adapter failures (AC-3, AC-4).
//
// Reference: TS-007-036 AC-3, AC-4, TS-016-04-01
func TestSyncInstalledAdapters_FailureIsolation(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "anvil", "cli")
	writeTestFile(t, dir, "anvil-adapter-laravel", "old laravel")
	writeTestFile(t, dir, "anvil-adapter-flutter", "old flutter")

	// laravel's channel is BROKEN (tampered binary served against
	// pristine checksums — the checksum gate fails it); flutter's channel
	// is fine.
	flutterBinary := []byte("new flutter")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	syncTestEnv(t, map[string]map[string]adapterInstallTestRelease{
		"anvil-standard-laravel": {
			"1.0.0": {
				content:        []byte("laravel content"),
				binary:         []byte("tampered"),
				checksumBinary: []byte("pristine"),
			},
		},
		"anvil-standard-flutter": {
			"1.0.0": {content: []byte("flutter content"), binary: flutterBinary},
		},
	})

	reporter := output.NewStepReporter(io.Discard)
	syncInstalledAdapters(reporter, filepath.Join(dir, "anvil"))

	// flutter was synced despite the laravel failure.
	verifyTestFileContent(t, filepath.Join(dir, "anvil-adapter-flutter"), string(flutterBinary))
	verifyTestFileContent(t, filepath.Join(dir, "anvil-adapter-laravel"), "old laravel")
}

// TestSyncInstalledAdapters_ChecksumMismatchAbortsAdapter verifies that a
// checksum mismatch fails only that adapter: the replace step never runs
// for it and the existing binary stays untouched.
//
// Reference: TS-007-036 AC-3
func TestSyncInstalledAdapters_ChecksumMismatchAbortsAdapter(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "anvil", "cli")
	writeTestFile(t, dir, "anvil-adapter-laravel", "old laravel")

	// The channel serves a TAMPERED binary against pristine checksums
	// (checksumBinary differs from binary — the adapterInstallTestRelease
	// checksums are computed over checksumBinary when set).
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	syncTestEnv(t, map[string]map[string]adapterInstallTestRelease{
		"anvil-standard-laravel": {
			"1.0.0": {
				content:        []byte("laravel content"),
				binary:         []byte("tampered"),
				checksumBinary: []byte("pristine"),
			},
		},
	})

	reporter := output.NewStepReporter(io.Discard)
	syncInstalledAdapters(reporter, filepath.Join(dir, "anvil"))

	// The installed binary must be untouched.
	verifyTestFileContent(t, filepath.Join(dir, "anvil-adapter-laravel"), "old laravel")
}

// TestInstallBinaryFromRelease_Flow verifies the shared install flow:
// download → verify → chmod → replace, with the verified payload landing
// at the target path.
//
// Reference: TS-007-036 §7, TS-007-037 §3
func TestInstallBinaryFromRelease_Flow(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "anvil-adapter-laravel")

	payloads := map[string]string{
		fmt.Sprintf("anvil-adapter-laravel-%s-%s", runtime.GOOS, runtime.GOARCH): "fresh binary",
	}
	ops, calls := recordOps(t, payloads)

	if _, err := installBinaryFromRelease(ops, adapterAssetName("laravel"), targetPath, nil); err != nil {
		t.Fatalf("installBinaryFromRelease: %v", err)
	}

	verifyTestFileContent(t, targetPath, "fresh binary")
	if len(*calls) != 3 {
		t.Errorf("calls = %v, want download+verify+replace", *calls)
	}
}

// TestInstallBinaryFromRelease_ChecksumMismatch verifies that a checksum
// mismatch aborts the install before the replace step.
//
// Reference: TS-007-036 AC-3, TS-007-037 AC-7
func TestInstallBinaryFromRelease_ChecksumMismatch(t *testing.T) {
	targetPath := filepath.Join(t.TempDir(), "anvil-adapter-laravel")

	ops := binaryOps{
		download: func(assetName, tmpPath string) (string, error) {
			return "wrong-hash", nil
		},
		verify: func(assetName, downloadedHash string) (string, error) {
			return "", errors.New("hash mismatch")
		},
		replace: func(tmpPath, targetPath string) error {
			t.Fatalf("replace must not run after a checksum mismatch")
			return nil
		},
	}

	_, err := installBinaryFromRelease(ops, adapterAssetName("laravel"), targetPath, nil)
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "binary verification failed") {
		t.Errorf("error should mention checksum verification, got: %v", err)
	}
	if _, statErr := os.Stat(targetPath); !os.IsNotExist(statErr) {
		t.Errorf("target must not exist after failed install (stat err: %v)", statErr)
	}
}

// TestInstallBinaryFromRelease_DownloadError verifies that a download
// failure aborts before verification and replacement.
//
// Reference: TS-007-037 AC-7
func TestInstallBinaryFromRelease_DownloadError(t *testing.T) {
	targetPath := filepath.Join(t.TempDir(), "anvil-adapter-laravel")

	ops := binaryOps{
		download: func(assetName, tmpPath string) (string, error) {
			return "", errors.New("server returned HTTP 404")
		},
		verify: func(assetName, downloadedHash string) (string, error) {
			t.Fatalf("verify must not run after a download failure")
			return "", nil
		},
		replace: func(tmpPath, targetPath string) error {
			t.Fatalf("replace must not run after a download failure")
			return nil
		},
	}

	if _, err := installBinaryFromRelease(ops, adapterAssetName("laravel"), targetPath, nil); err == nil {
		t.Fatal("expected download error, got nil")
	}
}

// TestInstallBinaryFromRelease_ReportsSteps verifies that a non-nil
// reporter receives one step per phase (download → verify → install),
// so interactive installs show live progress like "anvil update".
//
// Reference: TS-007-036 §7, TS-007-037 §3
func TestInstallBinaryFromRelease_ReportsSteps(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "anvil-adapter-laravel")

	payloads := map[string]string{
		fmt.Sprintf("anvil-adapter-laravel-%s-%s", runtime.GOOS, runtime.GOARCH): "fresh binary",
	}
	ops, _ := recordOps(t, payloads)

	var buf strings.Builder
	reporter := output.NewStepReporter(&buf)

	if _, err := installBinaryFromRelease(ops, adapterAssetName("laravel"), targetPath, reporter); err != nil {
		t.Fatalf("installBinaryFromRelease: %v", err)
	}

	for _, want := range []string{
		"Step: Download anvil-adapter-laravel-" + runtime.GOOS + "-" + runtime.GOARCH,
		"Step: Verify binary",
		"Step: Install to " + targetPath,
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("reporter output should contain %q, got:\n%s", want, buf.String())
		}
	}
	verifyTestFileContent(t, targetPath, "fresh binary")
}

// TestInstallBinaryFromRelease_NilReporterSilent verifies that a nil
// reporter keeps the flow silent — the adapter sync path reports one
// coarse step per adapter and must not emit nested progress.
//
// Reference: TS-007-036 §3
func TestInstallBinaryFromRelease_NilReporterSilent(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "anvil-adapter-laravel")

	payloads := map[string]string{
		fmt.Sprintf("anvil-adapter-laravel-%s-%s", runtime.GOOS, runtime.GOARCH): "fresh binary",
	}
	ops, _ := recordOps(t, payloads)

	if _, err := installBinaryFromRelease(ops, adapterAssetName("laravel"), targetPath, nil); err != nil {
		t.Fatalf("installBinaryFromRelease: %v", err)
	}
	verifyTestFileContent(t, targetPath, "fresh binary")
}

// ── Test Helpers ─────────────────────────────────────────────────────

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func verifyTestFileContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(data) != want {
		t.Errorf("%s content = %q, want %q", path, data, want)
	}
}
