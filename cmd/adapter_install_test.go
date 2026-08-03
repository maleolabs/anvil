// Package cmd implements the Anvil CLI commands.
//
// Tests for "anvil adapter install" (TS-007-037): known-name validation,
// install flow with fake download/verify/replace operations, the
// already-installed gate (with and without --force), checksum mismatch
// handling, and --json output validity. No test touches the network: the
// install directory seam points at t.TempDir() and adapterBinaryOps is
// stubbed with fakes.
//
// Reference: TS-007-037
package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/output"
)

// stubAdapterInstallDirAt points the install-directory seam at dir and
// registers cleanup.
func stubAdapterInstallDirAt(t *testing.T, dir string) {
	t.Helper()
	orig := adapterInstallDir
	adapterInstallDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { adapterInstallDir = orig })
}

// installOps builds a fake binaryOps set whose download writes a payload
// and whose replace installs it at the target (emulating the real atomic
// replace). verify returns nil unless failVerify is set.
func installOps(t *testing.T, payload string, failVerify error) binaryOps {
	t.Helper()
	return binaryOps{
		download: func(assetName, tmpPath string) (string, error) {
			if err := os.WriteFile(tmpPath, []byte(payload), 0644); err != nil {
				return "", err
			}
			return fakeHash(payload), nil
		},
		verify: func(assetName, downloadedHash string) error {
			return failVerify
		},
		replace: func(tmpPath, targetPath string) error {
			data, err := os.ReadFile(tmpPath)
			if err != nil {
				return err
			}
			return os.WriteFile(targetPath, data, 0755)
		},
	}
}

// TestAdapterInstall_UnknownName verifies that an unknown adapter name is
// rejected with a clear error naming the supported set, before any
// download or filesystem activity.
//
// Reference: TS-007-037 AC-3, §3
func TestAdapterInstall_UnknownName(t *testing.T) {
	stubKnownFrameworks(t, []string{"laravel", "flutter"})
	stubAdapterInstallDirAt(t, t.TempDir())
	stubAdapterBinaryOps(t, binaryOps{
		download: func(assetName, tmpPath string) (string, error) {
			t.Fatalf("download must not run for an unknown adapter")
			return "", nil
		},
		verify:  func(assetName, downloadedHash string) error { return nil },
		replace: func(tmpPath, targetPath string) error { return nil },
	})

	_, _, stderr, err := executeCommand("adapter", "install", "node")
	if err == nil {
		t.Fatal("expected error for unknown adapter, got nil")
	}
	if !strings.Contains(stderr, "unknown adapter") {
		t.Errorf("stderr should mention the unknown adapter, got: %s", stderr)
	}
	if !strings.Contains(stderr, "known adapters") {
		t.Errorf("stderr should list known adapters, got: %s", stderr)
	}
}

// TestAdapterInstall_InstallsWhenAbsent verifies that installing a known
// adapter downloads the platform asset, verifies it, and places the
// binary next to the CLI.
//
// Reference: TS-007-037 AC-1, AC-2, AC-7
func TestAdapterInstall_InstallsWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	stubAdapterBinaryOps(t, installOps(t, "adapter binary payload", nil))

	_, stdout, stderr, err := executeCommand("adapter", "install", "laravel")
	if err != nil {
		t.Fatalf("adapter install returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	targetPath := filepath.Join(dir, "anvil-adapter-laravel")
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("adapter binary not installed at %s: %v", targetPath, err)
	}
	if string(data) != "adapter binary payload" {
		t.Errorf("installed binary content = %q, want %q", data, "adapter binary payload")
	}
	if !strings.Contains(stdout, "Adapter laravel installed") {
		t.Errorf("stdout should confirm installation, got:\n%s", stdout)
	}
}

// TestAdapterInstall_AlreadyInstalledWithoutForce verifies that an
// existing adapter is left untouched and the command reports it
// informatively with exit 0 (non-fatal).
//
// Reference: TS-007-037 AC-1, §3
func TestAdapterInstall_AlreadyInstalledWithoutForce(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	writeTestFile(t, dir, "anvil-adapter-laravel", "existing binary")
	stubAdapterBinaryOps(t, binaryOps{
		download: func(assetName, tmpPath string) (string, error) {
			t.Fatalf("download must not run when already installed without --force")
			return "", nil
		},
		verify:  func(assetName, downloadedHash string) error { return nil },
		replace: func(tmpPath, targetPath string) error { return nil },
	})

	_, stdout, stderr, err := executeCommand("adapter", "install", "laravel")
	if err != nil {
		t.Fatalf("adapter install returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	if !strings.Contains(stdout, "already installed") || !strings.Contains(stdout, "--force") {
		t.Errorf("stdout should report already-installed state with --force hint, got:\n%s", stdout)
	}
	verifyTestFileContent(t, filepath.Join(dir, "anvil-adapter-laravel"), "existing binary")
}

// TestAdapterInstall_ForceReinstalls verifies that --force re-downloads,
// re-verifies, and replaces an existing adapter.
//
// Reference: TS-007-037 AC-1, §3
func TestAdapterInstall_ForceReinstalls(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	writeTestFile(t, dir, "anvil-adapter-laravel", "existing binary")
	stubAdapterBinaryOps(t, installOps(t, "fresh binary payload", nil))

	_, stdout, stderr, err := executeCommand("adapter", "install", "laravel", "--force")
	if err != nil {
		t.Fatalf("adapter install --force returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	verifyTestFileContent(t, filepath.Join(dir, "anvil-adapter-laravel"), "fresh binary payload")
	if !strings.Contains(stdout, "Adapter laravel installed") {
		t.Errorf("stdout should confirm reinstallation, got:\n%s", stdout)
	}
}

// TestAdapterInstall_ChecksumMismatch verifies that a tampered/corrupt
// download is caught: the command fails, the replace never happens, and
// an existing binary survives.
//
// Reference: TS-007-037 AC-7
func TestAdapterInstall_ChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	writeTestFile(t, dir, "anvil-adapter-laravel", "existing binary")
	stubAdapterBinaryOps(t, installOps(t, "tampered payload", errors.New("hash mismatch")))

	_, _, stderr, err := executeCommand("adapter", "install", "laravel", "--force")
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
	if !strings.Contains(stderr, "checksum verification failed") {
		t.Errorf("stderr should mention checksum verification, got: %s", stderr)
	}
	verifyTestFileContent(t, filepath.Join(dir, "anvil-adapter-laravel"), "existing binary")
}

// TestAdapterInstall_JSON verifies the --json envelope: a success object
// with adapter/status/path/message under the standard envelope (TS-P8-05).
//
// Reference: TS-007-037 AC-6
func TestAdapterInstall_JSON(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	stubAdapterBinaryOps(t, installOps(t, "payload", nil))

	_, stdout, stderr, err := executeCommand("adapter", "install", "flutter", "--json")
	if err != nil {
		t.Fatalf("adapter install --json returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	var envelope output.OutputEnvelope
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	if envelope.Status != "success" {
		t.Errorf("envelope status = %q, want %q", envelope.Status, "success")
	}

	raw, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("marshal envelope data: %v", err)
	}
	var result adapterBinaryResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("envelope data is not an install result: %v\n%s", err, raw)
	}
	if result.Adapter != "flutter" {
		t.Errorf("result.adapter = %q, want %q", result.Adapter, "flutter")
	}
	if result.Status != "installed" {
		t.Errorf("result.status = %q, want %q", result.Status, "installed")
	}
	wantPath := filepath.Join(dir, "anvil-adapter-flutter")
	if result.Path != wantPath {
		t.Errorf("result.path = %q, want %q", result.Path, wantPath)
	}
	if result.Message == "" {
		t.Error("result.message should not be empty")
	}
}

// TestAdapterInstall_AlreadyInstalledJSON verifies the JSON shape for the
// already-installed gate (still a success envelope, exit 0).
//
// Reference: TS-007-037 AC-6
func TestAdapterInstall_AlreadyInstalledJSON(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	writeTestFile(t, dir, "anvil-adapter-laravel", "existing")

	_, stdout, stderr, err := executeCommand("adapter", "install", "laravel", "--json")
	if err != nil {
		t.Fatalf("adapter install --json returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	var envelope output.OutputEnvelope
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	raw, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("marshal envelope data: %v", err)
	}
	var result adapterBinaryResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("envelope data is not an install result: %v\n%s", err, raw)
	}
	if result.Status != "already installed" {
		t.Errorf("result.status = %q, want %q", result.Status, "already installed")
	}
	if !strings.Contains(result.Message, "--force") {
		t.Errorf("result.message should hint --force, got: %s", result.Message)
	}
}

// TestAdapterInstall_UnknownNameJSON verifies that an unknown adapter
// produces the error envelope with --json (errors are conveyed through
// the machine-readable envelope, TS-P8-05).
//
// Reference: TS-007-037 AC-3, AC-6
func TestAdapterInstall_UnknownNameJSON(t *testing.T) {
	stubKnownFrameworks(t, []string{"laravel", "flutter"})
	stubAdapterInstallDirAt(t, t.TempDir())

	_, stdout, _, err := executeCommand("adapter", "install", "node", "--json")
	if err != nil {
		t.Fatalf("adapter install --json returned unexpected error: %v", err)
	}

	var envelope output.OutputEnvelope
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	if envelope.Status != "error" {
		t.Errorf("envelope status = %q, want %q", envelope.Status, "error")
	}
	if !strings.Contains(envelope.Error, "unknown adapter") {
		t.Errorf("envelope error should mention the unknown adapter, got: %s", envelope.Error)
	}
}

// TestAdapterInstall_AssetNameUsesCurrentPlatform verifies that the
// command derives the asset name for the current platform
// (anvil-adapter-<name>-<goos>-<goarch>).
//
// Reference: TS-007-037 §3
func TestAdapterInstall_AssetNameUsesCurrentPlatform(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)

	var gotAsset string
	stubAdapterBinaryOps(t, binaryOps{
		download: func(assetName, tmpPath string) (string, error) {
			gotAsset = assetName
			return fakeHash("payload"), nil
		},
		verify:  func(assetName, downloadedHash string) error { return nil },
		replace: func(tmpPath, targetPath string) error { return nil },
	})

	if _, _, stderr, err := executeCommand("adapter", "install", "laravel"); err != nil {
		t.Fatalf("adapter install returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	want := fmt.Sprintf("anvil-adapter-laravel-%s-%s", runtime.GOOS, runtime.GOARCH)
	if gotAsset != want {
		t.Errorf("downloaded asset = %q, want %q", gotAsset, want)
	}
}
