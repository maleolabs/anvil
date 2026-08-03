// Package cmd implements the Anvil CLI commands.
//
// Tests for "anvil adapter uninstall" (TS-007-037): known-name
// validation (no accidental file removal), removal of an installed
// binary, the graceful not-installed path (exit 0), and --json output
// validity. No network is involved; the install-directory seam points at
// t.TempDir().
//
// Reference: TS-007-037
package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/output"
)

// TestAdapterUninstall_UnknownName verifies that an unknown adapter name
// is rejected before any filesystem activity — only whitelisted
// "anvil-adapter-*" identifiers can be removed.
//
// Reference: TS-007-037 AC-3, §3, §7
func TestAdapterUninstall_UnknownName(t *testing.T) {
	stubKnownFrameworks(t, []string{"laravel", "flutter"})
	stubAdapterInstallDirAt(t, t.TempDir())

	_, _, stderr, err := executeCommand("adapter", "uninstall", "node")
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

// TestAdapterUninstall_RemovesInstalledBinary verifies that uninstalling
// a present adapter removes the binary from the CLI directory.
//
// Reference: TS-007-037 AC-4
func TestAdapterUninstall_RemovesInstalledBinary(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	writeTestFile(t, dir, "anvil-adapter-laravel", "binary")

	_, stdout, stderr, err := executeCommand("adapter", "uninstall", "laravel")
	if err != nil {
		t.Fatalf("adapter uninstall returned unexpected error: %v (stderr: %s)", err, stderr)
	}

	targetPath := filepath.Join(dir, "anvil-adapter-laravel")
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Errorf("adapter binary still exists after uninstall (stat err: %v)", err)
	}
	if !strings.Contains(stdout, "Adapter laravel uninstalled") {
		t.Errorf("stdout should confirm removal, got:\n%s", stdout)
	}
}

// TestAdapterUninstall_NotInstalledGraceful verifies that uninstalling an
// absent adapter reports an informative message and exits 0 (non-fatal).
//
// Reference: TS-007-037 AC-5
func TestAdapterUninstall_NotInstalledGraceful(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)

	_, stdout, stderr, err := executeCommand("adapter", "uninstall", "laravel")
	if err != nil {
		t.Fatalf("adapter uninstall returned unexpected error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "is not installed") {
		t.Errorf("stdout should report the not-installed state, got:\n%s", stdout)
	}
}

// TestAdapterUninstall_NotInstalledJSON verifies the JSON shape for the
// graceful not-installed path (success envelope, exit 0).
//
// Reference: TS-007-037 AC-5, AC-6
func TestAdapterUninstall_NotInstalledJSON(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)

	_, stdout, stderr, err := executeCommand("adapter", "uninstall", "flutter", "--json")
	if err != nil {
		t.Fatalf("adapter uninstall --json returned unexpected error: %v (stderr: %s)", err, stderr)
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
		t.Fatalf("envelope data is not an uninstall result: %v\n%s", err, raw)
	}
	if result.Adapter != "flutter" {
		t.Errorf("result.adapter = %q, want %q", result.Adapter, "flutter")
	}
	if result.Status != "not installed" {
		t.Errorf("result.status = %q, want %q", result.Status, "not installed")
	}
	if result.Path != filepath.Join(dir, "anvil-adapter-flutter") {
		t.Errorf("result.path = %q, want %q", result.Path, filepath.Join(dir, "anvil-adapter-flutter"))
	}
}

// TestAdapterUninstall_RemovedJSON verifies the JSON shape after a real
// removal.
//
// Reference: TS-007-037 AC-4, AC-6
func TestAdapterUninstall_RemovedJSON(t *testing.T) {
	dir := t.TempDir()
	stubAdapterInstallDirAt(t, dir)
	writeTestFile(t, dir, "anvil-adapter-laravel", "binary")

	_, stdout, stderr, err := executeCommand("adapter", "uninstall", "laravel", "--json")
	if err != nil {
		t.Fatalf("adapter uninstall --json returned unexpected error: %v (stderr: %s)", err, stderr)
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
		t.Fatalf("envelope data is not an uninstall result: %v\n%s", err, raw)
	}
	if result.Status != "uninstalled" {
		t.Errorf("result.status = %q, want %q", result.Status, "uninstalled")
	}
	if result.Message == "" {
		t.Error("result.message should not be empty")
	}
}

// TestAdapterUninstall_UnknownNameJSON verifies the error envelope for an
// unknown adapter with --json (errors are conveyed through the
// machine-readable envelope, TS-P8-05).
//
// Reference: TS-007-037 AC-3, AC-6
func TestAdapterUninstall_UnknownNameJSON(t *testing.T) {
	stubKnownFrameworks(t, []string{"laravel", "flutter"})
	stubAdapterInstallDirAt(t, t.TempDir())

	_, stdout, _, err := executeCommand("adapter", "uninstall", "node", "--json")
	if err != nil {
		t.Fatalf("adapter uninstall --json returned unexpected error: %v", err)
	}

	var envelope output.OutputEnvelope
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	if envelope.Status != "error" {
		t.Errorf("envelope status = %q, want %q", envelope.Status, "error")
	}
}
